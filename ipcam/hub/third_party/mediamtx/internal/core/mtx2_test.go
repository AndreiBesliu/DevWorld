package core

// The core tests of patch MTX-2 (hub/third_party/PATCHES.md) about its
// configuration keys: a path with recordAgeRecipients records age-encrypted
// segments, and recordAgePadLowBitrate, true when absent (O17), decides
// whether a part smaller than one 64 KiB age chunk is padded to the chunk
// boundary. The recorder's own MTX-2 tests are in internal/recorder.

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/stretchr/testify/require"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/test"
)

// mtx2Chunk is the plaintext size of an age chunk.
const mtx2Chunk = 65536

// mtx2Record runs a core whose path cam01_main records 2 s segments of 1 s
// parts, encrypted to two new recipients, with the path keys in extra. An
// RTSP publisher sends 4 s of 256 kbps H.264: IDR frames of 1,600 bytes with
// the SPS and PPS, 50 ms apart in PTS, so that each part holds 32,000 bytes.
// It returns the plaintexts of the two segments, which the keyframes at 2 s
// and 4 s close.
func mtx2Record(t *testing.T, extra string) [][]byte {
	riG, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	rk, err := age.GenerateX25519Identity()
	require.NoError(t, err)

	recordDir := t.TempDir()
	recordPath := filepath.Join(recordDir, "%path", "%Y-%m-%d_%H-%M-%S-%f")
	segmentDir := filepath.Join(recordDir, "cam01_main")
	var files []string

	func() {
		// Only the RTSP server, as in TestMTX3_HookEnvironment.
		p, ok := newInstance(t, "logLevel: warn\n"+
			"rtmp: no\n"+
			"hls: no\n"+
			"webrtc: no\n"+
			"srt: no\n"+
			"moq: no\n"+
			"rtspTransports: [tcp]\n"+
			"rtspAddress: 127.0.0.1:8554\n"+
			"pathDefaults:\n"+
			"  record: yes\n"+
			"  recordPath: "+yamlSingleQuoted(recordPath)+"\n"+
			"  recordPartDuration: 1s\n"+
			"  recordSegmentDuration: 2s\n"+
			"paths:\n"+
			"  cam01_main:\n"+
			"    recordAgeRecipients: ["+riG.Recipient().String()+", "+rk.Recipient().String()+"]\n"+
			extra)
		require.True(t, ok)
		defer p.Close()

		media0 := test.UniqueMediaH264()
		forma := media0.Formats[0].(*format.H264)
		encoder, err := forma.CreateEncoder()
		require.NoError(t, err)

		tcp := gortsplib.ProtocolTCP
		source := gortsplib.Client{Protocol: &tcp}
		err = source.StartRecording("rtsp://127.0.0.1:8554/cam01_main",
			&description.Session{Medias: []*description.Media{media0}})
		require.NoError(t, err)
		defer source.Close()

		// The sample of a frame: the SPS, the PPS and the IDR, each with its
		// 4-byte AVCC length.
		idr := make([]byte, 1600-len(forma.SPS)-len(forma.PPS)-3*4)
		idr[0] = 0x65
		for frame := range 81 {
			binary.BigEndian.PutUint32(idr[1:], uint32(frame))
			packets, err := encoder.Encode([][]byte{forma.SPS, forma.PPS, idr})
			require.NoError(t, err)
			for _, pkt := range packets {
				pkt.Timestamp = 45343 + 4500*uint32(frame)
				require.NoError(t, source.WritePacketRTP(media0, pkt))
			}
			// 2.5 times faster than real time: the recorder resets when the
			// wall clock and the PTS drift apart by more than 5 s.
			time.Sleep(20 * time.Millisecond)
		}

		deadline := time.Now().Add(10 * time.Second)
		for {
			files, err = filepath.Glob(filepath.Join(segmentDir, "*.mp4.age"))
			require.NoError(t, err)
			if len(files) == 2 {
				break
			}
			require.True(t, time.Now().Before(deadline), "the two segments were not completed: %v", files)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	slices.Sort(files)
	plaintexts := make([][]byte, len(files))
	for i, path := range files {
		file, err := os.ReadFile(path)
		require.NoError(t, err)
		r, err := age.Decrypt(bytes.NewReader(file), riG)
		require.NoError(t, err)
		plaintexts[i], err = io.ReadAll(r)
		require.NoError(t, err)
	}
	return plaintexts
}

// mtx2Part is a part of an encrypted segment's plaintext: its moof and mdat
// span [start, end), and padEnd is the end of the free box that pads it, or
// end when there is none.
type mtx2Part struct{ start, end, padEnd int64 }

// mtx2Parts lists the parts of an encrypted segment's plaintext. A free box
// right after an mdat pads its part when it starts off a chunk boundary; the
// close's 8-byte free box starts on one.
func mtx2Parts(t *testing.T, plaintext []byte) []mtx2Part {
	t.Helper()
	var parts []mtx2Part
	for pos := int64(0); pos < int64(len(plaintext)); {
		require.GreaterOrEqual(t, int64(len(plaintext))-pos, int64(8), "truncated box at %d", pos)
		size := int64(binary.BigEndian.Uint32(plaintext[pos:]))
		typ := string(plaintext[pos+4 : pos+8])
		require.GreaterOrEqual(t, size, int64(8), "box %q at %d", typ, pos)
		switch {
		case typ == "mdat":
			require.NotEmpty(t, parts, "mdat at %d without a moof", pos)
			parts[len(parts)-1].end = pos + size
			parts[len(parts)-1].padEnd = pos + size
		case typ == "moof":
			parts = append(parts, mtx2Part{start: pos})
		case typ == "free" && pos%mtx2Chunk != 0:
			require.NotEmpty(t, parts, "free box at %d before any part", pos)
			require.Equal(t, pos, parts[len(parts)-1].end, "free box at %d not right after an mdat", pos)
			parts[len(parts)-1].padEnd = pos + size
		}
		pos += size
	}
	return parts
}

// TestMTX2_LowBitratePadding_DefaultOn: with recordAgePadLowBitrate absent, a
// 256 kbps stream has every part end on a chunk boundary.
func TestMTX2_LowBitratePadding_DefaultOn(t *testing.T) {
	for i, plaintext := range mtx2Record(t, "") {
		parts := mtx2Parts(t, plaintext)
		require.Len(t, parts, 2, "segment %d", i+1)
		for j, part := range parts {
			require.Zero(t, part.padEnd%mtx2Chunk, "segment %d, part %d: ends at %d", i+1, j+1, part.padEnd)
		}
	}
}

// TestMTX2_LowBitratePadding_Off: with recordAgePadLowBitrate false, the
// parts of a 256 kbps stream follow the 10 % rule only: a part is padded only
// when its padding is at most 10 % of it.
func TestMTX2_LowBitratePadding_Off(t *testing.T) {
	unpadded := 0
	for i, plaintext := range mtx2Record(t, "    recordAgePadLowBitrate: false\n") {
		parts := mtx2Parts(t, plaintext)
		require.Len(t, parts, 2, "segment %d", i+1)
		for j, part := range parts {
			p := (mtx2Chunk - part.end%mtx2Chunk) % mtx2Chunk
			if p != 0 && p < 8 {
				p += mtx2Chunk
			}
			want := part.end
			if p != 0 && 10*p <= part.end-part.start {
				want += p
			}
			require.Equal(t, want, part.padEnd, "segment %d, part %d: %d bytes ending at %d",
				i+1, j+1, part.end-part.start, part.end)
			if part.padEnd%mtx2Chunk != 0 {
				unpadded++
			}
		}
	}
	require.NotZero(t, unpadded, "parts left off a chunk boundary")
}

// TestMTX2_ReloadWithOtherRecipientsOrPaddingRecreatesPath: a configuration
// reload that changes a path's recipients or its padding setting must
// recreate the path, so that its recorder starts again with the new scope.
// doReloadConf, which reloads a path in place, restarts the recorder for the
// Record* keys that pathConfCanBeUpdated copies, and the two MTX-2 keys are
// not among them; this test fails if a rebase makes them hot-reloadable.
func TestMTX2_ReloadWithOtherRecipientsOrPaddingRecreatesPath(t *testing.T) {
	recipients := func() string {
		riG, err := age.GenerateX25519Identity()
		require.NoError(t, err)
		rk, err := age.GenerateX25519Identity()
		require.NoError(t, err)
		return "[" + riG.Recipient().String() + ", " + rk.Recipient().String() + "]"
	}
	load := func(extra string) *conf.Path {
		t.Helper()
		loaded, _, err := conf.Load(test.CreateTempFile(t, []byte("paths:\n"+
			"  cam1/main:\n"+
			"    source: rtsp://127.0.0.1:8554/cam1/main\n"+
			"    record: yes\n"+
			"    recordPath: /rec/%path/%Y-%m-%d/%H%M%S_%f\n"+extra)), nil, nil)
		require.NoError(t, err)
		return loaded.Paths["cam1/main"]
	}
	pair := recipients()
	base := load("    recordAgeRecipients: " + pair + "\n")

	require.True(t, pathConfCanBeUpdated(base, load("    recordAgeRecipients: "+pair+"\n")),
		"the same configuration must reload in place")
	require.True(t, pathConfCanBeUpdated(base, load("    recordAgeRecipients: "+pair+"\n    recordSegmentDuration: 10s\n")),
		"a Record* key that upstream reloads in place")
	for name, extra := range map[string]string{
		"other recipients": "    recordAgeRecipients: " + recipients() + "\n",
		"padding off":      "    recordAgeRecipients: " + pair + "\n    recordAgePadLowBitrate: false\n",
		"no recipients":    "",
	} {
		require.False(t, pathConfCanBeUpdated(base, load(extra)), "%s: the path is reloaded in place", name)
	}
}
