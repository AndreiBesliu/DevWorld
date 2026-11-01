package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/health"
)

// heldWriter is ipcam-hub's stderr on a card that holds its writes: each
// Write waits until unblock. started is closed at the first Write.
type heldWriter struct {
	release, started chan struct{}
	once, freed      sync.Once
	mu               sync.Mutex
	buf              bytes.Buffer
}

func newHeldWriter() *heldWriter {
	return &heldWriter{release: make(chan struct{}), started: make(chan struct{})}
}

func (w *heldWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// unblock lets every Write through, from now on.
func (w *heldWriter) unblock() { w.freed.Do(func() { close(w.release) }) }

func (w *heldWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TestObserver_CardSeesALineBeforeStderr (T40; Task 8b review M1): a line of
// MediaMTX's log reaches the card sampler, and its failed write the
// recorder, without waiting for ipcam-hub's stderr. In MI2 that stderr is a
// file on the SD card that the sampler watches: a write that the card holds
// must hold neither MediaMTX's log reader, and with it MediaMTX's output
// pipe, nor the failed write's time, which must come within the 2 s after
// which MediaMTX starts the stream's next segment. The lines still reach
// stderr, in order, once it takes writes again.
func TestObserver_CardSeesALineBeforeStderr(t *testing.T) {
	stderr := newHeldWriter()
	rec := &fakeCardRecorder{stops: make(chan string, 1), failed: make(chan string, 2)}
	s := newCardSampler(t.TempDir(), "", &events{out: io.Discard})
	s.attach(rec)
	o := newObserver(&events{out: io.Discard}, stderr, s)
	t.Cleanup(o.close)
	t.Cleanup(stderr.unblock)

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		o.ChildLog(eioLine)
		o.ChildLog(eioLine)
	}()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("ChildLog waited for ipcam-hub's stderr, which the card holds")
	}
	if n := len(rec.failed); n != 2 {
		t.Errorf("%d failed writes reached the recorder, want 2", n)
	}
	if st := s.stats(); st.IOErrors24h != 2 {
		t.Errorf("stats %+v, want the 2 I/O errors counted", st)
	}
	stderr.unblock()
	o.close()
	if got, want := stderr.String(), strings.Repeat("mediamtx: "+eioLine+"\n", 2); got != want {
		t.Errorf("stderr %q, want %q", got, want)
	}
}

// TestObserver_StderrDropsAndCountsLinesWhenFull (T40; Task 8b review M1):
// at most logQueueLen lines of MediaMTX's log wait for a stderr that the
// card holds; a line that comes when they are all waiting is dropped and
// counted, and the count goes to stderr where the lines were dropped,
// before the next line written.
func TestObserver_StderrDropsAndCountsLinesWhenFull(t *testing.T) {
	stderr := newHeldWriter()
	o := newObserver(&events{out: io.Discard}, stderr, nil)
	t.Cleanup(o.close)
	t.Cleanup(stderr.unblock)

	const dropped = 3
	fed := make(chan struct{})
	go func() {
		defer close(fed)
		o.ChildLog("line 0")
		<-stderr.started // the write of line 0 is held: the queue is empty
		for i := 1; i <= logQueueLen+dropped; i++ {
			o.ChildLog(fmt.Sprintf("line %d", i))
		}
	}()
	select {
	case <-fed:
	case <-time.After(10 * time.Second):
		t.Fatal("ChildLog waited for ipcam-hub's stderr, which the card holds")
	}
	stderr.unblock()
	deadline := time.Now().Add(10 * time.Second)
	for strings.Count(stderr.String(), "\n") < logQueueLen+1 {
		if time.Now().After(deadline) {
			t.Fatalf("stderr has %d lines after 10 s, want the %d that waited", strings.Count(stderr.String(), "\n"), logQueueLen+1)
		}
		time.Sleep(time.Millisecond)
	}
	o.ChildLog("after")
	o.close()

	var want strings.Builder
	for i := 0; i <= logQueueLen; i++ {
		fmt.Fprintf(&want, "mediamtx: line %d\n", i)
	}
	fmt.Fprintf(&want, "ipcam-hub: dropped %d lines of MediaMTX's log: stderr did not keep up\n", dropped)
	want.WriteString("mediamtx: after\n")
	if got := stderr.String(); got != want.String() {
		t.Errorf("stderr ends %q, want it to end %q", tail(got, 3), tail(want.String(), 3))
	}
}

// tail is the last n lines of s.
func tail(s string, n int) string {
	lines := strings.SplitAfter(s, "\n")
	return strings.Join(lines[max(0, len(lines)-n-1):], "")
}

// cardLineWriter records the events' lines. At the card's failed line it
// waits hold before it returns, with the events' lock held, so that an
// upload event emitted meanwhile waits behind that line.
type cardLineWriter struct {
	hold  time.Duration
	mu    sync.Mutex
	lines []string
}

func (w *cardLineWriter) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if strings.Contains(line, `"type":"card"`) && strings.Contains(line, `"state":"failed"`) {
		time.Sleep(w.hold)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lines = append(w.lines, line)
	return len(p), nil
}

func (w *cardLineWriter) snapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.lines)
}

// TestUploads_NoUploadEventAfterCardFailed (g2-04, Task 7b review M2): the
// uploads' stop flag is set at the card's FAILED transition, before the
// card's failed event goes out, so an upload worker in flight emits nothing
// after that event, also when its event was on its way as the card failed.
func TestUploads_NoUploadEventAfterCardFailed(t *testing.T) {
	w := &cardLineWriter{hold: 200 * time.Millisecond}
	ev := &events{out: w}
	card := newCardSampler(t.TempDir(), "", ev)
	cat, err := catalog.Open(t.Context(), filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	obs := newObserver(ev, io.Discard, card)
	t.Cleanup(obs.close)
	u := &uploads{cfg: &uploadsConfig{HubName: "hub"}, hubID: testHubID, unlock: func() {}}
	if err := u.build(cat, ev, obs, t.TempDir(), http.DefaultClient, storeTokens(t.Context(), nil)); err != nil {
		t.Fatal(err)
	}
	u.start(card)
	t.Cleanup(u.stop)
	// A worker in flight reports its progress until the test ends.
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		p := catalog.Placement{ID: "p1", DestinationID: "dst-1"}
		for {
			select {
			case <-stop:
				return
			default:
				u.events.Progress(p, 1, 2)
			}
		}
	}()
	time.Sleep(20 * time.Millisecond)
	card.mu.Lock()
	card.observe(health.IoEvent{Kind: health.IoEROFS})
	card.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	close(stop)
	<-done
	lines := w.snapshot()
	failedAt := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, `"state":"failed"`) })
	if failedAt < 0 {
		t.Fatalf("no card failed event in %d lines", len(lines))
	}
	for _, l := range lines[failedAt+1:] {
		if strings.Contains(l, `"type":"upload_`) {
			t.Fatalf("upload event %s after the card failed", l)
		}
	}
	if !strings.Contains(strings.Join(lines[:failedAt], "\n"), `"type":"upload_progress"`) {
		t.Error("no upload_progress before the card failed: the worker never ran")
	}
}

// TestUploadEvents_QueuedKeepsTheStream (Task 7b review M4): upload_queued
// takes cam, profile and startMs from the placement that the queue read
// with its segment's row, so a catalog that fails to read the segment again
// takes none of them away.
func TestUploadEvents_QueuedKeepsTheStream(t *testing.T) {
	w := &cardLineWriter{}
	u := &uploadEvents{ev: &events{out: w}}
	u.Queued(catalog.Placement{ID: "p1", DestinationID: "dst-1", Class: catalog.ClassContinuous, Bytes: 10,
		Cam: testCam, Profile: testProfile, StartMs: 1234})
	lines := w.snapshot()
	if len(lines) != 1 {
		t.Fatalf("lines %q, want one upload_queued", lines)
	}
	var e map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatal(err)
	}
	if e["type"] != "upload_queued" || e["cam"] != testCam || e["profile"] != testProfile || e["startMs"] != float64(1234) {
		t.Errorf("event %v, want upload_queued of %s/%s at 1234", e, testCam, testProfile)
	}
}
