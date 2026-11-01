// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi9

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// closeGrace is how long the hub side holds a bulk channel open after its
// last byte for the client to close it. The client closes it: when the hub
// side reset the stream right after its last byte, Chrome now and then lost
// the bytes of the next bulk channel, which opened while that reset was
// under way (seen locally in 1 run of 7 with Chrome 155).
const closeGrace = 10 * time.Second

// Hub is the hub side of MI9 (§8.5 step 4): the answering peer, which
// serves a recording's plaintext to the client's calls.
type Hub struct {
	pc   *webrtc.PeerConnection
	seg  *Segment
	bulk bulkSet

	ctx    context.Context
	cancel context.CancelFunc
	calls  sync.WaitGroup

	mu      sync.Mutex
	rpc     *webrtc.DataChannel
	lastIDR *time.Duration
	// closed is set once Close has begun: no call starts after it (goCall).
	closed bool
}

// NewHub makes the hub side of seg.
func NewHub(api *webrtc.API, cfg webrtc.Configuration, seg *Segment) (*Hub, error) {
	pc, err := api.NewPeerConnection(cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{pc: pc, seg: seg, ctx: ctx, cancel: cancel}
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "rpc" {
			return
		}
		h.mu.Lock()
		h.rpc = dc
		h.mu.Unlock()
		dc.OnMessage(h.onRequest)
	})
	return h, nil
}

// Answer applies the client's offer and returns the hub side's answer, with
// all its candidates.
func (h *Hub) Answer(ctx context.Context, offer string) (string, error) {
	if err := h.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return "", fmt.Errorf("mi9: apply the offer: %w", err)
	}
	answer, err := h.pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	return gather(ctx, h.pc, answer)
}

// Close closes the hub side and waits for its calls. Pion's Close does not
// wait for an rpc callback under way, which may still start a call: closed,
// set first, makes goCall drop it, so that no call joins calls while Close
// waits for them (sync.WaitGroup's rule).
func (h *Hub) Close() error {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.cancel()
	err := h.pc.Close()
	h.calls.Wait()
	return err
}

// goCall runs f, a call's work, on a goroutine that Close waits for, unless
// Close has begun.
func (h *Hub) goCall(f func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.calls.Go(f)
	}
}

// onRequest serves one call of the client: the info at once, a range or a
// seek on a goroutine of its own, since its bytes take a while.
func (h *Hub) onRequest(m webrtc.DataChannelMessage) {
	var req request
	if err := json.Unmarshal(m.Data, &req); err != nil {
		return
	}
	switch req.T {
	case "info":
		h.info(req.Call)
	case "read":
		h.goCall(func() {
			if req.Off < 0 || req.N < 0 || req.Off > h.seg.Size()-req.N {
				h.fail(req.Call, "invalid_argument", fmt.Errorf("the range [%d, +%d) is outside the plaintext", req.Off, req.N))
				return
			}
			h.serveRange(req.Call, reply{T: "range", Call: req.Call, Off: req.Off, N: req.N})
		})
	case "seek":
		h.goCall(func() { h.serveSeek(req.Call, time.Duration(req.Ms)*time.Millisecond) })
	default:
		h.fail(req.Call, "invalid_argument", fmt.Errorf("unknown call %q", req.T))
	}
}

// info answers the recording's info.
func (h *Hub) info(call uint64) {
	h.mu.Lock()
	last := h.lastIDR
	h.mu.Unlock()
	if last == nil {
		d, err := h.seg.lastIDR()
		if err != nil {
			h.fail(call, "internal", err)
			return
		}
		last = &d
		h.mu.Lock()
		h.lastIDR = last
		h.mu.Unlock()
	}
	h.reply(reply{T: "info", Call: call, Size: h.seg.Size(), InitBytes: h.seg.InitSize(), Codec: h.seg.Codec(), LastIDRMs: last.Milliseconds()})
}

// serveRange answers a call with r, a range, then sends its bytes on the
// call's bulk channel, which the client closes once it has them all, or the
// hub side after closeGrace: a call that would open a fifth bulk channel is
// refused with resource_exhausted (§7.6).
func (h *Hub) serveRange(call uint64, r reply) {
	if err := h.bulk.acquire(); err != nil {
		h.fail(call, "resource_exhausted", err)
		return
	}
	defer h.bulk.release()
	dc, err := h.pc.CreateDataChannel(bulkLabel(call), nil)
	if err != nil {
		h.fail(call, "internal", err)
		return
	}
	defer func() { _ = dc.Close() }()
	opened, closed := make(chan struct{}), make(chan struct{})
	dc.OnOpen(func() { close(opened) })
	var closeOnce sync.Once
	dc.OnClose(func() { closeOnce.Do(func() { close(closed) }) })
	h.reply(r)
	select {
	case <-opened:
	case <-h.ctx.Done():
		return
	}
	size := maxMessage
	if sctp := h.pc.SCTP(); sctp != nil {
		size = messageSize(sctp.GetCapabilities().MaxMessageSize)
	}
	if err := sendBulk(h.ctx, dc, h.seg, r.Off, r.N, size); err != nil {
		// A stop of the hub side, or of the client, which closes the
		// channel when its measurement's wait ends, is normal: only a send
		// that failed with the channel open is worth a line on the screen.
		if h.ctx.Err() == nil && dc.ReadyState() == webrtc.DataChannelStateOpen {
			slog.Warn("mi9: a bulk channel stopped", "call", call, "err", err)
		}
		stopDecided()
		return
	}
	select {
	case <-closed:
	case <-h.ctx.Done():
	case <-time.After(closeGrace):
	}
}

// sendBulk sends a range's bytes on its bulk channel (send); a test
// replaces it with a slower peer's.
var sendBulk = send

// stopDecided is called once a range whose send failed has decided whether
// to log it; a test waits for it before it closes the hub side, whose stop
// would otherwise decide in its place.
var stopDecided = func() {}

// reply sends r on the rpc channel.
func (h *Hub) reply(r reply) {
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	h.mu.Lock()
	rpc := h.rpc
	h.mu.Unlock()
	if rpc != nil {
		_ = rpc.SendText(string(b))
	}
}

// fail answers a call with an error.
func (h *Hub) fail(call uint64, code string, err error) {
	h.reply(reply{T: "error", Call: call, Code: code, Message: err.Error()})
}
