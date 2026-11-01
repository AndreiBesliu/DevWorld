package oauth

import (
	"context"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	day          = 24 * time.Hour
	dailyDays    = 7                // days 1…7 (P53)
	dailyRetry   = 15 * time.Minute // a day's attempts come this far apart
	dailyMaxWait = 15 * time.Minute // RunDaily looks again at least this often
)

// dailyState is a login's daily refresh so far: its issuance, and each
// day's attempts, its next attempt, and whether it is decided.
type dailyState struct {
	issued   time.Time
	attempts [dailyDays + 1]int
	next     [dailyDays + 1]time.Time
	decided  [dailyDays + 1]bool
}

// RunDaily refreshes every login once a day until ctx ends (P53). Day N
// (1…7) has its first attempt at IssuedAt + N·24 h and retries every
// 15 min until IssuedAt + (N+1)·24 h. A day is ok at its first success and
// failed only when every attempt failed; a Google invalid_grant at or after
// IssuedAt + TestingExpiry is expected-expiry, never failed. emit gets each
// day once it is decided. Each daily success is recorded in the login's
// sealed row (login.DailyRefreshedAtMs), so after a restart a day that was
// ok is neither attempted nor emitted again (C2-R16 (d′)). A day that was
// failed or expected-expiry is attempted again when a restart falls in it,
// and a success then makes it ok: a success within its window.
func (s *Store) RunDaily(ctx context.Context, emit func(DailyResult)) error {
	for {
		next := s.dailyStep(ctx, emit)
		wait := dailyMaxWait
		if !next.IsZero() {
			wait = min(max(next.Sub(s.cfg.Clock()), time.Second), dailyMaxWait)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		case <-s.wake:
			timer.Stop()
		}
	}
}

// dailyStep makes every attempt that is due now, emits the days it
// decides, and returns when the next attempt is due (zero: none known).
func (s *Store) dailyStep(ctx context.Context, emit func(DailyResult)) time.Time {
	ids, err := s.cfg.Secrets.SecretIDs(ctx, loginPrefix)
	if err != nil {
		return time.Time{}
	}
	var next time.Time
	soonest := func(t time.Time) {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	for _, id := range ids {
		ref := strings.TrimPrefix(id, loginPrefix)
		_, l, ok, err := s.readLogin(ctx, ref)
		if err != nil || !ok {
			continue
		}
		if t, ok := s.dailyAttempt(ctx, ref, l, emit); ok {
			soonest(t)
		}
	}
	return next
}

// dailyAttempt makes the attempt of ref, whose row holds l, when one is due
// now, and returns when its next one is due, if ever.
func (s *Store) dailyAttempt(ctx context.Context, ref string, l login, emit func(DailyResult)) (time.Time, bool) {
	now := s.cfg.Clock()
	issued := time.UnixMilli(l.IssuedAtMs).UTC()
	s.mu.Lock()
	st := s.daily[ref]
	if st == nil || !st.issued.Equal(issued) {
		st = &dailyState{issued: issued} // a new login starts its days again
		s.daily[ref] = st
	}
	s.mu.Unlock()
	n := int(now.Sub(st.issued) / day)
	following := func() (time.Time, bool) {
		if n == dailyDays {
			return time.Time{}, false
		}
		return st.issued.Add(time.Duration(n+1) * day), true
	}
	switch {
	case n < 1:
		return st.issued.Add(day), true
	case n > dailyDays:
		return time.Time{}, false
	case st.decided[n]:
		return following()
	}
	first := st.issued.Add(time.Duration(n) * day)
	if at := time.UnixMilli(l.DailyRefreshedAtMs); l.DailyRefreshedAtMs != 0 && !at.Before(first) && at.Before(first.Add(day)) {
		// An earlier run of the hub refreshed this day and emitted it ok.
		st.decided[n] = true
		return following()
	}
	if st.next[n].IsZero() {
		st.next[n] = first
	}
	if now.Before(st.next[n]) {
		return st.next[n], true
	}
	st.attempts[n]++
	res, err := s.refresh(ctx, ref, true)
	result := DailyResult{DestID: ref, Day: n, Attempt: st.attempts[n], Rotated: res.Rotated}
	switch {
	case err == nil:
		result.Outcome = OutcomeOK
	case l.Provider == storage.KindGoogleDrive && s.cfg.TestingExpiry > 0 && isInvalidGrant(err) &&
		!now.Before(st.issued.Add(s.cfg.TestingExpiry)):
		result.Outcome = OutcomeExpectedExpiry
	default:
		// The next attempt on the 15-min grid of the day's first one.
		st.next[n] = first.Add((now.Sub(first)/dailyRetry + 1) * dailyRetry)
		if st.next[n].Before(first.Add(day)) {
			return st.next[n], true
		}
		result.Outcome = OutcomeFailed
	}
	st.decided[n] = true
	emit(result)
	return following()
}
