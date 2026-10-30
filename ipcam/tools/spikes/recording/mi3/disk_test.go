package mi3

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
)

// segment is an fMP4 recording at 90 kHz of parts, each of samples
// samples of 1,000 bytes, 20 per second, with a free box after each part
// as MTX-2's padding.
func segment(t *testing.T, parts, samples int) []byte {
	t.Helper()
	var buf seekablebuffer.Buffer
	init := fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: 90000, Codec: &codecs.H264{
		SPS: []byte{0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02, 0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20},
		PPS: []byte{0x08, 0x06, 0x07, 0x08},
	}}}}
	if err := init.Marshal(&buf); err != nil {
		t.Fatal(err)
	}
	for p := range parts {
		part := fmp4.Part{SequenceNumber: uint32(p + 1), Tracks: []*fmp4.PartTrack{{ID: 1, BaseTime: uint64(p * samples * 4500)}}} //nolint:gosec // a few parts
		for range samples {
			part.Tracks[0].Samples = append(part.Tracks[0].Samples, &fmp4.Sample{Duration: 4500, Payload: bytes.Repeat([]byte{7}, 1000)})
		}
		if err := part.Marshal(&buf); err != nil {
			t.Fatal(err)
		}
		if _, err := buf.Write(append([]byte{0, 0, 0, 16, 'f', 'r', 'e', 'e'}, make([]byte, 8)...)); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDisk_RatioPerModeAndPartFsyncs (§4.8, R02): disk ÷ bitrate is, per
// mode, the segment files' bytes over their samples' bytes, the encrypted
// ones decrypted with the run's identity; the fsync rate is the parts'
// closes per second of recording, without each file's last part, which a
// close or the stop may cut short.
func TestDisk_RatioPerModeAndPartFsyncs(t *testing.T) {
	rec := t.TempDir()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	closed := segment(t, 3, 20)  // three 1 s parts
	partial := segment(t, 2, 20) // a .part that a stop left, of two
	writeTestFile(t, filepath.Join(rec, "cam01", "main", "2026-10-29", "120000_000000.mp4"), closed)
	writeTestFile(t, filepath.Join(rec, "cam03", "main", "2026-10-29", "120000_000000.mp4.part"), partial)
	var enc bytes.Buffer
	w, err := age.Encrypt(&enc, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(closed); err != nil || w.Close() != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(rec, "cam02", "main", "2026-10-29", "120000_000000.mp4.age"), enc.Bytes())
	writeTestFile(t, filepath.Join(rec, "cam02", "main", "2026-10-29", "notes.txt"), []byte("not a segment"))

	got, err := measureDisk(rec, id)
	if err != nil {
		t.Fatal(err)
	}
	wantClear := float64(len(closed)+len(partial)) / float64((3+2)*20*1000)
	wantEnc := float64(enc.Len()) / float64(3*20*1000)
	if math.Abs(got.ratio[modeClear]-wantClear) > 1e-9 || math.Abs(got.ratio[modeEncrypted]-wantEnc) > 1e-9 {
		t.Errorf("ratios %v, want clear %v and encrypted %v", got.ratio, wantClear, wantEnc)
	}
	if got.ratio[modeEncrypted] <= got.ratio[modeClear] {
		t.Errorf("encrypted %v ≤ clear %v: age's overhead is missing", got.ratio[modeEncrypted], got.ratio[modeClear])
	}
	// 2 + 2 + 1 closes over 2 + 2 + 1 seconds.
	if math.Abs(got.fsyncPerSec-1) > 1e-9 {
		t.Errorf("fsync rate %v, want 1 per second", got.fsyncPerSec)
	}

	writeTestFile(t, filepath.Join(rec, "cam04", "main", "2026-10-29", "130000_000000.mp4.age"), closed)
	if _, err := measureDisk(rec, id); err == nil || !strings.Contains(err.Error(), "130000_000000.mp4.age") {
		t.Errorf("an .age file that does not decrypt: %v, want its error", err)
	}
}

// TestDiskQueuePeak_SumsEachRecordersLargest (C1-R17): the disk writer's
// peak is the sum, over the recorders, of each one's largest line; other
// lines of ipcam-hub's log do not count.
func TestDiskQueuePeak_SumsEachRecordersLargest(t *testing.T) {
	log := strings.Join([]string{
		`mediamtx: 2026/10/26 03:55:59 INF [path cam01/main] [recorder] disk queue peak: 2425241 bytes`,
		`mediamtx: 2026/10/26 04:00:59 INF [path cam01/main] [recorder] disk queue peak: 3000000 bytes`,
		`mediamtx: 2026/10/26 04:00:59 INF [path cam02/main] [recorder] disk queue peak: 1000000 bytes`,
		`mediamtx: 2026/10/26 04:00:59 INF [path cam02/main] [recorder] disk queue peak: 900000 bytes` + "\r",
		`mediamtx: 2026/10/26 04:00:59 INF [path cam03/main] [recorder] segment closed`,
		`{"type":"segment_closed","path":"x"}`,
		`ipcam-hub: disk queue peak: 99999999 bytes`,
	}, "\n")
	got, err := diskQueuePeak(strings.NewReader(log))
	if err != nil || got != 4_000_000 {
		t.Errorf("diskQueuePeak = %d, %v; want 3,000,000 + 1,000,000", got, err)
	}
}

// TestDiskQueuePeak_HubPinsTheSameLine (C1-R17): ipcam-hub's own test runs
// the real fork and checks its disk queue peak lines against
// diskQueuePeakPattern; that pattern is this package's, character for
// character, so that the line MI3 parses is the line the fork prints.
func TestDiskQueuePeak_HubPinsTheSameLine(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "hub", "cmd", "ipcam-hub", "run_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout may end its lines with CRLF.
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	m := regexp.MustCompile("(?m)^const diskQueuePeakPattern = `([^`]*)`$").FindSubmatch(src)
	if m == nil {
		t.Fatal("hub/cmd/ipcam-hub/run_test.go has no diskQueuePeakPattern")
	}
	if string(m[1]) != peakLine.String() {
		t.Errorf("the hub pins %q, MI3 parses %q", m[1], peakLine.String())
	}
}

// TestDisk_TimescaleFromMediaMTXsInit: MediaMTX's init has a udta box in
// its moov, which mediacommon's Init does not read; the timescale comes
// from the track's mdhd, version 0 or 1.
func TestDisk_TimescaleFromMediaMTXsInit(t *testing.T) {
	box := func(typ string, body ...[]byte) []byte {
		b := []byte{0, 0, 0, 0}
		b = append(b, typ...)
		for _, p := range body {
			b = append(b, p...)
		}
		binary.BigEndian.PutUint32(b, uint32(len(b))) //nolint:gosec // a small box
		return b
	}
	mdhd0 := append([]byte{0, 0, 0, 0}, make([]byte, 8)...)
	mdhd0 = binary.BigEndian.AppendUint32(mdhd0, 90000)
	mdhd1 := append([]byte{1, 0, 0, 0}, make([]byte, 16)...)
	mdhd1 = binary.BigEndian.AppendUint32(mdhd1, 48000)
	for want, mdhd := range map[uint32][]byte{90000: mdhd0, 48000: mdhd1} {
		moov := box("moov", box("mvhd", make([]byte, 100)), box("trak", box("tkhd", make([]byte, 84)), box("mdia", box("mdhd", mdhd))),
			box("mvex"), box("udta", box("mtxi", []byte{1, 2, 3})))
		if got, err := mdhdTimescale(moov); err != nil || got != want {
			t.Errorf("mdhdTimescale = %d, %v; want %d", got, err, want)
		}
	}
	if _, err := mdhdTimescale(box("moov", box("udta"))); err == nil {
		t.Error("a moov without a track gave a timescale")
	}
}
