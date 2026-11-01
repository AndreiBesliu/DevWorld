package main

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMI4_CLIRefusesWithoutSudo (G22; Task 10 review): `recspike mi4`
// started without sudo on Linux, where MI4 drops the page cache, prints
// exactly MI4's Romanian line, without the «recspike: eroare:» of other
// errors, since it tells the owner what to run; and no ipcam-hub starts.
func TestMI4_CLIRefusesWithoutSudo(t *testing.T) {
	dir := t.TempDir()
	realExecutable, realGeteuid := executable, geteuid
	t.Cleanup(func() { executable, geteuid = realExecutable, realGeteuid })
	executable = func() (string, error) { return filepath.Join(dir, "recspike"), nil }
	geteuid = func() int { return 1000 }
	started := filepath.Join(dir, "started")
	if err := os.WriteFile(filepath.Join(dir, "ipcam-hub"), []byte("#!/bin/sh\necho \"$@\" >> "+started+"\nexit 1\n"), 0o700); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}

	var out, stderr strings.Builder
	err := run(t.Context(), []string{"mi4", "--controller", "192.0.2.1"}, bufio.NewReader(strings.NewReader("")), &out)
	if code := report(err, &stderr); code != 1 || stderr.String() != "MI4 trebuie pornit cu sudo: ./recspike mi4 …\n" {
		t.Errorf("recspike mi4 as uid 1000 ended %d with %q, want 1 and only «MI4 trebuie pornit cu sudo: ./recspike mi4 …»\n%s", code, stderr.String(), out.String())
	}
	if b, err := os.ReadFile(started); !errors.Is(err, fs.ErrNotExist) { //nolint:gosec // the test's own file
		t.Errorf("ipcam-hub started: %q", b)
	}
}
