// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Command reccontroller is the T43 controller of the recorder spike. It runs
// on the device that is not under test and holds every inbound listener
// (R08): it serves the simulated cameras over RTSP with an SEI index in each
// access unit, receives the host's beacon, schedules each run's cut, shows
// the prompt of a real cut, computes the loss on its own clock (C16), and
// keeps the results that the host sends in results/ next to the binary.
// Every line it prints is Romanian; its log goes to reccontroller.log next
// to the binary.
//
//	reccontroller [-local] [-media DIR]
package main

import (
	"context"
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
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// realCutPrompt is the prompt of a real cut, which recspike mirrors.
const realCutPrompt = "Scoateți acum alimentarea"

// maxPacingP99 is R07's bound on the controller's send jitter: a run above
// it, or with a skipped access unit, is invalid and redone.
const maxPacingP99 = 100 * time.Millisecond

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "reccontroller: eroare:", err)
		stop()
		os.Exit(1)
	}
}

// run serves until ctx is done.
func run(ctx context.Context, args []string, out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	flags := flag.NewFlagSet("reccontroller", flag.ContinueOnError)
	flags.SetOutput(out)
	local := flags.Bool("local", false, "ascultă doar pe 127.0.0.1, pentru o gazdă pe același dispozitiv (proba de 72 h)")
	media := flags.String("media", filepath.Join(dir, "media"), "dosarul cu main.h264, sub.h264 și sub-256k.h264")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return errors.New("nu primește argumente în afară de -local și -media")
	}

	logFile, err := os.OpenFile(filepath.Join(dir, "reccontroller.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // next to the binary
	if err != nil {
		return fmt.Errorf("nu pot deschide jurnalul: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))

	creds, err := controller.NewCredentials()
	if err != nil {
		return err
	}
	cfg := controller.DefaultListenConfig(*local)
	lns, err := controller.ListenAll(ctx, cfg)
	if err != nil {
		return fmt.Errorf("nu pot asculta pe porturile probei (rulează deja un reccontroller?): %w", err)
	}
	defer func() { _ = lns.Close() }()

	clock := source.SystemClock{}
	sendLog := controller.NewSendLog()
	streams := controller.Streams(*media)
	srv, err := controller.ServePrecodedOn(ctx, lns.RTSP, streams, creds, sendLog, clock)
	if err != nil {
		return fmt.Errorf("lipsesc sau nu se pot citi fișierele video din %s: %w", *media, err)
	}
	defer srv.Close()
	beacons := controller.NewBeaconLog()
	go func() {
		if err := controller.ReceiveBeacons(ctx, lns.Beacon, beacons, clock); err != nil {
			slog.Error("beacon receiver stopped", "err", err)
		}
	}()
	// Tasks 9 to 11 serve the app endpoints and the upload sink here.
	for _, ln := range []net.Listener{lns.App, lns.Upload} {
		hs := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: 10 * time.Second}
		go func() { _ = hs.Serve(ln) }()
		defer func() { _ = hs.Close() }()
	}

	c := &ctl{
		srv: srv, sendLog: sendLog, beacons: beacons, clock: clock, out: out,
		results: filepath.Join(dir, "results"), rtspPort: cfg.RTSPPort,
		creds: creds, streams: streams, runs: map[string]*runState{},
		rnd: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())), //nolint:gosec // schedules cuts, not secrets
	}
	where := "pe toate interfețele IPv4"
	if *local {
		where = "doar pe acest dispozitiv (127.0.0.1)"
	}
	c.say("reccontroller rulează %s: RTSP %d, legătură %d, semnal %d, aplicație %d, încărcare %d.",
		where, cfg.RTSPPort, cfg.LinkPort, cfg.BeaconPort, cfg.AppPort, cfg.UploadPort)
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

// ctl is the controller's state across the host's connections: a run's
// report arrives on a new connection after the host's reboot.
type ctl struct {
	srv      *controller.Server
	sendLog  *controller.SendLog
	beacons  *controller.BeaconLog
	clock    source.Clock
	out      io.Writer
	results  string
	rtspPort int
	creds    controller.Credentials
	streams  []controller.Stream

	mu   sync.Mutex
	rnd  *rand.Rand
	runs map[string]*runState
}

// runState is one run, from run_start to its report.
type runState struct {
	start link.RunStart
	mark  controller.PacingMark
	cut   controller.Cut
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
			c.say("Gazda „%s” s-a conectat.", pseudonym(m.Host))
			c.reply(conn, c.session(local))
		case link.RunStart:
			c.startRun(m)
		case link.Ready:
			go c.scheduleCut(ctx, conn, m.RunID)
		case link.Report:
			c.report(m)
		case link.Results:
			c.saveResult(m)
		case link.PullResults:
			if err := conn.SendResultsTar(c.results); err != nil {
				c.say("Eroare la trimiterea rezultatelor: %v", err)
			} else {
				c.say("Am trimis dosarul de rezultate.")
			}
		case link.Bye:
			c.say("Gazda s-a deconectat.")
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

// pseudonym keeps a host name off the screen and the log (PH0-22).
func pseudonym(host string) string {
	switch host {
	case "pc", "pi", "lab-linux", "lab-windows", "lab-qemu":
		return host
	}
	return "necunoscută"
}

// session tells the host where the cameras are: at the address it reached
// the controller on.
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
		User:     c.creds.User, Pass: c.creds.Pass, Streams: names,
	}
}

func (c *ctl) startRun(m link.RunStart) {
	kind := controller.ShutdownKind(m.Kind)
	if !kind.Known() {
		c.say("Rularea %s are un tip de oprire necunoscut și nu se măsoară.", m.RunID)
		return
	}
	c.beacons.Expect(m.RunID)
	r := &runState{start: m, mark: c.srv.MarkPacing(), cut: controller.Cut{RunID: m.RunID, Kind: kind}}
	c.mu.Lock()
	c.runs[m.RunID] = r
	c.mu.Unlock()
	c.say("Rularea %s (%s, %s) a început.", m.RunID, m.Set, m.Kind)
}

func (c *ctl) run(id string) *runState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runs[id]
}

// scheduleCut waits for the run's first beacon (BootAt), draws the moment
// of the cut, and sends `cut` then; for a real cut it also shows the prompt.
func (c *ctl) scheduleCut(ctx context.Context, conn *link.Conn, runID string) {
	r := c.run(runID)
	if r == nil {
		c.say("Rularea %s nu a fost anunțată; nu se programează nicio oprire.", runID)
		return
	}
	boot, err := c.beacons.WaitFirst(ctx, runID)
	if err != nil {
		return // the connection ended before the host beaconed
	}
	c.mu.Lock()
	promptAt := controller.SchedulePrompt(boot.At, r.cut.Kind, c.rnd)
	c.mu.Unlock()
	if r.cut.Kind.Automated() {
		c.say("Rularea %s: oprirea automată e programată la %s.", runID, promptAt.Format("15:04:05"))
	}
	timer := time.NewTimer(time.Until(promptAt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	if !r.cut.Kind.Automated() {
		promptAt = c.clock.Now()
		c.say(">>> %s <<<", realCutPrompt)
	}
	c.mu.Lock()
	r.cut.BootAt, r.cut.PromptAt = boot.At, promptAt
	c.mu.Unlock()
	c.reply(conn, link.Cut{RunID: runID, Kind: string(r.cut.Kind), PromptAt: promptAt})
}

// report judges a run when the host's report arrives: the stop moment from
// the beacons, the validity of the cut, the controller's pacing, and the
// loss of each stream from the send log (C16).
func (c *ctl) report(m link.Report) {
	r := c.run(m.RunID)
	if r == nil {
		c.say("Raport pentru rularea necunoscută %s: rularea se reface.", m.RunID)
		return
	}
	stopAt, errBound := controller.StopMoment(r.cut.Kind, c.beacons.Beacons(m.RunID))
	c.mu.Lock()
	r.cut.StopAt = stopAt
	cut := r.cut
	delete(c.runs, m.RunID)
	c.mu.Unlock()
	c.beacons.Forget(m.RunID)

	pacing := c.srv.Pacing(r.mark, stopAt)
	verdict := "validă"
	switch {
	case !controller.Valid(cut):
		verdict = "nevalidă (oprire în afara ferestrei), se reface"
	case pacing.P99 > maxPacingP99 || pacing.SkippedAUs > 0:
		verdict = "nevalidă (controlorul a trimis neuniform), se reface"
	}
	var losses []string
	for _, s := range m.Streams {
		if loss, ok := controller.Loss(stopAt, s.Stream, s.LastRecordedIndex, c.sendLog); ok {
			losses = append(losses, fmt.Sprintf("%s %s s", s.Stream, seconds(loss)))
		} else {
			losses = append(losses, s.Stream+" necunoscută")
		}
	}
	c.say("Rularea %s: %s; pierdere %s (± %d ms); ritm p99 %d ms, cadre sărite %d.",
		m.RunID, verdict, strings.Join(losses, ", "), errBound.Milliseconds(), pacing.P99.Milliseconds(), pacing.SkippedAUs)
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
