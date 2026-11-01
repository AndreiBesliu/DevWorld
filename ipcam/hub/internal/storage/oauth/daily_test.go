package oauth

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakeoauth "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/oauth"
)

// runDays steps the daily refresh of s on e's clock from now until the
// clock reaches until, every step, and returns what it emitted, each with
// the time it was decided at.
func runDays(t *testing.T, e *env, s *Store, step time.Duration, until time.Time) ([]DailyResult, []time.Time) {
	t.Helper()
	var results []DailyResult
	var at []time.Time
	emit := func(r DailyResult) {
		results = append(results, r)
		at = append(at, e.clk.Now())
	}
	for e.clk.Now().Before(until) {
		s.dailyStep(t.Context(), emit)
		e.clk.Add(step)
	}
	return results, at
}

// TestRefresh_Day7Expiry_NotCountedAsFailure (Review Focus 1, P53): in
// Testing mode the Google refresh token dies 7 days after the consent: days
// 1–6 refresh ok, and day 7's invalid_grant is expected-expiry, never
// failed.
func TestRefresh_Day7Expiry_NotCountedAsFailure(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{TestingExpiry: 7 * 24 * time.Hour})
	s := e.store()
	s.rotationGrace = time.Millisecond
	issued := e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "").IssuedAt
	results, _ := runDays(t, e, s, 15*time.Minute, issued.Add(8*24*time.Hour+time.Hour))
	var outcomes []string
	for i, r := range results {
		if r.DestID != "g-windows" || r.Day != i+1 {
			t.Errorf("result %d: %+v", i, r)
		}
		outcomes = append(outcomes, r.Outcome)
	}
	want := []string{OutcomeOK, OutcomeOK, OutcomeOK, OutcomeOK, OutcomeOK, OutcomeOK, OutcomeExpectedExpiry}
	if !slices.Equal(outcomes, want) {
		t.Errorf("outcomes %q, want %q", outcomes, want)
	}
	if len(results) == 7 && (results[6].Attempt != 1 || !results[0].Rotated) {
		t.Errorf("day 7 at attempt %d, day 1 rotated %v; want day 7 decided at its first attempt, and the fake's rotation seen", results[6].Attempt, results[0].Rotated)
	}
}

// TestRefresh_DayCountedFromIssuance (P53): a login at 23:50 has day 1's
// first attempt 24 h later, not at the next midnight.
func TestRefresh_DayCountedFromIssuance(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	issued := e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "").IssuedAt
	if issued.Hour() != 23 || issued.Minute() != 50 {
		t.Fatalf("issued at %v, want 23:50", issued)
	}
	results, at := runDays(t, e, s, 10*time.Minute, issued.Add(25*time.Hour)) // the next midnight is a step
	if len(results) != 1 || results[0].Day != 1 || results[0].Outcome != OutcomeOK || results[0].Attempt != 1 {
		t.Fatalf("results %+v, want day 1 ok at its first attempt", results)
	}
	if !at[0].Equal(issued.Add(24 * time.Hour)) {
		t.Errorf("day 1 refreshed at %v, want %v, 24 h after the login", at[0], issued.Add(24*time.Hour))
	}
	if n := e.fake.Counts()["refresh_token"]; n != 1 {
		t.Errorf("%d refreshes, want 1: none at midnight", n)
	}
}

// TestRefresh_RetriesEvery15MinWithinDay (P53): a day's attempts come every
// 15 min from its first one; three failures then a success make the day ok
// with 4 attempts; a day whose 96 attempts all fail is failed.
func TestRefresh_RetriesEvery15MinWithinDay(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	issued := e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "").IssuedAt
	for range 3 {
		e.fake.FailNext(fakeoauth.PathGoogleToken, http.StatusServiceUnavailable, "temporarily_unavailable")
	}
	results, at := runDays(t, e, s, 5*time.Minute, issued.Add(48*time.Hour-time.Minute))
	if len(results) != 1 || results[0].Day != 1 || results[0].Outcome != OutcomeOK || results[0].Attempt != 4 {
		t.Fatalf("day 1: %+v, want ok at attempt 4", results)
	}
	if want := issued.Add(24*time.Hour + 45*time.Minute); !at[0].Equal(want) {
		t.Errorf("day 1's 4th attempt at %v, want %v: every 15 min", at[0], want)
	}
	for range 96 {
		e.fake.FailNext(fakeoauth.PathGoogleToken, http.StatusServiceUnavailable, "temporarily_unavailable")
	}
	results, at = runDays(t, e, s, 5*time.Minute, issued.Add(72*time.Hour))
	if len(results) != 1 || results[0].Day != 2 || results[0].Outcome != OutcomeFailed || results[0].Attempt != 96 {
		t.Fatalf("day 2: %+v, want failed after 96 attempts", results)
	}
	if want := issued.Add(48*time.Hour + 95*15*time.Minute); !at[0].Equal(want) {
		t.Errorf("day 2 failed at %v, want %v, its last attempt", at[0], want)
	}
}
