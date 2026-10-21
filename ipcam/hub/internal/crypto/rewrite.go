package crypto

import (
	"bytes"
	"context"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
)

// Rewrite writes the first keep plaintext bytes of src, with patches
// applied, to dst as a new age file for scope (C1-R9). Its file key and
// payload nonce are new, so no chunk is ever sealed again under a key and
// nonce that src used (C1-R7), wherever the patches lie in [0, keep). It
// reads, patches and seals one chunk at a time, in place in one buffer: at
// most one chunk of plaintext is in RAM, and only ciphertext reaches dst
// (HUB-04). It neither syncs nor closes dst; a cut leaves dst incomplete,
// and src as it was.
func Rewrite(ctx context.Context, src TornAge, keep int64, patches []Patch, scope KeyScope, dst io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if durable := src.DurableSize(); keep < 1 || keep > durable {
		return fmt.Errorf("crypto: cannot rewrite %d bytes of %d durable ones", keep, durable)
	}
	for _, p := range patches {
		if p.Off < 0 || p.Off > keep-int64(len(p.Data)) {
			return fmt.Errorf("crypto: a patch of %d bytes at %d does not fit in the %d bytes kept", len(p.Data), p.Off, keep)
		}
	}
	start, aead, err := newPayload(scope)
	if err != nil {
		return err
	}
	if _, err := dst.Write(start); err != nil {
		return fmt.Errorf("crypto: write the age header: %w", err)
	}
	buf := make([]byte, encChunkSize)
	chunks := (keep + ChunkSize - 1) / ChunkSize
	for i := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}
		off := i * ChunkSize
		size := min(int64(ChunkSize), keep-off)
		plaintext, err := readPlaintext(src, buf, i, size)
		if err != nil {
			return err
		}
		for _, p := range patches {
			lo, hi := max(p.Off, off), min(p.Off+int64(len(p.Data)), off+size)
			if lo < hi {
				copy(plaintext[lo-off:hi-off], p.Data[lo-p.Off:])
			}
		}
		nonce := chunkNonce(i, i == chunks-1)
		if _, err := dst.Write(aead.Seal(buf[:0], nonce[:], plaintext, nil)); err != nil {
			return fmt.Errorf("crypto: write age chunk %d: %w", i, err)
		}
	}
	return nil
}

// chunkReader is implemented by the TornAges of this package: it decrypts a
// chunk into the caller's buffer, without caching it.
type chunkReader interface {
	readChunk(buf []byte, i int64) ([]byte, error)
}

// readPlaintext returns the first size plaintext bytes of chunk i of src,
// in buf.
func readPlaintext(src TornAge, buf []byte, i, size int64) ([]byte, error) {
	if c, ok := src.(chunkReader); ok {
		plaintext, err := c.readChunk(buf, i)
		if err != nil {
			return nil, err
		}
		return plaintext[:size], nil
	}
	if err := readFullAt(src, buf[:size], i*ChunkSize); err != nil {
		return nil, fmt.Errorf("crypto: read chunk %d: %w", i, err)
	}
	return buf[:size], nil
}

// KeptTail holds in RAM the ciphertext of a torn recording's durable chunks
// from one chunk to its durable end, and no plaintext. Reconcile takes it
// before the Reseal that leaves the interim state of C1-R9, a file cut at
// the end of its last complete part, which seals that chunk again and cuts
// the later ones off; Source then reads the whole durable plaintext again
// for the background Rewrite of the salvage.
type KeptTail struct {
	aead       cipher.AEAD
	start      []byte // the header and payload nonce on disk
	payloadOff int64
	first      int64  // the first chunk kept
	ciphertext []byte // chunks first… up to the end of the last durable one

	chunks, lastSize int64
	lastFinal        bool
}

// KeepTail copies the ciphertext of the durable chunks of t, a TornAge from
// OpenTorn, that a Reseal(ctx, keep, …) of it seals again or cuts off.
func KeepTail(t TornAge, keep int64) (*KeptTail, error) {
	ta, ok := t.(*tornAge)
	if !ok || ta.f == nil {
		return nil, errors.New("crypto: KeepTail needs a TornAge that OpenTorn returned")
	}
	if keep < 1 || keep > ta.DurableSize() {
		return nil, fmt.Errorf("crypto: cannot keep %d bytes of %d durable ones", keep, ta.DurableSize())
	}
	first := (keep - 1) / ChunkSize
	end := ta.offset(ta.chunks-1) + ta.lastSize + tagSize
	k := &KeptTail{
		aead: ta.aead, start: make([]byte, ta.payloadOff), payloadOff: ta.payloadOff, first: first,
		ciphertext: make([]byte, end-ta.offset(first)), chunks: ta.chunks, lastSize: ta.lastSize, lastFinal: ta.lastFinal,
	}
	if err := readFullAt(ta.r, k.start, 0); err != nil {
		return nil, err
	}
	if err := readFullAt(ta.r, k.ciphertext, ta.offset(first)); err != nil {
		return nil, err
	}
	return k, nil
}

// Size is the number of ciphertext bytes held.
func (k *KeptTail) Size() int64 { return int64(len(k.ciphertext)) }

// Source returns a read-only TornAge with the durable plaintext of the
// recording when the tail was kept: the chunks before the kept ones come
// from r, the same recording after the Reseal (whose header and earlier
// chunks are byte-identical), and the kept ones from RAM. It refuses a
// recording with another header, whose chunks are under another key.
func (k *KeptTail) Source(r io.ReaderAt) (TornAge, error) {
	start := make([]byte, len(k.start))
	if err := readFullAt(r, start, 0); err != nil {
		return nil, fmt.Errorf("crypto: read the age header: %w", err)
	}
	if !bytes.Equal(start, k.start) {
		return nil, errors.New("crypto: not the recording whose tail was kept")
	}
	t := &tornAge{aead: k.aead, payloadOff: k.payloadOff}
	t.r = splicedCiphertext{head: r, tail: k.ciphertext, tailOff: t.offset(k.first)}
	t.setEnd(k.chunks, k.lastSize, k.lastFinal, false)
	return t, nil
}

// splicedCiphertext reads the ciphertext before tailOff from head and the
// rest from tail.
type splicedCiphertext struct {
	head    io.ReaderAt
	tail    []byte
	tailOff int64
}

func (s splicedCiphertext) ReadAt(p []byte, off int64) (int, error) {
	n := 0
	if off < s.tailOff {
		k := min(int64(len(p)), s.tailOff-off)
		m, err := s.head.ReadAt(p[:k], off)
		n += m
		if m < int(k) {
			return n, err
		}
		p, off = p[k:], off+k
	}
	if len(p) == 0 {
		return n, nil
	}
	if off-s.tailOff >= int64(len(s.tail)) {
		return n, io.EOF
	}
	m := copy(p, s.tail[off-s.tailOff:])
	n += m
	if m < len(p) {
		return n, io.EOF
	}
	return n, nil
}
