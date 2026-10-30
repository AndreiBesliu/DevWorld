// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// pushCommand is `recspike push --controller <PC>`, the final collection on
// the Pi (G15): `reccontroller pull` on the PC receives the results folder
// and lists what is missing.
func pushCommand(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("push", flag.ContinueOnError)
	addr := flags.String("controller", "", "")
	if err := parseFlags(flags, args, out); err != nil {
		return err
	}
	if *addr == "" {
		_, _ = fmt.Fprint(out, usage)
		return errors.New("lipsește --controller: adresa PC-ului unde rulează reccontroller pull")
	}
	f, err := ownFolders()
	if err != nil {
		return err
	}
	return mi1.Push(ctx, controllerAddr(*addr), f.results, ownerHost(), out)
}
