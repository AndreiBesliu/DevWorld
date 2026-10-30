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

// FileOwnerSID is Windows-only: the SID of a file's owner.
func FileOwnerSID(string) (string, error) { return "", errNotWindows }

// DesktopDir is Windows-only: the folder of the real cuts' shortcut.
func DesktopDir() (string, error) { return "", errNotWindows }

// CreateShortcut is Windows-only.
func CreateShortcut(string, string, string, string, bool) error { return errNotWindows }

// TimeService is Windows' time service, which only the PC's install
// changes (C1-R16, C1-R16b).
type TimeService struct{}

// StartType is Windows-only.
func (TimeService) StartType() (uint32, error) { return 0, errNotWindows }

// SetStartType is Windows-only.
func (TimeService) SetStartType(uint32) error { return errNotWindows }

// Triggers is Windows-only.
func (TimeService) Triggers() ([]ServiceTrigger, error) { return nil, errNotWindows }

// SetTriggers is Windows-only.
func (TimeService) SetTriggers([]ServiceTrigger) error { return errNotWindows }

// Start is Windows-only.
func (TimeService) Start() error { return errNotWindows }
