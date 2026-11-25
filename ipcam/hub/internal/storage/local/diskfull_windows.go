package local

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isDiskFull reports whether err says that the volume has no room left:
// ERROR_DISK_FULL, or ERROR_HANDLE_DISK_FULL for a write through a handle.
func isDiskFull(err error) bool {
	return errors.Is(err, windows.ERROR_DISK_FULL) || errors.Is(err, windows.ERROR_HANDLE_DISK_FULL)
}
