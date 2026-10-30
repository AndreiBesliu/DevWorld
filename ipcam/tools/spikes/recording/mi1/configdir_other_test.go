//go:build !windows

package mi1

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProtectConfigDir_RootsAlone (Final fix B review, M2): on the Pi, the
// folder of ipcam-hub's config is made, or made again, 0700: root, which
// runs recspike there, alone may write it.
func TestProtectConfigDir_RootsAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "var", "lib", "ipcam-recspike")
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // the mode that the test corrects
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil { //nolint:gosec // the mode that the test corrects
		t.Fatal(err)
	}
	if err := protectConfigDir(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("the config folder's mode is %v, want 0700", info.Mode().Perm())
	}
}
