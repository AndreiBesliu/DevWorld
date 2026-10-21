// Command redactscan runs the PH0-22 redaction scanner (package redact) over files and
// folders, the way CI checks the published Phase 0 reports and their raw data (R51):
//
//	redactscan <file or folder>...
//
// A folder is scanned with everything under it. Each match is printed as
// <path>:<line>:<column>: <kind>, never with the text it found, so that a public CI log
// does not repeat it. A file that holds a NUL byte is binary: it cannot be scanned, so it
// fails too. The exit code is 0 when nothing matched, 1 when something matched or a file
// could not be scanned, and 2 on a usage error or a path that cannot be read.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/redact"
)

// maxFileBytes is the largest file that is scanned; a report or a CSV of Phase 0 is far smaller.
const maxFileBytes = 64 << 20

// files lists the regular files of the arguments, folders walked, in a stable order.
func files(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		err := filepath.WalkDir(arg, func(path string, entry fs.DirEntry, err error) error { //nolint:gosec // the folders that the caller asked to scan
			if err != nil {
				return err
			}
			if entry.Type().IsRegular() {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

// scanFile prints the matches of one file and says whether it is clean.
func scanFile(path string, stdout io.Writer) (bool, error) {
	info, err := os.Stat(path) //nolint:gosec // a file under the paths that the caller asked to scan
	if err != nil {
		return false, err
	}
	if info.Size() > maxFileBytes {
		say(stdout, "%s: larger than %d MiB, cannot be scanned", path, maxFileBytes>>20)
		return false, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // a file under the paths that the caller asked to scan
	if err != nil {
		return false, err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		say(stdout, "%s: binary file, cannot be scanned", path)
		return false, nil
	}
	matches := redact.Scan(string(data))
	for _, m := range matches {
		say(stdout, "%s:%d:%d: %s", path, m.Line, m.Column, m.Kind)
	}
	return len(matches) == 0, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		say(stderr, "usage: redactscan <file or folder>...")
		return 2
	}
	paths, err := files(args)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			say(stderr, "redactscan: cannot read %s: %v", pathErr.Path, pathErr.Err)
		} else {
			say(stderr, "redactscan: %v", err)
		}
		return 2
	}
	failed := 0
	for _, path := range paths {
		clean, err := scanFile(path, stdout)
		if err != nil {
			say(stderr, "redactscan: cannot read %s: %v", path, err)
			return 2
		}
		if !clean {
			failed++
		}
	}
	if failed > 0 {
		say(stdout, "redactscan: %d of %d files hold text that PH0-22 forbids, or cannot be scanned; pseudonymise it (spec §16.2.3) and run again", failed, len(paths))
		return 1
	}
	say(stdout, "redactscan: no match in %d files", len(paths))
	return 0
}

// say writes a line to w. A message that cannot be written has nowhere else to go.
func say(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format+"\n", args...) }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
