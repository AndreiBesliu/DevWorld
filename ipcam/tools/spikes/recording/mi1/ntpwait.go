// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// MaxNTPWait bounds the wait for NTP sync after a boot (Review Focus 2).
const MaxNTPWait = 10 * time.Minute

// ntpPoll is how often the wait asks again.
const ntpPoll = 5 * time.Second

// NTPCheck reports whether the host's clock is synced by NTP.
type NTPCheck func(ctx context.Context) (bool, error)

// WaitNTP waits until check says the clock is synced, at most MaxNTPWait,
// asking every 5 s, and returns how long it waited and whether the clock
// synced (Review Focus 2). The loss never depends on it: the controller
// measures it on its own clock; the wait keeps the host's timestamps
// plausible and is recorded. A check that fails is asked again, the time
// service not running included (install starts it at every boot, C1-R16);
// one whose program does not exist, or that w32tm refuses to an
// unelevated recspike (ntpFinal), ends the wait at once, unsynced.
func WaitNTP(ctx context.Context, check NTPCheck, clock source.Clock) (waitMs int64, synced bool) {
	start := clock.Now()
	tick := clock.NewTicker(ntpPoll)
	defer tick.Stop()
	for {
		ok, err := check(ctx)
		waited := clock.Now().Sub(start)
		switch {
		case ok:
			return waited.Milliseconds(), true
		case ntpFinal(err), ctx.Err() != nil:
			return waited.Milliseconds(), false
		case waited >= MaxNTPWait:
			return waited.Milliseconds(), false
		}
		select {
		case <-ctx.Done():
			return clock.Now().Sub(start).Milliseconds(), false
		case <-tick.C():
		}
	}
}

// The failures of `w32tm /query /source` that the preflight names a remedy
// for: access denied, which waiting does not cure (the NTP wait ends at
// once, unsynced), and the time service not running.
var (
	// ErrNTPNeedsAdmin is «Access is denied» (0x80070005): w32tm answers
	// only an elevated recspike, as the boot task and the real cuts'
	// shortcut are.
	ErrNTPNeedsAdmin = errors.New("w32tm: access denied without administrator rights")
	// ErrTimeServiceStopped is «The service has not been started»
	// (0x80070426): Windows' time service (W32Time) is not running. On a PC
	// outside a domain it is stopped after a boot unless `recspike install`
	// set it to start automatically (C1-R16); after a boot it may still be
	// starting, so the NTP wait asks again.
	ErrTimeServiceStopped = errors.New("w32tm: the Windows time service is not running")
)

// The HRESULTs of those failures, which w32tm prints in hex in every
// language and returns as its exit code.
const (
	hresultAccessDenied      = 0x80070005
	hresultServiceNotStarted = 0x80070426
)

// w32tmError reads why `w32tm /query /source` failed, from its output or
// its exit code.
func w32tmError(out string, code uint32) error {
	text := strings.TrimSpace(out)
	has := func(hr uint32) bool {
		return code == hr || strings.Contains(strings.ToLower(text), fmt.Sprintf("0x%08x", hr))
	}
	switch {
	case has(hresultAccessDenied):
		return fmt.Errorf("%w: %s", ErrNTPNeedsAdmin, text)
	case has(hresultServiceNotStarted):
		return fmt.Errorf("%w: %s", ErrTimeServiceStopped, text)
	}
	return fmt.Errorf("w32tm /query /source: exit %#x: %s", code, text)
}

// ntpFinal reports whether a failed NTP check ends the wait: its program
// does not exist, or w32tm will not answer an unelevated recspike.
func ntpFinal(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, ErrNTPNeedsAdmin)
}

// SystemNTP is the host's own answer: `timedatectl show -p NTPSynchronized
// --value` on Linux, `w32tm /query /source` on Windows. The source alone
// has no label: /query /status's labels are translated, and the console
// writes them in its own code page (852 on a Romanian Windows), so matching
// them refused every PC set there (mi1-2).
func SystemNTP(goos string) NTPCheck {
	return func(ctx context.Context) (bool, error) {
		if goos == "windows" {
			out, err := runW32tm(ctx, "/query", "/source")
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return false, w32tmError(string(out)+string(exit.Stderr), uint32(exit.ExitCode())) //nolint:gosec // a Windows exit code is a DWORD
			}
			if err != nil {
				return false, err
			}
			return w32tmSynced(string(out)), nil
		}
		out, err := exec.CommandContext(ctx, "timedatectl", "show", "-p", "NTPSynchronized", "--value").Output()
		if err != nil {
			return false, err
		}
		return timedatectlSynced(string(out)), nil
	}
}

// runW32tm runs w32tm with args and returns its output; tests replace it.
var runW32tm = func(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "w32tm", args...).Output() //nolint:gosec // w32tm with recspike's own arguments
}

// timedatectlSynced reads timedatectl's answer: "yes" when synced.
func timedatectlSynced(out string) bool { return strings.TrimSpace(out) == "yes" }

// localSources are the sources that w32tm names, in English in every
// language, when the clock is not synced from the network.
var localSources = []string{"local cmos clock", "free-running system clock"}

// w32tmSynced reads `w32tm /query /source`'s answer, the clock's source:
// synced unless it is empty, the local clock, or an error that w32tm
// printed instead, which carries its HRESULT, "(0x8…)". Its bytes are read
// as they come: the console's code page changes none of these.
func w32tmSynced(out string) bool {
	v := strings.ToLower(strings.TrimSpace(out))
	if v == "" || strings.Contains(v, "(0x8") {
		return false
	}
	for _, local := range localSources {
		if strings.Contains(v, local) {
			return false
		}
	}
	return true
}
