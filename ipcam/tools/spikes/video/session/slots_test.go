package session

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/webrtc/v4"
)

// frames formats access units as "camera/frame" with a trailing "*" on an IDR.
func frames(aus []rxAU) string {
	var parts []string
	for _, au := range aus {
		s := fmt.Sprintf("%d/%d", au.camera, au.frame)
		if au.idr {
			s += "*"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func TestBind_FirstPacketStartsWithIDR(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 48, 12), 12)
	sim.emit(t, 3) // the GOP cache now holds frames 0 (IDR), 1 and 2
	c := newClient(t, clientConfig{slots: 2, offer: chromeLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, testOptions(source.SystemClock{}, nil))

	c.bind(t, 1, "sim-01", "sub", 7)
	m := c.next(t, "bound")
	if want := `{"t":"bound","slot":1,"epoch":7,"codec":"H264","profile":"4d401f"}`; m.raw != want {
		t.Errorf("bound = %s, want %s", m.raw, want)
	}
	var got []rxAU
	for range 3 {
		got = append(got, c.au(t, 1))
	}
	sim.emit(t, 2)
	got = append(got, c.au(t, 1), c.au(t, 1))

	if want := "1/0* 1/1 1/2 1/3 1/4"; frames(got) != want {
		t.Errorf("slot 1 received %s, want %s", frames(got), want)
	}
	if !strings.EqualFold(got[0].mime, webrtc.MimeTypeH264) {
		t.Errorf("first access unit came as %s, want H.264", got[0].mime)
	}
	checkContinuous(t, got)
	c.quiet(t, 0, 100*time.Millisecond)
}

func TestBind_SendsTheCachedGOPWithCompressedTimestamps(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 48, 12), 12)
	sim.emit(t, 4)
	clock := newFakeClock()
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, testOptions(clock, nil))

	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")
	var got []rxAU
	for range 4 {
		got = append(got, c.au(t, 0))
	}
	clock.advance(t, 40*time.Millisecond) // the camera's next frame comes 40 ms after the bind
	sim.emit(t, 1)
	got = append(got, c.au(t, 0))
	clock.advance(t, sim.period)
	sim.emit(t, 1)
	got = append(got, c.au(t, 0))
	if want := "1/0* 1/1 1/2 1/3 1/4 1/5"; frames(got) != want {
		t.Fatalf("slot 0 received %s, want %s", frames(got), want)
	}
	checkContinuous(t, got)

	// The cached frames are 1 ms (90 ticks) apart, so that the decoder catches
	// up without adding their age to the latency. The first live frame goes
	// out at the hub clock, 40 ms after the IDR and so 37 ms after the last
	// cached frame; live frames are one frame period (7500 ticks at 12 fps)
	// apart.
	checkSteps(t, got, []int64{90, 90, 90, 3330, 7500})
}

func TestRebind_SwitchesOnNextIDR(t *testing.T) {
	a := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 48, 4), 12)
	b := newTestSim(t, "sim-02", 2, "sub", h264AUs("4d401f", 2, 48, 4), 12)
	a.emit(t, 2)
	b.emit(t, 6) // b's GOP cache holds frames 4 (IDR) and 5
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	// The hub clock stands still, so no time passes between the binds.
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": a, "sim-02/sub": b}, testOptions(newFakeClock(), nil))

	c.bind(t, 0, "sim-01", "sub", 1)
	if m := c.next(t, "bound"); m.Epoch != 1 {
		t.Fatalf("bound = %s, want epoch 1", m.raw)
	}
	got := []rxAU{c.au(t, 0), c.au(t, 0)}
	a.emit(t, 1)
	got = append(got, c.au(t, 0))

	c.bind(t, 0, "sim-02", "sub", 2)
	if m := c.next(t, "bound"); m.Slot != 0 || m.Epoch != 2 {
		t.Fatalf("bound = %s, want slot 0 epoch 2", m.raw)
	}
	a.emit(t, 3) // the old camera keeps sending; none of it may reach the slot
	got = append(got, c.au(t, 0), c.au(t, 0))
	b.emit(t, 3)
	got = append(got, c.au(t, 0), c.au(t, 0), c.au(t, 0))

	if want := "1/0* 1/1 1/2 2/4* 2/5 2/6 2/7 2/8*"; frames(got) != want {
		t.Fatalf("slot 0 received %s, want %s", frames(got), want)
	}
	checkContinuous(t, got)
	// Each camera's cached GOP is compressed. The hub clock stands still, so
	// the frames that the slot stamps at the hub clock, the new camera's IDR
	// and each camera's first live frame, follow the frame before by 1 ms, the
	// least step; then live frames are one frame period apart.
	checkSteps(t, got, []int64{90, 90, 90, 90, 90, 7500, 7500})
	c.quiet(t, 0, 100*time.Millisecond)
}

func TestRebind_ChangesCodecWithinOneRTPStream(t *testing.T) {
	h264 := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 48, 4), 12)
	h265 := newTestSim(t, "mx-h265", 17, "main", h265AUs(17, 48, 4), 20)
	h264.emit(t, 1)
	h265.emit(t, 2)
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	// The hub clock stands still, so no time passes between the binds.
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": h264, "mx-h265/main": h265}, testOptions(newFakeClock(), nil))

	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")
	got := []rxAU{c.au(t, 0)}

	c.bind(t, 0, "mx-h265", "main", 2)
	if want := `{"t":"bound","slot":0,"epoch":2,"codec":"H265","profile":"017b"}`; c.next(t, "bound").raw != want {
		t.Errorf("bound for H.265 differs from %s", want)
	}
	got = append(got, c.au(t, 0), c.au(t, 0))
	h265.emit(t, 2)
	got = append(got, c.au(t, 0), c.au(t, 0))

	c.bind(t, 0, "sim-01", "sub", 3)
	c.next(t, "bound")
	got = append(got, c.au(t, 0))
	h264.emit(t, 2)
	got = append(got, c.au(t, 0), c.au(t, 0))

	if want := "1/0* 17/0* 17/1 17/2 17/3 1/0* 1/1 1/2"; frames(got) != want {
		t.Fatalf("slot 0 received %s, want %s", frames(got), want)
	}
	h264Mime, h265Mime := webrtc.MimeTypeH264, webrtc.MimeTypeH265
	wantMimes := []string{h264Mime, h265Mime, h265Mime, h265Mime, h265Mime, h264Mime, h264Mime, h264Mime}
	for i, au := range got {
		if !strings.EqualFold(au.mime, wantMimes[i]) {
			t.Errorf("access unit %d (%v) came as %s, want %s", i, au, au.mime, wantMimes[i])
		}
	}
	checkContinuous(t, got)
	// A new track continues the timestamps. The hub clock stands still, so
	// each bind's IDR follows the last frame by 1 ms, as do a cached frame and
	// the first live frame after the cached ones; then 20 fps H.265 frames are
	// 4500 ticks long, 12 fps H.264 frames 7500.
	checkSteps(t, got, []int64{90, 90, 90, 4500, 90, 90, 7500})
}

func TestUnbind_StopsTheSlot(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 48, 4), 12)
	sim.emit(t, 1)
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, testOptions(source.SystemClock{}, nil))

	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")
	got := []rxAU{c.au(t, 0)}

	c.send(t, `{"t":"unbind","slot":0}`)
	c.send(t, `{"t":"ping","c0":1}`)
	c.next(t, "pong") // the unbind has been handled
	sim.emit(t, 2)
	c.quiet(t, 0, 300*time.Millisecond)

	c.bind(t, 0, "sim-01", "sub", 2)
	c.next(t, "bound")
	got = append(got, c.au(t, 0), c.au(t, 0), c.au(t, 0))
	if want := "1/0* 1/0* 1/1 1/2"; frames(got) != want {
		t.Errorf("slot 0 received %s, want %s", frames(got), want)
	}
	checkContinuous(t, got)
}

func TestRebind_AfterAnIdleSlot_TimestampsFollowTheClock(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 48, 4), 12)
	sim.emit(t, 2) // the GOP cache holds frames 0 (IDR) and 1
	clock := newFakeClock()
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, testOptions(clock, nil))

	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")
	got := []rxAU{c.au(t, 0), c.au(t, 0)}
	c.send(t, `{"t":"unbind","slot":0}`)
	c.send(t, `{"t":"ping","c0":1}`)
	c.next(t, "pong") // the unbind has been handled
	sim.emit(t, 2)    // frames 2 and 3 of the camera, which the empty slot does not send
	c.quiet(t, 0, 100*time.Millisecond)

	clock.advance(t, 2*time.Second)
	c.bind(t, 0, "sim-01", "sub", 2)
	c.next(t, "bound")
	for range 4 {
		got = append(got, c.au(t, 0))
	}
	sim.emit(t, 1)
	got = append(got, c.au(t, 0))

	// Whole access units, one RTP stream, the re-bind starting at its IDR.
	if want := "1/0* 1/1 1/0* 1/1 1/2 1/3 1/4*"; frames(got) != want {
		t.Fatalf("slot 0 received %s, want %s", frames(got), want)
	}
	checkContinuous(t, got)
	// The slot's RTP clock does not stop while the slot is empty: the re-bind's
	// IDR is stamped 2 s (180000 ticks) after the first bind's, as the hub clock
	// says, not right after the last frame sent. The cached frames stay 1 ms
	// apart (R24); the first live frame, stamped at the hub clock, which has
	// not moved since the bind, follows them by 1 ms.
	checkSteps(t, got, []int64{90, 180000 - 90, 90, 90, 90, 90})
}

func TestRebind_AfterHoursIdle_SkipsAnHour(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 48, 4), 12)
	sim.emit(t, 2)
	clock := newFakeClock()
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, testOptions(clock, nil))

	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")
	got := []rxAU{c.au(t, 0), c.au(t, 0)}
	c.send(t, `{"t":"unbind","slot":0}`)
	clock.advance(t, 2*time.Hour)
	c.bind(t, 0, "sim-01", "sub", 2)
	c.next(t, "bound")
	got = append(got, c.au(t, 0))

	// The skip stops one hour (324000000 ticks) past the old track's next
	// timestamp, 1 ms after the last frame: well short of half the 32-bit
	// timestamp range (6.6 h), which a receiver reads as a step backward.
	checkSteps(t, got, []int64{90, 90 + 324000000})
}

func TestRebind_BackToBack_RTPClockKeepsToTheHubClock(t *testing.T) {
	const fps = 12
	period := time.Second / fps // 7500 ticks
	// Two cameras send a frame each period, sim-02 a third of a period after
	// sim-01. Each bind plays one GOP (12 live frames, 1 s), so a camera's
	// cached GOP holds as many frames at each of its binds: 4 and 9.
	sims := []*testSim{
		newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 48, fps), fps),
		newTestSim(t, "sim-02", 2, "sub", h264AUs("4d401f", 2, 48, fps), fps),
	}
	cached := []int{4, 9}
	offsets := []time.Duration{0, period / 3}
	for i, sim := range sims {
		sim.emit(t, cached[i])
	}
	clock := newFakeClock()
	start := clock.Now()
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sims[0], "sim-02/sub": sims[1]}, testOptions(clock, nil))

	// nextFrame is the hub time of camera i's first frame after at.
	nextFrame := func(i int, at time.Time) time.Time {
		next := start.Add(offsets[i])
		for !next.After(at) {
			next = next.Add(period)
		}
		return next
	}
	// lead is how far, in 90 kHz ticks, the RTP timestamp of an access unit is
	// ahead of the hub time at which the slot sent it. The slot's RTP clock
	// starts at the hub time of its first frame, the first bind's IDR.
	var firstTS uint32
	lead := func(au rxAU, sentAt time.Time) int64 {
		return int64(au.ts-firstTS) - int64(math.Round(sentAt.Sub(start).Seconds()*90000))
	}

	// 30 re-binds without a pause, alternating the cameras: half of them
	// direct switches (HUB-06), half an unbind and a bind at once (S4 paging).
	const binds = 30
	leads := make([]int64, binds)     // the largest lead of each bind's frames
	liveLeads := make([]int64, binds) // the smallest lead of each bind's live frames
	var all []rxAU
	for k := range binds {
		i := k % 2
		if k%4 >= 2 {
			c.send(t, `{"t":"unbind","slot":0}`)
		}
		c.bind(t, 0, sims[i].ID(), "sub", k+1)
		if m := c.next(t, "bound"); m.Epoch != int64(k+1) {
			t.Fatalf("bound = %s, want epoch %d", m.raw, k+1)
		}
		var aus []rxAU
		var sentAt []time.Time
		for range cached[i] {
			aus, sentAt = append(aus, c.au(t, 0)), append(sentAt, clock.Now())
		}
		for range fps {
			clock.advance(t, nextFrame(i, clock.Now()).Sub(clock.Now()))
			sims[i].emit(t, 1)
			aus, sentAt = append(aus, c.au(t, 0)), append(sentAt, clock.Now())
		}
		if !aus[0].idr {
			t.Fatalf("bind %d: first access unit %v, want an IDR", k+1, aus[0])
		}
		if k == 0 {
			firstTS = aus[0].ts
		}
		leads[k], liveLeads[k] = math.MinInt64, math.MaxInt64
		for j, au := range aus {
			if au.camera != i+1 {
				t.Fatalf("bind %d: access unit %v, want camera %d", k+1, au, i+1)
			}
			leads[k] = max(leads[k], lead(au, sentAt[j]))
			if j >= cached[i] {
				liveLeads[k] = min(liveLeads[k], lead(au, sentAt[j]))
			}
		}
		all = append(all, aus...)
	}
	checkContinuous(t, all)

	// After each bind, the RTP clock is at most one frame period ahead of the
	// hub clock, the burst and the live frames included, and the lead does not
	// build up from bind to bind: from the fourth bind on, it is what it was
	// two binds before, with the same camera, give or take a tick (the first
	// bind follows no frame, so it is 1 ms less). The live frames go out at the
	// hub clock, not behind it.
	wrong := func(k int, what string) {
		t.Helper()
		ms := func(ticks []int64) string {
			out := make([]string, len(ticks))
			for j, l := range ticks {
				out[j] = fmt.Sprintf("%.2f", float64(l)/90)
			}
			return strings.Join(out, " ")
		}
		t.Fatalf("bind %d: %s; RTP lead of each bind (ms): %s; of its live frames, the least (ms): %s", k+1, what, ms(leads), ms(liveLeads))
	}
	for k := range leads {
		if leads[k] > 7500+1 {
			wrong(k, "the RTP clock is more than one frame period (83.3 ms) ahead of the hub clock")
		}
		if liveLeads[k] < -1 {
			wrong(k, "the live frames lag the hub clock")
		}
	}
	for k, l := range leads[3:] { // two binds before is leads[k+1]
		if l > leads[k+1]+1 || l < leads[k+1]-1 {
			wrong(k+3, "the lead differs from two binds before")
		}
	}
}

// failingReplaceTrack returns session options in which the slots' ReplaceTrack
// fails while fail is set.
func failingReplaceTrack(clock source.Clock, fail *atomic.Bool) options {
	opts := testOptions(clock, nil)
	opts.replaceTrack = func(sender *webrtc.RTPSender, track webrtc.TrackLocal) error {
		if fail.Load() {
			return errors.New("injected ReplaceTrack failure")
		}
		return sender.ReplaceTrack(track)
	}
	return opts
}

func TestBind_FailedCodecSwitchLeavesTheSlotAsItWas(t *testing.T) {
	// Both are Baseline-or-Main H.264 that chromeLike offers: the first stream
	// goes out in the codec the slot starts with, the second needs a new track.
	old := newTestSim(t, "sim-01", 1, "sub", h264AUs("42c01e", 1, 48, 12), 12)
	next := newTestSim(t, "sim-02", 2, "sub", h264AUs("4d401f", 2, 48, 12), 12)
	old.emit(t, 1)
	next.emit(t, 1)
	var fail atomic.Bool
	clock := newFakeClock()
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s := connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": old, "sim-02/sub": next}, failingReplaceTrack(clock, &fail))

	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")
	got := []rxAU{c.au(t, 0)}

	fail.Store(true)
	c.bind(t, 0, "sim-02", "sub", 2)
	if m := c.next(t, "error"); m.raw != `{"t":"error","slot":0,"epoch":2,"code":"CODEC_UNSUPPORTED"}` {
		t.Errorf("error = %s, want CODEC_UNSUPPORTED for epoch 2", m.raw)
	}

	// The slot still carries the old camera, and only it.
	old.emit(t, 2)
	got = append(got, c.au(t, 0), c.au(t, 0))
	next.emit(t, 2)
	c.quiet(t, 0, 100*time.Millisecond)
	if want := "1/0* 1/1 1/2"; frames(got) != want {
		t.Errorf("slot 0 received %s, want %s", frames(got), want)
	}
	checkContinuous(t, got)

	waitPending(t, s, old, 2)
	if fanout.has(next, s) {
		t.Error("the session reports the source that the slot does not carry")
	}
	clock.advance(t, 500*time.Millisecond)
	if m := c.next(t, "sent"); m.Cam != 1 {
		t.Errorf("sent = %s, want only the old camera", m.raw)
	}
	s.sentMu.Lock()
	bound := len(s.bound)
	s.sentMu.Unlock()
	if bound != 1 {
		t.Errorf("the session has %d sources bound, want 1", bound)
	}

	// The slot is not stuck: the same bind works once the switch does.
	fail.Store(false)
	c.bind(t, 0, "sim-02", "sub", 3)
	if m := c.next(t, "bound"); m.Epoch != 3 {
		t.Fatalf("bound = %s, want epoch 3", m.raw)
	}
	if au := c.au(t, 0); !au.idr || au.camera != 2 {
		t.Errorf("first access unit after the switch = %v, want the IDR of camera 2", au)
	}
	waitUntil(t, "the session has released the old source", func() bool { return !fanout.has(old, s) })
}

func TestBind_FailedCodecSwitchOnAnEmptySlotBindsNothing(t *testing.T) {
	sim := newTestSim(t, "sim-02", 2, "sub", h264AUs("4d401f", 2, 48, 12), 12)
	sim.emit(t, 1)
	var fail atomic.Bool
	fail.Store(true)
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s := connect(t, testAPI(t), c, map[string]source.Source{"sim-02/sub": sim}, failingReplaceTrack(newFakeClock(), &fail))

	c.bind(t, 0, "sim-02", "sub", 1)
	if m := c.next(t, "error"); m.Epoch != 1 || m.Code != "CODEC_UNSUPPORTED" {
		t.Errorf("error = %s, want CODEC_UNSUPPORTED for epoch 1", m.raw)
	}
	c.quiet(t, 0, 100*time.Millisecond)
	if fanout.has(sim, s) {
		t.Error("the session reports a source that no slot carries")
	}

	fail.Store(false)
	c.bind(t, 0, "sim-02", "sub", 2)
	if m := c.next(t, "bound"); m.Epoch != 2 {
		t.Fatalf("bound = %s, want epoch 2", m.raw)
	}
	if au := c.au(t, 0); !au.idr || au.camera != 2 {
		t.Errorf("first access unit = %v, want the IDR of camera 2", au)
	}
}

func TestRebind_FailedTimestampSkipKeepsTheOldTrack(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 48, 4), 12)
	sim.emit(t, 2)
	var fail atomic.Bool
	clock := newFakeClock()
	var logs syncBuffer
	opts := failingReplaceTrack(clock, &fail)
	opts.log = slog.New(slog.NewTextHandler(&logs, nil))
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, opts)

	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")
	got := []rxAU{c.au(t, 0), c.au(t, 0)}
	c.send(t, `{"t":"unbind","slot":0}`)
	clock.advance(t, 2*time.Second)

	// The re-bind keeps the codec; it needs a new track only to move the
	// timestamps to the hub clock, and that fails. It is not a codec problem,
	// so the bind goes ahead on the old track.
	fail.Store(true)
	c.bind(t, 0, "sim-01", "sub", 2)
	for m := waitFor(t, c.msgs); m.T != "bound"; m = waitFor(t, c.msgs) {
		if m.T == "error" {
			t.Fatalf("got %s, want the bind to go ahead without the timestamp skip", m.raw)
		}
	}
	got = append(got, c.au(t, 0), c.au(t, 0))
	if want := "1/0* 1/1 1/0* 1/1"; frames(got) != want {
		t.Fatalf("slot 0 received %s, want %s", frames(got), want)
	}
	checkContinuous(t, got)
	// The old track goes on 1 ms after its last frame, without the 2 s skip.
	checkSteps(t, got, []int64{90, 90, 90})
	if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "injected ReplaceTrack failure") {
		t.Errorf("log = %q, want a warning with the ReplaceTrack error", out)
	}
}
