package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// The reasons of Observer.Failed, Task 7b's upload_failed.reason.
const (
	reasonAuthExpired     = "auth_expired"
	reasonQuotaFull       = "quota_full"
	reasonRateLimited     = "rate_limited"
	reasonDailyCap        = "daily_cap"
	reasonNotFound        = "not_found"
	reasonTransient       = "transient"
	reasonPermanent       = "permanent"
	reasonHashMismatch    = "hash_mismatch"
	reasonLocalUnreadable = "local_unreadable"
)

// defaultChunk is the chunk size of a destination that declares none.
const defaultChunk = 8 << 20

// maxSessionRestarts is how many new sessions one attempt opens after its
// provider forgot one (§9.9): 3, each from 0. Past them the expiry counts as
// a transient failure, with its backoff, so a provider that keeps forgetting
// sessions does not hold a worker in a loop.
const maxSessionRestarts = 3

// errPaused stops an upload whose destination paused before its next call.
var errPaused = errors.New("outbox: the destination paused")

// localErr is an error of the hub's own disk: of a catalog operation or of a
// spool file, which key names.
type localErr struct {
	key localKey
	err error
}

func (e *localErr) Error() string { return e.err.Error() }
func (e *localErr) Unwrap() error { return e.err }

// spoolErr is a read error of the spool file at path, with the file's name
// and not its directory, as recorder's fileError writes them.
func spoolErr(path string, err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		err = fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	return &localErr{key: localKey{kind: localSpool, name: path}, err: fmt.Errorf("outbox: read %s: %w", filepath.Base(path), err)}
}

// readSpool is Options.ReadSpool's default: it opens the file, reads p at
// off and closes the file.
func readSpool(path string, off int64, p []byte) (int, error) {
	f, err := os.Open(path) //nolint:gosec // G304: a segment's file under the spool, from the catalog
	if err != nil {
		return 0, err
	}
	n, err := f.ReadAt(p, off)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, err
}

// upload is one attempt at one placement. Every catalog write names the
// state and bytes that the upload last wrote or read, so a placement that
// changed under it (ErrStalePlacement) is left to the next poll.
type upload struct {
	q       *Queue
	d       *destination
	p       catalog.Placement // the row as the upload last wrote or read it
	path    string            // the spool file
	st      storage.UploadState
	session bool // st holds a session that BeginUpload or Resume opened
}

// work runs one upload of p on a goroutine of its own.
func (q *Queue) work(ctx context.Context, d *destination, p catalog.Placement) {
	defer func() {
		q.mu.Lock()
		delete(q.inflight, p.ID)
		d.busy--
		q.resetDue = true // the upload may have left its row uploading
		q.mu.Unlock()
		q.done()
	}()
	u := &upload{q: q, d: d, p: p}
	if err := u.run(ctx); err != nil {
		u.fail(ctx, err)
	}
}

// run marks the placement uploading, reads its segment, sends the spool file
// from where its session stands, commits and verifies the copy.
func (u *upload) run(ctx context.Context) error {
	if u.p.Bytes > largeUpload && u.d.caps.Quota != storage.QuotaNone && u.quotaFull(ctx) {
		return nil // QUOTA_FULL: the placement stays as it is, its copy on the hub
	}
	taken := u.p
	taken.State = catalog.PlacementUploading
	if err := u.write(ctx, taken); err != nil {
		return err
	}
	seg, err := u.q.cat.SegmentByID(ctx, u.p.ObjectID)
	if err = u.q.catalogErr(opSegmentByID, err); err != nil {
		return err
	}
	u.path = filepath.Join(u.q.opts.RecDir, filepath.FromSlash(seg.Path))
	key := KeyFor(u.q.opts.HubID, u.q.opts.HubName, seg, u.p.Class)
	meta := storage.Meta{ContentType: "video/mp4", Class: u.p.Class}
	if seg.Encrypted {
		meta.ContentType = "application/octet-stream"
	}
	ref, err := u.transfer(ctx, key, meta)
	// The provider forgot the session (§9.9): a new one from 0, at most
	// maxSessionRestarts times.
	for restarts := 0; errors.Is(err, storage.ErrSessionExpired) && ctx.Err() == nil; restarts++ {
		u.dropSession(ctx)
		if restarts == maxSessionRestarts {
			err = fmt.Errorf("outbox: %s: %d sessions expired in one attempt: %v: %w", key, restarts+1, err, storage.ErrTransient)
			break
		}
		ref, err = u.transfer(ctx, key, meta)
	}
	if err != nil {
		return err
	}
	return u.verify(ctx, ref)
}

// transfer sends the spool file through the placement's session and
// commits it.
func (u *upload) transfer(ctx context.Context, key storage.ObjectKey, meta storage.Meta) (storage.ObjectRef, error) {
	off, err := u.open(ctx, key, meta)
	if err != nil {
		return storage.ObjectRef{}, err
	}
	if err := u.send(ctx, off); err != nil {
		return storage.ObjectRef{}, err
	}
	return u.d.d.Commit(ctx, &u.st)
}

// open resumes the placement's session that the catalog holds (P25), or
// begins a new one and stores it, and returns the offset to send from.
func (u *upload) open(ctx context.Context, key storage.ObjectKey, meta storage.Meta) (int64, error) {
	saved, _, ok, err := u.q.cat.UploadSession(ctx, u.p.ID)
	if err = u.q.catalogErr(opUploadSession, err); err != nil {
		return 0, err
	}
	var st storage.UploadState
	if ok && json.Unmarshal(saved, &st) == nil && st.Key == key && st.Size == u.p.Bytes {
		u.st, u.session = st, true
		next, err := u.d.d.Resume(ctx, &u.st)
		switch {
		case err != nil:
			return 0, err
		case next < 0 || next > u.p.Bytes:
			return 0, fmt.Errorf("outbox: %s resumed at %d of %d bytes: %w", key, next, u.p.Bytes, storage.ErrSessionExpired)
		case next > 0:
			u.q.opts.Observer.Resumed(u.p, next)
		}
		return next, nil
	}
	if st, err = u.d.d.BeginUpload(ctx, key, u.p.Bytes, meta); err != nil {
		return 0, err
	}
	u.st, u.session = st, true
	return 0, u.saveSession(ctx)
}

// send sends the spool file from off to its end, one chunk at a time, and
// stores the session after each chunk that the provider confirmed. Each
// chunk is read with ReadSpool before PutChunk, so no handle of the file is
// open during a network call (g2-10).
func (u *upload) send(ctx context.Context, off int64) error {
	size := u.p.Bytes
	buf := make([]byte, min(chunkSize(u.d.caps), size-off))
	for off < size {
		if u.q.paused(u.d) {
			return errPaused
		}
		chunk := buf[:min(int64(len(buf)), size-off)]
		if err := u.readSpool(off, chunk); err != nil {
			return err
		}
		if err := u.d.d.PutChunk(ctx, &u.st, off, bytes.NewReader(chunk), int64(len(chunk))); err != nil {
			return err
		}
		if u.st.Offset <= off || u.st.Offset > size {
			return fmt.Errorf("outbox: %s confirmed %d bytes after a chunk at %d: %w", u.st.Key, u.st.Offset, off, storage.ErrTransient)
		}
		u.q.answered(u.d)
		off = u.st.Offset
		if err := u.saveSession(ctx); err != nil {
			return err
		}
		u.q.opts.Observer.Progress(u.p, off, size)
	}
	return nil
}

func chunkSize(caps storage.Caps) int64 {
	if caps.PartSize > 0 {
		return caps.PartSize
	}
	return defaultChunk
}

// readSpool reads p from the spool file at off, all of it.
func (u *upload) readSpool(off int64, p []byte) error {
	n, err := u.q.opts.ReadSpool(u.path, off, p)
	switch {
	case n == len(p):
		err = nil
	case err == nil || errors.Is(err, io.EOF):
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		return spoolErr(u.path, err)
	}
	u.q.localOK(localKey{kind: localSpool, name: u.path})
	return nil
}

// saveSession stores the upload's session while the placement is still the
// one the upload holds.
func (u *upload) saveSession(ctx context.Context) error {
	state, err := json.Marshal(u.st)
	if err == nil {
		err = u.q.cat.PutUploadSession(ctx, u.p.ID, u.p.Bytes, state, u.st.Offset)
	}
	return u.q.catalogErr(opPutUploadSession, err)
}

// write writes p over the placement's row while the row is as the upload
// last wrote or read it, and keeps p as the row.
func (u *upload) write(ctx context.Context, p catalog.Placement) error {
	if err := u.q.catalogErr(opUpdatePlacement, u.q.cat.UpdatePlacement(ctx, p, u.p.State, u.p.Bytes)); err != nil {
		return err
	}
	u.p = p
	return nil
}

// settle is write for the end of an upload. A row that changed under the
// upload stays as it is: it was superseded, so the upload's session is
// aborted (g1-4) and the queue forgets the placement. An error of the
// catalog is reported; the row then stays uploading until the next poll
// resets it. It returns the write's error.
func (u *upload) settle(ctx context.Context, p catalog.Placement) error {
	err := u.write(ctx, p)
	switch {
	case errors.Is(err, catalog.ErrStalePlacement):
		u.abort(ctx)
		u.q.forget(u.p.ID)
	case err != nil:
		u.q.localError(ctx, err)
	}
	return err
}

// abort releases the upload's session at its provider, best effort.
func (u *upload) abort(ctx context.Context) {
	if u.session {
		_ = u.d.d.Abort(ctx, &u.st)
		u.session = false
	}
}

// dropSession aborts the upload's session and forgets it, so the next
// upload of the placement starts from 0.
func (u *upload) dropSession(ctx context.Context) {
	u.abort(ctx)
	u.q.catalogFailed(ctx, opDeleteUploadSession, u.q.cat.DeleteUploadSession(ctx, u.p.ID))
}

// quotaFull reads the destination's quota before an upload over 1 GiB
// (§9.8 step 4), and reports whether it is full now. A quota that cannot be
// read holds no upload back.
func (u *upload) quotaFull(ctx context.Context) bool {
	quota, err := u.d.d.Quota(ctx)
	if err != nil {
		return false
	}
	u.q.mu.Lock()
	u.d.quotaRead(quota)
	full := u.d.state == StateQuotaFull
	u.q.mu.Unlock()
	u.q.notify(u.d)
	return full
}

// answered takes a chunk that d confirmed: d answers (P26).
func (q *Queue) answered(d *destination) {
	q.mu.Lock()
	d.answered()
	q.mu.Unlock()
	q.notify(d)
}

// paused reports whether d's uploads in flight must stop now.
func (q *Queue) paused(d *destination) bool {
	now := q.opts.Clock.Now()
	q.mu.Lock()
	defer q.mu.Unlock()
	return d.paused(now)
}

// fail takes the error that ended the upload.
func (u *upload) fail(ctx context.Context, err error) {
	if ctx.Err() != nil {
		return // a stop is no failure (g2-02): the row stays uploading, and Run's reset makes it due again
	}
	var local *localErr
	switch {
	case errors.Is(err, catalog.ErrStalePlacement):
		// The row changed under the upload (g1-4): the next poll uploads it as
		// it is now, from 0 when its bytes changed, as a placement new to the
		// queue.
		u.abort(ctx)
		u.q.forget(u.p.ID)
	case errors.Is(err, errPaused):
		_ = u.settle(ctx, u.withState(catalog.PlacementPending))
	case errors.As(err, &local) && local.key.kind == localSpool:
		u.spoolFailed(ctx, local)
	case errors.As(err, &local):
		u.q.localError(ctx, local)
		if u.p.State == catalog.PlacementUploading {
			_ = u.settle(ctx, u.withState(catalog.PlacementPending))
		}
	default:
		u.destFailed(ctx, err)
	}
}

// withState is the placement as the upload holds it, in state.
func (u *upload) withState(state string) catalog.Placement {
	p := u.p
	p.State = state
	return p
}

// spoolFailed fails the placement as local_unreadable (g2-04): a spool file
// that is gone is never due again; another read error is due again after
// Backoff. A placement whose bytes changed since the upload read them had
// its file rewritten, which is no error.
func (u *upload) spoolFailed(ctx context.Context, err *localErr) {
	gone := errors.Is(err, fs.ErrNotExist)
	p := u.withState(catalog.PlacementFailed)
	if !gone {
		p.Attempts++
		p.NextAttemptAtMs = u.q.opts.Clock.Now().Add(Backoff(p.Attempts, u.q.jitter())).UnixMilli()
	}
	settled := u.settle(ctx, p)
	if errors.Is(settled, catalog.ErrStalePlacement) {
		return
	}
	u.q.localError(ctx, err)
	if settled != nil {
		return
	}
	if gone {
		u.dropSession(ctx)
		u.q.forget(p.ID)
	}
	u.q.opts.Observer.Failed(p, reasonLocalUnreadable, p.Attempts)
}

// destFailed takes an error of the destination (P26). An expired sign-in, a
// full quota and the daily cap pause the destination, and the placement
// waits as pending. A refusal fails the placement for good. Any other error
// fails it until the destination's wait ends: max(Backoff, Retry-After).
func (u *upload) destFailed(ctx context.Context, err error) {
	now := u.q.opts.Clock.Now()
	p := u.p
	resetAfter, capped := dailyCap(err)
	u.q.mu.Lock()
	switch {
	case errors.Is(err, storage.ErrAuthExpired):
		u.d.blockAuth()
		p.State = catalog.PlacementPending
	case errors.Is(err, storage.ErrQuotaFull):
		u.d.full()
		p.State = catalog.PlacementPending
	case capped:
		u.d.capped(now, resetAfter)
		p.State = catalog.PlacementPending
	case errors.Is(err, storage.ErrPermanent):
		p.State, p.NextAttemptAtMs = catalog.PlacementFailed, 0
		p.Attempts++
	default:
		info := storage.Reason(err)
		limited := info.Code == storage.ReasonRateLimited
		wait := u.d.failure(now, u.q.opts.Rand(), info.RetryAfter, !limited)
		p.State, p.NextAttemptAtMs = catalog.PlacementFailed, now.Add(wait).UnixMilli()
		p.Attempts++
	}
	u.q.mu.Unlock()
	if u.settle(ctx, p) == nil {
		if errors.Is(err, storage.ErrPermanent) {
			u.dropSession(ctx)
			u.q.forget(p.ID)
		}
		u.q.opts.Observer.Failed(p, reasonOf(err), p.Attempts)
	}
	u.q.notify(u.d)
}

// reasonOf is the Failed reason of a destination's error.
func reasonOf(err error) string {
	_, capped := dailyCap(err)
	switch {
	case errors.Is(err, storage.ErrAuthExpired):
		return reasonAuthExpired
	case errors.Is(err, storage.ErrQuotaFull):
		return reasonQuotaFull
	case capped:
		return reasonDailyCap
	case storage.Reason(err).Code == storage.ReasonRateLimited:
		return reasonRateLimited
	case errors.Is(err, storage.ErrNotFound):
		return reasonNotFound
	case errors.Is(err, storage.ErrPermanent):
		return reasonPermanent
	}
	return reasonTransient
}

// dailyCap returns the ResetAfter of an ErrDailyCap in err, as a value or a
// pointer, and whether there is one.
func dailyCap(err error) (time.Duration, bool) {
	var v storage.ErrDailyCap
	var p *storage.ErrDailyCap
	switch {
	case errors.As(err, &v):
		return v.ResetAfter, true
	case errors.As(err, &p) && p != nil:
		return p.ResetAfter, true
	}
	return 0, false
}
