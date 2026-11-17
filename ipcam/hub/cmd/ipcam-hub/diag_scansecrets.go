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
// for, the files it removed, the files it could not read through, and the
// files that hold a value and could not be removed, relative to --dir with
// "/", never a value. The line is printed whenever the scan walked --dir,
// and the scan exits 1 when either of the last two is not empty (security
// review DIAG-1).
type scanSecretsLine struct {
	Schema     string   `json:"schema"`
	Values     int      `json:"values"`
	Removed    []string `json:"removed"`
	Unreadable []string `json:"unreadable"`
	Unremoved  []string `json:"unremoved"`
}

// diagScanSecretsCommand is "ipcam-hub diag scan-secrets [--config <run.json>]
// --dir <results dir>" (P45, §16.2.3, ARH-08): the known-secrets scan, in
// the hub, which holds the secrets sealed (uploadspike keeps none, P46). It
// unseals in RAM every client:* row (the clients' IDs and the Desktop and
// TV secrets) and every oauth:* refresh token of --config's catalog, which
// it opens read-only, so it never migrates and never takes the write lock,
// and adds the values of stdin's JSON lines {"value":"…"}. Every file under
// --dir that holds a value in one of the forms of encodedNeedles is
// removed. It creates no file of its own: the sealer only unseals, so it
// makes no key. SQLite's read-only open of a closed WAL catalog creates
// catalog.db-wal and -shm, which belong to the catalog file's owner, not to
// the scan's (robustFchown, C2-R15 (a)). On the Pi it runs as root on files
// that the hub's identity owns, so it follows no link as run.json, state/,
// the catalog or the sealer's key, and never blocks on a FIFO in their
// place (security review SEAL-1).
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
	res, err := scanAndRemove(*dir, needles, scanOpen)
	if err != nil {
		return failed(err)
	}
	code := printJSONLine(stdout, stderr, scanSecretsLine{Schema: scanSecretsSchema, Values: len(values), Removed: res.Removed,
		Unreadable: res.Unreadable, Unremoved: res.Unremoved})
	if code == exitOK && len(res.Unreadable)+len(res.Unremoved) > 0 {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag scan-secrets: some files could not be scanned or removed: see unreadable and unremoved")
		return exitFatal
	}
	return code
}

// stdinValues reads the JSON lines {"value":"…"} of r, one object a line;
// blank lines are skipped, and empty values too. A line that is not one
// object, or holds anything after it, is an error that does not quote it
// (security review DIAG-5).
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
		if _, err := dec.Token(); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf(`stdin line %d holds more than one {"value":…} object`, n)
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
// configuration at path holds sealed, which it opens read-only. It reads
// run.json and checks the catalog's files as a root command does
// (loadRunConfigStrict, checkCatalogFiles).
func sealedValues(ctx context.Context, path string) ([]string, error) {
	cfg, err := loadRunConfigStrict(path)
	if err != nil {
		return nil, err
	}
	if err := checkCatalogFiles(cfg.DataDir); err != nil {
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

// shiftedMin is the length from which a value is also searched for inside
// longer base64 text at each byte offset: shorter ones would match
// ordinary text.
const shiftedMin = 8

// encodedNeedles are values in each of the forms in which a value leaks,
// without duplicates: raw; URL-encoded (QueryEscape, PathEscape, and a
// URL's userinfo as a password or a user, as url.UserPassword writes a
// camera's credentials into its RTSP URL), each also with lowercase hex
// escapes; base64 (Std, URL, and both without padding); hex (lower,
// upper); the two JSON string bodies, which differ from the raw value when
// it holds a quote, a backslash, a control character, or (HTML-escaped)
// one of &, < and >: a typed camera password in a JSON results file (Task
// 9A review M4); and, for a value of shiftedMin bytes or more, the part of
// its base64 (Std and URL) that only its own bytes determine at each of
// the three byte offsets, which finds it inside a longer base64 text: an
// HTTP Basic credential, a base64'd JSON blob (security review DIAG-2).
func encodedNeedles(values []string) [][]byte {
	seen := map[string]bool{}
	var needles [][]byte
	add := func(e string) {
		if e != "" && !seen[e] {
			seen[e] = true
			needles = append(needles, []byte(e))
		}
	}
	for _, v := range values {
		b := []byte(v)
		query, path := url.QueryEscape(v), url.PathEscape(v)
		password, user := strings.TrimPrefix(url.UserPassword("", v).String(), ":"), url.User(v).String()
		for _, e := range []string{
			v, query, path, password, user,
			lowerEscapes(query), lowerEscapes(path), lowerEscapes(password), lowerEscapes(user),
			base64.StdEncoding.EncodeToString(b), base64.URLEncoding.EncodeToString(b),
			base64.RawStdEncoding.EncodeToString(b), base64.RawURLEncoding.EncodeToString(b),
			hex.EncodeToString(b), strings.ToUpper(hex.EncodeToString(b)),
			jsonStringBody(v, true), jsonStringBody(v, false),
		} {
			add(e)
		}
		if len(b) >= shiftedMin {
			for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
				for offset := range 3 {
					add(base64Inner(enc, b, offset))
				}
			}
		}
	}
	return needles
}

// base64Inner is the part of enc's encoding of offset bytes followed by b
// that b's bytes alone determine: its characters past those of the offset's
// bytes, and without its last one when that one also holds the bits of
// what follows b. b inside a longer base64 text, at a byte offset of
// offset modulo 3, is there in this form.
func base64Inner(enc *base64.Encoding, b []byte, offset int) string {
	s := enc.EncodeToString(append(make([]byte, offset), b...))
	start, end := (offset*8+5)/6, len(s)
	if (offset+len(b))%3 != 0 {
		end--
	}
	return s[start:end]
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
// no path leaves dir. A file or directory that vanishes during the walk (a
// results writer's temporary file, renamed) cannot leak and is skipped;
// one that cannot be read, or that holds a needle and cannot be removed,
// is named in the result and the walk goes on, so that the files removed
// before it are still reported (security review DIAG-1). open opens a file
// of the scan: scanOpen.
func scanAndRemove(dir string, needles [][]byte, open func(*os.Root, string) (*os.File, error)) (scanResult, error) {
	res := scanResult{Removed: []string{}, Unreadable: []string{}, Unremoved: []string{}}
	if len(needles) == 0 {
		return res, nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return scanResult{}, err
	}
	defer func() { _ = root.Close() }()
	sc := newScanner(needles, open)
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			res.Unreadable = append(res.Unreadable, rel)
			return nil
		case !d.Type().IsRegular():
			return nil
		}
		hit, err := sc.holds(root, rel)
		switch {
		case errors.Is(err, fs.ErrNotExist), err == nil && !hit:
			return nil
		case err != nil:
			res.Unreadable = append(res.Unreadable, rel)
			return nil
		}
		switch err := root.Remove(rel); {
		case err == nil:
			res.Removed = append(res.Removed, rel)
		case !errors.Is(err, fs.ErrNotExist):
			res.Unremoved = append(res.Unremoved, rel)
		}
		return nil
	})
	if err != nil {
		return scanResult{}, err
	}
	for _, list := range [][]string{res.Removed, res.Unreadable, res.Unremoved} {
		slices.Sort(list)
	}
	return res, nil
}

// scanResult is what a scan removed, the files it could not read through,
// and the files that hold a needle and that it could not remove.
type scanResult struct {
	Removed, Unreadable, Unremoved []string
}

// scanOpen opens a file of the scan; tests replace it.
var scanOpen = (*os.Root).Open

// scanner looks for needles in files, with buffers allocated once a scan.
type scanner struct {
	needles    [][]byte
	longest    int
	buf, chunk []byte
	open       func(*os.Root, string) (*os.File, error)
}

func newScanner(needles [][]byte, open func(*os.Root, string) (*os.File, error)) *scanner {
	longest := 0
	for _, n := range needles {
		longest = max(longest, len(n))
	}
	return &scanner{needles: needles, longest: longest, buf: make([]byte, 0, scanChunk+longest), chunk: make([]byte, scanChunk), open: open}
}

// holds reports whether the file rel of root holds a needle, reading it
// scanChunk at a time with an overlap of the longest needle.
func (sc *scanner) holds(root *os.Root, rel string) (bool, error) {
	f, err := sc.open(root, rel)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	buf := sc.buf[:0]
	for {
		n, err := f.Read(sc.chunk)
		buf = append(buf, sc.chunk[:n]...)
		for _, needle := range sc.needles {
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
		if keep := sc.longest - 1; len(buf) > keep {
			buf = append(buf[:0], buf[len(buf)-keep:]...)
		}
	}
}

// lowerEscapes is s with the hex digits of its percent-escapes in lower
// case, as some encoders write them.
func lowerEscapes(s string) string {
	b := []byte(s)
	for i := 0; i+2 < len(b); i++ {
		if b[i] == '%' {
			b[i+1], b[i+2] = lowerHex(b[i+1]), lowerHex(b[i+2])
			i += 2
		}
	}
	return string(b)
}

func lowerHex(c byte) byte {
	if c >= 'A' && c <= 'F' {
		return c + ('a' - 'A')
	}
	return c
}
