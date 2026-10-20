package crypto

import (
	"bytes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// ChunkSize is the number of plaintext bytes in an age STREAM chunk.
const ChunkSize = 64 << 10

const (
	tagSize          = chacha20poly1305.Overhead // the Poly1305 tag of each chunk
	encChunkSize     = ChunkSize + tagSize       // a full chunk on disk
	fileKeySize      = 16
	payloadNonceSize = 16
	headerIntro      = "age-encryption.org/v1\n"
	base64Columns    = 64 // the width of a stanza body line
)

// ErrEmptyFinalChunk is returned by Close when every byte written is already
// sealed, after a Flush at a chunk boundary: age readers reject an empty final
// chunk after other chunks. The Writer is left as it was, so the caller can
// write at least one more byte (MTX-2 writes an fMP4 free box) and Close again.
var ErrEmptyFinalChunk = errors.New("crypto: the final age chunk would be empty; write at least one more byte before Close")

var errClosed = errors.New("crypto: the age writer is closed")

// Writer writes an age v1 file (§10.2) to dst: the header for RI_g and RK and
// the payload nonce at once, then the STREAM payload in 64 KiB chunks.
// Unlike age.Encrypt's writer, it can Flush every complete chunk at a part's
// fsync (§4.5), so that only the bytes after the last chunk boundary are
// held in RAM.
//
// A Writer is not safe for concurrent use. After a write to dst fails, every
// method returns that error: what reached dst is unknown.
type Writer struct {
	dst    io.Writer
	aead   cipher.AEAD
	buf    []byte // ChunkSize bytes of plaintext, plus room for the tag
	held   int    // plaintext bytes in buf, not sealed yet
	chunk  int64  // index of the next chunk to seal
	err    error
	closed bool
}

// NewWriter writes the age header, which wraps a new random file key for
// RI_g and RK, and the 16-byte payload nonce to dst in one Write, and returns
// a Writer for the payload. It does not close dst.
func NewWriter(dst io.Writer, scope KeyScope) (*Writer, error) {
	recipients, err := scope.recipients()
	if err != nil {
		return nil, err
	}
	fileKey := make([]byte, fileKeySize)
	_, _ = rand.Read(fileKey) // crypto/rand.Read never fails
	hdr, err := marshalHeader(fileKey, recipients)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, payloadNonceSize)
	_, _ = rand.Read(nonce)
	aead, err := chacha20poly1305.New(payloadKey(fileKey, nonce))
	if err != nil {
		return nil, err
	}
	if _, err := dst.Write(append(hdr, nonce...)); err != nil {
		return nil, fmt.Errorf("crypto: write the age header: %w", err)
	}
	return &Writer{dst: dst, aead: aead, buf: make([]byte, encChunkSize)}, nil
}

// Write buffers p. A full chunk is sealed, as a non-final chunk, only once a
// byte follows it, since until then it may turn out to be the last.
func (w *Writer) Write(p []byte) (int, error) {
	if err := w.usable(); err != nil {
		return 0, err
	}
	n := 0
	for len(p) > 0 {
		if w.held == ChunkSize {
			if err := w.seal(false); err != nil {
				return n, err
			}
		}
		k := copy(w.buf[w.held:ChunkSize], p)
		w.held += k
		n += k
		p = p[k:]
	}
	return n, nil
}

// Flush seals the held chunk as a non-final chunk if it is complete. The
// remainder of less than ChunkSize bytes stays in RAM. After a Flush at a
// chunk boundary, at least one more byte must be written before Close.
func (w *Writer) Flush() error {
	if err := w.usable(); err != nil {
		return err
	}
	if w.held == ChunkSize {
		return w.seal(false)
	}
	return nil
}

// Buffered returns the number of plaintext bytes held in RAM, not yet sealed.
func (w *Writer) Buffered() int { return w.held }

// Close seals the held bytes as the final chunk. It returns
// ErrEmptyFinalChunk, and changes nothing, when nothing is held but the
// plaintext is not empty. It does not close dst.
func (w *Writer) Close() error {
	if err := w.usable(); err != nil {
		return err
	}
	if w.held == 0 && w.chunk > 0 {
		return ErrEmptyFinalChunk
	}
	if err := w.seal(true); err != nil {
		return err
	}
	w.closed = true
	return nil
}

func (w *Writer) usable() error {
	if w.err != nil {
		return w.err
	}
	if w.closed {
		return errClosed
	}
	return nil
}

// seal encrypts the held bytes as the next chunk and writes it to dst.
func (w *Writer) seal(last bool) error {
	nonce := chunkNonce(w.chunk, last)
	ciphertext := w.aead.Seal(w.buf[:0], nonce[:], w.buf[:w.held], nil)
	if _, err := w.dst.Write(ciphertext); err != nil {
		w.err = fmt.Errorf("crypto: write age chunk %d: %w", w.chunk, err)
		return w.err
	}
	w.chunk++
	w.held = 0
	return nil
}

// marshalHeader builds the age v1 header from public pieces (C04): the
// stanzas that each recipient's Wrap returns, in the text format, and the
// HMAC-SHA-256 under HKDF(file key, info "header") of everything up to "---".
func marshalHeader(fileKey []byte, recipients []age.Recipient) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(headerIntro)
	for _, r := range recipients {
		stanzas, err := r.Wrap(fileKey)
		if err != nil {
			return nil, fmt.Errorf("crypto: wrap the file key: %w", err)
		}
		for _, s := range stanzas {
			writeStanza(&b, s)
		}
	}
	b.WriteString("---")
	mac := hmac.New(sha256.New, hkdfKey(fileKey, nil, "header"))
	mac.Write(b.Bytes())
	b.WriteString(" " + base64.RawStdEncoding.EncodeToString(mac.Sum(nil)) + "\n")
	return b.Bytes(), nil
}

// writeStanza writes "-> type args…" and the body in unpadded base64, in lines
// of 64 columns; the last line is always shorter, so it may be empty.
func writeStanza(b *bytes.Buffer, s *age.Stanza) {
	b.WriteString("-> " + strings.Join(append([]string{s.Type}, s.Args...), " ") + "\n")
	body := base64.RawStdEncoding.EncodeToString(s.Body)
	for len(body) >= base64Columns {
		b.WriteString(body[:base64Columns] + "\n")
		body = body[base64Columns:]
	}
	b.WriteString(body + "\n")
}

// payloadKey is the STREAM key: HKDF(file key, salt = payload nonce, info "payload").
func payloadKey(fileKey, nonce []byte) []byte { return hkdfKey(fileKey, nonce, "payload") }

func hkdfKey(secret, salt []byte, info string) []byte {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(hkdf.New(sha256.New, secret, salt, []byte(info)), key); err != nil {
		panic("crypto: HKDF-SHA-256 cannot give 32 bytes: " + err.Error())
	}
	return key
}

// chunkNonce is the STREAM nonce of chunk i: an 11-byte big-endian counter,
// then 1 for the final chunk and 0 for the others.
func chunkNonce(i int64, last bool) [chacha20poly1305.NonceSize]byte {
	var nonce [chacha20poly1305.NonceSize]byte
	binary.BigEndian.PutUint64(nonce[3:11], uint64(i)) //nolint:gosec // chunk indexes are never negative
	if last {
		nonce[11] = 1
	}
	return nonce
}
