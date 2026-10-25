// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"fmt"
	"os"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// sysrqTrigger is the kernel's magic SysRq file.
const sysrqTrigger = "/proc/sysrq-trigger"

// OSStop stops the Linux host as a power cut would (G09, T43): sysrq is
// `echo b > /proc/sysrq-trigger`, an immediate reboot without a sync or an
// unmount, as root, on the test Pi only. A qemu-reset run's guest does
// nothing: the lab resets the virtual machine from outside (QMP). Any other
// kind is no Linux stop.
func OSStop(_ context.Context, kind controller.ShutdownKind, _ string) error {
	switch kind {
	case controller.KindSysrq:
		return os.WriteFile(sysrqTrigger, []byte("b"), 0o200)
	case controller.KindQEMUReset:
		return nil
	}
	return fmt.Errorf("mi1: %s is no stop of a Linux host", kind)
}
