package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal pair: the master, which plays the owner's
// keyboard, and the slave, uploadspike's terminal.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal here: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := unix.IoctlSetPointerInt(int(m.Fd()), unix.TIOCSPTLCK, 0); err != nil { //nolint:gosec // a file descriptor
		t.Skipf("unlockpt: %v", err)
	}
	n, err := unix.IoctlGetInt(int(m.Fd()), unix.TIOCGPTN) //nolint:gosec // a file descriptor
	if err != nil {
		t.Skipf("ptsname: %v", err)
	}
	s, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("the slave: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return m, s
}

// echoOn reports whether the terminal f echoes what is typed.
func echoOn(t *testing.T, f *os.File) bool {
	t.Helper()
	tio, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS) //nolint:gosec // a file descriptor
	if err != nil {
		t.Fatal(err)
	}
	return tio.Lflag&unix.ECHO != 0
}

// TestSetupTerminal_EchoRestoredWhenCut (Task 9B review): a handover, or
// Ctrl-C, that ends setup while it asks a secret without echo gives the
// owner's terminal its echo back.
func TestSetupTerminal_EchoRestoredWhenCut(t *testing.T) {
	for _, cut := range []string{"handover", "signal"} {
		t.Run(cut, func(t *testing.T) {
			master, slave := openPTY(t)
			if !echoOn(t, slave) {
				t.Fatal("a new terminal does not echo")
			}
			e, _ := unitEnv(t)
			e.in = slave
			h := &holder{e: e, handover: make(chan struct{})}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- h.setupTerminal(ctx) }()
			if _, err := master.WriteString("android-client-0001.apps.example\ndesktop-client-0001.apps.example\n"); err != nil {
				t.Fatal(err)
			}
			until(t, 10*time.Second, "the secret's prompt without echo", func() bool { return !echoOn(t, slave) })
			if cut == "handover" {
				h.handedOverFor(h)
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, errHandedOver) && !errors.Is(err, context.Canceled) {
					t.Fatalf("setupTerminal: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("setupTerminal did not end")
			}
			if !echoOn(t, slave) {
				t.Error("the terminal does not echo after the cut")
			}
		})
	}
}
