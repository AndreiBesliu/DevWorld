package session

import (
	"bytes"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// has reports whether the fan-out delivers the records of src to s.
func (f *sentFanout) has(src source.Source, s *Session) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.sinks[src][s]
	return ok
}

// waitPending waits until the session holds n records of src for its next
// sent message.
func waitPending(t *testing.T, s *Session, src source.Source, n int) {
	t.Helper()
	waitUntil(t, "the session holds the sent records", func() bool {
		s.sentMu.Lock()
		defer s.sentMu.Unlock()
		b := s.bound[src]
		return b != nil && len(b.recs) == n
	})
}

// pendingRecords returns the sent records of src that the session has not
// reported yet.
func pendingRecords(s *Session, src source.Source) [][3]float64 {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	if b := s.bound[src]; b != nil {
		return slices.Clone(b.recs)
	}
	return nil
}

func TestSentLog_BatchesEvery500ms(t *testing.T) {
	sim := newTestSim(t, "sim-04", 4, "sub", h264AUs("4d401f", 4, 48, 12), 12)
	sim.emit(t, 1) // frame 0 goes before the bind, and no sent message reports it
	clock := newFakeClock()
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s := connect(t, testAPI(t), c, map[string]source.Source{"sim-04/sub": sim}, testOptions(clock, nil))
	c.bind(t, 0, "sim-04", "sub", 1)
	c.next(t, "bound")

	sim.emit(t, 2)
	waitPending(t, s, sim, 2)
	clock.advance(t, 500*time.Millisecond)
	m := c.next(t, "sent")
	want := [][3]float64{{1, 0, sim.sentAt(2)}, {2, 0, sim.sentAt(3)}}
	if m.Cam != 4 || m.P != "sub" || !slices.Equal(m.R, want) {
		t.Errorf("sent = %s, want cam 4, p sub, r %v", m.raw, want)
	}
	if !strings.HasPrefix(m.raw, `{"t":"sent","cam":4,"p":"sub","r":[[1,0,`) {
		t.Errorf("sent = %s, want the fields in the order t, cam, p, r", m.raw)
	}

	sim.emit(t, 2)
	waitPending(t, s, sim, 2)
	clock.advance(t, 500*time.Millisecond)
	m = c.next(t, "sent")
	want = [][3]float64{{3, 0, sim.sentAt(4)}, {4, 0, sim.sentAt(5)}}
	if !slices.Equal(m.R, want) {
		t.Errorf("second sent = %s, want r %v", m.raw, want)
	}
}

// A simulator logs the frames it sends from the start, but the sent message of
// a session reports the frames since its source was bound: a cached frame that a
// bind sends carries its original emit time, so its latency would include the
// age of the cache (R26).
func TestSentLog_ReportsNoRecordFromBeforeTheFirstBind(t *testing.T) {
	sim := newTestSim(t, "sim-04", 4, "sub", h264AUs("4d401f", 4, 48, 12), 12)
	sim.emit(t, 3) // frames 0 to 2, before any session binds the simulator
	waitUntil(t, "the simulator logged its frames", func() bool { return len(sim.SentLog()) == 3 })
	clock := newFakeClock()
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s := connect(t, testAPI(t), c, map[string]source.Source{"sim-04/sub": sim}, testOptions(clock, nil))
	c.bind(t, 0, "sim-04", "sub", 1)
	c.next(t, "bound")
	if got := c.au(t, 0); got.frame != 0 {
		t.Fatalf("the bind started at frame %d, want the cached keyframe 0", got.frame)
	}

	sim.emit(t, 1) // frame 3
	waitUntil(t, "the session holds the record of frame 3", func() bool {
		recs := pendingRecords(s, sim)
		return len(recs) > 0 && recs[len(recs)-1][0] == 3
	})
	clock.advance(t, 500*time.Millisecond)
	m := c.next(t, "sent")
	bindAt := sim.sentAt(3) // the hub time of the last frame sent before the bind
	for _, r := range m.R {
		if r[2] <= bindAt {
			t.Errorf("sent holds the record %v from before the bind: %s", r, m.raw)
		}
	}
	if want := [][3]float64{{3, 0, sim.sentAt(4)}}; !slices.Equal(m.R, want) {
		t.Errorf("sent = %s, want r %v", m.raw, want)
	}
}

func TestSentLog_CarriesTheBoundProfile(t *testing.T) {
	// The sub and main streams of one simulated camera share its index.
	sub := newTestSim(t, "sim-04", 4, "sub", h264AUs("4d401f", 4, 48, 12), 12)
	main := newTestSim(t, "sim-04", 4, "main", h264AUs("4d4028", 4, 80, 20), 20)
	clock := newFakeClock()
	c := newClient(t, clientConfig{slots: 2, offer: chromeLike})
	s := connect(t, testAPI(t), c, map[string]source.Source{"sim-04/sub": sub, "sim-04/main": main}, testOptions(clock, nil))
	c.bind(t, 0, "sim-04", "sub", 1)
	c.next(t, "bound")
	c.bind(t, 1, "sim-04", "main", 2)
	c.next(t, "bound")

	sub.emit(t, 1)
	main.emit(t, 2)
	waitPending(t, s, sub, 1)
	waitPending(t, s, main, 2)
	clock.advance(t, 500*time.Millisecond)

	byProfile := map[string]rxMsg{}
	for range 2 {
		m := c.next(t, "sent")
		byProfile[m.P] = m
	}
	if m := byProfile["sub"]; m.Cam != 4 || !slices.Equal(m.R, [][3]float64{{0, 0, sub.sentAt(1)}}) {
		t.Errorf("sent for sub = %s", m.raw)
	}
	if m := byProfile["main"]; m.Cam != 4 || !slices.Equal(m.R, [][3]float64{{0, 0, main.sentAt(1)}, {1, 0, main.sentAt(2)}}) {
		t.Errorf("sent for main = %s", m.raw)
	}
}

func TestSentLog_ReachesEverySessionBoundToTheSource(t *testing.T) {
	sim := newTestSim(t, "sim-04", 4, "sub", h264AUs("4d401f", 4, 48, 12), 12)
	sources := map[string]source.Source{"sim-04/sub": sim}
	api := testAPI(t)
	type viewer struct {
		c     *testClient
		s     *Session
		clock *fakeClock
	}
	var viewers []viewer
	for range 2 {
		clock := newFakeClock()
		c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
		s := connect(t, api, c, sources, testOptions(clock, nil))
		c.bind(t, 0, "sim-04", "sub", 1)
		c.next(t, "bound")
		viewers = append(viewers, viewer{c, s, clock})
	}

	sim.emit(t, 3)
	want := [][3]float64{{0, 0, sim.sentAt(1)}, {1, 0, sim.sentAt(2)}, {2, 0, sim.sentAt(3)}}
	for i, v := range viewers {
		waitPending(t, v.s, sim, 3)
		v.clock.advance(t, 500*time.Millisecond)
		if m := v.c.next(t, "sent"); !slices.Equal(m.R, want) {
			t.Errorf("viewer %d: sent = %s, want r %v", i, m.raw, want)
		}
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

func TestSentLog_WarnsWhenTheStreamDiffersFromTheBindKey(t *testing.T) {
	sim := newTestSim(t, "sim-04", 4, "main", h264AUs("4d401f", 4, 48, 12), 12)
	clock := newFakeClock()
	var logs syncBuffer
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike})
	s := connect(t, testAPI(t), c, map[string]source.Source{"sim-04/sub": sim}, testOptions(clock, &logs))
	c.bind(t, 0, "sim-04", "sub", 1)
	c.next(t, "bound")

	sim.emit(t, 2)
	waitPending(t, s, sim, 2)
	clock.advance(t, 500*time.Millisecond)
	if m := c.next(t, "sent"); m.P != "sub" {
		t.Errorf("sent = %s, want p from the bind key", m.raw)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "stream=main") || !strings.Contains(out, "profile=sub") {
		t.Errorf("log = %q, want one warning naming both profiles", out)
	}
	if n := strings.Count(out, "level=WARN"); n != 1 {
		t.Errorf("log has %d warnings, want 1", n)
	}
}

func TestJitter_ReportsTheWorstBoundSourceEvery10s(t *testing.T) {
	late := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 48, 12), 12)
	steady := newTestSim(t, "sim-02", 2, "sub", h264AUs("4d401f", 2, 48, 12), 12)
	late.emit(t, 1)
	late.emitLate(t, 30*time.Millisecond)
	steady.emit(t, 3)
	clock := newFakeClock()
	c := newClient(t, clientConfig{slots: 2, offer: chromeLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": late, "sim-02/sub": steady}, testOptions(clock, nil))

	c.send(t, `{"t":"ping","c0":1}`)
	c.next(t, "pong") // the hub's end of the rpc channel is open
	clock.advance(t, 10*time.Second)
	c.send(t, `{"t":"ping","c0":2}`)
	for m := waitFor(t, c.msgs); m.T != "pong"; m = waitFor(t, c.msgs) {
		if m.T == "jitter" {
			t.Fatalf("got %s while nothing is bound, want no report", m.raw)
		}
	}
	c.bind(t, 0, "sim-02", "sub", 1)
	c.next(t, "bound")
	c.bind(t, 1, "sim-01", "sub", 2)
	c.next(t, "bound")
	clock.advance(t, 10*time.Second)

	m := c.next(t, "jitter")
	if want := `{"t":"jitter","ms":30}`; m.raw != want {
		t.Errorf("jitter = %s, want %s", m.raw, want)
	}
}
