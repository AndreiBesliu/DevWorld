package crypto

import (
	"bytes"
	"context"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"filippo.io/age"
	"golang.org/x/crypto/chacha20poly1305"
)

// Patch replaces len(Data) plaintext bytes at offset Off.
type Patch struct {
	Off  int64
	Data []byte
}

// TornAge is an encrypted recording whose end may be missing (§4.5): an
// .age.part after a cut or a crash, or the newest final-named segment of a
// stream, whose last bytes may not have reached the disk.
type TornAge interface {
	// ReadAt reads the plaintext of the authenticated chunks only, which it
	// decrypts on demand, in RAM.
	io.ReaderAt
	// DurableSize is the plaintext size up to the end of the last chunk that
	// authenticates.
	DurableSize() int64
	// Complete reports whether the file ends with a valid final chunk.
	Complete() bool
	// Reseal keeps the first keep plaintext bytes and ends the file there, in
	// place (G08): it seals the last kept chunk again, with patches applied,
	// as the final chunk, and returns the new size of the file. The chunks
	// before it stay byte-identical on disk. Sealing any chunk again with
	// other plaintext under the nonce it had would reuse that nonce (C1-R7),
	// so every patch with data must lie inside the last kept chunk, else
	// ErrPatchBeforeFinalChunk, and a chunk that is already final must keep
	// its plaintext, else ErrFinalChunkChanged.
	Reseal(ctx context.Context, keep int64, patches []Patch) (cipherSize int64, err error)
}

// ErrPatchBeforeFinalChunk is Reseal's refusal of a patch with data that
// starts before the last kept chunk: the chunk it changes would be sealed
// again under its old nonce (C1-R7). The file and the TornAge are left as
// they were.
var ErrPatchBeforeFinalChunk = errors.New("crypto: a patch lies before the last kept age chunk, which alone is sealed again")

// ErrFinalChunkChanged is Reseal's refusal to seal a chunk that is already
// final again with other plaintext, which would reuse its nonce. The file and
// the TornAge are left as they were.
var ErrFinalChunkChanged = errors.New("crypto: the age chunk is already final; sealing it again with other plaintext would reuse its nonce")

// TailSealer opens torn recordings for reconcile (§4.5).
type TailSealer interface {
	// OpenTorn reads the header of f, which must be open for reading, and for
	// writing too when the caller reseals it, and finds its durable chunks,
	// checking ctx at each chunk of the scan. f stays the caller's to close.
	OpenTorn(ctx context.Context, f *os.File) (TornAge, error)
}

// NewTailSealer returns a TailSealer that unwraps file keys with RI_g.
// ipcam-hub's main builds it, so package recorder never holds a key (§4.3).
func NewTailSealer(riG *age.X25519Identity) TailSealer { return tailSealer{riG: riG} }

type tailSealer struct{ riG *age.X25519Identity }

func (s tailSealer) OpenTorn(ctx context.Context, f *os.File) (TornAge, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.riG == nil {
		return nil, errors.New("crypto: the tail sealer has no RI_g")
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	hdr, err := age.ExtractHeader(io.NewSectionReader(f, 0, info.Size()))
	if err != nil {
		return nil, err
	}
	// Chunk offsets count the header bytes on disk, so they must be the
	// canonical header that ExtractHeader returns.
	onDisk := make([]byte, len(hdr)+payloadNonceSize)
	if err := readFullAt(f, onDisk, 0); err != nil {
		return nil, fmt.Errorf("crypto: the file ends before its payload: %w", err)
	}
	if !bytes.Equal(onDisk[:len(hdr)], hdr) {
		return nil, errors.New("crypto: the age header is not in canonical form")
	}
	fileKey, err := age.DecryptHeader(hdr, s.riG)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.New(payloadKey(fileKey, onDisk[len(hdr):]))
	if err != nil {
		return nil, err
	}
	t := &tornAge{r: f, f: f, aead: aead, payloadOff: int64(len(onDisk))}
	if err := t.scan(ctx, info.Size()-t.payloadOff); err != nil {
		return nil, err
	}
	return t, nil
}

// tornAge is a TornAge. ReadAt may run concurrently; Reseal may not run
// concurrently with ReadAt.
type tornAge struct {
	r          io.ReaderAt // the ciphertext
	f          *os.File    // the same file, for Reseal; nil for a read-only source (KeptTail.Source)
	aead       cipher.AEAD
	payloadOff int64 // len(header) + 16

	chunks    int64 // durable chunks
	lastSize  int64 // plaintext bytes of the last durable chunk
	lastFinal bool  // the last durable chunk carries the final-chunk flag
	complete  bool  // and it ends the file

	cache atomic.Pointer[cachedChunk] // the chunk ReadAt decrypted last
}

type cachedChunk struct {
	index int64
	data  []byte
}

// scan finds the durable chunks of a payload of payloadSize bytes. Writes are
// sequential and fsynced at every part, so a cut damages only the end of the
// file: the scan walks back from the end and stops at the first piece that
// authenticates, as a final chunk or as a full non-final one. The chunks
// before it are authenticated by ReadAt, each time it decrypts one. A long
// run of bytes that do not authenticate costs a decryption per chunk, so the
// scan checks ctx at each one; OpenTorn checked it before the trailing piece.
func (t *tornAge) scan(ctx context.Context, payloadSize int64) error {
	full := payloadSize / encChunkSize
	rest := payloadSize % encChunkSize
	buf := make([]byte, encChunkSize)
	if rest > 0 {
		plaintext, ok, err := t.open(buf[:rest], full, true)
		if err != nil {
			return err
		}
		// An empty final chunk is valid only as the only chunk.
		if ok && (len(plaintext) > 0 || full == 0) {
			t.setEnd(full+1, int64(len(plaintext)), true, true)
			return nil
		}
	}
	for i := full - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, final := range []bool{false, true} {
			_, ok, err := t.open(buf, i, final)
			if err != nil {
				return err
			}
			if ok {
				t.setEnd(i+1, ChunkSize, final, final && i == full-1 && rest == 0)
				return nil
			}
		}
	}
	t.setEnd(0, 0, false, false)
	return nil
}

func (t *tornAge) setEnd(chunks, lastSize int64, lastFinal, complete bool) {
	t.chunks, t.lastSize, t.lastFinal, t.complete = chunks, lastSize, lastFinal, complete
	t.cache.Store(nil)
}

// open reads chunk i into buf, whose length is the chunk's ciphertext size,
// and decrypts it in place. ok is false when it does not authenticate.
func (t *tornAge) open(buf []byte, i int64, final bool) (plaintext []byte, ok bool, err error) {
	if err := readFullAt(t.r, buf, t.offset(i)); err != nil {
		return nil, false, fmt.Errorf("crypto: read age chunk %d: %w", i, err)
	}
	nonce := chunkNonce(i, final)
	plaintext, err = t.aead.Open(buf[:0], nonce[:], buf, nil)
	return plaintext, err == nil, nil
}

// offset is where chunk i starts in the file.
func (t *tornAge) offset(i int64) int64 { return t.payloadOff + i*encChunkSize }

func (t *tornAge) DurableSize() int64 {
	if t.chunks == 0 {
		return 0
	}
	return (t.chunks-1)*ChunkSize + t.lastSize
}

func (t *tornAge) Complete() bool { return t.complete }

func (t *tornAge) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("crypto: negative offset %d", off)
	}
	size := t.DurableSize()
	if off > size {
		return 0, io.EOF
	}
	n := 0
	for len(p) > 0 && off < size {
		i := off / ChunkSize
		plaintext, err := t.chunk(i)
		if err != nil {
			return n, err
		}
		k := copy(p, plaintext[off-i*ChunkSize:])
		p = p[k:]
		off += int64(k)
		n += k
	}
	if len(p) > 0 {
		return n, io.EOF
	}
	return n, nil
}

// chunk returns the authenticated plaintext of durable chunk i.
func (t *tornAge) chunk(i int64) ([]byte, error) {
	if c := t.cache.Load(); c != nil && c.index == i {
		return c.data, nil
	}
	plaintext, err := t.readChunk(make([]byte, encChunkSize), i)
	if err != nil {
		return nil, err
	}
	t.cache.Store(&cachedChunk{index: i, data: plaintext})
	return plaintext, nil
}

// readChunk decrypts durable chunk i into buf, which holds a full chunk and
// its tag, authenticates it and returns its plaintext, a prefix of buf. It
// does not cache it.
func (t *tornAge) readChunk(buf []byte, i int64) ([]byte, error) {
	size, final := int64(ChunkSize), false
	if i == t.chunks-1 {
		size, final = t.lastSize, t.lastFinal
	}
	plaintext, ok, err := t.open(buf[:size+tagSize], i, final)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("crypto: age chunk %d does not authenticate", i)
	}
	return plaintext, nil
}

// Reseal re-encrypts in RAM only the last kept chunk, the one that holds byte
// keep−1, with the patches applied, as the final chunk under its
// last-chunk-flag nonce (spec §4.5); that chunk is never empty. It decrypts
// and authenticates that chunk before the file changes. Then it truncates the
// file right after the chunk before it and syncs, writes the new final chunk
// and syncs again; it never writes a temporary file (G08). The header, the
// payload nonce and the earlier chunks are never read back into the cipher
// path or rewritten, so they stay byte-identical on disk. When the last kept
// chunk is the file's final chunk already, kept whole and unchanged, Reseal
// only cuts the bytes after it and syncs.
//
// Nonce reuse (C1-R7). Sealing chunk i again under the STREAM nonce it had,
// (i, final), with the same file key and payload nonce but other plaintext,
// gives whoever holds both ciphertexts the XOR of the two plaintexts and that
// nonce's Poly1305 key, so they can forge that chunk. Old ciphertext can
// survive in old disk blocks, in wear-levelled flash, or in a copy uploaded
// before the repair. So Reseal refuses, before it touches the file:
//
//   - a patch with data that starts before the last kept chunk, whose chunk
//     was non-final and would stay so (ErrPatchBeforeFinalChunk);
//   - other plaintext in the last kept chunk when that chunk is the file's
//     last durable chunk and already final, as in a Complete file or a final
//     chunk followed by trailing bytes (ErrFinalChunkChanged). Reseal may
//     keep that chunk whole and unchanged, or cut it off with a keep in an
//     earlier chunk.
//
// In every other case the last kept chunk was non-final, so sealing it as
// final gives it a nonce it never had. A patch with no data changes no byte,
// so it is accepted wherever it lies in [0, keep]. A patch that reaches
// outside [0, keep] is an argument error.
//
// A cut during Reseal leaves a file that OpenTorn reads again, with at most
// the last kept chunk lost. After an error the file may have changed, so it
// must be opened again; a refused call leaves the file and t as they were. A
// read-only TornAge (KeptTail.Source) refuses every Reseal.
func (t *tornAge) Reseal(ctx context.Context, keep int64, patches []Patch) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if t.f == nil {
		return 0, errors.New("crypto: this torn recording is read-only")
	}
	durable := t.DurableSize()
	if keep < 1 || keep > durable {
		return 0, fmt.Errorf("crypto: cannot keep %d bytes of %d durable ones: the final chunk would be empty or not durable", keep, durable)
	}
	last := (keep - 1) / ChunkSize
	lastStart := last * ChunkSize
	for _, p := range patches {
		if p.Off < 0 || p.Off > keep-int64(len(p.Data)) {
			return 0, fmt.Errorf("crypto: a patch of %d bytes at %d does not fit in the %d bytes kept", len(p.Data), p.Off, keep)
		}
		if len(p.Data) > 0 && p.Off < lastStart {
			return 0, fmt.Errorf("%w: %d bytes at %d, before chunk %d at %d", ErrPatchBeforeFinalChunk, len(p.Data), p.Off, last, lastStart)
		}
	}
	// finalKept: the last kept chunk is already sealed under (last, true).
	finalKept := t.lastFinal && last == t.chunks-1
	if finalKept && keep != durable {
		return 0, fmt.Errorf("%w: chunk %d", ErrFinalChunkChanged, last)
	}
	plaintext := make([]byte, keep-lastStart)
	if _, err := t.ReadAt(plaintext, lastStart); err != nil {
		return 0, err
	}
	var finalBefore []byte
	if finalKept {
		finalBefore = bytes.Clone(plaintext)
	}
	for _, p := range patches {
		if len(p.Data) == 0 {
			continue // it may lie before lastStart
		}
		copy(plaintext[p.Off-lastStart:], p.Data)
	}
	if finalKept && !bytes.Equal(plaintext, finalBefore) {
		return 0, fmt.Errorf("%w: chunk %d", ErrFinalChunkChanged, last)
	}
	cipherSize := t.offset(last) + int64(len(plaintext)) + tagSize
	if finalKept {
		// The final chunk stays as it is: only the bytes after it go. Cutting
		// the file back to the chunk's start and writing it again would lose
		// a valid final chunk to a cut between the two syncs.
		if err := t.truncate(cipherSize); err != nil {
			return 0, err
		}
		t.setEnd(t.chunks, t.lastSize, true, true)
		return cipherSize, nil
	}

	// Truncating and syncing first means a cut can never leave the new final
	// chunk with older chunks after it, which the scan would take as durable.
	if err := t.truncate(t.offset(last)); err != nil {
		return 0, err
	}
	t.cache.Store(nil)
	nonce := chunkNonce(last, true)
	if _, err := t.f.WriteAt(t.aead.Seal(nil, nonce[:], plaintext, nil), t.offset(last)); err != nil {
		return 0, fmt.Errorf("crypto: write age chunk %d: %w", last, err)
	}
	if err := t.f.Sync(); err != nil {
		return 0, err
	}
	t.setEnd(last+1, int64(len(plaintext)), true, true)
	return cipherSize, nil
}

// resealCutHook, when set by a test, runs after each truncation and sync of
// a Reseal, where a cut could stop it.
var resealCutHook func()

// truncate cuts the file to size and syncs it.
func (t *tornAge) truncate(size int64) error {
	if err := t.f.Truncate(size); err != nil {
		return err
	}
	if err := t.f.Sync(); err != nil {
		return err
	}
	if resealCutHook != nil {
		resealCutHook()
	}
	return nil
}

// readFullAt reads len(buf) bytes at off.
func readFullAt(r io.ReaderAt, buf []byte, off int64) error {
	n, err := r.ReadAt(buf, off)
	if n == len(buf) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return err
}
