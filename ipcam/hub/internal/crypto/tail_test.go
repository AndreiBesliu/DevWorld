package crypto_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// tornFile is an age file that a cut left with tornChunks flushed chunks plus
// tornTrailing bytes of the next chunk.
type tornFile struct {
	path      string
	plaintext []byte // what was written; only the flushed chunks reached the file
	prefix    int    // len(header) + 16
}

const (
	tornChunks   = 5
	tornTrailing = 300
)

// writeTorn writes six chunks of plaintext into dir/name and flushes them,
// then cuts the file inside the sixth chunk, as a power cut during its write
// would.
func writeTorn(t *testing.T, dir, name string, scope crypto.KeyScope) tornFile {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path) //nolint:gosec // path is in the test's directory
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	w, err := crypto.NewWriter(f, scope)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	plaintext := randomBytes((tornChunks+1)*crypto.ChunkSize, 3)
	mustWrite(t, w, plaintext)
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	file, err := os.ReadFile(path) //nolint:gosec // path is in the test's directory
	if err != nil {
		t.Fatal(err)
	}
	prefix := len(header(t, file)) + 16
	if len(file) != prefix+(tornChunks+1)*encChunkSize {
		t.Fatalf("the flushed file has %d bytes, want %d", len(file), prefix+(tornChunks+1)*encChunkSize)
	}
	if err := f.Truncate(int64(prefix + tornChunks*encChunkSize + tornTrailing)); err != nil {
		t.Fatal(err)
	}
	return tornFile{path: path, plaintext: plaintext, prefix: prefix}
}

// openTorn opens path for reading and writing and hands it to a TailSealer.
func openTorn(t *testing.T, path string, riG *age.X25519Identity) crypto.TornAge {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // path is in the test's directory
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	torn, err := crypto.NewTailSealer(riG).OpenTorn(t.Context(), f)
	if err != nil {
		t.Fatalf("OpenTorn: %v", err)
	}
	return torn
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	file, err := os.ReadFile(path) //nolint:gosec // path is in the test's directory
	if err != nil {
		t.Fatal(err)
	}
	return file
}

// chunkAt returns the ciphertext of full chunk i of file.
func chunkAt(file []byte, prefix, i int) []byte {
	return file[prefix+i*encChunkSize : prefix+(i+1)*encChunkSize]
}

func TestRecording_ResealTail_DecryptsWithAgeCLI(t *testing.T) {
	scope, riG, rk := newScope(t)
	torn := writeTorn(t, t.TempDir(), "torn.mp4.age.part", scope)

	ta := openTorn(t, torn.path, riG)
	if got, want := ta.DurableSize(), int64(tornChunks*crypto.ChunkSize); got != want {
		t.Fatalf("DurableSize() = %d, want %d", got, want)
	}
	if ta.Complete() {
		t.Fatal("Complete() = true for a file without a final chunk")
	}
	if got := readAll(t, ta, ta.DurableSize()); !bytes.Equal(got, torn.plaintext[:ta.DurableSize()]) {
		t.Fatal("ReadAt does not give the durable plaintext")
	}

	keep := int64(tornChunks*crypto.ChunkSize - 1000)
	cipherSize, err := ta.Reseal(t.Context(), keep, nil)
	if err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	file := readFile(t, torn.path)
	wantSize := int64(torn.prefix+(tornChunks-1)*encChunkSize) + keep - (tornChunks-1)*crypto.ChunkSize + 16
	if cipherSize != wantSize || int64(len(file)) != wantSize {
		t.Errorf("Reseal returned %d and left %d bytes, want %d", cipherSize, len(file), wantSize)
	}
	want := torn.plaintext[:keep]
	if got := decryptStream(t, file, rk); !bytes.Equal(got, want) {
		t.Errorf("age.Decrypt gives %d bytes, not the %d kept", len(got), keep)
	}
	if again := openTorn(t, torn.path, riG); !again.Complete() || again.DurableSize() != keep {
		t.Errorf("after Reseal, OpenTorn gives Complete() = %v and DurableSize() = %d; want true and %d", again.Complete(), again.DurableSize(), keep)
	}

	tools := ageTools(t)
	if tools == nil {
		return
	}
	identityFile := writeIdentityFile(t, rk)
	for _, tool := range tools {
		got, err := decryptWithTool(t, tool, identityFile, torn.path)
		if err != nil {
			t.Errorf("%s: %v", tool.name, err)
		} else if !bytes.Equal(got, want) {
			t.Errorf("%s gives %d bytes, not the %d kept", tool.name, len(got), keep)
		}
	}
}

func TestTornAge_ResealAtChunkBoundary_RewritesLastChunkAsFinal(t *testing.T) {
	scope, riG, rk := newScope(t)
	torn := writeTorn(t, t.TempDir(), "torn.mp4.age.part", scope)
	before := readFile(t, torn.path)

	const keep = 4 * crypto.ChunkSize
	cipherSize, err := openTorn(t, torn.path, riG).Reseal(t.Context(), keep, nil)
	if err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	after := readFile(t, torn.path)
	if want := int64(torn.prefix + 4*encChunkSize); cipherSize != want || int64(len(after)) != want {
		t.Fatalf("Reseal returned %d and left %d bytes, want %d", cipherSize, len(after), want)
	}
	for i := range 3 {
		if !bytes.Equal(chunkAt(after, torn.prefix, i), chunkAt(before, torn.prefix, i)) {
			t.Errorf("chunk %d changed", i)
		}
	}
	if bytes.Equal(chunkAt(after, torn.prefix, 3), chunkAt(before, torn.prefix, 3)) {
		t.Error("chunk 3 was not rewritten")
	}
	// DecryptReaderAt authenticates the last chunk with the final-chunk flag.
	r, size, err := age.DecryptReaderAt(bytes.NewReader(after), int64(len(after)), rk)
	if err != nil {
		t.Fatalf("age.DecryptReaderAt: %v", err)
	}
	if got := readAll(t, r, size); !bytes.Equal(got, torn.plaintext[:keep]) {
		t.Errorf("the resealed file decrypts to %d bytes, not the %d kept", len(got), keep)
	}
	// A file that ends with a full final chunk is complete.
	if again := openTorn(t, torn.path, riG); !again.Complete() || again.DurableSize() != keep {
		t.Errorf("after Reseal, OpenTorn gives Complete() = %v and DurableSize() = %d; want true and %d", again.Complete(), again.DurableSize(), keep)
	}
}

func TestTornAge_PatchIsApplied(t *testing.T) {
	scope, riG, rk := newScope(t)
	torn := writeTorn(t, t.TempDir(), "torn.mp4.age.part", scope)
	before := readFile(t, torn.path)

	keep := int64(tornChunks*crypto.ChunkSize - 1000)
	// Inside the last kept chunk, chunk tornChunks-1 (C1-R7).
	patch := crypto.Patch{Off: keep - 500, Data: []byte("patched by TestTornAge_PatchIsApplied")}
	if _, err := openTorn(t, torn.path, riG).Reseal(t.Context(), keep, []crypto.Patch{patch}); err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	after := readFile(t, torn.path)

	want := bytes.Clone(torn.plaintext[:keep])
	copy(want[patch.Off:], patch.Data)
	if got := decryptStream(t, after, rk); !bytes.Equal(got, want) {
		t.Error("the resealed file does not hold the kept plaintext with the patch")
	}
	for i := range tornChunks - 1 {
		if !bytes.Equal(chunkAt(after, torn.prefix, i), chunkAt(before, torn.prefix, i)) {
			t.Errorf("chunk %d, before the last kept one, changed", i)
		}
	}
}

// TestTornAge_RefusesPatchBeforeLastChunk is C1-R7: a patch before the last
// kept chunk would seal an earlier chunk again under its old nonce, so Reseal
// refuses it before it touches the file, and the TornAge stays usable.
func TestTornAge_RefusesPatchBeforeLastChunk(t *testing.T) {
	scope, riG, rk := newScope(t)
	torn := writeTorn(t, t.TempDir(), "torn.mp4.age.part", scope)
	before := readFile(t, torn.path)
	ta := openTorn(t, torn.path, riG)

	keep := int64(tornChunks*crypto.ChunkSize - 1000)
	lastStart := int64((tornChunks - 1) * crypto.ChunkSize) // where the last kept chunk starts
	inLast := crypto.Patch{Off: keep - 500, Data: []byte("in the last kept chunk")}
	for _, tc := range []struct {
		name    string
		patches []crypto.Patch
	}{
		{"200 KiB before keep", []crypto.Patch{{Off: keep - 200<<10, Data: []byte("200 KiB before keep")}}},
		{"across into the last kept chunk", []crypto.Patch{{Off: lastStart - 3, Data: []byte("across")}}},
		{"after a patch in the last kept chunk", []crypto.Patch{inLast, {Off: 100, Data: []byte("in chunk 0")}}},
	} {
		if _, err := ta.Reseal(t.Context(), keep, tc.patches); !errors.Is(err, crypto.ErrPatchBeforeFinalChunk) {
			t.Errorf("%s: Reseal = %v, want ErrPatchBeforeFinalChunk", tc.name, err)
		}
		if !bytes.Equal(readFile(t, torn.path), before) {
			t.Fatalf("%s: the refused Reseal changed the file", tc.name)
		}
	}

	if ta.DurableSize() != tornChunks*crypto.ChunkSize || ta.Complete() {
		t.Fatalf("after the refusals, DurableSize() = %d and Complete() = %v", ta.DurableSize(), ta.Complete())
	}
	if _, err := ta.Reseal(t.Context(), keep, []crypto.Patch{inLast}); err != nil {
		t.Fatalf("Reseal with a patch in the last kept chunk: %v", err)
	}
	want := bytes.Clone(torn.plaintext[:keep])
	copy(want[inLast.Off:], inLast.Data)
	if got := decryptStream(t, readFile(t, torn.path), rk); !bytes.Equal(got, want) {
		t.Error("the resealed file does not hold the kept plaintext with the patch")
	}
}

// TestTornAge_EarlierChunksByteIdentical is C1-R7: Reseal seals only the last
// kept chunk, so the header, the payload nonce and every chunk before it stay
// byte-identical on disk, and the file decrypts with age, the age CLIs and
// rage.
func TestTornAge_EarlierChunksByteIdentical(t *testing.T) {
	scope, riG, rk := newScope(t)
	dir := t.TempDir()
	torn := writeTorn(t, dir, "torn.mp4.age.part", scope)
	original := readFile(t, torn.path)
	tools := ageTools(t)
	identityFile := writeIdentityFile(t, rk)

	for i, tc := range []struct {
		name  string
		keep  int64
		patch bool
	}{
		{"mid-chunk", tornChunks*crypto.ChunkSize - 1000, false},
		{"mid-chunk, with a patch", tornChunks*crypto.ChunkSize - 1000, true},
		{"on a chunk boundary", 3 * crypto.ChunkSize, false},
		{"on a chunk boundary, with a patch", 3 * crypto.ChunkSize, true},
		{"one byte into a chunk, with a patch", 2*crypto.ChunkSize + 1, true},
		{"in the first chunk, with a patch", 1000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, fmt.Sprintf("copy-%d.mp4.age.part", i))
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			last := int((tc.keep - 1) / crypto.ChunkSize)
			want := bytes.Clone(torn.plaintext[:tc.keep])
			var patches []crypto.Patch
			if tc.patch {
				// A patch that ends at keep, inside the last kept chunk.
				data := []byte("patched in the last kept chunk")
				data = data[:min(int64(len(data)), tc.keep-int64(last*crypto.ChunkSize))]
				patches = []crypto.Patch{{Off: tc.keep - int64(len(data)), Data: data}}
				copy(want[patches[0].Off:], data)
			}
			if _, err := openTorn(t, path, riG).Reseal(t.Context(), tc.keep, patches); err != nil {
				t.Fatalf("Reseal(%d): %v", tc.keep, err)
			}

			after := readFile(t, path)
			if !bytes.Equal(after[:torn.prefix], original[:torn.prefix]) {
				t.Error("the header or the payload nonce changed")
			}
			for c := range last {
				if !bytes.Equal(chunkAt(after, torn.prefix, c), chunkAt(original, torn.prefix, c)) {
					t.Errorf("chunk %d, before the last kept chunk %d, changed", c, last)
				}
			}
			if got := decryptStream(t, after, rk); !bytes.Equal(got, want) {
				t.Errorf("age.Decrypt gives %d bytes that differ from the %d kept", len(got), len(want))
			}
			for _, tool := range tools {
				got, err := decryptWithTool(t, tool, identityFile, path)
				if err != nil {
					t.Errorf("%s: %v", tool.name, err)
				} else if !bytes.Equal(got, want) {
					t.Errorf("%s gives %d bytes that differ from the %d kept", tool.name, len(got), len(want))
				}
			}
		})
	}
}

func TestTornAge_ResealRejectsBadArguments(t *testing.T) {
	scope, riG, _ := newScope(t)
	torn := writeTorn(t, t.TempDir(), "torn.mp4.age.part", scope)
	before := readFile(t, torn.path)
	ta := openTorn(t, torn.path, riG)
	durable := ta.DurableSize()

	for _, tc := range []struct {
		name    string
		keep    int64
		patches []crypto.Patch
	}{
		{"nothing kept", 0, nil},
		{"more than is durable", durable + 1, nil},
		{"a patch past keep", 1000, []crypto.Patch{{Off: 999, Data: []byte("ab")}}},
		{"a patch before the file", 1000, []crypto.Patch{{Off: -1, Data: []byte("a")}}},
	} {
		if _, err := ta.Reseal(t.Context(), tc.keep, tc.patches); err == nil {
			t.Errorf("%s: Reseal accepted it", tc.name)
		}
	}
	if !bytes.Equal(readFile(t, torn.path), before) {
		t.Error("a rejected Reseal changed the file")
	}
}

// TestTornAge_EmptyPatchChangesNothing: a patch with no data changes no byte,
// so it is accepted anywhere in [0, keep], before the last kept chunk too,
// and the file comes out byte-equal to the same Reseal without it.
func TestTornAge_EmptyPatchChangesNothing(t *testing.T) {
	scope, riG, rk := newScope(t)
	dir := t.TempDir()
	torn := writeTorn(t, dir, "torn.mp4.age.part", scope)
	original := readFile(t, torn.path)
	keep := int64(tornChunks*crypto.ChunkSize - 1000)
	patch := crypto.Patch{Off: keep - 500, Data: []byte("patched by TestTornAge_EmptyPatchChangesNothing")}

	// resealCopy reseals a new copy of the torn file, which has the same
	// file key and payload nonce, so equal plaintext gives equal bytes.
	copies := 0
	resealCopy := func(patches []crypto.Patch) ([]byte, error) {
		copies++
		path := filepath.Join(dir, fmt.Sprintf("copy-%d.mp4.age.part", copies))
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := openTorn(t, path, riG).Reseal(t.Context(), keep, patches)
		return readFile(t, path), err
	}
	unpatched, err := resealCopy(nil)
	if err != nil {
		t.Fatalf("Reseal without patches: %v", err)
	}
	if got := decryptStream(t, unpatched, rk); !bytes.Equal(got, torn.plaintext[:keep]) {
		t.Fatal("the resealed file does not decrypt to the kept plaintext")
	}
	patched, err := resealCopy([]crypto.Patch{patch})
	if err != nil {
		t.Fatalf("Reseal with a patch: %v", err)
	}

	for _, tc := range []struct {
		name    string
		patches []crypto.Patch
		want    []byte
	}{
		{"nil data at 0", []crypto.Patch{{Off: 0}}, unpatched},
		{"empty data at 0", []crypto.Patch{{Off: 0, Data: []byte{}}}, unpatched},
		{"nil data in the first chunk kept whole", []crypto.Patch{{Off: crypto.ChunkSize + 5}}, unpatched},
		{"nil data in the last kept chunk", []crypto.Patch{{Off: keep - 10}}, unpatched},
		{"nil data at keep", []crypto.Patch{{Off: keep}}, unpatched},
		{"nil data around a patch", []crypto.Patch{{Off: 0}, patch, {Off: keep}}, patched},
	} {
		got, err := resealCopy(tc.patches)
		switch {
		case err != nil:
			t.Errorf("%s: Reseal: %v", tc.name, err)
		case !bytes.Equal(got, tc.want):
			t.Errorf("%s: the file differs from the same Reseal without empty patches", tc.name)
		}
	}
}

// writeClosed writes plaintext through a Writer, closes it, and returns the
// file's path, its bytes and len(header) + 16.
func writeClosed(t *testing.T, dir, name string, scope crypto.KeyScope, plaintext []byte) (path string, file []byte, prefix int) {
	t.Helper()
	file = encrypt(t, scope, plaintext)
	path = filepath.Join(dir, name)
	if err := os.WriteFile(path, file, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, file, len(header(t, file)) + 16
}

// TestTornAge_ResealNeverChangesAFinalChunk: sealing the file's final chunk
// again with other plaintext would reuse its STREAM nonce, (index, final),
// so Reseal refuses it and leaves the file as it was. It may keep that chunk
// whole and unchanged, or cut it off with a keep in an earlier chunk.
func TestTornAge_ResealNeverChangesAFinalChunk(t *testing.T) {
	scope, riG, rk := newScope(t)
	dir := t.TempDir()

	t.Run("complete file", func(t *testing.T) {
		plaintext := randomBytes(2*crypto.ChunkSize+5000, 5)
		path, original, _ := writeClosed(t, dir, "complete.mp4.age", scope, plaintext)
		ta := openTorn(t, path, riG)
		durable := ta.DurableSize()
		if !ta.Complete() || durable != int64(len(plaintext)) {
			t.Fatalf("Complete() = %v, DurableSize() = %d; want true and %d", ta.Complete(), durable, len(plaintext))
		}
		finalStart := int64(2 * crypto.ChunkSize)
		same := crypto.Patch{Off: finalStart + 100, Data: bytes.Clone(plaintext[finalStart+100 : finalStart+200])}
		changed := crypto.Patch{Off: finalStart + 100, Data: []byte("changed")}
		across := crypto.Patch{Off: finalStart - 3, Data: []byte("across the boundary")}
		early := crypto.Patch{Off: 100, Data: []byte("early")}

		for _, tc := range []struct {
			name    string
			keep    int64
			patches []crypto.Patch
			want    error
		}{
			{"keep inside the final chunk", durable - 1000, nil, crypto.ErrFinalChunkChanged},
			{"keep one byte less", durable - 1, nil, crypto.ErrFinalChunkChanged},
			{"a patch that changes the final chunk", durable, []crypto.Patch{changed}, crypto.ErrFinalChunkChanged},
			{"an unchanging patch, then a changing one", durable, []crypto.Patch{same, changed}, crypto.ErrFinalChunkChanged},
			// Patches before the final chunk would seal a non-final chunk again.
			{"a patch across into the final chunk", durable, []crypto.Patch{across}, crypto.ErrPatchBeforeFinalChunk},
			{"a patch in chunk 0", durable, []crypto.Patch{early}, crypto.ErrPatchBeforeFinalChunk},
		} {
			if _, err := ta.Reseal(t.Context(), tc.keep, tc.patches); !errors.Is(err, tc.want) {
				t.Errorf("%s: Reseal = %v, want %v", tc.name, err, tc.want)
			}
			if !bytes.Equal(readFile(t, path), original) {
				t.Fatalf("%s: a refused Reseal changed the file", tc.name)
			}
		}

		// The final chunk kept whole and unchanged: the same bytes again.
		for _, patches := range [][]crypto.Patch{nil, {same}} {
			cipherSize, err := ta.Reseal(t.Context(), durable, patches)
			if err != nil {
				t.Fatalf("Reseal(%d, %d patches): %v", durable, len(patches), err)
			}
			if after := readFile(t, path); cipherSize != int64(len(original)) || !bytes.Equal(after, original) {
				t.Fatalf("Reseal(%d, %d patches) changed the file", durable, len(patches))
			}
		}

		// A keep in an earlier chunk cuts the final chunk off; chunk 1 becomes
		// the final chunk under a nonce it never had.
		ta = openTorn(t, path, riG)
		keep := int64(2*crypto.ChunkSize - 1000)
		if _, err := ta.Reseal(t.Context(), keep, nil); err != nil {
			t.Fatalf("Reseal(%d): %v", keep, err)
		}
		if got := decryptStream(t, readFile(t, path), rk); !bytes.Equal(got, plaintext[:keep]) {
			t.Error("the file cut in chunk 1 does not decrypt to the kept plaintext")
		}
	})

	t.Run("full final chunk with trailing bytes", func(t *testing.T) {
		// Close seals a full final chunk when the plaintext ends on a chunk
		// boundary without a Flush; bytes after it make the file incomplete.
		plaintext := randomBytes(2*crypto.ChunkSize, 6)
		path, original, _ := writeClosed(t, dir, "trailing.mp4.age", scope, plaintext)
		if got := decryptStream(t, original, rk); !bytes.Equal(got, plaintext) {
			t.Fatal("the closed file does not decrypt to its plaintext")
		}
		withTrailing := append(bytes.Clone(original), randomBytes(300, 7)...)
		if err := os.WriteFile(path, withTrailing, 0o600); err != nil {
			t.Fatal(err)
		}
		ta := openTorn(t, path, riG)
		durable := ta.DurableSize()
		if ta.Complete() || durable != int64(len(plaintext)) {
			t.Fatalf("Complete() = %v, DurableSize() = %d; want false and %d", ta.Complete(), durable, len(plaintext))
		}
		if _, err := ta.Reseal(t.Context(), durable-1000, nil); !errors.Is(err, crypto.ErrFinalChunkChanged) {
			t.Errorf("Reseal of a keep inside the final chunk = %v, want ErrFinalChunkChanged", err)
		}
		if !bytes.Equal(readFile(t, path), withTrailing) {
			t.Fatal("a refused Reseal changed the file")
		}
		// keep = DurableSize drops the trailing bytes only.
		if _, err := ta.Reseal(t.Context(), durable, nil); err != nil {
			t.Fatalf("Reseal(%d): %v", durable, err)
		}
		if !bytes.Equal(readFile(t, path), original) {
			t.Error("Reseal(DurableSize) did not leave the closed file")
		}
	})
}

// TestTornAge_EmptyFinalChunkAfterOthers_IsNotComplete builds a file that
// ends with an empty final chunk after a full one, which age rejects and the
// Writer never writes, and checks that OpenTorn does not call it complete.
func TestTornAge_EmptyFinalChunkAfterOthers_IsNotComplete(t *testing.T) {
	scope, riG, rk := newScope(t)
	path := filepath.Join(t.TempDir(), "empty-final.mp4.age")
	plaintext := randomBytes(crypto.ChunkSize, 4)
	var dst bytes.Buffer
	w, err := crypto.NewWriter(&dst, scope)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	mustWrite(t, w, plaintext)
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	file := dst.Bytes()
	hdr := header(t, file)
	fileKey, err := age.DecryptHeader(hdr, rk)
	if err != nil {
		t.Fatalf("DecryptHeader: %v", err)
	}
	streamKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(hkdf.New(sha256.New, fileKey, file[len(hdr):len(hdr)+16], []byte("payload")), streamKey); err != nil {
		t.Fatal(err)
	}
	aead, err := chacha20poly1305.New(streamKey)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSize)
	nonce[10], nonce[11] = 1, 1 // chunk 1, final
	file = aead.Seal(file, nonce, nil, nil)
	if err := os.WriteFile(path, file, 0o600); err != nil {
		t.Fatal(err)
	}
	if r, err := age.Decrypt(bytes.NewReader(file), rk); err == nil {
		if _, err := io.ReadAll(r); err == nil {
			t.Fatal("age.Decrypt accepts an empty final chunk after a full one")
		}
	}

	ta := openTorn(t, path, riG)
	if ta.Complete() || ta.DurableSize() != crypto.ChunkSize {
		t.Fatalf("Complete() = %v, DurableSize() = %d; want false and %d", ta.Complete(), ta.DurableSize(), crypto.ChunkSize)
	}
	if _, err := ta.Reseal(t.Context(), crypto.ChunkSize, nil); err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	if got := decryptStream(t, readFile(t, path), rk); !bytes.Equal(got, plaintext) {
		t.Error("the resealed file does not decrypt to the full chunk")
	}
}

// cancelAfter is a context whose Err reports context.Canceled once it has
// been called n times, as if the context were canceled while OpenTorn runs.
type cancelAfter struct {
	context.Context
	n int
}

func (c *cancelAfter) Err() error {
	if c.n == 0 {
		return context.Canceled
	}
	c.n--
	return nil
}

// TestTornAge_ScanStopsWhenContextIsCanceled: OpenTorn's backward scan checks
// ctx at every chunk, so a long run of bytes at the end of a file that do not
// authenticate does not hold it up after a cancel.
func TestTornAge_ScanStopsWhenContextIsCanceled(t *testing.T) {
	scope, riG, _ := newScope(t)
	path := filepath.Join(t.TempDir(), "garbage-tail.mp4.age.part")
	var dst bytes.Buffer
	w, err := crypto.NewWriter(&dst, scope)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	mustWrite(t, w, randomBytes(crypto.ChunkSize, 8))
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	const garbageChunks = 20
	if err := os.WriteFile(path, append(dst.Bytes(), randomBytes(garbageChunks*encChunkSize, 9)...), 0o600); err != nil {
		t.Fatal(err)
	}

	// Without a cancel, the scan walks back over the garbage to chunk 0.
	if ta := openTorn(t, path, riG); ta.DurableSize() != crypto.ChunkSize || ta.Complete() {
		t.Fatalf("DurableSize() = %d, Complete() = %v; want %d and false", ta.DurableSize(), ta.Complete(), crypto.ChunkSize)
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // path is in the test's directory
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	// OpenTorn's own check and the first chunk's pass; the second chunk's does not.
	ctx := &cancelAfter{Context: t.Context(), n: 2}
	if _, err := crypto.NewTailSealer(riG).OpenTorn(ctx, f); !errors.Is(err, context.Canceled) {
		t.Errorf("OpenTorn with a context canceled during the scan = %v, want context.Canceled", err)
	}
}

// TestTornAge_ReadAtPastEnd_ReturnsEOF: ReadAt follows io.ReaderAt at and
// past DurableSize, with io.EOF after the bytes that are there.
func TestTornAge_ReadAtPastEnd_ReturnsEOF(t *testing.T) {
	scope, riG, _ := newScope(t)
	torn := writeTorn(t, t.TempDir(), "torn.mp4.age.part", scope)
	ta := openTorn(t, torn.path, riG)
	size := ta.DurableSize()
	buf := make([]byte, 10)

	for _, off := range []int64{size, size + 1, size + crypto.ChunkSize} {
		if n, err := ta.ReadAt(buf, off); n != 0 || !errors.Is(err, io.EOF) {
			t.Errorf("ReadAt(10 bytes, %d) = %d, %v; want 0, io.EOF", off, n, err)
		}
	}
	n, err := ta.ReadAt(buf, size-4)
	if n != 4 || !errors.Is(err, io.EOF) || !bytes.Equal(buf[:4], torn.plaintext[size-4:size]) {
		t.Errorf("ReadAt(10 bytes, DurableSize - 4) = %d, %v; want the last 4 bytes and io.EOF", n, err)
	}
	if _, err := ta.ReadAt(buf, -1); err == nil || errors.Is(err, io.EOF) {
		t.Errorf("ReadAt(10 bytes, -1) = %v, want an error other than io.EOF", err)
	}
}

// TestTornAge_WritesNoTempFile is G08: OpenTorn and Reseal work in RAM and in
// the target file only, which keeps its inode.
func TestTornAge_WritesNoTempFile(t *testing.T) {
	scope, riG, rk := newScope(t)
	dir := t.TempDir()
	tmp := t.TempDir()
	// TMP and TEMP are where Windows looks; TMPDIR is the rest.
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, tmp)
	}
	torn := writeTorn(t, dir, "torn.mp4.age.part", scope)
	if err := os.WriteFile(filepath.Join(dir, "other.mp4.age"), []byte("another segment"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, dir)

	keep := int64(tornChunks*crypto.ChunkSize - 1000)
	if _, err := openTorn(t, torn.path, riG).Reseal(t.Context(), keep, nil); err != nil {
		t.Fatalf("Reseal: %v", err)
	}

	if entries, err := os.ReadDir(tmp); err != nil || len(entries) != 0 {
		t.Errorf("the temporary directory holds %d entries (%v)", len(entries), err)
	}
	after := snapshot(t, dir)
	if len(after) != len(before) {
		t.Errorf("the directory holds %d files, it held %d", len(after), len(before))
	}
	for name, was := range before {
		now, ok := after[name]
		switch {
		case !ok:
			t.Errorf("%s is gone", name)
		case !os.SameFile(was, now):
			t.Errorf("%s is a new inode", name)
		case filepath.Join(dir, name) == torn.path:
			if now.Size() == was.Size() {
				t.Errorf("%s kept its size", name)
			}
		case now.Size() != was.Size() || !now.ModTime().Equal(was.ModTime()):
			t.Errorf("%s changed", name)
		}
	}
	if got := decryptStream(t, readFile(t, torn.path), rk); !bytes.Equal(got, torn.plaintext[:keep]) {
		t.Error("the resealed file does not decrypt to the kept plaintext")
	}
}

// snapshot returns the FileInfo of each file in dir, by name.
func snapshot(t *testing.T, dir string) map[string]fs.FileInfo {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	infos := make(map[string]fs.FileInfo, len(entries))
	for _, e := range entries {
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		infos[e.Name()] = info
	}
	return infos
}
