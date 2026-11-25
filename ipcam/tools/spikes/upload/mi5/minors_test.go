package mi5

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// runTiming is labRunCtx with the timing tm.
func runTiming(ctx context.Context, f *fakeRun, start time.Time, tm Timing) (*results.Mi5Data, error) {
	cfg := Mi5Config{Cameras: 4, SegmentSecs: 30, Drive: DriveTest, BlockFor: 15 * time.Minute, ReadOpenCalls: 20, Lab: true,
		ProviderHosts: []string{LabHost}, LanProbe: "10.1.0.4:9", OneDrive: f.oneDrive}
	opts := RunOptions{Host: f, Firewall: f, StartMs: start.UnixMilli(), Dests: Destinations(cfg), Timing: tm,
		Events: f.events, Clock: f.clock, Sleep: f.sleep, Resolve: func(context.Context, string) error { return nil },
		Verify: func(context.Context) (ReachResult, error) { f.checks++; return f.verify(f.checks) }}
	return Run(ctx, cfg, opts)
}

// shortTiming is the lab's timing with a 5-min recovery bound, so that a
// run that waits it out ends soon on the virtual clock.
func shortTiming() Timing {
	tm := LabTiming(15 * time.Minute)
	tm.DrainMax = 5 * time.Minute
	return tm
}

// ownerLine reports whether err's text is a whole line for the owner.
func ownerLine(err error) bool {
	var l interface{ OwnerLine() }
	return errors.As(err, &l)
}

// withoutEvents is script without the events that drop matches.
func withoutEvents(script func(string, time.Time) []map[string]any, drop func(map[string]any) bool) func(string, time.Time) []map[string]any {
	return func(step string, at time.Time) []map[string]any {
		return slices.DeleteFunc(script(step, at), drop)
	}
}

// errFirewall is a firewall command's failure.
var errFirewall = errors.New("nft: exit status 1: Error: Could not process rule")

// errReset is the hub's control endpoint refusing reset-connections.
var errReset = errors.New("hubproc: control: reset-connections: 500 Internal Server Error")

// TestMI5_FailuresAreOwnerLines (C2-R29 (1), Task 9B M9): every way in
// which MI5 itself ends a run says so in Romanian, as a whole line for the
// owner (uploadspike prints it without «uploadspike: eroare:»), and keeps
// the diag's or the firewall's error as its cause: no upload in the middle
// of a placement after the unblock, the recovery's bound, diag dest-probe,
// diag uploads, diag reach, the block, its cleanup and its removal (an
// unblock that failed tells the owner how to remove it), the hub's
// reset-connections at the block's start (C2-R30 (1)), and the settings
// that the owner typed.
func TestMI5_FailuresAreOwnerLines(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	listed := `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	diagErr := &hubproc.DiagError{Code: 1, Stderr: "ipcam-hub: diag: failed"}
	run := func(t *testing.T, edit func(f *fakeRun)) error {
		t.Helper()
		f := newFakeRun(t, start)
		f.uploads = listed
		f.script = labScript(f, start, 1<<20)
		edit(f)
		_, err := runTiming(t.Context(), f, start, shortTiming())
		return err
	}
	for _, tc := range []struct {
		name  string
		err   func(t *testing.T) error
		line  string
		cause error
	}{
		{"no-upload-after-unblock", func(t *testing.T) error {
			return run(t, func(f *fakeRun) {
				f.script = withoutEvents(f.script, func(e map[string]any) bool { return e["type"] == "upload_progress" && e["offset"] == int64(1<<20) })
			})
		}, MsgNoUploadAfterUnblock, ErrNoUploadAfterUnblock},
		{"drain-bound", func(t *testing.T) error {
			return run(t, func(f *fakeRun) {
				f.script = withoutEvents(f.script, func(e map[string]any) bool { return e["type"] == "upload_verified" && e["placementId"] == "p-3" })
			})
		}, fmt.Sprintf(MsgDrainTooLong, "5 min"), nil},
		{"dest-probe", func(t *testing.T) error {
			return run(t, func(f *fakeRun) { f.oneDrive, f.script, f.destProbeErr = true, odScript(f, start), diagErr })
		}, fmt.Sprintf(MsgDestProbeFailed, "dst-od"), diagErr},
		{"diag-uploads-unlisted", func(t *testing.T) error {
			return run(t, func(f *fakeRun) {
				f.uploads = strings.ReplaceAll(listed, `"missing":0,"duplicates":0,"hashMismatches":0`, `"missing":null,"duplicates":null,"hashMismatches":null`)
				f.uploadsErr = diagErr
			})
		}, fmt.Sprintf(MsgAuditDestFailed, "dst-1"), diagErr},
		{"diag-uploads-no-run", func(t *testing.T) error {
			return run(t, func(f *fakeRun) { f.uploads, f.uploadsErr = "", os.ErrNotExist })
		}, MsgAuditFailed, os.ErrNotExist},
		{"diag-reach-no-provider", func(*testing.T) error { _, err := judgeReach(nil); return err }, MsgReachNoProvider, nil},
		{"block", func(t *testing.T) error { return run(t, func(f *fakeRun) { f.blockErr = errFirewall }) }, MsgBlockFailed, errFirewall},
		{"reset-connections", func(t *testing.T) error { return run(t, func(f *fakeRun) { f.resetErr = errReset }) }, MsgResetFailed, errReset},
		{"cleanup", func(t *testing.T) error { return run(t, func(f *fakeRun) { f.cleanupErr = errFirewall }) }, MsgCleanupFailed, errFirewall},
		{"unblock", func(t *testing.T) error { return run(t, func(f *fakeRun) { f.unblockErr = errFirewall }) }, MsgUnblockFailed, errFirewall},
		{"stale-block", func(t *testing.T) error { return run(t, func(f *fakeRun) { f.staleErr = errFirewall }) }, MsgUnblockFailed, errFirewall},
		{"linux-cleanup-timer", func(t *testing.T) error {
			fw := FirewallFor("linux", "/opt/ipcam-spike", func(_ context.Context, name string, _ []string, _ string) (string, error) {
				if name == "systemd-run" {
					return "", errFirewall
				}
				return "", nil
			})
			return fw.ScheduleCleanup(t.Context(), start.Add(time.Hour))
		}, MsgCleanupFailed, errFirewall},
		{"bad-drive", func(*testing.T) error {
			return Mi5Config{Cameras: 4, SegmentSecs: 30, BlockFor: time.Hour, Drive: "both"}.Validate()
		}, fmt.Sprintf(MsgBadDrive, "both"), nil},
		{"bad-block", func(*testing.T) error {
			return Mi5Config{Cameras: 4, SegmentSecs: 30, Drive: DriveTest}.Validate()
		}, fmt.Sprintf(MsgBadBlock, "0s"), nil},
		{"bad-segment", func(*testing.T) error {
			return Mi5Config{Cameras: 4, BlockFor: time.Hour, Drive: DriveTest}.Validate()
		}, fmt.Sprintf(MsgBadSegment, 0), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err(t)
			if err == nil {
				t.Fatal("no failure")
			}
			if !ownerLine(err) || err.Error() != tc.line || strings.Contains(err.Error(), "mi5:") {
				t.Errorf("the failure's line %q (an owner's line: %v); want %q", err, ownerLine(err), tc.line)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Errorf("the failure %v does not keep its cause %v", err, tc.cause)
			}
		})
	}
}

// TestMI5_BlockReadsCountRetried (C2-R27 (2), C2-R29 (6)): a read of the
// block whose first answer found no open segment and that read once more
// is counted in its series' Retried, beside the encrypted pre-run's, so
// that the envelope keeps the first misses that the second answers hide.
func TestMI5_BlockReadsCountRetried(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	f.script = labScript(f, start, 1<<20)
	n := 0
	f.readAt = func(time.Time) string { // every read's first answer misses, its second reads
		n++
		if n%2 == 1 {
			return "no-open-segment"
		}
		return ""
	}
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.ReadOpen) != 2 {
		t.Fatalf("reads %+v", data.ReadOpen)
	}
	for _, r := range data.ReadOpen {
		if r.OK != r.Calls || r.Retried != r.Calls {
			t.Errorf("reads %+v; want each read retried once and read", r)
		}
	}
}

// TestMI5_HubThatWillNotStartIsTriedOnce (C2-R29 (10)): a hub that ended
// on its own during an open-segment read and would not start again (the
// holder's restart gives up after its tries, errMI5HubDown) ends the run
// with that error at once: the read does not swallow it as a stopped read
// and leave the next wait to try the starts all over again.
func TestMI5_HubThatWillNotStartIsTriedOnce(t *testing.T) {
	for _, during := range []string{"prerun", "block"} {
		t.Run(during, func(t *testing.T) {
			start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
			f := newFakeRun(t, start)
			f.script = labScript(f, start, 1<<20)
			down := errors.New("the hub would not start again")
			f.restartErr = down
			var blockAt time.Time
			crashed := false
			f.crash = func(step string, at time.Time) bool {
				if step == "block" {
					blockAt = at
				}
				if !crashed && step == "diag-readopen-sub" && (during == "prerun" || !blockAt.IsZero() && at.After(blockAt.Add(5*time.Minute))) {
					crashed = true
					return true
				}
				return false
			}
			_, err := labRun(t, f, start)
			if !crashed || !errors.Is(err, down) {
				t.Fatalf("crashed %v, the run's end %v; want %v", crashed, err, down)
			}
			if f.restartCalls != 1 {
				t.Errorf("the holder's restart was asked %d times; want once (its own tries are mi5CrashTries)", f.restartCalls)
			}
			if during == "block" && f.unblocked.IsZero() {
				t.Error("the run ended in the block without the unblock")
			}
		})
	}
}

// TestMI5_RestartTargetsTheRunningHubsUpload (P16, C2-R29 (11)): a hub
// that crashed after the unblock in the middle of an upload left its
// upload_progress in the tally; the restarted hub resumes that placement
// only later. The restart's kill waits for the running hub's own progress
// (0 < offset < size), so that it lands between two of its parts, and
// records that progress's offset as the last confirmed one, never the
// crashed hub's.
func TestMI5_RestartTargetsTheRunningHubsUpload(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	base := labScript(f, start, 2<<20)
	const newOffset = int64(3 << 19) // 1.5 MiB of 4 MiB
	var oldAt time.Time
	newSent := false
	f.script = func(step string, at time.Time) []map[string]any {
		evs := base(step, at)
		for _, e := range evs {
			if e["type"] == "upload_progress" && e["offset"] == int64(1<<20) {
				// The upload of the hub that is about to crash, with a
				// rotation: the restart's moment waits tearAfter.
				oldAt = at
				evs = append(evs, map[string]any{"type": "segment_closed", "cam": "cam04", "profile": "sub", "startMs": at.Add(-30 * time.Second).UnixMilli()})
				break
			}
		}
		f.mu.Lock()
		restarts := slices.Clone(f.restarts)
		f.mu.Unlock()
		if len(restarts) == 1 && !newSent && at.Sub(restarts[0]) >= 30*time.Second {
			newSent = true
			evs = append(evs, map[string]any{"type": "upload_progress", "placementId": "p-2", "dest": "dst-1", "offset": newOffset, "size": int64(4 << 20)})
		}
		return evs
	}
	crashed := false
	f.crash = func(step string, _ time.Time) bool {
		if !crashed && !oldAt.IsZero() && step == "sleep" {
			crashed = true
			return true
		}
		return false
	}
	data, err := labRun(t, f, start)
	if err != nil {
		t.Fatal(err)
	}
	if !crashed || len(f.restarts) != 1 || data.HubCrashes != 1 {
		t.Fatalf("crashed %v, restarts %v, hubCrashes %d; want one crash after the unblock, restarted", crashed, f.restarts, data.HubCrashes)
	}
	if f.killed.Before(f.restarts[0].Add(30 * time.Second)) {
		t.Errorf("killed %v after the crash's restart; want after the running hub's own progress (30 s)", f.killed.Sub(f.restarts[0]))
	}
	if r := data.Restart; r == nil || r.LastConfirmedOffset != newOffset {
		t.Errorf("restart %+v; want the running hub's last confirmed offset %d, not the crashed hub's %d", data.Restart, newOffset, 1<<20)
	}
}

// TestMI5_RecoveryBoundFromTheUnblock (C2-R27 (3), C2-R29 (12)): DrainMax
// bounds the whole recovery from the unblock, the restart's wait and the
// drains together, so that a run whose uploads stop moving ends with its
// own line within DrainMax of the unblock (in the lab, before the mi5-lab
// step's timeout), not DrainMax after each of its waits.
func TestMI5_RecoveryBoundFromTheUnblock(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	f := newFakeRun(t, start)
	f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":3,"verified":3,"missing":0,"duplicates":0,"hashMismatches":0}`
	base := withoutEvents(labScript(f, start, 1<<20), func(e map[string]any) bool { return e["type"] == "upload_verified" && e["placementId"] == "p-3" })
	var held []map[string]any
	f.script = func(step string, at time.Time) []map[string]any {
		evs := base(step, at)
		// The upload after the unblock moves only 6 min later (the outbox's
		// backoff after the outage).
		for i, e := range evs {
			if e["type"] == "upload_progress" && e["offset"] == int64(1<<20) {
				held, evs = append(held, e), slices.Delete(evs, i, i+1)
				break
			}
		}
		if len(held) > 0 && !f.unblocked.IsZero() && !at.Before(f.unblocked.Add(6*time.Minute)) {
			for _, e := range held {
				e["atMs"] = at.UnixMilli()
			}
			evs, held = append(evs, held...), nil
		}
		return evs
	}
	tm := LabTiming(15 * time.Minute)
	tm.DrainMax = 10 * time.Minute
	_, err := runTiming(t.Context(), f, start, tm)
	if err == nil || err.Error() != fmt.Sprintf(MsgDrainTooLong, "10 min") {
		t.Fatalf("the run's end %v; want the recovery's bound", err)
	}
	if f.killed.IsZero() || f.killed.Before(f.unblocked.Add(6*time.Minute)) {
		t.Fatalf("killed %v after the unblock; want the restart once the upload moved", f.killed.Sub(f.unblocked))
	}
	if took := f.clock().Sub(f.unblocked); took > tm.DrainMax+time.Second {
		t.Errorf("the run ended %v after the unblock; want within DrainMax (%v)", took, tm.DrainMax)
	}
}

// labStepTimeout is the MI5 step's timeout-minutes in the mi5-lab job.
func labStepTimeout(t *testing.T) time.Duration {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", ".github", "workflows", "ipcam-spike-upload-lab.yml")
	f, err := os.Open(path) //nolint:gosec // the repository's own workflow
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	inStep := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "id: mi5":
			inStep = true
		case inStep && strings.HasPrefix(line, "- name:"):
			inStep = false
		case inStep && strings.HasPrefix(line, "timeout-minutes:"):
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "timeout-minutes:")))
			if err != nil {
				t.Fatal(err)
			}
			return time.Duration(n) * time.Minute
		}
	}
	t.Fatalf("%s: the MI5 step has no timeout-minutes", path)
	return 0
}

// TestLabTiming_EndsBeforeTheLabStep (C2-R27 (3), C2-R29 (12)): a lab run
// that waits out its recovery's bound still ends, with its own Romanian
// line and its envelope, before the mi5-lab job's MI5 step times out: the
// pre-run, the warm-up, the 15-min block and DrainMax, with 5 min for the
// logins, the hub's starts, the checks, the audit and the closing scan.
func TestLabTiming_EndsBeforeTheLabStep(t *testing.T) {
	const block, margin = 15 * time.Minute, 5 * time.Minute
	tm := LabTiming(block)
	if worst, step := tm.PreRun+tm.WarmUp+block+tm.DrainMax+margin, labStepTimeout(t); worst > step {
		t.Errorf("a lab run can last %v (DrainMax %v); the MI5 step times out at %v", worst, tm.DrainMax, step)
	}
}

// finalDrainScript is labScript whose first drain ends firstAfter the
// unblock (every verification held until then), and whose final hub, the
// drain without recording, queues the segment that the stop finalised
// (p-4) and verifies it finalAfter the unblock; a negative finalAfter
// never verifies it.
func finalDrainScript(f *fakeRun, start time.Time, firstAfter, finalAfter time.Duration) func(string, time.Time) []map[string]any {
	base := labScript(f, start, 1<<20)
	var held []map[string]any
	queued, verified := false, false
	return func(step string, at time.Time) []map[string]any {
		evs := slices.DeleteFunc(base(step, at), func(e map[string]any) bool {
			if e["type"] == "upload_verified" {
				held = append(held, e)
				return true
			}
			return false
		})
		if len(held) > 0 && !f.unblocked.IsZero() && !at.Before(f.unblocked.Add(firstAfter)) {
			evs, held = append(evs, held...), nil
		}
		if step == "start-final" && !queued {
			queued = true
			evs = append(evs, map[string]any{"type": "upload_queued", "placementId": "p-4", "dest": "dst-1", "cam": "cam01", "profile": "sub",
				"startMs": at.Add(-20 * time.Second).UnixMilli(), "bytes": int64(1 << 20)})
		}
		if queued && !verified && finalAfter >= 0 && !at.Before(f.unblocked.Add(finalAfter)) {
			verified = true
			evs = append(evs, map[string]any{"type": "upload_verified", "placementId": "p-4", "dest": "dst-1", "bytes": int64(1 << 20),
				"calls": map[string]float64{"put": 2, "get": 1}})
		}
		return evs
	}
}

// TestMI5_FinalDrainHasItsOwnAllowance (C2-R30 (3)): in the owner's
// timing, a backlog that finished uploading just before the recovery's
// bound (unblock + DrainMax) is followed by a final drain (the stop, the
// start without recording, the segments that the stop finalised, Quiet)
// that has its own allowance from the first drain's end, so the run
// finishes instead of ending invalid; past that allowance it ends with its
// own line. The lab keeps one budget from the unblock (C2-R29 (12)), which
// ends before the mi5-lab step's timeout. DrainMax is 10 min here, so that
// the virtual clock reaches it soon.
func TestMI5_FinalDrainHasItsOwnAllowance(t *testing.T) {
	const drainMax, firstAfter = 10 * time.Minute, 10*time.Minute - 10*time.Second
	owner := OwnerTiming(15 * time.Minute)
	owner.DrainMax = drainMax
	lab := LabTiming(15 * time.Minute)
	lab.DrainMax = drainMax
	for _, tc := range []struct {
		name       string
		tm         Timing
		finalAfter time.Duration
		want       string        // the run's end; "" for none
		endsBy     time.Duration // after the unblock
	}{
		{"owner", owner, 12 * time.Minute, "", firstAfter + owner.FinalDrain},
		{"owner-final-never", owner, -1, fmt.Sprintf(MsgFinalDrainTooLong, elapsed(owner.FinalDrain)), firstAfter + owner.FinalDrain},
		{"lab", lab, 12 * time.Minute, fmt.Sprintf(MsgDrainTooLong, "10 min"), drainMax},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
			f := newFakeRun(t, start)
			f.uploads = `{"schema":"ipcam.diag.uploads/1","dest":"dst-1","segments":4,"verified":4,"missing":0,"duplicates":0,"hashMismatches":0}`
			f.script = finalDrainScript(f, start, firstAfter, tc.finalAfter)
			data, err := runTiming(t.Context(), f, start, tc.tm)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want || err != nil && !ownerLine(err) {
				t.Fatalf("the run's end %q; want %q", got, tc.want)
			}
			if took := f.clock().Sub(f.unblocked); took > tc.endsBy+time.Second {
				t.Errorf("the run ended %v after the unblock; want by %v", took, tc.endsBy)
			}
			if err == nil && (data.RecoverySecs < firstAfter.Seconds() || data.RecoverySecs > drainMax.Seconds()) {
				t.Errorf("recovery %v s; want the first drain's (%v)", data.RecoverySecs, firstAfter)
			}
		})
	}
}
