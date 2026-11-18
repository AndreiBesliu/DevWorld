package mi1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// TestOSStop_QEMUResetAsksTheLab (R03): a qemu-reset run's guest stops by
// asking the lab runner for its reset, one "reset" line on the lab's
// virtio serial port, which mi1.QEMU answers with QMP's system_reset. A
// host without that port, not a lab guest, has no such stop.
func TestOSStop_QEMUResetAsksTheLab(t *testing.T) {
	port := filepath.Join(t.TempDir(), LabPort)
	if err := os.WriteFile(port, nil, 0o600); err != nil { // the port exists, as the guest's device does
		t.Fatal(err)
	}
	realPort := labPortPath
	t.Cleanup(func() { labPortPath = realPort })
	labPortPath = port
	if err := OSStop(t.Context(), controller.KindQEMUReset, ""); err != nil {
		t.Fatalf("OSStop(qemu-reset) = %v", err)
	}
	if data, err := os.ReadFile(port); err != nil || string(data) != "reset\n" { //nolint:gosec // the test's own file
		t.Errorf("the lab port holds %q (%v), want the reset request", data, err)
	}
	labPortPath = filepath.Join(t.TempDir(), "missing")
	if err := OSStop(t.Context(), controller.KindQEMUReset, ""); err == nil {
		t.Error("OSStop(qemu-reset) without the lab port: no error, want one")
	}
	if _, err := os.Stat(labPortPath); !os.IsNotExist(err) {
		t.Errorf("OSStop created the missing lab port: %v", err)
	}
}
