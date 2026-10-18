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

// jitterMeter measures how far the send intervals of a source deviate from its
// frame period.
type jitterMeter struct {
	period time.Duration

	mu     sync.Mutex
	last   time.Time
	seen   bool
	counts [maxJitter/jitterResolution + 1]uint32
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
	if m.total == 0 {
		return 0
	}
	rank := (m.total*99 + 99) / 100 // ceil(0.99 * total)
	var seen uint64
	for i, n := range m.counts {
		seen += uint64(n)
		if seen >= rank {
			return time.Duration(i) * jitterResolution
		}
	}
	return maxJitter
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
