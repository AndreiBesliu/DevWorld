// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi4"
)

// geteuid is the effective user's id; tests replace it.
var geteuid = os.Geteuid

// mi4Command is `recspike mi4 --controller ADRESA_PC`, with sudo on the
// Pi: MI4's age rig on this host (G13), which records the controller's
// camera and times reads, seeks and encryption through ipcam-hub. On the Pi
// it needs root, which drops the page cache; mi4.Run refuses without it
// before it reads the host, with mi4.ErrNeedsSudo, which report prints
// alone.
func mi4Command(ctx context.Context, args []string, in *bufio.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("mi4", flag.ContinueOnError)
	controller := flags.String("controller", "", "")
	if err := parseFlags(flags, args, out); err != nil {
		return err
	}
	if *controller == "" {
		_, _ = fmt.Fprint(out, usage)
		return errors.New("lipsește --controller: adresa PC-ului unde rulează reccontroller")
	}
	f, err := ownFolders()
	if err != nil {
		return err
	}
	return mi4.Run(ctx, mi4.Options{Controller: controllerAddr(*controller), Hub: f.hub, Results: f.results, Work: f.work, Out: out,
		Euid: geteuid,
		Host: func(ctx context.Context) (mi1.HostState, error) {
			return mi1.OwnerHost(ctx, f.hub, f.work, runtime.GOOS, in, out)
		}})
}
