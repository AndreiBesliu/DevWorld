package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/rtsptest"
)

// The test binary is ipcam-hub too: with mainEnv set, it runs main. The hook
// command that MediaMTX runs inherits the variable, so it is this binary
// with "hook segment-complete".
const mainEnv = "IPCAM_HUB_TEST_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(mainEnv) == "1" {
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// hub is an ipcam-hub process that a test started.
type hub struct {
	cmd    *exec.Cmd
	events chan map[string]any
	stderr *bytes.Buffer
	done   chan struct{}
}

func startHub(t *testing.T, args ...string) *hub {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...) //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), mainEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	h := &hub{cmd: cmd, events: make(chan map[string]any, 1024), stderr: &bytes.Buffer{}, done: make(chan struct{})}
	cmd.Stderr = h.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(h.done)
		defer close(h.events)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			var e map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				e = map[string]any{"type": "not-json", "line": scanner.Text()}
			}
			h.events <- e
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-h.done
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("ipcam-hub stderr:\n%s", h.stderr.String())
		}
	})
	return h
}

// next returns the next event that match accepts, failing after d.
func (h *hub) next(t *testing.T, d time.Duration, what string, match func(map[string]any) bool) map[string]any {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case e, ok := <-h.events:
			if !ok {
				t.Fatalf("ipcam-hub ended before %s", what)
			}
			if e["type"] == "not-json" {
				t.Fatalf("ipcam-hub printed a line that is not JSON: %q", e["line"])
			}
			if _, ok := e["atMs"].(float64); !ok {
				t.Fatalf("event without atMs: %v", e)
			}
			if match(e) {
				return e
			}
		case <-timeout:
			t.Fatalf("after %v: no %s", d, what)
		}
	}
}

func ofType(typ string) func(map[string]any) bool {
	return func(e map[string]any) bool { return e["type"] == typ }
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRun_EmitsReadyThenRecordingThenSegmentClosed: ipcam-hub run with the
// real MediaMTX reconciles, is ready, reports the first part of each stream,
// then each 10 s segment as it closes, and then each closed segment that the
// background verification passed. It exits 0 on SIGTERM.
func TestRun_EmitsReadyThenRecordingThenSegmentClosed(t *testing.T) {
	bin := rtsptest.MediaMTX(t)
	srv := rtsptest.Start(t, rtsptest.MainH264(t))
	riG, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rk, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	identity := filepath.Join(dir, "ri_g.key")
	if err := os.WriteFile(identity, []byte(riG.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	config := filepath.Join(dir, "run.json")
	writeJSON(t, config, map[string]any{
		"dataDir":      dataDir,
		"mediamtx":     bin,
		"identityFile": identity,
		"jobs": []map[string]any{
			{"cam": "cam1", "profile": "main", "restreamUrl": srv.URL("cam1/main"), "segmentSecs": 10,
				"recipients": []string{riG.Recipient().String(), rk.Recipient().String()}},
			{"cam": "cam1", "profile": "sub", "restreamUrl": srv.URL("cam1/sub"), "segmentSecs": 10, "recipients": nil},
		},
	})
	h := startHub(t, "run", "--config", config)

	reconcile := h.next(t, 30*time.Second, "reconcile event", ofType("reconcile"))
	if reconcile["aborted"] != false {
		t.Errorf("reconcile %v", reconcile)
	}
	h.next(t, 30*time.Second, "ready event", ofType("ready"))
	recording := map[string]bool{}
	closed := map[string]string{} // path → stream
	for len(closed) < 2 {
		e := h.next(t, 40*time.Second, "recording and segment_closed events", func(e map[string]any) bool {
			return e["type"] == "recording" || e["type"] == "segment_closed"
		})
		stream := e["cam"].(string) + "/" + e["profile"].(string)
		switch e["type"] {
		case "recording":
			recording[stream] = true
		case "segment_closed":
			if !recording[stream] {
				t.Errorf("segment_closed of %s before its recording event", stream)
			}
			closed[e["path"].(string)] = stream
			if e["encrypted"] != (stream == "cam1/main") {
				t.Errorf("segment_closed %v: encrypted %v", e, e["encrypted"])
			}
		}
	}
	verified := map[string]bool{}
	for len(verified) < 2 {
		e := h.next(t, 20*time.Second, "segment_verified events", ofType("segment_verified"))
		if _, ok := closed[e["path"].(string)]; ok {
			verified[e["path"].(string)] = true
		}
	}
	for path, stream := range closed {
		if !strings.HasPrefix(path, filepath.Join(dataDir, "rec", filepath.FromSlash(stream))) {
			t.Errorf("segment %s of %s is not under the stream's directory", path, stream)
		}
		if stream == "cam1/main" {
			f, err := os.Open(path) //nolint:gosec // a segment of the test
			if err != nil {
				t.Fatal(err)
			}
			r, err := age.Decrypt(f, rk)
			if err != nil {
				t.Errorf("%s does not decrypt with RK: %v", path, err)
			} else if _, err := io.Copy(io.Discard, r); err != nil {
				t.Errorf("%s: %v", path, err)
			}
			_ = f.Close()
		}
	}
	h.next(t, 5*time.Second, "status event", ofType("status"))
	if _, err := os.Stat(filepath.Join(dataDir, "state", "run", "status.json")); err != nil {
		t.Errorf("no status file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "state", "catalog.db")); err != nil {
		t.Errorf("no catalog: %v", err)
	}

	if runtime.GOOS == "windows" {
		return // no SIGTERM to send
	}
	if err := h.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	<-h.done
	if err := h.cmd.Wait(); err != nil {
		t.Errorf("ipcam-hub after SIGTERM: %v, want exit 0", err)
	}
}

// minimalFMP4 is an fMP4 recording with one part of one sample, cut short
// by cut bytes.
func minimalFMP4(t *testing.T, cut int) []byte {
	t.Helper()
	var buf seekablebuffer.Buffer
	init := fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: 90000, Codec: &codecs.H264{
		SPS: []byte{0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02, 0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20},
		PPS: []byte{0x08, 0x06, 0x07, 0x08},
	}}}}
	if err := init.Marshal(&buf); err != nil {
		t.Fatal(err)
	}
	part := fmp4.Part{Tracks: []*fmp4.PartTrack{{ID: 1, Samples: []*fmp4.Sample{{Duration: 4500, Payload: bytes.Repeat([]byte{1}, 5000)}}}}}
	if err := part.Marshal(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()[:buf.Len()-cut]
}

// TestRun_ExitCode3WhenReconcileAborts (HUB-09): when more than half of the
// segments would be lost, ipcam-hub reports the aborted reconcile, changes
// nothing, starts no MediaMTX and exits 3.
func TestRun_ExitCode3WhenReconcileAborts(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	files := map[string][]byte{}
	for i, cam := range []string{"cam1", "cam2", "cam3", "cam4", "cam5"} {
		data := bytes.Repeat([]byte("not an fMP4 file "), 300)
		if i < 2 {
			data = minimalFMP4(t, 100)
		}
		path := filepath.Join(dataDir, "rec", cam, "main", "2026-10-20", "100000_000000.mp4.part")
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		files[path] = data
	}
	config := filepath.Join(dir, "run.json")
	writeJSON(t, config, map[string]any{"dataDir": dataDir, "mediamtx": filepath.Join(dir, "no-mediamtx"), "jobs": []any{}})
	h := startHub(t, "run", "--config", config)

	e := h.next(t, 30*time.Second, "reconcile event", ofType("reconcile"))
	if e["aborted"] != true {
		t.Errorf("reconcile %v, want aborted", e)
	}
	<-h.done
	err := h.cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Errorf("ipcam-hub ended with %v, want exit code 3", err)
	}
	for path, data := range files {
		got, err := os.ReadFile(path) //nolint:gosec // a file of the test
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("%s changed (%v)", path, err)
		}
	}
}

// TestRun_ExitCode1OnBadConfig: a configuration that cannot run is an error
// event and exit code 1.
func TestRun_ExitCode1OnBadConfig(t *testing.T) {
	dir := t.TempDir()
	for name, cfg := range map[string]any{
		"no data directory": map[string]any{"jobs": []any{}},
		"an unknown key":    map[string]any{"dataDir": filepath.Join(dir, "d1"), "jobz": []any{}},
		"one recipient":     map[string]any{"dataDir": filepath.Join(dir, "d2"), "jobs": []map[string]any{{"cam": "c", "profile": "main", "restreamUrl": "rtsp://127.0.0.1:1/c", "recipients": []string{"age1x"}}}},
	} {
		config := filepath.Join(dir, "run.json")
		writeJSON(t, config, cfg)
		h := startHub(t, "run", "--config", config)
		e := h.next(t, 10*time.Second, "error event", ofType("error"))
		if strings.Contains(e["message"].(string), "AGE-SECRET-KEY-") {
			t.Errorf("%s: the error quotes a key", name)
		}
		<-h.done
		err := h.cmd.Wait()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Errorf("%s: ipcam-hub ended with %v, want exit code 1", name, err)
		}
	}
}
