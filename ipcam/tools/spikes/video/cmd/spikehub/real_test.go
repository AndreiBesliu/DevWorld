package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
)

// The pieces of the camera URL that must never reach a log, a response or a
// results file (PH0-22).
const (
	camUser  = "user"
	camPass  = "pw"
	camPath  = "secret-path/stream1"
	camQuery = "token=t0k3n"
)

// cameraServer is an RTSP server on the loopback interface that plays one
// H.264 stream and asks for the credentials of the URL, as a real camera does.
type cameraServer struct {
	stream *gortsplib.ServerStream
	addr   string
}

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

// startCamera starts the camera, which sends a keyframe every 10 frames.
func startCamera(t *testing.T) *cameraServer {
	t.Helper()
	sps := []byte{0x67, 0x4d, 0x40, 0x1f, 0xac, 0xd9}
	pps := []byte{0x68, 0xee, 0x3c, 0x80}
	forma := &format.H264{PayloadTyp: 96, PacketizationMode: 1, SPS: sps, PPS: pps}
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
				nalus = [][]byte{sps, pps, {0x65, 0x88, 0x80, 0xff}}
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

// assertNoSecrets fails when text contains a part of the camera URL or the
// camera's address.
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
}

func TestRealURL_NotLoggedNorReturned(t *testing.T) {
	cam := startCamera(t)
	asked := 0
	h := startHub(t, harnessOptions{
		args: []string{"-real"},
		askSecret: func(string) (string, error) {
			asked++
			if asked == 1 {
				return cam.url(), nil
			}
			return "", nil
		},
	})

	resp, cameras := h.get(t, "/spike/v1/cameras")
	if resp.StatusCode != 200 || !strings.Contains(string(cameras), `{"id":"real-1","camera":19,"codec":"H264","profiles":["main","sub"]}`) {
		t.Fatalf("cameras: status %d: %s", resp.StatusCode, cameras)
	}
	assertNoSecrets(t, "/spike/v1/cameras", string(cameras), cam.addr)

	// A results file has no way to hold the URL either, but its response and name come from the hub.
	postResp, err := postJSON(t, h.url+"/spike/v1/results", `{"device":"x","scenario":"S1"}`)
	if err != nil || !strings.Contains(postResp, "x-") {
		t.Fatalf("results: %v: %s", err, postResp)
	}
	assertNoSecrets(t, "the results response", postResp, cam.addr)
	for _, name := range resultFiles(t, h.resultsDir) {
		data, err := os.ReadFile(filepath.Join(h.resultsDir, name)) //nolint:gosec // a file in the test's results folder
		if err != nil {
			t.Fatal(err)
		}
		assertNoSecrets(t, name, string(data), cam.addr)
	}

	if err := h.stop(t); err != nil {
		t.Fatal(err)
	}
	// The hub says that it has a real camera, by its name.
	if out := h.stdout.String(); !strings.Contains(out, "real-1") {
		t.Errorf("the banner does not mention real-1:\n%s", out)
	}
	assertNoSecrets(t, "stdout", h.stdout.String(), cam.addr)
	assertNoSecrets(t, "stderr and the logs", h.stderr.String(), cam.addr)
	if ip := ipv4Literal.FindString(h.stderr.String()); ip != "" {
		t.Errorf("the logs contain the IP address %q:\n%s", ip, h.stderr.String())
	}
	if asked != 2 {
		t.Errorf("the prompt was shown %d times, want 2 (the URL, then Enter)", asked)
	}
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

	var prompts []string
	h := startHub(t, harnessOptions{
		args: []string{"-real"},
		askSecret: func(prompt string) (string, error) {
			prompts = append(prompts, prompt)
			if len(prompts) == 1 {
				return badURL, nil
			}
			return "", nil
		},
	})
	_, cameras := h.get(t, "/spike/v1/cameras")
	if strings.Contains(string(cameras), "real-1") {
		t.Errorf("a camera that did not connect is listed: %s", cameras)
	}
	if err := h.stop(t); err != nil {
		t.Fatal(err)
	}

	out := h.stdout.String() + h.stderr.String()
	if !strings.Contains(out, "real-1") || !strings.Contains(out, "a eșuat") {
		t.Errorf("the failure of real-1 is not reported in Romanian:\n%s", out)
	}
	assertNoSecrets(t, "the output", out, addr)
	// After a failure the same camera is asked for again, so that a typo costs nothing.
	if len(prompts) != 2 || !strings.Contains(prompts[0], "real-1") || !strings.Contains(prompts[1], "real-1") {
		t.Errorf("prompts = %q, want real-1 asked for twice", prompts)
	}
}

// fakeConnect returns a connect function that makes a silent simulator named
// after the camera, and records the URLs it got.
func fakeConnect(urls *[]string) func(context.Context, string, string) (source.Source, error) {
	return func(_ context.Context, id, rawURL string) (source.Source, error) {
		*urls = append(*urls, rawURL)
		if strings.HasPrefix(rawURL, "bad") {
			return nil, errors.New("rtsp " + id + ": connection refused")
		}
		return source.NewSim(id, 0, oneAU(), 20, 0, newFakeClock()), nil
	}
}

// answers returns an ask function that gives the lines in turn, then io.EOF, and
// the prompts it was shown.
func answers(lines ...string) (func(string) (string, error), *[]string) {
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

func TestAddRealCameras_AsksForAtMostFourURLs(t *testing.T) {
	cat, _ := newTestCatalog(t, 1)
	ask, prompts := answers("rtsp://a/1", "rtsp://a/2", "rtsp://a/3", "rtsp://a/4", "rtsp://a/5")
	var urls []string

	err := addRealCameras(t.Context(), cat, ask, io.Discard, fakeConnect(&urls))
	if err != nil {
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
	var ids []string
	for _, c := range cat.cameras[len(cat.cameras)-4:] {
		ids = append(ids, fmt.Sprintf("%s=%d", c.ID, c.Camera))
	}
	if want := "real-1=19,real-2=20,real-3=21,real-4=22"; strings.Join(ids, ",") != want {
		t.Errorf("real cameras = %v, want %s", ids, want)
	}
}

func TestAddRealCameras_AnEmptyLineEndsThePrompts(t *testing.T) {
	cat, _ := newTestCatalog(t, 1)
	before := len(cat.cameras)
	ask, prompts := answers("  rtsp://a/1  ", "  ", "rtsp://a/never")
	var urls []string

	if err := addRealCameras(t.Context(), cat, ask, io.Discard, fakeConnect(&urls)); err != nil {
		t.Fatal(err)
	}
	if len(*prompts) != 2 || len(cat.cameras) != before+1 {
		t.Errorf("%d prompts, %d new cameras; want 2 and 1", len(*prompts), len(cat.cameras)-before)
	}
	if len(urls) != 1 || urls[0] != "rtsp://a/1" {
		t.Errorf("connected to %q, want the one URL, trimmed", urls)
	}
}

func TestAddRealCameras_ARetryKeepsTheName(t *testing.T) {
	cat, _ := newTestCatalog(t, 1)
	ask, prompts := answers("bad-url", "rtsp://a/1", "")
	var urls []string
	var out strings.Builder

	if err := addRealCameras(t.Context(), cat, ask, &out, fakeConnect(&urls)); err != nil {
		t.Fatal(err)
	}
	if last := cat.cameras[len(cat.cameras)-1]; last.ID != "real-1" {
		t.Errorf("last camera = %q, want real-1 after the retry", last.ID)
	}
	if len(*prompts) != 3 || !strings.Contains((*prompts)[1], "real-1") || !strings.Contains((*prompts)[2], "real-2") {
		t.Errorf("prompts = %q", *prompts)
	}
	if !strings.Contains(out.String(), "real-1") || strings.Contains(out.String(), "bad-url") {
		t.Errorf("output = %q, want the failure of real-1 without the URL", out.String())
	}
}

func TestAddRealCameras_EndOfInputEndsThePromptsAndOtherErrorsFail(t *testing.T) {
	cat, _ := newTestCatalog(t, 1)
	ask, _ := answers("rtsp://a/1") // then io.EOF
	var urls []string
	if err := addRealCameras(t.Context(), cat, ask, io.Discard, fakeConnect(&urls)); err != nil {
		t.Errorf("end of input: %v, want the cameras so far to stand", err)
	}
	if last := cat.cameras[len(cat.cameras)-1]; last.ID != "real-1" {
		t.Errorf("last camera = %q, want real-1", last.ID)
	}

	boom := errors.New("terminal gone")
	err := addRealCameras(t.Context(), cat, func(string) (string, error) { return "", boom }, io.Discard, fakeConnect(&urls))
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the error of the terminal", err)
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

func TestWarnWhenSilent_SaysSoWhenNoAccessUnitArrives(t *testing.T) {
	clock := newFakeClock()
	silent := source.NewSim("real-2", 0, oneAU(), 20, 0, clock) // its clock never ticks
	var out syncBuffer

	warnWhenSilent(t.Context(), &out, "real-2", silent, 20*time.Millisecond)

	text := out.String()
	if !strings.Contains(text, "real-2") || !strings.Contains(text, "Atenție") || !strings.Contains(text, "video") {
		t.Errorf("warning = %q, want a Romanian warning that names real-2", text)
	}
}

func TestWarnWhenSilent_StaysQuietWhenFramesArrive(t *testing.T) {
	clock := newFakeClock()
	src := source.NewSim("real-1", 0, oneAU(), 20, 0, clock)
	var out syncBuffer
	var wg sync.WaitGroup
	wg.Go(func() { warnWhenSilent(t.Context(), &out, "real-1", src, 10*time.Second) })

	clock.tickAll(t) // the first frame; the watcher returns without waiting for the timeout
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not return after the first frame")
	}
	if out.String() != "" {
		t.Errorf("warning = %q for a camera that sends frames", out.String())
	}
}

func TestWarnWhenSilent_StaysQuietWhenTheSourceAlreadyHasFrames(t *testing.T) {
	clock := newFakeClock()
	src := source.NewSim("real-1", 0, oneAU(), 20, 0, clock)
	clock.tickAll(t)
	waitFor(t, src.SentLog())
	var out syncBuffer

	warnWhenSilent(t.Context(), &out, "real-1", src, time.Hour) // returns at once: the GOP cache has the frame

	if out.String() != "" {
		t.Errorf("warning = %q", out.String())
	}
}

func TestWarnWhenSilent_StopsWithTheContext(t *testing.T) {
	src := source.NewSim("real-1", 0, oneAU(), 20, 0, newFakeClock())
	ctx, cancel := context.WithCancel(t.Context())
	var out syncBuffer
	done := make(chan struct{})
	go func() {
		warnWhenSilent(ctx, &out, "real-1", src, time.Hour)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not stop with the context")
	}
	if out.String() != "" {
		t.Errorf("warning = %q after a shutdown", out.String())
	}
}
