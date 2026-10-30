// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// liveH264 are the H.264 profile-level-ids that a live peer offers, in
// packetization mode 1: Constrained Baseline, Main (the simulated
// cameras' profile) and High, as libwebrtc offers them.
var liveH264 = []string{"42e01f", "4d001f", "64001f"}

// gatherTimeout bounds a live peer's ICE gathering, which lists the
// controller's own IPv4 addresses.
const gatherTimeout = 10 * time.Second

// NewLiveAPI is the WebRTC API of the controller's live peers (G20): it
// receives H.264 and gathers IPv4 UDP candidates only, without mDNS, so
// that the peers' Pion sockets are reccontroller's only listeners beside
// R08's list.
func NewLiveAPI() (*webrtc.API, error) {
	me := &webrtc.MediaEngine{}
	feedback := []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}}
	for i, plid := range liveH264 {
		if err := me.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
				SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=" + plid, RTCPFeedback: feedback},
			PayloadType: webrtc.PayloadType(96 + i), //nolint:gosec // three payload types
		}, webrtc.RTPCodecTypeVideo); err != nil {
			return nil, err
		}
	}
	var se webrtc.SettingEngine
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	return webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se)), nil
}

// LivePeer is the consumer of one live session of MI3 (G20): a
// receive-only Pion peer in reccontroller that plays the app's grid. It
// offers one receive-only video m-line per tile and the rpc channel of
// the video spike's session protocol; once answered, it binds each slot
// to its tile's stream and hands every access unit that it receives to
// its callback, under that stream, with the controller's time, so that the
// controller times the tile's latency from the SEI index (C15).
type LivePeer struct {
	pc    *webrtc.PeerConnection
	tiles []string
	clock source.Clock
	onAU  func(stream string, nalus [][]byte, at time.Time)
	offer string
	dc    *webrtc.DataChannel
	open  chan struct{}
	msgs  chan rpcReply

	mu        sync.Mutex
	closed    bool           // no reader starts once set
	readers   sync.WaitGroup // the tiles' readers
	closeOnce sync.Once
}

// rpcReply is the session's answer to a bind: "bound" or "error".
type rpcReply struct {
	T    string `json:"t"`
	Slot int    `json:"slot"`
	Code string `json:"code"`
}

// NewLivePeer makes the peer of a session whose tiles are the streams, as
// the controller serves them ("cam01_sub", …, "cam01_main"), and gathers
// its offer. onAU runs on the peer's own goroutines, one per tile.
func NewLivePeer(api *webrtc.API, tiles []string, clock source.Clock, onAU func(stream string, nalus [][]byte, at time.Time)) (*LivePeer, error) {
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	p := &LivePeer{pc: pc, tiles: tiles, clock: clock, onAU: onAU, open: make(chan struct{}), msgs: make(chan rpcReply, 2*len(tiles)+8)}
	fail := func(err error) (*LivePeer, error) {
		p.Close()
		return nil, fmt.Errorf("controller: live peer: %w", err)
	}
	for range tiles {
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			return fail(err)
		}
	}
	pc.OnTrack(p.onTrack)
	if p.dc, err = pc.CreateDataChannel("rpc", nil); err != nil {
		return fail(err)
	}
	var opened sync.Once
	p.dc.OnOpen(func() { opened.Do(func() { close(p.open) }) })
	p.dc.OnMessage(p.onMessage)
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return fail(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		return fail(err)
	}
	select {
	case <-gathered:
	case <-time.After(gatherTimeout):
		return fail(errors.New("ICE gathering did not complete"))
	}
	p.offer = pc.LocalDescription().SDP
	return p, nil
}

// Offer is the peer's offer, with all its ICE candidates.
func (p *LivePeer) Offer() string { return p.offer }

// Accept applies the hub's answer, waits for the rpc channel, binds each
// tile, every camera's sub stream then the main, to its slot, and returns
// once each slot is bound; a slot that the hub refuses fails it with the
// session's code, as does ctx.
func (p *LivePeer) Accept(ctx context.Context, answer string) error {
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		return fmt.Errorf("controller: live peer: apply the answer: %w", err)
	}
	select {
	case <-p.open:
	case <-ctx.Done():
		return fmt.Errorf("controller: live peer: the rpc channel did not open: %w", ctx.Err())
	}
	for slot, tile := range p.tiles {
		cam, profile, _ := strings.Cut(tile, "_")
		bind, err := json.Marshal(map[string]any{"t": "bind", "slot": slot, "cam": cam, "profile": profile, "epoch": slot + 1})
		if err != nil {
			return err
		}
		if err := p.dc.SendText(string(bind)); err != nil {
			return fmt.Errorf("controller: live peer: bind %s: %w", tile, err)
		}
	}
	bound := map[int]bool{}
	for len(bound) < len(p.tiles) {
		select {
		case m := <-p.msgs:
			if m.Slot < 0 || m.Slot >= len(p.tiles) {
				continue
			}
			if m.T == "error" {
				return fmt.Errorf("controller: live peer: the hub refused %s: %s", p.tiles[m.Slot], m.Code)
			}
			bound[m.Slot] = true
		case <-ctx.Done():
			return fmt.Errorf("controller: live peer: %d of %d tiles bound: %w", len(bound), len(p.tiles), ctx.Err())
		}
	}
	return nil
}

// onMessage keeps the session's answers to the binds; its other messages
// (pair, jitter) are not needed.
func (p *LivePeer) onMessage(m webrtc.DataChannelMessage) {
	var r rpcReply
	if json.Unmarshal(m.Data, &r) != nil || (r.T != "bound" && r.T != "error") {
		return
	}
	select {
	case p.msgs <- r:
	default: // a session never answers more than the binds
	}
}

// onTrack depacketises one slot's RTP into access units, until the peer
// closes.
func (p *LivePeer) onTrack(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	slot := -1
	for i, tr := range p.pc.GetTransceivers() {
		if tr.Receiver() == receiver {
			slot = i
		}
	}
	if slot < 0 || slot >= len(p.tiles) {
		return
	}
	stream := p.tiles[slot]
	dec, err := (&format.H264{PayloadTyp: uint8(track.PayloadType()), PacketizationMode: 1}).CreateDecoder()
	if err != nil {
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.readers.Add(1)
	p.mu.Unlock()
	defer p.readers.Done()
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			return
		}
		nalus, err := dec.Decode(pkt)
		if err != nil {
			continue // more packets needed, or a lost one
		}
		p.onAU(stream, nalus, p.clock.Now())
	}
}

// Close closes the connection and waits until no callback runs.
func (p *LivePeer) Close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		_ = p.pc.Close()
		p.readers.Wait()
	})
}
