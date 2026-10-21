// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import "time"

// ShutdownKind is how a run stops the host.
type ShutdownKind string

// The kinds of stop. kill9, sysrq, bugcheck and qemu-reset are automated:
// recspike or the lab stops the host when the controller sends `cut`. power
// and forced are real cuts that the owner makes when prompted; forced is the
// hold-power shutdown of a laptop whose battery would ride through a pulled
// plug (Review Focus 5).
const (
	KindKill9     ShutdownKind = "kill9"
	KindSysrq     ShutdownKind = "sysrq"
	KindBugcheck  ShutdownKind = "bugcheck"
	KindQEMUReset ShutdownKind = "qemu-reset"
	KindPower     ShutdownKind = "power"
	KindForced    ShutdownKind = "forced"
)

// Automated reports whether the stop follows `cut` within milliseconds,
// without the owner.
func (k ShutdownKind) Automated() bool {
	switch k {
	case KindKill9, KindSysrq, KindBugcheck, KindQEMUReset:
		return true
	}
	return false
}

// Known reports whether k is one of the six kinds.
func (k ShutdownKind) Known() bool {
	return k.Automated() || k == KindPower || k == KindForced
}

// Cut is one stop of a host, with the controller's timestamps (C16): BootAt
// is the run's first beacon, PromptAt when the controller sent `cut` (for an
// automated kind, the scheduled stop), StopAt the last beacon.
type Cut struct {
	RunID                    string
	Kind                     ShutdownKind
	BootAt, PromptAt, StopAt time.Time
}

// AutoStopSlack is how long before PromptAt the last beacon of an automated
// stop may arrive: it left up to one beacon interval before the stop, which
// follows the prompt by milliseconds, plus StopMoment's bound.
const AutoStopSlack = BeaconEvery + 100*time.Millisecond

// promptToStop is how long after the prompt a stop still counts.
const promptToStop = 2 * time.Minute

// The owner window of Review Focus 1: boot to stop in [3 min, 12 min].
const (
	minBootToStop = 3 * time.Minute
	maxBootToStop = 12 * time.Minute
)

// Valid reports whether the cut counts toward MI1, in the owner window.
func Valid(c Cut) bool { return ValidWithin(c, minBootToStop, maxBootToStop) }

// ValidWithin reports whether the cut counts, with boot-to-stop window
// [minBoot, maxBoot] (the smoke runs use a shorter one):
//
//   - power and forced (the owner reacts to the prompt): minBoot ≤ StopAt −
//     BootAt ≤ maxBoot and PromptAt ≤ StopAt ≤ PromptAt + 2 min. A stop
//     before the prompt, even by 40 ms, was not prompted.
//   - kill9, sysrq, bugcheck and qemu-reset (the stop follows `cut` within
//     milliseconds): minBoot ≤ PromptAt − BootAt ≤ maxBoot, the scheduler's
//     draw, and PromptAt − AutoStopSlack ≤ StopAt ≤ PromptAt + 2 min, because
//     on a LAN the last beacon usually arrives just before PromptAt.
//
// A cut with a missing timestamp or an unknown kind is invalid.
func ValidWithin(c Cut, minBoot, maxBoot time.Duration) bool {
	if !c.Kind.Known() || c.BootAt.IsZero() || c.PromptAt.IsZero() || c.StopAt.IsZero() {
		return false
	}
	within := func(d, lo, hi time.Duration) bool { return lo <= d && d <= hi }
	stopAfterPrompt := c.StopAt.Sub(c.PromptAt)
	if c.Kind.Automated() {
		return within(c.PromptAt.Sub(c.BootAt), minBoot, maxBoot) && within(stopAfterPrompt, -AutoStopSlack, promptToStop)
	}
	return within(c.StopAt.Sub(c.BootAt), minBoot, maxBoot) && within(stopAfterPrompt, 0, promptToStop)
}

// Loss is how much of stream the host lost at the stop: StopAt minus the
// time when the controller sent the last frame that the host recorded, both
// on the controller's clock (Review Focus 2). ok is false without a stop
// moment or a send time for the index.
func Loss(stopAt time.Time, stream string, lastRecordedIndex uint64, log *SendLog) (time.Duration, bool) {
	if stopAt.IsZero() {
		return 0, false
	}
	sent, ok := log.SendTime(stream, lastRecordedIndex)
	if !ok {
		return 0, false
	}
	return stopAt.Sub(sent), true
}
