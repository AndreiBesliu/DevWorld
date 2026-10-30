// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

//go:build !windows

package mi1

import "os"

// protectConfigDir makes dir, the folder of ipcam-hub's config, root's
// alone: recspike runs as root there, and the folder's mode leaves no one
// else in.
func protectConfigDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700) //nolint:gosec // a folder: root may enter it
}
