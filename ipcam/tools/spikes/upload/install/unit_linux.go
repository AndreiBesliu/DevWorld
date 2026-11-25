// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package install

import (
	"context"
	"os"
	"path/filepath"
)

// AddUnit writes the unit under prefix ("" on the Pi) and enables and
// starts it, so resume waits from the install on.
func AddUnit(ctx context.Context, prefix string, run Commander) error {
	path := filepath.Join(prefix, UnitPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: systemd's folder
		return err
	}
	if err := os.WriteFile(path, []byte(SystemdUnit()), 0o644); err != nil { //nolint:gosec // G306: a unit file is world-readable
		return err
	}
	return RunAll(ctx, run, UnitCommands())
}

// RemoveUnit stops, disables and removes the unit.
func RemoveUnit(ctx context.Context, prefix string, run Commander) {
	RunEach(ctx, run, UnunitCommands())
	_ = os.Remove(filepath.Join(prefix, UnitPath))
	RunEach(ctx, run, [][]string{{"systemctl", "daemon-reload"}})
}
