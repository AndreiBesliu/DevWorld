package controller

import (
	"bytes"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// mediaFile returns the path of a file that mediagen or recmediagen wrote.
// Tests that need it skip when MEDIAGEN_DIR is unset and fail instead when
// SPIKE_REQUIRE_MEDIA=1, as the CI media job sets.
func mediaFile(t *testing.T, name string) string {
	t.Helper()
	dir := os.Getenv("MEDIAGEN_DIR")
	if dir == "" {
		if os.Getenv("SPIKE_REQUIRE_MEDIA") == "1" {
			t.Fatal("MEDIAGEN_DIR is unset but SPIKE_REQUIRE_MEDIA=1")
		}
		t.Skip("MEDIAGEN_DIR is unset: run mediagen and recmediagen and point MEDIAGEN_DIR at their output")
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil { //nolint:gosec // the media directory that the test run chose
		t.Fatalf("media file: %v", err)
	}
	return path
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

// fakeClock is a source.Clock whose tickers fire only when the test says so.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTicker(time.Duration) source.Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	tk := &fakeTicker{ch: make(chan time.Time), stopped: make(chan struct{})}
	c.tickers = append(c.tickers, tk)
	return tk
}

// tickAt moves the clock to at and fires every ticker. It returns when each
// ticker's receiver has taken the tick, or has stopped its ticker: a waiter
// that reads the new time before its tick may stop instead of receiving. A
// test waits for the effect (a SentRecord) before it ticks again.
func (c *fakeClock) tickAt(t *testing.T, at time.Time) {
	t.Helper()
	c.mu.Lock()
	c.now = at
	tickers := append([]*fakeTicker(nil), c.tickers...)
	c.mu.Unlock()
	for _, tk := range tickers {
		tk.mu.Lock()
		select {
		case tk.ch <- at:
		case <-tk.stopped:
		case <-time.After(10 * time.Second):
			tk.mu.Unlock()
			t.Fatal("nothing received the tick")
		}
		tk.mu.Unlock()
	}
}

// fakeTicker is a ticker of fakeClock. Stop marks it stopped before it
// closes its channel, under mu, so that tickAt never sends on a closed one.
type fakeTicker struct {
	mu      sync.Mutex
	ch      chan time.Time
	stopped chan struct{}
	once    sync.Once
}

func (tk *fakeTicker) C() <-chan time.Time { return tk.ch }

func (tk *fakeTicker) Stop() {
	tk.once.Do(func() {
		close(tk.stopped)
		tk.mu.Lock()
		defer tk.mu.Unlock()
		close(tk.ch)
	})
}

// synthAUs returns n H.264 access units with a keyframe every gop frames. A
// keyframe carries SPS, PPS and an IDR slice; any other frame is one non-IDR
// slice. Frame f carries the payload bytes f>>8, f.
func synthAUs(n, gop int) []source.AccessUnit {
	sps := []byte{0x67, 0x4d, 0x00, 0x28, 0xac, 0xd9}
	pps := []byte{0x68, 0xee, 0x3c, 0x80}
	aus := make([]source.AccessUnit, n)
	for f := range aus {
		payload := []byte{0x88, byte(f >> 8), byte(f)}
		if f%gop == 0 {
			aus[f] = source.AccessUnit{NALUs: [][]byte{sps, pps, append([]byte{0x65}, payload...)}, IDR: true}
		} else {
			aus[f] = source.AccessUnit{NALUs: [][]byte{append([]byte{0x41}, payload...)}}
		}
		aus[f].Frame = uint16(f) //nolint:gosec // n is small in tests
	}
	return aus
}

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

// captureLogs routes the default slog logger, at every level, and the
// standard logger, which gortsplib writes to when a handler lacks a
// callback, to the returned buffer until the test ends.
func captureLogs(t *testing.T) *logBuffer {
	t.Helper()
	b := &logBuffer{}
	previous, previousWriter, previousFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(b) // after SetDefault, which points the standard logger at the slog handler
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})
	return b
}
