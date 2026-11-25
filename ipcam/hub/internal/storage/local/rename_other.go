//go:build !windows

package local

import "os"

// The directory sync and rename, copied from recorder's proc_linux.go, which
// this package does not import.

// platformSyncDir makes the entries of dir durable.
func platformSyncDir(dir string) error {
	f, err := os.Open(dir) //nolint:gosec // a directory under the destination's root
	if err != nil {
		return err
	}
	err = f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// renameReplace renames from to to, replacing to.
func renameReplace(from, to string) error { return os.Rename(from, to) }
