package main

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
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
}

// fakeOS records what install and uninstall ask of the OS.
type fakeOS struct {
	commands [][]string
	elevated []string
	files    map[string][]byte
	links    []string
	time     *fakeService
}

// serviceDemandStart is SERVICE_DEMAND_START: W32Time's start type on a PC
// outside a domain.
const serviceDemandStart = 3

// fakeService is Windows' time service in a fake service manager: its start
// type, whether it runs, and what was asked of it.
type fakeService struct {
	startType uint32
	running   bool
	asked     []string
}

func (s *fakeService) StartType() (uint32, error) { return s.startType, nil }

func (s *fakeService) SetStartType(t uint32) error {
	s.asked = append(s.asked, "start type "+strconv.FormatUint(uint64(t), 10))
	s.startType = t
	return nil
}

func (s *fakeService) Start() error {
	s.asked = append(s.asked, "start")
	s.running = true
	return nil
}

func newAutostart(t *testing.T, goos string, privileged bool) (autostart, *fakeOS, string) {
	t.Helper()
	f := &fakeOS{files: map[string][]byte{}, time: &fakeService{startType: serviceDemandStart}}
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
		savedStart:  filepath.Join(dir, "work", "w32time-start.json"),
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
	if want := []string{"start type 3"}; !slices.Equal(f.time.asked, want) || f.time.startType != serviceDemandStart {
		t.Errorf("uninstall asked %q of the time service, start type %d; want %q: the type before the first install", f.time.asked, f.time.startType, want)
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
