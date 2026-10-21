// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// BeaconEvery is the interval of recspike's liveness beacon.
const BeaconEvery = 50 * time.Millisecond

// Beacon is one beacon of a run: its sequence number, and At, the
// controller's clock when it arrived (C16).
type Beacon struct {
	Seq uint64
	At  time.Time
}

// stopJitterGaps is how many of the last intervals between beacons
// StopMoment looks at to bound the delay of the last one.
const stopJitterGaps = 20

// StopMoment is when the host stopped: the arrival of its last beacon. The
// host stopped after it sent that beacon and before the next was due, and
// the network may have delayed the last beacon by as much as the arrivals
// before it wandered, so errBound is BeaconEvery plus the largest deviation
// of the last 20 intervals from their schedule: about 50 ms on a LAN.
//
// kind does not change the result (Review Focus 5): a forced shutdown, a
// power cut and a kill end the beacon the same way. With no beacon, at is
// the zero time.
func StopMoment(kind ShutdownKind, beacons []Beacon) (at time.Time, errBound time.Duration) {
	_ = kind
	if len(beacons) == 0 {
		return time.Time{}, 0
	}
	at = beacons[0].At
	for _, b := range beacons[1:] {
		if b.At.After(at) {
			at = b.At
		}
	}
	var worst time.Duration
	for i := max(1, len(beacons)-stopJitterGaps); i < len(beacons); i++ {
		prev, cur := beacons[i-1], beacons[i]
		if cur.Seq <= prev.Seq {
			continue // reordered or repeated in the network
		}
		due := time.Duration(cur.Seq-prev.Seq) * BeaconEvery //nolint:gosec // a gap of a few beacons
		deviation := cur.At.Sub(prev.At) - due
		worst = max(worst, deviation, -deviation)
	}
	return at, BeaconEvery + worst
}

// beaconsKept is how many of a run's newest beacons the log keeps, besides
// its first: more than StopMoment looks at.
const beaconsKept = 64

// BeaconLog holds the beacons of the runs that the controller expects. It
// is safe for concurrent use.
type BeaconLog struct {
	mu      sync.Mutex
	runs    map[uint64]*beaconRun
	arrived chan struct{} // closed and replaced at each kept beacon
}

type beaconRun struct {
	first Beacon
	seen  bool
	last  [beaconsKept]Beacon // the newest beacons after the first, a ring
	n     int                 // beacons kept in last
	next  int                 // where the next one goes
}

// NewBeaconLog returns a log that expects no run yet.
func NewBeaconLog() *BeaconLog {
	return &BeaconLog{runs: map[uint64]*beaconRun{}, arrived: make(chan struct{})}
}

// Expect starts keeping the beacons of runID.
func (l *BeaconLog) Expect(runID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if hash := link.RunHash(runID); l.runs[hash] == nil {
		l.runs[hash] = &beaconRun{}
	}
}

// Forget drops the beacons of runID and stops keeping them.
func (l *BeaconLog) Forget(runID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.runs, link.RunHash(runID))
}

// Beacons returns the run's first beacon, then its newest ones after it, in
// the order they arrived.
func (l *BeaconLog) Beacons(runID string) []Beacon {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.runs[link.RunHash(runID)]
	if r == nil || !r.seen {
		return nil
	}
	out := []Beacon{r.first}
	start := (r.next - r.n + beaconsKept) % beaconsKept
	for i := range r.n {
		out = append(out, r.last[(start+i)%beaconsKept])
	}
	return out
}

// First returns the run's first beacon: its BootAt.
func (l *BeaconLog) First(runID string) (Beacon, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.runs[link.RunHash(runID)]
	if r == nil || !r.seen {
		return Beacon{}, false
	}
	return r.first, true
}

// WaitFirst waits until the run's first beacon has arrived.
func (l *BeaconLog) WaitFirst(ctx context.Context, runID string) (Beacon, error) {
	for {
		l.mu.Lock()
		arrived := l.arrived
		l.mu.Unlock()
		if b, ok := l.First(runID); ok {
			return b, nil
		}
		select {
		case <-ctx.Done():
			return Beacon{}, ctx.Err()
		case <-arrived:
		}
	}
}

// add keeps a beacon of the run with the given hash, if it is expected.
func (l *BeaconLog) add(run, seq uint64, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.runs[run]
	if r == nil {
		return
	}
	b := Beacon{Seq: seq, At: at}
	if r.seen {
		r.last[r.next] = b
		r.next = (r.next + 1) % beaconsKept
		r.n = min(r.n+1, beaconsKept)
	} else {
		r.first, r.seen = b, true
	}
	close(l.arrived)
	l.arrived = make(chan struct{})
}

// ReceiveBeacons reads beacons from conn, stamps each with clock, and keeps
// those of expected runs in log, until conn is closed or ctx is done; other
// datagrams are dropped.
func ReceiveBeacons(ctx context.Context, conn net.PacketConn, log *BeaconLog, clock source.Clock) error {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	buf := make([]byte, 64)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		at := clock.Now()
		if run, seq, ok := link.ParseBeacon(buf[:n]); ok {
			log.add(run, seq, at)
		}
	}
}
