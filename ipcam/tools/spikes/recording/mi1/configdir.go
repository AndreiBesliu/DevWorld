// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"errors"
	"io/fs"
	"os"
)

// WriteProtected writes data into a new file at path, in the folder that
// ProtectConfigDir protects, which only Administrators and SYSTEM, or root,
// may change. A file already at path is removed, never written through
// (Final fix C review, I2): the new file has no owner, DACL or link of the
// one that was there, which on the PC another account might change, with
// its DACL, between this write and ipcam-hub's start, and a link would
// carry the write to wherever it points. The new file is created, never
// opened, so one put back at its name meanwhile makes the write fail.
func WriteProtected(path string, data []byte) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := createProtected(path)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
