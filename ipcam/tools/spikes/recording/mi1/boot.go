// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import "time"

// Boot identifies one boot of the host, so that recspike tells, after it
// asked the OS to stop (sysrq, bugcheck), whether the host rebooted since:
// on Linux the kernel's boot_id, which each boot draws anew; on Windows,
// which has none, the boot's wall time, the clock minus GetTickCount64. An
// uptime would not do: the uptime at the resume includes the wait for the
// controller after the boot, which can pass the old boot's uptime at the
// stop.
type Boot struct {
	ID string     `json:"id,omitempty"`
	At *time.Time `json:"at,omitempty"`
}

// bootTimeTolerance is how far two readings of one boot's wall time may
// differ: by the tick's resolution and the clock's corrections in between,
// well under a second on a synced clock (G14's NTP preflight). A reboot
// moves it by at least the old boot's uptime at the stop, which a run makes
// 3 min or more (G09).
const bootTimeTolerance = time.Minute

// same reports whether b and o may be one boot: the same boot id, or boot
// times within bootTimeTolerance. Boots that cannot be told apart, one of
// them unknown, are the same: recspike never assumes a reboot, which would
// measure a ColdBootMs without one.
func (b Boot) same(o Boot) bool {
	switch {
	case b.ID != "" && o.ID != "":
		return b.ID == o.ID
	case b.At != nil && o.At != nil:
		d := b.At.Sub(*o.At)
		return -bootTimeTolerance < d && d < bootTimeTolerance
	}
	return true
}
