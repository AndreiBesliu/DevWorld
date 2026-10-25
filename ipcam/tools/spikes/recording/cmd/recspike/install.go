// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// The autostart's names (G14).
const (
	autostartName   = "ipcam-recspike"
	systemdUnitPath = "/etc/systemd/system/ipcam-recspike.service"
	shortcutArgs    = "mi1 --real --resume"
)

// xmlText escapes s for an XML text node.
func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// taskXML is the PC's boot task (G14): at every boot, as the owner's user
// whether logged on or not (S4U), with the highest privileges, it runs
// `recspike resume` in recspike's folder; it never stops it, and runs it on
// battery too.
func taskXML(exe, dir, userSID string) string {
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Proba de înregistrare ipcam (MI1): continuă după fiecare repornire.</Description>
  </RegistrationInfo>
  <Triggers>
    <BootTrigger>
      <Enabled>true</Enabled>
    </BootTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + xmlText(userSID) + `</UserId>
      <LogonType>S4U</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + xmlText(exe) + `</Command>
      <Arguments>resume</Arguments>
      <WorkingDirectory>` + xmlText(dir) + `</WorkingDirectory>
    </Exec>
  </Actions>
</Task>
`
}

// systemdUnit is the Pi's boot service (G14): `recspike resume` as root,
// once the network and the clock are up.
func systemdUnit(exe string) string {
	command := exe
	if strings.ContainsAny(exe, " \t") {
		command = `"` + exe + `"`
	}
	return `[Unit]
Description=ipcam: proba de înregistrare (MI1) continuă după repornire
Wants=network-online.target time-sync.target
After=network-online.target time-sync.target

[Service]
Type=simple
User=root
WorkingDirectory=` + filepath.Dir(exe) + `
ExecStart=` + command + ` resume

[Install]
WantedBy=multi-user.target
`
}

// utf16File is s as schtasks reads a task file: UTF-16 LE with a BOM.
func utf16File(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xff, 0xfe
	for _, u := range units {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return out
}

// autostart is what install and uninstall need of the OS; tests replace
// each part.
type autostart struct {
	goos       string
	privileged func() bool
	elevate    func(args []string) (int, error)
	run        func(name string, args ...string) error
	userSID    func() (string, error)
	desktop    func() (string, error)
	shortcut   func(path, target, args, dir string) error
	unitPath   string
	tempDir    string
	out        io.Writer
}

// systemAutostart is this OS's.
func systemAutostart(out io.Writer) autostart {
	return autostart{
		goos:       runtime.GOOS,
		privileged: mi1.Privileged,
		elevate:    mi1.RelaunchElevated,
		run: func(name string, args ...string) error {
			b, err := exec.Command(name, args...).CombinedOutput() //nolint:gosec // schtasks and systemctl with recspike's own arguments
			if err != nil {
				return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(b)))
			}
			return nil
		},
		userSID:  mi1.UserSID,
		desktop:  mi1.DesktopDir,
		shortcut: mi1.CreateShortcut,
		unitPath: systemdUnitPath,
		tempDir:  os.TempDir(),
		out:      out,
	}
}

// folderOf is the folder of exe, a path of the autostart's OS.
func (a autostart) folderOf(exe string) string {
	if i := strings.LastIndexAny(exe, `\/`); a.goos == "windows" && i >= 0 {
		return exe[:i]
	}
	return filepath.Dir(exe)
}

// shortcutPath is the real cuts' desktop shortcut.
func (a autostart) shortcutPath() (string, error) {
	desktop, err := a.desktop()
	if err != nil {
		return "", err
	}
	return filepath.Join(desktop, mi1.RealShortcut+".lnk"), nil
}

// elevated runs cmd elevated when recspike is not: one UAC prompt. ok is
// false when it was not elevated and the elevated recspike did the work.
func (a autostart) elevated(cmd string) (ok bool, err error) {
	if a.privileged() {
		return true, nil
	}
	_, _ = fmt.Fprintln(a.out, "Windows cere acum permisiunea de administrator: răspundeți „Da”.")
	code, err := a.elevate([]string{cmd})
	if err != nil {
		return false, fmt.Errorf("nu am putut porni recspike ca administrator: %w", err)
	}
	if code != 0 {
		return false, fmt.Errorf("recspike %s ca administrator s-a încheiat cu codul %d", cmd, code)
	}
	return false, nil
}

// install installs the autostart of exe (G14): on the PC the boot task and
// the real cuts' desktop shortcut, on the Pi the boot service.
func (a autostart) install(exe string) error {
	dir := a.folderOf(exe)
	if a.goos != "windows" {
		if !a.privileged() {
			return errors.New("instalarea scrie în /etc/systemd: porniți-o cu sudo ./recspike install")
		}
		if err := os.WriteFile(a.unitPath, []byte(systemdUnit(exe)), 0o644); err != nil { //nolint:gosec // a systemd unit is world-readable
			return err
		}
		if err := a.run("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := a.run("systemctl", "enable", autostartName+".service"); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(a.out, "Pornirea automată este instalată: proba continuă singură după fiecare repornire.")
		return nil
	}
	if ok, err := a.elevated("install"); !ok || err != nil {
		return err
	}
	sid, err := a.userSID()
	if err != nil {
		return err
	}
	file := filepath.Join(a.tempDir, autostartName+".xml")
	if err := os.WriteFile(file, utf16File(taskXML(exe, dir, sid)), 0o600); err != nil {
		return err
	}
	defer func() { _ = os.Remove(file) }()
	if err := a.run("schtasks", "/Create", "/TN", autostartName, "/XML", file, "/F"); err != nil {
		return err
	}
	link, err := a.shortcutPath()
	if err != nil {
		return err
	}
	if err := a.shortcut(link, exe, shortcutArgs, dir); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.out, "Pornirea automată este instalată, iar pe desktop este scurtătura „%s”.\n", mi1.RealShortcut)
	return nil
}

// uninstall removes the autostart and the shortcut; what is already gone
// is no error.
func (a autostart) uninstall() error {
	if a.goos != "windows" {
		if !a.privileged() {
			return errors.New("dezinstalarea scrie în /etc/systemd: porniți-o cu sudo ./recspike uninstall")
		}
		if _, err := os.Stat(a.unitPath); errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintln(a.out, "Pornirea automată nu este instalată.")
			return nil
		}
		if err := a.run("systemctl", "disable", autostartName+".service"); err != nil {
			return err
		}
		if err := os.Remove(a.unitPath); err != nil {
			return err
		}
		if err := a.run("systemctl", "daemon-reload"); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(a.out, "Pornirea automată a fost ștearsă.")
		return nil
	}
	if ok, err := a.elevated("uninstall"); !ok || err != nil {
		return err
	}
	if a.run("schtasks", "/Query", "/TN", autostartName) == nil {
		if err := a.run("schtasks", "/Delete", "/TN", autostartName, "/F"); err != nil {
			return err
		}
	}
	link, err := a.shortcutPath()
	if err != nil {
		return err
	}
	if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, _ = fmt.Fprintln(a.out, "Pornirea automată și scurtătura de pe desktop au fost șterse.")
	return nil
}
