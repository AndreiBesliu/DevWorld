// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The pace of a step.
const (
	// recordingWait bounds the wait for every camera's first part.
	recordingWait = 2 * time.Minute
	// loadStartWait bounds a load's start, the live load's RTSP pulls and
	// answers included.
	loadStartWait = time.Minute
	// liveWait bounds the binds of every live tile.
	liveWait = 2 * time.Minute
	// resultWait bounds the controller's answers that need no wait.
	resultWait = 30 * time.Second
	// memoryEvery is how often a step reads the processes' memory.
	memoryEvery = 5 * time.Second
)

// previousDir is the folder under work/ that keeps the step logs of the
// run before (keepStepLogs).
const previousDir = "mi3-anterior"

// stepLogs are the logs that each step leaves in its folder.
var stepLogs = []string{"ipcam-hub.log", "loads.log"}

// keepStepLogs moves the step logs in dir, the folder of the run before,
// to prev, in place of what prev held: one generation, as MI2 keeps its
// last log. The screen and the envelope of a run that stopped name those
// logs, and the owner then starts the probe again. A dir without any step
// log, as after a start that stopped before its first step, leaves prev as
// it is.
func keepStepLogs(dir, prev string) error {
	steps, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var logs []string // relative to dir
	for _, st := range steps {
		for _, name := range stepLogs {
			rel := filepath.Join(st.Name(), name)
			if info, err := os.Stat(filepath.Join(dir, rel)); err == nil && info.Mode().IsRegular() {
				logs = append(logs, rel)
			}
		}
	}
	if len(logs) == 0 {
		return nil
	}
	if err := os.RemoveAll(prev); err != nil {
		return err
	}
	for _, rel := range logs {
		to := filepath.Join(prev, rel)
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(dir, rel), to); err != nil {
			return err
		}
	}
	return nil
}

// stepRunner runs the steps of one MI3 run.
type stepRunner struct {
	o    Options
	l    *controllerLink
	fx   fixture
	tier string
	dir  string
	n    int // the steps so far
}

// streams are the main streams of cameras, and the live tiles: every
// camera's sub stream, then cam01's main (G20).
func streams(cameras int) (mains, tiles []string) {
	for i := 1; i <= cameras; i++ {
		mains = append(mains, fmt.Sprintf("cam%02d_main", i))
		tiles = append(tiles, fmt.Sprintf("cam%02d_sub", i))
	}
	return mains, append(tiles, mains[0])
}

// measure is one step of the run: cameras recorded for minutes, the
// attempt-th try, with the tier's loads beside them.
func (r *stepRunner) measure(ctx context.Context, cameras, minutes, attempt int) (Figures, error) {
	if err := r.room(cameras, minutes); err != nil {
		return Figures{}, err
	}
	r.n++
	name := fmt.Sprintf("%02d-%dcam-%dmin-%d", r.n, cameras, minutes, attempt)
	dir := filepath.Join(r.dir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Figures{}, err
	}
	r.o.say("Pasul %d: %s, %d min, încercarea %d.", r.n, camerasText(cameras), minutes, attempt)
	s := &stepRun{r: r, runID: SetFor(r.o.Host.Pseudonym) + "-" + name, dir: dir, cameras: cameras, minutes: minutes}
	f, err := s.run(ctx)
	s.cleanup()
	// The recordings are measured, or of a step that failed: they would only
	// take space. The logs stay.
	if err := os.RemoveAll(filepath.Join(dir, "data")); err != nil {
		r.o.say("Înregistrările pasului %d nu s-au putut șterge (%v); se șterg la următorul recspike mi3.", r.n, err)
	}
	if err != nil {
		return Figures{}, fmt.Errorf("pasul %d (%s): %w; jurnalele sunt în %s", r.n, camerasText(cameras), err, dir)
	}
	verdict := "nevalid (ritmul controlorului), se reface"
	if Valid(f) {
		verdict = "trece"
		if !passes(r.tier, cameras, f) {
			verdict = "pică"
		}
	}
	r.o.say("Pasul %d: CPU total %s %%, RAM %s MB, latență p95 %s ms, ritm p99 %d ms, cadre sărite %d: %s.",
		r.n, results.Decimal(f.CPUPctTotal, 1), results.Decimal(f.RAMMb, 0), results.Decimal(f.LatencyP95Ms, 0), f.PacingP99Ms, f.SkippedAUs, verdict)
	return f, nil
}

// spaceMargin is the free space that a step leaves beside its recordings:
// 2 GiB, as MI1's runs on the Pi (G14).
const spaceMargin = 2 << 30

// stepBytes estimates what a step of cameras writes in window: cameras × 4
// Mbps × window × 1.03, §4.8's disk budget, plus spaceMargin.
func stepBytes(cameras int, window time.Duration) uint64 {
	written := int64(cameras) * cameraBps / 8 * window.Milliseconds() / 1000 * 103 / 100
	return uint64(written) + spaceMargin //nolint:gosec // a step's bytes, never negative
}

// room refuses a step of cameras for minutes that the free space cannot
// hold: a full disk would fail it midway. The refusal is a step's error,
// before anything starts (C1-R21): after a valid first hour, the envelope
// keeps that hour and says, in stepFailure's words, how much space the step
// needed.
func (r *stepRunner) room(cameras, minutes int) error {
	free, err := r.o.freeBytes()
	if err != nil {
		return fmt.Errorf("nu pot citi spațiul liber de pe disc: %w", err)
	}
	if need := stepBytes(cameras, r.o.window(minutes)); free < need {
		gb := func(n uint64) string { return results.Decimal(float64(n)/1e9, 1) + " GB" }
		return fmt.Errorf("pasul cu %s, %d min nu a pornit: %w; eliberați spațiu, apoi porniți din nou proba", camerasText(cameras), minutes,
			&stepFailure{fmt.Sprintf("pe disc sunt %s liberi, iar pasul are nevoie de %s", gb(free), gb(need))})
	}
	return nil
}

// stepRun is one step's processes and what undoes them, last first.
type stepRun struct {
	r                *stepRunner
	runID, dir       string
	cameras, minutes int
	undo             []func()
}

// later adds f to what cleanup undoes.
func (s *stepRun) later(f func()) { s.undo = append(s.undo, f) }

// cleanup undoes what the step started and has not undone itself, as
// after an error.
func (s *stepRun) cleanup() {
	for i := len(s.undo) - 1; i >= 0; i-- {
		s.undo[i]()
	}
	s.undo = nil
}

// run runs the step and measures it.
func (s *stepRun) run(ctx context.Context) (Figures, error) {
	o, l := s.r.o, s.r.l
	mains, tiles := streams(s.cameras)
	logFile, err := os.OpenFile(filepath.Join(s.dir, "ipcam-hub.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the step's own folder
	if err != nil {
		return Figures{}, err
	}
	s.later(func() { _ = logFile.Close() })
	loadLog, err := os.OpenFile(filepath.Join(s.dir, "loads.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the step's own folder
	if err != nil {
		return Figures{}, err
	}
	s.later(func() { _ = loadLog.Close() })
	config, riG, err := s.writeRun(mains)
	if err != nil {
		return Figures{}, fmt.Errorf("nu pot pregăti pasul: %w", err)
	}

	// The controller first: it marks its pacing and makes the live peers.
	sessions := Sessions(s.r.tier)
	m, err := l.request(ctx, link.Mi3Start{RunID: s.runID, Sessions: sessions, Tiles: tiles}, s.runID, "mi3_offers", resultWait)
	if err != nil {
		return Figures{}, err
	}
	offers := m.(link.Mi3Offers).Offers
	ended := false
	s.later(func() {
		if !ended { // the controller closes the live peers at the end
			_, _ = l.request(context.WithoutCancel(ctx), link.Mi3End{RunID: s.runID}, s.runID, "mi3_result", resultWait)
		}
	})

	hub, err := hubproc.Start(hubproc.Options{Binary: o.Hub, Config: config, Stderr: logFile})
	if err != nil {
		return Figures{}, fmt.Errorf("nu pot porni ipcam-hub: %w", err)
	}
	hubStopped := false
	stopHub := func() {
		hubStopped = true
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mi1.StopTimeout)
		defer cancel()
		if err := hub.Stop(stopCtx); err != nil {
			o.say("ipcam-hub nu s-a oprit curat: %v", err)
		}
	}
	s.later(func() {
		if !hubStopped {
			stopHub()
		}
	})
	waitCtx, cancel := context.WithTimeout(ctx, recordingWait)
	_, err = hub.WaitRecording(waitCtx)
	cancel()
	if err != nil {
		return Figures{}, fmt.Errorf("ipcam-hub nu a început să înregistreze toate camerele: %w", err)
	}

	loads, err := s.startLoads(ctx, tiles, offers, loadLog)
	if err != nil {
		return Figures{}, err
	}
	if _, err := l.request(ctx, link.Mi3Answers{RunID: s.runID, Answers: loads[LoadLive].first.Answers}, s.runID, "mi3_live", liveWait); err != nil {
		return Figures{}, err
	}

	roots := map[string]int{"hub": hub.Pid()}
	for kind, ld := range loads {
		roots[kind] = ld.cmd.Process.Pid
	}
	start, peak, end, err := s.window(ctx, roots, hub, loads)
	if err != nil {
		return Figures{}, err
	}
	reports := map[string]LoadReport{}
	for kind, ld := range loads {
		if reports[kind], err = ld.stop(); err != nil {
			return Figures{}, fmt.Errorf("încărcarea %s: %w", kind, err)
		}
	}
	m, err = l.request(ctx, link.Mi3End{RunID: s.runID}, s.runID, "mi3_result", resultWait)
	if err != nil {
		return Figures{}, err
	}
	ended = true
	res := m.(link.Mi3Result)
	stopHub()
	if err := logFile.Close(); err != nil {
		return Figures{}, err
	}
	return s.figures(start, end, peak, res, reports[LoadUpload], riG)
}

// writeRun writes the step's run.json: each main stream a job that pulls
// the controller's camera, the even ones encrypted to a fresh RI_g and RK
// (C11, PH0-22), whose RI_g identity is a 0600 file beside it, which the
// disk measurement decrypts with, and whose RK's identity is never kept.
func (s *stepRun) writeRun(mains []string) (config string, riG *age.X25519Identity, err error) {
	cfg := hubproc.Config{DataDir: filepath.Join(s.dir, "data"), MediaMTX: s.r.o.MediaMTX, SDCardConfirmed: s.r.o.Host.SdConfirmation != nil}
	if riG, err = age.GenerateX25519Identity(); err != nil {
		return "", nil, err
	}
	rk, err := age.GenerateX25519Identity()
	if err != nil {
		return "", nil, err
	}
	cfg.IdentityFile = filepath.Join(s.dir, "ri_g.key")
	if err := os.WriteFile(cfg.IdentityFile, []byte(riG.String()+"\n"), 0o600); err != nil {
		return "", nil, err
	}
	for i, stream := range mains {
		url, err := s.r.l.cameraURL(stream)
		if err != nil {
			return "", nil, err
		}
		cam, profile, _ := strings.Cut(stream, "_")
		job := hubproc.Job{Cam: cam, Profile: profile, RestreamURL: url}
		if i%2 == 1 { // cam02, cam04, …
			job.Recipients = []string{riG.Recipient().String(), rk.Recipient().String()}
		}
		cfg.Jobs = append(cfg.Jobs, job)
	}
	config = filepath.Join(s.dir, "run.json")
	return config, riG, hubproc.WriteConfig(config, cfg)
}

// startLoads starts the four loads, the live one answering offers over
// the tiles' streams.
func (s *stepRun) startLoads(ctx context.Context, tiles, offers []string, stderr io.Writer) (map[string]*load, error) {
	sources := map[string]string{}
	for _, tile := range tiles {
		url, err := s.r.l.cameraURL(tile)
		if err != nil {
			return nil, err
		}
		sources[splitTile(tile)] = url
	}
	fx := s.r.fx
	specs := map[string]LoadSpec{
		LoadLive:     {Sources: sources, Offers: offers},
		LoadPlayback: {Segment: fx.segment, Identity: fx.identity},
		LoadExport:   {Segment: fx.segment, Identity: fx.identity},
		LoadUpload:   {Plain: fx.plain, URL: s.r.l.sinkURL(), SinkCert: s.r.l.session.SinkCert, Bps: UploadBps(s.r.tier, s.cameras)},
	}
	loads := map[string]*load{}
	for _, kind := range Loads {
		ld, err := startLoad(ctx, s.r.o.Self, kind, specs[kind], stderr)
		if err != nil {
			return nil, fmt.Errorf("încărcarea %s nu a pornit: %w", kind, err)
		}
		loads[kind] = ld
		s.later(func() { _, _ = ld.stop() })
	}
	return loads, nil
}

// window measures the step: the CPU times at its start and end, and the
// memory's peak in between, read every memoryEvery; ipcam-hub or a load
// that ends meanwhile fails the step.
func (s *stepRun) window(ctx context.Context, roots map[string]int, hub *hubproc.Hub, loads map[string]*load) (start usage, peak uint64, end usage, err error) {
	if start, err = sampleUsage(roots); err != nil {
		return start, 0, end, err
	}
	peak = memory(roots)
	done := time.NewTimer(s.r.o.window(s.minutes))
	defer done.Stop()
	tick := time.NewTicker(memoryEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return start, peak, end, ctx.Err()
		case <-hub.Exited():
			return start, peak, end, hubExited(hub.ExitCode())
		case <-tick.C:
			for kind, ld := range loads {
				if ld.ended() {
					return start, peak, end, loadEnded(kind)
				}
			}
			peak = max(peak, memory(roots))
		case <-done.C:
			peak = max(peak, memory(roots))
			end, err = sampleUsage(roots)
			return start, peak, end, err
		}
	}
}
