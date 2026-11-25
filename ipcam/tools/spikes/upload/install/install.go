// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package install is `uploadspike install` and `uninstall` (P49, Task 8):
// on the PC, the Task Scheduler task that runs `uploadspike resume` at
// boot and every 5 min (Task 9's self-healing autostart) and the inbound
// firewall rules of the LAN logins; on the Pi, the hub's system user, the
// bundle under /opt/ipcam-spike, the data under /var/lib/ipcam-spike and
// the systemd unit; and the PC's keep-awake request while runs go on.
// The task XML copies recspike install's taskXML and utf16File.
package install

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"
)

// The names and places of the install.
const (
	// TaskName is the PC's Task Scheduler task.
	TaskName = "ipcam-uploadspike"
	// UnitName is the Pi's systemd unit, UnitPath its file.
	UnitName = "ipcam-uploadspike.service"
	UnitPath = "/etc/systemd/system/" + UnitName
	// OptDir holds the bundle's binaries and media on the Pi (root,
	// 0755), VarDir the data (hub/, state/, results/).
	OptDir = "/opt/ipcam-spike"
	VarDir = "/var/lib/ipcam-spike"
	// SpikeUser is the hub's system user on the Pi.
	SpikeUser = "ipcam-spike"
	// InboundGroup is the PC's inbound firewall rules' group: the LAN
	// logins reach uploadspike on 7091, scoped to the private profile.
	InboundGroup = "ipcam-spike-inbound"
	// HandoffPort is uploadspike's port (handoff.HandoffPort).
	HandoffPort = 7091
)

// Commander runs one system command with stdin and returns its output;
// tests replace it.
type Commander func(ctx context.Context, name string, args []string, stdin string) (string, error)

// Run runs name with args and stdin; a failure carries the output's last
// line.
func Run(ctx context.Context, name string, args []string, stdin string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the install's own fixed commands
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		last := text
		if i := strings.LastIndexByte(last, '\n'); i >= 0 {
			last = last[i+1:]
		}
		return text, fmt.Errorf("%s: %w: %s", name, err, last)
	}
	return text, nil
}

// xmlText escapes s for an XML element's text.
func xmlText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TaskXML is the PC's task (P49), recspike install's taskXML with a
// TimeTrigger: at every boot, and every 5 min from start with no end, as
// the owner's user whether logged on or not (S4U), with the highest
// privileges, it runs `uploadspike resume` in uploadspike's folder; a
// repetition starts resume only when none runs (IgnoreNew), never stops it
// (PT0S: the schema's default PT72H would end the 7-day resume on day 3),
// and runs on battery too, at the normal priority.
func TaskXML(exe, dir, userSID string, start time.Time) string {
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Proba de upload ipcam: ține hub-ul probei pornit (uploadspike resume).</Description>
  </RegistrationInfo>
  <Triggers>
    <BootTrigger>
      <Enabled>true</Enabled>
    </BootTrigger>
    <TimeTrigger>
      <StartBoundary>` + start.Format("2006-01-02T15:04:05") + `</StartBoundary>
      <Repetition>
        <Interval>PT5M</Interval>
        <StopAtDurationEnd>false</StopAtDurationEnd>
      </Repetition>
      <Enabled>true</Enabled>
    </TimeTrigger>
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

// UTF16File is s as UTF-16 LE with a BOM, which schtasks /XML needs.
func UTF16File(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xff, 0xfe
	for _, u := range units {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return out
}

// TaskCommands register the task from its XML file and start it once, so
// that resume waits from the install on, before any reboot.
func TaskCommands(xmlFile string) [][]string {
	return [][]string{
		{"schtasks", "/Create", "/XML", xmlFile, "/TN", TaskName, "/F"},
		{"schtasks", "/Run", "/TN", TaskName},
	}
}

// UntaskCommands stop the task, which ends the waiting resume and its
// hub, and remove it.
func UntaskCommands() [][]string {
	return [][]string{
		{"schtasks", "/End", "/TN", TaskName},
		{"schtasks", "/Delete", "/TN", TaskName, "/F"},
	}
}

// psQuote is s as a PowerShell single-quoted string.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// InboundLines add the PC's inbound rules (Task 9B review): uploadspike
// on TCP 7091 and ipcam-hub, each scoped to its program and to the
// private profile, so the first LAN bind brings no Windows Defender
// Firewall prompt that the owner could dismiss.
func InboundLines(uploadspike, hub string) []string {
	return append(UninboundLines(),
		fmt.Sprintf("New-NetFirewallRule -Name %s -DisplayName %s -Group %s -Direction Inbound -Action Allow -Profile Private -Program %s -Protocol TCP -LocalPort %d -ErrorAction Stop | Out-Null",
			psQuote(InboundGroup+"-uploadspike"), psQuote(InboundGroup), psQuote(InboundGroup), psQuote(uploadspike), HandoffPort),
		fmt.Sprintf("New-NetFirewallRule -Name %s -DisplayName %s -Group %s -Direction Inbound -Action Allow -Profile Private -Program %s -Protocol TCP -ErrorAction Stop | Out-Null",
			psQuote(InboundGroup+"-hub"), psQuote(InboundGroup), psQuote(InboundGroup), psQuote(hub)))
}

// UninboundLines remove the inbound rules, if any.
func UninboundLines() []string {
	return []string{fmt.Sprintf("Remove-NetFirewallRule -Group %s -ErrorAction SilentlyContinue", psQuote(InboundGroup))}
}

// InboundCheckLine prints "blocked" when an enabled inbound Block rule
// names uploadspike (a dismissed prompt makes one), "missing" without the
// Allow rule, else "ok".
func InboundCheckLine(uploadspike string) string {
	return fmt.Sprintf("$b = Get-NetFirewallApplicationFilter -Program %s -ErrorAction SilentlyContinue | Get-NetFirewallRule | "+
		"Where-Object { $_.Direction -eq 'Inbound' -and $_.Action -eq 'Block' -and $_.Enabled -eq 'True' }; "+
		"if ($b) { 'blocked' } elseif (-not (Get-NetFirewallRule -Name %s -ErrorAction SilentlyContinue)) { 'missing' } else { 'ok' }",
		psQuote(uploadspike), psQuote(InboundGroup+"-uploadspike"))
}

// PowerShell runs lines in one PowerShell, stopping at the first error;
// it ends with exit 0, since -Command exits 1 when its last command set
// $? false, as a removal of nothing does under SilentlyContinue.
func PowerShell(ctx context.Context, run Commander, lines []string) (string, error) {
	script := "$ErrorActionPreference = 'Stop'; " + strings.Join(lines, "; ") + "; exit 0"
	return run(ctx, "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script}, "")
}

// SystemdUnit is the Pi's unit (P49): `uploadspike resume` as root from
// /opt/ipcam-spike, once the network and the clock are up, restarted 10 s
// after any end, so resume waits from the install on.
func SystemdUnit() string {
	return `[Unit]
Description=ipcam: proba de upload, hub-ul probei pornit (uploadspike resume)
Wants=network-online.target time-sync.target
After=network-online.target time-sync.target

[Service]
Type=simple
User=root
WorkingDirectory=` + OptDir + `
ExecStart=` + OptDir + `/uploadspike resume
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
`
}

// UnitCommands enable and start the unit.
func UnitCommands() [][]string {
	return [][]string{{"systemctl", "daemon-reload"}, {"systemctl", "enable", "--now", UnitName}}
}

// UnunitCommands stop and disable the unit, which ends the waiting
// resume and its hub.
func UnunitCommands() [][]string {
	return [][]string{{"systemctl", "disable", "--now", UnitName}}
}

// UserCommands create the hub's system user unless it exists.
func UserCommands() [][]string {
	return [][]string{{"useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", SpikeUser}}
}

// UnuserCommands remove the hub's system user.
func UnuserCommands() [][]string { return [][]string{{"userdel", SpikeUser}} }

// RunAll runs each command, stopping at the first failure.
func RunAll(ctx context.Context, run Commander, cmds [][]string) error {
	for _, c := range cmds {
		if _, err := run(ctx, c[0], c[1:], ""); err != nil {
			return err
		}
	}
	return nil
}

// RunEach runs each command and joins nothing: an uninstall goes on past
// what is already gone.
func RunEach(ctx context.Context, run Commander, cmds [][]string) {
	for _, c := range cmds {
		_, _ = run(ctx, c[0], c[1:], "")
	}
}
