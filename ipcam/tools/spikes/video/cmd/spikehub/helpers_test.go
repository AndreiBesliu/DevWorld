package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/session"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/webrtc/v4"
)

// waitFor receives one value from ch, failing the test when none arrives.
func waitFor[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("channel closed while waiting for a value")
		}
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a value")
		panic("unreachable")
	}
}

// waitUntil polls cond until it holds, failing the test after 10 s.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// syncBuffer is a bytes.Buffer that a logger and the test may use at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fakeClock is a source.Clock whose tickers fire only when the test says so,
// so that the simulators of a test hub do not run on their own.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTicker(time.Duration) source.Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	tk := &fakeTicker{ch: make(chan time.Time)}
	c.tickers = append(c.tickers, tk)
	return tk
}

// tickAll fires every ticker once, 100 ms after the previous tick. It returns
// when each ticker's receiver has taken the tick; the receiver's work may
// still be running, so a test waits for the effect (a SentRecord).
func (c *fakeClock) tickAll(t *testing.T) {
	t.Helper()
	c.mu.Lock()
	c.now = c.now.Add(100 * time.Millisecond)
	now := c.now
	tickers := append([]*fakeTicker(nil), c.tickers...)
	c.mu.Unlock()
	for _, tk := range tickers {
		select {
		case tk.ch <- now:
		case <-time.After(10 * time.Second):
			t.Fatal("nothing received the tick")
		}
	}
}

type fakeTicker struct{ ch chan time.Time }

func (tk *fakeTicker) C() <-chan time.Time { return tk.ch }

func (tk *fakeTicker) Stop() {}

// The synthetic streams below carry, in the first slice of every frame, the
// camera index and the frame index as bytes with the top bit set, so that no
// two zero bytes (part of an Annex B start code) appear in a NAL unit. They
// have the frame counts of the real media, so that the phases and the loops
// are the real ones, and a few bytes per frame.

func frameTag(file byte, frame int) []byte {
	return []byte{0x80 | file, 0x80 | byte(frame>>7&0x7f), 0x80 | byte(frame&0x7f), 0xff}
}

// annexB returns the Annex B form of the access units.
func annexB(aus [][][]byte) []byte {
	var out []byte
	for _, au := range aus {
		for _, nalu := range au {
			out = append(out, 0, 0, 0, 1)
			out = append(out, nalu...)
		}
	}
	return out
}

// h264Stream returns an H.264 stream whose SPS carries the profile-level-id
// plid, with an IDR every gop frames.
func h264Stream(plid [3]byte, file byte, frames, gop int) []byte {
	sps := append(append([]byte{0x67}, plid[:]...), 0xac, 0xd9)
	pps := []byte{0x68, 0xee, 0x3c, 0x80}
	aus := make([][][]byte, frames)
	for i := range aus {
		if i%gop == 0 {
			aus[i] = [][]byte{sps, pps, append([]byte{0x65, 0x88}, frameTag(file, i)...)}
		} else {
			aus[i] = [][]byte{append([]byte{0x41, 0x9a}, frameTag(file, i)...)}
		}
	}
	return annexB(aus)
}

// h265Stream returns an H.265 Main stream with an IDR every gop frames.
func h265Stream(file byte, frames, gop int) []byte {
	vps := []byte{0x40, 0x01, 0x0c, 0x01, 0xff, 0xff, 0x01, 0x60}
	sps := []byte{0x42, 0x01, 0x01, 0x01, 0x60, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x7b, 0xa0}
	pps := []byte{0x44, 0x01, 0xc1, 0x72, 0xb4}
	aus := make([][][]byte, frames)
	for i := range aus {
		if i%gop == 0 {
			aus[i] = [][]byte{vps, sps, pps, append([]byte{0x26, 0x01, 0xaf}, frameTag(file, i)...)}
		} else {
			aus[i] = [][]byte{append([]byte{0x02, 0x01, 0xaf}, frameTag(file, i)...)}
		}
	}
	return annexB(aus)
}

// hlsSegment is the content of the one segment of the synthetic recording.
var hlsSegment = func() []byte {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}()

// writeMedia writes a directory in the layout of cmd/mediagen's output, with
// synthetic streams of the real frame counts and a small recording.
func writeMedia(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string][]byte{
		"sub.h264":          h264Stream([3]byte{0x4d, 0x40, 0x1f}, 1, 720, 12),
		"main.h264":         h264Stream([3]byte{0x4d, 0x40, 0x28}, 2, 1200, 20),
		"main-high51.h264":  h264Stream([3]byte{0x64, 0x00, 0x33}, 3, 400, 20),
		"main-h265.h265":    h265Stream(4, 1200, 20),
		"hls/index.m3u8":    []byte("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4.0,\nseg-00000.m4s\n#EXT-X-ENDLIST\n"),
		"hls/init.mp4":      []byte("init"),
		"hls/seg-00000.m4s": hlsSegment,
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// testWeb is the app that the test hubs embed.
var testWeb = fstest.MapFS{
	"index.html":    {Data: []byte("<!doctype html><title>spike</title>")},
	"assets/app.js": {Data: []byte("console.log('spike')")},
}

// fixedNow is the clock of the test hubs: it names the results files.
func fixedNow() time.Time { return time.Date(2026, 10, 18, 12, 51, 0, 0, time.UTC) }

// testAPI returns the hub's WebRTC API without mDNS.
func testAPI(t *testing.T) *webrtc.API {
	t.Helper()
	api, mdnsUnavailable, err := session.NewAPI(false)
	if err != nil {
		t.Fatal(err)
	}
	if mdnsUnavailable {
		t.Fatal("NewAPI(false) reported mDNS as unavailable")
	}
	return api
}

// testHub is a hub on synthetic media behind an httptest server.
type testHub struct {
	*hub
	srv        *httptest.Server
	mediaDir   string
	resultsDir string
	logs       *syncBuffer
	// console is the hub's window: the lines the owner reads.
	console *syncBuffer
}

// newTestHub starts a hub with the given number of simulators. tweak may change
// its configuration before it starts. The simulators run on a clock that does
// not tick.
func newTestHub(t *testing.T, sims int, tweak ...func(*hubConfig)) *testHub {
	t.Helper()
	dir := writeMedia(t)
	m, err := loadMedia(dir)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := newCatalog(m, sims, newFakeClock())
	if err != nil {
		t.Fatal(err)
	}
	logs, console := &syncBuffer{}, &syncBuffer{}
	cfg := hubConfig{
		console:    console,
		cat:        cat,
		api:        testAPI(t),
		hlsDir:     filepath.Join(dir, "hls"),
		resultsDir: filepath.Join(t.TempDir(), "results"),
		web:        testWeb,
		now:        fixedNow,
		log:        slog.New(slog.NewTextHandler(logs, nil)),
	}
	for _, f := range tweak {
		f(&cfg)
	}
	h := newHub(cfg)
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(h.Close)
	t.Cleanup(srv.Close)
	return &testHub{hub: h, srv: srv, mediaDir: dir, resultsDir: cfg.resultsDir, logs: logs, console: console}
}

// do sends a request to the hub and returns the response with its body read.
func (h *testHub) do(t *testing.T, method, path string, body io.Reader, header http.Header) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, h.srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

// captureLogs makes a logger that writes to the returned buffer the default
// one for the test, because the source package logs through slog.Default.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	b := &syncBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return b
}

// newAPIForTest returns the hub's WebRTC API without mDNS, as session.NewAPI(false).
func newAPIForTest() (*webrtc.API, bool, error) { return session.NewAPI(false) }

// postJSON posts body to url and returns the response body.
func postJSON(t *testing.T, url, body string) (string, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	return string(data), err
}
