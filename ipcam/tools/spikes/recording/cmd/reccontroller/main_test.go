package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net"
	"os"
	"path/filepath"
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
