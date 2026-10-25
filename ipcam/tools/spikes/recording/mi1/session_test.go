package mi1

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	go func() {
		r.f.waitEvent(t, "run_start pi-sysrq-02-1") // the set goes on with run 2
		cancel()
	}()
	if err := d.Resume(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resume = %v, want the test's cancel during run 2", err)
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
	go func() {
		r.f.waitEvent(t, "cut ")
		waitFor(t, "the prompt on recspike's screen", func() bool { return strings.Contains(r.out.String(), "Scoateți acum alimentarea") })
		cancel()
	}()
	if err := d.RunSet(ctx, SetOptions{Set: set, Mode: ModeReal, Kind: "power"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunSet until the power cut = %v, want context.Canceled", err)
	}
	killFakeHubs(t, r.log)
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
	go func() {
		r.f.waitEvent(t, "run_start real-pi-02-2")
		cancel()
	}()
	if err := d.Resume(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resume = %v, want the test's cancel at the next cut", err)
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
	r.f.waitEvent(t, "report real-pc-01-1")
	r.f.waitEvent(t, "run_start real-pc-02-2")
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
	r.f.waitEvent(t, "refused pc-kill9-01-1")
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
		r.f.waitEvent(t, "pull_results")
		r.f.waitEvent(t, "bye")
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
		r.f.waitEvent(t, "bye")
		if _, err := os.Stat(filepath.Join(r.f.pulled, "MI2", "pi", "mi2-1.json")); err != nil {
			t.Errorf("the PC's controller did not get the Pi's results: %v", err)
		}
		if _, err := os.Stat(filepath.Join(r.results, "pi")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the Pi pulled a results/pi folder of its own (%v)", err)
		}
	})
}
