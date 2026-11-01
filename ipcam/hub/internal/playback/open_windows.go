package playback

import (
	"os"

	"golang.org/x/sys/windows"
)

// openShared opens path to read, sharing delete access too, unlike
// os.Open: MediaMTX renames a segment once it is complete (MoveFileEx),
// which Windows refuses while a handle opened without FILE_SHARE_DELETE is
// open, so ReadOpen never holds a segment back from its final name.
func openShared(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
