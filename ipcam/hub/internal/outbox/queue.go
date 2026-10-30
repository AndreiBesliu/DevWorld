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
	"strings"
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
	Clock Clock // the system clock by default
	// Rand returns the backoff's jitter, in [0, 1); math/rand/v2's Float64
	// by default. The queue calls it with its mutex held, so it need not be
	// safe for concurrent use.
	Rand func() float64
	// Workers is how many uploads each destination runs at a time (§9.3
	// parallel_per_destination); 2 by default. It counts per destination, so
	// a backlog on one holds back no other (C2-R8). An upload holds a buffer
	// of Caps.PartSize while it sends and another while it verifies, so peak
	// buffer memory is about destinations × Workers × 2 × Caps.PartSize.
	Workers int
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
	// Queued: a worker read p from DuePlacements for the first time with its
	// bytes, so only committed placements are reported. A placement whose
	// bytes changed (superseded), or that came back after it failed for good,
	// is queued anew.
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
	// rate_limited, daily_cap, not_found, transient, permanent,
	// hash_mismatch and local_unreadable. p is as the catalog now holds it:
	// failed, or pending when its destination paused it.
	Failed(p catalog.Placement, reason string, attempts int)
	// DestState: destID's state is now s, with alert for the owner.
	DestState(destID string, s DestState, alert string)
	// LocalError: an error of the catalog or of the spool, with no directory
	// in it, as recorder's fileError writes them. A spool file, or a catalog
	// operation, that keeps failing is reported once, until it succeeds
	// again.
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

	mu     sync.Mutex
	active int // uploads and quota reads in flight
	// inflight holds the placements that a worker holds. A placement enters
	// it before its worker marks it uploading, so the reset of uploading
	// rows never takes an upload in flight.
	inflight map[string]bool
	// stranded holds the placements of the queue's destinations whose rows
	// are uploading with no worker, each as its row was last read or written:
	// those Run's start read finds (an earlier run of the hub left them so),
	// and those an upload hands over as it ends with its row uploading (a
	// stop, or a catalog error that its reset to pending hit too). Only an
	// upload of this queue writes uploading, and hub.lock keeps any other hub
	// off the catalog, so these are all such rows. Every poll resets them
	// (resetUploading) by their IDs, with no read of every placement; a row
	// leaves once it is reset, or once it is no longer uploading with its
	// bytes.
	stranded map[string]catalog.Placement
	// seen holds the placements reported Queued, each with its bytes then,
	// and mismatches each placement's hash mismatches (P34). A placement
	// leaves both once it is verified or failed for good, and when its
	// bytes change: superseded, it is queued anew.
	seen map[string]int64
	// mismatches are counted in RAM only: a restart forgets them, so each
	// restart allows a placement up to 2 more mismatched uploads, and
	// deletes, than maxMismatches. The report's «Limitări» states it.
	mismatches map[string]int
	failing    map[localKey]bool // the local errors reported, until their file or operation succeeds
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
		inflight: map[string]bool{}, stranded: map[string]catalog.Placement{},
		seen: map[string]int64{}, mismatches: map[string]int{}, failing: map[localKey]bool{},
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
// returns nil. It polls the due placements every second, on Enqueue and when
// an upload ends. Each poll first resets every uploading placement of its
// destinations that no worker holds to pending, keeping its attempts and its
// session: a row that a kill or a stop left uploading, or that an upload
// could not settle (a full card fails both writes), is due again and
// resumes. Run finds the rows that an earlier run left so in one read of
// every placement before its first poll, repeated before each poll until it
// succeeds; an upload of this queue that ends with its row uploading hands
// that row to the polls (Queue.stranded).
func (q *Queue) Run(ctx context.Context) error {
	found := false // the start read of the uploading rows succeeded
	for ctx.Err() == nil {
		if !found {
			found = q.findUploading(ctx)
		}
		q.poll(ctx)
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

// findUploading is Run's start read: it reads every placement and adds each
// uploading one of the queue's destinations that no worker holds to
// stranded, for the polls to reset. It reports whether the read succeeded.
func (q *Queue) findUploading(ctx context.Context) bool {
	all, err := q.cat.Placements(ctx)
	if q.catalogFailed(ctx, opPlacements, err) {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, p := range all {
		if p.State == catalog.PlacementUploading && q.dests[p.DestinationID] != nil && !q.inflight[p.ID] {
			q.stranded[p.ID] = p
		}
	}
	return true
}

// resetUploading sets every uploading placement of the queue's destinations
// that no worker holds, the rows in stranded, back to pending, keeping its
// attempts and its session. Each write names the row's uploading state and
// bytes, so a row that changed since (settled, or superseded) is left as it
// is. A catalog error keeps the rows for the next poll. Only this goroutine
// starts workers, so none takes such a row meanwhile.
func (q *Queue) resetUploading(ctx context.Context) {
	q.mu.Lock()
	var rows []catalog.Placement
	for id, p := range q.stranded {
		if !q.inflight[id] {
			rows = append(rows, p)
		}
	}
	q.mu.Unlock()
	slices.SortFunc(rows, func(a, b catalog.Placement) int { return strings.Compare(a.ID, b.ID) })
	for _, p := range rows {
		reset := p
		reset.State = catalog.PlacementPending
		if q.catalogFailed(ctx, opUpdatePlacement, q.cat.UpdatePlacement(ctx, reset, p.State, p.Bytes)) {
			return
		}
		q.mu.Lock()
		delete(q.stranded, p.ID)
		q.mu.Unlock()
	}
}

// poll resets the uploading placements that no worker holds, fails the due
// placements whose segment is lost, starts the quota reads that are due, and
// starts an upload of each due placement that its destination has room for.
func (q *Queue) poll(ctx context.Context) {
	now := q.opts.Clock.Now()
	q.resetUploading(ctx)
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
	if q.catalogFailed(ctx, opDuePlacements, err) {
		return
	}
	for _, p := range due {
		if slots == 0 {
			return
		}
		q.mu.Lock()
		held := q.inflight[p.ID]
		bytes, seen := q.seen[p.ID]
		first := !held && (!seen || bytes != p.Bytes)
		if first {
			// New to the queue, or superseded: its mismatches were of other
			// bytes.
			q.seen[p.ID] = p.Bytes
			delete(q.mismatches, p.ID)
		}
		if !held {
			slots--
			q.inflight[p.ID] = true
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
	if q.catalogFailed(ctx, opLostPlacements, err) {
		return
	}
	for _, p := range lost {
		if q.held(p.ID) {
			continue
		}
		failed := p
		failed.State, failed.NextAttemptAtMs = catalog.PlacementFailed, 0
		if err := q.catalogErr(opUpdatePlacement, q.cat.UpdatePlacement(ctx, failed, p.State, p.Bytes)); err != nil {
			q.localError(ctx, err) // ErrStalePlacement is none: the row changed since the read
			continue
		}
		q.forget(p.ID)
		q.opts.Observer.Failed(failed, reasonLocalUnreadable, failed.Attempts)
	}
}

// held reports whether a worker holds the placement id.
func (q *Queue) held(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.inflight[id]
}

// forget drops what the queue keeps in RAM of the placement id: it is
// verified, failed for good or superseded.
func (q *Queue) forget(id string) {
	q.mu.Lock()
	delete(q.seen, id)
	delete(q.mismatches, id)
	q.mu.Unlock()
}

// jitter draws Options.Rand with the queue's mutex held.
func (q *Queue) jitter() float64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.opts.Rand()
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

// localKey names what a local error is of: a spool file, by its path, or a
// catalog operation.
type localKey struct {
	kind localKind
	name string
}

// The catalog operations of the queue, by which their local errors are told
// apart.
const (
	opPlacements          = "Placements"
	opDuePlacements       = "DuePlacements"
	opLostPlacements      = "LostPlacements"
	opUpdatePlacement     = "UpdatePlacement"
	opSegmentByID         = "SegmentByID"
	opUploadSession       = "UploadSession"
	opPutUploadSession    = "PutUploadSession"
	opDeleteUploadSession = "DeleteUploadSession"
)

// catalogErr takes err, what the catalog operation op returned. A success,
// or ErrStalePlacement, which the catalog returns as it should, ends op's
// local error; any other error is returned as op's *localErr.
func (q *Queue) catalogErr(op string, err error) error {
	key := localKey{kind: localCatalog, name: op}
	if err == nil || errors.Is(err, catalog.ErrStalePlacement) {
		q.localOK(key)
		return err
	}
	return &localErr{key: key, err: err}
}

// catalogFailed reports err, what the catalog operation op returned, and
// whether op failed; ErrStalePlacement is no failure of the catalog.
func (q *Queue) catalogFailed(ctx context.Context, op string, err error) bool {
	if err = q.catalogErr(op, err); err == nil || errors.Is(err, catalog.ErrStalePlacement) {
		return false
	}
	q.localError(ctx, err)
	return true
}

// localError reports err, a *localErr, unless the error of its file or
// operation is already reported and that file or operation has not
// succeeded since, or ctx is done: a stop is no error (g2-02).
func (q *Queue) localError(ctx context.Context, err error) {
	var local *localErr
	if ctx.Err() != nil || !errors.As(err, &local) {
		return
	}
	q.mu.Lock()
	reported := q.failing[local.key]
	q.failing[local.key] = true
	q.mu.Unlock()
	if !reported {
		q.opts.Observer.LocalError(err)
	}
}

// localOK ends the local error of key, a spool file that was read or a
// catalog operation that succeeded, so its next error is reported again.
func (q *Queue) localOK(key localKey) {
	q.mu.Lock()
	delete(q.failing, key)
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
