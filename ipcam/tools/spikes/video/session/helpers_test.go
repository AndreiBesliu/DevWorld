package session

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/ice/v4"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

// waitFor receives one value from ch, failing the test when none arrives.
func waitFor[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("channel closed while waiting for a value")
		}
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a value")
		panic("unreachable")
	}
}

// waitClosed waits until ch is closed, failing the test after 10 s.
func waitClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a channel to close")
	}
}

// waitUntil polls cond until it holds, failing the test after 10 s.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// fakeClock is a Clock whose tickers fire only when the test advances it.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTicker(d time.Duration) source.Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	tk := &fakeTicker{clock: c, period: d, ch: make(chan time.Time)}
	c.tickers = append(c.tickers, tk)
	return tk
}

// advance moves the clock by d and fires every ticker whose period is d. It
// returns when each of them has taken its tick; the receiver's work may still
// be running.
func (c *fakeClock) advance(t *testing.T, d time.Duration) {
	t.Helper()
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	var due []*fakeTicker
	for _, tk := range c.tickers {
		if tk.period == d {
			due = append(due, tk)
		}
	}
	c.mu.Unlock()
	for _, tk := range due {
		select {
		case tk.ch <- now:
		case <-time.After(10 * time.Second):
			t.Fatalf("nothing received the tick of the %v ticker", d)
		}
	}
}

type fakeTicker struct {
	clock  *fakeClock
	period time.Duration
	ch     chan time.Time
}

func (tk *fakeTicker) C() <-chan time.Time { return tk.ch }

// Stop removes the ticker from its clock. Like time.Ticker, it does not close
// the channel.
func (tk *fakeTicker) Stop() {
	tk.clock.mu.Lock()
	defer tk.clock.mu.Unlock()
	for i, other := range tk.clock.tickers {
		if other == tk {
			tk.clock.tickers = append(tk.clock.tickers[:i], tk.clock.tickers[i+1:]...)
			return
		}
	}
}

// testSim is a simulated camera that sends a frame only when the test says so.
type testSim struct {
	source.Source
	clock  *fakeClock
	period time.Duration
	probe  <-chan source.AccessUnit
}

func newTestSim(t *testing.T, id string, camera uint8, stream string, aus []source.AccessUnit, fps int) *testSim {
	t.Helper()
	clock := newFakeClock()
	src := source.NewSim(id, camera, aus, fps, 0, clock, source.WithStreamProfile(stream))
	_, probe, cancel := src.Subscribe()
	t.Cleanup(cancel)
	return &testSim{Source: src, clock: clock, period: time.Second / time.Duration(fps), probe: probe}
}

// emit sends the next n frames and waits until the simulator has handed each
// of them to its subscribers.
func (s *testSim) emit(t *testing.T, n int) {
	t.Helper()
	for range n {
		s.clock.advance(t, s.period)
		waitFor(t, s.probe)
	}
}

// emitLate sends the next frame late by d, which the simulator's jitter shows.
func (s *testSim) emitLate(t *testing.T, d time.Duration) {
	t.Helper()
	s.clock.mu.Lock()
	s.clock.now = s.clock.now.Add(d)
	s.clock.mu.Unlock()
	s.emit(t, 1)
}

// sentAt is the hub time, in Unix ms, at which the simulator sent its n-th
// frame (counting from 1), when every frame was on time.
func (s *testSim) sentAt(n int) float64 {
	at := newFakeClock().now.Add(time.Duration(n) * s.period)
	return float64(at.UnixMicro()) / 1000
}

// The synthetic streams below carry, in the first slice of every frame, the
// camera index and the frame index as bytes with the top bit set, so that no
// two zero bytes (part of an Annex B start code) appear in a NAL unit.

func frameTag(camera byte, frame int) []byte {
	return []byte{0x80 | camera, 0x80 | byte(frame>>7&0x7f), 0x80 | byte(frame&0x7f), 0xff}
}

// h264AUs returns n H.264 access units whose SPS carries the given
// profile-level-id, with an IDR every gop frames.
func h264AUs(profileLevelID string, camera byte, n, gop int) []source.AccessUnit {
	plid, err := hex.DecodeString(profileLevelID)
	if err != nil || len(plid) != 3 {
		panic("bad profile-level-id " + profileLevelID)
	}
	sps := append(append([]byte{0x67}, plid...), 0xac, 0xd9)
	pps := []byte{0x68, 0xee, 0x3c, 0x80}
	aus := make([]source.AccessUnit, n)
	for i := range aus {
		au := source.AccessUnit{Frame: uint16(i)}
		if i%gop == 0 {
			au.IDR = true
			au.NALUs = [][]byte{sps, pps, append([]byte{0x65, 0x88}, frameTag(camera, i)...)}
		} else {
			au.NALUs = [][]byte{append([]byte{0x41, 0x9a}, frameTag(camera, i)...)}
		}
		aus[i] = au
	}
	return aus
}

// h265AUs returns n H.265 Main@4.1 access units (Profile "017b") with an IDR
// every gop frames.
func h265AUs(camera byte, n, gop int) []source.AccessUnit {
	vps := []byte{0x40, 0x01, 0x0c, 0x01, 0xff, 0xff, 0x01, 0x60}
	sps := []byte{0x42, 0x01, 0x01, 0x01, 0x60, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x7b, 0xa0}
	pps := []byte{0x44, 0x01, 0xc1, 0x72, 0xb4}
	aus := make([]source.AccessUnit, n)
	for i := range aus {
		au := source.AccessUnit{Frame: uint16(i)}
		if i%gop == 0 {
			au.IDR = true // IDR_W_RADL
			au.NALUs = [][]byte{vps, sps, pps, append([]byte{0x26, 0x01, 0xaf}, frameTag(camera, i)...)}
		} else { // TRAIL_R
			au.NALUs = [][]byte{append([]byte{0x02, 0x01, 0xaf}, frameTag(camera, i)...)}
		}
		aus[i] = au
	}
	return aus
}

// rxAU is an access unit that the test client received and depacketised.
type rxAU struct {
	mime              string
	pt                uint8
	firstSeq, lastSeq uint16
	ts                uint32
	idr               bool
	camera, frame     int
}

func (a rxAU) String() string {
	return fmt.Sprintf("cam %d frame %d idr %v pt %d seq %d-%d ts %d", a.camera, a.frame, a.idr, a.pt, a.firstSeq, a.lastSeq, a.ts)
}

// parseAU finds the first slice of an access unit in Annex B form and reads
// its frame tag.
func parseAU(mime string, annexB []byte) (rxAU, error) {
	au := rxAU{mime: mime}
	for _, nalu := range bytes.Split(annexB, []byte{0, 0, 0, 1}) {
		if len(nalu) == 0 {
			continue
		}
		var tag []byte
		if strings.EqualFold(mime, webrtc.MimeTypeH265) {
			typ := nalu[0] >> 1 & 0x3f
			if typ > 31 || len(nalu) < 6 {
				continue
			}
			au.idr = typ >= 19 && typ <= 21
			tag = nalu[3:]
		} else {
			typ := nalu[0] & 0x1f
			if (typ != 1 && typ != 5) || len(nalu) < 5 {
				continue
			}
			au.idr = typ == 5
			tag = nalu[2:]
		}
		au.camera = int(tag[0] & 0x7f)
		au.frame = int(tag[1]&0x7f)<<7 | int(tag[2]&0x7f)
		return au, nil
	}
	return au, fmt.Errorf("no slice in %x", annexB)
}

// rxMsg is a message the test client received on the rpc channel.
type rxMsg struct {
	raw     string
	T       string       `json:"t"`
	Slot    int          `json:"slot"`
	Epoch   int64        `json:"epoch"`
	Codec   string       `json:"codec"`
	Profile string       `json:"profile"`
	Code    string       `json:"code"`
	C0      float64      `json:"c0"`
	H       float64      `json:"h"`
	Cam     int          `json:"cam"`
	P       string       `json:"p"`
	R       [][3]float64 `json:"r"`
	Local   string       `json:"local"`
	Remote  string       `json:"remote"`
	MS      float64      `json:"ms"`
}

// codecOffer says what the test client offers: the H.264 profile-level-ids
// (each in packetization modes 1 and 0, like libwebrtc) and whether H.265.
type codecOffer struct {
	h264 []string
	h265 bool
}

// chromeLike is what desktop Chrome offers on a machine that decodes H.265.
var chromeLike = codecOffer{h264: []string{"42001f", "42e01f", "4d001f", "f4001f", "64001f", "640c1f"}, h265: true}

// testClient is an in-process Pion peer that plays the spike app: it offers
// recvonly video transceivers and the rpc data channel, depacketises what each
// transceiver receives into access units, and collects rpc messages.
type testClient struct {
	pc    *webrtc.PeerConnection
	dc    *webrtc.DataChannel
	offer string
	open  chan struct{}
	msgs  chan rxMsg
	aus   []chan rxAU
}

type clientConfig struct {
	slots int
	offer codecOffer
	// onlyIP, when set, is the one local address the client gathers on.
	onlyIP net.IP
}

func newClient(t *testing.T, cfg clientConfig) *testClient {
	t.Helper()
	me := &webrtc.MediaEngine{}
	pt := webrtc.PayloadType(96)
	register := func(mime, fmtp string) {
		t.Helper()
		feedback := []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}}
		if err := me.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: mime, ClockRate: 90000, SDPFmtpLine: fmtp, RTCPFeedback: feedback},
			PayloadType:        pt,
		}, webrtc.RTPCodecTypeVideo); err != nil {
			t.Fatal(err)
		}
		if err := me.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeRTX, ClockRate: 90000, SDPFmtpLine: fmt.Sprintf("apt=%d", pt)},
			PayloadType:        pt + 1,
		}, webrtc.RTPCodecTypeVideo); err != nil {
			t.Fatal(err)
		}
		pt += 2
	}
	register(webrtc.MimeTypeVP8, "")
	for _, plid := range cfg.offer.h264 {
		register(webrtc.MimeTypeH264, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id="+plid)
		register(webrtc.MimeTypeH264, "level-asymmetry-allowed=1;packetization-mode=0;profile-level-id="+plid)
	}
	if cfg.offer.h265 {
		register(webrtc.MimeTypeH265, "level-id=93;profile-id=1;tier-flag=0;tx-mode=SRST")
	}

	var se webrtc.SettingEngine
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	if cfg.onlyIP != nil {
		se.SetIPFilter(func(ip net.IP) bool { return ip.Equal(cfg.onlyIP) })
	}
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })

	c := &testClient{pc: pc, open: make(chan struct{}), msgs: make(chan rxMsg, 1024)}
	for range cfg.slots {
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			t.Fatal(err)
		}
		c.aus = append(c.aus, make(chan rxAU, 1024))
	}
	pc.OnTrack(c.onTrack)

	if c.dc, err = pc.CreateDataChannel("rpc", nil); err != nil {
		t.Fatal(err)
	}
	c.dc.OnOpen(func() { close(c.open) })
	c.dc.OnMessage(func(m webrtc.DataChannelMessage) {
		msg := rxMsg{raw: string(m.Data)}
		if err := json.Unmarshal(m.Data, &msg); err != nil {
			msg.T = "malformed: " + err.Error()
		}
		c.msgs <- msg
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	c.offer = pc.LocalDescription().SDP
	return c
}

// onTrack depacketises the RTP of one transceiver into access units.
func (c *testClient) onTrack(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	slot := -1
	for i, tr := range c.pc.GetTransceivers() {
		if tr.Receiver() == receiver {
			slot = i
		}
	}
	if slot < 0 || slot >= len(c.aus) {
		return
	}
	mimes := map[uint8]string{}
	for _, codec := range receiver.GetParameters().Codecs {
		mimes[uint8(codec.PayloadType)] = codec.MimeType
	}
	go func() {
		var (
			h264    codecs.H264Packet
			h265    codecs.H265Depacketizer
			annexB  []byte
			current rxAU
			started bool
		)
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			mime := mimes[pkt.PayloadType]
			if !started {
				current = rxAU{mime: mime, pt: pkt.PayloadType, firstSeq: pkt.SequenceNumber, ts: pkt.Timestamp}
				started = true
			}
			var out []byte
			if strings.EqualFold(mime, webrtc.MimeTypeH265) {
				out, err = h265.Unmarshal(pkt.Payload)
			} else {
				out, err = h264.Unmarshal(pkt.Payload)
			}
			if err != nil {
				continue
			}
			annexB = append(annexB, out...)
			if !pkt.Marker {
				continue
			}
			au, err := parseAU(mime, annexB)
			if err == nil {
				au.pt, au.firstSeq, au.lastSeq, au.ts = current.pt, current.firstSeq, pkt.SequenceNumber, current.ts
				c.aus[slot] <- au
			}
			annexB, started = nil, false
		}
	}()
}

// send writes one rpc message.
func (c *testClient) send(t *testing.T, msg string) {
	t.Helper()
	if err := c.dc.SendText(msg); err != nil {
		t.Fatal(err)
	}
}

func (c *testClient) bind(t *testing.T, slot int, cam, profile string, epoch int) {
	t.Helper()
	c.send(t, fmt.Sprintf(`{"t":"bind","slot":%d,"cam":%q,"profile":%q,"epoch":%d}`, slot, cam, profile, epoch))
}

// next returns the next rpc message of the given type, skipping any other.
func (c *testClient) next(t *testing.T, typ string) rxMsg {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m := <-c.msgs:
			if strings.HasPrefix(m.T, "malformed") {
				t.Fatalf("%s: %s", m.T, m.raw)
			}
			if m.T == typ {
				return m
			}
		case <-deadline:
			t.Fatalf("no %q message within 10 s", typ)
		}
	}
}

// au returns the next access unit that slot received.
func (c *testClient) au(t *testing.T, slot int) rxAU {
	t.Helper()
	return waitFor(t, c.aus[slot])
}

// quiet fails the test when slot receives an access unit within d.
func (c *testClient) quiet(t *testing.T, slot int, d time.Duration) {
	t.Helper()
	select {
	case au := <-c.aus[slot]:
		t.Fatalf("slot %d received %v, expected nothing", slot, au)
	case <-time.After(d):
	}
}

// testAPI returns the hub's API without mDNS.
func testAPI(t *testing.T) *webrtc.API {
	t.Helper()
	api, mdnsUnavailable, err := NewAPI(false)
	if err != nil {
		t.Fatal(err)
	}
	if mdnsUnavailable {
		t.Fatal("NewAPI(false) reported mDNS as unavailable")
	}
	return api
}

// testOptions returns session options with the given clock and a logger that
// writes to w (io.Discard when nil).
func testOptions(clock source.Clock, w io.Writer) options {
	if w == nil {
		w = io.Discard
	}
	return options{clock: clock, log: slog.New(slog.NewTextHandler(w, nil))}
}

// connect answers the client's offer and waits until the rpc channel is open.
func connect(t *testing.T, api *webrtc.API, c *testClient, sources map[string]source.Source, opts options) *Session {
	t.Helper()
	s, answerSDP, err := answer(api, c.offer, sources, opts)
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answerSDP}); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, c.open)
	return s
}

// checkSteps fails the test unless the RTP timestamp steps between the
// access units are the given numbers of 90 kHz ticks, give or take one tick of
// rounding.
func checkSteps(t *testing.T, aus []rxAU, want []int64) {
	t.Helper()
	if len(aus) != len(want)+1 {
		t.Fatalf("%d access units for %d steps", len(aus), len(want))
	}
	for i, w := range want {
		if d := int64(aus[i+1].ts - aus[i].ts); d < w-1 || d > w+1 {
			t.Errorf("timestamp step %d (%v → %v) = %d ticks, want %d", i, aus[i], aus[i+1], d, w)
		}
	}
}

// checkContinuous fails the test unless the access units form one RTP stream:
// sequence numbers without a gap and timestamps that increase.
func checkContinuous(t *testing.T, aus []rxAU) {
	t.Helper()
	for i := 1; i < len(aus); i++ {
		prev, cur := aus[i-1], aus[i]
		if cur.firstSeq != prev.lastSeq+1 {
			t.Errorf("sequence gap between %v and %v", prev, cur)
		}
		if step := cur.ts - prev.ts; step == 0 || step >= 1<<31 {
			t.Errorf("timestamp does not increase between %v and %v", prev, cur)
		}
	}
}
