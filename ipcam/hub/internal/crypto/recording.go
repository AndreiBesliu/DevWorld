// Package crypto writes and reads the hub's encrypted recordings: age v1
// files (§10.2) for exactly two X25519 recipients, RI_g and RK.
//
// The writer builds the format itself (C04), because filippo.io/age's writer
// cannot flush complete chunks at a part's fsync (§4.5): the header from
// Recipient.Wrap stanzas and an HMAC under HKDF(file key, "header"), then the
// payload nonce, then STREAM ChaCha20-Poly1305 chunks of 64 KiB. Chunk i
// starts at len(header) + 16 + i·65552 and its nonce is the 11-byte counter i
// plus the final-chunk flag. Recordings are read through filippo.io/age; only
// a torn file, which age rejects, is read and sealed again by TornAge. The
// tests check the files with age, with the age and rage CLIs, and against the
// C2SP CCTV vectors (ENC-06, ENC-07).
package crypto

import (
	"context"
	"crypto/sha256"
	"io"
	"slices"
	"sync"

	"filippo.io/age"
)

// Recording writes and reads encrypted recordings (§4.4).
type Recording interface {
	// NewWriter returns a *Writer for a new recording encrypted to scope.
	NewWriter(ctx context.Context, dst io.Writer, scope KeyScope) (io.WriteCloser, error)
	// OpenReaderAt returns the plaintext of the recording in src, which is
	// size bytes long, for random access (E6), and the plaintext size.
	OpenReaderAt(ctx context.Context, src io.ReaderAt, size int64) (io.ReaderAt, int64, error)
}

// fileKeyCacheSize bounds the file keys a Recording remembers: enough for the
// segments that a few hours of playback open again and again.
const fileKeyCacheSize = 1024

// NewRecording returns a Recording that opens recordings with ids. Its
// OpenReaderAt is age.DecryptReaderAt; the file key of each header is kept
// (ExtractHeader and DecryptHeader, then NewInjectedFileKeyIdentity), so the
// X25519 unwrap does not run again each time a segment is opened (§10.2).
func NewRecording(ids ...age.Identity) Recording {
	return &recording{ids: slices.Clone(ids), keys: make(map[[sha256.Size]byte][]byte)}
}

type recording struct {
	ids []age.Identity

	mu    sync.Mutex
	keys  map[[sha256.Size]byte][]byte // file key by the SHA-256 of its header
	order [][sha256.Size]byte          // keys in insertion order, the oldest first
}

func (r *recording) NewWriter(ctx context.Context, dst io.Writer, scope KeyScope) (io.WriteCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w, err := NewWriter(dst, scope)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (r *recording) OpenReaderAt(ctx context.Context, src io.ReaderAt, size int64) (io.ReaderAt, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	hdr, err := age.ExtractHeader(io.NewSectionReader(src, 0, size))
	if err != nil {
		return nil, 0, err
	}
	fileKey, err := r.fileKey(hdr)
	if err != nil {
		return nil, 0, err
	}
	// DecryptReaderAt checks the header MAC again with the injected key and
	// authenticates the final chunk before it returns.
	return age.DecryptReaderAt(src, size, age.NewInjectedFileKeyIdentity(fileKey))
}

// fileKey returns the file key of hdr, from the cache or from DecryptHeader.
func (r *recording) fileKey(hdr []byte) ([]byte, error) {
	sum := sha256.Sum256(hdr)
	r.mu.Lock()
	fileKey, ok := r.keys[sum]
	r.mu.Unlock()
	if ok {
		return fileKey, nil
	}
	fileKey, err := age.DecryptHeader(hdr, r.ids...)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.keys[sum]; !ok {
		if len(r.order) == fileKeyCacheSize {
			delete(r.keys, r.order[0])
			r.order = r.order[1:]
		}
		r.keys[sum] = fileKey
		r.order = append(r.order, sum)
	}
	return fileKey, nil
}
