package crypto_test

import (
	"bytes"
	"crypto/sha256"
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
	patch := crypto.Patch{Off: keep - 200<<10, Data: []byte("patched by TestTornAge_PatchIsApplied")}
	if _, err := openTorn(t, torn.path, riG).Reseal(t.Context(), keep, []crypto.Patch{patch}); err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	after := readFile(t, torn.path)

	want := bytes.Clone(torn.plaintext[:keep])
	copy(want[patch.Off:], patch.Data)
	if got := decryptStream(t, after, rk); !bytes.Equal(got, want) {
		t.Error("the resealed file does not hold the kept plaintext with the patch")
	}
	firstPatched := int(patch.Off / crypto.ChunkSize)
	for i := range firstPatched {
		if !bytes.Equal(chunkAt(after, torn.prefix, i), chunkAt(before, torn.prefix, i)) {
			t.Errorf("chunk %d, before the patch, changed", i)
		}
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
