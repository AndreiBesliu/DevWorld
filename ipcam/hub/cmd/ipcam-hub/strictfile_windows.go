package main

import (
	"fmt"
	"io"
	"os"
)

// readRegular reads the regular file at path, limit bytes at most. On
// Windows the hub and its diag commands run as one user (the owner's), so
// no other identity's file is read: see strictfile_other.go.
func readRegular(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304: a file that the caller named
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	switch {
	case err != nil:
		return nil, err
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("%s is not a regular file", path)
	case info.Size() > limit:
		return nil, fmt.Errorf("%s is larger than %d bytes", path, limit)
	}
	return io.ReadAll(io.LimitReader(f, limit))
}

// checkCatalogFiles is haveCatalog on Windows: see strictfile_other.go.
func checkCatalogFiles(dataDir string) error { return haveCatalog(dataDir) }
