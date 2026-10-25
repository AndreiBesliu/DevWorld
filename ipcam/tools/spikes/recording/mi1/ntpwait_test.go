package mi1

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// stepClock is a source.Clock whose tickers are always ready: each wait on
// a ticker's channel moves the clock by its period, so a wait of 10 min
// takes no real time.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func newStepClock() *stepClock {
	return &stepClock{now: time.Date(2026, 10, 25, 23, 0, 0, 0, time.Local)}
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func (c *stepClock) NewTicker(d time.Duration) source.Ticker { return &stepTicker{clock: c, d: d} }

// stepTicker ticks once per call of C, which a select makes each time it
// waits.
type stepTicker struct {
	clock *stepClock
	d     time.Duration
}

func (t *stepTicker) C() <-chan time.Time {
	t.clock.mu.Lock()
	t.clock.now = t.clock.now.Add(t.d)
	now := t.clock.now
	t.clock.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- now
	return ch
}

func (t *stepTicker) Stop() {}

// TestNTPWait_RecordsWaitAndGivesUpAfter10Min (Review Focus 2): after each
// boot, recspike waits for NTP sync at most 10 min and records how long it
// waited and whether the clock synced; a host whose check cannot run at all
// (no timedatectl) is not waited for.
func TestNTPWait_RecordsWaitAndGivesUpAfter10Min(t *testing.T) {
	checks := 0
	never := func(context.Context) (bool, error) {
		checks++
		return false, nil
	}
	clock := newStepClock()
	start := clock.Now()
	if ms, synced := WaitNTP(t.Context(), never, clock); ms != 600_000 || synced {
		t.Errorf("never synced: WaitNTP = %d ms, %v; want 600000 ms, false", ms, synced)
	}
	if waited := clock.Now().Sub(start); waited != MaxNTPWait || checks != 121 {
		t.Errorf("never synced: waited %v with %d checks, want 10 min, asked every 5 s", waited, checks)
	}

	clock = newStepClock()
	start = clock.Now()
	after3min := func(context.Context) (bool, error) { return !clock.Now().Before(start.Add(3 * time.Minute)), nil }
	if ms, synced := WaitNTP(t.Context(), after3min, clock); ms != 180_000 || !synced {
		t.Errorf("synced after 3 min: WaitNTP = %d ms, %v; want 180000 ms, true", ms, synced)
	}

	always := func(context.Context) (bool, error) { return true, nil }
	if ms, synced := WaitNTP(t.Context(), always, newStepClock()); ms != 0 || !synced {
		t.Errorf("synced at once: WaitNTP = %d ms, %v; want 0, true", ms, synced)
	}

	flaky := 0
	failsTwice := func(context.Context) (bool, error) {
		flaky++
		if flaky <= 2 {
			return false, errors.New("timedatectl: Failed to connect to bus")
		}
		return true, nil
	}
	if ms, synced := WaitNTP(t.Context(), failsTwice, newStepClock()); ms != 2*int64(ntpPoll/time.Millisecond) || !synced {
		t.Errorf("a check that fails twice: WaitNTP = %d ms, %v; want two polls, then true", ms, synced)
	}

	missing := func(context.Context) (bool, error) { return false, exec.ErrNotFound }
	if ms, synced := WaitNTP(t.Context(), missing, newStepClock()); ms != 0 || synced {
		t.Errorf("no check program: WaitNTP = %d ms, %v; want 0, false", ms, synced)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, synced := WaitNTP(ctx, never, newStepClock()); synced {
		t.Error("a cancelled wait reports a synced clock")
	}
}

// TestNTPWait_ParsesTheSystemsAnswers: timedatectl says yes or no; w32tm's
// status names a source, which is a sync only when it is not the local
// clock.
func TestNTPWait_ParsesTheSystemsAnswers(t *testing.T) {
	for out, want := range map[string]bool{"yes\n": true, "no\n": false, "": false} {
		if got := timedatectlSynced(out); got != want {
			t.Errorf("timedatectlSynced(%q) = %v, want %v", out, got, want)
		}
	}
	for out, want := range map[string]bool{
		"Leap Indicator: 0(no warning)\r\nSource: time.windows.com,0x9\r\nPoll Interval: 10 (1024s)\r\n": true,
		"Leap Indicator: 3(not synchronized)\r\nSource: Local CMOS Clock\r\n":                            false,
		"Source: Free-running System Clock\r\n":                                                          false,
		"Leap Indicator: 0\r\nSursă: ro.pool.ntp.org\r\n":                                                true,
		"The following error occurred: The service has not been started. (0x80070426)\r\n":               false,
	} {
		if got := w32tmSynced(out); got != want {
			t.Errorf("w32tmSynced(%q) = %v, want %v", out, got, want)
		}
	}
}

// TestNTPWait_WindowsErrorsEndTheWaitAtOnce (Review Focus 2, G14): w32tm's
// failures that waiting cannot cure end the NTP wait at once, unsynced,
// instead of after 10 min: access denied to a recspike without
// administrator rights (0x80070005), and the Windows time service not
// running (0x80070426), as on a PC outside a domain after a boot. They are
// read from the HRESULT, which w32tm prints in hex in every language, or
// from its exit code; any other failure is asked again.
func TestNTPWait_WindowsErrorsEndTheWaitAtOnce(t *testing.T) {
	for _, tc := range []struct {
		out  string
		code uint32
		want error
	}{
		{"The following error occurred: Access is denied. (0x80070005)\r\n", 1, ErrNTPNeedsAdmin},
		{"S-a produs următoarea eroare: Acces refuzat. (0x80070005)\r\n", 1, ErrNTPNeedsAdmin},
		{"", 0x80070005, ErrNTPNeedsAdmin},
		{"The following error occurred: The service has not been started. (0x80070426)\r\n", 1, ErrTimeServiceStopped},
		{"", 0x80070426, ErrTimeServiceStopped},
	} {
		if err := w32tmError(tc.out, tc.code); !errors.Is(err, tc.want) {
			t.Errorf("w32tmError(%q, %#x) = %v, want %v", tc.out, tc.code, err, tc.want)
		}
	}
	other := w32tmError("The following error occurred: The RPC server is unavailable. (0x800706BA)\r\n", 0x800706ba)
	if other == nil || errors.Is(other, ErrNTPNeedsAdmin) || errors.Is(other, ErrTimeServiceStopped) {
		t.Errorf("another failure = %v, want an error asked again", other)
	}

	for _, final := range []error{w32tmError("Access is denied. (0x80070005)", 1), w32tmError("(0x80070426)", 1)} {
		checks := 0
		check := func(context.Context) (bool, error) {
			checks++
			return false, final
		}
		if ms, synced := WaitNTP(t.Context(), check, newStepClock()); ms != 0 || synced || checks != 1 {
			t.Errorf("%v: WaitNTP = %d ms, %v after %d checks; want 0 ms, false, after 1", final, ms, synced, checks)
		}
	}
	checks := 0
	rpc := func(context.Context) (bool, error) {
		checks++
		return checks > 1, other
	}
	if ms, synced := WaitNTP(t.Context(), rpc, newStepClock()); ms != int64(ntpPoll/time.Millisecond) || !synced {
		t.Errorf("another failure: WaitNTP = %d ms, %v; want asked again after 5 s", ms, synced)
	}
}
