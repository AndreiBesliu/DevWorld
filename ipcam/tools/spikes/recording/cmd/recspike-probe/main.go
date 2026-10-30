// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Command recspike-probe is the first program that the owner runs on the
// Raspberry Pi (G32). Built for ARMv6, it runs on every Pi: it prints the
// Pi's model, from /proc/cpuinfo's Model line or else the device tree's,
// and the release archive that the Pi needs, chosen by `uname -m`. An
// armv6 Pi gets no archive: the spike cannot measure it, so it can only be
// the controller (reccontroller-linux-armv6), and its measurements move to
// Faza 0-bis. The probe then writes the marker that says so
// (results.PiProbeMarker) into results/ next to it, and `recspike-probe
// --push <PC>` sends that folder to the PC's `reccontroller pull`, as
// `recspike push` does on a Pi that the spike measures (G15). It imports
// only the link and the results envelope, to stay small. Every line it
// prints is Romanian.
//
//	recspike-probe-linux-armv6 [--push ADRESA_PC]
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// usage is the command's help, in Romanian.
const usage = `Folosire:
  recspike-probe-linux-armv6
      arată modelul acestui Raspberry Pi și arhiva probei pe care o folosește.
  recspike-probe-linux-armv6 --push ADRESA_PC
      doar pe un Pi armv6: trimite rezultatele către reccontroller pull de pe PC.
`

// The files that name the Pi's model (G32).
const (
	cpuinfoPath    = "/proc/cpuinfo"
	deviceTreePath = "/proc/device-tree/model"
)

// archives are the release archives by `uname -m`.
var archives = map[string]string{
	"aarch64": "spike-recording-linux-arm64.tar.gz",
	"armv7l":  "spike-recording-linux-armv7.tar.gz",
}

// armv6 is `uname -m` on an ARMv6 Pi: Pi 1, Zero, Zero W and CM1.
const armv6 = "armv6l"

// probe is where the probe reads the Pi and keeps its results; tests give
// their own.
type probe struct {
	cpuinfo, deviceTree string
	machine             func() (string, error)
	results             string
	now                 func() time.Time
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	p, err := systemProbe()
	if err == nil {
		err = run(ctx, p, os.Args[1:], os.Stdout)
	}
	stop()
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(os.Stderr, "recspike-probe: eroare:", err)
		}
		os.Exit(1)
	}
}

// systemProbe reads this Pi, with results/ next to the probe.
func systemProbe() (probe, error) {
	exe, err := os.Executable()
	if err != nil {
		return probe{}, err
	}
	return probe{cpuinfo: cpuinfoPath, deviceTree: deviceTreePath, machine: uname,
		results: filepath.Join(filepath.Dir(exe), "results"), now: time.Now}, nil
}

// uname is `uname -m`: the machine's architecture.
func uname() (string, error) {
	out, err := exec.Command("uname", "-m").Output()
	return strings.TrimSpace(string(out)), err
}

// run prints what p reads of the Pi and, on armv6, writes the marker and,
// with --push, sends the results to the PC.
func run(ctx context.Context, p probe, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("recspike-probe", flag.ContinueOnError)
	push := flags.String("push", "", "")
	if err := cli.ParseFlags(flags, args, out, usage); err != nil {
		return err
	}
	machine, err := p.machine()
	if err != nil {
		return fmt.Errorf("nu pot afla procesorul (uname -m): %w", err)
	}
	m := readModel(p.cpuinfo, p.deviceTree)
	if m.raw == "" {
		_, _ = fmt.Fprintln(out, "Modelul: necunoscut (nici /proc/cpuinfo, nici /proc/device-tree/model nu îl numesc)")
	} else {
		_, _ = fmt.Fprintf(out, "Modelul: %s (din %s)\n", m.raw, m.from)
	}
	_, _ = fmt.Fprintf(out, "Procesorul: %s\n", machine)
	if archive, ok := archives[machine]; ok {
		if *push != "" {
			return errors.New("pe acest Pi trimiteți rezultatele cu recspike: sudo ./recspike push --controller <adresa PC-ului>")
		}
		_, _ = fmt.Fprintf(out, "Pe acest Pi folosiți arhiva %s.\n", archive)
		return nil
	}
	if machine != armv6 {
		return fmt.Errorf("procesorul %s nu este al unui Raspberry Pi pe care proba îl cunoaște: porniți recspike-probe pe Raspberry Pi", machine)
	}
	_, _ = fmt.Fprintln(out, "Acest Raspberry Pi este armv6, pe care proba nu îl poate măsura. "+
		"Pi-ul poate fi doar controlorul, cu reccontroller-linux-armv6, cât testați PC-ul; măsurătorile Pi-ului trec în Faza 0-bis.")
	marker := results.PiProbeMarker(m.raw, m.revision, m.source, p.now().UnixMilli())
	if err := results.Write(p.results, marker); err != nil {
		return fmt.Errorf("nu pot scrie în rezultate că Pi-ul este armv6: %w", err)
	}
	if *push == "" {
		_, _ = fmt.Fprintln(out, "Am notat în rezultate că Pi-ul este armv6. La final, cu reccontroller pull pornit pe PC, "+
			"trimiteți rezultatele: ./recspike-probe-linux-armv6 --push <adresa PC-ului>")
		return nil
	}
	return pushResults(ctx, *push, p.results, out)
}

// pushResults sends the results folder dir to the PC's `reccontroller
// pull` at addr, with the link's port when addr has none (link.Push).
func pushResults(ctx context.Context, addr, dir string, out io.Writer) error {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, strconv.Itoa(link.Port))
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	c, err := link.Dial(dialCtx, addr)
	cancel()
	if err != nil {
		return fmt.Errorf("nu mă pot conecta la PC (portul %d; rulează acolo reccontroller pull?): %w", link.Port, err)
	}
	defer func() { _ = c.Close() }()
	var noPull *link.NoPullError
	switch err := link.Push(ctx, c, "pi", dir); {
	case errors.As(err, &noPull):
		return fmt.Errorf("PC-ul a închis legătura înainte să ceară rezultatele: %w", noPull.Err)
	case err != nil:
		return err
	}
	_, _ = fmt.Fprintln(out, "Am trimis rezultatele.")
	return nil
}

// model is the Pi's model as the probe read it: raw, its revision, and
// where it read raw (source, as results names it, and from, the file).
type model struct {
	raw, revision, source, from string
}

// readModel reads the model from cpuinfo's Model line, else from the
// device tree's model, without its trailing NUL; raw is empty when neither
// names it. The revision is cpuinfo's.
func readModel(cpuinfo, deviceTree string) model {
	var m model
	if data, err := os.ReadFile(cpuinfo); err == nil { //nolint:gosec // /proc/cpuinfo, or a test's file
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			key, value, ok := strings.Cut(sc.Text(), ":")
			switch key, value = strings.TrimSpace(key), strings.TrimSpace(value); {
			case !ok:
			case key == "Model":
				m.raw, m.source, m.from = value, "cpuinfo", cpuinfoPath
			case key == "Revision":
				m.revision = value
			}
		}
	}
	if m.raw != "" {
		return m
	}
	if data, err := os.ReadFile(deviceTree); err == nil { //nolint:gosec // the device tree, or a test's file
		if raw := strings.Trim(string(data), " \t\r\n\x00"); raw != "" {
			m.raw, m.source, m.from = raw, "device-tree", deviceTreePath
		}
	}
	return m
}
