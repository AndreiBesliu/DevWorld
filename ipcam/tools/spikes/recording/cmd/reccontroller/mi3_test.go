package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi3"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

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
	mediamtx := spiketest.Required(t, "MEDIAMTX_BIN", "RECSPIKE_REQUIRE_MEDIAMTX")
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	bin := t.TempDir()
	hub := spiketest.BuildBinary(t, bin, "ipcam-hub", "github.com/AndreiBesliu/DevWorld/ipcam/hub/cmd/ipcam-hub")
	recspike := spiketest.BuildBinary(t, bin, "recspike", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cmd/recspike")

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
			t.Logf("CPU %.1f %%, RAM %.0f MB, budgets %v, disk %v, fsync/s %.3f, latency p95 %v ms, pacing p99 %d ms, skipped %d, disk queue peak %d, load CPU %v",
				d.CPUPctTotal, d.RAMMb, d.Budgets, d.DiskRatio, d.FsyncPerSecPerFile, d.LatencyP95Ms, d.PacingP99Ms, d.SkippedAUs, d.DiskQueuePeakBytes, d.LoadCPUPct)
			if d.Tier != c.tier || d.Cameras != 2 || len(d.Steps) == 0 || d.Steps[0].Cameras != 2 || d.Steps[0].Minutes != mi3.FullMinutes {
				t.Fatalf("tier %s, %d cameras, steps %+v; want %s at 2 cameras for its hour first", d.Tier, d.Cameras, d.Steps, c.tier)
			}
			// The hour counts: its loads ran their whole window, by mis-2's
			// 95 % checks (the live frames, each camera's recorded seconds,
			// no MediaMTX restart), and the controller sent evenly (Final
			// fix B review, M4).
			if s := d.Steps[0]; !s.Valid || s.InvalidReason != "" {
				t.Errorf("the hour at 2 cameras does not count: valid %v, pacing p99 %d ms, %d skipped, %q", s.Valid, s.PacingP99Ms, s.SkippedAUs, s.InvalidReason)
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
			// Each load's CPU, as a share of the whole machine (C1-R19); the
			// export runs at full speed, so it always takes some.
			if len(d.LoadCPUPct) != len(mi3.Loads) || d.LoadCPUPct[mi3.LoadExport] <= 0 || d.LoadCPUPct[mi3.LoadExport] > 100 {
				t.Errorf("load CPU %v, want each load's share of the machine", d.LoadCPUPct)
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

// TestMI3_EndSaysTheLeastCoveredLiveTile (mis-2, Final fix C): each live
// peer's access units count for its own session's tiles, and the step's
// result names the tile that received frames for the least time since the
// binds, with its session and seconds, which the host compares with its
// window; the frames' count does not decide.
func TestMI3_EndSaysTheLeastCoveredLiveTile(t *testing.T) {
	r := newRig(t)
	conn := r.connect("pc")
	tiles := []string{"cam01_sub", "cam01_main"}
	st := newMi3Step(conn, r.c.sendLog, 2, tiles)
	r.c.mu.Lock()
	r.c.steps["pc-mi3-03"] = st
	r.c.mu.Unlock()
	t0 := r.clock.Now()
	st.coverage.Start(t0)
	first, second := st.onAU(1), st.onAU(2)
	for i := range 121 { // a frame every 500 ms for 60 s
		at := t0.Add(time.Duration(i) * 500 * time.Millisecond)
		for _, tile := range tiles {
			first(tile, nil, at)
		}
		second("cam01_sub", nil, at)
		if i <= 20 { // the second session's main stops after 10 s
			second("cam01_main", nil, at)
		}
	}
	r.clock.set(t0.Add(time.Minute))
	r.send(conn, link.Mi3End{RunID: "pc-mi3-03"})
	res, ok := r.receive(conn).(link.Mi3Result)
	if !ok || res.LiveLeastTile != "cam01_main" || res.LiveLeastSession != 2 || res.LiveLeastS != 10 {
		t.Errorf("got %#v, want cam01_main of session 2 covered for 10 s", res)
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

// deadAnswer answers offer as a peer of api that closes at once would: the
// live peer takes the answer, and its channel never opens.
func deadAnswer(t *testing.T, api *webrtc.API, offer string) string {
	t.Helper()
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		t.Fatal(err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	return pc.LocalDescription().SDP
}

// TestMI3_LiveAnswersEndWithTheirConnection (G20; Task 9 review): the
// controller waits for a step's live tiles to bind under the connection
// that sent the answers: when the host disconnects, the step's peers close
// and the wait ends at once, not after liveAcceptWait. The screen says in
// Romanian that the host disconnected, and the controller refuses nothing
// on the closed connection (a refusal is said on the screen too).
func TestMI3_LiveAnswersEndWithTheirConnection(t *testing.T) {
	waitFor(t, "the answers of earlier tests to end", func() bool { return running(t, "(*ctl).mi3Answers") == 0 })
	r := newRig(t)
	api, err := controller.NewLiveAPI()
	if err != nil {
		t.Fatal(err)
	}
	r.c.liveAPI, r.c.streams = api, []controller.Stream{{Path: "cam01_sub"}}
	conn := r.connect("pc")
	r.send(conn, link.Mi3Start{RunID: "pc-mi3-02", Sessions: 1, Tiles: []string{"cam01_sub"}})
	offers, ok := r.receive(conn).(link.Mi3Offers)
	if !ok || len(offers.Offers) != 1 {
		t.Fatalf("got %#v, want one offer", offers)
	}
	r.send(conn, link.Mi3Answers{RunID: "pc-mi3-02", Answers: []string{deadAnswer(t, api, offers.Offers[0])}})
	waitFor(t, "the wait for the live tiles", func() bool { return running(t, "(*ctl).mi3Answers") == 1 })

	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the wait for the live tiles to end with the connection", func() bool { return running(t, "(*ctl).mi3Answers") == 0 })
	screen := r.screen.String()
	if !strings.Contains(screen, "Pasul MI3 pc-mi3-02: gazda s-a deconectat înainte ca sesiunile live să pornească.") {
		t.Errorf("the screen does not say that the host disconnected:\n%s", screen)
	}
	if strings.Contains(screen, "nu a pornit") {
		t.Errorf("the controller refused the step on the closed connection:\n%s", screen)
	}
}

// TestMI3_AnswersOfAHostThatLeftAtOnce (Windows CI, d4bd2ab): a host that
// disconnects right after it sent its answers, before the controller
// applied them, is a host that disconnected, whichever comes first, the
// controller's wait or the closing of the connection's live peers: the
// controller says so, and refuses nothing.
func TestMI3_AnswersOfAHostThatLeftAtOnce(t *testing.T) {
	waitFor(t, "the answers of earlier tests to end", func() bool { return running(t, "(*ctl).mi3Answers") == 0 })
	r := newRig(t)
	api, err := controller.NewLiveAPI()
	if err != nil {
		t.Fatal(err)
	}
	r.c.liveAPI, r.c.streams = api, []controller.Stream{{Path: "cam01_sub"}}
	const steps = 20
	for i := range steps {
		runID := fmt.Sprintf("pc-mi3-%02d", i+1)
		conn := r.connect("pc")
		r.send(conn, link.Mi3Start{RunID: runID, Sessions: 1, Tiles: []string{"cam01_sub"}})
		offers, ok := r.receive(conn).(link.Mi3Offers)
		if !ok || len(offers.Offers) != 1 {
			t.Fatalf("got %#v, want one offer", offers)
		}
		r.send(conn, link.Mi3Answers{RunID: runID, Answers: []string{deadAnswer(t, api, offers.Offers[0])}})
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		left := "Pasul MI3 " + runID + ": gazda s-a deconectat înainte ca sesiunile live să pornească."
		for deadline := time.Now().Add(10 * time.Second); !strings.Contains(r.screen.String(), left); time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("the screen does not say that the host of %s disconnected:\n%s", runID, r.screen.String())
			}
		}
	}
	waitFor(t, "the answers to end", func() bool { return running(t, "(*ctl).mi3Answers") == 0 })
	if screen := r.screen.String(); strings.Contains(screen, "nu a pornit") || strings.Contains(screen, "nu a fost anunțat") {
		t.Errorf("the controller refused a step of a host that left:\n%s", screen)
	}
}
