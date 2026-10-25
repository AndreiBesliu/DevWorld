package hubproc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

// The test binary stands in for ipcam-hub when fakeEnv is set (TestMain):
// "ipcam-hub run --config <run.json>" prints the startup's reconcile
// events (seq 2 before seq 1, as they may come out of order), ready, a
// `recording` event of a camera that run.json does not have, then one
// `recording` event per job of run.json after the delays in fakeDelays
// (milliseconds, in job order), and a verify event. It exits 0 on SIGTERM
// or Ctrl+Break, and by itself with code 1 after fakeExitAfter
// milliseconds, when that is set.
const (
	fakeEnv       = "HUBPROC_FAKE_HUB"
	fakeDelays    = "HUBPROC_FAKE_DELAYS"
	fakeExitAfter = "HUBPROC_FAKE_EXIT_AFTER_MS"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) == "1" {
		os.Exit(fakeHub(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeHub(args []string) int {
	if len(args) != 3 || args[0] != "run" || args[1] != "--config" {
		fmt.Fprintln(os.Stderr, "fake ipcam-hub: usage: run --config <run.json>")
		return 1
	}
	cfg, err := ReadConfig(args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake ipcam-hub:", err)
		return 1
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	var mu sync.Mutex
	emit := func(fields map[string]any) {
		fields["atMs"] = time.Now().UnixMilli()
		b, _ := json.Marshal(fields)
		mu.Lock()
		defer mu.Unlock()
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
	emit(map[string]any{"type": "reconcile", "seq": 2, "final": true, "aborted": false, "catalogMismatches": 0})
	emit(map[string]any{"type": "reconcile", "seq": 1, "final": false, "aborted": false, "catalogMismatches": 0})
	emit(map[string]any{"type": "ready", "jobs": []any{}})
	emit(map[string]any{"type": "recording", "cam": "cam77", "profile": "main"})
	start := time.Now()
	var delays []int
	for _, d := range strings.Split(os.Getenv(fakeDelays), ",") {
		if n, err := strconv.Atoi(d); err == nil {
			delays = append(delays, n)
		}
	}
	go func() {
		for i, j := range cfg.Jobs {
			if i < len(delays) {
				time.Sleep(time.Until(start.Add(time.Duration(delays[i]) * time.Millisecond)))
				emit(map[string]any{"type": "recording", "cam": j.Cam, "profile": j.Profile})
			}
		}
		emit(map[string]any{"type": "verify", "unreadable": 0, "tornParts": 0, "catalogMismatches": 0})
	}()
	exitAfter := make(<-chan time.Time)
	if ms, err := strconv.Atoi(os.Getenv(fakeExitAfter)); err == nil {
		exitAfter = time.After(time.Duration(ms) * time.Millisecond)
	}
	select {
	case <-stop:
		return 0
	case <-exitAfter:
		return 1
	}
}

// writeRunJSON writes a run.json of two jobs and returns its path.
func writeRunJSON(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.json")
	off := false
	cfg := Config{DataDir: t.TempDir(), Jobs: []Job{
		{Cam: "cam01", Profile: "main", RestreamURL: "rtsp://127.0.0.1:8554/cam01_main", Recipients: []string{"age1a", "age1b"}},
		{Cam: "cam99", Profile: "main", RestreamURL: "rtsp://127.0.0.1:8554/cam99_main", Recipients: []string{"age1a", "age1b"}, PadLowBitrate: &off},
	}}
	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	return path
}

// startFake starts the test binary as ipcam-hub on a two-job run.json.
func startFake(t *testing.T, delays string, beacon *Beacon) *Hub {
	t.Helper()
	t.Setenv(fakeEnv, "1")
	t.Setenv(fakeDelays, delays)
	h, err := Start(Options{Binary: os.Args[0], Config: writeRunJSON(t), Beacon: beacon})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if _, err := h.Kill(); err != nil && !errors.Is(err, ErrExited) {
			t.Errorf("Kill at cleanup: %v", err)
		}
	})
	return h
}

func ctxFor(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), d)
	t.Cleanup(cancel)
	return ctx
}

func TestHubproc_ConfigRoundTrip(t *testing.T) {
	path := writeRunJSON(t)
	cfg, err := ReadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Jobs) != 2 || cfg.Jobs[1].PadLowBitrate == nil || *cfg.Jobs[1].PadLowBitrate || cfg.Jobs[0].PadLowBitrate != nil {
		t.Errorf("ReadConfig = %+v", cfg)
	}
	clearJobs := filepath.Join(t.TempDir(), "clear.json")
	if err := WriteConfig(clearJobs, Config{DataDir: "d", Jobs: []Job{{Cam: "cam01", Profile: "main", RestreamURL: "rtsp://x/cam01_main"}}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(clearJobs) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	// ipcam-hub records a job in clear only when recipients is null or
	// absent, and takes an absent padLowBitrate as true (O17).
	var raw struct {
		MediaMTX *string          `json:"mediamtx"`
		Jobs     []map[string]any `json:"jobs"`
	}
	if err := json.Unmarshal(b, &raw); err != nil || len(raw.Jobs) != 1 {
		t.Fatalf("run.json = %s (%v)", b, err)
	}
	recipients, hasRecipients := raw.Jobs[0]["recipients"]
	if _, hasPad := raw.Jobs[0]["padLowBitrate"]; !hasRecipients || recipients != nil || hasPad || raw.MediaMTX != nil {
		t.Errorf("run.json = %s, want recipients null and no padLowBitrate or mediamtx", b)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(clearJobs); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("run.json mode = %v (%v), want 0600", info.Mode().Perm(), err)
		}
	}
}

// beacons receives the beacons of runID on a loopback UDP socket.
type beacons struct {
	conn net.PacketConn
	mu   sync.Mutex
	n    int
	seqs []uint64
}

func listenBeacons(t *testing.T, runID string) *beacons {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	b := &beacons{conn: conn}
	go func() {
		buf := make([]byte, 64)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if run, seq, ok := link.ParseBeacon(buf[:n]); ok && run == link.RunHash(runID) {
				b.mu.Lock()
				b.n++
				b.seqs = append(b.seqs, seq)
				b.mu.Unlock()
			}
		}
	}()
	return b
}

func (b *beacons) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.n
}

// waitFor polls cond for up to 10 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// quiet reports whether no beacon arrives for d after the ones in flight.
func (b *beacons) quiet(d time.Duration) bool {
	time.Sleep(100 * time.Millisecond) // beacons sent before the stop and still in flight
	before := b.count()
	time.Sleep(d)
	return b.count() == before
}

// TestHubproc_KillStopsBeaconOnChildExit (C16): the beacon starts at
// ipcam-hub's ready, at 50 ms intervals, and stops for good when the child
// exits, killed or by itself; a restart never beacons again under the run.
func TestHubproc_KillStopsBeaconOnChildExit(t *testing.T) {
	t.Run("killed", func(t *testing.T) {
		const runID = "pc-kill9-01"
		b := listenBeacons(t, runID)
		h := startFake(t, "0,0", &Beacon{Addr: b.conn.LocalAddr().String(), RunID: runID})
		waitFor(t, "10 beacons", func() bool { return b.count() >= 10 })
		killedAt, err := h.Kill()
		if err != nil {
			t.Fatalf("Kill: %v", err)
		}
		select {
		case <-h.Exited():
		default:
			t.Error("Kill returned before the child exited")
		}
		if time.Since(killedAt) > 5*time.Second || killedAt.IsZero() {
			t.Errorf("killedAt = %v", killedAt)
		}
		if !b.quiet(500 * time.Millisecond) {
			t.Error("beacons after the kill")
		}
		b.mu.Lock()
		for i := 1; i < len(b.seqs); i++ {
			if b.seqs[i] <= b.seqs[i-1] {
				t.Errorf("beacon %d has seq %d after %d", i, b.seqs[i], b.seqs[i-1])
			}
		}
		b.mu.Unlock()

		again, err := h.Restart()
		if err != nil {
			t.Fatalf("Restart: %v", err)
		}
		t.Cleanup(func() { _, _ = again.Kill() })
		if _, err := again.WaitEvent(ctxFor(t, 10*time.Second), "ready", nil); err != nil {
			t.Fatalf("restarted child: %v", err)
		}
		if !b.quiet(300 * time.Millisecond) {
			t.Error("the restarted child beacons under the same run")
		}
	})

	t.Run("exited by itself", func(t *testing.T) {
		const runID = "pc-kill9-02"
		b := listenBeacons(t, runID)
		t.Setenv(fakeExitAfter, "600")
		h := startFake(t, "0,0", &Beacon{Addr: b.conn.LocalAddr().String(), RunID: runID})
		select {
		case <-h.Exited():
		case <-time.After(10 * time.Second):
			t.Fatal("the child did not exit")
		}
		if got := h.ExitCode(); got != 1 {
			t.Errorf("ExitCode = %d, want 1", got)
		}
		if n := b.count(); n < 5 {
			t.Errorf("%d beacons before the exit, want about 12", n)
		}
		if !b.quiet(500 * time.Millisecond) {
			t.Error("beacons after the child exited")
		}
		if _, err := h.Kill(); !errors.Is(err, ErrExited) {
			t.Errorf("Kill after the exit = %v, want ErrExited", err)
		}
	})
}

// TestHubproc_ResumeMsFromRecordingEvents (C07, G13): ResumeMs runs on the
// monotonic clock from the kill to the restarted child's `recording` event
// of the last job of run.json; a camera that run.json does not have counts
// for nothing.
func TestHubproc_ResumeMsFromRecordingEvents(t *testing.T) {
	h := startFake(t, "0,0", nil)
	if _, err := h.WaitRecording(ctxFor(t, 10*time.Second)); err != nil {
		t.Fatalf("WaitRecording: %v", err)
	}
	t.Setenv(fakeDelays, "100,400")
	killedAt, err := h.Kill()
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.Restart()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = again.Kill() })
	ms, err := again.ResumeMs(ctxFor(t, 10*time.Second), killedAt)
	if err != nil {
		t.Fatalf("ResumeMs: %v", err)
	}
	if ms < 400 || ms > 5000 {
		t.Errorf("ResumeMs = %d, want the 400 ms of the last job plus the restart", ms)
	}

	t.Run("a job that never records", func(t *testing.T) {
		h := startFake(t, "0", nil) // only the first job records
		if _, err := h.WaitRecording(ctxFor(t, 700*time.Millisecond)); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("WaitRecording = %v, want the context's deadline", err)
		}
	})
	t.Run("the child exits first", func(t *testing.T) {
		t.Setenv(fakeExitAfter, "200")
		h := startFake(t, "0", nil)
		if _, err := h.WaitRecording(ctxFor(t, 10*time.Second)); !errors.Is(err, ErrExited) {
			t.Errorf("WaitRecording = %v, want ErrExited", err)
		}
	})
}

// TestHubproc_WaitEvent: the current reconcile event is the one with the
// highest seq, whatever order they came in; other types are the latest.
func TestHubproc_WaitEvent(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	t.Setenv(fakeEnv, "1")
	t.Setenv(fakeDelays, "0,50")
	h, err := Start(Options{Binary: os.Args[0], Config: writeRunJSON(t), OnEvent: func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, e.Type)
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.Kill() })
	ctx := ctxFor(t, 10*time.Second)
	rec, err := h.WaitEvent(ctx, "reconcile", func(e Event) bool { return e.Final })
	if err != nil {
		t.Fatalf("WaitEvent(reconcile): %v", err)
	}
	if rec.Seq != 2 || !rec.Final || !strings.Contains(string(rec.Line), `"seq":2`) || rec.At.IsZero() {
		t.Errorf("reconcile = %+v, want seq 2, final, with its line", rec)
	}
	if v, err := h.WaitEvent(ctx, "verify", nil); err != nil || v.CatalogMismatches != 0 || v.Type != "verify" {
		t.Errorf("WaitEvent(verify) = %+v, %v", v, err)
	}
	mu.Lock()
	if got := strings.Join(seen, " "); !strings.HasPrefix(got, "reconcile reconcile ready recording") {
		t.Errorf("OnEvent saw %q", got)
	}
	mu.Unlock()

	if err := h.Stop(ctxFor(t, 10*time.Second)); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := h.ExitCode(); got != 0 {
		t.Errorf("ExitCode after Stop = %d, want 0: ipcam-hub stops cleanly on SIGTERM or Ctrl+Break", got)
	}
	if _, err := h.WaitEvent(ctx, "gap", nil); !errors.Is(err, ErrExited) {
		t.Errorf("WaitEvent after the exit = %v, want ErrExited", err)
	}
}
