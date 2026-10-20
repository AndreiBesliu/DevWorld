package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
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

// manifestDoc pins the JSON field names that other tools read from the
// recording manifest; they are the ones of mediagen's manifest.json.
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
}

// TestRecMediagen_Sub256k checks the 256 kbps stream of the recorder spike
// (plan Task 1, G02): H.264 Main@3.1, 640x360, 12 fps, GOP 12, a 60 s loop of
// 720 frames at 256 kbps ± 10 %, with band camera 5. It reads the media in
// MEDIAGEN_DIR, so that CI tests the very files it uploads; without it, it
// generates them into a temporary directory first.
func TestRecMediagen_Sub256k(t *testing.T) {
	requireFFmpeg(t)
	out := os.Getenv("MEDIAGEN_DIR")
	if out == "" {
		out = t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := generate(ctx, out); err != nil {
			t.Fatalf("generate: %v", err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(out, "manifest-recording.json")) //nolint:gosec // out is a test directory
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifestDoc
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if len(m.Streams) != 1 || m.Streams[0].Name != "sub-256k" {
		t.Fatalf("manifest streams = %+v, want the one entry sub-256k", m.Streams)
	}
	got := m.Streams[0]
	if got.File != "sub-256k.h264" || got.Camera != 5 {
		t.Errorf("file, camera = %q, %d; want sub-256k.h264, 5", got.File, got.Camera)
	}
	if got.Codec != "h264" || got.Profile != "Main" || got.Level != "3.1" {
		t.Errorf("codec/profile/level = %s/%s/%s, want h264/Main/3.1", got.Codec, got.Profile, got.Level)
	}
	if got.Width != 640 || got.Height != 360 || got.FPS != 12 || got.GOP != 12 {
		t.Errorf("size, fps, gop = %dx%d, %d, %d; want 640x360, 12, 12", got.Width, got.Height, got.FPS, got.GOP)
	}
	if got.Frames != 720 || got.HasBFrames != 0 {
		t.Errorf("frames, has_b_frames = %d, %d; want 720, 0", got.Frames, got.HasBFrames)
	}
	if d := math.Abs(float64(got.BitrateBPS-256_000)) / 256_000; d > 0.10 {
		t.Errorf("bitrate = %d bps, want 256000 within 10 %% (off by %.1f %%)", got.BitrateBPS, d*100)
	}
	// The manifest must not just echo itself: recompute the bitrate from the
	// size of the file on disk.
	info, err := os.Stat(filepath.Join(out, "sub-256k.h264")) //nolint:gosec // out is a test directory
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	fromSize := float64(info.Size()) * 8 * 12 / 720
	if d := math.Abs(fromSize-float64(got.BitrateBPS)) / fromSize; d > 0.001 {
		t.Errorf("manifest bitrate %d differs from file size %.0f bps", got.BitrateBPS, fromSize)
	}

	checkBandAtGOPStarts(t, filepath.Join(out, "sub-256k.h264"))
}

// checkBandAtGOPStarts reads the band back from the first frame of every GOP:
// it must carry camera 5 and the frame's own index, with a CRC that checks out
// (band.Decode). ffmpeg decodes only the key frames (-skip_frame nokey) and
// hands each one over once (-fps_mode passthrough). Below 1080 lines the band's
// blocks are 8 px.
func checkBandAtGOPStarts(t *testing.T, path string) {
	t.Helper()
	const side, gop, gops = 8, 12, 60
	width, height := 48*side, 2*side
	raw, err := exec.Command("ffmpeg", "-v", "error", "-skip_frame", "nokey", "-i", path, //nolint:gosec // path is a test file
		"-fps_mode", "passthrough", "-vf", fmt.Sprintf("crop=%d:%d:0:0,format=gray", width, height),
		"-f", "rawvideo", "pipe:1").Output()
	if err != nil {
		t.Fatalf("ffmpeg: %v", err)
	}
	if n := len(raw) / (width * height); n != gops || len(raw)%(width*height) != 0 {
		t.Fatalf("ffmpeg gave %d bytes, %d key frames of %dx%d; want %d", len(raw), n, width, height, gops)
	}
	for k := range gops {
		luma := raw[k*width*height : (k+1)*width*height]
		got, ok := band.Decode(bandRow(luma, width, 0, side), bandRow(luma, width, 1, side))
		want := band.Code{Camera: 5, Frame: uint16(k * gop)} //nolint:gosec // k*gop is below 720
		if !ok || got != want {
			t.Errorf("key frame %d: band decodes to %+v (valid %v), want %+v", k, got, ok, want)
		}
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
