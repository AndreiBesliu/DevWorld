// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The bounds of annotate's lines (c1 C32, P09): annotate.sh's escaping
// makes each % three bytes, and a step shows at most 10 annotations.
const (
	maxAnnotationBytes = 3500
	maxAnnotations     = 10
)

// runAnnotate is `uploadspike annotate emit --code <code> --set <set>`: it
// prints each envelope of results/<code>/<set>/ as one raw JSON line with
// no prefix, which the lab job gives to annotate.sh one at a time, so that
// uploadreport reads each notice's message as one envelope (Task 11). It
// fails when a line would pass 3,500 bytes after annotate.sh's escaping,
// or when the set holds more than 10 envelopes.
func runAnnotate(_ context.Context, e *env, args []string) error {
	if len(args) == 0 || args[0] != "emit" {
		return ownerError(msgAnnotateUsage)
	}
	flags := flag.NewFlagSet("annotate emit", flag.ContinueOnError)
	code := flags.String("code", "", "")
	set := flags.String("set", "", "")
	if err := cli.ParseFlags(flags, args[1:], e.out, usage); err != nil {
		return err
	}
	if *code == "" || *set == "" || strings.ContainsAny(*code+*set, `/\.`) {
		return ownerError(msgAnnotateUsage)
	}
	files, err := filepath.Glob(filepath.Join(e.resultsDir(), *code, *set, "*.json"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	if len(files) > maxAnnotations {
		return ownerError(fmt.Sprintf(msgAnnotateTooMany, len(files), maxAnnotations))
	}
	var lines [][]byte
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // a file of results/
		if err != nil {
			return err
		}
		var env results.Envelope
		if err := json.Unmarshal(b, &env); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		var line bytes.Buffer
		if err := json.Compact(&line, b); err != nil {
			return err
		}
		if n := escapedLen(line.Bytes()); n > maxAnnotationBytes {
			return ownerError(fmt.Sprintf(msgAnnotateTooLong, filepath.Base(f), n, maxAnnotationBytes))
		}
		lines = append(lines, line.Bytes())
	}
	for _, l := range lines {
		_, _ = e.out.Write(append(l, '\n'))
	}
	return nil
}

// escapedLen is a line's length after annotate.sh's escaping: each % is
// %25, and carriage returns are dropped.
func escapedLen(line []byte) int {
	return len(line) + 2*bytes.Count(line, []byte("%")) - bytes.Count(line, []byte("\r"))
}
