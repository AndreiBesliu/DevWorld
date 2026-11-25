package install

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// TestInstall_WindowsTaskXML (P49): a boot trigger and a time trigger
// repeating every 5 min with no end, IgnoreNew, no time limit, on battery
// too, S4U with the highest privileges, `resume` in uploadspike's folder;
// the file is UTF-16 LE with a BOM; the task is run once after its
// registration.
func TestInstall_WindowsTaskXML(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 30, 0, 0, time.Local)
	doc := TaskXML(`C:\Users\x\spike\uploadspike.exe`, `C:\Users\x\spike`, "S-1-5-21-1-2-3-1001", start)
	var task struct {
		Triggers struct {
			Boot *struct{ Enabled bool } `xml:"BootTrigger"`
			Time *struct {
				StartBoundary string
				Repetition    struct {
					Interval          string
					Duration          string
					StopAtDurationEnd bool
				}
			} `xml:"TimeTrigger"`
		}
		Principal struct {
			UserID    string `xml:"UserId"`
			LogonType string
			RunLevel  string
		} `xml:"Principals>Principal"`
		Settings struct {
			MultipleInstancesPolicy    string
			DisallowStartIfOnBatteries bool
			StopIfGoingOnBatteries     bool
			ExecutionTimeLimit         string
		}
		Exec struct {
			Command, Arguments, WorkingDirectory string
		} `xml:"Actions>Exec"`
	}
	dec := xml.NewDecoder(strings.NewReader(doc))
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := dec.Decode(&task); err != nil {
		t.Fatalf("the XML: %v", err)
	}
	tr := task.Triggers
	if tr.Boot == nil || !tr.Boot.Enabled || tr.Time == nil || tr.Time.StartBoundary != "2026-11-18T09:30:00" ||
		tr.Time.Repetition.Interval != "PT5M" || tr.Time.Repetition.Duration != "" {
		t.Errorf("triggers %+v", tr)
	}
	s := task.Settings
	if s.MultipleInstancesPolicy != "IgnoreNew" || s.ExecutionTimeLimit != "PT0S" || s.DisallowStartIfOnBatteries || s.StopIfGoingOnBatteries {
		t.Errorf("settings %+v", s)
	}
	p := task.Principal
	if p.LogonType != "S4U" || p.RunLevel != "HighestAvailable" || p.UserID != "S-1-5-21-1-2-3-1001" {
		t.Errorf("principal %+v", p)
	}
	if e := task.Exec; e.Command != `C:\Users\x\spike\uploadspike.exe` || e.Arguments != "resume" || e.WorkingDirectory != `C:\Users\x\spike` {
		t.Errorf("exec %+v", e)
	}
	file := UTF16File(doc)
	if !bytes.HasPrefix(file, []byte{0xff, 0xfe}) {
		t.Fatalf("no UTF-16 LE BOM: % x", file[:4])
	}
	units := make([]uint16, (len(file)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(file[2+2*i:])
	}
	if string(utf16.Decode(units)) != doc {
		t.Error("the file is not the XML in UTF-16 LE")
	}
	cmds := TaskCommands(`C:\t.xml`)
	if len(cmds) != 2 || strings.Join(cmds[0], " ") != `schtasks /Create /XML C:\t.xml /TN ipcam-uploadspike /F` ||
		strings.Join(cmds[1], " ") != "schtasks /Run /TN ipcam-uploadspike" {
		t.Errorf("the task's commands %q; want its registration, then one run", cmds)
	}
}

// TestInstall_SystemdUnit (P49).
func TestInstall_SystemdUnit(t *testing.T) {
	unit := SystemdUnit()
	for _, want := range []string{"Wants=network-online.target time-sync.target", "After=network-online.target time-sync.target",
		"User=root", "ExecStart=/opt/ipcam-spike/uploadspike resume", "Restart=always", "RestartSec=10", "WantedBy=multi-user.target"} {
		if !strings.Contains(unit, want+"\n") {
			t.Errorf("the unit lacks %q:\n%s", want, unit)
		}
	}
	if got := UnitCommands(); strings.Join(got[len(got)-1], " ") != "systemctl enable --now ipcam-uploadspike.service" {
		t.Errorf("unit commands %q", got)
	}
}

// TestInstall_InboundRulesShape (Task 9B review, cannot-verify): the PC's
// inbound rules allow uploadspike.exe on TCP 7091 and ipcam-hub.exe, each
// scoped to its program and to the private profile.
func TestInstall_InboundRulesShape(t *testing.T) {
	lines := InboundLines(`C:\s\uploadspike.exe`, `C:\s\ipcam-hub.exe`)
	joined := strings.Join(lines, "\n")
	if !strings.HasPrefix(lines[0], "Remove-NetFirewallRule -Group 'ipcam-spike-inbound'") {
		t.Errorf("the rules are not replaced: %s", lines[0])
	}
	for _, want := range []string{
		`-Direction Inbound -Action Allow -Profile Private -Program 'C:\s\uploadspike.exe' -Protocol TCP -LocalPort 7091`,
		`-Direction Inbound -Action Allow -Profile Private -Program 'C:\s\ipcam-hub.exe' -Protocol TCP`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the rules lack %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "-Profile Any") || strings.Contains(joined, "Public") {
		t.Errorf("a rule beyond the private profile:\n%s", joined)
	}
}

// TestKeepAwake_Releases: the request and its release work (a no-op off
// Windows).
func TestKeepAwake_Releases(t *testing.T) {
	release, err := KeepAwake()
	if err != nil {
		t.Fatal(err)
	}
	release()
	release()
}
