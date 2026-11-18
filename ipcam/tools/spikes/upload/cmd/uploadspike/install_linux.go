// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/install"
)

// firewallHint is the PC's: the Pi's firewall asks nothing.
func firewallHint(context.Context, *env) {}

// ensureUser creates the hub's system user unless it exists.
func ensureUser(ctx context.Context, run install.Commander) (uid, gid int, err error) {
	return install.EnsureUser(ctx, run)
}

// bundleBinaries are the bundle's programs that the Pi's install copies
// into /opt/ipcam-spike (those present).
var bundleBinaries = []string{"uploadspike", "ipcam-hub", "mediamtx", "reccontroller", "recmediagen", "mediagen", "fakeproviders"}

// installOS lays out the Pi (or the lab's runner) and, but in the lab,
// asks the SD-card confirmation and installs the unit.
func installOS(ctx context.Context, e *env, lab bool) error {
	sys := e.system()
	uid, gid, err := sys.ensureUser(ctx, sys.run)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	b := install.Bundle{SrcDir: filepath.Dir(exe), Binaries: bundleBinaries, Media: "media"}
	if err := install.Layout(sys.prefix, b, install.Owners{RootUID: sys.rootUID, RootGID: sys.rootGID, SpikeUID: uid, SpikeGID: gid}); err != nil {
		return err
	}
	if sys.prefix == "" {
		e.hubBin = filepath.Join(install.OptDir, defaultHubName)
	}
	if lab {
		e.printf(msgInstalledLab)
		return nil
	}
	if err := confirmSdCard(ctx, e); err != nil {
		return err
	}
	if err := install.AddUnit(ctx, sys.prefix, sys.run); err != nil {
		return err
	}
	if err := setAutostart(e, true); err != nil {
		return err
	}
	e.printf(msgInstalledPi)
	return nil
}

// uninstallOS removes the unit, which ends the waiting resume and its
// hub, the block and its timer, the hub's user and /opt/ipcam-spike.
func uninstallOS(ctx context.Context, e *env, sys *installSys) error {
	install.RemoveUnit(ctx, sys.prefix, sys.run)
	_ = e.firewall().RemoveStale(ctx)
	install.RunEach(ctx, sys.run, install.UnuserCommands())
	return os.RemoveAll(filepath.Join(sys.prefix, install.OptDir))
}
