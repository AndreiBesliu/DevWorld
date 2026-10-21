package lastindex

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// The parameter sets of a 1920x1080 H.264 stream: the init's writer parses
// the SPS for the track's size.
var (
	sps = []byte{
		0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02,
		0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04,
		0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9,
		0x20,
	}
	pps = []byte{0x68, 0xee, 0x3c, 0x80}
)

// frame returns the access unit of index as the controller sends it: the
// SEI with the index, then a keyframe every 20 frames (with SPS and PPS, as
// MediaMTX's remuxer puts them first) or a non-IDR slice.
func frame(index uint64) [][]byte {
	payload := binary.BigEndian.AppendUint64([]byte{0x88}, index)
	if index%20 == 0 {
		return append([][]byte{sps, pps}, controller.StampIndex([][]byte{append([]byte{0x65}, payload...)}, index)...)
	}
	return controller.StampIndex([][]byte{append([]byte{0x41}, payload...)}, index)
}

// recording builds the plaintext of an fMP4 recording as MediaMTX writes it:
// an init, then one part (moof and mdat) per entry of parts with the frames
// of those indexes, each followed by a free box, as MTX-2's padding.
func recording(t *testing.T, parts ...[]uint64) []byte {
	t.Helper()
	var out bytes.Buffer
	var init seekablebuffer.Buffer
	if err := (fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: 90000, Codec: &codecs.H264{SPS: sps, PPS: pps}}}}).Marshal(&init); err != nil {
		t.Fatal(err)
	}
	out.Write(init.Bytes())
	for i, indexes := range parts {
		track := &fmp4.PartTrack{ID: 1, BaseTime: indexes[0] * 4500}
		for _, index := range indexes {
			avcc, err := h264.AVCC(frame(index)).Marshal()
			if err != nil {
				t.Fatal(err)
			}
			track.Samples = append(track.Samples, &fmp4.Sample{Duration: 4500, IsNonSyncSample: index%20 != 0, Payload: avcc})
		}
		var part seekablebuffer.Buffer
		if err := (fmp4.Part{SequenceNumber: uint32(i + 1), Tracks: []*fmp4.PartTrack{track}}).Marshal(&part); err != nil { //nolint:gosec // few parts
			t.Fatal(err)
		}
		out.Write(part.Bytes())
		out.Write(freeBox(100))
	}
	return out.Bytes()
}

func freeBox(size int) []byte {
	b := make([]byte, size)
	binary.BigEndian.PutUint32(b, uint32(size)) //nolint:gosec // small
	copy(b[4:], "free")
	return b
}

// span returns the indexes from first to last.
func span(first, last uint64) []uint64 {
	var out []uint64
	for i := first; i <= last; i++ {
		out = append(out, i)
	}
	return out
}

// write puts data at rec/cam01/main/<rel> under dir, encrypted to recipient
// when it is not nil, and returns the path.
func write(t *testing.T, dir, rel string, data []byte, recipient age.Recipient) string {
	t.Helper()
	path := filepath.Join(dir, "rec", "cam01", "main", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if recipient != nil {
		var enc bytes.Buffer
		w, err := age.Encrypt(&enc, recipient)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		data = enc.Bytes()
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func lastRecorded(t *testing.T, files Before, id age.Identity) uint64 {
	t.Helper()
	got, err := LastRecorded(t.Context(), files, id)
	if err != nil {
		t.Fatalf("LastRecorded: %v", err)
	}
	return got
}

func TestLastIndex_ClearSegment(t *testing.T) {
	dir := t.TempDir()
	data := recording(t, span(0, 19), span(20, 39), span(40, 57))
	seg := write(t, dir, "2026-10-21/120000_000000.mp4", data, nil)
	if got := lastRecorded(t, Before{seg}, nil); got != 57 {
		t.Errorf("LastRecorded = %d, want 57", got)
	}

	t.Run("a torn part at the end is not read", func(t *testing.T) {
		next := recording(t, span(58, 77))
		init := len(recording(t)) // the init alone
		torn := append(bytes.Clone(data), next[init:init+(len(next)-init)/2]...)
		seg := write(t, t.TempDir(), "2026-10-21/120000_000000.mp4", torn, nil)
		if got := lastRecorded(t, Before{seg}, nil); got != 57 {
			t.Errorf("LastRecorded = %d, want 57, the last complete sample", got)
		}
	})
}

func TestLastIndex_AgeSegment(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	seg := write(t, t.TempDir(), "2026-10-21/120000_000000.mp4.age", recording(t, span(0, 19), span(20, 39), span(40, 57)), id.Recipient())
	if got := lastRecorded(t, Before{seg}, id); got != 57 {
		t.Errorf("LastRecorded = %d, want 57", got)
	}

	other, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := LastRecorded(t.Context(), Before{seg}, other); err == nil {
		t.Errorf("with another identity: LastRecorded = %d, want an error", got)
	}
	if got, err := LastRecorded(t.Context(), Before{seg}, nil); err == nil {
		t.Errorf("without an identity: LastRecorded = %d, want an error", got)
	}
}

// TestLastIndex_StartsMidGOP: MediaMTX joined the stream in the middle of a
// GOP and started recording at the next keyframe, index 60. The index read
// from the SEI is exact; counting samples from the file start would give 35.
func TestLastIndex_StartsMidGOP(t *testing.T) {
	seg := write(t, t.TempDir(), "2026-10-21/120000_000000.mp4", recording(t, span(60, 79), span(80, 95)), nil)
	if got := lastRecorded(t, Before{seg}, nil); got != 95 {
		t.Errorf("LastRecorded = %d, want 95", got)
	}
}

// TestLastIndex_SpansTwoSegments: reconcile left the newest segment empty
// (only its init was durable), so the last recorded sample is the previous
// segment's last one; the segments lie on both sides of midnight UTC.
func TestLastIndex_SpansTwoSegments(t *testing.T) {
	dir := t.TempDir()
	older := write(t, dir, "2026-10-21/235830_250000.mp4", recording(t, span(0, 19), span(20, 39)), nil)
	for name, newest := range map[string][]byte{"init only": recording(t), "no bytes": nil} {
		t.Run(name, func(t *testing.T) {
			newer := write(t, dir, "2026-10-22/000330_400000.mp4", newest, nil)
			// The order of Before does not matter: the names say which is newest.
			for _, files := range []Before{{older, newer}, {newer, older}} {
				if got := lastRecorded(t, files, nil); got != 39 {
					t.Errorf("LastRecorded(%v) = %d, want 39", files, got)
				}
			}
		})
	}
	t.Run("the next day's segment is the newest", func(t *testing.T) {
		newer := write(t, dir, "2026-10-22/000330_400000.mp4", recording(t, span(40, 45)), nil)
		if got := lastRecorded(t, Before{older, newer}, nil); got != 45 {
			t.Errorf("LastRecorded = %d, want 45 from the segment after midnight", got)
		}
	})
	if _, err := LastRecorded(t.Context(), Before{}, nil); err == nil {
		t.Error("LastRecorded of no files: no error")
	}
}

// TestLastIndex_IgnoresSegmentsAfterRestart: the restarted ipcam-hub records
// a new segment at once, which is not in Before and never supplies the
// index. The .part that was being written at the stop is read under the name
// that reconcile gave it; a .part that reconcile removed (it was empty) is
// skipped.
func TestLastIndex_IgnoresSegmentsAfterRestart(t *testing.T) {
	dir := t.TempDir()
	older := write(t, dir, "2026-10-21/120000_000000.mp4", recording(t, span(0, 39)), nil)
	reconciled := write(t, dir, "2026-10-21/120500_000000.mp4", recording(t, span(40, 59), span(60, 73)), nil)
	write(t, dir, "2026-10-21/121010_000000.mp4", recording(t, span(500, 519)), nil) // after the restart
	removed := filepath.Join(filepath.Dir(reconciled), "120900_000000.mp4.part")     // empty at the stop

	before := Before{older, reconciled + ".part", removed}
	if got := lastRecorded(t, before, nil); got != 73 {
		t.Errorf("LastRecorded = %d, want 73 from the reconciled .part", got)
	}
}
