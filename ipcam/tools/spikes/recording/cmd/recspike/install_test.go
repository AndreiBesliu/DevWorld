package main

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// task is what TestInstall_WindowsTaskXML reads of the task's XML.
type task struct {
	XMLName  xml.Name `xml:"Task"`
	Triggers struct {
		Boot *struct {
			Enabled bool `xml:"Enabled"`
		} `xml:"BootTrigger"`
	} `xml:"Triggers"`
	Principal struct {
		UserID    string `xml:"UserId"`
		LogonType string `xml:"LogonType"`
		RunLevel  string `xml:"RunLevel"`
	} `xml:"Principals>Principal"`
	Exec struct {
		Command          string `xml:"Command"`
		Arguments        string `xml:"Arguments"`
		WorkingDirectory string `xml:"WorkingDirectory"`
	} `xml:"Actions>Exec"`
	Priority *int `xml:"Settings>Priority"`
}

// fakeOS records what install and uninstall ask of the OS.
type fakeOS struct {
	commands [][]string
	elevated []string
	files    map[string][]byte
	links    []string
	time     *fakeService
	// noTask makes `schtasks /Query` fail: no boot task is registered.
	noTask bool
}

// serviceDemandStart is SERVICE_DEMAND_START: W32Time's start type on a PC
// outside a domain.
const serviceDemandStart = 3

// workgroupTriggers are W32Time's triggers on a PC outside a domain: it
// starts when the PC joins a domain, and stops when it is not in one
// (SERVICE_TRIGGER_TYPE_DOMAIN_JOIN with the DOMAIN_JOIN and DOMAIN_LEAVE
// GUIDs).
func workgroupTriggers() []mi1.ServiceTrigger {
	return []mi1.ServiceTrigger{
		{Type: 3, Action: 1, Subtype: "{1CE20ABA-9851-4421-9430-1DDEB766E809}"},
		{Type: 3, Action: 2, Subtype: "{DDAF516E-58C2-4866-9574-C3B615D42EA1}", Data: []mi1.ServiceTriggerData{{Type: 2, Bytes: []byte("a\x00b\x00\x00\x00")}}},
	}
}

// fakeService is Windows' time service in a fake service manager: its start
// type, its triggers, whether it runs, and what was asked of it.
type fakeService struct {
	startType uint32
	triggers  []mi1.ServiceTrigger
	running   bool
	asked     []string
}

func (s *fakeService) StartType() (uint32, error) { return s.startType, nil }

func (s *fakeService) SetStartType(t uint32) error {
	s.asked = append(s.asked, "start type "+strconv.FormatUint(uint64(t), 10))
	s.startType = t
	return nil
}

func (s *fakeService) Triggers() ([]mi1.ServiceTrigger, error) {
	return slices.Clone(s.triggers), nil
}

func (s *fakeService) SetTriggers(triggers []mi1.ServiceTrigger) error {
	s.asked = append(s.asked, "triggers "+strconv.Itoa(len(triggers)))
	s.triggers = slices.Clone(triggers)
	return nil
}

func (s *fakeService) Start() error {
	s.asked = append(s.asked, "start")
	s.running = true
	return nil
}

func newAutostart(t *testing.T, goos string, privileged bool) (autostart, *fakeOS, string) {
	t.Helper()
	f := &fakeOS{files: map[string][]byte{}, time: &fakeService{startType: serviceDemandStart, triggers: workgroupTriggers()}}
	dir := t.TempDir()
	desktop := filepath.Join(dir, "Desktop")
	if err := os.MkdirAll(desktop, 0o750); err != nil {
		t.Fatal(err)
	}
	a := autostart{
		goos:       goos,
		privileged: func() bool { return privileged },
		elevate: func(args []string) (int, error) {
			f.elevated = append(f.elevated, strings.Join(args, " "))
			return 0, nil
		},
		run: func(name string, args ...string) error {
			cmd := append([]string{name}, args...)
			f.commands = append(f.commands, cmd)
			if name == "schtasks" && slices.Contains(args, "/Query") && f.noTask {
				return errors.New("schtasks: exit status 1: the task does not exist")
			}
			if name == "schtasks" && slices.Contains(args, "/XML") {
				data, err := os.ReadFile(args[slices.Index(args, "/XML")+1])
				if err != nil {
					return err
				}
				f.files["task.xml"] = data
			}
			return nil
		},
		userSID: func() (string, error) { return "S-1-5-21-1004336348-1177238915-682003330-1001", nil },
		desktop: func() (string, error) { return desktop, nil },
		shortcut: func(path, target, args, workDir string, runAsAdmin bool) error {
			f.links = append(f.links, strings.Join([]string{path, target, args, workDir, strconv.FormatBool(runAsAdmin)}, "|"))
			return os.WriteFile(path, []byte("lnk"), 0o600)
		},
		unitPath:    filepath.Join(dir, "ipcam-recspike.service"),
		tempDir:     dir,
		timeService: f.time,
		savedStart:  filepath.Join(dir, "ProgramData", "ipcam-recspike", "w32time-start.json"),
		out:         io.Discard,
	}
	return a, f, desktop
}

// TestInstall_WindowsTaskXML (G14): the PC's boot task is registered from
// generated XML, UTF-16 with a BOM as schtasks reads it: a BootTrigger, the
// owner's user with LogonType S4U (logged on or not), RunLevel
// HighestAvailable, `recspike resume` in recspike's folder; and the
// desktop shortcut runs `recspike mi1 --real --resume` there, as
// administrator. Without elevation, recspike asks for it once (UAC) and
// registers nothing itself.
func TestInstall_WindowsTaskXML(t *testing.T) {
	exe := `C:\Users\Ana & Ion\proba\recspike.exe`
	dir := `C:\Users\Ana & Ion\proba`
	var tk task
	dec := xml.NewDecoder(strings.NewReader(taskXML(exe, dir, "S-1-5-21-1")))
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := dec.Decode(&tk); err != nil {
		t.Fatalf("the task's XML: %v", err)
	}
	if tk.Triggers.Boot == nil || !tk.Triggers.Boot.Enabled {
		t.Error("no enabled BootTrigger")
	}
	if tk.Principal.LogonType != "S4U" || tk.Principal.RunLevel != "HighestAvailable" || tk.Principal.UserID != "S-1-5-21-1" {
		t.Errorf("principal = %+v, want the user's SID, S4U and HighestAvailable", tk.Principal)
	}
	if tk.Exec.Command != exe || tk.Exec.Arguments != "resume" || tk.Exec.WorkingDirectory != dir {
		t.Errorf("exec = %+v, want %s resume in %s", tk.Exec, exe, dir)
	}
	// Task Scheduler's default 7 is below normal, which ipcam-hub and
	// MediaMTX would inherit in every run that the boot task records
	// (mi1-5): 5 is the normal class.
	if tk.Priority == nil || *tk.Priority != 5 {
		t.Errorf("priority = %v, want 5 (normal)", tk.Priority)
	}

	a, f, desktop := newAutostart(t, "windows", true)
	if err := a.install(exe); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(f.commands) != 1 || !slices.Equal(f.commands[0][:3], []string{"schtasks", "/Create", "/TN"}) || f.commands[0][3] != "ipcam-recspike" ||
		!slices.Contains(f.commands[0], "/F") {
		t.Errorf("commands = %q, want schtasks /Create /TN ipcam-recspike /XML <file> /F", f.commands)
	}
	data := f.files["task.xml"]
	if !bytes.HasPrefix(data, []byte{0xff, 0xfe}) || len(data)%2 != 0 {
		t.Fatalf("the task file does not start with a UTF-16 LE BOM: % x", data[:min(4, len(data))])
	}
	units := make([]uint16, (len(data)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[2+2*i:])
	}
	if got := string(utf16.Decode(units)); got != taskXML(exe, dir, "S-1-5-21-1004336348-1177238915-682003330-1001") {
		t.Errorf("the task file holds\n%s", got)
	}
	// The shortcut runs as administrator (one UAC prompt each time), like
	// the boot task: w32tm's NTP status needs it (G14).
	want := filepath.Join(desktop, "Proba înregistrare – tăieri reale.lnk") + "|" + exe + "|mi1 --real --resume|" + dir + "|true"
	if !slices.Equal(f.links, []string{want}) {
		t.Errorf("shortcuts = %q, want %q", f.links, want)
	}

	a, f, _ = newAutostart(t, "windows", false)
	if err := a.install(exe); err != nil {
		t.Fatalf("install without elevation: %v", err)
	}
	if !slices.Equal(f.elevated, []string{"install"}) || len(f.commands) != 0 || len(f.links) != 0 {
		t.Errorf("without elevation: elevated %q, commands %q, shortcuts %q; want one elevated install and nothing else", f.elevated, f.commands, f.links)
	}
}

// TestInstall_SystemdUnit (G14): `sudo ./recspike install` writes the Pi's
// boot service, which runs `recspike resume` as root once the network and
// the clock are up, and enables it; without root it refuses in Romanian.
func TestInstall_SystemdUnit(t *testing.T) {
	unit := systemdUnit("/home/pi/proba/recspike")
	for _, line := range []string{
		"User=root",
		"After=network-online.target time-sync.target",
		"ExecStart=/home/pi/proba/recspike resume",
		"WorkingDirectory=/home/pi/proba",
		"WantedBy=multi-user.target",
	} {
		if !slices.Contains(strings.Split(unit, "\n"), line) {
			t.Errorf("the unit lacks %q:\n%s", line, unit)
		}
	}
	if spaced := systemdUnit("/home/pi/proba nouă/recspike"); !strings.Contains(spaced, `ExecStart="/home/pi/proba nouă/recspike" resume`) {
		t.Errorf("a folder with a space is not quoted:\n%s", spaced)
	}

	a, f, _ := newAutostart(t, "linux", true)
	if err := a.install("/home/pi/proba/recspike"); err != nil {
		t.Fatalf("install: %v", err)
	}
	data, err := os.ReadFile(a.unitPath)
	if err != nil || string(data) != unit {
		t.Errorf("the unit file = %q, %v", data, err)
	}
	if want := [][]string{{"systemctl", "daemon-reload"}, {"systemctl", "enable", "ipcam-recspike.service"}}; !slices.EqualFunc(f.commands, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", f.commands, want)
	}

	a, f, _ = newAutostart(t, "linux", false)
	if err := a.install("/home/pi/proba/recspike"); err == nil || !strings.Contains(err.Error(), "sudo ./recspike install") || len(f.commands) != 0 {
		t.Errorf("install without root = %v, commands %q; want a Romanian refusal and nothing run", err, f.commands)
	}
}

// TestUninstall_RemovesTaskUnitAndShortcut (G14): uninstall removes the
// PC's boot task and its desktop shortcut, or the Pi's service, and is
// content when they are already gone.
func TestUninstall_RemovesTaskUnitAndShortcut(t *testing.T) {
	a, f, desktop := newAutostart(t, "windows", true)
	if err := a.install(`C:\proba\recspike.exe`); err != nil {
		t.Fatal(err)
	}
	f.commands = nil
	if err := a.uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if want := [][]string{{"schtasks", "/Query", "/TN", "ipcam-recspike"}, {"schtasks", "/Delete", "/TN", "ipcam-recspike", "/F"}}; !slices.EqualFunc(f.commands, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", f.commands, want)
	}
	if _, err := os.Stat(filepath.Join(desktop, "Proba înregistrare – tăieri reale.lnk")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the shortcut is still there (%v)", err)
	}
	if err := a.uninstall(); err != nil {
		t.Errorf("a second uninstall: %v", err)
	}

	a, f, _ = newAutostart(t, "linux", true)
	if err := a.install("/home/pi/proba/recspike"); err != nil {
		t.Fatal(err)
	}
	f.commands = nil
	if err := a.uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if want := [][]string{{"systemctl", "disable", "ipcam-recspike.service"}, {"systemctl", "daemon-reload"}}; !slices.EqualFunc(f.commands, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", f.commands, want)
	}
	if _, err := os.Stat(a.unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the unit is still there (%v)", err)
	}
	if err := a.uninstall(); err != nil {
		t.Errorf("a second uninstall: %v", err)
	}
}

// TestInstall_TimeServiceStartsAutomatically (C1-R16): on the PC, install
// sets Windows' time service (W32Time) to start automatically and starts
// it, so that the clock syncs after every boot of a PC outside a domain,
// and saves its previous start type in recspike's private folder;
// uninstall puts that start type back. A second install keeps the first
// one's saved type, and an uninstall without one changes nothing. The Pi
// and an unelevated recspike touch no service.
func TestInstall_TimeServiceStartsAutomatically(t *testing.T) {
	a, f, _ := newAutostart(t, "windows", true)
	if err := a.install(`C:\proba\recspike.exe`); err != nil {
		t.Fatalf("install: %v", err)
	}
	if f.time.startType != serviceAutoStart || !f.time.running {
		t.Errorf("after install the time service has start type %d, running %v; want automatic (%d) and running", f.time.startType, f.time.running, serviceAutoStart)
	}
	if err := a.install(`C:\proba\recspike.exe`); err != nil {
		t.Fatalf("install again: %v", err)
	}
	f.time.asked = nil
	if err := a.uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if want := []string{"start type 3", "triggers 2"}; !slices.Equal(f.time.asked, want) || f.time.startType != serviceDemandStart {
		t.Errorf("uninstall asked %q of the time service, start type %d; want %q: the type and the triggers before the first install", f.time.asked, f.time.startType, want)
	}
	if _, err := os.Stat(a.savedStart); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the saved start type is still there (%v)", err)
	}
	f.time.asked = nil
	if err := a.uninstall(); err != nil || len(f.time.asked) != 0 {
		t.Errorf("a second uninstall = %v, asked %q of the time service; want nothing", err, f.time.asked)
	}

	for _, tc := range []struct {
		goos       string
		privileged bool
	}{{"linux", true}, {"windows", false}} {
		a, f, _ := newAutostart(t, tc.goos, tc.privileged)
		_ = a.install("/home/pi/proba/recspike")
		_ = a.uninstall()
		if len(f.time.asked) != 0 {
			t.Errorf("%s, privileged %v: asked %q of the time service, want nothing", tc.goos, tc.privileged, f.time.asked)
		}
	}
}

// TestInstall_TimeServiceTriggersRemovedThenRestored (C1-R16b): on a PC
// outside a domain, W32Time's "not in a domain" trigger stops it after a
// boot even when it starts automatically. So install also saves its
// triggers in recspike's private folder and removes them, before it starts
// the service; uninstall puts the start type and the triggers back exactly,
// data items included. A second install keeps what the first one saved:
// by then the service has no triggers. A file saved before C1-R16b holds no
// triggers, and uninstall then leaves the service's triggers as they are.
func TestInstall_TimeServiceTriggersRemovedThenRestored(t *testing.T) {
	a, f, _ := newAutostart(t, "windows", true)
	if err := a.install(`C:\proba\recspike.exe`); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(f.time.triggers) != 0 || f.time.startType != serviceAutoStart || !f.time.running {
		t.Errorf("after install: triggers %+v, start type %d, running %v; want no trigger, automatic and running", f.time.triggers, f.time.startType, f.time.running)
	}
	if i, j := slices.Index(f.time.asked, "triggers 0"), slices.Index(f.time.asked, "start"); i < 0 || j < i {
		t.Errorf("install asked %q of the time service, want the triggers removed before the start", f.time.asked)
	}
	if err := a.install(`C:\proba\recspike.exe`); err != nil {
		t.Fatalf("install again: %v", err)
	}
	if err := a.uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !reflect.DeepEqual(f.time.triggers, workgroupTriggers()) || f.time.startType != serviceDemandStart {
		t.Errorf("after uninstall: triggers %+v, start type %d; want the triggers %+v and the start type %d of before",
			f.time.triggers, f.time.startType, workgroupTriggers(), serviceDemandStart)
	}

	a, f, _ = newAutostart(t, "windows", true)
	if err := os.MkdirAll(filepath.Dir(a.savedStart), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.savedStart, []byte(`{"startType":3}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.time.startType, f.time.triggers = serviceAutoStart, workgroupTriggers()[:1]
	if err := a.uninstall(); err != nil {
		t.Fatalf("uninstall of a file without triggers: %v", err)
	}
	if want := []string{"start type 3"}; !slices.Equal(f.time.asked, want) || len(f.time.triggers) != 1 {
		t.Errorf("uninstall of a file without triggers asked %q, left triggers %+v; want %q and the trigger kept", f.time.asked, f.time.triggers, want)
	}
}

// TestInstall_OverAFileWithoutTriggers_KeepsThemForUninstall (Task 7.2c
// review): an install from before C1-R16b saved the start type only and
// left the triggers on the service. An install over that file removes the
// triggers, so it first adds them to the file, which keeps its start type:
// uninstall then puts back both, as they were before the first install.
func TestInstall_OverAFileWithoutTriggers_KeepsThemForUninstall(t *testing.T) {
	a, f, _ := newAutostart(t, "windows", true)
	if err := os.MkdirAll(filepath.Dir(a.savedStart), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.savedStart, []byte(`{"startType":3}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.time.startType = serviceAutoStart // the old install's
	if err := a.install(`C:\proba\recspike.exe`); err != nil {
		t.Fatalf("install over a file without triggers: %v", err)
	}
	if len(f.time.triggers) != 0 {
		t.Errorf("after install: triggers %+v, want none", f.time.triggers)
	}
	if err := a.uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !reflect.DeepEqual(f.time.triggers, workgroupTriggers()) || f.time.startType != serviceDemandStart {
		t.Errorf("after uninstall: triggers %+v, start type %d; want the triggers %+v and the start type %d of before the first install",
			f.time.triggers, f.time.startType, workgroupTriggers(), serviceDemandStart)
	}
}

// TestInstall_TimeServiceSettingsOutliveTheReleaseFolder (mi1-6): the time
// service's settings from before the first install are kept outside the
// release folder, under %ProgramData%\ipcam-recspike, so that an owner who
// deletes the folder and extracts a new release there installs again
// without losing them: uninstall still puts back the settings of before
// the first install, not the probe's.
func TestInstall_TimeServiceSettingsOutliveTheReleaseFolder(t *testing.T) {
	programData := filepath.Join(t.TempDir(), "ProgramData")
	t.Setenv("ProgramData", programData)
	release := filepath.Join(t.TempDir(), "proba-inregistrare")
	saved := systemAutostart(io.Discard).savedStart
	if want := filepath.Join(programData, "ipcam-recspike", "w32time-start.json"); saved != want {
		t.Fatalf("the settings are saved at %s, want %s", saved, want)
	}
	a, f, _ := newAutostart(t, "windows", true)
	a.savedStart = saved
	if err := a.install(filepath.Join(release, "recspike.exe")); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := os.RemoveAll(release); err != nil { // a new release in a fresh folder
		t.Fatal(err)
	}
	if err := a.install(filepath.Join(release, "recspike.exe")); err != nil {
		t.Fatalf("install again: %v", err)
	}
	if err := a.uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !reflect.DeepEqual(f.time.triggers, workgroupTriggers()) || f.time.startType != serviceDemandStart {
		t.Errorf("after uninstall: triggers %+v, start type %d; want the triggers %+v and the start type %d of before the first install",
			f.time.triggers, f.time.startType, workgroupTriggers(), serviceDemandStart)
	}
	if _, err := os.Stat(filepath.Dir(saved)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s is still there after uninstall (%v)", filepath.Dir(saved), err)
	}
}

// TestInstall_ProbesSettingsAreNeverSavedAsTheOriginal (mi1-6): with no
// saved settings, a time service that already starts automatically
// without triggers while the boot task is registered carries the probe's
// own settings, from an install whose saved settings are gone: install
// does not save them as the ones from before, says so in Romanian, and
// gives the two sc commands that restore Windows' usual ones, which
// uninstall gives again. The same settings without a boot task are the
// owner's own, and are saved.
func TestInstall_ProbesSettingsAreNeverSavedAsTheOriginal(t *testing.T) {
	const restore = "sc config w32time start= demand, apoi sc triggerinfo w32time start/domainjoin stop/domainleave"
	a, f, _ := newAutostart(t, "windows", true)
	f.time.startType, f.time.triggers = serviceAutoStart, nil
	var out strings.Builder
	a.out = &out
	if err := a.install(`C:\proba-inregistrare\recspike.exe`); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(a.savedStart); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the probe's settings were saved as the original ones (%v)", err)
	}
	if !strings.Contains(out.String(), "nu le salvez") || !strings.Contains(out.String(), restore) {
		t.Errorf("install printed:\n%s\nwant the Romanian note and %q", out.String(), restore)
	}
	if f.time.startType != serviceAutoStart || len(f.time.triggers) != 0 || !f.time.running {
		t.Errorf("after install: start type %d, triggers %+v, running %v; want the probe's settings, running", f.time.startType, f.time.triggers, f.time.running)
	}
	out.Reset()
	f.time.asked = nil
	if err := a.uninstall(); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(f.time.asked) != 0 || !strings.Contains(out.String(), restore) || strings.Contains(out.String(), "din nou setările de dinainte") {
		t.Errorf("uninstall asked %q of the time service and printed:\n%s\nwant nothing changed, and %q", f.time.asked, out.String(), restore)
	}

	a, f, _ = newAutostart(t, "windows", true)
	f.time.startType, f.time.triggers, f.noTask = serviceAutoStart, nil, true
	if err := a.install(`C:\proba-inregistrare\recspike.exe`); err != nil {
		t.Fatalf("install without a boot task: %v", err)
	}
	if _, err := os.Stat(a.savedStart); err != nil {
		t.Errorf("the owner's own settings were not saved: %v", err)
	}
}

// TestInstall_TimeServiceSettingsErrorsAreRomanian (item 9): when install
// cannot save the time service's settings, or uninstall cannot read them
// back, the owner reads why in Romanian, with the system's error after it.
func TestInstall_TimeServiceSettingsErrorsAreRomanian(t *testing.T) {
	a, f, _ := newAutostart(t, "windows", true)
	folder := filepath.Dir(a.savedStart)
	if err := os.MkdirAll(filepath.Dir(folder), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(folder, nil, 0o600); err != nil { // the settings' folder is a file: nothing can be saved in it
		t.Fatal(err)
	}
	err := a.install(`C:\proba\recspike.exe`)
	if err == nil || !strings.HasPrefix(err.Error(), "nu pot salva setările de dinainte ale serviciului de timp Windows") {
		t.Errorf("install that cannot save = %v, want a Romanian line first", err)
	}
	if len(f.time.asked) != 0 {
		t.Errorf("install that cannot save asked %q of the time service, want nothing changed", f.time.asked)
	}

	a, f, _ = newAutostart(t, "windows", true)
	if err := os.MkdirAll(filepath.Dir(a.savedStart), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.savedStart, []byte("{start"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = a.uninstall()
	if err == nil || !strings.HasPrefix(err.Error(), "nu pot citi setările salvate ale serviciului de timp Windows") {
		t.Errorf("uninstall of an unreadable file = %v, want a Romanian line first", err)
	}
	if len(f.time.asked) != 0 {
		t.Errorf("uninstall of an unreadable file asked %q of the time service, want nothing changed", f.time.asked)
	}
}
