// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"flag"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/install"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// installSys is what install touches of the system: the prefix of its
// Linux paths ("" on the device), the commands it runs, and the hub's
// identity (EnsureUser); tests give their own.
type installSys struct {
	prefix     string
	run        install.Commander
	ensureUser func(ctx context.Context, run install.Commander) (uid, gid int, err error)
	rootUID    int
	rootGID    int
}

// system is the device's installSys.
func (e *env) system() *installSys {
	if e.sys != nil {
		return e.sys
	}
	return &installSys{run: install.Run, ensureUser: ensureUser}
}

// runInstall is `uploadspike install` (P49): on the PC (elevated once) the
// autostart's task and the inbound rules of the LAN logins; on the Pi
// (sudo) the hub's user, /opt/ipcam-spike, /var/lib/ipcam-spike, the
// SD-card confirmation and the systemd unit; with --lab the user and the
// folders only, and no autostart.
func runInstall(ctx context.Context, e *env, args []string) error {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	lab := flags.Bool("lab", false, "")
	if err := cli.ParseFlags(flags, args, e.out, usage); err != nil {
		return err
	}
	if err := elevate(e, append([]string{"install"}, args...)); err != nil {
		return err
	}
	return installOS(ctx, e, *lab)
}

// setAutostart records in the run state whether the autostart's resume
// waits to take the hub.
func setAutostart(e *env, on bool) error {
	return runstate.Update(e.statePath(), func(s *runstate.State) error {
		s.Autostart = on
		return nil
	})
}

// runUninstall is `uploadspike uninstall`: it stops and removes the task
// or the unit, which ends the waiting resume and its hub, then removes
// the block's rules and its cleanup, the inbound rules, the hub's user and
// /opt/ipcam-spike; the data stays.
func runUninstall(ctx context.Context, e *env, args []string) error {
	if err := cli.ParseFlags(flag.NewFlagSet("uninstall", flag.ContinueOnError), args, e.out, usage); err != nil {
		return err
	}
	if err := elevate(e, append([]string{"uninstall"}, args...)); err != nil {
		return err
	}
	if err := uninstallOS(ctx, e, e.system()); err != nil {
		return err
	}
	if err := setAutostart(e, false); err != nil {
		return err
	}
	e.printf(msgUninstalled)
	return nil
}
