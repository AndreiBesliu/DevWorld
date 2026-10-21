package recorder

import (
	"os"

	"golang.org/x/sys/windows"
)

// moveFileFlags replace an existing file and make MoveFileEx return only once
// the rename has been flushed to disk, which stands in for the directory sync
// that Windows does not have (dirsync_windows.go).
const moveFileFlags = windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH

// moveFileEx is the system call, a seam for TestMTX1_WindowsDirSyncNoOpAndWriteThroughRename.
var moveFileEx = windows.MoveFileEx

// renameReplace renames the closed file from to to with MoveFileEx, replacing to.
func renameReplace(from, to string) error {
	from16, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	to16, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	err = moveFileEx(from16, to16, moveFileFlags)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}
