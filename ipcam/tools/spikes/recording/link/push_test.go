package link

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// TestPush_AnswersTheSessionThenThePull (G15): the host says hello as
// host; a running reccontroller's session gets a goodbye, and its pull
// gets the results folder; a link that ends before the pull is a
// NoPullError.
func TestPush_AnswersTheSessionThenThePull(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller, host := pair(t)
	pulled := t.TempDir()
	done := make(chan error, 1)
	go func() {
		m, err := controller.Receive()
		if hello, ok := m.(Hello); err != nil || !ok || hello != (Hello{Role: "host", Host: "pi", Version: Version}) {
			done <- errors.Join(err, errors.New("no hello from the host"))
			return
		}
		if err := controller.Send(Session{Streams: []string{"cam01_main"}}); err != nil {
			done <- err
			return
		}
		if m, err := controller.Receive(); err != nil || m.Type() != (Bye{}).Type() {
			done <- errors.Join(err, errors.New("no goodbye after the session"))
			return
		}
		done <- controller.PullResults(pulled)
	}()
	if err := Push(t.Context(), host, "pi", dir); err != nil {
		t.Fatalf("Push = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pulled, "a.json")); err != nil {
		t.Errorf("the controller did not get the results: %v", err)
	}

	controller, host = pair(t)
	go func() {
		_, _ = controller.Receive()
		_ = controller.Close()
	}()
	var noPull *NoPullError
	if err := Push(t.Context(), host, "pi", dir); !errors.As(err, &noPull) {
		t.Errorf("Push on a link that closed before the pull = %v, want a NoPullError", err)
	}
}

// TestPushTo_CtrlCDuringTheDialIsTheCancel (Task 13 review, M1): the end of
// ctx while PushTo dials the controller is ctx's error, which the owner
// reads as their own Ctrl+C, not a connection failure; a dial that fails
// by itself is a DialError.
func TestPushTo_CtrlCDuringTheDialIsTheCancel(t *testing.T) {
	system := dial
	t.Cleanup(func() { dial = system })
	dialing := make(chan struct{})
	dial = func(ctx context.Context, _ string) (*Conn, error) {
		close(dialing)
		<-ctx.Done()
		return nil, errors.New("dial tcp4 192.0.2.1:7081: operation was canceled")
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- PushTo(ctx, "192.0.2.1", "pi", t.TempDir()) }()
	<-dialing
	cancel()
	var dialErr *DialError
	if err := <-done; !errors.Is(err, context.Canceled) || errors.As(err, &dialErr) {
		t.Errorf("PushTo cancelled during the dial = %v, want context.Canceled", err)
	}

	dial = system
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	_ = ln.Close()
	if err := PushTo(t.Context(), closed, "pi", t.TempDir()); !errors.As(err, &dialErr) {
		t.Errorf("PushTo to a closed port = %v, want a DialError", err)
	}
}
