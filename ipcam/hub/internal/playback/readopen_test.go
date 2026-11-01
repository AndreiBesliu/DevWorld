package playback

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// recording is an fMP4 init and n parts of one 1-s sample of payload bytes
// each, as MediaMTX writes them.
func recording(t *testing.T, n, payload int) (init []byte, parts [][]byte) {
	t.Helper()
	var buf seekablebuffer.Buffer
	i := fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: 90000, Codec: &codecs.H264{
		SPS: []byte{0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02, 0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20},
		PPS: []byte{0x08, 0x06, 0x07, 0x08},
	}}}}
	if err := i.Marshal(&buf); err != nil {
		t.Fatal(err)
	}
	init = bytes.Clone(buf.Bytes())
	for k := range n {
		var pb seekablebuffer.Buffer
		p := fmp4.Part{SequenceNumber: uint32(k + 1), Tracks: []*fmp4.PartTrack{{ID: 1, BaseTime: uint64(k) * 90000, //nolint:gosec // a few parts
			Samples: []*fmp4.Sample{{Duration: 90000, Payload: bytes.Repeat([]byte{byte(k)}, payload)}}}}}
		if err := p.Marshal(&pb); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, bytes.Clone(pb.Bytes()))
	}
	return init, parts
}

// streamDir is the directory of today's (UTC) segments of cam1/sub under
// recDir, as MediaMTX names it.
func streamDir(recDir string) string {
	return filepath.Join(recDir, "cam1", "sub", time.Now().UTC().Format(time.DateOnly))
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestPlayback_ReadOpen_ClearPart (P14): ReadOpen reads the newest .part of
// the stream directly: its ftyp, moov and every whole moof+mdat pair, and
// stops at the torn 4th part, where a finished segment's reader would fail.
// An older .part and a finished segment do not count.
func TestPlayback_ReadOpen_ClearPart(t *testing.T) {
	recDir := t.TempDir()
	dir := streamDir(recDir)
	init, parts := recording(t, 4, 3000)
	torn := slices.Concat(init, parts[0], parts[1], parts[2], parts[3][:len(parts[3])-20])
	writeFile(t, filepath.Join(dir, "101500_000000.mp4.part"), torn)
	writeFile(t, filepath.Join(dir, "101000_000000.mp4.part"), slices.Concat(init, parts[0]))
	writeFile(t, filepath.Join(dir, "100500_000000.mp4"), slices.Concat(init, parts[0], parts[1], parts[2], parts[3]))
	got, err := ReadOpen(t.Context(), recDir, "cam1", "sub", nil)
	if err != nil {
		t.Fatalf("ReadOpen: %v", err)
	}
	if got.Encrypted || got.Parts != 3 || got.DurableBytes != int64(len(torn)) || got.ReadMs < 0 {
		t.Errorf("ReadOpen = %+v, want clear, 3 parts and %d durable bytes", got, len(torn))
	}
}

// TestPlayback_ReadOpen_AgePart (P14): an .age.part is opened read-only
// through the tail sealer and read up to its durable size, the chunks that
// the writer flushed: 2 chunks, 2 × 64 KiB. Its parts are counted on the
// plaintext, those that end within the durable bytes.
func TestPlayback_ReadOpen_AgePart(t *testing.T) {
	riG, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rk, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	scope, err := crypto.ParseKeyScope(riG.Recipient().String(), rk.Recipient().String())
	if err != nil {
		t.Fatal(err)
	}
	init, parts := recording(t, 40, 4000)
	plain := slices.Concat(append([][]byte{init}, parts...)...)
	const durable = 2 * crypto.ChunkSize
	if len(plain) <= durable || len(plain) >= 3*crypto.ChunkSize {
		t.Fatalf("the plaintext holds %d bytes, want between 2 and 3 chunks", len(plain))
	}
	wantParts, end := 0, len(init)
	for _, p := range parts {
		if end += len(p); end <= durable {
			wantParts++
		}
	}
	var file bytes.Buffer
	w, err := crypto.NewWriter(&file, scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	recDir := t.TempDir()
	path := filepath.Join(streamDir(recDir), "101500_000000.mp4.age.part")
	writeFile(t, path, file.Bytes())
	before, err := os.ReadFile(path) //nolint:gosec // the test's file
	if err != nil {
		t.Fatal(err)
	}

	got, err := ReadOpen(t.Context(), recDir, "cam1", "sub", crypto.NewTailSealer(riG))
	if err != nil {
		t.Fatalf("ReadOpen: %v", err)
	}
	if !got.Encrypted || got.DurableBytes != durable || got.Parts != wantParts || wantParts == 0 {
		t.Errorf("ReadOpen = %+v, want encrypted, %d durable bytes and %d parts", got, durable, wantParts)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) { //nolint:gosec // the test's file
		t.Errorf("ReadOpen changed the open segment (%v)", err)
	}
	if _, err := ReadOpen(t.Context(), recDir, "cam1", "sub", nil); err == nil || errors.Is(err, ErrNoOpenSegment) {
		t.Errorf("ReadOpen of an .age.part without a tail sealer = %v, want an error other than ErrNoOpenSegment", err)
	}
}

// TestPlayback_ReadOpen_NoOpenSegment: a stream with no .part, its
// directory missing or holding finished segments only, has no open segment.
func TestPlayback_ReadOpen_NoOpenSegment(t *testing.T) {
	recDir := t.TempDir()
	if _, err := ReadOpen(t.Context(), recDir, "cam1", "sub", nil); !errors.Is(err, ErrNoOpenSegment) {
		t.Errorf("ReadOpen of no directory = %v, want ErrNoOpenSegment", err)
	}
	init, parts := recording(t, 1, 100)
	writeFile(t, filepath.Join(streamDir(recDir), "101500_000000.mp4"), slices.Concat(init, parts[0]))
	if _, err := ReadOpen(t.Context(), recDir, "cam1", "sub", nil); !errors.Is(err, ErrNoOpenSegment) {
		t.Errorf("ReadOpen of finished segments only = %v, want ErrNoOpenSegment", err)
	}
}

// TestPlayback_ReadOpen_NotFMP4: an open segment that is not laid out as
// MediaMTX writes one, its first box no ftyp, is an error of its own, not a
// torn tail of no part.
func TestPlayback_ReadOpen_NotFMP4(t *testing.T) {
	recDir := t.TempDir()
	writeFile(t, filepath.Join(streamDir(recDir), "101500_000000.mp4.part"), bytes.Repeat([]byte("not an fMP4 file "), 10))
	if _, err := ReadOpen(t.Context(), recDir, "cam1", "sub", nil); !errors.Is(err, ErrNotFMP4) {
		t.Errorf("ReadOpen of a file that is no fMP4 = %v, want ErrNotFMP4", err)
	}
}
