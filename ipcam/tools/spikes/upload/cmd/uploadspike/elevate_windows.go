// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// relaunchElevated runs uploadspike again with args, elevated: Windows
// shows one UAC prompt (a copy of c1's mi1.RelaunchElevated, through
// Start-Process -Verb RunAs). It waits for the elevated uploadspike, whose
// window shows its own lines, and returns its exit code.
func relaunchElevated(args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", "''") + "'"
	}
	script := "$p = Start-Process -FilePath $env:IPCAM_SPIKE_EXE -ArgumentList @(" + strings.Join(quoted, ",") +
		") -Verb RunAs -Wait -PassThru; exit $p.ExitCode"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script) //nolint:gosec // uploadspike itself
	cmd.Env = append(os.Environ(), "IPCAM_SPIKE_EXE="+exe)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 0, err
}
