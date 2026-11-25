// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi8

import (
	"fmt"
	"slices"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// G17's thresholds (§16.4, P43).
const (
	gate = "G17"
	// MinEpisodes is the reference episodes that a camera's coverage needs.
	MinEpisodes = 20
	// MinCoverage is the share of episodes that an event must match.
	MinCoverage = 0.95
	// MinHours is the observed hours that a pass needs.
	MinHours = 72.0
)

// The plans B of G17, per brand (PH0-31).
const (
	PlanBHikvision = "Hikvision `alertStream` (Technical Decision)"
	PlanBDahua     = "Dahua `eventManager` (Technical Decision)"
	PlanBReolink   = "Reolink push (Technical Decision)"
	PlanBUnknown   = "evenimente nesigure + FLW-16"
)

// The judge's texts, in Romanian.
const (
	NoteFewEpisodes      = "fără verdict: prea puține episoade (%d)"
	noteSubscribeFailed  = "abonare eșuată (configurare)"
	reasonNoEnvelope     = "nicio rulare MI8 (laboratorul și rezultatele cu variabile de test nu se judecă)"
	reasonHours          = "%.1f h observate din 72"
	reasonNoJudgedCamera = "nicio cameră cu cel puțin 20 de episoade"
	reasonSilentLoss     = "%s (%s): %d pierderi tăcute ale abonării"
	reasonCoverage       = "%s (%s): %d din %d episoade cu eveniment în 5 s (%.1f %%)"
	limitG17             = "≥ 95 % din episoade cu eveniment în 5 s (≥ 20 episoade); 0 pierderi tăcute; 72 h"
)

// PlanBFor is a brand's plan B: its native event adaptor, or «evenimente
// nesigure» + FLW-16.
func PlanBFor(brand string) string {
	b := strings.ToLower(brand)
	switch {
	case strings.Contains(b, "hikvision"):
		return PlanBHikvision
	case strings.Contains(b, "dahua"):
		return PlanBDahua
	case strings.Contains(b, "reolink"):
		return PlanBReolink
	}
	return PlanBUnknown
}

// brandName is a brand's display name for the plan B line: its own text,
// "necunoscută" when empty.
func brandName(brand string) string {
	if strings.TrimSpace(brand) == "" {
		return "marcă necunoscută"
	}
	return strings.TrimSpace(brand)
}

// Judge is G17 (P22, P43). Over the owner's MI8 envelopes (a lab envelope
// or one written with test seams is never judged), cameras are grouped by
// brand:
//   - a camera with a silent loss fails, whatever its episode count, and so
//     does a camera with at least 20 episodes and coverage below 95 %: its
//     brand's plan B; any failing camera fails G17, even when other parts
//     are missing;
//   - otherwise G17 passes with at least 72 observed hours and at least one
//     camera with 20 episodes, each such camera at 95 % or more; else it is
//     fără verdict.
//
// A camera whose subscription could not be created is reported apart from
// coverage, as «abonare eșuată (configurare)».
func Judge(envs []results.Envelope) results.Judgement {
	j := results.Judgement{Gate: gate, Reasons: []string{}, Rows: []results.Row{}, Decisions: []string{}}
	var hours float64
	var planBs []string
	judgedOK, judged := 0, 0
	failed := false
	for _, e := range envs {
		if e.Code != "MI8" || e.MI8 == nil || e.TestSeams || e.Host == "lab-linux" || e.Host == "lab-windows" {
			continue
		}
		judged++
		hours += e.MI8.Hours
		cams := slices.Clone(e.MI8.Cameras)
		slices.SortStableFunc(cams, func(a, b results.Mi8Camera) int {
			return strings.Compare(strings.ToLower(a.Brand), strings.ToLower(b.Brand))
		})
		for _, c := range cams {
			label := strings.TrimSpace(c.Brand + " " + c.Model)
			row := results.Row{Code: "MI8", Limit: limitG17, Source: "MI8/" + e.Set + "/" + e.RunID}
			switch {
			case c.SubscribeFailed:
				row.Value = fmt.Sprintf("%s (%s): %s", c.Camera, label, noteSubscribeFailed)
				j.Rows = append(j.Rows, row)
				continue
			case c.SilentLosses > 0:
				failed = true
				row.Pass = boolPtr(false)
				j.Reasons = append(j.Reasons, fmt.Sprintf(reasonSilentLoss, c.Camera, label, c.SilentLosses))
				planBs = appendOnce(planBs, brandPlanB(c.Brand))
			case c.Episodes >= MinEpisodes && c.Coverage < MinCoverage:
				failed = true
				row.Pass = boolPtr(false)
				j.Reasons = append(j.Reasons, fmt.Sprintf(reasonCoverage, c.Camera, label, c.Matched, c.Episodes, 100*c.Coverage))
				planBs = appendOnce(planBs, brandPlanB(c.Brand))
			case c.Episodes >= MinEpisodes:
				judgedOK++
				row.Pass = boolPtr(true)
			}
			row.Value = fmt.Sprintf("%s (%s): %d/%d episoade (%.1f %%), %d pierderi tăcute, L = %d ms, %s",
				c.Camera, label, c.Matched, c.Episodes, 100*c.Coverage, c.SilentLosses, c.RefLagMs, c.ReferenceMethod)
			if c.Episodes < MinEpisodes {
				row.Value += "; " + fmt.Sprintf(NoteFewEpisodes, c.Episodes)
			}
			j.Rows = append(j.Rows, row)
		}
	}
	switch {
	case failed:
		j.PlanB = strings.Join(planBs, "; ")
		j.Verdict = results.FailWith(j.PlanB)
		j.Decisions = append(j.Decisions, planBs...)
	case judged == 0:
		j.Verdict = results.Incomplete
		j.Reasons = append(j.Reasons, reasonNoEnvelope)
	case hours < MinHours || judgedOK == 0:
		j.Verdict = results.Incomplete
		if hours < MinHours {
			j.Reasons = append(j.Reasons, fmt.Sprintf(reasonHours, hours))
		}
		if judgedOK == 0 {
			j.Reasons = append(j.Reasons, reasonNoJudgedCamera)
		}
	default:
		j.Verdict = results.Pass
	}
	return j
}

// brandPlanB is the plan B line of a failing camera's brand.
func brandPlanB(brand string) string {
	p := PlanBFor(brand)
	if p == PlanBUnknown {
		return brandName(brand) + ": " + p
	}
	return p
}

// appendOnce appends s unless list holds it.
func appendOnce(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

func boolPtr(b bool) *bool { return &b }
