// Package gdrive is the Google Drive storage driver (§9.1, §9.2): its own
// REST client of the Drive API v3 over net/http (P05), which sets the
// bearer token of each request from an oauth2.TokenSource on a plain
// client (P27).
//
// Objects live under one folder in My Drive (RootName, "ipcam"), with a
// folder per directory of their key (§9.10). The driver stores each
// folder's ID through a FolderStore before it creates the folder, so a
// restart finds every folder with no files.list, which normal operation
// never calls (STO-07, Review Focus 3). An upload is a resumable session
// whose URI is UploadState.Session, so it resumes after a restart (§9.1),
// sent in chunks of PartSize; Commit reads the file's md5Checksum and size,
// which the outbox compares with the spool file (P34). Creates are held to
// two a second (STO-07). Every error is a typed error of package storage
// whose text names the API method, the status and Drive's reason, never a
// request's URL (g2-03).
package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	defaultBaseURL  = "https://www.googleapis.com"
	defaultRootName = "ipcam"
	defaultPartSize = 16 << 20  // §9.2
	chunkAlign      = 256 << 10 // every chunk but the last is a multiple of it
	dailyUploadCap  = 750 << 30 // bytes a day per user (§9.2)
	createsPerSec   = 2         // ≈ 2 creates a second (§9.2, STO-07)
	jsonType        = "application/json; charset=UTF-8"
)

// Options configure a Drive.
type Options struct {
	DestID     string
	HTTPClient *http.Client // plain; the driver sets "Authorization: Bearer" on each request from Tokens
	Tokens     oauth2.TokenSource
	// BaseURL and UploadBaseURL are the API's, "https://www.googleapis.com"
	// by default; an UploadBaseURL left empty takes BaseURL, so a fake
	// serves both.
	BaseURL, UploadBaseURL string
	PartSize               int64 // 0: 16 MiB; another value only with BaseURL set (the fakes), a multiple of 256 KiB; Capabilities() reports it
	Clock                  func() time.Time
	Folders                FolderStore
	HubID, RootName        string // RootName default "ipcam"
}

// Drive is the driver of one Drive account's folder RootName. It is safe for
// concurrent use.
type Drive struct {
	destID, hubID, rootName string
	client                  *http.Client
	tokens                  oauth2.TokenSource
	base, uploadBase        string
	partSize                int64
	clock                   func() time.Time
	store                   FolderStore
	bucket                  createBucket
	calls                   callLog

	mu sync.Mutex
	// done holds the file that each complete session made, by session URI,
	// from the last chunk (or a status query that found it complete) until
	// Commit or Abort, which then need no status query.
	done map[string]string
}

var _ storage.Destination = (*Drive)(nil)

// New returns the driver that opts describe. It makes no call.
func New(opts Options) (*Drive, error) {
	switch {
	case opts.DestID == "":
		return nil, errors.New("gdrive: Options.DestID is empty")
	case opts.HubID == "":
		return nil, errors.New("gdrive: Options.HubID is empty")
	case opts.HTTPClient == nil:
		return nil, errors.New("gdrive: Options.HTTPClient is nil")
	case opts.Tokens == nil:
		return nil, errors.New("gdrive: Options.Tokens is nil")
	case opts.Folders == nil:
		return nil, errors.New("gdrive: Options.Folders is nil")
	case opts.PartSize != 0 && opts.BaseURL == "":
		return nil, errors.New("gdrive: Options.PartSize is set for the live API, which takes 16 MiB only")
	case opts.PartSize < 0 || opts.PartSize%chunkAlign != 0:
		return nil, fmt.Errorf("gdrive: Options.PartSize %d is not a positive multiple of 256 KiB", opts.PartSize)
	}
	root := opts.RootName
	if root == "" {
		root = defaultRootName
	}
	if strings.ContainsAny(root, `/\`) || root == "." || root == ".." {
		return nil, fmt.Errorf("gdrive: Options.RootName %q is not a folder name", root)
	}
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	upload := strings.TrimRight(opts.UploadBaseURL, "/")
	if upload == "" {
		upload = base
	}
	part := opts.PartSize
	if part == 0 {
		part = defaultPartSize
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Drive{
		destID: opts.DestID, hubID: opts.HubID, rootName: root,
		client: opts.HTTPClient, tokens: opts.Tokens, base: base, uploadBase: upload,
		partSize: part, clock: clock, store: opts.Folders,
		calls: callLog{counts: storage.CallCounts{}}, done: map[string]string{},
	}, nil
}

// Kind is KindGoogleDrive.
func (*Drive) Kind() storage.Kind { return storage.KindGoogleDrive }

// Capabilities is Drive's golden Caps (P24); every other field is zero.
func (d *Drive) Capabilities() storage.Caps {
	return storage.Caps{
		Resumable: storage.ResumeSessionURI, ChunkAlign: chunkAlign, PartSize: d.partSize,
		Playback: storage.LinkProxyOnly, RangeRead: true, Delete: storage.DeletePermanent,
		Quota: storage.QuotaAccount, Hashes: []storage.HashKind{storage.HashMD5},
		DailyUploadCap: dailyUploadCap, CreatesPerSec: createsPerSec,
	}
}

// driveFile is a file's resource, with the fields that the driver asks for.
type driveFile struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	MimeType     string    `json:"mimeType"`
	MD5          string    `json:"md5Checksum"`
	Size         int64     `json:"size,string"`
	ModifiedTime time.Time `json:"modifiedTime"`
}

// ref is f as the ObjectRef of key.
func (f driveFile) ref(key storage.ObjectKey) storage.ObjectRef {
	ref := storage.ObjectRef{ID: f.ID, Key: key, Size: f.Size, Hash: f.MD5}
	if f.MD5 != "" {
		ref.HashKind = storage.HashMD5
	}
	return ref
}

// BeginUpload makes the folders of key that the store does not hold, then
// opens a resumable session for a file named key's base in the last of
// them (files.create.resumable, which takes a create slot). The state's
// Calls count those calls too.
func (d *Drive) BeginUpload(ctx context.Context, key storage.ObjectKey, size int64, meta storage.Meta) (storage.UploadState, error) {
	if err := checkKey(key); err != nil {
		return storage.UploadState{}, err
	}
	if size < 0 {
		return storage.UploadState{}, fmt.Errorf("gdrive: upload of %s: size %d: %w", key, size, storage.ErrPermanent)
	}
	st := storage.UploadState{Key: key, Size: size, Calls: storage.CallCounts{}}
	err := d.into(ctx, key, st.Calls, func(parent string) error {
		var err error
		st.Session, err = d.openSession(ctx, key, size, meta, parent, st.Calls)
		return err
	})
	if err != nil {
		return storage.UploadState{}, err
	}
	return st, nil
}

// openSession opens the resumable session and returns its URI.
func (d *Drive) openSession(ctx context.Context, key storage.ObjectKey, size int64, meta storage.Meta, parent string, calls storage.CallCounts) (string, error) {
	body, err := json.Marshal(map[string]any{"name": path.Base(string(key)), "parents": []string{parent}})
	if err != nil {
		return "", fmt.Errorf("gdrive: %s: %w: %w", methodCreateResumable, storage.ErrPermanent, err)
	}
	header := map[string]string{"Content-Type": jsonType, "X-Upload-Content-Length": strconv.FormatInt(size, 10)}
	if meta.ContentType != "" {
		header["X-Upload-Content-Type"] = meta.ContentType
	}
	a, err := d.send(ctx, call{
		method: methodCreateResumable, verb: http.MethodPost, url: d.uploadBase + "/upload/drive/v3/files?uploadType=resumable",
		header: header, body: bytes.NewReader(body), length: int64(len(body)), create: true, calls: calls,
	}, http.StatusOK)
	if err != nil {
		return "", err
	}
	defer a.Close()
	session := a.Header.Get("Location")
	if !d.ownSession(session) {
		return "", fmt.Errorf("gdrive: %s: no session URI under the upload URL: %w", methodCreateResumable, storage.ErrTransient)
	}
	return session, nil
}

// ownSession reports whether session is a session URI under this driver's
// upload URL, the only place where it sends a session's requests and their
// token.
func (d *Drive) ownSession(session string) bool {
	return strings.HasPrefix(session, d.uploadBase+"/upload/drive/v3/files?")
}

// PutChunk sends r's n bytes as the bytes from off of st's session
// (upload.put, with Content-Range). The session answers 308 with the bytes
// that it holds, which become st.Offset, or, after the last byte, the file.
// The driver buffers nothing: the request's body is r (C2-R8).
func (d *Drive) PutChunk(ctx context.Context, st *storage.UploadState, off int64, r io.Reader, n int64) error {
	if off < 0 || n <= 0 || off+n > st.Size {
		return fmt.Errorf("gdrive: upload of %s: bytes [%d, %d) of %d: %w", st.Key, off, off+n, st.Size, storage.ErrPermanent)
	}
	if !d.ownSession(st.Session) {
		return sessionGone(methodUploadPut)
	}
	if st.Calls == nil {
		st.Calls = storage.CallCounts{}
	}
	a, err := d.send(ctx, call{
		method: methodUploadPut, verb: http.MethodPut, url: st.Session,
		header: map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", off, off+n-1, st.Size)},
		body:   io.LimitReader(r, n), length: n, session: true, timeout: chunkTimeout, calls: st.Calls,
	}, http.StatusOK, http.StatusCreated, http.StatusPermanentRedirect)
	if err != nil {
		return err
	}
	return d.held(methodUploadPut, a, st)
}

// Resume asks st's session how many bytes it holds (upload.status:
// Content-Range bytes */<size>) and returns them; a session that holds
// every byte returns st.Size.
func (d *Drive) Resume(ctx context.Context, st *storage.UploadState) (int64, error) {
	if _, err := d.status(ctx, st); err != nil {
		return 0, err
	}
	return st.Offset, nil
}

// status queries st's session, sets st.Offset, and returns the ID of the
// file that the session made, or "" while it does not hold every byte.
func (d *Drive) status(ctx context.Context, st *storage.UploadState) (string, error) {
	if !d.ownSession(st.Session) {
		return "", sessionGone(methodUploadStatus)
	}
	if st.Calls == nil {
		st.Calls = storage.CallCounts{}
	}
	a, err := d.send(ctx, call{
		method: methodUploadStatus, verb: http.MethodPut, url: st.Session,
		header:  map[string]string{"Content-Range": fmt.Sprintf("bytes */%d", st.Size)},
		session: true, calls: st.Calls,
	}, http.StatusOK, http.StatusCreated, http.StatusPermanentRedirect)
	if err != nil {
		return "", err
	}
	if err := d.held(methodUploadStatus, a, st); err != nil {
		return "", err
	}
	return d.madeFile(st.Session), nil
}

// held takes a session's answer: 308 with the Range "bytes=0-<last>" of the
// bytes that it holds (none without a Range), or 200 or 201 with the file
// once it holds them all. It sets st.Offset, and keeps a complete session's
// file in done.
func (d *Drive) held(method string, a *answer, st *storage.UploadState) error {
	if a.StatusCode == http.StatusPermanentRedirect {
		defer a.Close()
		held := int64(0)
		if rng := a.Header.Get("Range"); rng != "" {
			last, ok := strings.CutPrefix(rng, "bytes=0-")
			n, err := strconv.ParseInt(last, 10, 64)
			if !ok || err != nil || n < 0 || n >= st.Size {
				return fmt.Errorf("gdrive: %s: a 308 with a Range of no bytes of the upload: %w", method, storage.ErrTransient)
			}
			held = n + 1
		}
		st.Offset = held
		return nil
	}
	var file driveFile
	if err := a.decode(method, &file); err != nil {
		return err
	}
	if file.ID == "" {
		return fmt.Errorf("gdrive: %s: a complete session with no file ID: %w", method, storage.ErrTransient)
	}
	d.mu.Lock()
	d.done[st.Session] = file.ID
	d.mu.Unlock()
	st.Offset = st.Size
	return nil
}

// madeFile returns the file that session made, if done holds it.
func (d *Drive) madeFile(session string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.done[session]
}

// forget drops session from done.
func (d *Drive) forget(session string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.done, session)
}

// sessionFile returns the file that st's session made: from done, or else
// from a status query. A session that does not hold every byte has made
// none ("").
func (d *Drive) sessionFile(ctx context.Context, st *storage.UploadState) (string, error) {
	if id := d.madeFile(st.Session); id != "" {
		return id, nil
	}
	return d.status(ctx, st)
}

// Commit returns the file that st's session made, with its md5Checksum and
// size (files.get); the outbox compares them with the spool file (P34).
func (d *Drive) Commit(ctx context.Context, st *storage.UploadState) (storage.ObjectRef, error) {
	id, err := d.sessionFile(ctx, st)
	if err != nil {
		return storage.ObjectRef{}, err
	}
	if id == "" {
		return storage.ObjectRef{}, fmt.Errorf("gdrive: commit of %s: the session holds %d of %d bytes: %w", st.Key, st.Offset, st.Size, storage.ErrTransient)
	}
	f, err := d.file(ctx, id, "id,md5Checksum,size", st.Calls)
	if err != nil {
		return storage.ObjectRef{}, err
	}
	d.forget(st.Session)
	st.Offset = st.Size
	return f.ref(st.Key), nil
}

// Abort ends st's upload. A session that holds every byte has made its
// file, which no verified placement references, as the outbox aborts only
// an upload that it gives up (superseded or refused, g1-4): Abort deletes
// that file, because Drive keeps any number of files of one name in a
// folder and only Commit's ID tells them apart, so a re-upload of the key
// would leave the old copy beside the new one. A session that does not, or
// that is gone, holds no file; Drive drops it a week after its start.
func (d *Drive) Abort(ctx context.Context, st *storage.UploadState) error {
	if st.Session == "" || !d.ownSession(st.Session) {
		return nil
	}
	id, err := d.sessionFile(ctx, st)
	switch {
	case errors.Is(err, storage.ErrSessionExpired):
		return nil
	case err != nil:
		return err
	case id == "":
		return nil
	}
	if err := d.deleteFile(ctx, id, st.Calls); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	d.forget(st.Session)
	return nil
}

// Stat reads ref's file now: its size, md5Checksum and modification time.
func (d *Drive) Stat(ctx context.Context, ref storage.ObjectRef) (storage.ObjectInfo, error) {
	f, err := d.file(ctx, ref.ID, "id,md5Checksum,size,modifiedTime", nil)
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	return storage.ObjectInfo{Ref: f.ref(ref.Key), ModTimeMs: f.ModifiedTime.UnixMilli()}, nil
}

// file reads the fields of the file id (files.get).
func (d *Drive) file(ctx context.Context, id, fields string, calls storage.CallCounts) (driveFile, error) {
	a, err := d.send(ctx, call{
		method: methodGet, verb: http.MethodGet, url: d.base + "/drive/v3/files/" + url.PathEscape(id) + "?fields=" + fields, calls: calls,
	}, http.StatusOK)
	if err != nil {
		return driveFile{}, err
	}
	var f driveFile
	err = a.decode(methodGet, &f)
	return f, err
}

// OpenRange reads n bytes of ref's file from off (files.get.media with a
// Range), or fewer where the file ends sooner.
func (d *Drive) OpenRange(ctx context.Context, ref storage.ObjectRef, off, n int64) (io.ReadCloser, error) {
	if off < 0 || n < 0 {
		return nil, fmt.Errorf("gdrive: read of %s: range %d+%d: %w", ref.Key, off, n, storage.ErrPermanent)
	}
	if n == 0 {
		return io.NopCloser(strings.NewReader("")), nil
	}
	a, err := d.send(ctx, call{
		method: methodGetMedia, verb: http.MethodGet, url: d.base + "/drive/v3/files/" + url.PathEscape(ref.ID) + "?alt=media",
		header: map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", off, off+n-1)}, timeout: chunkTimeout,
	}, http.StatusPartialContent, http.StatusOK)
	if err != nil {
		return nil, err
	}
	m := &media{r: a.Body, a: a}
	if a.StatusCode == http.StatusOK { // the whole file: skip to off
		switch _, err := io.CopyN(io.Discard, m, off); {
		case errors.Is(err, io.EOF): // the file ends before off
			n = 0
		case err != nil:
			a.Close()
			return nil, err
		}
	}
	m.r = io.LimitReader(a.Body, n)
	return m, nil
}

// media is a file's bytes as files.get.media answers them. Its read errors
// are ErrTransient, with no URL in their text.
type media struct {
	r io.Reader
	a *answer
}

func (m *media) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = networkError(methodGetMedia, err)
	}
	return n, err
}

func (m *media) Close() error {
	m.a.Close()
	return nil
}

// PlaybackURL is ErrProxyRequired: a Drive file has no link that a client
// can play without the account's token, so the hub serves it (LinkProxyOnly).
func (*Drive) PlaybackURL(_ context.Context, ref storage.ObjectRef, _ time.Duration) (string, error) {
	return "", fmt.Errorf("gdrive: playback of %s: %w", ref.Key, storage.ErrProxyRequired)
}

// Delete deletes ref's file for good (files.delete, which skips the trash
// for a file that the account owns). A failed Delete claims no freed space.
func (d *Drive) Delete(ctx context.Context, ref storage.ObjectRef) (storage.DeleteOutcome, error) {
	if err := d.deleteFile(ctx, ref.ID, nil); err != nil {
		return storage.DeletedRecycled, err
	}
	return storage.DeletedPermanent, nil
}

func (d *Drive) deleteFile(ctx context.Context, id string, calls storage.CallCounts) error {
	a, err := d.send(ctx, call{
		method: methodDelete, verb: http.MethodDelete, url: d.base + "/drive/v3/files/" + url.PathEscape(id), calls: calls,
	}, http.StatusNoContent, http.StatusOK)
	if err != nil {
		return err
	}
	a.Close()
	return nil
}

// Quota reads the account's storage quota and nothing else (about.get with
// fields=storageQuota, never user, P45): limit, usage, and the trash's usage
// as InBin. An account without a limit is Unknown.
func (d *Drive) Quota(ctx context.Context) (storage.Quota, error) {
	a, err := d.send(ctx, call{method: methodAbout, verb: http.MethodGet, url: d.base + "/drive/v3/about?fields=storageQuota"}, http.StatusOK)
	if err != nil {
		return storage.Quota{}, err
	}
	var about struct {
		StorageQuota struct {
			Limit *int64 `json:"limit,string"`
			Usage int64  `json:"usage,string"`
			Trash int64  `json:"usageInDriveTrash,string"`
		} `json:"storageQuota"`
	}
	if err := a.decode(methodAbout, &about); err != nil {
		return storage.Quota{}, err
	}
	q := about.StorageQuota
	if q.Limit == nil {
		return storage.Quota{Unknown: true}, nil
	}
	return storage.Quota{Total: *q.Limit, Used: q.Usage, Free: max(*q.Limit-q.Usage, 0), InBin: q.Trash}, nil
}

// PutSmall writes data to key in one request, or replaces the bytes of the
// file that an earlier PutSmall wrote there: the store keeps a small file's
// ID by its path, as a folder's, so a README or a catalog stays one file,
// found with no files.list. Its first write gets an ID from
// files.generateIds, stores it and creates the file with it (a multipart
// files.create); a later one updates the file's bytes (files.update), which
// Drive keeps as the file's new revision, a reader seeing the old bytes or
// the new. Either takes a create slot first.
func (d *Drive) PutSmall(ctx context.Context, key storage.ObjectKey, data []byte) (storage.ObjectRef, error) {
	if err := checkKey(key); err != nil {
		return storage.ObjectRef{}, err
	}
	var ref storage.ObjectRef
	err := d.into(ctx, key, nil, func(parent string) error {
		var err error
		ref, err = d.writeSmall(ctx, key, data, parent)
		return err
	})
	return ref, err
}

// writeSmall is PutSmall into the folder parent.
func (d *Drive) writeSmall(ctx context.Context, key storage.ObjectKey, data []byte, parent string) (storage.ObjectRef, error) {
	if wait, ok := d.bucket.take(d.clock()); !ok {
		return storage.ObjectRef{}, rateLimited(methodCreate, wait)
	}
	p := path.Join(d.rootName, string(key))
	id, ok, err := d.stored(ctx, p)
	switch {
	case err != nil:
		return storage.ObjectRef{}, err
	case ok:
		ref, err := d.updateSmall(ctx, id, key, data)
		if !errors.Is(err, storage.ErrNotFound) {
			return ref, err
		}
		// The file is gone: it is created again with its ID.
	default:
		if id, err = d.storeNewID(ctx, p, nil); err != nil {
			return storage.ObjectRef{}, err
		}
	}
	ref, created, err := d.createSmall(ctx, id, key, data, parent)
	if err != nil || created {
		return ref, err
	}
	return d.updateSmall(ctx, id, key, data) // another process created it first
}

// createSmall creates the small file id named key's base in parent with
// data, and reports whether it did: a 409 says that the file exists.
func (d *Drive) createSmall(ctx context.Context, id string, key storage.ObjectKey, data []byte, parent string) (storage.ObjectRef, bool, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	meta, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {jsonType}})
	if err == nil {
		err = json.NewEncoder(meta).Encode(map[string]any{"id": id, "name": path.Base(string(key)), "parents": []string{parent}})
	}
	var content io.Writer
	if err == nil {
		content, err = mw.CreatePart(textproto.MIMEHeader{"Content-Type": {contentType(key)}})
	}
	if err == nil {
		_, err = content.Write(data)
	}
	if err == nil {
		err = mw.Close()
	}
	if err != nil {
		return storage.ObjectRef{}, false, fmt.Errorf("gdrive: %s: %w: %w", methodCreate, storage.ErrPermanent, err)
	}
	a, err := d.send(ctx, call{
		method: methodCreate, verb: http.MethodPost, url: d.uploadBase + "/upload/drive/v3/files?uploadType=multipart&fields=id,md5Checksum,size",
		header: map[string]string{"Content-Type": "multipart/related; boundary=" + mw.Boundary()},
		body:   bytes.NewReader(body.Bytes()), length: int64(body.Len()),
	}, http.StatusOK, http.StatusConflict)
	if err != nil {
		return storage.ObjectRef{}, false, err
	}
	if a.StatusCode == http.StatusConflict {
		a.Close()
		return storage.ObjectRef{}, false, nil
	}
	var f driveFile
	if err := a.decode(methodCreate, &f); err != nil {
		return storage.ObjectRef{}, false, err
	}
	return f.ref(key), true, nil
}

// updateSmall replaces the bytes of the small file id with data.
func (d *Drive) updateSmall(ctx context.Context, id string, key storage.ObjectKey, data []byte) (storage.ObjectRef, error) {
	a, err := d.send(ctx, call{
		method: methodUpdate, verb: http.MethodPatch,
		url:    d.uploadBase + "/upload/drive/v3/files/" + url.PathEscape(id) + "?uploadType=media&fields=id,md5Checksum,size",
		header: map[string]string{"Content-Type": contentType(key)}, body: bytes.NewReader(data), length: int64(len(data)),
	}, http.StatusOK)
	if err != nil {
		return storage.ObjectRef{}, err
	}
	var f driveFile
	if err := a.decode(methodUpdate, &f); err != nil {
		return storage.ObjectRef{}, err
	}
	return f.ref(key), nil
}

// contentType is the MIME type of a small file, by its key's extension.
func contentType(key storage.ObjectKey) string {
	if t := mime.TypeByExtension(path.Ext(string(key))); t != "" {
		return t
	}
	return "application/octet-stream"
}

// List yields the files whose key starts with prefix, walking the folders
// from the deepest one of prefix's directory that the store holds, a page
// of files.list at a time: it serves reconciliation and import, never
// normal operation (STO-07). A directory that the store does not hold is
// one the driver never made, and yields nothing.
func (d *Drive) List(ctx context.Context, prefix storage.ObjectKey) iter.Seq2[storage.ObjectInfo, error] {
	return func(yield func(storage.ObjectInfo, error) bool) {
		dir := ""
		if i := strings.LastIndexByte(string(prefix), '/'); i >= 0 {
			dir = string(prefix[:i])
		}
		id, ok, err := d.stored(ctx, path.Join(d.rootName, dir))
		if err != nil {
			yield(storage.ObjectInfo{}, err)
			return
		}
		if ok {
			d.walk(ctx, id, dir, prefix, yield)
		}
	}
}

// walk yields the files in the folder id, whose key is dir, and in its
// folders, that start with prefix. It reports whether to go on.
func (d *Drive) walk(ctx context.Context, id, dir string, prefix storage.ObjectKey, yield func(storage.ObjectInfo, error) bool) bool {
	q := url.Values{
		"q":        {"'" + id + "' in parents and trashed = false"},
		"fields":   {"nextPageToken,files(id,name,mimeType,md5Checksum,size,modifiedTime)"},
		"pageSize": {"1000"},
	}
	for {
		a, err := d.send(ctx, call{method: methodList, verb: http.MethodGet, url: d.base + "/drive/v3/files?" + q.Encode()}, http.StatusOK)
		var page struct {
			NextPageToken string      `json:"nextPageToken"`
			Files         []driveFile `json:"files"`
		}
		if err == nil {
			err = a.decode(methodList, &page)
		}
		if err != nil {
			yield(storage.ObjectInfo{}, err)
			return false
		}
		for _, f := range page.Files {
			key := path.Join(dir, f.Name)
			switch {
			case !strings.HasPrefix(key, string(prefix)):
			case f.MimeType == folderMimeType:
				if !d.walk(ctx, f.ID, key, prefix, yield) {
					return false
				}
			case !yield(storage.ObjectInfo{Ref: f.ref(storage.ObjectKey(key)), ModTimeMs: f.ModifiedTime.UnixMilli()}, nil):
				return false
			}
		}
		if page.NextPageToken == "" {
			return true
		}
		q.Set("pageToken", page.NextPageToken)
	}
}

// checkKey refuses a key that is not a clean relative path with '/'
// (fs.ValidPath): each of its directories is a folder.
func checkKey(key storage.ObjectKey) error {
	k := string(key)
	if !fs.ValidPath(k) || k == "." || strings.Contains(k, `\`) {
		return fmt.Errorf("gdrive: invalid key %q: %w", key, storage.ErrPermanent)
	}
	return nil
}

// readmeText is the README that Probe writes to .ipcam/<hubID>/README.txt
// (§8.7 step 6).
const readmeText = "Acest folder conține înregistrările unui hub ipcam. Fișierele .age sunt criptate cu age " +
	"(https://age-encryption.org) și se deschid cu: age -d -i <cheia de recuperare> <fișier>.mp4.age > <fișier>.mp4"

const (
	// probeSize is Probe's write, and probeName its object's name in
	// .ipcam/<hubID>.
	probeSize = 1 << 20
	probeName = "probe.bin"
	// probeWaits bounds how many times one step of Probe waits for a create
	// slot.
	probeWaits = 10
	// warnNo409 is Probe's warning when Drive creates an existing folder ID
	// again instead of answering 409.
	warnNo409 = "Google Drive nu a răspuns 409 la crearea din nou a unui dosar existent; " +
		"hub-ul tratează răspunsul ca reușită, dar comportamentul diferă de cel așteptat."
)

// Probe checks the destination (§8.7 step 6, P35): it makes the root and
// stores its ID, creates the root's ID again to confirm Drive's 409 for a
// folder that exists, writes .ipcam/<hubID>/README.txt, uploads 1 MiB,
// reads a range of it back, deletes it and reads the quota. The report holds
// the steps that passed, and the error is the first step that failed. A
// probe whose read fails deletes its object, best effort. Probe is a one-off
// check that makes several creates, so where an upload returns
// ErrRateLimited at once, Probe waits for its slot.
func (d *Drive) Probe(ctx context.Context) (storage.ProbeReport, error) {
	var report storage.ProbeReport
	var root string
	err := waitForSlot(ctx, func() error {
		chain, err := d.folders(ctx, ".", nil)
		if err == nil {
			root = chain[0].id
		}
		return err
	})
	if err != nil {
		return report, err
	}
	for created, i := true, 0; created; i++ {
		if i == 2 {
			report.Warnings = append(report.Warnings, warnNo409)
			break
		}
		// A first 200 means the root was gone and is made again.
		err = waitForSlot(ctx, func() (err error) {
			created, err = d.createFolder(ctx, root, d.rootName, myDrive, true, nil)
			return err
		})
		if err != nil {
			return report, err
		}
	}
	err = waitForSlot(ctx, func() error {
		_, err := d.PutSmall(ctx, storage.ObjectKey(path.Join(".ipcam", d.hubID, "README.txt")), []byte(readmeText))
		return err
	})
	if err != nil {
		return report, err
	}
	data := make([]byte, probeSize)
	for i := range data {
		data[i] = byte(i % 251)
	}
	ref, err := d.probeWrite(ctx, data)
	if err != nil {
		return report, err
	}
	report.Writable = true
	if err := d.probeRange(ctx, ref, data); err != nil {
		_, _ = d.Delete(context.WithoutCancel(ctx), ref) // the error to report is the read's
		return report, err
	}
	report.RangeRead = true
	if _, err := d.Delete(ctx, ref); err != nil {
		return report, err
	}
	report.Deletable = true
	report.Quota, err = d.Quota(ctx)
	return report, err
}

// probeWrite uploads data as Probe's object, through a session as a
// segment's upload, and checks Commit's size and MD5.
func (d *Drive) probeWrite(ctx context.Context, data []byte) (storage.ObjectRef, error) {
	key := storage.ObjectKey(path.Join(".ipcam", d.hubID, probeName))
	var st storage.UploadState
	err := waitForSlot(ctx, func() (err error) {
		st, err = d.BeginUpload(ctx, key, int64(len(data)), storage.Meta{ContentType: "application/octet-stream"})
		return err
	})
	if err != nil {
		return storage.ObjectRef{}, err
	}
	for off := int64(0); off < st.Size && err == nil; off = st.Offset {
		n := min(d.partSize, st.Size-off)
		err = d.PutChunk(ctx, &st, off, bytes.NewReader(data[off:off+n]), n)
		if err == nil && st.Offset <= off {
			err = fmt.Errorf("gdrive: probe: the session holds %d bytes after a chunk at %d: %w", st.Offset, off, storage.ErrTransient)
		}
	}
	var ref storage.ObjectRef
	if err == nil {
		ref, err = d.Commit(ctx, &st)
	}
	if err != nil {
		_ = d.Abort(context.WithoutCancel(ctx), &st) // the error to report is the write's
		return storage.ObjectRef{}, err
	}
	want, err := storage.Digest(storage.HashMD5, bytes.NewReader(data))
	if err == nil && (ref.Size != int64(len(data)) || ref.Hash != want) {
		err = fmt.Errorf("gdrive: probe: Drive holds %d bytes with MD5 %s, want %d with %s: %w", ref.Size, ref.Hash, len(data), want, storage.ErrPermanent)
	}
	if err != nil {
		_, _ = d.Delete(context.WithoutCancel(ctx), ref)
		return storage.ObjectRef{}, err
	}
	return ref, nil
}

// probeRange reads a range of Probe's object, ref, and compares it with the
// same range of data, the bytes that Probe wrote.
func (d *Drive) probeRange(ctx context.Context, ref storage.ObjectRef, data []byte) error {
	const off, n = 1000, 4096
	r, err := d.OpenRange(ctx, ref, off, n)
	if err != nil {
		return err
	}
	got, err := io.ReadAll(r)
	if cerr := r.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(got, data[off:off+n]) {
		return fmt.Errorf("gdrive: probe: a range read returned %d bytes that differ from those written: %w", len(got), storage.ErrPermanent)
	}
	return nil
}

// waitForSlot runs step, and runs it again after each ErrRateLimited that it
// returns once the wait has passed (a second when the error names none), at
// most probeWaits times.
func waitForSlot(ctx context.Context, step func() error) error {
	for i := 0; ; i++ {
		err := step()
		var limited storage.ErrRateLimited
		if !errors.As(err, &limited) || i == probeWaits {
			return err
		}
		wait := limited.RetryAfter
		if wait <= 0 {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
