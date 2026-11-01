//go:build !windows

package main

import (
	"os"
	"syscall"
)

// fileUID is the owner of the file that info describes.
func fileUID(info os.FileInfo) (int, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}
