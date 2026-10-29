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
// count steps down (runSteps). Each step's processes run in step.go, the
// loads' segment is written in fixture.go, and a step's figures are
// computed in figures.go.
package mi3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
// into Sixteen, as information; after steps that stopped, none runs
// (C1-R20): the owner runs the probe again.
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
// text when it is a stepFailure, the owner's stop (Ctrl+C), or else where
// the error is (PH0-22): in the step logs (errorLog), which stay in
// work/mi3, and in work/mi3-anterior once the probe starts again
// (keepStepLogs).
func stopText(s results.Mi3Step, err error) string {
	why := "eroarea este în jurnalele pasului (work/mi3; după o nouă pornire, work/" + previousDir + ")"
	var failure *stepFailure
	switch {
	case errors.As(err, &failure):
		why = failure.text
	case errors.Is(err, context.Canceled):
		why = "proprietarul a oprit proba"
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
	// FreeBytes is the space free under Work; nil reads it from the file
	// system (mi1.FreeBytes). Tests replace it.
	FreeBytes func() (uint64, error)
}

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
	// The steps of the run before: their logs, which its screen and envelope
	// name, stay for one more run; the rest would only take space.
	if err := keepStepLogs(dir, filepath.Join(o.Work, previousDir)); err != nil {
		return fmt.Errorf("nu pot păstra jurnalele probei MI3 de dinainte: %w", err)
	}
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

	sixteen := o.Host.Cores6Plus && tier == TierH3
	o.say("%s", openingText(tier, cameras, sixteen))
	r := &stepRunner{o: o, l: l, fx: fx, tier: tier, dir: dir}
	d, sixteenErr, err := runSteps(ctx, tier, cameras, sixteen, r.measure)
	if d == nil {
		return o.unmeasured(err)
	}
	return o.finish(*d, sixteenErr, err)
}

// unmeasured is Run's error when the first hour has no valid attempt and
// err stopped it: there is no result to write. It says, as finish does,
// where the step logs that err names go at the next start.
func (o Options) unmeasured(err error) error {
	if errors.Is(err, context.Canceled) {
		o.say("Proba MI3 s-a întrerupt înainte de prima oră: nu are rezultat.")
		return errInterrupted
	}
	return fmt.Errorf("%w. Prima oră nu are rezultat: porniți din nou proba. %s", err, o.logsMove())
}

// logsMove says where the step logs of this run go at the next start.
func (o Options) logsMove() string {
	return fmt.Sprintf("La pornire, jurnalele pașilor de acum se mută în %s, unde rămân până la pornirea următoare",
		filepath.Join(o.Work, previousDir))
}

// errInterrupted is Run's error after the owner's stop (Ctrl+C): recspike
// ends as on its other interruptions, MI2's.
var errInterrupted = fmt.Errorf("proba MI3 s-a întrerupt: %w", context.Canceled)

// openingText is Run's first line on the owner's screen: what runs, and
// for how long: the hour, the 15-min steps and their confirming hour, the
// extra hour at 16 cameras when sixteen holds (a PC with 6 cores or more),
// and the attempts that a step paced unevenly adds.
func openingText(tier string, cameras int, sixteen bool) string {
	text := fmt.Sprintf("Proba MI3 începe: nivelul %s, %s, %s, redare, export și upload de %s Mbps, în paralel, timp de 1 h. "+
		"Dacă ora pică, proba continuă cu pași de 15 min la jumătate din camere, iar numărul care trece se confirmă cu încă o oră. ",
		tier, camerasText(cameras), SessionsText(Sessions(tier)), results.Decimal(float64(UploadBps(tier, cameras))/1e6, -1))
	if sixteen {
		text += "Fiindcă acest PC are 6 nuclee sau mai multe, la final urmează încă o oră la 16 camere, informativă. "
	}
	return text + fmt.Sprintf("Un pas pe care controlorul l-a trimis neuniform se reface, în cel mult %d încercări, deci proba poate dura mai mult. "+
		"Lăsați dispozitivul pornit și nefolosit până la final.", maxAttempts)
}

// finish writes d, what the steps measured, as the run's envelope, also
// when stepErr stopped the steps after the first hour, which decides G12
// (G27), and then returns stepErr. sixteenErr, the hour at 16 cameras', is
// information: it is said on the screen, and the run succeeds. The owner's
// stop (Ctrl+C) of either is an interruption, errInterrupted, as on
// recspike's other interruptions.
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
	if errors.Is(stepErr, context.Canceled) || errors.Is(sixteenErr, context.Canceled) {
		o.say("Proba MI3 s-a întrerupt; ce s-a măsurat este în rezultatul %s. Pașii: %s.", e.RunID, stepsSummary(d.Steps))
		return errInterrupted
	}
	if stepErr != nil {
		o.say("Proba MI3 s-a oprit înainte de final; ce s-a măsurat este în rezultatul %s. Pașii: %s.", e.RunID, stepsSummary(d.Steps))
		return fmt.Errorf("pașii de coborâre nu s-au încheiat: %w. Ora la %s rămâne măsurată; porniți din nou proba ca numărul recomandat să se stabilească. %s",
			stepErr, camerasText(d.Cameras), o.logsMove())
	}
	o.say("Proba MI3 s-a încheiat: rezultatul este %s. Pașii: %s.", e.RunID, stepsSummary(d.Steps))
	if last := d.Steps[len(d.Steps)-1]; !last.Valid {
		o.say("Controlorul a trimis neuniform de %d ori la rând, deci pasul nu are rezultat: verificați controlorul și rețeaua, apoi porniți din nou proba.", maxAttempts)
	}
	if sixteenErr != nil {
		o.say("Ora la 16 camere nu s-a putut măsura%s. În raport, 16 camere rămâne o estimare. Rezultatul de la %s nu depinde de ea.",
			o.sixteenWhy(sixteenErr), camerasText(d.Cameras))
	}
	return nil
}

// sixteenWhy is why the hour at 16 cameras has no result, as the screen
// says it: a stepFailure's text, which names nothing of the host, and where
// the step's logs are. The error's own text is in those logs only
// (errorLog): it may be English.
func (o Options) sixteenWhy(err error) string {
	why := ""
	var failure *stepFailure
	if errors.As(err, &failure) {
		why = ": " + failure.text
	}
	dir := filepath.Join(o.Work, "mi3")
	var se *stepError
	if errors.As(err, &se) {
		dir = se.dir
	}
	return fmt.Sprintf("%s; eroarea este în jurnalele pasului, în %s. %s", why, dir, o.logsMove())
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

func (o Options) freeBytes() (uint64, error) {
	if o.FreeBytes != nil {
		return o.FreeBytes()
	}
	return mi1.FreeBytes(o.Work)
}
