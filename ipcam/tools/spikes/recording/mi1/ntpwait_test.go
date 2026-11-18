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

// TestNTPWait_ParsesTheSystemsAnswers (mi1-2): timedatectl says yes or no;
// `w32tm /query /source` prints only the clock's source, with no label to
// translate, so a Romanian Windows' answer, which its console writes in
// code page 852, reads as an English one: synced unless the source is
// empty or the local clock ("Local CMOS Clock", "Free-running System
// Clock"); an error printed instead of a source is no sync either.
func TestNTPWait_ParsesTheSystemsAnswers(t *testing.T) {
	for out, want := range map[string]bool{"yes\n": true, "no\n": false, "": false} {
		if got := timedatectlSynced(out); got != want {
			t.Errorf("timedatectlSynced(%q) = %v, want %v", out, got, want)
		}
	}
	for out, want := range map[string]bool{
		"time.windows.com,0x9\r\n":                true,  // an English Windows synced by NTP
		cp852("ro.pool.ntp.org,0x9\r\n"):          true,  // a Romanian Windows' console, in code page 852
		"VM IC Time Synchronization Provider\r\n": true,  // a virtual machine's host
		"Local CMOS Clock\r\n":                    false, // the local clock
		"Free-running System Clock\r\n":           false, // no source at all
		"":                                        false,
		" \r\n":                                   false,
		cp852("A apărut următoarea eroare: Serviciul nu a fost pornit. (0x80070426)\r\n"): false,
	} {
		if got := w32tmSynced(out); got != want {
			t.Errorf("w32tmSynced(%q) = %v, want %v", out, got, want)
		}
	}
}

// TestSystemNTP_AsksW32tmForTheSourceAlone (mi1-2): on Windows the NTP
// check runs `w32tm /query /source`, whose answer has no translated label,
// and reads a Romanian console's answer, in code page 852, as synced;
// /query /status, matched by its translated labels, refused every PC set
// on a Romanian Windows.
func TestSystemNTP_AsksW32tmForTheSourceAlone(t *testing.T) {
	var args []string
	old := runW32tm
	runW32tm = func(_ context.Context, a ...string) ([]byte, error) {
		args = a
		return []byte(cp852("ro.pool.ntp.org,0x9\r\n")), nil
	}
	t.Cleanup(func() { runW32tm = old })
	synced, err := SystemNTP("windows")(t.Context())
	if !synced || err != nil || len(args) != 2 || args[0] != "/query" || args[1] != "/source" {
		t.Errorf("SystemNTP(windows) = %v, %v with w32tm %q; want synced, from w32tm /query /source", synced, err, args)
	}
}

// cp852 encodes s as a Romanian Windows console writes it: in code page
// 852, where ă is 0xC7, â 0x83 and î 0x8C, and ș and ț, which it lacks,
// become its s and t with a cedilla, 0xAD and 0xEE; ASCII stays as it is.
func cp852(s string) string {
	var b []byte
	for _, r := range s {
		switch r {
		case 'ă':
			b = append(b, 0xc7)
		case 'â':
			b = append(b, 0x83)
		case 'î':
			b = append(b, 0x8c)
		case 'ș':
			b = append(b, 0xad)
		case 'ț':
			b = append(b, 0xee)
		default:
			if r >= 0x80 {
				panic("cp852: no byte for " + string(r))
			}
			b = append(b, byte(r)) //nolint:gosec // r < 0x80
		}
	}
	return string(b)
}

// TestNTPWait_WindowsErrors (Review Focus 2, G14, C1-R16): w32tm's
// failures are read from the HRESULT, which w32tm prints in hex in every
// language, or from its exit code. Access denied to a recspike without
// administrator rights (0x80070005), which waiting cannot cure, ends the
// NTP wait at once, unsynced, instead of after 10 min. The Windows time
// service not running (0x80070426) is asked again within the 10 min, like
// any other failure: install sets it to start automatically (C1-R16), so
// after a boot it is starting.
func TestNTPWait_WindowsErrors(t *testing.T) {
	for _, tc := range []struct {
		out  string
		code uint32
		want error
	}{
		{"The following error occurred: Access is denied. (0x80070005)\r\n", 1, ErrNTPNeedsAdmin},
		{cp852("S-a produs următoarea eroare: Acces refuzat. (0x80070005)\r\n"), 1, ErrNTPNeedsAdmin},
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

	checks := 0
	denied := func(context.Context) (bool, error) {
		checks++
		return false, w32tmError("Access is denied. (0x80070005)", 1)
	}
	if ms, synced := WaitNTP(t.Context(), denied, newStepClock()); ms != 0 || synced || checks != 1 {
		t.Errorf("access denied: WaitNTP = %d ms, %v after %d checks; want 0 ms, false, after 1", ms, synced, checks)
	}
	for _, failure := range []error{other, w32tmError("(0x80070426)", 1)} {
		checks := 0
		twice := func(context.Context) (bool, error) {
			checks++
			return checks > 2, failure
		}
		if ms, synced := WaitNTP(t.Context(), twice, newStepClock()); ms != 2*int64(ntpPoll/time.Millisecond) || !synced {
			t.Errorf("%v twice: WaitNTP = %d ms, %v; want asked again every 5 s, then synced", failure, ms, synced)
		}
	}
	checks = 0
	stopped := func(context.Context) (bool, error) {
		checks++
		return false, w32tmError("(0x80070426)", 1)
	}
	if ms, synced := WaitNTP(t.Context(), stopped, newStepClock()); ms != 600_000 || synced || checks != 121 {
		t.Errorf("a time service that never starts: WaitNTP = %d ms, %v after %d checks; want the 10 min, unsynced", ms, synced, checks)
	}
}
