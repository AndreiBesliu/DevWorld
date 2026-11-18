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
	verify    func(n int) (ReachResult, error)
	checks    int
	readsOK   bool
	uploads   string // diag uploads' lines
	killed    time.Time
	unblocked time.Time
	lines     []string // the owner's lines
	// readAt, when set, answers each readopen at the clock's time: "" for
	// a read, else its error.
	readAt func(now time.Time) string
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
	f.mu.Unlock()
	if f.script != nil {
		f.send(f.script(name, at))
	}
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
	f.step("start-" + string(p))
	return nil
}
func (f *fakeRun) StopHub(context.Context) error          { f.step("stop"); return nil }
func (f *fakeRun) KillHub() error                         { f.killed = f.clock(); f.step("kill"); return nil }
func (f *fakeRun) ResetConnections(context.Context) error { f.step("reset-connections"); return nil }
func (f *fakeRun) HubConfig() hubproc.Config              { return hubproc.Config{} }
func (f *fakeRun) RunJSON() string                        { return "run.json" }
func (f *fakeRun) Printf(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, fmt.Sprintf(format, args...))
}
func (f *fakeRun) Diag(_ context.Context, args []string) ([]json.RawMessage, error) {
	f.step("diag-" + args[0] + "-" + strings.Join(args[len(args)-1:], ""))
	switch args[0] {
	case "readopen":
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
		return out, nil
	}
	return nil, nil
}

// fake firewall calls.
func (f *fakeRun) Block(context.Context) error { f.step("block"); return nil }
func (f *fakeRun) Unblock(context.Context) error {
	f.unblocked = f.clock()
	f.step("unblock")
	return nil
}
func (f *fakeRun) RemoveStale(context.Context) error { f.step("remove-stale"); return nil }
func (f *fakeRun) ScheduleCleanup(_ context.Context, at time.Time) error {
	f.step(fmt.Sprintf("cleanup-%d", at.UnixMilli()))
	return nil
}

// labRun runs Run with f, the lab's 15-min block and 30-s segments.
func labRun(t *testing.T, f *fakeRun, start time.Time) (*results.Mi5Data, error) {
	cfg := Mi5Config{Cameras: 4, SegmentSecs: 30, Drive: DriveTest, BlockFor: 15 * time.Minute, ReadOpenCalls: 20, Lab: true,
		ProviderHosts: []string{LabHost}, LanProbe: "10.1.0.4:9"}
	opts := RunOptions{Host: f, Firewall: f, StartMs: start.UnixMilli(), Dests: Destinations(cfg), Timing: LabTiming(cfg.BlockFor),
		Events: f.events, Clock: f.clock, Sleep: f.sleep, Resolve: func(context.Context, string) error { return nil },
		Verify: func(context.Context) (ReachResult, error) { f.checks++; return f.verify(f.checks) }}
	return Run(t.Context(), cfg, opts)
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
		ProviderHosts: []string{LabHost}, LanProbe: "10.1.0.4:9"}
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
// later; a camera with no open segment at all still fails.
func TestMI5_ReadOpenSettlesAfterRotation(t *testing.T) {
	const gap = time.Second
	for _, tc := range []struct {
		name    string
		latency time.Duration // from a rotation to its segment_closed
		never   bool          // no segment is ever open
		wantOK  int
	}{{"event-seen", 50 * time.Millisecond, false, 20}, {"event-late", 3 * time.Second, false, 20}, {"never-open", 50 * time.Millisecond, true, 0}} {
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
		})
	}
}
