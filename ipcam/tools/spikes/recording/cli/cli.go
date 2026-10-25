// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package cli holds what the spike's command lines (recspike and
// reccontroller) share: flag parsing whose usage and errors reach the owner
// in Romanian, never in the flag package's English.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// ParseFlags parses args with flags. On -h it prints usage and returns
// flag.ErrHelp; on an error, or an argument left over, it prints usage and
// returns the error in Romanian.
func ParseFlags(flags *flag.FlagSet, args []string, out io.Writer, usage string) error {
	flags.SetOutput(io.Discard)
	err := flags.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		_, _ = fmt.Fprint(out, usage)
		return err
	case err != nil:
		_, _ = fmt.Fprint(out, usage)
		return errors.New(FlagError(err))
	case flags.NArg() > 0:
		_, _ = fmt.Fprint(out, usage)
		return fmt.Errorf("argument necunoscut: %s", flags.Arg(0))
	}
	return nil
}

// FlagError translates an error of the flag package into Romanian.
func FlagError(err error) string {
	msg := err.Error()
	for _, t := range []struct{ en, ro string }{
		{"flag provided but not defined: ", "opțiune necunoscută: "},
		{"flag needs an argument: ", "opțiunea cere o valoare: "},
		{"bad flag syntax: ", "opțiune scrisă greșit: "},
	} {
		if rest, ok := strings.CutPrefix(msg, t.en); ok {
			return t.ro + rest
		}
	}
	if strings.HasPrefix(msg, "invalid ") {
		if _, rest, ok := strings.Cut(msg, " for "); ok {
			flagName, _, _ := strings.Cut(strings.TrimPrefix(rest, "flag "), ":")
			return "valoare greșită pentru " + flagName
		}
	}
	return "opțiuni greșite: " + msg
}
