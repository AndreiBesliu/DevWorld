package source

import (
	"bytes"
	"context"
	"log/slog"
	"net"
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
// buffer until the test ends.
func captureLogs(t *testing.T) *logBuffer {
	t.Helper()
	b := &logBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug})))
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
}

// requireLogin makes the test server ask for credentials, as a real camera does.
func requireLogin(user, pass string) func(*testServerHandler) {
	return func(h *testServerHandler) { h.user, h.pass = user, pass }
}

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

// startRTSPServer serves aus in a loop over RTSP on the loopback interface and
// returns its address. The parameter sets are in the SDP; they stay in the
// stream only when inBand is true.
func startRTSPServer(t *testing.T, codec Codec, aus []AccessUnit, fps int, inBand bool, opts ...func(*testServerHandler)) string {
	t.Helper()
	handler := &testServerHandler{status: base.StatusOK}
	for _, opt := range opts {
		opt(handler)
	}
	server := &gortsplib.Server{Handler: handler, RTSPAddress: "127.0.0.1:0"}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)

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
	desc := &description.Session{Medias: []*description.Media{{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{forma},
	}}}
	stream := &gortsplib.ServerStream{Server: server, Desc: desc}
	if err := stream.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stream.Close)
	handler.mu.Lock()
	handler.stream = stream
	handler.mu.Unlock()

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	t.Cleanup(func() {
		stop()
		<-done
	})
	go func() {
		defer close(done)
		step := uint32(90000 / fps) //nolint:gosec // fps is 12 or 20
		for i := 0; ctx.Err() == nil; i++ {
			nalus := aus[i%len(aus)].NALUs
			if !inBand {
				nalus = withoutParameterSets(codec, nalus)
			}
			pkts, err := encode(nalus)
			if err != nil {
				return
			}
			for _, p := range pkts {
				p.Timestamp = 1000 + uint32(i)*step
				if stream.WritePacketRTP(desc.Medias[0], p) != nil {
					return
				}
			}
			select {
			case <-ctx.Done():
			case <-time.After(8 * time.Millisecond):
			}
		}
	}()

	return server.NetListener().Addr().String()
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

// assertNoSecrets fails when text contains any part of the camera URL, or one
// of the extra secrets.
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
	assertNoSecrets(t, "log output", logs.String(), addr)
	assertNoSecrets(t, "log output", logs.String(), serverAddr)
}

func TestRTSP_ContextCancelEndsTheSource(t *testing.T) {
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
	desc := &description.Session{Medias: []*description.Media{{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{&format.H264{PayloadTyp: 96, PacketizationMode: 1}},
	}}}
	s, _, _, err := newRTSPSource("real-1", slog.Default(), desc)
	if err != nil {
		t.Fatal(err)
	}
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
