// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"cmp"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
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
	// piModesPath keeps the modes of the Pi's release from before install,
	// where only root writes.
	piModesPath = "/var/lib/ipcam-recspike/release-modes.json"
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
// battery too, at the normal priority (5), which ipcam-hub and MediaMTX
// inherit: Task Scheduler's default, 7, is below normal (mi1-5).
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
    <Priority>5</Priority>
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
// once the network and the clock are up. Its paths are Linux paths, on
// every OS that builds it.
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
WorkingDirectory=` + path.Dir(exe) + `
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

// serviceAutoStart is SERVICE_AUTO_START, the start type that install
// gives Windows' time service.
const serviceAutoStart = 2

// timeService is Windows' time service (W32Time) in the service manager
// (C1-R16, C1-R16b).
type timeService interface {
	StartType() (uint32, error)
	SetStartType(uint32) error
	Triggers() ([]mi1.ServiceTrigger, error)
	SetTriggers([]mi1.ServiceTrigger) error
	Start() error
}

// savedTimeService is the time service's settings before install changed
// them: its start type and its triggers, [] for none. A file saved before
// C1-R16b holds no triggers (nil): that install never removed them.
type savedTimeService struct {
	StartType uint32               `json:"startType"`
	Triggers  []mi1.ServiceTrigger `json:"triggers"`
}

// autostart is what install and uninstall need of the OS; tests replace
// each part.
type autostart struct {
	goos        string
	privileged  func() bool
	elevate     func(args []string) (int, error)
	run         func(name string, args ...string) error
	userSID     func() (string, error)
	desktop     func() (string, error)
	shortcut    func(path, target, args, dir string, runAsAdmin bool) error
	unitPath    string
	tempDir     string
	timeService timeService
	// savedStart keeps the time service's settings from before install,
	// outside the release folder (timeServiceFile), in a folder of
	// Administrators alone; ownerSID reads the owner of a file, as a SID.
	savedStart string
	ownerSID   func(path string) (string, error)
	// savedModes keeps the modes of the Pi's release folder and programs
	// from before install (piModesPath).
	savedModes string
	// memoryDump is Windows' kernel dump, which every bugcheck rewrites.
	memoryDump string
	// mkdirAll makes the PC's data folders; chown and chmod change the
	// owner and the mode of the Pi's release (security-1).
	mkdirAll func(path string) error
	chown    func(path string, uid, gid int) error
	chmod    func(path string, mode os.FileMode) error
	out      io.Writer
}

// timeServiceFile is where install keeps the time service's settings from
// before it: under %ProgramData%\ipcam-recspike, outside the release folder,
// so that an owner who deletes that folder and extracts a new release
// there does not lose them (mi1-6).
func timeServiceFile() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, autostartName, "w32time-start.json")
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
		userSID:     mi1.UserSID,
		desktop:     mi1.DesktopDir,
		shortcut:    mi1.CreateShortcut,
		unitPath:    systemdUnitPath,
		tempDir:     os.TempDir(),
		timeService: mi1.TimeService{},
		savedStart:  timeServiceFile(),
		ownerSID:    mi1.FileOwnerSID,
		savedModes:  piModesPath,
		memoryDump:  filepath.Join(cmp.Or(os.Getenv("SystemRoot"), `C:\Windows`), "MEMORY.DMP"),
		mkdirAll:    func(path string) error { return os.MkdirAll(path, 0o700) },
		chown:       os.Lchown,
		chmod:       os.Chmod,
		out:         out,
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
		err := a.saveModes(exe)
		if err == nil {
			err = a.ownRelease(exe, 0, 0, 0o755)
		}
		if err != nil {
			return fmt.Errorf("nu pot da folderul probei lui root, așa că nu instalez pornirea automată: %w", err)
		}
		_, _ = fmt.Fprintln(a.out, "Folderul probei și programele din el sunt acum ale lui root; le schimbați doar cu sudo.")
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
	if err := a.protectFolder(dir, sid); err != nil {
		return err
	}
	// Before the boot task: whether one is registered already tells the
	// probe's settings of the time service from the owner's (mi1-6).
	if err := a.saveTimeService(); err != nil {
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
	// The shortcut runs recspike as administrator, as the boot task does:
	// w32tm reports the clock's NTP state only to an elevated process, and
	// the shortcut's preflight and every run's NTP wait read it (G14).
	if err := a.shortcut(link, exe, shortcutArgs, dir, true); err != nil {
		return err
	}
	if err := a.startTimeService(); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(a.out, "Serviciul de timp Windows pornește acum automat și nu mai are declanșatoare care să-l oprească, ca ceasul să se sincronizeze după fiecare repornire; dezinstalarea îi pune la loc setările de dinainte.")
	_, _ = fmt.Fprintf(a.out, "Pornirea automată este instalată, iar pe desktop este scurtătura „%s”; când o deschideți, Windows cere permisiunea de administrator: răspundeți „Da”.\n", mi1.RealShortcut)
	return nil
}

// uninstall removes the autostart and the shortcut, and gives the release
// folder back as it was before install (security-1); what is already gone
// is no error. On the Pi, with no boot service installed, nothing changes.
func (a autostart) uninstall(exe string) error {
	if a.goos != "windows" {
		if !a.privileged() {
			return errors.New("dezinstalarea scrie în /etc/systemd: porniți-o cu sudo ./recspike uninstall")
		}
		if _, err := os.Stat(a.unitPath); errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintln(a.out, "Pornirea automată nu este instalată.")
			return nil
		}
		if uid, gid, ok := sudoUser(); ok {
			if err := a.giveRelease(exe, uid, gid); err != nil {
				return fmt.Errorf("nu pot da folderul probei înapoi contului dvs.: %w", err)
			}
			_, _ = fmt.Fprintln(a.out, "Folderul probei și programele din el sunt din nou ale contului dvs.")
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
	if err := a.restoreTimeService(); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(a.out, "Pornirea automată și scurtătura de pe desktop au fost șterse.")
	if err := a.unprotectFolder(a.folderOf(exe)); err != nil {
		return err
	}
	return a.removeBugcheckLeftovers()
}

// myFault is the service of NotMyFault's driver, which stays registered
// after the bugchecks.
const myFault = "MyFault"

// removeBugcheckLeftovers deals with what the bugchecks leave on the PC
// (mi1-7, security-3): it removes NotMyFault's driver service when it is
// there, and when Windows' kernel dump is there, often of several GB, it
// points to the guide's step that deletes the crash dumps.
func (a autostart) removeBugcheckLeftovers() error {
	if a.run("sc", "query", myFault) == nil {
		_ = a.run("sc", "stop", myFault) // a stopped driver is no error
		if err := a.run("sc", "delete", myFault); err != nil {
			return fmt.Errorf("nu pot șterge serviciul %s, lăsat de NotMyFault: %w", myFault, err)
		}
		_, _ = fmt.Fprintln(a.out, "Am șters serviciul MyFault, lăsat în Windows de NotMyFault.")
	}
	if _, err := os.Stat(a.memoryDump); err == nil {
		_, _ = fmt.Fprintf(a.out, "Ecranele albastre ale probei au lăsat pe PC %s, de obicei de câțiva GB, și alte fișiere de eroare: "+
			"ștergeți-le cu „Curățare disc”, ca la pasul 14.3 din ghid.\n", a.memoryDump)
	}
	return nil
}

// startTimeService sets Windows' time service to start automatically,
// removes its triggers and starts it (C1-R16, C1-R16b): on a PC outside a
// domain it is otherwise stopped after every boot, by its start type or by
// its "not in a domain" trigger, and the clock never syncs. install has
// saved its settings from before (saveTimeService).
func (a autostart) startTimeService() error {
	if err := a.timeService.SetStartType(serviceAutoStart); err != nil {
		return fmt.Errorf("nu pot porni automat serviciul de timp Windows: %w", err)
	}
	if err := a.timeService.SetTriggers(nil); err != nil {
		return fmt.Errorf("nu pot scoate declanșatoarele serviciului de timp Windows: %w", err)
	}
	if err := a.timeService.Start(); err != nil {
		return fmt.Errorf("nu pot porni serviciul de timp Windows: %w", err)
	}
	return nil
}

// restoreByHand gives Windows' time service its usual settings on a PC
// outside a domain back: started on demand, with its domain triggers.
const restoreByHand = "într-o fereastră de administrator: sc config w32time start= demand, apoi sc triggerinfo w32time start/domainjoin stop/domainleave"

// saveTimeService saves the time service's start type and triggers in
// savedStart, unless an install saved them already. A file saved before
// C1-R16b holds the start type only, and that install left the triggers on
// the service: the triggers that it has now are added to the file, which
// keeps its start type, so that uninstall puts both back. With no file, a
// service that starts automatically without triggers while the boot task
// is registered has the probe's settings, from an install whose file is
// gone: they are not saved as the ones from before, and the owner reads
// how to restore Windows' usual ones by hand (mi1-6).
func (a autostart) saveTimeService() error {
	const cannotSave = "nu pot salva setările de dinainte ale serviciului de timp Windows"
	if err := a.protectSavedStart(); err != nil {
		return fmt.Errorf("%s: %w", cannotSave, err)
	}
	saved, found, err := a.loadSavedTimeService()
	switch {
	case err != nil:
		return fmt.Errorf("%s: %w", cannotSave, err)
	case found && saved.Triggers != nil:
		return nil
	case !found:
		if saved.StartType, err = a.timeService.StartType(); err != nil {
			return fmt.Errorf("nu pot citi serviciul de timp Windows: %w", err)
		}
	}
	if saved.Triggers, err = a.timeService.Triggers(); err != nil {
		return fmt.Errorf("nu pot citi declanșatoarele serviciului de timp Windows: %w", err)
	}
	if !found && a.probeSettings(saved) {
		_, _ = fmt.Fprintf(a.out, "Serviciul de timp Windows pornește deja automat și nu are declanșatoare, ca după o instalare a probei, "+
			"dar setările lui de dinainte nu mai sunt salvate: nu le salvez pe acestea în locul lor. După probă, puneți-le la loc %s.\n", restoreByHand)
		return nil
	}
	if saved.Triggers == nil {
		saved.Triggers = []mi1.ServiceTrigger{} // saved as none, not as a file without triggers
	}
	data, err := json.Marshal(saved)
	if err == nil {
		err = os.WriteFile(a.savedStart, append(data, '\n'), 0o600)
	}
	if err == nil { // Windows may give it to the administrator's own account
		own := mi1.AdministratorsOwnCommand(a.savedStart)
		err = a.run(own[0], own[1:]...)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", cannotSave, err)
	}
	return nil
}

// protectSavedStart makes the folder of savedStart and gives it to
// Administrators alone, with a protected DACL (Administrators and SYSTEM):
// %ProgramData% lets any user create a folder in it first, and the elevated
// uninstall applies what the folder holds to the time service.
func (a autostart) protectSavedStart() error {
	dir := filepath.Dir(a.savedStart)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, cmd := range mi1.AdministratorsOnlyCommands(dir) {
		if err := a.run(cmd[0], cmd[1:]...); err != nil {
			return err
		}
	}
	return nil
}

// probeSettings reports whether the time service's settings, saved, are
// the probe's: it starts automatically, without triggers, and the boot
// task is registered.
func (a autostart) probeSettings(saved savedTimeService) bool {
	return saved.StartType == serviceAutoStart && len(saved.Triggers) == 0 && a.run("schtasks", "/Query", "/TN", autostartName) == nil
}

// loadSavedTimeService reads the settings that an install saved; found is
// false when there is no file. A file that Administrators or SYSTEM do not
// own is refused: another account may have put it there before the first
// install.
func (a autostart) loadSavedTimeService() (saved savedTimeService, found bool, err error) {
	data, err := os.ReadFile(a.savedStart)
	if errors.Is(err, os.ErrNotExist) {
		return saved, false, nil
	}
	var owner string
	if err == nil {
		owner, err = a.ownerSID(a.savedStart)
	}
	if err == nil && !mi1.AdministratorsOwned(owner) {
		err = fmt.Errorf("fișierul %s nu aparține administratorilor sau sistemului, deci poate l-a pus acolo alt cont, "+
			"și nu îl folosesc; ștergeți-l dintr-o fereastră de administrator", a.savedStart)
	}
	if err == nil {
		err = json.Unmarshal(data, &saved)
	}
	return saved, err == nil, err
}

// restoreTimeService gives the time service back the start type and the
// triggers that install saved, and removes the file and its folder.
// Without a saved file, install changed nothing, or found the probe's
// settings with nothing saved: then the owner reads again how to restore
// Windows' usual ones by hand, and the empty folder goes. A file saved
// before C1-R16b leaves the triggers as they are.
func (a autostart) restoreTimeService() error {
	saved, found, err := a.loadSavedTimeService()
	if err != nil {
		return fmt.Errorf("nu pot citi setările salvate ale serviciului de timp Windows (%s): %w", filepath.Base(a.savedStart), err)
	}
	if !found {
		now := savedTimeService{}
		if now.StartType, err = a.timeService.StartType(); err == nil {
			now.Triggers, err = a.timeService.Triggers()
		}
		if err == nil && now.StartType == serviceAutoStart && len(now.Triggers) == 0 {
			_, _ = fmt.Fprintf(a.out, "Setările de dinainte ale serviciului de timp Windows nu sunt salvate; ca să-i puneți la loc setările obișnuite: %s.\n", restoreByHand)
		}
		_ = os.Remove(filepath.Dir(a.savedStart)) // the folder that install made, once empty
		return nil
	}
	if err := a.timeService.SetStartType(saved.StartType); err != nil {
		return fmt.Errorf("nu pot pune la loc pornirea serviciului de timp Windows: %w", err)
	}
	if saved.Triggers != nil {
		if err := a.timeService.SetTriggers(saved.Triggers); err != nil {
			return fmt.Errorf("nu pot pune la loc declanșatoarele serviciului de timp Windows: %w", err)
		}
	}
	_, _ = fmt.Fprintln(a.out, "Serviciul de timp Windows are din nou setările de dinainte.")
	if err := os.Remove(a.savedStart); err != nil {
		return fmt.Errorf("nu pot șterge setările salvate ale serviciului de timp Windows: %w", err)
	}
	_ = os.Remove(filepath.Dir(a.savedStart)) // its folder, once empty
	return nil
}
