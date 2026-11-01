package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// encodings are the eight forms in which diag scan-secrets finds a value
// (P45, §16.2.3).
var encodings = map[string]func(string) string{
	"raw":       func(v string) string { return v },
	"query":     url.QueryEscape,
	"path":      url.PathEscape,
	"base64":    func(v string) string { return base64.StdEncoding.EncodeToString([]byte(v)) },
	"base64url": func(v string) string { return base64.URLEncoding.EncodeToString([]byte(v)) },
	"base64raw": func(v string) string { return base64.RawStdEncoding.EncodeToString([]byte(v)) },
	"hex":       func(v string) string { return hex.EncodeToString([]byte(v)) },
	"hexupper":  func(v string) string { return strings.ToUpper(hex.EncodeToString([]byte(v))) },
}

// runScan runs diag scan-secrets with args and stdin, and returns its exit
// code, its output's lines, its raw output and its stderr.
func runScan(t *testing.T, stdin string, args ...string) (int, []map[string]any, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := diagCommand(append([]string{"scan-secrets"}, args...), strings.NewReader(stdin), &stdout, &stderr)
	var lines []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("scan-secrets printed %q, not JSON", line)
		}
		lines = append(lines, m)
	}
	return code, lines, stdout.String(), stderr.String()
}

// refreshTokenOf is the refresh token that the fake issued last.
func refreshTokenOf(t *testing.T, f *oauthFixture) string {
	t.Helper()
	tokens := f.login.RefreshTokens()
	if len(tokens) != 1 {
		t.Fatalf("the fake issued %d refresh tokens, want 1", len(tokens))
	}
	return tokens[0]
}

// TestDiagScanSecrets_AllEncodings (P45, ARH-08, §16.2.3): a client secret
// and a refresh token that the catalog holds sealed, and a value given on
// stdin, each planted in each of the eight encodings in its own file under
// --dir, are found and their files removed; a clean file stays; the output
// counts the values and names the removed files, and holds no value.
func TestDiagScanSecrets_AllEncodings(t *testing.T) {
	f := newOAuthFixture(t)
	f.logIn(t, "g-windows", oauth.PlatformWindows, "acct-1")
	stdinValue := "SENT" + "INEL-camera-password"
	values := map[string]string{"secret": secretDesktop, "refresh": refreshTokenOf(t, f), "stdin": stdinValue}
	dir := filepath.Join(t.TempDir(), "results")
	var want []string
	for name, v := range values {
		for enc, encode := range encodings {
			rel := filepath.Join("MI6", "pc", name+"-"+enc+".json")
			if err := os.MkdirAll(filepath.Join(dir, "MI6", "pc"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, rel), []byte(`{"note":"`+encode(v)+`"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			want = append(want, filepath.ToSlash(rel))
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "clean.json"), []byte(`{"verdict":"trece"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	slices.Sort(want)
	code, lines, out, stderr := runScan(t, `{"value":"`+stdinValue+`"}`+"\n\n", "--config", f.config, "--dir", dir)
	if code != exitOK || len(lines) != 1 {
		t.Fatalf("scan-secrets = %d, %q (%s); want one line, exit 0", code, out, stderr)
	}
	line := lines[0]
	removed, _ := line["removed"].([]any)
	var got []string
	for _, r := range removed {
		got = append(got, fmt.Sprint(r))
	}
	if line["schema"] != "ipcam.diag.scansecrets/1" || !slices.Equal(got, want) {
		t.Errorf("line %v, want the schema and removed %q", line, want)
	}
	// The hub's clients' IDs and two secrets, the refresh token, the stdin
	// value.
	if n, _ := line["values"].(float64); n != 8 {
		t.Errorf("values %v, want 8", line["values"])
	}
	for _, rel := range want {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Errorf("%s is still there (%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "clean.json")); err != nil {
		t.Errorf("clean.json was removed (%v)", err)
	}
	for _, v := range values {
		for _, encode := range encodings {
			if strings.Contains(out, encode(v)) || strings.Contains(stderr, encode(v)) {
				t.Errorf("the output holds a value")
			}
		}
	}
	// Without --config, only stdin's values; a line that is no {"value"}
	// object is refused without its text.
	if code, lines, _, _ := runScan(t, `{"value":"`+stdinValue+`"}`, "--dir", dir); code != exitOK || lines[0]["values"] != float64(1) {
		t.Errorf("scan-secrets without --config = %d, %v; want 1 value", code, lines)
	}
	if code, _, out, stderr := runScan(t, "not-json "+stdinValue+"\n", "--dir", dir); code != exitFatal || out != "" || strings.Contains(stderr, stdinValue) {
		t.Errorf("a bad stdin line: %d, %q, %q; want exit 1, no line, and no echo of the line", code, out, stderr)
	}
}

// TestDiagScanSecrets_OpensCatalogReadOnly (P45, C2-R15 (a)): the scan
// opens the catalog read-only: its user_version and its bytes are unchanged
// afterwards, state/ holds the same files, and a scan run while another
// connection holds the write lock finishes without waiting for it.
func TestDiagScanSecrets_OpensCatalogReadOnly(t *testing.T) {
	f := newOAuthFixture(t)
	f.logIn(t, "g-windows", oauth.PlatformWindows, "acct-1")
	stateDir := filepath.Join(f.dataDir, "state")
	catalogFile := filepath.Join(stateDir, "catalog.db")
	conn, err := openCatalog(t, catalogFile).Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(t.Context(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(catalogFile) //nolint:gosec // the test's catalog
	if err != nil {
		t.Fatal(err)
	}
	files := dirNames(t, stateDir)
	dir := t.TempDir()
	start := time.Now()
	code, _, out, stderr := runScan(t, "", "--config", f.config, "--dir", dir)
	if code != exitOK {
		t.Fatalf("scan-secrets = %d, %q, %s", code, out, stderr)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("the scan took %v while another connection held the write lock, want no wait", took)
	}
	if _, err := conn.ExecContext(t.Context(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(catalogFile) //nolint:gosec // the test's catalog
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Error("the scan changed catalog.db")
	}
	if got := dirNames(t, stateDir); !slices.Equal(got, files) {
		t.Errorf("state/ holds %q after the scan, want %q", got, files)
	}
	var version int
	if err := conn.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil || version != catalogVersion(t) {
		t.Errorf("user_version %d (%v), want the hub's", version, err)
	}
}

// dirNames lists dir's names, sorted.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// catalogVersion is the schema version of a new catalog.
func catalogVersion(t *testing.T) int {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.db")
	cat, err := catalog.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	_ = cat.Close()
	var v int
	if err := openCatalog(t, path).QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}
