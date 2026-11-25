// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Command uploadspike-probe is the first program that the owner runs on
// the Raspberry Pi for the upload spike (P17), built for ARMv6 so that it
// runs on every Pi: it prints the Pi's model, from /proc/cpuinfo's Model
// line or else the device tree's, and the release archive that the Pi
// needs, chosen by `uname -m`. An armv6 Pi cannot run the hub, so its part
// of the probe moves to Faza 0-bis: the probe writes the marker that says
// so (results.PiProbeMarker, as c1's) into results/MI5/pi-probe/ next to
// it, and, since uploadspike cannot run on armv6, zips that folder itself
// into results-pi.zip in the current folder, which the owner sends (guide
// step 13). It imports only the results envelope, to stay small. Every
// line it prints is Romanian.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The probe's lines.
const (
	lineModel        = "Modelul: %s (din %s)"
	lineModelUnknown = "Modelul: necunoscut (nici /proc/cpuinfo, nici /proc/device-tree/model nu îl numesc)"
	lineMachine      = "Procesorul: %s"
	lineArchive      = "Pe acest Pi folosiți arhiva %s."
	lineArmv6        = "Acest Raspberry Pi (armv6) nu poate rula hub-ul. Proba de upload pe Pi trece în Faza 0-bis. Folosiți PC-ul."
	lineZip          = "Am notat în rezultate că Pi-ul este armv6: trimiteți-ne fișierul %s."
	errMachine       = "procesorul %s nu este al unui Raspberry Pi pe care proba îl cunoaște: porniți uploadspike-probe pe Raspberry Pi"
	errUname         = "nu pot afla procesorul (uname -m): %w"
	errWrite         = "nu pot scrie în rezultate că Pi-ul este armv6: %w"
	errZip           = "nu pot face arhiva rezultatelor: %w"
	lineError        = "uploadspike-probe: eroare:"
)

// The files that name the Pi's model.
const (
	cpuinfoPath    = "/proc/cpuinfo"
	deviceTreePath = "/proc/device-tree/model"
)

// archives are the release archives by `uname -m`.
var archives = map[string]string{
	"aarch64": "spike-upload-linux-arm64.tar.gz",
	"armv7l":  "spike-upload-linux-armv7.tar.gz",
}

// armv6 is `uname -m` on an ARMv6 Pi: Pi 1, Zero, Zero W and CM1.
const armv6 = "armv6l"

// probe is where the probe reads the Pi, keeps its results and puts the
// zip; tests give their own.
type probe struct {
	cpuinfo, deviceTree string
	machine             func() (string, error)
	results, zipDir     string
	now                 func() time.Time
}

func main() {
	p, err := systemProbe()
	if err == nil {
		err = run(p, os.Stdout)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, lineError, err)
		os.Exit(1)
	}
}

// systemProbe reads this Pi, with results/ next to the probe and the zip
// in the current folder.
func systemProbe() (probe, error) {
	exe, err := os.Executable()
	if err != nil {
		return probe{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return probe{}, err
	}
	return probe{cpuinfo: cpuinfoPath, deviceTree: deviceTreePath, machine: uname,
		results: filepath.Join(filepath.Dir(exe), "results"), zipDir: cwd, now: time.Now}, nil
}

// uname is `uname -m`: the machine's architecture.
func uname() (string, error) {
	out, err := exec.Command("uname", "-m").Output()
	return strings.TrimSpace(string(out)), err
}

// run prints what p reads of the Pi and, on armv6, writes the marker and
// zips it.
func run(p probe, out io.Writer) error {
	machine, err := p.machine()
	if err != nil {
		return fmt.Errorf(errUname, err)
	}
	m := readModel(p.cpuinfo, p.deviceTree)
	if m.raw == "" {
		_, _ = fmt.Fprintln(out, lineModelUnknown)
	} else {
		_, _ = fmt.Fprintf(out, lineModel+"\n", m.raw, m.from)
	}
	_, _ = fmt.Fprintf(out, lineMachine+"\n", machine)
	if archive, ok := archives[machine]; ok {
		_, _ = fmt.Fprintf(out, lineArchive+"\n", archive)
		return nil
	}
	if machine != armv6 {
		return fmt.Errorf(errMachine, machine)
	}
	_, _ = fmt.Fprintln(out, lineArmv6)
	marker := results.PiProbeMarker(m.raw, m.revision, m.source, p.now().UnixMilli())
	if err := results.Write(p.results, marker); err != nil {
		return fmt.Errorf(errWrite, err)
	}
	zipPath := filepath.Join(p.zipDir, "results-pi.zip")
	if err := zipFolder(p.results, filepath.Join("MI5", results.PiProbeSet), zipPath); err != nil {
		return fmt.Errorf(errZip, err)
	}
	_, _ = fmt.Fprintf(out, lineZip+"\n", zipPath)
	return nil
}

// zipFolder zips root's rel folder into path, its entries named
// results/<rel>/…, as uploadspike results names them.
func zipFolder(root, rel, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // the owner's own folder
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	walkErr := filepath.WalkDir(filepath.Join(root, rel), func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		name, err := filepath.Rel(filepath.Dir(root), p)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(name))
		if err != nil {
			return err
		}
		src, err := os.Open(p) //nolint:gosec // a file of results/
		if err != nil {
			return err
		}
		_, err = io.Copy(w, src)
		return errors.Join(err, src.Close())
	})
	return errors.Join(walkErr, zw.Close(), f.Close())
}

// model is the Pi's model as the probe read it: raw, its revision, and
// where it read raw (source, as results names it, and from, the file).
type model struct {
	raw, revision, source, from string
}

// readModel reads the model from cpuinfo's Model line, else from the
// device tree's model, without its trailing NUL; raw is empty when
// neither names it. The revision is cpuinfo's.
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
