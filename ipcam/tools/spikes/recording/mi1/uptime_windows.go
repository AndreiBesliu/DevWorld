// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"time"

	"golang.org/x/sys/windows"
)

var getTickCount64 = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount64")

// Uptime is how long the host has run since its boot, on its own boot
// clock, which NTP never shifts: GetTickCount64, in milliseconds.
func Uptime() (time.Duration, error) {
	if err := getTickCount64.Find(); err != nil {
		return 0, err
	}
	ms, _, _ := getTickCount64.Call()
	return time.Duration(ms) * time.Millisecond, nil //nolint:gosec // milliseconds since boot, far below 2^63
}

// ThisBoot is the host's boot: its wall time, the clock minus the uptime;
// Windows has no boot id. Two readings of one boot differ by the tick's
// resolution and the clock's corrections in between (bootTimeTolerance).
func ThisBoot() (Boot, error) {
	up, err := Uptime()
	if err != nil {
		return Boot{}, err
	}
	at := time.Now().Round(0).Add(-up)
	return Boot{At: &at}, nil
}
