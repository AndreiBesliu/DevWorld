// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi9

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"time"
)

// Seeks is how many timeline seeks a measurement makes (MI9, G25); the
// slowest decides.
const Seeks = 20

// seekTargets draws n seek targets, in ms from the first sample, uniform
// in [0, lastIDRMs], as ipcam-hub's seekbench draws its own.
func seekTargets(lastIDRMs int64, n int) []int64 {
	targets := make([]int64, n)
	for i := range targets {
		targets[i] = rand.Int64N(lastIDRMs + 1) //nolint:gosec // a measurement's draw, not a secret
	}
	return targets
}

// seekResult is one seek as the client saw it: its time from the request
// until the first IDR's bytes had arrived, and those bytes. A seek that ctx
// ended (cut) has no IDR: its time is then how long it had waited, a lower
// bound.
type seekResult struct {
	ms  float64
	idr []byte
	cut bool
}

// seek is one timeline seek: it asks the hub side for target, in ms from
// the first sample, and times the request until the bytes of the first IDR
// at or after it have arrived; it returns once the IDR's whole part has, so
// that the next seek starts on an idle channel. When the wait for its part
// ends with an error, it returns that error with the seek's time all the
// same (timed), which a measurement that ctx ended keeps (cut).
func (c *Client) seek(ctx context.Context, target int64) (seekResult, error) {
	start := time.Now()
	cl, err := c.start(request{T: "seek", Ms: target}, io.Discard, true)
	if err != nil {
		return seekResult{}, err
	}
	r, err := c.wait(ctx, cl)
	if err != nil {
		return cl.timed(start, time.Now()), err
	}
	if r.T != "range" || r.IDRSize <= 0 || r.IDROff < r.Off || r.IDROff+r.IDRSize > r.Off+r.N {
		return seekResult{}, fmt.Errorf("mi9: the hub side answered the seek with %+v", r)
	}
	cl.mu.Lock()
	defer cl.mu.Unlock()
	at := r.IDROff - r.Off
	return seekResult{ms: msBetween(start, cl.idrAt), idr: cl.data[at : at+r.IDRSize]}, nil
}

// timed is a seek call begun at start that ended unfinished at now: its
// time to the IDR when the IDR's bytes had arrived, the seek being
// measured all the same; else, cut, the time it had waited, a lower bound.
func (cl *call) timed(start, now time.Time) seekResult {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if !cl.idrAt.IsZero() {
		return seekResult{ms: msBetween(start, cl.idrAt)}
	}
	return seekResult{ms: msBetween(start, now), cut: true}
}

// msBetween is the time from start to end in ms.
func msBetween(start, end time.Time) float64 { return float64(end.Sub(start).Microseconds()) / 1000 }

// serveSeek answers a seek to target: the part of the first IDR at or after
// it, found as ipcam-hub's seekbench finds it, and where the IDR lies.
func (h *Hub) serveSeek(call uint64, target time.Duration) {
	found, err := h.seg.firstIDR(target)
	if err != nil {
		h.fail(call, "out_of_range", err)
		return
	}
	h.serveRange(call, reply{T: "range", Call: call, Off: found.part.off, N: found.part.n, IDROff: found.sample.off, IDRSize: found.sample.n})
}
