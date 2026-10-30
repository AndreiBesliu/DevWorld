package crypto_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"testing"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// encChunkSize is the size of a full age STREAM chunk on disk: 64 KiB of
// plaintext plus the 16-byte Poly1305 tag.
const encChunkSize = crypto.ChunkSize + 16

// newScope returns a KeyScope with fresh RI_g and RK identities.
func newScope(t *testing.T) (scope crypto.KeyScope, riG, rk *age.X25519Identity) {
	t.Helper()
	riG = generateIdentity(t)
	rk = generateIdentity(t)
	scope, err := crypto.ParseKeyScope(riG.Recipient().String(), rk.Recipient().String())
	if err != nil {
		t.Fatalf("ParseKeyScope: %v", err)
	}
	return scope, riG, rk
}

func generateIdentity(t *testing.T) *age.X25519Identity {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	return id
}

// pattern is the plaintext of the golden files: byte i is (i·31 + (i>>8)) & 0xff.
func pattern(size int) []byte {
	p := make([]byte, size)
	for i := range p {
		p[i] = byte(i*31 + i>>8)
	}
	return p
}

// randomBytes returns size bytes from a seeded generator, so a failure repeats.
func randomBytes(size int, seed uint64) []byte {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:8], seed)
	p := make([]byte, size)
	_, _ = rand.NewChaCha8(key).Read(p)
	return p
}

// encrypt writes plaintext through a Writer and returns the age file. It calls
// Flush each time the bytes written reach an offset of flushAt that lies
// inside the plaintext.
func encrypt(t *testing.T, scope crypto.KeyScope, plaintext []byte, flushAt ...int) []byte {
	t.Helper()
	var dst bytes.Buffer
	w, err := crypto.NewWriter(&dst, scope)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	written := 0
	for _, off := range flushAt {
		if off <= written || off >= len(plaintext) {
			continue
		}
		mustWrite(t, w, plaintext[written:off])
		if err := w.Flush(); err != nil {
			t.Fatalf("Flush at %d: %v", off, err)
		}
		written = off
	}
	mustWrite(t, w, plaintext[written:])
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dst.Bytes()
}

func mustWrite(t *testing.T, w io.Writer, p []byte) {
	t.Helper()
	if n, err := w.Write(p); err != nil || n != len(p) {
		t.Fatalf("Write(%d bytes) = %d, %v", len(p), n, err)
	}
}

// decryptStream decrypts a whole file with age.Decrypt.
func decryptStream(t *testing.T, file []byte, ids ...age.Identity) []byte {
	t.Helper()
	r, err := age.Decrypt(bytes.NewReader(file), ids...)
	if err != nil {
		t.Fatalf("age.Decrypt: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("age.Decrypt: read: %v", err)
	}
	return out
}

// header returns the age header at the start of file.
func header(t *testing.T, file []byte) []byte {
	t.Helper()
	hdr, err := age.ExtractHeader(bytes.NewReader(file))
	if err != nil {
		t.Fatalf("age.ExtractHeader: %v", err)
	}
	if !bytes.HasPrefix(file, hdr) {
		t.Fatal("the file does not start with its canonical header")
	}
	return hdr
}

// readAll reads size bytes from r at offset 0.
func readAll(t *testing.T, r io.ReaderAt, size int64) []byte {
	t.Helper()
	out := make([]byte, size)
	n, err := r.ReadAt(out, 0)
	if n != len(out) || (err != nil && !errors.Is(err, io.EOF)) {
		t.Fatalf("ReadAt(%d bytes, 0) = %d, %v", size, n, err)
	}
	return out
}

func TestRecording_RoundTrip(t *testing.T) {
	scope, riG, _ := newScope(t)
	rec := crypto.NewRecording(riG)
	plaintext := randomBytes(1<<20+17, 1)

	var file bytes.Buffer
	wc, err := rec.NewWriter(t.Context(), &file, scope)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, ok := wc.(*crypto.Writer); !ok {
		t.Errorf("Recording.NewWriter returned %T, want *crypto.Writer", wc)
	}
	mustWrite(t, wc, plaintext)
	if err := wc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r, size, err := rec.OpenReaderAt(t.Context(), bytes.NewReader(file.Bytes()), int64(file.Len()))
	if err != nil {
		t.Fatalf("OpenReaderAt: %v", err)
	}
	if size != int64(len(plaintext)) {
		t.Fatalf("plaintext size %d, want %d", size, len(plaintext))
	}
	if got := readAll(t, r, size); !bytes.Equal(got, plaintext) {
		t.Error("the plaintext read back differs from the plaintext written")
	}
}

// TestRecording_RandomRangesMatchStream is ENC-07's Go half: random ranges
// read through OpenReaderAt equal the same ranges of the age.Decrypt stream.
func TestRecording_RandomRangesMatchStream(t *testing.T) {
	scope, riG, rk := newScope(t)
	file := encrypt(t, scope, randomBytes(1<<20+17, 2), 1000, 3*crypto.ChunkSize, 700000)
	stream := decryptStream(t, file, rk)

	r, size, err := crypto.NewRecording(riG).OpenReaderAt(t.Context(), bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("OpenReaderAt: %v", err)
	}
	if size != int64(len(stream)) {
		t.Fatalf("plaintext size %d, the stream has %d", size, len(stream))
	}
	rng := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // reproducible test ranges, not keys
	const ranges = 10000
	for i := range ranges {
		off := rng.Int64N(size + 1)
		n := rng.Int64N(min(size-off, 3*crypto.ChunkSize) + 1)
		got := make([]byte, n)
		m, err := r.ReadAt(got, off)
		eofAtEnd := errors.Is(err, io.EOF) && off+n == size
		if int64(m) != n || (err != nil && !eofAtEnd) {
			t.Fatalf("range %d: ReadAt(%d bytes, %d) = %d, %v", i, n, off, m, err)
		}
		if !bytes.Equal(got, stream[off:off+n]) {
			t.Fatalf("range %d: [%d, %d) differs from the stream", i, off, off+n)
		}
	}
}

// TestRecording_HeaderHasExactlyRIgAndRK is C21: the header wraps the file
// key for exactly RI_g and RK, in that order, and for no one else.
func TestRecording_HeaderHasExactlyRIgAndRK(t *testing.T) {
	scope, riG, rk := newScope(t)
	file := encrypt(t, scope, []byte("one part"))
	hdr := header(t, file)

	stanzas := parseStanzas(t, hdr)
	if len(stanzas) != 2 {
		t.Fatalf("the header has %d stanzas, want 2:\n%s", len(stanzas), hdr)
	}
	for i, s := range stanzas {
		if s.Type != "X25519" {
			t.Errorf("stanza %d has type %q, want X25519", i, s.Type)
		}
	}
	for i, id := range []*age.X25519Identity{riG, rk} {
		if _, err := id.Unwrap(stanzas[i : i+1]); err != nil {
			t.Errorf("stanza %d does not unwrap with its identity: %v", i, err)
		}
		if _, err := id.Unwrap(stanzas[1-i : 2-i]); !errors.Is(err, age.ErrIncorrectIdentity) {
			t.Errorf("stanza %d unwraps with the other identity: %v", 1-i, err)
		}
	}

	keyRIg, err := age.DecryptHeader(hdr, riG)
	if err != nil {
		t.Fatalf("DecryptHeader with RI_g: %v", err)
	}
	keyRK, err := age.DecryptHeader(hdr, rk)
	if err != nil {
		t.Fatalf("DecryptHeader with RK: %v", err)
	}
	if !bytes.Equal(keyRIg, keyRK) || len(keyRIg) != 16 {
		t.Errorf("RI_g and RK unwrap different file keys (%d and %d bytes)", len(keyRIg), len(keyRK))
	}
	var noMatch *age.NoIdentityMatchError
	if _, err := age.DecryptHeader(hdr, generateIdentity(t)); !errors.As(err, &noMatch) {
		t.Errorf("DecryptHeader with a third identity = %v, want a NoIdentityMatchError", err)
	}

	t.Run("ParseKeyScope", testParseKeyScope)
}

// parseStanzas reads the recipient stanzas of an age v1 header.
func parseStanzas(t *testing.T, hdr []byte) []*age.Stanza {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(string(hdr), "\n"), "\n")
	if len(lines) < 2 || lines[0] != "age-encryption.org/v1" || !strings.HasPrefix(lines[len(lines)-1], "--- ") {
		t.Fatalf("not an age v1 header:\n%s", hdr)
	}
	var stanzas []*age.Stanza
	for _, line := range lines[1 : len(lines)-1] {
		if args, ok := strings.CutPrefix(line, "-> "); ok {
			fields := strings.Split(args, " ")
			stanzas = append(stanzas, &age.Stanza{Type: fields[0], Args: fields[1:]})
			continue
		}
		if len(stanzas) == 0 {
			t.Fatalf("body line before any stanza:\n%s", hdr)
		}
		body, err := base64.RawStdEncoding.DecodeString(line)
		if err != nil {
			t.Fatalf("stanza body %q: %v", line, err)
		}
		s := stanzas[len(stanzas)-1]
		s.Body = append(s.Body, body...)
	}
	return stanzas
}

// testParseKeyScope checks that ParseKeyScope takes exactly two different
// X25519 recipients, and that its errors never echo what it rejects.
func testParseKeyScope(t *testing.T) {
	riG := generateIdentity(t)
	rk := generateIdentity(t)
	third := generateIdentity(t)
	hybrid, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatalf("GenerateHybridIdentity: %v", err)
	}
	riGs, rks, thirds := riG.Recipient().String(), rk.Recipient().String(), third.Recipient().String()

	scope, err := crypto.ParseKeyScope(riGs, rks)
	if err != nil {
		t.Fatalf("ParseKeyScope(RI_g, RK): %v", err)
	}
	if got, err := scope.Strings(); err != nil || got != [2]string{riGs, rks} {
		t.Errorf("Strings() = %q, %v; want RI_g then RK", got, err)
	}

	for _, tc := range []struct{ name, riG, rk string }{
		{"one recipient", riGs, ""},
		{"one recipient, as RK", "", rks},
		{"three recipients", riGs + "," + rks, thirds},
		{"three recipients, space separated", riGs, rks + " " + thirds},
		{"a hybrid recipient", riGs, hybrid.Recipient().String()},
		{"a secret key", riG.String(), rks},
		{"a secret key as RK", riGs, rk.String()},
		// §10's two-key separation: RK must not be the recording key, in
		// whatever case its bech32 is written.
		{"RI_g as RK", riGs, riGs},
		{"RI_g as RK, upper case", riGs, strings.ToUpper(riGs)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := crypto.ParseKeyScope(tc.riG, tc.rk)
			if err == nil {
				t.Fatal("ParseKeyScope accepted it")
			}
			for _, input := range []string{tc.riG, tc.rk} {
				if input != "" && strings.Contains(err.Error(), input) {
					t.Errorf("the error echoes its input: %v", err)
				}
			}
			if strings.Contains(err.Error(), "AGE-SECRET-KEY-") {
				t.Errorf("the error contains secret key material: %v", err)
			}
		})
	}
}

// TestKeyScope_StringsWithoutBothKeys_ReturnsError: a KeyScope that did not
// come from ParseKeyScope, such as the zero value, gives an error, not a panic.
func TestKeyScope_StringsWithoutBothKeys_ReturnsError(t *testing.T) {
	scope, _, _ := newScope(t)
	for _, tc := range []struct {
		name  string
		scope crypto.KeyScope
	}{
		{"zero value", crypto.KeyScope{}},
		{"RI_g only", crypto.KeyScope{RecordingIdentity: scope.RecordingIdentity}},
		{"RK only", crypto.KeyScope{Recovery: scope.Recovery}},
	} {
		if got, err := tc.scope.Strings(); err == nil {
			t.Errorf("%s: Strings() = %q, want an error", tc.name, got)
		}
	}
}

// TestNewWriter_RejectsRIgAsRK: a KeyScope built by hand with RI_g as RK,
// which ParseKeyScope refuses, makes NewWriter fail before it writes anything,
// whether both fields hold the same recipient or two parses of one key.
func TestNewWriter_RejectsRIgAsRK(t *testing.T) {
	scope, riG, _ := newScope(t)
	again, err := age.ParseX25519Recipient(riG.Recipient().String())
	if err != nil {
		t.Fatalf("ParseX25519Recipient: %v", err)
	}
	for _, tc := range []struct {
		name  string
		scope crypto.KeyScope
	}{
		{"the same recipient", crypto.KeyScope{RecordingIdentity: scope.RecordingIdentity, Recovery: scope.RecordingIdentity}},
		{"the same key parsed twice", crypto.KeyScope{RecordingIdentity: scope.RecordingIdentity, Recovery: again}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dst bytes.Buffer
			if _, err := crypto.NewWriter(&dst, tc.scope); err == nil {
				t.Fatal("NewWriter accepted RI_g as RK")
			}
			if dst.Len() != 0 {
				t.Errorf("NewWriter wrote %d bytes before it failed", dst.Len())
			}
		})
	}
}

// countingIdentity counts the Unwrap calls that reach an X25519 identity.
type countingIdentity struct {
	id      *age.X25519Identity
	unwraps atomic.Int32
}

func (c *countingIdentity) Unwrap(stanzas []*age.Stanza) ([]byte, error) {
	c.unwraps.Add(1)
	return c.id.Unwrap(stanzas)
}

// TestRecording_OpenReaderAtCachesFileKey checks §10.2's file key cache: the
// X25519 unwrap runs once per file, not once per OpenReaderAt.
func TestRecording_OpenReaderAtCachesFileKey(t *testing.T) {
	scope, riG, _ := newScope(t)
	id := &countingIdentity{id: riG}
	rec := crypto.NewRecording(id)
	first := encrypt(t, scope, []byte("first segment"))
	second := encrypt(t, scope, []byte("second segment"))

	for i, file := range [][]byte{first, first, first, second, second} {
		r, size, err := rec.OpenReaderAt(t.Context(), bytes.NewReader(file), int64(len(file)))
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if got := readAll(t, r, size); !bytes.HasSuffix(got, []byte(" segment")) {
			t.Fatalf("open %d: read %q", i, got)
		}
	}
	if got := id.unwraps.Load(); got != 2 {
		t.Errorf("Unwrap ran %d times for 2 files opened 5 times, want 2", got)
	}
}
