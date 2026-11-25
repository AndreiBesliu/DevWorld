//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// checkQuarantined checks that only the scan's identity may read the
// quarantine's directory at p: its own, 0700. A file in it keeps its mode;
// its directories guard it.
func checkQuarantined(t *testing.T, p string, dir bool) {
	t.Helper()
	if !dir {
		return
	}
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := fileUID(info)
	if !info.IsDir() || info.Mode().Perm() != 0o700 || uid != os.Geteuid() {
		t.Errorf("%s: %v, owner %d; want a directory 0700 of uid %d", p, info.Mode(), uid, os.Geteuid())
	}
}

// TestDiagScanSecrets_QuarantineOfAnotherIdentityRefused (C2-R20 (a)): a
// quarantine that is a link, or a directory of another identity, is not
// used: the matched files stay in the results, named in unmoved, the line
// is printed and the scan exits 1, and nothing outside is touched. A
// quarantine of the scan's own identity that others may read is narrowed
// to 0700 before a file moves in.
func TestDiagScanSecrets_QuarantineOfAnotherIdentityRefused(t *testing.T) {
	value := "SENT" + "INEL-a-leaked-value"
	cases := []struct {
		name  string
		plant func(t *testing.T, q string)
		moved bool
	}{
		{"a link to a directory", func(t *testing.T, q string) {
			if err := os.Symlink(t.TempDir(), q); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a regular file", func(t *testing.T, q string) {
			if err := os.WriteFile(q, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a directory others may read", func(t *testing.T, q string) {
			if err := os.Mkdir(q, 0o755); err != nil { //nolint:gosec // G301: the mode under test
				t.Fatal(err)
			}
			if err := os.Chmod(q, 0o755); err != nil { //nolint:gosec // G302: the mode under test
				t.Fatal(err)
			}
		}, true},
	}
	if os.Geteuid() == 0 {
		cases = append(cases, struct {
			name  string
			plant func(t *testing.T, q string)
			moved bool
		}{"a directory of another user", func(t *testing.T, q string) {
			if err := os.Mkdir(q, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(q, 65534, 65534); err != nil {
				t.Fatal(err)
			}
		}, false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "results")
			writeTree(t, dir, map[string]string{"MI6/pc/run1.json": `{"note":"` + value + `"}`})
			q := filepath.Join(base, "results-quarantine")
			tc.plant(t, q)
			code, lines, out, stderr := runScan(t, stdinLine(value), "--dir", dir)
			if len(lines) != 1 {
				t.Fatalf("scan-secrets = %d, %q (%s); want one line", code, out, stderr)
			}
			moved, unmoved := stringsOf(lines[0], "moved"), stringsOf(lines[0], "unmoved")
			_, statErr := os.Stat(filepath.Join(dir, "MI6", "pc", "run1.json"))
			if tc.moved {
				if code != exitOK || !slices.Equal(moved, []string{"MI6/pc/run1.json"}) || len(unmoved) != 0 || statErr == nil {
					t.Errorf("scan-secrets = %d, moved %v, unmoved %v (%s); want the file moved", code, moved, unmoved, stderr)
				}
				checkQuarantined(t, q, true)
				return
			}
			if code != exitFatal || len(moved) != 0 || !slices.Equal(unmoved, []string{"MI6/pc/run1.json"}) || statErr != nil {
				t.Errorf("scan-secrets = %d, moved %v, unmoved %v, the file %v (%s); want exit 1 with the file kept and named in unmoved",
					code, moved, unmoved, statErr, stderr)
			}
		})
	}
}
