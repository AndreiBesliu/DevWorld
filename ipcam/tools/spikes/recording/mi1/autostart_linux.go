// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"errors"
	"os"
)

// errNotWindows is the answer of the Windows-only autostart steps.
var errNotWindows = errors.New("mi1: only on Windows")

// Privileged reports whether recspike runs as root, which install needs.
func Privileged() bool { return os.Geteuid() == 0 }

// RelaunchElevated does nothing on Linux: install asks for sudo instead.
func RelaunchElevated([]string) (int, error) { return 0, errNotWindows }

// UserSID is Windows-only: the boot task's user.
func UserSID() (string, error) { return "", errNotWindows }

// DesktopDir is Windows-only: the folder of the real cuts' shortcut.
func DesktopDir() (string, error) { return "", errNotWindows }

// CreateShortcut is Windows-only.
func CreateShortcut(string, string, string, string) error { return errNotWindows }
