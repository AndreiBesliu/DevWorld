// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"io/fs"
	"os"
	"syscall"
)

// DeviceChecks are the Pi's preflight checks (G14): root, and the free
// space of dataDir, where the runs record, for cameras main streams.
// binDir, recspike's folder, holds nothing that the Pi checks.
func DeviceChecks(_, dataDir string, cameras int) []Check {
	return PiChecks(PiProbes{
		Euid:      os.Geteuid,
		FreeBytes: func() (uint64, error) { return FreeBytes(dataDir) },
		Cameras:   cameras,
	})
}

// ownerUID is the user that owns the file, -1 when unknown.
func ownerUID(fi fs.FileInfo) int {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid)
	}
	return -1
}
