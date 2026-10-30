// Package outbox uploads the hub's segments to their destinations (§9.7–
// §9.9). A placement is one segment's copy on one destination, a row of the
// catalog written in the segment row's transaction (S3, P03). The queue's
// workers take the due placements, send each spool file in chunks through
// its destination's driver, resume an upload after a restart from its last
// confirmed offset (P25), and verify each copy's size and hash before it
// counts (P34). Each destination has a state (P26): an expired sign-in, a
// full quota or Drive's daily cap pause its uploads, and a destination that
// keeps failing shows as unreachable while it is retried with backoff.
package outbox

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	// pollEvery is how often Run reads the due placements, besides on
	// Enqueue and when an upload ends.
	pollEvery         = time.Second
	defaultWorkers    = 2
	defaultQuotaEvery = 15 * time.Minute // §9.8 step 4
)

// Clock is the queue's time: the system's, or a test's.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// Options are a Queue's settings. Every zero field takes its default.
type Options struct {
	Clock   Clock          // the system clock by default
	Rand    func() float64 // in [0, 1): the backoff's jitter; math/rand/v2's Float64 by default
	Workers int            // uploads at a time per destination (§9.3 parallel_per_destination); 2 by default
	// RecDir is the spool, ipcam-hub's <dataDir>/rec: a segment's Path is
	// relative to it.
	RecDir string
	// HubID and HubName name the objects (§9.10): encrypted copies by ID,
	// clear ones by name.
	HubID, HubName string
	Observer       Observer      // none by default
	QuotaEvery     time.Duration // how often each destination's quota is read; 15 min by default (§9.8 step 4)
	// ReadSpool reads one chunk of the spool file at path into p from off.
	// The default opens the file, reads at off and closes it, so no handle
	// stays open across a network call (g2-10). Tests inject read errors.
	ReadSpool func(path string, off int64, p []byte) (int, error)
}

// Observer hears what the queue does. Its methods are called from the
// queue's goroutines and must not call the Queue.
type Observer interface {
	// Queued: a worker read p from DuePlacements for the first time, so only
	// committed placements are reported.
	Queued(p catalog.Placement)
	// Resumed: p's upload continues at offset, which its provider confirmed.
	Resumed(p catalog.Placement, offset int64)
	// Progress: the provider confirmed confirmedOffset of p's size bytes,
	// and the catalog holds that session.
	Progress(p catalog.Placement, confirmedOffset, size int64)
	// Verified: p's copy matched its size and hash; calls counts the
	// upload's API calls (P18).
	Verified(p catalog.Placement, ref storage.ObjectRef, calls storage.CallCounts)
	// Failed: p's upload failed for reason, one of auth_expired, quota_full,
	// rate_limited, daily_cap, session_expired, not_found, transient,
	// permanent, hash_mismatch and local_unreadable. p is as the catalog now
	// holds it: failed, or pending when its destination paused it.
	Failed(p catalog.Placement, reason string, attempts int)
	// DestState: destID's state is now s, with alert for the owner.
	DestState(destID string, s DestState, alert string)
	// LocalError: an error of the catalog or of the spool, with no directory
	// in it, as recorder's fileError writes them. One that repeats is
	// reported once while it lasts.
	LocalError(err error)
}

// Queue uploads the placements of its destinations. Placements of a
// destination it does not have stay in the catalog untouched.
type Queue struct {
	cat   *catalog.DB
	opts  Options
	dests map[string]*destination
	ids   []string      // the destinations' IDs, sorted
	wake  chan struct{} // a poll is wanted now
	// idle, when a test sets it, is called each time Run is about to wait
	// with no upload or quota read in flight.
	idle func()
	wg   sync.WaitGroup // the uploads and quota reads in flight

	// emitMu keeps the DestState calls in the order of the changes.
	emitMu sync.Mutex

	mu         sync.Mutex
	active     int                  // uploads and quota reads in flight
	inflight   map[string]bool      // the placements that a worker holds
	seen       map[string]bool      // the placements reported Queued
	mismatches map[string]int       // each placement's hash mismatches in this run (P34)
	lastLocal  map[localKind]string // the last local error reported of each kind, while it lasts
}

// New returns the queue of dests, by destination ID, on cat.
func New(cat *catalog.DB, dests map[string]storage.Destination, opts Options) *Queue {
	if opts.Clock == nil {
		opts.Clock = systemClock{}
	}
	if opts.Rand == nil {
		opts.Rand = rand.Float64
	}
	if opts.Workers <= 0 {
		opts.Workers = defaultWorkers
	}
	if opts.QuotaEvery <= 0 {
		opts.QuotaEvery = defaultQuotaEvery
	}
	if opts.ReadSpool == nil {
		opts.ReadSpool = readSpool
	}
	if opts.Observer == nil {
		opts.Observer = noObserver{}
	}
	q := &Queue{
		cat: cat, opts: opts, dests: map[string]*destination{}, wake: make(chan struct{}, 1),
		inflight: map[string]bool{}, seen: map[string]bool{}, mismatches: map[string]int{}, lastLocal: map[localKind]string{},
	}
	for id, d := range dests {
		q.dests[id] = newDestination(id, d)
		q.ids = append(q.ids, id)
	}
	slices.Sort(q.ids)
	return q
}

// Enqueue writes p in the caller's transaction tx (S3), so it lives or dies
// with the segment row written there. A pair that already has a row is
// written as catalog.PutPlacementTx says.
func (q *Queue) Enqueue(ctx context.Context, tx *sql.Tx, p catalog.Placement) error {
	if _, err := catalog.PutPlacementTx(ctx, tx, p); err != nil {
		return err
	}
	q.signal()
	return nil
}

// Run uploads until ctx is done, then waits for its uploads to stop and
// returns nil. Before its first poll it resets every uploading placement of
// its destinations to pending, keeping its attempts and its session, so a
// row that a kill left uploading is due again and resumes. It then polls the
// due placements every second, on Enqueue and when an upload ends.
func (q *Queue) Run(ctx context.Context) error {
	reset := false
	for ctx.Err() == nil {
		if !reset {
			reset = q.resetUploading(ctx)
		}
		if reset {
			q.poll(ctx)
		}
		tick := q.opts.Clock.After(pollEvery)
		if q.idle != nil && q.quiet() {
			q.idle()
		}
		select {
		case <-ctx.Done():
		case <-tick:
		case <-q.wake:
		}
	}
	q.wg.Wait()
	return nil
}

// Unblock is a new sign-in for destID's account: a BLOCKED_AUTH destination
// is OK again.
func (q *Queue) Unblock(destID string) {
	d := q.dests[destID]
	if d == nil {
		return
	}
	q.mu.Lock()
	if d.state == StateBlockedAuth {
		d.set(StateOK, "")
	}
	q.mu.Unlock()
	q.notify(d)
	q.signal()
}

// State is destID's state, or "" for a destination the queue does not have.
func (q *Queue) State(destID string) DestState {
	d := q.dests[destID]
	if d == nil {
		return ""
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return d.state
}

// resetUploading sets every uploading placement of the queue's destinations
// back to pending, and reports whether it could.
func (q *Queue) resetUploading(ctx context.Context) bool {
	all, err := q.cat.Placements(ctx)
	if err != nil {
		q.localError(ctx, localCatalog, err)
		return false
	}
	for _, p := range all {
		if p.State != catalog.PlacementUploading || q.dests[p.DestinationID] == nil {
			continue
		}
		reset := p
		reset.State = catalog.PlacementPending
		if err := q.cat.UpdatePlacement(ctx, reset, p.State, p.Bytes); err != nil && !errors.Is(err, catalog.ErrStalePlacement) {
			q.localError(ctx, localCatalog, err)
			return false
		}
	}
	return true
}

// poll fails the due placements whose segment is lost, starts the quota
// reads that are due, and starts an upload of each due placement that its
// destination has room for.
func (q *Queue) poll(ctx context.Context) {
	now := q.opts.Clock.Now()
	q.failLost(ctx, now)
	q.readQuotas(ctx, now)
	for _, id := range q.ids {
		q.dispatch(ctx, q.dests[id], now)
	}
}

// dispatch starts uploads of d's due placements while d has room for them.
// Each destination reads its own due placements, so another's backlog ahead
// in the queue's order holds none of its workers back.
func (q *Queue) dispatch(ctx context.Context, d *destination, now time.Time) {
	q.mu.Lock()
	slots, busy := d.slots(now, q.opts.Workers), d.busy
	q.mu.Unlock()
	if slots == 0 {
		return
	}
	// A row in flight may be due again, superseded under its worker; it is
	// skipped until that worker lets it go.
	due, err := q.cat.DuePlacements(ctx, now.UnixMilli(), []string{d.id}, slots+busy)
	if err != nil {
		q.localError(ctx, localCatalog, err)
		return
	}
	for _, p := range due {
		if slots == 0 {
			return
		}
		q.mu.Lock()
		held := q.inflight[p.ID]
		first := !held && !q.seen[p.ID]
		if !held {
			slots--
			q.inflight[p.ID], q.seen[p.ID] = true, true
			d.busy++
			q.active++
			q.wg.Add(1)
		}
		q.mu.Unlock()
		if first {
			q.opts.Observer.Queued(p)
		}
		if !held {
			go q.work(ctx, d, p)
		}
	}
}

// failLost makes each due placement whose segment is lost failed and never
// due again (g1-4, g2-04): its spool file is gone.
func (q *Queue) failLost(ctx context.Context, now time.Time) {
	lost, err := q.cat.LostPlacements(ctx, now.UnixMilli(), q.ids)
	if err != nil {
		q.localError(ctx, localCatalog, err)
		return
	}
	for _, p := range lost {
		q.mu.Lock()
		held := q.inflight[p.ID]
		q.mu.Unlock()
		if held {
			continue
		}
		failed := p
		failed.State, failed.NextAttemptAtMs = catalog.PlacementFailed, 0
		if err := q.cat.UpdatePlacement(ctx, failed, p.State, p.Bytes); err != nil {
			if !errors.Is(err, catalog.ErrStalePlacement) {
				q.localError(ctx, localCatalog, err)
			}
			continue
		}
		q.opts.Observer.Failed(failed, reasonLocalUnreadable, failed.Attempts)
	}
}

// readQuotas starts each read of a destination's quota that is due: at the
// first poll, then every QuotaEvery (§9.8 step 4). The destination starts no
// upload while its read is in flight.
func (q *Queue) readQuotas(ctx context.Context, now time.Time) {
	for _, id := range q.ids {
		d := q.dests[id]
		q.mu.Lock()
		due := d.quotaDue(now, q.opts.QuotaEvery)
		if due {
			d.reading, d.quotaAt = true, now
			q.active++
			q.wg.Add(1)
		}
		q.mu.Unlock()
		if due {
			go q.readQuota(ctx, d)
		}
	}
}

func (q *Queue) readQuota(ctx context.Context, d *destination) {
	defer q.done()
	quota, err := d.d.Quota(ctx)
	q.mu.Lock()
	d.reading = false
	if err == nil { // a quota it could not read changes nothing until the next read
		d.quotaRead(quota)
	}
	q.mu.Unlock()
	q.notify(d)
}

// done ends an upload or a quota read: it is no longer in flight, and the
// next poll comes now.
func (q *Queue) done() {
	q.mu.Lock()
	q.active--
	q.mu.Unlock()
	q.signal()
	q.wg.Done()
}

// quiet reports whether nothing is in flight and no poll is wanted.
func (q *Queue) quiet() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.active == 0 && len(q.wake) == 0
}

// signal asks for a poll now.
func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// notify reports d's state to the Observer when it changed since its last
// report.
func (q *Queue) notify(d *destination) {
	q.emitMu.Lock()
	defer q.emitMu.Unlock()
	q.mu.Lock()
	s, alert := d.state, d.alert
	changed := s != d.reported || alert != d.reportedAlert
	d.reported, d.reportedAlert = s, alert
	q.mu.Unlock()
	if changed {
		q.opts.Observer.DestState(d.id, s, alert)
	}
}

// localKind is where a local error comes from.
type localKind int

const (
	localCatalog localKind = iota
	localSpool
)

// localError reports err, an error of the catalog or of the spool, unless it
// is the error of its kind reported last, or ctx is done: a stop is no error
// (g2-02).
func (q *Queue) localError(ctx context.Context, kind localKind, err error) {
	if ctx.Err() != nil {
		return
	}
	msg := err.Error()
	q.mu.Lock()
	repeated := q.lastLocal[kind] == msg
	q.lastLocal[kind] = msg
	q.mu.Unlock()
	if !repeated {
		q.opts.Observer.LocalError(err)
	}
}

// localOK ends the local error of kind, so the same error later is reported
// again.
func (q *Queue) localOK(kind localKind) {
	q.mu.Lock()
	delete(q.lastLocal, kind)
	q.mu.Unlock()
}

// PlacementFor is s's placement on destID in class, as Enqueue takes it. Its
// ID is left to the catalog.
func PlacementFor(s catalog.Segment, destID, class string) catalog.Placement {
	return catalog.Placement{
		ObjectKind: catalog.ObjectSegment, ObjectID: s.ID, DestinationID: destID, Class: class,
		Encrypt: s.Encrypted, State: catalog.PlacementPending, Bytes: s.Bytes,
	}
}

type systemClock struct{}

func (systemClock) Now() time.Time                         { return time.Now() }
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type noObserver struct{}

func (noObserver) Queued(catalog.Placement)                                          {}
func (noObserver) Resumed(catalog.Placement, int64)                                  {}
func (noObserver) Progress(catalog.Placement, int64, int64)                          {}
func (noObserver) Verified(catalog.Placement, storage.ObjectRef, storage.CallCounts) {}
func (noObserver) Failed(catalog.Placement, string, int)                             {}
func (noObserver) DestState(string, DestState, string)                               {}
func (noObserver) LocalError(error)                                                  {}
