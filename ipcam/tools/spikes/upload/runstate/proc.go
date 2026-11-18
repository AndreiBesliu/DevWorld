// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package runstate

// Alive reports whether the process pid runs and started at startMs, as
// ProcStartMs gives it: a pid that a new process took since is not alive.
func Alive(pid int, startMs int64) bool {
	if pid <= 0 {
		return false
	}
	start, err := ProcStartMs(pid)
	return err == nil && start == startMs
}
