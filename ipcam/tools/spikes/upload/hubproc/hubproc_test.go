package hubproc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

func TestMain(m *testing.M) {
	if os.Getenv(spiketest.FakeHubEnv) == "1" {
		os.Exit(spiketest.FakeHub(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// testConfig is a hub start in base: the stand-in, run.json with sdConfirmed,
// a control token and the PID file.
func testConfig(t *testing.T, base, hub string, sdConfirmed bool) Config {
	t.Helper()
	token, err := NewControlToken()
	if err != nil {
		t.Fatal(err)
	}
	runJSON, _ := json.Marshal(map[string]any{"dataDir": filepath.Join(base, "hub"), "sdCardConfirmed": sdConfirmed, "jobs": []any{}})
	return Config{Bin: hub, RunJSON: filepath.Join(base, "hub", "spike", "run.json"), Files: map[string][]byte{"run.json": runJSON},
		ControlToken: token, PIDFile: filepath.Join(base, "state", "hub.pid")}
}

// records are the stand-in's records of one event.
func records(t *testing.T, log, event string) []spiketest.FakeRecord {
	t.Helper()
	var out []spiketest.FakeRecord
	for _, r := range spiketest.FakeRecords(t, log) {
		if r.Event == event {
			out = append(out, r)
		}
	}
	return out
}

// TestHubproc_StopIsCleanWithoutConsole: Stop ends the stand-in hub through
// its clean-stop path, its stdin's end, which it records before it exits 0,
// never through Kill; the stand-in was started with `run --config
// <run.json> --stop-on-stdin-eof`, and hub.pid named it while it ran. The
// windows job runs this test inside an S4U task (ci/s4u-run.ps1), which
// gives it no console.
func TestHubproc_StopIsCleanWithoutConsole(t *testing.T) {
	base := t.TempDir()
	hub, log := spiketest.InstallFakeHub(t, t.TempDir())
	cfg := testConfig(t, base, hub, true)
	h, err := Start(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := readPIDFile(cfg.PIDFile)
	if err != nil || rec.PID != h.Pid() || !runstate.Alive(rec.PID, rec.ProcStartMs) {
		t.Errorf("hub.pid = %+v, %v; want the running hub %d", rec, err, h.Pid())
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.killed || h.ExitCode() != 0 {
		t.Errorf("killed %v, exit code %d; want a clean stop with 0", h.killed, h.ExitCode())
	}
	starts, stops := records(t, log, spiketest.RecordStart), records(t, log, spiketest.RecordEOFStop)
	if len(starts) != 1 || starts[0].Detail != "run --config "+cfg.RunJSON+" --stop-on-stdin-eof" || !starts[0].Token {
		t.Errorf("starts %+v; want one, run --config %s --stop-on-stdin-eof, with the control token", starts, cfg.RunJSON)
	}
	if len(stops) != 1 || stops[0].PID != h.Pid() {
		t.Errorf("the stand-in's stdin stops: %+v; want one, of pid %d", stops, h.Pid())
	}
	if _, err := os.Stat(cfg.PIDFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("hub.pid after the stop: %v; want it removed", err)
	}
}

// TestHubproc_EventsAndControl: the events arrive in order on Events,
// control before ready, the control client reaches the stand-in's endpoint
// with the token, and Events closes after the hub's stdout ended.
func TestHubproc_EventsAndControl(t *testing.T) {
	hub, log := spiketest.InstallFakeHub(t, t.TempDir())
	h, err := Start(t.Context(), testConfig(t, t.TempDir(), hub, true))
	if err != nil {
		t.Fatal(err)
	}
	c, err := h.Control(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Begin(t.Context(), BeginRequest{DestID: "g-web", Provider: "GOOGLE_DRIVE", Platform: "web"})
	if err != nil || res.Plan != "B1" || res.UserCode == "" {
		t.Errorf("Begin = %+v, %v; want a B1 login", res, err)
	}
	if err := NewControlClient(strings.TrimPrefix(c.base, "http://"), strings.Repeat("0", 64)).SetClients(t.Context(), json.RawMessage(`{}`)); err == nil {
		t.Error("a wrong token was accepted")
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	var types []string
	for e := range h.Events() {
		types = append(types, e.Type)
	}
	if !slices.Equal(types, []string{"control", "reconcile", "ready"}) {
		t.Errorf("events %v; want control, reconcile, ready", types)
	}
	if reqs := records(t, log, spiketest.RecordRequest); len(reqs) != 1 || reqs[0].Detail != "POST /control/v1/oauth/begin" {
		t.Errorf("requests %+v; want the begin alone (the wrong token's refused first)", reqs)
	}
}

// TestHubproc_SdGateExitIsRomanianPrompt (P02, P17): a hub that ends at
// ipcam-hub's SD gate (exit 4) gives ErrSdCardUnconfirmed, whose text tells
// the owner in Romanian what to run; no hub is left running and hub.pid is
// gone.
func TestHubproc_SdGateExitIsRomanianPrompt(t *testing.T) {
	hub, log := spiketest.InstallFakeHub(t, t.TempDir())
	t.Setenv(spiketest.FakeStorageEnv, "sd")
	cfg := testConfig(t, t.TempDir(), hub, false)
	h, err := Start(t.Context(), cfg)
	if !errors.Is(err, ErrSdCardUnconfirmed) || h != nil {
		t.Fatalf("Start = %v, %v; want ErrSdCardUnconfirmed", h, err)
	}
	if err.Error() != "Hub-ul nu pornește: cardul SD nu este confirmat. Rulați sudo ./uploadspike setup și scrieți DA." {
		t.Errorf("the text: %q", err)
	}
	if len(records(t, log, spiketest.RecordSdGate)) != 1 {
		t.Error("the stand-in did not stop at its SD gate")
	}
	if _, err := os.Stat(cfg.PIDFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("hub.pid after the refusal: %v; want none", err)
	}
	// Confirmed, the same card starts.
	cfg = testConfig(t, t.TempDir(), hub, true)
	h, err = Start(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = h.Stop(t.Context())
	if s := records(t, log, spiketest.RecordStart); len(s) != 2 || !s[1].SdCardConfirmed {
		t.Errorf("starts %+v; want the confirmed one second", s)
	}
}

// TestHubproc_WaitGone: a recorded hub that still runs is waited for, then
// killed after the grace; a recorded pid that a new process took, or a
// missing file, is gone at once.
func TestHubproc_WaitGone(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "hub.pid")
	if err := WaitGone(t.Context(), pidFile, time.Second); err != nil {
		t.Errorf("no hub.pid: %v", err)
	}
	t.Setenv(spiketest.FakeHubEnv, "1")
	cmd := exec.Command(os.Args[0], "run", "--config", writeRunJSON(t), "--stop-on-stdin-eof") //nolint:gosec // the test binary as the stand-in
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill() })
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	if err := writePIDFile(pidFile, cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := WaitGone(t.Context(), pidFile, 700*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < 700*time.Millisecond {
		t.Errorf("WaitGone returned after %v, before its grace", took)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the recorded hub runs after WaitGone")
	}
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("hub.pid after WaitGone: %v; want it removed", err)
	}
	// A pid of another start: gone at once.
	if err := os.WriteFile(pidFile, []byte(`{"pid":`+strings.TrimSpace(itoa(os.Getpid()))+`,"procStartMs":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := WaitGone(ctx, pidFile, time.Hour); err != nil {
		t.Errorf("a pid of another start: %v", err)
	}
}

// writeRunJSON writes a run.json for the stand-in.
func writeRunJSON(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "run.json")
	if err := os.WriteFile(p, []byte(`{"sdCardConfirmed":true,"jobs":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
