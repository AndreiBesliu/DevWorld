package mi1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/lastindex"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// rig is one host's folders, its fake log and its screen.
type rig struct {
	f             *fakeController
	results, work string
	log           string
	out           *syncBuffer
	host          string
}

func newRig(t *testing.T, host string) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{f: startFakeController(t), results: filepath.Join(dir, "results"), work: filepath.Join(dir, "work"),
		log: filepath.Join(dir, "fake.log"), out: &syncBuffer{}, host: host}
	t.Setenv(fakeLogEnv, r.log)
	t.Cleanup(func() { killFakeHubs(t, r.log) })
	return r
}

// driver is a new recspike process on the rig, as after a reboot.
func (r *rig) driver(t *testing.T) *Driver {
	t.Helper()
	return testDriver(t, r.f, r.host, r.results, r.work, r.out)
}

// cutPower runs the set until the controller has sent the cut of its first
// run and the host has acted on it, then cuts the host's power: recspike
// ends at once, and so does ipcam-hub. stopped is when the host is down: the
// OS stop for sysrq and bugcheck, the prompt for a real cut.
func (r *rig) cutPower(t *testing.T, o SetOptions, stopped func(d *Driver, down func())) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := r.driver(t)
	stopped(d, cancel)
	err := d.RunSet(ctx, o)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunSet until the power cut = %v, want context.Canceled", err)
	}
	killFakeHubs(t, r.log)
}

// TestResume_AfterReboot_StartsHubBeforeNTPWait (C07, Review Focus 2):
// after the reboot of a sysrq run, `recspike resume` lists the segment
// files of the stop and starts ipcam-hub at once, before it waits for NTP;
// ColdBootMs is the host's uptime at the last job's `recording` event;
// lastindex reads only the files of the stop, not the segment that the
// restarted ipcam-hub opened; and the report goes to the controller.
func TestResume_AfterReboot_StartsHubBeforeNTPWait(t *testing.T) {
	r := newRig(t, "pi")
	r.cutPower(t, SetOptions{Set: SetPiSysrq, Mode: ModeAutomated, Kind: "sysrq", Window: windowAroundNow(t)},
		func(d *Driver, down func()) {
			d.OSStop = func(_ context.Context, kind controller.ShutdownKind) error {
				if kind != controller.KindSysrq {
					t.Errorf("OS stop %s, want sysrq", kind)
				}
				down()
				return nil
			}
		})
	before := logEntries(t, r.log)
	if !slices.Equal(before, []string{"hub"}) {
		t.Fatalf("fake log before the reboot = %v, want one ipcam-hub start", before)
	}
	runDir := filepath.Join(r.work, "pi-sysrq-01-1", "data")
	stopFiles, err := filepath.Glob(filepath.Join(runDir, "rec", "cam01", "main", "*", "*"))
	if err != nil || len(stopFiles) != 1 {
		t.Fatalf("segment files at the stop: %v, %v", stopFiles, err)
	}

	d := r.driver(t)
	d.Uptime = func() (time.Duration, error) { return 41 * time.Second, nil }
	d.NTP = func(context.Context) (bool, error) {
		logLine("ntp")
		return true, nil
	}
	var read lastindex.Before
	d.LastRecorded = func(_ context.Context, files lastindex.Before, id age.Identity) (uint64, error) {
		if id == nil {
			t.Error("lastindex got no identity for an encrypted run")
		}
		read = files
		return 1234, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	next := cancelAt(r.f, "run_start pi-sysrq-02-1", cancel) // the set goes on with run 2
	if err := d.Resume(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resume = %v, want the test's cancel during run 2", err)
	}
	if err := <-next; err != nil {
		t.Fatal(err)
	}

	entries := logEntries(t, r.log)
	if len(entries) < 3 || !slices.Equal(entries[:3], []string{"hub", "hub", "ntp"}) {
		t.Errorf("fake log = %v, want ipcam-hub restarted before the NTP wait", entries)
	}
	if !slices.Equal(read, lastindex.Before(stopFiles)) {
		t.Errorf("lastindex read %v, want only the files of the stop %v", read, stopFiles)
	}
	reports := r.f.runReports()
	if len(reports) != 1 {
		t.Fatalf("%d reports, want 1", len(reports))
	}
	rep := reports[0]
	if rep.RunID != "pi-sysrq-01-1" || len(rep.Streams) != 1 || rep.Streams[0] != (link.StreamIndex{Stream: MainStream, LastRecordedIndex: 1234}) {
		t.Errorf("report = %+v", rep)
	}
	if rep.ColdBootMs == nil || *rep.ColdBootMs > 41_000 || *rep.ColdBootMs < 40_000 {
		t.Errorf("ColdBootMs = %v, want the 41 s of uptime at the recording event", rep.ColdBootMs)
	}
	if rep.ResumeMs != nil || !rep.NtpSynced || !rep.Hub04Clear || rep.WriteCache != "unknown" || len(rep.Reconcile) == 0 || len(rep.Verify) == 0 {
		t.Errorf("report = %+v, want no ResumeMs after a reboot, NTP synced, HUB-04 clear, the write cache and both events", rep)
	}
	st, err := LoadRunState(filepath.Join(r.results, "state", "run-state.json"))
	if err != nil || st == nil || len(st.Done) != 1 || st.Done[0].Run.RunID != "pi-sysrq-01-1" {
		t.Errorf("run state after the resume = %+v, %v; want run 1 done", st, err)
	}
	if _, err := os.Stat(filepath.Join(r.work, "pi-sysrq-01-1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run 1's folder is still there (%v): its recordings fill the disk", err)
	}
}

// realCut runs a real set until the prompt, then cuts the power.
func realCut(t *testing.T, r *rig, set string) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := r.driver(t)
	cut := make(chan error, 1)
	go func() {
		defer cancel()
		if err := r.f.waitEvent("cut "); err != nil {
			cut <- err
			return
		}
		waitFor(t, "the prompt on recspike's screen", func() bool { return strings.Contains(r.out.String(), "Scoateți acum alimentarea") })
		cut <- nil
	}()
	if err := d.RunSet(ctx, SetOptions{Set: set, Mode: ModeReal, Kind: "power"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunSet until the power cut = %v, want context.Canceled", err)
	}
	killFakeHubs(t, r.log)
	if err := <-cut; err != nil {
		t.Fatal(err)
	}
}

// waitFor polls cond for up to 30 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Errorf("timed out waiting for %s", what)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestResume_RealSet_LinuxContinues (G14): on Linux, the boot service
// finishes the cut's report and starts the next real cut itself; the prompt
// shows on the PC's controller.
func TestResume_RealSet_LinuxContinues(t *testing.T) {
	r := newRig(t, "pi")
	realCut(t, r, SetRealPi)
	if s := r.f.runStarts(); len(s) != 1 || s[0].RunID != "real-pi-01-1" || s[0].Slot != 1 || !s[0].Encrypted || s[0].Kind != "power" {
		t.Fatalf("run starts before the cut = %+v, want slot 1, encrypted, power", s)
	}
	d := r.driver(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	next := cancelAt(r.f, "run_start real-pi-02-2", cancel)
	if err := d.Resume(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resume = %v, want the test's cancel at the next cut", err)
	}
	if err := <-next; err != nil {
		t.Fatal(err)
	}
	if reps := r.f.runReports(); len(reps) != 1 || reps[0].RunID != "real-pi-01-1" {
		t.Errorf("reports = %+v, want the cut's", reps)
	}
	s := r.f.runStarts()
	if len(s) != 2 || s[1].Slot != 2 || s[1].Attempt != 2 || s[1].Encrypted {
		t.Errorf("run starts = %+v, want slot 2, attempt 2, in clear", s)
	}
}

// TestResume_RealSet_WindowsWaitsForShortcut (G14): on Windows, the boot
// task finishes the cut's report and exits; the owner continues with the
// desktop shortcut, which shows the prompt.
func TestResume_RealSet_WindowsWaitsForShortcut(t *testing.T) {
	r := newRig(t, "pc")
	realCut(t, r, SetRealPC)
	d := r.driver(t)
	d.GOOS = "windows"
	if err := d.Resume(t.Context()); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if reps := r.f.runReports(); len(reps) != 1 || reps[0].RunID != "real-pc-01-1" {
		t.Errorf("reports = %+v, want the cut's", reps)
	}
	if s := r.f.runStarts(); len(s) != 1 {
		t.Errorf("run starts = %+v, want none after the reboot", s)
	}
	if !strings.Contains(r.out.String(), "„"+RealShortcut+"”") {
		t.Errorf("screen:\n%s\nwant the owner sent to the shortcut %q", r.out.String(), RealShortcut)
	}
}

// TestResume_SecondInstanceWaits (G14): while one recspike holds the run
// state's lock, as the boot task does after a PC cut, a second one prints
// the Romanian waiting line, starts no ipcam-hub, and continues once the
// lock is free.
func TestResume_SecondInstanceWaits(t *testing.T) {
	r := newRig(t, "pc")
	realCut(t, r, SetRealPC)
	hubsBefore := len(logEntries(t, r.log))

	unlock, err := LockRunState(t.Context(), filepath.Join(r.results, "state", "run-state.lock"), nil)
	if err != nil {
		t.Fatal(err)
	}
	d := r.driver(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.RunSet(ctx, SetOptions{Set: SetRealPC, Mode: ModeReal, Kind: "power", Resume: true}) }()
	waitFor(t, "the waiting line", func() bool { return strings.Contains(r.out.String(), WaitingLine) })
	time.Sleep(300 * time.Millisecond)
	if n := len(logEntries(t, r.log)); n != hubsBefore {
		t.Errorf("ipcam-hub started %d times while the lock was held", n-hubsBefore)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	r.f.mustEvent(t, "report real-pc-01-1")
	r.f.mustEvent(t, "run_start real-pc-02-2")
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("RunSet = %v, want the test's cancel", err)
	}
	if WaitingLine != "Proba termină verificarea de după repornire. Așteptați…" {
		t.Errorf("WaitingLine = %q", WaitingLine)
	}
}

// TestWindow_DriverRefusesACutAfterTheWindow (G09): a kill that the
// controller asks for after the window has closed is refused: ipcam-hub is
// stopped cleanly, not killed, the controller hears why, the run is redone
// later, and the set stops until the next window.
func TestWindow_DriverRefusesACutAfterTheWindow(t *testing.T) {
	r := newRig(t, "pc")
	w, err := ParseWindow("22:00-23:00")
	if err != nil {
		t.Fatal(err)
	}
	clock := newStepClock()
	clock.set(at("22:30"))
	r.f.onReady = func(c *link.Conn, start link.RunStart) {
		clock.set(at("23:05"))
		_ = c.Send(link.Cut{RunID: start.RunID, Kind: start.Kind, PromptAt: time.Now()})
	}
	d := r.driver(t)
	d.Clock = clock
	if err := d.RunSet(t.Context(), SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: &w}); err != nil {
		t.Fatalf("RunSet = %v, want nil: the set waits for the next window", err)
	}
	r.f.mustEvent(t, "refused pc-kill9-01-1")
	if reps := r.f.runReports(); len(reps) != 0 {
		t.Errorf("reports = %+v, want none for a refused stop", reps)
	}
	if got := logEntries(t, r.log); !slices.Equal(got, []string{"hub", "stopped"}) {
		t.Errorf("fake log = %v, want one ipcam-hub, stopped cleanly", got)
	}
	if !strings.Contains(r.out.String(), "în afara ferestrei") {
		t.Errorf("screen:\n%s\nwant the refusal", r.out.String())
	}
	st, err := LoadRunState(filepath.Join(r.results, "state", "run-state.json"))
	if err != nil || st == nil {
		t.Fatalf("run state: %+v, %v", st, err)
	}
	plan, err := Plan(SetPCKill9, 1)
	if err != nil {
		t.Fatal(err)
	}
	if next, attempt, ok := st.nextAutomated(plan); !ok || next.Index != 1 || attempt != 2 {
		t.Errorf("next = run %d, attempt %d, %v; want run 1 again, attempt 2", next.Index, attempt, ok)
	}
}

// TestSession_PCPullsPiResultsAtEnd (G15): at the end of every session the
// PC side pulls the Pi's results folder into results/pi/: recspike on the
// PC asks the Pi's controller, and recspike on the Pi answers the PC's
// controller when it says bye.
func TestSession_PCPullsPiResultsAtEnd(t *testing.T) {
	complete := func(t *testing.T, r *rig, set string) {
		t.Helper()
		st := &RunState{Set: set, Mode: ModeReal, Kind: "power", Host: HostState{Pseudonym: r.host, OS: "linux", Storage: "ssd"}}
		for slot := 1; slot <= 5; slot++ {
			st.Done = append(st.Done, DoneRun{Index: slot, Run: results.Run{RunID: set + "-0" + string(rune('0'+slot)) + "-1", Valid: true, Slot: slot, Attempt: slot}})
		}
		if err := SaveRunState(filepath.Join(r.results, "state", "run-state.json"), st); err != nil {
			t.Fatal(err)
		}
	}
	writeFile := func(t *testing.T, path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("recspike on the PC", func(t *testing.T) {
		r := newRig(t, "pc")
		complete(t, r, SetRealPC)
		writeFile(t, filepath.Join(r.f.sendResults, "MI1", "pc-kill9", "pc-kill9-01-1.json"), "{}\n")
		if err := r.driver(t).RunSet(t.Context(), SetOptions{Set: SetRealPC, Mode: ModeReal, Kind: "power", Resume: true}); err != nil {
			t.Fatalf("RunSet: %v", err)
		}
		r.f.mustEvent(t, "pull_results")
		r.f.mustEvent(t, "bye")
		if _, err := os.Stat(filepath.Join(r.results, "pi", "MI1", "pc-kill9", "pc-kill9-01-1.json")); err != nil {
			t.Errorf("the Pi's results are not under results/pi: %v", err)
		}
	})

	t.Run("recspike on the Pi", func(t *testing.T) {
		r := newRig(t, "pi")
		r.f.pullAtBye = true
		complete(t, r, SetRealPi)
		writeFile(t, filepath.Join(r.results, "MI2", "pi", "mi2-1.json"), "{}\n")
		if err := r.driver(t).RunSet(t.Context(), SetOptions{Set: SetRealPi, Mode: ModeReal, Kind: "power", Resume: true}); err != nil {
			t.Fatalf("RunSet: %v", err)
		}
		r.f.mustEvent(t, "bye")
		if _, err := os.Stat(filepath.Join(r.f.pulled, "MI2", "pi", "mi2-1.json")); err != nil {
			t.Errorf("the PC's controller did not get the Pi's results: %v", err)
		}
		if _, err := os.Stat(filepath.Join(r.results, "pi")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the Pi pulled a results/pi folder of its own (%v)", err)
		}
	})
}

// TestLoop_RunsThatDoNotCountStopThisRecspikeNotTheSet (G10): runs that do
// not count are excluded and redone; three in a row stop this recspike with
// a Romanian line that says how to go on, instead of stopping the host
// again and again for a cause that persists. The set stays open: the same
// command continues it at the same position.
func TestLoop_RunsThatDoNotCountStopThisRecspikeNotTheSet(t *testing.T) {
	r := newRig(t, "pc")
	var mu sync.Mutex
	invalid := 3
	r.f.verdict = func(start link.RunStart, m link.Report) results.Run {
		mu.Lock()
		defer mu.Unlock()
		run := results.Run{RunID: m.RunID, Valid: true, Attempt: start.Attempt, ShutdownKind: start.Kind, Encrypted: start.Encrypted,
			PadLowBitrate: start.PadLowBitrate, Reconciled: true, Hub04Clear: m.Hub04Clear}
		if invalid > 0 {
			invalid--
			run.Valid, run.InvalidReason = false, "controlorul a trimis neuniform"
		}
		return run
	}
	o := SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: windowAroundNow(t)}
	err := r.driver(t).RunSet(t.Context(), o)
	if err == nil || !strings.Contains(err.Error(), "3 rulări la rând nu au contat") || !strings.Contains(err.Error(), "aceeași comandă") {
		t.Fatalf("RunSet = %v, want the set stopped after 3 runs in a row that did not count, with how to go on", err)
	}
	var ids []string
	for _, s := range r.f.runStarts() {
		ids = append(ids, s.RunID)
	}
	if want := []string{"pc-kill9-01-1", "pc-kill9-01-2", "pc-kill9-01-3"}; !slices.Equal(ids, want) {
		t.Fatalf("runs = %v, want %v", ids, want)
	}

	// The owner starts the same command again: run 1 is redone, counts,
	// and the set goes on.
	d := r.driver(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.RunSet(ctx, o) }()
	select {
	case err := <-done:
		t.Fatalf("RunSet again = %v, want the set continued", err)
	case <-waitEventCh(r.f, "run_start pc-kill9-02-1"):
	case <-time.After(30 * time.Second):
		t.Fatal("the set did not go on to run 2")
	}
	cancel()
	<-done
	ids = ids[:0]
	for _, s := range r.f.runStarts() {
		ids = append(ids, s.RunID)
	}
	if want := []string{"pc-kill9-01-1", "pc-kill9-01-2", "pc-kill9-01-3", "pc-kill9-01-4", "pc-kill9-02-1"}; !slices.Equal(ids, want) {
		t.Errorf("runs = %v, want %v", ids, want)
	}
}

// cancelAt calls cancel once the fake controller has an event that starts
// with prefix, or after waitEvent's timeout; the channel then gives
// waitEvent's error, for the test's goroutine.
func cancelAt(f *fakeController, prefix string, cancel context.CancelFunc) <-chan error {
	done := make(chan error, 1)
	go func() {
		defer cancel()
		done <- f.waitEvent(prefix)
	}()
	return done
}

// waitEventCh is closed when the fake controller has an event that starts
// with prefix; it never fails the test from its goroutine.
func waitEventCh(f *fakeController, prefix string) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		for e := range f.events {
			if strings.HasPrefix(e, prefix) {
				close(ch)
				return
			}
		}
	}()
	return ch
}

// TestAwaitCut_IgnoresACutOfAnotherRun (C16): recspike stops the host only
// for a cut of the run it records. A late cut of a run that has ended (its
// schedule outlived it at the controller) is ignored, and the run is
// stopped at its own cut.
func TestAwaitCut_IgnoresACutOfAnotherRun(t *testing.T) {
	r := newRig(t, "pc")
	type ready struct {
		c     *link.Conn
		start link.RunStart
	}
	readies := make(chan ready, 4)
	r.f.onReady = func(c *link.Conn, start link.RunStart) {
		if start.RunID == "pc-kill9-01-1" {
			_ = c.Send(link.Cut{RunID: "pc-bugcheck-07-2", Kind: "kill9", PromptAt: time.Now()})
		}
		readies <- ready{c, start}
	}
	d := r.driver(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- d.RunSet(ctx, SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: windowAroundNow(t)})
	}()
	var first ready
	select {
	case first = <-readies:
	case err := <-done:
		t.Fatalf("RunSet = %v before the run was ready", err)
	case <-time.After(30 * time.Second):
		t.Fatal("the run never got ready")
	}
	waitFor(t, "the stale cut on the screen", func() bool { return strings.Contains(r.out.String(), "pc-bugcheck-07-2") })
	time.Sleep(500 * time.Millisecond)
	if got := logEntries(t, r.log); !slices.Equal(got, []string{"hub"}) {
		t.Errorf("after the stale cut, fake log = %v, want ipcam-hub still recording, not killed and restarted", got)
	}
	if reps := r.f.runReports(); len(reps) != 0 {
		t.Errorf("reports after the stale cut = %+v, want none", reps)
	}
	if err := first.c.Send(link.Cut{RunID: first.start.RunID, Kind: "kill9", PromptAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waitEventCh(r.f, "report pc-kill9-01-1"):
	case err := <-done:
		t.Fatalf("RunSet = %v before the run's report", err)
	case <-time.After(30 * time.Second):
		t.Fatal("no report after the run's own cut")
	}
	cancel()
	<-done
	if got := logEntries(t, r.log); len(got) < 2 || !slices.Equal(got[:2], []string{"hub", "hub"}) {
		t.Errorf("fake log = %v, want ipcam-hub killed and restarted at the run's own cut", got)
	}
}

// TestBegin_LabShardsAndSmokeRunsKeepTheirOwnState (R03): a lab run state
// belongs to its set, shard and smoke flag. In the same results folder, a
// full shard after a smoke run of it, or shard 2 after shard 1, starts a
// state of its own instead of reporting complete from the old one, and a
// shard still in progress refuses another in Romanian; the same shard
// continues its own state.
func TestBegin_LabShardsAndSmokeRunsKeepTheirOwnState(t *testing.T) {
	d := &Driver{Results: t.TempDir(), Host: LabHost("linux")}
	state := func(shard int, smoke bool, counted int) *RunState {
		st := &RunState{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: shard, Smoke: smoke, Host: d.Host}
		plan, err := d.plan(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range plan[:min(counted, len(plan))] {
			st.Done = append(st.Done, DoneRun{Index: p.Index, Run: results.Run{RunID: fmt.Sprintf("%s-%02d-1", st.Set, p.Index), Valid: true}})
		}
		return st
	}
	lab := func(shard int, smoke bool) SetOptions {
		return SetOptions{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: shard, Smoke: smoke}
	}
	for _, tc := range []struct {
		name string
		old  *RunState
		o    SetOptions
	}{
		{"a full shard 1 after its smoke run", state(1, true, 2), lab(1, false)},
		{"shard 2 after shard 1", state(1, false, 10), lab(2, false)},
		{"a smoke run after a full shard 1", state(1, false, 10), lab(1, true)},
	} {
		if !d.complete(tc.old) {
			t.Fatalf("%s: the old state is not complete", tc.name)
		}
		st, err := d.begin(t.Context(), tc.old, tc.o)
		if err != nil {
			t.Errorf("%s: begin = %v", tc.name, err)
			continue
		}
		if st.Shard != tc.o.Shard || st.Smoke != tc.o.Smoke || len(st.Done) != 0 || d.complete(st) {
			t.Errorf("%s: begin gave shard %d, smoke %v, %d runs done, complete %v; want a new state of its own",
				tc.name, st.Shard, st.Smoke, len(st.Done), d.complete(st))
		}
	}

	half := state(1, false, 3)
	if _, err := d.begin(t.Context(), half, lab(2, false)); err == nil || !strings.Contains(err.Error(), "nu s-a încheiat") || !strings.Contains(err.Error(), "shard 1") {
		t.Errorf("shard 2 while shard 1 is in progress: begin = %v, want a Romanian refusal that names shard 1", err)
	}
	if st, err := d.begin(t.Context(), half, lab(1, false)); err != nil || st != half {
		t.Errorf("shard 1 again: begin = %+v, %v; want its own state continued", st, err)
	}
}

// TestRunSet_ReadsTheHostOnlyForASetThatStarts (C24, G16): a new set's
// host, with the owner's answers, is read once the set is sure to start:
// not while another set is in progress, which refuses it; and the host that
// it reads is the one the run state and the runs keep.
func TestRunSet_ReadsTheHostOnlyForASetThatStarts(t *testing.T) {
	r := newRig(t, "pc")
	asked := 0
	host := func(context.Context) (HostState, error) {
		asked++
		return HostState{Pseudonym: "pc", OS: "windows", Storage: "ssd", WriteCache: "unknown", Cameras: 2}, nil
	}
	state := filepath.Join(r.results, "state", "run-state.json")
	if err := SaveRunState(state, &RunState{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: "22:00-06:00", Host: HostState{Pseudonym: "pc"}}); err != nil {
		t.Fatal(err)
	}
	err := r.driver(t).RunSet(t.Context(), SetOptions{Set: SetPCBugcheck, Mode: ModeAutomated, Kind: "bugcheck", Window: windowAroundNow(t), Host: host})
	if err == nil || !strings.Contains(err.Error(), "proba pc-kill9 nu s-a încheiat") || asked != 0 {
		t.Errorf("another set in progress: RunSet = %v, host read %d times; want the refusal and no host read", err, asked)
	}

	if err := os.Remove(state); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := cancelAt(r.f, "run_start pc-kill9-01-1", cancel)
	if err := r.driver(t).RunSet(ctx, SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: windowAroundNow(t), Host: host}); !errors.Is(err, context.Canceled) {
		t.Fatalf("a new set: RunSet = %v, want the test's cancel at its first run", err)
	}
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	st, err := LoadRunState(state)
	if err != nil || st == nil || asked != 1 || st.Host.Cameras != 2 {
		t.Errorf("a new set: host read %d times, run state %+v, %v; want the host read once and kept", asked, st, err)
	}
	if s := r.f.runStarts(); len(s) != 1 || len(s[0].Streams) != 2 || s[0].OS != "windows" {
		t.Errorf("run starts = %+v, want the read host's two cameras", s)
	}
}

// TestPush_WithoutRootAsksForSudo (G15): on the Pi, MI1 runs as root, so
// root owns its results; `recspike push` started without sudo refuses
// before it connects, in Romanian, with the command to run, instead of
// failing on the first file with the system's English error. As root, or
// with results of its own, it goes on.
func TestPush_WithoutRootAsksForSudo(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "MI1", "pi-kill9"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MI1", "pi-kill9", "pi-kill9-01-1.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	realEuid, realOwner := geteuid, fileOwner
	t.Cleanup(func() { geteuid, fileOwner = realEuid, realOwner })
	ownedBy := func(uid int) func(fs.FileInfo) int { return func(fs.FileInfo) int { return uid } }

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	connected := make(chan struct{})
	go func() {
		if c, err := ln.Accept(); err == nil {
			close(connected)
			_ = c.Close()
		}
	}()
	geteuid, fileOwner = func() int { return 1000 }, ownedBy(0)
	err = Push(t.Context(), ln.Addr().String(), dir, "pi", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "sudo ./recspike push --controller") {
		t.Errorf("push without root of root's results = %v, want the Romanian hint to run it with sudo", err)
	}
	select {
	case <-connected:
		t.Error("push connected to the PC before it refused")
	case <-time.After(100 * time.Millisecond):
	}

	for _, tc := range []struct{ euid, owner int }{{0, 0}, {1000, 1000}, {-1, -1}} {
		geteuid, fileOwner = func() int { return tc.euid }, ownedBy(tc.owner)
		if root, err := needsRoot(dir); root || err != nil {
			t.Errorf("euid %d, results of uid %d: needsRoot = %v, %v; want false", tc.euid, tc.owner, root, err)
		}
	}
}

// TestResume_AfterAFailedOSStop_RunIsInvalidWithoutColdBoot: an OS stop
// that failed leaves its run in the phase `stopping` without a reboot.
// recspike started again finds that the host did not boot since (its uptime
// did not fall below the stop's), so it measures no ColdBootMs and sends no
// report: the run is invalid with its own reason, which the controller
// hears, and the set goes on with the run again.
func TestResume_AfterAFailedOSStop_RunIsInvalidWithoutColdBoot(t *testing.T) {
	r := newRig(t, "pi")
	d := r.driver(t)
	d.Uptime = func() (time.Duration, error) { return 3 * time.Hour, nil }
	d.OSStop = func(context.Context, controller.ShutdownKind) error {
		return errors.New("write /proc/sysrq-trigger: operation not permitted")
	}
	o := SetOptions{Set: SetPiSysrq, Mode: ModeAutomated, Kind: "sysrq", Window: windowAroundNow(t)}
	if err := d.RunSet(t.Context(), o); err == nil || !strings.Contains(err.Error(), "oprirea sysrq nu a pornit") {
		t.Fatalf("RunSet with a failing sysrq = %v, want the failed stop", err)
	}

	d = r.driver(t)
	d.Uptime = func() (time.Duration, error) { return 3*time.Hour + time.Minute, nil } // no reboot since
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	again := cancelAt(r.f, "run_start pi-sysrq-01-2", cancel)
	resumed := d.Resume(ctx)
	const reason = "gazda nu a repornit după oprirea sysrq"
	for _, rep := range r.f.runReports() {
		t.Errorf("report %s with ColdBootMs %v, want none: the uptime of a host that never rebooted is no cold boot", rep.RunID, ptrText(rep.ColdBootMs))
	}
	r.f.mu.Lock()
	refused := append([]link.Refused(nil), r.f.refused...)
	r.f.mu.Unlock()
	if len(refused) != 1 || refused[0].RunID != "pi-sysrq-01-1" || !strings.Contains(refused[0].Reason, reason) {
		t.Errorf("refusals = %+v, want pi-sysrq-01-1: %s", refused, reason)
	}
	st, err := LoadRunState(filepath.Join(r.results, "state", "run-state.json"))
	if err != nil || st == nil || len(st.Done) != 1 || Counts(st.Done[0].Run) || !strings.Contains(st.Done[0].Run.InvalidReason, reason) ||
		st.Done[0].Run.ColdBootMs != nil {
		t.Errorf("run state = %+v, %v; want run 1 done, invalid: %s, without ColdBootMs", st, err, reason)
	}
	if !errors.Is(resumed, context.Canceled) {
		t.Errorf("Resume = %v, want the test's cancel at the run's redo", resumed)
	}
	if err := <-again; err != nil {
		t.Error(err)
	}
}

// ptrText is *p, or "nil".
func ptrText(p *int64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprint(*p)
}

// TestBeacon_NoneUnderACutRunAfterItsStop (C16): recspike beacons a run
// only from its first ipcam-hub, until that one stops: the ipcam-hub that
// it restarts at once after a kill9, and the one that the resume starts
// after a reboot, never beacon under the run that was cut, so that no late
// beacon can move the run's stop at the controller.
func TestBeacon_NoneUnderACutRunAfterItsStop(t *testing.T) {
	t.Run("the restart after kill9", func(t *testing.T) {
		r := newRig(t, "pc")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		reported := cancelAt(r.f, "report pc-kill9-01-1", cancel)
		if err := r.driver(t).RunSet(ctx, SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: windowAroundNow(t)}); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunSet = %v, want the test's cancel after run 1's report", err)
		}
		if err := <-reported; err != nil {
			t.Fatal(err)
		}
		if got := logEntries(t, r.log); len(got) < 2 || !slices.Equal(got[:2], []string{"hub", "hub"}) {
			t.Fatalf("fake log = %v, want ipcam-hub killed and restarted", got)
		}
		seqs := r.f.beaconSeqs("pc-kill9-01-1")
		if len(seqs) == 0 {
			t.Fatal("no beacon of the run: the test proves nothing")
		}
		for i, seq := range seqs {
			if seq != uint64(i) { //nolint:gosec // a short test run
				t.Fatalf("beacons of the run = %v, want one sender counting from 0 once: a second sender started again", seqs)
			}
		}
	})

	t.Run("the resume after a reboot", func(t *testing.T) {
		r := newRig(t, "pi")
		r.cutPower(t, SetOptions{Set: SetPiSysrq, Mode: ModeAutomated, Kind: "sysrq", Window: windowAroundNow(t)},
			func(d *Driver, down func()) {
				d.OSStop = func(context.Context, controller.ShutdownKind) error {
					down()
					return nil
				}
			})
		// The power cut ended the first ipcam-hub: its beacons stop.
		atCut := r.f.beaconSeqs("pi-sysrq-01-1")
		for {
			time.Sleep(10 * controller.BeaconEvery)
			now := r.f.beaconSeqs("pi-sysrq-01-1")
			if len(now) == len(atCut) {
				break
			}
			atCut = now
		}
		if len(atCut) == 0 {
			t.Fatal("no beacon of the run before the cut: the test proves nothing")
		}
		d := r.driver(t)
		d.Uptime = func() (time.Duration, error) { return 41 * time.Second, nil }
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		next := cancelAt(r.f, "run_start pi-sysrq-02-1", cancel)
		if err := d.Resume(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Resume = %v, want the test's cancel at run 2", err)
		}
		if err := <-next; err != nil {
			t.Fatal(err)
		}
		if reps := r.f.runReports(); len(reps) != 1 || reps[0].RunID != "pi-sysrq-01-1" {
			t.Fatalf("reports = %+v, want run 1's, after the resume", reps)
		}
		if after := r.f.beaconSeqs("pi-sysrq-01-1"); len(after) != len(atCut) {
			t.Errorf("the run's beacons: %d at the cut, %d after the resume; want none after the cut", len(atCut), len(after))
		}
	})
}

// TestSession_EndsWithPullAndByeAfterAnError (G15): a set that stops on an
// error, with the link still up, still ends its session: the PC pulls the
// Pi's results and says bye.
func TestSession_EndsWithPullAndByeAfterAnError(t *testing.T) {
	r := newRig(t, "pc")
	r.f.verdict = func(start link.RunStart, m link.Report) results.Run {
		return results.Run{RunID: m.RunID, InvalidReason: "controlorul a trimis neuniform", Attempt: start.Attempt, ShutdownKind: start.Kind}
	}
	err := r.driver(t).RunSet(t.Context(), SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: windowAroundNow(t)})
	if err == nil || !strings.Contains(err.Error(), "nu au contat") {
		t.Fatalf("RunSet = %v, want the set stopped after 3 runs that did not count", err)
	}
	if err := r.f.waitEventWithin("pull_results", 5*time.Second); err != nil {
		t.Error(err)
	}
	if err := r.f.waitEventWithin("bye", 5*time.Second); err != nil {
		t.Error(err)
	}
}

// TestSession_ReconnectsAfterALinkDrop: in an unattended automated set, a
// link that drops while recspike waits for its cut, or for its verdict, is
// connected again (with bounded retries) instead of ending the set: the run
// repeats `ready` on the new link and gets its cut there, or sends its
// report again and gets its verdict, and the set goes on.
func TestSession_ReconnectsAfterALinkDrop(t *testing.T) {
	o := func(t *testing.T) SetOptions {
		return SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: windowAroundNow(t)}
	}
	t.Run("while it waits for the cut", func(t *testing.T) {
		r := newRig(t, "pc")
		var mu sync.Mutex
		readies := 0
		r.f.onReady = func(c *link.Conn, start link.RunStart) {
			mu.Lock()
			readies++
			first := readies == 1
			mu.Unlock()
			if first {
				_ = c.Close() // the link drops
				return
			}
			_ = c.Send(link.Cut{RunID: start.RunID, Kind: start.Kind, PromptAt: time.Now()})
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		reported := cancelAt(r.f, "report pc-kill9-01-1", cancel)
		if err := r.driver(t).RunSet(ctx, o(t)); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunSet = %v, want the set going on until the test's cancel", err)
		}
		if err := <-reported; err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if readies != 2 {
			t.Errorf("%d readies, want the run's ready repeated once on the new link", readies)
		}
		if got := logEntries(t, r.log); len(got) < 2 || !slices.Equal(got[:2], []string{"hub", "hub"}) {
			t.Errorf("fake log = %v, want the run's ipcam-hub kept through the drop, then killed and restarted at its cut", got)
		}
		if !strings.Contains(r.out.String(), "Legătura cu controlorul s-a refăcut") {
			t.Errorf("screen:\n%s\nwant the reconnection said", r.out.String())
		}
	})

	t.Run("while it waits for the verdict", func(t *testing.T) {
		r := newRig(t, "pc")
		r.f.dropReports = 1
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		next := cancelAt(r.f, "run_start pc-kill9-02-1", cancel)
		if err := r.driver(t).RunSet(ctx, o(t)); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunSet = %v, want the set going on until the test's cancel", err)
		}
		if err := <-next; err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, rep := range r.f.runReports() {
			ids = append(ids, rep.RunID)
		}
		if !slices.Equal(ids, []string{"pc-kill9-01-1", "pc-kill9-01-1"}) {
			t.Errorf("reports = %v, want run 1's report sent again on the new link", ids)
		}
	})
}
