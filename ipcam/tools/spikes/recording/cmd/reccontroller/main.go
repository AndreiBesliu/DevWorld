// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Command reccontroller is the T43 controller of the recorder spike. It runs
// on the device that is not under test and holds every inbound listener
// (R08): it serves the simulated cameras over RTSP with an SEI index in each
// access unit, receives the host's beacon, schedules each run's cut, shows
// the prompt of a real cut, computes the loss on its own clock and writes
// each MI1 run's envelope (C16), and keeps the results that the host sends
// in results/ next to the binary. For MI3 (G20) it receives the host's live
// sessions with receive-only Pion peers, times their tiles from the SEI
// index, measures its own pacing per step (R07), and takes the host's
// encrypted upload into an HTTPS sink that discards it. For MI4's phone step
// (G24) it answers the spike app on :7080 and keeps its AGE result in
// results/app/. For MI9's LAN run (G26) its Pion playback peer plays the
// host's recording over the data channel and measures it. `reccontroller
// pull` is the final
// collection on the PC: it waits for the Pi's `recspike push` and stores
// its results under results/pi/ (G15). Every line it prints is Romanian;
// its log goes to reccontroller.log next to the binary.
//
//	reccontroller [-local] [-media DIR]
//	reccontroller pull
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

// usage is the command's help, in Romanian.
const usage = `Folosire:
  reccontroller [-local] [-media DOSAR]
      servește camerele simulate și măsoară rulările gazdei.
      -local   ascultă doar pe 127.0.0.1, pentru o gazdă pe același dispozitiv (proba de 72 h)
      -media   dosarul cu main.h264, sub.h264 și sub-256k.h264 (implicit: media lângă reccontroller)
  reccontroller pull
      așteaptă rezultatele Pi-ului (./recspike push --controller <adresa PC-ului>) și le pune în results/pi.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "reccontroller: eroare:", err)
		}
		stop()
		os.Exit(1)
	}
}

// run runs the command in args until ctx is done.
func run(ctx context.Context, args []string, out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	if len(args) > 0 && args[0] == "pull" {
		if len(args) > 1 {
			_, _ = fmt.Fprint(out, usage)
			return fmt.Errorf("argument necunoscut: %s", args[1])
		}
		var lc net.ListenConfig
		ln, err := lc.Listen(ctx, "tcp4", ":"+strconv.Itoa(link.Port))
		if err != nil {
			return fmt.Errorf("nu pot asculta pe portul %d (rulează deja un reccontroller?): %w", link.Port, err)
		}
		return pull(ctx, ln, filepath.Join(dir, "results"), out)
	}
	flags := flag.NewFlagSet("reccontroller", flag.ContinueOnError)
	local := flags.Bool("local", false, "")
	media := flags.String("media", filepath.Join(dir, "media"), "")
	if err := cli.ParseFlags(flags, args, out, usage); err != nil {
		return err
	}
	return serve(ctx, serveConfig{dir: dir, local: *local, media: *media, listen: controller.DefaultListenConfig(*local)}, out)
}
