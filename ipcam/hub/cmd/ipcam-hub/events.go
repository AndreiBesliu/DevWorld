package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/recorder"
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
//     written, closed or moved.
//   - child_restart: MediaMTX was started again.
//   - verify: unreadable, tornParts, catalogMismatches of a background
//     verification pass.
//   - segment_verified: path, one per segment that verification passed.
//   - status: cam, profile, lastPartAgoMs, lastSegmentAgoMs, running; every
//     1 s per job.
//   - error: message.
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
// goes to stderr, never to the events.
type observer struct {
	ev     *events
	stderr io.Writer
	logMu  sync.Mutex
}

func (o *observer) ChildRestarted(time.Time) { o.ev.emit("child_restart", nil) }

func (o *observer) ChildLog(line string) {
	o.logMu.Lock()
	defer o.logMu.Unlock()
	_, _ = fmt.Fprintln(o.stderr, "mediamtx:", line)
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

func (o *observer) Error(err error) { o.ev.emit("error", map[string]any{"message": err.Error()}) }
