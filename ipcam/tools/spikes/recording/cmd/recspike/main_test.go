package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// TestCLI_RomanianUsageAndErrors: recspike's usage and its argument errors
// are Romanian, never the flag package's English, and a set's flags are
// checked before anything runs.
func TestCLI_RomanianUsageAndErrors(t *testing.T) {
	for args, want := range map[string]string{
		"":                                      "lipsește comanda",
		"zbor":                                  "comandă necunoscută: zbor",
		"mi1 -nope":                             "opțiune necunoscută: -nope",
		"mi1 --kind":                            "opțiunea cere o valoare: -kind",
		"mi1 --lab --kind kill9 --shard 3":      "--shard este 1 sau 2",
		"mi1 --lab --kind sysrq --shard 1":      "--kind kill9 sau qemu-reset",
		"mi1 --kind kill9 --window 22:00-06:00": "--controller",
		"mi1 --controller 192.0.2.1 --kind kill9":                      "--window",
		"mi1 --controller 192.0.2.1 --kind kill9 --window 25:00-06:00": "fereastra",
		"mi1 --controller 192.0.2.1 --window 22:00-06:00 --kind power": "--kind kill9, sysrq sau bugcheck",
		"mi1 --real --kind kill9":                                      "--real nu primește --kind",
		"mi1 --real":                                                   "--controller",
		"mi2":                                                          "--controller",
		"mi2 --controller 127.0.0.1 --hours 0":                         "--hours",
		"mi2 --controller 127.0.0.1 extra":                             "argument necunoscut: extra",
		"mi2 --controller 192.0.2.1":                                   "reccontroller -local",
		"mi2 --controller 192.0.2.1:7081":                              "reccontroller -local",
		"mi2 --controller pi.lan":                                      "reccontroller -local",
		"mi2 --controller localhost --hours 0":                         "--hours",
		"mi2 --controller 127.0.0.2:7081 --hours 0":                    "--hours",
		"mi2 --controller [::1]:7081":                                  "IPv4",
		"mi2 --controller ::1":                                         "IPv4",
		"mi3":                                                          "--controller",
		"mi3 --controller 127.0.0.1":                                   "chiar acest dispozitiv",
		"mi3 --controller localhost:7081":                              "chiar acest dispozitiv",
		"mi3 --controller 192.0.2.1 extra":                             "argument necunoscut: extra",
		"mi4":                                                          "--controller",
		"mi4 --controller 192.0.2.1 extra":                             "argument necunoscut: extra",
		"mi3-load":                                                     "mi3-load primește una dintre încărcările live, playback, export, upload",
		"mi3-load stream":                                              "mi3-load primește una dintre încărcările",
		"push":                                                         "--controller",
		"push --controller 192.0.2.1 extra":                            "argument necunoscut: extra",
		"resume now":                                                   "argument necunoscut: now",
		"install --force":                                              "opțiune necunoscută: -force",
	} {
		var out strings.Builder
		err := run(t.Context(), strings.Fields(args), nil, &out)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("recspike %s = %v, want an error with %q", args, err, want)
		}
		if strings.Contains(out.String(), "Usage") || strings.Contains(out.String(), "flag provided") {
			t.Errorf("recspike %s printed English:\n%s", args, out.String())
		}
	}
	var out strings.Builder
	if err := run(t.Context(), []string{"-h"}, nil, &out); !errors.Is(err, flag.ErrHelp) || !strings.Contains(out.String(), "Folosire:") {
		t.Errorf("recspike -h = %v, printed:\n%s", err, out.String())
	}
	if got := controllerAddr("192.0.2.1"); got != "192.0.2.1:7081" {
		t.Errorf("controllerAddr = %q, want the link port added", got)
	}
	if got := controllerAddr("192.0.2.1:9000"); got != "192.0.2.1:9000" {
		t.Errorf("controllerAddr = %q, want the port kept", got)
	}
}

// TestMI1_RefusesAnotherSetBeforeAskingTheOwner (G16): `recspike mi1` for a
// new set while another set is still in progress in the same results
// folder refuses at once, in Romanian: it reads no host and asks the owner
// nothing (an SD card's «DA»), since that set would not start.
func TestMI1_RefusesAnotherSetBeforeAskingTheOwner(t *testing.T) {
	dir := t.TempDir()
	realExecutable := executable
	t.Cleanup(func() { executable = realExecutable })
	executable = func() (string, error) { return filepath.Join(dir, "recspike"), nil }
	inProgress, other := "kill9", "sysrq"
	if ownerHost() == "pc" {
		other = "bugcheck"
	}
	set, err := mi1.SetFor(ownerHost(), inProgress, false)
	if err != nil {
		t.Fatal(err)
	}
	st := &mi1.RunState{Set: set, Mode: mi1.ModeAutomated, Kind: inProgress, Window: "22:00-06:00", Host: mi1.HostState{Pseudonym: ownerHost()}}
	if err := mi1.SaveRunState(filepath.Join(dir, "results", "state", "run-state.json"), st); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err = run(t.Context(), []string{"mi1", "--controller", "127.0.0.1", "--window", "00:00-23:59", "--kind", other},
		bufio.NewReader(strings.NewReader("DA\n")), &out)
	if err == nil || !strings.Contains(err.Error(), "proba "+set+" nu s-a încheiat") {
		t.Errorf("recspike mi1 --kind %s while %s is in progress = %v, want the Romanian refusal before anything else", other, set, err)
	}
	if strings.Contains(out.String(), "Scrieți DA") {
		t.Errorf("the owner was asked:\n%s", out.String())
	}
}

// TestMI3_RefusesAControllerOnThisDevice (G20; Task 9 review): recspike mi3
// refuses a --controller that is this device, also by one of its own
// interface addresses, such as its LAN IP: a reccontroller started there
// without -local would load the CPU that MI3 measures. Another device's
// address goes on to the host's checks.
func TestMI3_RefusesAControllerOnThisDevice(t *testing.T) {
	previous := interfaceAddrs
	interfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("192.0.2.7"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("2001:db8::7"), Mask: net.CIDRMask(64, 128)},
		}, nil
	}
	t.Cleanup(func() { interfaceAddrs = previous })
	for _, addr := range []string{"192.0.2.7", "192.0.2.7:7081", "[2001:db8::7]:7081", "127.0.0.1"} {
		var out strings.Builder
		if err := run(t.Context(), []string{"mi3", "--controller", addr}, nil, &out); err == nil || !strings.Contains(err.Error(), "chiar acest dispozitiv") {
			t.Errorf("recspike mi3 --controller %s = %v, want it refused as this device", addr, err)
		}
	}
	var out strings.Builder
	if err := run(t.Context(), []string{"mi3", "--controller", "192.0.2.8"}, nil, &out); err != nil && strings.Contains(err.Error(), "chiar acest dispozitiv") {
		t.Errorf("recspike mi3 --controller 192.0.2.8 = %v: another device's address was refused", err)
	}
}

// TestMI3_ControllerByNameOrUnlistedInterfaces (Task 9b review): recspike
// mi3 resolves a --controller that is a host name and refuses it when it
// names this device; it refuses, in Romanian, a name that does not resolve
// and a device whose own addresses it cannot list, since it then cannot
// tell that the controller is on the other device.
func TestMI3_ControllerByNameOrUnlistedInterfaces(t *testing.T) {
	previousAddrs, previousLookup := interfaceAddrs, lookupIP
	t.Cleanup(func() { interfaceAddrs, lookupIP = previousAddrs, previousLookup })
	listed := func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("192.0.2.7"), Mask: net.CIDRMask(24, 32)}}, nil
	}
	lookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "pc.lan":
			return []net.IP{net.ParseIP("2001:db8::9"), net.ParseIP("192.0.2.7")}, nil
		case "loop.lan":
			return []net.IP{net.ParseIP("127.0.1.1")}, nil
		case "pi.lan":
			return []net.IP{net.ParseIP("192.0.2.8")}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	for _, c := range []struct {
		addr  string
		addrs func() ([]net.Addr, error)
		want  string // "" when the address goes on to the host's checks
	}{
		{"pc.lan", listed, "chiar acest dispozitiv"},
		{"pc.lan:7081", listed, "chiar acest dispozitiv"},
		{"loop.lan", listed, "chiar acest dispozitiv"},
		{"nowhere.lan", listed, "nu pot afla adresa lui nowhere.lan"},
		{"pi.lan", listed, ""},
		{"192.0.2.8", func() ([]net.Addr, error) { return nil, errors.New("netlink: permission denied") }, "nu pot citi adresele acestui dispozitiv"},
	} {
		interfaceAddrs = c.addrs
		var out strings.Builder
		err := run(t.Context(), []string{"mi3", "--controller", c.addr}, nil, &out)
		switch {
		case c.want == "" && err != nil && (strings.Contains(err.Error(), "chiar acest dispozitiv") || strings.Contains(err.Error(), "nu pot")):
			t.Errorf("recspike mi3 --controller %s = %v: another device's address was refused", c.addr, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("recspike mi3 --controller %s = %v, want it refused with %q", c.addr, err, c.want)
		}
	}
}

// TestMain_OwnersStopEndsInARomanianLine (Task 9b review): after Ctrl+C,
// recspike's last line is wholly Romanian, for MI2 and MI3 alike, never
// Go's «context canceled», and it exits 1 as before; other errors keep
// their line.
func TestMain_OwnersStopEndsInARomanianLine(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("proba MI2 s-a întrerupt: %w", context.Canceled),
		fmt.Errorf("proba MI3 s-a întrerupt: %w", context.Canceled),
	} {
		var stderr strings.Builder
		if code := report(err, &stderr); code != 1 || stderr.String() != "Proba a fost oprită de dvs.\n" {
			t.Errorf("report(%v) = %d, printed %q; want 1 and «Proba a fost oprită de dvs.»", err, code, stderr.String())
		}
	}
	var stderr strings.Builder
	if code := report(errors.New("lipsește comanda"), &stderr); code != 1 || stderr.String() != "recspike: eroare: lipsește comanda\n" {
		t.Errorf("report of an error = %d, printed %q", code, stderr.String())
	}
	stderr.Reset()
	if code := report(flag.ErrHelp, &stderr); code != 1 || stderr.String() != "" {
		t.Errorf("report of the help = %d, printed %q", code, stderr.String())
	}
	if code := report(nil, &stderr); code != 0 {
		t.Errorf("report(nil) = %d, want 0", code)
	}
}
