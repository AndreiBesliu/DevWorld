// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package mi3 is the hardware-tier measurement MI3 (G12, §16.4, HUB-32,
// G20): Judge, the gate's one rule (C17), which reads the MI3 envelopes and,
// for §4.8's cold-boot row, MI1's reboot runs (coldboot.go); the budgets of
// each tier (budgets.go); and Run, recspike's capacity run on the host under
// test: ipcam-hub records the host's recommended cameras from the
// controller while the tier's loads run beside it as child processes
// (loads.go), each a proxy of a hub function, and the host's CPU (cpu_*.go),
// RAM, disk and fsyncs (disk.go) and the live tiles' latency, measured by
// the controller (latency.go), are judged against §4.8; on a failure the
// count steps down (runSteps).
package mi3

import (
	"bufio"
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
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

// The lengths of MI3's runs (G20): the hour at a count, and the 15-min runs
// of the step-down.
const (
	FullMinutes  = 60
	ShortMinutes = 15
)

// maxAttempts bounds the attempts of one step that the controller's pacing
// invalidates (R07).
const maxAttempts = 3

// sixteenCameras is §4.8's H3 count with 6 cores or more (R07).
const sixteenCameras = 16

// last reports whether the steps end after s: its attempts were all
// invalid, it passed a full hour, or it failed at 1 camera.
func last(s results.Mi3Step) bool {
	return !s.Valid || (s.Pass && s.Minutes == FullMinutes) || (!s.Pass && s.Cameras == 1)
}

// measureFunc measures one step: cameras recorded with the tier's loads
// for minutes, the step's attempt-th try.
type measureFunc func(ctx context.Context, cameras, minutes, attempt int) (Figures, error)

// runSteps runs MI3 on tier from cameras, the host's recommended count, with
// measure (G20, R07): the first hour at cameras; when it fails, the count
// halves in 15-min runs until one passes, and a full hour confirms it,
// halving again if the confirmation fails; a failure at 1 camera ends the
// steps, since the number never goes below 1 (O16). A step whose controller
// sent unevenly is redone, at most maxAttempts times, and never judged;
// after the last, the steps stop and the host's MI3 has no verdict. Every
// attempt goes into Steps, and the envelope's figures are the first hour's
// last attempt. With sixteen (a PC with 6 cores or more), one hour at 16
// cameras follows the steps that settled, whatever their count, and goes
// into Sixteen, as information; steps that stopped are run again whole,
// with their hour at 16.
//
// d is nil only when measure failed before an attempt of the first hour was
// valid: err is that error, and there is nothing to judge. After a valid
// first hour, an error of measure stops the steps, err is it, and d keeps
// what they measured, with StepsStopped saying which step stopped them: the
// first hour decides G12 (G27). An error of the hour at 16 cameras is
// sixteenErr, with d.SixteenStopped and no Sixteen: it is information too.
func runSteps(ctx context.Context, tier string, cameras int, sixteen bool, measure measureFunc) (d *results.Mi3Data, sixteenErr, err error) {
	var steps []results.Mi3Step
	// try measures n cameras for minutes until an attempt is valid, at most
	// maxAttempts times, and returns the last attempt; after an error, the
	// attempt that it stopped, unmeasured.
	try := func(n, minutes int, record bool) (results.Mi3Step, Figures, error) {
		var s results.Mi3Step
		var f Figures
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			var err error
			if f, err = measure(ctx, n, minutes, attempt); err != nil {
				return results.Mi3Step{Cameras: n, Minutes: minutes, Attempt: attempt}, f, err
			}
			s = results.Mi3Step{Cameras: n, Minutes: minutes, Attempt: attempt, Valid: Valid(f), PacingP99Ms: f.PacingP99Ms, SkippedAUs: f.SkippedAUs}
			s.Pass = s.Valid && passes(tier, n, f)
			if record {
				steps = append(steps, s)
			}
			if s.Valid {
				break
			}
		}
		return s, f, nil
	}

	// try stops at the first valid attempt, so an error here comes before
	// one.
	s, f, err := try(cameras, FullMinutes, true)
	if err != nil {
		return nil, nil, err
	}
	data := dataOf(tier, cameras, f)
	d = &data
	for !last(s) {
		n, minutes := s.Cameras, FullMinutes // confirm the count that passed
		if !s.Pass {
			n, minutes = max(1, s.Cameras/2), ShortMinutes
		}
		if s, _, err = try(n, minutes, true); err != nil {
			d.Steps, d.StepsStopped = steps, stopText(s, err)
			return d, nil, err
		}
	}
	d.Steps = steps
	if sixteen && s.Valid && cameras < sixteenCameras {
		six, _, sixErr := try(sixteenCameras, FullMinutes, false)
		if sixErr != nil {
			d.SixteenStopped = stopText(six, sixErr)
			return d, sixErr, nil
		}
		d.Sixteen = &six
	}
	return d, nil, nil
}

// stepFailure is an error of a step whose text names nothing of the host,
// no path, address or name, so that the envelope may say it (PH0-22).
type stepFailure struct{ text string }

func (e *stepFailure) Error() string { return e.text }

// hubExited is ipcam-hub's exit, with code, during a step.
func hubExited(code int) error {
	return &stepFailure{fmt.Sprintf("ipcam-hub s-a oprit în timpul pasului (codul %d)", code)}
}

// loadEnded is the end of the load kind during a step.
func loadEnded(kind string) error {
	return &stepFailure{fmt.Sprintf("încărcarea %s s-a oprit în timpul pasului", kind)}
}

// stopText is, for the envelope, why step s could not be measured: err's
// text when it is a stepFailure, the owner's stop, or else where the error
// is (PH0-22).
func stopText(s results.Mi3Step, err error) string {
	why := "eroarea este pe ecranul gazdei și în jurnalele pasului"
	var failure *stepFailure
	switch {
	case errors.As(err, &failure):
		why = failure.text
	case errors.Is(err, context.Canceled):
		why = "proba a fost oprită înainte de final"
	}
	return stepHead(s) + ": nu s-a putut măsura; " + why
}

// TierFor is the host's tier (§4.8): H3 on the PC; on a Pi, its model's
// tier, or TierUnknown for a model that the table lacks (C24).
func TierFor(h mi1.HostState) string {
	if h.PiModel == nil {
		return TierH3
	}
	switch h.PiModel.Tier {
	case TierH1, TierH1b, TierH1c:
		return h.PiModel.Tier
	}
	return TierUnknown
}

// Options are what MI3's run on a host needs.
type Options struct {
	// Host is the host as the envelope describes it, with its recommended
	// camera count (C24) and, on an SD card, the owner's confirmation (G16).
	// Its Cores6Plus, diag host's, gives a PC an hour at 16 cameras (R07).
	Host mi1.HostState
	// Controller is reccontroller's link address, on the other device.
	Controller string
	// Hub is ipcam-hub; MediaMTX is the fork, "" for the one next to Hub;
	// Self is recspike, which runs the loads (`recspike mi3-load <kind>`).
	Hub, MediaMTX, Self string
	// Results is recspike's published results folder; Work its private
	// one, which holds the steps' recordings, keys and logs.
	Results, Work string
	// Out is the owner's screen.
	Out io.Writer

	// Window is how long a step of minutes measures; nil is that many
	// minutes. FixtureBytes is the size of the segment that the playback,
	// the export and the upload use; 0 is 5 min of a 4 Mbps main stream.
	// Tests shorten both.
	Window       func(minutes int) time.Duration
	FixtureBytes int64
}

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
	// fixtureBytes is 5 min of a 4 Mbps main stream.
	fixtureBytes = 300 * cameraBps / 8
)

// Run is MI3 on this host (G12, HUB-32, G20): it records the host's
// recommended cameras, the main streams of reccontroller on the other
// device, half of them encrypted (the even ones: every count keeps a clear
// camera, which the disk budget needs, R02), while the tier's loads run as
// child processes beside ipcam-hub: the live sessions, a playback, an
// export and the encrypted upload. Each step measures the machine's CPU,
// each process's with its children, the memory's peak, the recordings'
// disk ratio and part fsyncs, and the disk writers' queue peak, and takes
// the controller's pacing and the live tiles' latency (R07); runSteps
// steps the count down on a failure. It keeps the host awake (R49) and
// writes the envelope MI3/<host>-mi3/<run id>.json, also when a step after
// the first hour could not be measured (finish). Every line it prints is
// Romanian.
func Run(ctx context.Context, o Options) error {
	tier := TierFor(o.Host)
	cameras := max(o.Host.Cameras, 1)
	dir := filepath.Join(o.Work, "mi3")
	// The steps of an earlier run, which an interruption left: their
	// recordings would only take space.
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("nu pot șterge probele MI3 de dinainte: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("nu pot crea dosarul de lucru: %w", err)
	}
	l, err := dialController(ctx, o.Controller, o.Host.Pseudonym)
	if err != nil {
		return err
	}
	defer func() {
		if err := l.close(o.Results, o.Host.Pseudonym == "pi"); err != nil {
			o.say("Rezultatele nu au ajuns la PC (%v): trimiteți-le la final cu recspike push.", err)
		}
	}()
	o.say("Pregătesc segmentul de 5 min al redării, exportului și upload-ului…")
	fx, err := makeFixture(filepath.Join(dir, "fixture"), o.fixtureBytes())
	defer func() { _ = os.RemoveAll(fx.dir) }()
	if err != nil {
		return fmt.Errorf("nu pot pregăti segmentul de test: %w", err)
	}
	release, err := mi1.KeepAwake()
	if err != nil {
		return fmt.Errorf("nu pot ține dispozitivul treaz: %w", err)
	}
	defer release()

	o.say("Proba MI3 începe: nivelul %s, %s, %d sesiuni live, redare, export și upload de %s Mbps, în paralel, timp de 1 h. "+
		"Dacă ora pică, proba continuă cu pași de 15 min la jumătate din camere, iar numărul care trece se confirmă cu încă o oră. "+
		"Lăsați dispozitivul pornit și nefolosit până la final.", tier, camerasText(cameras), Sessions(tier),
		decimal(float64(UploadBps(tier, cameras))/1e6, -1))
	r := &stepRunner{o: o, l: l, fx: fx, tier: tier, dir: dir}
	d, sixteenErr, err := runSteps(ctx, tier, cameras, o.Host.Cores6Plus && tier == TierH3, r.measure)
	if d == nil {
		return err
	}
	return o.finish(*d, sixteenErr, err)
}

// finish writes d, what the steps measured, as the run's envelope, also
// when stepErr stopped the steps after the first hour, which decides G12
// (G27), and then returns stepErr. sixteenErr, the hour at 16 cameras', is
// information: it is said on the screen, and the run succeeds.
func (o Options) finish(d results.Mi3Data, sixteenErr, stepErr error) error {
	d.Proxies = Proxies
	set := SetFor(o.Host.Pseudonym)
	e := results.Envelope{Schema: results.Schema, Code: "MI3", Set: set, Host: o.Host.Pseudonym, OS: o.Host.OS,
		PiModel: o.Host.PiModel, Storage: o.Host.Storage, SdConfirmation: o.Host.SdConfirmation, MI3: &d}
	for n := 1; ; n++ {
		e.RunID = fmt.Sprintf("%s-%d", set, n)
		err := results.Create(o.Results, e)
		if errors.Is(err, results.ErrExists) {
			continue
		}
		if err != nil {
			return errors.Join(stepErr, fmt.Errorf("nu pot scrie rezultatul probei: %w", err))
		}
		break
	}
	if stepErr != nil {
		o.say("Proba MI3 s-a oprit înainte de final; ce s-a măsurat este în rezultatul %s. Pașii: %s.", e.RunID, stepsSummary(d.Steps))
		return fmt.Errorf("pașii de coborâre nu s-au încheiat: %w. Ora la %s rămâne măsurată; porniți din nou proba ca numărul recomandat să se stabilească",
			stepErr, camerasText(d.Cameras))
	}
	o.say("Proba MI3 s-a încheiat: rezultatul este %s. Pașii: %s.", e.RunID, stepsSummary(d.Steps))
	if last := d.Steps[len(d.Steps)-1]; !last.Valid {
		o.say("Controlorul a trimis neuniform de %d ori la rând, deci pasul nu are rezultat: verificați controlorul și rețeaua, apoi porniți din nou proba.", maxAttempts)
	}
	if sixteenErr != nil {
		o.say("Ora la 16 camere nu s-a putut măsura (%v): în raport, 16 camere rămâne o estimare. Rezultatul de la %s nu depinde de ea.",
			sixteenErr, camerasText(d.Cameras))
	}
	return nil
}

// stepsSummary is the steps as the owner's screen shows them.
func stepsSummary(steps []results.Mi3Step) string {
	parts := make([]string, len(steps))
	for i, s := range steps {
		parts[i] = stepText(s)
	}
	return strings.Join(parts, "; ")
}

func (o Options) say(format string, args ...any) {
	if o.Out != nil {
		_, _ = fmt.Fprintf(o.Out, format+"\n", args...)
	}
}

func (o Options) window(minutes int) time.Duration {
	if o.Window != nil {
		return o.Window(minutes)
	}
	return time.Duration(minutes) * time.Minute
}

func (o Options) fixtureBytes() int64 {
	if o.FixtureBytes > 0 {
		return o.FixtureBytes
	}
	return fixtureBytes
}

// fixture is the 5-min segment of the playback, the export and the upload
// (G20), in random bytes: the cost of age does not depend on the content.
// plain is the clear segment, segment its age encryption to identity's key.
type fixture struct {
	dir, plain, segment, identity string
}

// makeFixture writes the fixture of size bytes in dir.
func makeFixture(dir string, size int64) (fixture, error) {
	fx := fixture{dir: dir, plain: filepath.Join(dir, "segment.mp4"), segment: filepath.Join(dir, "segment.mp4.age"), identity: filepath.Join(dir, "fixture.key")}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fx, err
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return fx, err
	}
	if err := os.WriteFile(fx.identity, []byte(id.String()+"\n"), 0o600); err != nil {
		return fx, err
	}
	var seed [32]byte
	if _, err := cryptorand.Read(seed[:]); err != nil {
		return fx, err
	}
	if err := writeFile(fx.plain, func(w io.Writer) error {
		_, err := io.CopyN(w, rand.NewChaCha8(seed), size)
		return err
	}); err != nil {
		return fx, err
	}
	return fx, writeFile(fx.segment, func(w io.Writer) error {
		in, err := os.Open(fx.plain)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		enc, err := age.Encrypt(w, id.Recipient())
		if err != nil {
			return err
		}
		if _, err := io.Copy(enc, in); err != nil {
			return err
		}
		return enc.Close()
	})
}

// writeFile writes path, 0600, with write.
func writeFile(path string, write func(io.Writer) error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // the run's own folder
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	err = write(bw)
	if err == nil {
		err = bw.Flush()
	}
	return errors.Join(err, f.Close())
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
		r.n, decimal(f.CPUPctTotal, 1), decimal(f.RAMMb, 0), decimal(f.LatencyP95Ms, 0), f.PacingP99Ms, f.SkippedAUs, verdict)
	return f, nil
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

// subBps is a camera's sub stream, 512 kbps.
const subBps = 512_000

// figures are the step's figures (budgets.go): the CPU of the machine and
// of each process from start to end, the live load's split over its tiles
// by their bitrates (Proxies), the upload's per 10 Mbps at the rate it
// reached, ipcam-hub's with MediaMTX's per camera and core; the memory's
// peak; the recordings' disk ratio and part fsyncs; the disk writers'
// queue peak from ipcam-hub's log; and the controller's pacing and latency.
func (s *stepRun) figures(start, end usage, peak uint64, res link.Mi3Result, upload LoadReport, riG age.Identity) (Figures, error) {
	if res.LatencySamples == 0 {
		return Figures{}, errors.New("sesiunile live nu au primit niciun cadru")
	}
	if upload.Seconds <= 0 || upload.Bytes <= 0 {
		return Figures{}, errors.New("upload-ul nu a trimis nimic")
	}
	disk, err := measureDisk(filepath.Join(s.dir, "data", "rec"), riG)
	if err != nil {
		return Figures{}, err
	}
	log, err := os.Open(filepath.Join(s.dir, "ipcam-hub.log"))
	if err != nil {
		return Figures{}, err
	}
	defer func() { _ = log.Close() }()
	queuePeak, err := diskQueuePeak(log)
	if err != nil {
		return Figures{}, err
	}
	machine := func(kind string) float64 { return end.pctOfMachine(start, end.cpuSince(start, kind)) }
	live := machine(LoadLive) / float64(Sessions(s.r.tier)*(s.cameras*subBps+cameraBps))
	uploaded := float64(upload.Bytes*8) / upload.Seconds
	return Figures{
		CPUPctTotal:        end.pctOfMachine(start, end.busy-start.busy),
		TileSubPct:         live * subBps,
		TileMainPct:        live * cameraBps,
		EncUploadPct:       machine(LoadUpload) * uploadBps / uploaded,
		IngestPerCameraPct: end.pctOfCore(start, end.cpuSince(start, "hub")) / float64(s.cameras),
		RAMMb:              float64(peak) / (1 << 20),
		DiskRatio:          disk.ratio,
		FsyncPerSecPerFile: disk.fsyncPerSec,
		LatencyP95Ms:       res.LatencyP95Ms,
		PacingP99Ms:        res.PacingP99Ms,
		SkippedAUs:         res.SkippedAUs,
		DiskQueuePeakBytes: queuePeak,
	}, nil
}
