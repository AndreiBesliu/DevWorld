// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

//go:build !windows

package install

// KeepAwake asks nothing outside Windows: the Pi does not sleep on its
// own.
func KeepAwake() (func(), error) { return func() {}, nil }
