package source

import (
	"testing"
	"time"
)

func TestPacer_ReportsJitter(t *testing.T) {
	const (
		fps    = 12
		ticks  = 100
		delay  = 30 * time.Millisecond
		period = time.Second / fps
	)

	t.Run("a busy hub delaying every tenth frame by 30 ms", func(t *testing.T) {
		clock := newFakeClock()
		src := NewSim("sim", 1, synthAUs(48, 12), fps, 0, clock)
		start := clock.Now()
		for k := 1; k <= ticks; k++ {
			at := start.Add(time.Duration(k) * period)
			if k%10 == 0 {
				at = at.Add(delay) // the send is late; the next one is on schedule again
			}
			clock.tickAt(t, at)
			waitFor(t, src.SentLog())
		}
		got := src.Jitter()
		if got < delay || got > delay+time.Millisecond {
			t.Fatalf("Jitter() = %v, want about %v", got, delay)
		}
	})

	t.Run("an idle hub", func(t *testing.T) {
		clock := newFakeClock()
		src := NewSim("sim", 1, synthAUs(48, 12), fps, 0, clock)
		runTicks(t, clock, src, fps, ticks)
		if got := src.Jitter(); got > time.Millisecond {
			t.Fatalf("Jitter() = %v on a perfectly paced source", got)
		}
	})

	t.Run("before the second frame", func(t *testing.T) {
		clock := newFakeClock()
		src := NewSim("sim", 1, synthAUs(48, 12), fps, 0, clock)
		if got := src.Jitter(); got != 0 {
			t.Fatalf("Jitter() = %v before any frame", got)
		}
		runTicks(t, clock, src, fps, 1)
		if got := src.Jitter(); got != 0 {
			t.Fatalf("Jitter() = %v after one frame: there is no interval yet", got)
		}
	})
}

func TestJitterMeter_P99(t *testing.T) {
	const period = 50 * time.Millisecond
	feed := func(offsets map[int]time.Duration, n int) time.Duration {
		m := newJitterMeter(period)
		at := time.Unix(1000, 0)
		m.observe(at)
		for i := 1; i <= n; i++ {
			at = at.Add(period + offsets[i])
			m.observe(at)
		}
		return m.p99()
	}

	// 100 intervals: the 99th smallest deviation decides.
	if got := feed(map[int]time.Duration{5: 7 * time.Millisecond, 6: 7 * time.Millisecond}, 100); got != 7*time.Millisecond {
		t.Errorf("2 late frames in 100 intervals: p99 = %v, want 7ms", got)
	}
	if got := feed(map[int]time.Duration{50: 7 * time.Millisecond}, 100); got != 0 {
		t.Errorf("1 late frame in 100 intervals: p99 = %v, want 0", got)
	}
	// Early and late count the same.
	if got := feed(map[int]time.Duration{3: -9 * time.Millisecond, 4: -9 * time.Millisecond}, 100); got != 9*time.Millisecond {
		t.Errorf("2 early frames in 100 intervals: p99 = %v, want 9ms", got)
	}
	// A stall longer than the histogram covers saturates instead of overflowing.
	if got := feed(map[int]time.Duration{1: time.Hour, 2: time.Hour}, 100); got != maxJitter {
		t.Errorf("an hour-long stall: p99 = %v, want %v", got, maxJitter)
	}
}

func TestJitterSnapshot_P99OverAnInterval(t *testing.T) {
	const period = 50 * time.Millisecond
	m := newJitterMeter(period)
	at := time.Unix(1000, 0)
	m.observe(at)
	feed := func(n int, late map[int]time.Duration) {
		for i := 1; i <= n; i++ {
			at = at.Add(period + late[i])
			m.observe(at)
		}
	}

	// Interval 1: two of 100 intervals are 7 ms late, so the 99th smallest deviation is 7 ms.
	feed(100, map[int]time.Duration{5: 7 * time.Millisecond, 6: 7 * time.Millisecond})
	first := m.snapshot()
	// Interval 2: 100 intervals on time.
	feed(100, nil)
	second := m.snapshot()
	// Interval 3: 100 intervals, three of them 20 ms late.
	feed(100, map[int]time.Duration{10: 20 * time.Millisecond, 11: 20 * time.Millisecond, 12: 20 * time.Millisecond})
	third := m.snapshot()

	zero := JitterSnapshot{}
	if got := first.P99Since(zero); got != 7*time.Millisecond {
		t.Errorf("interval 1: p99 = %v, want 7ms", got)
	}
	if got := second.P99Since(first); got != 0 {
		t.Errorf("interval 2: p99 = %v, want 0: the late frames of interval 1 are not part of it", got)
	}
	if got := third.P99Since(second); got != 20*time.Millisecond {
		t.Errorf("interval 3: p99 = %v, want 20ms", got)
	}
	// Intervals 2 and 3 together: 3 of 200 intervals are late, so the 99th percentile is the 198th smallest.
	if got := third.P99Since(first); got != 20*time.Millisecond {
		t.Errorf("intervals 2 and 3: p99 = %v, want 20ms", got)
	}
	if got, want := third.P99Since(zero), m.p99(); got != want {
		t.Errorf("since the start: p99 = %v, want the p99 of the whole run, %v", got, want)
	}
	// An interval with no send, a zero snapshot and snapshots in the wrong order
	// have nothing to report.
	if got := third.P99Since(third); got != 0 {
		t.Errorf("empty interval: p99 = %v, want 0", got)
	}
	if got := zero.P99Since(zero); got != 0 {
		t.Errorf("zero snapshots: p99 = %v, want 0", got)
	}
	if got := first.P99Since(third); got != 0 {
		t.Errorf("earlier.P99Since(later) = %v, want 0", got)
	}
}

func TestJitterSnapshot_IsACopy(t *testing.T) {
	const period = 50 * time.Millisecond
	m := newJitterMeter(period)
	at := time.Unix(1000, 0)
	m.observe(at)
	m.observe(at.Add(period))
	snap := m.snapshot()
	// Later sends, even a huge stall, do not change a snapshot that was taken.
	m.observe(at.Add(time.Hour))
	if got := snap.P99Since(JitterSnapshot{}); got != 0 {
		t.Errorf("snapshot changed after it was taken: p99 = %v, want 0", got)
	}
}

func TestSim_JitterSnapshotSeparatesTheIntervals(t *testing.T) {
	const (
		fps    = 12
		delay  = 30 * time.Millisecond
		period = time.Second / fps
	)
	clock := newFakeClock()
	src := NewSim("sim", 1, synthAUs(48, 12), fps, 0, clock)
	start := clock.Now()
	tick := func(k int, extra time.Duration) {
		clock.tickAt(t, start.Add(time.Duration(k)*period+extra))
		waitFor(t, src.SentLog())
	}

	before := src.JitterSnapshot()
	for k := 1; k <= 100; k++ {
		extra := time.Duration(0)
		if k%10 == 0 {
			extra = delay // the send is late; the next one is on schedule again
		}
		tick(k, extra)
	}
	mid := src.JitterSnapshot()
	for k := 101; k <= 200; k++ {
		tick(k, 0)
	}
	end := src.JitterSnapshot()

	if got := mid.P99Since(before); got < delay || got > delay+time.Millisecond {
		t.Errorf("busy interval: p99 = %v, want about %v", got, delay)
	}
	if got := end.P99Since(mid); got > time.Millisecond {
		t.Errorf("idle interval: p99 = %v, want about 0", got)
	}
	if got := end.P99Since(JitterSnapshot{}); got != src.Jitter() {
		t.Errorf("since the start: p99 = %v, Jitter() = %v, want them equal", got, src.Jitter())
	}
}

func TestSystemClock_Ticks(t *testing.T) {
	var clock Clock = SystemClock{}
	before := clock.Now()
	tk := clock.NewTicker(5 * time.Millisecond)
	defer tk.Stop()
	select {
	case <-tk.C():
	case <-time.After(10 * time.Second):
		t.Fatal("the ticker did not fire")
	}
	if !clock.Now().After(before) {
		t.Error("Now did not advance")
	}
}
