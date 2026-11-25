// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package hubproc

import (
	"io/fs"
	"os"
)

// fileOwner has no field on Windows: the hub runs as uploadspike.
type fileOwner struct{}

// spikeOwner is nil on Windows: the hub runs as uploadspike, and its files
// keep the data directory's ACL (%LOCALAPPDATA%, the user's).
func spikeOwner(string) (*fileOwner, error) { return nil, nil } //nolint:nilnil // no owner on Windows

// chmodOwn sets f's mode; Windows has no owner to set.
func chmodOwn(f *os.File, mode fs.FileMode, _ *fileOwner) error { return f.Chmod(mode.Perm()) }

// reownFile is never called on Windows (no owner).
func reownFile(*os.Root, string, string, *fileOwner) error { return nil }
