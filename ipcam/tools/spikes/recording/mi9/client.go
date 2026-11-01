// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi9

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

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
	// stopped is set by stop: a bulk channel that opens later is closed.
	stopped bool
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
	if cl.stopped {
		cl.mu.Unlock()
		_ = dc.Close()
		return
	}
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

// count is how many bytes of the call's range have arrived.
func (cl *call) count() int64 {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.got
}

// stop stops the call's transfer: it closes the call's bulk channel when
// the hub side has opened it, and onBulk closes one that opens later.
func (cl *call) stop() {
	cl.mu.Lock()
	cl.stopped = true
	dc := cl.dc
	cl.mu.Unlock()
	if dc != nil {
		_ = dc.Close()
	}
}

// wait waits for the call's reply and, for a range, all of its bytes. When
// ctx ends first, it stops the call's transfer.
func (c *Client) wait(ctx context.Context, cl *call) (reply, error) {
	defer c.forget(cl.id)
	var r reply
	select {
	case r = <-cl.replied:
	case <-c.failed:
		return reply{}, errors.New("mi9: the connection failed")
	case <-ctx.Done():
		cl.stop()
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
		return r, fmt.Errorf("mi9: the bulk channel closed after %d of %d bytes", cl.count(), r.N)
	case <-c.failed:
		return r, errors.New("mi9: the connection failed")
	case <-ctx.Done():
		cl.stop()
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

// read reads the n plaintext bytes at off into w, over one bulk channel,
// and returns how many of them arrived: all n, or fewer with the error that
// ended the call.
func (c *Client) read(ctx context.Context, off, n int64, w io.Writer) (int64, error) {
	cl, err := c.start(request{T: "read", Off: off, N: n}, w, false)
	if err != nil {
		return 0, err
	}
	r, err := c.wait(ctx, cl)
	if err == nil && (r.Off != off || r.N != n) {
		err = fmt.Errorf("mi9: asked for [%d, +%d), the hub side sent [%d, +%d)", off, n, r.Off, r.N)
	}
	return cl.count(), err
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
// then Seeks seeks to uniform targets up to the last IDR. When ctx's
// deadline, the measurement's wait, passes during the transfer or the
// seeks, Measure stops the transfer and returns what it measured, as
// Partial, without error, since a measured failure decides (G27); cut says
// what it keeps.
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
	got, err := c.read(ctx, 0, in.Size, io.Discard)
	elapsed := time.Since(start)
	if err != nil {
		return cut(r, got, in.Size, elapsed, nil, err)
	}
	r.ThroughputMbps = throughput(in.Size, elapsed)
	for _, target := range seekTargets(in.LastIDRMs, Seeks) {
		s, err := c.seek(ctx, target)
		if err != nil {
			return cut(r, in.Size, in.Size, elapsed, &s, fmt.Errorf("mi9: seek to %d ms: %w", target, err))
		}
		r.SeekMs = append(r.SeekMs, s.ms)
	}
	return r, nil
}

// cut ends a measurement that err stopped: during the transfer, received
// of the plaintext's size bytes having arrived in elapsed; or during the
// seeks, the whole transfer done in elapsed, inFlight being the seek under
// way. When err is the measurement's wait, ctx's deadline, r is a
// measurement all the same, without error (G27), as Partial:
//   - during the transfer, it has the throughput of the bytes received by
//     then, and no seek;
//   - during the seeks, it keeps the whole transfer's throughput and the
//     seeks timed by then, then inFlight: its time to the IDR when the
//     IDR had arrived; else its time so far, a lower bound, kept when it is
//     over MaxSeekMs, a measured failure (LastSeekCut), and dropped
//     otherwise, since it decides nothing.
//
// Any other error stays the measurement's.
func cut(r BulkResult, received, size int64, elapsed time.Duration, inFlight *seekResult, err error) (BulkResult, error) {
	if !errors.Is(err, context.DeadlineExceeded) {
		return r, err
	}
	p := &results.Mi9Partial{ReceivedBytes: received, SizeBytes: size, ElapsedMs: float64(elapsed.Microseconds()) / 1000}
	switch {
	case inFlight == nil:
		r.ThroughputMbps = throughput(received, elapsed)
	case !inFlight.cut || inFlight.ms > MaxSeekMs:
		r.SeekMs = append(r.SeekMs, inFlight.ms)
		p.LastSeekCut = inFlight.cut
	}
	r.Partial = p
	return r, nil
}

// throughput is n bytes in elapsed, in Mbps.
func throughput(n int64, elapsed time.Duration) float64 {
	return float64(n*8) / elapsed.Seconds() / 1e6
}
