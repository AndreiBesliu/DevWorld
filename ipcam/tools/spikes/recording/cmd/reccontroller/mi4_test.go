package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi4"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// TestMI4_SmokeEndToEnd (G13, C01, G15): a short MI4 run on a Pi 4 with the
// real chain: reccontroller serving the media; ipcam-hub with the fork
// recording its cam01_main, encrypted, into a segment of a few seconds; and
// ipcam-hub's diag benches reading, seeking in and encrypting it, their
// lines read by mi4. The envelope holds every figure, and reaches the PC's
// controller at the goodbye. The cache drops are left out (no root here).
func TestMI4_SmokeEndToEnd(t *testing.T) {
	mediamtx := spiketest.Required(t, "MEDIAMTX_BIN", "RECSPIKE_REQUIRE_MEDIAMTX")
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	hub := spiketest.BuildBinary(t, t.TempDir(), "ipcam-hub", "github.com/AndreiBesliu/DevWorld/ipcam/hub/cmd/ipcam-hub")

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
	work := t.TempDir()
	out := &syncBuffer{}
	err = mi4.Run(ctx, mi4.Options{Controller: lns.Link.Addr().String(), Hub: hub, MediaMTX: mediamtx,
		Results: filepath.Join(work, "results"), Work: filepath.Join(work, "work"), Out: out,
		Host: func(context.Context) (mi1.HostState, error) {
			return mi1.HostState{Pseudonym: "pi", OS: runtime.GOOS, Storage: "unknown", WriteCache: "unknown", PiModel: pi4, Cameras: 4}, nil
		},
		Euid: func() int { return 0 }, DropCaches: func() error { return nil },
		Pace: &mi4.Pace{SegmentSecs: 4, TwoMinSecs: 2, Reads: 20, Seeks: 5, CryptoSeconds: 1, InfoSizes: []int{64 << 10}},
	})
	if err != nil {
		t.Fatalf("Run: %v\nrecspike:\n%s\nreccontroller:\n%s", err, out.String(), screen.String())
	}
	t.Logf("recspike:\n%s", out.String())
	e, err := results.Read(filepath.Join(work, "results", "MI4", "pi-mi4", "pi-mi4-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := e.MI4
	t.Logf("reads max %.2f ms (p50 %.2f, p99 %.2f) of %d bytes, info %+v; seeks %v, RAM %v, 2 min %v; crypto %.1f MB/s (all %.1f), threshold %.0f, after %d",
		d.ReadMaxMs, d.ReadP50Ms, d.ReadP99Ms, d.ReadSize, d.InfoReads, d.SeekMs, d.SeekRAMMs, d.Seek2MinMs, d.CryptoMBs, d.CryptoMBsAllCores, d.ThresholdMBs, d.RecommendedAfter)
	// A 1 s part of the 4 Mbps stream is about 500 KB.
	if len(d.ReadMs) != 20 || d.ReadSize < 100_000 || d.ReadSize > 2_000_000 || d.ReadMaxMs <= 0 {
		t.Errorf("reads %v of %d bytes, want 20 reads of one part's length", d.ReadMs, d.ReadSize)
	}
	if len(d.InfoReads) != 1 || d.InfoReads[0].Size != 64<<10 || len(d.SeekMs) != 5 || d.CryptoMBs <= 0 || d.CryptoMBsAllCores <= 0 {
		t.Errorf("info reads %+v, seeks %v, crypto %v / %v: not measured", d.InfoReads, d.SeekMs, d.CryptoMBs, d.CryptoMBsAllCores)
	}
	if d.ThresholdMBs != 20 || d.Model != pi4.Raw {
		t.Errorf("threshold %v, model %q; want a Pi 4's 20 MB/s", d.ThresholdMBs, d.Model)
	}
	waitFor(t, "the controller's pull of the Pi's results", func() bool {
		return strings.Contains(screen.String(), "Am adus rezultatele Pi-ului")
	})
	if _, err := results.Read(filepath.Join(ctlDir, "results", "pi", "MI4", "pi-mi4", "pi-mi4-1.json")); err != nil {
		t.Errorf("the PC's controller does not have the Pi's envelope: %v\nreccontroller:\n%s", err, screen.String())
	}
	// One link for the whole run: no goodbye before the end, whose pull
	// would find nothing, or fail.
	if n := strings.Count(screen.String(), "Gazda „pi” s-a conectat"); n != 1 || strings.Contains(screen.String(), "Nu am putut aduce") {
		t.Errorf("the Pi connected %d times, or a pull failed:\n%s", n, screen.String())
	}
}
