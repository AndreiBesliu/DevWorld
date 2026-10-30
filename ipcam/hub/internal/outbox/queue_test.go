package outbox

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// fakeClock is a Clock whose time moves only when a test advances it.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, fakeWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

// Advance moves the time on by d, fires every waiter due by then and
// returns how many it fired.
func (c *fakeClock) Advance(d time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if w.at.After(c.now) {
			kept = append(kept, w)
			continue
		}
		w.ch <- c.now
	}
	fired := len(c.waiters) - len(kept)
	c.waiters = kept
	return fired
}

// Step moves the time on by d at a time until a waiter fires.
func (c *fakeClock) Step(d time.Duration) {
	fired := 0
	for fired == 0 {
		fired = c.Advance(d)
	}
}

// obsEvent is one call that the queue made on its Observer.
type obsEvent struct {
	Kind         string // the method's name
	P            catalog.Placement
	Dest         string
	State        DestState
	Alert        string
	Reason       string
	Attempts     int
	Offset, Size int64
	Ref          storage.ObjectRef
	Calls        storage.CallCounts
	Err          error
	At           time.Time
}

// recObserver records the queue's Observer calls, at its clock's time.
type recObserver struct {
	now    func() time.Time
	mu     sync.Mutex
	events []obsEvent
}

func (r *recObserver) add(e obsEvent) {
	e.At = r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recObserver) Queued(p catalog.Placement) {
	r.add(obsEvent{Kind: "Queued", P: p, Dest: p.DestinationID})
}

func (r *recObserver) Resumed(p catalog.Placement, offset int64) {
	r.add(obsEvent{Kind: "Resumed", P: p, Dest: p.DestinationID, Offset: offset})
}

func (r *recObserver) Progress(p catalog.Placement, confirmedOffset, size int64) {
	r.add(obsEvent{Kind: "Progress", P: p, Dest: p.DestinationID, Offset: confirmedOffset, Size: size})
}

func (r *recObserver) Verified(p catalog.Placement, ref storage.ObjectRef, calls storage.CallCounts) {
	r.add(obsEvent{Kind: "Verified", P: p, Dest: p.DestinationID, Ref: ref, Calls: calls})
}

func (r *recObserver) Failed(p catalog.Placement, reason string, attempts int) {
	r.add(obsEvent{Kind: "Failed", P: p, Dest: p.DestinationID, Reason: reason, Attempts: attempts})
}

func (r *recObserver) DestState(destID string, s DestState, alert string) {
	r.add(obsEvent{Kind: "DestState", Dest: destID, State: s, Alert: alert})
}

func (r *recObserver) LocalError(err error) { r.add(obsEvent{Kind: "LocalError", Err: err}) }

// of returns the events of kind, in order; every event when kind is empty.
func (r *recObserver) of(kind string) []obsEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var events []obsEvent
	for _, e := range r.events {
		if kind == "" || e.Kind == kind {
			events = append(events, e)
		}
	}
	return events
}

// harness is one test's catalog, spool, fake clock and observer.
type harness struct {
	t      *testing.T
	cat    *catalog.DB
	clk    *fakeClock
	obs    *recObserver
	recDir string
	start  time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	cat, err := catalog.Open(t.Context(), filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	clk := newFakeClock()
	return &harness{t: t, cat: cat, clk: clk, obs: &recObserver{now: clk.Now}, recDir: filepath.Join(dir, "rec"), start: clk.Now()}
}

// spoolPath is the spool file of the segment at rel, a path under rec/.
func (h *harness) spoolPath(rel string) string {
	return filepath.Join(h.recDir, filepath.FromSlash(rel))
}

// segment writes data as the spool file of a complete clear segment of
// c1/main named name that starts start after the harness's start, and
// returns its row.
func (h *harness) segment(name string, start time.Duration, data []byte) catalog.Segment {
	h.t.Helper()
	s := h.segmentRow(name, start, int64(len(data)))
	file := h.spoolPath(s.Path)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		h.t.Fatal(err)
	}
	return s
}

// segmentRow writes the complete row of such a segment of size bytes,
// without its file, and returns it.
func (h *harness) segmentRow(name string, start time.Duration, size int64) catalog.Segment {
	h.t.Helper()
	startMs := h.start.Add(start).UnixMilli()
	s := catalog.Segment{Cam: "c1", Profile: "main", Path: "c1/main/2026-10-17/" + name + ".mp4",
		StartMs: startMs, EndMs: startMs + 300_000, Bytes: size, State: catalog.StateComplete}
	if err := h.cat.PutSegment(h.t.Context(), s); err != nil {
		h.t.Fatal(err)
	}
	segs, err := h.cat.Segments(h.t.Context())
	if err != nil {
		h.t.Fatal(err)
	}
	for _, row := range segs {
		if row.Path == s.Path {
			return row
		}
	}
	h.t.Fatalf("no row for %s", s.Path)
	return catalog.Segment{}
}

// enqueue puts s's placement on dest in a transaction of its own and
// returns its row.
func (h *harness) enqueue(s catalog.Segment, dest, class string) catalog.Placement {
	h.t.Helper()
	err := h.cat.WithTx(h.t.Context(), func(tx *sql.Tx) error {
		_, err := catalog.PutPlacementTx(h.t.Context(), tx, PlacementFor(s, dest, class))
		return err
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return h.placementOf(s.ID, dest)
}

// placementOf returns the placement of objectID on dest as the catalog
// holds it now.
func (h *harness) placementOf(objectID, dest string) catalog.Placement {
	h.t.Helper()
	all, err := h.cat.Placements(h.t.Context())
	if err != nil {
		h.t.Fatal(err)
	}
	for _, p := range all {
		if p.ObjectID == objectID && p.DestinationID == dest {
			return p
		}
	}
	h.t.Fatalf("no placement of %s on %s", objectID, dest)
	return catalog.Placement{}
}

// set writes p's state, remote ref, attempts and next attempt over its row.
func (h *harness) set(p catalog.Placement) {
	h.t.Helper()
	was := h.placementOf(p.ObjectID, p.DestinationID)
	p.ID = was.ID
	if err := h.cat.UpdatePlacement(h.t.Context(), p, was.State, was.Bytes); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) hasSession(placementID string) bool {
	h.t.Helper()
	_, _, ok, err := h.cat.UploadSession(h.t.Context(), placementID)
	if err != nil {
		h.t.Fatal(err)
	}
	return ok
}

// verified reports whether every placement of ps, by object and
// destination, is verified.
func (h *harness) verified(ps ...catalog.Placement) func() bool {
	return func() bool {
		all, err := h.cat.Placements(h.t.Context())
		if err != nil {
			h.t.Error(err)
			return true
		}
		for _, p := range ps {
			if !slices.ContainsFunc(all, func(row catalog.Placement) bool {
				return row.ObjectID == p.ObjectID && row.DestinationID == p.DestinationID && row.State == catalog.PlacementVerified
			}) {
				return false
			}
		}
		return true
	}
}

// after reports whether d of fake time has passed since the harness's
// start.
func (h *harness) after(d time.Duration) func() bool {
	return func() bool { return !h.clk.Now().Before(h.start.Add(d)) }
}

// newQueue returns a queue of dests on the harness's catalog, clock, spool
// and observer, with a Rand of 0, after change has changed its Options.
func (h *harness) newQueue(dests map[string]storage.Destination, change ...func(*Options)) *Queue {
	opts := Options{
		Clock: h.clk, Rand: func() float64 { return 0 }, RecDir: h.recDir,
		HubID: "H", HubName: "hub", Observer: h.obs,
	}
	for _, c := range change {
		c(&opts)
	}
	return New(h.cat, dests, opts)
}

// run runs q until until says so, checked each time the queue waits with
// nothing in flight; each such wait moves the fake clock on by step. It
// fails the test after limit of fake time, and when Run returns before its
// context ends or returns an error.
func (h *harness) run(q *Queue, step, limit time.Duration, until func() bool) {
	h.t.Helper()
	ctx, cancel := context.WithCancel(h.t.Context())
	defer cancel()
	deadline := h.clk.Now().Add(limit)
	var timedOut bool
	q.idle = func() {
		switch {
		case until():
			cancel()
		case !h.clk.Now().Before(deadline):
			timedOut = true
			cancel()
		default:
			h.clk.Step(step)
		}
	}
	done := make(chan error, 1)
	go func() { done <- q.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			h.t.Errorf("Run = %v, want nil once its context ends", err)
		}
		if ctx.Err() == nil {
			h.t.Fatal("Run returned before its context ended")
		}
	case <-time.After(2 * time.Minute):
		h.t.Fatal("Run did not return within 2 min")
	}
	if timedOut {
		h.t.Fatalf("not done after %s of fake time (at %s)", limit, h.clk.Now().Sub(h.start))
	}
}

// randomBytes returns n bytes from a ChaCha8 seeded with seed.
func randomBytes(n int, seed byte) []byte {
	b := make([]byte, n)
	_, _ = rand.NewChaCha8([32]byte{seed}).Read(b)
	return b
}

// TestOutbox_UnconfiguredDestinationUntouched: a queue reads, resets and
// writes only its own destinations' rows; another destination's rows stay as
// they are, whatever their state, and are never an error of Run.
func TestOutbox_UnconfiguredDestinationUntouched(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	a := newFakeDest(storage.KindLocal, h.clk.Now)
	s1 := h.segment("s1", 0, randomBytes(fakePart+5, 1))
	s2 := h.segment("s2", time.Minute, randomBytes(2*fakePart, 2))
	s3 := h.segment("s3", 2*time.Minute, randomBytes(100, 3))
	h.enqueue(s1, "dst-gone", catalog.ClassContinuous)
	failed := h.enqueue(s2, "dst-gone", catalog.ClassContinuous)
	failed.State, failed.Attempts, failed.NextAttemptAtMs = catalog.PlacementFailed, 2, h.start.UnixMilli()-1
	h.set(failed)
	uploading := h.enqueue(s3, "dst-gone", catalog.ClassEventClip)
	uploading.State, uploading.Attempts = catalog.PlacementUploading, 1
	h.set(uploading)
	if err := h.cat.PutUploadSession(ctx, uploading.ID, uploading.Bytes, []byte(`{"Offset":50}`), 50); err != nil {
		t.Fatal(err)
	}
	mine := []catalog.Placement{h.enqueue(s1, "dst-a", catalog.ClassContinuous), h.enqueue(s2, "dst-a", catalog.ClassContinuous)}
	gone := func() []catalog.Placement {
		all, err := h.cat.Placements(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return slices.DeleteFunc(all, func(p catalog.Placement) bool { return p.DestinationID != "dst-gone" })
	}
	before := gone()

	q := h.newQueue(map[string]storage.Destination{"dst-a": a})
	h.run(q, 10*time.Second, 11*time.Minute, h.after(10*time.Minute))

	if after := gone(); !slices.Equal(after, before) {
		t.Errorf("dst-gone's rows changed:\n%+v\nwere\n%+v", after, before)
	}
	if !h.hasSession(uploading.ID) {
		t.Error("dst-gone's upload session is gone")
	}
	for _, e := range h.obs.of("") {
		if e.Dest == "dst-gone" {
			t.Errorf("an Observer call for dst-gone: %+v", e)
		}
	}
	if !h.verified(mine...)() {
		t.Errorf("dst-a's placements are not verified: %+v, %+v", h.placementOf(s1.ID, "dst-a"), h.placementOf(s2.ID, "dst-a"))
	}
	if q.State("dst-gone") != "" || q.State("dst-a") != StateOK {
		t.Errorf("State(dst-gone) = %q, State(dst-a) = %q; want none and OK", q.State("dst-gone"), q.State("dst-a"))
	}
}

// TestOutbox_EnqueueInSameTx (S3, P03): Enqueue writes the placement in the
// caller's transaction, so it lives or dies with the segment's row.
func TestOutbox_EnqueueInSameTx(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	q := h.newQueue(map[string]storage.Destination{"dst-a": newFakeDest(storage.KindLocal, h.clk.Now)})
	s := catalog.Segment{ID: "0192d0c4-0000-7000-8000-000000000001", Cam: "c1", Profile: "main", Path: "c1/main/2026-10-17/a.mp4",
		StartMs: h.start.UnixMilli(), EndMs: h.start.UnixMilli() + 1000, Bytes: 10, State: catalog.StateComplete}
	errRollback := errors.New("roll back")
	put := func(result error) error {
		return h.cat.WithTx(ctx, func(tx *sql.Tx) error {
			if err := catalog.PutSegmentTx(ctx, tx, s); err != nil {
				return err
			}
			if err := q.Enqueue(ctx, tx, PlacementFor(s, "dst-a", catalog.ClassContinuous)); err != nil {
				return err
			}
			return result
		})
	}
	count := func() (int, int) {
		segs, err := h.cat.Segments(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ps, err := h.cat.Placements(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(segs), len(ps)
	}
	if err := put(errRollback); !errors.Is(err, errRollback) {
		t.Fatalf("WithTx = %v, want the callback's error", err)
	}
	if segs, ps := count(); segs != 0 || ps != 0 {
		t.Errorf("after a rollback: %d segments and %d placements, want none", segs, ps)
	}
	if err := put(nil); err != nil {
		t.Fatal(err)
	}
	if segs, ps := count(); segs != 1 || ps != 1 {
		t.Errorf("after a commit: %d segments and %d placements, want 1 and 1", segs, ps)
	}
	if p := h.placementOf(s.ID, "dst-a"); p.State != catalog.PlacementPending || p.Bytes != 10 || p.ObjectKind != catalog.ObjectSegment {
		t.Errorf("the placement is %+v, want pending, 10 bytes, of a segment", p)
	}
}

// TestOutbox_ChangedBytesSupersedePending (g1-4): new bytes enqueued while
// a worker uploads the old ones make the worker's late writes change
// nothing: its session is aborted and the row uploads again from 0. The new
// bytes come between a chunk's PutChunk and its PutUploadSession, or between
// the Commit and the verified write.
func TestOutbox_ChangedBytesSupersedePending(t *testing.T) {
	for _, during := range []string{"PutChunk", "Commit"} {
		t.Run("during "+during, func(t *testing.T) { testSupersede(t, during) })
	}
}

func testSupersede(t *testing.T, during string) {
	h := newHarness(t)
	ctx := t.Context()
	f := newFakeDest(storage.KindLocal, h.clk.Now)
	s := h.segment("a", 0, randomBytes(3*fakePart+10, 1))
	h.enqueue(s, "dst-a", catalog.ClassContinuous)
	q := h.newQueue(map[string]storage.Destination{"dst-a": f})
	newData := randomBytes(2*fakePart+77, 2)

	var superseded, atAbort catalog.Placement
	var sessionAtAbort bool
	var hookErr error
	// rewrite is the recorder rewriting the file: its row and placement
	// change in one transaction, as Task 7b's callback does.
	rewrite := func() {
		if err := os.WriteFile(h.spoolPath(s.Path), newData, 0o600); err != nil {
			hookErr = err
			return
		}
		rewritten := s
		rewritten.Bytes = int64(len(newData))
		hookErr = h.cat.WithTx(ctx, func(tx *sql.Tx) error {
			if err := catalog.PutSegmentTx(ctx, tx, rewritten); err != nil {
				return err
			}
			return q.Enqueue(ctx, tx, PlacementFor(rewritten, "dst-a", catalog.ClassContinuous))
		})
		superseded = h.placementOf(s.ID, "dst-a")
	}
	f.stored = func(c fakeCall) {
		if during == "PutChunk" && c.Session == "s1" && c.Off == 0 {
			rewrite()
		}
	}
	f.setFault(func(_ context.Context, c fakeCall) error {
		switch {
		case during == "Commit" && c.Method == "Commit" && c.Session == "s1":
			rewrite()
		case c.Method == "Abort":
			atAbort = h.placementOf(s.ID, "dst-a")
			sessionAtAbort = h.hasSession(atAbort.ID)
		}
		return nil
	})
	h.run(q, time.Second, 5*time.Minute, h.verified(catalog.Placement{ObjectID: s.ID, DestinationID: "dst-a"}))

	if hookErr != nil {
		t.Fatal(hookErr)
	}
	if superseded.State != catalog.PlacementPending || superseded.Bytes != int64(len(newData)) || superseded.Attempts != 0 {
		t.Errorf("after the new bytes, the placement is %+v; want pending with %d bytes", superseded, len(newData))
	}
	aborts := f.callsOf("Abort")
	if len(aborts) != 1 || aborts[0].Session != "s1" {
		t.Fatalf("Abort calls %+v, want one, of the old session s1", aborts)
	}
	if atAbort != superseded || sessionAtAbort {
		t.Errorf("the worker's late writes changed the row to %+v (a session row: %v); want it as superseded, %+v, with no session", atAbort, sessionAtAbort, superseded)
	}
	var s2 []fakeCall
	for _, c := range f.callsOf("PutChunk") {
		if c.Session == "s2" {
			s2 = append(s2, c)
		}
	}
	var sent int64
	for _, c := range s2 {
		sent += c.N
	}
	if len(s2) == 0 || s2[0].Off != 0 || sent != int64(len(newData)) {
		t.Errorf("the new session's chunks %+v, want the new %d bytes from 0", s2, len(newData))
	}
	objects := f.objectMap()
	if len(objects) != 1 {
		t.Fatalf("%d objects, want 1", len(objects))
	}
	for key, data := range objects {
		if !bytes.Equal(data, newData) {
			t.Errorf("object %s holds %d bytes, want the new %d", key, len(data), len(newData))
		}
	}
	p := h.placementOf(s.ID, "dst-a")
	if p.Bytes != int64(len(newData)) || p.Attempts != 0 || h.hasSession(p.ID) {
		t.Errorf("the verified placement %+v (a session row: %v), want the new bytes, no attempt and no session", p, h.hasSession(p.ID))
	}
	if n := len(h.obs.of("Failed")) + len(h.obs.of("LocalError")) + f.count("Delete"); n != 0 {
		t.Errorf("%d Failed, LocalError or Delete calls, want none: %+v", n, h.obs.of(""))
	}
}

// TestOutbox_LostSegmentFailsOnce (g1-4, g2-04, g2-06): a placement whose
// segment is lost, or whose spool file is missing, fails once as
// local_unreadable and is never read again, until the file comes back and
// the placement is put again.
func TestOutbox_LostSegmentFailsOnce(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	f := newFakeDest(storage.KindLocal, h.clk.Now)
	lostData := randomBytes(fakePart+9, 1)
	lost := h.segment("lost", 0, lostData)
	missing := h.segment("missing", time.Minute, randomBytes(100, 2))
	for _, s := range []catalog.Segment{lost, missing} {
		if err := os.Remove(h.spoolPath(s.Path)); err != nil {
			t.Fatal(err)
		}
	}
	lostRow := lost
	lostRow.State = catalog.StateLost
	if err := h.cat.PutSegment(ctx, lostRow); err != nil {
		t.Fatal(err)
	}
	pl := h.enqueue(lost, "dst-a", catalog.ClassContinuous)
	pm := h.enqueue(missing, "dst-a", catalog.ClassContinuous)
	if due, err := h.cat.DuePlacements(ctx, h.start.UnixMilli(), []string{"dst-a"}, 10); err != nil || len(due) != 1 || due[0].ID != pm.ID {
		t.Fatalf("DuePlacements = %+v, %v; want only the missing file's placement", due, err)
	}
	reads := map[string]int{}
	var mu sync.Mutex
	q := h.newQueue(map[string]storage.Destination{"dst-a": f}, func(o *Options) {
		o.ReadSpool = func(path string, off int64, p []byte) (int, error) {
			mu.Lock()
			reads[filepath.Base(path)]++
			mu.Unlock()
			return readSpool(path, off, p)
		}
	})
	h.run(q, 10*time.Second, 11*time.Minute, h.after(10*time.Minute))

	for _, p := range []catalog.Placement{pl, pm} {
		got := h.placementOf(p.ObjectID, "dst-a")
		if got.State != catalog.PlacementFailed || got.NextAttemptAtMs != 0 {
			t.Errorf("placement %s is %+v, want failed and never due again", p.ID, got)
		}
		var fails []obsEvent
		for _, e := range h.obs.of("Failed") {
			if e.P.ID == p.ID {
				fails = append(fails, e)
			}
		}
		if len(fails) != 1 || fails[0].Reason != "local_unreadable" {
			t.Errorf("Failed calls of %s: %+v, want one, local_unreadable", p.ID, fails)
		}
	}
	mu.Lock()
	if reads["missing.mp4"] != 1 || reads["lost.mp4"] != 0 {
		t.Errorf("spool reads %v, want the missing file once and the lost one never", reads)
	}
	mu.Unlock()
	if local := h.obs.of("LocalError"); len(local) != 1 || !errors.Is(local[0].Err, fs.ErrNotExist) ||
		strings.Contains(local[0].Err.Error(), h.recDir) || !strings.Contains(local[0].Err.Error(), "missing.mp4") {
		t.Errorf("LocalError calls %+v, want one ENOENT naming missing.mp4 without its directory", local)
	}
	if n := f.count("PutChunk"); n != 0 {
		t.Errorf("%d PutChunk calls, want none", n)
	}

	// The lost file comes back with the same bytes: in one transaction its
	// row turns complete and its placement is put again, as Task 7b's
	// ReconcileFiles and callback do.
	if err := os.WriteFile(h.spoolPath(lost.Path), lostData, 0o600); err != nil {
		t.Fatal(err)
	}
	err := h.cat.WithTx(ctx, func(tx *sql.Tx) error {
		if err := catalog.PutSegmentTx(ctx, tx, lost); err != nil {
			return err
		}
		return q.Enqueue(ctx, tx, PlacementFor(lost, "dst-a", catalog.ClassContinuous))
	})
	if err != nil {
		t.Fatal(err)
	}
	h.run(q, 10*time.Second, 5*time.Minute, h.verified(pl))
	if got := h.placementOf(missing.ID, "dst-a"); got.State != catalog.PlacementFailed {
		t.Errorf("the missing file's placement is %+v, want still failed", got)
	}
}

// TestOutbox_SpoolReadErrorDueAgain (g2-04): an EIO of the spool fails the
// placement as local_unreadable, due again after Backoff; a repeated
// identical local error is reported once while it lasts.
func TestOutbox_SpoolReadErrorDueAgain(t *testing.T) {
	h := newHarness(t)
	f := newFakeDest(storage.KindLocal, h.clk.Now)
	s := h.segment("a", 0, randomBytes(2*fakePart+1, 1))
	p := h.enqueue(s, "dst-a", catalog.ClassContinuous)
	var reads atomic.Int32
	q := h.newQueue(map[string]storage.Destination{"dst-a": f}, func(o *Options) {
		o.Rand = func() float64 { return 0.5 }
		o.ReadSpool = func(path string, off int64, b []byte) (int, error) {
			if reads.Add(1) <= 3 {
				return 0, &fs.PathError{Op: "read", Path: path, Err: syscall.EIO}
			}
			return readSpool(path, off, b)
		}
	})
	h.run(q, 500*time.Millisecond, 5*time.Minute, h.verified(p))

	fails := h.obs.of("Failed")
	if len(fails) != 3 {
		t.Fatalf("Failed calls %+v, want 3", fails)
	}
	for i, e := range fails {
		wait := time.Duration(e.P.NextAttemptAtMs-e.At.UnixMilli()) * time.Millisecond
		if e.Reason != "local_unreadable" || e.Attempts != i+1 || e.P.State != catalog.PlacementFailed || wait != Backoff(i+1, 0.5) {
			t.Errorf("Failed call %d: %s, %d attempts, %s, due again after %s; want local_unreadable, %d, failed, after %s",
				i, e.Reason, e.Attempts, e.P.State, wait, i+1, Backoff(i+1, 0.5))
		}
	}
	local := h.obs.of("LocalError")
	if len(local) != 1 || !errors.Is(local[0].Err, syscall.EIO) || strings.Contains(local[0].Err.Error(), h.recDir) {
		t.Errorf("LocalError calls %+v, want one EIO without the spool's directory", local)
	}
	// Read from DuePlacements four times, the placement is reported queued
	// once, at its first read.
	if queued := h.obs.of("Queued"); len(queued) != 1 || queued[0].P.ID != p.ID || queued[0].P.State != catalog.PlacementPending {
		t.Errorf("Queued calls %+v, want one, of the pending placement", queued)
	}
}

// TestOutbox_NoSpoolHandleAcrossPutChunk (g2-10): the worker holds no
// handle of the spool file while it sends a chunk, so a rename over the
// file (MoveFileEx on Windows) succeeds during the upload.
func TestOutbox_NoSpoolHandleAcrossPutChunk(t *testing.T) {
	h := newHarness(t)
	f := newFakeDest(storage.KindLocal, h.clk.Now)
	data := randomBytes(3*fakePart+5, 1)
	s := h.segment("a", 0, data)
	p := h.enqueue(s, "dst-a", catalog.ClassContinuous)
	path := h.spoolPath(s.Path)
	var renames int
	var faults []string
	f.setFault(func(_ context.Context, c fakeCall) error {
		if c.Method != "PutChunk" {
			return nil
		}
		if open := openHandles(path); open != "" {
			faults = append(faults, "an open handle during PutChunk: "+open)
		}
		copyPath := path + ".copy"
		if err := os.WriteFile(copyPath, data, 0o600); err != nil {
			faults = append(faults, err.Error())
		}
		if err := os.Rename(copyPath, path); err != nil {
			faults = append(faults, "rename over the spool file: "+err.Error())
		}
		renames++
		return nil
	})
	q := h.newQueue(map[string]storage.Destination{"dst-a": f})
	h.run(q, time.Second, 5*time.Minute, h.verified(p))
	if len(faults) != 0 || renames != 4 {
		t.Errorf("%d renames, faults %q; want 4 and none", renames, faults)
	}
}

// openHandles names a descriptor of this process open on path, on Linux,
// where /proc lists them; elsewhere it finds none.
func openHandles(path string) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name()))
		if err == nil && strings.HasPrefix(target, path) && !strings.HasPrefix(target, path+".") {
			return e.Name() + " → " + target
		}
	}
	return ""
}

// TestOutbox_CancelIsNoFailure (g2-02): a stop during a PutChunk is no
// failure: no Failed, no LocalError, no attempt, and the row stays uploading
// until the next Run resets it and resumes it.
func TestOutbox_CancelIsNoFailure(t *testing.T) {
	h := newHarness(t)
	f := newFakeDest(storage.KindLocal, h.clk.Now)
	data := randomBytes(3*fakePart, 1)
	s := h.segment("a", 0, data)
	p := h.enqueue(s, "dst-a", catalog.ClassContinuous)
	held := make(chan struct{})
	var holds atomic.Int32
	f.setFault(func(ctx context.Context, c fakeCall) error {
		if c.Method == "PutChunk" && c.Off == fakePart && holds.Add(1) == 1 {
			close(held)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	q := h.newQueue(map[string]storage.Destination{"dst-a": f})
	q.idle = func() { h.clk.Step(time.Second) }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- q.Run(ctx) }()
	select {
	case <-held:
	case <-time.After(time.Minute):
		t.Fatal("no PutChunk at the second chunk within 1 min")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v after its context ended, want nil", err)
		}
	case <-time.After(time.Minute):
		t.Fatal("Run did not return within 1 min of its context ending")
	}

	if n := len(h.obs.of("Failed")) + len(h.obs.of("LocalError")); n != 0 {
		t.Errorf("a stop made %d Failed or LocalError calls: %+v", n, h.obs.of(""))
	}
	stopped := h.placementOf(s.ID, "dst-a")
	if stopped.State != catalog.PlacementUploading || stopped.Attempts != p.Attempts {
		t.Errorf("after the stop the placement is %+v, want uploading with %d attempts", stopped, p.Attempts)
	}
	if _, off, ok, err := h.cat.UploadSession(t.Context(), p.ID); err != nil || !ok || off != fakePart {
		t.Errorf("the upload session after the stop: at %d, %v, %v; want the first chunk's end, %d", off, ok, err, fakePart)
	}
	if d := q.dests["dst-a"]; d.failures != 0 || !d.waitUntil.IsZero() {
		t.Errorf("the stop counted %d failures of the destination and a wait until %s, want none", d.failures, d.waitUntil)
	}

	q = h.newQueue(map[string]storage.Destination{"dst-a": f})
	h.run(q, time.Second, 5*time.Minute, h.verified(p))
	resumed := h.obs.of("Resumed")
	if len(resumed) != 1 || resumed[0].Offset != fakePart {
		t.Errorf("Resumed calls %+v, want one at %d", resumed, fakePart)
	}
	if n := f.count("Commit"); n != 1 {
		t.Errorf("%d Commit calls, want 1", n)
	}
	for key, got := range f.objectMap() {
		if !bytes.Equal(got, data) {
			t.Errorf("object %s holds %d bytes, not the segment's %d", key, len(got), len(data))
		}
	}
}
