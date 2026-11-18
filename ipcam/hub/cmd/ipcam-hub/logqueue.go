package main

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// logQueueLen bounds the lines of MediaMTX's log that wait for ipcam-hub's
// stderr.
const logQueueLen = 1024

// logQueue writes lines to w on a goroutine of its own, so that the
// goroutine that reads MediaMTX's log never waits for w: in MI2, w is a
// file on the SD card that the card monitor watches (T40), and a write that
// the card holds would hold MediaMTX's output pipe. At most its length of
// lines wait; a line that comes when they all wait is dropped and counted,
// and the count goes to w as a line of its own, where the lines were
// dropped.
type logQueue struct {
	w     io.Writer
	lines chan string
	done  chan struct{} // closed once every line queued has been written

	mu      sync.Mutex
	dropped int // the lines dropped since the last count queued
	closed  bool
}

func newLogQueue(w io.Writer, n int) *logQueue {
	q := &logQueue{w: w, lines: make(chan string, n), done: make(chan struct{})}
	go q.run()
	return q
}

// add queues line, after the count of the lines dropped before it when
// there are any, or drops it: while lines are dropped, line goes only with
// the count before it, so the count is never written after a later line.
// It never waits for w.
func (q *logQueue) add(line string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	// add is the only sender and holds q.mu: room seen here stays.
	free := cap(q.lines) - len(q.lines)
	switch {
	case q.dropped > 0 && free >= 2:
		q.lines <- droppedLine(q.dropped)
		q.dropped = 0
		q.lines <- line
	case q.dropped == 0 && free >= 1:
		q.lines <- line
	default:
		q.dropped++
	}
}

// close takes no more lines and waits until those queued are written, for
// at most d, then writes the count of the lines dropped last, if any.
func (q *logQueue) close(d time.Duration) {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.lines)
	}
	q.mu.Unlock()
	select {
	case <-q.done:
	case <-time.After(d):
	}
}

func (q *logQueue) run() {
	defer close(q.done)
	for line := range q.lines {
		_, _ = fmt.Fprintln(q.w, line)
	}
	q.mu.Lock()
	n := q.dropped
	q.dropped = 0
	q.mu.Unlock()
	if n > 0 {
		_, _ = fmt.Fprintln(q.w, droppedLine(n))
	}
}

func droppedLine(n int) string {
	return fmt.Sprintf("ipcam-hub: dropped %d lines of MediaMTX's log: stderr did not keep up", n)
}
