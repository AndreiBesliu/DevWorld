package recorder

// syncDirectory does nothing on Windows and opens no handle. Windows has no
// directory fsync: NTFS journals the directory entries, and the rename that
// gives a segment its final name is write-through (rename_windows.go).
func syncDirectory(string) error {
	return nil
}
