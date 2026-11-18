//go:build unix

package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

// TestMain_HangupKeepsWorking (report-1): a recspike started from an ssh
// window gets SIGHUP when that window's far end dies, and its screen then
// fails (EPIPE, EIO). It keeps working through both: here `recspike push`,
// waiting for the controller as the hangup comes, still sends its results
// once asked, prints its last line into the dead screen, and ends cleanly.
func TestMain_HangupKeepsWorking(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "results", "MI2"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "results", "MI2", "x.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	screen, screenW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "push", "--controller", ln.Addr().String()) //nolint:gosec // the test's own binary
	cmd.Env = append(os.Environ(), asMainEnv+"="+filepath.Join(dir, "recspike"))
	cmd.Stdout = screenW
	var stderr strings.Builder
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

	nc, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	conn := link.NewConn(nc)
	defer func() { _ = conn.Close() }()
	if m, err := conn.Receive(); err != nil {
		t.Fatal(err)
	} else if _, ok := m.(link.Hello); !ok {
		t.Fatalf("recspike sent %s first, want hello", m.Type())
	}

	// The ssh window's far end dies: the hangup comes, and the screen is
	// gone, so recspike's last line fails with EPIPE.
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	_ = screen.Close()
	select {
	case <-exited:
		t.Fatalf("recspike ended after SIGHUP: %v; stderr:\n%s", waitErr, stderr.String())
	case <-time.After(500 * time.Millisecond):
	}

	if err := conn.Send(link.PullResults{}); err != nil {
		t.Fatal(err)
	}
	m, err := conn.Receive()
	if err != nil {
		t.Fatalf("after SIGHUP, no results: %v", err)
	}
	tarball, ok := m.(link.ResultsTar)
	if !ok {
		t.Fatalf("after SIGHUP, recspike sent %s, want its results", m.Type())
	}
	got := t.TempDir()
	if err := link.Untar(got, tarball.Data); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(got, "MI2", "x.json")); err != nil {
		t.Errorf("the results that came: %v", err)
	}
	select {
	case <-exited:
		if err := waitErr; err != nil {
			t.Errorf("recspike ended with %v after a dead screen, want a clean end; stderr:\n%s", err, stderr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("recspike did not end after sending its results")
	}
}
