// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
	if log := os.Getenv(envFirewallLog); log != "" {
		// The tests' seam: each command is recorded, none runs.
		return mi5.FirewallFor(runtime.GOOS, filepath.Dir(e.hubBin), func(_ context.Context, name string, args []string, _ string) (string, error) {
			f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own log
			if err != nil {
				return "", err
			}
			_, err = fmt.Fprintln(f, strings.Join(append([]string{name}, args...), " "))
			return "", errors.Join(err, f.Close())
		})
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
	if _, seam := os.LookupEnv(envRunAs); seam && runtime.GOOS != "windows" && isElevated() {
		return nil // the tests' hub runs as uploadspike: no root needed
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
