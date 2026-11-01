package controller

import (
	"context"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// TestSendLog_MapsIndexToSendTime: the index of a sent frame is
// Loop·frames + Frame, so frame 119 of the second pass over a 600-frame file
// is index 719, and it maps to the time when the simulator sent it.
func TestSendLog_MapsIndexToSendTime(t *testing.T) {
	const frames, fps = 600, 20
	clock := newFakeClock()
	sim := source.NewSim("cam01_main", 1, synthAUs(frames, fps), fps, 0, clock)
	sendLog := NewSendLog()
	go feedSendLog(t.Context(), "cam01_main", frames, sim.SentLog(), sendLog)

	start := clock.Now()
	period := time.Second / fps
	var sentAt [frames + 120]time.Time
	for i := range sentAt {
		// Half a millisecond late on odd frames: the log keeps what was sent,
		// not the schedule.
		at := start.Add(time.Duration(i)*period + time.Duration(i%2)*500*time.Microsecond)
		clock.tickAt(t, at)
		index := uint64(i) //nolint:gosec // i < 720
		waitUntil(t, "the send log has the frame", func() bool {
			_, ok := sendLog.SendTime("cam01_main", index)
			return ok
		})
		sentAt[i] = at
	}

	got, ok := sendLog.SendTime("cam01_main", 719)
	if !ok {
		t.Fatal("SendTime(cam01_main, 719): no record")
	}
	if d := got.Sub(sentAt[719]); d < -time.Millisecond || d > time.Millisecond {
		t.Errorf("SendTime(cam01_main, 719) = %v, want %v ± 1 ms", got, sentAt[719])
	}
	if got, ok := sendLog.SendTime("cam01_main", 0); !ok || !got.Equal(sentAt[0]) {
		t.Errorf("SendTime(cam01_main, 0) = %v, %v; want %v", got, ok, sentAt[0])
	}
	if _, ok := sendLog.SendTime("cam01_main", frames+120); ok {
		t.Error("SendTime of an index not sent yet is ok")
	}
	if _, ok := sendLog.SendTime("cam02_main", 719); ok {
		t.Error("SendTime of another stream is ok")
	}
}

// TestSendLog_KeepsTheLastHour: a stream's log holds sendLogKeep indexes, so
// a run's loss can still be computed after a reboot and the NTP wait, while a
// 72 h run takes bounded memory.
func TestSendLog_KeepsTheLastHour(t *testing.T) {
	if sendLogKeep < 20*3600 {
		t.Fatalf("sendLogKeep = %d, want at least an hour at 20 fps", sendLogKeep)
	}
	l := NewSendLog()
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	at := func(i uint64) time.Time { return start.Add(time.Duration(i) * 50 * time.Millisecond) } //nolint:gosec // small in this test
	newest := uint64(sendLogKeep + 99)
	for i := uint64(0); i <= newest; i++ {
		if i == 500 {
			continue // a record that the simulator's log dropped
		}
		l.Record("cam01_main", i, at(i))
	}
	for _, tc := range []struct {
		index uint64
		ok    bool
	}{
		{0, false},          // evicted
		{99, false},         // evicted
		{100, true},         // the oldest kept
		{500, false},        // never recorded, although in the window
		{newest, true},      // the newest
		{newest + 1, false}, // not sent yet
	} {
		got, ok := l.SendTime("cam01_main", tc.index)
		if ok != tc.ok || (ok && !got.Equal(at(tc.index))) {
			t.Errorf("SendTime(%d) = %v, %v; want ok %v", tc.index, got, ok, tc.ok)
		}
	}
}

// TestSendTimes_KeepARunsWindow (mi1-1): a run's copy of the send log,
// taken over [from, to] for its streams, still gives their send times in
// that window once the log itself has dropped them, and Loss reads it as
// it reads the log; nothing outside the window or the streams is copied.
func TestSendTimes_KeepARunsWindow(t *testing.T) {
	l := NewSendLog()
	start := time.Date(2026, 10, 21, 22, 0, 0, 0, time.UTC)
	at := func(i uint64) time.Time { return start.Add(time.Duration(i) * 50 * time.Millisecond) } //nolint:gosec // small in this test
	for i := range uint64(2000) {
		l.Record("cam01_main", i, at(i))
		l.Record("cam01_sub", i, at(i))
	}
	times := l.Times([]string{"cam01_main", "cam02_main"}, at(1000), at(1500))
	for i := uint64(2000); i < 2000+sendLogKeep; i++ {
		l.Record("cam01_main", i, at(i)) // an hour later, the log has moved on
	}
	if _, ok := l.SendTime("cam01_main", 1200); ok {
		t.Fatal("test setup: the log still has index 1200")
	}
	for _, tc := range []struct {
		stream string
		index  uint64
		ok     bool
	}{
		{"cam01_main", 1000, true}, {"cam01_main", 1200, true}, {"cam01_main", 1500, true},
		{"cam01_main", 999, false}, {"cam01_main", 1501, false}, // outside the window
		{"cam01_sub", 1200, false},  // not one of the run's streams
		{"cam02_main", 1200, false}, // one of them, never sent
	} {
		got, ok := times.SendTime(tc.stream, tc.index)
		if ok != tc.ok || (ok && !got.Equal(at(tc.index))) {
			t.Errorf("SendTime(%s, %d) = %v, %v; want ok %v", tc.stream, tc.index, got, ok, tc.ok)
		}
	}
	if loss, ok := Loss(at(1500), "cam01_main", 1200, times); !ok || loss != 15*time.Second {
		t.Errorf("Loss from the copy = %v, %v; want 15 s", loss, ok)
	}
}

// TestSendLog_WaitFedWaitsForTheFeed (Final fix B, item 0): WaitFed
// returns once every stream asked for has a send at or after the moment,
// and false when its context ends first; SendTimers takes the first timer
// that has the index, so the live log covers what a copy missed.
func TestSendLog_WaitFedWaitsForTheFeed(t *testing.T) {
	l := NewSendLog()
	start := time.Date(2026, 10, 30, 3, 0, 0, 0, time.UTC)
	l.Record("cam01_main", 0, start)
	l.Record("cam02_main", 0, start.Add(time.Second))
	streams := []string{"cam01_main", "cam02_main"}
	until := start.Add(2 * time.Second)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if l.WaitFed(ctx, streams, until) {
		t.Fatal("WaitFed = true before any stream was fed past the moment")
	}
	done := make(chan bool, 1)
	go func() { done <- l.WaitFed(t.Context(), streams, until) }()
	l.Record("cam01_main", 1, until)
	select {
	case <-done:
		t.Fatal("WaitFed returned while cam02_main was still behind")
	case <-time.After(20 * time.Millisecond):
	}
	l.Record("cam02_main", 1, until.Add(time.Millisecond))
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("WaitFed = false once both streams were fed past the moment")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("WaitFed did not return once both streams were fed past the moment")
	}

	copied := l.Times([]string{"cam01_main"}, start, start)
	l.Record("cam01_main", 2, until.Add(time.Second)) // after the copy
	both := SendTimers{copied, l}
	if at, ok := both.SendTime("cam01_main", 0); !ok || !at.Equal(start) {
		t.Errorf("SendTimers: index 0 = %v, %v; want the copy's %v", at, ok, start)
	}
	if at, ok := both.SendTime("cam01_main", 2); !ok || !at.Equal(until.Add(time.Second)) {
		t.Errorf("SendTimers: index 2 = %v, %v; want the live log's", at, ok)
	}
	if _, ok := both.SendTime("cam01_main", 3); ok {
		t.Error("SendTimers found an index that neither timer has")
	}
}

// TestSendTimes_SpanCappedAtTheLogsKeep (Final fix A review, Minor 4): a
// copy holds at most the log's span of indexes per stream, so a stream
// whose index restarted inside the window, with old high indexes still in
// the ring, never allocates up to the old maximum; the newest indexes of
// the window keep their times.
func TestSendTimes_SpanCappedAtTheLogsKeep(t *testing.T) {
	l := NewSendLog()
	start := time.Date(2026, 10, 30, 4, 0, 0, 0, time.UTC)
	const old = 10_000_000
	l.Record("cam01_main", old, start)
	for i := range uint64(100) { // the simulator started again: its index restarted
		l.Record("cam01_main", i, start.Add(time.Duration(i+1)*50*time.Millisecond))
	}
	times := l.Times([]string{"cam01_main"}, start, start.Add(time.Minute))
	if n := len(times.streams["cam01_main"].entries); n > sendLogKeep {
		t.Errorf("the copy holds %d entries for a stream, want at most %d", n, sendLogKeep)
	}
	if at, ok := times.SendTime("cam01_main", old); !ok || !at.Equal(start) {
		t.Errorf("the newest index of the window: %v, %v; want %v", at, ok, start)
	}
}
