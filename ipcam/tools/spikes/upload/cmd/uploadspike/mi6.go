// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/handoff"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// readerWait bounds the web reader's session.
const readerWait = 30 * time.Minute

// runMI6 is `uploadspike mi6` (P21, P39, P53, C2-R1): it takes the hub,
// opens the MI6 run on day 0 (MI6's destinations join the hub's
// composition), prints the schedule, runs each pair's login (B1 the same
// day after a refused plan A), then the visibility matrix, and gives the
// hub back to the autostart; without the autostart it holds the hub until
// day 7. `mi6 --b1 <platform>` and `mi6 reader --hub` are its other forms.
func runMI6(ctx context.Context, e *env, args []string) error {
	if len(args) > 0 && args[0] == "reader" {
		return runReader(ctx, e, args[1:])
	}
	flags := flag.NewFlagSet("mi6", flag.ContinueOnError)
	drive := flags.String("drive", "test", "")
	oneDrive := flags.Bool("onedrive", false, "")
	platformList := flags.String("platforms", "android,windows,web", "")
	b1 := flags.String("b1", "", "")
	if err := cli.ParseFlags(flags, args, e.out, usage); err != nil {
		return err
	}
	switch *drive {
	case "test", "test+personal", "personal":
	default:
		return ownerError(fmt.Sprintf(msgUnknownDrive, *drive))
	}
	platforms := strings.Split(*platformList, ",")
	for _, p := range platforms {
		if !slices.Contains([]string{"android", "windows", "web"}, p) {
			return ownerError(fmt.Sprintf(msgUnknownPlatform, p))
		}
	}
	ctx, stop := signalContext(ctx)
	defer stop()
	if *b1 != "" {
		if !slices.Contains([]string{"android", "windows", "web"}, *b1) {
			return ownerError(fmt.Sprintf(msgUnknownPlatform, *b1))
		}
		return runB1(ctx, e, *b1)
	}
	h, err := acquire(ctx, e, "uploadspike mi6")
	if err != nil {
		return err
	}
	opened := false
	if err := runstate.Update(e.statePath(), func(s *runstate.State) error {
		if _, ok := s.Open["MI6"]; ok {
			return nil
		}
		withOneDrive := *oneDrive && s.EntraClientID != ""
		if *oneDrive && !withOneDrive {
			e.printf(msgOneDriveNeedsA5)
		}
		now := e.now()
		s.Open["MI6"] = mi6.NewRun("mi6-"+now.UTC().Format("20060102T150405Z"), now, *drive, withOneDrive, platforms).Encode()
		opened = true
		return nil
	}); err != nil {
		h.giveBack()
		return err
	}
	if err := h.startHub(ctx); err != nil {
		h.giveBack()
		return err
	}
	if err := h.serve(ctx); err != nil {
		h.giveBack()
		return err
	}
	e.printf(msgHubStarted)
	if opened {
		printSchedule(e)
	}
	return h.finish(ctx, h.loginsAndMatrix(ctx))
}

// finish ends an interactive holder after its foreground work: a handover
// gives the hub back (an open login's cut is said), a signal stops the
// hub and frees the lock for the autostart; otherwise the hub goes back to
// the autostart, or, without one, the command holds it until day 7.
func (h *holder) finish(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, errHandedOver):
		h.handBack(ctx)
		return nil
	case err != nil && ctx.Err() != nil:
		h.giveBack()
		h.e.printf(msgSignalHandBack)
		return ctx.Err()
	case err != nil:
		h.giveBack()
		return err
	}
	st, err := runstate.Load(h.e.statePath())
	if err != nil {
		h.giveBack()
		return err
	}
	if st.Autostart {
		h.giveBack()
		h.e.printf(msgBackToAutostart)
		return nil
	}
	h.e.printf(msgKeepWindowOpen)
	switch h.hold(ctx) {
	case holdHandover:
		h.handBack(ctx)
		return nil
	case holdSignal:
		h.giveBack()
		h.e.printf(msgSignalHandBack)
		return ctx.Err()
	}
	h.giveBack()
	return nil
}

// printSchedule prints the run's dated schedule (Programul probei).
func printSchedule(e *env) {
	d := func(n int) string { return e.now().AddDate(0, 0, n).Format(dateLayout) }
	e.printf(scheduleTitle)
	e.printf(scheduleDay0, d(0))
	e.printf(scheduleMI8, d(0), d(1))
	e.printf(scheduleMI5, d(1), d(5))
	e.printf(scheduleDaily)
	e.printf(scheduleHub, d(7))
	e.printf(scheduleEnd, d(7))
}

// loadRun reads the open MI6 run.
func loadRun(e *env) (*mi6.Run, *runstate.State, error) {
	st, err := runstate.Load(e.statePath())
	if err != nil {
		return nil, nil, err
	}
	raw, ok := st.Open["MI6"]
	if !ok {
		return nil, st, ownerError(msgNoMI6Run)
	}
	run, err := mi6.Decode(raw)
	return run, st, err
}

// loginsAndMatrix runs day 0's logins of every pair that has none yet, in
// order, then the visibility matrix, once.
func (h *holder) loginsAndMatrix(ctx context.Context) error {
	run, _, err := loadRun(h.e)
	if err != nil {
		return err
	}
	for _, p := range run.Pairs {
		if p.HandoffOK || p.Plan == mi6.PlanB1 && p.B1Failure != "" {
			continue
		}
		plan := mi6.PlanA
		if !p.HasPlanA() || p.PlanAFailure != "" {
			plan = mi6.PlanB1
		}
		if err := h.login(ctx, p.PairDef, plan); err != nil {
			return err
		}
	}
	h.e.printf(promptLoginsEnd)
	if run, _, err = loadRun(h.e); err != nil || run.LoginsDone {
		return err
	}
	if err := h.matrix(ctx); err != nil {
		return err
	}
	return runstate.Update(h.e.statePath(), func(s *runstate.State) error {
		run, err := mi6.Decode(s.Open["MI6"])
		if err != nil {
			return err
		}
		run.LoginsDone = true
		s.Open["MI6"] = run.Encode()
		return nil
	})
}

// remote reports whether a login of platform runs on another device than
// this one: Android always; Windows and web when uploadspike runs on Linux.
func remote(platform string) bool {
	return platform == "android" || runtime.GOOS == "linux"
}

// login waits for a pair's login under plan: it shows the platform's
// prompt and opens the LAN listener when the platform runs on another
// device, until the hub reports the login's end. A refused plan A runs B1
// at once, the same day, in this window (P39); a refused B1 is final (B2);
// a login that never reached the provider, or whose state expired, is
// asked again. A handover or a signal cuts it.
func (h *holder) login(ctx context.Context, def mi6.PairDef, plan string) error {
	platform, provider := mi6.PlatformName(def.Platform), mi6.ProviderName(def.Provider)
	defer h.await(def.Platform, handoff.Next{})
	for {
		h.await(def.Platform, handoff.Next{Dest: def.Dest, Provider: def.Provider, Plan: plan})
		if remote(def.Platform) {
			if err := h.srv.OpenLAN(ctx, def.Dest); err != nil {
				h.e.printf(msgNoLANAddress)
			} else {
				h.printAddresses()
			}
		}
		h.e.printf("%s", loginPrompt(def, plan))
		h.e.printf(promptWaiting, platform, provider)
		end, err := h.waitEnd(ctx, def.Dest)
		h.srv.CloseLAN()
		if err != nil {
			return err
		}
		if end.ok {
			h.e.printf(promptDone, platform, provider, end.plan)
			return nil
		}
		switch mi6.FailKind(end.reason) {
		case mi6.FailRefusal:
			if plan == mi6.PlanA {
				h.e.printf(promptPlanAFail, platform, provider, end.reason)
				plan = mi6.PlanB1
				continue
			}
			h.e.printf(promptB1Fail, platform, provider, end.reason)
			return nil
		case mi6.FailTransient:
			h.e.printf(promptTransient)
		default:
			h.e.printf(promptExpired)
		}
	}
}

// loginPrompt is the owner's prompt for a pair's login under plan.
func loginPrompt(def mi6.PairDef, plan string) string {
	provider := mi6.ProviderName(def.Provider)
	if plan == mi6.PlanB1 {
		return fmt.Sprintf(promptB1, mi6.PlatformName(def.Platform), provider)
	}
	switch def.Platform {
	case "android":
		return fmt.Sprintf(promptAndroidA, provider)
	case "windows":
		return fmt.Sprintf(promptWindowsA, provider)
	}
	return fmt.Sprintf(promptWebA, provider)
}

// printAddresses prints this device's LAN addresses for the other device.
func (h *holder) printAddresses() {
	addrs, _ := handoff.RFC1918Addrs()
	for _, ip := range addrs {
		h.e.printf(msgAddress, "http://"+net.JoinHostPort(ip.String(), strconv.Itoa(h.e.port)))
	}
}

// waitEnd waits for the end of dest's login, a handover or ctx's end.
func (h *holder) waitEnd(ctx context.Context, dest string) (loginEnd, error) {
	for {
		select {
		case end := <-h.ends:
			if end.dest == dest {
				return end, nil
			}
		case <-h.handoverRequested():
			return loginEnd{}, errHandedOver
		case <-ctx.Done():
			return loginEnd{}, ctx.Err()
		}
	}
}

// visibilityLine is diag visibility's line.
type visibilityLine struct {
	Schema string `json:"schema"`
	Listed bool   `json:"listed"`
	Read   bool   `json:"read"`
	Error  string `json:"error"`
}

// matrix measures the visibility matrix (P21): each hub creator × hub
// reader through diag visibility, as the catalog's owner, then the web
// reader's cells, which the reader page posts: on this device's browser,
// or, when uploadspike runs on Linux, on the PC through `mi6 reader --hub`
// and the LAN listener.
func (h *holder) matrix(ctx context.Context) error {
	run, st, err := loadRun(h.e)
	if err != nil {
		return err
	}
	byClient := map[string]string{}
	for _, p := range run.Pairs {
		if p.HandoffOK && p.Provider == mi6.ProviderDrive && p.ClientType != "" {
			if _, ok := byClient[p.ClientType]; !ok {
				byClient[p.ClientType] = p.Dest
			}
		}
	}
	var cells []results.VisibilityCell
	for _, want := range mi6.Matrix() {
		cd, ok1 := byClient[want.Creator]
		rd, ok2 := byClient[want.Reader]
		if want.Reader == "web" || !ok1 || !ok2 {
			continue
		}
		lines, err := hubproc.Diag(ctx, h.e.hubConfig(), []string{"visibility", "--config", h.e.runJSON(), "--creator", cd, "--reader", rd}, nil)
		var v visibilityLine
		if len(lines) > 0 {
			_ = json.Unmarshal(lines[0], &v)
		}
		if err != nil || v.Error != "" || v.Schema == "" {
			h.e.printf("%s visibility %s → %s: %v %s", msgError, want.Creator, want.Reader, err, v.Error)
			continue
		}
		want.Listed, want.Read = v.Listed, v.Read
		cells = append(cells, want)
	}
	if err := saveCells(h.e, cells); err != nil {
		return err
	}
	if st.WebClientID == "" {
		h.e.printf(pageNoClient)
	} else if err := h.webReader(ctx, byClient); err != nil {
		return err
	}
	run, _, err = loadRun(h.e)
	if err != nil {
		return err
	}
	measured := 0
	for _, c := range run.Visibility {
		if !c.Self {
			measured++
		}
	}
	h.e.printf(promptVisDone, measured)
	return nil
}

// webReader opens the reader session and records the web cells of the
// creators that have a login, until each came, a handover, or 30 min.
func (h *holder) webReader(ctx context.Context, byClient map[string]string) error {
	if runtime.GOOS == "linux" {
		// On the Pi the reader page runs on the PC; a Linux host with a
		// browser of its own reads on the loopback listener too.
		if err := h.srv.OpenLAN(ctx, "reader"); err != nil {
			h.e.printf(msgNoLANAddress)
		} else {
			defer h.srv.CloseLAN()
			if addrs, _ := handoff.RFC1918Addrs(); len(addrs) > 0 {
				h.e.printf(promptReaderPi, addrs[0].String())
			}
		}
	} else {
		h.e.printf(promptReader)
		openBrowser(h.e, fmt.Sprintf("http://localhost:%d/reader", h.e.port))
	}
	want := map[string]bool{}
	for _, c := range []string{"android", "desktop", "tv"} {
		if _, ok := byClient[c]; ok {
			want[c] = true
		}
	}
	timeout := time.After(readerWait)
	for len(want) > 0 {
		select {
		case c := <-h.webCells:
			if !want[c.Creator] {
				continue
			}
			delete(want, c.Creator)
			if err := saveCells(h.e, []results.VisibilityCell{c}); err != nil {
				return err
			}
		case <-h.handoverRequested():
			return errHandedOver
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return nil
		}
	}
	return nil
}

// saveCells records cells in the open MI6 run, each replacing the cell of
// its creator and reader.
func saveCells(e *env, cells []results.VisibilityCell) error {
	return runstate.Update(e.statePath(), func(s *runstate.State) error {
		run, err := mi6.Decode(s.Open["MI6"])
		if err != nil {
			return err
		}
		for _, c := range cells {
			run.Visibility = slices.DeleteFunc(run.Visibility, func(o results.VisibilityCell) bool {
				return o.Creator == c.Creator && o.Reader == c.Reader
			})
			run.Visibility = append(run.Visibility, c)
		}
		s.Open["MI6"] = run.Encode()
		return nil
	})
}

// runB1 is `uploadspike mi6 --b1 <platform>` (P39): it takes the hub
// through the handover, runs the B1 login of the platform's pair that asks
// for it, and gives the hub back. While an MI5 run is open it is refused,
// recording no B1 outcome (MI5 is never handed over).
func runB1(ctx context.Context, e *env, platform string) error {
	run, st, err := loadRun(e)
	if err != nil {
		return err
	}
	var def *mi6.PairDef
	for _, p := range run.Pairs {
		if p.Platform == platform && mi6.NeedsB1(p, st.Gaps, e.now()) {
			d := p.PairDef
			def = &d
			break
		}
	}
	if def == nil {
		for _, p := range run.Pairs {
			if p.Platform == platform && p.HasPlanA() {
				e.printf(msgB1NotNeeded, mi6.PlatformName(p.Platform), mi6.ProviderName(p.Provider))
				return nil
			}
		}
		return ownerError(fmt.Sprintf(msgUnknownPlatform, platform))
	}
	h, err := acquire(ctx, e, "uploadspike mi6 --b1 "+platform)
	if err != nil {
		return err
	}
	if err := h.startHub(ctx); err != nil {
		h.giveBack()
		return err
	}
	if err := h.serve(ctx); err != nil {
		h.giveBack()
		return err
	}
	return h.finish(ctx, h.login(ctx, *def, mi6.PlanB1))
}

// runReader is `uploadspike mi6 reader --hub <address>` on the PC when the
// hub runs on the Pi (P21): it takes the PC's hub.lock for its session, so
// that it is the PC's only listener on 7091, starts no hub, and serves the
// reader page at http://localhost:7091/reader, which reads the targets
// from, and posts its cells to, the Pi's uploadspike.
func runReader(ctx context.Context, e *env, args []string) error {
	flags := flag.NewFlagSet("mi6 reader", flag.ContinueOnError)
	hub := flags.String("hub", "", "")
	if err := cli.ParseFlags(flags, args, e.out, usage); err != nil {
		return err
	}
	if *hub == "" {
		return ownerError(msgReaderHubNeeded)
	}
	ctx, stop := signalContext(ctx)
	defer stop()
	h, err := acquire(ctx, e, "uploadspike mi6 reader")
	if err != nil {
		return err
	}
	defer h.giveBack()
	remoteBase := *hub
	if _, _, err := net.SplitHostPort(remoteBase); err != nil {
		remoteBase = net.JoinHostPort(remoteBase, strconv.Itoa(handoff.HandoffPort))
	}
	h.remoteReader = "http://" + remoteBase
	h.awaiting, h.open = map[string]handoff.Next{}, map[string]string{}
	h.ends, h.webCells, h.handover = make(chan loginEnd, 1), make(chan results.VisibilityCell, 64), make(chan struct{})
	h.srv = handoff.New(nil, handoff.Options{Clock: e.now, Port: e.port, RemoteHub: *hub, Driver: h})
	if err := h.srv.ServeLoopback(ctx); err != nil {
		return err
	}
	openBrowser(e, fmt.Sprintf("http://localhost:%d/reader", e.port))
	e.printf(msgReaderOpen)
	select {
	case <-ctx.Done():
	case <-h.handoverRequested():
	case <-time.After(readerWait):
	}
	return nil
}
