// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// WanBlock is the host firewall's block of the hub's WAN (P15, P56): one
// rule group for ipcam-hub and mediamtx only, which matches destination
// addresses outside the allow list, never an interface, so the LAN,
// loopback and the rest of the host stay online.
type WanBlock interface {
	// Block adds the rule group.
	Block(ctx context.Context) error
	// Unblock removes it.
	Unblock(ctx context.Context) error
	// RemoveStale removes a rule group and a cleanup that an earlier run
	// left, at startup.
	RemoveStale(ctx context.Context) error
	// ScheduleCleanup has the system run `uploadspike unblock` at at, so
	// that a run that dies blocked never leaves the hub cut off.
	ScheduleCleanup(ctx context.Context, at time.Time) error
}

// The names of the block's rules and cleanup.
const (
	// RuleGroup is the Windows Firewall group of the block's rules.
	RuleGroup = "ipcam-spike-wanblock"
	// NftTable is the nft table of the block: table inet ipcam_spike.
	NftTable = "ipcam_spike"
	// iptablesChain is the fallback's chain in iptables and ip6tables.
	iptablesChain = "IPCAM_SPIKE"
	// CleanupName is the cleanup's scheduled task (Windows) or transient
	// systemd unit (Linux).
	CleanupName = "ipcam-spike-unblock"
	// SpikeUser is the hub's identity on Linux, which the block matches.
	SpikeUser = "ipcam-spike"
	// CleanupAfter is the cleanup's margin after the block's end.
	CleanupAfter = 30 * time.Minute
)

// CleanupAt is when the cleanup runs for a block that starts at start and
// lasts blockFor: 30 min after its planned end.
func CleanupAt(start time.Time, blockFor time.Duration) time.Time {
	return start.Add(blockFor + CleanupAfter)
}

// commander runs one system command with stdin; tests replace it.
type commander func(ctx context.Context, name string, args []string, stdin string) (string, error)

// runCommand runs name with args and stdin, and returns its combined
// output; a failure carries the output's last line.
func runCommand(ctx context.Context, name string, args []string, stdin string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the block's own fixed commands
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

// psQuote is s as a PowerShell single-quoted string.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// windowsRuleLines are the PowerShell lines that add the block on Windows:
// one outbound Block rule per program, in RuleGroup, whose -RemoteAddress
// lists the allow list's complement explicitly, IPv4 and IPv6. There is
// no Allow rule: a Windows Block rule wins over any Allow rule.
func windowsRuleLines(programs []string) []string {
	remote := make([]string, 0, len(BlockV4)+len(BlockV6))
	for _, r := range append(append([]string{}, BlockV4...), BlockV6...) {
		remote = append(remote, psQuote(r))
	}
	lines := make([]string, 0, len(programs))
	for i, p := range programs {
		lines = append(lines, fmt.Sprintf("New-NetFirewallRule -Name %s -DisplayName %s -Group %s -Direction Outbound -Action Block -Profile Any -Program %s -RemoteAddress %s -ErrorAction Stop | Out-Null",
			psQuote(fmt.Sprintf("%s-%d", RuleGroup, i+1)), psQuote(RuleGroup), psQuote(RuleGroup), psQuote(p), strings.Join(remote, ",")))
	}
	return lines
}

// windowsRemoveLines remove the block's rules, if any.
func windowsRemoveLines() []string {
	return []string{fmt.Sprintf("Remove-NetFirewallRule -Group %s -ErrorAction SilentlyContinue", psQuote(RuleGroup))}
}

// windowsCleanupLines register the scheduled task that runs `uploadspike
// unblock` once at at, as SYSTEM.
func windowsCleanupLines(uploadspike string, at time.Time) []string {
	return []string{
		fmt.Sprintf("$a = New-ScheduledTaskAction -Execute %s -Argument 'unblock'", psQuote(uploadspike)),
		fmt.Sprintf("$t = New-ScheduledTaskTrigger -Once -At ([DateTime]::Parse(%s).ToLocalTime())", psQuote(at.UTC().Format(time.RFC3339))),
		fmt.Sprintf("Register-ScheduledTask -TaskName %s -Action $a -Trigger $t -User 'NT AUTHORITY\\SYSTEM' -RunLevel Highest -Force | Out-Null", psQuote(CleanupName)),
	}
}

// windowsUnscheduleLines remove the cleanup's scheduled task, if any.
func windowsUnscheduleLines() []string {
	return []string{fmt.Sprintf("Unregister-ScheduledTask -TaskName %s -Confirm:$false -ErrorAction SilentlyContinue", psQuote(CleanupName))}
}

// nftScript is the block on Linux: table inet ipcam_spike, chain output
// (hook output, priority 0), dropping the packets of the hub's uid whose
// destination is outside the allow list, IPv4 and IPv6. It names no
// interface.
func nftScript() string {
	return fmt.Sprintf(`table inet %[1]s {
	chain output {
		type filter hook output priority 0; policy accept;
		meta skuid "%[2]s" ip daddr != { %[3]s } drop
		meta skuid "%[2]s" ip6 daddr != { %[4]s } drop
	}
}
`, NftTable, SpikeUser, strings.Join(nftSet(AllowV4), ", "), strings.Join(nftSet(AllowV6), ", "))
}

// nftSet is an allow list as nft's set elements: "::" as ::/128, since a
// bare :: reads as nothing in some nft versions.
func nftSet(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s == "::" {
			s = "::/128"
		}
		out = append(out, s)
	}
	return out
}

// iptablesScript are the fallback's commands for one of iptables and
// ip6tables: a chain that returns for each allowed destination and drops
// the rest, which the OUTPUT chain sends the hub's uid to.
func iptablesScript(allow []string) [][]string {
	cmds := [][]string{{"-N", iptablesChain}}
	for _, a := range nftSet(allow) {
		cmds = append(cmds, []string{"-A", iptablesChain, "-d", a, "-j", "RETURN"})
	}
	return append(cmds,
		[]string{"-A", iptablesChain, "-j", "DROP"},
		[]string{"-I", "OUTPUT", "-m", "owner", "--uid-owner", SpikeUser, "-j", iptablesChain})
}

// iptablesRemove are the fallback's removal commands for one of iptables
// and ip6tables; each may fail when nothing is there.
func iptablesRemove() [][]string {
	return [][]string{
		{"-D", "OUTPUT", "-m", "owner", "--uid-owner", SpikeUser, "-j", iptablesChain},
		{"-F", iptablesChain},
		{"-X", iptablesChain},
	}
}
