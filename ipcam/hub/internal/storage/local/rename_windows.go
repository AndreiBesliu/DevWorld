package local

import (
	"os"

	"golang.org/x/sys/windows"
)

// The Windows directory sync and rename, copied from recorder's
// proc_windows.go, which this package does not import.

// platformSyncDir does nothing: Windows has no directory fsync, and NTFS
// journals directory entries (MTX-1 does the same).
func platformSyncDir(string) error { return nil }

// renameReplace renames from to to, replacing to, with
// MOVEFILE_WRITE_THROUGH so that the rename is on disk when it returns, as
// MTX-1 renames segments.
func renameReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(f, t, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}
