package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/lastindex"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi4"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// TestMI4_SmokeEndToEnd (G13, C01, G15): a short MI4 run on a Pi 4 with the
// real chain: reccontroller serving the media; ipcam-hub with the fork
// recording its cam01_main, encrypted, into a segment of a few seconds; and
// ipcam-hub's diag benches reading, seeking in and encrypting it, their
// lines read by mi4. The envelope holds every figure, and reaches the PC's
// controller at the goodbye. The cache drops are left out (no root here):
// at the first one, before the reads, the test reads the closed segment's
// parts back with lastindex, since every read must be as long as its
// median full part (G22). The segment's parts are not 500 KB each: the
// controller's 60-s loop of main.h264 starts with x264's rate control
// settling (seconds 1 to 13 of the loop: 2.4 MB, then 66 KB to 480 KB), and
// the few seconds that the smoke records fall in it. The recording and its
// key are gone after the run.
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
	runs := filepath.Join(work, "work", "mi4")
	var parts []lastindex.Part
	dropCaches := func() error {
		if parts != nil {
			return nil
		}
		var err error
		parts, err = segmentParts(runs)
		return err
	}
	out := &syncBuffer{}
	err = mi4.Run(ctx, mi4.Options{Controller: lns.Link.Addr().String(), Hub: hub, MediaMTX: mediamtx,
		Results: filepath.Join(work, "results"), Work: filepath.Join(work, "work"), Out: out,
		Host: func(context.Context) (mi1.HostState, error) {
			return mi1.HostState{Pseudonym: "pi", OS: runtime.GOOS, Storage: "unknown", WriteCache: "unknown", PiModel: pi4, Cameras: 4}, nil
		},
		Euid: func() int { return 0 }, DropCaches: dropCaches,
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
	want := fullPartLength(parts)
	t.Logf("the segment's parts, bytes [first-last SEI index]: %s; the median full part: %d bytes", partsText(parts), want)
	// Windows' monotonic clock moves at the system timer's tick (about 0.5 ms
	// on the runner), so a read of a short part can time at 0 ms there.
	if len(d.ReadMs) != 20 || d.ReadSize != int(want) || d.ReadMaxMs != slices.Max(d.ReadMs) || (runtime.GOOS != "windows" && d.ReadMaxMs <= 0) {
		t.Errorf("reads %v (max %v) of %d bytes, want 20 reads of the median full part's %d bytes", d.ReadMs, d.ReadMaxMs, d.ReadSize, want)
	}
	if keys := keysIn(t, runs); len(keys) != 0 {
		t.Errorf("identities left after the run: %q", keys)
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

// segmentParts reads back the parts of the segment that MI4 measures, the
// first closed one under its run folders, root, with the one identity
// there, which decrypts it: MediaMTX names a segment by its start, and the
// one that the hub's stop may close after it comes later.
func segmentParts(root string) ([]lastindex.Part, error) {
	var segments, keys []string
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case strings.HasSuffix(path, ".mp4.age"):
			segments = append(segments, path)
		case strings.HasSuffix(path, ".key"):
			keys = append(keys, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 || len(keys) != 1 {
		return nil, fmt.Errorf("segments %q and identities %q under %s, want a segment and one identity", segments, keys, root)
	}
	slices.Sort(segments)
	f, err := os.Open(keys[0])
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	ids, err := age.ParseIdentities(f)
	if err != nil {
		return nil, err
	}
	_, parts, err := lastindex.ReadParts(segments[0], ids[0])
	if err == nil && len(parts) < 2 {
		err = fmt.Errorf("the segment has %d parts, want a full part before its last", len(parts))
	}
	return parts, err
}

// fullPartLength is G22's read length, which readbench takes in ipcam-hub
// (tools do not import the hub, C01): the nearest-rank median length of the
// parts without the last one, and without those shorter than half that
// median.
func fullPartLength(parts []lastindex.Part) int64 {
	median := func(lengths []int64) int64 {
		slices.Sort(lengths)
		return lengths[(len(lengths)+1)/2-1]
	}
	var lengths []int64
	for _, p := range parts[:len(parts)-1] {
		lengths = append(lengths, p.Bytes)
	}
	m := median(slices.Clone(lengths))
	return median(slices.DeleteFunc(lengths, func(n int64) bool { return 2*n < m }))
}

// partsText is each part's bytes and the SEI indexes of its first and last
// samples, which place it in the controller's loop of the media.
func partsText(parts []lastindex.Part) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d [%d-%d]", p.Bytes, p.First, p.Last)
	}
	return b.String()
}

// keysIn is every identity file under root.
func keysIn(t *testing.T, root string) []string {
	t.Helper()
	var keys []string
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if strings.HasSuffix(path, ".key") {
			keys = append(keys, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return keys
}
