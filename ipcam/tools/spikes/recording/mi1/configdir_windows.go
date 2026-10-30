// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// protectConfigDir makes dir, the folder of ipcam-hub's config under
// %ProgramData%, where any user may create a folder first, and gives it to
// Administrators with a protected DACL: Administrators and SYSTEM alone
// (AdministratorsOnlyCommands), as install does for its own files.
func protectConfigDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, cmd := range AdministratorsOnlyCommands(dir) {
		if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil { //nolint:gosec // icacls with recspike's own arguments
			return fmt.Errorf("%s: %w: %s", cmd[0], err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
