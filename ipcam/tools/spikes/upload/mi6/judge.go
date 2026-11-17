// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi6

import (
	"fmt"
	"slices"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The G15 texts (P22, P39, §2.2), verbatim.
const (
	gate        = "G15"
	planB2      = "B2 (proxy fără stare în cloud, §5.6)"
	planF6      = "F6 folosește tipul de client al hub-ului"
	oneDriveA5  = "înregistrarea Microsoft cere un director plătit (A5)"
	decisionB1  = "G15: %s × %s folosește B1 (device flow pe hub)."
	decisionB2  = "G15: %s × %s trece pe B2: proxy fără stare în cloud (§5.6), construit cu cloud-ul în Faza 2; rândurile din harta datelor și din politica de confidențialitate se actualizează întâi."
	decisionF6  = "F6 pe %s folosește tipul de client al hub-ului (creatori: %s)."
	limitPair   = "plan A, sau B1 după un eșec al planului A; zilele 1–6 ok"
	limitMatrix = "9 celule judecate, fiecare listată și citită"
	judgedDays  = 6 // days 1–6; day 7 is the Testing project's expiry (Review Focus 1)
)

// pairState is a judged pair's state.
type pairState int

const (
	pairPass pairState = iota
	pairIncomplete
	pairFail
)

// Judge is G15's Phase 0 part (P22, P39): the Drive pairs of Android,
// Windows and web, the OneDrive pairs only when A5 passed (else deferred to
// Faza 0-bis), and the visibility matrix (P21). A pair passes with its
// handoff and days 1–6 ok, under plan A, or under B1 after a plan A
// failure. A failed day or redemption under plan A is never final (B1 runs
// that day); a failed B1 handoff, or a failed day under B1, gives B2. A
// failed judged visibility cell gives F6's decision for its reader's
// platform. A measured failure decides even when other parts are missing;
// otherwise a missing or blocked part, or an empty set, has no verdict.
func Judge(envs []results.Envelope) results.Judgement {
	j := results.Judgement{Gate: gate, Reasons: []string{}, Rows: []results.Row{}, Decisions: []string{}}
	pairs, source, oneDrive, cells := latest(envs)
	var b2, incomplete []string
	judgedPairs := 0
	for _, def := range append(slices.Clone(Drive), OneDrive...) {
		if def.Provider == ProviderOneDrive && !oneDrive {
			j.Rows = append(j.Rows, results.Row{Code: "MI6", Value: def.label() + ": " + string(results.Deferred(oneDriveA5)),
				Limit: limitPair, Source: source})
			continue
		}
		p, ok := pairs[def.Dest]
		state, value, decision := judgePair(def, p, ok, pairs)
		row := results.Row{Code: "MI6", Value: def.label() + ": " + value, Limit: limitPair, Source: source}
		switch state {
		case pairPass:
			judgedPairs++
			row.Pass = boolPtr(true)
		case pairFail:
			judgedPairs++
			row.Pass = boolPtr(false)
			b2 = append(b2, def.label())
		default:
			incomplete = append(incomplete, def.label()+": "+value)
		}
		if decision != "" {
			j.Decisions = append(j.Decisions, decision)
		}
		j.Rows = append(j.Rows, row)
	}
	f6, matrixValue, matrixDone := judgeMatrix(cells)
	matrixRow := results.Row{Code: "MI6", Value: "vizibilitate: " + matrixValue, Limit: limitMatrix, Source: source}
	if matrixDone || len(f6) > 0 {
		matrixRow.Pass = boolPtr(len(f6) == 0)
	}
	j.Rows = append(j.Rows, matrixRow)
	j.Decisions = append(j.Decisions, f6...)
	switch {
	case len(b2) > 0:
		j.Verdict, j.PlanB = results.FailWith(planB2), planB2
		j.Reasons = append(j.Reasons, "B1 a eșuat pentru: "+strings.Join(b2, ", "))
		if len(f6) > 0 {
			j.Reasons = append(j.Reasons, "și vizibilitatea între clienți a eșuat (F6)")
		}
	case len(f6) > 0:
		j.Verdict, j.PlanB = results.FailWith(planF6), planF6
		j.Reasons = append(j.Reasons, "vizibilitatea între clienți a eșuat: "+matrixValue)
	case judgedPairs == 0:
		j.Verdict = results.Incomplete
		j.Reasons = append(j.Reasons, "nicio pereche măsurată")
	case len(incomplete) > 0 || !matrixDone:
		j.Verdict = results.Incomplete
		j.Reasons = append(j.Reasons, incomplete...)
		if !matrixDone {
			j.Reasons = append(j.Reasons, "vizibilitate: "+matrixValue)
		}
	default:
		j.Verdict = results.Pass
	}
	return j
}

// latest gathers the MI6 envelopes' pairs, each dest's from the envelope
// stored last, the latest visibility matrix, whether A5 passed, and the
// source of the rows (the hosts, joined).
func latest(envs []results.Envelope) (map[string]results.Mi6Pair, string, bool, []results.VisibilityCell) {
	pairs := map[string]results.Mi6Pair{}
	at := map[string]int64{}
	var hosts []string
	var cells []results.VisibilityCell
	var cellsAt int64 = -1
	oneDrive := false
	for _, e := range envs {
		if e.Code != "MI6" || e.MI6 == nil {
			continue
		}
		if e.Host != "" && !slices.Contains(hosts, e.Host) {
			hosts = append(hosts, e.Host)
		}
		oneDrive = oneDrive || e.MI6.OneDriveEnabled
		for _, p := range e.MI6.Pairs {
			if prev, ok := at[p.Dest]; !ok || e.AtMs >= prev {
				pairs[p.Dest], at[p.Dest] = p, e.AtMs
			}
		}
		if len(e.MI6.Visibility) > 0 && e.AtMs >= cellsAt {
			cells, cellsAt = e.MI6.Visibility, e.AtMs
		}
	}
	return pairs, strings.Join(hosts, ", "), oneDrive, cells
}

// judgePair judges one pair: its state, the row's value, and its §2.2
// decision line, if any.
func judgePair(def PairDef, p results.Mi6Pair, ok bool, pairs map[string]results.Mi6Pair) (pairState, string, string) {
	platform, provider := PlatformName(def.Platform), ProviderName(def.Provider)
	switch {
	case !ok || p.Plan == "" || p.Plan == PlanNone:
		if ok && p.PlanAFailure != "" {
			return pairIncomplete, "planul A a eșuat (" + p.PlanAFailure + "), B1 nu a rulat", ""
		}
		return pairIncomplete, "nicio autentificare", ""
	case p.Plan == PlanB1 && !p.HandoffOK:
		return pairFail, "B1 a eșuat (" + p.B1Failure + ")", fmt.Sprintf(decisionB2, platform, provider)
	case !p.HandoffOK:
		return pairIncomplete, "nicio autentificare reușită", ""
	}
	series, from := p.Refresh, ""
	if p.Plan == PlanB1 && p.RefreshFrom != "" {
		evidence, ok := pairs[p.RefreshFrom]
		if !ok {
			return pairIncomplete, "B1, fără seria lui " + p.RefreshFrom, ""
		}
		series, from = evidence.Refresh, " (seria lui "+p.RefreshFrom+")"
	}
	failedDays, missing := daysOf(series)
	b1Decision := ""
	if p.Plan == PlanB1 && def.HasPlanA() {
		b1Decision = fmt.Sprintf(decisionB1, platform, provider)
	}
	switch {
	case len(failedDays) > 0 && p.Plan == PlanB1:
		return pairFail, "B1, zile eșuate" + from + ": " + joinInts(failedDays), fmt.Sprintf(decisionB2, platform, provider)
	case len(failedDays) > 0:
		return pairIncomplete, "plan A, zile eșuate: " + joinInts(failedDays) + "; B1 nu a rulat", ""
	case len(missing) > 0:
		return pairIncomplete, "plan " + p.Plan + ", zile fără rezultat" + from + ": " + joinInts(missing), b1Decision
	}
	return pairPass, "plan " + p.Plan + ", zilele 1–6 ok" + from, b1Decision
}

// daysOf reads days 1–6 of a series: the failed ones, and the ones with no
// result or blocked by the test. A day is ok when any of its entries is ok
// (C2-R16 (d′)).
func daysOf(series []results.RefreshDay) (failedDays, missing []int) {
	for day := 1; day <= judgedDays; day++ {
		okDay, failedDay := false, false
		for _, d := range series {
			if d.Day != day {
				continue
			}
			switch d.Outcome {
			case results.OutcomeOK:
				okDay = true
			case results.OutcomeFailed, results.OutcomeExpectedExpiry:
				failedDay = true // an expiry before day 7 is no expected one
			}
		}
		switch {
		case okDay:
		case failedDay:
			failedDays = append(failedDays, day)
		default: // no result, or blocked by the test
			missing = append(missing, day)
		}
	}
	return failedDays, missing
}

// judgeMatrix judges the visibility matrix: the F6 decisions of the reader
// platforms with a failed judged cell, the row's value, and whether every
// judged cell was measured.
func judgeMatrix(cells []results.VisibilityCell) ([]string, string, bool) {
	failedBy := map[string][]string{} // reader platform → creators
	measured, failed := 0, 0
	for _, want := range Matrix() {
		if want.Self {
			continue
		}
		i := slices.IndexFunc(cells, func(c results.VisibilityCell) bool {
			return c.Creator == want.Creator && c.Reader == want.Reader && !c.Self
		})
		if i < 0 {
			continue
		}
		measured++
		if c := cells[i]; !c.Listed || !c.Read {
			failed++
			platform := readerPlatform(c.Reader)
			if !slices.Contains(failedBy[platform], c.Creator) {
				failedBy[platform] = append(failedBy[platform], c.Creator)
			}
		}
	}
	var decisions []string
	for _, platform := range []string{"web", "android", "windows"} {
		creators := failedBy[platform]
		if len(creators) == 0 {
			continue
		}
		slices.SortFunc(creators, func(a, b string) int { return slices.Index(creators3, a) - slices.Index(creators3, b) })
		decisions = append(decisions, fmt.Sprintf(decisionF6, PlatformName(platform), strings.Join(creators, ", ")))
	}
	value := fmt.Sprintf("%d din 9 celule măsurate, %d eșuate", measured, failed)
	return decisions, value, measured == 9
}

// readerPlatform is the platform whose F6 a reader's cell decides: the web
// page and the TV client's device flow are web's, android Android's,
// desktop Windows's.
func readerPlatform(reader string) string {
	switch reader {
	case "android":
		return "android"
	case "desktop":
		return "windows"
	}
	return "web"
}

// joinInts joins days with ", ".
func joinInts(days []int) string {
	parts := make([]string, 0, len(days))
	for _, d := range days {
		parts = append(parts, fmt.Sprint(d))
	}
	return strings.Join(parts, ", ")
}

func boolPtr(b bool) *bool { return &b }
