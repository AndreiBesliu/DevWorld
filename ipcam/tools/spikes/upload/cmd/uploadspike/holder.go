// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/handoff"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// The holder's bounds (One hub per device).
const (
	waitGoneGrace = 60 * time.Second
	hubStopWait   = 35 * time.Second
	hubStartWait  = 2 * time.Minute
	crashRestart  = 5 * time.Second
	devicePoll    = 5 * time.Second
	heartbeatTick = time.Minute
)

// The holder's ends: a handover to another command, a signal, nothing
// left to hold.
var (
	errHandedOver       = errors.New("handed over")
	errHubStopped error = ownerError(msgHubStoppedWait)
	errMI5Running error = ownerError(msgMI5Running)
	errHubBusy    error = ownerError(msgHubBusy)
)

// mi5Holder is what every command reads of an open MI5 run: the mi5 process
// that opened it, and whether it marked the run interrupted (Task 8 keeps
// the rest of the run beside it).
type mi5Holder struct {
	PID         int   `json:"pid"`
	ProcStartMs int64 `json:"procStartMs"`
	Interrupted bool  `json:"interrupted,omitempty"`
}

// mi5State reports whether st holds an open MI5 run, and whether the mi5
// that opened it still runs and has not marked it interrupted.
func mi5State(st *runstate.State) (open, live bool) {
	raw, ok := st.Open["MI5"]
	if !ok {
		return false, false
	}
	var m mi5Holder
	if json.Unmarshal(raw, &m) != nil {
		return true, false
	}
	return true, !m.Interrupted && runstate.Alive(m.PID, m.ProcStartMs)
}

// loginEnd is the end of a login: its pair, the hub's stage and reason of
// a failure, or its plan on success; hubRestarted when the hub crashed
// and restarted during it.
type loginEnd struct {
	dest, stage, reason, plan string
	ok, hubRestarted          bool
}

// holder is the uploadspike process that holds the device's hub.lock, its
// hub and its server on port 7091 (One hub per device).
type holder struct {
	e       *env
	command string // the command line, for an interrupted login's message
	release func()

	hub    *hubproc.Hub
	ctl    *hubproc.ControlClient
	srv    *handoff.Server
	tokens []string
	// setupHandler serves /setup while `setup` holds the hub, and
	// setupDone gets a value once the page's clients are in the hub.
	setupHandler http.HandlerFunc
	setupDone    chan struct{}
	// remoteReader is `mi6 reader --hub`'s Pi uploadspike, whose targets
	// the reader page reads and to which it posts.
	remoteReader string

	mu         sync.Mutex
	awaiting   map[string]handoff.Next // by platform
	open       map[string]string       // logins begun and not ended: state → dest
	stopping   bool
	handedOver bool
	ends       chan loginEnd
	webCells   chan results.VisibilityCell
	handover   chan struct{}
	hubGone    chan struct{}
	crashAt    int64
	crashes    retry // the restarts after a crash
	// mi5 is the MI5 run that this process holds (`uploadspike mi5`): its
	// composition, which holds the cameras' credentials and so lives in
	// RAM only; sink gets each of the hub's events for it.
	mi5  *mi5Comp
	sink func(hubproc.Event)
}

// acquire takes the device's hub for command (One hub per device): no MI5
// run with a live mi5 may be open; its claim goes into the run state; while
// another process holds hub.lock, it asks that holder for a handover and
// takes the lock within 60 s; then the previous holder's hub is waited for
// (WaitGone), and the claim is dropped.
func acquire(ctx context.Context, e *env, command string) (*holder, error) {
	if err := refuseLiveMI5(e); err != nil {
		return nil, err
	}
	claim, err := runstate.NewClaim(e.now())
	if err != nil {
		return nil, err
	}
	var token string
	if err := runstate.Update(e.statePath(), func(s *runstate.State) error {
		s.Claim, token = claim, s.HandoverToken
		return nil
	}); err != nil {
		return nil, err
	}
	pauseAfterCheck()
	release, err := takeLock(ctx, e, token)
	if err != nil {
		dropClaim(e)
		return nil, err
	}
	h := &holder{e: e, command: command, release: release}
	if err := hubproc.WaitGone(ctx, e.pidFile(), e.goneGrace()); err != nil {
		h.release()
		dropClaim(e)
		return nil, err
	}
	dropClaim(e)
	// An MI5 run whose mi5 died: the next command that takes the hub
	// ends it when no resume did first (Holding the hub).
	endDeadMI5(ctx, e)
	return h, nil
}

// takeLock takes hub.lock, asking its holder for a handover every few
// seconds, for 60 s: a 409 means an MI5 run is open.
func takeLock(ctx context.Context, e *env, token string) (func(), error) {
	deadline := time.Now().Add(handoverTimeout)
	var asked time.Time
	for {
		release, err := runstate.Lock(e.hubLock())
		switch {
		case err == nil:
			return release, nil
		case !errors.Is(err, runstate.ErrLocked):
			return nil, err
		case time.Now().After(deadline):
			return nil, errHubBusy
		}
		if time.Since(asked) > 5*time.Second {
			asked = time.Now()
			if askHandover(ctx, e.port, token) == http.StatusConflict {
				return nil, errMI5Running
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// askHandover sends the handover request to this device's holder; 0 when
// none answers.
func askHandover(ctx context.Context, port int, token string) int {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, handoff.HandoverURL(port), nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// dropClaim removes this process's claim from the run state.
func dropClaim(e *env) {
	_ = runstate.Update(e.statePath(), func(s *runstate.State) error {
		if s.Claim.Mine() {
			s.Claim = nil
		}
		return nil
	})
}

// pauseAfterCheck is the tests' seam between a command's MI5 check and its
// handover request: with envPauseAfter set to a path, it creates
// <path>.ready and waits until <path> exists.
func pauseAfterCheck() {
	path := os.Getenv(envPauseAfter)
	if path == "" {
		return
	}
	_ = os.WriteFile(path+".ready", nil, 0o600) //nolint:gosec // G703: the test's own file
	for {
		if _, err := os.Stat(path); err == nil { //nolint:gosec // G703: the test's own file
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// signalContext is ctx, ended by SIGINT, SIGTERM or SIGHUP (on Windows
// CTRL_C, CTRL_BREAK and CTRL_CLOSE), and by the tests' signal file, which
// injects a SIGTERM into the handler.
func signalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	path := os.Getenv(envSignalFile)
	if path == "" {
		return ctx, stop
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		for ctx.Err() == nil {
			if _, err := os.Stat(path); err == nil { //nolint:gosec // G703: the test's own file
				_ = os.Remove(path) //nolint:gosec // G703: the test's own file
				cancel()
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return ctx, func() { cancel(); stop() }
}

// runConfig is run.json as ipcam-hub run reads it.
type runConfig struct {
	DataDir         string          `json:"dataDir"`
	MediaMTX        string          `json:"mediamtx,omitempty"`
	IdentityFile    string          `json:"identityFile,omitempty"`
	SDCardConfirmed bool            `json:"sdCardConfirmed"`
	Jobs            []any           `json:"jobs"`
	Uploads         *uploadsSection `json:"uploads,omitempty"`
	OAuth           *oauthSection   `json:"oauth,omitempty"`
}

type uploadsSection struct {
	HubName      string    `json:"hubName"`
	Destinations []destDef `json:"destinations"`
	Routes       []any     `json:"routes"`
}

type destDef struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	TokenRef      string `json:"tokenRef,omitempty"`
	BaseURL       string `json:"baseUrl,omitempty"`
	UploadBaseURL string `json:"uploadBaseUrl,omitempty"`
	PartSize      int64  `json:"partSize,omitempty"`
}

type oauthSection struct {
	Endpoints map[string]string `json:"endpoints,omitempty"`
}

// files are the hub's files from the open runs: run.json and, while this
// process holds an MI5 run, the encrypted pre-run's identity.
func (h *holder) files(st *runstate.State) (map[string][]byte, error) {
	cfg, err := h.e.composeConfig(st)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	if h.mi5 != nil {
		h.mi5.add(h.e, &cfg, files)
	}
	b, err := marshalRun(cfg)
	if err != nil {
		return nil, err
	}
	files["run.json"] = b
	return files, nil
}

// composeConfig is the hub's run.json from the device's open runs: MI6's
// destinations from day 0 to its close, sdCardConfirmed from the run
// state; with the fakes (the lab, the tests), their endpoints. MI5's
// destinations, jobs and routes join it only in the mi5 process that
// holds the run (files), since its jobs carry the cameras' credentials.
func (e *env) composeConfig(st *runstate.State) (runConfig, error) {
	cfg := runConfig{DataDir: e.dataDir(), MediaMTX: e.mediamtx, SDCardConfirmed: st.Sd != nil, Jobs: []any{}}
	if raw, ok := st.Open["MI6"]; ok {
		run, err := mi6.Decode(raw)
		if err != nil {
			return cfg, err
		}
		up := &uploadsSection{HubName: "ipcam-spike", Destinations: []destDef{}, Routes: []any{}}
		for _, p := range run.Pairs {
			d := destDef{ID: p.Dest, Kind: p.Provider, TokenRef: p.Dest}
			e.fakeDest(&d)
			up.Destinations = append(up.Destinations, d)
		}
		cfg.Uploads = up
	}
	if e.fakes != "" {
		o := e.fakes + "/oauth"
		cfg.OAuth = &oauthSection{Endpoints: map[string]string{
			"googleAuth": o + "/google/auth", "googleToken": o + "/google/token", "googleDevice": o + "/google/device/code",
			"googleRevoke": o + "/google/revoke", "microsoftAuth": o + "/microsoft/authorize", "microsoftToken": o + "/microsoft/token",
			"microsoftDevice": o + "/microsoft/devicecode",
		}}
	}
	return cfg, nil
}

// marshalRun is run.json's bytes.
func marshalRun(cfg runConfig) ([]byte, error) {
	b, err := json.MarshalIndent(cfg, "", "  ")
	return append(b, '\n'), err
}

// fakeDest points d at the fakes (the lab, the tests).
func (e *env) fakeDest(d *destDef) {
	if e.fakes == "" {
		return
	}
	if d.Kind == mi6.ProviderDrive {
		d.BaseURL, d.UploadBaseURL = e.fakes+"/gdrive", e.fakes+"/gdrive-upload"
	} else {
		d.BaseURL = e.fakes + "/graph"
	}
}

// startHub starts the device's hub with the composition of its open runs,
// a fresh control token, and its control client; the hub's events go to
// consume until its stdout ends.
func (h *holder) startHub(ctx context.Context) error {
	st, err := runstate.Load(h.e.statePath())
	if err != nil {
		return err
	}
	files, err := h.files(st)
	if err != nil {
		return err
	}
	token, err := hubproc.NewControlToken()
	if err != nil {
		return err
	}
	cfg := h.e.hubConfig()
	cfg.Files, cfg.ControlToken = files, token
	ctx, cancel := context.WithTimeout(ctx, hubStartWait)
	defer cancel()
	hub, err := hubproc.Start(ctx, cfg)
	if err != nil {
		return err
	}
	ctl, err := hub.Control(ctx)
	if err != nil {
		_ = hub.Kill()
		return err
	}
	h.mu.Lock()
	h.hub, h.ctl, h.stopping = hub, ctl, false
	h.tokens = append(h.tokens, token)
	h.hubGone = make(chan struct{})
	gone := h.hubGone
	h.mu.Unlock()
	if h.srv != nil {
		h.srv.SetHub(ctl)
	}
	go h.consume(hub, gone)
	return nil
}

// serve serves the handoff pages on the loopback listener while the holder
// holds the lock.
func (h *holder) serve(ctx context.Context) error {
	st, err := runstate.Load(h.e.statePath())
	if err != nil {
		return err
	}
	oneDrive := false
	if raw, ok := st.Open["MI6"]; ok {
		if run, err := mi6.Decode(raw); err == nil {
			oneDrive = run.OneDrive
		}
	}
	h.mu.Lock()
	if h.awaiting == nil {
		h.awaiting, h.open = map[string]handoff.Next{}, map[string]string{}
		h.ends, h.webCells, h.handover = make(chan loginEnd, 64), make(chan results.VisibilityCell, 64), make(chan struct{})
	}
	h.mu.Unlock()
	h.srv = handoff.New(h.ctl, handoff.Options{Clock: h.e.now, Port: h.e.port, ResultsDir: h.e.resultsDir(), Driver: h, OneDrive: oneDrive, App: appFS()})
	return h.srv.ServeLoopback(ctx)
}

// stopHub stops the hub cleanly (its stdin), and kills it after 35 s.
func (h *holder) stopHub() {
	h.mu.Lock()
	hub := h.hub
	h.stopping = true
	h.mu.Unlock()
	if hub == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), hubStopWait)
	defer cancel()
	_ = hub.Stop(ctx)
}

// giveBack stops the hub and the server, and releases the lock.
func (h *holder) giveBack() {
	h.stopHub()
	if h.srv != nil {
		h.srv.Close()
	}
	if h.release != nil {
		h.release()
		h.release = nil
	}
}

// handBack gives the hub back after a handover: unless an MI5 run is open,
// it scans the results with this process's control tokens first (P45);
// a login open then is cut, and the owner reads which command to run
// again.
func (h *holder) handBack(ctx context.Context) {
	st, _ := runstate.Load(h.e.statePath())
	if open, _ := mi5State(st); !open {
		h.scanOrFail(ctx)
	}
	h.mu.Lock()
	cut := len(h.open) > 0
	h.mu.Unlock()
	h.giveBack()
	if cut {
		h.e.printf(msgLoginInterrupted, h.command)
	} else {
		h.e.printf(msgHandedOver)
	}
}

// scanOrFail runs the results' known-secrets scan with this process's
// control tokens and the handover token; a non-empty moved or unmoved, or
// a failed scan, fails the open MI6 run (C2-R20), with the scan's fixed
// reason (PH0-22).
func (h *holder) scanOrFail(ctx context.Context) {
	if f := scanResults(ctx, h.e, h.tokens); f != nil {
		h.e.printf("%s", f.line)
		failMI6(h.e, f.reason)
	}
}

// failMI6 records why the open MI6 run failed: a fixed reason, which names
// no path, since it goes into the envelope (PH0-22).
func failMI6(e *env, reason string) {
	_ = runstate.Update(e.statePath(), func(s *runstate.State) error {
		raw, ok := s.Open["MI6"]
		if !ok {
			return nil
		}
		run, derr := mi6.Decode(raw)
		if derr != nil {
			return derr
		}
		run.ScanFailed = reason
		s.Open["MI6"] = run.Encode()
		return nil
	})
}

// consume applies the hub's events to the run state until its stdout ends,
// then closes gone.
func (h *holder) consume(hub *hubproc.Hub, gone chan struct{}) {
	defer close(gone)
	var log *os.File
	if h.e.eventLog != "" {
		log, _ = os.OpenFile(h.e.eventLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		defer func() {
			if log != nil {
				_ = log.Close()
			}
		}()
	}
	for ev := range hub.Events() {
		if log != nil {
			_, _ = log.Write(append(bytes.Clone(ev.Raw), '\n'))
		}
		h.apply(ev)
		h.mu.Lock()
		sink := h.sink
		h.mu.Unlock()
		if sink != nil {
			sink(ev)
		}
	}
}

// hubEvent is the fields of the OAuth events.
type hubEvent struct {
	DestID     string `json:"destId"`
	Plan       string `json:"plan"`
	Provider   string `json:"provider"`
	ClientType string `json:"clientType"`
	IssuedAtMs int64  `json:"issuedAtMs"`
	Stage      string `json:"stage"`
	Reason     string `json:"reason"`
	Day        int    `json:"day"`
	Attempt    int    `json:"attempt"`
	Outcome    string `json:"outcome"`
}

// apply records one OAuth event in the open MI6 run: a completion (and its
// handoff), a failure, a refresh day.
func (h *holder) apply(ev hubproc.Event) {
	switch ev.Type {
	case "oauth_completed", "oauth_failed", "oauth_refresh":
	default:
		return
	}
	var f hubEvent
	if json.Unmarshal(ev.Raw, &f) != nil {
		return
	}
	end := loginEnd{dest: f.DestID}
	err := runstate.Update(h.e.statePath(), func(s *runstate.State) error {
		raw, ok := s.Open["MI6"]
		if !ok {
			return nil
		}
		run, err := mi6.Decode(raw)
		if err != nil {
			return err
		}
		if run.Pair(f.DestID) == nil {
			return nil // one of MI5's own logins: mi5 counts it, on its account
		}
		switch ev.Type {
		case "oauth_completed":
			run.Completed(f.DestID, f.Plan, f.ClientType, f.IssuedAtMs)
			mi6.CountHandoff(s.Handoffs, f.Provider, f.ClientType)
			end.ok, end.plan = true, f.Plan
		case "oauth_failed":
			run.Failed(f.DestID, f.Stage, f.Reason)
			end.stage, end.reason = f.Stage, f.Reason
		case "oauth_refresh":
			run.Refreshed(f.DestID, f.Day, f.Attempt, f.Outcome, ev.AtMs)
		}
		s.Open["MI6"] = run.Encode()
		return nil
	})
	if err != nil || ev.Type == "oauth_refresh" || f.Stage == "refresh" {
		return
	}
	h.mu.Lock()
	for state, dest := range h.open {
		if dest == f.DestID {
			delete(h.open, state)
		}
	}
	ends := h.ends
	h.mu.Unlock()
	if ends != nil {
		select {
		case ends <- end:
		default:
		}
	}
}

// heartbeat records a heartbeat in the run state.
func (h *holder) heartbeat() {
	mono, err := runstate.Mono()
	if err != nil {
		return
	}
	boot, err := runstate.BootID()
	if err != nil {
		return
	}
	_ = runstate.Update(h.e.statePath(), func(s *runstate.State) error {
		s.Heartbeat(h.e.now(), mono, boot)
		return nil
	})
}

// restartAfterCrash restarts a hub that ended on its own: the gap starts,
// and the hub restarts after crashRestart; a restart that fails is tried
// again after a growing wait (retry), its error printed once; the gap
// ends once a hub is ready again. A handover cuts the wait.
func (h *holder) restartAfterCrash(ctx context.Context) {
	if ctx.Err() != nil {
		return // a signal ends the holder: nothing to restart
	}
	wake := h.handoverRequested()
	select {
	case <-wake:
		return
	default:
	}
	h.mu.Lock()
	first := h.crashAt == 0
	if first {
		h.crashAt = h.e.now().UnixMilli()
	}
	h.mu.Unlock()
	if first {
		h.e.printf(msgHubCrashed)
		select {
		case <-ctx.Done():
			return
		case <-wake:
			return
		case <-time.After(h.e.crashRestart()):
		}
	}
	h.crashes.e = h.e
	if err := h.startHub(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		h.crashes.failed(ctx, err, h.e.crashRestart(), wake)
		return
	}
	h.crashes.ok()
	h.mu.Lock()
	gap := results.Gap{StartMs: h.crashAt, EndMs: h.e.now().UnixMilli(), Reason: results.GapHubCrash}
	h.crashAt = 0
	h.mu.Unlock()
	_ = runstate.Update(h.e.statePath(), func(s *runstate.State) error {
		s.Gaps = append(s.Gaps, gap)
		return nil
	})
}

// gone is the current hub's end.
func (h *holder) gone() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hubGone == nil {
		return nil
	}
	return h.hubGone
}

// isStopping reports whether the holder is stopping its hub on purpose.
func (h *holder) isStopping() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopping
}

// await makes dest's login under plan the one awaited on its platform; an
// empty plan awaits none there.
func (h *holder) await(platform string, n handoff.Next) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n.Dest == "" {
		delete(h.awaiting, platform)
		return
	}
	h.awaiting[platform] = n
}

// The handoff server's driver.

// Next is the login awaited on platform.
func (h *holder) Next(platform string) (handoff.Next, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	n, ok := h.awaiting[platform]
	return n, ok
}

// AllowBegin refuses a login past the handoff cap of its account and
// client (P39).
func (h *holder) AllowBegin(dest, platform, plan string) error {
	st, err := runstate.Load(h.e.statePath())
	if err != nil {
		return err
	}
	for _, d := range append(append([]mi6.PairDef{}, mi6.Drive...), mi6.OneDrive...) {
		if d.Dest == dest && d.Platform == platform {
			err := mi6.AllowHandoff(st.Handoffs, mi6.ClientFor(d, plan))
			if err != nil {
				// The owner reads it where they wait, not only in the app
				// (Task 9B review M7).
				h.e.printf("%s", err)
			}
			return err
		}
	}
	return fmt.Errorf("unknown pair %s", dest)
}

// Begun records a login that began, and polls a device login's status, so
// that the hub polls the provider's device token endpoint.
func (h *holder) Begun(dest string, res hubproc.BeginResult) {
	h.mu.Lock()
	h.open[res.State] = dest
	ctl := h.ctl
	h.mu.Unlock()
	if res.Plan == mi6.PlanB1 && ctl != nil {
		go h.pollDevice(ctl, res.State)
	}
}

// pollDevice asks the hub for a device login's status every 5 s until the
// login ends, its hub stops, it expires, or the holder accepted a handover:
// a login must not complete into a hub that is being handed over (Task 9B
// review M2).
func (h *holder) pollDevice(ctl *hubproc.ControlClient, state string) {
	deadline := time.Now().Add(31 * time.Minute)
	wake := h.handoverRequested()
	for time.Now().Before(deadline) {
		select {
		case <-wake:
			return
		case <-time.After(h.e.devicePoll()):
		}
		h.mu.Lock()
		_, open := h.open[state]
		current := h.ctl == ctl && !h.stopping && !h.handedOver
		h.mu.Unlock()
		if !open || !current {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		s, err := ctl.Status(ctx, state)
		cancel()
		if err == nil && s.Status != "pending" {
			return
		}
	}
}

// WebCell records a cell of the web reader.
func (h *holder) WebCell(creator string, listed, read bool) error {
	switch creator {
	case "android", "desktop", "tv":
	default:
		return fmt.Errorf("unknown creator %q", creator)
	}
	h.mu.Lock()
	cells := h.webCells
	h.mu.Unlock()
	select {
	case cells <- results.VisibilityCell{Provider: mi6.ProviderDrive, Creator: creator, Reader: "web", Listed: listed, Read: read}:
		return nil
	default:
		return errors.New("too many cells")
	}
}

// WebInterrupted records a reading of the web reader that the page's
// going to the background interrupted (C2-R18 (d)): counted in the open
// MI6 run, and said on the terminal; the page restarts it on return.
func (h *holder) WebInterrupted() {
	if err := runstate.Update(h.e.statePath(), func(s *runstate.State) error {
		raw, ok := s.Open["MI6"]
		if !ok {
			return nil
		}
		run, err := mi6.Decode(raw)
		if err != nil {
			return err
		}
		run.ReaderInterruptions++
		s.Open["MI6"] = run.Encode()
		return nil
	}); err != nil {
		h.e.printf("%s %v", msgError, err)
	}
	h.e.printf(msgReaderInterrupted)
}

// WebClientID is the web reader's client ID, from the run state.
func (h *holder) WebClientID() string {
	st, err := runstate.Load(h.e.statePath())
	if err != nil {
		return ""
	}
	return st.WebClientID
}

// HandoverToken is the run state's handover token.
func (h *holder) HandoverToken() string {
	st, err := runstate.Load(h.e.statePath())
	if err != nil {
		return ""
	}
	return st.HandoverToken
}

// Handover answers another command's handover request: 409 while an MI5
// run is open (MI5 is never handed over), else 204, and the holder gives
// the hub back.
func (h *holder) Handover() int {
	st, err := runstate.Load(h.e.statePath())
	if err != nil {
		return http.StatusInternalServerError
	}
	if open, _ := mi5State(st); open {
		h.e.printf(msgHandoverMI5)
		return http.StatusConflict
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.handedOver {
		h.handedOver = true
		close(h.handover)
	}
	return http.StatusNoContent
}

// handoverRequested is closed once a handover was accepted.
func (h *holder) handoverRequested() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handover
}

// Setup serves /setup while `setup` holds the hub.
func (h *holder) Setup(w http.ResponseWriter, r *http.Request) {
	if h.setupHandler == nil {
		http.Error(w, pageNotFound, http.StatusNotFound)
		return
	}
	h.setupHandler(w, r)
}

// Status serves the status page and its JSON: the same lines as
// `uploadspike status`.
func (h *holder) Status(w http.ResponseWriter, r *http.Request) {
	lines, err := statusLines(h.e)
	if err != nil {
		http.Error(w, pageRunStateUnread, http.StatusInternalServerError)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/spike/v1/") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string][]string{"lines": lines})
		return
	}
	renderPage(w, "status.html", pageData{Title: pageStatusTitle, Lines: lines})
}
