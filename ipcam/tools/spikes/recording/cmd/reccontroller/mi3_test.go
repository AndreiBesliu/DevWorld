package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi3"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// required returns the path that the environment variable name holds; it
// skips without one, and fails instead when require is "1", as CI sets it.
func required(t *testing.T, name, require string) string {
	t.Helper()
	path := os.Getenv(name)
	if path == "" {
		if os.Getenv(require) == "1" {
			t.Fatalf("%s=1, but %s is empty", require, name)
		}
		t.Skipf("%s is unset", name)
	}
	if _, err := os.Stat(path); err != nil { //nolint:gosec // the test run's own path
		t.Fatalf("%s: %v", name, err)
	}
	return path
}

// buildBinary builds the command pkg into dir and returns its path.
func buildBinary(t *testing.T, dir, name, pkg string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, pkg) //nolint:gosec // the test's own build
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, b)
	}
	return out
}

// TestMI3_SmokeEndToEnd (G20, R07, C1-R17, G15): short MI3 runs with the
// real chain: reccontroller serving the media on this machine's ports, not
// -local, so that its live peers listen; ipcam-hub with the fork recording
// 2 cameras, one clear and one encrypted; recspike's four loads as child
// processes, the live sessions answered by the video spike's session
// package and received by the controller's peers, the upload going over
// TLS into the sink. Every figure of the envelope is measured: the CPU, the
// memory, the disk ratio of each mode, the part fsyncs, the disk writers'
// queue peak, the live tiles' latency and the controller's pacing. The PC
// is H3, with 3 live sessions; a Pi 4 is H1b, with 1, judged on its CPU and
// RAM, and its results reach the PC's controller at the goodbye.
func TestMI3_SmokeEndToEnd(t *testing.T) {
	mediamtx := required(t, "MEDIAMTX_BIN", "RECSPIKE_REQUIRE_MEDIAMTX")
	media := required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	bin := t.TempDir()
	hub := buildBinary(t, bin, "ipcam-hub", "github.com/AndreiBesliu/DevWorld/ipcam/hub/cmd/ipcam-hub")
	recspike := buildBinary(t, bin, "recspike", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cmd/recspike")

	// serve sends the controller's log to its file; serveOn uses slog's.
	defaultLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(defaultLog) })
	ctx, cancel := context.WithCancel(t.Context())
	lns, err := controller.ListenAll(ctx, controller.ListenConfig{Host: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	screen := &syncBuffer{}
	ctlDir := t.TempDir()
	served := make(chan error, 1)
	go func() { served <- serveOn(ctx, serveConfig{dir: ctlDir, media: media}, lns, screen) }()
	defer func() {
		cancel()
		if err := <-served; err != nil {
			t.Errorf("serveOn: %v", err)
		}
		_ = lns.Close()
	}()

	pi4 := &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Tier: "H1b", Supported: true, Known: true, RecommendedMin: 4, RecommendedMax: 4, Source: "cpuinfo"}
	for _, c := range []struct {
		host   mi1.HostState
		tier   string
		pulled string // where the controller keeps the envelope, if it does
	}{
		{host: mi1.HostState{Pseudonym: "pc", OS: runtime.GOOS, Storage: "unknown", WriteCache: "unknown", Cameras: 2}, tier: mi3.TierH3},
		{host: mi1.HostState{Pseudonym: "pi", OS: runtime.GOOS, Storage: "unknown", WriteCache: "unknown", PiModel: pi4, Cameras: 2},
			tier: mi3.TierH1b, pulled: filepath.Join(ctlDir, "results", "pi", "MI3", "pi-mi3", "pi-mi3-1.json")},
	} {
		t.Run(c.host.Pseudonym, func(t *testing.T) {
			work := t.TempDir()
			out := &syncBuffer{}
			err := mi3.Run(ctx, mi3.Options{
				Host: c.host, Controller: lns.Link.Addr().String(), Hub: hub, MediaMTX: mediamtx, Self: recspike,
				Results: filepath.Join(work, "results"), Work: filepath.Join(work, "work"), Out: out,
				// An hour is 12 s, 15 min 3 s.
				Window:       func(minutes int) time.Duration { return time.Duration(minutes) * 200 * time.Millisecond },
				FixtureBytes: 4 << 20,
			})
			if err != nil {
				t.Fatalf("Run: %v\nrecspike:\n%s\nreccontroller:\n%s", err, out.String(), screen.String())
			}
			t.Logf("recspike:\n%s", out.String())
			set := mi3.SetFor(c.host.Pseudonym)
			e, err := results.Read(filepath.Join(work, "results", "MI3", set, set+"-1.json"))
			if err != nil {
				t.Fatal(err)
			}
			d := e.MI3
			t.Logf("CPU %.1f %%, RAM %.0f MB, budgets %v, disk %v, fsync/s %.3f, latency p95 %v ms, pacing p99 %d ms, skipped %d, disk queue peak %d",
				d.CPUPctTotal, d.RAMMb, d.Budgets, d.DiskRatio, d.FsyncPerSecPerFile, d.LatencyP95Ms, d.PacingP99Ms, d.SkippedAUs, d.DiskQueuePeakBytes)
			if d.Tier != c.tier || d.Cameras != 2 || len(d.Steps) == 0 || d.Steps[0].Cameras != 2 || d.Steps[0].Minutes != mi3.FullMinutes {
				t.Errorf("tier %s, %d cameras, steps %+v; want %s at 2 cameras for its hour first", d.Tier, d.Cameras, d.Steps, c.tier)
			}
			if d.CPUPctTotal <= 0 || d.CPUPctTotal > 100 || d.RAMMb < 10 {
				t.Errorf("CPU %v %%, RAM %v MB: not measured", d.CPUPctTotal, d.RAMMb)
			}
			if len(d.Budgets) != len(mi3.BudgetKeys(c.tier)) {
				t.Errorf("budgets %v, want the keys of %s", d.Budgets, c.tier)
			}
			// A run without a live sample ends with an error: the latency is
			// measured, and may round to 0 ms on loopback.
			for _, key := range mi3.BudgetKeys(c.tier) {
				if b, ok := d.Budgets[key]; !ok || b.Value < 0 || (b.Value == 0 && key != "latencyP95") {
					t.Errorf("budget %s: %+v, %v; want a measured value", key, b, ok)
				}
			}
			if cl, enc := d.DiskRatio["clear"], d.DiskRatio["encrypted"]; cl <= 1 || cl > 1.1 || enc <= 1 || enc > 1.2 {
				t.Errorf("disk ratios %v, want each a little over 1", d.DiskRatio)
			}
			if d.FsyncPerSecPerFile < 0.8 || d.FsyncPerSecPerFile > 1.05 {
				t.Errorf("%v fsyncs per second per file, want the 1 s parts' 1", d.FsyncPerSecPerFile)
			}
			if d.LatencyP95Ms < 0 || d.LatencyP95Ms > 5000 || d.DiskQueuePeakBytes <= 0 || len(d.Proxies) == 0 {
				t.Errorf("latency p95 %v ms, disk queue peak %d, proxies %d: not measured", d.LatencyP95Ms, d.DiskQueuePeakBytes, len(d.Proxies))
			}
			if c.pulled != "" {
				// The host is done once it has sent its folder, which the
				// controller then unpacks.
				waitFor(t, "the controller's pull of the Pi's results", func() bool {
					return strings.Contains(screen.String(), "Am adus rezultatele Pi-ului")
				})
				if _, err := results.Read(c.pulled); err != nil {
					t.Errorf("the PC's controller does not have the Pi's envelope: %v\nreccontroller:\n%s", err, screen.String())
				}
			}
		})
	}
	if got := strings.Count(screen.String(), "toate plăcile live primesc"); got < 2 {
		t.Errorf("the controller's screen shows the live tiles bound %d times, want each run's:\n%s", got, screen.String())
	}
}

// TestMI3_LocalControllerRefusesAStep: a -local controller has no live
// peers: MI3's step is refused, in Romanian, since its cameras would load
// the host that MI3 measures.
func TestMI3_LocalControllerRefusesAStep(t *testing.T) {
	r := newRig(t)
	conn := r.connect("pc")
	r.send(conn, link.Mi3Start{RunID: "pc-mi3-01", Sessions: 3, Tiles: []string{"cam01_sub", "cam01_main"}})
	if refused, ok := r.receive(conn).(link.Refused); !ok || refused.RunID != "pc-mi3-01" || refused.Reason != noLocalMI3 {
		t.Errorf("got %#v, want the step refused: %s", refused, noLocalMI3)
	}
}
