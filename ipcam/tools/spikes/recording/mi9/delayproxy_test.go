package mi9

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// delayedURL is R06's fallback when netem is unavailable: a TCP proxy on
// 127.0.0.1 in front of the TURN server of turnURL ("turns:<host>:<port>?…")
// that holds every byte from a peer to the server for d, and turnURL with
// the proxy's address. Each direction of the relayed path between two peers
// crosses it once (peer → TURN), so the round trip gains 2d, as with netem
// on the host side of coturn's veth.
func delayedURL(t *testing.T, turnURL string, d time.Duration) string {
	t.Helper()
	rest, ok := strings.CutPrefix(turnURL, "turns:")
	if !ok {
		t.Fatalf("MI9_TURN_URL %q is not a turns: URL", turnURL)
	}
	server, query, _ := strings.Cut(rest, "?")
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			peer, err := ln.Accept()
			if err != nil {
				return
			}
			go proxy(peer, server, d)
		}
	}()
	return "turns:" + ln.Addr().String() + "?" + query
}

// proxy joins peer to server, delaying peer's bytes by d.
func proxy(peer net.Conn, server string, d time.Duration) {
	defer func() { _ = peer.Close() }()
	up, err := net.Dial("tcp4", server) //nolint:gosec // the job's own TURN server
	if err != nil {
		return
	}
	defer func() { _ = up.Close() }()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(peer, up)
		close(done)
	}()
	delayedCopy(up, peer, d)
	<-done
}

// delayedCopy copies src to dst, each read's bytes written d after they
// were read, in order.
func delayedCopy(dst, src net.Conn, d time.Duration) {
	type chunk struct {
		at time.Time
		b  []byte
	}
	queue := make(chan chunk, 1024)
	written := make(chan struct{})
	go func() {
		defer close(written)
		for c := range queue {
			time.Sleep(time.Until(c.at))
			if _, err := dst.Write(c.b); err != nil {
				return
			}
		}
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			queue <- chunk{at: time.Now().Add(d), b: bytes.Clone(buf[:n])}
		}
		if err != nil {
			close(queue)
			<-written
			return
		}
	}
}

// TestDelayedCopy_HoldsEachByte: the fallback's copy holds bytes for its
// delay and keeps their order.
func TestDelayedCopy_HoldsEachByte(t *testing.T) {
	a, b := net.Pipe()
	c, e := net.Pipe()
	go func() {
		delayedCopy(c, b, 30*time.Millisecond)
		_ = c.Close()
	}()
	start := time.Now()
	go func() {
		_, _ = a.Write([]byte("first,"))
		_, _ = a.Write([]byte("second"))
		_ = a.Close()
	}()
	got, err := io.ReadAll(e)
	if err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	if string(got) != "first,second" || time.Since(start) < 30*time.Millisecond {
		t.Errorf("got %q after %v, want first,second after 30 ms or more", got, time.Since(start))
	}
}
