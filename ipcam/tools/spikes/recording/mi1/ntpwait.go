// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"errors"
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
// plausible and is recorded. A check that fails is asked again; one whose
// program does not exist ends the wait, unsynced.
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
		case errors.Is(err, exec.ErrNotFound), ctx.Err() != nil:
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

// SystemNTP is the host's own answer: `timedatectl show -p NTPSynchronized
// --value` on Linux, `w32tm /query /status` on Windows.
func SystemNTP(goos string) NTPCheck {
	return func(ctx context.Context) (bool, error) {
		if goos == "windows" {
			out, err := exec.CommandContext(ctx, "w32tm", "/query", "/status").Output()
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

// timedatectlSynced reads timedatectl's answer: "yes" when synced.
func timedatectlSynced(out string) bool { return strings.TrimSpace(out) == "yes" }

// localSources are the sources that w32tm names when the clock is not
// synced from the network.
var localSources = []string{"local cmos clock", "free-running system clock"}

// w32tmSynced reads w32tm's status: synced when its source, the value of
// the line labelled Source (or Sursă, on a Romanian Windows), is not the
// local clock.
func w32tmSynced(out string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		label, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(label)) {
		case "source", "sursă", "sursa":
			v := strings.ToLower(strings.TrimSpace(value))
			for _, local := range localSources {
				if strings.Contains(v, local) {
					return false
				}
			}
			return v != ""
		}
	}
	return false
}
