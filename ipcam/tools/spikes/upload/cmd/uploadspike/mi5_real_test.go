package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi5"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// mi5TestTiming is a short run's timing for the process tests: a 20-s
// encrypted pre-run (a read a second), a 5-s warm-up, checks every 20 s,
// a read every 3 s, and drainMax above the outbox's 5-min backoff cap (as
// mi5ShortTiming).
const mi5TestTiming = `{"preRun":20000,"preRunEvery":1000,"warmUp":5000,"checkEvery":20000,"readOpenEvery":3000,"floor":2000,"quiet":8000,"drainMax":480000}`

// TestMI5_LabRun_RealHub (P14, P16, g3-16): a short lab run end to end on
// the real ipcam-hub, the fork's MediaMTX, reccontroller --local's
// simulated cameras and fakeproviders, throttled as the lab runs it, here
// to 256 KiB/s (-throttle 262144): each 512 KiB part stays about 2 s on
// the wire, so the restart lands between two parts of one segment
// (unthrottled, a segment's four parts can all go up between two of the
// run's 50-ms polls), and tearAfter after a rotation, when the cameras'
// next segments hold parts, although the firewall seam lets the uploads
// go on through the block and leaves no backlog (at 1 MiB/s every
// segment was up within 2 s of its rotation): the lab's login rig, the
// quota probe, the encrypted pre-run's reads, the recording, a 75-s block
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
	r, f := realRigWith(t, []string{"-throttle", "262144"})
	recc := binary(t, "reccontroller", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cmd/reccontroller")
	log, seam := r.firewallLog()
	p := r.start([]string{seam, envReccontroller + "=" + recc, envMediaDir + "=" + media, envLabLAN + "=127.0.0.1", envMI5Timing + "=" + mi5TestTiming},
		"mi5", "--lab", "--block", "75s", "--segment-secs", "30", "--part-size", "512KiB", "--cameras", "4", "--fakes", f.base)
	if !p.wait(15 * time.Minute) {
		t.Fatalf("mi5 did not end\nstdout:\n%s\nstderr:\n%s", p.stdout, tail(p.stderr.String(), 40))
	}
	if p.code != 0 {
		t.Fatalf("mi5: exit %d (%s)\nstdout:\n%s\nstderr:\n%s", p.code, p.errorLine(), p.stdout, tail(p.stderr.String(), 40))
	}
	if first := strings.SplitN(p.stdout.String(), "\n", 2)[0]; first != msgMI5KeepOpen {
		t.Errorf("the first line %q; want %q", first, msgMI5KeepOpen)
	}
	if started := strings.SplitN(msgMI5Started, "%s", 2)[0] + "75 s "; !strings.Contains(p.stdout.String(), started) {
		t.Errorf("no %q… line:\n%s", started, p.stdout)
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
