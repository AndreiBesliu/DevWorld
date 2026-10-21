// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import (
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// PacingStats is how evenly the controller sent during one run (R07): P99 is
// the largest p99 deviation of any stream's send interval from its frame
// period, from the simulators' JitterSnapshot; SkippedAUs counts the access
// units that a session dropped or failed to write. A run with P99 above
// 100 ms or any skipped AU is invalid and redone, so the controller's own
// delays never read as the host's loss.
type PacingStats struct {
	P99        time.Duration
	SkippedAUs int
}

// PacingMark is the pacing state at the start of a run.
type PacingMark struct {
	at     time.Time
	jitter map[string]source.JitterSnapshot
}

// skipBuffer bounds the skip records: the oldest go first. A record is one
// access unit or a run of them, at most one per frame of a session, so the
// buffer holds far more than a run can be asked about, and a run with one
// skip is invalid anyway.
const skipBuffer = 1 << 16

// pacing measures the simulators' send jitter and records skipped access
// units with the controller's clock, so that each run takes only its own.
type pacing struct {
	sims map[string]source.Source

	mu    sync.Mutex
	skips []skip
}

type skip struct {
	at time.Time
	n  int
}

func newPacing(sims map[string]source.Source) *pacing {
	return &pacing{sims: sims}
}

// mark captures the pacing state at at, the start of a run.
func (p *pacing) mark(at time.Time) PacingMark {
	m := PacingMark{at: at, jitter: make(map[string]source.JitterSnapshot, len(p.sims))}
	for name, sim := range p.sims {
		m.jitter[name] = sim.JitterSnapshot()
	}
	return m
}

// skipped records that n access units were skipped at at.
func (p *pacing) skipped(at time.Time, n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.skips = append(p.skips, skip{at: at, n: n})
	if len(p.skips) > skipBuffer {
		p.skips = p.skips[len(p.skips)-skipBuffer:] // append copies only these when it grows
	}
}

// since returns the pacing of the run that started at m and stopped at
// until: the jitter of every send since m, and the skips in [m, until].
// After the stop, a host that lost power leaves its session's writes to
// fail until the connection times out; those belong to no run.
func (p *pacing) since(m PacingMark, until time.Time) PacingStats {
	var stats PacingStats
	for name, sim := range p.sims {
		stats.P99 = max(stats.P99, sim.JitterSnapshot().P99Since(m.jitter[name]))
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.skips {
		if !s.at.Before(m.at) && !s.at.After(until) {
			stats.SkippedAUs += s.n
		}
	}
	return stats
}
