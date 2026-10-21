package crypto_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"testing/cryptotest"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/golden with the writer, under deterministic randomness")

// The identities of the golden files. They are test keys, published on
// purpose: the golden files hold only the test pattern, and other tests
// (ENC-06 here, agerange's in Task 10a) decrypt them.
const (
	goldenRIgIdentity = "AGE-SECRET-KEY-16UYARTRJ7LK57JFNM887K9Q0CS8VRZKHWZPYC7HFCJEQUQUJTF0SXYAU2W"
	goldenRKIdentity  = "AGE-SECRET-KEY-1PHTT43VF0TNZP7S9PR2F37VV46VL23FKT49GTK5G4E6PHQLUF58Q65VXU7"
)

const goldenDir = "testdata/golden"

// goldenSizes are the plaintext sizes of the golden files: one byte, one byte
// short of a chunk, the chunk boundary plus a few bytes, three chunks plus a
// few bytes, and 1 MiB + 17.
var goldenSizes = []int{1, 65535, 65536 + 8, 65537, 3*65536 + 8, 1<<20 + 17}

// goldenFlushOffsets are where the golden writer calls Flush, when the offset
// lies inside the plaintext: inside a chunk, at and just after a boundary.
var goldenFlushOffsets = []int{1, 1000, 65536, 65537, 100000, 3 * 65536, 500000, 1 << 20}

// goldenRangeCount is the number of ranges vectors.json lists per file.
const goldenRangeCount = 64

// goldenVector is one entry of testdata/golden/vectors.json.
type goldenVector struct {
	File            string        `json:"file"`
	FileKeyHex      string        `json:"fileKeyHex"`
	PlaintextSize   int           `json:"plaintextSize"`
	PlaintextSha256 string        `json:"plaintextSha256"`
	Ranges          []goldenRange `json:"ranges"`
}

type goldenRange struct {
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	Sha256 string `json:"sha256"`
}

func goldenName(size int) string { return fmt.Sprintf("size-%07d.age", size) }

func goldenIdentities(t *testing.T) (riG, rk *age.X25519Identity) {
	t.Helper()
	riG, err := age.ParseX25519Identity(goldenRIgIdentity)
	if err != nil {
		t.Fatalf("golden RI_g: %v", err)
	}
	rk, err = age.ParseX25519Identity(goldenRKIdentity)
	if err != nil {
		t.Fatalf("golden RK: %v", err)
	}
	return riG, rk
}

func goldenScope(t *testing.T) crypto.KeyScope {
	t.Helper()
	riG, rk := goldenIdentities(t)
	scope, err := crypto.ParseKeyScope(riG.Recipient().String(), rk.Recipient().String())
	if err != nil {
		t.Fatalf("ParseKeyScope: %v", err)
	}
	return scope
}

func sha256Hex(p []byte) string {
	sum := sha256.Sum256(p)
	return hex.EncodeToString(sum[:])
}

// recordingWriter keeps what is written to it and the size of each Write.
type recordingWriter struct {
	bytes.Buffer
	writes []int
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, len(p))
	return w.Buffer.Write(p)
}

func TestNewWriter_WritesHeaderAndNonceInOneWrite(t *testing.T) {
	scope, _, _ := newScope(t)
	var dst recordingWriter
	if _, err := crypto.NewWriter(&dst, scope); err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	hdr := header(t, dst.Bytes())
	if len(dst.writes) != 1 || dst.Len() != len(hdr)+16 {
		t.Errorf("NewWriter made writes of %v bytes, want one of len(header) + 16 = %d", dst.writes, len(hdr)+16)
	}
}

func TestRecording_FlushSealsCompleteChunks(t *testing.T) {
	scope, _, rk := newScope(t)
	var dst recordingWriter
	w, err := crypto.NewWriter(&dst, scope)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	prefix := len(header(t, dst.Bytes())) + 16
	plaintext := pattern(3*crypto.ChunkSize + 100)

	// Write seals a full chunk only when a byte follows it, so the third
	// chunk stays in RAM until Flush.
	mustWrite(t, w, plaintext[:3*crypto.ChunkSize])
	if got, want := dst.Len(), prefix+2*encChunkSize; got != want || w.Buffered() != crypto.ChunkSize {
		t.Fatalf("before Flush: %d bytes written, %d buffered; want %d and %d", got, w.Buffered(), want, crypto.ChunkSize)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got, want := dst.Len(), prefix+3*encChunkSize; got != want || w.Buffered() != 0 {
		t.Fatalf("after Flush: %d bytes written, %d buffered; want %d and 0", got, w.Buffered(), want)
	}

	mustWrite(t, w, plaintext[3*crypto.ChunkSize:])
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got, want := dst.Len(), prefix+3*encChunkSize; got != want || w.Buffered() != 100 {
		t.Errorf("after 3·65536 + 100 bytes and Flush: %d bytes written, %d buffered; want %d and 100", got, w.Buffered(), want)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := decryptStream(t, dst.Bytes(), rk); !bytes.Equal(got, plaintext) {
		t.Error("the flushed file does not decrypt to the plaintext")
	}
}

func TestRecording_FlushNeverSealsPartialChunk(t *testing.T) {
	scope, _, _ := newScope(t)
	var dst recordingWriter
	w, err := crypto.NewWriter(&dst, scope)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	mustWrite(t, w, pattern(100))
	written, writes := dst.Len(), len(dst.writes)
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if dst.Len() != written || len(dst.writes) != writes {
		t.Errorf("Flush with 100 bytes held wrote %d bytes in %d writes", dst.Len()-written, len(dst.writes)-writes)
	}
	if w.Buffered() != 100 {
		t.Errorf("Buffered() = %d, want 100", w.Buffered())
	}
}

func TestWriter_CloseWithNothingBuffered_ReturnsErrEmptyFinalChunk(t *testing.T) {
	scope, _, rk := newScope(t)
	var dst bytes.Buffer
	w, err := crypto.NewWriter(&dst, scope)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	plaintext := pattern(2*crypto.ChunkSize + 1)
	mustWrite(t, w, plaintext[:2*crypto.ChunkSize])
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	written := dst.Len()
	if err := w.Close(); !errors.Is(err, crypto.ErrEmptyFinalChunk) {
		t.Fatalf("Close = %v, want ErrEmptyFinalChunk", err)
	}
	if dst.Len() != written {
		t.Fatalf("the failed Close wrote %d bytes", dst.Len()-written)
	}

	// The writer is left as it was, so one more byte makes a valid file.
	mustWrite(t, w, plaintext[2*crypto.ChunkSize:])
	if err := w.Close(); err != nil {
		t.Fatalf("Close after one more byte: %v", err)
	}
	if got := decryptStream(t, dst.Bytes(), rk); !bytes.Equal(got, plaintext) {
		t.Error("the file does not decrypt to the plaintext")
	}
}

// TestWriter_CloseSealsFullFinalChunk: a plaintext of exactly n chunks,
// written without Flush, ends with a full chunk, which Write holds and Close
// seals as the final chunk.
func TestWriter_CloseSealsFullFinalChunk(t *testing.T) {
	scope, _, rk := newScope(t)
	for n := 1; n <= 3; n++ {
		t.Run(fmt.Sprintf("chunks=%d", n), func(t *testing.T) {
			var dst bytes.Buffer
			w, err := crypto.NewWriter(&dst, scope)
			if err != nil {
				t.Fatalf("NewWriter: %v", err)
			}
			prefix := len(header(t, dst.Bytes())) + 16
			plaintext := randomBytes(n*crypto.ChunkSize, uint64(10+n)) //nolint:gosec // n is a small positive count
			mustWrite(t, w, plaintext)
			if w.Buffered() != crypto.ChunkSize {
				t.Errorf("before Close, Buffered() = %d, want %d", w.Buffered(), crypto.ChunkSize)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if got, want := dst.Len(), prefix+n*encChunkSize; got != want {
				t.Errorf("the file has %d bytes, want len(header) + 16 + %d·65552 = %d", got, n, want)
			}
			if got := decryptStream(t, dst.Bytes(), rk); !bytes.Equal(got, plaintext) {
				t.Error("the file does not decrypt to the plaintext")
			}
		})
	}
}

func TestWriter_CloseWithEmptyPlaintext_WritesEmptyFile(t *testing.T) {
	scope, _, rk := newScope(t)
	var dst bytes.Buffer
	w, err := crypto.NewWriter(&dst, scope)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := decryptStream(t, dst.Bytes(), rk); len(got) != 0 {
		t.Errorf("the empty file decrypts to %d bytes", len(got))
	}
}

// TestWriter_GoldenVectors checks testdata/golden: each file decrypts to the
// pattern, with the file key and the range hashes that vectors.json lists.
// With -update-golden it first rewrites them from the writer, with Flush at
// goldenFlushOffsets and deterministic randomness, so a rewrite is repeatable.
func TestWriter_GoldenVectors(t *testing.T) {
	if *updateGolden {
		writeGolden(t)
	}
	riG, rk := goldenIdentities(t)
	vectors := readGoldenVectors(t)
	if len(vectors) != len(goldenSizes) {
		t.Fatalf("vectors.json lists %d files, want %d", len(vectors), len(goldenSizes))
	}
	for i, v := range vectors {
		size := goldenSizes[i]
		t.Run(v.File, func(t *testing.T) {
			if v.File != goldenName(size) || v.PlaintextSize != size {
				t.Fatalf("entry %d is %s of %d bytes, want %s of %d", i, v.File, v.PlaintextSize, goldenName(size), size)
			}
			file := readGoldenFile(t, v.File)
			plaintext := decryptStream(t, file, rk)
			if len(plaintext) != size || sha256Hex(plaintext) != v.PlaintextSha256 {
				t.Errorf("decrypts to %d bytes with SHA-256 %s, want %d and %s", len(plaintext), sha256Hex(plaintext), size, v.PlaintextSha256)
			}
			if !bytes.Equal(plaintext, pattern(size)) {
				t.Error("the plaintext is not the golden pattern")
			}
			fileKey, err := age.DecryptHeader(header(t, file), riG)
			if err != nil {
				t.Fatalf("DecryptHeader: %v", err)
			}
			if hex.EncodeToString(fileKey) != v.FileKeyHex {
				t.Errorf("file key %x, vectors.json has %s", fileKey, v.FileKeyHex)
			}
			checkGoldenRanges(t, file, v, rk)
		})
	}
}

// checkGoldenRanges checks that v has goldenRangeCount ranges, that they start
// at every chunk boundary ± 1, and that each one's hash matches what
// age.DecryptReaderAt reads.
func checkGoldenRanges(t *testing.T, file []byte, v goldenVector, id age.Identity) {
	t.Helper()
	if len(v.Ranges) != goldenRangeCount {
		t.Errorf("%d ranges, want %d", len(v.Ranges), goldenRangeCount)
	}
	starts := map[int]bool{}
	for _, r := range v.Ranges {
		starts[r.Offset] = true
	}
	for b := crypto.ChunkSize; b < v.PlaintextSize; b += crypto.ChunkSize {
		for _, off := range []int{b - 1, b, b + 1} {
			if !starts[off] {
				t.Errorf("no range starts at %d, next to the chunk boundary %d", off, b)
			}
		}
	}
	r, size, err := age.DecryptReaderAt(bytes.NewReader(file), int64(len(file)), id)
	if err != nil {
		t.Fatalf("age.DecryptReaderAt: %v", err)
	}
	for _, gr := range v.Ranges {
		if gr.Offset < 0 || gr.Length < 0 || int64(gr.Offset+gr.Length) > size {
			t.Errorf("range [%d, +%d) lies outside the %d-byte plaintext", gr.Offset, gr.Length, size)
			continue
		}
		got := make([]byte, gr.Length)
		if n, err := r.ReadAt(got, int64(gr.Offset)); n != gr.Length || (err != nil && !errors.Is(err, io.EOF)) {
			t.Errorf("range [%d, +%d): ReadAt = %d, %v", gr.Offset, gr.Length, n, err)
			continue
		}
		if sha256Hex(got) != gr.Sha256 {
			t.Errorf("range [%d, +%d): SHA-256 %s, vectors.json has %s", gr.Offset, gr.Length, sha256Hex(got), gr.Sha256)
		}
	}
}

func readGoldenVectors(t *testing.T) []goldenVector {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(goldenDir, "vectors.json"))
	if err != nil {
		t.Fatalf("read vectors.json (run with -update-golden to create it): %v", err)
	}
	var vectors []goldenVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("parse vectors.json: %v", err)
	}
	return vectors
}

func readGoldenFile(t *testing.T, name string) []byte {
	t.Helper()
	file, err := os.ReadFile(filepath.Join(goldenDir, name)) //nolint:gosec // name comes from goldenSizes
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return file
}

// writeGolden rewrites testdata/golden. cryptotest.SetGlobalRandom makes the
// file keys, payload nonces and X25519 ephemeral keys deterministic.
func writeGolden(t *testing.T) {
	scope := goldenScope(t)
	riG, _ := goldenIdentities(t)
	if err := os.MkdirAll(goldenDir, 0o750); err != nil {
		t.Fatal(err)
	}
	vectors := make([]goldenVector, 0, len(goldenSizes))
	for i, size := range goldenSizes {
		cryptotest.SetGlobalRandom(t, uint64(i)+1) //nolint:gosec // i is a small index
		plaintext := pattern(size)
		file := encrypt(t, scope, plaintext, goldenFlushOffsets...)
		fileKey, err := age.DecryptHeader(header(t, file), riG)
		if err != nil {
			t.Fatalf("DecryptHeader: %v", err)
		}
		v := goldenVector{
			File:            goldenName(size),
			FileKeyHex:      hex.EncodeToString(fileKey),
			PlaintextSize:   size,
			PlaintextSha256: sha256Hex(plaintext),
		}
		rng := rand.New(rand.NewPCG(uint64(size), 31)) //nolint:gosec // reproducible test ranges, not keys; size is positive
		for _, r := range goldenRanges(size, rng) {
			v.Ranges = append(v.Ranges, goldenRange{Offset: r[0], Length: r[1], Sha256: sha256Hex(plaintext[r[0] : r[0]+r[1]])})
		}
		if err := os.WriteFile(filepath.Join(goldenDir, v.File), file, 0o600); err != nil {
			t.Fatal(err)
		}
		vectors = append(vectors, v)
	}
	data, err := json.MarshalIndent(vectors, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goldenDir, "vectors.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("rewrote %d golden files and vectors.json", len(vectors))
}

// goldenRanges returns goldenRangeCount [offset, length] pairs inside a
// plaintext of size bytes: the whole plaintext, its first and last byte, the
// ranges that start at every chunk boundary ± 1, then random ranges.
func goldenRanges(size int, rng *rand.Rand) [][2]int {
	var ranges [][2]int
	add := func(off, n int) {
		off = min(max(off, 0), size)
		ranges = append(ranges, [2]int{off, min(n, size-off)})
	}
	add(0, size)
	add(0, 1)
	add(size-1, 1)
	for b := crypto.ChunkSize; b < size; b += crypto.ChunkSize {
		add(b-1, 2)
		add(b, 1)
		add(b+1, 1)
	}
	for len(ranges) < goldenRangeCount {
		off := rng.IntN(size + 1)
		add(off, rng.IntN(min(size-off, 3*crypto.ChunkSize)+1))
	}
	return ranges[:goldenRangeCount]
}

// TestRecording_ByteCompatibleWithAge checks that every golden file decrypts
// to the same bytes with age.Decrypt and age.DecryptReaderAt, with RI_g and
// with RK.
func TestRecording_ByteCompatibleWithAge(t *testing.T) {
	riG, rk := goldenIdentities(t)
	for _, size := range goldenSizes {
		name := goldenName(size)
		t.Run(name, func(t *testing.T) {
			file := readGoldenFile(t, name)
			want := pattern(size)
			for _, id := range []*age.X25519Identity{riG, rk} {
				if got := decryptStream(t, file, id); !bytes.Equal(got, want) {
					t.Errorf("age.Decrypt with %s gives other bytes", id.Recipient())
				}
				r, n, err := age.DecryptReaderAt(bytes.NewReader(file), int64(len(file)), id)
				if err != nil {
					t.Fatalf("age.DecryptReaderAt with %s: %v", id.Recipient(), err)
				}
				if got := readAll(t, r, n); !bytes.Equal(got, want) {
					t.Errorf("age.DecryptReaderAt with %s gives other bytes", id.Recipient())
				}
			}
		})
	}
}
