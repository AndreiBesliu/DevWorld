package recorder

// The tests of patch MTX-2 (hub/third_party/PATCHES.md): an fMP4 segment with
// age recipients is written strictly in order through crypto.Writer, as
// <name>.mp4.age.part, renamed to <name>.mp4.age; each part's end is padded to
// the 64 KiB chunk boundary with a free box when §4.5's rule (O17) says so,
// and its complete chunks are sealed before its fsync; the final chunk is
// never empty. They run a real Recorder with the seams of mtx1_test.go and
// decrypt what it wrote. TestMTX2_LowBitratePadding_DefaultOn and _Off are in
// internal/core, since they are about the configuration key.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	rtspformat "github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/stretchr/testify/require"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/test"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/unit"
)

// mtx2PPS is a PPS of 16 bytes. test.FormatH264's has 4, which HUB-04's search
// in the ciphertext would find by chance (ruling C1-R10).
var mtx2PPS = []byte{
	0x68, 0xce, 0x3c, 0x80, 0x4d, 0x54, 0x58, 0x32,
	0x2d, 0x50, 0x50, 0x53, 0x9a, 0x7c, 0x31, 0xe5,
}

// mtx2Format is test.FormatH264 with mtx2PPS, so that every NAL unit of an
// MTX-2 test stream is at least 16 bytes long.
var mtx2Format = &rtspformat.H264{
	PayloadTyp:        96,
	SPS:               test.FormatH264.SPS,
	PPS:               mtx2PPS,
	PacketizationMode: 1,
}

// mtx2FrameOverhead is the size of a frame's sample besides its IDR: the SPS,
// the PPS and the three 4-byte AVCC lengths.
var mtx2FrameOverhead = len(mtx2Format.SPS) + len(mtx2Format.PPS) + 3*4

// mtx2Start is the wall time of an MTX-2 test stream's first frame. Segment i
// of the stream (3 s each) is mtx2SegmentName(i).
var mtx2Start = time.Date(2008, 5, 20, 22, 15, 25, 0, time.UTC)

func mtx2SegmentName(i int) string {
	return fmt.Sprintf("2008-05-20_22-15-%02d-000000.mp4.age", 25+3*i)
}

// framesPerPart is the number of frames in a part: 1 s of frames 50 ms apart.
const framesPerPart = int(testPartDuration / testFrameInterval)

// ageKeys are a recording's two identities and their KeyScope.
type ageKeys struct {
	riG, rk *age.X25519Identity
	scope   crypto.KeyScope
}

func newAgeKeys(t *testing.T) ageKeys {
	t.Helper()
	riG, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	rk, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	scope, err := crypto.ParseKeyScope(riG.Recipient().String(), rk.Recipient().String())
	require.NoError(t, err)
	return ageKeys{riG: riG, rk: rk, scope: scope}
}

// decrypt returns the plaintext of an encrypted segment, read with age.
func (k ageKeys) decrypt(t *testing.T, file []byte) []byte {
	t.Helper()
	r, err := age.Decrypt(bytes.NewReader(file), k.riG)
	require.NoError(t, err)
	plaintext, err := io.ReadAll(r)
	require.NoError(t, err)
	return plaintext
}

// startAgeRecording is startRecording for mtx2Format, encrypted to keys, with
// the low-bitrate padding setting pad (O17).
func startAgeRecording(t *testing.T, keys ageKeys, pad bool, configure ...func(*Recorder)) *testRecording {
	return startRecordingWith(t, mtx2Start, mtx2Format, func(r *Recorder) {
		r.AgeScope = &keys.scope
		r.AgePadLowBitrate = pad
		for _, c := range configure {
			c(r)
		}
	})
}

// mtx2IDR returns an IDR NAL unit of size bytes (at least 16), whose first 16
// bytes are unique to the frame.
func mtx2IDR(frame, size int) []byte {
	nalu := make([]byte, size)
	copy(nalu, []byte{0x65, 'M', 'T', 'X', '2', '-', 'I', 'D', 'R'})
	binary.BigEndian.PutUint32(nalu[9:], uint32(frame))
	for i := 13; i < size; i++ {
		nalu[i] = byte(i*7 + frame)
	}
	return nalu
}

// writeAgeFrame sends the next frame, an IDR of idrSize bytes with mtx2Format's
// SPS and PPS, and returns its IDR.
func (r *testRecording) writeAgeFrame(idrSize int) []byte {
	idr := mtx2IDR(r.frames, idrSize)
	r.writeFrame(unit.PayloadH264{mtx2Format.SPS, mtx2Format.PPS, idr})
	return idr
}

// writeAgePart sends the frames of one part, whose samples hold payload bytes
// in all: the part, its moof and mdat, is a few hundred bytes longer.
func (r *testRecording) writeAgePart(payload int) {
	frame := payload / framesPerPart
	for i := range framesPerPart {
		size := frame
		if i == framesPerPart-1 {
			size += payload - frame*framesPerPart
		}
		r.writeAgeFrame(size - mtx2FrameOverhead)
	}
}

// writeAgeParts sends one part per payload. Each 3 parts make a segment.
func (r *testRecording) writeAgeParts(payloads ...int) {
	for _, p := range payloads {
		r.writeAgePart(p)
	}
}

// closeSegment sends one small frame. It closes the segment that the frames
// before it end, and stays in the recorder as the next segment's first
// sample, so the next segment never creates a file.
func (r *testRecording) closeSegment() {
	r.writeAgeFrame(100)
}

// waitForSegments waits until n segments are complete and stops the recorder.
func (r *testRecording) waitForSegments(n int) {
	r.t.Helper()
	r.waitFor(fmt.Sprintf("%d segments", n), func(l *durabilityLog) bool { return l.count("complete") == n })
	r.stop()
}

// readSegment returns the bytes of the final file of segment i.
func (r *testRecording) readSegment(i int) []byte {
	r.t.Helper()
	file, err := os.ReadFile(filepath.Join(r.dir, mtx2SegmentName(i)))
	require.NoError(r.t, err)
	return file
}

// randomPayloads returns n part payloads of nominal bytes ± 30 %, uniformly,
// from a seeded generator.
func randomPayloads(seed uint64, n, nominal int) []int {
	rng := rand.New(rand.NewPCG(seed, 0))
	out := make([]int, n)
	for i := range out {
		out[i] = int(float64(nominal) * (0.7 + 0.6*rng.Float64()))
	}
	return out
}

// mp4Box is a top-level box of an fMP4 plaintext.
type mp4Box struct {
	typ        string
	start, end int64
}

// topBoxes splits an fMP4 plaintext into its top-level boxes.
func topBoxes(t *testing.T, plaintext []byte) []mp4Box {
	t.Helper()
	var boxes []mp4Box
	for pos := int64(0); pos < int64(len(plaintext)); {
		require.GreaterOrEqual(t, int64(len(plaintext))-pos, int64(8), "truncated box header at %d", pos)
		size := int64(binary.BigEndian.Uint32(plaintext[pos:]))
		typ := string(plaintext[pos+4 : pos+8])
		if size == 1 {
			require.GreaterOrEqual(t, int64(len(plaintext))-pos, int64(16), "truncated box size at %d", pos)
			size = int64(binary.BigEndian.Uint64(plaintext[pos+8:]))
		}
		require.GreaterOrEqual(t, size, int64(8), "box %q at %d has size %d", typ, pos, size)
		require.LessOrEqual(t, pos+size, int64(len(plaintext)), "box %q at %d runs past the end", typ, pos)
		boxes = append(boxes, mp4Box{typ: typ, start: pos, end: pos + size})
		pos += size
	}
	return boxes
}

// agePart is one part of an encrypted segment's plaintext.
type agePart struct {
	start, end int64 // its moof and mdat
	padEnd     int64 // the end of the free box after it, or end when there is none
}

func (p agePart) size() int64    { return p.end - p.start }
func (p agePart) padding() int64 { return p.padEnd - p.end }

// ageLayout is the plaintext of an encrypted segment: the init, then the
// parts, each padded or not, and the close's 8-byte free box when there is one.
type ageLayout struct {
	parts     []agePart
	closeFree bool
}

// parseAgeLayout reads the layout of an encrypted segment's plaintext. A free
// box right after a part's mdat is its padding when it starts off a chunk
// boundary, since padding ends on one; the close's free box starts on one.
func parseAgeLayout(t *testing.T, plaintext []byte) ageLayout {
	t.Helper()
	boxes := topBoxes(t, plaintext)
	require.GreaterOrEqual(t, len(boxes), 2)
	require.Equal(t, "ftyp", boxes[0].typ)
	require.Equal(t, "moov", boxes[1].typ)

	var l ageLayout
	for i := 2; i < len(boxes); {
		b := boxes[i]
		switch {
		case b.typ == "moof":
			require.Less(t, i+1, len(boxes), "moof at %d without an mdat", b.start)
			require.Equal(t, "mdat", boxes[i+1].typ, "box after the moof at %d", b.start)
			part := agePart{start: b.start, end: boxes[i+1].end, padEnd: boxes[i+1].end}
			i += 2
			if i < len(boxes) && boxes[i].typ == "free" && boxes[i].start%crypto.ChunkSize != 0 {
				part.padEnd = boxes[i].end
				i++
			}
			l.parts = append(l.parts, part)

		case b.typ == "free" && i == len(boxes)-1 && b.end-b.start == 8 && b.start%crypto.ChunkSize == 0:
			l.closeFree = true
			i++

		default:
			t.Fatalf("unexpected box %q at %d", b.typ, b.start)
		}
	}
	return l
}

// wantPadding is §4.5's rule as amended by O17, for a part of size bytes (its
// moof and mdat) that ends at the plaintext offset end: p is the distance to
// the next 64 KiB boundary, one more chunk when p is under 8 bytes (a free
// box's header); the part is padded with p bytes when the setting pad is on
// and the part is under 64 KiB, or when p is at most 10 % of the part.
func wantPadding(end, size int64, pad bool) int64 {
	p := (65536 - end%65536) % 65536
	if p == 0 {
		return 0
	}
	if p < 8 {
		p += 65536
	}
	if (pad && size < 65536) || 10*p <= size {
		return p
	}
	return 0
}

// requirePaddingRule checks every part of a segment against wantPadding, and
// that a padded part ends on a chunk boundary.
func requirePaddingRule(t *testing.T, l ageLayout, pad bool) {
	t.Helper()
	for i, part := range l.parts {
		want := wantPadding(part.end, part.size(), pad)
		require.Equal(t, want, part.padding(), "part %d: %d bytes ending at %d", i+1, part.size(), part.end)
		if want != 0 {
			require.Zero(t, part.padEnd%crypto.ChunkSize, "part %d: padded end", i+1)
		}
	}
}

// ageHeaderLen returns the length of an age file's header, up to and with the
// line break after its MAC.
func ageHeaderLen(t *testing.T, file []byte) int {
	t.Helper()
	mac := bytes.Index(file, []byte("\n--- "))
	require.GreaterOrEqual(t, mac, 0, "no age header MAC")
	end := bytes.IndexByte(file[mac+1:], '\n')
	require.GreaterOrEqual(t, end, 0, "no line break after the age header MAC")
	return mac + 1 + end + 1
}

// sealedSize is the size of an age file whose first plaintext bytes, rounded
// down to whole chunks, are sealed: the header, the 16-byte payload nonce and
// 65,552 bytes per chunk.
func sealedSize(headerLen int, plaintext int64) int64 {
	return int64(headerLen) + 16 + plaintext/crypto.ChunkSize*(crypto.ChunkSize+16)
}

// countMoofMfhd counts the offsets where "moof" is followed, 4 bytes later, by
// "mfhd": the start of a part's moof.
func countMoofMfhd(b []byte) int {
	n := 0
	for i := 0; i+12 <= len(b); i++ {
		if string(b[i:i+4]) == "moof" && string(b[i+8:i+12]) == "mfhd" {
			n++
		}
	}
	return n
}

// openRecording returns the plaintext of an encrypted segment, read through
// crypto.Recording with the identity id.
func openRecording(t *testing.T, id *age.X25519Identity, file []byte) []byte {
	t.Helper()
	plain, size, err := crypto.NewRecording(id).OpenReaderAt(context.Background(), bytes.NewReader(file), int64(len(file)))
	require.NoError(t, err)
	plaintext := make([]byte, size)
	n, err := plain.ReadAt(plaintext, 0)
	if !errors.Is(err, io.EOF) {
		require.NoError(t, err)
	}
	require.Equal(t, size, int64(n))
	return plaintext
}

// TestMTX2_EncryptedSegmentHasNoClearFrames is HUB-04: neither the final file
// of an encrypted segment nor the .part at any of its fsyncs holds the ftyp
// and its brand, a moof followed by its mfhd, or the first 16 bytes of an SPS,
// a PPS or an IDR of the stream; the file decrypts with crypto.Recording, with
// RI_g and with RK.
func TestMTX2_EncryptedSegmentHasNoClearFrames(t *testing.T) {
	keys := newAgeKeys(t)
	r := startAgeRecording(t, keys, true)
	var idrs [][]byte
	for range oneSegmentFrames - 1 {
		idrs = append(idrs, r.writeAgeFrame(2000))
	}
	r.writeAgeFrame(2000) // the next segment's first frame
	r.waitForSegments(1)

	file := r.readSegment(0)
	plaintext := openRecording(t, keys.riG, file)
	require.True(t, bytes.Equal(plaintext, openRecording(t, keys.rk, file)), "RK reads another plaintext")

	needles := map[string][]byte{
		"ftyp and brand": plaintext[4:12],
		"SPS":            mtx2Format.SPS[:16],
		"PPS":            mtx2Format.PPS[:16],
	}
	require.Equal(t, "ftyp", string(needles["ftyp and brand"][:4]))
	for i, idr := range idrs {
		needles[fmt.Sprintf("IDR %d", i)] = idr[:16]
	}

	// The needles are in the plaintext, so that their absence below means
	// something.
	for name, needle := range needles {
		require.True(t, bytes.Contains(plaintext, needle), "the plaintext lacks the %s", name)
	}
	require.Equal(t, 3, countMoofMfhd(plaintext), "moofs in the plaintext")

	snapshots := [][]byte{file}
	for _, ev := range eventsOf(r.log.snapshot(), "syncFile") {
		snapshots = append(snapshots, ev.content)
	}
	for i, b := range snapshots {
		for name, needle := range needles {
			require.Zero(t, bytes.Count(b, needle), "the %s, in file state %d of %d", name, i, len(snapshots))
		}
		require.Zero(t, countMoofMfhd(b), "moof and mfhd, in file state %d of %d", i, len(snapshots))
	}
}

// TestMTX2_PartBoundaryChunkAligned_WhenPaddingAtMost10Pct: in a 4 Mbps
// stream (parts of about 500 KB), a part is padded exactly when its padding
// is at most 10 % of it, and a padded part ends on a chunk boundary. The
// second segment's parts are sized so that one needs about 20,000 bytes of
// padding and is padded, and the next about 60,000 bytes, over 10 %, and is
// not; a small part before them puts them on a chunk boundary.
func TestMTX2_PartBoundaryChunkAligned_WhenPaddingAtMost10Pct(t *testing.T) {
	keys := newAgeKeys(t)
	r := startAgeRecording(t, keys, true)
	r.writeAgeParts(randomPayloads(4, 3, 500_000)...)
	// The part's moof and mdat headers add a few hundred bytes, well inside
	// the margins of the two cases.
	r.writeAgeParts(30_000, 8*crypto.ChunkSize-20_000, 7*crypto.ChunkSize-60_000)
	r.closeSegment()
	r.waitForSegments(2)

	padded, overTenPct := 0, 0
	for i := range 2 {
		l := parseAgeLayout(t, keys.decrypt(t, r.readSegment(i)))
		require.Len(t, l.parts, 3, "segment %d", i+1)
		requirePaddingRule(t, l, true)
		for _, part := range l.parts {
			if part.size() < 65536 {
				continue
			}
			p := (65536 - part.end%65536) % 65536
			switch {
			case part.padding() != 0:
				padded++
			case 10*p > part.size():
				overTenPct++
			}
		}
	}
	require.NotZero(t, padded, "4 Mbps parts that were padded")
	require.NotZero(t, overTenPct, "4 Mbps parts left unpadded, needing over 10 %")
}

// TestMTX2_LowBitrateDecidedPerPart: the low-bitrate setting acts per part
// (§4.5 as amended by O17).
func TestMTX2_LowBitrateDecidedPerPart(t *testing.T) {
	// HUB-07's boundary: a 63 KiB part and a 65 KiB part, each starting 30,000
	// bytes after a chunk boundary, so that neither needs 10 % or less.
	t.Run("63 and 65 KiB", func(t *testing.T) {
		keys := newAgeKeys(t)
		for _, tc := range []struct {
			name    string
			pad     bool
			size    int64
			aligned bool
		}{
			{"setting on, 63 KiB", true, 63 << 10, true},
			{"setting on, 65 KiB", true, 65 << 10, false},
			{"setting off, 63 KiB", false, 63 << 10, false},
			{"setting off, 65 KiB", false, 65 << 10, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var file bytes.Buffer
				w, err := newAgeSegmentWriter(&file, keys.scope, tc.pad)
				require.NoError(t, err)
				_, err = w.Write(make([]byte, 30_000)) // stands for the init
				require.NoError(t, err)
				start := w.size()
				_, err = w.Write(bytes.Repeat([]byte{0xa5}, int(tc.size)))
				require.NoError(t, err)
				require.NoError(t, w.endPart(start))
				require.NoError(t, w.close())

				plaintext := keys.decrypt(t, file.Bytes())
				end := 30_000 + tc.size
				if !tc.aligned {
					require.Equal(t, end, int64(len(plaintext)), "plaintext size")
					return
				}
				// The part, its free box up to the boundary at 131,072, and the
				// close's 8-byte free box.
				require.Equal(t, int64(2*65536+8), int64(len(plaintext)), "plaintext size")
				require.Equal(t, uint32(2*65536-end), binary.BigEndian.Uint32(plaintext[end:]), "padding size")
				require.Equal(t, "free", string(plaintext[end+4:end+8]))
				require.Equal(t, []byte{0, 0, 0, 8, 'f', 'r', 'e', 'e'}, plaintext[2*65536:])
			})
		}
	})

	// A 450 kbps stream, ± 30 %: parts of 39 to 73 KB, on both sides of 64 KiB.
	t.Run("450 kbps", func(t *testing.T) {
		keys := newAgeKeys(t)
		r := startAgeRecording(t, keys, true)
		r.writeAgeParts(randomPayloads(450, 12, 450_000/8)...)
		r.closeSegment()
		r.waitForSegments(4)

		small, largeUnaligned := 0, 0
		for i := range 4 {
			l := parseAgeLayout(t, keys.decrypt(t, r.readSegment(i)))
			require.Len(t, l.parts, 3, "segment %d", i+1)
			requirePaddingRule(t, l, true)
			for _, part := range l.parts {
				if part.size() < 65536 {
					small++
					require.Zero(t, part.padEnd%crypto.ChunkSize, "a part of %d bytes ends off a chunk boundary", part.size())
				} else if part.padEnd%crypto.ChunkSize != 0 {
					largeUnaligned++
				}
			}
		}
		require.NotZero(t, small, "parts under 64 KiB")
		require.NotZero(t, largeUnaligned, "parts of 64 KiB or more, not aligned")
	})

	// At 4 Mbps every part is over 64 KiB, so only the 10 % rule applies.
	t.Run("4 Mbps", func(t *testing.T) {
		keys := newAgeKeys(t)
		r := startAgeRecording(t, keys, true)
		r.writeAgeParts(randomPayloads(4000, 3, 500_000)...)
		r.closeSegment()
		r.waitForSegments(1)

		l := parseAgeLayout(t, keys.decrypt(t, r.readSegment(0)))
		require.Len(t, l.parts, 3)
		requirePaddingRule(t, l, false)
	})
}

// TestMTX2_PaddingTooShortForAFreeBoxTakesAChunkMore: a part that ends 1 to 7
// bytes before a chunk boundary, too few for a free box's header, is padded
// to the boundary after it, 65,536 bytes more, when the rules pad it at all:
// a small part with the setting on, or a part of 640 KiB or more by the 10 %
// rule.
func TestMTX2_PaddingTooShortForAFreeBoxTakesAChunkMore(t *testing.T) {
	keys := newAgeKeys(t)
	const start = 30_000
	for _, tc := range []struct {
		name   string
		pad    bool
		chunks int64 // the part ends 4 bytes before chunk boundary number chunks
		padded bool
	}{
		{"small part, setting on", true, 1, true},
		{"small part, setting off", false, 1, false},
		{"under 10 %", false, 11, true},
		{"over 10 %", true, 10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var file bytes.Buffer
			w, err := newAgeSegmentWriter(&file, keys.scope, tc.pad)
			require.NoError(t, err)
			_, err = w.Write(make([]byte, start)) // stands for the init
			require.NoError(t, err)
			end := tc.chunks*65536 - 4
			_, err = w.Write(bytes.Repeat([]byte{0x5a}, int(end-start)))
			require.NoError(t, err)
			require.NoError(t, w.endPart(start))
			require.NoError(t, w.close())

			plaintext := keys.decrypt(t, file.Bytes())
			if !tc.padded {
				require.Equal(t, end, int64(len(plaintext)), "plaintext size")
				return
			}
			// The part, a free box of 4 + 65,536 bytes, and the close's.
			require.Equal(t, end+65540+8, int64(len(plaintext)), "plaintext size")
			require.Equal(t, uint32(65540), binary.BigEndian.Uint32(plaintext[end:]), "padding size")
			require.Equal(t, "free", string(plaintext[end+4:end+8]))
			require.Equal(t, []byte{0, 0, 0, 8, 'f', 'r', 'e', 'e'}, plaintext[end+65540:])
		})
	}
}

// TestMTX2_FinalChunkSyncedBeforeRename: the close writes the final age chunk,
// after the 8-byte free box when nothing is held, before the segment's last
// syncFile, on the open file; then Close, renameFinal, syncDir and the hook.
func TestMTX2_FinalChunkSyncedBeforeRename(t *testing.T) {
	for _, tc := range []struct {
		name      string
		pad       bool
		closeFree bool
	}{
		// Every 256 kbps part is padded to a chunk boundary.
		{"with the free box", true, true},
		// Without padding, the last part ends inside a chunk.
		{"without the free box", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := newAgeKeys(t)
			r := startAgeRecording(t, keys, tc.pad)
			r.writeAgeParts(32_000, 32_000, 32_000)
			r.closeSegment()
			r.waitForSegments(1)

			file := r.readSegment(0)
			events := r.log.snapshot()
			rename := slices.IndexFunc(events, func(ev durabilityEvent) bool { return ev.op == "rename" })
			require.GreaterOrEqual(t, rename, 1, "events: %v", eventOps(events))
			require.Equal(t, []string{"syncFile", "rename", "syncDir", "complete"}, eventOps(events[rename-1:]))

			lastSync, ren := events[rename-1], events[rename]
			require.True(t, lastSync.fileOpen, "the last syncFile ran after Close")
			require.False(t, ren.fileOpen, "renameFinal ran before Close")
			require.True(t, bytes.Equal(file, lastSync.content),
				"the file changed after its last syncFile (%d bytes then, %d at the end)", len(lastSync.content), len(file))

			// What was synced is a complete age file: age authenticates its final chunk.
			l := parseAgeLayout(t, keys.decrypt(t, lastSync.content))
			require.Len(t, l.parts, 3)
			require.Equal(t, tc.closeFree, l.closeFree, "the close's free box")
		})
	}
}

// TestMTX2_FlushAtEachPartFsync: at each part's syncFile, the file holds the
// header, the payload nonce and every complete chunk of the plaintext so far,
// and nothing more: len(header) + 16 + n·65552, n = ⌊plaintext ÷ 65536⌋.
func TestMTX2_FlushAtEachPartFsync(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pad      bool
		payloads []int
	}{
		{"256 kbps, padding on", true, []int{32_000, 32_000, 32_000}},
		{"450 kbps, padding off", false, randomPayloads(4501, 3, 450_000/8)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := newAgeKeys(t)
			r := startAgeRecording(t, keys, tc.pad)
			r.writeAgeParts(tc.payloads...)
			r.closeSegment()
			r.waitForSegments(1)

			file := r.readSegment(0)
			headerLen := ageHeaderLen(t, file)
			l := parseAgeLayout(t, keys.decrypt(t, file))
			syncs := eventsOf(r.log.snapshot(), "syncFile")
			require.Len(t, l.parts, 3)
			require.Len(t, syncs, len(l.parts)+1, "a syncFile per part and one at the close")

			for i, part := range l.parts {
				content := syncs[i].content
				require.Equal(t, sealedSize(headerLen, part.padEnd), int64(len(content)),
					"file size at part %d's syncFile, with %d bytes of plaintext", i+1, part.padEnd)
				require.True(t, bytes.HasPrefix(file, content), "the file at part %d's syncFile is not a prefix of the final file", i+1)
			}
			require.Equal(t, len(file), len(syncs[len(syncs)-1].content), "file size at the close's syncFile")
		})
	}
}

// appendOnlyFile is a segment file that only appends: Read, Seek, WriteAt and
// Truncate fail and are recorded.
type appendOnlyFile struct {
	f *os.File

	mu      sync.Mutex
	refused []string
}

func (a *appendOnlyFile) refuse(op string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refused = append(a.refused, op)
	return fmt.Errorf("append-only file: %s refused", op)
}

func (a *appendOnlyFile) Write(p []byte) (int, error) { return a.f.Write(p) }
func (a *appendOnlyFile) Name() string                { return a.f.Name() }
func (a *appendOnlyFile) Sync() error                 { return a.f.Sync() }
func (a *appendOnlyFile) Close() error                { return a.f.Close() }

func (a *appendOnlyFile) Read([]byte) (int, error)           { return 0, a.refuse("Read") }
func (a *appendOnlyFile) Seek(int64, int) (int64, error)     { return 0, a.refuse("Seek") }
func (a *appendOnlyFile) WriteAt([]byte, int64) (int, error) { return 0, a.refuse("WriteAt") }
func (a *appendOnlyFile) Truncate(int64) error               { return a.refuse("Truncate") }

// useAppendOnlyFiles makes the recorder create appendOnlyFiles for the rest of
// the test, and returns a function that lists the calls they refused.
func useAppendOnlyFiles(t *testing.T) func() []string {
	var mu sync.Mutex
	var files []*appendOnlyFile

	realCreateFile := createFile
	t.Cleanup(func() { createFile = realCreateFile })
	createFile = func(name string) (segmentFile, error) {
		f, err := os.Create(name)
		if err != nil {
			return nil, err
		}
		a := &appendOnlyFile{f: f}
		mu.Lock()
		files = append(files, a)
		mu.Unlock()
		return a, nil
	}

	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		var refused []string
		for _, a := range files {
			a.mu.Lock()
			refused = append(refused, a.refused...)
			a.mu.Unlock()
		}
		return refused
	}
}

// TestMTX2_NoSeekBack is C13 and HUB-30's "strict secvențial": an encrypted
// segment is written, closed and renamed through a file that only appends,
// both when the next keyframe closes it and when the recorder stops; a clear
// segment, whose mvhd duration is written back at the close, does seek.
func TestMTX2_NoSeekBack(t *testing.T) {
	t.Run("encrypted", func(t *testing.T) {
		refused := useAppendOnlyFiles(t)
		keys := newAgeKeys(t)
		r := startAgeRecording(t, keys, true)
		// The first segment is closed by the next keyframe, the second, once
		// its first part is written, by the stop.
		r.writeAgeParts(32_000, 32_000, 32_000, 32_000, 32_000)
		r.waitFor("the second segment's first part", func(l *durabilityLog) bool {
			return l.count("complete") == 1 && l.count("syncFile") >= 5
		})
		r.stop()

		require.Empty(t, refused())
		require.Equal(t, []string{mtx2SegmentName(0), mtx2SegmentName(1)}, dirEntries(t, r.dir))
		for i := range 2 {
			l := parseAgeLayout(t, keys.decrypt(t, r.readSegment(i)))
			require.NotEmpty(t, l.parts, "segment %d", i+1)
		}
	})

	// The control: the wrapper does catch a seek-back.
	t.Run("clear", func(t *testing.T) {
		refused := useAppendOnlyFiles(t)
		r := startRecording(t, mtx2Start)
		r.writeFrames(oneSegmentFrames)
		r.waitFor("the segment's close", func(l *durabilityLog) bool { return len(refused()) != 0 })
		r.stop()

		require.Contains(t, refused(), "Seek")
	})
}

// TestMTX2_FinalChunkNeverEmpty: a segment whose last part ends on a chunk
// boundary, as every padded part does, closes with an 8-byte free box in a
// final chunk of its own, and age decrypts it.
func TestMTX2_FinalChunkNeverEmpty(t *testing.T) {
	keys := newAgeKeys(t)
	r := startAgeRecording(t, keys, true)
	r.writeAgeParts(32_000, 32_000, 32_000)
	r.closeSegment()
	r.waitForSegments(1)

	file := r.readSegment(0)
	plaintext := keys.decrypt(t, file)
	l := parseAgeLayout(t, plaintext)
	require.Len(t, l.parts, 3)
	last := l.parts[len(l.parts)-1]
	require.Zero(t, last.padEnd%crypto.ChunkSize, "the last part ends off a chunk boundary")
	require.True(t, l.closeFree, "no free box after the last part")
	require.Equal(t, []byte{0, 0, 0, 8, 'f', 'r', 'e', 'e'}, plaintext[last.padEnd:])
	// Whole chunks up to the last part's end, then a final chunk of 8 bytes.
	require.Equal(t, sealedSize(ageHeaderLen(t, file), last.padEnd)+8+16, int64(len(file)))
}

// TestMTX2_EncryptedNamingPartThenRename: while an encrypted segment is
// written only <name>.mp4.age.part exists, and after its close only
// <name>.mp4.age, which the hooks name.
func TestMTX2_EncryptedNamingPartThenRename(t *testing.T) {
	keys := newAgeKeys(t)
	r := startAgeRecording(t, keys, true)
	r.writeAgeParts(32_000, 32_000, 32_000)
	r.closeSegment()
	r.waitForSegments(1)

	events := r.log.snapshot()
	name := mtx2SegmentName(0)
	final := filepath.Join(r.dir, name)

	creates := eventsOf(events, "create")
	require.Len(t, creates, 1)
	require.Equal(t, final+".part", creates[0].path, "path given to OnSegmentCreate")
	syncs := eventsOf(events, "syncFile")
	require.Len(t, syncs, 4)
	for i, ev := range syncs {
		require.Equal(t, final+".part", ev.path, "syncFile %d", i+1)
		require.Equal(t, []string{name + ".part"}, ev.entries, "directory at syncFile %d", i+1)
	}
	renames := eventsOf(events, "rename")
	require.Len(t, renames, 1)
	require.Equal(t, final+".part", renames[0].from)
	require.Equal(t, final, renames[0].path)
	completes := eventsOf(events, "complete")
	require.Len(t, completes, 1)
	require.Equal(t, final, completes[0].path, "path given to OnSegmentComplete")
	require.Equal(t, []string{name}, dirEntries(t, r.dir), "directory after the close")
}

// TestMTX2_RestartedRecorderStillEncrypts: after a failed part fsync restarts
// the recorder, the next segment is encrypted too (HUB-04).
func TestMTX2_RestartedRecorderStillEncrypts(t *testing.T) {
	keys := newAgeKeys(t)
	r := startAgeRecording(t, keys, true, func(rec *Recorder) { rec.restartPause = 10 * time.Millisecond })

	recorded := syncFile
	calls := 0
	syncFile = func(f segmentFile) error {
		err := recorded(f)
		calls++
		if calls == 1 {
			return errors.New("injected fsync failure")
		}
		return err
	}

	// Frames go on until the restarted recorder has created its segment.
	deadline := time.Now().Add(10 * time.Second)
	for r.log.count("create") < 2 {
		require.True(t, time.Now().Before(deadline), "no segment after the restart; events: %v", eventOps(r.log.snapshot()))
		r.writeAgeFrame(1600)
		time.Sleep(time.Millisecond)
	}
	r.stop()

	creates := eventsOf(r.log.snapshot(), "create")
	for i, ev := range creates {
		require.True(t, strings.HasSuffix(ev.path, ".mp4.age.part"), "segment %d: %s", i+1, ev.path)
	}
	for _, name := range dirEntries(t, r.dir) {
		require.True(t, strings.HasSuffix(name, ".mp4.age") || strings.HasSuffix(name, ".mp4.age.part"), name)
	}
}
