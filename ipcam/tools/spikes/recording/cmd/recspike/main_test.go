package main

import (
	"bufio"
	"errors"
	"flag"
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
