package crypto_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// salvagedTorn returns a torn file (writeTorn), the keep and the patch of a
// salvage whose patch lies 200 KiB before keep, so Reseal refuses it (C1-R9),
// and the plaintext that the rewrite must give.
func salvagedTorn(t *testing.T, scope crypto.KeyScope) (torn tornFile, keep int64, patches []crypto.Patch, want []byte) {
	t.Helper()
	torn = writeTorn(t, t.TempDir(), "torn.mp4.age.part", scope)
	keep = int64(tornChunks*crypto.ChunkSize - 1000)
	patch := crypto.Patch{Off: keep - 200<<10, Data: []byte("the moof's new sample count")}
	want = bytes.Clone(torn.plaintext[:keep])
	copy(want[patch.Off:], patch.Data)
	return torn, keep, []crypto.Patch{patch}, want
}

// chunksOf splits the payload of an age file into its ciphertext chunks.
func chunksOf(t *testing.T, file []byte) [][]byte {
	t.Helper()
	payload := file[len(header(t, file))+16:]
	var chunks [][]byte
	for len(payload) > 0 {
		n := min(len(payload), encChunkSize)
		chunks = append(chunks, payload[:n])
		payload = payload[n:]
	}
	return chunks
}

func rewrite(t *testing.T, src crypto.TornAge, keep int64, patches []crypto.Patch, scope crypto.KeyScope) []byte {
	t.Helper()
	var dst bytes.Buffer
	if err := crypto.Rewrite(t.Context(), src, keep, patches, scope, &dst); err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	return dst.Bytes()
}

// TestRewrite_FreshFileKeyNoChunkReused (C1-R9): the rewritten file has a file
// key of its own, so no chunk is ever sealed again under a key and nonce that
// the source used (C1-R7): no ciphertext chunk of it equals a chunk of the
// source, though most of the plaintext is the same.
func TestRewrite_FreshFileKeyNoChunkReused(t *testing.T) {
	scope, riG, rk := newScope(t)
	torn, keep, patches, want := salvagedTorn(t, scope)
	source := readFile(t, torn.path)
	src := openTorn(t, torn.path, riG)

	out := rewrite(t, src, keep, patches, scope)
	if !bytes.Equal(readFile(t, torn.path), source) {
		t.Error("Rewrite changed its source")
	}
	for _, id := range []age.Identity{riG, rk} {
		if got := decryptStream(t, out, id); !bytes.Equal(got, want) {
			t.Fatalf("the rewritten file decrypts to %d bytes that are not the salvaged plaintext", len(got))
		}
	}
	oldKey, err := age.DecryptHeader(header(t, source), riG)
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := age.DecryptHeader(header(t, out), riG)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(oldKey, newKey) {
		t.Fatal("the rewritten file has the source's file key")
	}
	if bytes.Equal(source[len(header(t, source)):][:16], out[len(header(t, out)):][:16]) {
		t.Error("the rewritten file has the source's payload nonce")
	}
	for i, c := range chunksOf(t, out) {
		for j, s := range chunksOf(t, source) {
			if bytes.Equal(c, s) {
				t.Errorf("chunk %d of the rewritten file equals chunk %d of the source", i, j)
			}
		}
	}
}

// TestRewrite_SalvageBeforeLastChunk_DecryptsWithAgeCLI: a salvage whose
// patch lies chunks before keep, which Reseal refuses, is rewritten into a
// file that age, the age CLIs and rage decrypt with RK alone.
func TestRewrite_SalvageBeforeLastChunk_DecryptsWithAgeCLI(t *testing.T) {
	scope, riG, rk := newScope(t)
	torn, keep, patches, want := salvagedTorn(t, scope)
	src := openTorn(t, torn.path, riG)
	if _, err := src.Reseal(t.Context(), keep, patches); !errors.Is(err, crypto.ErrPatchBeforeFinalChunk) {
		t.Fatalf("Reseal = %v, want ErrPatchBeforeFinalChunk: the salvage must need a rewrite", err)
	}

	path := filepath.Join(t.TempDir(), "torn.mp4.age.rewrite")
	if err := os.WriteFile(path, rewrite(t, src, keep, patches, scope), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := decryptStream(t, readFile(t, path), rk); !bytes.Equal(got, want) {
		t.Fatal("age.Decrypt does not give the salvaged plaintext")
	}
	identityFile := writeIdentityFile(t, rk)
	for _, tool := range ageTools(t) {
		got, err := decryptWithTool(t, tool, identityFile, path)
		if err != nil {
			t.Errorf("%s: %v", tool.name, err)
		} else if !bytes.Equal(got, want) {
			t.Errorf("%s: %d bytes that are not the salvaged plaintext", tool.name, len(got))
		}
	}
}

// TestRewrite_RejectsBadArguments: a keep outside [1, DurableSize] or a patch
// outside [0, keep) writes nothing.
func TestRewrite_RejectsBadArguments(t *testing.T) {
	scope, riG, _ := newScope(t)
	torn := writeTorn(t, t.TempDir(), "torn.mp4.age.part", scope)
	src := openTorn(t, torn.path, riG)
	durable := src.DurableSize()
	for _, tc := range []struct {
		name    string
		keep    int64
		patches []crypto.Patch
	}{
		{"keep 0", 0, nil},
		{"keep past the durable end", durable + 1, nil},
		{"a patch past keep", 1000, []crypto.Patch{{Off: 995, Data: []byte("0123456789")}}},
		{"a negative patch", 1000, []crypto.Patch{{Off: -1, Data: []byte("x")}}},
	} {
		var dst bytes.Buffer
		if err := crypto.Rewrite(t.Context(), src, tc.keep, tc.patches, scope, &dst); err == nil {
			t.Errorf("%s: Rewrite accepted it", tc.name)
		}
		if dst.Len() != 0 {
			t.Errorf("%s: a refused Rewrite wrote %d bytes", tc.name, dst.Len())
		}
	}
}

// TestKeptTail_ReadsWhatTheInterimResealCutOff (C1-R9): reconcile seals a
// torn file at the end of its last complete part as the interim state, which
// cuts off the chunks that the salvage needs. KeepTail keeps their
// ciphertext in RAM first, so that a rewrite after the interim Reseal still
// reads the whole durable plaintext. It holds no plaintext.
func TestKeptTail_ReadsWhatTheInterimResealCutOff(t *testing.T) {
	scope, riG, rk := newScope(t)
	torn, keep, patches, want := salvagedTorn(t, scope)
	interimKeep := keep - 300<<10 // the end of the last complete part
	src := openTorn(t, torn.path, riG)
	durable := src.DurableSize()

	kept, err := crypto.KeepTail(src, interimKeep)
	if err != nil {
		t.Fatalf("KeepTail: %v", err)
	}
	if kept.Size() > durable-(interimKeep-1)/crypto.ChunkSize*crypto.ChunkSize+2*encChunkSize {
		t.Errorf("KeepTail holds %d bytes, more than the chunks from the interim's last one on", kept.Size())
	}
	if _, err := src.Reseal(t.Context(), interimKeep, nil); err != nil {
		t.Fatalf("the interim Reseal: %v", err)
	}
	if got := decryptStream(t, readFile(t, torn.path), rk); !bytes.Equal(got, torn.plaintext[:interimKeep]) {
		t.Fatal("the interim file does not decrypt to the plaintext up to the last complete part")
	}

	f, err := os.Open(torn.path) //nolint:gosec // path is in the test's directory
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	source, err := kept.Source(f)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if source.DurableSize() != durable || source.Complete() {
		t.Errorf("Source: DurableSize %d, Complete %v; want %d and false", source.DurableSize(), source.Complete(), durable)
	}
	if got := readAll(t, source, durable); !bytes.Equal(got, torn.plaintext[:durable]) {
		t.Fatal("Source does not read the durable plaintext of the file before the interim Reseal")
	}
	if _, err := source.Reseal(t.Context(), keep, nil); err == nil {
		t.Error("Source's Reseal changed something; it is read-only")
	}
	if got := decryptStream(t, rewrite(t, source, keep, patches, scope), rk); !bytes.Equal(got, want) {
		t.Fatal("the rewrite from Source does not give the salvaged plaintext")
	}

	// Another recording is refused: its chunks are under another key.
	other := writeTorn(t, t.TempDir(), "other.mp4.age.part", scope)
	g, err := os.Open(other.path) //nolint:gosec // path is in the test's directory
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	if _, err := kept.Source(g); err == nil {
		t.Error("Source accepted another recording")
	}
}

// TestTornAge_ResealFinalChunkAtDurableSize_OnlyTruncates: when the last
// durable chunk is already final and keep is DurableSize, Reseal only drops
// the bytes after it. A cut during that Reseal must not lose the valid final
// chunk, as cutting the file back to the chunk's start and writing it again
// would. (A full final chunk is found under trailing bytes, as Close seals
// one when the plaintext ends on a chunk boundary.)
func TestTornAge_ResealFinalChunkAtDurableSize_OnlyTruncates(t *testing.T) {
	scope, riG, rk := newScope(t)
	plaintext := randomBytes(2*crypto.ChunkSize, 8)
	path, closed, _ := writeClosed(t, t.TempDir(), "trailing.mp4.age", scope, plaintext)
	if err := os.WriteFile(path, append(bytes.Clone(closed), randomBytes(300, 9)...), 0o600); err != nil {
		t.Fatal(err)
	}
	ta := openTorn(t, path, riG)
	if ta.Complete() || ta.DurableSize() != int64(len(plaintext)) {
		t.Fatalf("Complete() = %v, DurableSize() = %d; want false and %d", ta.Complete(), ta.DurableSize(), len(plaintext))
	}
	var atCut [][]byte
	restore := crypto.SetResealCutHook(func() { atCut = append(atCut, readFile(t, path)) })
	defer restore()
	cipherSize, err := ta.Reseal(t.Context(), ta.DurableSize(), nil)
	if err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	if cipherSize != int64(len(closed)) || !bytes.Equal(readFile(t, path), closed) {
		t.Fatal("Reseal(DurableSize) did not leave exactly the closed file")
	}
	for _, file := range atCut {
		if got := decryptStream(t, file, rk); !bytes.Equal(got, plaintext) {
			t.Fatal("a cut during the Reseal would leave a file without its final chunk")
		}
	}
}
