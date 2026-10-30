// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// SendTimes is when the controller sent each access unit of a stream, by
// its SEI index, on the controller's clock: *controller.SendLog.
type SendTimes interface {
	SendTime(stream string, index uint64) (time.Time, bool)
}

// latencyBuckets bounds the histogram: 1 ms buckets up to a minute, and
// one bucket for anything later.
const latencyBuckets = 60_000

// Latencies is the added latency of the live tiles (§4.8: ≤ 150 ms p95):
// for each access unit that a live peer receives, the controller's receive
// time minus the send time of its SEI index in the send log (C15), both on
// the controller's clock, so that no host clock and no RTP timestamp
// counts. It takes each sample as the access unit arrives, while the send
// log still holds its index, into a histogram of 1 ms buckets that an
// hour of tiles fits in. It is safe for concurrent use.
type Latencies struct {
	sent SendTimes

	mu      sync.Mutex
	buckets []int
	n       int
}

// NewLatencies returns an empty set of samples over sent.
func NewLatencies(sent SendTimes) *Latencies {
	return &Latencies{sent: sent, buckets: make([]int, latencyBuckets+1)}
}

// Arrived takes the access unit nalus of stream that a live peer received
// at at: one sample when it carries an SEI index that stream's log holds.
func (l *Latencies) Arrived(stream string, nalus [][]byte, at time.Time) {
	index, ok := controller.ReadIndex(nalus)
	if !ok {
		return
	}
	sent, ok := l.sent.SendTime(stream, index)
	if !ok {
		return
	}
	ms := max(0, at.Sub(sent).Milliseconds())
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buckets[min(ms, latencyBuckets)]++
	l.n++
}

// P95 is the 95th percentile of the samples (nearest rank), to the
// millisecond, and their number; 0 without samples.
func (l *Latencies) P95() (time.Duration, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.n == 0 {
		return 0, 0
	}
	rank := (95*l.n + 99) / 100 // ⌈0.95 n⌉
	seen := 0
	for ms, count := range l.buckets {
		if seen += count; seen >= rank {
			return time.Duration(ms) * time.Millisecond, l.n
		}
	}
	return latencyBuckets * time.Millisecond, l.n
}
