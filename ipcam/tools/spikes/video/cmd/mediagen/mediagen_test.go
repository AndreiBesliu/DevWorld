package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/band"
)

// requireFFmpeg skips the test when ffmpeg or ffprobe is missing. CI sets
// MEDIAGEN_REQUIRE_FFMPEG=1 after installing them, so that a broken install
// fails the job instead of silently skipping the test.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("MEDIAGEN_REQUIRE_FFMPEG") == "1" {
				t.Fatalf("%s not found but MEDIAGEN_REQUIRE_FFMPEG=1: %v", tool, err)
			}
			t.Skipf("%s not found: %v", tool, err)
		}
	}
}

// manifestDoc pins the JSON field names that other tools read from manifest.json.
type manifestDoc struct {
	Streams []struct {
		Name       string `json:"name"`
		File       string `json:"file"`
		Camera     int    `json:"camera"`
		Codec      string `json:"codec"`
		Profile    string `json:"profile"`
		Level      string `json:"level"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		FPS        int    `json:"fps"`
		GOP        int    `json:"gop"`
		BitrateBPS int    `json:"bitrate_bps"`
		Frames     int    `json:"frames"`
		HasBFrames int    `json:"has_b_frames"`
	} `json:"streams"`
	HLS struct {
		Playlist       string  `json:"playlist"`
		SegmentSeconds int     `json:"segment_seconds"`
		Segments       int     `json:"segments"`
		DurationSecond float64 `json:"duration_seconds"`
	} `json:"hls"`
}

// The test generates all media into a temporary directory, which takes a few
// minutes. With MEDIAGEN_DIR set it checks the media already in that directory
// instead, so that CI can test the very files it uploads.
func TestMediagen_ManifestMatchesSpec(t *testing.T) {
	requireFFmpeg(t)
	out := os.Getenv("MEDIAGEN_DIR")
	if out == "" {
		out = t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
		defer cancel()
		if err := generate(ctx, out); err != nil {
			t.Fatalf("generate: %v", err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(out, "manifest.json")) //nolint:gosec // out is a test directory
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifestDoc
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	// Global Constraints, "Simulated media". Camera is the media file id (R1).
	want := []struct {
		name, file, codec, profile, level string
		camera, width, height, fps        int
		bitrate, frames                   int
	}{
		{"sub", "sub.h264", "h264", "Main", "3.1", 1, 640, 360, 12, 512_000, 720},
		{"main", "main.h264", "h264", "Main", "4.0", 2, 1920, 1080, 20, 4_000_000, 1200},
		{"main-high51", "main-high51.h264", "h264", "High", "5.1", 3, 3840, 2160, 20, 8_000_000, 400},
		{"main-h265", "main-h265.h265", "hevc", "Main", "4.1", 4, 1920, 1080, 20, 2_000_000, 1200},
	}
	if len(m.Streams) != len(want) {
		t.Fatalf("manifest has %d streams, want %d", len(m.Streams), len(want))
	}
	for i, w := range want {
		got := m.Streams[i]
		t.Run(w.name, func(t *testing.T) {
			if got.Name != w.name || got.File != w.file || got.Camera != w.camera {
				t.Errorf("identity = %q %q camera %d, want %q %q camera %d",
					got.Name, got.File, got.Camera, w.name, w.file, w.camera)
			}
			if got.Codec != w.codec || got.Profile != w.profile || got.Level != w.level {
				t.Errorf("codec/profile/level = %s/%s/%s, want %s/%s/%s",
					got.Codec, got.Profile, got.Level, w.codec, w.profile, w.level)
			}
			if got.Width != w.width || got.Height != w.height {
				t.Errorf("size = %dx%d, want %dx%d", got.Width, got.Height, w.width, w.height)
			}
			if got.FPS != w.fps {
				t.Errorf("fps = %d, want %d", got.FPS, w.fps)
			}
			if got.GOP != w.fps {
				t.Errorf("gop = %d, want fps (%d)", got.GOP, w.fps)
			}
			if got.Frames != w.frames {
				t.Errorf("frames = %d, want %d", got.Frames, w.frames)
			}
			if got.HasBFrames != 0 {
				t.Errorf("has_b_frames = %d, want 0", got.HasBFrames)
			}
			if d := math.Abs(float64(got.BitrateBPS-w.bitrate)) / float64(w.bitrate); d > 0.05 {
				t.Errorf("bitrate = %d bps, want %d within 5 %% (off by %.1f %%)", got.BitrateBPS, w.bitrate, d*100)
			}

			// The manifest must not just echo itself: recompute the bitrate from
			// the size of the file on disk.
			info, err := os.Stat(filepath.Join(out, w.file)) //nolint:gosec // out is a test directory
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			fromSize := float64(info.Size()) * 8 * float64(w.fps) / float64(w.frames)
			if d := math.Abs(fromSize-float64(got.BitrateBPS)) / fromSize; d > 0.001 {
				t.Errorf("manifest bitrate %d differs from file size %.0f bps", got.BitrateBPS, fromSize)
			}
		})
	}

	t.Run("hls", func(t *testing.T) {
		if m.HLS.Playlist != "hls/index.m3u8" || m.HLS.SegmentSeconds != 4 ||
			m.HLS.Segments != 150 || math.Abs(m.HLS.DurationSecond-600) > 0.5 {
			t.Errorf("manifest hls = %+v, want hls/index.m3u8, 4 s segments, 150 segments, 600 s", m.HLS)
		}
		checkHLSFolder(t, out)
	})
}

// TestMediagen_BandDecodesAtGOPStarts reads the band back from the encoded
// files in MEDIAGEN_DIR: the first frame of every GOP of each of the four
// streams must carry the file's id as its camera, its own index as its frame
// and a CRC that checks out (band.Decode). ffmpeg decodes only the key frames
// (-skip_frame nokey) and hands each one over once (-fps_mode passthrough; at
// the rate it guesses for a raw .h264 file it would duplicate frames). It skips
// without MEDIAGEN_DIR, since generating the media takes minutes; CI's media
// job runs it on the files it uploads. Under either require flag
// (MEDIAGEN_REQUIRE_FFMPEG, SPIKE_REQUIRE_MEDIA), which CI sets, a missing
// MEDIAGEN_DIR fails instead (R19): CI must not skip it silently.
func TestMediagen_BandDecodesAtGOPStarts(t *testing.T) {
	requireFFmpeg(t)
	out := os.Getenv("MEDIAGEN_DIR")
	if out == "" {
		for _, flag := range []string{"MEDIAGEN_REQUIRE_FFMPEG", "SPIKE_REQUIRE_MEDIA"} {
			if os.Getenv(flag) == "1" {
				t.Fatalf("MEDIAGEN_DIR is not set but %s=1", flag)
			}
		}
		t.Skip("MEDIAGEN_DIR is not set")
	}
	// Global Constraints, "Simulated media" and "Band": GOP = fps; 16 px blocks
	// from 1080 lines up, else 8 px. Camera is the media file id (R1).
	for _, s := range []struct {
		file                 string
		camera, side, gop, n int // n: the number of GOPs
	}{
		{"sub.h264", 1, 8, 12, 60},
		{"main.h264", 2, 16, 20, 60},
		{"main-high51.h264", 3, 16, 20, 20},
		{"main-h265.h265", 4, 16, 20, 60},
	} {
		t.Run(s.file, func(t *testing.T) {
			width, height := 48*s.side, 2*s.side
			raw, err := exec.Command("ffmpeg", "-v", "error", "-skip_frame", "nokey", //nolint:gosec // out is a test directory
				"-i", filepath.Join(out, s.file), "-fps_mode", "passthrough",
				"-vf", fmt.Sprintf("crop=%d:%d:0:0,format=gray", width, height),
				"-f", "rawvideo", "pipe:1").Output()
			if err != nil {
				t.Fatalf("ffmpeg: %v", err)
			}
			if got := len(raw) / (width * height); got != s.n || len(raw)%(width*height) != 0 {
				t.Fatalf("ffmpeg gave %d bytes, %d key frames of %dx%d; want %d", len(raw), got, width, height, s.n)
			}
			for k := range s.n {
				luma := raw[k*width*height : (k+1)*width*height]
				got, ok := band.Decode(bandRow(luma, width, 0, s.side), bandRow(luma, width, 1, s.side))
				want := band.Code{Camera: uint8(s.camera), Frame: uint16(k * s.gop)} //nolint:gosec // small constants
				if !ok || got != want {
					t.Errorf("key frame %d: band decodes to %+v (valid %v), want %+v", k, got, ok, want)
				}
			}
		})
	}
}

// checkHLSFolder reads the playlist and the media back independently of the
// code that wrote the manifest.
func checkHLSFolder(t *testing.T, out string) {
	t.Helper()
	playlist, err := os.ReadFile(filepath.Join(out, "hls", "index.m3u8")) //nolint:gosec // out is a test directory
	if err != nil {
		t.Fatalf("read playlist: %v", err)
	}
	text := string(playlist)
	for _, tag := range []string{"#EXT-X-PLAYLIST-TYPE:VOD", "#EXT-X-MAP:URI=", "#EXT-X-ENDLIST"} {
		if !strings.Contains(text, tag) {
			t.Errorf("playlist lacks %s", tag)
		}
	}
	extinf := regexp.MustCompile(`#EXTINF:([0-9.]+),`).FindAllStringSubmatch(text, -1)
	if len(extinf) != 150 {
		t.Fatalf("playlist has %d segments, want 150", len(extinf))
	}
	for i, match := range extinf {
		d, err := strconv.ParseFloat(match[1], 64)
		if err != nil || math.Abs(d-4) > 0.01 {
			t.Fatalf("segment %d lasts %q s, want 4", i, match[1])
		}
	}
	segments, err := filepath.Glob(filepath.Join(out, "hls", "*.m4s"))
	if err != nil || len(segments) != 150 {
		t.Errorf("found %d .m4s segments (err %v), want 150", len(segments), err)
	}

	// Copy of main, looped ten times: same codec, size and 12000 packets.
	probe, err := exec.Command("ffprobe", "-v", "error", "-count_packets", "-select_streams", "v:0", //nolint:gosec // out is a test directory
		"-show_entries", "stream=codec_name,profile,width,height,nb_read_packets:format=duration",
		"-of", "json", filepath.Join(out, "hls", "index.m3u8")).Output()
	if err != nil {
		t.Fatalf("ffprobe hls: %v", err)
	}
	var doc struct {
		Streams []struct {
			Codec   string `json:"codec_name"`
			Profile string `json:"profile"`
			Width   int    `json:"width"`
			Height  int    `json:"height"`
			Packets string `json:"nb_read_packets"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(probe, &doc); err != nil || len(doc.Streams) != 1 {
		t.Fatalf("parse ffprobe hls output %s: %v", probe, err)
	}
	s := doc.Streams[0]
	if s.Codec != "h264" || s.Profile != "Main" || s.Width != 1920 || s.Height != 1080 || s.Packets != "12000" {
		t.Errorf("hls stream = %+v, want h264 Main 1920x1080 with 12000 packets", s)
	}
	if d, err := strconv.ParseFloat(doc.Format.Duration, 64); err != nil || math.Abs(d-600) > 0.5 {
		t.Errorf("hls duration = %q s (err %v), want 600", doc.Format.Duration, err)
	}
}

// bandRow reads one row of the band back from the luma plane: the mean of the
// central 50 % of each block, thresholded at 128.
func bandRow(luma []byte, width, rowIndex, side int) [48]bool {
	var row [48]bool
	for i := range row {
		sum, n := 0, 0
		for y := rowIndex*side + side/4; y < (rowIndex+1)*side-side/4; y++ {
			for x := i*side + side/4; x < (i+1)*side-side/4; x++ {
				sum += int(luma[y*width+x])
				n++
			}
		}
		row[i] = sum/n >= 128
	}
	return row
}

func TestRenderer_BandCarriesFileIDAndFrameIndex(t *testing.T) {
	for _, s := range specs() {
		t.Run(s.name, func(t *testing.T) {
			r := newRenderer(s)
			side := 8
			if s.height >= 1080 {
				side = 16
			}
			for _, i := range []int{0, 1, s.frames() / 2, s.frames() - 1} {
				luma := r.render(i)[:s.width*s.height]
				got, ok := band.Decode(bandRow(luma, s.width, 0, side), bandRow(luma, s.width, 1, side))
				want := band.Code{Camera: s.camera, Frame: uint16(i), Loop: 0} //nolint:gosec // i is below 65536
				if !ok || got != want {
					t.Errorf("frame %d: band decodes to %+v, %v; want %+v", i, got, ok, want)
				}
			}
		})
	}
}

func TestRenderer_FramesMove(t *testing.T) {
	s := specs()[0]
	r := newRenderer(s)
	first := append([]byte(nil), r.render(100)...)
	second := r.render(101)
	different := 0
	for i := range first[:s.width*s.height] {
		if first[i] != second[i] {
			different++
		}
	}
	if different < s.width*s.height/2 {
		t.Errorf("only %d of %d luma pixels differ between consecutive frames", different, s.width*s.height)
	}
}

func TestGopLength(t *testing.T) {
	keys := func(n, gop int) []bool {
		k := make([]bool, n)
		for i := 0; i < n; i += gop {
			k[i] = true
		}
		return k
	}
	tests := []struct {
		name    string
		keys    []bool
		want    int
		wantErr bool
	}{
		{"regular", keys(60, 12), 12, false},
		{"single GOP", keys(20, 20), 20, false},
		{"extra key frame", func() []bool { k := keys(60, 12); k[30] = true; return k }(), 0, true},
		{"does not start with a key frame", append([]bool{false}, keys(59, 12)...), 0, true},
		{"partial last GOP", keys(50, 12), 0, true},
		{"no frames", nil, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := gopLength(tc.keys)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("gopLength = %d, %v; want %d, error %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	for _, tc := range []struct {
		codec string
		idc   int
		want  string
	}{
		{"h264", 31, "3.1"}, {"h264", 40, "4.0"}, {"h264", 51, "5.1"},
		{"hevc", 123, "4.1"}, {"hevc", 120, "4.0"}, {"hevc", 153, "5.1"},
	} {
		if got := levelString(tc.codec, tc.idc); got != tc.want {
			t.Errorf("levelString(%s, %d) = %s, want %s", tc.codec, tc.idc, got, tc.want)
		}
	}
}
