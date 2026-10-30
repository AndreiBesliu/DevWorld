package link

import (
	"errors"
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
