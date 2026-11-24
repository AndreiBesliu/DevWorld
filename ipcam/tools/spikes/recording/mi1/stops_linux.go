// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// sysrqTrigger is the kernel's magic SysRq file.
const sysrqTrigger = "/proc/sysrq-trigger"

// labPortPath is the lab port in a lab guest (QEMU); tests replace it.
var labPortPath = "/dev/virtio-ports/" + LabPort

// OSStop stops the Linux host as a power cut would (G09, T43): sysrq is
// `echo b > /proc/sysrq-trigger`, an immediate reboot without a sync or an
// unmount, as root, on the test Pi only. A qemu-reset run's guest asks the
// lab runner, on the lab port, to reset the virtual machine, which the
// runner does with QMP's system_reset (QEMU). Any other kind is no Linux
// stop.
func OSStop(_ context.Context, kind controller.ShutdownKind, _ string) error {
	switch kind {
	case controller.KindSysrq:
		return os.WriteFile(sysrqTrigger, []byte("b"), 0o200)
	case controller.KindQEMUReset:
		f, err := os.OpenFile(labPortPath, os.O_WRONLY, 0)
		if err != nil {
			return fmt.Errorf("mi1: a qemu-reset run asks the lab for its reset on the lab port: %w", err)
		}
		_, err = f.WriteString(resetRequest + "\n")
		return errors.Join(err, f.Close())
	}
	return fmt.Errorf("mi1: %s is no stop of a Linux host", kind)
}
