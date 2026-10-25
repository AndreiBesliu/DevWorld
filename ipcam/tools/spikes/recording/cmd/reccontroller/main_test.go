package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// manualClock is a source.Clock that moves only when the test sets it;
// each move wakes every ticker.
type manualClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*manualTicker
}

func newManualClock() *manualClock {
	return &manualClock{now: time.Date(2026, 10, 25, 22, 0, 0, 0, time.UTC)}
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) set(t time.Time) {
	c.mu.Lock()
	c.now = t
	tickers := append([]*manualTicker(nil), c.tickers...)
	c.mu.Unlock()
	for _, tk := range tickers {
		select {
		case tk.ch <- t:
		default:
		}
	}
}

func (c *manualClock) NewTicker(time.Duration) source.Ticker {
	tk := &manualTicker{ch: make(chan time.Time, 1)}
	c.mu.Lock()
	c.tickers = append(c.tickers, tk)
	c.mu.Unlock()
	return tk
}

type manualTicker struct{ ch chan time.Time }

func (t *manualTicker) C() <-chan time.Time { return t.ch }
func (t *manualTicker) Stop()               {}

// fakePacer records when the controller marks the pacing and returns stats.
type fakePacer struct {
	clock *manualClock
	stats controller.PacingStats
	mu    sync.Mutex
	marks []time.Time
}

func (p *fakePacer) MarkPacing() controller.PacingMark {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.marks = append(p.marks, p.clock.Now())
	return controller.PacingMark{}
}

func (p *fakePacer) Pacing(controller.PacingMark, controller.PacingMark, time.Time) controller.PacingStats {
	return p.stats
}

func (p *fakePacer) markTimes() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Time(nil), p.marks...)
}

// syncBuffer is a goroutine-safe screen.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// rig is a controller with a manual clock, a fake pacer, a real beacon
// socket and its screen.
type rig struct {
	t       *testing.T
	c       *ctl
	clock   *manualClock
	pacer   *fakePacer
	screen  *syncBuffer
	beacon  net.Conn // the host's beacon socket
	results string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	clock := newManualClock()
	r := &rig{t: t, clock: clock, pacer: &fakePacer{clock: clock, stats: controller.PacingStats{P99: 4 * time.Millisecond}},
		screen: &syncBuffer{}, results: filepath.Join(t.TempDir(), "results")}
	r.c = newCtl(t.Context(), ctlConfig{pacer: r.pacer, sendLog: controller.NewSendLog(), clock: clock, out: r.screen,
		results: r.results, rtspPort: 8554, beaconPort: 7082, creds: controller.Credentials{User: "u", Pass: "p"}})
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	go func() { _ = controller.ReceiveBeacons(t.Context(), udp, r.c.beacons, clock) }()
	if r.beacon, err = net.Dial("udp4", udp.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.beacon.Close() })
	return r
}

// connect opens a host connection to the controller and says hello.
func (r *rig) connect(host string) *link.Conn {
	r.t.Helper()
	a, b := net.Pipe()
	go r.c.serve(r.t.Context(), link.NewConn(a), &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: link.Port})
	conn := link.NewConn(b)
	r.t.Cleanup(func() { _ = conn.Close() })
	r.send(conn, link.Hello{Role: "host", Host: host, Version: "c1"})
	if _, ok := r.receive(conn).(link.Session); !ok {
		r.t.Fatal("no session after hello")
	}
	return conn
}

func (r *rig) send(conn *link.Conn, m link.Message) {
	r.t.Helper()
	if err := conn.Send(m); err != nil {
		r.t.Fatalf("send %s: %v", m.Type(), err)
	}
}

// receive returns the controller's next message, within 10 s.
func (r *rig) receive(conn *link.Conn) link.Message {
	r.t.Helper()
	type got struct {
		m   link.Message
		err error
	}
	ch := make(chan got, 1)
	go func() {
		m, err := conn.Receive()
		ch <- got{m, err}
	}()
	select {
	case g := <-ch:
		if g.err != nil {
			r.t.Fatalf("receive: %v", g.err)
		}
		return g.m
	case <-time.After(10 * time.Second):
		r.t.Fatal("the controller sent nothing")
		return nil
	}
}

// beaconAt sends the run's beacon seq at clock time at and waits until
// the controller has it.
func (r *rig) beaconAt(runID string, seq uint64, at time.Time) {
	r.t.Helper()
	r.clock.set(at)
	// Until the controller has read the run_start, it drops the run's
	// beacons: send again until it keeps one.
	waitFor(r.t, "the beacon", func() bool {
		if _, err := r.beacon.Write(link.BeaconPacket(runID, seq)); err != nil {
			r.t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
		b := r.c.beacons.Beacons(runID)
		return len(b) > 0 && b[len(b)-1].Seq == seq
	})
}

// waitFor polls cond for up to 10 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestScheduleCut_OncePerRunWhateverTheLink (C16): a run's cut is scheduled
// once, under the controller's own context: when the host's link drops
// after `ready`, the real cut's prompt still shows at its moment, and when
// the host reconnects and repeats `ready`, the schedule and its PromptAt
// stay, and the cut goes out on the new link.
func TestScheduleCut_OncePerRunWhateverTheLink(t *testing.T) {
	r := newRig(t)
	first := r.connect("pc")
	r.send(first, link.RunStart{RunID: "real-pc-01-1", Set: "real-pc", Kind: "power", Encrypted: true, PadLowBitrate: true,
		Streams: []string{"cam01_main"}, Host: "pc", OS: "windows", Storage: "ssd", Slot: 1, Attempt: 1})
	r.send(first, link.Ready{RunID: "real-pc-01-1"})
	boot := r.clock.Now().Add(time.Second)
	r.beaconAt("real-pc-01-1", 0, boot)
	waitFor(t, "the schedule", func() bool {
		r.c.mu.Lock()
		defer r.c.mu.Unlock()
		run := r.c.runs["real-pc-01-1"]
		return run != nil && run.scheduled
	})
	_ = first.Close() // the link drops

	again := r.connect("pc")
	r.send(again, link.Ready{RunID: "real-pc-01-1"})
	waitFor(t, "the repeated ready", func() bool { return strings.Contains(r.screen.String(), "din nou „ready”") })

	// The prompt is drawn in [3, 10] min after the boot; keep the beacons
	// going until past it.
	at := boot
	for seq := uint64(20); at.Before(boot.Add(10*time.Minute + time.Second)); seq += 20 {
		at = at.Add(time.Second)
		r.beaconAt("real-pc-01-1", seq, at)
	}
	cut, ok := r.receive(again).(link.Cut)
	if !ok || cut.RunID != "real-pc-01-1" {
		t.Fatalf("the reconnected host got %+v, want the cut", cut)
	}
	if d := cut.PromptAt.Sub(boot); d < 3*time.Minute || d > 10*time.Minute+time.Second {
		t.Errorf("PromptAt is %v after the boot, want 3 to 10 min", d)
	}
	if n := strings.Count(r.screen.String(), "Scoateți acum alimentarea"); n != 1 {
		t.Errorf("the prompt shows %d times, want once:\n%s", n, r.screen.String())
	}
	r.c.mu.Lock()
	got := r.c.runs["real-pc-01-1"].cut.PromptAt
	r.c.mu.Unlock()
	if !got.Equal(cut.PromptAt) {
		t.Errorf("the run's PromptAt %v, the cut's %v", got, cut.PromptAt)
	}
}

// TestStartRun_RefusesInRomanian: a run of an unknown kind, or a smoke run
// of an owner's set, gets a Romanian reply instead of silence.
func TestStartRun_RefusesInRomanian(t *testing.T) {
	r := newRig(t)
	conn := r.connect("pc")
	r.send(conn, link.RunStart{RunID: "pc-kill9-01-1", Set: "pc-kill9", Kind: "restart", Streams: []string{"cam01_main"}, Host: "pc"})
	if m, ok := r.receive(conn).(link.Refused); !ok || m.RunID != "pc-kill9-01-1" || !strings.Contains(m.Reason, "„restart” nu este cunoscut") {
		t.Errorf("reply = %#v, want a Romanian refusal of the kind", m)
	}
	r.send(conn, link.RunStart{RunID: "pc-kill9-01-2", Set: "pc-kill9", Kind: "kill9", Streams: []string{"cam01_main"}, Host: "pc", Smoke: true})
	if m, ok := r.receive(conn).(link.Refused); !ok || !strings.Contains(m.Reason, "laborator") {
		t.Errorf("reply = %#v, want the smoke windows refused outside the lab", m)
	}
	r.c.mu.Lock()
	defer r.c.mu.Unlock()
	if len(r.c.runs) != 0 {
		t.Errorf("refused runs are kept: %v", r.c.runs)
	}
}

// TestReport_NoBeaconHasItsOwnReason: a run whose host never beaconed is
// invalid for that reason, «niciun semnal de la gazdă», not as a stop
// outside the window.
func TestReport_NoBeaconHasItsOwnReason(t *testing.T) {
	r := newRig(t)
	conn := r.connect("pc")
	r.send(conn, link.RunStart{RunID: "pc-kill9-01-1", Set: "pc-kill9", Kind: "kill9", Streams: []string{"cam01_main"}, Host: "pc", OS: "windows", Attempt: 1})
	r.send(conn, link.Report{RunID: "pc-kill9-01-1", Hub04Clear: true})
	v, ok := r.receive(conn).(link.Verdict)
	if !ok || v.Run.Valid || v.Run.InvalidReason != "niciun semnal de la gazdă" {
		t.Errorf("verdict = %+v, want invalid: niciun semnal de la gazdă", v)
	}
	if mi1.Counts(v.Run) {
		t.Error("the run counts")
	}
}

// TestReport_WritesTheEnvelope (C16): the controller judges a kill9 run on
// its own clock (the stop is the last beacon, the loss is the stop minus
// the send time of the last recorded index), marks the end of its pacing
// when the beacons go silent, writes the run's envelope with what the host
// said of itself, and answers the report with the verdict; the same report
// sent again gets the same verdict.
func TestReport_WritesTheEnvelope(t *testing.T) {
	r := newRig(t)
	conn := r.connect("pi")
	piModel := &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Tier: "H1b", Supported: true, Known: true, RecommendedMin: 4, RecommendedMax: 4, Source: "cpuinfo"}
	sd := &results.SdConfirmation{Text: "DA", AtMs: 1792610000000}
	r.send(conn, link.RunStart{RunID: "pi-kill9-05-1", Set: "pi-kill9", Kind: "kill9", Encrypted: true, PadLowBitrate: true,
		Streams: []string{"cam01_main", "cam02_main"}, Host: "pi", OS: "linux", Storage: "sd", PiModel: piModel, SdConfirmation: sd, Attempt: 1})
	r.send(conn, link.Ready{RunID: "pi-kill9-05-1"})
	boot := r.clock.Now().Add(time.Second)
	r.beaconAt("pi-kill9-05-1", 0, boot)
	var cut link.Cut
	cutCh := make(chan link.Cut, 1)
	go func() {
		m, err := conn.Receive()
		if err == nil {
			if c, ok := m.(link.Cut); ok {
				cutCh <- c
			}
		}
		close(cutCh)
	}()
	at, seq := boot, uint64(0)
	for cut.RunID == "" {
		at, seq = at.Add(time.Second), seq+20
		r.beaconAt("pi-kill9-05-1", seq, at)
		select {
		case c, ok := <-cutCh:
			if !ok {
				t.Fatal("the link ended before the cut")
			}
			cut = c
		default:
		}
		if at.After(boot.Add(13 * time.Minute)) {
			t.Fatal("no cut 13 min after the boot")
		}
	}
	// The host is killed 10 ms after the prompt; its last beacon is the one
	// it sent before, the stop moment.
	stopAt := at
	if cut.PromptAt.After(stopAt) {
		stopAt = cut.PromptAt.Add(-10 * time.Millisecond)
		r.beaconAt("pi-kill9-05-1", seq+1, stopAt)
	}
	r.clock.set(stopAt.Add(controller.BeaconSilence + 100*time.Millisecond))
	waitFor(t, "the pacing's end mark", func() bool {
		marks := r.pacer.markTimes()
		return len(marks) >= 2 && marks[len(marks)-1].After(stopAt.Add(controller.BeaconSilence))
	})
	r.c.sendLog.Record("cam01_main", 7000, stopAt.Add(-850*time.Millisecond))
	r.c.sendLog.Record("cam02_main", 7001, stopAt.Add(-1200*time.Millisecond))
	resume := int64(812)
	report := link.Report{
		RunID: "pi-kill9-05-1",
		Streams: []link.StreamIndex{
			{Stream: "cam01_main", LastRecordedIndex: 7000},
			{Stream: "cam02_main", LastRecordedIndex: 7001},
		},
		Reconcile:  json.RawMessage(`{"type":"reconcile","seq":1,"final":true,"aborted":false,"unreadable":0,"tornParts":0,"catalogMismatches":0}`),
		Verify:     json.RawMessage(`{"type":"verify","unreadable":0,"tornParts":0,"catalogMismatches":0}`),
		Hub04Clear: true, ResumeMs: &resume, NtpWaitMs: 2000, NtpSynced: true, WriteCache: "unknown",
	}
	r.send(conn, report)
	v, ok := r.receive(conn).(link.Verdict)
	if !ok {
		t.Fatal("no verdict")
	}
	run := v.Run
	if !run.Valid || run.InvalidReason != "" || !mi1.Counts(run) || run.LossMs != 1200 || !run.Reconciled || !run.Hub04Clear ||
		run.PacingP99Ms != 4 || run.Cameras != 2 || run.ResumeMs == nil || *run.ResumeMs != 812 || run.ShutdownKind != "kill9" {
		t.Errorf("verdict = %+v", run)
	}
	if len(run.Streams) != 2 || run.Streams[0].LossMs != 850 || run.Streams[0].BitrateKbps != 4000 || !run.Streams[0].Encrypted || run.Streams[0].LimitMs != 2000 {
		t.Errorf("streams = %+v", run.Streams)
	}
	env, err := results.Read(filepath.Join(r.results, "MI1", "pi-kill9", "pi-kill9-05-1.json"))
	if err != nil {
		t.Fatalf("the envelope: %v", err)
	}
	if env.Host != "pi" || env.OS != "linux" || env.Storage != "sd" || env.PiModel == nil || *env.PiModel != *piModel ||
		env.SdConfirmation == nil || *env.SdConfirmation != *sd || len(env.Runs) != 1 || env.Runs[0].LossMs != 1200 {
		t.Errorf("envelope = %+v", env)
	}
	r.send(conn, report)
	if again, ok := r.receive(conn).(link.Verdict); !ok || again.Run.LossMs != run.LossMs || !again.Run.Valid {
		t.Errorf("the report sent again got %+v, want the same verdict", again)
	}

	t.Run("a stream the host could not read lost the whole run", func(t *testing.T) {
		r := newRig(t)
		conn := r.connect("pc")
		r.send(conn, link.RunStart{RunID: "pc-kill9-02-1", Set: "pc-kill9", Kind: "kill9", Streams: []string{"cam01_main"}, Host: "pc", Attempt: 1})
		boot := r.clock.Now()
		r.beaconAt("pc-kill9-02-1", 0, boot)
		r.beaconAt("pc-kill9-02-1", 20, boot.Add(time.Second))
		r.send(conn, link.Report{RunID: "pc-kill9-02-1"})
		if v, ok := r.receive(conn).(link.Verdict); !ok || len(v.Run.Streams) != 1 || v.Run.Streams[0].LossMs != 1000 || v.Run.Reconciled {
			t.Errorf("verdict = %+v, want cam01_main lost from its first beacon, and not reconciled without the events", v.Run)
		}
	})
}

// TestBeaconCheck_AcknowledgesOverTCP (R08): the preflight's beacon check
// is answered over the link once a beacon of its id arrives.
func TestBeaconCheck_AcknowledgesOverTCP(t *testing.T) {
	r := newRig(t)
	conn := r.connect("pc")
	r.send(conn, link.BeaconCheck{ID: "verificare-1"})
	waitFor(t, "the check's expectation", func() bool {
		if _, err := r.beacon.Write(link.BeaconPacket("verificare-1", 0)); err != nil {
			t.Fatal(err)
		}
		_, ok := r.c.beacons.First("verificare-1")
		return ok
	})
	if ack, ok := r.receive(conn).(link.BeaconAck); !ok || ack.ID != "verificare-1" || !ack.Received {
		t.Errorf("reply = %#v, want the beacon acknowledged", ack)
	}
}

// TestSuperviseBeacons_ShowsAndRestarts: a beacon receiver that fails is
// shown on the screen and started again; one that keeps failing stops for
// good, and says so.
func TestSuperviseBeacons_ShowsAndRestarts(t *testing.T) {
	delay := beaconRestartDelay
	beaconRestartDelay = time.Millisecond
	t.Cleanup(func() { beaconRestartDelay = delay })
	var screen syncBuffer
	say := func(f string, _ ...any) { _, _ = screen.Write([]byte(strings.TrimSpace(f) + "\n")) }
	failures := 0
	superviseBeacons(t.Context(), func(context.Context) error {
		failures++
		if failures <= 2 {
			return errors.New("wsarecvfrom: connection reset")
		}
		return nil
	}, say)
	if failures != 3 || strings.Count(screen.String(), "O repornesc.") != 2 {
		t.Errorf("%d receives, screen:\n%s\nwant two failures shown and a third receive", failures, screen.String())
	}
	screen = syncBuffer{}
	superviseBeacons(t.Context(), func(context.Context) error { return errors.New("broken") }, say)
	if !strings.Contains(screen.String(), "definitiv") {
		t.Errorf("screen:\n%s\nwant the receiver stopped for good", screen.String())
	}
}

// TestFlags_RomanianUsageAndErrors: reccontroller's usage and its flag
// errors are Romanian, never the flag package's English.
func TestFlags_RomanianUsageAndErrors(t *testing.T) {
	for args, want := range map[string]string{
		"-nope":        "opțiune necunoscută: -nope",
		"-local=maybe": "valoare greșită pentru -local",
		"-media":       "opțiunea cere o valoare: -media",
		"extra":        "argument necunoscut: extra",
		"pull extra":   "argument necunoscut: extra",
	} {
		var out syncBuffer
		err := run(t.Context(), strings.Fields(args), &out)
		if err == nil || err.Error() != want {
			t.Errorf("reccontroller %s = %v, want %q", args, err, want)
		}
		if !strings.Contains(out.String(), "Folosire:") || strings.Contains(out.String(), "Usage") || strings.Contains(out.String(), "flag provided") {
			t.Errorf("reccontroller %s printed:\n%s\nwant the Romanian usage only", args, out.String())
		}
	}
	var out syncBuffer
	if err := run(t.Context(), []string{"-h"}, &out); !errors.Is(err, flag.ErrHelp) || !strings.Contains(out.String(), "-local") {
		t.Errorf("reccontroller -h = %v, printed:\n%s", err, out.String())
	}
}

// writeEnvelope writes an envelope of code and set on host under dir.
func writeEnvelope(t *testing.T, dir, code, set, host string, piModel *results.PiModelInfo) {
	t.Helper()
	e := results.Envelope{Schema: results.Schema, Code: code, RunID: set + "-1", Set: set, Host: host, OS: "linux", PiModel: piModel,
		Runs: []results.Run{{RunID: set + "-1", Valid: true}}}
	if err := results.Write(dir, e); err != nil {
		t.Fatal(err)
	}
}

// TestSession_PiResultsPulledAtBye (G15): when the Pi, as the host, ends
// its session, the PC's controller pulls its results folder into
// results/pi; another host, or a host-local controller, is not pulled.
func TestSession_PiResultsPulledAtBye(t *testing.T) {
	piResults := t.TempDir()
	writeEnvelope(t, piResults, "MI2", "mi2", "pi", nil)
	for _, tc := range []struct {
		host  string
		local bool
		want  bool
	}{{"pi", false, true}, {"pc", false, false}, {"pi", true, false}} {
		r := newRig(t)
		r.c.local = tc.local
		conn := r.connect(tc.host)
		r.send(conn, link.Bye{})
		m, err := conn.Receive()
		if tc.want {
			if _, ok := m.(link.PullResults); !ok || err != nil {
				t.Fatalf("%s: after bye got %#v, %v; want pull_results", tc.host, m, err)
			}
			if err := conn.SendResultsTar(piResults); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the Pi's results", func() bool {
				_, err := os.Stat(filepath.Join(r.results, "pi", "MI2", "mi2", "mi2-1.json"))
				return err == nil
			})
			continue
		}
		if err == nil {
			t.Errorf("host %s, local %v: after bye got %#v, want the link closed", tc.host, tc.local, m)
		}
	}
}

// TestPull_PushStoresPiFolderAndPrintsInventory (G15, guide step 13):
// `reccontroller pull` on the PC and `recspike push` on the Pi, in process
// on loopback: the Pi's folder lands under results/pi, and the PC prints
// what both hold and, in Romanian, what is missing; with an unsupported
// Pi, nothing is expected with the Pi as host.
func TestPull_PushStoresPiFolderAndPrintsInventory(t *testing.T) {
	for _, supported := range []bool{true, false} {
		pcResults := filepath.Join(t.TempDir(), "results")
		writeEnvelope(t, filepath.Join(pcResults, "pi"), "MI1", "pc-kill9", "pc", nil) // pulled from the Pi's controller earlier
		piResults := t.TempDir()
		model := &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Tier: "H1b", Supported: true, Known: true, RecommendedMin: 4, RecommendedMax: 4, Source: "cpuinfo"}
		if !supported {
			model = &results.PiModelInfo{Raw: "Raspberry Pi Zero W Rev 1.1", Tier: "unsupported", Known: true, Source: "cpuinfo"}
		}
		writeEnvelope(t, piResults, "MI4", "mi4", "pi", model)

		ln, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		var pcOut, piOut syncBuffer
		done := make(chan error, 1)
		go func() { done <- pull(t.Context(), ln, pcResults, &pcOut) }()
		if err := mi1.Push(t.Context(), ln.Addr().String(), piResults, "pi", &piOut); err != nil {
			t.Fatalf("push: %v", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("pull: %v", err)
		}
		if _, err := os.Stat(filepath.Join(pcResults, "pi", "MI4", "mi4", "mi4-1.json")); err != nil {
			t.Errorf("the Pi's folder is not under results/pi: %v", err)
		}
		out := pcOut.String()
		if !strings.Contains(out, "Prezente: ") || !strings.Contains(out, "MI4 mi4 (pi)") || !strings.Contains(out, "MI1 pc-kill9 (pc)") {
			t.Errorf("supported %v: the inventory:\n%s\nwant both devices' results", supported, out)
		}
		if !strings.Contains(out, "Lipsesc: ") || !strings.Contains(out, "MI1 pc-bugcheck (pc)") {
			t.Errorf("supported %v: the inventory:\n%s\nwant the missing PC sets", supported, out)
		}
		if strings.Contains(out, "MI1 pi-kill9 (pi)") == !supported {
			t.Errorf("supported %v: the inventory:\n%s\nwant the Pi's sets expected only when it is supported", supported, out)
		}
		if !strings.Contains(piOut.String(), "Am trimis rezultatele.") {
			t.Errorf("the Pi printed:\n%s", piOut.String())
		}
	}
}

// TestScheduleCut_NoStaleCutAfterTheRunEnds (C16): a run that ends before
// its drawn moment, by its report (ipcam-hub exited by itself) or by the
// host's refusal, has its schedule cancelled: when the moment passes, the
// controller shows no prompt and sends no cut on the host's link, where it
// would stop the host's next run at a moment never drawn for it.
func TestScheduleCut_NoStaleCutAfterTheRunEnds(t *testing.T) {
	for _, tc := range []struct{ kind, end string }{{"kill9", "report"}, {"kill9", "refused"}, {"power", "report"}, {"power", "refused"}} {
		r := newRig(t)
		conn := r.connect("pc")
		id := "pc-kill9-04-1"
		set := "pc-kill9"
		if tc.kind == "power" {
			id, set = "real-pc-02-3", "real-pc"
		}
		r.send(conn, link.RunStart{RunID: id, Set: set, Kind: tc.kind, Streams: []string{"cam01_main"}, Host: "pc", OS: "windows", Attempt: 1})
		r.send(conn, link.Ready{RunID: id})
		boot := r.clock.Now().Add(time.Second)
		r.beaconAt(id, 0, boot)
		if tc.kind == "kill9" {
			waitFor(t, "the drawn moment", func() bool { return strings.Contains(r.screen.String(), "e programată la") })
		} else {
			time.Sleep(100 * time.Millisecond) // the schedule waits for its moment
		}
		switch tc.end {
		case "report":
			r.send(conn, link.Report{RunID: id})
			if _, ok := r.receive(conn).(link.Verdict); !ok {
				t.Fatalf("%s %s: no verdict", tc.kind, tc.end)
			}
		case "refused":
			r.send(conn, link.Refused{RunID: id, Reason: "oprire automată cerută în afara ferestrei 22:00-06:00"})
			waitFor(t, "the refusal on the screen", func() bool { return strings.Contains(r.screen.String(), "a renunțat") })
		}
		r.clock.set(boot.Add(13 * time.Minute)) // past every moment the schedule may have drawn
		got := make(chan link.Message, 1)
		go func() {
			if m, err := conn.Receive(); err == nil {
				got <- m
			}
		}()
		select {
		case m := <-got:
			t.Errorf("%s %s: the controller sent %#v after the run ended", tc.kind, tc.end, m)
		case <-time.After(500 * time.Millisecond):
		}
		if strings.Contains(r.screen.String(), "Scoateți acum alimentarea") {
			t.Errorf("%s %s: the screen shows the prompt of a run that ended:\n%s", tc.kind, tc.end, r.screen.String())
		}
	}
}

// validKill9Run announces a kill9 run that the controller will judge
// valid: a cut drawn 3 min after its first beacon, beacons every second up
// to 10 ms before it, and the send time of the index that the returned
// report says was recorded last.
func (r *rig) validKill9Run(conn *link.Conn, id string) link.Report {
	r.t.Helper()
	r.send(conn, link.RunStart{RunID: id, Set: "pc-kill9", Kind: "kill9", Encrypted: true, PadLowBitrate: true,
		Streams: []string{"cam01_main"}, Host: "pc", OS: "windows", Storage: "ssd", Attempt: 1})
	waitFor(r.t, "the run", func() bool {
		r.c.mu.Lock()
		defer r.c.mu.Unlock()
		return r.c.runs[id] != nil
	})
	boot := r.clock.Now().Add(time.Second)
	prompt := boot.Add(3 * time.Minute)
	r.c.mu.Lock()
	r.c.runs[id].cut.BootAt, r.c.runs[id].cut.PromptAt = boot, prompt
	r.c.mu.Unlock()
	seq := uint64(0)
	for at := boot; at.Before(prompt); at = at.Add(time.Second) {
		r.beaconAt(id, seq, at)
		seq += 20
	}
	stopAt := prompt.Add(-10 * time.Millisecond)
	r.beaconAt(id, seq, stopAt)
	r.c.sendLog.Record("cam01_main", 5000, stopAt.Add(-700*time.Millisecond))
	return link.Report{
		RunID:      id,
		Streams:    []link.StreamIndex{{Stream: "cam01_main", LastRecordedIndex: 5000}},
		Reconcile:  json.RawMessage(`{"type":"reconcile","seq":1,"final":true,"aborted":false,"unreadable":0,"tornParts":0,"catalogMismatches":0}`),
		Verify:     json.RawMessage(`{"type":"verify","unreadable":0,"tornParts":0,"catalogMismatches":0}`),
		Hub04Clear: true, NtpSynced: true, WriteCache: "unknown",
	}
}

// TestReport_UnwrittenEnvelopeIsRedone (C16, G15): when the controller
// cannot write a run's envelope (a full disk on the Pi controller, an
// envelope that fails its checks), the run does not count: the host gets an
// invalid verdict with the Romanian reason and redoes it, and the same
// report sent again is not answered with a counting verdict from memory.
// The same run with a writable folder counts, so the test is not vacuous.
func TestReport_UnwrittenEnvelopeIsRedone(t *testing.T) {
	for _, writable := range []bool{true, false} {
		r := newRig(t)
		if !writable {
			file := filepath.Join(t.TempDir(), "not-a-folder")
			if err := os.WriteFile(file, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			r.c.results = filepath.Join(file, "results") // every write fails: its parent is a file
		}
		conn := r.connect("pc")
		report := r.validKill9Run(conn, "pc-kill9-03-1")
		r.send(conn, report)
		v, ok := r.receive(conn).(link.Verdict)
		if !ok {
			t.Fatal("no verdict")
		}
		_, statErr := os.Stat(filepath.Join(r.c.results, "MI1", "pc-kill9", "pc-kill9-03-1.json"))
		if writable {
			if !mi1.Counts(v.Run) || v.Run.LossMs != 700 || statErr != nil {
				t.Errorf("writable: verdict %+v, envelope %v; want a counting run of 700 ms and its envelope", v.Run, statErr)
			}
			continue
		}
		if v.Run.Valid || v.Run.InvalidReason != "rezultatul nu s-a putut scrie la controlor" || mi1.Counts(v.Run) {
			t.Errorf("unwritable: verdict %+v, want invalid: rezultatul nu s-a putut scrie la controlor", v.Run)
		}
		if !strings.Contains(r.screen.String(), "nu pot scrie rezultatul") {
			t.Errorf("screen:\n%s\nwant the write error shown", r.screen.String())
		}
		r.send(conn, report)
		if again, ok := r.receive(conn).(link.Verdict); !ok || mi1.Counts(again.Run) {
			t.Errorf("the report sent again got %+v, want no counting verdict", again.Run)
		}
	}
}

// running counts the goroutines that run fn, a function of this package,
// such as "(*ctl).markEnd", somewhere in their stack.
func running(t *testing.T, fn string) int {
	t.Helper()
	var b strings.Builder
	if err := pprof.Lookup("goroutine").WriteTo(&b, 2); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range strings.Split(b.String(), "\n\n") {
		if strings.Contains(g, "/reccontroller."+fn+"(") {
			n++
		}
	}
	return n
}

// TestRun_GoroutinesEndWithTheRun: a run's goroutines at the controller,
// the watch for its beacons' end (markEnd) and its cut's schedule
// (scheduleCut), end with the run: at its report, at the host's refusal,
// and, for a run whose host never beacons, a minute after its ready, when
// the controller refuses it with «niciun semnal de la gazdă», so that
// neither side waits for a cut that never comes.
func TestRun_GoroutinesEndWithTheRun(t *testing.T) {
	both := func() int { return running(t, "(*ctl).markEnd") + running(t, "(*ctl).scheduleCut") }
	for _, end := range []string{"report", "refused", "no beacon"} {
		waitFor(t, "the goroutines of earlier runs to end", func() bool { return both() == 0 })
		r := newRig(t)
		conn := r.connect("pc")
		id := "pc-kill9-06-1"
		r.send(conn, link.RunStart{RunID: id, Set: "pc-kill9", Kind: "kill9", Streams: []string{"cam01_main"}, Host: "pc", OS: "windows", Attempt: 1})
		r.send(conn, link.Ready{RunID: id})
		boot := r.clock.Now().Add(time.Second)
		if end != "no beacon" {
			r.beaconAt(id, 0, boot)
			waitFor(t, "the drawn moment", func() bool { return strings.Contains(r.screen.String(), "e programată la") })
		}
		waitFor(t, "the run's two goroutines", func() bool { return both() == 2 })
		switch end {
		case "report":
			r.send(conn, link.Report{RunID: id})
			if _, ok := r.receive(conn).(link.Verdict); !ok {
				t.Fatal("no verdict")
			}
		case "refused":
			r.send(conn, link.Refused{RunID: id, Reason: "oprire automată cerută în afara ferestrei 22:00-06:00"})
		case "no beacon":
			r.clock.set(r.clock.Now().Add(firstBeaconWait + time.Second))
			if m, ok := r.receive(conn).(link.Refused); !ok || m.RunID != id || m.Reason != "niciun semnal de la gazdă" {
				t.Errorf("no beacon: the host got %#v, want the run refused: niciun semnal de la gazdă", m)
			}
		}
		waitFor(t, end+": the run's goroutines to end", func() bool { return both() == 0 })
		r.c.mu.Lock()
		if n := len(r.c.runs); n != 0 {
			t.Errorf("%s: the controller still holds %d runs", end, n)
		}
		r.c.mu.Unlock()
	}
}

// TestReport_BeaconGapMidRunHasItsOwnReason: a run whose beacons stopped
// for more than 2 s mid-run, and came back, is invalid with its own
// Romanian reason, which points at the network, not as a stop outside the
// window.
func TestReport_BeaconGapMidRunHasItsOwnReason(t *testing.T) {
	r := newRig(t)
	conn := r.connect("pc")
	id := "pc-kill9-07-1"
	r.send(conn, link.RunStart{RunID: id, Set: "pc-kill9", Kind: "kill9", Streams: []string{"cam01_main"}, Host: "pc", OS: "windows", Attempt: 1})
	boot := r.clock.Now().Add(time.Second)
	at, seq := boot, uint64(0)
	for ; at.Before(boot.Add(time.Minute)); at, seq = at.Add(time.Second), seq+20 {
		r.beaconAt(id, seq, at)
	}
	// 3 s without a beacon, then the host's beacons again.
	r.clock.set(at.Add(3 * time.Second))
	for range 3 {
		if _, err := r.beacon.Write(link.BeaconPacket(id, seq+60)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Millisecond)
	}
	r.send(conn, link.Report{RunID: id, Hub04Clear: true})
	v, ok := r.receive(conn).(link.Verdict)
	if !ok || v.Run.Valid || v.Run.InvalidReason != "semnalul gazdei s-a întrerupt peste 2 s în timpul rulării (verificați rețeaua)" {
		t.Errorf("verdict = %+v, want invalid with the beacon gap's own reason", v.Run)
	}
}

// TestReady_AgainAfterTheCutGetsTheCutAgain (C16): a host whose link
// dropped as its cut was sent reconnects and repeats `ready`; the
// controller sends it the same cut again, on the new link, rather than
// leaving it waiting for a cut that went out on the dead one.
func TestReady_AgainAfterTheCutGetsTheCutAgain(t *testing.T) {
	r := newRig(t)
	first := r.connect("pc")
	id := "pc-kill9-08-1"
	r.send(first, link.RunStart{RunID: id, Set: "pc-kill9", Kind: "kill9", Streams: []string{"cam01_main"}, Host: "pc", OS: "windows", Attempt: 1})
	r.send(first, link.Ready{RunID: id})
	boot := r.clock.Now().Add(time.Second)
	r.beaconAt(id, 0, boot)
	waitFor(t, "the drawn moment", func() bool { return strings.Contains(r.screen.String(), "e programată la") })
	r.clock.set(boot.Add(13 * time.Minute)) // past every moment the schedule may draw
	cut, ok := r.receive(first).(link.Cut)
	if !ok || cut.RunID != id {
		t.Fatalf("the host got %+v, want the run's cut", cut)
	}
	_ = first.Close() // the cut was lost with the link

	again := r.connect("pc")
	r.send(again, link.Ready{RunID: id})
	if got, ok := r.receive(again).(link.Cut); !ok || got.RunID != id || !got.PromptAt.Equal(cut.PromptAt) {
		t.Errorf("after ready again the host got %+v, want the same cut %+v", got, cut)
	}
}

// TestReport_UnwrittenEnvelopeKeepsTheRunsReason: a run that is already
// invalid keeps its reason when its envelope cannot be written; the write
// failure is added to it.
func TestReport_UnwrittenEnvelopeKeepsTheRunsReason(t *testing.T) {
	r := newRig(t)
	file := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.c.results = filepath.Join(file, "results")
	conn := r.connect("pc")
	r.send(conn, link.RunStart{RunID: "pc-kill9-09-1", Set: "pc-kill9", Kind: "kill9", Streams: []string{"cam01_main"}, Host: "pc", OS: "windows", Attempt: 1})
	r.send(conn, link.Report{RunID: "pc-kill9-09-1", Hub04Clear: true})
	v, ok := r.receive(conn).(link.Verdict)
	if want := "niciun semnal de la gazdă; rezultatul nu s-a putut scrie la controlor"; !ok || v.Run.Valid || v.Run.InvalidReason != want {
		t.Errorf("verdict = %+v, want invalid: %s", v.Run, want)
	}
}

// TestReady_UnknownRunIsRefused: a host that repeats `ready` after a
// reconnection to a controller that no longer knows the run (it was
// started again) is refused in Romanian, instead of waiting for a cut that
// no schedule will send.
func TestReady_UnknownRunIsRefused(t *testing.T) {
	r := newRig(t)
	conn := r.connect("pc")
	r.send(conn, link.Ready{RunID: "pc-kill9-10-1"})
	if m, ok := r.receive(conn).(link.Refused); !ok || m.RunID != "pc-kill9-10-1" || m.Reason != "rularea nu a fost anunțată controlorului" {
		t.Errorf("reply = %#v, want the run refused: rularea nu a fost anunțată controlorului", m)
	}
}
