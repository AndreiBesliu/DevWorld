package main

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// TestOnvifsim_ReadyCommandsAndCounts: onvifsim prints its cameras' device
// service addresses, answers each command with one line, refuses an unknown
// one, and exits 0 at the end of its stdin.
func TestOnvifsim_ReadyCommandsAndCounts(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"-user", "u", "-pass", "p", "-cam", "Hikvision/A", "-cam", "Dahua/B"}, inR, outW, io.Discard)
	}()
	lines := bufio.NewScanner(outR)
	next := func() map[string]any {
		t.Helper()
		if !lines.Scan() {
			t.Fatal("no line")
		}
		var m map[string]any
		if err := json.Unmarshal(lines.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	ready := next()
	cams, _ := ready["cameras"].([]any)
	if ready["type"] != "ready" || len(cams) != 2 {
		t.Fatalf("ready = %v", ready)
	}
	if first, _ := cams[0].(map[string]any); !strings.HasSuffix(first["xaddr"].(string), "/onvif/device_service") { //nolint:forcetypeassert // the test fails on a panic too
		t.Errorf("xaddr = %v", first["xaddr"])
	}
	for _, tc := range []struct{ cmd, want string }{
		{"emit 0 1", "ok"}, {"silent 1 1", "ok"}, {"expire 0", "ok"}, {"counts", "counts"}, {"bogus 0", "error"}, {"emit 9 1", "error"},
	} {
		if _, err := io.WriteString(inW, tc.cmd+"\n"); err != nil {
			t.Fatal(err)
		}
		if got := next()["type"]; got != tc.want {
			t.Errorf("%q: %v, want %s", tc.cmd, got, tc.want)
		}
	}
	_ = inW.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("onvifsim did not end with its stdin")
	}
	if code := run(nil, strings.NewReader(""), io.Discard, io.Discard); code != 2 {
		t.Errorf("no camera: exit %d, want 2", code)
	}
}
