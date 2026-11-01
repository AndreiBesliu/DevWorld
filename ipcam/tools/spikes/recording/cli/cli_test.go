package cli

import (
	"errors"
	"flag"
	"strings"
	"testing"
)

const usage = "Folosire:\n  demo [-n N] [-v]\n"

// TestParseFlags_RomanianUsageAndErrors: every error of the flag package
// reaches the owner in Romanian, after the command's own usage, and the
// flag package's English is never printed; -h prints the usage and returns
// flag.ErrHelp.
func TestParseFlags_RomanianUsageAndErrors(t *testing.T) {
	for args, want := range map[string]string{
		"-nope":   "opțiune necunoscută: -nope",
		"-n":      "opțiunea cere o valoare: -n",
		"-n x":    "valoare greșită pentru -n",
		"-v=pe":   "valoare greșită pentru -v",
		"---n=1":  "opțiune scrisă greșit: ---n=1",
		"-n 1 in": "argument necunoscut: in",
	} {
		flags := flag.NewFlagSet("demo", flag.ContinueOnError)
		flags.Int("n", 0, "")
		flags.Bool("v", false, "")
		var out strings.Builder
		err := ParseFlags(flags, strings.Fields(args), &out, usage)
		if err == nil || err.Error() != want {
			t.Errorf("demo %s = %v, want %q", args, err, want)
		}
		if out.String() != usage {
			t.Errorf("demo %s printed %q, want only the usage", args, out.String())
		}
	}
	flags := flag.NewFlagSet("demo", flag.ContinueOnError)
	n := flags.Int("n", 0, "")
	var out strings.Builder
	if err := ParseFlags(flags, []string{"-n", "3"}, &out, usage); err != nil || *n != 3 || out.Len() != 0 {
		t.Errorf("demo -n 3 = %v, n %d, printed %q", err, *n, out.String())
	}
	out.Reset()
	if err := ParseFlags(flag.NewFlagSet("demo", flag.ContinueOnError), []string{"-h"}, &out, usage); !errors.Is(err, flag.ErrHelp) || out.String() != usage {
		t.Errorf("demo -h = %v, printed %q", err, out.String())
	}
	if got := FlagError(errors.New("something new")); got != "opțiuni greșite: something new" {
		t.Errorf("FlagError of an unknown error = %q", got)
	}
}
