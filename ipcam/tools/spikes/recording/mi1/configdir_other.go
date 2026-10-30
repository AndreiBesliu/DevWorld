// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

//go:build !windows

package mi1

import "os"

// ProtectConfigDir makes dir, the folder of recspike's own files and of
// ipcam-hub's config, root's alone: recspike runs as root there, and the
// folder's mode leaves no one else in.
func ProtectConfigDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700) //nolint:gosec // a folder: root may enter it
}

// createProtected creates the file at path, which must not exist, for
// root alone.
func createProtected(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // recspike's own folder
}
