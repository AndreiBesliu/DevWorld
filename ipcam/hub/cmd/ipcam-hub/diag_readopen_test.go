package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
)

// diagFixture is a data directory as a run of ipcam-hub leaves it: its
// catalog, which the fixture keeps open to write rows, and, with uploads,
// state/hub_id, beside the run.json that names it.
type diagFixture struct {
	dataDir, config, hubID string
	cat                    *catalog.DB
}

// newDiagFixture makes a data directory with a catalog, and state/hub_id
// when hubID is set, and a run.json of cam1/sub with uploads when it is not
// nil.
func newDiagFixture(t *testing.T, uploads map[string]any, hubID string) *diagFixture {
	t.Helper()
	dir := t.TempDir()
	f := &diagFixture{dataDir: filepath.Join(dir, "data"), hubID: hubID}
	stateDir := filepath.Join(f.dataDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Open(t.Context(), filepath.Join(stateDir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	f.cat = cat
	if hubID != "" {
		if err := os.WriteFile(filepath.Join(stateDir, "hub_id"), []byte(hubID+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.config = uploadConfig(t, f.dataDir, uploads)
	return f
}

// runDiag runs ipcam-hub diag with args and returns its exit code, its stdout's
// lines as JSON objects and its stderr.
func runDiag(t *testing.T, args ...string) (int, []map[string]any, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := diagCommand(args, strings.NewReader(""), &stdout, &stderr)
	var lines []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("diag %v printed a line that is not JSON: %q", args, line)
		}
		lines = append(lines, m)
	}
	return code, lines, stderr.String()
}

// TestDiagReadopen_JSONShape (P14): diag readopen prints one line, exit 0:
// the open segment's read, or ok false with no-open-segment or unreadable;
// it prints no path. A usage error exits 1.
func TestDiagReadopen_JSONShape(t *testing.T) {
	f := newDiagFixture(t, nil, "")
	args := []string{"readopen", "--config", f.config, "--cam", testCam, "--profile", testProfile}
	check := func(want map[string]any) {
		t.Helper()
		code, lines, stderr := runDiag(t, args...)
		if code != exitOK || len(lines) != 1 {
			t.Fatalf("diag readopen = %d, %v (%s); want one line, exit 0", code, lines, stderr)
		}
		got := lines[0]
		for k, v := range want {
			if got[k] != v {
				t.Errorf("diag readopen %v: %s is %v, want %v", got, k, got[k], v)
			}
		}
		line, _ := json.Marshal(got)
		if strings.Contains(string(line), f.dataDir) || strings.Contains(string(line), string(filepath.Separator)+"rec") {
			t.Errorf("diag readopen printed a path: %s", line)
		}
	}
	check(map[string]any{"schema": "ipcam.diag.readopen/1", "ok": false, "error": "no-open-segment"})

	init, parts, err := fakeRecording(5)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.dataDir, "rec", testCam, testProfile, time.Now().UTC().Format(time.DateOnly))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	open := filepath.Join(dir, "235959_000000.mp4.part")
	data := slices.Concat(init, parts[0], parts[1], parts[2], parts[3], parts[4][:30])
	if err := os.WriteFile(open, data, 0o600); err != nil {
		t.Fatal(err)
	}
	code, lines, _ := runDiag(t, args...)
	if code != exitOK || len(lines) != 1 {
		t.Fatalf("diag readopen = %d, %v", code, lines)
	}
	if ms, ok := lines[0]["readMs"].(float64); !ok || ms < 0 {
		t.Errorf("readMs %v, want a time", lines[0]["readMs"])
	}
	check(map[string]any{"schema": "ipcam.diag.readopen/1", "ok": true, "encrypted": false, "parts": float64(4), "durableBytes": float64(len(data))})

	if err := os.WriteFile(open, bytes.Repeat([]byte("not an fMP4 file "), 10), 0o600); err != nil {
		t.Fatal(err)
	}
	check(map[string]any{"schema": "ipcam.diag.readopen/1", "ok": false, "error": "unreadable"})

	for _, bad := range [][]string{{"readopen", "--config", f.config}, {"readopen", "--cam", testCam}, {"readopen", "--config", f.config, "--cam", testCam, "extra"}} {
		if code, lines, _ := runDiag(t, bad...); code != exitFatal || len(lines) != 0 {
			t.Errorf("diag %v = %d, %v; want a usage error, exit 1", bad, code, lines)
		}
	}
}
