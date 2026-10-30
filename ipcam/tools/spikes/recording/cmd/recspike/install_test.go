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
	// dirs are the folders that install made; owners the owner and mode
	// changes, as "path uid:gid" and "path mode".
	dirs   []string
	owners []string
	// failICacls makes every icacls command fail.
	failICacls bool
	// protected are the folders that install gave to Administrators and
	// SYSTEM alone, and written the files that it wrote anew there.
	protected, written []string
	// owner is the owner's SID of every file, as ownerSID reads it.
	owner string
	// myFault makes `sc query MyFault` succeed: NotMyFault's driver
	// service is registered.
	myFault bool
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
	f := &fakeOS{files: map[string][]byte{}, time: &fakeService{startType: serviceDemandStart, triggers: workgroupTriggers()},
		owner: "S-1-5-32-544"}
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
			if name == "icacls" && f.failICacls {
				return errors.New("icacls: exit status 5: Access is denied")
			}
			if name == "schtasks" && slices.Contains(args, "/Query") && f.noTask {
				return errors.New("schtasks: exit status 1: the task does not exist")
			}
			if name == "sc" && args[0] == "query" && !f.myFault {
				return errors.New("sc: exit status 1060: the service does not exist")
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
		savedModes:  filepath.Join(dir, "var", "lib", "ipcam-recspike", "release-modes.json"),
		memoryDump:  filepath.Join(dir, "Windows", "MEMORY.DMP"),
		hubConfig:   filepath.Join(dir, "ProgramData", "ipcam-recspike", mi1.HubConfigName),
		ownerSID:    func(string) (string, error) { return f.owner, nil },
		protect: func(dir string) error {
			f.protected = append(f.protected, dir)
			return os.MkdirAll(dir, 0o700)
		},
		writeProtected: func(path string, data []byte) error {
			f.written = append(f.written, path)
			return os.WriteFile(path, data, 0o600)
		},
		mkdirAll: func(path string) error {
			f.dirs = append(f.dirs, path)
			return nil
		},
		chown: func(path string, uid, gid int) error {
			f.owners = append(f.owners, path+" "+strconv.Itoa(uid)+":"+strconv.Itoa(gid))
			return nil
		},
		chmod: func(path string, mode os.FileMode) error {
			f.owners = append(f.owners, path+" "+mode.String())
			return nil
		},
		out: io.Discard,
	}
	return a, f, desktop
}

// onRelease is the commands of cmds on the release folder of the PC's
// tests, C:\proba-inregistrare, and what is in it.
func onRelease(cmds [][]string) [][]string {
	var out [][]string
	for _, c := range cmds {
		if len(c) > 1 && strings.HasPrefix(c[1], `C:\proba-inregistrare`) {
			out = append(out, c)
		}
	}
	return out
}

// modesOf is the permission bits of each path, as the fake chmod records
// them: on Linux the modes that the test gave, on Windows, where the Pi's
// tests run too, the ones that Go makes up.
func modesOf(t *testing.T, paths ...string) map[string]string {
	t.Helper()
	modes := map[string]string{}
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		modes[p] = info.Mode().Perm().String()
	}
	return modes
}

// named is the commands of cmds that run name.
func named(cmds [][]string, name string) [][]string {
	var out [][]string
	for _, c := range cmds {
		if c[0] == name {
			out = append(out, c)
		}
	}
	return out
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
	if tasks := named(f.commands, "schtasks"); len(tasks) != 1 || !slices.Equal(tasks[0][:3], []string{"schtasks", "/Create", "/TN"}) || tasks[0][3] != "ipcam-recspike" ||
		!slices.Contains(tasks[0], "/F") {
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
	// The config that the owner's MI1 runs started ipcam-hub with, which
	// names the session's camera addresses, goes too.
	hubConfigLeft := func(t *testing.T, a autostart) {
		t.Helper()
		if _, err := os.Stat(a.hubConfig); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("ipcam-hub's config is still there (%v)", err)
		}
	}
	writeHubConfig := func(t *testing.T, a autostart) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(a.hubConfig), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(a.hubConfig, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a, f, desktop := newAutostart(t, "windows", true)
	if err := a.install(`C:\proba\recspike.exe`); err != nil {
		t.Fatal(err)
	}
	writeHubConfig(t, a)
	f.commands = nil
	if err := a.uninstall(`C:\proba\recspike.exe`); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	hubConfigLeft(t, a)
	if want := [][]string{{"schtasks", "/Query", "/TN", "ipcam-recspike"}, {"schtasks", "/Delete", "/TN", "ipcam-recspike", "/F"},
		{"icacls", `C:\proba`, "/reset", "/T", "/C", "/Q"}, {"sc", "query", "MyFault"}}; !slices.EqualFunc(f.commands, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", f.commands, want)
	}
	if _, err := os.Stat(filepath.Join(desktop, "Proba înregistrare – tăieri reale.lnk")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the shortcut is still there (%v)", err)
	}
	if err := a.uninstall(`C:\proba\recspike.exe`); err != nil {
		t.Errorf("a second uninstall: %v", err)
	}

	a, f, _ = newAutostart(t, "linux", true)
	if err := a.install("/home/pi/proba/recspike"); err != nil {
		t.Fatal(err)
	}
	writeHubConfig(t, a)
	f.commands = nil
	if err := a.uninstall("/home/pi/proba/recspike"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	hubConfigLeft(t, a)
	if want := [][]string{{"systemctl", "disable", "ipcam-recspike.service"}, {"systemctl", "daemon-reload"}}; !slices.EqualFunc(f.commands, want, slices.Equal) {
		t.Errorf("commands = %q, want %q", f.commands, want)
	}
	if _, err := os.Stat(a.unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the unit is still there (%v)", err)
	}
	if err := a.uninstall("/home/pi/proba/recspike"); err != nil {
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
	if err := a.uninstall(`C:\proba\recspike.exe`); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if want := []string{"start type 3", "triggers 2"}; !slices.Equal(f.time.asked, want) || f.time.startType != serviceDemandStart {
		t.Errorf("uninstall asked %q of the time service, start type %d; want %q: the type and the triggers before the first install", f.time.asked, f.time.startType, want)
	}
	if _, err := os.Stat(a.savedStart); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the saved start type is still there (%v)", err)
	}
	f.time.asked = nil
	if err := a.uninstall(`C:\proba\recspike.exe`); err != nil || len(f.time.asked) != 0 {
		t.Errorf("a second uninstall = %v, asked %q of the time service; want nothing", err, f.time.asked)
	}

	for _, tc := range []struct {
		goos       string
		privileged bool
	}{{"linux", true}, {"windows", false}} {
		a, f, _ := newAutostart(t, tc.goos, tc.privileged)
		_ = a.install("/home/pi/proba/recspike")
		_ = a.uninstall("/home/pi/proba/recspike")
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
	if err := a.uninstall(`C:\proba\recspike.exe`); err != nil {
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
	if err := a.uninstall(`C:\proba\recspike.exe`); err != nil {
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
	if err := a.uninstall(`C:\proba\recspike.exe`); err != nil {
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
	// Settings that an older release saved in its own work folder, the
	// probe's (automatic, no trigger): install never reads them there.
	stale := filepath.Join(release, "work", "w32time-start.json")
	if err := os.MkdirAll(filepath.Dir(stale), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte(`{"startType":2,"triggers":[]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, f, _ := newAutostart(t, "windows", true)
	a.savedStart = saved
	if err := a.install(filepath.Join(release, "recspike.exe")); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := os.RemoveAll(release); err != nil { // a new release in a fresh folder
		t.Fatal(err)
	}
	if _, err := os.Stat(release); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the old release folder is still there (%v)", err)
	}
	if err := a.install(filepath.Join(release, "recspike.exe")); err != nil {
		t.Fatalf("install again: %v", err)
	}
	if err := a.uninstall(filepath.Join(release, "recspike.exe")); err != nil {
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
	if err := a.uninstall(`C:\proba-inregistrare\recspike.exe`); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(f.time.asked) != 0 || !strings.Contains(out.String(), restore) || strings.Contains(out.String(), "din nou setările de dinainte") {
		t.Errorf("uninstall asked %q of the time service and printed:\n%s\nwant nothing changed, and %q", f.time.asked, out.String(), restore)
	}
	if _, err := os.Stat(filepath.Dir(a.savedStart)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the empty folder of the settings is still there after uninstall (%v)", err)
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
	err = a.uninstall(`C:\proba\recspike.exe`)
	if err == nil || !strings.HasPrefix(err.Error(), "nu pot citi setările salvate ale serviciului de timp Windows") {
		t.Errorf("uninstall of an unreadable file = %v, want a Romanian line first", err)
	}
	if len(f.time.asked) != 0 {
		t.Errorf("uninstall of an unreadable file asked %q of the time service, want nothing changed", f.time.asked)
	}
}

// TestInstall_WindowsProtectsTheReleaseFolder (security-1): the boot task,
// the real cuts' shortcut and the bugcheck run the release's programs
// elevated, and a folder in the root of C: lets every authenticated user
// change them. So install, elevated, first makes the data folders and
// gives the release folder a protected DACL without inheritance:
// Administrators and SYSTEM Full, the owner's user Read & Execute, OWNER
// RIGHTS Read & Execute (the owner extracted the files, and would
// otherwise keep the right to change their DACL), and the owner's user
// Modify on results and work, which the programs write. Only then does it
// register the boot task. Uninstall resets the folder and everything in it
// to the permissions they inherit, so that the owner can delete it.
func TestInstall_WindowsProtectsTheReleaseFolder(t *testing.T) {
	const sid = "S-1-5-21-1004336348-1177238915-682003330-1001"
	a, f, _ := newAutostart(t, "windows", true)
	var out strings.Builder
	a.out = &out
	if err := a.install(`C:\proba-inregistrare\recspike.exe`); err != nil {
		t.Fatalf("install: %v", err)
	}
	if want := []string{`C:\proba-inregistrare\results`, `C:\proba-inregistrare\work`}; !slices.Equal(f.dirs, want) {
		t.Errorf("install made the folders %q, want %q", f.dirs, want)
	}
	want := [][]string{
		{"icacls", `C:\proba-inregistrare`, "/inheritance:r", "/grant:r",
			"*S-1-5-32-544:(OI)(CI)F", "*S-1-5-18:(OI)(CI)F", "*" + sid + ":(OI)(CI)RX", "*S-1-3-4:(OI)(CI)RX"},
		{"icacls", `C:\proba-inregistrare\results`, "/grant:r", "*" + sid + ":(OI)(CI)M"},
		{"icacls", `C:\proba-inregistrare\work`, "/grant:r", "*" + sid + ":(OI)(CI)M"},
	}
	if got := onRelease(named(f.commands, "icacls")); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("icacls commands = %q, want %q", got, want)
	}
	if i, j := slices.IndexFunc(f.commands, func(c []string) bool { return c[0] == "icacls" }),
		slices.IndexFunc(f.commands, func(c []string) bool { return c[0] == "schtasks" }); i < 0 || j < i {
		t.Errorf("commands = %q, want the folder protected before the boot task is registered", f.commands)
	}
	if !strings.Contains(out.String(), "Folderul probei poate fi schimbat acum doar de un administrator") {
		t.Errorf("install printed:\n%s\nwant the Romanian line about the folder", out.String())
	}

	f.commands = nil
	out.Reset()
	if err := a.uninstall(`C:\proba-inregistrare\recspike.exe`); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if got, want := onRelease(named(f.commands, "icacls")), [][]string{{"icacls", `C:\proba-inregistrare`, "/reset", "/T", "/C", "/Q"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("uninstall's icacls commands = %q, want %q", got, want)
	}
	if !strings.Contains(out.String(), "Folderul probei are din nou permisiunile de dinainte") {
		t.Errorf("uninstall printed:\n%s\nwant the Romanian line about the folder", out.String())
	}
}

// TestInstall_RefusesWhenTheFolderStaysOpen (security-1): when the release
// folder cannot be protected, install refuses in Romanian, with the
// system's error after it, and installs nothing: no boot task, no
// shortcut, no change to the time service. An uninstall that cannot reset
// the folder says so too.
func TestInstall_RefusesWhenTheFolderStaysOpen(t *testing.T) {
	a, f, _ := newAutostart(t, "windows", true)
	f.failICacls = true
	err := a.install(`C:\proba-inregistrare\recspike.exe`)
	if err == nil || !strings.HasPrefix(err.Error(), "nu pot proteja folderul probei") || !strings.Contains(err.Error(), "Access is denied") {
		t.Errorf("install over an unprotected folder = %v, want a Romanian refusal with the system's error", err)
	}
	if len(named(f.commands, "schtasks")) != 0 || len(f.links) != 0 || len(f.time.asked) != 0 {
		t.Errorf("after the refusal: commands %q, shortcuts %q, time service %q; want nothing installed", f.commands, f.links, f.time.asked)
	}
	if err := a.uninstall(`C:\proba-inregistrare\recspike.exe`); err == nil || !strings.HasPrefix(err.Error(), "nu pot pune la loc permisiunile folderului probei") {
		t.Errorf("uninstall that cannot reset the folder = %v, want a Romanian line first", err)
	}
}

// TestInstall_PiReleaseBelongsToRoot (security-1): the Pi's boot service
// runs recspike as root, and recspike runs ipcam-hub and MediaMTX, so
// `sudo ./recspike install` gives root the release folder and its
// programs, mode 0755, before it writes the unit; the data folders stay as
// they are, for what writes them. A program that is a symbolic link is
// refused, and so is a folder whose owner cannot change. `sudo ./recspike
// uninstall` gives the folder and the programs back to the account that
// ran sudo, with their modes of before.
func TestInstall_PiReleaseBelongsToRoot(t *testing.T) {
	release := t.TempDir()
	if err := os.Chmod(release, 0o750); err != nil { //nolint:gosec // the folder's mode of before install
		t.Fatal(err)
	}
	exe := filepath.Join(release, "recspike")
	for _, name := range []string{"recspike", "reccontroller", "ipcam-hub", "mediamtx"} {
		if err := os.WriteFile(filepath.Join(release, name), []byte("ELF"), 0o755); err != nil { //nolint:gosec // a program's stand-in
			t.Fatal(err)
		}
	}
	for _, data := range []string{"results", "work", "media"} {
		if err := os.MkdirAll(filepath.Join(release, data), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	before := modesOf(t, release, exe, filepath.Join(release, "reccontroller"), filepath.Join(release, "ipcam-hub"), filepath.Join(release, "mediamtx"))
	a, f, _ := newAutostart(t, "linux", true)
	var out strings.Builder
	a.out = &out
	if err := a.install(exe); err != nil {
		t.Fatalf("install: %v", err)
	}
	var want []string
	for _, p := range []string{release, exe, filepath.Join(release, "reccontroller"), filepath.Join(release, "ipcam-hub"), filepath.Join(release, "mediamtx")} {
		want = append(want, p+" 0:0", p+" -rwxr-xr-x")
	}
	if !slices.Equal(f.owners, want) {
		t.Errorf("install changed\n%q\nwant\n%q", f.owners, want)
	}
	if !strings.Contains(out.String(), "Folderul probei și programele din el sunt acum ale lui root") {
		t.Errorf("install printed:\n%s\nwant the Romanian line about the folder", out.String())
	}

	f.owners = nil
	out.Reset()
	t.Setenv("SUDO_UID", "1000")
	t.Setenv("SUDO_GID", "1001")
	if err := a.uninstall(exe); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	// The modes of before first, while root owns the folder, then the
	// owner.
	want = nil
	for _, p := range []string{release, exe, filepath.Join(release, "reccontroller"), filepath.Join(release, "ipcam-hub"), filepath.Join(release, "mediamtx")} {
		want = append(want, p+" "+before[p])
	}
	for _, p := range []string{release, exe, filepath.Join(release, "reccontroller"), filepath.Join(release, "ipcam-hub"), filepath.Join(release, "mediamtx")} {
		want = append(want, p+" 1000:1001")
	}
	if !slices.Equal(f.owners, want) || !strings.Contains(out.String(), "sunt din nou ale contului dvs.") {
		t.Errorf("uninstall changed %q and printed:\n%s\nwant %q and the Romanian line", f.owners, out.String(), want)
	}

	if err := os.Remove(filepath.Join(release, "mediamtx")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/shadow", filepath.Join(release, "mediamtx")); err != nil {
		t.Fatal(err)
	}
	a, f, _ = newAutostart(t, "linux", true)
	if err := a.install(exe); err == nil || !strings.HasPrefix(err.Error(), "nu pot da folderul probei lui root") || len(f.owners) != 0 || len(f.commands) != 0 {
		t.Errorf("install with a linked program = %v, changed %q, ran %q; want a Romanian refusal and nothing done", err, f.owners, f.commands)
	}
	if _, err := os.Stat(a.unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the unit was written although the release stays the user's (%v)", err)
	}

	a, f, _ = newAutostart(t, "linux", true)
	a.chown = func(string, int, int) error { return errors.New("operation not permitted") }
	if err := a.install(filepath.Join(t.TempDir(), "recspike")); err == nil || !strings.HasPrefix(err.Error(), "nu pot da folderul probei lui root") ||
		!strings.Contains(err.Error(), "operation not permitted") || len(f.commands) != 0 {
		t.Errorf("install whose chown fails = %v, ran %q; want a Romanian refusal with the system's error, and no unit", err, f.commands)
	}
}

// TestInstall_TimeServiceSettingsAreAdministrators (Final fix A review,
// Minor 2; Final fix C review, I2): %ProgramData%\ipcam-recspike inherits
// ProgramData's ACL, where any user may create a folder and a file first.
// So install gives the folder to Administrators, with a protected DACL of
// Administrators and SYSTEM alone (mi1.ProtectConfigDir), before it saves
// the settings there in a new file that only they may change
// (mi1.WriteProtected); and install and uninstall refuse a saved file that
// Administrators or SYSTEM do not own: settings that another account
// planted are never applied to the time service.
func TestInstall_TimeServiceSettingsAreAdministrators(t *testing.T) {
	a, f, _ := newAutostart(t, "windows", true)
	if err := a.install(`C:\proba-inregistrare\recspike.exe`); err != nil {
		t.Fatalf("install: %v", err)
	}
	if folder := filepath.Dir(a.savedStart); !slices.Equal(f.protected, []string{folder}) {
		t.Errorf("install protected %q, want %s", f.protected, folder)
	}
	if !slices.Equal(f.written, []string{a.savedStart}) {
		t.Errorf("install wrote %q as Administrators' own, want the saved settings %s", f.written, a.savedStart)
	}
	if got := named(f.commands, "icacls"); slices.ContainsFunc(got, func(c []string) bool { return strings.HasPrefix(c[1], filepath.Dir(a.savedStart)) }) {
		t.Errorf("install ran icacls on the settings' folder: %q", got)
	}

	for _, owner := range []string{"S-1-5-21-1004336348-1177238915-682003330-1002", ""} {
		a, f, _ := newAutostart(t, "windows", true)
		if err := os.MkdirAll(filepath.Dir(a.savedStart), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(a.savedStart, []byte(`{"startType":2,"triggers":[]}`+"\n"), 0o600); err != nil { // planted
			t.Fatal(err)
		}
		f.owner = owner
		if err := a.install(`C:\proba-inregistrare\recspike.exe`); err == nil ||
			!strings.HasPrefix(err.Error(), "nu pot salva setările de dinainte ale serviciului de timp Windows") || !strings.Contains(err.Error(), "administrator") {
			t.Errorf("owner %q: install over a planted file = %v, want a Romanian refusal", owner, err)
		}
		if len(f.time.asked) != 0 || len(named(f.commands, "schtasks")) != 0 {
			t.Errorf("owner %q: after the refusal, time service %q, commands %q; want nothing installed", owner, f.time.asked, f.commands)
		}
		if err := a.uninstall(`C:\proba-inregistrare\recspike.exe`); err == nil ||
			!strings.HasPrefix(err.Error(), "nu pot citi setările salvate ale serviciului de timp Windows") {
			t.Errorf("owner %q: uninstall with a planted file = %v, want a Romanian refusal", owner, err)
		}
		if len(f.time.asked) != 0 {
			t.Errorf("owner %q: uninstall applied the planted settings: %q", owner, f.time.asked)
		}
	}
}

// TestUninstall_PiRestoresTheModesOfBefore (Final fix A review, Minor 9):
// `sudo ./recspike install` gives the Pi's release folder and programs
// mode 0755, and keeps their modes of before where only root writes;
// uninstall puts them back with their ownership. An uninstall with no
// boot service installed changes nothing in the folder.
func TestUninstall_PiRestoresTheModesOfBefore(t *testing.T) {
	release := filepath.Join(t.TempDir(), "proba")
	if err := os.MkdirAll(release, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(release, 0o750); err != nil { //nolint:gosec // the folder's mode of before install, whatever the umask
		t.Fatal(err)
	}
	exe := filepath.Join(release, "recspike")
	for name, mode := range map[string]os.FileMode{"recspike": 0o755, "ipcam-hub": 0o700} {
		if err := os.WriteFile(filepath.Join(release, name), []byte("ELF"), mode); err != nil {
			t.Fatal(err)
		}
	}
	before := modesOf(t, release, exe, filepath.Join(release, "ipcam-hub"))
	t.Setenv("SUDO_UID", "1000")
	t.Setenv("SUDO_GID", "1001")

	a, f, _ := newAutostart(t, "linux", true)
	if err := a.uninstall(exe); err != nil {
		t.Fatalf("uninstall without a boot service: %v", err)
	}
	if len(f.owners) != 0 {
		t.Errorf("uninstall without a boot service changed %q, want nothing", f.owners)
	}

	if err := a.install(exe); err != nil {
		t.Fatalf("install: %v", err)
	}
	// What the fake chmod recorded, for real: the release's modes are now
	// the probe's, which an install again must not keep as those of before.
	for _, p := range []string{release, exe, filepath.Join(release, "ipcam-hub")} {
		if err := os.Chmod(p, 0o755); err != nil { //nolint:gosec // the mode that install gives the release
			t.Fatal(err)
		}
	}
	if err := a.install(exe); err != nil { // again: the modes of before stay those of the first
		t.Fatalf("install again: %v", err)
	}
	f.owners = nil
	if err := a.uninstall(exe); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	for _, p := range []string{release, exe, filepath.Join(release, "ipcam-hub")} {
		change := p + " " + before[p]
		if !slices.Contains(f.owners, change) {
			t.Errorf("uninstall changed %q, want %q among them", f.owners, change)
		}
	}
	if _, err := os.Stat(a.savedModes); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the modes of before are still kept after uninstall (%v)", err)
	}
}

// TestUninstall_CrashDumpsAndNotMyFaultsService (mi1-7, security-3): the
// bugchecks leave a kernel dump, MEMORY.DMP, often of several GB, and
// NotMyFault registers its MyFault driver as a service. Uninstall removes
// that service when it is there, and when MEMORY.DMP is there it says so
// and points to the guide's step that deletes it.
func TestUninstall_CrashDumpsAndNotMyFaultsService(t *testing.T) {
	for _, left := range []bool{true, false} {
		a, f, _ := newAutostart(t, "windows", true)
		if err := a.install(`C:\proba\recspike.exe`); err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		a.out, f.commands, f.myFault = &out, nil, left
		if left {
			if err := os.MkdirAll(filepath.Dir(a.memoryDump), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(a.memoryDump, []byte("PAGEDU64"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := a.uninstall(`C:\proba\recspike.exe`); err != nil {
			t.Fatalf("left %v: uninstall: %v", left, err)
		}
		sc := named(f.commands, "sc")
		want := [][]string{{"sc", "query", "MyFault"}}
		if left {
			want = append(want, []string{"sc", "stop", "MyFault"}, []string{"sc", "delete", "MyFault"})
		}
		if !slices.EqualFunc(sc, want, slices.Equal) {
			t.Errorf("left %v: sc commands = %q, want %q", left, sc, want)
		}
		text := out.String()
		for _, line := range []string{"Am șters serviciul MyFault", "MEMORY.DMP", "pasul 14.3"} {
			if strings.Contains(text, line) != left {
				t.Errorf("left %v: uninstall printed:\n%s\nwant %q only when NotMyFault left it", left, text, line)
			}
		}
	}
}

// TestUninstall_PiAfterAFailedInstallGivesTheFolderBack (Final fix B,
// concern on item 15): an install that gave the release folder to root
// and then could not write the boot service leaves no unit, but its saved
// record of the modes of before: uninstall restores the folder to the
// owner's account whenever that record exists, with or without a unit.
func TestUninstall_PiAfterAFailedInstallGivesTheFolderBack(t *testing.T) {
	release := filepath.Join(t.TempDir(), "proba")
	if err := os.MkdirAll(release, 0o750); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(release, "recspike")
	if err := os.WriteFile(exe, []byte("ELF"), 0o755); err != nil { //nolint:gosec // a program of the release
		t.Fatal(err)
	}
	t.Setenv("SUDO_UID", "1000")
	t.Setenv("SUDO_GID", "1001")
	a, f, _ := newAutostart(t, "linux", true)
	a.unitPath = filepath.Join(t.TempDir(), "no-such-folder", "ipcam-recspike.service")
	if err := a.install(exe); err == nil {
		t.Fatal("install wrote a unit where it cannot")
	}
	if !slices.Contains(f.owners, release+" 0:0") {
		t.Fatalf("install changed %q, want the folder given to root before the unit", f.owners)
	}
	var out strings.Builder
	a.out, f.owners = &out, nil
	if err := a.uninstall(exe); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !slices.Contains(f.owners, release+" 1000:1001") || !strings.Contains(out.String(), "Folderul probei și programele din el sunt din nou ale contului dvs.") {
		t.Errorf("uninstall changed %q and printed\n%s\nwant the folder given back to 1000:1001", f.owners, out.String())
	}
	if _, err := os.Stat(a.savedModes); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the record of the modes of before is still there (%v)", err)
	}
}
