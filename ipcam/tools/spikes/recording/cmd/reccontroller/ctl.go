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
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
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
	// sinkPort and sinkCert are MI3's upload sink (G20); liveAPI makes its
	// live peers, nil on a -local controller, which MI3 does not use.
	sinkPort int
	sinkCert []byte
	liveAPI  *webrtc.API
	// mi9API makes MI9's playback peers, nil on a -local controller,
	// which MI9 does not use.
	mi9API *webrtc.API
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

	mu      sync.Mutex
	rnd     *rand.Rand
	runs    map[string]*runState
	judged  map[string]link.Verdict // the verdicts sent, for a report sent again
	steps   map[string]*mi3Step     // MI3's steps in progress (mi3.go)
	mi9Runs map[string]*mi9Run      // MI9's runs awaiting their answer (mi9.go)

	// saveMu serialises saveResult: results.Create looks before it writes,
	// so two connections that send one name at once could both pass the
	// look, and the second would replace the first's envelope.
	saveMu sync.Mutex
}

func newCtl(ctx context.Context, cfg ctlConfig) *ctl {
	return &ctl{
		ctlConfig: cfg, ctx: ctx, beacons: controller.NewBeaconLog(),
		rnd:  rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), //nolint:gosec // schedules cuts, not secrets
		runs: map[string]*runState{}, judged: map[string]link.Verdict{}, steps: map[string]*mi3Step{}, mi9Runs: map[string]*mi9Run{},
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
	// sent is the send times of the run's streams from its first beacon to
	// just after its stop, copied after its beacons went silent, once the
	// send log's feed had caught up: the send log may have dropped them
	// when a late report comes (mi1-1).
	sent      *controller.SendTimes
	cut       controller.Cut
	scheduled bool       // its cut is scheduled: a repeated ready changes nothing
	conn      *link.Conn // the host's latest connection that named the run
	// judging is made by the run's first report and closed once that
	// report has its verdict, in verdict: a report sent again meanwhile,
	// on another link, waits for it and gets that verdict, so the run is
	// judged, and its envelope written, once.
	judging chan struct{}
	verdict link.Verdict
}

// say prints one line on the controller's screen.
func (c *ctl) say(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = fmt.Fprintf(c.out, format+"\n", args...)
}

// serve handles one connection of a host until it ends: local is the
// address that the host reached, remote the host's own. The link takes any
// peer on the LAN, so a connection whose first message is not a valid
// hello (validHello) is closed with nothing done (security-2).
func (c *ctl) serve(ctx context.Context, conn *link.Conn, local, remote net.Addr) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		stop()
		// The connection's context ends before its MI3 steps and MI9 runs
		// are dropped: a wait under it (mi3Answers, mi9Answer) that fails
		// on a closed peer, or finds its step gone, knows that the host left.
		cancel()
		c.dropMI9(conn)
		c.dropSteps(conn)
		_ = conn.Close()
	}()
	host := ""
	for {
		m, err := conn.Receive()
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				slog.Warn("link ended", "err", err)
			}
			return
		}
		if hello, ok := m.(link.Hello); host == "" && (!ok || !validHello(hello)) {
			slog.Warn("link closed: its first message is not a valid hello", "type", m.Type())
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
			c.reply(conn, c.report(m))
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
		case link.Mi3Start:
			c.mi3Start(conn, m)
		case link.Mi3Answers:
			go c.mi3Answers(ctx, conn, m)
		case link.Mi3End:
			c.mi3End(conn, m)
		case link.Mi9Start:
			c.mi9Start(conn, m, local, remote)
		case link.Mi9Answer:
			go c.mi9Answer(ctx, conn, m)
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

// validHello reports whether m is a host's hello of this protocol, from
// one of the probe's devices.
func validHello(m link.Hello) bool {
	return m.Role == "host" && m.Version == link.Version && pseudonym(m.Host) == m.Host
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
		SinkPort: c.sinkPort, SinkCert: c.sinkCert,
	}
}

// startRun announces a run: the controller keeps its beacons, marks the
// pacing, and watches for the beacons' end, near the stop. A run of an
// unknown kind, or a smoke run outside the lab, is refused in Romanian. A
// run announced again, by a host whose link dropped and which cannot know
// whether the old link lost its run_start, keeps its schedule, its cut and
// its pacing start, and only moves to the new link, as a repeated ready
// does: a new announcement would draw its cut again and shorten its
// pacing. A run that the controller first hears of after its host began to
// beacon (its run_start was lost with a link, or this controller was
// started again) has lost those beacons, which the log drops for runs that
// it does not expect: its envelope says that its timing is partial
// (startedLate).
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
	c.mu.Lock()
	if known := c.runs[m.RunID]; known != nil {
		known.conn = conn
		c.mu.Unlock()
		c.say("Rularea %s: gazda a anunțat-o din nou, după o reconectare; rularea continuă.", m.RunID)
		return
	}
	c.mu.Unlock()
	c.beacons.Expect(m.RunID)
	ctx, cancel := context.WithCancel(c.ctx)
	r := &runState{ctx: ctx, cancel: cancel, start: m, startMark: c.pacer.MarkPacing(), cut: controller.Cut{RunID: m.RunID, Kind: kind}, conn: conn}
	c.mu.Lock()
	if old := c.runs[m.RunID]; old != nil {
		old.cancel() // announced twice at once: the latest announcement counts
	}
	c.runs[m.RunID] = r
	c.mu.Unlock()
	go c.markEnd(r)
	c.say("Rularea %s (%s, %s) a început.", m.RunID, m.Set, m.Kind)
}

// sentAfterStop is how long after its last beacon a run's copy of the
// send times reaches: a host stops within a beacon interval of its last
// beacon, and the copy is taken once the beacons have been silent this
// long.
const sentAfterStop = controller.BeaconSilence

// feedCatchUp bounds the wait for the send log's feed to pass the end of a
// run's copy: each simulator buffers about 12 s of its sends
// (source.SentLog), so a feed further behind has dropped some of them
// already.
const feedCatchUp = 15 * time.Second

// markEnd marks the end of the run's pacing when its beacons go silent,
// shortly after the stop (R07), and copies the send times that the run's
// judgement needs, from its first beacon to sentAfterStop after its last
// (mi1-1). The send log's feed is asynchronous, so the copy waits until
// the feed has passed the copy's end, at most feedCatchUp after it; a feed
// that has not caught up by then is named on the screen, and the copy
// holds what it has.
func (c *ctl) markEnd(r *runState) {
	runID := r.start.RunID
	last, err := c.beacons.WaitSilence(r.ctx, runID, c.clock)
	if err != nil {
		return // the run ended first
	}
	mark := c.pacer.MarkPacing()
	c.mu.Lock()
	if c.runs[runID] == r {
		r.endMark = &mark
	}
	c.mu.Unlock()
	from, to := last.At, last.At.Add(sentAfterStop)
	if first, ok := c.beacons.First(runID); ok {
		from = first.At
	}
	caughtUp := c.waitFed(r, to)
	if r.ctx.Err() != nil {
		return // the run ended first, judged from the live log
	}
	if !caughtUp {
		c.say("Rularea %s: jurnalul trimiterilor a rămas în urmă la oprire; orele de trimitere copiate pentru raport pot fi incomplete.", runID)
	}
	sent := c.sendLog.Times(r.start.Streams, from, to)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runs[runID] == r {
		r.sent = sent
	}
}

// waitFed waits until the send log has every frame of the run's streams
// sent up to to, at most until the clock passes to + feedCatchUp, and
// reports whether it got them.
func (c *ctl) waitFed(r *runState, to time.Time) bool {
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	go func() {
		if sleepUntil(ctx, c.clock, to.Add(feedCatchUp)) == nil {
			cancel()
		}
	}()
	return c.sendLog.WaitFed(ctx, r.start.Streams, to)
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
	noBeacon       = "niciun semnal de la gazdă"
	beaconGap      = "semnalul gazdei s-a întrerupt peste 2 s în timpul rulării (verificați rețeaua)"
	outsideWindow  = "oprire în afara ferestrei"
	unevenPacing   = "controlorul a trimis neuniform"
	unknownRun     = "rularea nu a fost anunțată controlorului"
	notWritten     = "rezultatul nu s-a putut scrie la controlor"
	envelopeExists = "există deja la controlor un rezultat cu numele acestei rulări: nu îl suprascriu"
	lateStart      = "rularea a ajuns la controlor abia după ce pornise (după o reconectare): pornirea și ritmul ei sunt măsurate doar în parte"
)

// lostFirstBeacons is how many of a run's first beacons the network may
// lose, BeaconSilence of them, before the controller takes the run's start
// as missed.
const lostFirstBeacons = uint64(controller.BeaconSilence / controller.BeaconEvery)

// startedLate reports whether the controller missed the start of a run
// whose first kept beacon is first. The host numbers a run's beacons from 0
// at ipcam-hub's ready, and the controller keeps them only from the run's
// run_start: a run_start that reached it after the beacons began, as one
// that a reconnection announced again, leaves a first beacon numbered past
// them. The run's BootAt, the draw of its cut from it (G09) and its pacing
// (R07) then start late, so the controller cannot tell whether the cut fell
// in its window or the pacing held: the run does not count, and is redone.
func startedLate(first controller.Beacon) bool { return first.Seq > lostFirstBeacons }

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
// same verdict: from memory once the run is judged, and, while its first
// report is still being judged (the host's link dropped meanwhile, and the
// host sent it again on a new one, on its own goroutine here), once that
// first report has its verdict. Judging it a second time would find the
// first report's envelope and call the run invalid on the new link, the
// one the host listens to, while that envelope stays counted. A run whose
// envelope cannot be written does not count: the host redoes it, so no set
// is complete without its envelopes. The verdict of a run whose stream lost
// more than its limit also carries the controller's side of the run's
// diagnostics (lossTiming), a report sent again included.
func (c *ctl) report(m link.Report) link.Verdict {
	c.mu.Lock()
	r := c.runs[m.RunID]
	judged, again := c.judged[m.RunID]
	var judging, done chan struct{}
	if r != nil {
		if judging = r.judging; judging == nil {
			done = make(chan struct{})
			r.judging = done
		}
	}
	c.mu.Unlock()
	switch {
	case r == nil && again:
		return judged
	case r == nil:
		c.say("Raport pentru rularea necunoscută %s: rularea se reface.", m.RunID)
		return link.Verdict{Run: results.Run{RunID: m.RunID, InvalidReason: unknownRun}}
	case judging != nil:
		c.say("Rularea %s: gazda a trimis din nou raportul, după o reconectare; primește același verdict.", m.RunID)
		<-judging
		return r.verdict // set before judging closed
	}
	r.verdict = c.judgeReport(r, m) // read only once done is closed
	close(done)
	return r.verdict
}

// judgeReport is report's judgement of run r, by the run's first report:
// it writes the envelope, ends the run and returns its verdict, with the
// diagnostics' timing of a stream over its limit.
func (c *ctl) judgeReport(r *runState, m link.Report) link.Verdict {
	beacons := c.beacons.Beacons(m.RunID)
	interrupted := c.beacons.Interrupted(m.RunID)
	late := len(beacons) > 0 && startedLate(beacons[0])
	stopAt, errBound := controller.StopMoment(r.cut.Kind, beacons)
	c.mu.Lock()
	r.cut.StopAt = stopAt
	if r.cut.BootAt.IsZero() && len(beacons) > 0 {
		r.cut.BootAt = beacons[0].At // no cut was sent: the run's first beacon
	}
	cut, end := r.cut, r.endMark
	// The run's copy, then the live log: the frames that reached the log
	// after the copy are there, and a report before the copy has only it.
	var sent controller.SendTimer = c.sendLog
	if r.sent != nil {
		sent = controller.SendTimers{r.sent, c.sendLog}
	}
	c.mu.Unlock()
	if end == nil {
		mark := c.pacer.MarkPacing() // the report came before the silence was seen
		end = &mark
	}
	pacing := c.pacer.Pacing(r.startMark, *end, stopAt)
	run := c.judge(r, m, cut, beaconsSeen{n: len(beacons), interrupted: interrupted, late: late}, errBound, pacing, sent)

	// A lab smoke run's envelope goes under its own results set, apart from
	// the set's counted envelopes. An envelope of the same run id, on disk
	// already, is another run's and stays: this run does not count.
	env := results.Envelope{Schema: results.Schema, Code: "MI1", RunID: m.RunID, Set: mi1.ResultsSet(r.start.Set, r.start.Smoke), Host: r.start.Host, OS: r.start.OS,
		PiModel: r.start.PiModel, Storage: r.start.Storage, SdConfirmation: r.start.SdConfirmation, Runs: []results.Run{run}}
	written := results.Create(c.results, env)
	switch {
	case errors.Is(written, results.ErrExists):
		c.say("Rularea %s: rezultatul ei există deja; nu îl suprascriu, iar rularea nu contează.", m.RunID)
		run.Valid, run.InvalidReason = false, addReason(run.InvalidReason, envelopeExists)
	case written != nil:
		c.say("Rularea %s: nu pot scrie rezultatul (%v); rularea nu contează.", m.RunID, written)
		run.Valid, run.InvalidReason = false, addReason(run.InvalidReason, notWritten)
	}
	v := link.Verdict{Run: run, Timing: lossTiming(run, m, stopAt, sent)}
	c.mu.Lock()
	r.cancel()
	if c.runs[m.RunID] == r {
		delete(c.runs, m.RunID)
	}
	if written == nil {
		c.judged[m.RunID] = v // a report sent again gets this verdict
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
	return v
}

// lossTiming is the controller's side of the diagnostics of run, judged
// from m, when one of its streams lost more than its limit (mi1.OverLimit):
// the stop moment and the send times of those streams' last recorded
// access units, from sent, on the controller's clock; nil when every
// stream kept within its limit.
func lossTiming(run results.Run, m link.Report, stopAt time.Time, sent controller.SendTimer) *link.Timing {
	var timing *link.Timing
	for _, s := range run.Streams {
		if !mi1.OverLimit(s) {
			continue
		}
		if timing == nil {
			timing = &link.Timing{StopAtMs: stopAt.UnixMilli(), LastSentMs: map[string]int64{}}
		}
		for _, index := range m.Streams {
			if index.Stream != s.Stream {
				continue
			}
			if at, ok := sent.SendTime(s.Stream, index.LastRecordedIndex); ok {
				timing.LastSentMs[s.Stream] = at.UnixMilli()
			}
		}
	}
	return timing
}

// addReason adds a reason why a run does not count to the ones it has.
func addReason(reasons, reason string) string {
	if reasons == "" {
		return reason
	}
	return reasons + "; " + reason
}

// beaconsSeen is what the controller kept of a run's beacons: how many,
// whether they came back after a silence (interrupted), and whether its
// first one came late (startedLate).
type beaconsSeen struct {
	n                 int
	interrupted, late bool
}

// judge is the run as its envelope holds it, with the send times of sent.
// A stream that the host could not read, or whose index has no send time,
// lost everything since the run's first beacon. A run whose beacons came back after a silence
// (interrupted) is invalid for that reason: its stop is not its last kept
// beacon; so is a run whose start the controller missed (late), whose
// timing is partial.
func (c *ctl) judge(r *runState, m link.Report, cut controller.Cut, beacons beaconsSeen, errBound time.Duration,
	pacing controller.PacingStats, sent controller.SendTimer) results.Run {
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
	case beacons.n == 0:
		run.Valid, run.InvalidReason = false, noBeacon
	case beacons.interrupted:
		run.Valid, run.InvalidReason = false, beaconGap
	case beacons.late:
		run.Valid, run.InvalidReason = false, lateStart
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
			if l, ok := controller.Loss(cut.StopAt, stream, s.LastRecordedIndex, sent); ok {
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

// maxResultBytes bounds the one results file that a host sends: MI9's
// LAN envelope, a few KiB.
const maxResultBytes = 64 << 10

// saveResult keeps a results file that the host sent, under results/: the
// only one that a host sends, MI9's LAN envelope (mi9.LANSet), named
// MI9/lan/<its run id>.json, at most maxResultBytes, and only when no
// envelope of that name is there yet (security-2). Anything else is
// refused: the link takes any peer on the LAN, and the counted envelopes
// stay as they are. One connection saves at a time (saveMu).
func (c *ctl) saveResult(m link.Results) {
	refuse := func(why string) { c.say("Am refuzat un rezultat primit pe legătură: %s.", why) }
	if len(m.Data) > maxResultBytes {
		refuse("este mai mare de 64 KiB")
		return
	}
	e, err := results.Parse(m.Data)
	switch {
	case err != nil:
		refuse("nu este un rezultat al probei")
		return
	case e.Code != "MI9" || e.Set != mi9.LANSet || m.Name != path.Join("MI9", mi9.LANSet, e.RunID+".json"):
		refuse("o gazdă trimite doar rezultatul MI9 pe LAN, cu numele lui")
		return
	}
	c.saveMu.Lock()
	err = results.Create(c.results, e)
	c.saveMu.Unlock()
	switch {
	case errors.Is(err, results.ErrExists):
		refuse("există deja un rezultat cu numele " + m.Name + ", care rămâne")
	case err != nil:
		c.say("Eroare la salvarea rezultatului %s: %v", m.Name, err)
	default:
		c.say("Am primit %s.", m.Name)
	}
}
