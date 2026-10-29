// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Command recreport turns the recorder spike's results into the owner's
// report of Phase 0 (c1): a Romanian Markdown report with the verdicts of
// G10–G13 and G18, the decisions they trigger (PH0-30) and what moves to
// Faza 0-bis, and one CSV per measurement with every sample.
//
//	recreport -in <dir> [-in <dir>…] [-annotations <workflow run id>…] -out docs/research [-date YYYY-MM-DD]
//
// It reads every envelope of the results folders, the PC's with the Pi's
// pulled into it, each envelope once (G15), the app's AGE results under
// app/, and, for each workflow run, the check-run annotations of its jobs
// through gh (C32). Each gate is judged once, by its package's Judge
// (C17); recreport only renders. It writes
// <out>/<date>-phase0c-recorder-results.md and
// <out>/phase0-data/phase0c-recorder-{mi1,mi2,mi3,mi4,mi9}.csv, the date
// being the day of the newest result unless -date gives it. Before it
// writes anything, the redaction scanner of the video spike reads every
// file: any match writes nothing and fails, naming the file, the line and
// the kind, never the text (PH0-22, G28).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/redact"
)

// dateLayout is the layout of -date and of the date in the report's name.
const dateLayout = "2006-01-02"

// runIDPattern is a workflow run id: digits only, as it goes into gh's
// arguments.
var runIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

// repeated is a flag that may be given more than once.
type repeated []string

func (l *repeated) String() string { return strings.Join(*l, ",") }

func (l *repeated) Set(v string) error {
	*l = append(*l, v)
	return nil
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("recreport", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var dirs, runs repeated
	flags.Var(&dirs, "in", "a results folder (repeatable): the PC's, with the Pi's pulled into it, or a saved copy")
	flags.Var(&runs, "annotations", "a workflow run id whose jobs' check-run annotations are read with gh (repeatable)")
	out := flags.String("out", filepath.Join("docs", "research"), "folder for the report; the CSV files go to phase0-data/ inside it")
	date := flags.String("date", "", "date of the report, YYYY-MM-DD (default: the day of the newest result)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	switch {
	case flags.NArg() > 0:
		say(stderr, "recreport: takes flags only")
		return 2
	case len(dirs) == 0 && len(runs) == 0:
		say(stderr, "recreport: give at least one -in <folder> or -annotations <workflow run id>")
		return 2
	}
	for _, id := range runs {
		if !runIDPattern.MatchString(id) {
			say(stderr, "recreport: -annotations takes a workflow run id, digits only")
			return 2
		}
	}
	if *date != "" {
		if _, err := time.Parse(dateLayout, *date); err != nil {
			say(stderr, "recreport: -date must be YYYY-MM-DD")
			return 2
		}
	}

	in, err := load(dirs, runs)
	if err != nil {
		say(stderr, "recreport: %v", err)
		return 1
	}
	for _, s := range in.skipped {
		if s.err != nil {
			say(stderr, "recreport: skipped %s: %s (%v)", s.file, s.reason, s.err)
		} else {
			say(stderr, "recreport: skipped %s: %s", s.file, s.reason)
		}
	}
	if *date == "" {
		newest := in.newest()
		if newest.IsZero() {
			say(stderr, "recreport: no result says when it was made: give the date with -date")
			return 1
		}
		*date = newest.UTC().Format(dateLayout)
	}

	r := newReport(in, *date)
	files := r.files()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	if leaks := scanOutputs(names, files); len(leaks) > 0 {
		say(stderr, "recreport: nothing written: the redaction scanner (PH0-22, G28) found text that must not be published:")
		for _, leak := range leaks {
			say(stderr, "recreport:   %s", leak)
		}
		say(stderr, "recreport: remove it from the results, or pseudonymise it, then run again")
		return 1
	}
	for _, name := range names {
		path := filepath.Join(*out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // documentation, meant to be committed and read
			say(stderr, "recreport: %v", err)
			return 1
		}
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil { //nolint:gosec // documentation, meant to be committed and read
			say(stderr, "recreport: %v", err)
			return 1
		}
		say(stdout, "wrote %s", path)
	}
	return 0
}

// scanOutputs runs the redaction scanner on every file the run would write,
// and lists each match as <file>:<line>:<column>: <kind>, without the text
// it found.
func scanOutputs(names []string, files map[string]string) []string {
	var leaks []string
	for _, name := range names {
		for _, m := range redact.Scan(files[name]) {
			leaks = append(leaks, fmt.Sprintf("%s:%d:%d: %s", name, m.Line, m.Column, m.Kind))
		}
	}
	return leaks
}

// say writes a line to w. A message that cannot be written has nowhere else
// to go.
func say(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format+"\n", args...) }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
