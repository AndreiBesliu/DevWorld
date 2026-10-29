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
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
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
// each seek, and the selected candidate pair's round trip. Partial is set
// when the measurement's wait ended it first (G27), as results.Mi9Partial
// says: during the transfer, the throughput is that of the bytes received
// by then, and there is no seek; during the seeks, the seeks are those
// timed by then, the last one perhaps cut (Mi9Partial.LastSeekCut).
type BulkResult struct {
	Transport      Transport
	ThroughputMbps float64
	SeekMs         []float64
	DisplayMs      []float64
	RTTMs          float64
	Partial        *results.Mi9Partial
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
