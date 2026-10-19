package session

import (
	"bytes"
	"regexp"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/webrtc/v4"
)

func TestAnswer_CreatesSlotPerRecvonlyLine(t *testing.T) {
	c := newClient(t, clientConfig{slots: 5, offer: chromeLike})
	s := connect(t, testAPI(t), c, nil, testOptions(source.SystemClock{}, nil))

	if got := len(s.slots); got != 5 {
		t.Fatalf("slots = %d, want 5", got)
	}
	answerSDP := c.pc.RemoteDescription().SDP
	var video, sendonly int
	for _, section := range strings.Split(answerSDP, "\r\nm=")[1:] {
		if strings.HasPrefix(section, "video ") {
			video++
			if strings.Contains(section, "\r\na=sendonly") && strings.Contains(section, "\r\na=ssrc:") {
				sendonly++
			}
		}
	}
	if video != 5 || sendonly != 5 {
		t.Errorf("answer has %d video m-lines, %d of them sendonly with an SSRC; want 5 and 5", video, sendonly)
	}
	if !strings.Contains(answerSDP, "\r\nm=application ") {
		t.Error("answer has no application m-line for the rpc channel")
	}
}

func TestAnswer_RejectsAnOfferWithoutAVideoCodecTheHubCanSend(t *testing.T) {
	c := newClient(t, clientConfig{slots: 2})
	if _, _, err := answer(testAPI(t), c.offer, nil, testOptions(source.SystemClock{}, nil)); err == nil {
		t.Fatal("answer accepted an offer with VP8 only")
	}
}

func TestBind_UnknownCameraAndBadSlotReturnErrors(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 24, 12), 12)
	c := newClient(t, clientConfig{slots: 2, offer: chromeLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, testOptions(source.SystemClock{}, nil))

	c.bind(t, 0, "sim-02", "sub", 3)
	if m := c.next(t, "error"); m.Slot != 0 || m.Epoch != 3 || m.Code != "UNKNOWN_CAMERA" {
		t.Errorf("unknown camera: got %s", m.raw)
	}
	c.bind(t, 0, "sim-01", "main", 4)
	if m := c.next(t, "error"); m.Slot != 0 || m.Epoch != 4 || m.Code != "UNKNOWN_CAMERA" {
		t.Errorf("unknown profile: got %s", m.raw)
	}
	c.bind(t, 2, "sim-01", "sub", 5)
	if m := c.next(t, "error"); m.Slot != 2 || m.Epoch != 5 || m.Code != "BAD_SLOT" {
		t.Errorf("slot beyond the offer: got %s", m.raw)
	}
	c.bind(t, -1, "sim-01", "sub", 6)
	if m := c.next(t, "error"); m.Slot != -1 || m.Epoch != 6 || m.Code != "BAD_SLOT" {
		t.Errorf("negative slot: got %s", m.raw)
	}
}

func TestPingPong_EchoesClientTime(t *testing.T) {
	clock := newFakeClock()
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	connect(t, testAPI(t), c, nil, testOptions(clock, nil))

	c.send(t, `{"t":"ping","c0":123.4}`)
	m := c.next(t, "pong")
	want := `{"t":"pong","c0":123.4,"h":1792324800000}`
	if m.raw != want {
		t.Errorf("pong = %s, want %s", m.raw, want)
	}

	clock.advance(t, 1500*time.Microsecond)
	c.send(t, `{"t":"ping","c0":98765.4321}`)
	if m := c.next(t, "pong"); m.C0 != 98765.4321 || m.H != 1792324800001.5 {
		t.Errorf("pong = %s, want c0 98765.4321 and h 1792324800001.5", m.raw)
	}
}

func TestRPC_IgnoresMalformedAndUnknownMessages(t *testing.T) {
	var logs syncBuffer
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	connect(t, testAPI(t), c, nil, testOptions(source.SystemClock{}, &logs))

	c.send(t, `not json`)
	c.send(t, `{"t":"reboot"}`)
	c.send(t, `{"t":"bind","slot":"one","cam":"sim-01","profile":"sub","epoch":1}`)
	c.send(t, `{"t":"ping","c0":5}`)
	for m := waitFor(t, c.msgs); m.T != "pong"; m = waitFor(t, c.msgs) {
		if m.T != "pair" {
			t.Fatalf("got %s, want no answer to a malformed or unknown message", m.raw)
		}
	}
	if n := strings.Count(logs.String(), "level=WARN"); n != 3 {
		t.Errorf("log has %d warnings, want 3:\n%s", n, logs.String())
	}
}

// ipLiteral matches an IPv4 address or the start of an IPv6 address.
var ipLiteral = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}|[0-9a-fA-F]{0,4}:[0-9a-fA-F]{0,4}:`)

func TestSelectedPair_TypesOnly(t *testing.T) {
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s := connect(t, testAPI(t), c, nil, testOptions(source.SystemClock{}, nil))

	m := c.next(t, "pair")
	types := map[string]bool{"host": true, "srflx": true, "prflx": true, "relay": true}
	if !types[m.Local] || !types[m.Remote] {
		t.Errorf("pair = %s, want candidate types", m.raw)
	}
	if ipLiteral.MatchString(m.raw) {
		t.Errorf("pair message %s contains an IP address", m.raw)
	}
	if want := `{"t":"pair","local":"` + m.Local + `","remote":"` + m.Remote + `"}`; m.raw != want {
		t.Errorf("pair = %s, want %s", m.raw, want)
	}
	local, remote, ok := s.SelectedPair()
	if !ok || local != m.Local || remote != m.Remote {
		t.Errorf("SelectedPair() = %q, %q, %v; want %q, %q, true", local, remote, ok, m.Local, m.Remote)
	}
}

func TestSession_ClosesWhenTheClientCloses(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 24, 12), 12)
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s := connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, testOptions(source.SystemClock{}, nil))
	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")

	if err := c.pc.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("the session did not close after the client closed")
	}
	waitUntil(t, "the session leaves the sent fan-out", func() bool { return !fanout.has(sim, s) })
	waitUntil(t, "the hub's peer connection is closed", func() bool {
		return s.pc.ConnectionState() == webrtc.PeerConnectionStateClosed
	})
}

func TestClose_IsIdempotentAndReleasesSources(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 24, 12), 12)
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s := connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, testOptions(source.SystemClock{}, nil))
	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")
	if !fanout.has(sim, s) {
		t.Fatal("a bound session is not in the sent fan-out")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if fanout.has(sim, s) {
		t.Error("a closed session is still in the sent fan-out")
	}
}

func TestDone_ClosesWhenTheSessionCloses(t *testing.T) {
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s := connect(t, testAPI(t), c, nil, testOptions(source.SystemClock{}, nil))

	select {
	case <-s.Done():
		t.Fatal("Done is closed on a session that is open")
	default:
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("Done is not closed after Close")
	}
}

// discardSink is a sent-record sink that drops what it gets.
type discardSink struct{}

func (discardSink) addSent(source.Source, source.SentRecord) {}

// steadyGoroutines returns the number of goroutines once it has not changed
// for 100 ms, or after 5 s.
func steadyGoroutines() int {
	deadline := time.Now().Add(5 * time.Second)
	n := runtime.NumGoroutine()
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		m := runtime.NumGoroutine()
		if m == n {
			break
		}
		n = m
	}
	return n
}

// settledGoroutines waits until no more than want goroutines run, for up to
// 10 s, and returns the count it last saw.
func settledGoroutines(want int) int {
	deadline := time.Now().Add(10 * time.Second)
	n := runtime.NumGoroutine()
	for n > want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		n = runtime.NumGoroutine()
	}
	return n
}

// S5 on the phone (R44): the app closes its session when it goes to the
// background and opens a new one when it returns, at once. The new session
// binds the same camera and gets its frames, the old one releases what it had
// within a second, and the two leave no goroutine behind: no slot writer, no
// RTCP reader, no fan-out subscription.
func TestSession_AViewerThatReconnectsAtOnceGetsTheSameCameraAndLeavesNothingBehind(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 24, 12), 12)
	sources := map[string]source.Source{"sim-01/sub": sim}
	api := testAPI(t)
	// The fan-out reads a simulator's sent log for as long as the simulator
	// runs, which is the life of the hub (R22, R28): start that reader first.
	fanout.subscribe(sim, discardSink{})
	fanout.unsubscribe(sim, discardSink{})
	before := steadyGoroutines()

	first := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s1 := connect(t, api, first, sources, testOptions(source.SystemClock{}, nil))
	first.bind(t, 0, "sim-01", "sub", 1)
	first.next(t, "bound")
	sim.emit(t, 1)
	if au := first.au(t, 0); au.camera != 1 || !au.idr {
		t.Fatalf("first session got %v, want the keyframe of camera 1", au)
	}

	if err := first.pc.Close(); err != nil {
		t.Fatal(err)
	}
	left := time.Now()
	released := make(chan time.Duration, 1) // how long after the viewer left
	go func() {
		for fanout.has(sim, s1) && time.Since(left) < 10*time.Second {
			time.Sleep(time.Millisecond)
		}
		released <- time.Since(left)
	}()
	second := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s2 := connect(t, api, second, sources, testOptions(source.SystemClock{}, nil))
	second.bind(t, 0, "sim-01", "sub", 1)
	second.next(t, "bound")
	// The cached GOP comes first: the keyframe of the first session's frame.
	if au := second.au(t, 0); au.camera != 1 || au.frame != 0 || !au.idr {
		t.Fatalf("second session got %v first, want the cached keyframe of camera 1", au)
	}
	sim.emit(t, 2)
	for want := 1; want <= 2; want++ {
		if au := second.au(t, 0); au.camera != 1 || au.frame != want {
			t.Fatalf("second session got %v, want frame %d of camera 1", au, want)
		}
	}

	select {
	case <-s1.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the first session did not close after its viewer left")
	}
	took := <-released
	if fanout.has(sim, s1) {
		t.Fatal("the first session is still in the sent fan-out 10 s after its viewer left")
	}
	if took > time.Second {
		t.Errorf("the first session released its camera %v after its viewer left, want within 1 s", took)
	}
	if !fanout.has(sim, s2) {
		t.Error("the second session is not in the sent fan-out")
	}

	if err := second.pc.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s2.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the second session did not close after its viewer left")
	}
	if err := s2.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}
	if after := settledGoroutines(before); after > before {
		var dump bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&dump, 1)
		t.Errorf("%d goroutines after both sessions closed, %d before:\n%s", after, before, dump.String())
	}
}
