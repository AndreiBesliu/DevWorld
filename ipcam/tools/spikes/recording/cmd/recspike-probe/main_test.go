package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The /proc/cpuinfo of a Pi 4, whose kernel names the model, and of a Pi
// Zero W, whose older kernel does not.
const (
	cpuinfoPi4 = "processor\t: 0\nBogoMIPS\t: 108.00\n\nHardware\t: BCM2835\nRevision\t: d03115\n" +
		"Model\t\t: Raspberry Pi 4 Model B Rev 1.5\n"
	cpuinfoNoModel = "processor\t: 0\nmodel name\t: ARMv6-compatible processor rev 7 (v6l)\n\nHardware\t: BCM2835\nRevision\t: 9000c1\n"
)

// fakePi is a Pi for the probe: its cpuinfo, its device tree's model, if
// any, uname -m, and the folder next to the probe.
func fakePi(t *testing.T, cpuinfo, deviceTree, machine string) probe {
	t.Helper()
	dir := t.TempDir()
	p := probe{
		cpuinfo:    filepath.Join(dir, "cpuinfo"),
		deviceTree: filepath.Join(dir, "model"),
		machine:    func() (string, error) { return machine, nil },
		results:    filepath.Join(dir, "results"),
		now:        func() time.Time { return time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC) },
	}
	if err := os.WriteFile(p.cpuinfo, []byte(cpuinfo), 0o600); err != nil {
		t.Fatal(err)
	}
	if deviceTree != "" {
		if err := os.WriteFile(p.deviceTree, []byte(deviceTree+"\x00"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// probeOut runs the probe with args and returns what it printed.
func probeOut(t *testing.T, p probe, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(t.Context(), p, args, &out)
	return out.String(), err
}

// TestProbe_ChoosesArchiveByMachine (G32): the probe names the archive to
// use by uname -m: arm64 on aarch64, armv7 on armv7l; on armv6l none, but
// the Romanian explanation that the Pi can only be the controller, with
// reccontroller-linux-armv6, and that its measurements move to Faza 0-bis.
func TestProbe_ChoosesArchiveByMachine(t *testing.T) {
	for _, c := range []struct {
		machine    string
		has, lacks []string
	}{
		{"aarch64", []string{"Raspberry Pi 4 Model B Rev 1.5", "spike-recording-linux-arm64.tar.gz"}, []string{"armv7", "Faza 0-bis"}},
		{"armv7l", []string{"Raspberry Pi 4 Model B Rev 1.5", "spike-recording-linux-armv7.tar.gz"}, []string{"arm64", "Faza 0-bis"}},
		{"armv6l", []string{"reccontroller-linux-armv6", "Faza 0-bis", "--push"}, []string{".tar.gz"}},
	} {
		out, err := probeOut(t, fakePi(t, cpuinfoPi4, "", c.machine))
		if err != nil {
			t.Errorf("%s: %v", c.machine, err)
		}
		for _, want := range c.has {
			if !strings.Contains(out, want) {
				t.Errorf("%s: the probe printed\n%s\nwithout «%s»", c.machine, out, want)
			}
		}
		for _, bad := range c.lacks {
			if strings.Contains(out, bad) {
				t.Errorf("%s: the probe printed\n%s\nwith «%s»", c.machine, out, bad)
			}
		}
	}
	out, err := probeOut(t, fakePi(t, "", "", "x86_64"))
	if err == nil || strings.Contains(out, ".tar.gz") {
		t.Errorf("x86_64: %q, %v; want no archive and an error", out, err)
	}
}

// TestProbe_DeviceTreeFallback (G32): when cpuinfo names no model, the
// probe reads the device tree's, without its trailing NUL, and says where
// it read it.
func TestProbe_DeviceTreeFallback(t *testing.T) {
	out, err := probeOut(t, fakePi(t, cpuinfoNoModel, "Raspberry Pi Zero W Rev 1.1", "armv6l"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Raspberry Pi Zero W Rev 1.1 (din /proc/device-tree/model)") || strings.Contains(out, "\x00") {
		t.Errorf("the probe printed\n%q\nwithout the device tree's model and its source", out)
	}
	out, err = probeOut(t, fakePi(t, cpuinfoPi4, "Raspberry Pi Zero W Rev 1.1", "aarch64"))
	if err != nil || !strings.Contains(out, "Raspberry Pi 4 Model B Rev 1.5 (din /proc/cpuinfo)") {
		t.Errorf("with a model in cpuinfo the probe printed\n%s\n%v; want cpuinfo's", out, err)
	}
	out, err = probeOut(t, fakePi(t, cpuinfoNoModel, "", "armv7l"))
	if err != nil || !strings.Contains(out, "necunoscut") {
		t.Errorf("without any model the probe printed\n%s\n%v; want «necunoscut»", out, err)
	}
}

// TestProbe_WritesMarkerOnArmv6Only (G31, G32; Task 12 review M9): on an
// armv6 Pi, the probe writes the marker that tells the judges and the
// report that the Pi is unsupported: results.PiProbeMarker, with the
// model it read; on a Pi that the spike measures it writes nothing.
func TestProbe_WritesMarkerOnArmv6Only(t *testing.T) {
	p := fakePi(t, cpuinfoNoModel, "Raspberry Pi Zero W Rev 1.1", "armv6l")
	if _, err := probeOut(t, p); err != nil {
		t.Fatal(err)
	}
	e, err := results.Read(filepath.Join(p.results, "MI1", results.PiProbeSet, results.PiProbeSet+"-1.json"))
	if err != nil {
		t.Fatalf("no marker: %v", err)
	}
	m := e.PiModel
	if e.Host != "pi" || e.Code != "MI1" || len(e.Runs) != 0 || m == nil || !m.Known || m.Supported ||
		m.Raw != "Raspberry Pi Zero W Rev 1.1" || m.Source != "device-tree" || m.Revision != "9000c1" {
		t.Errorf("marker %+v, model %+v; want host pi, no run, the model known and unsupported", e, m)
	}
	for _, machine := range []string{"aarch64", "armv7l"} {
		p := fakePi(t, cpuinfoPi4, "", machine)
		if _, err := probeOut(t, p); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(p.results); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: the probe wrote results (%v); want none", machine, err)
		}
	}
}

// TestProbe_PushSendsTheResults (G15): on an armv6 Pi, which has no
// recspike, `recspike-probe --push <PC>` sends the results folder, the
// marker included, as `recspike push` does: reccontroller pull's side is
// the link's PullResults. On a Pi that the spike measures, --push refuses
// and names recspike push.
func TestProbe_PushSendsTheResults(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	pulled := t.TempDir()
	done := make(chan error, 1)
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = nc.Close() }()
		c := link.NewConn(nc)
		m, err := c.Receive()
		if hello, ok := m.(link.Hello); err != nil || !ok || hello.Host != "pi" {
			done <- errors.Join(err, errors.New("no hello from the Pi"))
			return
		}
		done <- c.PullResults(pulled)
	}()

	p := fakePi(t, cpuinfoNoModel, "Raspberry Pi Zero W Rev 1.1", "armv6l")
	out, err := probeOut(t, p, "--push", ln.Addr().String())
	if err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	if err := <-done; err != nil {
		t.Fatalf("the pull: %v", err)
	}
	if _, err := results.Read(filepath.Join(pulled, "MI1", results.PiProbeSet, results.PiProbeSet+"-1.json")); err != nil {
		t.Errorf("the PC did not get the marker: %v", err)
	}
	if !strings.Contains(out, "Am trimis rezultatele") {
		t.Errorf("the probe printed\n%s\nwithout saying that the results went out", out)
	}

	out, err = probeOut(t, fakePi(t, cpuinfoPi4, "", "aarch64"), "--push", ln.Addr().String())
	if err == nil || !strings.Contains(err.Error(), "sudo ./recspike push --controller") {
		t.Errorf("--push on an arm64 Pi = %q, %v; want a refusal that names recspike push", out, err)
	}
}

// TestProbe_CtrlCDuringThePushIsTheCancel (Task 13 review, M1): Ctrl+C
// before the probe reaches the PC is the owner's own stop, as for
// `recspike push`, not «nu mă pot conecta la PC».
func TestProbe_CtrlCDuringThePushIsTheCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := pushResults(ctx, "192.0.2.1", t.TempDir(), io.Discard)
	if !errors.Is(err, context.Canceled) || strings.Contains(fmt.Sprint(err), "nu mă pot conecta") {
		t.Errorf("pushResults after Ctrl+C = %v, want context.Canceled", err)
	}
}
