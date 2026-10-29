// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

//go:build !linux

package mi4

// coldReads: outside Linux the page cache stays, and the reads and seeks
// are reported as warm (G22).
const coldReads = false

// requireRoot needs no root: nothing is dropped.
func requireRoot(int) error { return nil }

// dropCaches drops nothing.
func dropCaches() error { return nil }
