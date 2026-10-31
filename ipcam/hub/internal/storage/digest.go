package storage

import (
	"crypto/md5" //nolint:gosec // G501: Drive reports md5Checksum; Digest reproduces it, it secures nothing
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
)

// Digest returns the hash of r's bytes in kind, written as the provider
// writes it: MD5 and SHA256 in lowercase hex, QUICKXOR in standard base64 of
// its 20 bytes. Before a placement is marked verified, the outbox compares it
// with Commit's ObjectRef.Hash (P34). ETAG_MULTIPART is no hash of the bytes
// (§9.2 never interprets S3's final ETag), so it is an error, as is a kind
// that Digest does not know.
func Digest(kind HashKind, r io.Reader) (string, error) {
	var h hash.Hash
	encode := hex.EncodeToString
	switch kind {
	case HashMD5:
		h = md5.New() //nolint:gosec // G401: a provider's checksum, not a security check
	case HashSHA256:
		h = sha256.New()
	case HashQuickXor:
		h = new(quickXor)
		encode = base64.StdEncoding.EncodeToString
	default:
		return "", fmt.Errorf("storage: no digest of the bytes for hash kind %q", kind)
	}
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("storage: %s digest: %w", kind, err)
	}
	return encode(h.Sum(nil)), nil
}

// QuickXorHash, as Microsoft's reference describes it: byte n of the stream
// is XORed into a 160-bit register at bit (11·n) mod 160, its bits past the
// register's top wrapping to bit 0, and the stream's length, as 8
// little-endian bytes, is then XORed into the register's last 8 bytes. The
// register is little-endian: its bit b is bit b mod 8 of byte b/8.
const (
	quickXorBits  = 160 // the register's width
	quickXorShift = 11  // the bits between the positions of consecutive bytes
	quickXorSize  = quickXorBits / 8
)

// quickXor is QuickXorHash as a hash.Hash. Bytes 160 apart in the stream land
// on the same bits, so Write only XORs byte n into lanes[n mod 160], and Sum
// places each lane in the register.
type quickXor struct {
	lanes  [quickXorBits]byte
	lane   int    // the lane of the next byte: the bytes written, mod 160
	length uint64 // the bytes written
}

func (q *quickXor) Write(p []byte) (int, error) {
	for _, b := range p {
		q.lanes[q.lane] ^= b
		q.lane++
		if q.lane == quickXorBits {
			q.lane = 0
		}
	}
	q.length += uint64(len(p))
	return len(p), nil
}

func (q *quickXor) Sum(b []byte) []byte {
	var register [quickXorSize]byte
	for n, v := range q.lanes {
		bit := n * quickXorShift % quickXorBits
		at, shift := bit/8, bit%8
		register[at] ^= v << shift
		if shift > 0 {
			register[(at+1)%quickXorSize] ^= v >> (8 - shift)
		}
	}
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], q.length)
	for i, v := range length {
		register[quickXorSize-len(length)+i] ^= v
	}
	return append(b, register[:]...)
}

func (q *quickXor) Reset()         { *q = quickXor{} }
func (q *quickXor) Size() int      { return quickXorSize }
func (q *quickXor) BlockSize() int { return quickXorBits }
