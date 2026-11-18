// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

// diskFree is the free space, for an unprivileged writer, of the file
// system that holds dir (or its nearest existing parent).
func diskFree(dir string) (int64, error) {
	for {
		if _, err := os.Stat(dir); err == nil || filepath.Dir(dir) == dir {
			break
		}
		dir = filepath.Dir(dir)
	}
	var s syscall.Statfs_t
	if err := syscall.Statfs(dir, &s); err != nil {
		return 0, err
	}
	return int64(s.Bavail) * int64(s.Bsize), nil //nolint:gosec,unconvert // G115: a file system's size fits; Bsize is int32 on arm
}
