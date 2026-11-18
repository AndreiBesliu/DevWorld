package outbox

import "time"

const (
	backoffBase = time.Second     // the first retry's wait
	backoffCap  = 5 * time.Minute // the longest wait (§9.9)
)

// Backoff is the wait before the next attempt after n failures in a row,
// with r in [0, 1) as its jitter (§9.9, P25): 1 s + r·(min(5 min, 1 s·2^n) −
// 1 s). Every wait lies in [1 s, 5 min], and the cap holds from n = 9.
func Backoff(n int, r float64) time.Duration {
	ceiling := backoffCap
	if n < 30 { // past it the shift overflows, and the cap holds anyway
		ceiling = min(backoffCap, backoffBase<<max(n, 0))
	}
	return backoffBase + time.Duration(r*float64(ceiling-backoffBase))
}
