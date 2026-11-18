package mi1

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// TestRenameReplace_WritesThrough (mi1-3): on Windows the run state is
// renamed with MoveFileEx(MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH),
// as the hub renames its files, so that the rename is on disk when it
// returns, before NotMyFault; and the rename replaces the old file.
func TestRenameReplace_WritesThrough(t *testing.T) {
	var flags uint32
	system := moveFileEx
	moveFileEx = func(from, to *uint16, f uint32) error {
		flags = f
		return system(from, to, f)
	}
	t.Cleanup(func() { moveFileEx = system })
	dir := t.TempDir()
	from, to := filepath.Join(dir, "run-state.1.tmp"), filepath.Join(dir, "run-state.json")
	for path, data := range map[string]string{from: "new\n", to: "old\n"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := renameReplace(from, to); err != nil {
		t.Fatalf("renameReplace: %v", err)
	}
	if want := uint32(windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH); flags != want {
		t.Errorf("MoveFileEx flags %#x, want %#x", flags, want)
	}
	if data, err := os.ReadFile(to); err != nil || string(data) != "new\n" { //nolint:gosec // the test's own file
		t.Errorf("the renamed file holds %q (%v), want the new one", data, err)
	}
}
