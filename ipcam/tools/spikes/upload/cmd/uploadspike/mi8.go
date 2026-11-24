// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi8"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// MI8's test seams: its timing, and an ffmpeg to use without a download
// ("-": none, the snapshot diff).
const (
	envMI8Timing = "UPLOADSPIKE_TEST_MI8_TIMING" // target=10s,progress=1s,firstframe=5s,beat=0s
	envFFmpeg    = "UPLOADSPIKE_FFMPEG"
)

// mi8Closed is the run state's note of MI8's close, for status.
const mi8Closed = "MI8Closed"

// maxMI8Cameras bounds the cameras of MI8.
const maxMI8Cameras = 16

// mi8Timing is MI8's timing: the observed time that closes the run, the
// progress's period, the wait for a substream's first frame, and a delay of
// the detached copy's first heartbeat (a test seam).
type mi8Timing struct {
	target, progress, firstFrame, firstBeat time.Duration
}

// timing reads envMI8Timing over MI8's defaults.
func mi8TimingOf() (mi8Timing, error) {
	t := mi8Timing{target: mi8.Target, progress: time.Minute}
	v := os.Getenv(envMI8Timing)
	if v == "" {
		return t, nil
	}
	for kv := range strings.SplitSeq(v, ",") {
		k, val, _ := strings.Cut(kv, "=")
		d, err := time.ParseDuration(val)
		if err != nil {
			return t, fmt.Errorf("%s: %s", envMI8Timing, kv)
		}
		switch k {
		case "target":
			t.target = d
		case "progress":
			t.progress = d
		case "firstframe":
			t.firstFrame = d
		case "beat":
			t.firstBeat = d
		default:
			return t, fmt.Errorf("%s: %s", envMI8Timing, kv)
		}
	}
	return t, nil
}

// mi8LockPath is <base>/state/mi8.lock.
func (e *env) mi8LockPath() string { return filepath.Join(e.stateDir(), runstate.MI8Lock) }

// mi8Hub is diag onvif's runner: this device's ipcam-hub, as uploadspike.
func (e *env) mi8Hub() mi8.Hub { return mi8.Hub{Bin: e.hubBin, Start: startWithUs} }

// runMI8 is `uploadspike mi8 [--one-per-brand] | --probe-only | --continue`
// (Task 10, P42, P49): it asks the cameras' addresses, users and passwords
// on the hub device, keeps them in RAM, probes them through ipcam-hub diag
// onvif, and runs MI8 for 72 h without the terminal: on Linux in a detached
// copy of itself, on Windows in this window. One MI8 per device: mi8 and
// mi8 --continue take <base>/state/mi8.lock before any prompt.
func runMI8(ctx context.Context, e *env, args []string) error {
	flags := flag.NewFlagSet("mi8", flag.ContinueOnError)
	cont := flags.Bool("continue", false, "")
	probeOnly := flags.Bool("probe-only", false, "")
	onePerBrand := flags.Bool("one-per-brand", false, "")
	detached := flags.Bool("detached", false, "")
	ffmpegPath := flags.String("ffmpeg", "", "")
	if err := cli.ParseFlags(flags, args, e.out, usage); err != nil {
		return err
	}
	if *detached {
		return runMI8Detached(ctx, e, *ffmpegPath)
	}
	if _, err := mi8TimingOf(); err != nil {
		return err
	}
	hub := e.mi8Hub()
	if *probeOnly {
		cams, err := askCameras(e)
		if err != nil {
			return err
		}
		probes, err := hub.Probe(ctx, cams)
		if err != nil && len(probes) == 0 {
			return err
		}
		printProbes(e, probes)
		return nil
	}
	lock, release, err := lockMI8(e)
	if errors.Is(err, runstate.ErrLocked) {
		e.printf(msgMI8Running)
		return exitError(1)
	}
	if err != nil {
		return err
	}
	defer release()
	if st, err := runstate.Load(e.statePath()); err == nil && !*cont {
		if _, open := st.Open["MI8"]; open {
			return ownerError(msgMI8OpenRun) // its hours join only through --continue
		}
	}
	var cams []mi8.CameraRef
	if *cont {
		if cams, err = continueCameras(ctx, e, hub); err != nil {
			return err
		}
	} else if cams, err = newMI8Run(ctx, e, hub, *onePerBrand); err != nil {
		return err
	}
	ffmpeg := mi8FFmpeg(ctx, e)
	if canDetach {
		return detachMI8(ctx, e, lock, cams, ffmpeg)
	}
	e.printf(msgMI8KeepWindow)
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runMI8Session(sigCtx, e, cams, ffmpeg)
}

// lockMI8 takes MI8's lock: its file on Linux, which the detached copy
// inherits, runstate.Lock elsewhere.
func lockMI8(e *env) (*os.File, func(), error) {
	if canDetach {
		f, err := runstate.LockFile(e.mi8LockPath())
		if err != nil {
			return nil, nil, err
		}
		return f, func() { _ = f.Close() }, nil
	}
	release, err := runstate.Lock(e.mi8LockPath())
	return nil, release, err
}

// readLine reads one answer line, trimmed.
func readLine(e *env) (string, error) {
	line, err := readAnswer(e.in)
	line = strings.TrimSpace(line)
	if err != nil && line == "" {
		return "", ownerError(msgMI8NoAnswer)
	}
	return line, nil
}

// askCamera asks camera id's address, user and password; with optional, an
// empty address skips the camera (ok false).
func askCamera(e *env, id string, optional bool) (mi8.CameraRef, bool, error) {
	for {
		if optional {
			_, _ = fmt.Fprintf(e.out, promptMI8Again, id)
		} else {
			_, _ = fmt.Fprintf(e.out, promptMI8Address, id)
		}
		addr, err := readLine(e)
		if err != nil {
			return mi8.CameraRef{}, false, err
		}
		if addr == "" && optional {
			return mi8.CameraRef{}, false, nil
		}
		xaddr, err := mi8.XAddrFor(addr)
		if err != nil {
			e.printf(msgMI8BadAddress)
			continue
		}
		_, _ = fmt.Fprintf(e.out, promptMI8User, id)
		user, err := readLine(e)
		if err != nil {
			return mi8.CameraRef{}, false, err
		}
		_, _ = fmt.Fprintf(e.out, promptMI8Pass, id)
		pass, err := readSecret(e)
		if err != nil && pass == "" {
			return mi8.CameraRef{}, false, ownerError(msgMI8NoAnswer)
		}
		return mi8.CameraRef{ID: id, XAddr: xaddr, User: user, Pass: strings.TrimRight(pass, "\r\n")}, true, nil
	}
}

// askCameras asks how many cameras, then each one's address, user and
// password: real-1, real-2, … in the order typed.
func askCameras(e *env) ([]mi8.CameraRef, error) {
	var n int
	for {
		_, _ = fmt.Fprint(e.out, promptMI8Count)
		line, err := readLine(e)
		if err != nil {
			return nil, err
		}
		if v, err := strconv.Atoi(line); err == nil && v >= 1 && v <= maxMI8Cameras {
			n = v
			break
		}
		e.printf(msgMI8BadCount)
	}
	cams := make([]mi8.CameraRef, 0, n)
	for i := 1; i <= n; i++ {
		c, _, err := askCamera(e, "real-"+strconv.Itoa(i), false)
		if err != nil {
			return nil, err
		}
		cams = append(cams, c)
	}
	return cams, nil
}

// yesNo is «da» or «nu».
func yesNo(b bool) string {
	if b {
		return msgMI8Yes
	}
	return msgMI8No
}

// cameraLabel is a camera's brand and model for the owner.
func cameraLabel(brand, model string) string {
	l := strings.TrimSpace(brand + " " + model)
	if l == "" {
		return msgMI8UnknownBrand
	}
	return l
}

// printProbes prints each camera's probe: brand and model, or why it cannot
// take part.
func printProbes(e *env, probes []mi8.Probe) {
	for _, p := range probes {
		switch {
		case p.Error == mi8.ProbeErrOnvifPort:
			e.printf(msgMI8ProbePort, p.Cam)
		case p.Error == mi8.ProbeErrAuth:
			e.printf(msgMI8ProbeAuth, p.Cam)
		case p.Error != "":
			e.printf(msgMI8ProbeDown, p.Cam)
		case !p.WithEvents():
			e.printf(msgMI8ProbeNoEvents, p.Cam, p.Brand, p.Model)
		default:
			e.printf(msgMI8ProbeOK, p.Cam, p.Brand, p.Model, yesNo(p.EventsService), yesNo(p.PullPoint))
		}
	}
}

// newMI8Run asks the cameras, probes them, selects those that take part
// (D3) and opens the run in the run state. It returns the cameras that take
// part, in RAM.
func newMI8Run(ctx context.Context, e *env, hub mi8.Hub, onePerBrand bool) ([]mi8.CameraRef, error) {
	cams, err := askCameras(e)
	if err != nil {
		return nil, err
	}
	probes, err := hub.Probe(ctx, cams)
	if err != nil && len(probes) == 0 {
		return nil, err
	}
	printProbes(e, probes)
	selected := mi8.Select(probes, onePerBrand)
	if len(selected) == 0 {
		return nil, ownerError(msgMI8NoCameras)
	}
	timing, _ := mi8TimingOf()
	now := e.now()
	st := &mi8.State{RunID: "mi8-" + now.UTC().Format("20060102-150405"), StartMs: now.UnixMilli(), OnePerBrand: onePerBrand,
		QuietGapSecs: int(mi8.DefaultEpisodeParams.QuietGap / time.Second), TargetMs: timing.target.Milliseconds(),
		Sessions: []mi8.Session{}, Cameras: []mi8.CameraState{}, TestSeams: e.seams}
	var keep []mi8.CameraRef
	var names []string
	byID := map[string]mi8.Probe{}
	for _, p := range selected {
		byID[p.Cam] = p
	}
	for _, c := range cams {
		p, ok := byID[c.ID]
		if !ok {
			if onePerBrand {
				for _, q := range probes {
					if q.Cam == c.ID && q.WithEvents() {
						e.printf(msgMI8NotSelected, c.ID)
					}
				}
			}
			continue
		}
		keep = append(keep, c)
		names = append(names, fmt.Sprintf("%s (%s)", c.ID, cameraLabel(p.Brand, p.Model)))
		st.Cameras = append(st.Cameras, mi8.CameraState{Camera: c.ID, Brand: p.Brand, Model: p.Model})
	}
	e.printf(msgMI8Selected, strings.Join(names, ", "))
	err = runstate.Update(e.statePath(), func(s *runstate.State) error {
		s.Open["MI8"] = st.Encode()
		delete(s.Pending, mi8Closed)
		return nil
	})
	return keep, err
}

// continueCameras is --continue's prompts (P49): each camera of the open
// run, listed with its brand and model from the run state, is asked its
// address, user and password again, and probed: a probe that names another
// brand or model is refused and the camera asked again; an empty address
// leaves the camera out of this session.
func continueCameras(ctx context.Context, e *env, hub mi8.Hub) ([]mi8.CameraRef, error) {
	st, err := runstate.Load(e.statePath())
	if err != nil {
		return nil, err
	}
	raw, ok := st.Open["MI8"]
	if !ok {
		return nil, ownerError(msgMI8NoRun)
	}
	run, err := mi8.Decode(raw)
	if err != nil {
		return nil, err
	}
	e.printf(msgMI8ContinueHours, run.Hours())
	for _, c := range run.Cameras {
		e.printf(msgMI8ContinueCam, c.Camera, cameraLabel(c.Brand, c.Model))
	}
	var cams []mi8.CameraRef
	for _, c := range run.Cameras {
		for {
			ref, ok, err := askCamera(e, c.Camera, true)
			if err != nil {
				return nil, err
			}
			if !ok {
				e.printf(msgMI8Skipped, c.Camera)
				break
			}
			probes, _ := hub.Probe(ctx, []mi8.CameraRef{ref})
			if len(probes) == 1 && probes[0].SameCamera(c.Brand, c.Model) {
				cams = append(cams, ref)
				break
			}
			if len(probes) == 1 && probes[0].Error != "" {
				printProbes(e, probes)
			}
			e.printf(msgMI8NotThisCamera, c.Camera, cameraLabel(c.Brand, c.Model))
		}
	}
	if len(cams) == 0 {
		return nil, ownerError(msgMI8NoCameras)
	}
	return cams, nil
}

// mi8FFmpeg is the motion reference's FFmpeg (P41): the test seam's, else
// the pinned build fetched next to uploadspike on the first run; empty when
// there is none (armv7, another platform, a failed download, no tar or xz),
// and MI8 uses the snapshot diff.
func mi8FFmpeg(ctx context.Context, e *env) string {
	if v, ok := os.LookupEnv(envFFmpeg); ok {
		if v == "-" {
			return ""
		}
		return v
	}
	self, err := os.Executable()
	if err != nil {
		e.printf(msgMI8NoFFmpeg)
		return ""
	}
	f, err := mi8.NewFetcher(filepath.Dir(self))
	if err != nil {
		e.printf(msgMI8NoFFmpeg)
		return ""
	}
	if _, err := os.Stat(f.Path()); err != nil {
		e.printf(msgMI8FFmpeg)
	}
	path, err := f.Fetch(ctx)
	if err != nil {
		e.printf(msgMI8NoFFmpeg)
		return ""
	}
	return path
}

// readCameraLines reads the cameras that the parent wrote on the detached
// copy's stdin, until its end.
func readCameraLines(e *env) ([]mi8.CameraRef, error) {
	var cams []mi8.CameraRef
	sc := bufio.NewScanner(e.in)
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	for sc.Scan() {
		var c struct{ ID, XAddr, User, Pass string }
		if json.Unmarshal(sc.Bytes(), &c) != nil || c.ID == "" {
			return nil, errors.New("mi8: a camera line is not valid")
		}
		cams = append(cams, mi8.CameraRef{ID: c.ID, XAddr: c.XAddr, User: c.User, Pass: c.Pass})
	}
	if len(cams) == 0 {
		return nil, errors.New("mi8: no camera on stdin")
	}
	return cams, sc.Err()
}

// runMI8Session runs one MI8 session with cams until the run's observed
// time reaches 72 h or ctx ends. It begins a session in the run state (the
// stretch since the last one is a reboot or mi8-stopped gap), writes the
// heartbeat, the progress and the envelope every period, and at the target
// closes the run: the last envelope, the known-secrets scan with every
// typed address, user and password (P45), and the run state's close.
func runMI8Session(ctx context.Context, e *env, cams []mi8.CameraRef, ffmpeg string) error {
	timing, err := mi8TimingOf()
	if err != nil {
		return err
	}
	pid := os.Getpid()
	procStart, _ := runstate.ProcStartMs(pid)
	bootID, _ := runstate.BootID()
	var state *mi8.State
	if timing.firstBeat > 0 {
		select {
		case <-time.After(timing.firstBeat):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err = runstate.Update(e.statePath(), func(s *runstate.State) error {
		raw, ok := s.Open["MI8"]
		if !ok {
			return ownerError(msgMI8NoRun)
		}
		st, err := mi8.Decode(raw)
		if err != nil {
			return err
		}
		st.BeginSession(e.now().UnixMilli(), bootID, runstate.SameBoot)
		st.PID, st.ProcStartMs = pid, procStart
		st.TestSeams = st.TestSeams || e.seams
		s.Open["MI8"] = st.Encode()
		state = st
		return nil
	})
	if err != nil {
		return err
	}
	host, info := hostName(ctx, e)
	meta := mi8.Meta{Host: host, Set: host, OS: runtime.GOOS, Storage: "unknown", TestSeams: e.seams}
	if info != nil {
		meta.PiModel, meta.Storage = info.PiModel, info.Storage
	}
	if st, err := runstate.Load(e.statePath()); err == nil {
		meta.SdConfirmation = st.Sd
	}
	write := func(s mi8.State) error {
		if err := runstate.Update(e.statePath(), func(rs *runstate.State) error {
			rs.Open["MI8"] = s.Encode()
			return nil
		}); err != nil {
			return err
		}
		return results.Write(e.resultsDir(), s.Envelope(meta, e.now().UnixMilli()))
	}
	cfg := mi8.Mi8Config{Hub: e.mi8Hub(), Start: startWithUs, Cameras: cams, State: state, FFmpeg: ffmpeg,
		Progress: write, ProgressEvery: timing.progress, FirstFrame: timing.firstFrame, Now: e.now}
	if e.fast {
		cfg.Resubscribe = time.Second
	}
	_, runErr := mi8.Run(ctx, cfg)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if runErr != nil {
		return runErr
	}
	return closeMI8(context.WithoutCancel(ctx), e, cams)
}

// closeMI8 closes the run at its target: the known-secrets scan over
// results/ with every typed address, user and password (P45), then the run
// leaves the run state, with a note for status.
func closeMI8(ctx context.Context, e *env, cams []mi8.CameraRef) error {
	var values []string
	for _, c := range cams {
		values = append(values, c.Secrets()...)
	}
	note := "ok"
	if f := scanResults(ctx, e, values); f != nil {
		e.printf("%s", f.line)
		note = f.reason
	}
	if err := runstate.Update(e.statePath(), func(s *runstate.State) error {
		delete(s.Open, "MI8")
		s.Pending[mi8Closed] = json.RawMessage(strconv.Quote(note))
		return nil
	}); err != nil {
		return err
	}
	e.printf(msgMI8Done)
	return nil
}

// runMI8Detached is the detached copy (Linux): it checks that it holds
// MI8's lock (descriptor 3, from its parent), ignores SIGHUP, reads the
// cameras from its stdin into RAM until the parent closes it, and runs the
// session until the target or SIGTERM.
func runMI8Detached(ctx context.Context, e *env, ffmpeg string) error {
	lock := os.NewFile(3, runstate.MI8Lock)
	if lock == nil || !runstate.HoldsLock(lock) {
		return exitError(1)
	}
	ignoreHangup()
	cams, err := readCameraLines(e)
	if err != nil {
		return err
	}
	closeStdin()
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = runMI8Session(sigCtx, e, cams, ffmpeg)
	_ = lock.Close()
	return err
}

// mi8StatusLines are MI8's lines for status and status.html: the observed
// hours and each camera's episodes while it runs; that it stopped when its
// process ended (reboot, closed window); its close.
func mi8StatusLines(st *runstate.State) []string {
	raw, ok := st.Open["MI8"]
	if !ok {
		if _, closed := st.Pending[mi8Closed]; closed {
			return []string{msgMI8Done}
		}
		return nil
	}
	run, err := mi8.Decode(raw)
	if err != nil {
		return nil
	}
	var lines []string
	if runstate.Alive(run.PID, run.ProcStartMs) {
		lines = append(lines, fmt.Sprintf(msgMI8Progress, run.Hours()))
	} else {
		lines = append(lines, msgMI8Stopped, fmt.Sprintf(msgMI8StoppedHours, run.Hours()))
	}
	for _, c := range run.Cameras {
		r := c.Result()
		if r.SubscribeFailed {
			lines = append(lines, fmt.Sprintf(msgMI8CameraFailed, c.Camera, cameraLabel(c.Brand, c.Model)))
			continue
		}
		lines = append(lines, fmt.Sprintf(msgMI8Camera, c.Camera, cameraLabel(c.Brand, c.Model), r.Episodes, r.Matched, r.SilentLosses))
	}
	return lines
}
