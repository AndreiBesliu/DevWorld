// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

//go:build !windows

package mi1

import "os"

// renameReplace renames from to to, replacing to; the folder's sync after
// it makes the rename durable.
func renameReplace(from, to string) error { return os.Rename(from, to) }
