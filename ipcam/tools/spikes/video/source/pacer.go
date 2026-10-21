package source

import (
	"sync"
	"time"
)

// Clock is the hub clock. Tests replace it to drive a simulator tick by tick.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
}

// Ticker delivers a tick on C every period until it is stopped.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// SystemClock is the Clock of the time package.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now() }

// NewTicker returns a ticker that fires every d.
func (SystemClock) NewTicker(d time.Duration) Ticker { return systemTicker{time.NewTicker(d)} }

type systemTicker struct{ t *time.Ticker }

func (s systemTicker) C() <-chan time.Time { return s.t.C }

func (s systemTicker) Stop() { s.t.Stop() }

// Jitter statistics are kept in a histogram, so a source that runs for hours
// still takes constant memory.
const (
	jitterResolution = 100 * time.Microsecond
	// maxJitter is where the histogram saturates: a larger deviation counts as
	// this one.
	maxJitter = 2 * time.Second
)

// jitterBuckets is the number of buckets of the jitter histogram.
const jitterBuckets = int(maxJitter/jitterResolution) + 1

// jitterMeter measures how far the send intervals of a source deviate from its
// frame period.
type jitterMeter struct {
	period time.Duration

	mu     sync.Mutex
	last   time.Time
	seen   bool
	counts [jitterBuckets]uint32
	total  uint64
}

func newJitterMeter(period time.Duration) *jitterMeter {
	return &jitterMeter{period: period}
}

// observe records a send at the given hub time. The first send only starts the
// measurement: a deviation needs an interval.
func (m *jitterMeter) observe(at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen {
		deviation := at.Sub(m.last) - m.period
		if deviation < 0 {
			deviation = -deviation
		}
		m.counts[min(deviation, maxJitter)/jitterResolution]++
		m.total++
	}
	m.last, m.seen = at, true
}

// p99 returns the smallest deviation that at least 99 % of the intervals
// stayed within, rounded down to the resolution of the histogram. It is 0 until
// the second send.
func (m *jitterMeter) p99() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return p99Of(&m.counts, nil, m.total)
}

// snapshot copies the histogram.
func (m *jitterMeter) snapshot() JitterSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := m.counts
	return JitterSnapshot{counts: &counts, total: m.total}
}

// p99Of returns the p99 of the total intervals that counts holds, less the
// ones that base holds, if it is not nil.
func p99Of(counts, base *[jitterBuckets]uint32, total uint64) time.Duration {
	if total == 0 {
		return 0
	}
	rank := (total*99 + 99) / 100 // ceil(0.99 * total)
	var seen uint64
	for i, n := range counts {
		if base != nil {
			n -= base[i]
		}
		seen += uint64(n)
		if seen >= rank {
			return time.Duration(i) * jitterResolution
		}
	}
	return maxJitter
}

// JitterSnapshot is the pacing statistics of a source at one moment. It lets a
// caller measure the jitter of an interval instead of the whole run: take a
// snapshot at the start of the interval and another at its end, and ask the
// later one for P99Since the earlier. The zero value stands for the start of
// the source.
type JitterSnapshot struct {
	counts *[jitterBuckets]uint32
	total  uint64
}

// P99Since is the p99 deviation of the frame send interval from the frame
// period, over the sends between the earlier snapshot and this one, rounded
// down to 0.1 ms. It is 0 when there was no send in between, and for a source
// that does not pace its sends. The snapshots must come from the same source,
// the earlier one first; when they do not, the result is 0 or meaningless.
func (s JitterSnapshot) P99Since(earlier JitterSnapshot) time.Duration {
	if s.counts == nil || earlier.total > s.total {
		return 0
	}
	return p99Of(s.counts, earlier.counts, s.total-earlier.total)
}

// pacer calls a function once per frame period and measures how evenly it does.
type pacer struct {
	ticker Ticker
	clock  Clock
	meter  *jitterMeter
}

// newPacer creates the ticker at once, so that a caller that drives a fake
// clock can fire it as soon as newPacer returns.
func newPacer(clock Clock, period time.Duration) *pacer {
	return &pacer{ticker: clock.NewTicker(period), clock: clock, meter: newJitterMeter(period)}
}

// run calls tick for every tick of the ticker, with the hub clock reading at
// the moment the tick is handled, so that time lost to a busy hub shows up as
// jitter. It returns when the ticker's channel is closed.
func (p *pacer) run(tick func(at time.Time)) {
	for range p.ticker.C() {
		at := p.clock.Now()
		p.meter.observe(at)
		tick(at)
	}
}

// jitter is the p99 deviation of the intervals between ticks from the period.
func (p *pacer) jitter() time.Duration { return p.meter.p99() }

// jitterSnapshot is the pacing statistics so far.
func (p *pacer) jitterSnapshot() JitterSnapshot { return p.meter.snapshot() }
