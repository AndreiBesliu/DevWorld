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
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
)

// mi9Command is `recspike mi9 --lan --controller ADRESA`: MI9's LAN run on
// this host (G18, G26 leg 3), whose recording the other device's
// reccontroller plays over the data channel; the owner runs it once on
// each device.
func mi9Command(ctx context.Context, args []string, in *bufio.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("mi9", flag.ContinueOnError)
	lan := flags.Bool("lan", false, "")
	controller := flags.String("controller", "", "")
	if err := parseFlags(flags, args, out); err != nil {
		return err
	}
	switch {
	case !*lan:
		_, _ = fmt.Fprint(out, usage)
		return errors.New("lipsește --lan: proba MI9 de pe dispozitivele dvs. este cea pe LAN")
	case *controller == "":
		_, _ = fmt.Fprint(out, usage)
		return errors.New("lipsește --controller: adresa celuilalt dispozitiv, unde rulează reccontroller")
	}
	f, err := ownFolders()
	if err != nil {
		return err
	}
	return mi9.RunLAN(ctx, mi9.LANOptions{Controller: controllerAddr(*controller), Hub: f.hub, Results: f.results, Work: f.work, Out: out,
		Host: func(ctx context.Context) (mi1.HostState, error) {
			return mi1.OwnerHost(ctx, f.hub, f.work, runtime.GOOS, in, out)
		}})
}
