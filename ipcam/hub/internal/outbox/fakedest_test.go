package outbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"maps"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// fakePart is the fake destination's chunk size: small, so that a test file
// of a few chunks stays small.
const fakePart = 64 << 10

// fakeCall is one call that a fakeDest received, at the time of its clock.
type fakeCall struct {
	Method  string
	Key     storage.ObjectKey
	Session string
	Off, N  int64
	At      time.Time
}

// fakeSession is an upload session of a fakeDest: the bytes it holds, and
// whether Commit made them an object.
type fakeSession struct {
	key       storage.ObjectKey
	size      int64
	data      []byte
	committed bool
}

// fakeDest is a storage.Destination in memory for the outbox's tests: its
// objects and sessions, the calls it received, and faults that a test
// injects per call. A session survives the client that opened it, as a
// provider's does, so the kill test serves one fakeDest to two processes.
type fakeDest struct {
	kind storage.Kind
	caps storage.Caps
	now  func() time.Time

	mu       sync.Mutex
	objects  map[storage.ObjectKey][]byte
	sessions map[string]*fakeSession
	calls    []fakeCall
	quota    storage.Quota
	sessionN int
	// fault, when set, runs at the start of each call, after the call is
	// logged and without mu held, so it may block or write the catalog; an
	// error it returns fails the call.
	fault func(ctx context.Context, c fakeCall) error
	// stored, when set, runs inside PutChunk once the chunk is stored.
	stored func(c fakeCall)
	// commitRef, when set, changes the ref that Commit reports: a provider
	// that reports another size or hash.
	commitRef func(ref storage.ObjectRef) storage.ObjectRef
}

var _ storage.Destination = (*fakeDest)(nil)

// newFakeDest returns an empty fake of kind that logs its calls at now's
// time. Its Caps declare SHA256, fakePart chunks, no quota and permanent
// deletes; tests change caps before the queue is made.
func newFakeDest(kind storage.Kind, now func() time.Time) *fakeDest {
	return &fakeDest{
		kind:     kind,
		caps:     fakeCaps(),
		now:      now,
		objects:  map[storage.ObjectKey][]byte{},
		sessions: map[string]*fakeSession{},
	}
}

func fakeCaps() storage.Caps {
	return storage.Caps{
		Resumable: storage.ResumeSessionURI,
		PartSize:  fakePart,
		Delete:    storage.DeletePermanent,
		Hashes:    []storage.HashKind{storage.HashSHA256},
	}
}

// setFault sets the fault of every later call.
func (f *fakeDest) setFault(fault func(ctx context.Context, c fakeCall) error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fault = fault
}

// setQuota sets what Quota reports from now on.
func (f *fakeDest) setQuota(q storage.Quota) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quota = q
}

// log records c and runs the fault.
func (f *fakeDest) log(ctx context.Context, c fakeCall) error {
	if f.now != nil {
		c.At = f.now()
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	fault := f.fault
	f.mu.Unlock()
	if fault != nil {
		return fault(ctx, c)
	}
	return nil
}

// callsOf returns the calls of method, in order; every call when method is
// empty.
func (f *fakeDest) callsOf(method string) []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var calls []fakeCall
	for _, c := range f.calls {
		if method == "" || c.Method == method {
			calls = append(calls, c)
		}
	}
	return calls
}

func (f *fakeDest) count(method string) int { return len(f.callsOf(method)) }

// objectMap returns a copy of the objects.
func (f *fakeDest) objectMap() map[storage.ObjectKey][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.objects)
}

func (f *fakeDest) Kind() storage.Kind         { return f.kind }
func (f *fakeDest) Capabilities() storage.Caps { return f.caps }

func (f *fakeDest) Probe(context.Context) (storage.ProbeReport, error) {
	return storage.ProbeReport{}, storage.ErrPermanent
}

func (f *fakeDest) BeginUpload(ctx context.Context, key storage.ObjectKey, size int64, _ storage.Meta) (storage.UploadState, error) {
	if err := f.log(ctx, fakeCall{Method: "BeginUpload", Key: key, N: size}); err != nil {
		return storage.UploadState{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionN++
	id := fmt.Sprintf("s%d", f.sessionN)
	f.sessions[id] = &fakeSession{key: key, size: size}
	return storage.UploadState{Key: key, Size: size, Session: id, Calls: storage.CallCounts{"begin": 1}}, nil
}

func (f *fakeDest) PutChunk(ctx context.Context, st *storage.UploadState, off int64, r io.Reader, n int64) error {
	data, err := io.ReadAll(io.LimitReader(r, n))
	if err != nil {
		return err
	}
	c := fakeCall{Method: "PutChunk", Key: st.Key, Session: st.Session, Off: off, N: n}
	if err := f.log(ctx, c); err != nil {
		return err
	}
	f.mu.Lock()
	s := f.sessions[st.Session]
	switch {
	case s == nil:
		f.mu.Unlock()
		return fmt.Errorf("fake: no session %s: %w", st.Session, storage.ErrSessionExpired)
	case int64(len(data)) != n || off > int64(len(s.data)) || off+n > s.size:
		f.mu.Unlock()
		return fmt.Errorf("fake: %d bytes at %d of a session holding %d of %d: %w", len(data), off, len(s.data), s.size, storage.ErrPermanent)
	}
	s.data = append(s.data[:off], data...)
	stored := f.stored
	f.mu.Unlock()
	st.Offset = off + n
	countCall(st, "put")
	if stored != nil {
		stored(c)
	}
	return nil
}

func (f *fakeDest) Resume(ctx context.Context, st *storage.UploadState) (int64, error) {
	if err := f.log(ctx, fakeCall{Method: "Resume", Key: st.Key, Session: st.Session}); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[st.Session]
	if s == nil {
		return 0, fmt.Errorf("fake: no session %s: %w", st.Session, storage.ErrSessionExpired)
	}
	st.Offset = int64(len(s.data))
	countCall(st, "status")
	return st.Offset, nil
}

func (f *fakeDest) Commit(ctx context.Context, st *storage.UploadState) (storage.ObjectRef, error) {
	if err := f.log(ctx, fakeCall{Method: "Commit", Key: st.Key, Session: st.Session}); err != nil {
		return storage.ObjectRef{}, err
	}
	f.mu.Lock()
	s := f.sessions[st.Session]
	if s == nil {
		f.mu.Unlock()
		return storage.ObjectRef{}, fmt.Errorf("fake: no session %s: %w", st.Session, storage.ErrSessionExpired)
	}
	if int64(len(s.data)) != s.size {
		f.mu.Unlock()
		return storage.ObjectRef{}, fmt.Errorf("fake: commit of %d of %d bytes: %w", len(s.data), s.size, storage.ErrPermanent)
	}
	s.committed = true
	f.objects[s.key] = bytes.Clone(s.data)
	data, commitRef := s.data, f.commitRef
	f.mu.Unlock()
	kind := f.caps.Hashes[0]
	hash, err := storage.Digest(kind, bytes.NewReader(data))
	if err != nil {
		return storage.ObjectRef{}, err
	}
	st.Offset = st.Size
	countCall(st, "commit")
	ref := storage.ObjectRef{ID: "obj-" + string(st.Key), Key: st.Key, Size: int64(len(data)), Hash: hash, HashKind: kind}
	if commitRef != nil {
		ref = commitRef(ref)
	}
	return ref, nil
}

func (f *fakeDest) Abort(ctx context.Context, st *storage.UploadState) error {
	if err := f.log(ctx, fakeCall{Method: "Abort", Key: st.Key, Session: st.Session}); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := f.sessions[st.Session]; s != nil && !s.committed {
		delete(f.sessions, st.Session)
	}
	return nil
}

func (f *fakeDest) Stat(context.Context, storage.ObjectRef) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, storage.ErrPermanent
}

func (f *fakeDest) OpenRange(context.Context, storage.ObjectRef, int64, int64) (io.ReadCloser, error) {
	return nil, storage.ErrPermanent
}

func (f *fakeDest) PlaybackURL(context.Context, storage.ObjectRef, time.Duration) (string, error) {
	return "", storage.ErrProxyRequired
}

func (f *fakeDest) Delete(ctx context.Context, ref storage.ObjectRef) (storage.DeleteOutcome, error) {
	if err := f.log(ctx, fakeCall{Method: "Delete", Key: ref.Key}); err != nil {
		return storage.DeletedRecycled, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.objects[ref.Key]; !ok {
		return storage.DeletedRecycled, storage.ErrNotFound
	}
	delete(f.objects, ref.Key)
	if f.caps.Delete == storage.DeleteRecycleBin {
		return storage.DeletedRecycled, nil
	}
	return storage.DeletedPermanent, nil
}

func (f *fakeDest) Quota(ctx context.Context) (storage.Quota, error) {
	if err := f.log(ctx, fakeCall{Method: "Quota"}); err != nil {
		return storage.Quota{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.quota, nil
}

func (f *fakeDest) PutSmall(context.Context, storage.ObjectKey, []byte) (storage.ObjectRef, error) {
	return storage.ObjectRef{}, storage.ErrPermanent
}

func (f *fakeDest) List(context.Context, storage.ObjectKey) iter.Seq2[storage.ObjectInfo, error] {
	return func(yield func(storage.ObjectInfo, error) bool) {
		yield(storage.ObjectInfo{}, storage.ErrPermanent)
	}
}

// countCall counts one API call of the upload, as a driver does (P18).
func countCall(st *storage.UploadState, method string) {
	if st.Calls == nil {
		st.Calls = storage.CallCounts{}
	}
	st.Calls[method]++
}
