package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// rootedPath matches a local absolute path at the start of a string or
// of a word in it: "/…", "C:\…", "C:/…" or "\\…".
var rootedPath = regexp.MustCompile(`(^|[\s(«"'=])(/[^\s/]|[A-Za-z]:[\\/]|\\\\)`)

// assertNoLocalPath fails when text, a result's bytes, names this device's
// base (raw or JSON-escaped), or, being JSON, holds a string value that is
// a rooted local path (PH0-22: on Windows the base is under the owner's
// profile folder, C:\Users\<profile>\…).
func assertNoLocalPath(t *testing.T, what string, text []byte, base string) {
	t.Helper()
	escaped, _ := json.Marshal(base)
	if strings.Contains(string(text), base) || strings.Contains(string(text), strings.Trim(string(escaped), `"`)) {
		t.Errorf("%s names the device's base folder %s", what, base)
	}
	var v any
	if json.Unmarshal(text, &v) != nil {
		return
	}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case string:
			if rootedPath.MatchString(v) || strings.ContainsAny(v, `\`) {
				t.Errorf("%s holds a local path: %q", what, v)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		case map[string]any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(v)
}

// envelopeBytes is the MI6 envelope that results wrote, and its bytes.
func envelopeBytes(t *testing.T, r *rig) (results.Envelope, []byte) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(r.base, "results", "MI6", "*", "*.json"))
	if len(matches) != 1 {
		t.Fatalf("MI6 envelopes %v; want one", matches)
	}
	b, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var e results.Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e, b
}

// zipEntries reads the results' zip in the rig's current folder: its
// entries' names and contents.
func zipEntries(t *testing.T, r *rig) map[string]string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(r.cwd, "results-*.zip"))
	if len(matches) != 1 {
		t.Fatalf("zips %v; want one", matches)
	}
	zr, err := zip.OpenReader(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(b)
	}
	return out
}

// plant writes a file under the rig's results/ that holds value.
func plant(t *testing.T, r *rig, rel, value string) string {
	t.Helper()
	path := filepath.Join(r.base, "results", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"note":"`+value+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// lastScanAfter fails unless the stand-in scanned at least atLeast times,
// the last time at or after atMs, the last write of results/.
func lastScanAfter(t *testing.T, r *rig, atMs int64, atLeast int) {
	t.Helper()
	scans := r.records(spiketest.RecordScan)
	if len(scans) < atLeast || scans[len(scans)-1].AtMs < atMs {
		t.Errorf("scans %+v; want %d or more, the last one at or after the envelope's write at %d", scans, atLeast, atMs)
	}
}

// TestClose_ScanFailureKeepsPathsOutOfTheEnvelope (PH0-22, C2-R20): a
// close whose known-secrets scan moves a file fails the MI6 run with a
// fixed reason that names no folder of this device; the envelope written
// again after the failure is scanned once more before the zip; the zip
// holds the envelope, not the moved file, and no local path; the
// quarantine's folder is said on the owner's terminal only.
func TestClose_ScanFailureKeepsPathsOutOfTheEnvelope(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	plant(t, r, "MI6/lab-planted/planted.json", r.state().HandoverToken)
	p := r.run("", nil, "results")
	if p.code != 1 || !strings.Contains(p.stdout.String(), "Scanarea rezultatelor a găsit un secret") ||
		!strings.Contains(p.stdout.String(), filepath.Join(r.base, "results-quarantine")) {
		t.Fatalf("results: exit %d\nstdout:\n%s\nstderr:\n%s", p.code, p.stdout, p.stderr)
	}
	run, err := mi6.Decode(r.state().Open["MI6"])
	if err != nil || run.ScanFailed != "scan-secrets: moved 1, unmoved 0" {
		t.Errorf("the run's scan failure %q (%v); want the fixed reason", run.ScanFailed, err)
	}
	e, raw := envelopeBytes(t, r)
	if e.Runs[0].Valid || e.Runs[0].InvalidReason != "scan-secrets: moved 1, unmoved 0" {
		t.Errorf("the envelope's run %+v; want it invalid with the fixed reason", e.Runs[0])
	}
	assertNoLocalPath(t, "the envelope", raw, r.base)
	lastScanAfter(t, r, e.AtMs, 2)
	entries := zipEntries(t, r)
	if _, ok := entries["results/MI6/lab-planted/planted.json"]; ok || len(entries) != 1 {
		t.Errorf("the zip holds %d entries, the planted file %v; want the envelope alone", len(entries), ok)
	}
	for name, body := range entries {
		assertNoLocalPath(t, "the zip's "+name, []byte(body), r.base)
	}
}

// TestResults_ScansBeforeEveryZip (P45, C2-R20): results scans results/
// right before it zips it, also when no MI6 run is open any more (after
// day 7's close), and a scan that leaves a file it found in results/
// stops the zip.
func TestResults_ScansBeforeEveryZip(t *testing.T) {
	t.Run("after the close", func(t *testing.T) {
		r := newRig(t)
		r.update(func(*runstate.State) {})
		plant(t, r, "app/run-1/result.json", r.state().HandoverToken)
		p := r.run("", nil, "results")
		if p.code != 1 || !strings.Contains(p.stdout.String(), "Scanarea rezultatelor a găsit un secret") {
			t.Fatalf("results: exit %d\nstdout:\n%s\nstderr:\n%s", p.code, p.stdout, p.stderr)
		}
		if n := len(r.records(spiketest.RecordScan)); n != 1 {
			t.Errorf("%d scans; want one, before the zip", n)
		}
		if entries := zipEntries(t, r); len(entries) != 0 {
			t.Errorf("the zip holds %v; want nothing (the planted file was moved)", entries)
		}
	})
	t.Run("a file left in results", func(t *testing.T) {
		r := newRig(t)
		r.openMI6("android")
		planted := plant(t, r, "app/run-1/result.json", r.state().HandoverToken)
		p := r.run("", []string{spiketest.FakeScanUnmovedEnv + "=1"}, "results")
		if p.code != 1 || !strings.Contains(p.stdout.String(), msgResultsNotZipped) {
			t.Fatalf("results: exit %d\nstdout:\n%s\nstderr:\n%s", p.code, p.stdout, p.stderr)
		}
		if matches, _ := filepath.Glob(filepath.Join(r.cwd, "results-*.zip")); len(matches) != 0 {
			t.Errorf("results zipped %v with a secret left in %s", matches, planted)
		}
		e, raw := envelopeBytes(t, r)
		if e.Runs[0].Valid || e.Runs[0].InvalidReason != "scan-secrets: moved 0, unmoved 1" {
			t.Errorf("the envelope's run %+v; want it invalid with the fixed reason", e.Runs[0])
		}
		assertNoLocalPath(t, "the envelope", raw, r.base)
	})
}

// TestClose_Day7CloseScansTheRewrittenEnvelope (Close, PH0-22): resume's
// close at day 7 writes the envelope, and when the scan fails the run, it
// writes it again with the fixed reason and scans once more, so the last
// write of results/ is scanned; the run leaves the run state.
func TestClose_Day7CloseScansTheRewrittenEnvelope(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	r.editMI6(func(run *mi6.Run) { run.StartMs = time.Now().Add(-8 * 24 * time.Hour).UnixMilli() })
	plant(t, r, "MI6/lab-planted/planted.json", r.state().HandoverToken)
	res := r.start(nil, "resume")
	until(t, time.Minute, "the close", func() bool { _, open := r.state().Open["MI6"]; return !open })
	until(t, 10*time.Second, "the close's line", func() bool { return strings.Contains(res.stdout.String(), msgMI6Closed) })
	e, raw := envelopeBytes(t, r)
	if e.Runs[0].Valid || e.Runs[0].InvalidReason != "scan-secrets: moved 1, unmoved 0" {
		t.Errorf("the envelope's run %+v; want it invalid with the fixed reason", e.Runs[0])
	}
	assertNoLocalPath(t, "the envelope", raw, r.base)
	lastScanAfter(t, r, e.AtMs, 2)
	if _, err := os.Stat(filepath.Join(r.base, "results", "MI6", "lab-planted", "planted.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the planted file is still in results/: %v", err)
	}
}

// TestTestSeams_WarnedAndRecorded (C2-R22 (3)): a command run with one of
// uploadspike's test seams set says so in one Romanian line naming the
// set seams, and the envelope that it writes records testSeams, which G15
// does not judge; the seams' values never appear in the line.
func TestTestSeams_WarnedAndRecorded(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	p := r.run("", nil, "results")
	if p.code != 0 {
		t.Fatalf("results: exit %d\n%s\n%s", p.code, p.stdout, p.stderr)
	}
	var warning string
	for _, line := range strings.Split(p.stderr.String(), "\n") {
		if strings.HasPrefix(line, strings.SplitN(msgTestSeams, "%", 2)[0]) {
			warning = line
		}
	}
	for _, seam := range []string{envBase, envHubBin, envPort, envNoBrowser, envFast, envRunAs, envHost} {
		if !strings.Contains(warning, seam) {
			t.Errorf("the warning %q does not name %s", warning, seam)
		}
	}
	if strings.Contains(warning, r.base) || strings.Contains(warning, r.hub) {
		t.Errorf("the warning %q shows a seam's value", warning)
	}
	if e, _ := envelopeBytes(t, r); !e.TestSeams {
		t.Errorf("the envelope written with seams set: testSeams %v; want true", e.TestSeams)
	}
}
