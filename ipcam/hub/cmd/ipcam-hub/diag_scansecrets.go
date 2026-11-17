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
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// scanSecretsSchema names the output of diag scan-secrets.
const scanSecretsSchema = "ipcam.diag.scansecrets/1" //nolint:gosec // G101: a schema's name

// scanChunk is how much of a file the scan reads at a time.
const scanChunk = 1 << 20

// scanSecretsLine is diag scan-secrets' line: how many values it looked
// for, this scan's directory of the quarantine, relative to --dir's parent
// with "/" ("" when no file moved), the files it moved there, the files it
// could not read through, and the files that hold a value and could not be
// moved (they stay in --dir), relative to --dir with "/", never a value. A
// moved file is at <quarantine>/<its path>. The line is printed whenever
// the scan walked --dir, and the scan exits 1 when either of the last two
// is not empty (security review DIAG-1).
type scanSecretsLine struct {
	Schema string `json:"schema"`
	Values int    `json:"values"`
	// ShortValues counts the values of fewer than shortMax bytes, found as
	// whole tokens only (C2-R16 (e)).
	ShortValues int      `json:"shortValues"`
	Quarantine  string   `json:"quarantine"`
	Moved       []string `json:"moved"`
	Unreadable  []string `json:"unreadable"`
	Unmoved     []string `json:"unmoved"`
}

// diagScanSecretsCommand is "ipcam-hub diag scan-secrets [--config <run.json>]
// --dir <results dir>" (P45, §16.2.3, ARH-08): the known-secrets scan, in
// the hub, which holds the secrets sealed (uploadspike keeps none, P46). It
// unseals in RAM every client:* row (the clients' IDs and the Desktop and
// TV secrets) and every oauth:* refresh token of --config's catalog, which
// it opens read-only, so it never migrates and never takes the write lock,
// and adds the values of stdin's JSON lines {"value":"…"}. Every file under
// --dir that holds a value in one of the forms of encodedNeedles moves into
// the quarantine beside --dir, and no file is removed (quarantine, C2-R20
// (a)); a value of fewer than shortMax bytes is found in its text forms
// only as a whole token, and an empty value is refused (C2-R16 (e)). Past
// the quarantine, it creates no file of its own: the sealer only unseals,
// so it makes no key. SQLite's read-only open of a closed WAL catalog creates
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
	res, err := scanAndQuarantine(*dir, needles, scanOpen)
	if err != nil {
		return failed(err)
	}
	if res.QuarantineErr != nil {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag scan-secrets: the quarantine:", res.QuarantineErr)
	}
	short := 0
	for _, v := range values {
		if len(v) < shortMax {
			short++
		}
	}
	code := printJSONLine(stdout, stderr, scanSecretsLine{Schema: scanSecretsSchema, Values: len(values), ShortValues: short,
		Quarantine: res.Quarantine, Moved: res.Moved, Unreadable: res.Unreadable, Unmoved: res.Unmoved})
	if code == exitOK && len(res.Unreadable)+len(res.Unmoved) > 0 {
		_, _ = fmt.Fprintln(stderr, "ipcam-hub: diag scan-secrets: some files could not be scanned or quarantined: see unreadable and unmoved")
		return exitFatal
	}
	return code
}

// stdinValues reads the JSON lines {"value":"…"} of r, one object a line;
// blank lines are skipped. A line that is not one object, holds anything
// after it (security review DIAG-5), or holds an empty value (C2-R16 (e)),
// is an error that does not quote it.
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
		if *v.Value == "" {
			return nil, fmt.Errorf("stdin line %d holds an empty value, which would match every file", n)
		}
		values = append(values, *v.Value)
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

// shortMax: a value of fewer bytes (a typed camera user such as "admin", a
// 4-digit password) is found in its text forms (raw, URL-encoded, a JSON
// string's body) only as a whole token, never inside a longer word or
// number, since an ordinary substring hit would quarantine result files
// that leak nothing (C2-R16 (e)); its base64 and hex forms are not
// searched, being short enough to occur in ordinary base64 and hex text
// (C2-R20 (f)).
const shortMax = 8

// needle is a form of a value that the scan searches for; token: only as a
// whole token (tokenAt).
type needle struct {
	b     []byte
	token bool
}

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
// HTTP Basic credential, a base64'd JSON blob (security review DIAG-2). A
// short value has its text forms only, as whole-token needles (shortMax);
// a form that is both kinds is a whole-token needle.
func encodedNeedles(values []string) []needle {
	index := map[string]int{}
	var needles []needle
	add := func(e string, token bool) {
		if e == "" {
			return
		}
		if i, ok := index[e]; ok {
			needles[i].token = needles[i].token || token
			return
		}
		index[e] = len(needles)
		needles = append(needles, needle{b: []byte(e), token: token})
	}
	for _, v := range values {
		b := []byte(v)
		short := len(b) < shortMax
		query, path := url.QueryEscape(v), url.PathEscape(v)
		password, user := strings.TrimPrefix(url.UserPassword("", v).String(), ":"), url.User(v).String()
		for _, e := range []string{
			v, query, path, password, user,
			lowerEscapes(query), lowerEscapes(path), lowerEscapes(password), lowerEscapes(user),
			jsonStringBody(v, true), jsonStringBody(v, false),
		} {
			add(e, short)
		}
		if short {
			continue
		}
		for _, e := range []string{
			base64.StdEncoding.EncodeToString(b), base64.URLEncoding.EncodeToString(b),
			base64.RawStdEncoding.EncodeToString(b), base64.RawURLEncoding.EncodeToString(b),
			hex.EncodeToString(b), strings.ToUpper(hex.EncodeToString(b)),
		} {
			add(e, false)
		}
		if len(b) >= shiftedMin {
			for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
				for offset := range 3 {
					add(base64Inner(enc, b, offset), false)
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

// scanAndQuarantine moves every regular file under dir that holds a
// needle into this scan's directory of dir's quarantine (quarantine), and
// returns their paths relative to dir with "/", sorted; it removes no file
// (C2-R20 (a)). It walks dir as an os.Root, so a symbolic link is neither
// followed nor moved, and no path leaves dir. A file or directory that
// vanishes during the walk (a results writer's temporary file, renamed)
// cannot leak and is skipped; one that cannot be read, or that holds a
// needle and cannot be moved, is named in the result and the walk goes on,
// so that the files moved before it are still reported (security review
// DIAG-1). open opens a file of the scan: scanOpen.
func scanAndQuarantine(dir string, needles []needle, open func(*os.Root, string) (*os.File, error)) (scanResult, error) {
	res := scanResult{Moved: []string{}, Unreadable: []string{}, Unmoved: []string{}}
	if len(needles) == 0 {
		return res, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return scanResult{}, err
	}
	q, err := newQuarantine(abs)
	if err != nil {
		return scanResult{}, err
	}
	defer q.close()
	root, err := os.OpenRoot(abs)
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
		switch err := q.move(rel); {
		case err == nil:
			res.Moved = append(res.Moved, rel)
		case q.err == nil && errors.Is(err, fs.ErrNotExist) && gone(root, rel):
			// It vanished after its read: it cannot leak.
		default:
			res.Unmoved = append(res.Unmoved, rel)
		}
		return nil
	})
	if err != nil {
		return scanResult{}, err
	}
	for _, list := range [][]string{res.Moved, res.Unreadable, res.Unmoved} {
		slices.Sort(list)
	}
	if q.scan != "" {
		res.Quarantine = q.name + "/" + q.scan
	}
	res.QuarantineErr = q.err
	return res, nil
}

// gone reports whether rel is no longer in root.
func gone(root *os.Root, rel string) bool {
	_, err := root.Lstat(rel)
	return errors.Is(err, fs.ErrNotExist)
}

// scanResult is what a scan moved and into which directory of the
// quarantine (relative to the results root's parent, with "/"; "" when it
// made none), the files it could not read through, the files that hold a
// needle and that it could not move, and why the quarantine could not take
// any file, if so.
type scanResult struct {
	Quarantine                 string
	Moved, Unreadable, Unmoved []string
	QuarantineErr              error
}

// quarantineSuffix names a results root's quarantine: <results>-quarantine,
// beside it.
const quarantineSuffix = "-quarantine"

// quarantine is the directory beside a results root into which diag
// scan-secrets moves the files that hold a value, each scan into a
// directory of its own, <results>-quarantine/<scan>/<the file's path>
// (C2-R20 (a)). A scan that a value steers (a string that every envelope
// holds) then destroys nothing, and a real leak leaves the results, which
// is the folder the owner sends, while its bytes stay on the disk. Only
// the scan's identity may read the quarantine: on Linux it is that
// identity's directory, 0700 (root's on the Pi, where uploadspike runs the
// scan as root); on Windows its DACL grants SYSTEM and the Administrators
// alone, and so does each moved file's (restrictDir, restrictFile). The
// quarantine and the scan's directory are made at the first file that
// moves, so a clean scan makes nothing.
type quarantine struct {
	parentDir string // the results root's parent
	results   string // the results root's name in parentDir
	name      string // the quarantine's name in parentDir
	parent    *os.Root
	scan      string // this scan's directory in the quarantine, once made
	err       error  // why no file can move, once preparing the quarantine failed
}

// newQuarantine is the quarantine of the results root at abs.
func newQuarantine(abs string) (*quarantine, error) {
	parent, results := filepath.Dir(abs), filepath.Base(abs)
	if parent == abs {
		return nil, fmt.Errorf("%s has no parent directory to hold its quarantine", abs)
	}
	return &quarantine{parentDir: parent, results: results, name: results + quarantineSuffix}, nil
}

// move moves the results' file rel into this scan's directory of the
// quarantine, at the same path, preparing the quarantine first when no
// file moved yet; once preparing failed, no file moves.
func (q *quarantine) move(rel string) error {
	if q.err == nil && q.parent == nil {
		if q.err = q.prepare(); q.err != nil {
			return q.err
		}
	}
	if q.err != nil {
		return q.err
	}
	from := filepath.Join(q.results, filepath.FromSlash(rel))
	to := filepath.Join(q.name, q.scan, filepath.FromSlash(rel))
	if err := q.parent.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if beforeQuarantineMove != nil {
		beforeQuarantineMove(rel)
	}
	if err := restrictFile(q.parent, q.parentDir, from); err != nil {
		return err
	}
	return q.parent.Rename(from, to)
}

// beforeQuarantineMove, when set, runs before a file's move into the
// quarantine: a test seam that swaps a path meanwhile.
var beforeQuarantineMove func(rel string)

// prepare opens the results root's parent, makes the quarantine beside the
// results root, or takes the one there, which must be a directory of the
// scan's identity, restricted to it (restrictDir), and makes this scan's
// directory in it, named by the scan's UTC time. restrictDir is told which
// directories this scan made, and checks the owner of any other.
func (q *quarantine) prepare() error {
	parent, err := os.OpenRoot(q.parentDir)
	if err != nil {
		return err
	}
	q.parent = parent
	if info, err := parent.Lstat(q.results); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("%s is a link or no directory", filepath.Join(q.parentDir, q.results))
	}
	created := true
	if err := parent.Mkdir(q.name, 0o700); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		created = false
	}
	if err := restrictDir(parent, q.parentDir, q.name, created); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for i := 1; ; i++ {
		scan := stamp
		if i > 1 {
			scan = fmt.Sprintf("%s-%d", stamp, i)
		}
		err := parent.Mkdir(filepath.Join(q.name, scan), 0o700)
		switch {
		case err == nil:
			q.scan = scan
			return restrictDir(parent, q.parentDir, filepath.Join(q.name, scan), true)
		case !errors.Is(err, fs.ErrExist) || i == 100:
			return err
		}
	}
}

func (q *quarantine) close() {
	if q.parent != nil {
		_ = q.parent.Close()
	}
}

// scanOpen opens a file of the scan; tests replace it.
var scanOpen = (*os.Root).Open

// scanner looks for needles in files, with buffers allocated once a scan.
type scanner struct {
	needles    []needle
	longest    int
	buf, chunk []byte
	open       func(*os.Root, string) (*os.File, error)
}

func newScanner(needles []needle, open func(*os.Root, string) (*os.File, error)) *scanner {
	longest := 0
	for _, n := range needles {
		longest = max(longest, len(n.b))
	}
	return &scanner{needles: needles, longest: longest, buf: make([]byte, 0, scanChunk+longest+1), chunk: make([]byte, scanChunk), open: open}
}

// holds reports whether the file rel of root holds a needle, reading it
// scanChunk at a time. Each read keeps the last longest+1 bytes of the one
// before, so that a needle across two reads is found with the byte before
// it, and a whole-token needle at the end of what was read waits for the
// byte after it (tokenAt).
func (sc *scanner) holds(root *os.Root, rel string) (bool, error) {
	f, err := sc.open(root, rel)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	buf, start := sc.buf[:0], true
	for {
		n, err := f.Read(sc.chunk)
		buf = append(buf, sc.chunk[:n]...)
		eof := errors.Is(err, io.EOF)
		for _, needle := range sc.needles {
			if needle.token && tokenAt(buf, needle.b, start, eof) || !needle.token && bytes.Contains(buf, needle.b) {
				return true, nil
			}
		}
		if eof {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if keep := sc.longest + 1; len(buf) > keep {
			buf, start = append(buf[:0], buf[len(buf)-keep:]...), false
		}
	}
}

// tokenAt reports whether buf holds needle as a whole token: where its
// first byte is a token byte (isTokenByte), the byte before it is not one
// or the file's start (start: buf begins the file), and where its last
// byte is one, the byte after it is not one or the file's end (eof: buf
// ends the file). A match whose byte before or after is not in buf is not
// one yet: holds keeps it for the next read.
func tokenAt(buf, needle []byte, start, eof bool) bool {
	first, last := isTokenByte(needle[0]), isTokenByte(needle[len(needle)-1])
	for off := 0; ; {
		i := bytes.Index(buf[off:], needle)
		if i < 0 {
			return false
		}
		i += off
		end := i + len(needle)
		before := !first || (i == 0 && start) || (i > 0 && !isTokenByte(buf[i-1]))
		after := !last || (end == len(buf) && eof) || (end < len(buf) && !isTokenByte(buf[end]))
		if before && after {
			return true
		}
		off = i + 1
	}
}

// isTokenByte reports a byte of a word or a number: an ASCII letter or
// digit, '_', or a byte of a multi-byte UTF-8 character.
func isTokenByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c >= 0x80
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
