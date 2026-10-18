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
