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
	"path/filepath"
	"runtime"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hostinfo"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// mi1Options are the flags of `recspike mi1`.
type mi1Options struct {
	controller, window, kind string
	real, resume, lab, smoke bool
	shard                    int
}

// parseMI1 reads and checks the flags of `recspike mi1` before anything
// runs.
func parseMI1(args []string, out io.Writer) (mi1Options, *mi1.Window, error) {
	var o mi1Options
	flags := flag.NewFlagSet("mi1", flag.ContinueOnError)
	flags.StringVar(&o.controller, "controller", "", "")
	flags.StringVar(&o.window, "window", "", "")
	flags.StringVar(&o.kind, "kind", "", "")
	flags.BoolVar(&o.real, "real", false, "")
	flags.BoolVar(&o.resume, "resume", false, "")
	flags.BoolVar(&o.lab, "lab", false, "")
	flags.BoolVar(&o.smoke, "smoke", false, "")
	flags.IntVar(&o.shard, "shard", 0, "")
	if err := parseFlags(flags, args, out); err != nil {
		return o, nil, err
	}
	bad := func(msg string) (mi1Options, *mi1.Window, error) {
		_, _ = fmt.Fprint(out, usage)
		return o, nil, errors.New(msg)
	}
	switch {
	case o.lab:
		switch {
		case o.kind != "kill9" && o.kind != "qemu-reset":
			return bad("în laborator, --kind kill9 sau qemu-reset")
		case o.shard != 1 && o.shard != 2:
			return bad("--shard este 1 sau 2")
		case o.real || o.window != "" || o.resume:
			return bad("--lab nu primește --real, --window sau --resume")
		}
		return o, nil, nil
	case o.real:
		switch {
		case o.kind != "":
			return bad("--real nu primește --kind: tăierile reale le faceți dvs.")
		case o.window != "":
			return bad("--real nu primește --window: tăierile reale nu țin de fereastră")
		case o.controller == "" && !o.resume:
			return bad("lipsește --controller: adresa dispozitivului unde rulează reccontroller")
		case o.smoke || o.shard != 0:
			return bad("--smoke și --shard sunt doar pentru --lab")
		}
		return o, nil, nil
	}
	switch {
	case o.controller == "":
		return bad("lipsește --controller: adresa dispozitivului unde rulează reccontroller")
	case o.window == "":
		return bad("lipsește --window HH:MM-HH:MM: fereastra opririlor automate")
	case o.smoke || o.shard != 0 || o.resume:
		return bad("--smoke, --shard și --resume nu sunt pentru opririle automate")
	}
	w, err := mi1.ParseWindow(o.window)
	if err != nil {
		return bad(err.Error())
	}
	if o.kind != "kill9" && o.kind != "sysrq" && o.kind != "bugcheck" {
		return bad("--kind kill9, sysrq sau bugcheck")
	}
	return o, &w, nil
}

// mi1Command is `recspike mi1`: one set of MI1 on this host.
func mi1Command(ctx context.Context, args []string, in *bufio.Reader, out io.Writer) error {
	o, window, err := parseMI1(args, out)
	if err != nil {
		return err
	}
	f, err := ownFolders()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(f.work, 0o700); err != nil {
		return err
	}
	if o.lab && o.kind == "qemu-reset" && o.controller == "" {
		// This runner boots the guest that runs the shard against its
		// reccontroller (R03).
		q := &mi1.QEMU{Dir: filepath.Join(f.work, "qemu"), Bins: f.dir, Shard: o.shard, Smoke: o.smoke, Out: out}
		return q.Run(ctx)
	}
	d := &mi1.Driver{Hub: f.hub, Results: f.results, Work: f.work, Out: out}
	switch {
	case o.resume:
		if d.Controller, err = f.savedController(); err != nil {
			return err
		}
	case o.lab && o.controller == "":
		d.Controller = controllerAddr("127.0.0.1")
	default:
		d.Controller = controllerAddr(o.controller)
	}
	if err := f.saveController(d.Controller); err != nil {
		return err
	}
	if o.lab {
		set := mi1.LabSet(runtime.GOOS, o.kind)
		host := mi1.LabHost(runtime.GOOS)
		if o.kind == "qemu-reset" {
			host.Pseudonym = "lab-qemu"
		}
		info, err := hostinfo.Read(ctx, hostinfo.Options{Hub: f.hub, RecDir: f.work})
		if err != nil {
			return err
		}
		host.Storage, host.WriteCache = info.Storage, info.WriteCache
		d.Host = host
		d.Volumes = mi1.LabVolumes(o.kind, f.work)
		return d.RunSet(ctx, mi1.SetOptions{Set: set, Mode: mi1.ModeLab, Kind: o.kind, Shard: o.shard, Smoke: o.smoke})
	}
	set, err := mi1.SetFor(ownerHost(), o.kind, o.real)
	if err != nil {
		return err
	}
	d.ConfigDir = protectedDir() // the owner's runs start ipcam-hub elevated
	// A new set reads the host with diag host, with the owner's answers for
	// an unknown Pi model and an SD card (C24, G16), once it is sure to
	// start.
	host := func(ctx context.Context) (mi1.HostState, error) {
		return mi1.OwnerHost(ctx, f.hub, f.work, runtime.GOOS, in, out)
	}
	if o.real {
		return d.RunSet(ctx, mi1.SetOptions{Set: set, Mode: mi1.ModeReal, Kind: "power", Resume: o.resume, Host: host})
	}
	return d.RunSet(ctx, mi1.SetOptions{Set: set, Mode: mi1.ModeAutomated, Kind: o.kind, Window: window, WaitForWindow: true, Host: host})
}

// resumeCommand is `recspike resume`, which the boot task or service runs
// after each reboot (G14).
func resumeCommand(ctx context.Context, args []string, out io.Writer) error {
	if err := parseFlags(flag.NewFlagSet("resume", flag.ContinueOnError), args, out); err != nil {
		return err
	}
	f, err := ownFolders()
	if err != nil {
		return err
	}
	// The boot task and the boot service resume as administrator, or root.
	d := &mi1.Driver{Hub: f.hub, Results: f.results, Work: f.work, Out: out, ConfigDir: protectedDir()}
	if d.Controller, err = f.savedController(); err != nil {
		if st, _ := d.State(); st == nil {
			_, _ = fmt.Fprintln(out, "Nicio probă MI1 de continuat.")
			return nil
		}
		return err
	}
	return d.Resume(ctx)
}
