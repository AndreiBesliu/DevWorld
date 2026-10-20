// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Command recmediagen writes the simulated camera stream that the recorder
// spike adds to the video spike's media: sub-256k.h264, the 256 kbps stream of
// HUB-07's low-bitrate runs (G02), and manifest-recording.json with what
// ffprobe measured in it. mediagen's manifest.json is left alone, so its own
// checks still describe exactly its four streams.
//
// The frames are rendered as in the video spike's mediagen (a moving gradient
// with noise, the frame number in large digits and the frame band) and piped to
// ffmpeg. The renderer is copied from mediagen, which is package main. ffmpeg
// with libx264 is only a CI and developer tool; it is never shipped.
//
//	recmediagen -out DIR
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/band"
)

// manifestName is the manifest that recmediagen writes next to mediagen's.
const manifestName = "manifest-recording.json"

// streamSpec is one H.264 elementary stream with GOP = fps and no B-frames.
type streamSpec struct {
	name    string
	file    string
	camera  uint8 // media file id painted into the band
	profile string
	level   string
	width   int
	height  int
	fps     int
	seconds int
	bitrate int // bits per second
}

func (s streamSpec) frames() int { return s.fps * s.seconds }

// sub256k is the sub stream of the video spike at half its bitrate. The band's
// camera field is the media file id; 1 to 4 are mediagen's files.
var sub256k = streamSpec{"sub-256k", "sub-256k.h264", 5, "main", "3.1", 640, 360, 12, 60, 256_000}

// streamInfo is what the manifest records for one file, as measured by ffprobe
// and from the size of the file. The fields are those of mediagen's manifest.
type streamInfo struct {
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
}

type manifest struct {
	Streams []streamInfo `json:"streams"`
}

func main() {
	out := flag.String("out", "", "directory to write the media to (required)")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: recmediagen -out DIR")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := generate(ctx, *out)
	stop()
	if err != nil {
		log.Printf("recmediagen: %v", err)
		os.Exit(1)
	}
}

// generate writes sub-256k.h264 and the manifest into out.
func generate(ctx context.Context, out string) error {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is required: %w", tool, err)
		}
	}
	if err := os.MkdirAll(out, 0o750); err != nil { //nolint:gosec // out is the directory the caller asked for
		return err
	}
	// The manifest comes last, so an interrupted run never leaves a manifest
	// that describes another file than the one on disk.
	manifestPath := filepath.Join(out, manifestName)
	if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) { //nolint:gosec // out is the directory the caller asked for
		return err
	}

	s := sub256k
	log.Printf("encoding %s: %dx%d h264 at %d fps, %d s", s.name, s.width, s.height, s.fps, s.seconds)
	path := filepath.Join(out, s.file)
	if err := encode(ctx, s, path); err != nil {
		return fmt.Errorf("encode %s: %w", s.name, err)
	}
	info, err := measure(ctx, s, path)
	if err != nil {
		return fmt.Errorf("measure %s: %w", s.name, err)
	}

	raw, err := json.MarshalIndent(manifest{Streams: []streamInfo{info}}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, append(raw, '\n'), 0o600) //nolint:gosec // out is the directory the caller asked for
}

// encoderArgs are the ffmpeg arguments for one pass of a two-pass encode, as in
// mediagen. They read raw I420 from stdin; pass 1 only writes the statistics
// file, pass 2 writes the Annex-B stream to path.
func encoderArgs(s streamSpec, pass int, stats, path string) []string {
	gop := strconv.Itoa(s.fps)
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "rawvideo", "-pix_fmt", "yuv420p",
		"-s", fmt.Sprintf("%dx%d", s.width, s.height),
		"-framerate", strconv.Itoa(s.fps), "-i", "pipe:0",
		"-b:v", strconv.Itoa(s.bitrate), "-profile:v", s.profile,
		"-c:v", "libx264", "-preset", "veryfast", "-level:v", s.level,
		"-g", gop, "-keyint_min", gop, "-sc_threshold", "0", "-bf", "0",
		"-pass", strconv.Itoa(pass), "-passlogfile", stats,
	}
	if pass == 1 {
		return append(args, "-f", "null", "-")
	}
	return append(args, "-f", "h264", path)
}

// runFFmpeg starts ffmpeg, lets feed write to its stdin, and waits for it.
func runFFmpeg(ctx context.Context, args []string, feed func(io.Writer) error) error {
	//nolint:gosec // The arguments come from the fixed stream spec and the output directory.
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	feedErr := feed(stdin)
	closeErr := stdin.Close()
	if err := cmd.Wait(); err != nil {
		return errors.Join(fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String())), feedErr)
	}
	return errors.Join(feedErr, closeErr)
}

// encode renders the frames of s and encodes them in two passes, which land
// within a percent or two of the target bitrate.
func encode(ctx context.Context, s streamSpec, path string) error {
	dir, err := os.MkdirTemp("", "recmediagen-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	stats := filepath.Join(dir, "stats")

	r := newRenderer(s)
	for pass := 1; pass <= 2; pass++ {
		err := runFFmpeg(ctx, encoderArgs(s, pass, stats, path), func(w io.Writer) error {
			for i := range s.frames() {
				if _, err := w.Write(r.render(i)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("pass %d: %w", pass, err)
		}
	}
	return nil
}

// probeOutput is the part of ffprobe's JSON that measure reads.
type probeOutput struct {
	Streams []struct {
		Codec      string `json:"codec_name"`
		Profile    string `json:"profile"`
		Level      int    `json:"level"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		FrameRate  string `json:"r_frame_rate"`
		HasBFrames int    `json:"has_b_frames"`
	} `json:"streams"`
	Frames []struct {
		KeyFrame int    `json:"key_frame"`
		PictType string `json:"pict_type"`
	} `json:"frames"`
}

// measure runs ffprobe on the finished stream and checks that it is a whole
// number of regular GOPs without B-frames, so that it loops seamlessly.
func measure(ctx context.Context, s streamSpec, path string) (streamInfo, error) {
	//nolint:gosec // path is built from the output directory and the fixed stream spec.
	raw, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,profile,level,width,height,r_frame_rate,has_b_frames:frame=key_frame,pict_type",
		"-of", "json", path).Output()
	if err != nil {
		return streamInfo{}, fmt.Errorf("ffprobe: %w", err)
	}
	var p probeOutput
	if err := json.Unmarshal(raw, &p); err != nil || len(p.Streams) != 1 {
		return streamInfo{}, fmt.Errorf("unexpected ffprobe output (%v): %s", err, raw)
	}
	st := p.Streams[0]

	var num, den int
	if _, err := fmt.Sscanf(st.FrameRate, "%d/%d", &num, &den); err != nil || den == 0 {
		return streamInfo{}, fmt.Errorf("unexpected frame rate %q", st.FrameRate)
	}
	// The H.264 VUI counts field ticks, so ffprobe reports twice the rate.
	fps := num / den / 2

	keyFrames := make([]bool, len(p.Frames))
	for i, f := range p.Frames {
		keyFrames[i] = f.KeyFrame == 1
		if f.PictType == "B" {
			return streamInfo{}, fmt.Errorf("frame %d is a B-frame", i)
		}
	}
	gop, err := gopLength(keyFrames)
	if err != nil {
		return streamInfo{}, err
	}
	if len(p.Frames) != s.frames() {
		return streamInfo{}, fmt.Errorf("stream has %d frames, want %d", len(p.Frames), s.frames())
	}

	fileInfo, err := os.Stat(path) //nolint:gosec // path is inside the directory the caller asked for
	if err != nil {
		return streamInfo{}, err
	}
	seconds := float64(len(p.Frames)) / float64(fps)

	return streamInfo{
		Name: s.name, File: s.file, Camera: int(s.camera),
		Codec: st.Codec, Profile: st.Profile, Level: fmt.Sprintf("%d.%d", st.Level/10, st.Level%10),
		Width: st.Width, Height: st.Height, FPS: fps, GOP: gop,
		BitrateBPS: int(math.Round(float64(fileInfo.Size()) * 8 / seconds)),
		Frames:     len(p.Frames), HasBFrames: st.HasBFrames,
	}, nil
}

// gopLength returns the distance between key frames. It fails unless the first
// frame is a key frame and the stream is a whole number of equal GOPs.
func gopLength(keyFrames []bool) (int, error) {
	var starts []int
	for i, k := range keyFrames {
		if k {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 || starts[0] != 0 {
		return 0, errors.New("stream does not start with a key frame")
	}
	gop := len(keyFrames)
	if len(starts) > 1 {
		gop = starts[1]
	}
	for i, start := range starts {
		if start != i*gop {
			return 0, fmt.Errorf("irregular GOP: key frame %d is at frame %d, want %d", i, start, i*gop)
		}
	}
	if len(keyFrames)%gop != 0 {
		return 0, fmt.Errorf("%d frames are not a whole number of GOPs of %d", len(keyFrames), gop)
	}
	return gop, nil
}

// renderer draws the raw I420 frames of one stream into a reused buffer. It is
// mediagen's renderer: drawing a frame only copies prepared rows, which keeps
// it fast, also under the race detector.
type renderer struct {
	spec  streamSpec
	buf   []byte
	tex   []byte // noiseRows rows of texWidth bytes: the gradient plus frozen noise
	uRow  []byte // chroma gradients, long enough to be sliced at any offset below the period
	vRow  []byte
	panel []byte // one line of the black panel behind the number
	cell  []byte // one line of a lit cell of the digit font
	scale int    // pixels per cell of the digit font
}

const (
	gradientPeriod = 256 // pixels in one wave of the gradient
	gradientSpeed  = 6   // pixels the gradient moves per frame
	noiseLevels    = 32  // noise adds 0..31 to the gradient, which never goes above 200
	noiseRows      = 64  // frozen noise patterns
	noiseShifts    = 16  // whole gradient periods by which a line can start inside a texture row
	digitCount     = 5   // the frame field of the band has 16 bits, so up to 5 digits

	panelLevel = 16  // black behind the number
	digitLevel = 235 // white digits
)

// wave returns one period of a triangle wave between low and high.
func wave(phase, low, high int) []byte {
	row := make([]byte, gradientPeriod)
	for x := range row {
		p := (x + phase) % gradientPeriod
		if p >= gradientPeriod/2 {
			p = gradientPeriod - 1 - p
		}
		row[x] = byte(low + (high-low)*p/(gradientPeriod/2-1)) //nolint:gosec // the value is between low and high, both below 256
	}
	return row
}

func newRenderer(s streamSpec) *renderer {
	r := &renderer{
		spec:  s,
		buf:   make([]byte, s.width*s.height*3/2),
		scale: max(2, s.height/60),
	}
	repeat := func(period []byte, n int) []byte {
		return bytes.Repeat(period, n/len(period)+2)
	}
	r.uRow = repeat(wave(0, 104, 152), s.width/2)
	r.vRow = repeat(wave(gradientPeriod/2, 104, 152), s.width/2)

	panelWidth := (digitCount*6 + 1) * r.scale
	r.panel = bytes.Repeat([]byte{panelLevel}, panelWidth)
	r.cell = bytes.Repeat([]byte{digitLevel}, r.scale)

	// The texture is what makes the encoder spend its bitrate: lines of it are
	// picked at a different place for every line of every frame. A fixed seed
	// keeps the media reproducible.
	gradient := wave(0, 32, 200)
	texWidth := gradientPeriod*noiseShifts + s.width
	r.tex = make([]byte, noiseRows*texWidth)
	state := uint64(0x9e3779b97f4a7c15)
	for row := range noiseRows {
		line := r.tex[row*texWidth : (row+1)*texWidth]
		for x := range line {
			state = splitmix64(state)
			line[x] = gradient[x%gradientPeriod] + byte((state>>32)%noiseLevels)
		}
	}
	return r
}

func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ x>>30) * 0xbf58476d1ce4e5b9
	x = (x ^ x>>27) * 0x94d049bb133111eb
	return x ^ x>>31
}

// render returns frame i of the loop. The returned slice is reused by the next call.
func (r *renderer) render(i int) []byte {
	w, h := r.spec.width, r.spec.height
	luma := r.buf[:w*h]
	u := r.buf[w*h : w*h+w*h/4]
	v := r.buf[w*h+w*h/4:]
	texWidth := len(r.tex) / noiseRows

	shift := i * gradientSpeed
	for y := range h {
		// The start inside the row keeps the gradient moving: its phase is
		// the offset modulo the period.
		phase := (shift + 2*y) % gradientPeriod
		from := ((i*7+y*13)%noiseRows)*texWidth + ((i*5+y*3)%noiseShifts)*gradientPeriod + phase
		copy(luma[y*w:(y+1)*w], r.tex[from:from+w])
	}
	for y := range h / 2 {
		from := (shift/2 + 3*y) % gradientPeriod
		copy(u[y*(w/2):(y+1)*(w/2)], r.uRow[from:from+w/2])
		copy(v[y*(w/2):(y+1)*(w/2)], r.vRow[from:from+w/2])
	}

	r.drawNumber(luma, i)
	band.Paint(r.buf, w, h, band.Code{Camera: r.spec.camera, Frame: uint16(i)}) //nolint:gosec // a loop has fewer than 65536 frames
	return r.buf
}

// digitGlyphs is a 5x7 font for the digits; each row holds five bits, left to right.
var digitGlyphs = [10][7]uint8{
	{0b01110, 0b10001, 0b10011, 0b10101, 0b11001, 0b10001, 0b01110},
	{0b00100, 0b01100, 0b00100, 0b00100, 0b00100, 0b00100, 0b01110},
	{0b01110, 0b10001, 0b00001, 0b00010, 0b00100, 0b01000, 0b11111},
	{0b11110, 0b00001, 0b00001, 0b01110, 0b00001, 0b00001, 0b11110},
	{0b00010, 0b00110, 0b01010, 0b10010, 0b11111, 0b00010, 0b00010},
	{0b11111, 0b10000, 0b11110, 0b00001, 0b00001, 0b10001, 0b01110},
	{0b00110, 0b01000, 0b10000, 0b11110, 0b10001, 0b10001, 0b01110},
	{0b11111, 0b00001, 0b00010, 0b00100, 0b01000, 0b01000, 0b01000},
	{0b01110, 0b10001, 0b10001, 0b01110, 0b10001, 0b10001, 0b01110},
	{0b01110, 0b10001, 0b10001, 0b01111, 0b00001, 0b00010, 0b01100},
}

// drawNumber prints n in large white digits on a black panel in the middle of
// the luma plane, below the band, so that a person can read the frame index.
func (r *renderer) drawNumber(luma []byte, n int) {
	w, h, s := r.spec.width, r.spec.height, r.scale
	// Five columns per digit and a gap of one column between digits and around the text.
	left, top := (w-len(r.panel))/2, h/3

	for y := top; y < top+9*s; y++ {
		copy(luma[y*w+left:], r.panel)
	}
	for d, ch := range fmt.Sprintf("%0*d", digitCount, n) {
		for row, bits := range digitGlyphs[ch-'0'] {
			for col := range 5 {
				if bits>>(4-col)&1 == 0 {
					continue
				}
				x := left + (1+d*6+col)*s
				for y := top + (1+row)*s; y < top+(2+row)*s; y++ {
					copy(luma[y*w+x:], r.cell)
				}
			}
		}
	}
}
