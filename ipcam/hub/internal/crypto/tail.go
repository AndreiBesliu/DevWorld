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
	// Reseal keeps the first keep plaintext bytes, with patches applied, and
	// ends the file there with a final chunk, in place (G08). It returns the
	// new size of the file.
	Reseal(ctx context.Context, keep int64, patches []Patch) (cipherSize int64, err error)
}

// TailSealer opens torn recordings for reconcile (§4.5).
type TailSealer interface {
	// OpenTorn reads the header of f, which must be open for reading and
	// writing, and finds its durable chunks. f stays the caller's to close.
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
	t := &tornAge{f: f, aead: aead, payloadOff: int64(len(onDisk))}
	if err := t.scan(info.Size() - t.payloadOff); err != nil {
		return nil, err
	}
	return t, nil
}

// tornAge is a TornAge. ReadAt may run concurrently; Reseal may not run
// concurrently with ReadAt.
type tornAge struct {
	f          *os.File
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
// before it are authenticated by ReadAt, each time it decrypts one.
func (t *tornAge) scan(payloadSize int64) error {
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
	if err := readFullAt(t.f, buf, t.offset(i)); err != nil {
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
	size := t.DurableSize()
	if off < 0 || off > size {
		return 0, fmt.Errorf("crypto: offset %d is outside the %d durable bytes", off, size)
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
	size, final := int64(ChunkSize), false
	if i == t.chunks-1 {
		size, final = t.lastSize, t.lastFinal
	}
	plaintext, ok, err := t.open(make([]byte, size+tagSize), i, final)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("crypto: age chunk %d does not authenticate", i)
	}
	t.cache.Store(&cachedChunk{index: i, data: plaintext})
	return plaintext, nil
}

// Reseal re-encrypts in RAM every chunk from the one that holds the lowest
// patch, or else the last kept chunk, up to keep; the last of them becomes
// the final chunk, which is never empty. It truncates the file and syncs it
// before it writes those chunks back in place, and syncs again; it never
// writes a temporary file (G08). The plaintext of those chunks is decrypted
// and authenticated before the file changes.
//
// A patched chunk other than the last is sealed again under its old nonce:
// whoever holds both the old and the new ciphertext of that chunk learns the
// XOR of the two plaintexts, and can forge that chunk.
//
// A cut during Reseal leaves a file that OpenTorn reads again, with at most
// the last kept chunk lost, unless it tears a chunk being written. After an
// error the file may have changed, so it must be opened again.
func (t *tornAge) Reseal(ctx context.Context, keep int64, patches []Patch) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if durable := t.DurableSize(); keep < 1 || keep > durable {
		return 0, fmt.Errorf("crypto: cannot keep %d bytes of %d durable ones: the final chunk would be empty or not durable", keep, durable)
	}
	last := (keep - 1) / ChunkSize
	first := last
	for _, p := range patches {
		if p.Off < 0 || p.Off > keep-int64(len(p.Data)) {
			return 0, fmt.Errorf("crypto: a patch of %d bytes at %d does not fit in the %d bytes kept", len(p.Data), p.Off, keep)
		}
		if len(p.Data) > 0 {
			first = min(first, p.Off/ChunkSize)
		}
	}
	start := first * ChunkSize
	plaintext := make([]byte, keep-start)
	if _, err := t.ReadAt(plaintext, start); err != nil {
		return 0, err
	}
	for _, p := range patches {
		copy(plaintext[p.Off-start:], p.Data)
	}

	// Truncating first means a cut can never leave the new final chunk with
	// older chunks after it.
	cipherSize := t.offset(last) + keep - last*ChunkSize + tagSize
	if err := t.f.Truncate(cipherSize); err != nil {
		return 0, err
	}
	if err := t.f.Sync(); err != nil {
		return 0, err
	}
	t.cache.Store(nil)
	for i := first; i <= last; i++ {
		lo := (i - first) * ChunkSize
		chunk := plaintext[lo:min(lo+ChunkSize, int64(len(plaintext)))]
		nonce := chunkNonce(i, i == last)
		if _, err := t.f.WriteAt(t.aead.Seal(nil, nonce[:], chunk, nil), t.offset(i)); err != nil {
			return 0, fmt.Errorf("crypto: write age chunk %d: %w", i, err)
		}
	}
	if err := t.f.Sync(); err != nil {
		return 0, err
	}
	t.setEnd(last+1, keep-last*ChunkSize, true, true)
	return cipherSize, nil
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
