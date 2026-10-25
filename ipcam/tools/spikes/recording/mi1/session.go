// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/lastindex"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
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
	// realCutPrompt is the prompt of a real cut, mirrored from the
	// controller (C16).
	realCutPrompt = "Scoateți acum alimentarea"
)

// Timeouts of a run's steps.
const (
	readyTimeout   = 2 * time.Minute  // ipcam-hub's ready
	eventTimeout   = 10 * time.Minute // the reconcile and verify after a stop
	stopTimeout    = 30 * time.Second // a clean stop of ipcam-hub
	verdictTimeout = 2 * time.Minute  // the controller's verdict on a report
	connectTimeout = 5 * time.Minute  // the link, after a boot
	// realCutWait is how long after the prompt recspike waits for the
	// owner's cut: past the 2 min that the cut counts for, it stops
	// ipcam-hub and the cut is redone (Review Focus 1).
	realCutWait = 2*time.Minute + 10*time.Second
	// hostDownWait is how long recspike waits for the host to go down after
	// it asked the OS to stop.
	hostDownWait = 5 * time.Minute
	// maxRunAttempts bounds the attempts at one position of an automated
	// set: a run that keeps failing to count stops the set.
	maxRunAttempts = 3
)

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
	// GOOS is the host's OS; "" is runtime.GOOS.
	GOOS string

	// The rest are the system's when nil.
	Clock        source.Clock
	NTP          NTPCheck
	Uptime       func() (time.Duration, error)
	OSStop       func(ctx context.Context, kind controller.ShutdownKind) error
	LastRecorded func(ctx context.Context, files lastindex.Before, id age.Identity) (uint64, error)
	Volumes      Volumes
	// Preflight checks the host and the link before a set's runs (G14,
	// R08); the default runs DeviceChecks for automated sets and
	// LinkChecks for the owner's sets, and nothing in the lab.
	Preflight func(ctx context.Context, mode string, link LinkProbes) error
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

func (d *Driver) osStop(ctx context.Context, kind controller.ShutdownKind) error {
	if d.OSStop != nil {
		return d.OSStop(ctx, kind)
	}
	return OSStop(ctx, kind, filepath.Dir(d.Hub))
}

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
		checks = DeviceChecks(d.Work, d.Host.Cameras)
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
	if st, err = d.begin(st, o); err != nil {
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
			return err
		}
	}
	if !d.complete(st) {
		if err := d.preflight(ctx, st.Mode, s.probes(d.ntp())); err != nil {
			return err
		}
	}
	if err := d.loop(ctx, s, st, o.WaitForWindow); err != nil {
		return err
	}
	return d.endSession(ctx, s)
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
			return err
		}
	}
	if st.Mode == ModeReal && d.goos() == "windows" && !d.complete(st) {
		d.say("Continuați tăierile reale cu scurtătura de pe desktop „%s”.", RealShortcut)
		return d.endSession(ctx, s)
	}
	if err := d.loop(ctx, s, st, false); err != nil {
		return err
	}
	return d.endSession(ctx, s)
}

// begin returns the run state of the set o asks for: the one in progress,
// or a new one when the last set is complete or there is none.
// A set that is complete stays so: its run ids are taken, and its envelopes
// are the controller's.
func (d *Driver) begin(st *RunState, o SetOptions) (*RunState, error) {
	switch {
	case st != nil && st.Set == o.Set:
		if o.Window != nil {
			st.Window = o.Window.String()
		}
		d.Host = st.Host
		return st, nil
	case o.Resume:
		return nil, fmt.Errorf("nu există o probă %s de continuat", o.Set)
	case st != nil && (st.Current != nil || !d.complete(st)):
		return nil, fmt.Errorf("proba %s nu s-a încheiat: continuați-o înainte de %s", st.Set, o.Set)
	}
	st = &RunState{Set: o.Set, Mode: o.Mode, Kind: o.Kind, Shard: o.Shard, Smoke: o.Smoke, Host: d.Host}
	if o.Window != nil {
		st.Window = o.Window.String()
	}
	return st, SaveRunState(d.statePath(), st)
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

// complete reports whether the state's set has every run it needs, or has
// stopped at its attempts.
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
// complete, a run keeps failing to count, or the window closes.
func (d *Driver) loop(ctx context.Context, s *session, st *RunState, waitForWindow bool) error {
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
			if attempt > maxRunAttempts {
				return fmt.Errorf("rularea %d a probei %s nu a contat de %d ori: proba se oprește", next.Index, st.Set, maxRunAttempts)
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
		if err := d.record(ctx, s, st, next, attempt); err != nil {
			return err
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

// answerPull serves the far side's pull_results with the results folder,
// until it closes the link.
func answerPull(ctx context.Context, s *session, dir string, out io.Writer) error {
	timeout := time.NewTimer(verdictTimeout)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return nil
		case r, ok := <-s.in:
			if !ok || r.err != nil {
				return nil // the controller closed the link
			}
			if _, ok := r.m.(link.PullResults); ok {
				if err := s.c.SendResultsTar(dir); err != nil {
					return err
				}
				if out != nil {
					_, _ = fmt.Fprintln(out, "Am trimis rezultatele.")
				}
			}
		}
	}
}

// Push is `recspike push --controller <PC>` on the Pi, the final collection
// (G15): it connects to the PC's reccontroller, which pulls the results
// folder dir, and returns when the PC has it.
func Push(ctx context.Context, addr, dir, host string, out io.Writer) error {
	s, err := dialSession(ctx, addr, host, 0)
	if err != nil {
		return err
	}
	defer s.close()
	// `reccontroller pull` asks at once; a running reccontroller sends its
	// session first and pulls a Pi's folder after the bye.
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r, ok := <-s.in:
			if !ok || r.err != nil {
				return fmt.Errorf("controlorul a închis legătura înainte să ceară rezultatele: %v", r.err)
			}
			switch r.m.(type) {
			case link.Session:
				if err := s.send(link.Bye{}); err != nil {
					return err
				}
			case link.PullResults:
				if err := s.c.SendResultsTar(dir); err != nil {
					return err
				}
				_, _ = fmt.Fprintln(out, "Am trimis rezultatele.")
				return nil
			}
		}
	}
}

// session is the host's link to the controller: one goroutine reads its
// messages into in.
type session struct {
	c          *link.Conn
	in         chan received
	closed     chan struct{}
	info       link.Session
	beaconAddr string
}

type received struct {
	m   link.Message
	err error
}

// connect connects to the controller, retrying for a while after a boot,
// when the network may not be up yet, and receives the session.
func (d *Driver) connect(ctx context.Context) (*session, error) {
	s, err := dialSession(ctx, d.Controller, d.Host.Pseudonym, connectTimeout)
	if err != nil {
		return nil, err
	}
	m, err := s.await(ctx, time.Minute, "session")
	if err != nil {
		s.close()
		return nil, fmt.Errorf("controlorul nu a trimis sesiunea: %w", err)
	}
	s.info = m.(link.Session)
	host, _, err := net.SplitHostPort(d.Controller)
	if err != nil {
		s.close()
		return nil, err
	}
	port := s.info.BeaconPort
	if port == 0 {
		port = link.BeaconPort
	}
	s.beaconAddr = net.JoinHostPort(host, strconv.Itoa(port))
	return s, nil
}

// dialSession dials the controller, retrying for up to retry, and says
// hello as the host.
func dialSession(ctx context.Context, addr, host string, retry time.Duration) (*session, error) {
	deadline := time.Now().Add(retry)
	for {
		dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		c, err := link.Dial(dialCtx, addr)
		cancel()
		if err == nil {
			s := &session{c: c, in: make(chan received, 16), closed: make(chan struct{})}
			go s.read()
			if err := s.send(link.Hello{Role: "host", Host: host, Version: "c1"}); err != nil {
				s.close()
				return nil, err
			}
			return s, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("nu mă pot conecta la controlor (portul %d): %w", link.Port, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// read delivers the controller's messages, then its error, and closes in.
func (s *session) read() {
	defer close(s.in)
	for {
		m, err := s.c.Receive()
		select {
		case s.in <- received{m, err}:
		case <-s.closed:
			return
		}
		if err != nil {
			return
		}
	}
}

func (s *session) send(m link.Message) error { return s.c.Send(m) }

func (s *session) close() {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	_ = s.c.Close()
}

// await returns the next message of type typ, dropping others, within d.
func (s *session) await(ctx context.Context, d time.Duration, typ string) (link.Message, error) {
	timeout := time.NewTimer(d)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout.C:
			return nil, fmt.Errorf("controlorul nu a răspuns (%s) în %v", typ, d)
		case r, ok := <-s.in:
			if !ok {
				return nil, io.EOF
			}
			if r.err != nil {
				return nil, r.err
			}
			if r.m.Type() == typ {
				return r.m, nil
			}
		}
	}
}

// report sends a run's report and returns the controller's verdict.
func (s *session) report(ctx context.Context, r link.Report) (results.Run, error) {
	if err := s.send(r); err != nil {
		return results.Run{}, err
	}
	for {
		m, err := s.await(ctx, verdictTimeout, "verdict")
		if err != nil {
			return results.Run{}, err
		}
		if v := m.(link.Verdict); v.Run.RunID == r.RunID {
			return v.Run, nil
		}
	}
}

// probes are the link's preflight probes (R08): the beacon acknowledged
// over TCP, an RTSP pull of cam01_main, and the NTP check.
func (s *session) probes(ntp NTPCheck) LinkProbes {
	return LinkProbes{
		Beacon: s.checkBeacon,
		RTSP: func(ctx context.Context) error {
			base, err := url.Parse(s.info.RTSPBase)
			if err != nil {
				return err
			}
			u := url.URL{Scheme: base.Scheme, User: url.UserPassword(s.info.User, s.info.Pass), Host: base.Host, Path: "/" + MainStream}
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			src, err := source.NewRTSP(ctx, "preflight", u.String())
			if err != nil {
				return err
			}
			cached, live, stop := src.Subscribe()
			defer stop()
			if len(cached) > 0 {
				return nil
			}
			select {
			case _, ok := <-live:
				if !ok {
					return errors.New("the camera stopped")
				}
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		NTP: ntp,
	}
}

// checkBeacon sends beacons of a check id while it asks the controller
// whether they arrive.
func (s *session) checkBeacon(ctx context.Context) error {
	id := fmt.Sprintf("verificare-%d", time.Now().UnixNano())
	conn, err := net.Dial("udp4", s.beaconAddr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := s.send(link.BeaconCheck{ID: id}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	go func() {
		tick := time.NewTicker(controller.BeaconEvery)
		defer tick.Stop()
		for seq := uint64(0); ctx.Err() == nil; seq++ {
			_, _ = conn.Write(link.BeaconPacket(id, seq))
			select {
			case <-ctx.Done():
			case <-tick.C:
			}
		}
	}()
	m, err := s.await(ctx, 10*time.Second, "beacon_ack")
	if err != nil {
		return err
	}
	if !m.(link.BeaconAck).Received {
		return errors.New("no beacon arrived")
	}
	return nil
}
