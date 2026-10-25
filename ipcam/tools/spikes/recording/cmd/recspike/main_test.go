package main

import (
	"errors"
	"flag"
	"strings"
	"testing"
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
