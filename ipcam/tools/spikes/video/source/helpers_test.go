package source

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// mediaFile returns the path of a file that cmd/mediagen generated. Tests that
// need it skip when MEDIAGEN_DIR is unset and fail instead when
// SPIKE_REQUIRE_MEDIA=1, as the CI media job sets.
func mediaFile(t *testing.T, name string) string {
	t.Helper()
	dir := os.Getenv("MEDIAGEN_DIR")
	if dir == "" {
		if os.Getenv("SPIKE_REQUIRE_MEDIA") == "1" {
			t.Fatal("MEDIAGEN_DIR is unset but SPIKE_REQUIRE_MEDIA=1")
		}
		t.Skip("MEDIAGEN_DIR is unset: run cmd/mediagen and point MEDIAGEN_DIR at its output")
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil { //nolint:gosec // dir is the media directory that the test run chose
		t.Fatalf("media file: %v", err)
	}
	return path
}

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

// waitClosed fails the test unless ch is closed with nothing left to read.
func waitClosed[T any](t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("received a value, expected the channel to be closed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the channel to close")
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

// fakeClock is a Clock whose tickers fire only when the test says so.
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

func (c *fakeClock) NewTicker(d time.Duration) Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	tk := &fakeTicker{period: d, ch: make(chan time.Time)}
	c.tickers = append(c.tickers, tk)
	return tk
}

// tickAt moves the clock to at and fires every ticker. It returns when each
// ticker's receiver has taken the tick; the receiver's work may still be running,
// so a test waits for the effect (a SentRecord, an AU) before it ticks again.
func (c *fakeClock) tickAt(t *testing.T, at time.Time) {
	t.Helper()
	c.mu.Lock()
	c.now = at
	tickers := append([]*fakeTicker(nil), c.tickers...)
	c.mu.Unlock()
	for _, tk := range tickers {
		select {
		case tk.ch <- at:
		case <-time.After(10 * time.Second):
			t.Fatal("nothing received the tick")
		}
	}
}

func (c *fakeClock) periods() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []time.Duration
	for _, tk := range c.tickers {
		out = append(out, tk.period)
	}
	return out
}

type fakeTicker struct {
	period time.Duration
	ch     chan time.Time
	once   sync.Once
}

func (tk *fakeTicker) C() <-chan time.Time { return tk.ch }

func (tk *fakeTicker) Stop() { tk.once.Do(func() { close(tk.ch) }) }

// synthAUs returns n H.264 access units with a keyframe every gop frames. A
// keyframe carries SPS, PPS and an IDR slice; any other frame is one non-IDR
// slice. The first byte after each NAL header has its top bit set, which marks
// the first slice of a picture. Frame f of the file has payload bytes f>>8, f.
func synthAUs(n, gop int) []AccessUnit {
	sps := []byte{0x67, 0x4d, 0x00, 0x28, 0xac, 0xd9}
	pps := []byte{0x68, 0xee, 0x3c, 0x80}
	aus := make([]AccessUnit, n)
	for i := range aus {
		pic := []byte{0x41, 0x9a, byte(i >> 8), byte(i)}
		au := AccessUnit{Frame: uint16(i)}
		if i%gop == 0 {
			pic[0] = 0x65
			au.IDR = true
			au.NALUs = [][]byte{sps, pps, pic}
		} else {
			au.NALUs = [][]byte{pic}
		}
		aus[i] = au
	}
	return aus
}
