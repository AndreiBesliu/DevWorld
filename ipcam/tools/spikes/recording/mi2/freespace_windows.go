// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi2

import "golang.org/x/sys/windows"

// freeBytes is the space that this user has free on dir's volume.
func freeBytes(dir string) (uint64, error) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(path, &free, nil, nil); err != nil {
		return 0, err
	}
	return free, nil
}
