// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RequireNftEnv set to "1" makes the Linux block refuse the iptables
// fallback: the lab's root test sets it, so that it measures nft's rules.
const RequireNftEnv = "UPLOADSPIKE_REQUIRE_NFT"

// windowsFirewall is the block in Windows Firewall: the rules of
// windowsRuleLines for ipcam-hub.exe and mediamtx.exe in binDir, through
// PowerShell; the cleanup is the scheduled task CleanupName, which runs
// binDir's uploadspike.exe unblock.
type windowsFirewall struct {
	binDir string
	run    commander
}

func newWindowsFirewall(binDir string, run commander) *windowsFirewall {
	return &windowsFirewall{binDir: binDir, run: run}
}

// programs are the absolute paths of the blocked programs.
func (w *windowsFirewall) programs() []string {
	return []string{winJoin(w.binDir, "ipcam-hub.exe"), winJoin(w.binDir, "mediamtx.exe")}
}

// winJoin is a Windows path in dir, also when built elsewhere (the rule
// shape's tests run on Linux).
func winJoin(dir, name string) string { return strings.TrimRight(dir, `\/`) + `\` + name }

// ps runs PowerShell lines, stopping at the first error. It ends with
// exit 0: -Command exits 1 when its last command set $? false, which a
// removal of nothing does even with -ErrorAction SilentlyContinue; a real
// error stops the script before that line, with exit 1.
func (w *windowsFirewall) ps(ctx context.Context, lines []string) error {
	script := "$ErrorActionPreference = 'Stop'; " + strings.Join(lines, "; ") + "; exit 0"
	_, err := w.run(ctx, "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script}, "")
	return err
}

func (w *windowsFirewall) Block(ctx context.Context) error {
	return w.ps(ctx, windowsRuleLines(w.programs()))
}

func (w *windowsFirewall) Unblock(ctx context.Context) error { return w.ps(ctx, windowsRemoveLines()) }

func (w *windowsFirewall) RemoveStale(ctx context.Context) error {
	return w.ps(ctx, append(windowsRemoveLines(), windowsUnscheduleLines()...))
}

func (w *windowsFirewall) ScheduleCleanup(ctx context.Context, at time.Time) error {
	return w.ps(ctx, append(windowsUnscheduleLines(), windowsCleanupLines(winJoin(w.binDir, "uploadspike.exe"), at)...))
}

// linuxFirewall is the block in nft (nftScript), or, where nft is
// unusable and RequireNftEnv is not set, in iptables and ip6tables with
// the same sets; the cleanup is a transient systemd timer, CleanupName,
// which runs binDir's uploadspike unblock.
type linuxFirewall struct {
	binDir string
	run    commander
	now    func() time.Time
	// requireNft refuses the iptables fallback.
	requireNft bool
}

func newLinuxFirewall(binDir string, run commander) *linuxFirewall {
	return &linuxFirewall{binDir: binDir, run: run, now: time.Now, requireNft: os.Getenv(RequireNftEnv) == "1"}
}

// errNoNft is a block that needs nft where it is unusable.
var errNoNft = errors.New("mi5: nft is unusable here, and " + RequireNftEnv + "=1 refuses the iptables fallback")

// nftUsable reports whether nft lists the tables.
func (l *linuxFirewall) nftUsable(ctx context.Context) bool {
	_, err := l.run(ctx, "nft", []string{"list", "tables"}, "")
	return err == nil
}

func (l *linuxFirewall) Block(ctx context.Context) error {
	if l.nftUsable(ctx) {
		_, err := l.run(ctx, "nft", []string{"-f", "-"}, nftScript())
		return err
	}
	if l.requireNft {
		return errNoNft
	}
	for _, t := range []struct {
		bin   string
		allow []string
	}{{"iptables", AllowV4}, {"ip6tables", AllowV6}} {
		for _, args := range iptablesScript(t.allow) {
			if _, err := l.run(ctx, t.bin, args, ""); err != nil {
				_ = l.Unblock(ctx)
				return err
			}
		}
	}
	return nil
}

func (l *linuxFirewall) Unblock(ctx context.Context) error {
	var errs []error
	if l.nftUsable(ctx) {
		if _, err := l.run(ctx, "nft", []string{"list", "table", "inet", NftTable}, ""); err == nil {
			if _, err := l.run(ctx, "nft", []string{"delete", "table", "inet", NftTable}, ""); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, bin := range []string{"iptables", "ip6tables"} {
		for _, args := range iptablesRemove() {
			_, _ = l.run(ctx, bin, args, "") // nothing to remove is no error
		}
	}
	return errors.Join(errs...)
}

func (l *linuxFirewall) RemoveStale(ctx context.Context) error {
	err := l.Unblock(ctx)
	_, _ = l.run(ctx, "systemctl", []string{"stop", CleanupName + ".timer"}, "")
	_, _ = l.run(ctx, "systemctl", []string{"reset-failed", CleanupName + ".service", CleanupName + ".timer"}, "")
	return err
}

func (l *linuxFirewall) ScheduleCleanup(ctx context.Context, at time.Time) error {
	secs := max(int64(at.Sub(l.now()).Seconds()), 1)
	_, _ = l.run(ctx, "systemctl", []string{"stop", CleanupName + ".timer"}, "")
	_, err := l.run(ctx, "systemd-run", []string{"--on-active=" + strconv.FormatInt(secs, 10) + "s", "--unit", CleanupName,
		path.Join(filepath.ToSlash(l.binDir), "uploadspike"), "unblock"}, "")
	if err != nil {
		return &Failure{Line: MsgCleanupFailed, Cause: fmt.Errorf("the cleanup timer: %w", err)}
	}
	return nil
}
