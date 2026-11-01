package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// scanSecretsSchema names the output of diag scan-secrets.
const scanSecretsSchema = "ipcam.diag.scansecrets/1" //nolint:gosec // G101: a schema's name

// scanChunk is how much of a file the scan reads at a time.
const scanChunk = 1 << 20

// scanSecretsLine is diag scan-secrets' line: how many values it looked
// for and the files it removed, relative to --dir with "/", never a value.
type scanSecretsLine struct {
	Schema  string   `json:"schema"`
	Values  int      `json:"values"`
	Removed []string `json:"removed"`
}

// diagScanSecretsCommand is "ipcam-hub diag scan-secrets [--config <run.json>]
// --dir <results dir>" (P45, §16.2.3, ARH-08): the known-secrets scan, in
// the hub, which holds the secrets sealed (uploadspike keeps none, P46). It
// unseals in RAM every client:* row (the clients' IDs and the Desktop and
// TV secrets) and every oauth:* refresh token of --config's catalog, which
// it opens read-only, so it never migrates and never takes the write lock,
// and adds the values of stdin's JSON lines {"value":"…"}. Every file under
// --dir that holds a value raw, URL-encoded (QueryEscape, PathEscape),
// base64 (Std, URL, Raw), hex (lower, upper), or as a JSON string's body
// that encoding/json writes (with and without its HTML escaping, Task 9A
// review M4) is removed. It creates no file of its own: the sealer only
// unseals, so it makes no key. SQLite's read-only open of a closed WAL
// catalog creates catalog.db-wal and -shm, which belong to the catalog
// file's owner, not to the scan's (robustFchown, C2-R15 (a)).
func diagScanSecretsCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diag scan-secrets", flag.ContinueOnError)
	flags.SetOutput(stderr)
	config := flags.String("config", "", "the run configuration (run.json) whose catalog holds the sealed secrets")
	dir := flags.String("dir", "", "the results directory to scan")
	if err := flags.Parse(args); err != nil {
		return exitFatal
	}
	if *dir == "" || flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, diagUsage)
		return exitFatal
	}
	failed := func(err error) int {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag scan-secrets:", err)
		return exitFatal
	}
	ctx := context.Background()
	values, err := stdinValues(stdin)
	if err != nil {
		return failed(err)
	}
	if *config != "" {
		sealed, err := sealedValues(ctx, *config)
		if err != nil {
			return failed(err)
		}
		values = append(values, sealed...)
	}
	slices.Sort(values)
	values = slices.Compact(values)
	needles := encodedNeedles(values)
	removed, err := scanAndRemove(*dir, needles)
	if err != nil {
		return failed(err)
	}
	return printJSONLine(stdout, stderr, scanSecretsLine{Schema: scanSecretsSchema, Values: len(values), Removed: removed})
}

// stdinValues reads the JSON lines {"value":"…"} of r; blank lines are
// skipped, and empty values too. A line that is not one is an error that
// does not quote it.
func stdinValues(r io.Reader) ([]string, error) {
	var values []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var v struct {
			Value *string `json:"value"`
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&v); err != nil || v.Value == nil {
			return nil, fmt.Errorf(`stdin line %d is not a {"value":…} object`, n)
		}
		if *v.Value != "" {
			values = append(values, *v.Value)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("stdin: %w", err)
	}
	return values, nil
}

// sealedValues returns the secret values that the catalog of the run
// configuration at path holds sealed, which it opens read-only.
func sealedValues(ctx context.Context, path string) ([]string, error) {
	cfg, _, _, err := loadRunConfig(path)
	if err != nil {
		return nil, err
	}
	if err := haveCatalog(cfg.DataDir); err != nil {
		return nil, err
	}
	cat, err := catalog.OpenReadOnly(ctx, catalogPath(cfg.DataDir))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cat.Close() }()
	sealer, err := crypto.NewSealer(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	return oauth.KnownSecrets(ctx, cat, sealer)
}

// encodedNeedles are values in each of the encodings, without duplicates:
// the brief's eight, and the two JSON string bodies, which differ from the
// raw value when it holds a quote, a backslash, a control character, or
// (HTML-escaped) one of &, < and >: a typed camera password in a JSON
// results file.
func encodedNeedles(values []string) [][]byte {
	seen := map[string]bool{}
	var needles [][]byte
	for _, v := range values {
		b := []byte(v)
		for _, e := range []string{
			v, url.QueryEscape(v), url.PathEscape(v),
			base64.StdEncoding.EncodeToString(b), base64.URLEncoding.EncodeToString(b), base64.RawStdEncoding.EncodeToString(b),
			hex.EncodeToString(b), strings.ToUpper(hex.EncodeToString(b)),
			jsonStringBody(v, true), jsonStringBody(v, false),
		} {
			if e != "" && !seen[e] {
				seen[e] = true
				needles = append(needles, []byte(e))
			}
		}
	}
	return needles
}

// jsonStringBody is v as encoding/json writes it as a JSON string, without
// its quotes, its HTML characters (&, <, >) escaped or not.
func jsonStringBody(v string, escapeHTML bool) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(escapeHTML)
	_ = enc.Encode(v) // a string always encodes
	quoted := strings.TrimSuffix(b.String(), "\n")
	return quoted[1 : len(quoted)-1]
}

// scanAndRemove removes every regular file under dir that holds a needle,
// and returns their paths relative to dir with "/", sorted. It walks dir
// as an os.Root, so a symbolic link is neither followed nor removed, and
// no path leaves dir.
func scanAndRemove(dir string, needles [][]byte) ([]string, error) {
	removed := []string{}
	if len(needles) == 0 {
		return removed, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		hit, err := fileHolds(root, rel, needles)
		if err != nil || !hit {
			return err
		}
		if err := root.Remove(rel); err != nil {
			return fmt.Errorf("remove %s: %w", rel, errors.Unwrap(err))
		}
		removed = append(removed, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(removed)
	return removed, nil
}

// fileHolds reports whether the file rel of root holds a needle, reading it
// scanChunk at a time with an overlap of the longest needle.
func fileHolds(root *os.Root, rel string, needles [][]byte) (bool, error) {
	f, err := root.Open(rel)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	longest := 0
	for _, n := range needles {
		longest = max(longest, len(n))
	}
	buf := make([]byte, 0, scanChunk+longest)
	chunk := make([]byte, scanChunk)
	for {
		n, err := f.Read(chunk)
		buf = append(buf, chunk[:n]...)
		for _, needle := range needles {
			if bytes.Contains(buf, needle) {
				return true, nil
			}
		}
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if keep := longest - 1; len(buf) > keep {
			buf = append(buf[:0], buf[len(buf)-keep:]...)
		}
	}
}
