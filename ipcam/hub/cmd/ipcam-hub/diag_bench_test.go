package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// The test recordings of the diag benches are written as MediaMTX writes
// them, with mediacommon: an init (ftyp, moov), then a moof and an mdat per
// part, one H.264 track at 90 kHz with samples of 1/30 s and an IDR every
// benchGOP samples, then encrypted through crypto's age writer (MTX-2).
const (
	benchTimeScale = 90000
	benchSampleDur = 3000
	benchGOP       = 20
)

var (
	benchSPS = []byte{
		0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02,
		0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04,
		0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20,
	}
	benchPPS = []byte{0x08, 0x06, 0x07, 0x08}
)

// benchSegment is a test recording: its encrypted file and RI_g's identity
// file, and, in its plaintext, each part's span from its moof to the end of
// its mdat, and each sample's decode time, sync flag and payload.
type benchSegment struct {
	path, identity string
	parts          []span
	samples        []benchSample
}

type benchSample struct {
	time    uint64
	sync    bool
	payload []byte
}

// writeBenchSegment writes a recording of parts parts of perPart samples
// into dir. Each sample's bytes name its index; the samples of part p are
// size + 4p bytes, so that no two parts have the same length.
func writeBenchSegment(t *testing.T, dir string, parts, perPart, size int) benchSegment {
	t.Helper()
	var buf seekablebuffer.Buffer
	init := fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: benchTimeScale, Codec: &codecs.H264{SPS: benchSPS, PPS: benchPPS}}}}
	if err := init.Marshal(&buf); err != nil {
		t.Fatal(err)
	}
	var seg benchSegment
	for p := range parts {
		track := &fmp4.PartTrack{ID: 1, BaseTime: uint64(p * perPart * benchSampleDur)} //nolint:gosec // small test values
		for s := range perPart {
			i := p*perPart + s
			payload := bytes.Repeat(binary.BigEndian.AppendUint32(nil, uint32(i)), size/4+p) //nolint:gosec // small test values
			sync := i%benchGOP == 0
			track.Samples = append(track.Samples, &fmp4.Sample{Duration: benchSampleDur, IsNonSyncSample: !sync, Payload: payload})
			seg.samples = append(seg.samples, benchSample{time: uint64(i * benchSampleDur), sync: sync, payload: payload}) //nolint:gosec // small test values
		}
		var part seekablebuffer.Buffer
		if err := (&fmp4.Part{SequenceNumber: uint32(p), Tracks: []*fmp4.PartTrack{track}}).Marshal(&part); err != nil { //nolint:gosec // small test values
			t.Fatal(err)
		}
		seg.parts = append(seg.parts, span{off: int64(buf.Len()), n: int64(part.Len())})
		_, _ = buf.Write(part.Bytes()) // a seekablebuffer's Write never fails
	}

	riG, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rk, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	var file bytes.Buffer
	w, err := crypto.NewWriter(&file, crypto.KeyScope{RecordingIdentity: riG.Recipient(), Recovery: rk.Recipient()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	seg.path = filepath.Join(dir, "20261029-120000-000000.mp4.age")
	seg.identity = filepath.Join(dir, "ri_g.key")
	if err := os.WriteFile(seg.path, file.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seg.identity, []byte(riG.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return seg
}

// decodeLine decodes the one JSON line that a diag bench printed into v,
// with no field that v lacks.
func decodeLine(t *testing.T, line string, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
}
