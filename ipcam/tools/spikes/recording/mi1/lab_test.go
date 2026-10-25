package mi1

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

// TestMI1_LabKill9Smoke (R03, C16): the lab kill9 driver's smoke on
// loopback, with the real chain: reccontroller -local serving the media,
// ipcam-hub with the MediaMTX fork, and two smoke runs (an encrypted one
// and a clear one) with the smoke windows, a prompt 20 to 40 s after the
// first beacon. The controller writes each run's envelope; each counts,
// reconciled without intervention, HUB-04 clear, and loses less than 2 s.
func TestMI1_LabKill9Smoke(t *testing.T) {
	mediamtx := required(t, "MEDIAMTX_BIN", "RECSPIKE_REQUIRE_MEDIAMTX")
	media := required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	bin := t.TempDir()
	hub := buildBinary(t, bin, "ipcam-hub", "github.com/AndreiBesliu/DevWorld/ipcam/hub/cmd/ipcam-hub")
	ctlBin := buildBinary(t, bin, "reccontroller", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cmd/reccontroller")

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	ctl := exec.CommandContext(ctx, ctlBin, "-local", "-media", media) //nolint:gosec // the test's build
	stdout, err := ctl.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	ctl.Stderr = os.Stderr
	if err := ctl.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ctl.Process.Kill()
		_ = ctl.Wait()
	})
	screen := &syncBuffer{}
	listening := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stdout)
		once := false
		for sc.Scan() {
			_, _ = screen.Write([]byte(sc.Text() + "\n"))
			if !once && strings.HasPrefix(sc.Text(), "reccontroller rulează") {
				once = true
				close(listening)
			}
		}
	}()
	select {
	case <-listening:
	case <-time.After(time.Minute):
		t.Fatalf("reccontroller did not start:\n%s", screen.String())
	}

	dir := t.TempDir()
	out := &syncBuffer{}
	d := &Driver{
		Controller: "127.0.0.1:7081",
		Hub:        hub,
		MediaMTX:   mediamtx,
		Results:    filepath.Join(dir, "results"),
		Work:       filepath.Join(dir, "work"),
		Host:       LabHost(runtime.GOOS),
		Out:        out,
		NTP:        func(context.Context) (bool, error) { return true, nil },
	}
	set := LabSet(runtime.GOOS, "kill9")
	if err := d.RunSet(ctx, SetOptions{Set: set, Mode: ModeLab, Kind: "kill9", Shard: 1, Smoke: true}); err != nil {
		t.Fatalf("RunSet: %v\nrecspike:\n%s\nreccontroller:\n%s", err, out.String(), screen.String())
	}

	// A smoke run's envelopes are under its own results set, apart from
	// the set's counted ones.
	smokeSet := ResultsSet(set, true)
	files, err := filepath.Glob(filepath.Join(bin, "results", "MI1", smokeSet, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	counted := 0
	for _, f := range files {
		e, err := results.Read(f)
		if err != nil {
			t.Fatalf("envelope %s: %v", filepath.Base(f), err)
		}
		if len(e.Runs) != 1 || e.Host != d.Host.Pseudonym || e.Set != smokeSet {
			t.Errorf("envelope %s: %+v", filepath.Base(f), e)
			continue
		}
		r := e.Runs[0]
		if !Counts(r) {
			t.Logf("run %s does not count (%s): redone", r.RunID, r.InvalidReason)
			continue
		}
		counted++
		if !r.Reconciled || !r.Hub04Clear || r.Unreadable != 0 || r.TornParts != 0 || len(r.Streams) == 0 || r.ResumeMs == nil {
			t.Errorf("run %s: %+v", r.RunID, r)
			continue
		}
		t.Logf("run %s (%s): loss %d ms, resume %d ms, pacing p99 %d ms", r.RunID, encryptionText(r.Encrypted), r.LossMs, *r.ResumeMs, r.PacingP99Ms)
		for _, s := range r.Streams {
			if s.LossMs >= 2000 || s.LossMs < 0 {
				t.Errorf("run %s: %s lost %d ms, want under 2 s", r.RunID, s.Stream, s.LossMs)
			}
		}
	}
	if counted != 2 {
		t.Errorf("%d runs count, want the 2 smoke runs\nrecspike:\n%s\nreccontroller:\n%s", counted, out.String(), screen.String())
	}
}
