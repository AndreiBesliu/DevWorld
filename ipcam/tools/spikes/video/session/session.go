// Package session is the WebRTC side of the Phase 0 (b) spike hub: one Pion
// PeerConnection per viewer (A7, spec §6.3). The viewer offers N recvonly
// video transceivers, the slots, and a data channel called "rpc"; over that
// channel it binds cameras to slots, and the hub reports what it sent so that
// the viewer can measure glass-to-glass latency.
//
// The rpc channel carries one JSON object per message (see proto.go).
// Client to hub:
//
//	{"t":"bind","slot":3,"cam":"sim-04","profile":"sub","epoch":7}
//	{"t":"unbind","slot":3}
//	{"t":"ping","c0":123.4}
//
// Hub to client:
//
//	{"t":"bound","slot":3,"epoch":7,"codec":"H264","profile":"4d401f"}
//	{"t":"error","slot":3,"epoch":7,"code":"CODEC_UNSUPPORTED"}
//	{"t":"pong","c0":123.4,"h":1729260000123.5}
//	{"t":"sent","cam":4,"p":"sub","r":[[frame,loop,hubMs],...]}
//	{"t":"pair","local":"host","remote":"prflx"}
//	{"t":"jitter","ms":3.1}
//
// Slots are numbered from 0 in the order of the offer's recvonly video
// m-lines. A bind names the source "<cam>/<profile>" (R2); "bound" carries the
// source's codec and codec profile, and its epoch is the bind's. "sent" comes
// every 500 ms for each bound simulator, "jitter" every 10 s while anything is
// bound, and "pair" whenever ICE selects a pair.
//
// H.264 goes out in the offered profile-level-id whose profile_idc is the
// stream's, at the highest offered level (R20). When the offer has none, a
// "main" bind fails with CODEC_UNSUPPORTED, as does an H.265 stream that the
// offer lacks (APP-05: the client falls back to "sub"). A "sub" bind instead
// goes out in the closest offered H.264 profile (R23, spec §6.3 step 7): on
// the ladder Baseline, Main, High, High 10, High 4:2:2, High 4:4:4, the
// nearest richer one, whose decoder decodes the stream, else the nearest
// poorer one, at its highest offered level. Then "bound" names that
// profile-level-id in "fallback", which is absent otherwise:
//
//	{"t":"bound","slot":0,"epoch":8,"codec":"H264","profile":"4d401f","fallback":"42e01f"}
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

// gatherTimeout bounds ICE gathering, which on a LAN without STUN servers only
// lists the host's own addresses.
const gatherTimeout = 10 * time.Second

// mdnsAddr is the mDNS group and port. Pion's ICE agent listens on it to
// resolve the .local candidates of browsers; Go sets SO_REUSEADDR on a socket
// bound to a multicast address, so it shares the port with avahi or the
// Windows DNS client (G7).
var mdnsAddr = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// NewAPI returns the hub's WebRTC API. It sends H.264 (every profile-level-id
// that WebRTC clients offer, in packetization mode 1) and H.265, and gathers
// IPv4 candidates only: the spike measures LAN pairs on private IPv4 addresses
// (MV13), and Pion's ICE agent turns mDNS off on a host without IPv6 unless it
// is limited to IPv4.
//
// With mdns, the hub resolves the .local host candidates of browsers
// (MulticastDNSModeQueryOnly). When the mDNS port cannot be opened, the API
// works without mDNS and mdnsUnavailable is true, for the banner and the
// results to report.
func NewAPI(mdns bool) (api *webrtc.API, mdnsUnavailable bool, err error) {
	me := &webrtc.MediaEngine{}
	if err := registerCodecs(me); err != nil {
		return nil, false, err
	}
	var se webrtc.SettingEngine
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	if mdns {
		if err := probeMDNS(); err != nil {
			mdnsUnavailable = true
			slog.Warn("mDNS is unavailable, so the hub cannot resolve the .local candidates of browsers",
				"port", mdnsAddr.Port, "err", err)
		} else {
			se.SetICEMulticastDNSMode(ice.MulticastDNSModeQueryOnly)
		}
	}
	// Without an interceptor registry, Pion registers its default
	// interceptors: NACK responses, RTCP reports and TWCC.
	return webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se)), mdnsUnavailable, nil
}

// probeMDNS opens and closes the socket that Pion's ICE agent opens for mDNS.
func probeMDNS() error {
	conn, err := net.ListenUDP("udp4", mdnsAddr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// Session is one viewer's PeerConnection with its slots and rpc channel. It
// closes itself when the connection fails or the viewer closes it.
type Session struct {
	pc      *webrtc.PeerConnection
	sources map[string]source.Source
	slots   []*slot
	clock   source.Clock
	log     *slog.Logger

	sendMu   sync.Mutex
	rpc      *webrtc.DataChannel // the rpc channel, once the viewer opened it
	pair     *pairMsg            // the selected candidate pair, once ICE has one
	pairSent bool

	rpcMu      sync.Mutex
	slotSource []source.Source // per slot, the source of the last accepted bind

	sentMu sync.Mutex
	bound  map[source.Source]*boundSource
	closed bool

	done      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

// options are the parts of a session that tests replace.
type options struct {
	clock source.Clock
	log   *slog.Logger
}

// Answer answers a viewer's offer with a new session. Every recvonly video
// m-line of the offer becomes a slot; sources maps "<cam>/<profile>" (for
// example "sim-04/sub") to the source a bind may name. The answer holds all
// ICE candidates, so no trickle is needed.
func Answer(api *webrtc.API, offerSDP string, sources map[string]source.Source) (*Session, string, error) {
	return answer(api, offerSDP, sources, options{clock: source.SystemClock{}, log: slog.Default()})
}

func answer(api *webrtc.API, offerSDP string, sources map[string]source.Source, opts options) (*Session, string, error) {
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, "", fmt.Errorf("create peer connection: %w", err)
	}
	s := &Session{
		pc:      pc,
		sources: sources,
		clock:   opts.clock,
		log:     opts.log,
		bound:   map[source.Source]*boundSource{},
		done:    make(chan struct{}),
	}
	answerSDP, err := s.negotiate(offerSDP)
	if err != nil {
		return nil, "", errors.Join(err, pc.Close())
	}
	s.start()
	return s, answerSDP, nil
}

// negotiate applies the offer, creates a slot per recvonly video m-line and
// returns the answer once ICE gathering is complete.
func (s *Session) negotiate(offerSDP string) (string, error) {
	s.pc.OnDataChannel(s.onDataChannel)
	s.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			go func() { _ = s.Close() }()
		}
	})
	s.pc.SCTP().Transport().ICETransport().OnSelectedCandidatePairChange(s.onSelectedPair)

	if err := s.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}); err != nil {
		return "", fmt.Errorf("apply the offer: %w", err)
	}
	for _, tr := range s.pc.GetTransceivers() {
		// Pion answers a recvonly m-line with a sendonly transceiver.
		if tr.Kind() != webrtc.RTPCodecTypeVideo || tr.Direction() != webrtc.RTPTransceiverDirectionSendonly {
			continue
		}
		sl, err := newSlot(len(s.slots), tr.Receiver().GetParameters().Codecs, s.notify, s.log)
		if err != nil {
			return "", err
		}
		if sl.sender, err = s.pc.AddTrack(sl.track); err != nil {
			return "", fmt.Errorf("slot %d: add track: %w", sl.index, err)
		}
		if tr.Sender() != sl.sender {
			return "", fmt.Errorf("slot %d: the track went to another m-line", sl.index)
		}
		s.slots = append(s.slots, sl)
	}
	s.slotSource = make([]source.Source, len(s.slots))

	answer, err := s.pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("create the answer: %w", err)
	}
	gathered := webrtc.GatheringCompletePromise(s.pc)
	if err := s.pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("apply the answer: %w", err)
	}
	select {
	case <-gathered:
	case <-time.After(gatherTimeout):
		return "", errors.New("ICE gathering did not complete")
	}
	return s.pc.LocalDescription().SDP, nil
}

// start runs the slot writers and the periodic reports.
func (s *Session) start() {
	for _, sl := range s.slots {
		s.wg.Go(func() { sl.run(s.done) })
	}
	sentTicker := s.clock.NewTicker(sentInterval)
	jitterTicker := s.clock.NewTicker(jitterInterval)
	s.wg.Go(func() {
		defer sentTicker.Stop()
		defer jitterTicker.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-sentTicker.C():
				s.flushSent()
			case <-jitterTicker.C():
				s.reportJitter()
			}
		}
	})
}

// Close stops the slots, releases the sources and closes the connection. It
// is safe to call more than once.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.wg.Wait()
		s.rpcMu.Lock() // no bind may retain a source after this
		s.releaseAll()
		s.rpcMu.Unlock()
		s.closeErr = s.pc.Close()
	})
	return s.closeErr
}

// SelectedPair returns the ICE candidate types ("host", "srflx", "prflx" or
// "relay") of the selected pair, never addresses (PH0-22). ok is false while
// ICE has selected no pair.
func (s *Session) SelectedPair() (local, remote string, ok bool) {
	pair, err := s.pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair()
	if err != nil || pair == nil {
		return "", "", false
	}
	return pair.Local.Typ.String(), pair.Remote.Typ.String(), true
}

func (s *Session) onSelectedPair(pair *webrtc.ICECandidatePair) {
	if pair == nil || pair.Local == nil || pair.Remote == nil {
		return
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.pair = &pairMsg{T: "pair", Local: pair.Local.Typ.String(), Remote: pair.Remote.Typ.String()}
	s.pairSent = false
	s.sendPairLocked()
}

// onDataChannel takes the viewer's rpc channel. Pion may deliver a message
// before the channel's OnOpen handler runs, so the channel is kept from here.
func (s *Session) onDataChannel(dc *webrtc.DataChannel) {
	if dc.Label() != "rpc" {
		return
	}
	s.sendMu.Lock()
	s.rpc = dc
	s.sendMu.Unlock()
	dc.OnOpen(func() {
		s.sendMu.Lock()
		defer s.sendMu.Unlock()
		s.sendPairLocked()
	})
	dc.OnMessage(s.onMessage)
}

// sendPairLocked sends the selected pair if it is known and not sent yet.
func (s *Session) sendPairLocked() {
	if s.pair != nil && !s.pairSent && s.sendLocked(*s.pair) {
		s.pairSent = true
	}
}

// notify sends a message on the rpc channel.
func (s *Session) notify(msg any) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	s.sendLocked(msg)
}

// sendLocked sends msg if the rpc channel is open and reports whether it did.
func (s *Session) sendLocked(msg any) bool {
	if s.rpc == nil || s.rpc.ReadyState() != webrtc.DataChannelStateOpen {
		return false
	}
	data, err := json.Marshal(msg)
	if err != nil {
		s.log.Error("encode an rpc message", "err", err)
		return false
	}
	if err := s.rpc.SendText(string(data)); err != nil {
		s.log.Debug("send an rpc message", "err", err)
		return false
	}
	return true
}

// onMessage handles one rpc message. Pion calls it for one message at a time
// per channel; the lock covers a viewer that opens a second rpc channel.
func (s *Session) onMessage(m webrtc.DataChannelMessage) {
	req, err := parseRequest(m.Data)
	if err != nil {
		s.log.Warn("ignored an rpc message", "err", err)
		return
	}
	s.rpcMu.Lock()
	defer s.rpcMu.Unlock()
	switch req.T {
	case "bind":
		s.bind(req)
	case "unbind":
		s.unbind(req)
	case "ping":
		s.notify(pongMsg{T: "pong", C0: req.C0, H: unixMs(s.clock.Now())})
	}
}

// bind checks a bind and hands it to the slot, which sends "bound" when it
// switches. A failed bind leaves the slot as it was.
func (s *Session) bind(req request) {
	fail := func(code string) {
		s.notify(errorMsg{T: "error", Slot: req.Slot, Epoch: req.Epoch, Code: code})
	}
	if req.Slot < 0 || req.Slot >= len(s.slots) {
		fail(codeBadSlot)
		return
	}
	src, ok := s.sources[req.Cam+"/"+req.Profile]
	if !ok {
		fail(codeUnknownCamera)
		return
	}
	codec, fallback, ok := chooseCodec(src, req.Profile == "sub", s.slots[req.Slot].offered)
	if !ok {
		fail(codeCodecUnsupported)
		return
	}
	s.retain(src, req.Profile)
	if old := s.slotSource[req.Slot]; old != nil {
		s.release(old)
	}
	s.slotSource[req.Slot] = src
	s.submit(req.Slot, binding{src: src, codec: codec, fallback: fallback, epoch: req.Epoch})
}

// unbind empties a slot.
func (s *Session) unbind(req request) {
	if req.Slot < 0 || req.Slot >= len(s.slots) {
		s.log.Warn("ignored an unbind of a slot the offer does not have", "slot", req.Slot)
		return
	}
	if old := s.slotSource[req.Slot]; old != nil {
		s.release(old)
		s.slotSource[req.Slot] = nil
	}
	s.submit(req.Slot, binding{})
}

// submit queues a request for a slot's writer.
func (s *Session) submit(slot int, b binding) {
	select {
	case s.slots[slot].reqs <- b:
	case <-s.done:
	}
}
