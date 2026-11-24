package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/recorder"
)

// TestHookCommand_PostsTheSegment (G05): "ipcam-hub hook segment-complete"
// posts the segment that MediaMTX describes in its environment to the
// address in IPCAM_HOOK_ADDR, with the token as a bearer token; the camera
// and profile come from MTX_PATH, split at its "/". A refusal is exit code 1.
func TestHookCommand_PostsTheSegment(t *testing.T) {
	got := make(chan recorder.HookRequest, 2)
	var status atomic.Int32
	status.Store(http.StatusNoContent)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:gosec // a test server
		if r.Method != http.MethodPost || r.URL.Path != recorder.HookPath || r.Header.Get("Authorization") != "Bearer the-token" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var req recorder.HookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		got <- req
		w.WriteHeader(int(status.Load()))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	hook := func() error {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "hook", "segment-complete") //nolint:gosec // the test binary itself
		cmd.Env = append(os.Environ(), mainEnv+"=1",
			recorder.HookAddrEnv+"="+ln.Addr().String(), recorder.HookTokenEnv+"=the-token",
			"MTX_PATH=cam7/sub", "MTX_SEGMENT_PATH=/data/rec/cam7/sub/2026-10-20/100000_000123.mp4.age",
			"MTX_SEGMENT_START=1792483200000", "MTX_SEGMENT_DURATION=300.25")
		return cmd.Run()
	}
	if err := hook(); err != nil {
		t.Fatalf("the hook command: %v", err)
	}
	want := recorder.HookRequest{Path: "/data/rec/cam7/sub/2026-10-20/100000_000123.mp4.age", StartMs: 1792483200000, DurationMs: 300250,
		Cam: "cam7", Profile: "sub", Encrypted: true}
	if req := <-got; req != want {
		t.Errorf("the hook posted %+v, want %+v", req, want)
	}

	status.Store(http.StatusUnauthorized)
	err = hook()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Errorf("a refused hook ended with %v, want exit code 1", err)
	}
}
