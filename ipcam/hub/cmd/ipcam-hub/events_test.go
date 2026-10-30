package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
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
