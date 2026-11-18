// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The lab's results leave the runners only as check-run annotations (C32):
// each lab run goes out as one notice, an envelope of one run with its part
// of the set, which `annotate read` checks once they are saved with
// `gh api …/check-runs/<id>/annotations`.
const (
	// maxLine is the longest notice line, measured after annotate.sh's
	// escaping: GitHub cut an annotation of about 4,000 characters
	// (0e9ae3d).
	maxLine = 3500
	// batchLines is how many notices one step emits: GitHub keeps 10 per
	// step.
	batchLines = 10
	// maxShardLines is the most lines of a shard: the notices of its two
	// emit steps.
	maxShardLines = 2 * batchLines
	// setParts is the runs of a lab set: 20 default runs and the O17-off
	// run (G10, O17).
	setParts = 21
)

// annotateCommand is `recspike annotate split|emit|read`.
func annotateCommand(args []string, out io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(out, usage)
		return errors.New("lipsește subcomanda: annotate split, emit sau read")
	}
	switch args[0] {
	case "split":
		return annotateSplit(args[1:], out)
	case "emit":
		return annotateEmit(args[1:], out)
	case "read":
		return annotateRead(args[1:], out)
	}
	_, _ = fmt.Fprint(out, usage)
	return fmt.Errorf("subcomandă necunoscută: annotate %s", args[0])
}

// parseInterleaved parses the flags wherever they stand among the other
// arguments, as `annotate split <envelope.json>… --first N --out DIR` has
// them, and returns the other arguments.
func parseInterleaved(flags *flag.FlagSet, args []string, out io.Writer) ([]string, error) {
	flags.SetOutput(io.Discard)
	var rest []string
	for {
		if err := flags.Parse(args); err != nil {
			_, _ = fmt.Fprint(out, usage)
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, errors.New(cli.FlagError(err))
		}
		if flags.NArg() == 0 {
			return rest, nil
		}
		rest = append(rest, flags.Arg(0))
		args = flags.Args()[1:]
	}
}

// escapedLen is the length of line once annotate.sh has escaped it: each
// "%" becomes "%25" and each line break "%0A"; carriage returns go.
func escapedLen(line []byte) int {
	s := string(line)
	return len(s) - strings.Count(s, "\r") + 2*strings.Count(s, "%") + 2*strings.Count(s, "\n")
}

// annotateSplit is `annotate split <envelope.json>… --first <part> --out
// <dir>`: one line per run of the shard's envelopes that counts (an invalid
// run is redone at its position, so it is no part of the set), in the order
// of their run ids, which is the set's: a one-run envelope with Part, its
// position in the set from --first, and Parts, 21. Each line is written to
// <dir>/<part>.json. It fails, and writes nothing, when a line is over
// 3,500 bytes once escaped, when the shard has more than 20 runs, or when a
// part would fall outside the set.
func annotateSplit(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("annotate split", flag.ContinueOnError)
	first := flags.Int("first", 0, "")
	dir := flags.String("out", "", "")
	files, err := parseInterleaved(flags, args, out)
	if err != nil {
		return err
	}
	switch {
	case len(files) == 0:
		return errors.New("lipsesc plicurile shardului")
	case *first < 1 || *first > setParts:
		return fmt.Errorf("--first este partea primei rulări a shardului, între 1 și %d", setParts)
	case *dir == "":
		return errors.New("lipsește --out: dosarul liniilor")
	}
	var runs []results.Envelope
	skipped := 0
	for _, f := range files {
		e, err := results.Read(f)
		if err != nil {
			return err
		}
		for _, r := range e.Runs {
			if !mi1.Counts(r) {
				skipped++
				continue
			}
			one := e
			one.RunID, one.Runs = r.RunID, []results.Run{r}
			runs = append(runs, one)
		}
	}
	slices.SortFunc(runs, func(a, b results.Envelope) int { return strings.Compare(a.RunID, b.RunID) })
	switch last := *first + len(runs) - 1; {
	case len(runs) > maxShardLines:
		return fmt.Errorf("%d rulări într-un shard: cei doi pași emit cel mult %d notificări", len(runs), maxShardLines)
	case last > setParts:
		return fmt.Errorf("%d rulări de la partea %d trec de partea %d a setului", len(runs), *first, setParts)
	}
	lines := make([][]byte, len(runs))
	for i, e := range runs {
		e.Part, e.Parts = *first+i, setParts
		if lines[i], err = json.Marshal(e); err != nil {
			return err
		}
		if n := escapedLen(lines[i]); n > maxLine {
			return fmt.Errorf("rularea %s: linia are %d octeți după escapare, peste %d", e.RunID, n, maxLine)
		}
	}
	if entries, err := os.ReadDir(*dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("dosarul %s nu este gol", *dir)
	}
	if err := os.MkdirAll(*dir, 0o750); err != nil {
		return err
	}
	for i, line := range lines {
		if err := os.WriteFile(filepath.Join(*dir, fmt.Sprintf("%02d.json", *first+i)), append(line, '\n'), 0o600); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(out, "Am scris %d linii, din partea %d, în %s; %d rulări care nu contează nu sunt părți ale setului.\n",
		len(lines), *first, *dir, skipped)
	return nil
}

// annotateEmit is `annotate emit <dir> --batch <k>`: it prints the lines
// 10(k−1)+1 to 10k that split wrote, so that a step never emits more than
// GitHub's 10 notices.
func annotateEmit(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("annotate emit", flag.ContinueOnError)
	batch := flags.Int("batch", 0, "")
	rest, err := parseInterleaved(flags, args, out)
	if err != nil {
		return err
	}
	switch {
	case len(rest) != 1:
		return errors.New("annotate emit primește un dosar de linii")
	case *batch < 1:
		return errors.New("--batch este 1, 2, …")
	}
	entries, err := os.ReadDir(rest[0])
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	from := batchLines * (*batch - 1)
	for _, name := range names[min(from, len(names)):min(from+batchLines, len(names))] {
		line, err := os.ReadFile(filepath.Join(rest[0], name)) //nolint:gosec // the lines that split wrote
		if err != nil {
			return err
		}
		if _, err := out.Write(line); err != nil {
			return err
		}
	}
	return nil
}

// annotateRead is `annotate read <dir|file>`: it parses the annotations
// saved with `gh api …/check-runs/<id>/annotations --paginate`, a file per
// job, and prints for each set how many of its parts came back:
// "<set>: <n>/<parts>". A file that does not parse, an envelope of more
// than one run or of no part, an envelope whose Parts differs from its
// set's first envelope's, a part outside the set's parts and a part that
// comes twice are errors.
func annotateRead(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("annotate read", flag.ContinueOnError)
	rest, err := parseInterleaved(flags, args, out)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("annotate read primește un dosar sau un fișier de adnotări")
	}
	files := rest
	if info, err := os.Stat(rest[0]); err != nil { //nolint:gosec // the annotations that the caller saved
		return err
	} else if info.IsDir() {
		entries, err := os.ReadDir(rest[0])
		if err != nil {
			return err
		}
		files = nil
		for _, e := range entries {
			if e.Type().IsRegular() {
				files = append(files, filepath.Join(rest[0], e.Name()))
			}
		}
	}
	type set struct {
		parts int
		seen  map[int]bool
	}
	sets := map[string]*set{}
	for _, file := range files {
		f, err := os.Open(file) //nolint:gosec // the annotations that the caller saved
		if err != nil {
			return err
		}
		envs, _, err := results.ReadAnnotations(f)
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(file), err)
		}
		for _, e := range envs {
			if len(e.Runs) != 1 {
				return fmt.Errorf("%s: plicul %s are %d rulări, nu una", filepath.Base(file), e.RunID, len(e.Runs))
			}
			if e.Parts == 0 {
				return fmt.Errorf("%s: plicul %s nu este o parte a unui set din laborator", filepath.Base(file), e.RunID)
			}
			s := sets[e.Set]
			if s == nil {
				s = &set{parts: e.Parts, seen: map[int]bool{}}
				sets[e.Set] = s
			}
			// results.Parse keeps Part within 1..Parts, so within the set's.
			switch {
			case e.Parts != s.parts:
				return fmt.Errorf("%s: plicul %s dă numărul de părți %d setului %s, iar primul plic al setului dă %d",
					filepath.Base(file), e.RunID, e.Parts, e.Set, s.parts)
			case s.seen[e.Part]:
				return fmt.Errorf("%s: partea %d a setului %s apare de două ori", filepath.Base(file), e.Part, e.Set)
			}
			s.seen[e.Part] = true
		}
	}
	names := make([]string, 0, len(sets))
	for name := range sets {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		_, _ = fmt.Fprintf(out, "%s: %d/%d\n", name, len(sets[name].seen), sets[name].parts)
	}
	return nil
}
