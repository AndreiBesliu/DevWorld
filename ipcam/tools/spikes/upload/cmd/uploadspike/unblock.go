// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"flag"
	"path/filepath"
	"runtime"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi5"
)

// runUnblock is `uploadspike unblock` (Deblocare): it removes MI5's WAN
// block, the rule group on Windows (elevated, one UAC prompt) or the nft
// table and the fallback's chains on Linux (sudo). The cleanup's timer
// runs it at the block's end + 30 min; the owner can run it at any time.
func runUnblock(ctx context.Context, e *env, args []string) error {
	if err := cli.ParseFlags(flag.NewFlagSet("unblock", flag.ContinueOnError), args, e.out, usage); err != nil {
		return err
	}
	if err := elevate(e, append([]string{"unblock"}, args...)); err != nil {
		return err
	}
	if err := e.firewall().Unblock(ctx); err != nil {
		return err
	}
	e.printf(msgUnblocked)
	return nil
}

// firewall is this device's WAN block, for the binaries next to its hub.
func (e *env) firewall() mi5.WanBlock {
	if e.firewallFor != nil {
		return e.firewallFor(filepath.Dir(e.hubBin))
	}
	return mi5.FirewallFor(runtime.GOOS, filepath.Dir(e.hubBin), mi5.RunCommand)
}

// elevate makes sure that uploadspike runs elevated: on Windows it
// relaunches itself with args through one UAC prompt and ends with the
// elevated run's code; on Linux it asks for sudo.
func elevate(e *env, args []string) error {
	if isElevated() && (runtime.GOOS == "windows" || e.root()) {
		return nil
	}
	if runtime.GOOS != "windows" {
		return ownerError(msgNeedsSudo)
	}
	code, err := relaunchElevated(args)
	if err != nil {
		return err
	}
	return exitError(code)
}
