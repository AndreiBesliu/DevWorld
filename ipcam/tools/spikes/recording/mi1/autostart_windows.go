// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

// Privileged reports whether recspike runs elevated, which the boot task's
// registration needs.
func Privileged() bool { return windows.GetCurrentProcessToken().IsElevated() }

// RelaunchElevated runs recspike again with args, elevated: Windows shows
// one UAC prompt (G14). It waits for the elevated recspike, whose window
// shows its own lines, and returns its exit code.
func RelaunchElevated(args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", "''") + "'"
	}
	script := "$p = Start-Process -FilePath $env:RECSPIKE_EXE -ArgumentList @(" + strings.Join(quoted, ",") +
		") -Verb RunAs -Wait -PassThru; exit $p.ExitCode"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script) //nolint:gosec // recspike itself
	cmd.Env = append(os.Environ(), "RECSPIKE_EXE="+exe)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	return 0, err
}

// UserSID is the SID of the user that runs recspike: the boot task runs as
// that user, whether logged on or not (S4U).
func UserSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

// DesktopDir is the user's desktop, wherever Windows keeps it (OneDrive
// included).
func DesktopDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Desktop, windows.KF_FLAG_DEFAULT)
}

// CreateShortcut writes a .lnk shortcut at path that runs target with args
// in dir, through the shell's own COM object; with runAsAdmin, the target
// starts elevated (RunAsAdministrator).
func CreateShortcut(path, target, args, dir string, runAsAdmin bool) error {
	const script = "$s = (New-Object -ComObject WScript.Shell).CreateShortcut($env:LNK_PATH); " +
		"$s.TargetPath = $env:LNK_TARGET; $s.Arguments = $env:LNK_ARGS; $s.WorkingDirectory = $env:LNK_DIR; $s.Save()"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "LNK_PATH="+path, "LNK_TARGET="+target, "LNK_ARGS="+args, "LNK_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mi1: the shortcut: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if runAsAdmin {
		return RunAsAdministrator(path)
	}
	return nil
}

// TimeService is Windows' time service, W32Time, in the service manager:
// `recspike install` sets it to start automatically and starts it, and
// uninstall gives it back its start type (C1-R16). The service manager
// needs administrator rights, which install has.
type TimeService struct{}

// timeServiceName is W32Time's service name.
const timeServiceName = "W32Time"

// withTimeService runs f on W32Time in the service manager.
func withTimeService(f func(s *mgr.Service) error) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(timeServiceName)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return f(s)
}

// StartType is W32Time's start type: SERVICE_AUTO_START (2),
// SERVICE_DEMAND_START (3) or SERVICE_DISABLED (4).
func (TimeService) StartType() (uint32, error) {
	var t uint32
	err := withTimeService(func(s *mgr.Service) error {
		cfg, err := s.Config()
		t = cfg.StartType
		return err
	})
	return t, err
}

// SetStartType changes W32Time's start type alone: every other setting of
// the service stays as it is.
func (TimeService) SetStartType(t uint32) error {
	return withTimeService(func(s *mgr.Service) error {
		return windows.ChangeServiceConfig(s.Handle, windows.SERVICE_NO_CHANGE, t, windows.SERVICE_NO_CHANGE,
			nil, nil, nil, nil, nil, nil, nil)
	})
}

// Start starts W32Time; a service that runs already is no error.
func (TimeService) Start() error {
	return withTimeService(func(s *mgr.Service) error {
		if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return err
		}
		return nil
	})
}
