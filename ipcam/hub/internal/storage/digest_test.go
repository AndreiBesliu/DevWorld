package storage_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// quickXorVectors is rclone's QuickXorHash test table (testdata/quickxor/SOURCE.md).
const quickXorVectors = "testdata/quickxor/vectors.json"

// quickXorVectorCount is how many vectors SOURCE.md says the file holds.
const quickXorVectorCount = 70

// TestDigest_FixedVectors (P34): MD5 and SHA256 against their standard
// vectors (RFC 1321, FIPS 180-2), QUICKXOR against the vectors of an
// independent implementation.
func TestDigest_FixedVectors(t *testing.T) {
	fixed := []struct {
		kind     storage.HashKind
		in, want string
	}{
		{storage.HashMD5, "", "d41d8cd98f00b204e9800998ecf8427e"},
		{storage.HashMD5, "abc", "900150983cd24fb0d6963f7d28e17f72"},
		{storage.HashSHA256, "abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
	}
	for _, v := range fixed {
		got, err := storage.Digest(v.kind, strings.NewReader(v.in))
		if err != nil || got != v.want {
			t.Errorf("Digest(%s, %q) = %q, %v; want %q", v.kind, v.in, got, err, v.want)
		}
	}

	raw, err := os.ReadFile(quickXorVectors)
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Size     int    `json:"size"`
		Data     string `json:"data"`     // the input, in standard base64
		QuickXor string `json:"quickxor"` // its hash, in standard base64
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("%s: %v", quickXorVectors, err)
	}
	if len(vectors) != quickXorVectorCount {
		t.Fatalf("%s holds %d vectors, want %d", quickXorVectors, len(vectors), quickXorVectorCount)
	}
	for _, v := range vectors {
		data, err := base64.StdEncoding.DecodeString(v.Data)
		if err != nil || len(data) != v.Size {
			t.Fatalf("vector of size %d: %d bytes, %v", v.Size, len(data), err)
		}
		got, err := storage.Digest(storage.HashQuickXor, bytes.NewReader(data))
		if err != nil || got != v.QuickXor {
			t.Errorf("vector of size %d: Digest(QUICKXOR) = %q, %v; want %q", v.Size, got, err, v.QuickXor)
		}
	}
}

// TestDigest_QuickXorStreamingEqualsWhole: the hash of a stream does not
// depend on how it is cut. 10 MiB + 7 bytes hashed in one write and in
// pieces of 1 byte, of 320 KiB (a multiple of the 160-byte cycle) and of
// 7 MiB (not one) give the same QUICKXOR.
func TestDigest_QuickXorStreamingEqualsWhole(t *testing.T) {
	data := randomBytes(10<<20+7, 1)
	whole, err := storage.Digest(storage.HashQuickXor, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, piece := range []int{1, 320 << 10, 7 << 20} {
		got, err := storage.Digest(storage.HashQuickXor, &pieceReader{data: data, piece: piece})
		if err != nil || got != whole {
			t.Errorf("in pieces of %d bytes: Digest(QUICKXOR) = %q, %v; want %q, as in one write", piece, got, err, whole)
		}
	}
}

// TestDigest_Errors: ETAG_MULTIPART is not a digest of the bytes (§9.2 never
// interprets S3's final ETag), so Digest refuses it, as it refuses a kind it
// does not know; and a read error is returned, never a digest of part of
// the bytes.
func TestDigest_Errors(t *testing.T) {
	for _, kind := range []storage.HashKind{storage.HashETagMultipart, "CRC32C"} {
		if got, err := storage.Digest(kind, strings.NewReader("abc")); err == nil {
			t.Errorf("Digest(%s) = %q, nil; want an error", kind, got)
		}
	}
	readErr := errors.New("read failed")
	for _, kind := range []storage.HashKind{storage.HashMD5, storage.HashSHA256, storage.HashQuickXor} {
		r := io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(readErr))
		if got, err := storage.Digest(kind, r); !errors.Is(err, readErr) {
			t.Errorf("Digest(%s) of a failing reader = %q, %v; want %v", kind, got, err, readErr)
		}
	}
}

// randomBytes returns size bytes from a seeded generator, so a failure repeats.
func randomBytes(size int, seed uint64) []byte {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:8], seed)
	p := make([]byte, size)
	_, _ = rand.NewChaCha8(key).Read(p)
	return p
}

// pieceReader hands out its data piece bytes at a time: Read returns at most
// piece bytes, and WriteTo, which io.Copy prefers to Read, writes piece bytes
// per Write, so a hash that io.Copy feeds sees exactly those pieces.
type pieceReader struct {
	data  []byte
	piece int
}

func (r *pieceReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p[:min(len(p), r.piece)], r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *pieceReader) WriteTo(w io.Writer) (int64, error) {
	var written int64
	for len(r.data) > 0 {
		n, err := w.Write(r.data[:min(len(r.data), r.piece)])
		written += int64(n)
		r.data = r.data[n:]
		if err != nil {
			return written, err
		}
	}
	return written, nil
}
