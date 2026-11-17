package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// TestResume_FailedHubStartBacksOff (Self-healing autostart): a hub that
// fails its start, or a hub binary that is missing, makes resume wait
// before its next start, longer after each failure, and print the
// repeated error once, so a broken hub never pins a CPU or floods the
// autostart's log for the rest of the 7 days.
func TestResume_FailedHubStartBacksOff(t *testing.T) {
	t.Run("hub fails early", func(t *testing.T) {
		r := newRig(t)
		r.openMI6("android")
		res := r.start([]string{spiketest.FakeFailStartEnv + "=1"}, "resume")
		time.Sleep(4 * time.Second)
		starts := len(r.startsBy(res.cmd.Process.Pid))
		// The tests' backoff starts at 300 ms and doubles up to 2 s: about
		// five starts in 4 s, against hundreds without it.
		if starts < 2 || starts > 8 {
			t.Errorf("resume started a failing hub %d times in 4 s; want a few, each after a growing wait", starts)
		}
		if n := strings.Count(res.stdout.String(), msgError); n != 1 {
			t.Errorf("resume printed the error %d times; want once\n%s", n, res.stdout)
		}
	})
	t.Run("hub binary missing", func(t *testing.T) {
		r := newRig(t)
		r.openMI6("android")
		missing := filepath.Join(t.TempDir(), "no-such-hub")
		res := r.start([]string{envHubBin + "=" + missing}, "resume")
		time.Sleep(3 * time.Second)
		out := res.stdout.String()
		if n := strings.Count(out, msgError); n != 1 || !strings.Contains(out, "no-such-hub") {
			t.Errorf("resume printed the error %d times; want once, naming the binary\n%.2000s", n, out)
		}
	})
}

// TestResume_CrashRestartBacksOff (Self-healing autostart, P49): a hub
// that ends on its own under resume is restarted; while its restarts
// fail, resume says so once and waits longer each time, and once a
// restart succeeds the run state records the hub-crash gap and resume's
// new hub runs.
func TestResume_CrashRestartBacksOff(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	failFile := filepath.Join(t.TempDir(), "fail")
	res := r.start([]string{spiketest.FakeFailStartEnv + "=" + failFile}, "resume")
	until(t, time.Minute, "resume's hub", func() bool { return len(r.startsBy(res.cmd.Process.Pid)) == 1 })
	first := r.startsBy(res.cmd.Process.Pid)[0].PID
	if err := os.WriteFile(failFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if p, err := os.FindProcess(first); err != nil || p.Kill() != nil {
		t.Fatalf("killing resume's hub %d: %v", first, err)
	}
	until(t, time.Minute, "three failed restarts", func() bool { return len(r.startsBy(res.cmd.Process.Pid)) >= 4 })
	out := res.stdout.String()
	if n := strings.Count(out, msgHubCrashed); n != 1 {
		t.Errorf("resume said the hub crashed %d times; want once\n%s", n, out)
	}
	if n := strings.Count(out, msgError); n != 1 {
		t.Errorf("resume printed the restart's error %d times; want once\n%s", n, out)
	}
	if err := os.Remove(failFile); err != nil {
		t.Fatal(err)
	}
	until(t, time.Minute, "the hub-crash gap", func() bool {
		gaps := r.state().Gaps
		return len(gaps) == 1 && gaps[0].Reason == results.GapHubCrash
	})
	if live := r.liveHubs(); len(live) != 1 || live[0] == first {
		t.Errorf("live hubs %v after the restart; want resume's new hub alone", live)
	}
}
