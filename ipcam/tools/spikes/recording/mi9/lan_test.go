package mi9

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubrec"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi4"
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
// answer, and keeps the results that the host sends. refuseStart and
// refuseAnswer, when set, are its refusals of the start and of the answer.
type lanController struct {
	addr                      string
	refuseStart, refuseAnswer string
	results                   chan link.Results
}

func startLANController(t *testing.T, refuseStart, refuseAnswer string) *lanController {
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
	c := &lanController{addr: ln.Addr().String(), refuseStart: refuseStart, refuseAnswer: refuseAnswer, results: make(chan link.Results, 4)}
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
			if c.refuseStart != "" {
				_ = conn.Send(link.Refused{RunID: m.RunID, Reason: c.refuseStart})
				continue
			}
			if client, err = NewClient(api, Configuration(LAN, nil)); err != nil {
				t.Error(err)
				return
			}
			_ = conn.Send(link.Mi9Offer{RunID: m.RunID, Offer: client.Offer()})
		case link.Mi9Answer:
			if c.refuseAnswer != "" {
				_ = conn.Send(link.Refused{RunID: m.RunID, Reason: c.refuseAnswer})
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			err := client.Accept(ctx, m.Answer)
			var r BulkResult
			if err == nil {
				r, err = client.Measure(ctx)
			}
			cancel()
			if err != nil {
				_ = conn.Send(link.Refused{RunID: m.RunID, Reason: err.Error()})
				continue
			}
			_ = conn.Send(link.Mi9Result{RunID: m.RunID, ThroughputMbps: r.ThroughputMbps, SeekMs: r.SeekMs, RTTMs: r.RTTMs})
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
// records it (mi4.Runs, hubrec.FiveMin), and records nothing itself; the
// PC records its own segment even then. Each writes its lan envelope.
func TestRunLAN_PiPlaysMI4sSegment(t *testing.T) {
	dir := t.TempDir()
	hub, jobs := spiketest.InstallFakeHub(t, dir)
	run, err := mi4.Runs(filepath.Join(dir, "work")).Start(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	left, err := hubrec.Record(t.Context(), hubrec.Options{Hub: hub, CameraURL: "rtsp://127.0.0.1:8554/cam01_main", Run: run,
		Name: hubrec.FiveMin, SegmentSecs: 300})
	if err != nil {
		t.Fatal(err)
	}
	c := startLANController(t, "", "")

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
	screen, err := lanRun(t, dir, hub, "pc", startLANController(t, reason, ""))
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
			screen, err := lanRun(t, dir, hub, "pc", startLANController(t, "", refuse))
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
