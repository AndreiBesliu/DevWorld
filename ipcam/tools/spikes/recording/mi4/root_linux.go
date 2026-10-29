// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi4

import (
	"os"
	"syscall"
)

// coldReads: on Linux, MI4 drops the page cache before each set, so the
// reads and seeks are cold (G22).
const coldReads = true

// requireRoot refuses an euid other than root's.
func requireRoot(euid int) error {
	if euid != 0 {
		return ErrNeedsSudo
	}
	return nil
}

// dropCaches writes the dirty pages, then drops the page cache, the
// dentries and the inodes (`sync; echo 3 > /proc/sys/vm/drop_caches`), as
// root.
func dropCaches() error {
	syscall.Sync()
	return os.WriteFile("/proc/sys/vm/drop_caches", []byte("3\n"), 0o200)
}
