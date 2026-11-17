//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// readRegular reads the regular file at path, limit bytes at most, without
// following a link as its last element and without blocking on a FIFO in
// its place. On the Pi, root runs diag scan-secrets and diag visibility on
// a run.json and a data directory that the hub's identity owns, which
// could put a link to a device there (its open alone can start a
// watchdog) or a FIFO that blocks the open for good (security review
// SEAL-1). A missing file is fs.ErrNotExist.
func readRegular(path string, limit int64) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	switch {
	case isLink(err):
		return nil, fmt.Errorf("%s is a link, which is not followed", path)
	case err != nil:
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { _ = f.Close() }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, &fs.PathError{Op: "stat", Path: path, Err: err}
	}
	switch {
	case st.Mode&unix.S_IFMT != unix.S_IFREG:
		return nil, fmt.Errorf("%s is not a regular file", path)
	case st.Size > limit:
		return nil, fmt.Errorf("%s is larger than %d bytes", path, limit)
	}
	return io.ReadAll(io.LimitReader(f, limit))
}

// isLink reports the error of an O_NOFOLLOW open of a link: ELOOP, or
// EMLINK on the BSDs.
func isLink(err error) bool { return errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EMLINK) }

// catalogFiles are the catalog's file and SQLite's files beside it.
var catalogFiles = []string{"catalog.db", "catalog.db-wal", "catalog.db-shm", "catalog.db-journal"}

// checkCatalogFiles refuses, without opening any of them, a state/ of
// dataDir that is a link or no directory, and a catalog file (catalogFiles)
// that is a link or no regular file: SQLite follows a link to the catalog
// and blocks on a FIFO, and root's diag commands open the catalog of a
// data directory that the hub's identity owns (security review SEAL-1). It
// opens nothing, so it never drops a lock that SQLite holds in this
// process. errNoCatalog when there is no catalog. A file that another
// process puts in place between this check and SQLite's open is not
// caught: that window is the time between two system calls.
func checkCatalogFiles(dataDir string) error {
	var st unix.Stat_t
	state := filepath.Join(dataDir, "state")
	switch err := unix.Lstat(state, &st); {
	case errors.Is(err, unix.ENOENT):
		return errNoCatalog
	case err != nil:
		return &fs.PathError{Op: "lstat", Path: state, Err: err}
	case st.Mode&unix.S_IFMT != unix.S_IFDIR:
		return fmt.Errorf("%s is a link or no directory", state)
	}
	for _, name := range catalogFiles {
		path := filepath.Join(state, name)
		switch err := unix.Lstat(path, &st); {
		case errors.Is(err, unix.ENOENT) && name == catalogFiles[0]:
			return errNoCatalog
		case errors.Is(err, unix.ENOENT):
		case err != nil:
			return &fs.PathError{Op: "lstat", Path: path, Err: err}
		case st.Mode&unix.S_IFMT != unix.S_IFREG:
			return fmt.Errorf("%s is a link or no regular file", path)
		}
	}
	return nil
}
