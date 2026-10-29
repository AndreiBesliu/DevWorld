// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package mi9 is the data-channel playback measurement MI9 (G18, CTR-12):
// its own two-peer Pion harness (C19), an offerer that plays a recording
// (the playback client) and an answerer on the hub side that reads it with
// age.DecryptReaderAt and sends its plaintext fMP4 over bulk/<call_id>
// channels by §7.6's rules; JudgeBulk and Judge, G18's rules (G25); and
// recspike's LAN run, which plays a segment recorded through ipcam-hub
// (hubrec).
package mi9

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"filippo.io/age"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/session"
)

// Transport is how the client reaches the hub side.
type Transport string

// The transports of MI9's legs (G26).
const (
	// LAN is a direct path over host candidates: the owner's LAN, or the
	// runner's own interfaces in CI (information only).
	LAN Transport = "lan"
	// TURNS443 is the relay through a TURN server's TLS listener on 443,
	// relay candidates only.
	TURNS443 Transport = "turns443"
	// Chrome is the page in Chrome as the client, over host candidates.
	Chrome Transport = "chrome"
)

// BulkResult is one measurement of a bulk channel: the throughput of the
// whole segment's plaintext over one channel, the 20 seeks' times to the
// first IDR's bytes, the Chrome leg's times to the first frame shown after
// each seek, and the selected candidate pair's round trip.
type BulkResult struct {
	Transport      Transport
	ThroughputMbps float64
	SeekMs         []float64
	DisplayMs      []float64
	RTTMs          float64
}

// Option configures a measurement beyond its transport.
type Option func(*options)

type options struct {
	// roots are the TURN-TLS server's trusted roots, nil for the system's.
	roots *x509.CertPool
}

// TURNRoots makes the TURN-TLS connections trust roots, such as the CA of
// a test TURN server's self-signed certificate, instead of the system's.
// TLS is always verified.
func TURNRoots(roots *x509.CertPool) Option { return func(o *options) { o.roots = roots } }

// The waits of a session.
const (
	// gatherWait bounds the gathering of a peer's candidates, a TURN
	// allocation over TLS included.
	gatherWait = 30 * time.Second
	// dialWait bounds the TLS connection to a TURN server.
	dialWait = 15 * time.Second
	// closeGrace is how long the hub side holds a bulk channel open after
	// its last byte for the client to close it. The client closes it: when
	// the hub side reset the stream right after its last byte, Chrome now
	// and then lost the bytes of the next bulk channel, which opened while
	// that reset was under way (seen locally in 1 run of 7 with Chrome 155).
	closeGrace = 10 * time.Second
)

// NewAPI is the WebRTC API of MI9's peers for transport t: the video
// spike's (session.NewAPI, IPv4 UDP, C19), with mDNS resolution of the
// browser's .local candidates for Chrome; for TURNS443 an API of its own,
// whose TURN connections are TLS, verified against the roots of
// TURNRoots or the system's.
func NewAPI(t Transport, opts ...Option) (*webrtc.API, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch t {
	case LAN, Chrome:
		api, _, err := session.NewAPI(t == Chrome)
		return api, err
	case TURNS443:
		var se webrtc.SettingEngine
		// Relay candidates are published only with a UDP network type, and
		// a TURN URL over TCP is used only with a TCP one.
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeTCP4})
		se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
		se.SetICEProxyDialer(tlsDialer{pool: o.roots})
		return webrtc.NewAPI(webrtc.WithSettingEngine(se)), nil
	}
	return nil, fmt.Errorf("mi9: unknown transport %q", t)
}

// Configuration is a peer's configuration for transport t: with ice, the
// TURN servers, and relay candidates only for TURNS443.
func Configuration(t Transport, ice []webrtc.ICEServer) webrtc.Configuration {
	if t != TURNS443 {
		return webrtc.Configuration{}
	}
	return webrtc.Configuration{ICEServers: ice, ICETransportPolicy: webrtc.ICETransportPolicyRelay}
}

// tlsDialer opens the TLS connection to a turns: server for Pion's ICE
// agent (its proxy dialer, which it uses for TURN over TCP), verifying the
// server's certificate against pool, nil for the system's roots, for the
// host that the URL names.
type tlsDialer struct{ pool *x509.CertPool }

func (d tlsDialer) Dial(network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialWait)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	tc := tls.Client(conn, &tls.Config{ServerName: host, RootCAs: d.pool, MinVersion: tls.VersionTLS12})
	if err := tc.HandshakeContext(ctx); err != nil {
		return nil, errors.Join(err, conn.Close())
	}
	return tc, nil
}

// MeasureBulk measures a bulk channel (C19, G25) over transport t between
// two peers in this process: the hub side reads the encrypted recording at
// recPath with id, and the client times the whole plaintext over one
// channel, 20 seeks to uniform targets, and the pair's round trip. For
// TURNS443, ice holds the TURN server, every URL a turns: one over TCP, and
// both peers use relay candidates only.
func MeasureBulk(ctx context.Context, recPath string, id age.Identity, t Transport, ice []webrtc.ICEServer, opts ...Option) (BulkResult, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	seg, err := OpenSegment(recPath, id)
	if err != nil {
		return BulkResult{}, err
	}
	defer func() { _ = seg.Close() }()
	c, h, err := newPair(ctx, seg, t, ice, o)
	if err != nil {
		return BulkResult{}, err
	}
	defer func() { _ = c.Close(); _ = h.Close() }()
	r, err := c.Measure(ctx)
	r.Transport = t
	return r, err
}

// newPair connects a client to a hub side of seg, both in this process.
func newPair(ctx context.Context, seg *Segment, t Transport, ice []webrtc.ICEServer, o options) (*Client, *Hub, error) {
	if t == TURNS443 {
		for _, s := range ice {
			for _, u := range s.URLs {
				if !strings.HasPrefix(u, "turns:") || !strings.Contains(u, "transport=tcp") {
					return nil, nil, fmt.Errorf("mi9: %s is not a turns: URL over TCP", u)
				}
			}
		}
	}
	api, err := NewAPI(t, func(p *options) { *p = o })
	if err != nil {
		return nil, nil, err
	}
	cfg := Configuration(t, ice)
	c, err := NewClient(api, cfg)
	if err != nil {
		return nil, nil, err
	}
	h, err := NewHub(api, cfg, seg)
	if err != nil {
		return nil, nil, errors.Join(err, c.Close())
	}
	answer, err := h.Answer(ctx, c.Offer())
	if err == nil {
		err = c.Accept(ctx, answer)
	}
	if err != nil {
		return nil, nil, errors.Join(err, c.Close(), h.Close())
	}
	return c, h, nil
}

// The rpc protocol of the harness, JSON text on the client's rpc channel
// (§7.6's procedures stand-in): the client asks for the recording's info,
// for a byte range ("read"), or for a seek to a time from the first sample;
// the hub side answers each call on rpc ("info", "range" or "error") and
// sends a range's bytes on bulk/<call>, which it opens for the call. The
// reply names the range's length; the client closes the channel once it
// has every byte (closeGrace says why the client, not the hub side).
type request struct {
	T    string `json:"t"`
	Call uint64 `json:"call"`
	Off  int64  `json:"off,omitempty"`
	N    int64  `json:"n,omitempty"`
	Ms   int64  `json:"ms,omitempty"`
}

type reply struct {
	T    string `json:"t"`
	Call uint64 `json:"call"`
	// info: the plaintext's length, its init's, the video codec as MSE
	// names it, and the last IDR's time from the first sample, up to which
	// seeks go.
	Size      int64  `json:"size,omitempty"`
	InitBytes int64  `json:"initBytes,omitempty"`
	Codec     string `json:"codec,omitempty"`
	LastIDRMs int64  `json:"lastIdrMs,omitempty"`
	// range: the bytes that bulk/<call> carries and, for a seek, where its
	// IDR lies in the plaintext.
	Off     int64 `json:"off,omitempty"`
	N       int64 `json:"n,omitempty"`
	IDROff  int64 `json:"idrOff,omitempty"`
	IDRSize int64 `json:"idrSize,omitempty"`
	// error: a Connect code (§7.7) and the error's text.
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// bulkLabel is the label of a call's bulk channel.
func bulkLabel(call uint64) string { return fmt.Sprintf("bulk/%d", call) }

// gather sets pc's local description to desc, waits until its candidates
// are gathered, and returns the description, which must hold one.
func gather(ctx context.Context, pc *webrtc.PeerConnection, desc webrtc.SessionDescription) (string, error) {
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(desc); err != nil {
		return "", err
	}
	timer := time.NewTimer(gatherWait)
	defer timer.Stop()
	select {
	case <-gathered:
	case <-timer.C:
		return "", errors.New("mi9: ICE gathering did not complete")
	case <-ctx.Done():
		return "", ctx.Err()
	}
	sdp := pc.LocalDescription().SDP
	if !strings.Contains(sdp, "a=candidate:") {
		return "", errors.New("mi9: no ICE candidate was gathered (with relay only: the TURN server gave none)")
	}
	return sdp, nil
}

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

// Close closes the hub side and waits for its calls.
func (h *Hub) Close() error {
	h.cancel()
	err := h.pc.Close()
	h.calls.Wait()
	return err
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
		h.calls.Go(func() {
			if req.Off < 0 || req.N < 0 || req.Off > h.seg.Size()-req.N {
				h.fail(req.Call, "invalid_argument", fmt.Errorf("the range [%d, +%d) is outside the plaintext", req.Off, req.N))
				return
			}
			h.serveRange(req.Call, reply{T: "range", Call: req.Call, Off: req.Off, N: req.N})
		})
	case "seek":
		h.calls.Go(func() { h.serveSeek(req.Call, time.Duration(req.Ms)*time.Millisecond) })
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
	if err := send(h.ctx, dc, h.seg, r.Off, r.N, size); err != nil {
		slog.Warn("mi9: a bulk channel stopped", "call", call, "err", err)
		return
	}
	select {
	case <-closed:
	case <-h.ctx.Done():
	case <-time.After(closeGrace):
	}
}

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

// Client is the playback side of MI9: the offering peer, which asks the hub
// side for a recording's bytes and times them.
type Client struct {
	pc     *webrtc.PeerConnection
	rpc    *webrtc.DataChannel
	offer  string
	open   chan struct{}
	failed chan struct{}
	next   atomic.Uint64
	// largest is the largest bulk message received.
	largest atomic.Int64

	mu    sync.Mutex
	calls map[uint64]*call
}

// NewClient makes the playback side, with its rpc channel, and gathers its
// offer.
func NewClient(api *webrtc.API, cfg webrtc.Configuration) (*Client, error) {
	pc, err := api.NewPeerConnection(cfg)
	if err != nil {
		return nil, err
	}
	c := &Client{pc: pc, open: make(chan struct{}), failed: make(chan struct{}), calls: map[uint64]*call{}}
	var failOnce sync.Once
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateFailed {
			failOnce.Do(func() { close(c.failed) })
		}
	})
	pc.OnDataChannel(c.onBulk)
	if c.rpc, err = pc.CreateDataChannel("rpc", nil); err != nil {
		return nil, errors.Join(err, pc.Close())
	}
	var opened sync.Once
	c.rpc.OnOpen(func() { opened.Do(func() { close(c.open) }) })
	c.rpc.OnMessage(c.onReply)
	offer, err := pc.CreateOffer(nil)
	if err == nil {
		c.offer, err = gather(context.Background(), pc, offer)
	}
	if err != nil {
		return nil, errors.Join(err, pc.Close())
	}
	return c, nil
}

// Offer is the client's offer, with all its candidates.
func (c *Client) Offer() string { return c.offer }

// Accept applies the hub side's answer and waits for the rpc channel.
func (c *Client) Accept(ctx context.Context, answer string) error {
	if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		return fmt.Errorf("mi9: apply the answer: %w", err)
	}
	select {
	case <-c.open:
		return nil
	case <-c.failed:
		return errors.New("mi9: the connection failed before the rpc channel opened")
	case <-ctx.Done():
		return fmt.Errorf("mi9: the rpc channel did not open: %w", ctx.Err())
	}
}

// Close closes the playback side.
func (c *Client) Close() error { return c.pc.Close() }

// largestMessage is the largest bulk message received.
func (c *Client) largestMessage() int { return int(c.largest.Load()) }

// call is one call of the client: its reply, and the bytes of its range as
// they arrive on its bulk channel, which may come before the reply.
type call struct {
	id      uint64
	replied chan reply
	sink    io.Writer
	keep    bool

	mu      sync.Mutex
	r       *reply
	got     int64
	data    []byte    // the range's bytes, when kept
	idrAt   time.Time // when the IDR's last byte arrived
	done    chan struct{}
	doneAt  time.Time
	closed  bool
	closing chan struct{}
	dc      *webrtc.DataChannel
}

// start starts a call with req: its bytes go to sink, and are kept too
// when keep is set.
func (c *Client) start(req request, sink io.Writer, keep bool) (*call, error) {
	req.Call = c.next.Add(1)
	cl := &call{id: req.Call, replied: make(chan reply, 1), sink: sink, keep: keep, done: make(chan struct{}), closing: make(chan struct{})}
	c.mu.Lock()
	c.calls[req.Call] = cl
	c.mu.Unlock()
	b, err := json.Marshal(req)
	if err == nil {
		err = c.rpc.SendText(string(b))
	}
	if err != nil {
		c.forget(req.Call)
		return nil, err
	}
	return cl, nil
}

func (c *Client) forget(id uint64) {
	c.mu.Lock()
	delete(c.calls, id)
	c.mu.Unlock()
}

func (c *Client) lookup(id uint64) *call {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[id]
}

// onReply hands the hub side's reply to its call.
func (c *Client) onReply(m webrtc.DataChannelMessage) {
	var r reply
	if json.Unmarshal(m.Data, &r) != nil {
		return
	}
	if cl := c.lookup(r.Call); cl != nil {
		select {
		case cl.replied <- r:
		default:
		}
	}
}

// onBulk takes a call's bulk channel, which the hub side opens.
func (c *Client) onBulk(dc *webrtc.DataChannel) {
	var id uint64
	if _, err := fmt.Sscanf(dc.Label(), "bulk/%d", &id); err != nil {
		return
	}
	cl := c.lookup(id)
	if cl == nil {
		_ = dc.Close()
		return
	}
	cl.mu.Lock()
	cl.dc = dc
	cl.mu.Unlock()
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		if n := int64(len(m.Data)); n > c.largest.Load() {
			c.largest.Store(n)
		}
		cl.received(m.Data)
	})
	dc.OnClose(func() {
		cl.mu.Lock()
		if !cl.closed {
			cl.closed = true
			close(cl.closing)
		}
		cl.mu.Unlock()
	})
}

// received takes the next bytes of the call's range.
func (cl *call) received(b []byte) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	_, _ = cl.sink.Write(b)
	if cl.keep {
		cl.data = append(cl.data, b...)
	}
	cl.got += int64(len(b))
	cl.progress(time.Now())
}

// progress marks, the reply known, when the IDR's bytes and all the bytes
// have arrived. The caller holds mu.
func (cl *call) progress(now time.Time) {
	r := cl.r
	if r == nil {
		return
	}
	if r.IDRSize > 0 && cl.idrAt.IsZero() && cl.got >= r.IDROff+r.IDRSize-r.Off {
		cl.idrAt = now
	}
	if cl.doneAt.IsZero() && cl.got >= r.N {
		cl.doneAt = now
		close(cl.done)
		if cl.dc != nil {
			go func(dc *webrtc.DataChannel) { _ = dc.Close() }(cl.dc)
		}
	}
}

// wait waits for the call's reply and, for a range, all of its bytes.
func (c *Client) wait(ctx context.Context, cl *call) (reply, error) {
	defer c.forget(cl.id)
	var r reply
	select {
	case r = <-cl.replied:
	case <-c.failed:
		return reply{}, errors.New("mi9: the connection failed")
	case <-ctx.Done():
		return reply{}, ctx.Err()
	}
	if r.T == "error" {
		return r, fmt.Errorf("mi9: the hub side refused the call: %s: %s", r.Code, r.Message)
	}
	if r.T != "range" {
		return r, nil
	}
	cl.mu.Lock()
	cl.r = &r
	cl.progress(time.Now())
	cl.mu.Unlock()
	select {
	case <-cl.done:
		return r, nil
	case <-cl.closing:
		select {
		case <-cl.done:
			return r, nil
		default:
		}
		cl.mu.Lock()
		got := cl.got
		cl.mu.Unlock()
		return r, fmt.Errorf("mi9: the bulk channel closed after %d of %d bytes", got, r.N)
	case <-c.failed:
		return r, errors.New("mi9: the connection failed")
	case <-ctx.Done():
		return r, ctx.Err()
	}
}

// info asks for the recording's info.
func (c *Client) info(ctx context.Context) (info, error) {
	cl, err := c.start(request{T: "info"}, io.Discard, false)
	if err != nil {
		return info{}, err
	}
	r, err := c.wait(ctx, cl)
	if err == nil && r.T != "info" {
		err = fmt.Errorf("mi9: the hub side answered info with %q", r.T)
	}
	return info{Size: r.Size, InitBytes: r.InitBytes, Codec: r.Codec, LastIDRMs: r.LastIDRMs}, err
}

// info is the recording as the hub side describes it.
type info struct {
	Size, InitBytes, LastIDRMs int64
	Codec                      string
}

// read reads the n plaintext bytes at off into w, over one bulk channel.
func (c *Client) read(ctx context.Context, off, n int64, w io.Writer) error {
	cl, err := c.start(request{T: "read", Off: off, N: n}, w, false)
	if err != nil {
		return err
	}
	r, err := c.wait(ctx, cl)
	if err == nil && (r.Off != off || r.N != n) {
		err = fmt.Errorf("mi9: asked for [%d, +%d), the hub side sent [%d, +%d)", off, n, r.Off, r.N)
	}
	return err
}

// rttWait bounds the wait for the pair's round trip at rest: its first
// check and one keepalive, which Pion sends every 2 s.
const rttWait = 6 * time.Second

// rtt is the selected candidate pair's current round trip in ms, once the
// pair has answered a keepalive at rest, before any bulk transfer: a
// keepalive behind a transfer's bytes would time the queue too.
func (c *Client) rtt(ctx context.Context) (float64, error) {
	sctp := c.pc.SCTP()
	if sctp == nil || sctp.Transport() == nil {
		return 0, errors.New("mi9: no transport for the round trip")
	}
	transport := sctp.Transport().ICETransport()
	deadline := time.Now().Add(rttWait)
	for {
		stats, ok := transport.GetSelectedCandidatePairStats()
		if ok && (stats.ResponsesReceived >= 2 || (time.Now().After(deadline) && stats.ResponsesReceived >= 1)) {
			return stats.CurrentRoundTripTime * 1000, nil
		}
		if time.Now().After(deadline) {
			return 0, errors.New("mi9: the selected candidate pair has no round trip")
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Measure times the bulk channel (G25): the pair's round trip at rest, the
// whole segment's plaintext over one channel (throughput = bits ÷ time),
// then Seeks seeks to uniform targets up to the last IDR.
func (c *Client) Measure(ctx context.Context) (BulkResult, error) {
	var r BulkResult
	in, err := c.info(ctx)
	if err != nil {
		return r, err
	}
	if r.RTTMs, err = c.rtt(ctx); err != nil {
		return r, err
	}
	start := time.Now()
	if err := c.read(ctx, 0, in.Size, io.Discard); err != nil {
		return r, err
	}
	r.ThroughputMbps = float64(in.Size*8) / time.Since(start).Seconds() / 1e6
	for _, target := range seekTargets(in.LastIDRMs, Seeks) {
		s, err := c.seek(ctx, target)
		if err != nil {
			return r, fmt.Errorf("mi9: seek to %d ms: %w", target, err)
		}
		r.SeekMs = append(r.SeekMs, s.ms)
	}
	return r, nil
}
