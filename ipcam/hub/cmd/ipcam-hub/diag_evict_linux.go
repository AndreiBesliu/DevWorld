package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// dropFileCache drops f's clean pages from the page cache (posix_fadvise
// DONTNEED), which needs no privilege: the benches' reads then start cold
// for that file, also after they have read its index.
func dropFileCache(f *os.File) error {
	return unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED) //nolint:gosec // a file descriptor
}
