// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/handoff"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hostinfo"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/install"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/linkclient"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi5"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// The MI5 run's environment: the seams of its timing (the tests' and the
// lab's short runs), of reccontroller, and of its media.
const (
	envMI5Timing      = "UPLOADSPIKE_MI5_TIMING" // JSON of mi5.Timing in ms
	envReccontroller  = "UPLOADSPIKE_RECCONTROLLER"
	envMediaDir       = "UPLOADSPIKE_MEDIA"
	envLabLAN         = "UPLOADSPIKE_TEST_LAB_LAN" // the LAN probe listener's address: the lab's where the host has no RFC 1918 one, and the tests' owner runs
	identityFileName  = "ri_g.key"
	reccontrollerName = "reccontroller"
	linkWait          = 60 * time.Second
)

// mi5Open is the open MI5 run in the run state: the mi5 that holds it
// (mi5Holder), its start, the boot it started on, its settings and
// destinations, and the issuance of its own logins (P53).
type mi5Open struct {
	mi5Holder
	RunID    string           `json:"runId"`
	StartMs  int64            `json:"startMs"`
	BootID   string           `json:"bootId,omitempty"`
	Drive    string           `json:"drive"`
	OneDrive bool             `json:"oneDrive,omitempty"`
	Lab      bool             `json:"lab,omitempty"`
	Dests    []mi5.DestConfig `json:"dests"`
	Logins   []mi5Login       `json:"logins,omitempty"`
}

// mi5Login is one of MI5's own logins: its token row and issuance.
type mi5Login struct {
	TokenRef   string `json:"tokenRef"`
	IssuedAtMs int64  `json:"issuedAtMs"`
}

// mi5Comp is MI5's part of the hub's composition, in RAM only: its
// settings, destinations and phase, the cameras' session (credentials
// included) and the encrypted pre-run's identity and recipients.
type mi5Comp struct {
	cfg        mi5.Mi5Config
	dests      []mi5.DestConfig
	startMs    int64
	phase      mi5.Phase
	session    linkclient.Session
	identity   string
	recipients []string
}

// mi5Job is one of run.json's jobs.
type mi5Job struct {
	Cam         string    `json:"cam"`
	Profile     string    `json:"profile"`
	RestreamURL string    `json:"restreamUrl"`
	SegmentSecs int       `json:"segmentSecs"`
	Recipients  *[]string `json:"recipients,omitempty"`
}

// add adds MI5's destinations, routes and the phase's jobs to cfg, and
// the identity file to files.
func (m *mi5Comp) add(e *env, cfg *runConfig, files map[string][]byte) {
	if cfg.Uploads == nil {
		cfg.Uploads = &uploadsSection{HubName: "ipcam-spike", Destinations: []destDef{}, Routes: []any{}}
	}
	for _, d := range m.dests {
		def := destDef{ID: d.ID, Kind: d.Kind, TokenRef: d.TokenRef, PartSize: m.cfg.PartSize}
		e.fakeDest(&def)
		cfg.Uploads.Destinations = append(cfg.Uploads.Destinations, def)
	}
	for _, r := range mi5.Routes(m.cfg, m.dests, m.startMs) {
		cfg.Uploads.Routes = append(cfg.Uploads.Routes, r)
	}
	files[identityFileName] = []byte(m.identity + "\n")
	cfg.IdentityFile = filepath.Join(filepath.Dir(e.runJSON()), identityFileName)
	switch m.phase {
	case mi5.PhasePreRun:
		recipients := m.recipients
		cfg.Jobs = append(cfg.Jobs, mi5Job{Cam: "cam01", Profile: "sub", RestreamURL: m.streamURL("cam01_sub"), SegmentSecs: m.cfg.SegmentSecs,
			Recipients: &recipients})
	case mi5.PhaseMain:
		for _, cam := range mi5.CameraNames(m.cfg.Cameras) {
			cfg.Jobs = append(cfg.Jobs, mi5Job{Cam: cam, Profile: "sub", RestreamURL: m.streamURL(cam + "_sub"), SegmentSecs: m.cfg.SegmentSecs})
		}
	}
}

// streamURL is a camera's RTSP URL at the session's server, with its
// credentials: it goes into run.json only.
func (m *mi5Comp) streamURL(stream string) string {
	base, err := url.Parse(m.session.RTSPBase)
	if err != nil {
		return ""
	}
	u := url.URL{Scheme: base.Scheme, User: url.UserPassword(m.session.User, m.session.Pass), Host: base.Host, Path: "/" + stream}
	return u.String()
}

// mi5Flags are uploadspike mi5's settings.
type mi5Flags struct {
	drive, fakes        string
	oneDrive, lab       bool
	block               time.Duration
	segmentSecs, camera int
	partSize            int64
}

// parsePartSize reads --part-size: bytes, or KiB/MiB.
func parsePartSize(s string) (int64, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "KiB"):
		s, mult = strings.TrimSuffix(s, "KiB"), 1<<10
	case strings.HasSuffix(s, "MiB"):
		s, mult = strings.TrimSuffix(s, "MiB"), 1<<20
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, ownerError(fmt.Sprintf(msgBadPartSize, s))
	}
	return n * mult, nil
}

// fakesBase reads --fakes: fakeproviders' schema line, or its base URL.
func fakesBase(s string) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(s), "{") {
		return strings.TrimSuffix(s, "/"), nil
	}
	var line struct {
		Schema string `json:"schema"`
		OAuth  string `json:"oauth"`
	}
	if json.Unmarshal([]byte(s), &line) != nil || line.Schema != "ipcam.fakeproviders/1" || !strings.HasSuffix(line.OAuth, "/oauth") {
		return "", ownerError(msgBadFakes)
	}
	return strings.TrimSuffix(line.OAuth, "/oauth"), nil
}

// runMI5 is `uploadspike mi5` (G14): it takes the device's one hub (the
// MI6 hub through the handover, Task 9), opens the MI5 run, logs in the
// destinations that need it, checks the quotas, the spool and the daily
// refreshes, records the simulated cameras of reccontroller --local, runs
// MI5's order (mi5.Run), writes the envelope, scans the results, and
// gives the hub back: the autostart's waiting resume takes it with MI6's
// destinations only. It is never handed over. A run that ends any other
// way leaves its hub to end with it, and resume marks the run invalid.
func runMI5(ctx context.Context, e *env, args []string) error {
	fs := flag.NewFlagSet("mi5", flag.ContinueOnError)
	var f mi5Flags
	var partSize string
	fs.StringVar(&f.drive, "drive", mi5.DriveTest, "")
	fs.BoolVar(&f.oneDrive, "onedrive", false, "")
	fs.DurationVar(&f.block, "block", mi5.OwnerBlock, "")
	fs.IntVar(&f.segmentSecs, "segment-secs", 300, "")
	fs.StringVar(&partSize, "part-size", "", "")
	fs.IntVar(&f.camera, "cameras", 0, "")
	fs.BoolVar(&f.lab, "lab", false, "")
	fs.StringVar(&f.fakes, "fakes", "", "")
	if err := cli.ParseFlags(fs, args, e.out, usage); err != nil {
		return err
	}
	var err error
	if f.partSize, err = parsePartSize(partSize); err != nil {
		return err
	}
	if f.fakes != "" {
		if e.fakes, err = fakesBase(f.fakes); err != nil {
			return err
		}
		e.seams = true // the lab's rig: never judged (C2-R22 (3))
	}
	if err := mi5.CheckPartSize(f.partSize, e.fakes); err != nil {
		return err
	}
	if err := elevate(e, append([]string{"mi5"}, args...)); err != nil {
		return err
	}
	e.printf(msgMI5KeepOpen)
	e.printf(msgMI5HowToKeepOpen)
	ctx, stop := signalContext(ctx)
	defer stop()
	if release, err := install.KeepAwake(); err == nil {
		defer release()
	}
	return (&mi5Cmd{e: e, f: f}).run(ctx)
}

// mi5Cmd is one uploadspike mi5.
type mi5Cmd struct {
	e    *env
	f    mi5Flags
	h    *holder
	host string
	info *hostinfo.Host
	recc *exec.Cmd
	// reccDone is closed once reccontroller ended.
	reccDone <-chan struct{}
	events   chan hubproc.Event
	open     mi5Open
	// lanListener is the lab's LAN probe listener.
	lanListener net.Listener
}

// run is runMI5's body.
func (c *mi5Cmd) run(ctx context.Context) error {
	e := c.e
	cfg, err := c.config(ctx)
	if err != nil {
		return err
	}
	defer c.closeLAN()
	if cfg.LanProbe, err = c.lanProbe(ctx); err != nil {
		return err
	}
	h, err := acquire(ctx, e, "uploadspike mi5")
	if err != nil {
		return err
	}
	c.h = h
	defer h.giveBack()
	if err := c.openRun(cfg); err != nil {
		return err
	}
	// The holder's server on 127.0.0.1:7091 (Inbound listeners), once the
	// run is open: status, and a 409 to every handover (MI5 is never
	// handed over), whose caller exits with «Proba MI5 rulează…».
	if err := h.serve(ctx); err != nil {
		return c.fail(ctx, err)
	}
	fw := e.firewall()
	if err := fw.RemoveStale(ctx); err != nil {
		return c.fail(ctx, err)
	}
	comp, err := c.composition(cfg)
	if err != nil {
		return c.fail(ctx, err)
	}
	h.mi5 = comp
	c.events = make(chan hubproc.Event, 4096)
	h.mu.Lock()
	h.sink = func(ev hubproc.Event) {
		if !mi5Event(ev.Type) {
			return // the status ticks and the rest: the tally reads none
		}
		select {
		case c.events <- ev:
		case <-time.After(time.Minute):
			// Never stall the hub's stdout behind a run that stopped
			// reading; a lost event shows in the audit, never in a hang.
		}
	}
	h.mu.Unlock()
	stopSink := func() {
		h.mu.Lock()
		h.sink = nil
		h.mu.Unlock()
	}
	defer stopSink()
	if err := c.logins(ctx, comp); err != nil {
		return c.fail(ctx, err)
	}
	if err := c.preChecks(ctx, cfg, comp); err != nil {
		if errors.As(err, new(refusal)) {
			return c.refuse(err)
		}
		return c.fail(ctx, err)
	}
	// The cameras start once the run passed its pre-checks: a refused run
	// started nothing.
	session, err := c.startCameras(ctx)
	if err != nil {
		return c.fail(ctx, err)
	}
	defer c.stopCameras()
	comp.session = session
	timing := runTiming(cfg)
	e.printf(msgMI5Started, mi5.Span(cfg.BlockFor), time.Now().Add(timing.PreRun+timing.WarmUp).Format("15:04"))
	opts := mi5.RunOptions{Host: &mi5Host{h: h}, Firewall: fw, StartMs: comp.startMs, Dests: comp.dests, Timing: timing, Events: c.events}
	if os.Getenv(envFirewallLog) != "" {
		// The tests' firewall blocks nothing, so its checks pass by the
		// seam's say: such a run is never judged (testSeams).
		opts.Verify = func(context.Context) (mi5.ReachResult, error) {
			return mi5.ReachResult{ProviderBlockedV4: true, ProviderBlockedV6: true, Lan: true, Loopback: true}, nil
		}
	}
	data, err := mi5.Run(ctx, cfg, opts)
	stopSink() // nothing reads the events once Run returned
	if err != nil {
		return c.fail(ctx, err)
	}
	return c.close(ctx, data)
}

// mi5Event reports whether the run's tally reads events of typ.
func mi5Event(typ string) bool {
	return strings.HasPrefix(typ, "upload_") || typ == "dest_state" || typ == "segment_closed" || typ == "gap"
}

// config is the run's settings from the flags and the host (P17, P38).
func (c *mi5Cmd) config(ctx context.Context) (mi5.Mi5Config, error) {
	e, f := c.e, c.f
	cfg := mi5.Mi5Config{HubBin: e.hubBin, MediaMTXBin: e.mediamtx, DataDir: e.dataDir(), SegmentSecs: f.segmentSecs, PartSize: f.partSize,
		EventCamera: mi5.EventCamera, Drive: f.drive, BlockFor: f.block, ReadOpenCalls: mi5.ReadOpenCalls, Lab: f.lab, Fakes: e.fakes}
	st, err := runstate.Load(e.statePath())
	if err != nil {
		return cfg, err
	}
	cfg.OneDrive = f.oneDrive && st.EntraClientID != ""
	if f.oneDrive && !cfg.OneDrive {
		e.printf(msgOneDriveNeedsA5)
	}
	host, info := hostName(ctx, e)
	c.host = host
	if f.lab {
		c.host = "lab-linux"
		if runtime.GOOS == "windows" {
			c.host = "lab-windows"
		}
	}
	if info == nil {
		if hi, err := hostinfo.Read(ctx, e.hubBin, recDir(e)); err == nil {
			info = &hi
		}
	}
	c.info = info
	cameras := mi5.MaxCameras
	if info != nil {
		if cameras, err = mi5.PiCameras(*info); err != nil {
			return cfg, err
		}
		if info.Storage == "sd" && st.Sd == nil {
			return cfg, hubproc.ErrSdCardUnconfirmed
		}
	}
	if f.camera > 0 {
		cameras = min(f.camera, cameras) // never above the host's count (P17, P38)
	}
	cfg.Cameras = cameras
	cfg.ProviderHosts = mi5.ProviderHostsFor(cfg)
	self, err := os.Executable()
	if err != nil {
		return cfg, err
	}
	dir := filepath.Dir(e.hubBin)
	cfg.ReccontrollerBin = firstSet(os.Getenv(envReccontroller), filepath.Join(dir, exeName(reccontrollerName)))
	cfg.MediaDir = firstSet(os.Getenv(envMediaDir), filepath.Join(filepath.Dir(self), "media"))
	return cfg, cfg.Validate()
}

// firstSet is the first non-empty string.
func firstSet(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// exeName is a program's file name on this OS.
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// runTiming is the run's timing: the owner's, the lab's, or the tests'
// (envMI5Timing).
func runTiming(cfg mi5.Mi5Config) mi5.Timing {
	if t, ok := testTiming(); ok {
		return t
	}
	if cfg.Lab {
		return mi5.LabTiming(cfg.BlockFor)
	}
	return mi5.OwnerTiming(cfg.BlockFor)
}

// testTiming is the tests' timing (envMI5Timing), in ms.
func testTiming() (mi5.Timing, bool) {
	raw := os.Getenv(envMI5Timing)
	if raw == "" {
		return mi5.Timing{}, false
	}
	var ms map[string]int64
	if json.Unmarshal([]byte(raw), &ms) != nil {
		return mi5.Timing{}, false
	}
	d := func(k string) time.Duration { return time.Duration(ms[k]) * time.Millisecond }
	return mi5.Timing{PreRun: d("preRun"), PreRunEvery: d("preRunEvery"), WarmUp: d("warmUp"), CheckEvery: d("checkEvery"),
		ReadOpenEvery: d("readOpenEvery"), Floor: d("floor"), Quiet: d("quiet"), DrainMax: d("drainMax")}, true
}

// lanProbe is the block's LAN probe: in the lab a listener of this
// process on the runner's first RFC 1918 address; for the owner the
// routing table's default gateway (C2-R18: nothing typed), which stays in
// RAM and in no output. The tests' seam envLabLAN gives the listener's
// address, in the lab and in an owner's run alike.
func (c *mi5Cmd) lanProbe(ctx context.Context) (string, error) {
	ip := os.Getenv(envLabLAN)
	if !c.f.lab && ip == "" {
		return mi5.FindLanProbe(ctx)
	}
	if ip == "" {
		addrs, err := handoff.RFC1918Addrs()
		if err != nil || len(addrs) == 0 {
			return "", ownerError(msgNoLANAddress)
		}
		ip = addrs[0].String()
	}
	ln, err := net.Listen("tcp4", net.JoinHostPort(ip, "0"))
	if err != nil {
		return "", err
	}
	c.lanListener = ln
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return ln.Addr().String(), nil
}

func (c *mi5Cmd) closeLAN() {
	if c.lanListener != nil {
		_ = c.lanListener.Close()
	}
}

// openRun records the open MI5 run: this process, its start (every
// route's fromMs), the boot, the settings.
func (c *mi5Cmd) openRun(cfg mi5.Mi5Config) error {
	start, err := runstate.ProcStartMs(os.Getpid())
	if err != nil {
		return err
	}
	boot, _ := runstate.BootID()
	now := c.e.now()
	c.open = mi5Open{mi5Holder: mi5Holder{PID: os.Getpid(), ProcStartMs: start}, RunID: "mi5-" + now.UTC().Format("20060102T150405Z"),
		StartMs: now.UnixMilli(), BootID: boot, Drive: cfg.Drive, OneDrive: cfg.OneDrive, Lab: cfg.Lab, Dests: mi5.Destinations(cfg)}
	return c.saveOpen()
}

// saveOpen writes c.open into the run state.
func (c *mi5Cmd) saveOpen() error {
	raw, err := json.Marshal(c.open)
	if err != nil {
		return err
	}
	return runstate.Update(c.e.statePath(), func(s *runstate.State) error {
		s.Open["MI5"] = raw
		return nil
	})
}

// composition is MI5's composition: its destinations, each on an MI6 row
// of its account when this hub has one (dst-1: g-windows on the PC,
// g-web on the Pi, any MI6 row of the D1 account), else its own; an
// ephemeral age pair for the encrypted pre-run. The cameras' session joins
// it once they started, after the pre-checks.
func (c *mi5Cmd) composition(cfg mi5.Mi5Config) (*mi5Comp, error) {
	riG, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	rk, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	dests := c.open.Dests
	st, err := runstate.Load(c.e.statePath())
	if err != nil {
		return nil, err
	}
	if raw, ok := st.Open["MI6"]; ok {
		if run, err := mi6.Decode(raw); err == nil {
			for i := range dests {
				if ref := mi6Row(run, dests[i], runtime.GOOS); ref != "" {
					dests[i].TokenRef = ref
				}
			}
		}
	}
	c.open.Dests = dests
	if err := c.saveOpen(); err != nil {
		return nil, err
	}
	return &mi5Comp{cfg: cfg, dests: dests, startMs: c.open.StartMs, phase: mi5.PhasePreRun, identity: riG.String(),
		recipients: []string{riG.Recipient().String(), rk.Recipient().String()}}, nil
}

// mi6Row is the MI6 row that d reuses: a pair of d's provider with a
// login, on the D1 account (MI6's drive setting gives the account kind),
// the platform's own first (Windows on the PC, web on the Pi).
func mi6Row(run *mi6.Run, d mi5.DestConfig, goos string) string {
	kind := "test"
	if run.Drive == mi5.DrivePersonal {
		kind = "personal"
	}
	if d.Account != "acct-1" || d.AccountKind != kind {
		return ""
	}
	prefer := "windows"
	if goos == "linux" {
		prefer = "web"
	}
	provider := mi6.ProviderDrive
	if d.Kind == mi5.KindOneDrive {
		provider = mi6.ProviderOneDrive
	}
	var other string
	for _, p := range run.Pairs {
		if p.Provider != provider || !p.HandoffOK {
			continue
		}
		if p.Platform == prefer {
			return p.Dest
		}
		if other == "" {
			other = p.Dest
		}
	}
	return other
}

// startCameras starts reccontroller --local, which serves the simulated
// cameras on 127.0.0.1 (ports 7080–7083 and 8554), and learns their RTSP
// base, credentials and streams through the link (g3-16).
func (c *mi5Cmd) startCameras(ctx context.Context) (linkclient.Session, error) {
	bin := firstSet(os.Getenv(envReccontroller), filepath.Join(filepath.Dir(c.e.hubBin), exeName(reccontrollerName)))
	media := firstSet(os.Getenv(envMediaDir), filepath.Join(filepath.Dir(bin), "media"))
	cmd := exec.Command(bin, "-local", "-media", media) //nolint:gosec // the bundle's own reccontroller
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	done, err := startWithUs(cmd)
	if err != nil {
		return linkclient.Session{}, fmt.Errorf("reccontroller: %w", err)
	}
	c.recc, c.reccDone = cmd, done
	deadline := time.Now().Add(linkWait)
	for {
		s, err := linkclient.Dial(ctx, linkclient.LocalAddr, c.host)
		if err == nil {
			return s, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return linkclient.Session{}, ownerError(msgNoCameras)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (c *mi5Cmd) stopCameras() {
	if c.recc != nil && c.recc.Process != nil {
		_ = c.recc.Process.Kill()
		<-c.reccDone
	}
}

// logins logs in each destination whose token row is its own, on a hub
// without jobs: in the lab through the fake provider itself (the lab's
// login rig, SPIKE-ONLY); for the owner through the hub's device flow
// (B1), whose address and code the owner opens from any browser. Each
// counts in the handoff cap.
func (c *mi5Cmd) logins(ctx context.Context, comp *mi5Comp) error {
	var need []mi5.DestConfig
	for _, d := range comp.dests {
		if d.TokenRef == d.ID {
			need = append(need, d)
		}
	}
	if len(need) == 0 {
		return nil
	}
	comp.phase = mi5.PhaseFinal // no job while the owner logs in
	if err := c.h.startHub(ctx); err != nil {
		return err
	}
	defer c.h.stopHub()
	if c.f.lab {
		if err := labClients(ctx, c.e.fakes, c.h.ctl); err != nil {
			return err
		}
	}
	for _, d := range need {
		var done hubproc.Completion
		var err error
		if c.f.lab {
			done, err = labLogin(ctx, c.h.ctl, d)
		} else {
			done, err = c.deviceLogin(ctx, d)
		}
		if err != nil {
			return err
		}
		if err := runstate.Update(c.e.statePath(), func(s *runstate.State) error {
			s.Handoffs[d.Account+"/"+mi6ClientKey(d.Kind, done.ClientType)]++
			return nil
		}); err != nil {
			return err
		}
		c.open.Logins = append(c.open.Logins, mi5Login{TokenRef: d.TokenRef, IssuedAtMs: done.IssuedAt.UnixMilli()})
		if err := c.saveOpen(); err != nil {
			return err
		}
		c.e.printf(msgMI5LoginDone, d.ID)
	}
	return nil
}

// mi6ClientKey is the handoff cap's client key of a login.
func mi6ClientKey(kind, clientType string) string {
	if kind == mi5.KindOneDrive {
		return "microsoft:entra"
	}
	if clientType == "" {
		clientType = "tv"
	}
	return "google:" + clientType
}

// deviceLoginWait bounds a device login's wait, as the holder's
// pollDevice does: a status that never comes (the hub restarted, the
// login's state gone) ends the login.
const deviceLoginWait = 31 * time.Minute

// deviceLogin is a destination's device flow on the hub (B1): the owner
// opens the address from any browser and types the code, within
// deviceLoginWait.
func (c *mi5Cmd) deviceLogin(ctx context.Context, d mi5.DestConfig) (hubproc.Completion, error) {
	st, err := runstate.Load(c.e.statePath())
	if err != nil {
		return hubproc.Completion{}, err
	}
	client := mi6ClientKey(d.Kind, "tv")
	if st.Handoffs[d.Account+"/"+client] >= mi6.HandoffCap {
		return hubproc.Completion{}, mi6.ErrCap
	}
	provider := mi6.ProviderDrive
	if d.Kind == mi5.KindOneDrive {
		provider = mi6.ProviderOneDrive
	}
	res, err := c.h.ctl.Begin(ctx, hubproc.BeginRequest{DestID: d.TokenRef, Provider: provider, Platform: "web", Plan: mi6.PlanB1})
	if err != nil {
		return hubproc.Completion{}, err
	}
	c.e.printf(msgMI5DeviceLogin, d.ID, res.VerificationURL, res.UserCode)
	deadline := c.e.now().Add(deviceLoginWait)
	for {
		select {
		case <-ctx.Done():
			return hubproc.Completion{}, ctx.Err()
		case <-time.After(c.e.devicePoll()):
		}
		if !c.e.now().Before(deadline) {
			return hubproc.Completion{}, ownerError(fmt.Sprintf(msgMI5LoginFailed, d.ID, msgMI5LoginNoAnswer))
		}
		s, err := c.h.ctl.Status(ctx, res.State)
		if err != nil {
			continue
		}
		switch s.Status {
		case "completed":
			if s.Completion != nil {
				return *s.Completion, nil
			}
			return hubproc.Completion{IssuedAt: time.Now()}, nil
		case "expired", "failed":
			return hubproc.Completion{}, ownerError(fmt.Sprintf(msgMI5LoginFailed, d.ID, s.Reason))
		}
	}
}

// labClient is a client of the lab's login rig at the fakes and in the
// hub: SPIKE-ONLY values that no provider issued.
var labClient = map[string]any{"provider": "google", "type": "desktop", "id": "lab-desktop-client.apps.example", "secret": "lab-desktop-secret-0001", //nolint:gosec // G101: the lab rig's fake client
	"project": "lab"}

// labClients registers the rig's clients at the fakes and seals them in
// the hub.
func labClients(ctx context.Context, fakes string, ctl *hubproc.ControlClient) error {
	b, _ := json.Marshal(labClient) //nolint:errchkjson // a map of strings
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fakes+"/oauth/fake/clients", strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("mi5: the lab's clients: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("mi5: the lab's clients: %d", resp.StatusCode)
	}
	clients := map[string]any{"google": map[string]any{
		"android": map[string]string{"id": "lab-android-client.apps.example"},
		"desktop": map[string]string{"id": labClient["id"].(string), "secret": labClient["secret"].(string)}, //nolint:forcetypeassert // strings
		"tv":      map[string]string{"id": "lab-tv-client.apps.example", "secret": "lab-tv-secret-0001"},     //nolint:gosec // G101: the lab rig's fake client
		"web":     map[string]string{"id": "lab-web-client.apps.example"},
	}}
	raw, _ := json.Marshal(clients) //nolint:errchkjson // maps of strings
	return ctl.SetClients(ctx, raw)
}

// labLogin is the lab's login rig (SPIKE-ONLY): it begins a plan A login
// on the hub, fetches its AuthURL from the fake provider, which approves
// at once, takes the redirect's code and completes the login.
func labLogin(ctx context.Context, ctl *hubproc.ControlClient, d mi5.DestConfig) (hubproc.Completion, error) {
	provider := mi6.ProviderDrive
	if d.Kind == mi5.KindOneDrive {
		provider = mi6.ProviderOneDrive
	}
	res, err := ctl.Begin(ctx, hubproc.BeginRequest{DestID: d.TokenRef, Provider: provider, Platform: "windows", LoopbackPort: 1, Plan: mi6.PlanA})
	if err != nil {
		return hubproc.Completion{}, err
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, res.AuthURL, nil)
	if err != nil {
		return hubproc.Completion{}, errors.New("mi5: the lab login's address")
	}
	resp, err := client.Do(req)
	if err != nil {
		return hubproc.Completion{}, errors.New("mi5: the lab login's provider did not answer")
	}
	_ = resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || loc.Query().Get("code") == "" {
		return hubproc.Completion{}, errors.New("mi5: the lab login's provider gave no code")
	}
	return ctl.Complete(ctx, loc.Query().Get("state"), loc.Query().Get("code"))
}

// preChecks are P38's and P53's, with the hub stopped (dest-probe
// refuses while a hub holds the catalog): each destination's quota, the
// spool's free space, and no daily refresh of the hub's tokens in the
// block's window.
func (c *mi5Cmd) preChecks(ctx context.Context, cfg mi5.Mi5Config, comp *mi5Comp) error {
	st, err := runstate.Load(c.e.statePath())
	if err != nil {
		return err
	}
	files, err := c.h.files(st)
	if err != nil {
		return err
	}
	hc := c.e.hubConfig()
	hc.Files = files
	if err := hubproc.WriteFiles(hc); err != nil {
		return err
	}
	free := map[string]int64{}
	for _, d := range comp.dests {
		lines, err := hubproc.Diag(ctx, c.e.hubConfig(), []string{"dest-probe", "--config", c.e.runJSON(), "--dest", d.ID}, nil)
		if err != nil {
			return &mi5.Failure{Line: fmt.Sprintf(mi5.MsgDestProbeFailed, d.ID), Cause: err}
		}
		for _, raw := range lines {
			var l struct {
				Schema string `json:"schema"`
				Probe  struct {
					Quota struct {
						Free int64 `json:"free"`
					} `json:"quota"`
				} `json:"probe"`
			}
			if json.Unmarshal(raw, &l) == nil && l.Schema == "ipcam.diag.destprobe/1" {
				free[d.ID] = l.Probe.Quota.Free
			}
		}
	}
	spool, err := diskFree(c.e.dataDir())
	if err != nil {
		return err
	}
	if err := mi5.PreCheck(cfg, comp.dests, free, spool); err != nil {
		return refusal{err}
	}
	var tokens []mi5.Token
	if raw, ok := st.Open["MI6"]; ok {
		if run, err := mi6.Decode(raw); err == nil {
			for _, p := range run.Pairs {
				if p.IssuedAtMs != 0 {
					tokens = append(tokens, mi5.Token{Name: p.Dest, IssuedAt: time.UnixMilli(p.IssuedAtMs)})
				}
			}
		}
	}
	for _, l := range c.open.Logins {
		tokens = append(tokens, mi5.Token{Name: l.TokenRef, IssuedAt: time.UnixMilli(l.IssuedAtMs)})
	}
	timing := runTiming(cfg) // the block's start as this run's own timing puts it
	if err := mi5.CheckRefreshes(c.e.now().Add(timing.PreRun+timing.WarmUp), cfg.BlockFor, tokens); err != nil {
		return refusal{err}
	}
	return nil
}

// refusal is a pre-check's refusal (P38, P53): the run never started.
type refusal struct{ error }

func (r refusal) Unwrap() error { return r.error }

// refuse ends a run that its pre-checks refused before it started: no
// camera recorded, nothing was blocked, so it writes no envelope; the run
// leaves the run state, and the owner reads the refusal's own line.
func (c *mi5Cmd) refuse(err error) error {
	c.detail(err)
	c.h.stopHub()
	c.closeRun()
	return err
}

// detail prints the cause of one of MI5's own failures (mi5.Failure) on a
// detail line, for the lab's and the tests' runs only, which test seams
// mark and nobody judges: the owner reads the Romanian line alone.
func (c *mi5Cmd) detail(err error) {
	var f *mi5.Failure
	if c.e.seams && errors.As(err, &f) && f.Cause != nil {
		_, _ = fmt.Fprintf(c.e.errOut, msgFailureDetail+"\n", f.Cause)
	}
}

// fail ends a run that could not go on: the block removed, the run's
// failure in its envelope, the run closed; its line is the owner's.
func (c *mi5Cmd) fail(ctx context.Context, err error) error {
	c.detail(err)
	_ = c.e.firewall().RemoveStale(context.WithoutCancel(ctx))
	c.h.stopHub()
	if ctx.Err() != nil {
		// A signal: the run ends invalid, which resume records (P49).
		c.open.Interrupted = true
		_ = c.saveOpen()
		return err
	}
	reason := invalidStopped
	if errors.Is(err, errMI5HubDown) {
		reason = invalidHubDown
	}
	c.writeEnvelope(nil, false, reason)
	c.closeRun()
	return err
}

// The reasons of an invalid MI5 run.
const (
	invalidReboot  = "repornire în timpul probei MI5"
	invalidStopped = "proba MI5 s-a oprit înainte de final"
	invalidHubDown = "hub-ul s-a oprit singur și nu a mai pornit"
)

// writeEnvelope writes the run's envelope: valid with data, or invalid
// with reason.
func (c *mi5Cmd) writeEnvelope(data *results.Mi5Data, valid bool, reason string) {
	if err := writeMI5(c.e, c.open, c.host, c.info, data, valid, reason); err != nil {
		c.e.printf("%s %v", msgError, err)
	}
}

// writeMI5 writes an MI5 envelope of run o.
func writeMI5(e *env, o mi5Open, host string, info *hostinfo.Host, data *results.Mi5Data, valid bool, reason string) error {
	st, err := runstate.Load(e.statePath())
	if err != nil {
		return err
	}
	env := results.Envelope{Schema: results.Schema, Code: "MI5", RunID: o.RunID, Set: host, Host: host, OS: runtime.GOOS,
		Storage: "unknown", SdConfirmation: st.Sd, Sealer: "software", Account: "acct-1", AccountKind: "test", TestSeams: e.seams || o.Lab,
		Runs: []results.Run{{RunID: o.RunID, StartMs: o.StartMs, EndMs: e.now().UnixMilli(), Valid: valid, InvalidReason: reason}}, MI5: data}
	if o.Drive == mi5.DrivePersonal {
		env.AccountKind = "personal"
	}
	if runtime.GOOS == "windows" {
		env.Sealer = "dpapi-machine"
	}
	if info != nil {
		env.PiModel, env.Storage = info.PiModel, info.Storage
	}
	for _, d := range o.Dests {
		if d.ID == "dst-1" {
			env.Cameras = len(d.Cameras)
		}
	}
	for _, g := range st.Gaps {
		if g.EndMs > o.StartMs {
			env.Runs[0].Gaps = append(env.Runs[0].Gaps, g)
		}
	}
	return results.Write(e.resultsDir(), env)
}

// closeRun removes the run from the run state.
func (c *mi5Cmd) closeRun() {
	_ = runstate.Update(c.e.statePath(), func(s *runstate.State) error {
		delete(s.Open, "MI5")
		return nil
	})
}

// close is step 9: the envelope, then the results' scan with this
// process's control tokens and the cameras' credentials (P45); a failed
// scan fails the run and its envelope is written again and scanned once
// more; then the run leaves the run state, mi5 stops its hub and releases
// the lock, and the autostart's resume takes the hub.
func (c *mi5Cmd) close(ctx context.Context, data *results.Mi5Data) error {
	c.recordBlockGap(data)
	c.writeEnvelope(data, true, "")
	values := append([]string{}, c.h.tokens...)
	if c.h.mi5 != nil {
		values = append(values, c.h.mi5.session.User, c.h.mi5.session.Pass)
	}
	failed := false
	if f := scanResults(ctx, c.e, values); f != nil {
		failed = true
		c.e.printf("%s", f.line)
		c.writeEnvelope(data, false, f.reason)
		if last := scanResults(ctx, c.e, values); last != nil {
			c.e.printf("%s", last.line)
		}
	}
	c.closeRun()
	c.h.stopHub()
	c.e.printf(msgMI5Done)
	if failed {
		return exitError(1)
	}
	return nil
}

// recordBlockGap records the block as a gap of the device (P53): an MI6
// refresh day whose attempts all fell in it is blocked-by-test.
func (c *mi5Cmd) recordBlockGap(data *results.Mi5Data) {
	if data == nil || len(data.Reach) == 0 || data.BlockSecs <= 0 {
		return
	}
	start := data.Reach[0].AtMs
	gap := results.Gap{StartMs: start, EndMs: start + int64(data.BlockSecs*1000), Reason: results.GapMI5Block}
	_ = runstate.Update(c.e.statePath(), func(s *runstate.State) error {
		s.Gaps = append(s.Gaps, gap)
		return nil
	})
}

// endDeadMI5 ends an open MI5 run whose mi5 no longer runs, or that
// marked itself interrupted (Holding the hub): its block and cleanup are
// removed, and the run is closed invalid («repornire în timpul probei
// MI5» after a reboot, else «proba MI5 s-a oprit înainte de final»); its
// pending placements stay in the catalog, which no MI5 route of the next
// composition reaches. The caller holds hub.lock.
func endDeadMI5(ctx context.Context, e *env) {
	st, err := runstate.Load(e.statePath())
	if err != nil {
		return
	}
	raw, ok := st.Open["MI5"]
	if !ok {
		return
	}
	if _, live := mi5State(st); live {
		return
	}
	_ = e.firewall().RemoveStale(ctx)
	var o mi5Open
	_ = json.Unmarshal(raw, &o)
	reason := invalidStopped
	if boot, err := runstate.BootID(); err == nil && o.BootID != "" && !runstate.SameBoot(boot, o.BootID) {
		reason = invalidReboot
	}
	if o.RunID != "" {
		host, info := hostName(ctx, e)
		if o.Lab {
			host = "lab-linux"
			if runtime.GOOS == "windows" {
				host = "lab-windows"
			}
		}
		_ = writeMI5(e, o, host, info, nil, false, reason)
	}
	_ = runstate.Update(e.statePath(), func(s *runstate.State) error {
		delete(s.Open, "MI5")
		return nil
	})
	e.printf(msgMI5EndedInvalid, reason)
}

// mi5Host is mi5.Host through the holder.
type mi5Host struct{ h *holder }

func (m *mi5Host) StartHub(ctx context.Context, p mi5.Phase) error {
	m.h.mi5.phase = p
	return m.h.startHub(ctx)
}

func (m *mi5Host) StopHub(context.Context) error {
	m.h.stopHub()
	return nil
}

func (m *mi5Host) KillHub() error {
	m.h.mu.Lock()
	hub := m.h.hub
	m.h.stopping = true
	m.h.mu.Unlock()
	if hub == nil {
		return nil
	}
	return hub.Kill()
}

func (m *mi5Host) ResetConnections(ctx context.Context) error {
	m.h.mu.Lock()
	ctl := m.h.ctl
	m.h.mu.Unlock()
	return ctl.ResetConnections(ctx)
}

func (m *mi5Host) Diag(ctx context.Context, args []string) ([]json.RawMessage, error) {
	return hubproc.Diag(ctx, m.h.e.hubConfig(), args, nil)
}

func (m *mi5Host) HubGone() <-chan struct{} { return m.h.gone() }

// mi5CrashTries bounds the starts that one crash's restart tries: a hub
// that no longer starts ends the run (errMI5HubDown) instead of holding
// the owner's window open for hours.
const mi5CrashTries = 5

// errMI5HubDown is a run whose hub ended on its own and would not start
// again.
var errMI5HubDown = ownerError(msgMI5HubDown)

// RestartCrashed is the holder's restart after a crash (Task 9) under
// mi5: a hub that ended on its own, neither stopped nor killed by the run,
// is said («Hub-ul s-a oprit singur; îl repornesc.») and started again
// with the composition of the run's phase, its hub-crash gap recorded in
// the run state, whence the envelope's gaps; a start that fails is tried
// again after the holder's growing wait, mi5CrashTries starts at most.
func (m *mi5Host) RestartCrashed(ctx context.Context) (bool, error) {
	h := m.h
	if h.isStopping() {
		return false, nil
	}
	gone := h.gone()
	for range mi5CrashTries {
		h.restartAfterCrash(ctx)
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if h.gone() != gone {
			return true, nil
		}
	}
	return false, errMI5HubDown
}

func (m *mi5Host) HubConfig() hubproc.Config { return m.h.e.hubConfig() }
func (m *mi5Host) RunJSON() string           { return m.h.e.runJSON() }
func (m *mi5Host) Printf(format string, args ...any) {
	m.h.e.printf(format, args...)
}
