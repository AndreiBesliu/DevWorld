// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestElevatedCopy_WaitsForEnter (Task 12 review): the copy that UAC
// relaunches runs in a console window of its own, which closes when it
// exits. It prints its last line, here an error, then waits for Enter, so
// that the owner can read it; a run without the marker does not wait.
func TestElevatedCopy_WaitsForEnter(t *testing.T) {
	if got := elevatedArgs([]string{"install"}); !slices.Equal(got, []string{relaunchMarker, "install"}) {
		t.Fatalf("elevatedArgs = %q", got)
	}
	e, out := unitEnv(t)
	in, feed := io.Pipe()
	e.in = in
	done := make(chan int, 1)
	go func() { done <- runMain(context.Background(), e, elevatedArgs([]string{"nosuch"})) }()
	until(t, 5*time.Second, "the pause line", func() bool { return strings.Contains(out.String(), msgPressEnterToClose) })
	select {
	case <-done:
		t.Fatal("the elevated copy ended before Enter")
	case <-time.After(200 * time.Millisecond):
	}
	errLine := fmt.Sprintf(msgUnknownCommand, "nosuch")
	if i, j := strings.Index(out.String(), errLine), strings.Index(out.String(), msgPressEnterToClose); i < 0 || i > j {
		t.Errorf("the error line must come before the pause: %q", out)
	}
	_, _ = feed.Write([]byte("\n"))
	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("exit code %d; want 1", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the elevated copy did not end after Enter")
	}

	e, out = unitEnv(t)
	e.in = strings.NewReader("")
	if code := runMain(context.Background(), e, []string{"nosuch"}); code != 1 || strings.Contains(out.String(), msgPressEnterToClose) {
		t.Errorf("a direct run: code %d, output %q", code, out)
	}
}
