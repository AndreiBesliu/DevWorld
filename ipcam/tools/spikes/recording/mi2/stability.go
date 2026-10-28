// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package mi2 is the crash and stability measurement MI2 (G11, §16.4,
// HUB-31): Judge, the gate's one rule (C17), which takes the 20 resumes
// from MI1's owner kill9 sets (G13) and the 72 h run's envelope; and Run,
// recspike's 72 h run on the host under test (G18, G19, C07), with its
// rolling window (window.go).
package mi2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// SetFor is the 72 h run's set on host: pc-72h or pi-72h.
func SetFor(host string) string { return host + "-72h" }

// Hub is one ipcam-hub, as the 72 h run sees it: *hubproc.Hub.
type Hub interface {
	Exited() <-chan struct{}
	ExitCode() int
	Stop(ctx context.Context) error
}

// Options are what the 72 h run needs.
type Options struct {
	// Hours is the run's length: 72.
	Hours float64
	// Host is the host as the envelope describes it, with its recommended
	// camera count and, on an SD card, the owner's confirmation (G16).
	Host mi1.HostState
	// Hub is ipcam-hub; MediaMTX is the fork, "" for the one next to Hub.
	Hub, MediaMTX string
	// Results is recspike's published results folder, with the run's state
	// in state/; Work is its private folder: the run's data and keys.
	Results, Work string
	// Out is the owner's screen.
	Out io.Writer
	// CameraURLs gives the RTSP address of each stream, from the
	// controller, a host-local reccontroller -local (G18, G19).
	CameraURLs func(ctx context.Context, streams []string) (map[string]string, error)

	// The rest are the system's when nil.
	FreeBytes func() (uint64, error)
	Clock     source.Clock
	ThisBoot  func() (mi1.Boot, error)
	StartHub  func(config string, stderr io.Writer, onEvent func(hubproc.Event)) (Hub, error)
}

// The 72 h run's pace.
const (
	// pruneEvery is how often the run deletes the segments older than the
	// window, and checks its end.
	pruneEvery = time.Minute
	// maxStartsUnready ends a run whose ipcam-hub exited that many times in
	// a row before its ready: it cannot record, and restarting it for 72 h
	// would measure nothing more.
	maxStartsUnready = 3
)

// Run is MI2's 72 h run on this host (G11, G19, C07): it records the host's
// recommended number of cameras, main streams from the controller, half of
// them encrypted, for Hours, with a rolling window of the free space; it
// keeps the host awake (R49), restarts ipcam-hub at once whenever it exits
// unexpectedly and counts that exit as a crash, counts MediaMTX's restarts
// (HUB-13), notes the SD-card monitor's states (T40), and writes the
// envelope MI2/<host>-72h/<run id>.json. recspike is the supervisor here:
// its restart is a lower bound for Faza 1's SCM and systemd recovery
// (C07). A run that an OS reboot, or the end of recspike, interrupted is
// invalid: the next Run redoes it under the next run id, removes its
// recordings, and lists it in Invalidated; it never counts as a crash.
func Run(ctx context.Context, o Options) error {
	if o.CameraURLs == nil {
		return errors.New("mi2: no controller for the cameras")
	}
	set := SetFor(o.Host.Pseudonym)
	cameras := max(o.Host.Cameras, 1)
	if err := os.MkdirAll(o.Work, 0o700); err != nil {
		return err
	}
	free, err := o.freeBytes()
	if err != nil {
		return fmt.Errorf("nu pot citi spațiul liber: %w", err)
	}
	window, err := Window(free, cameras)
	o.sayWindow(free, cameras, window)
	if err != nil {
		o.say("Spațiul liber nu ajunge pentru o fereastră de cel puțin 1 h. Eliberați spațiu (cel puțin %d GB liberi) și porniți din nou proba.", NeededGB(cameras, MinWindow))
		return err
	}

	// The controller first: a run that cannot get its cameras never begins.
	streams := make([]string, cameras)
	for i := range streams {
		streams[i] = fmt.Sprintf("cam%02d_main", i+1)
	}
	urls, err := o.CameraURLs(ctx, streams)
	if err != nil {
		return err
	}

	statePath := filepath.Join(o.Results, "state", "mi2-"+set+".json")
	boot, bootErr := o.thisBoot()
	st, err := beginRun(statePath, set, boot, bootErr == nil)
	if err != nil {
		return err
	}
	runID := runIDOf(set, st.Attempt)
	if n := len(st.Invalidated); n > 0 {
		o.say("Rularea %s; se reface ca %s.", st.Invalidated[n-1], runID)
	}
	// One run at a time: the recordings of the runs before it go.
	if err := os.RemoveAll(filepath.Join(o.Work, "mi2")); err != nil {
		return err
	}
	dir := filepath.Join(o.Work, "mi2", runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	config, err := o.writeRun(dir, streams, urls)
	if err != nil {
		return err
	}
	release, err := mi1.KeepAwake()
	if err != nil {
		return err
	}
	defer release()
	log, err := os.OpenFile(filepath.Join(dir, "ipcam-hub.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the run's own folder
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()

	o.say("Proba MI2 %s începe: %s h, %d camere. Lăsați dispozitivul pornit, cu reccontroller -local pornit tot timpul.", runID, decimal(o.Hours, -1), cameras)
	data, err := o.record(ctx, config, log, window)
	if errors.Is(err, context.Canceled) {
		o.say("Proba MI2 %s s-a întrerupt: nu contează, și se reface la următorul recspike mi2.", runID)
	}
	if err != nil {
		return err
	}
	data.Cameras, data.OnSdCard, data.WindowHours, data.Invalidated = cameras, o.Host.Storage == "sd", window.Hours(), st.Invalidated
	e := results.Envelope{Schema: results.Schema, Code: "MI2", RunID: runID, Set: set, Host: o.Host.Pseudonym, OS: o.Host.OS,
		PiModel: o.Host.PiModel, Storage: o.Host.Storage, SdConfirmation: o.Host.SdConfirmation, MI2: &data}
	if err := results.Create(o.Results, e); err != nil {
		return err
	}
	if err := finishRun(statePath, st); err != nil {
		return err
	}
	o.say("Proba MI2 %s s-a încheiat după %s h. Crash-uri ipcam-hub: %d; reporniri MediaMTX: %d.", runID, decimal(data.Hours, 2), data.Crashes72h, data.ChildRestarts)
	return nil
}

// record runs ipcam-hub on config for the run's hours and supervises it
// (C07): an exit that recspike did not ask for is a crash, and ipcam-hub
// starts again at once. Every pruneEvery it deletes the verified segments
// older than window. At the end, or when ctx is done, it stops ipcam-hub
// cleanly, waiting mi1.StopTimeout, long enough for an SD card (Task
// 7.2d): that stop is no crash, even when it ends in a kill.
func (o Options) record(ctx context.Context, config string, stderr io.Writer, window time.Duration) (results.Mi2Data, error) {
	clock := o.clock()
	start := clock.Now()
	end := start.Add(time.Duration(o.Hours * float64(time.Hour)))
	tick := clock.NewTicker(pruneEvery)
	defer tick.Stop()
	pruner := NewPruner(window)
	var t tally
	onEvent := func(e hubproc.Event) {
		if e.Type == "segment_verified" {
			pruner.Verified(e.Path)
			return
		}
		t.event(e)
	}
	hub, err := o.startHub(config, stderr, onEvent)
	if err != nil {
		return results.Mi2Data{}, err
	}
	stop := func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mi1.StopTimeout)
		defer cancel()
		if err := hub.Stop(stopCtx); err != nil {
			o.say("Oprirea ipcam-hub: %v", err)
		}
	}
	unready := 0
	for {
		select {
		case <-ctx.Done():
			stop()
			return results.Mi2Data{}, ctx.Err()
		case <-hub.Exited():
			crashes, ready := t.crash()
			o.say("ipcam-hub s-a oprit neașteptat (codul %d): crash-ul %d. Îl pornesc din nou.", hub.ExitCode(), crashes)
			if ready {
				unready = 0
			} else {
				unready++
			}
			if unready >= maxStartsUnready {
				o.say("ipcam-hub s-a oprit de %d ori la rând înainte să înregistreze: proba se oprește.", unready)
				return t.data(clock.Now().Sub(start)), nil
			}
			if hub, err = o.startHub(config, stderr, onEvent); err != nil {
				return results.Mi2Data{}, err
			}
		case now := <-tick.C():
			// ipcam-hub's verify counts the deleted files' rows as mismatches,
			// which MI2 does not judge: the deletes are the spike's.
			if _, err := pruner.Prune(now); err != nil {
				o.say("Fereastra păstrată: %v", err)
			}
			if !now.Before(end) {
				stop()
				return t.data(now.Sub(start)), nil
			}
		}
	}
}

// tally is what the run counts of ipcam-hub's events and exits.
type tally struct {
	mu            sync.Mutex
	ready         bool // the running ipcam-hub sent ready
	crashes       int
	childRestarts int
	cardStates    []results.CardState
}

// event counts one of ipcam-hub's events.
func (t *tally) event(e hubproc.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch e.Type {
	case "ready":
		t.ready = true
	case "child_restart":
		t.childRestarts++
	case "card":
		t.cardStates = append(t.cardStates, results.CardState{State: e.State, AtMs: e.AtMs})
	}
}

// crash counts an unexpected exit of ipcam-hub, and returns the crashes so
// far and whether it had been ready; the next ipcam-hub starts unready.
func (t *tally) crash() (crashes int, ready bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.crashes++
	ready, t.ready = t.ready, false
	return t.crashes, ready
}

// data is the run's measurements after it ran for d.
func (t *tally) data(d time.Duration) results.Mi2Data {
	t.mu.Lock()
	defer t.mu.Unlock()
	return results.Mi2Data{Hours: d.Hours(), Crashes72h: t.crashes, ChildRestarts: t.childRestarts, CardStates: t.cardStates}
}

// writeRun writes the run's run.json in dir: each stream a job that pulls
// the controller's camera, the odd ones encrypted to a fresh RI_g and RK of
// the run (C11, PH0-22), whose RI_g is a 0600 file beside it and whose RK's
// identity is never kept.
func (o Options) writeRun(dir string, streams []string, urls map[string]string) (string, error) {
	cfg := hubproc.Config{DataDir: filepath.Join(dir, "data"), MediaMTX: o.MediaMTX, SDCardConfirmed: o.Host.SdConfirmation != nil}
	riG, err := age.GenerateX25519Identity()
	if err != nil {
		return "", err
	}
	rk, err := age.GenerateX25519Identity()
	if err != nil {
		return "", err
	}
	cfg.IdentityFile = filepath.Join(dir, "ri_g.key")
	if err := mi1.WriteDurably(cfg.IdentityFile, []byte(riG.String()+"\n"), 0o600); err != nil {
		return "", err
	}
	for i, stream := range streams {
		cam, profile, _ := strings.Cut(stream, "_")
		job := hubproc.Job{Cam: cam, Profile: profile, RestreamURL: urls[stream]}
		if i%2 == 0 { // cam01, cam03, …: half of them, rounded up
			job.Recipients = []string{riG.Recipient().String(), rk.Recipient().String()}
		}
		cfg.Jobs = append(cfg.Jobs, job)
	}
	config := filepath.Join(dir, "run.json")
	return config, hubproc.WriteConfig(config, cfg)
}

// sayWindow prints the free space that the run needs on this host and the
// window that it has (G19).
func (o Options) sayWindow(free uint64, cameras int, window time.Duration) {
	o.say("Proba de %s h înregistrează %d camere de 4 Mbps (jumătate criptate): %s GB pe oră.", decimal(o.Hours, -1), cameras, decimal(WriteRate(cameras)*3600/1e9, 1))
	o.say("Păstrează doar ultimele N ore: N = min(6 h, jumătate din spațiul liber ÷ ritmul de scriere), cel puțin 1 h. Sunt necesari cel puțin %d GB liberi; pentru fereastra întreagă de 6 h, ≈ %d GB.",
		NeededGB(cameras, MinWindow), NeededGB(cameras, MaxWindow))
	o.say("Spațiu liber aici: %s GB; fereastra este %s h.", decimal(float64(free)/1e9, 0), decimal(window.Hours(), 2))
}

func (o Options) say(format string, args ...any) {
	if o.Out != nil {
		_, _ = fmt.Fprintf(o.Out, format+"\n", args...)
	}
}

func (o Options) freeBytes() (uint64, error) {
	if o.FreeBytes != nil {
		return o.FreeBytes()
	}
	return freeBytes(o.Work)
}

func (o Options) clock() source.Clock {
	if o.Clock != nil {
		return o.Clock
	}
	return source.SystemClock{}
}

func (o Options) thisBoot() (mi1.Boot, error) {
	if o.ThisBoot != nil {
		return o.ThisBoot()
	}
	return mi1.ThisBoot()
}

func (o Options) startHub(config string, stderr io.Writer, onEvent func(hubproc.Event)) (Hub, error) {
	if o.StartHub != nil {
		return o.StartHub(config, stderr, onEvent)
	}
	return hubproc.Start(hubproc.Options{Binary: o.Hub, Config: config, Stderr: stderr, OnEvent: onEvent})
}

// decimal renders f with prec decimals (-1: as few as it needs) and a
// decimal comma.
func decimal(f float64, prec int) string {
	return strings.Replace(strconv.FormatFloat(f, 'f', prec, 64), ".", ",", 1)
}

// runState is where the host's 72 h runs stand, in
// results/state/mi2-<set>.json: the last run started, whether it is still
// open, the boot it started in, and the runs before it that did not end.
// The file lives in the published results folder, so it names no address
// and no path (PH0-22).
type runState struct {
	Attempt     int      `json:"attempt"`
	Open        bool     `json:"open"`
	Boot        mi1.Boot `json:"boot"`
	Invalidated []string `json:"invalidated,omitempty"`
}

// runIDOf is the id of the attempt-th run of set.
func runIDOf(set string, attempt int) string { return fmt.Sprintf("%s-%d", set, attempt) }

// beginRun starts the next run of set in the state at path. A run that is
// still open did not end: it is invalid, because the host rebooted since it
// began (another boot, when both are known), or because recspike stopped.
func beginRun(path, set string, boot mi1.Boot, bootKnown bool) (runState, error) {
	var st runState
	data, err := os.ReadFile(path) //nolint:gosec // recspike's own state
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &st); err != nil {
			return st, fmt.Errorf("mi2: %s: %w", filepath.Base(path), err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return st, err
	}
	if st.Open {
		reason := "proba s-a întrerupt înainte de final (recspike s-a oprit)"
		if bootKnown && !boot.Same(st.Boot) {
			reason = "gazda a repornit în timpul probei"
		}
		st.Invalidated = append(st.Invalidated, runIDOf(set, st.Attempt)+": "+reason)
	} else {
		st.Invalidated = nil
	}
	st.Attempt++
	st.Open, st.Boot = true, boot
	return st, saveState(path, st)
}

// finishRun records that the run of st ended.
func finishRun(path string, st runState) error {
	st.Open, st.Invalidated = false, nil
	return saveState(path, st)
}

func saveState(path string, st runState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return mi1.WriteDurably(path, append(data, '\n'), 0o600)
}
