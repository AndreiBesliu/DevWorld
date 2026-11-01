package local

import "golang.org/x/sys/windows"

// diskSpace reads dir's volume with GetDiskFreeSpaceExW: free is
// lpFreeBytesAvailableToCaller, the bytes this user may still write (a disk
// quota lowers it), and total is lpTotalNumberOfBytes, the volume's size as
// this user sees it.
func diskSpace(dir string) (free, total int64, err error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, 0, err
	}
	var freeToCaller, totalBytes uint64
	if err := windows.GetDiskFreeSpaceEx(path, &freeToCaller, &totalBytes, nil); err != nil {
		return 0, 0, err
	}
	return int64(freeToCaller), int64(totalBytes), nil
}
