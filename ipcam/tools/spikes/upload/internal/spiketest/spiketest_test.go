package spiketest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requiredHelperEnv makes TestRequiredHelper call Required on the variable
// that it names.
const requiredHelperEnv = "UPLOADSPIKE_TEST_REQUIRED_HELPER"

// TestRequiredHelper is TestRequired_SkipsOrFails's child.
func TestRequiredHelper(t *testing.T) {
	name := os.Getenv(requiredHelperEnv)
	if name == "" {
		t.Skip("a helper of TestRequired_SkipsOrFails")
	}
	Required(t, name, "UPLOADSPIKE_TEST_REQUIRE_IT")
}

// TestRequired_SkipsOrFails: without the variable, Required skips, and
// fails instead when its *_REQUIRE_* variable is "1"; with a path that
// exists it passes, and with one that does not it fails.
func TestRequired_SkipsOrFails(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(existing, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value, require, want string
	}{
		{"unset", "", "", "--- SKIP"},
		{"unset but required", "", "1", "--- FAIL"},
		{"an existing path", existing, "1", "--- PASS"},
		{"a missing path", existing + "-missing", "", "--- FAIL"},
	} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRequiredHelper$", "-test.v") //nolint:gosec // the test binary
		cmd.Env = append(os.Environ(), requiredHelperEnv+"=UPLOADSPIKE_TEST_BIN", "UPLOADSPIKE_TEST_BIN="+tc.value,
			"UPLOADSPIKE_TEST_REQUIRE_IT="+tc.require)
		out, _ := cmd.CombinedOutput()
		if !strings.Contains(string(out), tc.want+": TestRequiredHelper") {
			t.Errorf("%s: %s; want %s", tc.name, out, tc.want)
		}
	}
}

// TestBuildBinary: it builds a command of the workspace into the folder,
// with .exe on Windows.
func TestBuildBinary(t *testing.T) {
	dir := t.TempDir()
	bin := BuildBinary(t, dir, "fakeproviders", "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/cmd/fakeproviders")
	if filepath.Dir(bin) != dir {
		t.Errorf("built %s, want it in %s", bin, dir)
	}
	if out, _ := exec.Command(bin).CombinedOutput(); !strings.Contains(string(out), "usage: fakeproviders") { //nolint:gosec // the built command
		t.Errorf("the built command printed %q", out)
	}
}
