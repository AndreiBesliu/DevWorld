// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// OSStop stops the PC as a power cut would (G09, T43): bugcheck is
// `notmyfault64.exe /accepteula /crash` from binDir, recspike's folder,
// where the preflight checked its signature; with CrashControl\AutoReboot =
// 1, Windows restarts after the blue screen. Any other kind is no Windows
// stop.
func OSStop(ctx context.Context, kind controller.ShutdownKind, binDir string) error {
	if kind != controller.KindBugcheck {
		return fmt.Errorf("mi1: %s is no stop of a Windows host", kind)
	}
	return exec.CommandContext(ctx, filepath.Join(binDir, NotMyFault), "/accepteula", "/crash").Start() //nolint:gosec // the owner's NotMyFault, checked by the preflight
}
