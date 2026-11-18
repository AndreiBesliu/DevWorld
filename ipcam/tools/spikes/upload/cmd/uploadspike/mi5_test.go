package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hostinfo"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi5"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// openDeadMI5 records an open MI5 run whose mi5 is dead: this test's own
// PID with another start time, a PID that now names another process.
func (r *rig) openDeadMI5(boot string) {
	r.t.Helper()
	start, err := runstate.ProcStartMs(os.Getpid())
	if err != nil {
		r.t.Fatal(err)
	}
	o := mi5Open{mi5Holder: mi5Holder{PID: os.Getpid(), ProcStartMs: start + 1}, RunID: "mi5-cut", StartMs: time.Now().Add(-time.Hour).UnixMilli(),
		BootID: boot, Drive: mi5.DriveTest, Dests: mi5.Destinations(mi5.Mi5Config{Cameras: 4, Drive: mi5.DriveTest})}
	raw, err := json.Marshal(o)
	if err != nil {
		r.t.Fatal(err)
	}
	r.update(func(s *runstate.State) { s.Open["MI5"] = raw })
}

// firewallLog is the rig's firewall seam's file and its variable.
func (r *rig) firewallLog() (string, string) {
	log := filepath.Join(r.base, "firewall.log")
	return log, envFirewallLog + "=" + log
}

// cutEnvelope reads the cut run's envelope.
func (r *rig) cutEnvelope() results.Envelope {
	r.t.Helper()
	files, _ := filepath.Glob(filepath.Join(r.base, "results", "MI5", "*", "mi5-cut.json"))
	if len(files) != 1 {
		r.t.Fatalf("the cut run's envelopes: %v", files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		r.t.Fatal(err)
	}
	var e results.Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		r.t.Fatal(err)
	}
	return e
}

// removedBlock reports whether the firewall log shows the block's and its
// cleanup's removal.
func removedBlock(log string) bool {
	b, _ := os.ReadFile(log) //nolint:gosec // the test's own log
	if runtime.GOOS == "windows" {
		return strings.Contains(string(b), "Remove-NetFirewallRule -Group 'ipcam-spike-wanblock'")
	}
	return strings.Contains(string(b), "nft list tables") && strings.Contains(string(b), "systemctl stop ipcam-spike-unblock.timer")
}

// TestResume_OpenMI5AfterRebootIsInvalid (Holding the hub): resume after
// a reboot, or after an mi5 that ended before its close, removes the
// stale rules and cleanup, marks the open MI5 run invalid («repornire în
// timpul probei MI5», or «proba MI5 s-a oprit înainte de final») and
// restarts the hub with MI6's destinations only.
func TestResume_OpenMI5AfterRebootIsInvalid(t *testing.T) {
	boot, err := runstate.BootID()
	if err != nil {
		t.Skipf("no boot ID here: %v", err)
	}
	for _, tc := range []struct {
		boot, reason string
	}{{"another-boot", invalidReboot}, {boot, invalidStopped}} {
		r := newRig(t)
		r.openMI6("android")
		r.openDeadMI5(tc.boot)
		log, seam := r.firewallLog()
		signalFile := filepath.Join(t.TempDir(), "signal")
		res := r.start([]string{seam, envSignalFile + "=" + signalFile}, "resume")
		until(t, time.Minute, "resume's hub", func() bool { return len(r.startsBy(res.cmd.Process.Pid)) == 1 })
		if s := r.startsBy(res.cmd.Process.Pid)[0]; !slices.Equal(s.Destinations, []string{"g-android"}) {
			t.Errorf("the hub's destinations %v; want MI6's only", s.Destinations)
		}
		if _, open := r.state().Open["MI5"]; open {
			t.Error("the MI5 run is still open")
		}
		if !removedBlock(log) {
			b, _ := os.ReadFile(log) //nolint:gosec // the test's own log
			t.Errorf("the block's removal is not in the log:\n%s", b)
		}
		if e := r.cutEnvelope(); len(e.Runs) != 1 || e.Runs[0].Valid || e.Runs[0].InvalidReason != tc.reason || e.MI5 != nil {
			t.Errorf("the cut run's envelope %+v; want invalid with %q", e.Runs, tc.reason)
		}
		if !strings.Contains(res.stdout.String(), tc.reason) {
			t.Errorf("resume did not say %q:\n%s", tc.reason, res.stdout)
		}
		stopHolder(t, res, signalFile)
	}
}

// TestMI5_DeadMI5WithoutResume_NextCommandEndsTheRun (Holding the hub):
// with no resume, an open MI5 run whose mi5 died (its PID now another
// process's) is ended by the next command that takes the free hub.lock:
// mi6 --b1 android removes the rule, marks the run invalid and runs its
// login.
func TestMI5_DeadMI5WithoutResume_NextCommandEndsTheRun(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	r.editMI6(func(run *mi6.Run) {
		run.Completed("g-android", mi6.PlanA, "android", time.Now().Add(-49*time.Hour).UnixMilli())
		run.Refreshed("g-android", 1, 96, "failed", time.Now().Add(-time.Hour).UnixMilli())
	})
	r.openDeadMI5("")
	log, seam := r.firewallLog()
	signalFile := filepath.Join(t.TempDir(), "signal")
	m := r.start([]string{seam, envSignalFile + "=" + signalFile}, "mi6", "--b1", "android")
	waiting := strings.SplitN(promptWaiting, "%", 2)[0]
	until(t, time.Minute, "the B1 login's wait", func() bool { return strings.Contains(m.stdout.String(), waiting) })
	if _, open := r.state().Open["MI5"]; open || !removedBlock(log) {
		t.Errorf("open %v, block removed %v; want the run ended and the block removed", open, removedBlock(log))
	}
	if e := r.cutEnvelope(); e.Runs[0].Valid || e.Runs[0].InvalidReason != invalidStopped {
		t.Errorf("the cut run: %+v", e.Runs)
	}
	stopHolder(t, m, signalFile)
}

// TestMI5_PiCameraCountRefusal (P17): on a Pi whose model stays unknown,
// mi5 refuses with its line and starts no hub; its first line asks to
// keep the window open.
func TestMI5_PiCameraCountRefusal(t *testing.T) {
	t.Setenv("UPLOADSPIKE_FAKE_PIMODEL", `{"raw":"Raspberry Pi ?","tier":"unknown","known":false,"recommendedMax":0}`)
	r := newRig(t)
	p := r.run("", nil, "mi5")
	if p.code != 1 || !strings.Contains(p.stderr.String(), mi5.MsgPiUnknown) {
		t.Errorf("mi5 on an unknown Pi: exit %d, stderr %q", p.code, p.stderr)
	}
	if first := strings.SplitN(p.stdout.String(), "\n", 2)[0]; first != msgMI5KeepOpen {
		t.Errorf("the first line %q; want %q", first, msgMI5KeepOpen)
	}
	if starts := r.records("start"); len(starts) != 0 {
		t.Errorf("hub starts %v; want none", starts)
	}
}

// TestMI5_UsesStoredSdConfirmation (P02): an SD device without the run
// state's confirmation refuses with Task 9's text and starts no hub; the
// envelope carries the stored confirmation.
func TestMI5_UsesStoredSdConfirmation(t *testing.T) {
	t.Setenv("UPLOADSPIKE_FAKE_STORAGE", "sd")
	r := newRig(t)
	p := r.run("", nil, "mi5")
	if p.code != 1 || !strings.Contains(p.stderr.String(), "cardul SD nu este confirmat") || len(r.records("start")) != 0 {
		t.Errorf("mi5 on an unconfirmed SD card: exit %d, stderr %q, starts %d", p.code, p.stderr, len(r.records("start")))
	}
	e, _ := unitEnv(t)
	conf := results.SdConfirmation{Text: "DA", AtMs: 1}
	if err := runstate.Update(e.statePath(), func(s *runstate.State) error { s.Sd = &conf; return nil }); err != nil {
		t.Fatal(err)
	}
	o := mi5Open{RunID: "mi5-sd", StartMs: 1, Drive: mi5.DriveTest, Dests: mi5.Destinations(mi5.Mi5Config{Cameras: 4, Drive: mi5.DriveTest})}
	info := &hostinfo.Host{Storage: "sd", PiModel: &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B", Known: true, Supported: true, RecommendedMax: 4}}
	if err := writeMI5(e, o, "pi", info, &results.Mi5Data{}, true, ""); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(e.resultsDir(), "MI5", "pi", "mi5-sd.json")) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	var env results.Envelope
	if err := json.Unmarshal(b, &env); err != nil || env.SdConfirmation == nil || env.SdConfirmation.Text != "DA" || env.Storage != "sd" ||
		env.Cameras != 4 || env.Host != "pi" {
		t.Errorf("the envelope %+v (%v)", env, err)
	}
}
