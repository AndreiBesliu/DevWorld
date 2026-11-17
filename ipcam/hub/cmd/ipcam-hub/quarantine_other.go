//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// restrictDir makes the quarantine's directory at path the scan's
// identity's alone (C2-R20 (a)): it must be a directory, not a link, that
// this identity owns, and it is narrowed to 0700 through its own
// descriptor, so the check and the change cover the same directory.
func restrictDir(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	switch {
	case isLink(err), errors.Is(err, unix.ENOTDIR):
		return fmt.Errorf("%s is a link or no directory", path)
	case err != nil:
		return &fs.PathError{Op: "open", Path: path, Err: err}
	}
	defer func() { _ = unix.Close(fd) }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return &fs.PathError{Op: "stat", Path: path, Err: err}
	}
	if euid := os.Geteuid(); int(st.Uid) != euid {
		return fmt.Errorf("%s belongs to uid %d, not to the scan's uid %d", path, st.Uid, euid)
	}
	if st.Mode&0o7777 != 0o700 {
		if err := unix.Fchmod(fd, 0o700); err != nil {
			return &fs.PathError{Op: "chmod", Path: path, Err: err}
		}
	}
	return nil
}

// restrictFile leaves a file to be quarantined as it is: on Linux the
// quarantine's directories, 0700, keep every other identity from it.
func restrictFile(string) error { return nil }
