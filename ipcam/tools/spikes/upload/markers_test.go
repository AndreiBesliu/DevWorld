package upload

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// markerText is what every spike source file of the upload spike starts with,
// in the comment syntax of its file type. It is built from parts so that this
// file never matches a search for the marker.
const markerText = "SPIKE" + "-ONLY" + ": ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md"

// markedNonGoFiles are the spike files outside the Go code of this module that
// must carry the marker, relative to this directory: the windows job's S4U
// helper (Task 1). Task 9 adds uploadspike's pages, Task 9a the video app's
// OAuth files.
var markedNonGoFiles = []string{"ci/s4u-run.ps1"}

// markerLine is the first line that a file of the given type must have.
func markerLine(path string) (string, error) {
	switch filepath.Ext(path) {
	case ".go", ".ts", ".js":
		return "// " + markerText, nil
	case ".html":
		return "<!-- " + markerText + " -->", nil
	case ".ps1":
		return "# " + markerText, nil
	default:
		return "", fmt.Errorf("%s: no comment syntax known for this file type", path)
	}
}

// checkMarked reports an error unless the file at path starts with the marker
// line of its file type. A CRLF checkout (the windows job's) passes too.
func checkMarked(path string) error {
	want, err := markerLine(path)
	if err != nil {
		return err
	}
	f, err := os.Open(path) //nolint:gosec // the paths are this module's files and the list above
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // read-only file
	scanner := bufio.NewScanner(f)
	first := ""
	if scanner.Scan() {
		first = strings.TrimSuffix(scanner.Text(), "\r")
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if first != want {
		return fmt.Errorf("%s starts with %q, want %q", path, first, want)
	}
	return nil
}

// TestEverySourceFileIsMarkedSpikeOnly: every non-test .go file of the module
// starts with the marker line, so that spike code is easy to find and delete.
func TestEverySourceFileIsMarkedSpikeOnly(t *testing.T) {
	checked := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return err
		}
		checked++
		if err := checkMarked(path); err != nil {
			t.Error(err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if checked == 0 {
		t.Fatal("found no source files to check")
	}
}

// TestMarkedNonGoFiles: every listed file exists and starts with the marker in
// its file type's comment syntax.
func TestMarkedNonGoFiles(t *testing.T) {
	for _, path := range markedNonGoFiles {
		if err := checkMarked(path); err != nil {
			t.Error(err)
		}
	}
}

// TestCheckMarked pins the check that the two tests above rely on, also while
// markedNonGoFiles holds a single file.
func TestCheckMarked(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, tc := range []struct {
		name, file, content string
		ok                  bool
	}{
		{"go file with the marker", "a.go", "// " + markerText + "\n\npackage a\n", true},
		{"ts file with the marker and CRLF", "a.ts", "// " + markerText + "\r\nexport {};\r\n", true},
		{"html file with the marker", "a.html", "<!-- " + markerText + " -->\n<!doctype html>\n", true},
		{"ps1 file with the marker", "a.ps1", "# " + markerText + "\nparam()\n", true},
		{"ps1 file with the marker and CRLF", "b.ps1", "# " + markerText + "\r\nparam()\r\n", true},
		{"html file with the go syntax", "b.html", "// " + markerText + "\n", false},
		{"marker on the second line", "b.ts", "\n// " + markerText + "\n", false},
		{"marker for another plan", "c.go", "// SPIKE" + "-ONLY: ipcam/docs/other.md\n", false},
		{"empty file", "d.go", "", false},
		{"unknown file type", "a.txt", markerText + "\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkMarked(write(tc.file, tc.content))
			if (err == nil) != tc.ok {
				t.Errorf("checkMarked = %v, want ok %v", err, tc.ok)
			}
		})
	}
	t.Run("missing file", func(t *testing.T) {
		err := checkMarked(filepath.Join(dir, "missing.ts"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("checkMarked = %v, want a not-exist error", err)
		}
	})
}

// TestMarkerLine_KnownTypes: each file type that carries the marker gets the
// marker in its own comment syntax, and any other type is an error.
func TestMarkerLine_KnownTypes(t *testing.T) {
	for path, want := range map[string]string{
		"a.go":   "// " + markerText,
		"a.ts":   "// " + markerText,
		"a.js":   "// " + markerText,
		"a.html": "<!-- " + markerText + " -->",
		"a.ps1":  "# " + markerText,
	} {
		if got, err := markerLine(path); err != nil || got != want {
			t.Errorf("markerLine(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
	if got, err := markerLine("a.txt"); err == nil {
		t.Errorf("markerLine(%q) = %q, want an error", "a.txt", got)
	}
}
