package mi9

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubrec"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The test binary stands in for ipcam-hub when spiketest.FakeHubEnv is set
// (spiketest.FakeHub).
func TestMain(m *testing.M) {
	if os.Getenv(spiketest.FakeHubEnv) == "1" {
		os.Exit(spiketest.FakeHub(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// lanController is the other device's reccontroller as RunLAN sees it: it
// gives the host its session, offers with a playback client (Client) when
// the host starts the run, plays the host's recording with the host's
// answer, as reccontroller does, and keeps the results that the host sends.
// Its zero fields are reccontroller's own behaviour; set, they change it:
//   - refuseStart and refuseAnswer are its refusals of the start and of the
//     answer;
//   - strayStart and strayAnswer are messages of the run that it sends
//     instead of its offer and of its result;
//   - measureWait is its measurement's wait, reccontroller's
//     mi9MeasureWait, 2 min when unset.
type lanController struct {
	refuseStart, refuseAnswer string
	strayStart, strayAnswer   func(runID string) link.Message
	measureWait               time.Duration

	addr    string
	results chan link.Results
}

func startLANController(t *testing.T, c *lanController) *lanController {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	api, err := NewAPI(LAN)
	if err != nil {
		t.Fatal(err)
	}
	c.addr, c.results = ln.Addr().String(), make(chan link.Results, 4)
	if c.measureWait == 0 {
		c.measureWait = 2 * time.Minute
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go c.serve(t, api, link.NewConn(conn))
		}
	}()
	return c
}

func (c *lanController) serve(t *testing.T, api *webrtc.API, conn *link.Conn) {
	defer func() { _ = conn.Close() }()
	var client *Client
	defer func() {
		if client != nil {
			_ = client.Close()
		}
	}()
	for {
		m, err := conn.Receive()
		if err != nil {
			return
		}
		switch m := m.(type) {
		case link.Hello:
			_ = conn.Send(link.Session{RTSPBase: "rtsp://127.0.0.1:8554", User: "u", Pass: "p", Streams: []string{"cam01_main"}})
		case link.Mi9Start:
			switch {
			case c.refuseStart != "":
				_ = conn.Send(link.Refused{RunID: m.RunID, Reason: c.refuseStart})
				continue
			case c.strayStart != nil:
				_ = conn.Send(c.strayStart(m.RunID))
				continue
			}
			if client, err = NewClient(api, Configuration(LAN, nil)); err != nil {
				t.Error(err)
				return
			}
			_ = conn.Send(link.Mi9Offer{RunID: m.RunID, Offer: client.Offer()})
		case link.Mi9Answer:
			switch {
			case c.refuseAnswer != "":
				_ = conn.Send(link.Refused{RunID: m.RunID, Reason: c.refuseAnswer})
				continue
			case c.strayAnswer != nil:
				_ = conn.Send(c.strayAnswer(m.RunID))
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			err := client.Accept(ctx, m.Answer)
			cancel()
			var r BulkResult
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), c.measureWait)
				r, err = client.Measure(ctx)
				cancel()
			}
			if err != nil {
				_ = conn.Send(link.Refused{RunID: m.RunID, Reason: err.Error()})
				continue
			}
			_ = conn.Send(link.Mi9Result{RunID: m.RunID, ThroughputMbps: r.ThroughputMbps, SeekMs: r.SeekMs, RTTMs: r.RTTMs, Partial: r.Partial})
		case link.Results:
			c.results <- m
		case link.Bye:
			return
		}
	}
}

// lanRun is RunLAN on host (pc or pi) in the folder dir, with ipcam-hub at
// hub, against the controller c; it returns the owner's screen.
func lanRun(t *testing.T, dir, hub, host string, c *lanController) (string, error) {
	t.Helper()
	var out strings.Builder
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	err := RunLAN(ctx, LANOptions{Controller: c.addr, Hub: hub, Results: filepath.Join(dir, "results"), Work: filepath.Join(dir, "work"), Out: &out,
		Host: func(context.Context) (mi1.HostState, error) {
			return mi1.HostState{Pseudonym: host, OS: "linux", Storage: "ssd"}, nil
		}})
	return out.String(), err
}

// leftUnder is every recording and identity file left under root.
func leftUnder(t *testing.T, root string) []string {
	t.Helper()
	var left []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && (d.Name() == hubrec.IdentityFile || strings.HasSuffix(path, ".mp4.age")) {
			left = append(left, path)
		}
		return nil
	})
	return left
}

// TestRunLAN_PiPlaysMI4sSegment (G26 leg 3): on the Pi, RunLAN plays the
// 5-min segment that MI4 left in its runs' folder, recorded there as MI4
// records it (hubrec.Runs(work, "mi4"), hubrec.FiveMin), and records
// nothing itself; the PC records its own segment even then. Each writes
// its lan envelope.
func TestRunLAN_PiPlaysMI4sSegment(t *testing.T) {
	dir := t.TempDir()
	hub, jobs := spiketest.InstallFakeHub(t, dir)
	run, err := hubrec.Runs(filepath.Join(dir, "work"), "mi4").Start(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	left, err := hubrec.Record(t.Context(), hubrec.Options{Hub: hub, CameraURL: "rtsp://127.0.0.1:8554/cam01_main", Run: run,
		Name: hubrec.FiveMin, SegmentSecs: 300})
	if err != nil {
		t.Fatal(err)
	}
	c := startLANController(t, &lanController{})

	screen, err := lanRun(t, dir, hub, "pi", c)
	if err != nil {
		t.Fatalf("RunLAN on the Pi = %v\n%s", err, screen)
	}
	if got := spiketest.HubJobs(t, jobs); len(got) != 1 {
		t.Errorf("ipcam-hub ran %d jobs, want only the one of MI4's segment: the Pi plays it", len(got))
	}
	if want := "Proba MI9 începe cu segmentul de 5 min al probei MI4 (" + left.Path + ")."; !strings.Contains(screen, want) {
		t.Errorf("the screen does not say «%s»:\n%s", want, screen)
	}
	e, err := results.Read(filepath.Join(dir, "results", "MI9", LANSet, "pi-mi9-1.json"))
	if err != nil || e.MI9 == nil || len(e.MI9.Legs) != 1 || e.MI9.Legs[0].Source != SourceLAN || len(e.MI9.Legs[0].SeekMs) != Seeks {
		t.Errorf("the Pi's envelope: %+v, %v; want its lan leg with %d seeks", e, err, Seeks)
	}

	screen, err = lanRun(t, dir, hub, "pc", c)
	if err != nil {
		t.Fatalf("RunLAN on the PC = %v\n%s", err, screen)
	}
	if got := spiketest.HubJobs(t, jobs); len(got) != 2 || strings.Contains(screen, "probei MI4") {
		t.Errorf("ipcam-hub ran %d jobs and the screen says:\n%s\nwant the PC to record its own segment", len(got), screen)
	}
	if _, err := results.Read(filepath.Join(dir, "results", "MI9", LANSet, "pc-mi9-1.json")); err != nil {
		t.Errorf("the PC's envelope: %v", err)
	}
	if left := leftUnder(t, filepath.Join(dir, "work", "mi9")); len(left) != 0 {
		t.Errorf("left under work/mi9: %q", left)
	}
}

// TestRunLAN_ControllerRefusesBeforeRecording: the run starts on the
// controller first, so a controller that cannot measure this host over the
// LAN (started with -local, or on this same device) refuses before
// anything is recorded: no ipcam-hub job, no envelope, and the refusal is
// the run's error.
func TestRunLAN_ControllerRefusesBeforeRecording(t *testing.T) {
	dir := t.TempDir()
	hub, jobs := spiketest.InstallFakeHub(t, dir)
	const reason = "gazda și reccontroller sunt pe același dispozitiv"
	screen, err := lanRun(t, dir, hub, "pc", startLANController(t, &lanController{refuseStart: reason}))
	if err == nil || !strings.Contains(err.Error(), "controlorul a refuzat proba: "+reason) {
		t.Errorf("RunLAN = %v, want the controller's refusal: %s\n%s", err, reason, screen)
	}
	if got := spiketest.HubJobs(t, jobs); len(got) != 0 {
		t.Errorf("ipcam-hub ran %d jobs before the refusal, want none", len(got))
	}
	if _, err := os.Stat(filepath.Join(dir, "results", "MI9")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("results/MI9: %v, want no envelope", err)
	}
}

// TestRunLAN_SaysWhenTheIdentityStays (PH0-22): when RunLAN cannot remove
// its recording and RI_g's identity at the end, the owner reads a Romanian
// line that names the folder to delete by hand, and a run that would
// otherwise succeed returns that error; a run that fails keeps its own.
func TestRunLAN_SaysWhenTheIdentityStays(t *testing.T) {
	realRemoveAll := removeAll
	t.Cleanup(func() { removeAll = realRemoveAll })
	removeAll = func(path string) error {
		if filepath.Base(path) == hubrec.IdentityFile {
			return &fs.PathError{Op: "unlinkat", Path: path, Err: fs.ErrPermission}
		}
		return realRemoveAll(path)
	}
	for _, refuse := range []string{"", "redarea nu a reușit"} {
		t.Run("refuse="+refuse, func(t *testing.T) {
			dir := t.TempDir()
			hub, _ := spiketest.InstallFakeHub(t, dir)
			screen, err := lanRun(t, dir, hub, "pc", startLANController(t, &lanController{refuseAnswer: refuse}))
			folder := filepath.Join(dir, "work", "mi9")
			line := "Nu am putut șterge înregistrarea probei și cheia ei: ștergeți de mână dosarul " + folder + "."
			if !strings.Contains(screen, line) {
				t.Errorf("the screen does not say «%s»:\n%s", line, screen)
			}
			switch {
			case refuse == "" && (err == nil || !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), folder)):
				t.Errorf("RunLAN = %v, want the removal's error, which names %s", err, folder)
			case refuse != "" && (err == nil || errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), refuse)):
				t.Errorf("RunLAN = %v, want the run's own error alone: %s", err, refuse)
			}
			if left := leftUnder(t, folder); len(left) != 1 || filepath.Base(left[0]) != hubrec.IdentityFile {
				t.Errorf("left under work/mi9: %q, want only the identity that could not be removed", left)
			}
		})
	}
}

// stallAfter makes the hub side a peer too slow for the measurement's
// wait: it sends the first n bytes of each range, then nothing more until
// it closes.
func stallAfter(t *testing.T, n int64) {
	t.Helper()
	realSend := sendBulk
	t.Cleanup(func() { sendBulk = realSend })
	sendBulk = func(ctx context.Context, ch channel, src io.ReaderAt, off, size int64, message int) error {
		if err := realSend(ctx, ch, src, off, min(size, n), message); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}
}

// TestRunLAN_SlowRunWritesAMeasuredFailure (G27): a LAN too slow for the
// controller's measurement wait still decides. When the wait ends during
// the transfer, the controller stops it and sends the throughput of the
// bytes received by then, with those bytes and the time elapsed; RunLAN
// writes the lan envelope with that leg, partial and without seeks, and
// JudgeBulk and Judge fail it on the throughput: «pică», not «fără
// verdict».
func TestRunLAN_SlowRunWritesAMeasuredFailure(t *testing.T) {
	const sent = 64 << 10
	stallAfter(t, sent)
	dir := t.TempDir()
	hub, _ := spiketest.InstallFakeHub(t, dir)
	screen, err := lanRun(t, dir, hub, "pc", startLANController(t, &lanController{measureWait: 8 * time.Second}))
	if err != nil {
		t.Fatalf("RunLAN of a run too slow for its wait = %v, want its measured failure\n%s", err, screen)
	}
	e, err := results.Read(filepath.Join(dir, "results", "MI9", LANSet, "pc-mi9-1.json"))
	if err != nil || e.MI9 == nil || len(e.MI9.Legs) != 1 || e.MI9.Legs[0].Partial == nil {
		t.Fatalf("the envelope: %+v, %v; want one lan leg, partial", e, err)
	}
	leg := e.MI9.Legs[0]
	p := leg.Partial
	if p.ReceivedBytes != sent || p.SizeBytes <= sent || p.ElapsedMs <= 0 || len(leg.SeekMs) != 0 {
		t.Errorf("the leg: %+v, partial %+v; want the %d bytes received of the segment's, the time elapsed, and no seek", leg, *p, sent)
	}
	if want := float64(sent*8) / p.ElapsedMs / 1e3; math.Abs(leg.ThroughputMbps-want) > want/1e3 {
		t.Errorf("the leg's throughput is %v Mbps, want %v: the bytes received over the time elapsed", leg.ThroughputMbps, want)
	}
	r := BulkResult{Transport: Transport(leg.Transport), ThroughputMbps: leg.ThroughputMbps, SeekMs: leg.SeekMs, RTTMs: leg.RTTMs, Partial: p}
	if pass, reasons := JudgeBulk(r); pass || !strings.Contains(strings.Join(reasons, "\n"), "pe LAN (pragul ≥ 8 Mbps)") {
		t.Errorf("JudgeBulk = %v %q, want a failure on the LAN throughput", pass, reasons)
	}
	if j := Judge([]results.Envelope{e}); j.Verdict != results.Fail || j.PlanB != PlanB {
		t.Errorf("Judge = %q → %q, want the measured failure: pică → %s", j.Verdict, j.PlanB, PlanB)
	}
	if want := "Redarea nu s-a încheiat în timpul probei: au sosit 0,1 MB"; !strings.Contains(screen, want) {
		t.Errorf("the screen does not say «%s…»:\n%s", want, screen)
	}
	if left := leftUnder(t, filepath.Join(dir, "work", "mi9")); len(left) != 0 {
		t.Errorf("left under work/mi9: %q", left)
	}
}

// TestRunLAN_UnexpectedMessageIsAnError: a message of the run that the
// controller should not send at that point, a result instead of its offer
// or an offer instead of its result, stops RunLAN with a Romanian error,
// never a panic, and writes no envelope.
func TestRunLAN_UnexpectedMessageIsAnError(t *testing.T) {
	for name, c := range map[string]struct {
		ctl  *lanController
		want string
	}{
		"a result for the offer": {&lanController{strayStart: func(runID string) link.Message {
			return link.Mi9Result{RunID: runID, ThroughputMbps: 99, SeekMs: seeks(100)}
		}}, "controlorul a trimis mi9_result în loc de offer"},
		"an offer for the result": {&lanController{strayAnswer: func(runID string) link.Message {
			return link.Mi9Offer{RunID: runID, Offer: "v=0"}
		}}, "controlorul a trimis mi9_offer în loc de rezultat"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			hub, _ := spiketest.InstallFakeHub(t, dir)
			screen, err := lanRun(t, dir, hub, "pc", startLANController(t, c.ctl))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("RunLAN = %v, want the error «%s»\n%s", err, c.want, screen)
			}
			if _, err := os.Stat(filepath.Join(dir, "results", "MI9")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("results/MI9: %v, want no envelope", err)
			}
		})
	}
}

// TestMI9_DoesNotImportMI4: MI9 finds MI4's runs' folder through hubrec
// (hubrec.Runs), so neither mi9 nor any package that it imports is MI4's
// runner: reccontroller and mi9page, which import mi9, do not link it.
func TestMI9_DoesNotImportMI4(t *testing.T) {
	const module = "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/"
	importers := map[string][]string{} // a package of the module → the module's packages that import it
	seen := map[string]bool{}
	var visit func(pkg string)
	visit = func(pkg string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		files, err := filepath.Glob(filepath.Join("..", filepath.FromSlash(strings.TrimPrefix(pkg, module)), "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(path, module) {
					importers[path] = append(importers[path], pkg)
					visit(path)
				}
			}
		}
	}
	visit(module + "mi9")
	if by := importers[module+"mi4"]; len(by) > 0 {
		t.Errorf("mi9 links MI4's runner: mi4 is imported by %q", by)
	}
}
