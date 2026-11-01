package recorder

// The Windows test of patch MTX-1 (hub/third_party/PATCHES.md). The other MTX-1
// tests (mtx1_test.go) also run on Windows, where they rename through MoveFileEx.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// TestMTX1_WindowsDirSyncNoOpAndWriteThroughRename: on Windows syncDir returns
// nil without opening a handle on the directory, and renameFinal is MoveFileEx
// with MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH.
func TestMTX1_WindowsDirSyncNoOpAndWriteThroughRename(t *testing.T) {
	// Opening a handle on a directory that does not exist would fail.
	require.NoError(t, syncDir(filepath.Join(t.TempDir(), "missing")))

	var flags []uint32
	realMoveFileEx := moveFileEx
	t.Cleanup(func() { moveFileEx = realMoveFileEx })
	moveFileEx = func(from, to *uint16, f uint32) error {
		flags = append(flags, f)
		return realMoveFileEx(from, to, f)
	}

	dir := t.TempDir()
	from := filepath.Join(dir, "segment.mp4.part")
	to := filepath.Join(dir, "segment.mp4")
	require.NoError(t, os.WriteFile(from, []byte("closed segment"), 0o644))
	require.NoError(t, os.WriteFile(to, []byte("stale file"), 0o644))

	require.NoError(t, renameFinal(from, to))

	require.Equal(t, []uint32{windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH}, flags)
	content, err := os.ReadFile(to)
	require.NoError(t, err)
	require.Equal(t, "closed segment", string(content), "the rename replaces an existing file")
	_, err = os.Stat(from)
	require.ErrorIs(t, err, os.ErrNotExist)
}
