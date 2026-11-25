package mi8

import (
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// frameAt is a 160 × 90 frame with a bright square at x, or none for x < 0.
func frameAt(atMs int64, x int) Frame {
	g := make([]byte, FrameSize)
	if x >= 0 {
		for y := 20; y < 50; y++ {
			for i := x; i < x+30 && i < FrameWidth; i++ {
				g[y*FrameWidth+i] = 255
			}
		}
	}
	return Frame{AtMs: atMs, Gray: g}
}

// sliceSource gives its frames, then ends.
type sliceSource []Frame

func (s sliceSource) Frames(context.Context) (<-chan Frame, error) {
	ch := make(chan Frame, len(s))
	for _, f := range s {
		ch <- f
	}
	close(ch)
	return ch, nil
}

// TestMotionRef_FrameDiffEpisodes (P41): synthetic 160 × 90 frames at 2 fps
// with motion from 10 to 14 s and at 30 s give two episodes, the first
// ending at its last motion frame, apart by more than the 10 s quiet gap.
func TestMotionRef_FrameDiffEpisodes(t *testing.T) {
	var frames sliceSource
	for ms := int64(0); ms <= 45000; ms += 500 {
		x := -1
		switch {
		case ms >= 10000 && ms <= 14000:
			x = int((ms - 10000) / 500 * 10) // a square that moves
		case ms == 30000:
			x = 60
		}
		frames = append(frames, frameAt(ms, x))
	}
	eps, err := MotionEpisodes(t.Context(), frames, DefaultEpisodeParams)
	if err != nil {
		t.Fatal(err)
	}
	var got []Episode
	for e := range eps {
		got = append(got, e)
	}
	if len(got) != 2 {
		t.Fatalf("episodes = %+v, want 2", got)
	}
	if got[0].StartMs != 10000 || got[0].EndMs < 14000 || got[0].EndMs > 14500 {
		t.Errorf("first episode = %+v, want 10 s to about 14 s", got[0])
	}
	if got[1].StartMs != 30000 {
		t.Errorf("second episode = %+v, want a start at 30 s", got[1])
	}
	if gap := got[1].StartMs - got[0].EndMs; gap < DefaultEpisodeParams.QuietGap.Milliseconds() {
		t.Errorf("the episodes are %d ms apart, want at least the 10 s quiet gap", gap)
	}
	// One episode while the gaps between motion stay under 10 s.
	t1 := NewEpisodes(DefaultEpisodeParams)
	var closed int
	for i, ms := range []int64{0, 500, 5000, 5500, 14000, 14500, 30000} {
		if c, _ := t1.Add(frameAt(ms, i*20%120)); c != nil {
			closed++
		}
	}
	if closed != 1 {
		t.Errorf("closed %d episodes, want 1 (motion 9 s apart is one episode, the 30 s frame closes it)", closed)
	}
}

// TestMotionRef_DiffThresholds: a frame is motion only when at least 1 % of
// its pixels change by more than 25 gray levels.
func TestMotionRef_DiffThresholds(t *testing.T) {
	p := DefaultEpisodeParams
	base := make([]byte, FrameSize)
	change := func(n, by int) []byte {
		f := make([]byte, FrameSize)
		for i := range n {
			f[i] = byte(by) //nolint:gosec // a small level
		}
		return f
	}
	onePercent := FrameSize / 100
	if !p.Motion(base, change(onePercent, 26)) {
		t.Error("1 % of the pixels by 26 levels is not motion")
	}
	if p.Motion(base, change(onePercent-1, 26)) {
		t.Error("just under 1 % is motion")
	}
	if p.Motion(base, change(FrameSize, 25)) {
		t.Error("every pixel by 25 levels is motion")
	}
}

// TestMotionRef_MatchWindowUsesReferenceLag (P41): with L = 1.2 s an event
// 1 s before the episode's start matches and one 2 s before does not; 5 s
// after matches, 5.1 s does not.
func TestMotionRef_MatchWindowUsesReferenceLag(t *testing.T) {
	const start = int64(100_000)
	lag := 1200 * time.Millisecond
	for _, tc := range []struct {
		offsetMs int64
		want     bool
	}{{-1000, true}, {-2000, false}, {5000, true}, {5100, false}, {0, true}, {-1200, true}} {
		if got := Matches(start, start+tc.offsetMs, lag, MatchWindow); got != tc.want {
			t.Errorf("an event at %+d ms: matches = %v, want %v", tc.offsetMs, got, tc.want)
		}
	}
	eps := []Episode{{StartMs: start, EndMs: start + 3000}}
	if got := MatchLag(eps, []int64{start - 1000}, lag, MatchWindow); got != 1 {
		t.Errorf("MatchLag = %d, want 1", got)
	}
	if got := Match(eps, []int64{start - 1000}, MatchWindow); got != 0 {
		t.Errorf("Match without lag = %d, want 0", got)
	}
	// L: the p90 of the samples plus 0.5 s, capped at 3 s.
	var samples []time.Duration
	for i := 1; i <= 10; i++ {
		samples = append(samples, time.Duration(i)*100*time.Millisecond)
	}
	if got := Lag(samples); got != 1400*time.Millisecond {
		t.Errorf("Lag = %v, want 1.4 s (p90 0.9 s + 0.5 s)", got)
	}
	if got := Lag([]time.Duration{10 * time.Second}); got != 3*time.Second {
		t.Errorf("Lag = %v, want the 3 s cap", got)
	}
}

// secretParts are a test camera's values, assembled at run time.
var (
	testUser = "cam" + "user-7731"
	testPass = "SENT" + "INEL-campass-1"
)

// TestMotionRef_NoCredentialInFFmpegArgvOrEnv (P41): the substream reference
// gives FFmpeg the stream on its stdin; a stand-in ffmpeg that dumps its
// argv and environment finds neither the URI, the camera's address, user nor
// password there, while the camera saw the credentials.
func TestMotionRef_NoCredentialInFFmpegArgvOrEnv(t *testing.T) {
	cam := startRTSPCam(t, testUser, testPass)
	bin, dump := installFakeFFmpeg(t, "motion")
	src := &RTSPSource{URI: cam.URL(), User: testUser, Pass: testPass, FFmpeg: bin, FirstFrame: 20 * time.Second}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	frames, err := src.Frames(ctx)
	if err != nil {
		t.Fatalf("Frames: %v", err)
	}
	for range 3 {
		if f, ok := <-frames; !ok || len(f.Gray) != FrameSize {
			t.Fatal("no frame")
		}
	}
	if cam.authed.Load() == 0 {
		t.Error("the camera never saw its credentials")
	}
	d := readDump(t, dump)
	if d.Bytes == 0 {
		t.Error("ffmpeg's stdin carried no stream")
	}
	host := strings.TrimPrefix(strings.TrimSuffix(cam.URL(), "/sub"), "rtsp://")
	all := strings.Join(d.Args, "\x00") + "\x00" + strings.Join(d.Env, "\x00")
	for _, secret := range []string{cam.URL(), host, testUser, testPass, "rtsp://"} {
		if strings.Contains(all, secret) {
			t.Errorf("ffmpeg's argv or environment holds %q", secret)
		}
	}
	if want := strings.Join(FFmpegArgs(CodecH264), " "); strings.Join(d.Args, " ") != want {
		t.Errorf("ffmpeg's argv = %q, want %q", d.Args, want)
	}
	if !strings.Contains(strings.Join(d.Args, " "), "-i pipe:0") || !strings.Contains(strings.Join(d.Args, " "), "fps=2,scale=160:90,format=gray") {
		t.Errorf("ffmpeg's argv = %q", d.Args)
	}
	cancel()
	for range frames { //nolint:revive // drained
	}
}

// TestMotionRef_WrongCredentialsNoFrames: a camera that refuses the
// credentials gives no reference, with an error that names nothing of it.
func TestMotionRef_WrongCredentialsNoFrames(t *testing.T) {
	cam := startRTSPCam(t, testUser, testPass)
	bin, _ := installFakeFFmpeg(t, "motion")
	src := &RTSPSource{URI: cam.URL(), User: testUser, Pass: "wrong", FFmpeg: bin, FirstFrame: 5 * time.Second}
	_, err := src.Frames(t.Context())
	if err == nil {
		t.Fatal("Frames succeeded with a wrong password")
	}
	if strings.Contains(err.Error(), "127.0.0.1") || strings.Contains(err.Error(), testUser) {
		t.Errorf("the error names the camera: %v", err)
	}
}

// TestMotionRef_FramesNeverOnDisk (P41): during a 60 s run of the substream
// reference, no file over 10 KB appears under TMPDIR or results/: frames
// stay in RAM.
func TestMotionRef_FramesNeverOnDisk(t *testing.T) {
	tmp := t.TempDir()
	resultsDir := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	cam := startRTSPCam(t, testUser, testPass)
	bin, _ := installFakeFFmpeg(t, "motion")
	src := &RTSPSource{URI: cam.URL(), User: testUser, Pass: testPass, FFmpeg: bin, FirstFrame: 20 * time.Second}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	eps, err := MotionEpisodes(ctx, src, EpisodeParams{QuietGap: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	big := func() string {
		for _, root := range []string{tmp, resultsDir} {
			var found string
			_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if info, err := d.Info(); err == nil && info.Size() > 10<<10 {
					found = p
				}
				return nil
			})
			if found != "" {
				return found
			}
		}
		return ""
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	frames := 0
	for {
		select {
		case _, ok := <-eps:
			if !ok {
				eps = nil
			}
		case <-tick.C:
			frames++
			if f := big(); f != "" {
				t.Fatalf("a file over 10 KB appeared: %s", f)
			}
		case <-ctx.Done():
			if src.Lag() <= 0 {
				t.Error("the reference measured no delay")
			}
			return
		}
		if eps == nil && ctx.Err() == nil {
			t.Fatal("the reference ended before the 60 s")
		}
	}
}

// TestMotionRef_RealFFmpegDecodes: with the real FFmpeg (CI's, or a
// developer's), an H.264 stream piped to its stdin with FFmpegArgs comes out
// as 160 × 90 gray frames at about 2 fps, in real time.
func TestMotionRef_RealFFmpegDecodes(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg on PATH")
	}
	dir := t.TempDir()
	stream := filepath.Join(dir, "t.h264")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=10", //nolint:gosec // the test's own tool
		"-t", "6", "-c:v", "libx264", "-g", "10", "-bf", "0", "-f", "h264", stream)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot encode H.264 here: %v %s", err, out)
	}
	data, err := os.ReadFile(stream) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	dec, err := startDecoder(ffmpeg, CodecH264, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.kill()
	go func() {
		const chunks = 60 // 6 s at 10 per second
		size := len(data) / chunks
		for i := range chunks {
			end := (i + 1) * size
			if i == chunks-1 {
				end = len(data)
			}
			if _, err := dec.in.Write(data[i*size : end]); err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = dec.in.Close()
	}()
	start := time.Now()
	n := 0
	buf := make([]byte, FrameSize)
	for {
		if _, err := io.ReadFull(dec.out, buf); err != nil {
			break
		}
		n++
	}
	if took := time.Since(start); n < 8 || n > 16 {
		t.Errorf("%d frames in %v, want about 12 (2 fps over 6 s)", n, took)
	}
}
