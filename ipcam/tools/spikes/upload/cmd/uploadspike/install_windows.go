// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"errors"
	"os"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/install"
)

// ensureUser is the Pi's: none on Windows.
func ensureUser(context.Context, install.Commander) (uid, gid int, err error) {
	return 0, 0, errors.New("install: the hub's user is the Pi's")
}

// firewallHint tells the owner when Windows Firewall blocks the LAN's
// logins (Task 9B review): a dismissed prompt left an inbound Block rule
// for uploadspike, or install's Allow rules are missing.
func firewallHint(ctx context.Context, e *env) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if state := install.InboundBlocked(ctx, e.system().run, exe); state == "blocked" || state == "missing" {
		e.printf(msgFirewallBlocks)
	}
}

// installTask registers and starts the PC's task for exe.
func installTask(ctx context.Context, run install.Commander, exe string, e *env) error {
	return install.AddTask(ctx, run, exe, e.now())
}

// installOS registers and starts the task, adds the inbound rules,
// and records the autostart in the run state.
func installOS(ctx context.Context, e *env, _ bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	sys := e.system()
	if err := installTask(ctx, sys.run, exe, e); err != nil {
		return err
	}
	if _, err := install.PowerShell(ctx, sys.run, install.InboundLines(exe, e.hubBin)); err != nil {
		return err
	}
	if err := setAutostart(e, true); err != nil {
		return err
	}
	e.printf(msgInstalledPC)
	return nil
}

// uninstallOS stops and removes the task, which ends the waiting resume
// and its hub, then the block's rules and cleanup and the inbound rules.
func uninstallOS(ctx context.Context, e *env, sys *installSys) error {
	install.RunEach(ctx, sys.run, install.UntaskCommands())
	_ = e.firewall().RemoveStale(ctx)
	_, _ = install.PowerShell(ctx, sys.run, install.UninboundLines())
	return nil
}
