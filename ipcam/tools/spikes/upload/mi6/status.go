// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi6

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The status lines of an MI6 run, as `uploadspike status` prints them and
// the holder's status.html shows them (Task 12 checks the guide against
// them).
const (
	StatusHeader            = "MI6: ziua %d din 7 (pornită pe %s)."
	StatusPair              = "%s × %s: %s."
	StatusReconnect         = "Reconectați %s cu B1: uploadspike mi6 --b1 %s"
	StatusReconnectAfterMI5 = "Reconectați %s cu B1, după proba MI5: uploadspike mi6 --b1 %s"
	StatusVisibility        = "MI6, vizibilitate: %d din 9 celule măsurate."
	pairNoLogin             = "nicio autentificare încă"
	pairPlan                = "plan %s"
	pairB1Failed            = "B1 a eșuat (%s)"
	pairPlanAFailed         = "planul A a eșuat (%s), rulați B1"
	pairDaysOK              = "zile ok: %s"
	pairDaysFailed          = "zile eșuate: %s"
	pairDaysBlocked         = "zile oprite de test: %s"
	dateLayout              = "02.01.2006"
)

// StatusLines is the run's state for the owner at now, with the host's
// gaps; mi5Open words a reconnection for after MI5's run (MI5 is never
// handed over).
func StatusLines(r *Run, gaps []results.Gap, now time.Time, mi5Open bool) []string {
	lines := []string{fmt.Sprintf(StatusHeader, min(r.Day(now), Days), time.UnixMilli(r.StartMs).Format(dateLayout))}
	var reconnect []string
	for _, p := range r.Pairs {
		lines = append(lines, fmt.Sprintf(StatusPair, PlatformName(p.Platform), ProviderName(p.Provider), pairWords(p, gaps, now)))
		if NeedsB1(p, gaps, now) {
			format := StatusReconnect
			if mi5Open {
				format = StatusReconnectAfterMI5
			}
			reconnect = append(reconnect, fmt.Sprintf(format, PlatformName(p.Platform), p.Platform))
		}
	}
	measured := 0
	for _, c := range r.Visibility {
		if !c.Self {
			measured++
		}
	}
	lines = append(lines, fmt.Sprintf(StatusVisibility, measured))
	return append(lines, reconnect...)
}

// NeedsB1 reports whether a pair under plan A had a refresh day fail, or
// its redemption refused, so the owner runs its B1 (P39).
func NeedsB1(p *Pair, gaps []results.Gap, now time.Time) bool {
	if !p.HasPlanA() || p.Plan == PlanB1 {
		return false
	}
	if !p.HandoffOK {
		return p.PlanAFailure != ""
	}
	return slices.ContainsFunc(p.Series(gaps, now.UnixMilli()), func(d results.RefreshDay) bool {
		return d.Outcome == results.OutcomeFailed
	})
}

// pairWords is a pair's state in words.
func pairWords(p *Pair, gaps []results.Gap, now time.Time) string {
	switch {
	case p.Plan == PlanB1 && !p.HandoffOK:
		return fmt.Sprintf(pairB1Failed, p.B1Failure)
	case !p.HandoffOK && p.PlanAFailure != "":
		return fmt.Sprintf(pairPlanAFailed, p.PlanAFailure)
	case !p.HandoffOK:
		return pairNoLogin
	}
	parts := []string{fmt.Sprintf(pairPlan, p.Plan)}
	var okDays, failedDays, blockedDays []string
	for _, d := range p.Series(gaps, now.UnixMilli()) {
		switch d.Outcome {
		case results.OutcomeOK, results.OutcomeExpectedExpiry:
			okDays = append(okDays, strconv.Itoa(d.Day))
		case results.OutcomeFailed:
			failedDays = append(failedDays, strconv.Itoa(d.Day))
		case results.OutcomeBlockedByTest:
			blockedDays = append(blockedDays, strconv.Itoa(d.Day))
		}
	}
	for _, x := range []struct {
		format string
		days   []string
	}{{pairDaysOK, okDays}, {pairDaysFailed, failedDays}, {pairDaysBlocked, blockedDays}} {
		if len(x.days) > 0 {
			parts = append(parts, fmt.Sprintf(x.format, strings.Join(x.days, ", ")))
		}
	}
	return strings.Join(parts, "; ")
}
