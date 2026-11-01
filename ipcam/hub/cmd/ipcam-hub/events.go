package main

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/outbox"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/recorder"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// events writes one JSON object per line on stdout, the run event schema.
// Every event has "type" and "atMs", the host's wall clock in Unix ms, for
// information only: the spike measures time on the controller (Review
// Focus 2). The types and their other fields:
//
//   - reconcile: finalized, truncated, lostTails, gapsWritten, rewritten,
//     unreadable, tornParts, catalogMismatches, aborted, seq and final. The
//     whole reconcile report so far, not a delta: the startup's has seq 1,
//     and each later one, after a background rewrite or a repair of the
//     .part files that MediaMTX left while the hub runs, or when such work
//     is queued, has seq one more. Events can come out of order, so a
//     consumer uses the one with the highest seq. final is true when no
//     background rewrite or repair is pending: that report is complete.
//   - ready: jobs [{cam, profile, encrypted, padLowBitrate}].
//   - recording: cam, profile; the first part of a stream after a (re)start.
//   - segment_closed: cam, profile, path, startMs, endMs, encrypted.
//   - gap: cam, profile, startMs, endMs (0 while open), reason; for each gap
//     written, closed or moved, and for the gap of a job that the card's
//     failure stopped also when the card did not let it be written (an
//     error event says so).
//   - child_restart: MediaMTX was started again.
//   - verify: unreadable, tornParts, catalogMismatches of a background
//     verification pass.
//   - segment_verified: path, one per segment that verification passed.
//   - status: cam, profile, lastPartAgoMs, lastSegmentAgoMs, running; every
//     1 s per job.
//   - error: message.
//   - card: state ("ok", "degraded" or "failed"), only while rec/ is on an
//     SD card (T40, §13.1): the card's state as ipcam-hub starts, then each
//     change. At failed, every job stops with a card gap.
//   - alert: kind ("card"), when the card's state leaves ok (§13.3); once,
//     until the card has been ok for 2 min.
//   - sd_card_unconfirmed: rec/ is on an SD card and run.json does not
//     confirm the wear warning (HUB-17, T40); ipcam-hub has changed nothing
//     and exits 4.
//
// With an uploads section only (Task 7b), the uploads' events. None carries
// a session URI, an uploadUrl, a token or a provider's error text: a
// driver's error goes out only as upload_failed's reason, and only the
// outbox's local errors, of the catalog and the spool, as error events,
// without directories.
//   - upload_queued: placementId, cam, profile, startMs, dest, class, bytes;
//     when a worker first reads the placement, so only committed placements
//     are reported, before or after the segment's segment_closed. A
//     placement superseded by new bytes is queued again.
//   - upload_resumed: placementId, dest, offset; the upload continues at the
//     offset that its provider confirmed.
//   - upload_progress: placementId, dest, offset (confirmed), size.
//   - upload_verified: placementId, dest, bytes, hashKind, hash, attempts,
//     calls ({method: n}, the upload's API calls, P18).
//   - upload_failed: placementId, dest, reason, attempts; reason is one of
//     auth_expired, quota_full, rate_limited, daily_cap, not_found,
//     transient, permanent, hash_mismatch and local_unreadable (a spool file
//     that is gone, never due again, or unreadable, due again after its
//     backoff).
//   - dest_state: dest, state, alert; each change of a destination's state.
//
// When the SD card fails, the uploads stop: one error event says so, and no
// upload event follows (g2-04).
//
// The token store's and the control endpoint's events (Task 9). None
// carries a token, a client secret, a code, a verifier, a URL or a
// provider's own words: a failure goes out as its reason only.
//   - control: addr ("127.0.0.1:<port>"), once the control endpoint
//     listens, with IPCAM_CONTROL_TOKEN set; the token is never printed.
//   - oauth_begin: destId, provider, platform, plan; a login began.
//   - oauth_completed: destId, provider, platform, plan, clientType,
//     issuedAtMs; its refresh token is sealed in the catalog, and the
//     destinations of its tokenRef are unblocked.
//   - oauth_failed: destId, stage ("complete", "device" or "refresh"),
//     reason (invalid_grant, access_denied, redirect_uri_mismatch,
//     invalid_client, expired_token, state for an unknown, used or expired
//     state, transient when the provider did not answer, error otherwise).
//   - oauth_refresh: destId, day, attempt, outcome ("ok", "failed" or
//     "expected-expiry"), rotated; a day of the daily refresh, once decided
//     (P53).
type events struct {
	mu  sync.Mutex
	out io.Writer
}

func (e *events) emit(typ string, fields map[string]any) {
	event := map[string]any{"type": typ, "atMs": time.Now().UnixMilli()}
	for k, v := range fields {
		event[k] = v
	}
	line, err := json.Marshal(event)
	if err != nil {
		line, _ = json.Marshal(map[string]any{"type": "error", "atMs": time.Now().UnixMilli(), "message": err.Error()})
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = e.out.Write(append(line, '\n'))
}

func reportFields(r recorder.ReconcileReport) map[string]any {
	return map[string]any{
		"finalized": r.Finalized, "truncated": r.Truncated, "lostTails": r.LostTails, "gapsWritten": r.GapsWritten,
		"rewritten": r.Rewritten, "unreadable": r.Unreadable, "tornParts": r.TornParts,
		"catalogMismatches": r.CatalogMismatches, "aborted": r.Aborted,
	}
}

func statusFields(cam recorder.CameraID, p recorder.Profile, s recorder.RecStatus) map[string]any {
	return map[string]any{"cam": cam, "profile": p, "lastPartAgoMs": s.LastPartAgoMs, "lastSegmentAgoMs": s.LastSegmentAgoMs, "running": s.Running}
}

// observer turns what the recorder reports into events; the child's log
// goes to stderr, never to the events. The child's log and the errors go
// to the card monitor too, while rec/ is on an SD card.
type observer struct {
	ev   *events
	log  *logQueue    // the child's log, on its way to stderr
	card *cardSampler // nil when rec/ is not on an SD card
}

func newObserver(ev *events, stderr io.Writer, card *cardSampler) *observer {
	return &observer{ev: ev, log: newLogQueue(stderr, logQueueLen), card: card}
}

// close writes the child's log lines that still wait for stderr, for at
// most shutdownWait: a stderr that the card holds does not hold ipcam-hub's
// stop.
func (o *observer) close() { o.log.close(shutdownWait) }

func (o *observer) ChildRestarted(time.Time) { o.ev.emit("child_restart", nil) }

// ChildLog gives line to the card monitor first, then queues it for stderr:
// the goroutine that reads MediaMTX's log never waits for stderr, which in
// MI2 is a file on the same SD card (T40), so a write that the card holds
// delays neither the card's I/O errors nor the failed write's time.
func (o *observer) ChildLog(line string) {
	if o.card != nil {
		o.card.childLog(line)
	}
	o.log.add("mediamtx: " + line)
}

func (o *observer) FirstPart(cam recorder.CameraID, p recorder.Profile, _ time.Time) {
	o.ev.emit("recording", map[string]any{"cam": cam, "profile": p})
}

func (o *observer) Verified(r recorder.ReconcileReport) {
	o.ev.emit("verify", map[string]any{"unreadable": r.Unreadable, "tornParts": r.TornParts, "catalogMismatches": r.CatalogMismatches})
}

func (o *observer) SegmentVerified(path string) {
	o.ev.emit("segment_verified", map[string]any{"path": path})
}

func (o *observer) Reconciled(u recorder.ReconcileUpdate) {
	fields := reportFields(u.ReconcileReport)
	fields["seq"], fields["final"] = u.Seq, u.Final
	o.ev.emit("reconcile", fields)
}

func (o *observer) Gap(g catalog.Gap) {
	o.ev.emit("gap", map[string]any{"cam": g.Cam, "profile": g.Profile, "startMs": g.StartMs, "endMs": g.EndMs, "reason": g.Reason})
}

func (o *observer) Error(err error) {
	o.errorEvent(err)
	o.cardError(err)
}

// errorEvent emits err as an error event.
func (o *observer) errorEvent(err error) { o.ev.emit("error", map[string]any{"message": err.Error()}) }

// cardError gives err to the card monitor, while rec/ is on an SD card.
func (o *observer) cardError(err error) {
	if o.card != nil {
		o.card.hubError(err)
	}
}

// uploadEvents turns what the outbox reports into the uploads' events
// (outbox.Observer); the outbox writes nothing to stderr itself. Its local
// errors go through the observer, so the card monitor counts an I/O error of
// the spool or the catalog.
type uploadEvents struct {
	ev  *events
	obs *observer
	// mu orders the events against the stop: an event is emitted with mu
	// held, and stop takes mu, so once stop has returned no event goes out.
	mu sync.Mutex
	// stopped is set once the uploads stopped on a failed card: nothing more
	// goes out.
	stopped bool
}

var _ outbox.Observer = (*uploadEvents)(nil)

// stop makes every later event a no-op. The card sampler calls it at the
// FAILED transition, before the card's failed event (g2-04): an event
// already on its way goes out before that one.
func (u *uploadEvents) stop() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.stopped = true
}

func (u *uploadEvents) emit(typ string, fields map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.stopped {
		u.ev.emit(typ, fields)
	}
}

// Queued emits upload_queued with the stream and the start of p's segment,
// which the queue read with p (Task 7b review M4).
func (u *uploadEvents) Queued(p catalog.Placement) {
	u.emit("upload_queued", map[string]any{"placementId": p.ID, "cam": p.Cam, "profile": p.Profile, "startMs": p.StartMs,
		"dest": p.DestinationID, "class": p.Class, "bytes": p.Bytes})
}

func (u *uploadEvents) Resumed(p catalog.Placement, offset int64) {
	u.emit("upload_resumed", map[string]any{"placementId": p.ID, "dest": p.DestinationID, "offset": offset})
}

func (u *uploadEvents) Progress(p catalog.Placement, confirmedOffset, size int64) {
	u.emit("upload_progress", map[string]any{"placementId": p.ID, "dest": p.DestinationID, "offset": confirmedOffset, "size": size})
}

func (u *uploadEvents) Verified(p catalog.Placement, ref storage.ObjectRef, calls storage.CallCounts) {
	if calls == nil {
		calls = storage.CallCounts{}
	}
	u.emit("upload_verified", map[string]any{"placementId": p.ID, "dest": p.DestinationID, "bytes": ref.Size,
		"hashKind": ref.HashKind, "hash": ref.Hash, "attempts": p.Attempts, "calls": calls})
}

func (u *uploadEvents) Failed(p catalog.Placement, reason string, attempts int) {
	u.emit("upload_failed", map[string]any{"placementId": p.ID, "dest": p.DestinationID, "reason": reason, "attempts": attempts})
}

func (u *uploadEvents) DestState(destID string, s outbox.DestState, alert string) {
	u.emit("dest_state", map[string]any{"dest": destID, "state": s, "alert": alert})
}

// LocalError emits err as an error event, then gives it to the card monitor
// outside u.mu: the monitor may reach FAILED there, and its hook, stop,
// takes u.mu.
func (u *uploadEvents) LocalError(err error) {
	u.mu.Lock()
	stopped := u.stopped
	if !stopped {
		u.obs.errorEvent(err)
	}
	u.mu.Unlock()
	if !stopped {
		u.obs.cardError(err)
	}
}
