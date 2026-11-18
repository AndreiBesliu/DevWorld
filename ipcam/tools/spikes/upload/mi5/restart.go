// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The restart's failures (P16).
var (
	ErrResumedAtZero  = errors.New("mi5: the resumed session started again at offset 0")
	ErrResumedTooLow  = errors.New("mi5: the resumed session's first offset is below the last confirmed offset before the kill")
	ErrNeverResumed   = errors.New("mi5: the restarted hub never resumed the placement in flight")
	errRestartMissing = errors.New("mi5: no restart")
)

// CheckResume checks the restart's evidence (P16): the resumed session's
// first offset is > 0 and ≥ the last confirmed offset seen before the
// kill (the provider's status query may report bytes persisted after the
// last event).
func CheckResume(r *results.Mi5Restart) error {
	switch {
	case r == nil:
		return errRestartMissing
	case r.ResumedOffset == 0:
		return ErrResumedAtZero
	case r.ResumedOffset < r.LastConfirmedOffset:
		return ErrResumedTooLow
	}
	return nil
}

// tearAfter is how long after a camera's segment rotation the restart's
// kill waits: the next segment has no .part until MediaMTX writes its
// first part (the hub's parts are 1 s), so a kill right after a rotation
// tears no segment; two parts in, it tears one that the restarted hub's
// reconcile finalises (P16).
const tearAfter = 2 * time.Second

// progress is an upload_progress: a placement, its confirmed offset and
// its size.
type progress struct {
	placement     string
	offset, size  int64
	atMs          int64
	dest          string
	afterUnblock  bool
	beforeRestart bool
}

// placement is one placement of the run's own segments, as the hub's
// events show it.
type placement struct {
	dest            string
	cam             string
	startMs         int64
	bytes           int64
	queuedAtMs      int64
	verifiedAtMs    int64
	queuedAfterKill bool
	// verifications counts its upload_verified events; mismatched is
	// whether its last upload ended in hash_mismatch.
	verifications int
	mismatched    bool
}

// tally is what the hub's events show of a run (Task 7b's events).
type tally struct {
	startMs   int64
	dests     map[string]DestConfig
	lastEvent int64
	// The block: its start, the floor (start + 2 min) and the unblock.
	blockStart, floorStart, unblockAt int64
	// The restart: the kill, its placement and last confirmed offset,
	// and the offset at which the new hub resumed it.
	killedAt, lastConfirmed int64
	restartPlacement        string
	resumedOffset           int64
	resumed                 bool
	progress                []progress
	placements              map[string]*placement
	// Per destination: uploads verified and failed between the floor and
	// the unblock, UNREACHABLE, and the calls of each verified upload.
	verifiedInBlock, failedInBlock, rateLimitedInBlock map[string]int
	unreachable                                        map[string]bool
	calls                                              map[string]map[string]float64
	verified                                           map[string]int
	// segmentStarts are the run's closed segments' starts, by camera.
	segmentStarts map[string][]int64
	// gapsAfterKill counts the recording gaps that the kill caused: those
	// open across the kill, which began before the restarted hub ran
	// (restartedAt), so a later crash's gaps are not the kill's.
	gapsAfterKill int
	restartedAt   int64
	// rotated is when each camera's last segment closed (a new one began);
	// closedAt when each segment (camera and start) closed.
	rotated, closedAt map[string]int64
	// hubSince is when the run saw the last crashed hub's end: the
	// restart's target is an upload of the running hub, so progress from
	// before it is a crashed hub's (C2-R29 (11)).
	hubSince int64
}

func newTally(startMs int64, dests []DestConfig) *tally {
	t := &tally{startMs: startMs, dests: map[string]DestConfig{}, placements: map[string]*placement{},
		verifiedInBlock: map[string]int{}, failedInBlock: map[string]int{}, rateLimitedInBlock: map[string]int{},
		unreachable: map[string]bool{}, calls: map[string]map[string]float64{}, verified: map[string]int{},
		segmentStarts: map[string][]int64{}, rotated: map[string]int64{}, closedAt: map[string]int64{}}
	for _, d := range dests {
		t.dests[d.ID] = d
	}
	return t
}

// event is the fields of the events that the tally reads.
type event struct {
	PlacementID string             `json:"placementId"`
	Dest        string             `json:"dest"`
	Cam         string             `json:"cam"`
	Profile     string             `json:"profile"`
	StartMs     int64              `json:"startMs"`
	EndMs       int64              `json:"endMs"`
	Bytes       int64              `json:"bytes"`
	Offset      int64              `json:"offset"`
	Size        int64              `json:"size"`
	Reason      string             `json:"reason"`
	State       string             `json:"state"`
	Calls       map[string]float64 `json:"calls"`
}

// inBlock reports whether at lies between the floor and the unblock.
func (t *tally) inBlock(at int64) bool {
	return t.floorStart != 0 && at >= t.floorStart && (t.unblockAt == 0 || at < t.unblockAt)
}

// observe reads one hub event.
func (t *tally) observe(ev hubproc.Event) {
	var f event
	if json.Unmarshal(ev.Raw, &f) != nil {
		return
	}
	at := ev.AtMs
	if strings.HasPrefix(ev.Type, "upload_") {
		t.lastEvent = max(t.lastEvent, at) // the final drain's quiet: no upload moves
	}
	switch ev.Type {
	case "upload_queued":
		if f.StartMs < t.startMs {
			return // another run's placement: in no count
		}
		if p, ok := t.placements[f.PlacementID]; ok {
			p.queuedAtMs = at
			return
		}
		t.placements[f.PlacementID] = &placement{dest: f.Dest, cam: f.Cam, startMs: f.StartMs, bytes: f.Bytes, queuedAtMs: at,
			queuedAfterKill: t.killedAt != 0 && at >= t.killedAt}
	case "upload_progress":
		if _, ours := t.placements[f.PlacementID]; !ours {
			return
		}
		t.progress = append(t.progress, progress{placement: f.PlacementID, offset: f.Offset, size: f.Size, atMs: at, dest: f.Dest,
			afterUnblock: t.unblockAt != 0 && at >= t.unblockAt, beforeRestart: t.killedAt == 0})
	case "upload_resumed":
		if t.killedAt != 0 && !t.resumed && f.PlacementID == t.restartPlacement {
			t.resumed, t.resumedOffset = true, f.Offset
		}
	case "upload_verified":
		p, ours := t.placements[f.PlacementID]
		if !ours {
			return
		}
		if p.verifiedAtMs == 0 {
			t.verified[f.Dest]++
			if t.calls[f.Dest] == nil {
				t.calls[f.Dest] = map[string]float64{}
			}
			for k, v := range f.Calls {
				t.calls[f.Dest][k] += v
			}
		}
		p.verifiedAtMs, p.bytes = at, max(p.bytes, f.Bytes)
		p.verifications++
		p.mismatched = false
		if t.inBlock(at) {
			t.verifiedInBlock[f.Dest]++
		}
	case "upload_failed":
		if p, ours := t.placements[f.PlacementID]; ours && f.Reason == "hash_mismatch" {
			p.mismatched = true
		}
		if !t.inBlock(at) {
			return
		}
		switch f.Reason {
		case "transient":
			t.failedInBlock[f.Dest]++
		case "rate_limited":
			t.rateLimitedInBlock[f.Dest]++
		}
	case "dest_state":
		if f.State == "UNREACHABLE" && t.inBlock(at) {
			t.unreachable[f.Dest] = true
		}
	case "segment_closed":
		if f.Profile == "sub" {
			t.rotated[f.Cam] = at
			if _, seen := t.closedAt[segmentKey(f.Cam, f.StartMs)]; !seen {
				t.closedAt[segmentKey(f.Cam, f.StartMs)] = at
			}
		}
		if f.Profile == "sub" && f.StartMs >= t.startMs {
			t.segmentStarts[f.Cam] = append(t.segmentStarts[f.Cam], f.StartMs)
		}
	case "gap":
		if t.killedAt != 0 && f.EndMs >= t.killedAt && (t.restartedAt == 0 || f.StartMs <= t.restartedAt) {
			t.gapsAfterKill++
		}
	}
}

// segmentKey names a segment by its camera and start.
func segmentKey(cam string, startMs int64) string { return cam + "/" + strconv.FormatInt(startMs, 10) }

// midUpload is a placement in the middle of its upload after the unblock,
// of the running hub: its latest upload_progress, after the unblock and
// after the last crashed hub's end (hubSince), has 0 < offset < size with
// at least a quarter of its bytes still to go (so the kill lands between
// two parts, not after the last), and it is not verified. It is the
// restart's moment once no camera rotated within tearAfter of nowMs, so
// that the kill also tears an open segment.
func (t *tally) midUpload(nowMs int64) (progress, bool) {
	if t.unblockAt == 0 || t.killedAt != 0 {
		return progress{}, false
	}
	for _, at := range t.rotated {
		if nowMs-at < tearAfter.Milliseconds() {
			return progress{}, false
		}
	}
	latest := map[string]progress{}
	var order []string
	for _, p := range t.progress {
		if !p.afterUnblock || p.atMs < t.hubSince {
			continue
		}
		if _, seen := latest[p.placement]; !seen {
			order = append(order, p.placement)
		}
		if p.offset >= latest[p.placement].offset {
			latest[p.placement] = p
		}
	}
	for _, id := range order {
		p := latest[id]
		if pl := t.placements[id]; pl == nil || pl.verifiedAtMs != 0 {
			continue
		}
		if p.offset > 0 && p.offset < p.size && p.size-p.offset >= p.size/4 {
			return p, true
		}
	}
	return progress{}, false
}

// drained reports whether the restart's placement and every placement
// of the run are verified, and, with quiet, nothing happened for quiet.
func (t *tally) drained(nowMs, quietMs int64) bool {
	if t.killedAt == 0 {
		return false
	}
	if p, ok := t.placements[t.restartPlacement]; !ok || p.verifiedAtMs == 0 {
		return false
	}
	for _, p := range t.placements {
		if p.verifiedAtMs == 0 {
			return false
		}
	}
	return quietMs == 0 || nowMs-t.lastEvent >= quietMs
}

// drainBytes are the bytes verified between the unblock and end.
func (t *tally) drainBytes(end int64) int64 {
	var n int64
	for _, p := range t.placements {
		if p.verifiedAtMs >= t.unblockAt && p.verifiedAtMs <= end {
			n += p.bytes
		}
	}
	return n
}

// segmentsDuringBlock are the run's segments of d's cameras that started
// during the block, within d's window.
func (t *tally) segmentsDuringBlock(d DestConfig) int {
	n := 0
	until := int64(0)
	if d.Window > 0 {
		until = t.startMs + d.Window.Milliseconds()
	}
	for _, cam := range d.Cameras {
		for _, s := range t.segmentStarts[cam] {
			if s >= t.blockStart && s < t.unblockAt && (until == 0 || s < until) {
				n++
			}
		}
	}
	return n
}

// duplicates are dest's uploads verified beyond the first of each of the
// run's segments: a placement that the hub verified twice, or a second
// placement of one segment (C2-R15 (c), where no listing counts them).
func (t *tally) duplicates(dest string) int {
	bySegment := map[string]int{}
	for _, p := range t.placements {
		if p.dest == dest {
			bySegment[segmentKey(p.cam, p.startMs)] += p.verifications
		}
	}
	n := 0
	for _, v := range bySegment {
		n += max(v-1, 0)
	}
	return n
}

// mismatched are dest's placements whose last upload ended in
// hash_mismatch: the hub's own hash and size check failed (C2-R15 (c)).
func (t *tally) mismatched(dest string) int {
	n := 0
	for _, p := range t.placements {
		if p.dest == dest && p.mismatched {
			n++
		}
	}
	return n
}

// tornUploaded reports whether the segments that the kill tore, which the
// restarted hub's reconcile finalised and enqueued, were all uploaded:
// placements first queued after the kill for segments that started before
// it and had not closed before it (a closed segment's placement may also
// be queued first by the restarted hub).
func (t *tally) tornUploaded() bool {
	torn := 0
	for _, p := range t.placements {
		if closed, ok := t.closedAt[segmentKey(p.cam, p.startMs)]; ok && closed < t.killedAt {
			continue
		}
		if p.queuedAfterKill && p.startMs < t.killedAt {
			torn++
			if p.verifiedAtMs == 0 {
				return false
			}
		}
	}
	return torn > 0
}
