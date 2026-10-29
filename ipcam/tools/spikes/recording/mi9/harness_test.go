package mi9

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"
	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// testSegment writes an encrypted test recording of secs seconds and
// returns its path, the identity that decrypts it and its frames.
func testSegment(t *testing.T, secs int) (string, age.Identity, []spiketest.Sample) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cam01_main.mp4.age")
	samples, err := spiketest.WriteSegment(path, secs, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	return path, id, samples
}

// plaintextOf is the whole plaintext of the recording, as age.Decrypt reads
// it, independent of the harness's random-access reader.
func plaintextOf(t *testing.T, path string, id age.Identity) []byte {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	r, err := age.Decrypt(f, id)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// idrAt is the payload of the first IDR whose time from the first frame is
// target or later, from the frames as the test recording wrote them.
func idrAt(samples []spiketest.Sample, target time.Duration) []byte {
	ticks := uint64(target.Milliseconds()) * spiketest.TimeScale / 1000 //nolint:gosec // a target in the recording
	for _, s := range samples {
		if s.IDR && s.Time >= ticks {
			return s.Payload
		}
	}
	return nil
}

// TestMeasureBulk_LoopbackPlumbing (C19, §8.5 step 4): the playback client
// offers and the hub side answers, in this process, over host candidates.
// The hub reads the recording with age.DecryptReaderAt and sends plaintext:
// one bulk channel carries the whole of it, byte for byte what age.Decrypt
// gives, in messages of at most 64 KiB. Each seek delivers the bytes of the
// first IDR at or after its target, the rule of ipcam-hub's seekbench. A
// read while four bulk channels are open is refused (§7.6). MeasureBulk
// gives a throughput, 20 timed seeks and the pair's round trip.
func TestMeasureBulk_LoopbackPlumbing(t *testing.T) {
	path, id, samples := testSegment(t, 30)
	plain := plaintextOf(t, path, id)
	seg, err := OpenSegment(path, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = seg.Close() }()
	if seg.Size() != int64(len(plain)) || seg.Codec() != "avc1.42c028" {
		t.Errorf("the segment: %d bytes of plaintext, codec %q; want %d and avc1.42c028", seg.Size(), seg.Codec(), len(plain))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	c, h, err := newPair(ctx, seg, LAN, nil, options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(); _ = h.Close() }()

	in, err := c.info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if in.Size != int64(len(plain)) || in.InitBytes <= 0 || in.Codec != "avc1.42c028" || in.LastIDRMs != 28500 {
		t.Errorf("info %+v, want %d bytes, the init's length, avc1.42c028 and the last IDR at 28500 ms", in, len(plain))
	}
	sum := sha256.New()
	if err := c.read(ctx, 0, in.Size, sum); err != nil {
		t.Fatal(err)
	}
	if got, want := sum.Sum(nil), sha256.Sum256(plain); !bytes.Equal(got, want[:]) {
		t.Errorf("the plaintext over the bulk channel differs from age.Decrypt's")
	}
	if m := c.largestMessage(); m <= 0 || m > maxMessage {
		t.Errorf("the largest bulk message was %d bytes, want 1 to %d", m, maxMessage)
	}
	for _, target := range []int64{0, 1, 1499, 1500, 1501, 14000, 28500} {
		s, err := c.seek(ctx, target)
		if err != nil {
			t.Fatalf("seek to %d ms: %v", target, err)
		}
		if want := idrAt(samples, time.Duration(target)*time.Millisecond); !bytes.Equal(s.idr, want) {
			t.Errorf("seek to %d ms delivered %d bytes, not the %d of the first IDR at or after it", target, len(s.idr), len(want))
		}
	}
	if _, err := c.seek(ctx, 28501); err == nil {
		t.Error("a seek past the last IDR succeeded")
	}

	waitBulkIdle(t, h)
	for range MaxBulkChannels {
		if err := h.bulk.acquire(); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.read(ctx, 0, 1000, io.Discard); err == nil || !strings.Contains(err.Error(), "resource_exhausted") {
		t.Errorf("a read with %d bulk channels open = %v, want resource_exhausted", MaxBulkChannels, err)
	}
	for range MaxBulkChannels {
		h.bulk.release()
	}

	r, err := MeasureBulk(ctx, path, id, LAN, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkMeasured(t, r, LAN)
}

// waitBulkIdle waits until the hub side holds no bulk channel. A call
// returns once its last byte has arrived; the client then closes the
// call's channel, and the hub side gives its slot back only when that
// close reaches it, or after closeGrace.
func waitBulkIdle(t *testing.T, h *Hub) {
	t.Helper()
	deadline := time.Now().Add(closeGrace + 5*time.Second)
	for {
		h.bulk.mu.Lock()
		open := h.bulk.open
		h.bulk.mu.Unlock()
		if open == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the hub side still holds %d bulk channels %v after the last call", open, closeGrace+5*time.Second)
		}
		time.Sleep(time.Millisecond)
	}
}

// checkMeasured checks the figures of a measurement: a throughput, 20
// seeks, and the pair's round trip. Windows' monotonic clock moves at the
// system timer's tick, so a single figure can time at 0 ms there; their sum
// cannot.
func checkMeasured(t *testing.T, r BulkResult, transport Transport) {
	t.Helper()
	var sum float64
	for _, ms := range r.SeekMs {
		if ms < 0 {
			t.Errorf("a seek of %v ms", ms)
		}
		sum += ms
	}
	if r.Transport != transport || r.ThroughputMbps <= 0 || len(r.SeekMs) != Seeks || sum <= 0 || r.RTTMs < 0 || (r.RTTMs == 0 && runtime.GOOS != "windows") {
		t.Errorf("MeasureBulk = %+v, want %s with a throughput, %d timed seeks and a round trip", r, transport, Seeks)
	}
}

// selfSigned is a certificate for 127.0.0.1, and the pool that trusts it.
func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mi9 test TURN"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// quietLogs keeps pion/turn's log of the connections that the tests close
// off their output.
func quietLogs() logging.LoggerFactory {
	f := logging.NewDefaultLoggerFactory()
	f.DefaultLogLevel = logging.LogLevelDisabled
	return f
}

// startTURN starts a pion/turn server with a TLS listener on 127.0.0.1, its
// only listener, and returns its ICE server, the pool that trusts its
// certificate, and how many allocations it has made.
func startTURN(t *testing.T) (webrtc.ICEServer, *x509.CertPool, func() int64) {
	t.Helper()
	cert, pool := selfSigned(t)
	ln, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	const realm, user, pass = "mi9.test", "mi9", "secret"
	key := turn.GenerateAuthKey(user, realm, pass)
	var allocations atomic.Int64
	srv, err := turn.NewServer(turn.ServerConfig{
		Realm: realm,
		AuthHandler: func(ra *turn.RequestAttributes) (string, []byte, bool) {
			return ra.Username, key, ra.Username == user
		},
		EventHandler: turn.EventHandler{OnAllocationCreated: func(net.Addr, net.Addr, string, string, string, net.Addr, int) {
			allocations.Add(1)
		}},
		LoggerFactory: quietLogs(),
		ListenerConfigs: []turn.ListenerConfig{{Listener: ln,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{RelayAddress: net.IPv4(127, 0, 0, 1), Address: "127.0.0.1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	return webrtc.ICEServer{URLs: []string{"turns:127.0.0.1:" + strconv.Itoa(port) + "?transport=tcp"}, Username: user, Credential: pass},
		pool, allocations.Load
}

// TestMeasureBulk_RelayInProcessTURN (G36, R06): with the relay transport,
// both peers gather relay candidates only, through a pion/turn server's
// TLS listener, whose self-signed certificate they trust explicitly; the
// plumbing works through it. Without that trust, the system's roots refuse
// the certificate and nothing is measured: TLS is verified.
func TestMeasureBulk_RelayInProcessTURN(t *testing.T) {
	path, id, _ := testSegment(t, 10)
	server, roots, allocations := startTURN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	r, err := MeasureBulk(ctx, path, id, TURNS443, []webrtc.ICEServer{server}, TURNRoots(roots))
	if err != nil {
		t.Fatal(err)
	}
	checkMeasured(t, r, TURNS443)
	if n := allocations(); n < 2 {
		t.Errorf("%d TURN allocations over TLS, want one per peer at least", n)
	}
	if _, err := MeasureBulk(ctx, path, id, TURNS443, []webrtc.ICEServer{server}); err == nil {
		t.Error("MeasureBulk trusted the TURN server's self-signed certificate without its roots")
	}
}

// liveSegment is the recording that the mi9 job made from main.h264 with
// ipcam-hub (mi9page record), from MI9_SEGMENT and MI9_IDENTITY: required
// when MI9_REQUIRE_TURN=1, as the mi9 job sets it, and skipped otherwise
// (C20).
func liveSegment(t *testing.T) (string, age.Identity) {
	t.Helper()
	path := spiketest.Required(t, "MI9_SEGMENT", "MI9_REQUIRE_TURN")
	idFile := spiketest.Required(t, "MI9_IDENTITY", "MI9_REQUIRE_TURN")
	b, err := os.ReadFile(idFile) //nolint:gosec // the job's own file
	if err != nil {
		t.Fatal(err)
	}
	id, err := age.ParseX25519Identity(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return path, id
}

// printLine prints the leg's envelope as the "mi9: <json>" line that the
// mi9 job turns into a notice (C32), after checking that it fits one: at
// most 3,500 bytes after annotate.sh's escaping.
func printLine(t *testing.T, set, source string, r BulkResult) {
	t.Helper()
	e := results.Envelope{Schema: results.Schema, Code: "MI9", RunID: set + "-1", Set: set, Host: "lab-linux", OS: runtime.GOOS,
		MI9: &results.Mi9Data{Legs: []results.Mi9Leg{Leg(r, source)}}}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if escaped := strings.ReplaceAll(string(b), "%", "%25"); len(escaped) > 3500 {
		t.Errorf("the mi9 line is %d bytes after escaping, over 3,500", len(escaped))
	}
	t.Logf("%s: %s", set, b)
	_, _ = os.Stdout.WriteString("mi9: " + string(b) + "\n")
}

// TestMeasureBulk_LiveLoopback (G26 leg 1, information only): in the mi9
// job, the recording made from main.h264 over host candidates on the
// runner; it prints the ci-loopback line.
func TestMeasureBulk_LiveLoopback(t *testing.T) {
	path, id := liveSegment(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	r, err := MeasureBulk(ctx, path, id, LAN, nil)
	if err != nil {
		t.Fatal(err)
	}
	printLine(t, "ci-loopback", SourceLoopback, r)
	checkMeasured(t, r, LAN)
}

// TestMeasureBulk_Coturn443 (C20, G36, R06): in the mi9 job only
// (MI9_REQUIRE_TURN=1), the relay through coturn's TLS listener on 443 at
// 50 ms RTT, the relay verdict's leg: the job delays the traffic into
// coturn's container by 25 ms with netem (MI9_TURN_DELAY=netem), or this
// test does with a TCP proxy in front of it (MI9_TURN_DELAY=proxy). It
// prints the ci-relay line, then holds the measured round trip to 45–60 ms.
func TestMeasureBulk_Coturn443(t *testing.T) {
	if os.Getenv("MI9_REQUIRE_TURN") != "1" {
		t.Skip("MI9_REQUIRE_TURN is unset: coturn and the 50 ms delay run in the mi9 job only (C20)")
	}
	path, id := liveSegment(t)
	turnURL, user, pass, caFile := os.Getenv("MI9_TURN_URL"), os.Getenv("MI9_TURN_USER"), os.Getenv("MI9_TURN_PASS"), os.Getenv("MI9_TURN_CA")
	pem, err := os.ReadFile(caFile) //nolint:gosec // the job's own file
	if err != nil {
		t.Fatalf("MI9_TURN_CA: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		t.Fatal("MI9_TURN_CA holds no certificate")
	}
	switch delay := os.Getenv("MI9_TURN_DELAY"); delay {
	case "netem":
	case "proxy":
		turnURL = delayedURL(t, turnURL, 25*time.Millisecond)
	default:
		t.Fatalf("MI9_TURN_DELAY=%q, want netem or proxy", delay)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	r, err := MeasureBulk(ctx, path, id, TURNS443, []webrtc.ICEServer{{URLs: []string{turnURL}, Username: user, Credential: pass}}, TURNRoots(roots))
	if err != nil {
		t.Fatal(err)
	}
	printLine(t, "ci-relay", SourceRelay, r)
	checkMeasured(t, r, TURNS443)
	if r.RTTMs < 45 || r.RTTMs > 60 {
		t.Errorf("the relay's round trip is %.1f ms, want 45–60 ms", r.RTTMs)
	}
}
