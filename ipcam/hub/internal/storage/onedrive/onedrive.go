// Package onedrive is the OneDrive personal storage driver (§9.1, §9.2): its
// own client of Microsoft Graph over net/http (P05), which sets the bearer
// token of each API request from an oauth2.TokenSource on a plain client
// (P27), and none on an upload session's pre-authenticated uploadUrl
// (STO-08).
//
// The account is a personal Microsoft account (Authority) that grants Scope
// only (P36), so the driver sees the app folder, /special/approot, and
// nothing else; every key is a path under it. Graph makes the folders of a
// key's path when an upload first writes there, so the driver creates no
// folder and stores no ID. An upload is an upload session, whose uploadUrl is
// UploadState.Session so that it resumes after a restart (§9.1), sent in
// fragments of PartSize, in order, each but the last a multiple of 320 KiB;
// each session replaces the item at its key (conflictBehavior replace), so a
// key holds one item. Commit returns the item's quickXorHash and size, which
// the outbox compares with the spool file (P34). A deleted item goes to the
// recycle bin, where it keeps occupying the quota (Quota.InBin). Every error
// is a typed error of package storage whose text names the API method, the
// status and Graph's code, never a request's URL (g2-03): the uploadUrl is a
// secret.
package onedrive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/rest"
)

// Authority is the Microsoft identity platform's authority of a personal
// account, and Scope the delegated permissions that the hub asks for (P36):
// the app folder only, and a refresh token.
const (
	Authority = "consumers"
	Scope     = "Files.ReadWrite.AppFolder offline_access"
)

const (
	defaultBaseURL  = "https://graph.microsoft.com"
	defaultPartSize = 10 << 20  // §9.2: 32 × 320 KiB
	fragmentAlign   = 320 << 10 // every fragment but the last is a multiple of it
	maxRequest      = 60<<20 - 1
	jsonType        = "application/json"
	// sessionBody is createUploadSession's body: a new session replaces the
	// item at the key, an earlier copy or one that a cut upload left.
	sessionBody = `{"item":{"@microsoft.graph.conflictBehavior":"replace"}}`
)

// Options configure a OneDrive. Tokens must be safe for concurrent use, as
// oauth2.ReuseTokenSource is.
type Options struct {
	DestID, HubID string
	HTTPClient    *http.Client // plain; the driver sets "Authorization: Bearer" on each API request from Tokens
	Tokens        oauth2.TokenSource
	BaseURL       string // "https://graph.microsoft.com" by default
	PartSize      int64  // 0: 10 MiB; another value only with BaseURL set (the fakes), a multiple of 320 KiB under 60 MiB; Capabilities() reports it
	Clock         func() time.Time
}

// OneDrive is the driver of one OneDrive personal account's app folder. It
// is safe for concurrent use.
type OneDrive struct {
	hubID    string
	client   *http.Client
	tokens   oauth2.TokenSource
	base     string
	partSize int64
	clock    func() time.Time
	// plainHTTP is true when Options.BaseURL was set, for the fakes: their
	// uploadUrls and download URLs may then be http (preauthURL).
	plainHTTP bool

	mu sync.Mutex
	// done holds the item that each complete session made, by uploadUrl,
	// from the last fragment (or a Resume that found it at the key) until
	// Commit or Abort, which then need no call.
	done map[string]driveItem
}

var _ storage.Destination = (*OneDrive)(nil)

// New returns the driver that opts describe. It makes no call.
func New(opts Options) (*OneDrive, error) {
	switch {
	case opts.DestID == "":
		return nil, errors.New("onedrive: Options.DestID is empty")
	case opts.HubID == "" || !validName(opts.HubID):
		return nil, fmt.Errorf("onedrive: Options.HubID %q is not a folder name", opts.HubID)
	case opts.HTTPClient == nil:
		return nil, errors.New("onedrive: Options.HTTPClient is nil")
	case opts.Tokens == nil:
		return nil, errors.New("onedrive: Options.Tokens is nil")
	case opts.PartSize != 0 && opts.BaseURL == "":
		return nil, errors.New("onedrive: Options.PartSize is set for the live API, which takes 10 MiB only")
	case opts.PartSize < 0 || opts.PartSize%fragmentAlign != 0 || opts.PartSize > maxRequest:
		return nil, fmt.Errorf("onedrive: Options.PartSize %d is not a positive multiple of 320 KiB under 60 MiB", opts.PartSize)
	}
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("onedrive: Options.BaseURL is not an http or https URL")
	}
	part := opts.PartSize
	if part == 0 {
		part = defaultPartSize
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	return &OneDrive{
		hubID: opts.HubID, client: opts.HTTPClient, tokens: opts.Tokens, base: base,
		partSize: part, clock: clock, plainHTTP: opts.BaseURL != "", done: map[string]driveItem{},
	}, nil
}

// Kind is KindOneDrivePersonal.
func (*OneDrive) Kind() storage.Kind { return storage.KindOneDrivePersonal }

// Capabilities is OneDrive personal's golden Caps (P24); every other field
// is zero.
func (o *OneDrive) Capabilities() storage.Caps {
	return storage.Caps{
		Resumable: storage.ResumeUploadURL, ChunkAlign: fragmentAlign, PartSize: o.partSize, MaxRequest: maxRequest,
		Playback: storage.LinkPreauthShort, RangeRead: true, Delete: storage.DeleteRecycleBin,
		Quota: storage.QuotaAccount, Hashes: []storage.HashKind{storage.HashQuickXor},
	}
}

// BeginUpload opens an upload session of key (createUploadSession), whose
// item replaces the one at the key when the last fragment arrives. An upload
// session holds one byte at least, so one of 0 bytes is refused before any
// call (storage.Destination).
func (o *OneDrive) BeginUpload(ctx context.Context, key storage.ObjectKey, size int64, _ storage.Meta) (storage.UploadState, error) {
	if err := checkKey(key); err != nil {
		return storage.UploadState{}, err
	}
	if size <= 0 {
		return storage.UploadState{}, fmt.Errorf("onedrive: upload of %s: size %d: an upload session holds one byte at least: %w", key, size, storage.ErrPermanent)
	}
	st := storage.UploadState{Key: key, Size: size, Calls: storage.CallCounts{}}
	a, err := o.send(ctx, call{
		method: methodCreateSession, verb: http.MethodPost, url: o.pathURL(key) + ":/createUploadSession",
		header: map[string]string{"Content-Type": jsonType}, body: strings.NewReader(sessionBody), length: int64(len(sessionBody)),
		calls: st.Calls,
	}, http.StatusOK)
	if err != nil {
		return storage.UploadState{}, err
	}
	var s sessionAnswer
	if err := a.Decode(methodCreateSession, &s); err != nil {
		return storage.UploadState{}, err
	}
	if !o.preauthURL(s.UploadURL) {
		return storage.UploadState{}, fmt.Errorf("onedrive: %s: no uploadUrl that the driver may use in the answer: %w", methodCreateSession, storage.ErrTransient)
	}
	st.Session = s.UploadURL
	return st, nil
}

// PutChunk sends r's n bytes as the fragment from off of st's session
// (upload.put, with Content-Range and no Authorization). The session
// answers 202 with the range that it expects next, which becomes st.Offset,
// or, after the last byte, the item. The driver buffers nothing: the
// request's body is r (C2-R8).
func (o *OneDrive) PutChunk(ctx context.Context, st *storage.UploadState, off int64, r io.Reader, n int64) error {
	if off < 0 || n <= 0 || off+n > st.Size || n > maxRequest {
		return fmt.Errorf("onedrive: upload of %s: bytes [%d, %d) of %d: %w", st.Key, off, off+n, st.Size, storage.ErrPermanent)
	}
	if !o.preauthURL(st.Session) {
		return sessionGone(methodUploadPut)
	}
	if st.Calls == nil {
		st.Calls = storage.CallCounts{}
	}
	a, err := o.send(ctx, call{
		method: methodUploadPut, verb: http.MethodPut, url: st.Session,
		header: map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", off, off+n-1, st.Size)},
		body:   io.LimitReader(r, n), length: n, upload: true, timeout: transferTimeout, calls: st.Calls,
	}, http.StatusAccepted, http.StatusOK, http.StatusCreated)
	if err != nil {
		return err
	}
	if a.StatusCode != http.StatusAccepted {
		var it driveItem
		if err := a.Decode(methodUploadPut, &it); err != nil {
			return err
		}
		if it.ID == "" {
			return fmt.Errorf("onedrive: %s: a complete session with no item ID: %w", methodUploadPut, storage.ErrTransient)
		}
		o.keep(st, it)
		return nil
	}
	var s sessionAnswer
	if err := a.Decode(methodUploadPut, &s); err != nil {
		return err
	}
	next, ok := s.next(st.Size)
	if !ok {
		return fmt.Errorf("onedrive: %s: a 202 with no expected range of the upload: %w", methodUploadPut, storage.ErrTransient)
	}
	st.Offset = next
	return nil
}

// Resume asks st's session for the range it expects next (upload.status, a
// GET of the uploadUrl) and returns its start. Graph forgets a session once
// it completes, so a session that is gone sends Resume to the item at the
// key: one of Size bytes is the upload's, and Resume returns Size.
func (o *OneDrive) Resume(ctx context.Context, st *storage.UploadState) (int64, error) {
	if _, err := o.status(ctx, st); err != nil {
		return 0, err
	}
	return st.Offset, nil
}

// status queries st's session and sets st.Offset, and returns the item that
// the upload made, or nil while the session expects more bytes.
func (o *OneDrive) status(ctx context.Context, st *storage.UploadState) (*driveItem, error) {
	if !o.preauthURL(st.Session) {
		return nil, sessionGone(methodUploadStatus)
	}
	if st.Calls == nil {
		st.Calls = storage.CallCounts{}
	}
	a, err := o.send(ctx, call{method: methodUploadStatus, verb: http.MethodGet, url: st.Session, upload: true, calls: st.Calls}, http.StatusOK)
	if errors.Is(err, storage.ErrSessionExpired) {
		return o.completed(ctx, st, err)
	}
	if err != nil {
		return nil, err
	}
	var s sessionAnswer
	if err := a.Decode(methodUploadStatus, &s); err != nil {
		return nil, err
	}
	next, ok := s.next(st.Size)
	if !ok {
		return nil, fmt.Errorf("onedrive: %s: no expected range of the upload: %w", methodUploadStatus, storage.ErrTransient)
	}
	st.Offset = next
	return nil, nil
}

// completed reads the item at st's key once its session is gone, as gone
// (the status query's ErrSessionExpired) says (items.getByPath,
// $select=id,size,file): an item of Size bytes means that the upload
// completed, whose hash the outbox then checks (P34); none, or one of
// another size, means that the session expired, and gone is the error.
func (o *OneDrive) completed(ctx context.Context, st *storage.UploadState, gone error) (*driveItem, error) {
	a, err := o.send(ctx, call{
		method: methodGetByPath, verb: http.MethodGet, url: o.pathURL(st.Key) + "?$select=id,size,file", calls: st.Calls,
	}, http.StatusOK)
	var it driveItem
	if err == nil {
		err = a.Decode(methodGetByPath, &it)
	}
	switch {
	case errors.Is(err, storage.ErrNotFound):
	case err != nil:
		return nil, err
	case it.ID != "" && it.File != nil && it.Size == st.Size:
		o.keep(st, it)
		return &it, nil
	}
	return nil, fmt.Errorf("%w (no item of its %d bytes at its key)", gone, st.Size)
}

// keep takes it as the item that st's session made: st holds every byte.
func (o *OneDrive) keep(st *storage.UploadState, it driveItem) {
	o.mu.Lock()
	o.done[st.Session] = it
	o.mu.Unlock()
	st.Offset = st.Size
}

// made returns the item that st's session made, if done holds it.
func (o *OneDrive) made(st *storage.UploadState) (driveItem, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	it, ok := o.done[st.Session]
	return it, ok
}

// forget drops st's session from done.
func (o *OneDrive) forget(st *storage.UploadState) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.done, st.Session)
}

// Commit returns the item that st's session made, with its quickXorHash and
// size; the outbox compares them with the spool file (P34). The last
// fragment's answer gives them, or, after a restart, the item at the key; a
// hash that Graph has not given yet is read once more (items.get).
func (o *OneDrive) Commit(ctx context.Context, st *storage.UploadState) (storage.ObjectRef, error) {
	it, ok := o.made(st)
	if !ok {
		found, err := o.status(ctx, st)
		if err != nil {
			return storage.ObjectRef{}, err
		}
		if found == nil {
			return storage.ObjectRef{}, fmt.Errorf("onedrive: commit of %s: the session expects bytes from %d of %d: %w", st.Key, st.Offset, st.Size, storage.ErrTransient)
		}
		it = *found
	}
	if it.hash() == "" {
		read, err := o.item(ctx, it.ID, "id,size,file", st.Calls)
		if err != nil {
			return storage.ObjectRef{}, err
		}
		it = read
	}
	o.forget(st)
	st.Offset = st.Size
	return it.ref(st.Key), nil
}

// Abort ends st's upload. A session that expects more bytes is cancelled
// (upload.cancel, a DELETE of the uploadUrl), which frees the bytes that it
// holds. A complete one has made its item, which Abort leaves at the key:
// the next upload of the key replaces it (conflictBehavior replace), and a
// delete would only move it to the recycle bin, where it would occupy the
// quota for 30 days. A session that is gone needs nothing.
func (o *OneDrive) Abort(ctx context.Context, st *storage.UploadState) error {
	if st.Session == "" || !o.preauthURL(st.Session) {
		return nil
	}
	if _, ok := o.made(st); ok {
		o.forget(st)
		return nil
	}
	a, err := o.send(ctx, call{method: methodUploadCancel, verb: http.MethodDelete, url: st.Session, upload: true, calls: st.Calls},
		http.StatusNoContent, http.StatusOK)
	if errors.Is(err, storage.ErrSessionExpired) {
		return nil
	}
	if err != nil {
		return err
	}
	a.Close()
	return nil
}

// Stat reads ref's item now: its size, quickXorHash and modification time.
func (o *OneDrive) Stat(ctx context.Context, ref storage.ObjectRef) (storage.ObjectInfo, error) {
	it, err := o.item(ctx, ref.ID, "id,size,file,lastModifiedDateTime", nil)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	return storage.ObjectInfo{Ref: it.ref(ref.Key), ModTimeMs: it.Modified.UnixMilli()}, nil
}

// item reads the properties sel of the item id (items.get, $select=sel).
func (o *OneDrive) item(ctx context.Context, id, sel string, calls storage.CallCounts) (driveItem, error) {
	a, err := o.send(ctx, call{method: methodGet, verb: http.MethodGet, url: o.itemURL(id) + "?$select=" + sel, calls: calls}, http.StatusOK)
	if err != nil {
		return driveItem{}, err
	}
	var it driveItem
	err = a.Decode(methodGet, &it)
	return it, err
}

// OpenRange reads n bytes of ref's item from off, or fewer where the item
// ends sooner: /content with a Range, which Graph redirects to the item's
// pre-authenticated download URL, where the client takes the Range and
// leaves the token behind (a host of its own).
func (o *OneDrive) OpenRange(ctx context.Context, ref storage.ObjectRef, off, n int64) (io.ReadCloser, error) {
	if off < 0 || n < 0 {
		return nil, fmt.Errorf("onedrive: read of %s: range %d+%d: %w", ref.Key, off, n, storage.ErrPermanent)
	}
	if n == 0 {
		return io.NopCloser(strings.NewReader("")), nil
	}
	a, err := o.send(ctx, call{
		method: methodContent, verb: http.MethodGet, url: o.itemURL(ref.ID) + "/content",
		header: map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", off, off+n-1)}, timeout: transferTimeout,
	}, http.StatusPartialContent, http.StatusOK)
	if err != nil {
		return nil, err
	}
	// Read errors are ErrTransient, with no URL in their text: not the
	// download URL's either.
	return rest.RangeBody(a, methodContent, off, n)
}

// PlaybackURL returns ref's item's pre-authenticated download URL
// (@microsoft.graph.downloadUrl), which a client plays with no token
// (LinkPreauthShort). Graph keeps it valid for minutes, whatever ttl asks; a
// client that needs it longer asks again. Like the uploadUrl, it is a secret:
// it goes to the client only, never into an error, a log or a result.
func (o *OneDrive) PlaybackURL(ctx context.Context, ref storage.ObjectRef, _ time.Duration) (string, error) {
	it, err := o.item(ctx, ref.ID, "id,@microsoft.graph.downloadUrl", nil)
	if err != nil {
		return "", err
	}
	if !o.preauthURL(it.DownloadURL) {
		return "", fmt.Errorf("onedrive: playback of %s: no download URL that the driver may use: %w", ref.Key, storage.ErrProxyRequired)
	}
	return it.DownloadURL, nil
}

// Delete deletes ref's item (items.delete). OneDrive personal moves it to the
// recycle bin, where it keeps occupying the quota (quota.deleted) for 30
// days or until the owner empties the bin: DeletedRecycled.
func (o *OneDrive) Delete(ctx context.Context, ref storage.ObjectRef) (storage.DeleteOutcome, error) {
	a, err := o.send(ctx, call{method: methodDelete, verb: http.MethodDelete, url: o.itemURL(ref.ID)}, http.StatusNoContent, http.StatusOK)
	if err != nil {
		return storage.DeletedRecycled, err
	}
	a.Close()
	return storage.DeletedRecycled, nil
}

// Quota reads the drive's quota and nothing else ($select=quota, never its
// owner, P45): total, used, remaining as Free, and the recycle bin's bytes
// (deleted) as InBin. A drive with no quota is Unknown.
func (o *OneDrive) Quota(ctx context.Context) (storage.Quota, error) {
	a, err := o.send(ctx, call{method: methodDrive, verb: http.MethodGet, url: o.base + "/v1.0/me/drive?$select=quota"}, http.StatusOK)
	if err != nil {
		return storage.Quota{}, err
	}
	var drive struct {
		Quota *struct {
			Total     int64 `json:"total"`
			Used      int64 `json:"used"`
			Remaining int64 `json:"remaining"`
			Deleted   int64 `json:"deleted"`
		} `json:"quota"`
	}
	if err := a.Decode(methodDrive, &drive); err != nil {
		return storage.Quota{}, err
	}
	q := drive.Quota
	if q == nil || q.Total <= 0 {
		return storage.Quota{Unknown: true}, nil
	}
	return storage.Quota{Total: q.Total, Used: q.Used, Free: max(q.Remaining, 0), InBin: q.Deleted}, nil
}

// PutSmall writes data to key, or atomically replaces the item there, in an
// upload session of one fragment: the item's bytes change when the fragment
// completes, so a reader sees the old bytes or the new. No data is refused
// before any call, as BeginUpload refuses 0 bytes (storage.Destination).
func (o *OneDrive) PutSmall(ctx context.Context, key storage.ObjectKey, data []byte) (storage.ObjectRef, error) {
	if len(data) > maxRequest {
		return storage.ObjectRef{}, fmt.Errorf("onedrive: small write of %s: %d bytes, more than one request takes: %w", key, len(data), storage.ErrPermanent)
	}
	st, err := o.BeginUpload(ctx, key, int64(len(data)), storage.Meta{})
	if err != nil {
		return storage.ObjectRef{}, err
	}
	err = o.PutChunk(ctx, &st, 0, bytes.NewReader(data), int64(len(data)))
	var ref storage.ObjectRef
	if err == nil {
		ref, err = o.Commit(ctx, &st)
	}
	if err != nil {
		_ = o.Abort(context.WithoutCancel(ctx), &st) // the error to report is the write's
		return storage.ObjectRef{}, err
	}
	return ref, nil
}

// List is not built in Phase 0: it serves reconciliation and import only
// (Faza 1), which need Graph's children listing, a call that this driver does
// not make. It yields one error, errors.ErrUnsupported and ErrPermanent.
func (o *OneDrive) List(_ context.Context, prefix storage.ObjectKey) iter.Seq2[storage.ObjectInfo, error] {
	return func(yield func(storage.ObjectInfo, error) bool) {
		yield(storage.ObjectInfo{}, fmt.Errorf("onedrive: list of %q: %w: %w", prefix, errors.ErrUnsupported, storage.ErrPermanent))
	}
}

// sessionGone is the error of a call on a session that is no uploadUrl that
// the driver may use (preauthURL): a new upload starts.
func sessionGone(method string) error {
	return fmt.Errorf("onedrive: %s: the session is not an uploadUrl that the driver may use: %w", method, storage.ErrSessionExpired)
}

// checkKey refuses a key that is not a clean relative path with '/'
// (fs.ValidPath) whose every name OneDrive takes.
func checkKey(key storage.ObjectKey) error {
	k := string(key)
	if !fs.ValidPath(k) || k == "." {
		return fmt.Errorf("onedrive: invalid key %q: %w", key, storage.ErrPermanent)
	}
	for _, name := range strings.Split(k, "/") {
		if !validName(name) {
			return fmt.Errorf("onedrive: invalid key %q: %w", key, storage.ErrPermanent)
		}
	}
	return nil
}

// validName reports whether name is a file or folder name that OneDrive
// takes: not empty, "." or "..", without the characters that it refuses.
func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `"*:<>?/\|`)
}
