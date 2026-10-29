// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi3"
)

// mi3Command is `recspike mi3 --controller ADRESA`: MI3's capacity run on
// this host (G12, G20), with the cameras of reccontroller on the other
// device: a controller on this one is refused before anything runs, since
// its simulators would load the CPU that MI3 measures.
func mi3Command(ctx context.Context, args []string, in *bufio.Reader, out io.Writer) error {
	flags := flag.NewFlagSet("mi3", flag.ContinueOnError)
	controller := flags.String("controller", "", "")
	if err := parseFlags(flags, args, out); err != nil {
		return err
	}
	switch {
	case *controller == "":
		_, _ = fmt.Fprint(out, usage)
		return errors.New("lipsește --controller: adresa dispozitivului unde rulează reccontroller")
	case loopback(*controller):
		_, _ = fmt.Fprint(out, usage)
		return fmt.Errorf("--controller %s este chiar acest dispozitiv: proba MI3 ia camerele de la reccontroller de pe celălalt dispozitiv, ca procesorul măsurat să nu le simuleze", *controller)
	}
	f, err := ownFolders()
	if err != nil {
		return err
	}
	self, err := executable()
	if err != nil {
		return err
	}
	// The host's recommended camera count, its cores, its storage and, on an
	// SD card, the owner's confirmation (C24, R07, G16).
	host, err := mi1.OwnerHost(ctx, f.hub, f.work, runtime.GOOS, in, out)
	if err != nil {
		return err
	}
	if m := host.PiModel; m != nil && m.Known && !m.Supported {
		return errors.New("acest Raspberry Pi nu este suportat (armv6): proba MI3 se amână în Faza 0-bis")
	}
	return mi3.Run(ctx, mi3.Options{Host: host, Controller: controllerAddr(*controller), Hub: f.hub, Self: self,
		Results: f.results, Work: f.work, Out: out})
}

// mi3LoadCommand is `recspike mi3-load live|playback|export|upload`, which
// recspike mi3 starts: one load of a step, its own process so that its CPU
// is its own (G20). Its spec is the first line of its stdin; the end of
// its stdin ends it. It prints its reports as JSON lines: one once it
// runs, one at its end.
func mi3LoadCommand(ctx context.Context, args []string, in *bufio.Reader, out io.Writer) error {
	if len(args) != 1 || !slices.Contains(mi3.Loads, args[0]) {
		_, _ = fmt.Fprint(out, usage)
		return fmt.Errorf("mi3-load primește una dintre încărcările %s", strings.Join(mi3.Loads, ", "))
	}
	line, err := in.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("mi3-load: nu am primit încărcarea: %w", err)
	}
	var spec mi3.LoadSpec
	if err := json.Unmarshal(line, &spec); err != nil {
		return fmt.Errorf("mi3-load: încărcarea nu se poate citi: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_, _ = io.Copy(io.Discard, in)
		cancel()
	}()
	enc := json.NewEncoder(out)
	report, err := mi3.RunLoad(ctx, args[0], spec, func(r mi3.LoadReport) error { return enc.Encode(r) })
	if err != nil {
		return err
	}
	return enc.Encode(report)
}
