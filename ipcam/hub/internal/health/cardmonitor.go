package health

import (
	"slices"
	"time"
)

// CardHealth is the state of §13.1's „Card SD” row, while rec/ is on an SD
// card (T40, HUB-17).
type CardHealth int

// The states of the card.
const (
	CardOK CardHealth = iota
	CardDegraded
	CardFailed
)

// String is the state as ipcam-hub's card event names it.
func (h CardHealth) String() string {
	switch h {
	case CardOK:
		return "ok"
	case CardDegraded:
		return "degraded"
	}
	return "failed"
}

// The kinds of IoEvent.
const (
	IoEIO   = "eio"   // an I/O error on the card (EIO, or a write that failed on it)
	IoEROFS = "erofs" // the card is mounted read-only
	IoFsync = "fsync" // a probe's fsync, which took Latency
)

// IoEvent is what the card monitor observes.
type IoEvent struct {
	Kind    string // IoEIO, IoEROFS or IoFsync
	Latency time.Duration
}

// IoStats are the card's figures that §13.1 judges.
type IoStats struct {
	IOErrors24h, IOErrors60s int
	ReadOnly                 bool
	FsyncP99_10m             time.Duration //nolint:revive // the spec's name: p99 over 10 min
}

// §13.1's thresholds for the „Card SD” row, verbatim: OK is 0 I/O errors in
// 24 h and fsync p99 ≤ 500 ms over 10 min; DEGRADED is one I/O error in 24
// h, or fsync p99 > 500 ms over 10 min; FAILED is 3 I/O errors in 60 s, or
// a read-only remount. §13.3 closes an alert after 2 min of good state.
const (
	errorWindow    = 24 * time.Hour
	burstWindow    = 60 * time.Second
	burstErrors    = 3
	fsyncWindow    = 10 * time.Minute
	fsyncP99Limit  = 500 * time.Millisecond
	alertClearedBy = 2 * time.Minute
)

// fsyncSample is one probe's fsync.
type fsyncSample struct {
	at      time.Time
	latency time.Duration
}

// CardMonitor is the card's state machine (G17), driven by the caller's
// clock: the sampler in ipcam-hub feeds it, and tests drive it on a fake
// clock. FAILED lasts: a card that failed stays failed until ipcam-hub
// starts again, since its jobs are stopped. It is not safe for concurrent
// use.
type CardMonitor struct {
	errors   []time.Time   // the I/O errors of the last 24 h
	fsyncs   []fsyncSample // the probes of the last 10 min
	readOnly bool
	failed   bool
	// alertRaised is true while the card alert is raised (§13.3): from the
	// first state that is not OK until 2 min of OK, okSince.
	alertRaised bool
	okSince     time.Time
}

// NewCardMonitor returns a monitor of a card that is OK.
func NewCardMonitor() *CardMonitor { return &CardMonitor{} }

// Observe adds ev, at now, and returns the card's state and whether the
// card alert is raised now: once, when the state leaves OK, and again only
// after the card was OK for 2 min (§13.3).
func (m *CardMonitor) Observe(ev IoEvent, now time.Time) (CardHealth, bool) {
	m.forget(now)
	switch ev.Kind {
	case IoEIO:
		if !m.failed {
			m.errors = append(m.errors, now)
		}
	case IoEROFS:
		m.readOnly = true
	case IoFsync:
		m.fsyncs = append(m.fsyncs, fsyncSample{at: now, latency: ev.Latency})
	}
	state := m.state(now)
	if state == CardOK {
		if m.okSince.IsZero() {
			m.okSince = now
		}
		if m.alertRaised && now.Sub(m.okSince) >= alertClearedBy {
			m.alertRaised = false
		}
		return state, false
	}
	m.okSince = time.Time{}
	raised := !m.alertRaised
	m.alertRaised = true
	return state, raised
}

// Stats are the card's figures at now.
func (m *CardMonitor) Stats(now time.Time) IoStats {
	m.forget(now)
	return IoStats{IOErrors24h: len(m.errors), IOErrors60s: m.errorsSince(now.Add(-burstWindow)), ReadOnly: m.readOnly, FsyncP99_10m: m.fsyncP99()}
}

// state is the card's state at now, with what it observed until now.
func (m *CardMonitor) state(now time.Time) CardHealth {
	if m.readOnly || m.errorsSince(now.Add(-burstWindow)) >= burstErrors {
		m.failed = true
	}
	switch {
	case m.failed:
		return CardFailed
	case len(m.errors) > 0 || m.fsyncP99() > fsyncP99Limit:
		return CardDegraded
	}
	return CardOK
}

// forget drops the errors older than 24 h and the probes older than 10 min.
func (m *CardMonitor) forget(now time.Time) {
	m.errors = slices.DeleteFunc(m.errors, func(at time.Time) bool { return !at.After(now.Add(-errorWindow)) })
	m.fsyncs = slices.DeleteFunc(m.fsyncs, func(s fsyncSample) bool { return !s.at.After(now.Add(-fsyncWindow)) })
}

// errorsSince counts the errors after from.
func (m *CardMonitor) errorsSince(from time.Time) int {
	n := 0
	for _, at := range m.errors {
		if !at.Before(from) {
			n++
		}
	}
	return n
}

// fsyncP99 is the 99th percentile of the probes' fsyncs, by nearest rank: of
// fewer than 100 probes, the slowest. 0 without any.
func (m *CardMonitor) fsyncP99() time.Duration {
	if len(m.fsyncs) == 0 {
		return 0
	}
	latencies := make([]time.Duration, len(m.fsyncs))
	for i, s := range m.fsyncs {
		latencies[i] = s.latency
	}
	slices.Sort(latencies)
	rank := (len(latencies)*99 + 99) / 100 // ⌈0.99 n⌉
	return latencies[rank-1]
}
