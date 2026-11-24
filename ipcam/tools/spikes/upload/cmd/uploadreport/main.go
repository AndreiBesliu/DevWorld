// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Command uploadreport turns the upload spike's results into the owner's
// report of Phase 0 (c2): a Romanian Markdown report with the verdicts of
// G14, G15 (its Phase 0 part) and G17, the decisions they trigger (PH0-30),
// what moves to Faza 0-bis, and one CSV per measurement with every sample.
//
//	uploadreport -in <dir> [-in <dir>…] [-annotations <workflow run id>…] -onedrive free|paid|unknown
//	    [-d3 a|b|c] [-console-units <n>] [-date YYYY-MM-DD] [-out docs/research]
//	uploadreport -scan <file>…
//
// It runs from ipcam/. It reads every envelope of the results folders (the
// owner's zips, unpacked), the lab's mi5-lab notices through gh (C32, P09),
// keeps one envelope per run id (the one stored last), judges each gate
// once with its package's Judge, and only renders. It writes
// <out>/<date>-phase0c-upload-results.md and
// <out>/phase0-data/phase0c-upload-{mi5,mi6,mi8}.csv. Before it writes
// anything, every field it renders is checked against its shape, and every
// file goes through the video spike's redaction scanner and the token
// patterns (P45): any match writes nothing and fails, naming the file, the
// line and the kind, never the text (PH0-22, Review Focus 5).
//
// -scan applies the redaction scanner and the token patterns to committed
// files, and exits 1 on a match.
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

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/redact"
)

// dateLayout is the layout of -date and of the date in the report's name.
const dateLayout = "2006-01-02"

// runIDPattern is a workflow run id: digits only, as it goes into gh's
// arguments.
var runIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

// The answers of -onedrive (C2-R4): A5 passed; it stopped at a directory,
// a card or a subscription; no answer, which defers OneDrive too.
const (
	oneDriveFree    = "free"
	oneDrivePaid    = "paid"
	oneDriveUnknown = "unknown"
)

// The owner's D3 answers (C2-R3): every camera with ONVIF events, one per
// brand, or MI8 after Phase 0 (a)'s detector.
const (
	d3All        = "a"
	d3OnePer     = "b"
	d3AfterPhase = "c"
)

// repeated is a flag that may be given more than once.
type repeated []string

func (l *repeated) String() string { return strings.Join(*l, ",") }

func (l *repeated) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// options are the report's settings from the flags.
type options struct {
	oneDrive, d3, date string
	// consoleUnits is the Drive quota units per segment that the Cloud
	// console showed (P18), -1 when not given.
	consoleUnits int
}

// The texts of uploadreport's own lines, in Romanian.
const (
	msgUsage        = "uploadreport: folosire: -in <folder> … -onedrive free|paid|unknown [-d3 a|b|c] [-console-units <n>] [-date AAAA-LL-ZZ] [-out <folder>], sau -scan <fișier> …"
	msgNoDate       = "uploadreport: niciun rezultat nu spune când a fost făcut: dați data cu -date AAAA-LL-ZZ. Nu am scris nimic."
	msgNothingWrit  = "uploadreport: nu am scris nimic: raportul ar conține text care nu se publică (PH0-22):"
	msgFieldBad     = "uploadreport:   %s: câmpul %s nu are forma permisă"
	msgLeak         = "uploadreport:   %s:%d:%d: %s"
	msgFixResults   = "uploadreport: scoateți textul din rezultate sau înlocuiți-l cu un pseudonim, apoi rulați din nou."
	msgScanFound    = "uploadreport: %s:%d:%d: %s"
	msgScanClean    = "uploadreport: %d fișiere verificate, nimic de ascuns"
	msgWrote        = "am scris %s"
	msgUnreadable   = "uploadreport: necitit: %s (%s)"
	msgDropped      = "uploadreport: copie lăsată deoparte: %s (păstrat: %s)"
	msgD3WithMI8    = "uploadreport: -d3 c, dar există rezultate MI8: G17 le judecă"
	msgNeedOneDrive = "uploadreport: -onedrive e obligatoriu: free, paid sau unknown"
)

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("uploadreport", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var dirs, runs repeated
	flags.Var(&dirs, "in", "a results folder (repeatable): an owner's results zip, unpacked")
	flags.Var(&runs, "annotations", "a workflow run id whose jobs' mi5-lab notices are read with gh (repeatable)")
	oneDrive := flags.String("onedrive", "", "the A5 answer: free, paid or unknown (C2-R4)")
	d3 := flags.String("d3", d3All, "the owner's D3 answer: a, b or c")
	consoleUnits := flags.Int("console-units", -1, "the Drive quota units per segment that the Cloud console showed (P18)")
	date := flags.String("date", "", "date of the report, YYYY-MM-DD (default: the UTC day of the newest result)")
	out := flags.String("out", filepath.Join("docs", "research"), "folder for the report; the CSV files go to phase0-data/ inside it")
	scan := flags.Bool("scan", false, "scan the files given as arguments, write nothing")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *scan {
		return scanFiles(flags.Args(), stdout, stderr)
	}
	switch {
	case flags.NArg() > 0, len(dirs) == 0 && len(runs) == 0:
		say(stderr, msgUsage)
		return 2
	case !slices.Contains([]string{oneDriveFree, oneDrivePaid, oneDriveUnknown}, *oneDrive):
		say(stderr, msgNeedOneDrive)
		return 2
	case !slices.Contains([]string{d3All, d3OnePer, d3AfterPhase}, *d3), *consoleUnits < -1:
		say(stderr, msgUsage)
		return 2
	}
	for _, id := range runs {
		if !runIDPattern.MatchString(id) {
			say(stderr, msgUsage)
			return 2
		}
	}
	if *date != "" {
		if _, err := time.Parse(dateLayout, *date); err != nil {
			say(stderr, msgUsage)
			return 2
		}
	}

	in, err := load(dirs, runs)
	if err != nil {
		say(stderr, "uploadreport: %v", err)
		return 1
	}
	for _, s := range in.unreadable {
		say(stderr, msgUnreadable, s.file, s.reason)
	}
	for _, d := range in.dropped {
		say(stderr, msgDropped, d.from, d.kept)
	}
	if bad := in.badFields(); len(bad) > 0 {
		say(stderr, msgNothingWrit)
		for _, b := range bad {
			say(stderr, msgFieldBad, b.from, b.field)
		}
		say(stderr, msgFixResults)
		return 1
	}
	if *date == "" {
		newest := in.newest()
		if newest.IsZero() {
			say(stderr, msgNoDate)
			return 1
		}
		*date = newest.UTC().Format(dateLayout)
	}
	opts := options{oneDrive: *oneDrive, d3: *d3, date: *date, consoleUnits: *consoleUnits}
	if opts.d3 == d3AfterPhase && in.hasMI8() {
		say(stderr, msgD3WithMI8)
	}

	files := newReport(in, opts).files()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	var leaks []string
	for _, name := range names {
		for _, m := range scanText(files[name]) {
			leaks = append(leaks, fmt.Sprintf(msgLeak, name, m.line, m.column, m.kind))
		}
	}
	if len(leaks) > 0 {
		say(stderr, msgNothingWrit)
		for _, l := range leaks {
			say(stderr, "%s", l)
		}
		say(stderr, msgFixResults)
		return 1
	}
	for _, name := range names {
		path := filepath.Join(*out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // documentation, meant to be committed and read
			say(stderr, "uploadreport: %v", err)
			return 1
		}
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil { //nolint:gosec // documentation, meant to be committed and read
			say(stderr, "uploadreport: %v", err)
			return 1
		}
		say(stdout, msgWrote, path)
	}
	return 0
}

// finding is one match of the scanners: where and what kind, never the
// text.
type finding struct {
	line, column int
	kind         string
}

// scanText applies the redaction scanner, then the token patterns (P45).
func scanText(text string) []finding {
	var out []finding
	for _, m := range redact.Scan(text) {
		out = append(out, finding{m.Line, m.Column, string(m.Kind)})
	}
	for _, m := range results.ScanTokens(text) {
		out = append(out, finding{m.Line, m.Column, string(m.Kind)})
	}
	slices.SortStableFunc(out, func(a, b finding) int {
		if a.line != b.line {
			return a.line - b.line
		}
		return a.column - b.column
	})
	return out
}

// scanFiles is -scan: each file through scanText; 1 on any match or an
// unreadable file, 2 without files.
func scanFiles(paths []string, stdout, stderr io.Writer) int {
	if len(paths) == 0 {
		say(stderr, msgUsage)
		return 2
	}
	code := 0
	for _, p := range paths {
		data, err := os.ReadFile(p) //nolint:gosec // a committed file named by the caller
		if err != nil {
			say(stderr, "uploadreport: %v", err)
			code = 1
			continue
		}
		for _, m := range scanText(string(data)) {
			say(stderr, msgScanFound, p, m.line, m.column, m.kind)
			code = 1
		}
	}
	if code == 0 {
		say(stdout, msgScanClean, len(paths))
	}
	return code
}

// say writes a line to w. A message that cannot be written has nowhere else
// to go.
func say(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format+"\n", args...) }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
