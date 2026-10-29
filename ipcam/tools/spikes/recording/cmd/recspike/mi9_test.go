package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The test binary stands in for ipcam-hub when fakeHubEnv is set: "diag host"
// prints a host on an SSD with no Pi model; "run --config <run.json>
// --stop-on-stdin-eof" appends each job of run.json, as JSON, to the file
// that fakeLogEnv names, writes a 10-s encrypted test recording for the
// first job's recipients (spiketest.WriteSegment), prints reconcile, ready,
// the job's recording and its segment_closed, and exits at the end of its
// stdin.
const (
	fakeHubEnv = "RECSPIKE_FAKE_HUB"
	fakeLogEnv = "RECSPIKE_FAKE_LOG"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeHubEnv) == "1" {
		os.Exit(fakeHub(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeHub(args []string) int {
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, "fake ipcam-hub:", err)
		return 1
	}
	if len(args) >= 2 && args[0] == "diag" && args[1] == "host" {
		fmt.Println(`{"schema":"ipcam.diag.host/1","os":"` + runtime.GOOS + `","arch":"amd64","physicalCores":4,"logicalCpus":8,"ramMb":8000,"piModel":null,"storage":"ssd","writeCache":"on","recommendedCameras":4,"cores6Plus":false}`)
		return 0
	}
	if len(args) != 4 || args[0] != "run" || args[1] != "--config" || args[3] != "--stop-on-stdin-eof" {
		return fail(errors.New("usage: run --config <run.json> --stop-on-stdin-eof"))
	}
	cfg, err := hubproc.ReadConfig(args[2])
	if err != nil {
		return fail(err)
	}
	log, err := os.OpenFile(os.Getenv(fakeLogEnv), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own log
	if err != nil {
		return fail(err)
	}
	for _, j := range cfg.Jobs {
		b, _ := json.Marshal(j)
		_, _ = log.Write(append(b, '\n'))
	}
	_ = log.Close()
	if len(cfg.Jobs) == 0 {
		return fail(errors.New("no job"))
	}
	j := cfg.Jobs[0]
	var recipients []age.Recipient
	for _, r := range j.Recipients {
		rec, err := age.ParseX25519Recipient(r)
		if err != nil {
			return fail(err)
		}
		recipients = append(recipients, rec)
	}
	dir := filepath.Join(cfg.DataDir, "rec", j.Cam, j.Profile, "2026-10-29")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fail(err)
	}
	segment := filepath.Join(dir, "120000_000000.mp4.age")
	if _, err := spiketest.WriteSegment(segment, 10, recipients...); err != nil {
		return fail(err)
	}
	emit := func(fields map[string]any) {
		fields["atMs"] = time.Now().UnixMilli()
		b, _ := json.Marshal(fields)
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
	emit(map[string]any{"type": "reconcile", "seq": 1, "final": true, "aborted": false})
	emit(map[string]any{"type": "ready", "jobs": []any{}})
	emit(map[string]any{"type": "recording", "cam": j.Cam, "profile": j.Profile})
	emit(map[string]any{"type": "segment_closed", "cam": j.Cam, "profile": j.Profile, "path": segment})
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

// fakeController is the other device's reccontroller as MI9 sees it: it
// gives the host its session, plays the host's recording with a Pion
// client (mi9.Client) when the host starts the run, and keeps the results
// that the host sends.
type fakeController struct {
	addr    string
	results chan link.Results
	errs    chan error
}

func startFakeController(t *testing.T) *fakeController {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeController{addr: ln.Addr().String(), results: make(chan link.Results, 4), errs: make(chan error, 4)}
	api, err := mi9.NewAPI(mi9.LAN)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		conn := link.NewConn(c)
		defer func() { _ = conn.Close() }()
		var client *mi9.Client
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
				if client, err = mi9.NewClient(api, webrtc.Configuration{}); err != nil {
					f.errs <- err
					return
				}
				_ = conn.Send(link.Mi9Offer{RunID: m.RunID, Offer: client.Offer()})
			case link.Mi9Answer:
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				r, err := playAnswer(ctx, client, m.Answer)
				cancel()
				if err != nil {
					f.errs <- err
					_ = conn.Send(link.Refused{RunID: m.RunID, Reason: err.Error()})
					continue
				}
				_ = conn.Send(link.Mi9Result{RunID: m.RunID, ThroughputMbps: r.ThroughputMbps, SeekMs: r.SeekMs, RTTMs: r.RTTMs})
			case link.Results:
				f.results <- m
			case link.Bye:
				return
			}
		}
	}()
	return f
}

// playAnswer applies the host's answer and measures.
func playAnswer(ctx context.Context, c *mi9.Client, answer string) (mi9.BulkResult, error) {
	if err := c.Accept(ctx, answer); err != nil {
		return mi9.BulkResult{}, err
	}
	return c.Measure(ctx)
}

// TestMI9Lan_RecordsSegmentWhenNoneExists (G26 leg 3): on a host without
// MI4's segment, `recspike mi9 --lan` first records one through ipcam-hub,
// an encrypted cam01_main job of 300 s from the controller's camera, then
// serves that first closed segment to the controller's playback peer, which
// measures it; recspike writes the lan envelope, sends it to the
// controller (G15), and removes the recording and its identity (PH0-22).
func TestMI9Lan_RecordsSegmentWhenNoneExists(t *testing.T) {
	dir := t.TempDir()
	realExecutable := executable
	t.Cleanup(func() { executable = realExecutable })
	executable = func() (string, error) { return filepath.Join(dir, "recspike"), nil }
	hub := filepath.Join(dir, "ipcam-hub")
	if runtime.GOOS == "windows" {
		hub += ".exe"
	}
	self, err := os.ReadFile(os.Args[0]) //nolint:gosec // the test binary, the fake ipcam-hub
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hub, self, 0o700); err != nil { //nolint:gosec // the test binary as the fake ipcam-hub
		t.Fatal(err)
	}
	jobs := filepath.Join(dir, "jobs.log")
	t.Setenv(fakeHubEnv, "1")
	t.Setenv(fakeLogEnv, jobs)
	ctl := startFakeController(t)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	var out strings.Builder
	if err := run(ctx, []string{"mi9", "--lan", "--controller", ctl.addr}, bufio.NewReader(strings.NewReader("")), &out); err != nil {
		select {
		case cerr := <-ctl.errs:
			err = errors.Join(err, cerr)
		default:
		}
		t.Fatalf("recspike mi9 --lan = %v\n%s", err, out.String())
	}

	b, err := os.ReadFile(jobs) //nolint:gosec // the test's own log
	if err != nil {
		t.Fatal(err)
	}
	var j hubproc.Job
	if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &j) != nil ||
		j.Cam != "cam01" || j.Profile != "main" || j.SegmentSecs != 300 || len(j.Recipients) != 2 || !strings.HasPrefix(j.RestreamURL, "rtsp://") {
		t.Errorf("ipcam-hub's jobs: %s; want one encrypted cam01_main job of 300 s", b)
	}
	host := ownerHost()
	runID := host + "-mi9-1"
	e, err := results.Read(filepath.Join(dir, "results", "MI9", "lan", runID+".json"))
	if err != nil {
		t.Fatalf("the envelope: %v\n%s", err, out.String())
	}
	if e.Host != host || e.Set != "lan" || e.MI9 == nil || len(e.MI9.Legs) != 1 {
		t.Fatalf("the envelope %+v, want one lan leg of %s", e, host)
	}
	if l := e.MI9.Legs[0]; l.Transport != "lan" || l.Source != mi9.SourceLAN || l.ThroughputMbps <= 0 || len(l.SeekMs) != mi9.Seeks {
		t.Errorf("the leg %+v, want the LAN's throughput and %d seeks", l, mi9.Seeks)
	}
	select {
	case r := <-ctl.results:
		if r.Name != "MI9/lan/"+runID+".json" {
			t.Errorf("the controller got %q", r.Name)
		}
	case <-time.After(10 * time.Second):
		t.Error("the controller did not get the envelope")
	}
	var left []string
	_ = filepath.WalkDir(filepath.Join(dir, "work", "mi9"), func(path string, _ fs.DirEntry, err error) error {
		if err == nil && (strings.HasSuffix(path, ".key") || strings.HasSuffix(path, ".mp4.age")) {
			left = append(left, path)
		}
		return nil
	})
	if len(left) != 0 {
		t.Errorf("left under work/mi9: %q", left)
	}
}
