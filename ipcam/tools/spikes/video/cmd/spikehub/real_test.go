package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
	"github.com/pion/webrtc/v4"
)

// The pieces of the camera URL that must never reach a log, a response or a
// results file (PH0-22).
const (
	camUser  = "user"
	camPass  = "pw"
	camPath  = "secret-path/stream1"
	camQuery = "token=t0k3n"
)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// The parameter sets of the simulated cameras' media, as cmd/mediagen's
// ffmpeg writes them: the sub stream (H.264 Main@3.1, 640x360) and the main
// stream of mx-h265 (H.265 Main@4.1, 1920x1080).
var (
	subSPS  = mustHex("674d401fda0280bfe584000003000400000300603c60ca80")
	subPPS  = []byte{0x68, 0xee, 0x3c, 0x80}
	h265VPS = mustHex("40010c01ffff01600000030090000003000003007bba0240")
	h265SPS = mustHex("42010101600000030090000003000003007ba003c0801107cb96e92930b9a02000000300200000030281")
	h265PPS = mustHex("4401c073c189")
)

// cameraServer is an RTSP server on the loopback interface that plays one
// H.264 stream and asks for the credentials of the URL, as a real camera does.
// It counts the connections that clients open and close.
type cameraServer struct {
	stream         *gortsplib.ServerStream
	addr           string
	opened, closed atomic.Int32
}

func (c *cameraServer) OnConnOpen(*gortsplib.ServerHandlerOnConnOpenCtx)   { c.opened.Add(1) }
func (c *cameraServer) OnConnClose(*gortsplib.ServerHandlerOnConnCloseCtx) { c.closed.Add(1) }

// openConns is the number of client connections that are open.
func (c *cameraServer) openConns() int32 { return c.opened.Load() - c.closed.Load() }

func (c *cameraServer) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !ctx.Conn.VerifyCredentials(ctx.Request, camUser, camPass) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	return &base.Response{StatusCode: base.StatusOK}, c.stream, nil
}

func (c *cameraServer) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !ctx.Conn.VerifyCredentials(ctx.Request, camUser, camPass) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	return &base.Response{StatusCode: base.StatusOK}, c.stream, nil
}

func (c *cameraServer) OnPlay(*gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	return &base.Response{StatusCode: base.StatusOK}, nil
}

// startCamera starts the camera. It sends the sub stream's parameter sets and
// a keyframe every 10 frames, or, when silent, it plays but sends nothing.
func startCamera(t *testing.T, silent bool) *cameraServer {
	t.Helper()
	forma := &format.H264{PayloadTyp: 96, PacketizationMode: 1, SPS: subSPS, PPS: subPPS}
	enc, err := forma.CreateEncoder()
	if err != nil {
		t.Fatal(err)
	}
	cam := &cameraServer{}
	server := &gortsplib.Server{Handler: cam, RTSPAddress: "127.0.0.1:0"}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	medi := &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{forma}}
	cam.stream = &gortsplib.ServerStream{Server: server, Desc: &description.Session{Medias: []*description.Media{medi}}}
	if err := cam.stream.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cam.stream.Close)
	cam.addr = server.NetListener().Addr().String()
	if silent {
		return cam
	}

	ctx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	t.Cleanup(func() {
		stop()
		<-done
	})
	go func() {
		defer close(done)
		for i := 0; ctx.Err() == nil; i++ {
			nalus := [][]byte{{0x41, 0x9a, 0x80, 0xff}}
			if i%10 == 0 {
				nalus = [][]byte{subSPS, subPPS, {0x65, 0x88, 0x80, 0xff}}
			}
			pkts, err := enc.Encode(nalus)
			if err != nil {
				return
			}
			for _, p := range pkts {
				p.Timestamp = 1000 + uint32(i)*4500 //nolint:gosec // i counts the frames of a short test
				if cam.stream.WritePacketRTP(medi, p) != nil {
					return
				}
			}
			select {
			case <-ctx.Done():
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	return cam
}

func (c *cameraServer) url() string {
	return "rtsp://" + camUser + ":" + camPass + "@" + c.addr + "/" + camPath + "?" + camQuery
}

// ipv4Literal matches an IPv4 address.
var ipv4Literal = regexp.MustCompile(`\d{1,3}(?:\.\d{1,3}){3}`)

// assertNoSecrets fails when text contains a part of the camera URL, the
// camera's address or any IPv4 address.
func assertNoSecrets(t *testing.T, what, text, camAddr string) {
	t.Helper()
	host, _, err := net.SplitHostPort(camAddr)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{camUser + ":", camPass, camPath, "stream1", camQuery, "t0k3n", camAddr, host + ":", "rtsp://"} {
		if strings.Contains(text, secret) {
			t.Errorf("%s contains %q:\n%s", what, secret, text)
		}
	}
	if ip := ipv4Literal.FindString(text); ip != "" {
		t.Errorf("%s contains the IP address %q:\n%s", what, ip, text)
	}
}

// probeRun is a run of spikehub -real against test cameras.
type probeRun struct {
	stdout, stderr *syncBuffer
	prompts        []string
	err            error
	// openAtEnter is, per camera, the connections it had open when the
	// probe asked for the final Enter.
	openAtEnter []int32
}

// runProbe runs spikehub -real with the given answers to its prompts, then
// an Enter for «Apăsați Enter pentru a închide.». The hub must not listen,
// start WebRTC or read media: the probe serves nothing.
func runProbe(t *testing.T, frameTimeout time.Duration, cams []*cameraServer, answers ...string) *probeRun {
	t.Helper()
	captureLogs(t) // restores the default logger, which run replaces
	r := &probeRun{stdout: &syncBuffer{}, stderr: &syncBuffer{}}
	ask := func(prompt string) (string, error) {
		r.prompts = append(r.prompts, prompt)
		if len(answers) > 0 {
			line := answers[0]
			answers = answers[1:]
			return line, nil
		}
		// The probe has closed its connections by now; the camera sees each
		// close a moment later. The hub waits at this prompt, so a close seen
		// here is the probe's, not the end of the process.
		for _, cam := range cams {
			deadline := time.Now().Add(5 * time.Second)
			for cam.openConns() != 0 && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			r.openAtEnter = append(r.openAtEnter, cam.openConns())
		}
		return "", nil
	}
	e := env{
		stdout:    r.stdout,
		stderr:    r.stderr,
		askSecret: ask,
		listen: func(int) (net.Listener, error) {
			t.Error("spikehub -real opened its HTTP port")
			return nil, errors.New("the probe must not listen")
		},
		newAPI: func(bool) (*webrtc.API, bool, error) {
			t.Error("spikehub -real started WebRTC")
			return nil, false, errors.New("the probe must not start WebRTC")
		},
		realFrameTimeout: frameTimeout,
	}
	done := make(chan error, 1)
	// No media folder: the probe does not need one.
	go func() { done <- run(t.Context(), []string{"-real", "-media", filepath.Join(t.TempDir(), "none")}, e) }()
	select {
	case r.err = <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("spikehub -real did not end")
	}
	return r
}

// checkClosedBeforeEnter fails unless the probe ended with the Enter prompt
// and the cameras had no connection open then.
func (r *probeRun) checkClosedBeforeEnter(t *testing.T) {
	t.Helper()
	if n := len(r.prompts); n == 0 || r.prompts[n-1] != "Apăsați Enter pentru a închide." {
		t.Errorf("prompts = %q, want «Apăsați Enter pentru a închide.» last", r.prompts)
	}
	for i, open := range r.openAtEnter {
		if open != 0 {
			t.Errorf("camera %d had %d RTSP connections open when the probe asked for Enter, want 0", i+1, open)
		}
	}
}

func TestRealProbe_PrintsCodecProfileAndResolutionAndClosesTheCamera(t *testing.T) {
	cam := startCamera(t, false)

	r := runProbe(t, 10*time.Second, []*cameraServer{cam}, cam.url(), "")

	if r.err != nil {
		t.Fatalf("run: %v\nstdout: %s\nstderr: %s", r.err, r.stdout, r.stderr)
	}
	const want = "Camera real-1 este conectată: H264, profil 4d401f, 640x360."
	// Once when the camera answers, once in the summary for the chat.
	if n := strings.Count(r.stdout.String(), want); n != 2 {
		t.Errorf("stdout has %q %d times, want 2:\n%s", want, n, r.stdout)
	}
	if cam.opened.Load() == 0 {
		t.Error("the probe never connected to the camera")
	}
	r.checkClosedBeforeEnter(t)
}

func TestRealProbe_TimesOutCleanlyOnACameraThatSendsNothing(t *testing.T) {
	cam := startCamera(t, true)

	start := time.Now()
	r := runProbe(t, 300*time.Millisecond, []*cameraServer{cam}, cam.url(), "")

	if r.err != nil {
		t.Fatalf("run: %v\nstdout: %s\nstderr: %s", r.err, r.stdout, r.stderr)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("the probe took %v for a camera that sends nothing, with a 300 ms timeout", took)
	}
	const want = "Conectarea la camera real-1 a eșuat: camera răspunde, dar nu a trimis niciun cadru video decodabil în 0.3 secunde."
	if n := strings.Count(r.stdout.String(), want); n != 2 {
		t.Errorf("stdout has %q %d times, want 2:\n%s", want, n, r.stdout)
	}
	if !strings.Contains(r.stdout.String(), "H.264 sau H.265") {
		t.Errorf("stdout does not say which video the hub can read:\n%s", r.stdout)
	}
	if cam.opened.Load() == 0 {
		t.Error("the probe never connected to the camera")
	}
	r.checkClosedBeforeEnter(t)
}

func TestRealURL_NotLoggedNorReturned(t *testing.T) {
	cam := startCamera(t, false)
	silent := startCamera(t, true)
	// A port that nothing listens on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := ln.Addr().String()
	_ = ln.Close()
	refusedURL := "rtsp://" + camUser + ":" + camPass + "@" + refused + "/" + camPath + "?" + camQuery

	// real-2 fails on the silent camera and is asked for again; the refused URL
	// fails as real-2 too.
	r := runProbe(t, 300*time.Millisecond, []*cameraServer{cam, silent}, cam.url(), silent.url(), refusedURL, "")

	if r.err != nil {
		t.Fatalf("run: %v", r.err)
	}
	for _, addr := range []string{cam.addr, silent.addr, refused} {
		assertNoSecrets(t, "stdout", r.stdout.String(), addr)
		assertNoSecrets(t, "stderr and the logs", r.stderr.String(), addr)
		assertNoSecrets(t, "the prompts", strings.Join(r.prompts, "\n"), addr)
	}
	out := r.stdout.String()
	for _, want := range []string{
		"Camera real-1 este conectată",
		"Conectarea la camera real-2 a eșuat: camera răspunde",
		"Conectarea la camera real-2 a eșuat: rtsp real-2: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	r.checkClosedBeforeEnter(t)
}

func TestRealURL_FailedConnectionNamesNoURL(t *testing.T) {
	// A port that nothing listens on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	badURL := "rtsp://" + camUser + ":" + camPass + "@" + addr + "/" + camPath + "?" + camQuery

	r := runProbe(t, time.Second, nil, badURL, "")

	out := r.stdout.String() + r.stderr.String()
	if !strings.Contains(r.stdout.String(), "Conectarea la camera real-1 a eșuat: ") {
		t.Errorf("the failure of real-1 is not reported in Romanian:\n%s", out)
	}
	assertNoSecrets(t, "the output", out, addr)
	// After a failure the same camera is asked for again, so that a typo costs nothing.
	if len(r.prompts) != 3 || !strings.Contains(r.prompts[0], "real-1") || !strings.Contains(r.prompts[1], "real-1") {
		t.Errorf("prompts = %q, want real-1 asked for twice, then Enter", r.prompts)
	}
	r.checkClosedBeforeEnter(t)
}

// fakeCamera is a real camera as the probe sees it: a source whose first
// access unit is a keyframe, or that sends nothing when silent, and that ends
// when the context of its connection does.
type fakeCamera struct {
	id     string
	silent bool
	ctx    context.Context // the connection's, as source.NewRTSP keeps it
}

func (f *fakeCamera) ID() string                            { return f.id }
func (f *fakeCamera) Codec() source.Codec                   { return source.CodecH264 }
func (f *fakeCamera) Profile() string                       { return "4d401f" }
func (f *fakeCamera) FPS() int                              { return 0 }
func (f *fakeCamera) SentLog() <-chan source.SentRecord     { return nil }
func (f *fakeCamera) Jitter() time.Duration                 { return 0 }
func (f *fakeCamera) JitterSnapshot() source.JitterSnapshot { return source.JitterSnapshot{} }

// Subscribe returns the keyframe as the cached GOP, unless the camera is
// silent, and a channel that closes when the connection ends.
func (f *fakeCamera) Subscribe() ([]source.AccessUnit, <-chan source.AccessUnit, func()) {
	ch := make(chan source.AccessUnit)
	var once sync.Once
	end := func() { once.Do(func() { close(ch) }) }
	go func() {
		<-f.ctx.Done()
		end()
	}()
	if f.silent {
		return nil, ch, end
	}
	return []source.AccessUnit{{NALUs: [][]byte{subSPS, subPPS, {0x65, 0x88}}, IDR: true}}, ch, end
}

// fakeConnect returns a connect function that makes a fake camera named after
// the camera, and records the URLs it got. A URL that starts with "bad" does
// not connect, and one that starts with "silent" sends nothing.
func fakeConnect(urls *[]string) connectFunc {
	return func(ctx context.Context, id, rawURL string) (source.Source, error) {
		*urls = append(*urls, rawURL)
		if strings.HasPrefix(rawURL, "bad") {
			return nil, errors.New("rtsp " + id + ": connection refused")
		}
		return &fakeCamera{id: id, silent: strings.HasPrefix(rawURL, "silent"), ctx: ctx}, nil
	}
}

// answers returns an ask function that gives the lines in turn, then io.EOF, and
// the prompts it was shown.
func answers(lines ...string) (askFunc, *[]string) {
	var prompts []string
	return func(prompt string) (string, error) {
		prompts = append(prompts, prompt)
		if len(lines) == 0 {
			return "", io.EOF
		}
		line := lines[0]
		lines = lines[1:]
		return line, nil
	}, &prompts
}

// summary returns the lines under the heading «Rezumat …» of the probe's output.
func summary(t *testing.T, out string) []string {
	t.Helper()
	_, after, ok := strings.Cut(out, "Rezumat")
	if !ok {
		t.Fatalf("no summary in:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(after), "\n")
	return lines[1:] // lines[0] is the rest of the heading
}

func TestProbeRealCameras_AsksForAtMostFourURLs(t *testing.T) {
	ask, prompts := answers("rtsp://a/1", "rtsp://a/2", "rtsp://a/3", "rtsp://a/4", "rtsp://a/5")
	var urls []string
	var out strings.Builder

	if err := probeRealCameras(t.Context(), ask, &out, fakeConnect(&urls), time.Second); err != nil {
		t.Fatal(err)
	}
	if len(*prompts) != 4 || len(urls) != 4 {
		t.Fatalf("%d prompts, %d connections; want 4 and 4", len(*prompts), len(urls))
	}
	for i, p := range *prompts {
		if !strings.Contains(p, fmt.Sprintf("real-%d", i+1)) {
			t.Errorf("prompt %d = %q, want it to name real-%d", i, p, i+1)
		}
	}
	got := summary(t, out.String())
	if len(got) != 4 {
		t.Fatalf("summary = %q, want 4 lines", got)
	}
	for i, line := range got {
		if want := fmt.Sprintf("Camera real-%d este conectată: H264, profil 4d401f, 640x360.", i+1); line != want {
			t.Errorf("summary line %d = %q, want %q", i, line, want)
		}
	}
}

func TestProbeRealCameras_AnEmptyLineEndsThePrompts(t *testing.T) {
	ask, prompts := answers("  rtsp://a/1  ", "  ", "rtsp://a/never")
	var urls []string
	var out strings.Builder

	if err := probeRealCameras(t.Context(), ask, &out, fakeConnect(&urls), time.Second); err != nil {
		t.Fatal(err)
	}
	if len(*prompts) != 2 {
		t.Errorf("%d prompts, want 2", len(*prompts))
	}
	if len(urls) != 1 || urls[0] != "rtsp://a/1" {
		t.Errorf("connected to %q, want the one URL, trimmed", urls)
	}
	if got := summary(t, out.String()); len(got) != 1 {
		t.Errorf("summary = %q, want one camera", got)
	}
}

func TestProbeRealCameras_ARetryKeepsTheName(t *testing.T) {
	ask, prompts := answers("bad-url", "rtsp://a/1", "")
	var urls []string
	var out strings.Builder

	if err := probeRealCameras(t.Context(), ask, &out, fakeConnect(&urls), time.Second); err != nil {
		t.Fatal(err)
	}
	if len(*prompts) != 3 || !strings.Contains((*prompts)[1], "real-1") || !strings.Contains((*prompts)[2], "real-2") {
		t.Errorf("prompts = %q", *prompts)
	}
	text := out.String()
	if !strings.Contains(text, "Conectarea la camera real-1 a eșuat: rtsp real-1: connection refused.") || strings.Contains(text, "bad-url") {
		t.Errorf("output = %q, want the failure of real-1 without the URL", text)
	}
	// The summary has the outcome of the last attempt only.
	if got := summary(t, text); len(got) != 1 || got[0] != "Camera real-1 este conectată: H264, profil 4d401f, 640x360." {
		t.Errorf("summary = %q, want real-1 connected", got)
	}
}

func TestProbeRealCameras_AFailureThatIsNotRetriedStaysInTheSummary(t *testing.T) {
	ask, _ := answers("rtsp://a/1", "silent-url", "")
	var urls []string
	var out strings.Builder

	if err := probeRealCameras(t.Context(), ask, &out, fakeConnect(&urls), 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Camera real-1 este conectată: H264, profil 4d401f, 640x360.",
		"Conectarea la camera real-2 a eșuat: camera răspunde, dar nu a trimis niciun cadru video decodabil în 0.05 secunde.",
	}
	if got := summary(t, out.String()); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

func TestProbeRealCameras_WithoutAnyURLSaysSo(t *testing.T) {
	ask, _ := answers("")
	var out strings.Builder
	if err := probeRealCameras(t.Context(), ask, &out, fakeConnect(new([]string)), time.Second); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Nu ați introdus nicio adresă de cameră.") {
		t.Errorf("output = %q", out.String())
	}
}

func TestProbeRealCameras_EndOfInputEndsThePromptsAndOtherErrorsFail(t *testing.T) {
	ask, _ := answers("rtsp://a/1") // then io.EOF
	var urls []string
	var out strings.Builder
	if err := probeRealCameras(t.Context(), ask, &out, fakeConnect(&urls), time.Second); err != nil {
		t.Errorf("end of input: %v, want the cameras so far to stand", err)
	}
	if got := summary(t, out.String()); len(got) != 1 {
		t.Errorf("summary = %q, want real-1", got)
	}

	boom := errors.New("terminal gone")
	err := probeRealCameras(t.Context(), func(string) (string, error) { return "", boom }, io.Discard, fakeConnect(&urls), time.Second)
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the error of the terminal", err)
	}
}

func TestResolutionOf_ReadsTheSPSOfBothCodecs(t *testing.T) {
	tests := []struct {
		name  string
		codec source.Codec
		nalus [][]byte
		want  string
	}{
		{"H.264", source.CodecH264, [][]byte{subSPS, subPPS, {0x65, 0x88}}, "640x360"},
		{"H.265", source.CodecH265, [][]byte{h265VPS, h265SPS, h265PPS, {0x26, 0x01, 0xaf}}, "1920x1080"},
		{"an SPS that does not parse", source.CodecH264, [][]byte{{0x67, 0x4d, 0x40, 0x1f, 0xac, 0xd9}, subPPS, {0x65, 0x88}}, "rezoluție necunoscută"},
		{"no SPS", source.CodecH264, [][]byte{{0x65, 0x88}}, "rezoluție necunoscută"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolutionOf(tc.codec, source.AccessUnit{NALUs: tc.nalus, IDR: true}); got != tc.want {
				t.Errorf("resolution = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTerminalAsker_RefusesAnInputThatIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	_, err = newTerminalAsker(t.Context(), r, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Errorf("newTerminalAsker on a pipe: %v, want an error that says a terminal is needed", err)
	}
}
