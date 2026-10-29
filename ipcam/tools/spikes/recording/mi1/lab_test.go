package mi1

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/lastindex"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/band"
)

// labChain is the lab's chain on loopback: reccontroller -local serving the
// media, with its screen, and ipcam-hub with the MediaMTX fork, as a
// shard's runner has them.
type labChain struct {
	hub, mediamtx string
	screen        *syncBuffer
}

// startLabChain builds ipcam-hub and reccontroller and starts
// reccontroller until ctx ends; it skips, or fails when they are required,
// without the fork or the media.
func startLabChain(ctx context.Context, t *testing.T) labChain {
	t.Helper()
	mediamtx := spiketest.Required(t, "MEDIAMTX_BIN", "RECSPIKE_REQUIRE_MEDIAMTX")
	media := spiketest.Required(t, "MEDIAGEN_DIR", "SPIKE_REQUIRE_MEDIA")
	bin := t.TempDir()
	hub := spiketest.BuildBinary(t, bin, "ipcam-hub", "github.com/AndreiBesliu/DevWorld/ipcam/hub/cmd/ipcam-hub")
	ctlBin := spiketest.BuildBinary(t, bin, "reccontroller", "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/cmd/reccontroller")

	ctl := exec.CommandContext(ctx, ctlBin, "-local", "-media", media) //nolint:gosec // the test's build
	stdout, err := ctl.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	ctl.Stderr = os.Stderr
	if err := ctl.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ctl.Process.Kill()
		_ = ctl.Wait()
	})
	screen := &syncBuffer{}
	listening := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stdout)
		once := false
		for sc.Scan() {
			_, _ = screen.Write([]byte(sc.Text() + "\n"))
			if !once && strings.HasPrefix(sc.Text(), "reccontroller rulează") {
				once = true
				close(listening)
			}
		}
	}()
	select {
	case <-listening:
	case <-time.After(time.Minute):
		t.Fatalf("reccontroller did not start:\n%s", screen.String())
	}
	return labChain{hub: hub, mediamtx: mediamtx, screen: screen}
}

// driver is recspike's driver of a lab smoke run in dir, on this runner's
// OS, against the chain's controller; its screen is out.
func (c labChain) driver(dir string, out io.Writer) *Driver {
	return &Driver{
		Controller: "127.0.0.1:7081",
		Hub:        c.hub,
		MediaMTX:   c.mediamtx,
		Results:    filepath.Join(dir, "results"),
		Work:       filepath.Join(dir, "work"),
		Host:       LabHost(runtime.GOOS),
		Out:        out,
		NTP:        func(context.Context) (bool, error) { return true, nil },
	}
}

// smoke runs the lab kill9 smoke of shard 1 with d: its two runs, an
// encrypted one and a clear one. The lines of a run whose stream lost more
// than its limit (Task 7.2d) go to the test's output, where the recorder
// job turns them into notices.
func (c labChain) smoke(ctx context.Context, t *testing.T, d *Driver, out *syncBuffer) {
	t.Helper()
	err := d.RunSet(ctx, SetOptions{Set: LabSet(runtime.GOOS, "kill9"), Mode: ModeLab, Kind: "kill9", Shard: 1, Smoke: true})
	for line := range strings.SplitSeq(out.String(), "\n") {
		if strings.HasPrefix(line, DiagPrefix) {
			fmt.Println(line)
		}
	}
	if err != nil {
		t.Fatalf("RunSet: %v\nrecspike:\n%s\nreccontroller:\n%s", err, out.String(), c.screen.String())
	}
}

// TestMI1_LabKill9Smoke (R03, C16): the lab kill9 driver's smoke on
// loopback, with the real chain: reccontroller -local serving the media,
// ipcam-hub with the MediaMTX fork, and two smoke runs (an encrypted one
// and a clear one) with the smoke windows, a prompt 20 to 40 s after the
// first beacon. The controller writes each run's envelope; each counts,
// reconciled without intervention, HUB-04 clear, and loses less than 2 s.
func TestMI1_LabKill9Smoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	chain := startLabChain(ctx, t)
	dir := t.TempDir()
	out := &syncBuffer{}
	d := chain.driver(dir, out)
	chain.smoke(ctx, t, d, out)
	screen := chain.screen
	bin := filepath.Dir(chain.hub)
	set := LabSet(runtime.GOOS, "kill9")
	// A smoke run's envelopes are under its own results set, apart from
	// the set's counted ones.
	smokeSet := ResultsSet(set, true)
	files, err := filepath.Glob(filepath.Join(bin, "results", "MI1", smokeSet, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	counted := 0
	for _, f := range files {
		e, err := results.Read(f)
		if err != nil {
			t.Fatalf("envelope %s: %v", filepath.Base(f), err)
		}
		if len(e.Runs) != 1 || e.Host != d.Host.Pseudonym || e.Set != smokeSet {
			t.Errorf("envelope %s: %+v", filepath.Base(f), e)
			continue
		}
		r := e.Runs[0]
		if !Counts(r) {
			t.Logf("run %s does not count (%s): redone", r.RunID, r.InvalidReason)
			continue
		}
		counted++
		if !r.Reconciled || !r.Hub04Clear || r.Unreadable != 0 || r.TornParts != 0 || len(r.Streams) == 0 || r.ResumeMs == nil {
			t.Errorf("run %s: %+v\nrecspike about it:\n%s", r.RunID, r, linesOf(out.String(), r.RunID))
			continue
		}
		t.Logf("run %s (%s): loss %d ms, resume %d ms, pacing p99 %d ms", r.RunID, encryptionText(r.Encrypted), r.LossMs, *r.ResumeMs, r.PacingP99Ms)
		for _, s := range r.Streams {
			if s.LossMs >= 2000 || s.LossMs < 0 {
				t.Errorf("run %s: %s lost %d ms, want under 2 s", r.RunID, s.Stream, s.LossMs)
			}
		}
	}
	if counted != 2 {
		t.Errorf("%d runs count, want the 2 smoke runs\nrecspike:\n%s\nreccontroller:\n%s", counted, out.String(), screen.String())
	}
}

// linesOf is the lines of screen that name runID.
func linesOf(screen, runID string) string {
	var out []string
	for line := range strings.SplitSeq(screen, "\n") {
		if strings.Contains(line, runID) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// mainFrames is the number of frames of main.h264, cam01_main's loop: the
// band of each frame carries its index in the loop (C15).
const mainFrames = 1200

// keptFile is the file of a clear run's stop that holds its last recorded
// access unit, copied before the run's folder goes, and that unit's SEI
// index, as lastindex read it.
type keptFile struct {
	path  string
	index uint64
}

// TestMI1_SmokeBandCrossCheck (C15, CI only): the access unit that
// lastindex names as a clear smoke run's last recorded one is the frame
// that ffmpeg decodes last from the file of the stop that holds it: the
// frame's band carries main.h264's camera and, as its Frame, the SEI index
// modulo the file's 1,200 frames. The SEI index, which the loss rests on,
// is then the frame really recorded. It needs ffmpeg, and skips without it
// unless RECSPIKE_REQUIRE_FFMPEG=1, as the mi1-smoke job sets.
func TestMI1_SmokeBandCrossCheck(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		if os.Getenv("RECSPIKE_REQUIRE_FFMPEG") == "1" {
			t.Fatalf("RECSPIKE_REQUIRE_FFMPEG=1, but: %v", err)
		}
		t.Skipf("no ffmpeg: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	chain := startLabChain(ctx, t)
	out := &syncBuffer{}
	d := chain.driver(t.TempDir(), out)
	keep := t.TempDir()
	var mu sync.Mutex
	var kept []keptFile
	var keepErrs []error
	d.LastRecorded = func(ctx context.Context, files lastindex.Before, id age.Identity) (uint64, error) {
		index, err := lastindex.LastRecorded(ctx, files, id)
		if err != nil || id != nil {
			return index, err
		}
		mu.Lock()
		defer mu.Unlock()
		path, kerr := keepLastFile(ctx, files, index, filepath.Join(keep, fmt.Sprintf("%d.mp4", len(kept))))
		if kerr != nil {
			keepErrs = append(keepErrs, kerr)
		} else {
			kept = append(kept, keptFile{path: path, index: index})
		}
		return index, nil
	}
	chain.smoke(ctx, t, d, out)
	mu.Lock()
	defer mu.Unlock()
	if len(keepErrs) > 0 || len(kept) == 0 {
		t.Fatalf("the clear runs' files: %d kept, errors %v", len(kept), keepErrs)
	}
	for _, k := range kept {
		code, frames := lastBand(t, ffmpeg, k.path)
		want := band.Code{Camera: mainCamera, Frame: uint16(k.index % mainFrames)} //nolint:gosec // under 1,200
		if code.Camera != want.Camera || code.Frame != want.Frame {
			t.Errorf("the last of %d frames decoded: band %+v; want camera %d, frame %d (SEI index %d)", frames, code, want.Camera, want.Frame, k.index)
			continue
		}
		t.Logf("SEI index %d: the last decoded frame's band reads frame %d of the loop", k.index, code.Frame)
	}
}

// mainCamera is the band's camera of main.h264 (video/band).
const mainCamera = 2

// keepLastFile copies to dst the newest of files (by their final names, as
// lastindex orders them) whose own last complete sample is index, and
// returns dst.
func keepLastFile(ctx context.Context, files lastindex.Before, index uint64, dst string) (string, error) {
	finals := make([]string, 0, len(files))
	for _, f := range files {
		finals = append(finals, strings.TrimSuffix(f, ".part"))
	}
	slices.Sort(finals)
	for i := len(finals) - 1; i >= 0; i-- {
		last, err := lastindex.LastRecorded(ctx, lastindex.Before{finals[i]}, nil)
		if err != nil || last != index {
			continue
		}
		data, err := os.ReadFile(finals[i])
		if err != nil {
			return "", err
		}
		return dst, os.WriteFile(dst, data, 0o600) //nolint:gosec // the test's own folder
	}
	return "", fmt.Errorf("no file of %v ends at SEI index %d", files, index)
}

// The band of a 1080-line frame: two rows of 48 blocks of 16 px in the
// top-left corner (video/band).
const (
	bandBlocks = 48
	bandSide   = 16
)

// lastBand decodes the file with ffmpeg, only the band's corner of each
// frame in gray, and reads the band of the last frame; frames is how many
// frames ffmpeg decoded.
func lastBand(t *testing.T, ffmpeg, path string) (band.Code, int) {
	t.Helper()
	width, height := bandBlocks*bandSide, 2*bandSide
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", path, //nolint:gosec // the test's own file
		"-vf", fmt.Sprintf("crop=%d:%d:0:0", width, height), "-pix_fmt", "gray", "-f", "rawvideo", "-")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	frame, last := make([]byte, width*height), make([]byte, width*height)
	frames := 0
	for {
		if _, err := io.ReadFull(stdout, frame); err != nil {
			break
		}
		frames++
		frame, last = last, frame
	}
	if err := cmd.Wait(); err != nil || frames == 0 {
		t.Fatalf("ffmpeg of %s: %v, %d frames: %s", path, err, frames, stderr.String())
	}
	var rows [2][bandBlocks]bool
	for r := range rows {
		for i := range bandBlocks {
			sum, n := 0, 0
			for y := r*bandSide + 4; y < (r+1)*bandSide-4; y++ {
				for x := i*bandSide + 4; x < (i+1)*bandSide-4; x++ {
					sum += int(last[y*width+x])
					n++
				}
			}
			rows[r][i] = sum/n > 128
		}
	}
	code, ok := band.Decode(rows[0], rows[1])
	if !ok {
		t.Fatalf("the last frame of %s carries no valid band: rows %v", path, rows)
	}
	return code, frames
}
