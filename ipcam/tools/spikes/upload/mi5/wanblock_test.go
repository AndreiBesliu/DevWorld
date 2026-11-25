package mi5

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/handoff"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
)

// recorder is a commander that records each command and answers from
// fail (a command's prefix → its error).
type recorder struct {
	mu   sync.Mutex
	cmds []string
	fail map[string]error
}

func (r *recorder) run(_ context.Context, name string, args []string, stdin string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	line := strings.Join(append([]string{name}, args...), " ")
	if stdin != "" {
		line += " <<" + stdin
	}
	r.cmds = append(r.cmds, line)
	for prefix, err := range r.fail {
		if strings.HasPrefix(line, prefix) {
			return "", err
		}
	}
	return "", nil
}

// TestWanBlock_WindowsRuleShape (P15, P56): the rules are in group
// ipcam-spike-wanblock, outbound Block, one per program with its absolute
// path, with exactly the block ranges, and no Allow rule.
func TestWanBlock_WindowsRuleShape(t *testing.T) {
	rec := &recorder{}
	w := newWindowsFirewall(`C:\Users\x\ipcam-spike`, rec.run)
	if err := w.Block(t.Context()); err != nil {
		t.Fatal(err)
	}
	script := rec.cmds[0]
	rules := regexp.MustCompile(`New-NetFirewallRule [^;]*`).FindAllString(script, -1)
	if len(rules) != 2 {
		t.Fatalf("%d rules in %q; want one per program", len(rules), script)
	}
	wantRemote := []string{}
	for _, r := range append(slices.Clone(BlockV4), BlockV6...) {
		wantRemote = append(wantRemote, "'"+r+"'")
	}
	for i, prog := range []string{`C:\Users\x\ipcam-spike\ipcam-hub.exe`, `C:\Users\x\ipcam-spike\mediamtx.exe`} {
		r := rules[i]
		for _, want := range []string{"-Group 'ipcam-spike-wanblock'", "-Direction Outbound", "-Action Block", "-Program '" + prog + "'",
			"-RemoteAddress " + strings.Join(wantRemote, ",") + " "} {
			if !strings.Contains(r, want) {
				t.Errorf("rule %d lacks %q:\n%s", i, want, r)
			}
		}
	}
	if strings.Contains(script, "-Action Allow") || strings.Contains(script, "InterfaceAlias") {
		t.Errorf("an Allow rule or an interface in %q", script)
	}
}

// TestWanBlock_LinuxRuleShape (P15, P56): the nft script matches the hub's
// uid, drops by daddr outside the allow sets for ip and ip6, and never
// names an interface.
func TestWanBlock_LinuxRuleShape(t *testing.T) {
	rec := &recorder{}
	l := newLinuxFirewall("/opt/ipcam-spike", rec.run)
	if err := l.Block(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(rec.cmds) != 2 || !strings.HasPrefix(rec.cmds[1], "nft -f - <<") {
		t.Fatalf("commands %q; want nft -f - after the check", rec.cmds)
	}
	script := nftScript()
	for _, want := range []string{
		"table inet ipcam_spike", "chain output", "type filter hook output priority 0;",
		`meta skuid "ipcam-spike" ip daddr != { ` + strings.Join(AllowV4, ", ") + ` } drop`,
		`meta skuid "ipcam-spike" ip6 daddr != { ::1, ::/128, fe80::/10, fc00::/7, ff00::/8 } drop`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the nft script lacks %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "oif") || strings.Contains(script, "iif") {
		t.Errorf("the nft script names an interface:\n%s", script)
	}
	// Without nft, iptables and ip6tables with the same sets.
	rec = &recorder{fail: map[string]error{"nft": errors.New("no nft")}}
	l = newLinuxFirewall("/opt/ipcam-spike", rec.run)
	if err := l.Block(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rec.cmds, "\n")
	for _, want := range []string{"iptables -A IPCAM_SPIKE -d 192.168.0.0/16 -j RETURN", "ip6tables -A IPCAM_SPIKE -d fe80::/10 -j RETURN",
		"iptables -I OUTPUT -m owner --uid-owner ipcam-spike -j IPCAM_SPIKE", "ip6tables -A IPCAM_SPIKE -j DROP"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the fallback lacks %q:\n%s", want, joined)
		}
	}
	l.requireNft = true
	if err := l.Block(t.Context()); !errors.Is(err, errNoNft) {
		t.Errorf("with %s=1 and no nft: %v", RequireNftEnv, err)
	}
}

// TestWanBlock_CleanupScheduledAtBlockPlus30m: the cleanup runs 30 min
// after the block's planned end.
func TestWanBlock_CleanupScheduledAtBlockPlus30m(t *testing.T) {
	start := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		block time.Duration
		after string
	}{{6 * time.Hour, "23400s"}, {15 * time.Minute, "2700s"}} {
		at := CleanupAt(start, tc.block)
		rec := &recorder{}
		l := newLinuxFirewall("/opt/ipcam-spike", rec.run)
		l.now = func() time.Time { return start }
		if err := l.ScheduleCleanup(t.Context(), at); err != nil {
			t.Fatal(err)
		}
		want := "systemd-run --on-active=" + tc.after + " --unit ipcam-spike-unblock /opt/ipcam-spike/uploadspike unblock"
		if !slices.Contains(rec.cmds, want) {
			t.Errorf("a %v block: %q; want %q", tc.block, rec.cmds, want)
		}
		rec = &recorder{}
		w := newWindowsFirewall(`C:\b`, rec.run)
		if err := w.ScheduleCleanup(t.Context(), at); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rec.cmds[0], "Parse('"+at.UTC().Format(time.RFC3339)+"')") ||
			!strings.Contains(rec.cmds[0], `-Execute 'C:\b\uploadspike.exe' -Argument 'unblock'`) ||
			!strings.Contains(rec.cmds[0], "-TaskName 'ipcam-spike-unblock'") {
			t.Errorf("the Windows cleanup: %s", rec.cmds[0])
		}
	}
}

// TestWanBlock_StartupRemovesStaleGroup: RemoveStale removes the rule
// group (or table, and the fallback's chains) and the cleanup that an
// earlier run left.
func TestWanBlock_StartupRemovesStaleGroup(t *testing.T) {
	rec := &recorder{}
	if err := newWindowsFirewall(`C:\b`, rec.run).RemoveStale(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.cmds[0], "Remove-NetFirewallRule -Group 'ipcam-spike-wanblock'") ||
		!strings.Contains(rec.cmds[0], "Unregister-ScheduledTask -TaskName 'ipcam-spike-unblock'") {
		t.Errorf("Windows: %q", rec.cmds)
	}
	rec = &recorder{}
	if err := newLinuxFirewall("/opt/ipcam-spike", rec.run).RemoveStale(t.Context()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rec.cmds, "\n")
	for _, want := range []string{"nft delete table inet ipcam_spike", "iptables -D OUTPUT -m owner --uid-owner ipcam-spike -j IPCAM_SPIKE",
		"ip6tables -X IPCAM_SPIKE", "systemctl stop ipcam-spike-unblock.timer"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Linux lacks %q:\n%s", want, joined)
		}
	}
}

// lanListener listens on the host's first RFC 1918 address, else fails
// the test when must, else skips it.
func lanListener(t *testing.T, must bool) string {
	t.Helper()
	addrs, err := handoff.RFC1918Addrs()
	if err != nil || len(addrs) == 0 {
		if must {
			t.Fatalf("no RFC 1918 address here (%v)", err)
		}
		t.Skip("no RFC 1918 address here")
	}
	ln, err := net.Listen("tcp4", net.JoinHostPort(addrs[0].String(), "0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return ln.Addr().String()
}

// reachAs runs diag reach as the hub's identity and returns its lines by
// target and family.
func reachAs(t *testing.T, hub hubproc.Config, hosts []string, lan string) map[string]reachLine {
	t.Helper()
	lines, err := hubproc.Diag(t.Context(), hub, []string{"reach", "--hosts", strings.Join(hosts, ","), "--lan", lan}, nil)
	if err != nil {
		t.Fatalf("diag reach: %v", err)
	}
	out := map[string]reachLine{}
	for _, raw := range lines {
		var l reachLine
		if json.Unmarshal(raw, &l) == nil {
			out[l.Target+"/"+l.Family] = l
		}
	}
	return out
}

// TestWanBlock_KeepsLanAndLoopback (Review Focus 2; root and nft, the
// mi5-lab job): before the block the fakes answer the hub's identity;
// after a real Block they read blocked (timeout or denied) while loopback
// and the runner's RFC 1918 listener answer; this process (root, not the
// hub) still reaches the fakes; after Unblock they answer again.
func TestWanBlock_KeepsLanAndLoopback(t *testing.T) {
	if os.Getenv(RequireNftEnv) != "1" {
		t.Skip(RequireNftEnv + " is not set: the mi5-lab job runs this as root")
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Fatal("this test runs as root on Linux")
	}
	hubBin := os.Getenv("UPLOADSPIKE_HUB_BIN")
	hub := hubproc.Config{Bin: hubBin, RunAsUser: SpikeUser}
	lan := lanListener(t, true)
	fw := HostFirewall(filepath.Dir(hubBin))
	t.Cleanup(func() { _ = fw.RemoveStale(context.Background()) })
	if err := fw.RemoveStale(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := reachAs(t, hub, []string{LabHost}, lan)
	if !before["provider/v4"].OK {
		t.Fatalf("before the block the fakes do not answer the hub's identity: %+v", before)
	}
	if err := fw.Block(t.Context()); err != nil {
		t.Fatal(err)
	}
	during := reachAs(t, hub, []string{LabHost}, lan)
	if p := during["provider/v4"]; p.OK || p.Err != "timeout" && p.Err != "denied" {
		t.Errorf("during the block the fakes read %+v; want blocked (timeout or denied)", p)
	}
	if !during["lan/v4"].OK || !during["loopback/v4"].OK {
		t.Errorf("during the block: LAN %+v, loopback %+v; want both answering", during["lan/v4"], during["loopback/v4"])
	}
	if l6, ok := during["loopback/v6"]; ok && !l6.OK {
		t.Errorf("during the block ::1 does not answer: %+v", l6)
	}
	if _, err := VerifyBlocked(t.Context(), hub, []string{LabHost}, lan); err != nil {
		t.Errorf("VerifyBlocked during the block: %v", err)
	}
	if err := Dialer(t.Context(), LabHost); err != nil {
		t.Errorf("the rest of the host lost the fakes during the block: %v", err)
	}
	if err := fw.Unblock(t.Context()); err != nil {
		t.Fatal(err)
	}
	if after := reachAs(t, hub, []string{LabHost}, lan); !after["provider/v4"].OK {
		t.Errorf("after Unblock the fakes read %+v", after["provider/v4"])
	}
}

// TestWanBlock_WindowsSmoke (the windows job): www.googleapis.com reads
// blocked from ipcam-hub.exe over IPv4 (and IPv6, or no-address),
// loopback and the runner's private address answer, and after Unblock the
// provider answers again.
func TestWanBlock_WindowsSmoke(t *testing.T) {
	if os.Getenv("UPLOADSPIKE_REQUIRE_WINFW") != "1" {
		t.Skip("UPLOADSPIKE_REQUIRE_WINFW is not set: the windows job runs this")
	}
	if runtime.GOOS != "windows" {
		t.Fatal("this test runs on Windows")
	}
	hubBin := os.Getenv("UPLOADSPIKE_HUB_BIN")
	hub := hubproc.Config{Bin: hubBin}
	lan := lanListener(t, true)
	fw := HostFirewall(filepath.Dir(hubBin))
	t.Cleanup(func() { _ = fw.RemoveStale(context.Background()) })
	if err := fw.RemoveStale(t.Context()); err != nil {
		t.Fatal(err)
	}
	host := "www.googleapis.com"
	if before := reachAs(t, hub, []string{host}, lan); !before["provider/v4"].OK {
		t.Fatalf("before the block %s does not answer: %+v", host, before["provider/v4"])
	}
	if err := fw.Block(t.Context()); err != nil {
		t.Fatal(err)
	}
	res, err := VerifyBlocked(t.Context(), hub, []string{host}, lan)
	if err != nil || !res.ProviderBlockedV4 || !res.ProviderBlockedV6 || !res.Lan || !res.Loopback {
		t.Errorf("during the block: %+v, %v", res, err)
	}
	if err := fw.Unblock(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		after := reachAs(t, hub, []string{host}, lan)
		if after["provider/v4"].OK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after Unblock %s reads %+v", host, after["provider/v4"])
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Println("windows smoke: blocked and unblocked")
}
