package main

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// gatedWriter lets one Write through for each permit, and every Write once
// permits is closed.
type gatedWriter struct {
	permits chan struct{}
	mu      sync.Mutex
	buf     bytes.Buffer
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	<-w.permits
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *gatedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// waitUntil fails the test when ok does not hold within 10 s.
func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestLogQueue_DropCountComesBeforeLaterLines (Task 9b review M1): when
// lines were dropped and exactly one slot is free, a new line does not take
// it ahead of the drop count, which would then be written after lines that
// came later: it is dropped and counted too, so the count always goes to
// stderr where the lines were dropped.
func TestLogQueue_DropCountComesBeforeLaterLines(t *testing.T) {
	w := &gatedWriter{permits: make(chan struct{})}
	const n = 4
	q := newLogQueue(w, n)
	released := false
	release := func() {
		if !released {
			released = true
			close(w.permits)
		}
	}
	t.Cleanup(func() { release(); q.close(time.Second) })

	q.add("line 0")
	waitUntil(t, "the writer to take line 0", func() bool { return len(q.lines) == 0 })
	for i := 1; i <= n+1; i++ { // line 5 finds the queue full and is dropped
		q.add(fmt.Sprintf("line %d", i))
	}
	w.permits <- struct{}{} // line 0 is written, and the writer takes line 1
	waitUntil(t, "one free slot", func() bool { return len(q.lines) == n-1 })
	q.add("line 6")

	release()
	waitUntil(t, "the lines that waited", func() bool { return strings.Count(w.String(), "\n") >= n+1 })
	q.add("after")
	q.close(5 * time.Second)

	var want strings.Builder
	for i := 0; i <= n; i++ {
		fmt.Fprintf(&want, "line %d\n", i)
	}
	want.WriteString(droppedLine(2) + "\n")
	want.WriteString("after\n")
	if got := w.String(); got != want.String() {
		t.Errorf("stderr:\n%s\nwant:\n%s", got, want.String())
	}
}
