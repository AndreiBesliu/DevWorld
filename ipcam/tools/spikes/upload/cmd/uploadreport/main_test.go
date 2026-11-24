package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMain_DateFromNewestResult: without -date the report's name carries
// the UTC day of the newest envelope's AtMs, else of its newest run's end;
// with no time anywhere, exit 1 with a Romanian line and nothing written.
func TestMain_DateFromNewestResult(t *testing.T) {
	newest := mi8Env()
	newest.AtMs = ms(time.Date(2026, 11, 25, 23, 30, 0, 0, time.UTC))
	dir := t.TempDir()
	writeEnv(t, dir, mi5Env("pc", "ssd"))
	writeEnv(t, dir, newest)
	out := t.TempDir()
	if code, _, stderr := runCmd("-in", dir, "-onedrive", oneDrivePaid, "-out", out); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !slices.Contains(filesIn(t, out), "2026-11-25-phase0c-upload-results.md") {
		t.Errorf("files %v", filesIn(t, out))
	}

	untimed := mi5Env("pc", "ssd")
	untimed.AtMs = 0
	untimed.Runs[0].EndMs = ms(time.Date(2026, 11, 22, 0, 30, 0, 0, time.UTC))
	dir = t.TempDir()
	writeEnv(t, dir, untimed)
	out = t.TempDir()
	if code, _, stderr := runCmd("-in", dir, "-onedrive", oneDrivePaid, "-out", out); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !slices.Contains(filesIn(t, out), "2026-11-22-phase0c-upload-results.md") {
		t.Errorf("files %v", filesIn(t, out))
	}

	untimed.Runs[0].StartMs, untimed.Runs[0].EndMs = 0, 0
	dir = t.TempDir()
	writeEnv(t, dir, untimed)
	out = filepath.Join(t.TempDir(), "out")
	code, _, stderr := runCmd("-in", dir, "-onedrive", oneDrivePaid, "-out", out)
	if code != 1 || !strings.Contains(stderr, msgNoDate) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(msgNoDate, "dați data cu -date") {
		t.Errorf("not Romanian: %q", msgNoDate)
	}
	if files := filesIn(t, out); len(files) > 0 {
		t.Errorf("wrote %v", files)
	}
}

// TestMain_Usage: -onedrive is required, and the flags' values are checked.
func TestMain_Usage(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, mi5Env("pc", "ssd"))
	for _, args := range [][]string{
		{"-in", dir},
		{"-in", dir, "-onedrive", "maybe"},
		{"-in", dir, "-onedrive", "free", "-d3", "z"},
		{"-in", dir, "-onedrive", "free", "-date", "24.11.2026"},
		{"-in", dir, "-onedrive", "free", "-annotations", "12a"},
		{"-onedrive", "free"},
		{"-scan"},
	} {
		if code, _, _ := runCmd(args...); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}

// TestScan_CommittedFiles: -scan exits 1 on a planted token and 0 on clean
// files, naming the file, the line and the kind, never the text.
func TestScan_CommittedFiles(t *testing.T) {
	dir := t.TempDir()
	clean := filepath.Join(dir, "2026-11-24-phase0c-upload-results.md")
	csv := filepath.Join(dir, "phase0c-upload-mi5.csv")
	writeFile(t, clean, []byte("# Rezultate\n\nTST-07, OWN-09, mi5-20261124t101010z\n"))
	writeFile(t, csv, []byte("host,set\npc,pc\n"))
	if code, stdout, stderr := runCmd("-scan", clean, csv); code != 0 {
		t.Fatalf("clean: exit %d: %s%s", code, stdout, stderr)
	}
	token := "ya" + "29." + "a0AfB_byC1d2E3f4G5h6I7j8K9"
	dirty := filepath.Join(dir, "dirty.md")
	writeFile(t, dirty, []byte("linia 1\nvaloare: "+token+"\n"))
	code, _, stderr := runCmd("-scan", clean, dirty)
	if code != 1 || !strings.Contains(stderr, "dirty.md:2:10: Google access token") || strings.Contains(stderr, token) {
		t.Fatalf("dirty: exit %d: %s", code, stderr)
	}
	ip := filepath.Join(dir, "ip.csv")
	writeFile(t, ip, []byte("192.168."+"1.64\n"))
	if code, _, stderr := runCmd("-scan", ip); code != 1 || !strings.Contains(stderr, "IPv4") {
		t.Fatalf("ip: exit %d: %s", code, stderr)
	}
	if code, _, _ := runCmd("-scan", filepath.Join(dir, "missing.md")); code != 1 {
		t.Errorf("missing file: exit %d", code)
	}
	if _, err := os.Stat(clean); err != nil {
		t.Fatal(err)
	}
}
