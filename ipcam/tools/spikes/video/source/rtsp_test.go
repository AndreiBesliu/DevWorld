package source

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
	"github.com/pion/rtp"
)

// logBuffer is a goroutine-safe sink for captured log output.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs routes the default slog logger, at every level, to the returned
// buffer until the test ends. The lines carry no time, whose colons would look
// like an IPv6 address to assertNoSecrets.
func captureLogs(t *testing.T) *logBuffer {
	t.Helper()
	b := &logBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return b
}

// testServerHandler serves one stream to every reader.
type testServerHandler struct {
	mu     sync.Mutex
	stream *gortsplib.ServerStream
	status base.StatusCode
	// When user is set, DESCRIBE and SETUP need these credentials.
	user, pass string
	// When gate is set, the publisher waits until it is closed.
	gate chan struct{}
	// When once is set, the publisher stops after one pass over its access units.
	once bool
}

// requireLogin makes the test server ask for credentials, as a real camera does.
func requireLogin(user, pass string) func(*testServerHandler) {
	return func(h *testServerHandler) { h.user, h.pass = user, pass }
}

// holdPublisher keeps the test server from publishing until release is called.
// A client that NewRTSP has returned to is playing, so it receives everything
// that is published after that.
func holdPublisher() (hold func(*testServerHandler), release func()) {
	gate := make(chan struct{})
	var once sync.Once
	return func(h *testServerHandler) { h.gate = gate }, func() { once.Do(func() { close(gate) }) }
}

// publishOnce makes the test server publish its access units once, not in a loop.
func publishOnce(h *testServerHandler) { h.once = true }

// authorised reports whether the request may proceed. The library asks the
// client for credentials when it gets a 401 and none were sent.
func (h *testServerHandler) authorised(conn *gortsplib.ServerConn, req *base.Request) bool {
	return h.user == "" || conn.VerifyCredentials(req, h.user, h.pass)
}

func (h *testServerHandler) current() (*base.Response, *gortsplib.ServerStream) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return &base.Response{StatusCode: h.status}, h.stream
}

func (h *testServerHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !h.authorised(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	res, stream := h.current()
	return res, stream, nil
}

func (h *testServerHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !h.authorised(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	res, stream := h.current()
	return res, stream, nil
}

func (h *testServerHandler) OnPlay(*gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	return &base.Response{StatusCode: base.StatusOK}, nil
}

// parameterSets returns the VPS (H.265 only), SPS and PPS of the first access
// unit that has them.
func parameterSets(codec Codec, aus []AccessUnit) (vps, sps, pps []byte) {
	for _, au := range aus {
		for _, n := range au.NALUs {
			switch paramKindOf(codec, n) {
			case paramVPS:
				vps = n
			case paramSPS:
				sps = n
			case paramPPS:
				pps = n
			}
		}
		if sps != nil && pps != nil {
			return vps, sps, pps
		}
	}
	return nil, nil, nil
}

type encodeFunc func(au [][]byte) ([]*rtp.Packet, error)

// The pieces of a camera URL that must never be logged.
const (
	testUser  = "admin"
	testPass  = "hunter2"
	testPath  = "secret-cam/stream1"
	testQuery = "token=abc123"
)

func cameraURL(addr string) string {
	return "rtsp://" + testUser + ":" + testPass + "@" + addr + "/" + testPath + "?" + testQuery
}

// newTestStream starts an RTSP server on the loopback interface with one video
// stream of the given format, and returns its address.
func newTestStream(t *testing.T, forma format.Format, opts ...func(*testServerHandler)) (addr string, stream *gortsplib.ServerStream, medi *description.Media, handler *testServerHandler) {
	t.Helper()
	handler = &testServerHandler{status: base.StatusOK}
	for _, opt := range opts {
		opt(handler)
	}
	server := &gortsplib.Server{Handler: handler, RTSPAddress: "127.0.0.1:0"}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)

	medi = &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{forma}}
	stream = &gortsplib.ServerStream{Server: server, Desc: &description.Session{Medias: []*description.Media{medi}}}
	if err := stream.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stream.Close)
	handler.mu.Lock()
	handler.stream = stream
	handler.mu.Unlock()
	return server.NetListener().Addr().String(), stream, medi, handler
}

// publish calls write for i = 0, 1, ... 8 ms apart, until write returns false or
// the test ends. It starts when the handler's gate is open.
func publish(t *testing.T, handler *testServerHandler, write func(i int) bool) {
	t.Helper()
	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	t.Cleanup(func() {
		stop()
		<-done
	})
	go func() {
		defer close(done)
		if handler.gate != nil {
			select {
			case <-handler.gate:
			case <-ctx.Done():
				return
			}
		}
		for i := 0; ctx.Err() == nil; i++ {
			if !write(i) {
				return
			}
			select {
			case <-ctx.Done():
			case <-time.After(8 * time.Millisecond):
			}
		}
	}()
}

// startRTSPServer serves aus in a loop over RTSP on the loopback interface and
// returns its address. The parameter sets are in the SDP; they stay in the
// stream only when inBand is true.
func startRTSPServer(t *testing.T, codec Codec, aus []AccessUnit, fps int, inBand bool, opts ...func(*testServerHandler)) string {
	t.Helper()
	vps, sps, pps := parameterSets(codec, aus)
	var (
		forma  format.Format
		encode encodeFunc
	)
	switch codec {
	case CodecH264:
		f := &format.H264{PayloadTyp: 96, PacketizationMode: 1, SPS: sps, PPS: pps}
		enc, err := f.CreateEncoder()
		if err != nil {
			t.Fatal(err)
		}
		forma, encode = f, enc.Encode
	case CodecH265:
		f := &format.H265{PayloadTyp: 96, VPS: vps, SPS: sps, PPS: pps}
		enc, err := f.CreateEncoder()
		if err != nil {
			t.Fatal(err)
		}
		forma, encode = f, enc.Encode
	}
	addr, stream, medi, handler := newTestStream(t, forma, opts...)

	step := uint32(90000 / fps) //nolint:gosec // fps is 12 or 20
	publish(t, handler, func(i int) bool {
		if handler.once && i == len(aus) {
			return false
		}
		nalus := aus[i%len(aus)].NALUs
		if !inBand {
			nalus = withoutParameterSets(codec, nalus)
		}
		pkts, err := encode(nalus)
		if err != nil {
			return false
		}
		for _, p := range pkts {
			p.Timestamp = 1000 + uint32(i)*step //nolint:gosec // i counts the frames of a short test
			if stream.WritePacketRTP(medi, p) != nil {
				return false
			}
		}
		return true
	})
	return addr
}

// startGarbageRTSPServer serves an H.264 stream that starts with one IDR slice,
// which the client needs to place the stream in time, and goes on with STAP-B
// packets, a type that the depacketiser does not support, so that nothing decodes.
func startGarbageRTSPServer(t *testing.T, opts ...func(*testServerHandler)) string {
	t.Helper()
	addr, stream, medi, handler := newTestStream(t, &format.H264{PayloadTyp: 96, PacketizationMode: 1}, opts...)
	publish(t, handler, func(i int) bool {
		payload := []byte{0x19, 0x00, 0x01, 0xff}
		if i == 0 {
			payload = []byte{0x65, 0x88, 0x84, 0x01}
		}
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version: 2, PayloadType: 96, Marker: true,
				SequenceNumber: uint16(i),             //nolint:gosec // wraps, as sequence numbers do
				Timestamp:      1000 + uint32(i)*7500, //nolint:gosec // i counts the frames of a short test
			},
			Payload: payload,
		}
		return stream.WritePacketRTP(medi, pkt) == nil
	})
	return addr
}

func withoutParameterSets(codec Codec, nalus [][]byte) [][]byte {
	var out [][]byte
	for _, n := range nalus {
		switch paramKindOf(codec, n) {
		case paramVPS, paramSPS, paramPPS:
		default:
			out = append(out, n)
		}
	}
	return out
}

// ipLiteral matches an IPv4 address, or an IPv6 one with at least two colons.
var ipLiteral = regexp.MustCompile(`\d{1,3}(?:\.\d{1,3}){3}|[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{0,4}){2,}`)

// assertNoSecrets fails when text contains any part of the camera URL, one of
// the extra secrets, or an IP address (PH0-22).
func assertNoSecrets(t *testing.T, what, text, addr string, extra ...string) {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	secrets := append([]string{cameraURL(addr), testUser, testPass, addr, host, port, testPath, "stream1", testQuery, "abc123"}, extra...)
	for _, secret := range secrets {
		if strings.Contains(text, secret) {
			t.Errorf("%s contains %q:\n%s", what, secret, text)
		}
	}
	if ip := ipLiteral.FindString(text); ip != "" {
		t.Errorf("%s contains the IP address %q:\n%s", what, ip, text)
	}
}

func TestRTSP_ReadsFromLocalServer(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		codec   Codec
		fps     int
		profile string
		inBand  bool
	}{
		{"H264 with parameter sets in the stream", "sub.h264", CodecH264, 12, "4d401f", true},
		{"H264 with parameter sets only in the SDP", "sub.h264", CodecH264, 12, "4d401f", false},
		{"H265 with parameter sets only in the SDP", "main-h265.h265", CodecH265, 20, "017b", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			aus, err := ReadAnnexB(mediaFile(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			logs := captureLogs(t)
			addr := startRTSPServer(t, tc.codec, aus, tc.fps, tc.inBand)

			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			src, err := NewRTSP(ctx, "real-1", cameraURL(addr))
			if err != nil {
				t.Fatal(err)
			}
			if src.ID() != "real-1" || src.Codec() != tc.codec || src.Profile() != tc.profile {
				t.Fatalf("ID %q, codec %s, profile %q; want real-1, %s, %q", src.ID(), src.Codec(), src.Profile(), tc.codec, tc.profile)
			}
			if src.SentLog() != nil {
				t.Error("an RTSP source has no sent log")
			}

			got, live, cancel := src.Subscribe()
			defer cancel()
			for len(got) < 30 {
				got = append(got, waitFor(t, live))
			}
			assertStream(t, got, aus, tc.fps)
			if src.FPS() != tc.fps {
				t.Errorf("FPS() = %d, want %d", src.FPS(), tc.fps)
			}
			if src.Jitter() != 0 {
				t.Errorf("Jitter() = %v: the hub does not pace an RTSP source", src.Jitter())
			}
			if snap := src.JitterSnapshot(); snap.P99Since(JitterSnapshot{}) != 0 || snap.P99Since(snap) != 0 {
				t.Error("an RTSP source reports pacing jitter in its snapshot")
			}

			stop() // the source ends and closes the subscription
			for range live {
			}
			out := logs.String()
			if !strings.Contains(out, "real-1") {
				t.Errorf("the source logged nothing under its id:\n%s", out)
			}
			assertNoSecrets(t, "log output", out, addr)
		})
	}
}

// assertStream checks that got is a run of whole frames of file, starting at a
// keyframe, with steady counters and timestamps.
func assertStream(t *testing.T, got, file []AccessUnit, fps int) {
	t.Helper()
	if !got[0].IDR {
		t.Fatal("the first access unit is not an IDR")
	}
	// Find where in the looped file the stream starts.
	start := -1
	for i, au := range file {
		if au.IDR && equalNALUs(au.NALUs, got[0].NALUs) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("the first access unit matches no keyframe of the file: %d NAL units", len(got[0].NALUs))
	}
	period := time.Second / time.Duration(fps)
	for i, au := range got {
		want := file[(start+i)%len(file)]
		if !equalNALUs(au.NALUs, want.NALUs) || au.IDR != want.IDR {
			t.Fatalf("access unit %d does not match frame %d of the file (IDR %v, want %v)",
				i, (start+i)%len(file), au.IDR, want.IDR)
		}
		if au.Frame != got[0].Frame+uint16(i) || au.Loop != 0 {
			t.Fatalf("access unit %d: Frame %d, Loop %d; frames must count up and Loop stay 0", i, au.Frame, au.Loop)
		}
		if i > 0 {
			if d := au.PTS - got[i-1].PTS; d < period-time.Millisecond || d > period+time.Millisecond {
				t.Fatalf("access unit %d is %v after the previous one, want %v", i, d, period)
			}
		}
	}
}

func TestRTSP_ErrorsAndLogsNeverContainTheURL(t *testing.T) {
	logs := captureLogs(t)

	// A port with nothing listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	_, closedPort, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	byName := "localhost:" + closedPort // Go's dial error then names the resolved IP address

	// A server that answers DESCRIBE with an error.
	handler := &testServerHandler{status: base.StatusNotFound}
	server := &gortsplib.Server{Handler: handler, RTSPAddress: "127.0.0.1:0"}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	serverAddr := server.NetListener().Addr().String()

	tests := []struct {
		name string
		url  string
		addr string
	}{
		{"connection refused", cameraURL(addr), addr},
		{"connection refused, host by name", cameraURL(byName), byName},
		{"DESCRIBE refused", cameraURL(serverAddr), serverAddr},
		{"malformed URL", "rtsp://" + testUser + ":" + testPass + "@[::1/" + testPath + "?" + testQuery, addr},
		{"wrong scheme", "http://" + testUser + ":" + testPass + "@" + addr + "/" + testPath + "?" + testQuery, addr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, err := NewRTSP(t.Context(), "real-2", tc.url)
			if err == nil {
				t.Fatalf("got source %q and no error", src.ID())
			}
			assertNoSecrets(t, "error", err.Error(), tc.addr)
		})
	}
	// The failure paths return their error and log nothing, so these two are a
	// guard for the day that one starts to log. TestRTSP_ClientCallbacksScrubTheirErrors
	// and TestRTSP_WarnsOnceWhenNothingDecodes check lines that exist.
	assertNoSecrets(t, "log output", logs.String(), addr)
	assertNoSecrets(t, "log output", logs.String(), serverAddr)
}

func TestRTSP_ContextCancelEndsTheSource(t *testing.T) {
	logs := captureLogs(t)
	addr := startRTSPServer(t, CodecH264, synthAUs(24, 12), 12, true)
	ctx, stop := context.WithCancel(t.Context())
	src, err := NewRTSP(ctx, "real-1", cameraURL(addr))
	if err != nil {
		t.Fatal(err)
	}
	_, live, cancel := src.Subscribe()
	defer cancel()
	waitFor(t, live)

	stop()
	for range live { // drains, then returns when the source closes the channel
	}

	_, late, cancelLate := src.Subscribe()
	defer cancelLate()
	waitClosed(t, late)
	assertNoSecrets(t, "log output", logs.String(), addr)
}

func TestRTSP_LogsInWithTheCredentialsOfTheURL(t *testing.T) {
	aus := synthAUs(24, 12)
	addr := startRTSPServer(t, CodecH264, aus, 12, true, requireLogin(testUser, testPass))
	logs := captureLogs(t)

	t.Run("right password", func(t *testing.T) {
		ctx, stop := context.WithCancel(t.Context())
		defer stop()
		src, err := NewRTSP(ctx, "real-1", cameraURL(addr))
		if err != nil {
			t.Fatal(err)
		}
		_, live, cancel := src.Subscribe()
		defer cancel()
		if au := waitFor(t, live); len(au.NALUs) == 0 {
			t.Fatal("an empty access unit")
		}
	})

	const wrongPass = "not-the-password"
	for name, rawURL := range map[string]string{
		"wrong password": "rtsp://" + testUser + ":" + wrongPass + "@" + addr + "/" + testPath + "?" + testQuery,
		"no credentials": "rtsp://" + addr + "/" + testPath + "?" + testQuery,
	} {
		t.Run(name, func(t *testing.T) {
			src, err := NewRTSP(t.Context(), "real-2", rawURL)
			if err == nil {
				t.Fatalf("got source %q and no error", src.ID())
			}
			assertNoSecrets(t, "error", err.Error(), addr, wrongPass)
		})
	}
	assertNoSecrets(t, "log output", logs.String(), addr, wrongPass)
}

// The depacketiser returns slices of the RTP packet, and the client may reuse
// the packet's buffer for the next one. An access unit in the GOP cache must
// not change when that happens.
func TestRTSP_AccessUnitsDoNotAliasPackets(t *testing.T) {
	captureLogs(t)
	s := newTestRTSPSource(t, cameraURL("camera.example:8554"), &format.H264{PayloadTyp: 96, PacketizationMode: 1})
	packet := func(seq uint16, payload []byte) *rtp.Packet {
		return &rtp.Packet{
			Header:  rtp.Header{Version: 2, PayloadType: 96, SequenceNumber: seq, Timestamp: 1000 + uint32(seq)*7500, Marker: true},
			Payload: payload,
		}
	}

	s.onPacket(packet(0, []byte{0x65, 0x88, 0x84, 0x00}), 0) // the frame the stream was joined in is dropped
	payload := []byte{0x65, 0x88, 0x84, 0x01}
	s.onPacket(packet(1, payload), time.Second/12)
	for i := range payload {
		payload[i] = 0xff
	}

	cached, _, cancel := s.Subscribe()
	defer cancel()
	want := [][]byte{{0x65, 0x88, 0x84, 0x01}}
	if len(cached) != 1 || !cached[0].IDR || !equalNALUs(cached[0].NALUs, want) {
		t.Fatalf("cached = %+v, want one IDR access unit with NAL units %x", cached, want)
	}
}

// Parameter sets and other NAL units for the tests below. The "stored" sets are
// the ones in the SDP; the "stream" ones are what an access unit carries itself.
var (
	h264StoredSPS = []byte{0x67, 0x4d, 0x40, 0x1f, 0xac, 0xd9}
	h264StoredPPS = []byte{0x68, 0xee, 0x3c, 0x80}
	h264StreamSPS = []byte{0x67, 0x64, 0x00, 0x28, 0xac, 0xd9}
	h264StreamPPS = []byte{0x68, 0xce, 0x31, 0x12}
	h264IDR       = []byte{0x65, 0x88, 0x84, 0x01}
	h264NonIDR    = []byte{0x41, 0x9a, 0x00, 0x01}
	h264AUD       = []byte{0x09, 0xf0}
	h264SEI       = []byte{0x06, 0x05, 0x01, 0xff, 0x80}

	h265StoredVPS = []byte{0x40, 0x01, 0x0c, 0x01, 0xff, 0xff, 0x01, 0x60}
	h265StoredSPS = []byte{0x42, 0x01, 0x01, 0x01, 0x60, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x7b, 0xa0}
	h265StoredPPS = []byte{0x44, 0x01, 0xc1, 0x72, 0xb4}
	h265StreamSPS = []byte{0x42, 0x01, 0x01, 0x01, 0x60, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x7b, 0xa8}
	h265IDR       = []byte{0x26, 0x01, 0xaf, 0x01}
	h265AUD       = []byte{0x46, 0x01, 0x50}
	h265SEI       = []byte{0x4e, 0x01, 0x05, 0x01}
)

// newTestRTSPSource creates a source for the stream that forma describes, as
// NewRTSP does after DESCRIBE. rawURL is the camera URL that the source must
// keep out of its logs.
func newTestRTSPSource(t *testing.T, rawURL string, forma format.Format) *rtspSource {
	t.Helper()
	desc := &description.Session{Medias: []*description.Media{{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{forma},
	}}}
	u, err := base.ParseURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	s, _, _, err := newRTSPSource("real-1", slog.Default(), newScrubber(rawURL, u), desc)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func h264Source(t *testing.T) *rtspSource {
	t.Helper()
	return newTestRTSPSource(t, cameraURL("camera.example:8554"), &format.H264{
		PayloadTyp: 96, PacketizationMode: 1, SPS: h264StoredSPS, PPS: h264StoredPPS,
	})
}

func h265Source(t *testing.T) *rtspSource {
	t.Helper()
	return newTestRTSPSource(t, cameraURL("camera.example:8554"), &format.H265{
		PayloadTyp: 96, VPS: h265StoredVPS, SPS: h265StoredSPS, PPS: h265StoredPPS,
	})
}

func assertNALUs(t *testing.T, got, want [][]byte) {
	t.Helper()
	if !equalNALUs(got, want) {
		t.Errorf("NAL units = %x\nwant       %x", got, want)
	}
}

func TestWithParameterSets_SPSWithoutPPS_Order(t *testing.T) {
	s := h264Source(t)

	// The PPS comes after the SPS that it belongs to, not in front of it. The
	// SPS of the access unit stays, and the stored one is not added next to it.
	got := s.withParameterSets([][]byte{h264StreamSPS, h264IDR})
	assertNALUs(t, got, [][]byte{h264StreamSPS, h264StoredPPS, h264IDR})

	// A PPS without an SPS gets the stored SPS in front of it.
	got = s.withParameterSets([][]byte{h264StreamPPS, h264IDR})
	assertNALUs(t, got, [][]byte{h264StoredSPS, h264StreamPPS, h264IDR})

	// Other NAL units keep their order behind the parameter sets.
	got = s.withParameterSets([][]byte{h264StreamSPS, h264SEI, h264IDR})
	assertNALUs(t, got, [][]byte{h264StreamSPS, h264StoredPPS, h264SEI, h264IDR})

	// Nothing is missing and nothing is duplicated.
	got = s.withParameterSets([][]byte{h264StreamSPS, h264StreamPPS, h264IDR})
	assertNALUs(t, got, [][]byte{h264StreamSPS, h264StreamPPS, h264IDR})

	// Both are missing: the stored sets, SPS first.
	got = s.withParameterSets([][]byte{h264IDR})
	assertNALUs(t, got, [][]byte{h264StoredSPS, h264StoredPPS, h264IDR})
}

func TestWithParameterSets_AUDLed_Order(t *testing.T) {
	s := h264Source(t)

	// The access unit delimiter is the first NAL unit of an access unit.
	got := s.withParameterSets([][]byte{h264AUD, h264IDR})
	assertNALUs(t, got, [][]byte{h264AUD, h264StoredSPS, h264StoredPPS, h264IDR})

	got = s.withParameterSets([][]byte{h264AUD, h264StreamSPS, h264IDR})
	assertNALUs(t, got, [][]byte{h264AUD, h264StreamSPS, h264StoredPPS, h264IDR})

	got = s.withParameterSets([][]byte{h264AUD, h264SEI, h264IDR})
	assertNALUs(t, got, [][]byte{h264AUD, h264StoredSPS, h264StoredPPS, h264SEI, h264IDR})
}

func TestWithParameterSets_H265_Order(t *testing.T) {
	s := h265Source(t)

	// The VPS goes first, then the SPS and the PPS.
	got := s.withParameterSets([][]byte{h265StreamSPS, h265IDR})
	assertNALUs(t, got, [][]byte{h265StoredVPS, h265StreamSPS, h265StoredPPS, h265IDR})

	got = s.withParameterSets([][]byte{h265AUD, h265StreamSPS, h265SEI, h265IDR})
	assertNALUs(t, got, [][]byte{h265AUD, h265StoredVPS, h265StreamSPS, h265StoredPPS, h265SEI, h265IDR})

	got = s.withParameterSets([][]byte{h265IDR})
	assertNALUs(t, got, [][]byte{h265StoredVPS, h265StoredSPS, h265StoredPPS, h265IDR})

	// Nothing is missing and nothing is duplicated.
	got = s.withParameterSets([][]byte{h265StoredVPS, h265StreamSPS, h265StoredPPS, h265IDR})
	assertNALUs(t, got, [][]byte{h265StoredVPS, h265StreamSPS, h265StoredPPS, h265IDR})
}

func TestRTSP_WarnsOnceWhenNothingDecodes(t *testing.T) {
	logs := captureLogs(t)
	hold, release := holdPublisher()
	addr := startGarbageRTSPServer(t, hold)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	src, err := NewRTSP(ctx, "real-1", cameraURL(addr))
	if err != nil {
		t.Fatal(err)
	}
	release()
	// Once the source has dropped three more packets, it has had its chance to
	// warn again.
	waitUntil(t, "the source dropped packets after the first", func() bool {
		return strings.Count(logs.String(), `msg="rtsp frame dropped"`) >= 3
	})
	if src.FPS() != 0 || src.Profile() != "" {
		t.Errorf("FPS %d, profile %q: a stream that does not decode has neither", src.FPS(), src.Profile())
	}

	_, live, cancel := src.Subscribe()
	defer cancel()
	stop()
	for range live {
	}
	var warnings []string
	for line := range strings.SplitSeq(logs.String(), "\n") {
		if strings.Contains(line, "level=WARN") {
			warnings = append(warnings, line)
		}
	}
	if len(warnings) != 1 {
		t.Fatalf("%d warnings, want exactly one:\n%s", len(warnings), logs.String())
	}
	if !strings.Contains(warnings[0], "source=real-1") || !strings.Contains(warnings[0], "not supported") {
		t.Errorf("warning %q should name the source and the decode error", warnings[0])
	}
	assertNoSecrets(t, "warning", warnings[0], addr)
}

func TestRTSP_KeepsTheFirstAccessUnitWhenItIsACompleteIDR(t *testing.T) {
	captureLogs(t)
	// One keyframe, then P frames, published once from the moment the client plays.
	aus := synthAUs(24, 24)
	hold, release := holdPublisher()
	addr := startRTSPServer(t, CodecH264, aus, 12, true, hold, publishOnce)

	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	src, err := NewRTSP(ctx, "real-1", cameraURL(addr))
	if err != nil {
		t.Fatal(err)
	}
	_, live, cancel := src.Subscribe()
	defer cancel()
	release()

	first := waitFor(t, live)
	if !first.IDR || !equalNALUs(first.NALUs, aus[0].NALUs) {
		t.Fatalf("first access unit = IDR %v, %x; want the keyframe of the stream, %x", first.IDR, first.NALUs, aus[0].NALUs)
	}
	if next := waitFor(t, live); next.IDR || !equalNALUs(next.NALUs, aus[1].NALUs) {
		t.Errorf("second access unit = IDR %v, %x; want frame 1, %x", next.IDR, next.NALUs, aus[1].NALUs)
	}
}

// onlyAU makes the source's depacketiser return nalus for the next packet.
func onlyAU(s *rtspSource, nalus [][]byte) {
	s.decode = func(*rtp.Packet) ([][]byte, error) { return nalus, nil }
}

func TestRTSP_DropsTheFirstAccessUnitUnlessItBeginsWithParameterSets(t *testing.T) {
	captureLogs(t)
	h264Keyframe := [][]byte{h264StreamSPS, h264StreamPPS, h264IDR}
	h265Keyframe := [][]byte{h265StoredVPS, h265StreamSPS, h265StoredPPS, h265IDR}
	tests := []struct {
		name     string
		source   func(*testing.T) *rtspSource
		first    [][]byte
		keyframe [][]byte // a complete keyframe of the codec, which ends any doubt
		kept     bool
	}{
		{"H.264 IDR with SPS and PPS", h264Source, h264Keyframe, h264Keyframe, true},
		{"H.264 IDR with AUD, SPS and PPS", h264Source, [][]byte{h264AUD, h264StreamSPS, h264StreamPPS, h264IDR}, h264Keyframe, true},
		{"H.264 IDR without parameter sets", h264Source, [][]byte{h264IDR}, h264Keyframe, false},
		{"H.264 IDR without PPS", h264Source, [][]byte{h264StreamSPS, h264IDR}, h264Keyframe, false},
		{"H.264 P frame", h264Source, [][]byte{h264NonIDR}, h264Keyframe, false},
		{"H.264 P frame with SPS and PPS", h264Source, [][]byte{h264StreamSPS, h264StreamPPS, h264NonIDR}, h264Keyframe, false},
		{"H.265 IDR with VPS, SPS and PPS", h265Source, h265Keyframe, h265Keyframe, true},
		{"H.265 IDR without VPS", h265Source, [][]byte{h265StreamSPS, h265StoredPPS, h265IDR}, h265Keyframe, false},
		{"H.265 IDR without parameter sets", h265Source, [][]byte{h265IDR}, h265Keyframe, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.source(t)
			packet := &rtp.Packet{Header: rtp.Header{Marker: true}}
			onlyAU(s, tc.first)
			s.onPacket(packet, 0)

			cached, _, cancel := s.Subscribe()
			defer cancel()
			if tc.kept {
				if len(cached) != 1 || !cached[0].IDR || !equalNALUs(cached[0].NALUs, tc.first) {
					t.Fatalf("cached = %+v, want the first access unit, %x", cached, tc.first)
				}
				return
			}
			if len(cached) != 0 {
				t.Fatalf("cached = %+v, want the first access unit dropped", cached)
			}
			// The stream goes on with the next keyframe.
			onlyAU(s, tc.keyframe)
			s.onPacket(packet, time.Second/12)
			cached, _, cancelAgain := s.Subscribe()
			defer cancelAgain()
			if len(cached) != 1 || !cached[0].IDR || !equalNALUs(cached[0].NALUs, tc.keyframe) {
				t.Errorf("cached = %+v, want the second access unit, %x", cached, tc.keyframe)
			}
		})
	}
}

func TestScrubber_RemovesIPLiterals(t *testing.T) {
	rawURL := cameraURL("camera.example:8554")
	u, err := base.ParseURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	scrub := newScrubber(rawURL, u)
	tests := []struct{ name, in, want string }{
		{
			"IPv4 dial error",
			"dial tcp 192.0.2.7:8554: connect: connection refused",
			"dial tcp <redacted>:<redacted>: connect: connection refused",
		},
		{
			"IPv6 dial error",
			"dial tcp [2001:db8::7]:8554: connect: connection refused",
			"dial tcp [<redacted>]:<redacted>: connect: connection refused",
		},
		{
			"IPv6 loopback",
			"dial tcp [::1]:8554: connect: connection refused",
			"dial tcp [<redacted>]:<redacted>: connect: connection refused",
		},
		{
			"IPv6 with a zone",
			"dial tcp [fe80::1%eth0]:8554: i/o timeout",
			"dial tcp [<redacted>]:<redacted>: i/o timeout",
		},
		{
			"IPv4-mapped IPv6",
			"dial tcp [::ffff:192.0.2.7]:8554: i/o timeout",
			"dial tcp [<redacted>]:<redacted>: i/o timeout",
		},
		{
			"both ends of a connection",
			"read tcp [2001:db8::1]:50000->[2001:db8::7]:8554: i/o timeout",
			"read tcp [<redacted>]:50000->[<redacted>]:<redacted>: i/o timeout",
		},
		{
			"the DNS server of a lookup",
			"lookup camera.example on 192.0.2.53:53: no such host",
			"lookup <redacted> on <redacted>:53: no such host",
		},
		{
			"text without an address",
			"unexpected status 401 at 12:30:45 after 1.5s: bad credentials",
			"unexpected status 401 at 12:30:45 after 1.5s: bad credentials",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scrub.text(errors.New(tc.in)); got != tc.want {
				t.Errorf("text(%q)\n = %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

// leakyError is an error whose text holds everything that must stay out of the
// logs: the camera URL, its credentials, its host and the IP address that the
// host resolved to.
func leakyError(addr string) error {
	return errors.New("dial tcp 192.0.2.7:8554: connect to " + cameraURL(addr) + " as " + testUser + ":" + testPass + " failed")
}

func TestRTSP_ClientCallbacksScrubTheirErrors(t *testing.T) {
	logs := captureLogs(t)
	const addr = "camera.example:8554"
	rawURL := cameraURL(addr)
	u, err := base.ParseURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	c := newRTSPClient(u, slog.Default().With("source", "real-1"), newScrubber(rawURL, u))

	c.OnDecodeError(leakyError(addr))
	c.OnTransportSwitch(leakyError(addr))
	c.OnPacketsLost(3)

	out := logs.String()
	for _, want := range []string{`msg="rtsp decode error"`, `msg="rtsp transport switch"`, `msg="rtsp packets lost"`, "source=real-1", "<redacted>"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output lacks %s:\n%s", want, out)
		}
	}
	assertNoSecrets(t, "log output", out, addr, "192.0.2.7")
}

func TestRTSPSource_WarnsAboutTheFirstDecodeErrorScrubbed(t *testing.T) {
	logs := captureLogs(t)
	const addr = "camera.example:8554"
	s := h264Source(t)
	s.decode = func(*rtp.Packet) ([][]byte, error) { return nil, leakyError(addr) }

	for range 3 {
		s.onPacket(&rtp.Packet{Header: rtp.Header{Marker: true}}, 0)
	}

	out := logs.String()
	if n := strings.Count(out, "level=WARN"); n != 1 {
		t.Errorf("%d warnings for three decode errors, want 1:\n%s", n, out)
	}
	if n := strings.Count(out, "level=DEBUG"); n != 2 {
		t.Errorf("%d debug lines for three decode errors, want 2:\n%s", n, out)
	}
	assertNoSecrets(t, "log output", out, addr, "192.0.2.7")
}
