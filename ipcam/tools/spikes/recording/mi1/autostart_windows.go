// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unsafe"

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
// `recspike install` sets it to start automatically, removes its triggers
// and starts it, and uninstall gives it back its start type and its
// triggers (C1-R16, C1-R16b). The service manager needs administrator
// rights, which install has.
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

// serviceTriggerInfo, serviceTrigger and serviceTriggerItem are Win32's
// SERVICE_TRIGGER_INFO, SERVICE_TRIGGER and
// SERVICE_TRIGGER_SPECIFIC_DATA_ITEM, which x/sys/windows does not declare;
// their fields are laid out as Windows' are.
type serviceTriggerInfo struct {
	count    uint32
	triggers *serviceTrigger
	reserved *byte
}

type serviceTrigger struct {
	triggerType uint32
	action      uint32
	subtype     *windows.GUID
	itemCount   uint32
	items       *serviceTriggerItem
}

type serviceTriggerItem struct {
	dataType uint32
	size     uint32
	data     *byte
}

// Triggers are W32Time's triggers, as the service manager holds them; none
// is an empty list.
func (TimeService) Triggers() ([]ServiceTrigger, error) {
	out := []ServiceTrigger{}
	err := withTimeService(func(s *mgr.Service) error {
		buf, err := queryTriggerInfo(s.Handle)
		if err != nil {
			return err
		}
		info := (*serviceTriggerInfo)(unsafe.Pointer(&buf[0])) //nolint:gosec // the structure that QueryServiceConfig2 wrote into buf
		if info.count == 0 {
			return nil
		}
		for _, tr := range unsafe.Slice(info.triggers, info.count) { //nolint:gosec // the triggers that QueryServiceConfig2 wrote into buf
			t := ServiceTrigger{Type: tr.triggerType, Action: tr.action}
			if tr.subtype != nil {
				t.Subtype = tr.subtype.String()
			}
			if tr.itemCount > 0 {
				for _, item := range unsafe.Slice(tr.items, tr.itemCount) { //nolint:gosec // the trigger's data items, in buf
					d := ServiceTriggerData{Type: item.dataType, Bytes: []byte{}}
					if item.size > 0 {
						d.Bytes = append(d.Bytes, unsafe.Slice(item.data, item.size)...) //nolint:gosec // the item's bytes, in buf
					}
					t.Data = append(t.Data, d)
				}
			}
			out = append(out, t)
		}
		return nil
	})
	return out, err
}

// queryTriggerInfo is QueryServiceConfig2's SERVICE_CONFIG_TRIGGER_INFO of
// the service h: a SERVICE_TRIGGER_INFO at the start of the buffer, which
// also holds what its pointers point to.
func queryTriggerInfo(h windows.Handle) ([]byte, error) {
	n := uint32(1024)
	for {
		buf := make([]byte, n)
		err := windows.QueryServiceConfig2(h, windows.SERVICE_CONFIG_TRIGGER_INFO, &buf[0], n, &n)
		if err == nil {
			return buf, nil
		}
		if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || n <= uint32(len(buf)) { //nolint:gosec // a buffer of a few KiB
			return nil, err
		}
	}
}

// SetTriggers makes triggers W32Time's triggers, all of them: none removes
// every trigger it has, and leaves a service that has none as it is.
func (t TimeService) SetTriggers(triggers []ServiceTrigger) error {
	if len(triggers) == 0 {
		current, err := t.Triggers()
		if err != nil {
			return err
		}
		if len(current) == 0 {
			return nil
		}
	}
	info := serviceTriggerInfo{count: uint32(len(triggers))} //nolint:gosec // a service's few triggers
	if len(triggers) > 0 {
		list := make([]serviceTrigger, len(triggers))
		for i, tr := range triggers {
			list[i] = serviceTrigger{triggerType: tr.Type, action: tr.Action}
			if tr.Subtype != "" {
				guid, err := windows.GUIDFromString(tr.Subtype)
				if err != nil {
					return fmt.Errorf("mi1: trigger subtype %q: %w", tr.Subtype, err)
				}
				list[i].subtype = &guid
			}
			if len(tr.Data) > 0 {
				items := make([]serviceTriggerItem, len(tr.Data))
				for j, d := range tr.Data {
					items[j] = serviceTriggerItem{dataType: d.Type, size: uint32(len(d.Bytes))} //nolint:gosec // a trigger's few bytes
					if len(d.Bytes) > 0 {
						items[j].data = &d.Bytes[0]
					}
				}
				list[i].itemCount, list[i].items = uint32(len(items)), &items[0] //nolint:gosec // a trigger's few items
			}
		}
		info.triggers = &list[0]
	}
	return withTimeService(func(s *mgr.Service) error {
		return windows.ChangeServiceConfig2(s.Handle, windows.SERVICE_CONFIG_TRIGGER_INFO, (*byte)(unsafe.Pointer(&info))) //nolint:gosec // SERVICE_TRIGGER_INFO, as Windows reads it
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
