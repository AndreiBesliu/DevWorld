package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/band"
)

// mediagenSource is mediagen's main.go, which recmediagen copies from. The
// path is relative to this package and follows the recording module's
// replace directive for the video module (../video).
var mediagenSource = filepath.Join("..", "..", "..", "video", "cmd", "mediagen", "main.go")

// copiedFromMediagen are the declarations of main.go that are mediagen's,
// unchanged: the renderer, the ffmpeg runner and the probe. Comments and
// layout may differ from mediagen's; the tokens may not.
var copiedFromMediagen = []string{
	"(streamSpec).frames", "streamInfo",
	"runFFmpeg", "probeOutput", "gopLength",
	"renderer", "gradientPeriod", "wave", "newRenderer", "splitmix64",
	"(*renderer).render", "digitGlyphs", "(*renderer).drawNumber",
}

// adaptedDecl pins one declaration that main.go adapts from mediagen, by the
// fingerprints (declFingerprint) of both sides as they were when adapted.
type adaptedDecl struct {
	mediagen    string
	recmediagen string // "" when main.go has no declaration of that name: it is folded into another one
	how         string // what main.go changes
}

// adaptedFromMediagen are the declarations that main.go copies from mediagen
// with changes. When mediagen's side changes, the change has to be mirrored
// in main.go by hand; then both fingerprints are updated here.
var adaptedFromMediagen = map[string]adaptedDecl{
	"streamSpec": {
		mediagen:    "2af375bf191465d4921dddcaeca7ac6b9fbb8c433eee96e558fb68c9ead5d609",
		recmediagen: "c33c4d5c0307c865a64a4a777675e825ee788106486241f1092dcf49fa89b65a",
		how:         "drops the codec field: recmediagen writes H.264 only",
	},
	"encoderArgs": {
		mediagen:    "9da622db2eee608662daec43018dbdf3052264cbff52e85935050ce6f6e58af2",
		recmediagen: "1f3d2b8ef8c39e1b04dc04ef2f0c44a9b6727e935d218be19639d41762ef82bd",
		how:         "drops the libx265 branch and keeps the libx264 arguments",
	},
	"encode": {
		mediagen:    "359412b629fb33975ecd106311e024f5b32d8da0ff4d73667e4d8ed604b88773",
		recmediagen: "4b8c68f2182f3d1372afb0f310c4f538db4039293b01ecb8a54d9b716578f0df",
		how:         "names its temporary directory recmediagen-",
	},
	"measure": {
		mediagen:    "c582dc505a5bd6422e81d3fed5823197d3859bed36af62f40fba5b5cb75c5845",
		recmediagen: "b01d388d31022796a4a5323c5b4458f1a628b3076e58b7ab67f2714e32ff1361",
		how:         "halves the frame rate and formats the level for H.264 only, with levelString folded in",
	},
	"levelString": {
		mediagen: "ed6207901d96812949330f7cd1436686931b4d00c1a423512cfe1dfb4d7ede96",
		how:      "is folded into measure, H.264 case only",
	},
}

// ownDecls are the declarations of main.go that have no counterpart in mediagen.
var ownDecls = []string{"manifestName", "sub256k", "manifest", "main", "generate"}

// TestRecMediagen_CopiesMatchMediagen keeps the code that main.go copies from
// mediagen (package main, so it cannot be imported) in step with it, so that
// sub-256k.h264 is rendered, encoded and measured like mediagen's four streams
// that the recorder runs mix it with. A change to mediagen's renderer, encoder
// or probe fails this test until it is mirrored in main.go. Every declaration
// of main.go must be listed in one of the three lists above, so a newly copied
// one cannot slip past.
func TestRecMediagen_CopiesMatchMediagen(t *testing.T) {
	mediagen := parseDecls(t, mediagenSource)
	ours := parseDecls(t, "main.go")

	for _, name := range copiedFromMediagen {
		theirs, okTheirs := mediagen[name]
		mine, okMine := ours[name]
		switch {
		case !okTheirs:
			t.Errorf("mediagen has no declaration %s any more: drop or adapt recmediagen's copy and update this test", name)
		case !okMine:
			t.Errorf("main.go has no declaration %s, which copiedFromMediagen lists", name)
		case theirs != mine:
			t.Errorf("%s differs from mediagen's (%s): main.go copies it unchanged, so make the same change in both files", name, mediagenSource)
		}
	}

	for name, want := range adaptedFromMediagen {
		if theirs, ok := mediagen[name]; !ok {
			t.Errorf("mediagen has no declaration %s any more: update recmediagen's adapted copy and this test", name)
		} else if got := declFingerprint(theirs); got != want.mediagen {
			t.Errorf("mediagen's %s changed (fingerprint %s, pinned %q): mirror the change in main.go, whose copy %s, then update both fingerprints in adaptedFromMediagen",
				name, got, want.mediagen, want.how)
		}
		mine, ok := ours[name]
		switch {
		case want.recmediagen == "" && ok:
			t.Errorf("main.go declares %s, which adaptedFromMediagen says is folded into another declaration", name)
		case want.recmediagen != "" && !ok:
			t.Errorf("main.go has no declaration %s, which adaptedFromMediagen lists", name)
		case ok && declFingerprint(mine) != want.recmediagen:
			t.Errorf("main.go's %s changed (fingerprint %s, pinned %q): it is adapted from mediagen's (%s), so check that both still make the same media, then update its fingerprint in adaptedFromMediagen",
				name, declFingerprint(mine), want.recmediagen, mediagenSource)
		}
	}

	for name := range ours {
		_, adapted := adaptedFromMediagen[name]
		if !adapted && !slices.Contains(copiedFromMediagen, name) && !slices.Contains(ownDecls, name) {
			t.Errorf("main.go declares %s, which this test does not classify: add it to copiedFromMediagen, adaptedFromMediagen or ownDecls", name)
		}
	}
}

// parseDecls returns the top-level declarations of the Go file at path, keyed
// by name ("(*renderer).render" for a method, the first name for a const or
// var block), each as its token sequence without comments or layout. Imports
// are left out.
func parseDecls(t *testing.T, path string) map[string]string {
	t.Helper()
	src, err := os.ReadFile(path) //nolint:gosec // path is main.go or mediagen's main.go
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	decls := map[string]string{}
	for _, d := range f.Decls {
		var name string
		switch d := d.(type) {
		case *ast.FuncDecl:
			name = d.Name.Name
			if d.Recv != nil {
				name = "(" + types.ExprString(d.Recv.List[0].Type) + ")." + name
			}
		case *ast.GenDecl:
			switch spec := d.Specs[0].(type) {
			case *ast.ImportSpec:
				continue
			case *ast.TypeSpec:
				name = spec.Name.Name
			case *ast.ValueSpec:
				name = spec.Names[0].Name
			}
		}
		if _, dup := decls[name]; dup {
			t.Fatalf("%s declares %s twice", path, name)
		}
		decls[name] = declTokens(src[fset.Position(d.Pos()).Offset:fset.Position(d.End()).Offset])
	}
	if len(decls) == 0 {
		t.Fatalf("%s has no declarations", path)
	}
	return decls
}

// declTokens returns the Go tokens of src one per line, without comments, so
// that only a change to the code itself changes the result. Semicolons are
// written the same whether they are explicit or inserted at a line end.
func declTokens(src []byte) string {
	fset := token.NewFileSet()
	var s scanner.Scanner
	s.Init(fset.AddFile("", fset.Base(), len(src)), src, nil, 0)
	var b strings.Builder
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return b.String()
		}
		b.WriteString(tok.String())
		if tok != token.SEMICOLON && lit != "" {
			b.WriteString(" " + lit)
		}
		b.WriteByte('\n')
	}
}

// declFingerprint is the SHA-256 of a declaration's tokens.
func declFingerprint(tokens string) string {
	sum := sha256.Sum256([]byte(tokens))
	return hex.EncodeToString(sum[:])
}

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
