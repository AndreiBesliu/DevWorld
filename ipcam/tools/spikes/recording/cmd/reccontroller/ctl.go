// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// The smoke runs' windows (R03): the prompt 20 to 40 s after the first
// beacon, and a cut valid 10 s to 60 s after it.
const (
	smokePromptFrom, smokePromptTo = 20 * time.Second, 40 * time.Second
	smokeValidFrom, smokeValidTo   = 10 * time.Second, 60 * time.Second
)

// beaconCheckWait is how long a preflight's beacon check waits for the
// host's first beacon.
const beaconCheckWait = 5 * time.Second

// firstBeaconWait bounds the wait for a run's first beacon after its
// `ready`: the host beacons from ipcam-hub's ready, when it sends its own, so
// a run still without a beacon a minute later has no beacon path. The
// controller refuses it, so that neither side waits for a cut that never
// comes.
const firstBeaconWait = time.Minute

// pacer is the RTSP server's pacing of the controller's sends (R07).
type pacer interface {
	MarkPacing() controller.PacingMark
	Pacing(start, end controller.PacingMark, stopAt time.Time) controller.PacingStats
}

// ctlConfig is what a controller needs.
type ctlConfig struct {
	pacer                pacer
	sendLog              *controller.SendLog
	clock                source.Clock
	out                  io.Writer
	results              string
	local                bool
	rtspPort, beaconPort int
	creds                controller.Credentials
	streams              []controller.Stream
	needles              [][]byte
}

// ctl is the controller's state across the host's connections: a run's
// report arrives on a new connection after the host's reboot. Each run's
// schedule runs under the run's own context, a child of the controller's,
// not under a connection's: it outlives a dropped link, and ends with the
// run's report or the host's refusal.
type ctl struct {
	ctlConfig
	ctx     context.Context
	beacons *controller.BeaconLog

	mu     sync.Mutex
	rnd    *rand.Rand
	runs   map[string]*runState
	judged map[string]results.Run // the verdicts sent, for a report sent again
}

func newCtl(ctx context.Context, cfg ctlConfig) *ctl {
	return &ctl{
		ctlConfig: cfg, ctx: ctx, beacons: controller.NewBeaconLog(),
		rnd:  rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), //nolint:gosec // schedules cuts, not secrets
		runs: map[string]*runState{}, judged: map[string]results.Run{},
	}
}

// runState is one run, from run_start to its report.
type runState struct {
	// ctx ends when the run does (its report, the host's refusal, or the
	// controller's end): the run's cut and its watch for the beacons' end
	// stop with it.
	ctx       context.Context
	cancel    context.CancelFunc
	start     link.RunStart
	startMark controller.PacingMark
	endMark   *controller.PacingMark // when its beacons went silent, near the stop
	cut       controller.Cut
	scheduled bool       // its cut is scheduled: a repeated ready changes nothing
	conn      *link.Conn // the host's latest connection that named the run
}

// say prints one line on the controller's screen.
func (c *ctl) say(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = fmt.Fprintf(c.out, format+"\n", args...)
}

// serve handles one connection of a host until it ends.
func (c *ctl) serve(ctx context.Context, conn *link.Conn, local net.Addr) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer func() { _ = conn.Close() }()
	host := ""
	for {
		m, err := conn.Receive()
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				slog.Warn("link ended", "err", err)
			}
			return
		}
		switch m := m.(type) {
		case link.Hello:
			host = pseudonym(m.Host)
			c.say("Gazda „%s” s-a conectat.", host)
			c.reply(conn, c.session(local))
		case link.RunStart:
			c.startRun(conn, m)
		case link.Ready:
			c.ready(conn, m.RunID)
		case link.Report:
			c.reply(conn, link.Verdict{Run: c.report(m)})
		case link.Refused:
			c.hostRefused(m)
		case link.BeaconCheck:
			go c.checkBeacon(conn, m.ID)
		case link.Results:
			c.saveResult(m)
		case link.PullResults:
			if err := conn.SendResultsTar(c.results); err != nil {
				c.say("Eroare la trimiterea rezultatelor: %v", err)
			} else {
				c.say("Am trimis dosarul de rezultate.")
			}
		case link.Bye:
			c.bye(conn, host)
			return
		default:
			slog.Warn("unexpected link message", "type", m.Type())
		}
	}
}

func (c *ctl) reply(conn *link.Conn, m link.Message) {
	if err := conn.Send(m); err != nil {
		c.say("Eroare pe legătura cu gazda: %v", err)
	}
}

// bye ends a host's session. When the host is the Pi and this controller
// runs on the PC (not -local), it first pulls the Pi's results folder into
// results/pi (G15).
func (c *ctl) bye(conn *link.Conn, host string) {
	if host != "pi" || c.local {
		c.say("Gazda s-a deconectat.")
		return
	}
	if err := conn.PullResults(filepath.Join(c.results, "pi")); err != nil {
		c.say("Nu am putut aduce rezultatele Pi-ului: %v", err)
		return
	}
	c.say("Am adus rezultatele Pi-ului în %s. Gazda s-a deconectat.", filepath.Join(c.results, "pi"))
}

// pseudonym keeps a host name off the screen and the log (PH0-22).
func pseudonym(host string) string {
	switch host {
	case "pc", "pi", "lab-linux", "lab-windows", "lab-qemu":
		return host
	}
	return "necunoscută"
}

// session tells the host where the cameras and the beacon are, at the
// address it reached the controller on, and what HUB-04 looks for.
func (c *ctl) session(local net.Addr) link.Session {
	host := "127.0.0.1"
	if a, ok := local.(*net.TCPAddr); ok {
		host = a.IP.String()
	}
	names := make([]string, len(c.streams))
	for i, s := range c.streams {
		names[i] = s.Path
	}
	return link.Session{
		RTSPBase: "rtsp://" + net.JoinHostPort(host, strconv.Itoa(c.rtspPort)),
		User:     c.creds.User, Pass: c.creds.Pass, Streams: names, BeaconPort: c.beaconPort, Needles: c.needles,
	}
}

// startRun announces a run: the controller keeps its beacons, marks the
// pacing, and watches for the beacons' end, near the stop. A run of an
// unknown kind, or a smoke run outside the lab, is refused in Romanian.
func (c *ctl) startRun(conn *link.Conn, m link.RunStart) {
	kind := controller.ShutdownKind(m.Kind)
	var refusal string
	switch {
	case !kind.Known():
		refusal = fmt.Sprintf("tipul de oprire „%s” nu este cunoscut", m.Kind)
	case m.Smoke && !strings.HasPrefix(m.Set, "lab-"):
		refusal = "rulările scurte (smoke) sunt doar pentru laborator"
	}
	if refusal != "" {
		c.say("Rularea %s nu se măsoară: %s.", m.RunID, refusal)
		c.reply(conn, link.Refused{RunID: m.RunID, Reason: refusal})
		return
	}
	c.beacons.Expect(m.RunID)
	ctx, cancel := context.WithCancel(c.ctx)
	r := &runState{ctx: ctx, cancel: cancel, start: m, startMark: c.pacer.MarkPacing(), cut: controller.Cut{RunID: m.RunID, Kind: kind}, conn: conn}
	c.mu.Lock()
	if old := c.runs[m.RunID]; old != nil {
		old.cancel() // announced again: the new announcement's schedule counts
	}
	c.runs[m.RunID] = r
	c.mu.Unlock()
	go c.markEnd(r)
	c.say("Rularea %s (%s, %s) a început.", m.RunID, m.Set, m.Kind)
}

// markEnd marks the end of the run's pacing when its beacons go silent,
// shortly after the stop (R07).
func (c *ctl) markEnd(r *runState) {
	if _, err := c.beacons.WaitSilence(r.ctx, r.start.RunID, c.clock); err != nil {
		return // the run ended first
	}
	mark := c.pacer.MarkPacing()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runs[r.start.RunID] == r {
		r.endMark = &mark
	}
}

// ready schedules the run's cut once; a repeated ready, on any connection,
// only moves the cut's delivery to that connection. A host that repeats it
// after its cut went out, on a link that dropped meanwhile, gets the same
// cut again, on the new connection. A run that the controller does not
// know is refused.
func (c *ctl) ready(conn *link.Conn, runID string) {
	c.mu.Lock()
	r := c.runs[runID]
	scheduled := r != nil && r.scheduled
	var sent *link.Cut
	if r != nil {
		r.conn, r.scheduled = conn, true
		if scheduled && !r.cut.PromptAt.IsZero() {
			sent = &link.Cut{RunID: runID, Kind: string(r.cut.Kind), PromptAt: r.cut.PromptAt}
		}
	}
	c.mu.Unlock()
	switch {
	case r == nil:
		c.say("Rularea %s nu a fost anunțată; nu se programează nicio oprire.", runID)
		c.reply(conn, link.Refused{RunID: runID, Reason: unknownRun})
	case sent != nil:
		c.say("Rularea %s: gazda a trimis din nou „ready” după oprire; îi trimit oprirea din nou.", runID)
		c.reply(conn, *sent)
	case scheduled:
		c.say("Rularea %s: gazda a trimis din nou „ready”; oprirea rămâne cea programată.", runID)
	default:
		go c.scheduleCut(r)
	}
}

// scheduleCut waits for the run's first beacon (BootAt), draws the moment
// of the cut, and sends `cut` then on the host's latest connection; for a
// real cut it shows the prompt then, whatever became of the connection. A
// run that ended before its moment (its report, the host's refusal) gets
// neither: its cut would stop the host's next run at a moment never drawn
// for it. A run without a beacon firstBeaconWait after its ready is
// refused.
func (c *ctl) scheduleCut(r *runState) {
	runID := r.start.RunID
	boot, err := c.waitFirstBeacon(r)
	if err != nil {
		if r.ctx.Err() == nil {
			c.refuseSilent(r)
		}
		return // the run ended, or the controller stops
	}
	c.mu.Lock()
	var promptAt time.Time
	if r.start.Smoke {
		promptAt = controller.SchedulePromptWithin(boot.At, smokePromptFrom, smokePromptTo, c.rnd)
	} else {
		promptAt = controller.SchedulePrompt(boot.At, r.cut.Kind, c.rnd)
	}
	c.mu.Unlock()
	if r.cut.Kind.Automated() {
		c.say("Rularea %s: oprirea automată e programată la %s.", runID, promptAt.Format("15:04:05"))
	}
	if err := sleepUntil(r.ctx, c.clock, promptAt); err != nil {
		return
	}
	c.mu.Lock()
	if r.ctx.Err() != nil || c.runs[runID] != r {
		c.mu.Unlock()
		return // the run ended as its moment came
	}
	if !r.cut.Kind.Automated() {
		promptAt = c.clock.Now()
	}
	r.cut.BootAt, r.cut.PromptAt = boot.At, promptAt
	conn := r.conn
	c.mu.Unlock()
	if !r.cut.Kind.Automated() {
		c.say(">>> %s <<< (rularea %s)", mi1.RealCutPrompt, runID)
	}
	if err := conn.Send(link.Cut{RunID: runID, Kind: string(r.cut.Kind), PromptAt: promptAt}); err != nil {
		c.say("Rularea %s: nu pot trimite oprirea gazdei (legătura s-a întrerupt): %v", runID, err)
	}
}

// waitFirstBeacon waits for the run's first beacon until the run ends or
// the clock passes firstBeaconWait from now.
func (c *ctl) waitFirstBeacon(r *runState) (controller.Beacon, error) {
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	deadline := c.clock.Now().Add(firstBeaconWait)
	go func() {
		if sleepUntil(ctx, c.clock, deadline) == nil {
			cancel()
		}
	}()
	b, err := c.beacons.WaitFirst(ctx, r.start.RunID)
	if first, ok := c.beacons.First(r.start.RunID); err != nil && ok && r.ctx.Err() == nil {
		return first, nil // it came as the wait ended
	}
	return b, err
}

// refuseSilent drops a run whose host never beaconed, and tells the host,
// which stops waiting for its cut.
func (c *ctl) refuseSilent(r *runState) {
	runID := r.start.RunID
	c.mu.Lock()
	if c.runs[runID] != r {
		c.mu.Unlock()
		return
	}
	delete(c.runs, runID)
	conn := r.conn
	c.mu.Unlock()
	r.cancel()
	c.beacons.Forget(runID)
	c.say("Rularea %s: %s la un minut după „ready”; rularea nu se măsoară (verificați firewall-ul pentru portul %d).", runID, noBeacon, c.beaconPort)
	c.reply(conn, link.Refused{RunID: runID, Reason: noBeacon})
}

// tick is how often sleepUntil looks at the clock.
const tick = 5 * time.Millisecond

// sleepUntil waits until the clock reaches t.
func sleepUntil(ctx context.Context, clock source.Clock, t time.Time) error {
	ticker := clock.NewTicker(tick)
	defer ticker.Stop()
	for clock.Now().Before(t) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C():
		}
	}
	return nil
}

// checkBeacon answers a preflight's beacon check: whether a beacon of the
// check's id arrives within beaconCheckWait (R08).
func (c *ctl) checkBeacon(conn *link.Conn, id string) {
	c.beacons.Expect(id)
	defer c.beacons.Forget(id)
	ctx, cancel := context.WithTimeout(c.ctx, beaconCheckWait)
	defer cancel()
	_, err := c.beacons.WaitFirst(ctx, id)
	c.reply(conn, link.BeaconAck{ID: id, Received: err == nil})
}

// hostRefused drops a run that the host ended without its stop, with its
// schedule.
func (c *ctl) hostRefused(m link.Refused) {
	c.mu.Lock()
	if r := c.runs[m.RunID]; r != nil {
		r.cancel()
		delete(c.runs, m.RunID)
	}
	c.mu.Unlock()
	c.beacons.Forget(m.RunID)
	c.say("Gazda a renunțat la rularea %s: %s; rularea se reface.", m.RunID, m.Reason)
}

// The reasons why a run does not count, in Romanian.
const (
	noBeacon      = "niciun semnal de la gazdă"
	beaconGap     = "semnalul gazdei s-a întrerupt peste 2 s în timpul rulării (verificați rețeaua)"
	outsideWindow = "oprire în afara ferestrei"
	unevenPacing  = "controlorul a trimis neuniform"
	unknownRun    = "rularea nu a fost anunțată controlorului"
	notWritten    = "rezultatul nu s-a putut scrie la controlor"
)

// hubEvent is what the controller reads of ipcam-hub's reconcile and
// verify events, as the host sent them.
type hubEvent struct {
	Aborted           bool `json:"aborted"`
	Final             bool `json:"final"`
	Unreadable        int  `json:"unreadable"`
	TornParts         int  `json:"tornParts"`
	CatalogMismatches int  `json:"catalogMismatches"`
}

// report judges a run when the host's report arrives (C16): the stop moment
// from the beacons, the validity of the cut, the controller's pacing up to
// the beacons' end, and each stream's loss from the send log; it writes
// the run's envelope and returns the verdict. A report sent again gets the
// same verdict. A run whose envelope cannot be written does not count: the
// host redoes it, so no set is complete without its envelopes.
func (c *ctl) report(m link.Report) results.Run {
	c.mu.Lock()
	r := c.runs[m.RunID]
	judged, again := c.judged[m.RunID]
	c.mu.Unlock()
	switch {
	case r == nil && again:
		return judged
	case r == nil:
		c.say("Raport pentru rularea necunoscută %s: rularea se reface.", m.RunID)
		return results.Run{RunID: m.RunID, InvalidReason: unknownRun}
	}
	beacons := c.beacons.Beacons(m.RunID)
	interrupted := c.beacons.Interrupted(m.RunID)
	stopAt, errBound := controller.StopMoment(r.cut.Kind, beacons)
	c.mu.Lock()
	r.cut.StopAt = stopAt
	if r.cut.BootAt.IsZero() && len(beacons) > 0 {
		r.cut.BootAt = beacons[0].At // no cut was sent: the run's first beacon
	}
	cut, end := r.cut, r.endMark
	c.mu.Unlock()
	if end == nil {
		mark := c.pacer.MarkPacing() // the report came before the silence was seen
		end = &mark
	}
	pacing := c.pacer.Pacing(r.startMark, *end, stopAt)
	run := c.judge(r, m, cut, len(beacons), interrupted, errBound, pacing)

	env := results.Envelope{Schema: results.Schema, Code: "MI1", RunID: m.RunID, Set: r.start.Set, Host: r.start.Host, OS: r.start.OS,
		PiModel: r.start.PiModel, Storage: r.start.Storage, SdConfirmation: r.start.SdConfirmation, Runs: []results.Run{run}}
	written := results.Write(c.results, env)
	if written != nil {
		c.say("Rularea %s: nu pot scrie rezultatul (%v); rularea nu contează.", m.RunID, written)
		run.Valid, run.InvalidReason = false, addReason(run.InvalidReason, notWritten)
	}
	c.mu.Lock()
	r.cancel()
	if c.runs[m.RunID] == r {
		delete(c.runs, m.RunID)
	}
	if written == nil {
		c.judged[m.RunID] = run // a report sent again gets this verdict
	}
	c.mu.Unlock()
	c.beacons.Forget(m.RunID)

	verdict := "validă"
	if run.InvalidReason != "" {
		verdict = "nevalidă (" + run.InvalidReason + "), se reface"
	}
	var losses []string
	for _, s := range run.Streams {
		losses = append(losses, fmt.Sprintf("%s %s s", s.Stream, seconds(time.Duration(s.LossMs)*time.Millisecond)))
	}
	c.say("Rularea %s: %s; pierdere %s (± %d ms); ritm p99 %d ms, cadre sărite %d.",
		m.RunID, verdict, strings.Join(losses, ", "), errBound.Milliseconds(), pacing.P99.Milliseconds(), pacing.SkippedAUs)
	return run
}

// addReason adds a reason why a run does not count to the ones it has.
func addReason(reasons, reason string) string {
	if reasons == "" {
		return reason
	}
	return reasons + "; " + reason
}

// judge is the run as its envelope holds it. A stream that the host could
// not read, or whose index has no send time, lost everything since the
// run's first beacon. A run whose beacons came back after a silence
// (interrupted) is invalid for that reason: its stop is not its last kept
// beacon.
func (c *ctl) judge(r *runState, m link.Report, cut controller.Cut, beacons int, interrupted bool, errBound time.Duration,
	pacing controller.PacingStats) results.Run {
	valid := controller.Valid(cut)
	if r.start.Smoke {
		valid = controller.ValidWithin(cut, smokeValidFrom, smokeValidTo)
	}
	run := results.Run{
		RunID: m.RunID, Valid: valid, Slot: r.start.Slot, Attempt: r.start.Attempt, ShutdownKind: r.start.Kind,
		Encrypted: r.start.Encrypted, PadLowBitrate: r.start.PadLowBitrate, ErrBoundMs: errBound.Milliseconds(),
		Hub04Clear: m.Hub04Clear, PacingP99Ms: pacing.P99.Milliseconds(), SkippedAUs: pacing.SkippedAUs,
		ResumeMs: m.ResumeMs, Cameras: len(r.start.Streams), ColdBootMs: m.ColdBootMs, NtpWaitMs: m.NtpWaitMs,
		NtpSynced: m.NtpSynced, WriteCache: m.WriteCache,
	}
	switch {
	case beacons == 0:
		run.Valid, run.InvalidReason = false, noBeacon
	case interrupted:
		run.Valid, run.InvalidReason = false, beaconGap
	case !valid:
		run.InvalidReason = outsideWindow
	case pacing.P99.Milliseconds() > mi1.MaxPacingP99Ms || pacing.SkippedAUs > 0:
		run.InvalidReason = unevenPacing
	}
	var rec, ver hubEvent
	recOK := len(m.Reconcile) > 0 && json.Unmarshal(m.Reconcile, &rec) == nil
	verOK := len(m.Verify) > 0 && json.Unmarshal(m.Verify, &ver) == nil
	run.Reconciled = recOK && verOK && !rec.Aborted && rec.Final && ver.CatalogMismatches == 0
	run.Unreadable, run.TornParts = max(rec.Unreadable, ver.Unreadable), max(rec.TornParts, ver.TornParts)
	for _, stream := range r.start.Streams {
		loss := cut.StopAt.Sub(cut.BootAt)
		for _, s := range m.Streams {
			if s.Stream != stream {
				continue
			}
			if l, ok := controller.Loss(cut.StopAt, stream, s.LastRecordedIndex, c.sendLog); ok {
				loss = l
			}
		}
		kbps := mi1.BitrateKbps(stream)
		run.Streams = append(run.Streams, results.StreamLoss{
			Stream: stream, BitrateKbps: kbps, Encrypted: r.start.Encrypted, PadLowBitrate: r.start.PadLowBitrate, LossMs: loss.Milliseconds(),
			LimitMs: mi1.StreamLimitMs(kbps*1000, r.start.Encrypted, r.start.PadLowBitrate),
		})
	}
	run.LossMs = mi1.RunLossMs(run)
	return run
}

// seconds formats a duration in seconds with a decimal comma.
func seconds(d time.Duration) string {
	return strings.Replace(strconv.FormatFloat(d.Seconds(), 'f', 2, 64), ".", ",", 1)
}

// saveResult writes a results file that the host sent, under results/.
func (c *ctl) saveResult(m link.Results) {
	name := filepath.FromSlash(m.Name)
	if !filepath.IsLocal(name) {
		c.say("Am refuzat rezultatul cu numele nepermis %q.", m.Name)
		return
	}
	if err := writeUnder(c.results, name, m.Data); err != nil {
		c.say("Eroare la salvarea rezultatului %s: %v", m.Name, err)
		return
	}
	c.say("Am primit %s.", m.Name)
}

// writeUnder writes data to dir/name through an os.Root, so that nothing
// lands outside dir.
func writeUnder(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if parent := filepath.Dir(name); parent != "." {
		if err := root.MkdirAll(parent, 0o750); err != nil {
			return err
		}
	}
	return root.WriteFile(name, data, 0o600)
}
