package mi5

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// fakeRun is a run on a virtual clock: each sleep advances it, and the
// hub's events scheduled up to the new time reach the run, every one
// observed before the sleep returns.
type fakeRun struct {
	t      *testing.T
	mu     sync.Mutex
	now    time.Time
	events chan hubproc.Event
	calls  []string // the host's and the firewall's calls, with their times
	// script is called on each step with the step's name and the time;
	// it returns the events to send.
	script func(step string, at time.Time) []map[string]any
	// verify answers the block's checks; reads answers readopen.
	verify  func(n int) (ReachResult, error)
	checks  int
	readsOK bool
	uploads string // diag uploads' lines
	// uploadsErr is diag uploads' exit, beside its lines (a failed
	// listing exits 1 after printing every destination's line).
	uploadsErr error
	killed     time.Time
	unblocked  time.Time
	lines      []string // the owner's lines
	// readAt, when set, answers each readopen at the clock's time: "" for
	// a read, else its error.
	readAt func(now time.Time) string
	// oneDrive runs with A5 passed: dst-od takes cam01.
	oneDrive bool
	// gone is the running hub's end; crash, when it returns true on a
	// step, ends the hub on its own there: down, it sends no event until
	// a start or RestartCrashed. stopping is set by StopHub and KillHub;
	// restarts are the times of RestartCrashed's restarts.
	gone     chan struct{}
	crash    func(step string, at time.Time) bool
	down     bool
	stopping bool
	restarts []time.Time
	// restartErr, when set, is RestartCrashed's answer (a hub that would
	// not start again); restartCalls counts its calls.
	restartErr   error
	restartCalls int
	// destProbeErr is diag dest-probe's exit.
	destProbeErr error
	// The firewall's failures: Block's, ScheduleCleanup's, Unblock's and
	// RemoveStale's.
	blockErr, cleanupErr, unblockErr, staleErr error
	// resetErr is ResetConnections' failure.
	resetErr error
}

func newFakeRun(t *testing.T, start time.Time) *fakeRun {
	return &fakeRun{t: t, now: start, events: make(chan hubproc.Event), readsOK: true,
		verify: func(int) (ReachResult, error) { return ReachResult{true, true, true, true}, nil }}
}

func (f *fakeRun) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// send gives the run each event, then a barrier: once the barrier is
// taken, every event before it was observed.
func (f *fakeRun) send(evs []map[string]any) {
	for _, e := range append(evs, map[string]any{"type": "barrier"}) {
		if _, ok := e["atMs"]; !ok {
			e["atMs"] = f.clock().UnixMilli()
		}
		raw, _ := json.Marshal(e)
		f.events <- hubproc.Event{Type: e["type"].(string), AtMs: e["atMs"].(int64), Raw: raw} //nolint:forcetypeassert // the test's own events
	}
}

func (f *fakeRun) step(name string) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("%s@%d", name, f.now.UnixMilli()))
	at := f.now
	if f.crash != nil && !f.down && f.gone != nil && f.crash(name, at) {
		close(f.gone)
		f.down = true
	}
	down := f.down
	f.mu.Unlock()
	if f.script != nil && !down {
		f.send(f.script(name, at))
	}
}

// hubUp starts a fake hub: a new end, up.
func (f *fakeRun) hubUp() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gone, f.down, f.stopping = make(chan struct{}), false, false
}

// hubDown ends the fake hub on purpose.
func (f *fakeRun) hubDown() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopping = true
	if f.gone != nil && !f.down {
		close(f.gone)
	}
	f.down = true
}

func (f *fakeRun) HubGone() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gone
}

func (f *fakeRun) RestartCrashed(context.Context) (bool, error) {
	f.mu.Lock()
	f.restartCalls++
	if f.restartErr != nil {
		f.mu.Unlock()
		return false, f.restartErr
	}
	if f.stopping {
		f.mu.Unlock()
		return false, nil
	}
	f.restarts = append(f.restarts, f.now)
	f.mu.Unlock()
	f.hubUp()
	f.step("restart-crashed")
	return true, nil
}

func (f *fakeRun) sleep(ctx context.Context, d time.Duration) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
	f.step("sleep")
	return nil
}

func (f *fakeRun) StartHub(_ context.Context, p Phase) error {
	f.hubUp()
	f.step("start-" + string(p))
	return nil
}
func (f *fakeRun) StopHub(context.Context) error { f.hubDown(); f.step("stop"); return nil }
func (f *fakeRun) KillHub() error {
	f.killed = f.clock()
	f.hubDown()
	f.step("kill")
	return nil
}
func (f *fakeRun) ResetConnections(context.Context) error {
	f.step("reset-connections")
	return f.resetErr
}
func (f *fakeRun) HubConfig() hubproc.Config { return hubproc.Config{} }
func (f *fakeRun) RunJSON() string           { return "run.json" }
func (f *fakeRun) Printf(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, fmt.Sprintf(format, args...))
}
func (f *fakeRun) Diag(_ context.Context, args []string) ([]json.RawMessage, error) {
	f.step("diag-" + args[0] + "-" + strings.Join(args[len(args)-1:], ""))
	switch args[0] {
	case "readopen":
		f.mu.Lock()
		down := f.down
		f.mu.Unlock()
		if down {
			return nil, &hubproc.DiagError{Code: 1, Stderr: "ipcam-hub: diag readopen: no hub"}
		}
		if f.readAt != nil {
			if why := f.readAt(f.clock()); why != "" {
				return []json.RawMessage{json.RawMessage(`{"schema":"ipcam.diag.readopen/1","ok":false,"error":"` + why + `"}`)}, nil
			}
		}
		return []json.RawMessage{json.RawMessage(fmt.Sprintf(`{"schema":"ipcam.diag.readopen/1","ok":%v,"readMs":12}`, f.readsOK))}, nil
	case "uploads":
		var out []json.RawMessage
		for l := range strings.SplitSeq(f.uploads, "\n") {
			if l != "" {
				out = append(out, json.RawMessage(l))
			}
		}
		return out, f.uploadsErr
	case "dest-probe":
		return nil, f.destProbeErr
	}
	return nil, nil
}

// fake firewall calls.
func (f *fakeRun) Block(context.Context) error { f.step("block"); return f.blockErr }
func (f *fakeRun) Unblock(context.Context) error {
	f.unblocked = f.clock()
	f.step("unblock")
	return f.unblockErr
}
func (f *fakeRun) RemoveStale(context.Context) error { f.step("remove-stale"); return f.staleErr }
func (f *fakeRun) ScheduleCleanup(_ context.Context, at time.Time) error {
	f.step(fmt.Sprintf("cleanup-%d", at.UnixMilli()))
	return f.cleanupErr
}

// labRun runs Run with f, the lab's 15-min block and 30-s segments.
func labRun(t *testing.T, f *fakeRun, start time.Time) (*results.Mi5Data, error) {
	return labRunCtx(t.Context(), t, f, start)
}

// labScript is a lab run's hub: placements queued from the main start,
// segments and transient failures during the block, a mid-upload
// progress during the block (never the restart's), one after the
// unblock, then the resume, a torn segment and the verifications.
func labScript(f *fakeRun, start time.Time, resumed int64) func(string, time.Time) []map[string]any {
	var blockAt time.Time
	stage := 0
	return func(step string, at time.Time) []map[string]any {
		ms := at.UnixMilli()
		switch {
		case step == "start-main" && stage == 0:
			stage = 1
			return []map[string]any{
				{"type": "upload_queued", "placementId": "p-1", "dest": "dst-1", "cam": "cam01", "profile": "sub", "startMs": start.Add(time.Minute).UnixMilli(), "bytes": int64(1 << 20)},
				{"type": "upload_queued", "placementId": "p-9", "dest": "dst-1", "cam": "cam01", "profile": "sub", "startMs": start.Add(-time.Hour).UnixMilli()},
			}
		case step == "block":
			blockAt = at
		case step == "sleep" && stage == 1 && !blockAt.IsZero() && at.After(blockAt.Add(3*time.Minute)) && f.unblocked.IsZero():
			stage = 2
			return []map[string]any{
				{"type": "segment_closed", "cam": "cam02", "profile": "sub", "startMs": blockAt.Add(time.Minute).UnixMilli()},
				{"type": "upload_queued", "placementId": "p-2", "dest": "dst-1", "cam": "cam02", "profile": "sub", "startMs": blockAt.Add(time.Minute).UnixMilli(), "bytes": int64(4 << 20)},
				{"type": "upload_failed", "placementId": "p-2", "dest": "dst-1", "reason": "transient"},
				{"type": "upload_progress", "placementId": "p-2", "dest": "dst-1", "offset": int64(512 << 10), "size": int64(4 << 20)},
			}
		case step == "sleep" && stage == 2 && !f.unblocked.IsZero():
			stage = 3
			return []map[string]any{{"type": "upload_progress", "placementId": "p-2", "dest": "dst-1", "offset": int64(1 << 20), "size": int64(4 << 20)}}
		case step == "start-main" && stage == 3:
			stage = 4
			return []map[string]any{
				{"type": "upload_resumed", "placementId": "p-2", "dest": "dst-1", "offset": resumed},
				{"type": "gap", "cam": "cam01", "profile": "sub", "startMs": f.killed.UnixMilli() - 500, "endMs": ms},
				{"type": "upload_queued", "placementId": "p-3", "dest": "dst-1", "cam": "cam03", "profile": "sub", "startMs": f.killed.Add(-20 * time.Second).UnixMilli(), "bytes": int64(1 << 20)},
				{"type": "upload_verified", "placementId": "p-1", "dest": "dst-1", "bytes": int64(1 << 20), "calls": map[string]float64{"put": 2, "get": 1}},
				{"type": "upload_verified", "placementId": "p-2", "dest": "dst-1", "bytes": int64(4 << 20), "calls": map[string]float64{"put": 8, "get": 1}},
				{"type": "upload_verified", "placementId": "p-3", "dest": "dst-1", "bytes": int64(1 << 20), "calls": map[string]float64{"put": 2, "get": 1}},
			}
		}
		return nil
	}
}

// TestMI5_RestartOnlyAfterUnblockMidUpload (P16): a mid-upload progress
// during the block never kills the hub; the kill follows the first one
// after the unblock, with 0 < offset < size, whose placement's last
// confirmed offset is recorded.
func TestMI5_RestartOnlyAfterUnblockMidUpload(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	f.script = labScript(f, start, 1<<20)
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatal(err)
	}
	if f.killed.IsZero() || !f.killed.After(f.unblocked) {
		t.Fatalf("killed at %v, unblocked at %v; want the kill after the unblock", f.killed, f.unblocked)
	}
	if r := data.Restart; r == nil || r.LastConfirmedOffset != 1<<20 || r.Size != 4<<20 || r.ResumedOffset != 1<<20 || !r.TornSegmentUploaded {
		t.Errorf("restart %+v", data.Restart)
	}
	d := data.Dests[0]
	if d.Dest != "dst-1" || d.Verified != 3 || d.Segments != 3 || d.FailedDuringBlock != 1 || d.VerifiedDuringBlock != 0 ||
		d.SegmentsDuringBlock != 1 || d.AuditFrom != AuditListRemote {
		t.Errorf("dst-1 %+v", d)
	}
	if d.CallsPerSegment["put"] != 4 || d.CallsPerSegment["get"] != 1 {
		t.Errorf("calls per segment %v; want put 4, get 1 (the other run's placement 9 counts nowhere)", d.CallsPerSegment)
	}
	if data.RecoverySecs <= 0 || data.UplinkMbps <= 0 || data.BlockSecs < (15*time.Minute).Seconds() {
		t.Errorf("recovery %v s, uplink %v Mbps, block %v s", data.RecoverySecs, data.UplinkMbps, data.BlockSecs)
	}
	if last := data.Dests[len(data.Dests)-1]; last.Dest != "dst-od" || last.Deferred == "" {
		t.Errorf("without A5, dst-od is not deferred: %+v", last)
	}
}

// TestMI5_BlockResetsConnections: Block is followed by one
// reset-connections call and the cleanup at the block's start + 15 min +
// 30 min, before the clock starts.
func TestMI5_BlockResetsConnections(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	f.script = labScript(f, start, 1<<20)
	if _, err := labRun(t, f, start); err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, c := range f.calls {
		name, _, _ := strings.Cut(c, "@")
		if name == "block" || name == "reset-connections" || strings.HasPrefix(name, "cleanup-") || name == "unblock" {
			seq = append(seq, name)
		}
	}
	i := slices.Index(seq, "block")
	if i < 0 || len(seq) < i+3 || seq[i+1] != "reset-connections" || !strings.HasPrefix(seq[i+2], "cleanup-") ||
		strings.Count(strings.Join(seq, " "), "reset-connections") != 1 {
		t.Errorf("the block's calls: %v; want block, one reset-connections, the cleanup", seq)
	}
	blockAt := ""
	for _, c := range f.calls {
		if strings.HasPrefix(c, "block@") {
			blockAt = strings.TrimPrefix(c, "block@")
		}
	}
	var at int64
	_, _ = fmt.Sscan(blockAt, &at)
	if want := fmt.Sprintf("cleanup-%d", at+(45*time.Minute).Milliseconds()); seq[i+2] != want {
		t.Errorf("cleanup %s; want %s", seq[i+2], want)
	}
}

// TestMI5_ReadOpenAtLeast20Times: 20 reads in the encrypted pre-run, and
// at least 20 during the block on a clear camera, with their successes
// and timings.
func TestMI5_ReadOpenAtLeast20Times(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	f.script = labScript(f, start, 1<<20)
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.ReadOpen) != 2 || !data.ReadOpen[0].Encrypted || data.ReadOpen[0].Calls != 20 || data.ReadOpen[0].OK != 20 ||
		data.ReadOpen[1].Encrypted || data.ReadOpen[1].Calls < 20 || data.ReadOpen[1].OK != data.ReadOpen[1].Calls || data.ReadOpen[1].P50Ms != 12 {
		t.Errorf("reads %+v", data.ReadOpen)
	}
	cam02 := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "diag-readopen-sub") {
			cam02++
		}
	}
	if cam02 < 40 {
		t.Errorf("%d reads in all", cam02)
	}
}

// TestMI5_ResumeOffsetAtLeastLastConfirmed (P16).
func TestMI5_ResumeOffsetAtLeastLastConfirmed(t *testing.T) {
	for _, tc := range []struct {
		resumed int64
		want    error
	}{{0, ErrResumedAtZero}, {512 << 10, ErrResumedTooLow}, {1 << 20, nil}, {2 << 20, nil}} {
		r := &results.Mi5Restart{Size: 4 << 20, LastConfirmedOffset: 1 << 20, ResumedOffset: tc.resumed}
		if err := CheckResume(r); !errors.Is(err, tc.want) {
			t.Errorf("resumed at %d: %v; want %v", tc.resumed, err, tc.want)
		}
	}
	// The judge: a resume below the last confirmed offset misses a floor.
	e := passing()
	e.MI5.Restart.ResumedOffset = e.MI5.Restart.LastConfirmedOffset - 1
	if j := Judge([]results.Envelope{e}); j.Verdict != results.Incomplete {
		t.Errorf("a resume below the last confirmed offset: %+v", j)
	}
	// A run whose hub resumed at 0 records it.
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	f.script = labScript(f, start, 0)
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(CheckResume(data.Restart), ErrResumedAtZero) {
		t.Errorf("restart %+v", data.Restart)
	}
}

// TestMI5_InjectedGapNotMissing: the kill's recording gap counts in
// InjectedGaps and never as a missing segment.
func TestMI5_InjectedGapNotMissing(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	f.script = labScript(f, start, 1<<20)
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatal(err)
	}
	if data.InjectedGaps != 1 || data.Dests[0].Missing != 0 {
		t.Errorf("gaps %d, missing %d; want the kill's gap counted, nothing missing", data.InjectedGaps, data.Dests[0].Missing)
	}
}

// TestMI5_TornSegmentEnqueuedAndUploaded (from the audit): the segment
// that the kill tore, finalised and enqueued by the restarted hub's
// reconcile, is uploaded, and the audit counts it.
func TestMI5_TornSegmentEnqueuedAndUploaded(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	base := labScript(f, start, 1<<20)
	f.script = func(step string, at time.Time) []map[string]any {
		evs := base(step, at)
		// Without the torn segment's verification.
		return slices.DeleteFunc(evs, func(e map[string]any) bool { return e["type"] == "upload_verified" && e["placementId"] == "p-3" })
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if _, err := labRunCtx(ctx, t, f, start); err == nil {
		t.Fatal("a run whose torn segment never uploaded drained")
	}
	f2 := newFakeRun(t, start)
	f2.uploads = f.uploads
	f2.script = labScript(f2, start, 1<<20)
	data, err := labRun(t, f2, start)
	if err != nil || data.Restart == nil || !data.Restart.TornSegmentUploaded {
		t.Fatalf("torn segment uploaded: %+v, %v", data, err)
	}
}

// labRunCtx is labRun with ctx.
func labRunCtx(ctx context.Context, _ *testing.T, f *fakeRun, start time.Time) (*results.Mi5Data, error) {
	cfg := Mi5Config{Cameras: 4, SegmentSecs: 30, Drive: DriveTest, BlockFor: 15 * time.Minute, ReadOpenCalls: 20, Lab: true,
		ProviderHosts: []string{LabHost}, LanProbe: "10.1.0.4:9", OneDrive: f.oneDrive}
	opts := RunOptions{Host: f, Firewall: f, StartMs: start.UnixMilli(), Dests: Destinations(cfg), Timing: LabTiming(cfg.BlockFor),
		Events: f.events, Clock: f.clock, Sleep: f.sleep, Resolve: func(context.Context, string) error { return nil },
		Verify: func(context.Context) (ReachResult, error) { f.checks++; return f.verify(f.checks) }}
	return Run(ctx, cfg, opts)
}

// TestVerifyBlocked_RunnerUnblocksOnFailure: a check that fails before the
// clock, or during the block, unblocks and ends the run with its line.
func TestVerifyBlocked_RunnerUnblocksOnFailure(t *testing.T) {
	for _, tc := range []struct {
		failAt int
		err    error
	}{{1, ErrProviderReachV6}, {2, ErrBlockCutLAN}, {1, ErrProviderReachV4}} {
		start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
		f := newFakeRun(t, start)
		f.verify = func(n int) (ReachResult, error) {
			if n == tc.failAt {
				return ReachResult{}, tc.err
			}
			return ReachResult{true, true, true, true}, nil
		}
		_, err := labRun(t, f, start)
		if !errors.Is(err, tc.err) {
			t.Errorf("check %d failing: %v; want %v", tc.failAt, err, tc.err)
		}
		if f.unblocked.IsZero() || !f.killed.IsZero() {
			t.Errorf("check %d failing: unblocked %v, killed %v; want unblocked and no restart", tc.failAt, f.unblocked, f.killed)
		}
	}
}

// TestMI5_SaysWhereTheRunIs (C2-R18): a run of hours says, in Romanian,
// when the block starts and until when, when it ends, when the hub
// restarted, and when the backlog is up, in that order.
func TestMI5_SaysWhereTheRunIs(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	f.script = labScript(f, start, 1<<20)
	if _, err := labRun(t, f, start); err != nil {
		t.Fatal(err)
	}
	prefixes := []string{strings.SplitN(MsgBlockStarted, "%", 2)[0], MsgUnblocked, MsgRestarted, strings.SplitN(MsgDrained, "%", 2)[0]}
	if len(f.lines) != len(prefixes) {
		t.Fatalf("the owner's lines %q; want %d", f.lines, len(prefixes))
	}
	for i, p := range prefixes {
		if !strings.HasPrefix(f.lines[i], p) {
			t.Errorf("line %d %q; want %q…", i, f.lines[i], p)
		}
	}
	if !strings.Contains(f.lines[0], "15 min") {
		t.Errorf("the block's line %q names no length", f.lines[0])
	}
}

// TestMI5_ReadOpenSettlesAfterRotation (MI5's open-segment reads): a
// camera has no open segment for a moment after each rotation, until
// MediaMTX opens the next .part; a series whose period is the segment's,
// as the lab's encrypted pre-run (a read every 30 s, 30-s segments), lands
// on every rotation and reads nothing open: the lab and a local run of its
// timing read 0 of 20 there, each «no-open-segment». A read waits until 5 s
// after the camera's last segment_closed, and a read that still finds no
// open segment (its rotation's event not yet seen) tries once more 5 s
// later; a camera with no open segment at all still fails. Each read that
// tried once more is counted in Retried (C2-R27 (2), C2-R29 (6)), so that
// a first miss stays visible: here the late event's first read, and every
// read of a camera with nothing open.
func TestMI5_ReadOpenSettlesAfterRotation(t *testing.T) {
	const gap = time.Second
	for _, tc := range []struct {
		name        string
		latency     time.Duration // from a rotation to its segment_closed
		never       bool          // no segment is ever open
		wantOK      int
		wantRetried int
	}{{"event-seen", 50 * time.Millisecond, false, 20, 0}, {"event-late", 3 * time.Second, false, 20, 1}, {"never-open", 50 * time.Millisecond, true, 0, 20}} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
			first := start.Add(30*time.Second - 500*time.Millisecond) // the first rotation, just before the first read
			f := newFakeRun(t, start)
			f.readAt = func(now time.Time) string {
				if tc.never {
					return "no-open-segment"
				}
				if since := now.Sub(first); since >= 0 && since%(30*time.Second) < gap {
					return "no-open-segment"
				}
				return ""
			}
			sent := 0
			f.script = func(_ string, at time.Time) []map[string]any {
				var evs []map[string]any
				for ; ; sent++ {
					seen := first.Add(time.Duration(sent) * 30 * time.Second).Add(tc.latency)
					if seen.After(at) {
						break
					}
					evs = append(evs, map[string]any{"type": "segment_closed", "cam": "cam01", "profile": "sub", "atMs": seen.UnixMilli(),
						"startMs": seen.Add(-30 * time.Second).UnixMilli()})
				}
				return evs
			}
			cfg := Mi5Config{Cameras: 4, SegmentSecs: 30, Drive: DriveTest, BlockFor: 15 * time.Minute, ReadOpenCalls: 20, Lab: true}
			r := newRun(cfg, RunOptions{Host: f, Firewall: f, StartMs: start.UnixMilli(), Dests: Destinations(cfg), Timing: LabTiming(cfg.BlockFor),
				Events: f.events, Clock: f.clock, Sleep: f.sleep})
			go r.consume(f.events)
			defer r.stopConsuming()
			res, err := r.readOpenSeries(t.Context(), true, "cam01", ReadOpenCalls, 30*time.Second, 10*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if res.Calls != ReadOpenCalls || res.OK != tc.wantOK || tc.wantOK == 0 && res.Failures["no-open-segment"] != ReadOpenCalls {
				t.Errorf("reads %+v; want %d calls, %d read", res, ReadOpenCalls, tc.wantOK)
			}
			if res.Retried != tc.wantRetried {
				t.Errorf("reads %+v; want %d retried", res, tc.wantRetried)
			}
		})
	}
}

// observeAt gives tl one hub event of type typ at at, with fields f.
func observeAt(tl *tally, typ string, at time.Time, f map[string]any) {
	f["type"], f["atMs"] = typ, at.UnixMilli()
	raw, _ := json.Marshal(f)
	tl.observe(hubproc.Event{Type: typ, AtMs: at.UnixMilli(), Raw: raw})
}

// TestMI5_TornSegmentIsOneOpenAtTheKill (P16's restart assertions): the
// torn segment is one that was open when the kill came, which the
// restarted hub's reconcile finalises and enqueues. A segment that closed
// before the kill, whose placement the restarted hub happens to queue
// first, is not one: the local lab run of the lab's timing counted such
// placements and said tornSegmentUploaded while its reconcile finalised
// nothing (the kill came 1 s after a rotation, before any part).
func TestMI5_TornSegmentIsOneOpenAtTheKill(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	kill := start.Add(10 * time.Minute)
	tl := newTally(start.UnixMilli(), Destinations(Mi5Config{Cameras: 4, Drive: DriveTest}))
	closedStart := kill.Add(-31 * time.Second).UnixMilli()
	observeAt(tl, "segment_closed", kill.Add(-time.Second), map[string]any{"cam": "cam02", "profile": "sub", "startMs": closedStart})
	tl.killedAt = kill.UnixMilli()
	observeAt(tl, "upload_queued", kill.Add(2*time.Second), map[string]any{"placementId": "p-closed", "dest": "dst-1", "cam": "cam02",
		"profile": "sub", "startMs": closedStart, "bytes": 1})
	observeAt(tl, "upload_verified", kill.Add(3*time.Second), map[string]any{"placementId": "p-closed", "dest": "dst-1", "bytes": 1})
	if tl.tornUploaded() {
		t.Error("a segment that closed before the kill counts as the torn one")
	}
	openStart := kill.Add(-20 * time.Second).UnixMilli()
	observeAt(tl, "upload_queued", kill.Add(2*time.Second), map[string]any{"placementId": "p-torn", "dest": "dst-1", "cam": "cam01",
		"profile": "sub", "startMs": openStart, "bytes": 1})
	if tl.tornUploaded() {
		t.Error("the torn segment counts as uploaded before its verification")
	}
	observeAt(tl, "upload_verified", kill.Add(4*time.Second), map[string]any{"placementId": "p-torn", "dest": "dst-1", "bytes": 1})
	if !tl.tornUploaded() {
		t.Error("the torn segment, open at the kill and verified after it, does not count")
	}
}

// TestMI5_KillTearsASegmentWithParts (P16): the restart's kill waits until
// every camera's open segment holds parts (tearAfter after its last
// rotation, the hub's parts being 1 s), so that the kill tears a segment
// that the restarted hub's reconcile finalises: right after a rotation,
// the next segment has no .part yet, and a kill there tears nothing.
func TestMI5_KillTearsASegmentWithParts(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	base := labScript(f, start, 1<<20)
	var rotated time.Time
	f.script = func(step string, at time.Time) []map[string]any {
		evs := base(step, at)
		for _, e := range evs {
			if e["type"] == "upload_progress" && e["offset"] == int64(1<<20) {
				// The mid-upload moment after the unblock comes with a rotation.
				rotated = at
				evs = append(evs, map[string]any{"type": "segment_closed", "cam": "cam04", "profile": "sub", "startMs": at.Add(-30 * time.Second).UnixMilli()})
				break
			}
		}
		return evs
	}
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.IsZero() || f.killed.Sub(rotated) < tearAfter {
		t.Errorf("killed %v after the rotation; want at least %v", f.killed.Sub(rotated), tearAfter)
	}
	if r := data.Restart; r == nil || r.LastConfirmedOffset != 1<<20 || !r.TornSegmentUploaded {
		t.Errorf("restart %+v", data.Restart)
	}
}

// odScript is labScript with OneDrive (A5 passed): dst-od's cam01
// placements, of which od-2 the hub verified twice (a duplicate upload),
// queued and verified with the drain's verifications.
func odScript(f *fakeRun, start time.Time) func(string, time.Time) []map[string]any {
	base := labScript(f, start, 1<<20)
	return func(step string, at time.Time) []map[string]any {
		evs := base(step, at)
		for _, e := range evs {
			if e["type"] != "upload_verified" || e["placementId"] != "p-1" {
				continue
			}
			queued := func(id string, segAt time.Duration) map[string]any {
				return map[string]any{"type": "upload_queued", "placementId": id, "dest": "dst-od", "cam": "cam01", "profile": "sub",
					"startMs": start.Add(segAt).UnixMilli(), "bytes": int64(1 << 20)}
			}
			verified := func(id string) map[string]any {
				return map[string]any{"type": "upload_verified", "placementId": id, "dest": "dst-od", "bytes": int64(1 << 20),
					"calls": map[string]float64{"put": 1}}
			}
			return append(evs, queued("od-1", time.Minute), queued("od-2", 2*time.Minute), verified("od-1"), verified("od-2"), verified("od-2"))
		}
		return evs
	}
}

// TestMI5_OneDriveAuditFromEventsWhenListUnsupported (C2-R15 (c)): with
// A5 passed, `diag uploads --list-remote` lists dst-1 but cannot list
// dst-od (OneDrive's List is unsupported, C2-R12): it prints both lines,
// dst-od's three checks null, and exits 1. The run goes on to its
// envelope: dst-1's checks come from the listing; dst-od's from the
// hub's events and the catalog, said in AuditFrom: missing are the
// segments the catalog verified no copy of (the deleted one is already
// out of segments), and a placement the hub verified twice is a
// duplicate.
func TestMI5_OneDriveAuditFromEventsWhenListUnsupported(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.oneDrive = true
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}
{"schema":"ipcam.diag.uploads/1","dest":"dst-od","segments":3,"verified":2,"deleted":1,"missing":null,"duplicates":null,"hashMismatches":null}`
	f.uploadsErr = &hubproc.DiagError{Code: 1, Stderr: "ipcam-hub: diag uploads: dst-od: onedrive: list: unsupported"}
	f.script = odScript(f, start)
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatalf("a OneDrive run whose listing is unsupported: %v", err)
	}
	byID := map[string]results.Mi5Dest{}
	for _, d := range data.Dests {
		byID[d.Dest] = d
	}
	if d := byID["dst-1"]; d.AuditFrom != AuditListRemote || d.Verified != 3 || d.Missing != 0 || d.Duplicates != 0 {
		t.Errorf("dst-1 %+v; want its checks from the listing", d)
	}
	od := byID["dst-od"]
	if od.AuditFrom != AuditEvents || od.Deferred != "" || od.Segments != 3 || od.Verified != 2 {
		t.Fatalf("dst-od %+v; want its checks from the events and the catalog", od)
	}
	if od.Missing != 1 || od.Duplicates != 1 || od.HashMismatches != 0 {
		t.Errorf("dst-od missing %d, duplicates %d, mismatches %d; want 1 (3 segments, 2 verified, the deleted one already out), 1 (od-2 verified twice), 0",
			od.Missing, od.Duplicates, od.HashMismatches)
	}
}

// TestMI5_AuditRefusesAFailedListing: a failed exit of diag uploads is
// accepted only for a destination whose List is unsupported: a Drive
// destination whose listing failed (its checks null), or a destination
// with no line, ends the run with the diag's error.
func TestMI5_AuditRefusesAFailedListing(t *testing.T) {
	diagErr := &hubproc.DiagError{Code: 1, Stderr: "ipcam-hub: diag uploads: dst-1: list failed"}
	for name, uploads := range map[string]string{
		"drive-null": `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":null,"duplicates":null,"hashMismatches":null}
{"schema":"ipcam.diag.uploads/1","dest":"dst-od","segments":2,"verified":2,"missing":null,"duplicates":null,"hashMismatches":null}`,
		"no-line": `{"schema":"ipcam.diag.uploads/1","dest":"dst-od","segments":2,"verified":2,"missing":null,"duplicates":null,"hashMismatches":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
			f := newFakeRun(t, start)
			f.oneDrive = true
			f.uploads, f.uploadsErr = uploads, diagErr
			f.script = odScript(f, start)
			if _, err := labRun(t, f, start); !errors.Is(err, diagErr) {
				t.Errorf("the audit's error %v; want the diag's", err)
			}
		})
	}
}

// TestMI5_OneDriveHashMismatchFromEvents (C2-R15 (c)): on OneDrive the
// hub's own verification is the hash check: a placement whose last
// upload ended in hash_mismatch counts as a mismatch, not as missing; one
// verified after a mismatch counts as neither.
func TestMI5_OneDriveHashMismatchFromEvents(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	od := DestConfig{ID: "dst-od", Kind: KindOneDrive, Account: "acct-1", Cameras: []string{"cam01"}}
	tl := newTally(start.UnixMilli(), []DestConfig{od})
	at := start.Add(time.Hour)
	for i, id := range []string{"od-1", "od-2", "od-3"} {
		observeAt(tl, "upload_queued", at, map[string]any{"placementId": id, "dest": "dst-od", "cam": "cam01", "profile": "sub",
			"startMs": start.Add(time.Duration(i+1) * time.Minute).UnixMilli(), "bytes": 1})
	}
	observeAt(tl, "upload_verified", at, map[string]any{"placementId": "od-1", "dest": "dst-od", "bytes": 1})
	observeAt(tl, "upload_failed", at, map[string]any{"placementId": "od-2", "dest": "dst-od", "reason": "hash_mismatch"})
	observeAt(tl, "upload_verified", at.Add(time.Second), map[string]any{"placementId": "od-2", "dest": "dst-od", "bytes": 1})
	observeAt(tl, "upload_failed", at, map[string]any{"placementId": "od-3", "dest": "dst-od", "reason": "hash_mismatch"})
	null := uploadsLine{Dest: "dst-od", Segments: 3, Verified: 2}
	got := tl.dest(od, null, nil)
	if got.HashMismatches != 1 || got.Missing != 0 || got.Duplicates != 0 || got.AuditFrom != AuditEvents {
		t.Errorf("dst-od %+v; want 1 mismatch (od-3), 0 missing, 0 duplicates", got)
	}
}

// TestMI5_HubCrashDuringBlock_RestartedAndCounted (Task 9's hub
// lifetime under mi5): a hub that ends on its own during the block,
// neither stopped nor killed by the run, sends nothing more, so a run
// that never watched it waited out the restart's bound (DrainMax: 12 h
// for the owner) for an upload after the unblock, then ended invalid
// with no data. The run notices the end at once, has the holder restart
// the hub (its line and hub-crash gap are the holder's), counts it in
// HubCrashes and goes on to its end; the run's own stops and its P16
// kill are no crash.
func TestMI5_HubCrashDuringBlock_RestartedAndCounted(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	f.script = labScript(f, start, 1<<20)
	var blockAt, crashedAt time.Time
	f.crash = func(step string, at time.Time) bool {
		if step == "block" {
			blockAt = at
		}
		if crashedAt.IsZero() && !blockAt.IsZero() && step == "sleep" && at.After(blockAt.Add(5*time.Minute)) {
			crashedAt = at
			return true
		}
		return false
	}
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatalf("a run whose hub crashed in the block: %v", err)
	}
	if crashedAt.IsZero() || len(f.restarts) != 1 || f.restarts[0].After(crashedAt.Add(time.Second)) || !f.restarts[0].Before(f.unblocked) {
		t.Fatalf("crashed at %v, restarts %v, unblocked %v; want one restart at once, in the block", crashedAt, f.restarts, f.unblocked)
	}
	if data.HubCrashes != 1 {
		t.Errorf("hubCrashes %d; want 1", data.HubCrashes)
	}
	if f.killed.IsZero() || data.Restart == nil || data.Restart.ResumedOffset != 1<<20 || data.Dests[0].Verified != 3 {
		t.Errorf("the run after the crash: killed %v, restart %+v, dst-1 %+v", f.killed, data.Restart, data.Dests[0])
	}
}

// TestMI5_RunStopsAreNoCrash: the run's own stops and its P16 kill end
// hubs on purpose, which no restart follows and HubCrashes never counts.
func TestMI5_RunStopsAreNoCrash(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	f.script = labScript(f, start, 1<<20)
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.restarts) != 0 || data.HubCrashes != 0 {
		t.Errorf("restarts %v, hubCrashes %d; want none", f.restarts, data.HubCrashes)
	}
}

// TestMI5_InjectedGapsAreTheKills: InjectedGaps counts the recording gaps
// that the P16 kill caused, open across the kill; a gap that a later
// crash of the restarted hub caused begins after that hub ran, and is the
// crash's (HubCrashes), not an injected one.
func TestMI5_InjectedGapsAreTheKills(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	kill := start.Add(time.Hour)
	tl := newTally(start.UnixMilli(), Destinations(Mi5Config{Cameras: 4, Drive: DriveTest}))
	tl.killedAt = kill.UnixMilli()
	observeAt(tl, "gap", kill.Add(3*time.Second), map[string]any{"cam": "cam01", "profile": "sub",
		"startMs": kill.Add(-time.Second).UnixMilli(), "endMs": kill.Add(3 * time.Second).UnixMilli()})
	tl.restartedAt = kill.Add(4 * time.Second).UnixMilli()
	observeAt(tl, "gap", kill.Add(6*time.Second), map[string]any{"cam": "cam02", "profile": "sub",
		"startMs": kill.Add(-time.Second).UnixMilli(), "endMs": kill.Add(5 * time.Second).UnixMilli()})
	crash := kill.Add(10 * time.Minute)
	observeAt(tl, "gap", crash.Add(5*time.Second), map[string]any{"cam": "cam01", "profile": "sub",
		"startMs": crash.UnixMilli(), "endMs": crash.Add(5 * time.Second).UnixMilli()})
	if tl.gapsAfterKill != 2 {
		t.Errorf("injected gaps %d; want the kill's 2, not the later crash's", tl.gapsAfterKill)
	}
}

// TestMI5_ReadDuringACrashIsReadAgain: an open-segment read that fails
// because the hub ended on its own during it measured the crash, not the
// open segment (G14 would take it for plan B's 2-min segments): the hub
// is restarted at once and the read made again.
func TestMI5_ReadDuringACrashIsReadAgain(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	f.script = labScript(f, start, 1<<20)
	var blockAt time.Time
	crashed := false
	f.crash = func(step string, at time.Time) bool {
		if step == "block" {
			blockAt = at
		}
		if !crashed && !blockAt.IsZero() && step == "diag-readopen-sub" && at.After(blockAt.Add(5*time.Minute)) {
			crashed = true
			return true
		}
		return false
	}
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatal(err)
	}
	if !crashed || data.HubCrashes != 1 || len(f.restarts) != 1 {
		t.Fatalf("crashed %v, hubCrashes %d, restarts %v; want the crash restarted once", crashed, data.HubCrashes, f.restarts)
	}
	if r := data.ReadOpen[1]; r.OK != r.Calls || len(r.Failures) != 0 {
		t.Errorf("the block's reads %+v; want the crash's read made again, none failed", r)
	}
}
