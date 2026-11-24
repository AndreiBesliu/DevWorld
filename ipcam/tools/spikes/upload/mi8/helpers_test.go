package mi8

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
)

// The test binary stands in for ffmpeg when it runs under that name: FFmpeg
// gets an empty environment, so its mode and its dump live next to it.
func TestMain(m *testing.M) {
	if name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe"); name == "ffmpeg" {
		os.Exit(fakeFFmpeg())
	}
	code := m.Run()
	if binDir != "" {
		_ = os.RemoveAll(binDir)
	}
	os.Exit(code)
}

// fakeDump is what the stand-in ffmpeg saw: its argv and environment, and
// how many bytes its stdin carried.
type fakeDump struct {
	Args  []string `json:"args"`
	Env   []string `json:"env"`
	Bytes int64    `json:"bytes"`
}

// fakeFFmpeg is the stand-in ffmpeg: it dumps its argv and environment to
// dump.json beside itself, and, once its stdin carried bytes, writes a
// 160 × 90 gray frame every 500 ms until its stdin ends: with mode
// "motion" each frame differs from the last, with "static" none does, with
// "silent" it writes none.
func fakeFFmpeg() int {
	dir := filepath.Dir(os.Args[0])
	mode, _ := os.ReadFile(filepath.Join(dir, "mode")) //nolint:gosec // the test's own file
	var n atomic.Int64
	done := make(chan struct{})
	dump := func() {
		b, _ := json.Marshal(fakeDump{Args: os.Args[1:], Env: os.Environ(), Bytes: n.Load()})
		_ = os.WriteFile(filepath.Join(dir, "dump.json"), b, 0o600) //nolint:gosec // beside the stand-in
	}
	dump()
	go func() {
		defer close(done)
		buf := make([]byte, 32<<10)
		for {
			k, err := os.Stdin.Read(buf)
			n.Add(int64(k))
			if err != nil {
				return
			}
		}
	}()
	out := bufio.NewWriter(os.Stdout)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for i := 0; ; i++ {
		select {
		case <-done:
			dump()
			return 0
		case <-tick.C:
		}
		dump()
		if n.Load() == 0 || strings.TrimSpace(string(mode)) == "silent" {
			continue
		}
		frame := make([]byte, FrameSize)
		if strings.TrimSpace(string(mode)) == "motion" && i%2 == 1 {
			for j := range frame {
				frame[j] = 200
			}
		}
		if _, err := out.Write(frame); err != nil {
			return 0
		}
		if out.Flush() != nil {
			return 0
		}
	}
}

// installFakeFFmpeg copies the test binary as ffmpeg into a folder of its
// own, in mode, and returns its path and its dump's.
func installFakeFFmpeg(t *testing.T, mode string) (bin, dump string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "ffmpeg")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	self, err := os.ReadFile(os.Args[0]) //nolint:gosec // the test binary as the stand-in
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, self, 0o700); err != nil { //nolint:gosec // the stand-in ffmpeg
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mode"), []byte(mode), 0o600); err != nil {
		t.Fatal(err)
	}
	return bin, filepath.Join(dir, "dump.json")
}

// readDump reads the stand-in ffmpeg's dump.
func readDump(t *testing.T, path string) fakeDump {
	t.Helper()
	var d fakeDump
	b, err := os.ReadFile(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// rtspCam is a test RTSP camera: one H.264 stream of access units, each
// an IDR with its parameter sets, at 10 fps, behind a user and password.
type rtspCam struct {
	server     *gortsplib.Server
	stream     *gortsplib.ServerStream
	medi       *description.Media
	user, pass string
	authed     atomic.Int32
}

// The test stream's parameter sets and picture: not decodable, but shaped
// as H.264 (an SPS, a PPS and an IDR slice).
var (
	testSPS = []byte{0x67, 0x42, 0xc0, 0x1e, 0xd9, 0x00, 0xa0, 0x47, 0xfe, 0xc8}
	testPPS = []byte{0x68, 0xce, 0x3c, 0x80}
	testIDR = append([]byte{0x65, 0x88, 0x84}, make([]byte, 200)...)
)

// startRTSPCam serves the test stream on 127.0.0.1 until the test ends.
func startRTSPCam(t *testing.T, user, pass string) *rtspCam {
	t.Helper()
	c := &rtspCam{user: user, pass: pass}
	c.medi = &description.Media{Type: description.MediaTypeVideo, Formats: []format.Format{
		&format.H264{PayloadTyp: 96, PacketizationMode: 1, SPS: testSPS, PPS: testPPS},
	}}
	c.server = &gortsplib.Server{Handler: c, RTSPAddress: "127.0.0.1:0"}
	if err := c.server.Start(); err != nil {
		t.Fatal(err)
	}
	c.stream = &gortsplib.ServerStream{Server: c.server, Desc: &description.Session{Medias: []*description.Media{c.medi}}}
	if err := c.stream.Initialize(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { c.publish(ctx) })
	t.Cleanup(func() {
		cancel()
		wg.Wait()
		c.stream.Close()
		c.server.Close()
	})
	return c
}

// URL is the camera's stream URI, without credentials, as GetStreamUri
// gives it.
func (c *rtspCam) URL() string {
	return "rtsp://" + c.server.NetListener().Addr().String() + "/sub"
}

// publish writes an access unit every 100 ms.
func (c *rtspCam) publish(ctx context.Context) {
	enc, err := c.medi.Formats[0].(*format.H264).CreateEncoder()
	if err != nil {
		return
	}
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
		pkts, err := enc.Encode([][]byte{testSPS, testPPS, testIDR})
		if err != nil {
			return
		}
		for _, p := range pkts {
			p.Timestamp = uint32(i * 9000) //nolint:gosec // wraps as RTP timestamps do
			_ = c.stream.WritePacketRTP(c.medi, p)
		}
	}
}

// OnDescribe asks for the camera's credentials.
func (c *rtspCam) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !ctx.Conn.VerifyCredentials(ctx.Request, c.user, c.pass) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	c.authed.Add(1)
	return &base.Response{StatusCode: base.StatusOK}, c.stream, nil
}

// OnSetup sets the stream up.
func (c *rtspCam) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !ctx.Conn.VerifyCredentials(ctx.Request, c.user, c.pass) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	return &base.Response{StatusCode: base.StatusOK}, c.stream, nil
}

// OnPlay plays it.
func (c *rtspCam) OnPlay(*gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	return &base.Response{StatusCode: base.StatusOK}, nil
}

// The binaries that the tests build once: the real ipcam-hub and the
// simulated cameras (the process boundary, P01).
var (
	buildMu sync.Mutex
	binDir  string
	built   = map[string]string{}
)

// binary builds the command pkg once per test binary.
func binary(t *testing.T, name, pkg string) string {
	t.Helper()
	buildMu.Lock()
	defer buildMu.Unlock()
	if p, ok := built[name]; ok {
		return p
	}
	if binDir == "" {
		d, err := os.MkdirTemp("", "mi8-bin-")
		if err != nil {
			t.Fatal(err)
		}
		binDir = d
	}
	p := spiketest.BuildBinary(t, binDir, name, pkg)
	built[name] = p
	return p
}

// hubBin is the real ipcam-hub.
func hubBin(t *testing.T) string {
	return binary(t, "ipcam-hub", "github.com/AndreiBesliu/DevWorld/ipcam/hub/cmd/ipcam-hub")
}

// sim is an onvifsim process: simulated ONVIF cameras.
type sim struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  *bufio.Scanner
	mu     sync.Mutex
	XAddrs []string
	Hosts  []string
}

// startSim starts onvifsim with the cameras (brand/model) and args.
func startSim(t *testing.T, user, pass string, extra []string, cams ...string) *sim {
	t.Helper()
	args := []string{"-user", user, "-pass", pass}
	for _, c := range cams {
		args = append(args, "-cam", c)
	}
	args = append(args, extra...)
	cmd := exec.Command(binary(t, "onvifsim", "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/cameras/events/onviftest/cmd/onvifsim"), args...) //nolint:gosec // the test's own build
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Wait() })
	s := &sim{t: t, cmd: cmd, stdin: stdin, lines: bufio.NewScanner(out)}
	var ready struct {
		Cameras []struct {
			XAddr    string `json:"xaddr"`
			HostPort string `json:"hostPort"`
		} `json:"cameras"`
	}
	if !s.lines.Scan() || json.Unmarshal(s.lines.Bytes(), &ready) != nil {
		t.Fatal("onvifsim gave no ready line")
	}
	for _, c := range ready.Cameras {
		s.XAddrs = append(s.XAddrs, c.XAddr)
		s.Hosts = append(s.Hosts, c.HostPort)
	}
	return s
}

// do sends a command and returns its answer line.
func (s *sim) do(cmd string) []byte {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := io.WriteString(s.stdin, cmd+"\n"); err != nil {
		s.t.Fatal(err)
	}
	if !s.lines.Scan() {
		s.t.Fatal("onvifsim ended")
	}
	return append([]byte(nil), s.lines.Bytes()...)
}

// simCounts are a camera's subscription counts.
type simCounts struct {
	Subscriptions, Creates, Renews, Pulls, Unsubscribes, Expired int
}

// counts are the cameras' counts.
func (s *sim) counts() []simCounts {
	s.t.Helper()
	var c struct {
		Cameras []simCounts `json:"cameras"`
	}
	if err := json.Unmarshal(s.do("counts"), &c); err != nil {
		s.t.Fatal(err)
	}
	return c.Cameras
}

// until polls cond every 100 ms for d.
func until(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", d, what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
