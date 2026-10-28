package recorder

import (
	"context"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
)

// failedWrite is a write that MediaMTX could not make for a stream, as
// WriteFailed was told of it: its reason, and when, by the hub's clock.
type failedWrite struct {
	key    streamKey
	reason catalog.GapReason
	at     time.Time
}

// WriteFailed queues the failed write for repairLoop and returns at once: it
// comes from the goroutine that reads MediaMTX's log, which must never wait
// for the card, or MediaMTX's output pipe would fill and hold MediaMTX
// (T40; Task 8 review M3).
func (r *rec) WriteFailed(cam CameraID, p Profile, reason catalog.GapReason) {
	r.mu.Lock()
	r.failedWrites = append(r.failedWrites, failedWrite{key: streamKey{cam, p}, reason: reason, at: time.Now()})
	r.mu.Unlock()
	select {
	case r.partsKick <- struct{}{}:
	default:
	}
}

// noteFailedWrites notes each failed write that WriteFailed queued
// (noteFailedWrite), and reports what it could not note. It notes them also
// while the hub stops: a hole that the next start could not find again is
// not lost to the stop.
func (r *rec) noteFailedWrites(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	r.mu.Lock()
	writes := r.failedWrites
	r.failedWrites = nil
	r.mu.Unlock()
	for _, fw := range writes {
		if err := r.noteFailedWrite(ctx, fw); err != nil {
			r.notifyError(err)
		}
	}
}

// noteFailedWrite records the hole that fw left in its stream's recording,
// by the stream's newest segment file that started before fw, to the
// microsecond of the names (nameTime):
//   - a .part that this hub's MediaMTX was writing keeps the hole as its lost
//     tail: fw's reason goes to the catalog with the file's path, so that
//     the file's repair, live or at the next start, writes the tail gap with
//     it instead of restart (repairParts, Reconcile);
//   - a final file means that MediaMTX could not create the next segment's
//     file, so no .part holds the hole (Task 8 concern 2): it gets an open
//     gap of fw's reason from the end of that file, the end of the last
//     good part, which the stream's next part closes at its start
//     (partWatcher.expectResume). The gap is written at once, so that a
//     stop of the hub before that part keeps it; the first part after the
//     next start closes it then.
//
// A .part in the stream's baseline, which no child of this hub writes,
// holds no hole of fw's. A stream that has an open gap already, such as one
// from before this child started, or that is no longer recorded, whose Stop
// wrote its gap, needs no new one: its open gap covers the hole.
func (r *rec) noteFailedWrite(ctx context.Context, fw failedWrite) error {
	last, ok := newestBefore(streamSegments(r.cfg.RecDir, fw.key), nameTime(fw.at))
	switch {
	case ok && last.part && !r.watch.inBaseline(fw.key, last.abs):
		return r.cfg.Catalog.PutTailReason(ctx, last.rel, fw.reason)
	case !ok || last.part || !r.recorded(fw.key):
		return nil
	}
	end := last.start.UnixMilli() + fileDurationMs(ctx, last, r.cfg.Tail)
	r.gapMu.Lock()
	defer r.gapMu.Unlock()
	if open, err := r.openGaps(ctx, fw.key); err != nil || len(open) > 0 {
		return err
	}
	g := catalog.Gap{Cam: string(fw.key.cam), Profile: string(fw.key.profile), StartMs: end, Reason: fw.reason}
	if err := r.cfg.Catalog.PutGap(ctx, g); err != nil {
		return err
	}
	r.watch.expectResume(fw.key, fw.at)
	r.notifyGap(g)
	return nil
}

// newestBefore returns the one of segs that started last before at.
func newestBefore(segs []segFile, at time.Time) (segFile, bool) {
	var newest segFile
	found := false
	for _, s := range segs {
		if s.start.Before(at) && (!found || s.start.After(newest.start)) {
			newest, found = s, true
		}
	}
	return newest, found
}

// recorded reports whether the stream k has an applied job.
func (r *rec) recorded(k streamKey) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.jobs[k]
	return ok
}
