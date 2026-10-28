// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi2"
)

// mi2Command is `recspike mi2 --controller 127.0.0.1 [--hours 72]`: MI2's
// 72 h run on this host (G11), with the cameras of a reccontroller -local on
// the same device (G18, G19).
func mi2Command(ctx context.Context, args []string, in *bufio.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("mi2", flag.ContinueOnError)
	controller := flags.String("controller", "", "")
	hours := flags.Float64("hours", mi2.RunHours, "")
	if err := parseFlags(flags, args, out); err != nil {
		return err
	}
	switch {
	case *controller == "":
		_, _ = fmt.Fprint(out, usage)
		return errors.New("lipsește --controller: adresa reccontroller -local, 127.0.0.1")
	case *hours <= 0:
		_, _ = fmt.Fprint(out, usage)
		return errors.New("--hours este un număr de ore mai mare decât 0")
	}
	f, err := ownFolders()
	if err != nil {
		return err
	}
	// The host's recommended camera count, its storage and, on an SD card,
	// the owner's confirmation (C24, G16).
	host, err := mi1.OwnerHost(ctx, f.hub, f.work, runtime.GOOS, in, out)
	if err != nil {
		return err
	}
	addr := controllerAddr(*controller)
	return mi2.Run(ctx, mi2.Options{Hours: *hours, Host: host, Hub: f.hub, Results: f.results, Work: f.work, Out: out,
		CameraURLs: func(ctx context.Context, streams []string) (map[string]string, error) {
			return mi1.CameraURLs(ctx, addr, host.Pseudonym, streams)
		}})
}
