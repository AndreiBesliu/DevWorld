package session

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/mdns/v2"
	"github.com/pion/webrtc/v4"
	"golang.org/x/net/ipv4"
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

// lanIPv4 returns the first IPv4 address of a running interface other than
// loopback.
func lanIPv4(t *testing.T) net.IP {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagRunning == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
				return ipNet.IP.To4()
			}
		}
	}
	t.Skip("no running interface with an IPv4 address")
	return nil
}

// publishMDNS answers mDNS queries for name with ip on conn, as a browser's
// mDNS responder does for the .local name of one of its host candidates.
func publishMDNS(t *testing.T, conn net.PacketConn, name string, ip net.IP) {
	t.Helper()
	server, err := mdns.NewServer(ipv4.NewPacketConn(conn), nil, mdns.WithLocalNames(name), mdns.WithLocalAddress(ip))
	if err != nil {
		t.Fatalf("start the test's mDNS responder: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
}

// withHostName replaces the address ip of the host candidates in an SDP by
// name, and fails the test when no candidate has that address.
func withHostName(t *testing.T, sdp string, ip net.IP, name string) string {
	t.Helper()
	lines := strings.Split(sdp, "\r\n")
	replaced := 0
	for i, line := range lines {
		fields := strings.Fields(line) // a=candidate:foundation component transport priority address port typ type ...
		if strings.HasPrefix(line, "a=candidate:") && len(fields) >= 8 && fields[4] == ip.String() && fields[7] == "host" {
			fields[4] = name
			lines[i] = strings.Join(fields, " ")
			replaced++
		}
	}
	if replaced == 0 {
		t.Fatalf("the offer has no host candidate on %s", ip)
	}
	return strings.Join(lines, "\r\n")
}

func TestMDNS_CoexistsWithExistingListener(t *testing.T) {
	// The existing listener is the test's own mDNS responder, bound like avahi
	// or the Windows DNS client: 0.0.0.0:5353 with SO_REUSEADDR. It publishes
	// the client's one address under a .local name, as a browser does. (Pion
	// as the client would publish one name for all its addresses and answer
	// with the address of the interface a query came in on, which on a host
	// with several interfaces resolves to an address the candidate's socket is
	// not bound to.)
	listener, err := listenMDNSPort(t, true)
	if err != nil {
		t.Fatalf("cannot bind :5353 with SO_REUSEADDR before NewAPI: %v", err)
	}
	ip := lanIPv4(t)
	name := "spike-test-" + strings.ToLower(rand.Text()) + ".local"
	publishMDNS(t, listener, name, ip)

	api, mdnsUnavailable, err := NewAPI(true)
	if err != nil {
		t.Fatal(err)
	}
	if mdnsUnavailable {
		t.Fatal("NewAPI(true) reported mDNS as unavailable next to a listener with SO_REUSEADDR")
	}

	c := newClient(t, clientConfig{slots: 1, offer: chromeLike, onlyIP: ip})
	offer := withHostName(t, c.offer, ip, name)
	s, answerSDP, err := answer(api, offer, nil, testOptions(source.SystemClock{}, nil))
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// Without the hub's candidates the client cannot reach the hub: the
	// connection can only start with the hub's checks to the address it
	// resolved for the client's .local name.
	if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: withoutCandidates(answerSDP)}); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, c.open)
	// Pion adds a .local remote candidate to its agent only once the mDNS
	// query for it has an answer.
	waitUntil(t, "the hub holds the client's resolved host candidate", func() bool {
		return hasRemoteHostCandidate(s, name)
	})
	// The client has one address, so its checks reach the hub from the
	// resolved address and the pair is host to host.
	if local, remote, ok := s.SelectedPair(); !ok || local != "host" || remote != "host" {
		t.Errorf("SelectedPair() = %q, %q, %v; want host, host, true", local, remote, ok)
	}
}

// hasRemoteHostCandidate reports whether the hub's ICE agent holds a remote
// host candidate with the given address.
func hasRemoteHostCandidate(s *Session, address string) bool {
	for _, stat := range s.pc.GetStats() {
		if c, ok := stat.(webrtc.ICECandidateStats); ok && c.Type == webrtc.StatsTypeRemoteCandidate &&
			c.CandidateType == webrtc.ICECandidateTypeHost && c.IP == address {
			return true
		}
	}
	return false
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
