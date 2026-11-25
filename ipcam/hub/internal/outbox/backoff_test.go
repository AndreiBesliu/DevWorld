package outbox

import (
	"math/rand/v2"
	"testing"
	"time"
)

// TestOutbox_Backoff (P25, §9.9): 1 s + r·(min(5 min, 1 s·2^n) − 1 s), so
// every delay lies in [1 s, 5 min]: 1 s at r = 0, 2^n s as r nears 1 up to
// the cap, reached at n = 9, and jittered between those bounds.
func TestOutbox_Backoff(t *testing.T) {
	for n := range 21 {
		if d := Backoff(n, 0); d != time.Second {
			t.Errorf("Backoff(%d, 0) = %s, want 1s", n, d)
		}
	}
	const r = 0.999999
	for n := 1; n <= 20; n++ {
		want := 300 * time.Second
		if n <= 8 {
			want = time.Duration(1<<n) * time.Second
		}
		if d := Backoff(n, r); d < want-time.Millisecond || d > want+time.Millisecond {
			t.Errorf("Backoff(%d, %v) = %s, want %s ± 1 ms", n, r, d, want)
		}
	}
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // G404: a seeded jitter for the test, no secret
	seen := map[time.Duration]bool{}
	for i := range 50 {
		d := Backoff(i%12, rng.Float64())
		if d < time.Second || d > 300*time.Second {
			t.Errorf("Backoff(%d, …) = %s, outside [1s, 5m]", i%12, d)
		}
		seen[d] = true
	}
	if len(seen) < 2 {
		t.Errorf("50 jittered delays are all %v", seen)
	}
}
