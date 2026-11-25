// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// diskFree is the free space, for this user, of the volume that holds dir
// (or its nearest existing parent).
func diskFree(dir string) (int64, error) {
	for {
		if _, err := os.Stat(dir); err == nil || filepath.Dir(dir) == dir {
			break
		}
		dir = filepath.Dir(dir)
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free, total, all uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &all); err != nil {
		return 0, err
	}
	return int64(free), nil //nolint:gosec // G115: a volume's size fits
}
