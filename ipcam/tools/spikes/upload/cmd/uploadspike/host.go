// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"flag"
	"os/exec"
	"runtime"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hostinfo"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
)

// runHost prints D2's recommendation for this device (C2-R2), from
// ipcam-hub's diag host; it takes no lock and opens no listener.
func runHost(ctx context.Context, e *env, args []string) error {
	if err := cli.ParseFlags(flag.NewFlagSet("host", flag.ContinueOnError), args, e.out, usage); err != nil {
		return err
	}
	h, err := hostinfo.Read(ctx, e.hubBin, recDir(e))
	if err != nil {
		return err
	}
	letter, reason := hostinfo.RecommendD2(h)
	e.printf(hostRecommend, letter)
	e.printf("%s", reason)
	e.printf(hostStorage, h.Storage)
	return nil
}

// hostName is this device's pseudonym (PH0-22): the lab's from the
// environment; "pc" on Windows; on Linux "pi" for a Pi, else "lab-linux".
func hostName(ctx context.Context, e *env) (string, *hostinfo.Host) {
	if e.host != "" {
		return e.host, nil
	}
	if runtime.GOOS == "windows" {
		return "pc", nil
	}
	h, err := hostinfo.Read(ctx, e.hubBin, recDir(e))
	if err == nil && h.PiModel != nil {
		return "pi", &h
	}
	return "lab-linux", nil
}

// openBrowser opens url in the owner's browser (C2-R18: the owner never
// types an address that the program knows), and always prints it.
func openBrowser(e *env, url string) {
	if e.noBrowser {
		e.printf(msgOpenPage, url)
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) //nolint:gosec // uploadspike's own page
	default:
		cmd = exec.Command("xdg-open", url) //nolint:gosec // uploadspike's own page
	}
	if err := cmd.Start(); err != nil {
		e.printf(msgOpenPage, url)
		return
	}
	go func() { _ = cmd.Wait() }()
	e.printf(msgPageOpened, url)
}
