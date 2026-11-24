// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

//go:build !linux

package hostinfo

// deviceTreeModel is empty: only Linux has a device tree to read, and diag
// host reports no Pi model elsewhere.
func deviceTreeModel(string) string { return "" }
