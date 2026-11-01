// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

//go:build !windows

package mi1

// KeepAwake asks nothing of the OS outside Windows (copied from the video
// spike's spikehub, R49): a Raspberry Pi or a lab runner does not sleep on
// its own.
func KeepAwake() (func(), error) { return func() {}, nil }
