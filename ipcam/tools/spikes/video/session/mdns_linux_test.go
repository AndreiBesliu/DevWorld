package session

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/webrtc/v4"
)

// The tests in this file bind the mDNS port 5353, so they must not run in
// parallel with each other.

// listenMDNSPort binds UDP port 5353 on all IPv4 addresses, with SO_REUSEADDR
// when reuse is set, as avahi and the Windows DNS client do.
func listenMDNSPort(t *testing.T, reuse bool) (net.PacketConn, error) {
	t.Helper()
	lc := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		if !reuse {
			return nil
		}
		var sockErr error
		if err := raw.Control(func(fd uintptr) {
			sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		}); err != nil {
			return err
		}
		return sockErr
	}}
	conn, err := lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:5353")
	if err == nil {
		t.Cleanup(func() { _ = conn.Close() })
	}
	return conn, err
}

// withoutCandidates removes the ICE candidates from an SDP, so that the peer
// that receives it cannot start connectivity checks itself.
func withoutCandidates(sdp string) string {
	var kept []string
	for _, line := range strings.Split(sdp, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") || line == "a=end-of-candidates" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\r\n")
}

func TestMDNS_CoexistsWithExistingListener(t *testing.T) {
	if _, err := listenMDNSPort(t, true); err != nil {
		t.Fatalf("cannot bind :5353 with SO_REUSEADDR before NewAPI: %v", err)
	}
	api, mdnsUnavailable, err := NewAPI(true)
	if err != nil {
		t.Fatal(err)
	}
	if mdnsUnavailable {
		t.Fatal("NewAPI(true) reported mDNS as unavailable next to a listener with SO_REUSEADDR")
	}

	c := newClient(t, clientConfig{slots: 1, offer: chromeLike, mdns: true})
	var candidates int
	for _, line := range strings.Split(c.offer, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") {
			candidates++
			if !strings.Contains(line, ".local ") || ipLiteral.MatchString(line) {
				t.Fatalf("the client published %q, want a .local host candidate", line)
			}
		}
	}
	if candidates == 0 {
		t.Fatal("the offer holds no candidate")
	}

	s, answerSDP, err := answer(api, c.offer, nil, testOptions(source.SystemClock{}, nil))
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// Without the hub's candidates the client cannot reach the hub: the
	// connection comes up only if the hub resolves the client's .local name
	// and checks it.
	if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: withoutCandidates(answerSDP)}); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, c.open)
	if _, remote, ok := s.SelectedPair(); !ok || remote != "host" {
		t.Errorf("hub's remote candidate = %q (selected %v), want the resolved host candidate", remote, ok)
	}
}

func TestNewAPI_ReportsMDNSUnavailableWhenThePortIsTaken(t *testing.T) {
	// The peer connections of an earlier test release the port shortly
	// after they close.
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := listenMDNSPort(t, false)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Skipf("another process holds :5353, so this test cannot take it: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var logs syncBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	api, mdnsUnavailable, err := NewAPI(true)
	if err != nil {
		t.Fatal(err)
	}
	if !mdnsUnavailable {
		t.Fatal("NewAPI(true) did not report mDNS as unavailable while :5353 is taken")
	}
	if out := logs.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "5353") {
		t.Errorf("log = %q, want a warning that names the mDNS port", out)
	}

	// The API still works, without mDNS.
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	connect(t, api, c, nil, testOptions(source.SystemClock{}, nil))
}
