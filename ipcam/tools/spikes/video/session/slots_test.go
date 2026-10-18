package session

import (
	"errors"
	"fmt"
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
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, testOptions(source.SystemClock{}, nil))

	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")
	var got []rxAU
	for range 4 {
		got = append(got, c.au(t, 0))
	}
	sim.emit(t, 2)
	got = append(got, c.au(t, 0), c.au(t, 0))
	if want := "1/0* 1/1 1/2 1/3 1/4 1/5"; frames(got) != want {
		t.Fatalf("slot 0 received %s, want %s", frames(got), want)
	}

	// The cached frames are 1 ms (90 ticks) apart, so that the decoder catches
	// up without adding their age to the latency; live frames are one frame
	// period (7500 ticks at 12 fps) apart.
	checkSteps(t, got, []int64{90, 90, 90, 7500, 7500})
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
	// Each camera's cached GOP is compressed; the new camera's IDR follows the
	// old camera's last frame by one frame period.
	checkSteps(t, got, []int64{90, 7500, 7500, 90, 7500, 7500, 7500})
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
	h265.emit(t, 1)
	got = append(got, c.au(t, 0))

	c.bind(t, 0, "sim-01", "sub", 3)
	c.next(t, "bound")
	got = append(got, c.au(t, 0))
	h264.emit(t, 1)
	got = append(got, c.au(t, 0))

	if want := "1/0* 17/0* 17/1 17/2 1/0* 1/1"; frames(got) != want {
		t.Fatalf("slot 0 received %s, want %s", frames(got), want)
	}
	wantMimes := []string{webrtc.MimeTypeH264, webrtc.MimeTypeH265, webrtc.MimeTypeH265, webrtc.MimeTypeH265, webrtc.MimeTypeH264, webrtc.MimeTypeH264}
	for i, au := range got {
		if !strings.EqualFold(au.mime, wantMimes[i]) {
			t.Errorf("access unit %d (%v) came as %s, want %s", i, au, au.mime, wantMimes[i])
		}
	}
	checkContinuous(t, got)
	// A new track continues the timestamps: 12 fps H.264 frames are 7500
	// ticks long, 20 fps H.265 frames 4500, a cached frame 90.
	checkSteps(t, got, []int64{7500, 90, 4500, 4500, 7500})
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
	// says, not one frame period after the last frame sent. The cached frames
	// stay 1 ms apart (R24).
	checkSteps(t, got, []int64{90, 180000 - 90, 90, 90, 90, 7500})
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

	// The skip stops at one hour (324000000 ticks), well short of half the
	// 32-bit timestamp range (6.6 h), which a receiver reads as a step backward.
	checkSteps(t, got, []int64{90, 7500 + 324000000})
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
