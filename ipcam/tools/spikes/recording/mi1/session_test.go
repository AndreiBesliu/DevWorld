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
	"sync/atomic"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/lastindex"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
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

// TestBegin_LabShardsAndSmokeRunsKeepTheirOwnState (R03, item 2): a lab
// set's state is its results set's: its two shards share it, and a smoke
// run's is its own. In the same results folder, a full shard after a smoke
// run of it, a smoke run after a full shard, or shard 2 after shard 1,
// starts at that shard's first run instead of reporting complete from the
// state before; shard 2 keeps shard 1's runs in their set's state. A shard
// still in progress refuses another in Romanian; the same shard continues
// its own state. (A smoke run's envelopes never meet the full shard's:
// TestLab_SmokeAndFullRunsNeverShareAnEnvelope.)
func TestBegin_LabShardsAndSmokeRunsKeepTheirOwnState(t *testing.T) {
	template := &Driver{Host: LabHost("linux")}
	state := func(shard int, smoke bool, counted int) *RunState {
		st := &RunState{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: shard, Smoke: smoke, Host: template.Host}
		plan, err := template.plan(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range plan[:min(counted, len(plan))] {
			st.Done = append(st.Done, DoneRun{Index: p.Index, Run: results.Run{RunID: fmt.Sprintf("%s-%02d-1", st.key(), p.Index), Valid: true}})
		}
		return st
	}
	lab := func(shard int, smoke bool) SetOptions {
		return SetOptions{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: shard, Smoke: smoke}
	}
	// active saves st as the active set's state of a new results folder,
	// and returns its driver and the state that it reads back.
	active := func(st *RunState) (*Driver, *RunState) {
		d := &Driver{Results: t.TempDir(), Host: LabHost("linux")}
		if err := SaveRunState(d.statePath(), st); err != nil {
			t.Fatal(err)
		}
		st, err := d.State()
		if err != nil || st == nil {
			t.Fatalf("the active state: %+v, %v", st, err)
		}
		return d, st
	}
	for _, tc := range []struct {
		name string
		old  *RunState
		o    SetOptions
		kept int // runs of the state before that the new one holds
	}{
		{"a full shard 1 after its smoke run", state(1, true, 2), lab(1, false), 0},
		{"shard 2 after shard 1", state(1, false, 10), lab(2, false), 10},
		{"a smoke run after a full shard 1", state(1, false, 10), lab(1, true), 0},
	} {
		d, old := active(tc.old)
		if !d.complete(old) {
			t.Fatalf("%s: the old state is not complete", tc.name)
		}
		st, err := d.begin(t.Context(), old, tc.o)
		if err != nil {
			t.Errorf("%s: begin = %v", tc.name, err)
			continue
		}
		plan, err := d.plan(st)
		if err != nil {
			t.Fatal(err)
		}
		next, attempt, ok := st.nextAutomated(plan)
		if st.Shard != tc.o.Shard || st.Smoke != tc.o.Smoke || !ok || next.Index != plan[0].Index || attempt != 1 || len(st.Done) != tc.kept {
			t.Errorf("%s: begin gave shard %d, smoke %v, next run %d attempt %d (%v), %d runs done; want the shard's first run %d, attempt 1, and %d runs kept",
				tc.name, st.Shard, st.Smoke, next.Index, attempt, ok, len(st.Done), plan[0].Index, tc.kept)
		}
	}

	d, half := active(state(1, false, 3))
	if _, err := d.begin(t.Context(), half, lab(2, false)); err == nil || !strings.Contains(err.Error(), "nu s-a încheiat") || !strings.Contains(err.Error(), "shard 1") {
		t.Errorf("shard 2 while shard 1 is in progress: begin = %v, want a Romanian refusal that names shard 1", err)
	}
	if st, err := d.begin(t.Context(), half, lab(1, false)); err != nil || st != half {
		t.Errorf("shard 1 again: begin = %+v, %v; want its own state continued", st, err)
	}
}

// TestLab_SmokeAndFullRunsNeverShareAnEnvelope (item 3, R03): a lab
// shard's smoke run and the full shard, in either order in the same results
// folder, never name the same envelope. reccontroller writes a run's
// envelope at MI1/<results set>/<runId>.json: a smoke run's results set is
// <set>-smoke and its run ids start with it, so a smoke window's result
// never overwrites a counted one, never lands beside the set's envelopes,
// and never shares a run id that Judge would merge. A smoke run that is
// complete stays so: started again, it records nothing (item 2).
func TestLab_SmokeAndFullRunsNeverShareAnEnvelope(t *testing.T) {
	lab := func(smoke bool) SetOptions {
		return SetOptions{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: 1, Smoke: smoke}
	}
	// smokeRun is a smoke run of shard 1, to its end.
	smokeRun := func(t *testing.T, r *rig) {
		t.Helper()
		if err := r.driver(t).RunSet(t.Context(), lab(true)); err != nil {
			t.Fatalf("the smoke run: RunSet = %v\nscreen:\n%s", err, r.out.String())
		}
	}
	// fullShard is the full shard 1: its first two runs are the ones whose
	// indexes the smoke run shares; the test ends it as its third starts.
	fullShard := func(t *testing.T, r *rig) {
		t.Helper()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		third := cancelAt(r.f, "run_start "+SetLabLinuxKill9+"-03-1", cancel)
		if err := r.driver(t).RunSet(ctx, lab(false)); !errors.Is(err, context.Canceled) {
			t.Fatalf("the full shard: RunSet = %v, want the test's cancel at its third run\nscreen:\n%s", err, r.out.String())
		}
		if err := <-third; err != nil {
			t.Fatal(err)
		}
		killFakeHubs(t, r.log)
	}
	// shardComplete saves the full shard 1 complete in its place.
	shardComplete := func(t *testing.T, r *rig) {
		t.Helper()
		d := r.driver(t)
		if err := SaveRunState(d.statePath(), completeState(t, d, &RunState{Set: SetLabLinuxKill9, Mode: ModeLab, Kind: "kill9", Shard: 1, Host: d.Host})); err != nil {
			t.Fatal(err)
		}
	}
	envelope := func(s link.RunStart) string {
		return filepath.Join("MI1", ResultsSet(s.Set, s.Smoke), s.RunID+".json")
	}
	for _, order := range []struct {
		name  string
		steps []func(*testing.T, *rig)
	}{
		{"the smoke run first, and again once the shard is complete", []func(*testing.T, *rig){smokeRun, fullShard, shardComplete, smokeRun}},
		{"the full shard first", []func(*testing.T, *rig){fullShard, shardComplete, smokeRun}},
	} {
		t.Run(order.name, func(t *testing.T) {
			r := newRig(t, "lab-linux")
			for _, step := range order.steps {
				step(t, r)
			}
			var smoke, full []link.RunStart
			for _, s := range r.f.runStarts() {
				if s.Smoke {
					smoke = append(smoke, s)
				} else {
					full = append(full, s)
				}
			}
			if len(smoke) != smokeRuns || len(full) != 3 {
				t.Fatalf("%d smoke runs and %d full runs, want the smoke run's %d, once, and the full shard's first 3", len(smoke), len(full), smokeRuns)
			}
			for _, s := range smoke {
				if dir, want := filepath.Dir(envelope(s)), filepath.Join("MI1", SetLabLinuxKill9+"-smoke"); dir != want {
					t.Errorf("smoke run %s: envelope in %s, want %s", s.RunID, dir, want)
				}
				for _, f := range full {
					if s.RunID == f.RunID || envelope(s) == envelope(f) {
						t.Errorf("smoke run %s and full run %s share the run id or the envelope %s", s.RunID, f.RunID, envelope(f))
					}
				}
			}
			for _, f := range full {
				if dir, want := filepath.Dir(envelope(f)), filepath.Join("MI1", SetLabLinuxKill9); dir != want {
					t.Errorf("full run %s: envelope in %s, want %s", f.RunID, dir, want)
				}
			}
		})
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
// recspike started again finds that the host did not boot since (its boot
// is the stop's), so it measures no ColdBootMs and sends no report: the run
// is invalid with its own reason, which the controller hears, and the set
// goes on with the run again.
func TestResume_AfterAFailedOSStop_RunIsInvalidWithoutColdBoot(t *testing.T) {
	r := newRig(t, "pi")
	d := r.driver(t)
	boot := d.ThisBoot
	d.Uptime = func() (time.Duration, error) { return 3 * time.Hour, nil }
	d.OSStop = func(context.Context, controller.ShutdownKind) error {
		return errors.New("write /proc/sysrq-trigger: operation not permitted")
	}
	o := SetOptions{Set: SetPiSysrq, Mode: ModeAutomated, Kind: "sysrq", Window: windowAroundNow(t)}
	if err := d.RunSet(t.Context(), o); err == nil || !strings.Contains(err.Error(), "oprirea sysrq nu a pornit") {
		t.Fatalf("RunSet with a failing sysrq = %v, want the failed stop", err)
	}

	d = r.driver(t)
	d.ThisBoot = boot // no reboot since
	d.Uptime = func() (time.Duration, error) { return 3*time.Hour + time.Minute, nil }
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

// TestResume_StopSaveLost_ColdBootStillMeasured (mi1-3): a bugcheck or
// sysrq can come before the save of the phase `stopping` is on disk, so
// that the reboot finds the run still `recording`, without the stop's
// boot. The run's own boot, saved when it began, shows the reboot: the
// resume measures ColdBootMs all the same, so G12's cold-boot row does not
// lose a counted run.
func TestResume_StopSaveLost_ColdBootStillMeasured(t *testing.T) {
	r := newRig(t, "pi")
	statePath := filepath.Join(r.results, "state", "run-state.json")
	r.cutPower(t, SetOptions{Set: SetPiSysrq, Mode: ModeAutomated, Kind: "sysrq", Window: windowAroundNow(t)},
		func(d *Driver, down func()) {
			d.OSStop = func(context.Context, controller.ShutdownKind) error {
				// The stop's save never reached the disk: the file is the
				// one that the run's start saved.
				st, err := LoadRunState(statePath)
				if err != nil || st == nil || st.Current == nil || st.Current.Phase != PhaseStopping {
					t.Errorf("the state at the stop = %+v, %v; want the run stopping", st, err)
				} else {
					st.Current.Phase, st.Current.StopBoot = PhaseRecording, nil
					if err := SaveRunState(statePath, st); err != nil {
						t.Error(err)
					}
				}
				down()
				return nil
			}
		})

	d := r.driver(t) // another boot
	d.Uptime = func() (time.Duration, error) { return 41 * time.Second, nil }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	next := cancelAt(r.f, "run_start pi-sysrq-02-1", cancel)
	if err := d.Resume(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resume = %v, want the test's cancel during run 2", err)
	}
	if err := <-next; err != nil {
		t.Fatal(err)
	}
	reports := r.f.runReports()
	if len(reports) != 1 || reports[0].RunID != "pi-sysrq-01-1" {
		t.Fatalf("reports %+v, want run 1's", reports)
	}
	if ms := reports[0].ColdBootMs; ms == nil || *ms > 41_000 || *ms < 40_000 {
		t.Errorf("ColdBootMs = %s, want the 41 s of uptime at the recording event although the stop's save was lost", ptrText(ms))
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
		// The cut comes after a few beacons, so that a second sender, which
		// would count from 0 again, would follow them.
		r.f.onReady = func(c *link.Conn, start link.RunStart) {
			time.Sleep(6 * controller.BeaconEvery)
			_ = c.Send(link.Cut{RunID: start.RunID, Kind: start.Kind, PromptAt: time.Now()})
		}
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
		if len(seqs) < 3 {
			t.Fatalf("beacons of the run = %v, want a few before the cut: the test proves nothing", seqs)
		}
		// One sender's numbers only grow, even when the network loses one;
		// a second sender would start again from 0.
		for i := 1; i < len(seqs); i++ {
			if seqs[i] <= seqs[i-1] {
				t.Fatalf("beacons of the run = %v, want one sender's numbers, strictly increasing: a second sender started again from 0", seqs)
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

// gateClock is a stepClock whose first tick waits for the test: the
// driver's wait for its window stops there (waiting is closed) until the
// test sends the time to open on open.
type gateClock struct {
	*stepClock
	waiting chan struct{}
	open    chan time.Time
	once    sync.Once
}

func newGateClock(now time.Time) *gateClock {
	c := &gateClock{stepClock: newStepClock(), waiting: make(chan struct{}), open: make(chan time.Time, 1)}
	c.set(now)
	return c
}

func (c *gateClock) NewTicker(d time.Duration) source.Ticker { return &gateTicker{c: c, d: d} }

type gateTicker struct {
	c *gateClock
	d time.Duration
}

func (t *gateTicker) C() <-chan time.Time {
	first := false
	t.c.once.Do(func() { first = true })
	if !first {
		return (&stepTicker{clock: t.c.stepClock, d: t.d}).C()
	}
	close(t.c.waiting)
	ch := make(chan time.Time, 1)
	go func() {
		now := <-t.c.open
		t.c.set(now)
		ch <- now
	}()
	return ch
}

func (t *gateTicker) Stop() {}

// TestSession_ReconnectsAfterALinkDrop (item 11): in an unattended
// automated set, a link that drops is connected again instead of ending the
// set, in each of its subtests: while recspike waits for the cut; while the
// set waits for its window, hours before its run; when the run_start was
// lost with a link that dies at the run's ready; when the link dies with
// the run_start, while ipcam-hub starts, so that recspike sends its ready
// on no link; as recspike tells the controller why a run was not stopped;
// and while it waits for the verdict. A new link hears the whole
// announcement of the run in progress, run_start then ready, as the old one
// may have lost both: the run then gets its cut there; a report is sent
// again and gets its verdict; and the set goes on. The bounds of the
// reconnection are TestSession_ReconnectsWithinItsBounds'.
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
			first := false
			if start.RunID == "pc-kill9-01-1" { // run 2's, if the set gets there before the cancel, is not counted
				readies++
				first = readies == 1
			}
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
			t.Errorf("%d readies of run 1, want its ready repeated once on the new link", readies)
		}
		if got := logEntries(t, r.log); len(got) < 2 || !slices.Equal(got[:2], []string{"hub", "hub"}) {
			t.Errorf("fake log = %v, want the run's ipcam-hub kept through the drop, then killed and restarted at its cut", got)
		}
		if !strings.Contains(r.out.String(), "Legătura cu controlorul s-a refăcut") {
			t.Errorf("screen:\n%s\nwant the reconnection said", r.out.String())
		}
	})

	t.Run("while it waits for its window", func(t *testing.T) {
		r := newRig(t, "pc")
		clock := newGateClock(at("21:00"))
		d := r.driver(t)
		d.Clock = clock
		seen := make(chan struct{})
		var seenOnce sync.Once
		d.linkDown = func() { seenOnce.Do(func() { close(seen) }) }
		w, err := ParseWindow("22:00-23:00")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- d.RunSet(ctx, SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: &w, WaitForWindow: true})
		}()
		select {
		case <-clock.waiting:
		case err := <-done:
			t.Fatalf("RunSet = %v before it waited for its window", err)
		case <-time.After(30 * time.Second):
			t.Fatal("the set never waited for its window")
		}
		r.f.dropLinks() // the link drops during the wait, hours before the run
		select {
		case <-seen: // recspike's session reports it down
		case <-time.After(30 * time.Second):
			t.Fatal("recspike never saw the link drop")
		}
		reported := cancelAt(r.f, "report pc-kill9-01-1", cancel)
		clock.open <- at("22:00")
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("RunSet = %v, want the link connected again before the run and the set going on until the test's cancel\nscreen:\n%s", err, r.out.String())
		}
		if err := <-reported; err != nil {
			t.Fatal(err)
		}
		if n := r.f.helloCount(); n != 2 {
			t.Errorf("the host said hello %d times, want twice: at the start and once after the drop", n)
		}
		if s := r.f.runStartsOf("pc-kill9-01-1"); len(s) != 1 {
			t.Errorf("run 1's starts = %+v, want it announced once, on the new link", s)
		}
		if !strings.Contains(r.out.String(), "Legătura cu controlorul s-a refăcut.\nRularea pc-kill9-01-1") {
			t.Errorf("screen:\n%s\nwant the link connected again before run 1 begins", r.out.String())
		}
	})

	t.Run("when its run_start is lost with the link", func(t *testing.T) {
		r := newRig(t, "pc")
		r.f.mu.Lock()
		r.f.loseStarts = 1
		r.f.mu.Unlock()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		reported := cancelAt(r.f, "report pc-kill9-01-1", cancel)
		if err := r.driver(t).RunSet(ctx, o(t)); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunSet = %v, want the run announced again on the new link and the set going on\nscreen:\n%s", err, r.out.String())
		}
		if err := <-reported; err != nil {
			t.Fatal(err)
		}
		if s := r.f.runStartsOf("pc-kill9-01-1"); len(s) != 1 || s[0].Kind != "kill9" || len(s[0].Streams) != 1 {
			t.Errorf("run 1's starts kept = %+v, want its announcement, made again in full on the new link", s)
		}
		if n := r.f.helloCount(); n != 2 {
			t.Errorf("the host said hello %d times, want twice", n)
		}
	})

	t.Run("while ipcam-hub starts, the link dead with the run_start", func(t *testing.T) {
		r := newRig(t, "pc")
		// ipcam-hub is ready only once the controller has closed the link,
		// so recspike sees the drop before it sends the ready.
		dropped := filepath.Join(t.TempDir(), "dropped")
		t.Setenv(fakeReadyAfterEnv, dropped)
		r.f.mu.Lock()
		r.f.dropAtStarts, r.f.droppedSignal = 1, dropped
		r.f.mu.Unlock()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		reported := cancelAt(r.f, "report pc-kill9-01-1", cancel)
		if err := r.driver(t).RunSet(ctx, o(t)); !errors.Is(err, context.Canceled) {
			t.Fatalf("RunSet = %v, want the ready delivered on a new link, after the run's run_start, and the set going on\nscreen:\n%s", err, r.out.String())
		}
		if err := <-reported; err != nil {
			t.Fatal(err)
		}
		if s := r.f.runStartsOf("pc-kill9-01-1"); len(s) != 1 {
			t.Errorf("run 1's starts kept = %+v, want it made again on the new link before its ready", s)
		}
		if n := r.f.helloCount(); n != 2 {
			t.Errorf("the host said hello %d times, want twice", n)
		}
		// The ready found the link down and reconnected first; it was not
		// lost on the dead link, then made again while waiting for the cut.
		if !strings.Contains(r.out.String(), errLinkDown.Error()+"; mă reconectez") {
			t.Errorf("screen:\n%s\nwant the ready's reconnection, before the wait for the cut", r.out.String())
		}
	})

	t.Run("when it refuses a stop after its window", func(t *testing.T) {
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
			_ = c.Close() // the link drops after the cut
		}
		d := r.driver(t)
		d.Clock = clock
		if err := d.RunSet(t.Context(), SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: &w}); err != nil {
			t.Fatalf("RunSet = %v, want nil: the set waits for the next window", err)
		}
		if err := r.f.waitEventWithin("refused pc-kill9-01-1", 5*time.Second); err != nil {
			t.Errorf("%v: the controller never heard why the run was not stopped", err)
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

// completeState is st with a counted run done at every position of its
// plan, attempt 1: a set that is complete.
func completeState(t *testing.T, d *Driver, st *RunState) *RunState {
	t.Helper()
	plan, err := d.plan(st)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan {
		id := fmt.Sprintf("%s-%02d-1", ResultsSet(st.Set, st.Smoke), p.Index)
		st.Done = append(st.Done, DoneRun{Index: p.Index, Run: results.Run{RunID: id, Valid: true, Attempt: 1}})
	}
	if !d.complete(st) {
		t.Fatalf("the state of %s is not complete", st.Set)
	}
	return st
}

// TestRunSet_ACompleteSetStaysCompleteAfterAnother (item 2): the run state
// keeps each set's progress, by results set. A set that is complete stays
// so after another set ran in the same results folder: set A complete, then
// set B, then set A again records nothing, so its run ids, whose envelopes
// count at the controller, are never used again; and A's progress is still
// in the run state.
func TestRunSet_ACompleteSetStaysCompleteAfterAnother(t *testing.T) {
	r := newRig(t, "pc")
	d := r.driver(t)
	a := completeState(t, d, &RunState{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: "22:00-06:00", Host: d.Host})
	if err := SaveRunState(d.statePath(), a); err != nil {
		t.Fatal(err)
	}
	// Set B starts, and the test ends it at its first run: then B is
	// complete too.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := cancelAt(r.f, "run_start pc-bugcheck-01-1", cancel)
	if err := r.driver(t).RunSet(ctx, SetOptions{Set: SetPCBugcheck, Mode: ModeAutomated, Kind: "bugcheck", Window: windowAroundNow(t)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("set B: RunSet = %v, want the test's cancel at its first run", err)
	}
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	killFakeHubs(t, r.log)
	b := completeState(t, d, &RunState{Set: SetPCBugcheck, Mode: ModeAutomated, Kind: "bugcheck", Window: "22:00-06:00", Host: d.Host})
	if err := SaveRunState(d.statePath(), b); err != nil {
		t.Fatal(err)
	}

	// Set A again: it is complete, and records nothing.
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	r.f.onReady = func(c *link.Conn, start link.RunStart) {
		if strings.HasPrefix(start.RunID, SetPCKill9+"-") {
			cancel() // a run of A again: its envelope would replace a counted one
		}
		_ = c.Send(link.Cut{RunID: start.RunID, Kind: start.Kind, PromptAt: time.Now()})
	}
	err := r.driver(t).RunSet(ctx, SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: windowAroundNow(t)})
	var again []string
	for _, s := range r.f.runStarts() {
		if strings.HasPrefix(s.RunID, SetPCKill9+"-") {
			again = append(again, s.RunID)
		}
	}
	if err != nil || len(again) != 0 {
		t.Errorf("set A again: RunSet = %v, runs %v; want nil and none: A is complete", err, again)
	}
	if !strings.Contains(r.out.String(), "Proba pc-kill9 are toate rulările.") {
		t.Errorf("screen:\n%s\nwant set A said complete", r.out.String())
	}
	st, err := LoadRunState(d.statePath())
	if err != nil || st == nil || st.Set != SetPCKill9 || len(st.Done) != len(a.Done) || !d.complete(st) {
		t.Errorf("run state after set A again = %+v, %v; want A's own, complete, with its %d runs", st, err, len(a.Done))
	}
}

// TestSession_ReconnectsWithinItsBounds (item 11's bounds): a link that
// keeps dropping is connected again at most maxReconnects times in one
// wait; then the unattended set ends, with the Romanian reason of the drop
// and ipcam-hub stopped. A real set's owner is at the host: its link is
// never connected again, and its first drop ends the set.
func TestSession_ReconnectsWithinItsBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		o      SetOptions
		hellos int
	}{
		{"an automated set", SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9", Window: windowAroundNow(t)}, 1 + maxReconnects},
		{"a real set", SetOptions{Set: SetRealPC, Mode: ModeReal, Kind: "power"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "pc")
			r.f.onReady = func(c *link.Conn, _ link.RunStart) { _ = c.Close() } // every link drops at the run's ready
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			err := r.driver(t).RunSet(ctx, tc.o)
			if !errors.Is(err, errLinkDown) || !strings.HasPrefix(err.Error(), "legătura cu controlorul s-a întrerupt") {
				t.Errorf("RunSet = %v, want the set ended on the drop, in Romanian", err)
			}
			if n := r.f.helloCount(); n != tc.hellos {
				t.Errorf("the host said hello %d times, want %d", n, tc.hellos)
			}
			if got := logEntries(t, r.log); !slices.Equal(got, []string{"hub", "stopped"}) {
				t.Errorf("fake log = %v, want the run's ipcam-hub stopped when the set ends", got)
			}
			if again := strings.Count(r.out.String(), "Legătura cu controlorul s-a refăcut."); again != tc.hellos-1 {
				t.Errorf("screen:\n%s\nwant %d reconnections", r.out.String(), tc.hellos-1)
			}
		})
	}
}

// mutedConn is a link whose writes fail once the controller's first bytes
// (the session) have been read: the host's hello goes out, and every
// message after it fails as recspike sends it.
type mutedConn struct {
	net.Conn
	read atomic.Bool
}

func (c *mutedConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.read.Store(true)
	}
	return n, err
}

func (c *mutedConn) Write(b []byte) (int, error) {
	if c.read.Load() {
		return 0, errors.New("muted link")
	}
	return c.Conn.Write(b)
}

// muteLinksFrom makes the links of the nth dial (1-based) and of every
// later one mutedConns.
func muteLinksFrom(t *testing.T, n int64) {
	t.Helper()
	var dials atomic.Int64
	system := dialLink
	dialLink = func(ctx context.Context, addr string) (*link.Conn, error) {
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp4", addr)
		if err != nil {
			return nil, err
		}
		if dials.Add(1) >= n {
			return link.NewConn(&mutedConn{Conn: c}), nil
		}
		return link.NewConn(c), nil
	}
	t.Cleanup(func() { dialLink = system })
}

// TestSession_ReconnectBudgets (Task 7.2c review, M4): in an unattended
// set, each wait connects the link again at most maxReconnects times, then
// the set ends on the drop, in Romanian: a delivery whose every send fails
// (the run_start), a report whose verdict never comes, and the wait for the
// cut, whose own announcements on each new link fail too: they share the
// wait's reconnections, so one wait never makes more than maxReconnects.
func TestSession_ReconnectBudgets(t *testing.T) {
	o := SetOptions{Set: SetPCKill9, Mode: ModeAutomated, Kind: "kill9"}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, r *rig)
		log   []string
	}{
		{"a delivery whose sends fail", func(t *testing.T, _ *rig) { muteLinksFrom(t, 1) }, nil},
		{"a report without its verdict", func(_ *testing.T, r *rig) { r.f.dropReports = 1000 }, []string{"hub", "hub", "stopped"}},
		{"the wait for the cut, its announcements failing", func(t *testing.T, r *rig) {
			r.f.onReady = func(c *link.Conn, _ link.RunStart) { _ = c.Close() } // the first link drops at the ready
			muteLinksFrom(t, 2)
		}, []string{"hub", "stopped"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "pc")
			tc.setup(t, r)
			opts := o
			opts.Window = windowAroundNow(t)
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			err := r.driver(t).RunSet(ctx, opts)
			if !errors.Is(err, errLinkDown) || !strings.HasPrefix(err.Error(), "legătura cu controlorul s-a întrerupt") {
				t.Errorf("RunSet = %v, want the set ended on the drop, in Romanian", err)
			}
			if n := r.f.helloCount(); n != 1+maxReconnects {
				t.Errorf("the host said hello %d times, want %d: the first link and %d reconnections", n, 1+maxReconnects, maxReconnects)
			}
			if got := logEntries(t, r.log); !slices.Equal(got, tc.log) {
				t.Errorf("fake log = %v, want %v", got, tc.log)
			}
		})
	}
}

// TestSession_CamerasAtTheControllersAddress (R03): ipcam-hub pulls the
// cameras at the address that recspike dialed for the controller, on the
// RTSP port that the controller names. A lab guest reaches the runner's
// reccontroller at 10.0.2.2 through QEMU's user network, while the
// controller sees the link arrive on its own loopback address and names
// that one.
func TestSession_CamerasAtTheControllersAddress(t *testing.T) {
	f := startFakeController(t) // it names rtsp://127.0.0.1:1
	_, port, err := net.SplitHostPort(f.addr())
	if err != nil {
		t.Fatal(err)
	}
	d := &Driver{Controller: net.JoinHostPort("localhost", port), Host: HostState{Pseudonym: "lab-qemu"}}
	s, err := d.connect(t.Context())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.close()
	if got, err := s.cameraURL(MainStream); err != nil || got != "rtsp://u:p@localhost:1/cam01_main" { //nolint:gosec // the fake controller's test credentials
		t.Errorf("cameraURL = %q, %v; want the camera at the dialed address", got, err)
	}
}
