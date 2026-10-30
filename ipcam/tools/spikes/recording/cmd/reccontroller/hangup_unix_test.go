//go:build unix

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

// TestMain_HangupKeepsServing (report-1): a reccontroller started from an
// ssh window gets SIGHUP when that window's far end dies (the PC under test
// in a bugcheck or a power cut), and its screen then fails (EPIPE, EIO).
// It keeps serving through both: a host that connects afterwards gets its
// session, and the line that the dead screen lost is in reccontroller.log.
func TestMain_HangupKeepsServing(t *testing.T) {
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	screen, screenW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-local", "-media", media) //nolint:gosec // the test's own binary
	cmd.Env = append(os.Environ(), asMainEnv+"="+dir)
	cmd.Stdout = screenW
	var stderr syncBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = screenW.Close()
	exited := make(chan struct{})
	var waitErr error // set once exited is closed
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
	})

	port := ""
	lines := bufio.NewScanner(screen)
	portRE := regexp.MustCompile(`legătură (\d+),`)
	for port == "" && lines.Scan() {
		if m := portRE.FindStringSubmatch(lines.Text()); m != nil {
			port = m[1]
		}
	}
	if port == "" {
		t.Fatalf("reccontroller did not start; stderr:\n%s", stderr.String())
	}

	// The ssh window's far end dies: the hangup comes, and the screen is
	// gone, so every later line fails with EPIPE.
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	_ = screen.Close()
	select {
	case <-exited:
		t.Fatalf("reccontroller ended after SIGHUP: %v; stderr:\n%s", waitErr, stderr.String())
	case <-time.After(500 * time.Millisecond):
	}

	conn, err := link.Dial(t.Context(), "127.0.0.1:"+port)
	if err != nil {
		t.Fatalf("after SIGHUP: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.Send(link.Hello{Role: "host", Host: "pc", Version: link.Version}); err != nil {
		t.Fatal(err)
	}
	if m, err := conn.Receive(); err != nil {
		t.Fatalf("after SIGHUP and a dead screen, no session: %v; stderr:\n%s", err, stderr.String())
	} else if _, ok := m.(link.Session); !ok {
		t.Fatalf("after SIGHUP, the host got %s, want its session", m.Type())
	}
	waitFor(t, "the host's line in reccontroller.log", func() bool {
		data, err := os.ReadFile(filepath.Join(dir, "reccontroller.log")) //nolint:gosec // the test's folder
		return err == nil && strings.Contains(string(data), "Gazda „pc” s-a conectat.")
	})

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
		if err := waitErr; err != nil {
			t.Errorf("reccontroller stopped with %v, want a clean stop; stderr:\n%s", err, stderr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("reccontroller did not stop on SIGTERM")
	}
}
