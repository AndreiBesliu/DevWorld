// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import (
	"math/rand/v2"
	"time"
)

// The windows that the scheduler draws a prompt from, after BootAt (G09,
// C16): an automated stop anywhere in the owner window, a real cut's prompt
// early enough that the owner's reaction (up to 2 min) still ends inside it.
const (
	autoPromptMax = 12 * time.Minute
	realPromptMax = 10 * time.Minute
)

// SchedulePrompt draws the moment of the cut uniformly after bootAt: in
// [3, 12] min for kill9, sysrq, bugcheck and qemu-reset, in [3, 10] min for
// power and forced (and any other kind, which Valid refuses anyway).
func SchedulePrompt(bootAt time.Time, kind ShutdownKind, rnd *rand.Rand) time.Time {
	if kind.Automated() {
		return SchedulePromptWithin(bootAt, minBootToStop, autoPromptMax, rnd)
	}
	return SchedulePromptWithin(bootAt, minBootToStop, realPromptMax, rnd)
}

// SchedulePromptWithin draws the moment uniformly in [lo, hi] after bootAt,
// to the nanosecond. The smoke runs use [20 s, 40 s].
func SchedulePromptWithin(bootAt time.Time, lo, hi time.Duration, rnd *rand.Rand) time.Time {
	return bootAt.Add(lo + time.Duration(rnd.Int64N(int64(hi-lo)+1)))
}
