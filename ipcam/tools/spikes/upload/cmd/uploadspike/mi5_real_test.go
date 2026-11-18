package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi5"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// mi5TestTiming is a short run's timing for the process tests: a 20-s
// encrypted pre-run (a read a second), a 5-s warm-up, checks every 20 s,
// a read every 3 s.
const mi5TestTiming = `{"preRun":20000,"preRunEvery":1000,"warmUp":5000,"checkEvery":20000,"readOpenEvery":3000,"floor":2000,"quiet":8000,"drainMax":300000}`

// TestMI5_LabRun_RealHub (P14, P16, g3-16): a short lab run end to end on
// the real ipcam-hub, the fork's MediaMTX, reccontroller --local's
// simulated cameras and fakeproviders: the lab's login rig, the quota
// probe, the encrypted pre-run's reads, the recording, a 75-s block
// (the tests' firewall seam records its commands and blocks nothing, so
// its checks pass by the seam and the run is never judged), the restart
// in the middle of a 512 KiB part, the drain, the final drain without
// recording and the audit. The envelope shows every segment verified,
// none missing or duplicated, the resumed offset at or above the last
// confirmed one, the torn segment uploaded and every read ok; the run
// leaves the run state and the block's rules are removed.
func TestMI5_LabRun_RealHub(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the lab's run is Linux's")
	}
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	r, f := realRig(t)
	recc := binary(t, "reccontroller", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cmd/reccontroller")
	log, seam := r.firewallLog()
	p := r.start([]string{seam, envReccontroller + "=" + recc, envMediaDir + "=" + media, envLabLAN + "=127.0.0.1", envMI5Timing + "=" + mi5TestTiming},
		"mi5", "--lab", "--block", "75s", "--segment-secs", "30", "--part-size", "512KiB", "--cameras", "4", "--fakes", f.base)
	if !p.wait(10 * time.Minute) {
		t.Fatalf("mi5 did not end\nstdout:\n%s\nstderr:\n%s", p.stdout, tail(p.stderr.String(), 40))
	}
	if p.code != 0 {
		t.Fatalf("mi5: exit %d\nstdout:\n%s\nstderr:\n%s", p.code, p.stdout, tail(p.stderr.String(), 40))
	}
	if first := strings.SplitN(p.stdout.String(), "\n", 2)[0]; first != msgMI5KeepOpen {
		t.Errorf("the first line %q; want %q", first, msgMI5KeepOpen)
	}
	files, _ := filepath.Glob(filepath.Join(r.base, "results", "MI5", "lab-linux", "*.json"))
	if len(files) != 1 {
		t.Fatalf("envelopes %v", files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var e results.Envelope
	if err := json.Unmarshal(b, &e); err != nil || e.MI5 == nil {
		t.Fatalf("the envelope: %v\n%s", err, b)
	}
	d := e.MI5
	if !e.TestSeams || !d.Lab || len(e.Runs) != 1 || !e.Runs[0].Valid || e.Cameras != 4 {
		t.Errorf("the envelope's frame: seams %v, lab %v, runs %+v, cameras %d", e.TestSeams, d.Lab, e.Runs, e.Cameras)
	}
	dst := d.Dests[0]
	if dst.Dest != "dst-1" || dst.Segments == 0 || dst.Verified != dst.Segments || dst.Missing != 0 || dst.Duplicates != 0 ||
		dst.HashMismatches != 0 || dst.AuditFrom != mi5.AuditListRemote || dst.SegmentsDuringBlock == 0 {
		t.Errorf("dst-1 %+v", dst)
	}
	if err := mi5.CheckResume(d.Restart); err != nil || !d.Restart.TornSegmentUploaded || d.Restart.LastConfirmedOffset <= 0 ||
		d.Restart.LastConfirmedOffset >= d.Restart.Size {
		t.Errorf("restart %+v: %v", d.Restart, err)
	}
	for _, ro := range d.ReadOpen {
		if ro.Calls < mi5.ReadOpenCalls || ro.OK != ro.Calls {
			t.Errorf("reads %+v", d.ReadOpen)
		}
	}
	if _, open := r.state().Open["MI5"]; open {
		t.Error("the MI5 run is still open")
	}
	fw, _ := os.ReadFile(log) //nolint:gosec // the test's own log
	if !strings.Contains(string(fw), "nft -f -") || strings.Count(string(fw), "nft delete table inet ipcam_spike") < 2 {
		t.Errorf("the block's commands:\n%s", fw)
	}
	for _, secret := range []string{"lab-desktop-secret-0001", "rtsp://"} {
		if strings.Contains(string(b), secret) || strings.Contains(p.stdout.String(), secret) {
			t.Errorf("%q in the envelope or the terminal", secret)
		}
	}
}

// tail is s's last n lines.
func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// liveExes are the PIDs of the processes that run the program at path
// (Linux's /proc).
func liveExes(path string) []int {
	var out []int
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe")); err == nil && exe == path {
			out = append(out, pid)
		}
	}
	return out
}

// TestMI5_HolderEndsAnyWay_OneHubUnderResume (Holding the hub, Linux): an
// mi5 that ends during its block by SIGHUP (its window closed), or by
// SIGKILL, leaves within a minute exactly one hub, resume's, with MI6's
// destinations only: no block rule left (the firewall seam's log shows
// the removal after the cut), no orphaned hub, and the MI5 run invalid
// («proba MI5 s-a oprit înainte de final»). mi5's first line asks to keep
// the window open.
func TestMI5_HolderEndsAnyWay_OneHubUnderResume(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("signals of Linux; the windows job's TerminateProcess variant is not written")
	}
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGKILL} {
		t.Run(sig.String(), func(t *testing.T) {
			r, f := realRig(t)
			r.openMI6("android")
			recc := binary(t, "reccontroller", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cmd/reccontroller")
			log, seam := r.firewallLog()
			timing := `{"preRun":5000,"preRunEvery":1000,"warmUp":2000,"checkEvery":20000,"readOpenEvery":3000,"floor":2000,"quiet":8000,"drainMax":300000}`
			extra := []string{seam, envReccontroller + "=" + recc, envMediaDir + "=" + media, envLabLAN + "=127.0.0.1", envMI5Timing + "=" + timing}
			m := r.start(extra, "mi5", "--lab", "--block", "10m", "--segment-secs", "30", "--part-size", "512KiB", "--cameras", "2", "--fakes", f.base)
			until(t, 3*time.Minute, "the block", func() bool {
				b, _ := os.ReadFile(log) //nolint:gosec // the test's own log
				return strings.Contains(string(b), "nft -f -")
			})
			if first := strings.SplitN(m.stdout.String(), "\n", 2)[0]; first != msgMI5KeepOpen {
				t.Errorf("mi5's first line %q", first)
			}
			cutAt := len(mustRead(t, log))
			if err := m.cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			if !m.wait(time.Minute) {
				t.Fatal("mi5 did not end")
			}
			if left := liveExes(recc); len(left) != 0 {
				t.Errorf("reccontroller outlived mi5: %v", left)
			}
			res := r.start(extra, "resume")
			until(t, 2*time.Minute, "resume's one hub, the run ended", func() bool {
				_, open := r.state().Open["MI5"]
				hubs := liveExes(r.hub)
				return !open && len(hubs) == 1 && r.hubPID() == hubs[0]
			})
			if fw := mustRead(t, log)[cutAt:]; !strings.Contains(fw, "nft delete table inet ipcam_spike") {
				t.Errorf("no removal of the block after the cut:\n%s", fw)
			}
			b, err := os.ReadFile(filepath.Join(r.base, "hub", "spike", "run.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), `"g-android"`) || strings.Contains(string(b), `"dst-1"`) || strings.Contains(string(b), "rtsp://") {
				t.Errorf("resume's run.json holds MI5's parts:\n%s", b)
			}
			files, _ := filepath.Glob(filepath.Join(r.base, "results", "MI5", "lab-linux", "*.json"))
			if len(files) != 1 || !strings.Contains(mustRead(t, files[0]), invalidStopped) {
				t.Errorf("the cut run's envelope %v", files)
			}
			_ = res.cmd.Process.Signal(syscall.SIGTERM)
			res.wait(time.Minute)
		})
	}
}

// mustRead is a file's text.
func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
