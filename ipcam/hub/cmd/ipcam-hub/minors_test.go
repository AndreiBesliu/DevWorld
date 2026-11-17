package main

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The tests of the carried Task 9A review minors on ipcam-hub.

// TestDiagScanSecrets_ShortValuesAreWholeTokens (C2-R16 (e), C2-R20 (f)):
// a value of fewer than 8 bytes (a typed camera user, a 4-digit password)
// is found raw, URL-encoded or as a JSON string's body only as a whole
// token, not inside a longer word or number, also across the scan's 1 MiB
// chunks; its base64 and hex forms are not searched, since they are short
// enough to occur in ordinary base64 and hex text; the output counts such
// values.
func TestDiagScanSecrets_ShortValuesAreWholeTokens(t *testing.T) {
	dir := t.TempDir()
	edge := strings.Repeat(" ", scanChunk-3)
	user := "admin" // a camera URL with credentials, assembled at run time (P46)
	files := map[string]string{
		"user.json":          `{"user":"admin"}`,
		"url.json":           `{"uri":"rtsp://` + user + `:x@192.0.2.1/"}`,
		"pin.json":           `{"pin":"1234"}`,
		"b64.json":           `{"blob":"` + base64.StdEncoding.EncodeToString([]byte("admin")) + `"}`,
		"hex.json":           `{"hash":"` + hex.EncodeToString([]byte("1234")) + `"}`,
		"edge-token.txt":     edge + "admin ",
		"inside-word.json":   `{"role":"administrator"}`,
		"inside-number.json": `{"ms":12345}`,
		"edge-word.txt":      edge + "adminx",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	code, lines, out, stderr := runScan(t, stdinLine("admin")+stdinLine("1234"), "--dir", dir)
	if code != exitOK || len(lines) != 1 {
		t.Fatalf("scan-secrets = %d, %q (%s)", code, out, stderr)
	}
	for _, kept := range []string{"b64.json", "hex.json", "inside-word.json", "inside-number.json", "edge-word.txt"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s left the results (%v): a short value's base64 or hex form, or a part of a word, was searched", kept, err)
		}
	}
	got := stringsOf(lines[0], "moved")
	want := []string{"edge-token.txt", "pin.json", "url.json", "user.json"}
	if !slices.Equal(got, want) {
		t.Errorf("moved %v, want %v", got, want)
	}
	if lines[0]["shortValues"] != float64(2) || lines[0]["values"] != float64(2) {
		t.Errorf("line %v, want values 2 and shortValues 2", lines[0])
	}
}

// TestDiagScanSecrets_RefusesAnEmptyValue (C2-R16 (e)): an empty value on
// stdin refuses the scan, which moves nothing.
func TestDiagScanSecrets_RefusesAnEmptyValue(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"x":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, out, _ := runScan(t, stdinLine("some-value-1")+stdinLine(""), "--dir", dir); code != exitFatal || out != "" {
		t.Errorf("an empty stdin value: %d, %q; want exit 1 and no line", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.json")); err != nil {
		t.Errorf("a.json left the results (%v)", err)
	}
}
