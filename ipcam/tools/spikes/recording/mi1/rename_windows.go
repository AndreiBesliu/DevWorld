// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"os"

	"golang.org/x/sys/windows"
)

// moveFileEx is windows.MoveFileEx; tests see the flags.
var moveFileEx = windows.MoveFileEx

// renameReplace renames from to to, replacing to, with
// MOVEFILE_WRITE_THROUGH, so that the rename is on disk when it returns
// (mi1-3): NTFS commits a plain rename's log lazily, and the bugcheck that
// follows the run state's last save within a second can lose it. The hub
// renames its files the same way (hub/internal/recorder/proc_windows.go;
// tools do not import the hub, C01).
func renameReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	if err := moveFileEx(f, t, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}
