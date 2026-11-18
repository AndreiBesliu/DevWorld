package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func scan(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRedactscan_CleanFilesPass(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "2026-10-20-phase0b-results.md"), "# Rezultate\n\nDeschideți `http://127.0.0.1:7080`.\n")
	write(t, filepath.Join(dir, "phase0-data", "phase0b-android.csv"), "device,scenario,run,status,key,index,value\nandroid,S1,a.json,used,firstFrameMs,0,310\n")
	code, stdout, stderr := scan(t, filepath.Join(dir, "2026-10-20-phase0b-results.md"), filepath.Join(dir, "phase0-data"))
	if code != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q: want 0 for clean files", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "2 files") {
		t.Errorf("stdout %q does not say how many files it scanned", stdout)
	}
}

// A match fails the run and is named by file, line, column and kind, never by its value:
// the CI log of a public repository must not repeat what it found.
func TestRedactscan_AMatchFailsWithoutShowingTheValue(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "2026-10-20-phase0b-results.md")
	write(t, report, "# Rezultate\n\nTelefonul a ajuns la hub pe 192.168.1.23.\n")
	data := filepath.Join(dir, "phase0-data")
	write(t, filepath.Join(data, "nested", "notes.csv"), "a,b\nowner@firma.ro,x\n")
	code, stdout, stderr := scan(t, report, data)
	if code != 1 {
		t.Fatalf("exit %d, want 1 when something matches (stdout %q, stderr %q)", code, stdout, stderr)
	}
	all := stdout + stderr
	for _, want := range []string{
		report + ":3:29: IPv4 address",
		filepath.Join(data, "nested", "notes.csv") + ":2:1: e-mail address",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("output lacks %q:\n%s", want, all)
		}
	}
	for _, secret := range []string{"192.168", "owner@", "firma.ro"} {
		if strings.Contains(all, secret) {
			t.Errorf("output repeats the value %q:\n%s", secret, all)
		}
	}
}

// A binary file cannot be scanned, so it fails rather than pass unread.
func TestRedactscan_ABinaryFileFails(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shot.png"), "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	code, stdout, stderr := scan(t, dir)
	if code != 1 || !strings.Contains(stdout+stderr, "shot.png") || !strings.Contains(stdout+stderr, "binary") {
		t.Errorf("exit %d, output %q: want a failure that names the binary file", code, stdout+stderr)
	}
}

func TestRedactscan_UsageErrors(t *testing.T) {
	if code, _, stderr := scan(t); code != 2 || stderr == "" {
		t.Errorf("no argument: exit %d, stderr %q, want 2 with a usage line", code, stderr)
	}
	if code, _, stderr := scan(t, filepath.Join(t.TempDir(), "missing.md")); code != 2 || stderr == "" {
		t.Errorf("a missing path: exit %d, stderr %q, want 2 with a message", code, stderr)
	}
}
