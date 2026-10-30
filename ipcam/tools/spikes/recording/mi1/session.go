// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/lastindex"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// The lines that the owner reads, in Romanian.
const (
	// WaitingLine is what a second recspike shows while the first holds
	// the run state (G14), as the boot task does after a PC cut.
	WaitingLine = "Proba termină verificarea de după repornire. Așteptați…"
	// RealShortcut names the desktop shortcut that continues the real cuts
	// on Windows.
	RealShortcut = "Proba înregistrare – tăieri reale"
	// RealCutPrompt is the prompt of a real cut: reccontroller shows it, and
	// recspike mirrors it on the host (C16).
	RealCutPrompt = "Scoateți acum alimentarea"
)

// Timeouts of a run's steps.
const (
	readyTimeout   = 2 * time.Minute  // ipcam-hub's ready
	eventTimeout   = 10 * time.Minute // the reconcile and verify after a stop
	verdictTimeout = 2 * time.Minute  // the controller's verdict on a report
	connectTimeout = 5 * time.Minute  // the link, after a boot
	// reconnectWait bounds the reconnection after a link drop in an
	// unattended set, and maxReconnects how many one wait (for a cut, for
	// a verdict) may need.
	reconnectWait = 2 * time.Minute
	maxReconnects = 5
	// realCutWait is how long after the prompt recspike waits for the
	// owner's cut: past the 2 min that the cut counts for, it stops
	// ipcam-hub and the cut is redone (Review Focus 1).
	realCutWait = 2*time.Minute + 10*time.Second
	// hostDownWait is how long recspike waits for the host to go down after
	// it asked the OS to stop.
	hostDownWait = 5 * time.Minute
	// maxMissesInARow is how many runs in a row that do not count stop one
	// recspike of an automated set: a cause that persists (the controller's
	// pacing, the link) then stops this recspike instead of the host, again
	// and again. Invalid runs are still excluded and redone without a bound
	// (G10): the set stays open, and the owner's same command continues it.
	// A sysrq or bugcheck run is one recspike per boot, so only the window
	// bounds those.
	maxMissesInARow = 3
)

// StopTimeout is how long recspike waits for a clean stop of ipcam-hub
// before it kills it. On a throttled disk, as an SD card under load, Task
// 7.2d measured stops over 30 s (MediaMTX closing its segments, reconcile's
// and the catalog's fsyncs): a shorter wait would kill a stop that was
// going well, abort an MI1 set and, in MI2, end a run with a kill.
const StopTimeout = 5 * time.Minute

// Driver runs MI1's sets on the host under test, recspike's side of T43:
// it starts ipcam-hub for each run, stops the host when the controller
// says, finishes each run after the stop or the reboot (reconcile, NTP,
// lastindex, HUB-04) and reports it; the controller judges the run and
// writes its envelope (C16). The run state lets every step survive a
// reboot (G14).
type Driver struct {
	// Controller is the controller's link address, host:port.
	Controller string
	// Hub is ipcam-hub; MediaMTX is the fork, "" for the one next to Hub.
	Hub, MediaMTX string
	// Results is recspike's published results folder, with the run state
	// in state/; Work is its private folder: the runs' data and keys.
	Results, Work string
	// Host is the host as the envelopes describe it.
	Host HostState
	// Out is the owner's screen.
	Out io.Writer
	// ConfigDir is where the owner's runs write the config that each start
	// of ipcam-hub reads (hubConfig): a folder that only Administrators and
	// SYSTEM, or root, may write. "" keeps it in the run's folder in Work,
	// as the lab's runs do.
	ConfigDir string
	// GOOS is the host's OS; "" is runtime.GOOS.
	GOOS string

	// The rest are the system's when nil.
	Clock        source.Clock
	NTP          NTPCheck
	Uptime       func() (time.Duration, error)
	ThisBoot     func() (Boot, error)
	OSStop       func(ctx context.Context, kind controller.ShutdownKind) error
	LastRecorded func(ctx context.Context, files lastindex.Before, id age.Identity) (uint64, error)
	Volumes      Volumes
	// Preflight checks the host and the link before a set's runs (G14,
	// R08); the default runs DeviceChecks for automated sets and
	// LinkChecks for the owner's sets, and nothing in the lab.
	Preflight func(ctx context.Context, mode string, link LinkProbes) error

	// deviceChecks and osStopIn stand in for DeviceChecks and OSStop in
	// tests, so that the folders they get are checked on every OS.
	deviceChecks func(binDir, dataDir string, cameras int) []Check
	osStopIn     func(ctx context.Context, kind controller.ShutdownKind, binDir string) error
	// protectDir makes ConfigDir writable by Administrators and SYSTEM, or
	// root, alone; nil is protectConfigDir.
	protectDir func(dir string) error
	// linkDown, when set, runs each time a link to the controller has
	// failed and its session reports it down: tests wait for it instead of
	// sleeping.
	linkDown func()
}

// SetOptions say which set a driver runs.
type SetOptions struct {
	Set, Mode, Kind string
	// Window is the owner's window of an automated set.
	Window *Window
	// WaitForWindow waits for the window to open before the first run,
	// when the owner starts the set ahead of it.
	WaitForWindow bool
	// Shard and Smoke are the lab's: shard 1 or 2 of the set, and only the
	// first two runs, with the smoke windows.
	Shard int
	Smoke bool
	// Resume continues the set of the run state, as the real cuts'
	// shortcut does.
	Resume bool
	// Host, when set, reads the host of a new set: only once the set is
	// sure to start, so that the owner answers nothing (C24, G16) for a set
	// that another set in progress refuses. A set in progress keeps the
	// host that its start read.
	Host func(ctx context.Context) (HostState, error)
}

func (d *Driver) goos() string {
	if d.GOOS != "" {
		return d.GOOS
	}
	return runtime.GOOS
}

func (d *Driver) clock() source.Clock {
	if d.Clock != nil {
		return d.Clock
	}
	return source.SystemClock{}
}

func (d *Driver) ntp() NTPCheck {
	if d.NTP != nil {
		return d.NTP
	}
	return SystemNTP(d.goos())
}

func (d *Driver) uptime() (time.Duration, error) {
	if d.Uptime != nil {
		return d.Uptime()
	}
	return Uptime()
}

func (d *Driver) thisBoot() (Boot, error) {
	if d.ThisBoot != nil {
		return d.ThisBoot()
	}
	return ThisBoot()
}

func (d *Driver) osStop(ctx context.Context, kind controller.ShutdownKind) error {
	if d.OSStop != nil {
		return d.OSStop(ctx, kind)
	}
	stop := d.osStopIn
	if stop == nil {
		stop = OSStop
	}
	return stop(ctx, kind, d.binDir())
}

// binDir is recspike's folder, which holds ipcam-hub and the owner's
// notmyfault64.exe: the bugcheck runs NotMyFault from there, and the PC's
// preflight checks it there (G14).
func (d *Driver) binDir() string { return filepath.Dir(d.Hub) }

func (d *Driver) lastRecorded(ctx context.Context, files lastindex.Before, id age.Identity) (uint64, error) {
	if d.LastRecorded != nil {
		return d.LastRecorded(ctx, files, id)
	}
	return lastindex.LastRecorded(ctx, files, id)
}

func (d *Driver) volumes() Volumes {
	if d.Volumes != nil {
		return d.Volumes
	}
	return Dirs{Root: d.Work}
}

func (d *Driver) preflight(ctx context.Context, mode string, p LinkProbes) error {
	if d.Preflight != nil {
		return d.Preflight(ctx, mode, p)
	}
	var checks []Check
	switch mode {
	case ModeLab:
		return nil
	case ModeAutomated:
		device := d.deviceChecks
		if device == nil {
			device = DeviceChecks
		}
		// NotMyFault where the bugcheck runs it; the free space where the
		// runs record.
		checks = device(d.binDir(), d.Work, d.Host.Cameras)
	}
	return Preflight(ctx, append(checks, LinkChecks(p)...))
}

// say prints one line on the owner's screen.
func (d *Driver) say(format string, args ...any) {
	if d.Out != nil {
		_, _ = fmt.Fprintf(d.Out, format+"\n", args...)
	}
}

func (d *Driver) statePath() string { return filepath.Join(d.Results, "state", "run-state.json") }
func (d *Driver) lockPath() string  { return filepath.Join(d.Results, "state", "run-state.lock") }
func (d *Driver) runDir(id string) string {
	return filepath.Join(d.Work, id)
}

// State is the active set's run state in the driver's results folder; nil
// when there is none.
func (d *Driver) State() (*RunState, error) { return LoadRunState(d.statePath()) }

// lock takes the run state's lock, telling the owner when it waits.
func (d *Driver) lock(ctx context.Context) (func() error, error) {
	return LockRunState(ctx, d.lockPath(), func() { d.say("%s", WaitingLine) })
}

// RunSet runs a set until it is complete, its window closes, or ctx ends;
// a set in the run state is continued, after the run that a reboot cut
// short is finished.
func (d *Driver) RunSet(ctx context.Context, o SetOptions) error {
	unlock, err := d.lock(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()
	st, err := LoadRunState(d.statePath())
	if err != nil {
		return err
	}
	if st, err = d.begin(ctx, st, o); err != nil {
		return err
	}
	release, err := KeepAwake()
	if err != nil {
		return err
	}
	defer release()
	s, err := d.connect(ctx)
	if err != nil {
		return err
	}
	defer s.close()
	if st.Current != nil {
		if err := d.finishAfterReboot(ctx, s, st); err != nil {
			return d.end(ctx, s, err)
		}
	}
	if !d.complete(st) {
		if err := d.preflight(ctx, st.Mode, s.probes(d.ntp())); err != nil {
			return d.end(ctx, s, err)
		}
	}
	return d.end(ctx, s, d.loop(ctx, s, st, o.WaitForWindow))
}

// Resume is `recspike resume`, the boot task's command (G14): it finishes
// the run that the reboot stopped and reports it, then continues an
// automated set, or a real set on Linux, whose prompt shows on the
// controller; on Windows the owner continues the real cuts with the
// desktop shortcut, which shows the prompt.
func (d *Driver) Resume(ctx context.Context) error {
	unlock, err := d.lock(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()
	st, err := LoadRunState(d.statePath())
	if err != nil {
		return err
	}
	if st == nil || (st.Current == nil && d.complete(st)) {
		d.say("Nicio probă MI1 de continuat.")
		return nil
	}
	d.Host = st.Host
	release, err := KeepAwake()
	if err != nil {
		return err
	}
	defer release()
	s, err := d.connect(ctx)
	if err != nil {
		return err
	}
	defer s.close()
	if st.Current != nil {
		if err := d.finishAfterReboot(ctx, s, st); err != nil {
			return d.end(ctx, s, err)
		}
	}
	if st.Mode == ModeReal && d.goos() == "windows" && !d.complete(st) {
		d.say("Continuați tăierile reale cu scurtătura de pe desktop „%s”.", RealShortcut)
		return d.endSession(ctx, s)
	}
	return d.end(ctx, s, d.loop(ctx, s, st, false))
}

// begin returns the run state of the set o asks for, given st, the active
// set's: the active set continues; another set starts only once the active
// one is complete, and then continues its own state, kept by results set
// (ResultsSet) when it ran in this folder before, or starts a new one. A
// set that is complete stays so, whatever ran since: its run ids are taken,
// and their envelopes are the controller's. A lab set's shards share its
// state, the shard that runs now in it; a smoke run's results set, so its
// state, its run ids and its envelopes, are its own, and a smoke run and
// its full shard may follow each other in one folder, in either order. A
// new set's host is o.Host's, when it has one.
func (d *Driver) begin(ctx context.Context, st *RunState, o SetOptions) (*RunState, error) {
	key := ResultsSet(o.Set, o.Smoke)
	switch {
	case st != nil && st.key() == key && st.Shard == o.Shard:
		if o.Window != nil {
			st.Window = o.Window.String()
		}
		d.Host = st.Host
		return st, nil
	case st != nil && (st.Current != nil || !d.complete(st)):
		return nil, fmt.Errorf("proba %s nu s-a încheiat: continuați-o înainte de %s",
			setLabel(st.Set, st.Shard, st.Smoke), setLabel(o.Set, o.Shard, o.Smoke))
	}
	states, err := loadRunStates(d.statePath())
	if err != nil {
		return nil, err
	}
	if own := states.Sets[key]; own != nil {
		own.Shard = o.Shard
		if o.Window != nil {
			own.Window = o.Window.String()
		}
		d.Host = own.Host
		return own, SaveRunState(d.statePath(), own)
	}
	if o.Resume {
		return nil, fmt.Errorf("nu există o probă %s de continuat", o.Set)
	}
	if o.Host != nil {
		host, err := o.Host(ctx)
		if err != nil {
			return nil, err
		}
		d.Host = host
	}
	st = &RunState{Set: o.Set, Mode: o.Mode, Kind: o.Kind, Shard: o.Shard, Smoke: o.Smoke, Host: d.Host}
	if o.Window != nil {
		st.Window = o.Window.String()
	}
	return st, SaveRunState(d.statePath(), st)
}

// setLabel names a set for the owner, with its lab shard and smoke run.
func setLabel(set string, shard int, smoke bool) string {
	switch {
	case shard > 0 && smoke:
		return fmt.Sprintf("%s (shard %d, smoke)", set, shard)
	case shard > 0:
		return fmt.Sprintf("%s (shard %d)", set, shard)
	}
	return set
}

// plan is the runs of the state's set that this driver records: the set's
// plan, or the lab shard's, or its first two for a smoke run.
func (d *Driver) plan(st *RunState) ([]RunPlan, error) {
	cameras := max(st.Host.Cameras, 1)
	plan, err := Plan(st.Set, cameras)
	if err != nil {
		return nil, err
	}
	if st.Mode != ModeLab {
		return plan, nil
	}
	if plan, err = Shard(plan, st.Shard); err != nil {
		return nil, err
	}
	if st.Smoke {
		plan = plan[:smokeRuns]
	}
	return plan, nil
}

// complete reports whether the state's set has every run it needs, or, for
// a real set, has stopped at MaxRealAttempts.
func (d *Driver) complete(st *RunState) bool {
	if st.Mode == ModeReal {
		_, status := NextRealCut(st.runs())
		return status != RealNext
	}
	plan, err := d.plan(st)
	if err != nil {
		return true
	}
	_, _, ok := st.nextAutomated(plan)
	return !ok
}

// window is the state's window, if any.
func (d *Driver) window(st *RunState) *Window {
	if st.Window == "" {
		return nil
	}
	w, err := ParseWindow(st.Window)
	if err != nil {
		return nil
	}
	return &w
}

// loop records the set's runs, one after the other, until the set is
// complete, the window closes, or maxMissesInARow runs in a row of an
// automated set do not count.
func (d *Driver) loop(ctx context.Context, s *session, st *RunState, waitForWindow bool) error {
	misses := 0
	for first := true; ; first = false {
		var next RunPlan
		var attempt int
		if st.Mode == ModeReal {
			cut, status := NextRealCut(st.runs())
			switch status {
			case RealComplete:
				d.say("Proba %s are toate cele 5 tăieri valide.", st.Set)
				return nil
			case RealIncomplete:
				d.say("Proba %s s-a oprit după %d încercări, fără 5 tăieri valide.", st.Set, MaxRealAttempts)
				return nil
			}
			next, attempt = cut.RunPlan, cut.Attempt
		} else {
			plan, err := d.plan(st)
			if err != nil {
				return err
			}
			var ok bool
			if next, attempt, ok = st.nextAutomated(plan); !ok {
				d.say("Proba %s are toate rulările.", st.Set)
				return nil
			}
		}
		if st.Mode == ModeAutomated {
			w := d.window(st)
			if w == nil {
				return errors.New("o probă automată are nevoie de o fereastră (--window HH:MM-HH:MM)")
			}
			if !w.Fits(d.clock().Now(), MaxRunSpan) {
				if !first || !waitForWindow {
					d.say("Fereastra %s s-a încheiat: proba %s continuă în fereastra următoare (porniți din nou recspike mi1 în fereastră).", w, st.Set)
					return nil
				}
				d.say("Aștept fereastra %s…", w)
				if err := d.waitWindow(ctx, w); err != nil {
					return err
				}
			}
		}
		// A link that dropped since the last run, or while the set waited
		// for its window, is connected again before the run is prepared:
		// its run.json takes the controller's cameras from the session.
		if !s.up() {
			if err := d.reconnect(ctx, s, st, errLinkDown); err != nil {
				return err
			}
		}
		if err := d.record(ctx, s, st, next, attempt); err != nil {
			return err
		}
		if st.Mode == ModeReal || len(st.Done) == 0 {
			continue // a real set stops at MaxRealAttempts
		}
		last := st.Done[len(st.Done)-1].Run // the run just recorded
		if Counts(last) {
			misses = 0
			continue
		}
		if misses++; misses >= maxMissesInARow {
			return fmt.Errorf("%d rulări la rând nu au contat (ultima: %s), așa că proba %s se oprește aici. "+
				"Rulările valide rămân: verificați controlorul și rețeaua, apoi porniți din nou aceeași comandă, iar proba continuă de la rularea %d",
				misses, invalidText(last), st.Set, next.Index)
		}
	}
}

// waitWindow waits until a run fits in the window.
func (d *Driver) waitWindow(ctx context.Context, w *Window) error {
	tick := d.clock().NewTicker(30 * time.Second)
	defer tick.Stop()
	for !w.Fits(d.clock().Now(), MaxRunSpan) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C():
		}
	}
	return nil
}

// end ends the session after a set's err: with endSession when the set
// ended well, and also after an error while the link is up, so that the PC
// still has the Pi's results (G15); the error stays the set's.
func (d *Driver) end(ctx context.Context, s *session, err error) error {
	if err == nil {
		return d.endSession(ctx, s)
	}
	if ctx.Err() == nil && s.up() {
		if endErr := d.endSession(ctx, s); endErr != nil {
			d.say("Sesiunea cu controlorul nu s-a încheiat cum trebuie: %v", endErr)
		}
	}
	return err
}

// endSession ends a session with the controller (G15): on the PC, recspike
// first brings the Pi's results folder, which the Pi's controller holds,
// into results/pi/; on the Pi, it answers the PC's controller, which pulls
// its results folder after the bye.
func (d *Driver) endSession(ctx context.Context, s *session) error {
	if d.Host.Pseudonym == hostPC {
		if err := s.send(link.PullResults{}); err != nil {
			return err
		}
		m, err := s.await(ctx, verdictTimeout, "results_tar")
		if err != nil {
			return err
		}
		if err := link.Untar(filepath.Join(d.Results, "pi"), m.(link.ResultsTar).Data); err != nil {
			return err
		}
		d.say("Am adus rezultatele Pi-ului în %s.", filepath.Join("results", "pi"))
	}
	if err := s.send(link.Bye{}); err != nil {
		return err
	}
	if d.Host.Pseudonym != hostPi {
		return nil
	}
	return answerPull(ctx, s, d.Results, d.Out)
}
