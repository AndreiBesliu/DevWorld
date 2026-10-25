// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"os"
	"syscall"
)

// DeviceChecks are the Pi's preflight checks (G14), with the free space of
// the folder dir, where the runs record.
func DeviceChecks(dir string, cameras int) []Check {
	return PiChecks(PiProbes{
		Euid: os.Geteuid,
		FreeBytes: func() (uint64, error) {
			var st syscall.Statfs_t
			if err := syscall.Statfs(dir, &st); err != nil {
				return 0, err
			}
			return st.Bavail * uint64(st.Bsize), nil //nolint:gosec // a block size
		},
		Cameras: cameras,
	})
}
