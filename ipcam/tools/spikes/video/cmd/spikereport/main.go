// Command spikereport turns the result files that spikehub stores into the owner's
// report of the Phase 0 (b) video spike: a Romanian Markdown report with the A3
// verdicts (AND-1, WIN-1, web) and the decisions they trigger, and CSV files with the
// raw samples (PH0-30).
//
//	spikereport [-in results] [-out docs/research] [-date YYYY-MM-DD]
//
// It writes <out>/<date>-phase0b-results.md and <out>/phase0-data/phase0b-<device>.csv.
// The date is the day of the newest run, on the devices' clocks, unless -date gives it.
// The output depends only on the input files: running it twice gives the same bytes.
//
// Everything that reaches the output passes through scrubText first, so that an address,
// a MAC, an e-mail, a serial number or an SSID in a note cannot reach the public
// repository (PH0-22). See ipcam/docs/superpowers/plans/2026-10-18-phase0b-video-spike.md, Task 13.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxResultBytes is the largest results file that is read. The hub stores at most 2 MiB.
const maxResultBytes = 8 << 20

// dateLayout is the layout of -date and of the date in the report's file name.
const dateLayout = "2006-01-02"

// platformName is what the app puts in a result's device.platform (device.ts): lower case,
// digits and hyphens. It names a CSV file, so nothing else is accepted.
var platformName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

var (
	ssidPattern   = regexp.MustCompile(`(?i)(\bssid\s*[:=]\s*).*`)
	serialPattern = regexp.MustCompile(`(?i)(\bserial(?:[ _-]?(?:number|no))?\s*[:=]\s*)\S+`)
	urlPattern    = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://\S+`)
	emailPattern  = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	macPattern    = regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?:[:-][0-9a-f]{2}){5}\b`)
	// An IPv6 literal: with `::`, or eight groups in full; with a zone, and in brackets with a port.
	ipv6Pattern = regexp.MustCompile(`(?i)\[?(?:(?:[0-9a-f]{1,4}:)*[0-9a-f]{0,4}::(?:[0-9a-f]{0,4}(?::[0-9a-f]{1,4})*)|(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4})(?:%[\w.]+)?(?:\](?::\d{1,5})?)?`)
	ipv4Pattern = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d{1,5})?\b`)
	// A dotted version of four numbers, as a browser or WebView reports it: 141.0.0.0.
	versionPattern = regexp.MustCompile(`(/?)(\d+)\.(\d+)\.(\d+)\.(\d+)`)
)

// scrubText removes what must not reach a public report (PH0-22): URLs, e-mail addresses,
// MAC addresses, IPv4 and IPv6 literals, and what follows "serial" and "ssid". The app
// already does this for its notes (scrubText in results.ts); the report does it again
// for any file it is given.
func scrubText(s string) string {
	s = ssidPattern.ReplaceAllString(s, "${1}[redacted]")
	s = serialPattern.ReplaceAllString(s, "${1}[redacted]")
	s = urlPattern.ReplaceAllString(s, "[url]")
	s = emailPattern.ReplaceAllString(s, "[email]")
	s = macPattern.ReplaceAllString(s, "[mac]")
	s = ipv6Pattern.ReplaceAllString(s, "[ip]")
	return ipv4Pattern.ReplaceAllString(s, "[ip]")
}

// shortVersions drops the fourth number of a four-number version (Chrome/141.0.0.0 becomes
// Chrome/141.0.0), which a scanner for addresses would take for an IPv4 literal. Only what
// is a version for sure is shortened: a number after a slash, or one above 255. A real
// address is left to scrubText, which removes it whole.
func shortVersions(s string) string {
	return versionPattern.ReplaceAllStringFunc(s, func(m string) string {
		parts := versionPattern.FindStringSubmatch(m)
		sure := parts[1] == "/"
		for _, n := range parts[2:] {
			if v, err := strconv.Atoi(n); err != nil || v > 255 {
				sure = true
			}
		}
		if !sure {
			return m
		}
		return parts[1] + strings.Join(parts[2:5], ".")
	})
}

// Skipped is a file in the input folder that is not a usable result.
type Skipped struct {
	File   string
	Reason string
}

// loadResults reads every *.json file of dir (not its subfolders), in the order of their
// names. A file that is not a usable result is listed in the second return value with the
// reason, and the others are still read.
func loadResults(dir string) ([]Result, []Skipped, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var results []Result
	var skipped []Skipped
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		r, err := readResult(filepath.Join(dir, name))
		if err != nil {
			skipped = append(skipped, Skipped{File: name, Reason: err.Error()})
			continue
		}
		r.File = name
		results = append(results, r)
	}
	return results, skipped, nil
}

// readResult parses one results file. Its errors are in Romanian: they reach the report.
func readResult(path string) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, errors.New("nu se poate citi")
	}
	if info.Size() > maxResultBytes {
		return Result{}, fmt.Errorf("fișier mai mare de %d MiB", maxResultBytes>>20)
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is a file of the folder that the owner named
	if err != nil {
		return Result{}, errors.New("nu se poate citi")
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		var wrongType *json.UnmarshalTypeError
		switch {
		case errors.As(err, &wrongType) && wrongType.Field != "":
			return Result{}, fmt.Errorf("câmpul „%s” are alt tip decât cel din aplicație", scrubText(wrongType.Field))
		case errors.As(err, &wrongType):
			return Result{}, errors.New("nu e un obiect de rezultat")
		default:
			return Result{}, errors.New("nu e JSON valid")
		}
	}
	if _, known := scenarioOrder[r.Scenario]; !known {
		return Result{}, fmt.Errorf("scenariu necunoscut „%s”", truncate(scrubText(r.Scenario), 24))
	}
	if !platformName.MatchString(r.Device.Platform) {
		return Result{}, errors.New("lipsește platforma dispozitivului sau nu e una cunoscută")
	}
	return r, nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// reportDate is the UTC day of the newest run, on the devices' clocks.
func reportDate(results []Result) (string, error) {
	var newest time.Time
	for _, r := range results {
		if t := r.startedAt(); t.After(newest) {
			newest = t
		}
	}
	if newest.IsZero() {
		return "", errors.New("no result has a readable startedAt: give the date with -date")
	}
	return newest.UTC().Format(dateLayout), nil
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("spikereport", flag.ContinueOnError)
	flags.SetOutput(stderr)
	in := flags.String("in", "results", "folder with the results files that spikehub stored")
	out := flags.String("out", filepath.Join("docs", "research"), "folder for the report; the CSV files go to phase0-data/ inside it")
	date := flags.String("date", "", "date of the report, YYYY-MM-DD (default: the day of the newest run)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		say(stderr, "spikereport: takes flags only")
		return 2
	}
	if *date != "" {
		if _, err := time.Parse(dateLayout, *date); err != nil {
			say(stderr, "spikereport: -date must be YYYY-MM-DD")
			return 2
		}
	}

	results, skipped, err := loadResults(*in)
	if err != nil {
		say(stderr, "spikereport: cannot read %s: %v", *in, err)
		return 1
	}
	for _, s := range skipped {
		say(stderr, "spikereport: skipped %s: %s", scrubText(s.File), scrubText(s.Reason))
	}
	if len(results) == 0 {
		say(stderr, "spikereport: no usable result in %s", *in)
		return 1
	}
	if *date == "" {
		if *date, err = reportDate(results); err != nil {
			say(stderr, "spikereport: %v", err)
			return 1
		}
	}

	report := BuildReport(results, skipped, *date)
	files := map[string]string{report.FileName(): report.Markdown()}
	for name, content := range report.CSVs() {
		files[name] = content
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(*out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // documentation, meant to be committed and read
			say(stderr, "spikereport: %v", err)
			return 1
		}
		if err := os.WriteFile(path, []byte(files[name]), 0o644); err != nil { //nolint:gosec // documentation, meant to be committed and read
			say(stderr, "spikereport: %v", err)
			return 1
		}
		say(stdout, "wrote %s", path)
	}
	return 0
}

// say writes a line to w. A message that cannot be written has nowhere else to go.
func say(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format+"\n", args...) }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
