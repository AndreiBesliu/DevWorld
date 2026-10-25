// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Command reccontroller is the T43 controller of the recorder spike. It runs
// on the device that is not under test and holds every inbound listener
// (R08): it serves the simulated cameras over RTSP with an SEI index in each
// access unit, receives the host's beacon, schedules each run's cut, shows
// the prompt of a real cut, computes the loss on its own clock and writes
// each MI1 run's envelope (C16), and keeps the results that the host sends
// in results/ next to the binary. `reccontroller pull` is the final
// collection on the PC: it waits for the Pi's `recspike push` and stores
// its results under results/pi/ (G15). Every line it prints is Romanian;
// its log goes to reccontroller.log next to the binary.
//
//	reccontroller [-local] [-media DIR]
//	reccontroller pull
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// realCutPrompt is the prompt of a real cut, which recspike mirrors.
const realCutPrompt = "Scoateți acum alimentarea"

// The smoke runs' windows (R03): the prompt 20 to 40 s after the first
// beacon, and a cut valid 10 s to 60 s after it.
const (
	smokePromptFrom, smokePromptTo = 20 * time.Second, 40 * time.Second
	smokeValidFrom, smokeValidTo   = 10 * time.Second, 60 * time.Second
)

// beaconCheckWait is how long a preflight's beacon check waits for the
// host's first beacon.
const beaconCheckWait = 5 * time.Second

// usage is the command's help, in Romanian.
const usage = `Folosire:
  reccontroller [-local] [-media DOSAR]
      servește camerele simulate și măsoară rulările gazdei.
      -local   ascultă doar pe 127.0.0.1, pentru o gazdă pe același dispozitiv (proba de 72 h)
      -media   dosarul cu main.h264, sub.h264 și sub-256k.h264 (implicit: media lângă reccontroller)
  reccontroller pull
      așteaptă rezultatele Pi-ului (./recspike push --controller <adresa PC-ului>) și le pune în results/pi.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "reccontroller: eroare:", err)
		}
		stop()
		os.Exit(1)
	}
}

// run runs the command in args until ctx is done.
func run(ctx context.Context, args []string, out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	if len(args) > 0 && args[0] == "pull" {
		if len(args) > 1 {
			_, _ = fmt.Fprint(out, usage)
			return fmt.Errorf("argument necunoscut: %s", args[1])
		}
		var lc net.ListenConfig
		ln, err := lc.Listen(ctx, "tcp4", ":"+strconv.Itoa(link.Port))
		if err != nil {
			return fmt.Errorf("nu pot asculta pe portul %d (rulează deja un reccontroller?): %w", link.Port, err)
		}
		return pull(ctx, ln, filepath.Join(dir, "results"), out)
	}
	flags := flag.NewFlagSet("reccontroller", flag.ContinueOnError)
	local := flags.Bool("local", false, "")
	media := flags.String("media", filepath.Join(dir, "media"), "")
	if err := parseFlags(flags, args, out); err != nil {
		return err
	}
	return serve(ctx, serveConfig{dir: dir, local: *local, media: *media, listen: controller.DefaultListenConfig(*local)}, out)
}

// parseFlags parses args and, on an error, prints it and the usage in
// Romanian, never the flag package's English.
func parseFlags(flags *flag.FlagSet, args []string, out io.Writer) error {
	flags.SetOutput(io.Discard)
	err := flags.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		_, _ = fmt.Fprint(out, usage)
		return err
	case err != nil:
		_, _ = fmt.Fprint(out, usage)
		return errors.New(romanianFlagError(err))
	case flags.NArg() > 0:
		_, _ = fmt.Fprint(out, usage)
		return fmt.Errorf("argument necunoscut: %s", flags.Arg(0))
	}
	return nil
}

// romanianFlagError translates the flag package's errors.
func romanianFlagError(err error) string {
	msg := err.Error()
	for _, t := range []struct{ en, ro string }{
		{"flag provided but not defined: ", "opțiune necunoscută: "},
		{"flag needs an argument: ", "opțiunea cere o valoare: "},
		{"bad flag syntax: ", "opțiune scrisă greșit: "},
	} {
		if rest, ok := strings.CutPrefix(msg, t.en); ok {
			return t.ro + rest
		}
	}
	if strings.HasPrefix(msg, "invalid ") {
		if _, rest, ok := strings.Cut(msg, " for "); ok {
			flagName, _, _ := strings.Cut(strings.TrimPrefix(rest, "flag "), ":")
			return "valoare greșită pentru " + flagName
		}
	}
	return "opțiuni greșite: " + msg
}

// serveConfig is where reccontroller serves and keeps its results.
type serveConfig struct {
	dir, media string
	local      bool
	listen     controller.ListenConfig
}

// serve serves until ctx is done.
func serve(ctx context.Context, cfg serveConfig, out io.Writer) error {
	logFile, err := os.OpenFile(filepath.Join(cfg.dir, "reccontroller.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // next to the binary
	if err != nil {
		return fmt.Errorf("nu pot deschide jurnalul: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))

	creds, err := controller.NewCredentials()
	if err != nil {
		return err
	}
	lns, err := controller.ListenAll(ctx, cfg.listen)
	if err != nil {
		return fmt.Errorf("nu pot asculta pe porturile probei (rulează deja un reccontroller?): %w", err)
	}
	defer func() { _ = lns.Close() }()

	clock := source.SystemClock{}
	sendLog := controller.NewSendLog()
	streams := controller.Streams(cfg.media)
	srv, err := controller.ServePrecodedOn(ctx, lns.RTSP, streams, creds, sendLog, clock)
	if err != nil {
		return fmt.Errorf("lipsesc sau nu se pot citi fișierele video din %s: %w", cfg.media, err)
	}
	defer srv.Close()
	needles, err := mi1.MediaNeedles(mediaFiles(streams)...)
	if err != nil {
		return fmt.Errorf("nu pot citi fișierele video din %s: %w", cfg.media, err)
	}
	c := newCtl(ctx, ctlConfig{
		pacer: srv, sendLog: sendLog, clock: clock, out: out, results: filepath.Join(cfg.dir, "results"), local: cfg.local,
		rtspPort: lns.RTSP.Addr().(*net.TCPAddr).Port, beaconPort: lns.Beacon.LocalAddr().(*net.UDPAddr).Port,
		creds: creds, streams: streams, needles: needles,
	})
	go superviseBeacons(ctx, func(ctx context.Context) error {
		return controller.ReceiveBeacons(ctx, lns.Beacon, c.beacons, clock)
	}, c.say)
	// Tasks 9 to 11 serve the app endpoints and the upload sink here.
	for _, ln := range []net.Listener{lns.App, lns.Upload} {
		hs := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = hs.Serve(ln) }()
		defer func() { _ = hs.Close() }()
	}
	where := "pe toate interfețele IPv4"
	if cfg.local {
		where = "doar pe acest dispozitiv (127.0.0.1)"
	}
	c.say("reccontroller rulează %s: RTSP %d, legătură %d, semnal %d, aplicație %d, încărcare %d.",
		where, cfg.listen.RTSPPort, cfg.listen.LinkPort, cfg.listen.BeaconPort, cfg.listen.AppPort, cfg.listen.UploadPort)
	c.say("Rezultatele se păstrează în %s. Oprire: Ctrl+C.", c.results)

	go func() {
		<-ctx.Done()
		_ = lns.Link.Close()
	}()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := lns.Link.Accept()
		if err != nil {
			if ctx.Err() != nil {
				c.say("reccontroller se oprește.")
				return nil
			}
			return err
		}
		wg.Go(func() { c.serve(ctx, link.NewConn(conn), conn.LocalAddr()) })
	}
}

// mediaFiles is each media file of the streams, once.
func mediaFiles(streams []controller.Stream) []string {
	var files []string
	seen := map[string]bool{}
	for _, s := range streams {
		if !seen[s.File] {
			seen[s.File] = true
			files = append(files, s.File)
		}
	}
	return files
}

// beaconRestartDelay is how long superviseBeacons waits before it starts a
// failed receiver again.
var beaconRestartDelay = time.Second

// superviseBeacons runs the beacon receiver until ctx ends. A receiver
// that fails is shown on the screen and started again after a second, since
// a run measured without beacons is invalid; after 10 failures in a row it
// stops for good and says so.
func superviseBeacons(ctx context.Context, receive func(context.Context) error, say func(string, ...any)) {
	const maxFailures = 10
	for failures := 1; ; failures++ {
		err := receive(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		if failures >= maxFailures {
			say("Recepția semnalului de la gazdă s-a oprit definitiv: %v. Reporniți reccontroller; rulările de acum nu se pot măsura.", err)
			return
		}
		say("Recepția semnalului de la gazdă s-a oprit: %v. O repornesc.", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(beaconRestartDelay):
		}
	}
}

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
// schedule runs under the controller's context, not a connection's.
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
	r := &runState{start: m, startMark: c.pacer.MarkPacing(), cut: controller.Cut{RunID: m.RunID, Kind: kind}, conn: conn}
	c.mu.Lock()
	c.runs[m.RunID] = r
	c.mu.Unlock()
	go c.markEnd(m.RunID)
	c.say("Rularea %s (%s, %s) a început.", m.RunID, m.Set, m.Kind)
}

// markEnd marks the end of the run's pacing when its beacons go silent,
// shortly after the stop (R07).
func (c *ctl) markEnd(runID string) {
	if _, err := c.beacons.WaitSilence(c.ctx, runID, c.clock); err != nil {
		return
	}
	mark := c.pacer.MarkPacing()
	c.mu.Lock()
	defer c.mu.Unlock()
	if r := c.runs[runID]; r != nil {
		r.endMark = &mark
	}
}

// ready schedules the run's cut once; a repeated ready, on any connection,
// only moves the cut's delivery to that connection.
func (c *ctl) ready(conn *link.Conn, runID string) {
	c.mu.Lock()
	r := c.runs[runID]
	scheduled := r != nil && r.scheduled
	if r != nil {
		r.conn, r.scheduled = conn, true
	}
	c.mu.Unlock()
	switch {
	case r == nil:
		c.say("Rularea %s nu a fost anunțată; nu se programează nicio oprire.", runID)
	case scheduled:
		c.say("Rularea %s: gazda a trimis din nou „ready”; oprirea rămâne cea programată.", runID)
	default:
		go c.scheduleCut(r)
	}
}

// scheduleCut waits for the run's first beacon (BootAt), draws the moment
// of the cut, and sends `cut` then on the host's latest connection; for a
// real cut it shows the prompt then, whatever became of the connection.
func (c *ctl) scheduleCut(r *runState) {
	runID := r.start.RunID
	boot, err := c.beacons.WaitFirst(c.ctx, runID)
	if err != nil {
		return // the controller stops
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
	if err := sleepUntil(c.ctx, c.clock, promptAt); err != nil {
		return
	}
	if !r.cut.Kind.Automated() {
		promptAt = c.clock.Now()
		c.say(">>> %s <<< (rularea %s)", realCutPrompt, runID)
	}
	c.mu.Lock()
	r.cut.BootAt, r.cut.PromptAt = boot.At, promptAt
	conn := r.conn
	c.mu.Unlock()
	if err := conn.Send(link.Cut{RunID: runID, Kind: string(r.cut.Kind), PromptAt: promptAt}); err != nil {
		c.say("Rularea %s: nu pot trimite oprirea gazdei (legătura s-a întrerupt): %v", runID, err)
	}
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

// hostRefused drops a run that the host ended without its stop.
func (c *ctl) hostRefused(m link.Refused) {
	c.mu.Lock()
	delete(c.runs, m.RunID)
	c.mu.Unlock()
	c.beacons.Forget(m.RunID)
	c.say("Gazda a renunțat la rularea %s: %s; rularea se reface.", m.RunID, m.Reason)
}

// The reasons why a run does not count, in Romanian.
const (
	noBeacon      = "niciun semnal de la gazdă"
	outsideWindow = "oprire în afara ferestrei"
	unevenPacing  = "controlorul a trimis neuniform"
	unknownRun    = "rularea nu a fost anunțată controlorului"
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
// same verdict.
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
	run := c.judge(r, m, cut, len(beacons), errBound, pacing)

	env := results.Envelope{Schema: results.Schema, Code: "MI1", RunID: m.RunID, Set: r.start.Set, Host: r.start.Host, OS: r.start.OS,
		PiModel: r.start.PiModel, Storage: r.start.Storage, SdConfirmation: r.start.SdConfirmation, Runs: []results.Run{run}}
	if err := results.Write(c.results, env); err != nil {
		c.say("Rularea %s: nu pot scrie rezultatul: %v", m.RunID, err)
	}
	c.mu.Lock()
	delete(c.runs, m.RunID)
	c.judged[m.RunID] = run
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

// judge is the run as its envelope holds it. A stream that the host could
// not read, or whose index has no send time, lost everything since the
// run's first beacon.
func (c *ctl) judge(r *runState, m link.Report, cut controller.Cut, beacons int, errBound time.Duration, pacing controller.PacingStats) results.Run {
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

// pull is `reccontroller pull` (G15): it waits on ln for the Pi's `recspike
// push`, stores the Pi's results folder under results/pi, and prints what
// both devices hold and, in Romanian, what is missing.
func pull(ctx context.Context, ln net.Listener, resultsDir string, out io.Writer) error {
	defer func() { _ = ln.Close() }()
	_, _ = fmt.Fprintln(out, "Aștept rezultatele Pi-ului: porniți pe Pi ./recspike push --controller <adresa acestui PC>.")
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()
	for {
		nc, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		done, err := pullFrom(link.NewConn(nc), resultsDir, out)
		_ = nc.Close()
		if err != nil {
			_, _ = fmt.Fprintf(out, "Transferul nu a reușit: %v. Porniți din nou push pe Pi.\n", err)
		}
		if done {
			return nil
		}
	}
}

// pullFrom pulls one host's results; done is true when the host was the Pi
// and its folder arrived.
func pullFrom(conn *link.Conn, resultsDir string, out io.Writer) (done bool, err error) {
	m, err := conn.Receive()
	if err != nil {
		return false, err
	}
	if hello, ok := m.(link.Hello); !ok || pseudonym(hello.Host) != "pi" {
		return false, errors.New("s-a conectat altă gazdă decât Pi-ul")
	}
	if err := conn.PullResults(filepath.Join(resultsDir, "pi")); err != nil {
		return false, err
	}
	_ = conn.Send(link.Bye{})
	_, _ = fmt.Fprintf(out, "Am pus rezultatele Pi-ului în %s.\n", filepath.Join(resultsDir, "pi"))
	piSupported := !unsupportedPi(resultsDir)
	if !piSupported {
		_, _ = fmt.Fprintln(out, "Modelul Pi-ului nu este suportat: nu se așteaptă rezultate cu Pi-ul ca gazdă.")
	}
	_, _ = fmt.Fprint(out, results.InventoryText(results.Inventory(resultsDir, piSupported)))
	return true, nil
}

// unsupportedPi reports whether an envelope of the results says that the
// Pi's model is known and unsupported.
func unsupportedPi(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" || found {
			return nil
		}
		if e, err := results.Read(path); err == nil && e.Host == "pi" && e.PiModel != nil && e.PiModel.Known && !e.PiModel.Supported {
			found = true
		}
		return nil
	})
	return found
}
