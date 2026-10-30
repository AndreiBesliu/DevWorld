package outbox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// maxMismatches is how many uploads of a placement in a run may end in a
// mismatch; after the last it stays failed (P34).
const maxMismatches = 3

// verify compares ref, the committed copy, with the spool file that was
// uploaded: its size, and its digest in the destination's first hash kind
// (P34; drivers do not verify). A match makes the placement verified. A
// mismatch deletes the copy and fails the placement, to be uploaded again
// from 0 after Backoff, at most maxMismatches times.
func (u *upload) verify(ctx context.Context, ref storage.ObjectRef) error {
	if len(u.d.caps.Hashes) == 0 {
		return fmt.Errorf("outbox: %s declares no hash to verify a copy with: %w", u.d.id, storage.ErrPermanent)
	}
	kind := u.d.caps.Hashes[0]
	spool := bufio.NewReaderSize(&spoolReader{u: u, size: u.p.Bytes}, int(chunkSize(u.d.caps)))
	digest, err := storage.Digest(kind, spool)
	if err != nil {
		var local *localErr
		if errors.As(err, &local) {
			return err
		}
		return fmt.Errorf("outbox: %w: %w", storage.ErrPermanent, err)
	}
	if ref.Size != u.p.Bytes || ref.HashKind != kind || ref.Hash != digest {
		return u.mismatch(ctx, ref)
	}
	done := u.p
	done.State, done.RemoteRef, done.RemoteHash = catalog.PlacementVerified, ref.ID, ref.Hash
	if err := u.write(ctx, done); err != nil {
		return err
	}
	if err := u.q.cat.DeleteUploadSession(ctx, done.ID); err != nil {
		u.q.localError(ctx, localCatalog, err)
	}
	u.q.mu.Lock()
	u.d.success()
	delete(u.q.mismatches, done.ID)
	u.q.mu.Unlock()
	u.q.opts.Observer.Verified(done, ref, u.st.Calls)
	u.q.notify(u.d)
	return nil
}

// mismatch fails the placement as hash_mismatch, then deletes the bad copy,
// best effort, and forgets its session. A placement whose bytes changed
// since the upload read them refuses the write: its spool file changed, so
// the mismatch is not its own.
func (u *upload) mismatch(ctx context.Context, ref storage.ObjectRef) error {
	u.q.mu.Lock()
	n := u.q.mismatches[u.p.ID] + 1
	u.q.mu.Unlock()
	p := u.withState(catalog.PlacementFailed)
	p.Attempts++
	p.NextAttemptAtMs = 0
	if n < maxMismatches {
		p.NextAttemptAtMs = u.q.opts.Clock.Now().Add(Backoff(p.Attempts, u.q.opts.Rand())).UnixMilli()
	}
	if err := u.write(ctx, p); err != nil {
		return err
	}
	u.q.mu.Lock()
	u.q.mismatches[p.ID] = n
	u.q.mu.Unlock()
	_, _ = u.d.d.Delete(ctx, ref) // a copy left behind is overwritten by the next upload of its key
	u.dropSession(ctx)
	u.q.opts.Observer.Failed(p, reasonHashMismatch, p.Attempts)
	return nil
}

// spoolReader reads the spool file from 0 to size through ReadSpool, so no
// handle of it stays open.
type spoolReader struct {
	u         *upload
	off, size int64
}

func (r *spoolReader) Read(p []byte) (int, error) {
	if r.off >= r.size {
		return 0, io.EOF
	}
	p = p[:min(int64(len(p)), r.size-r.off)]
	if err := r.u.readSpool(r.off, p); err != nil {
		return 0, err
	}
	r.off += int64(len(p))
	return len(p), nil
}
