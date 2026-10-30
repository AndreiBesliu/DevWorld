// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package hostinfo

import (
	"os"
	"strings"
)

// deviceTreeModelPath is where Linux names the board, as the firmware's
// device tree does: "Raspberry Pi 4 Model B Rev 1.5" with a trailing NUL.
const deviceTreeModelPath = "/proc/device-tree/model"

// deviceTreeModel reads the board's model from path, by default
// deviceTreeModelPath (G32); it is empty when the board has none, as on a
// PC or a lab runner.
func deviceTreeModel(path string) string {
	if path == "" {
		path = deviceTreeModelPath
	}
	b, err := os.ReadFile(path) //nolint:gosec // the device tree, or a test's file
	if err != nil {
		return ""
	}
	return strings.Trim(string(b), " \t\r\n\x00")
}
