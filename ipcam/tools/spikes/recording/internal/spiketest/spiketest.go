// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package spiketest holds what the recording module's tests share: the
// environment's binaries and media, and the commands they build.
package spiketest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Required returns the path that the environment variable name holds; it
// skips without one, and fails instead when require is "1", as CI sets it.
func Required(t *testing.T, name, require string) string {
	t.Helper()
	path := os.Getenv(name)
	if path == "" {
		if os.Getenv(require) == "1" {
			t.Fatalf("%s=1, but %s is empty", require, name)
		}
		t.Skipf("%s is unset", name)
	}
	if _, err := os.Stat(path); err != nil { //nolint:gosec // the test run's own path
		t.Fatalf("%s: %v", name, err)
	}
	return path
}

// BuildBinary builds the command pkg into dir and returns its path.
func BuildBinary(t *testing.T, dir, name, pkg string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", out, pkg) //nolint:gosec // the test's own build
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, b)
	}
	return out
}
