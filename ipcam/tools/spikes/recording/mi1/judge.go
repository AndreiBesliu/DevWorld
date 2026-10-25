// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The thresholds of MI1 (HUB-07, SUC-05): per set, the median loss and the
// maximum loss of any stream.
const (
	GeneralMedianMs = 1000
	GeneralMaxMs    = 2000
)

// PartMs is the length of a recording part (§4.2): 1 s.
const PartMs = 1000

// MaxPacingP99Ms is R07's bound on the controller's send jitter: a run
// above it, or with a skipped access unit, does not count.
const MaxPacingP99Ms = 100

// PlanB is G10's plan B (spec HUB-30).
const PlanB = "recorder propriu pe gortsplib/mediacommon în spatele `Recorder`, retestat înainte de Faza 1"

// piDeferral is why the Pi's sets move to Faza 0-bis (G31).
const piDeferral = "Pi armv6"

// O17OffLabel names the O17-off run's row; it is reported apart and never
// decides G10.
const O17OffLabel = "Pierdere minimă la pană de curent: oprită (256 kbps, criptat)"

// chunkBits is an age STREAM chunk, 64 KiB, in bits (§4.5).
const chunkBits = 65536 * 8

// UnpaddedLimitMs is §4.5's loss bound for an encrypted stream without the
// O17 padding, as amended by O17: one part plus 64 KiB ÷ bitrate, rounded up
// to whole parts, PartMs·(1 + ⌈65536·8·1000 ÷ (bitrateBps·PartMs)⌉): 4,000 ms
// at 256 kbps. A bitrate that is not positive has no bound: 0.
func UnpaddedLimitMs(bitrateBps int) int64 {
	if bitrateBps <= 0 {
		return 0
	}
	perPart := int64(bitrateBps) * PartMs
	return PartMs * (1 + (chunkBits*1000+perPart-1)/perPart)
}

// StreamLimitMs is a stream's loss limit: UnpaddedLimitMs for an encrypted
// stream with the setting off whose parts are under 64 KiB (below ≈ 524
// kbps), else GeneralMaxMs.
func StreamLimitMs(bitrateBps int, encrypted, padLowBitrate bool) int64 {
	if encrypted && !padLowBitrate && bitrateBps > 0 && int64(bitrateBps)*PartMs < chunkBits*1000 {
		return UnpaddedLimitMs(bitrateBps)
	}
	return GeneralMaxMs
}

// ConfidenceUpperBound is the one-sided 95 % upper bound of the failure
// rate after failures in n runs, the exact (Clopper-Pearson) bound: with 0
// failures, 1 − 0.05^(1/n). It is 1 without runs.
func ConfidenceUpperBound(failures, n int) float64 {
	switch {
	case n <= 0 || failures < 0 || failures >= n:
		return 1
	case failures == 0:
		return 1 - math.Pow(0.05, 1/float64(n))
	}
	// The binomial CDF at failures falls as the rate p grows: bisect for the
	// p where it is 0.05.
	lo, hi := 0.0, 1.0
	for range 100 {
		p := (lo + hi) / 2
		if binomialCDF(failures, n, p) > 0.05 {
			lo = p
		} else {
			hi = p
		}
	}
	return hi
}

// binomialCDF is P(X ≤ k) for X ~ Binomial(n, p), 0 < p < 1.
func binomialCDF(k, n int, p float64) float64 {
	lgn, _ := math.Lgamma(float64(n + 1))
	var sum float64
	for i := 0; i <= k; i++ {
		lgi, _ := math.Lgamma(float64(i + 1))
		lgni, _ := math.Lgamma(float64(n - i + 1))
		sum += math.Exp(lgn - lgi - lgni + float64(i)*math.Log(p) + float64(n-i)*math.Log1p(-p))
	}
	return sum
}

// ConfidenceText renders a bound as the report does: "< ≈ 7 %".
func ConfidenceText(p float64) string {
	return fmt.Sprintf("< ≈ %d %%", int(math.Round(p*100)))
}

// Counts reports whether a run counts toward its set: the cut is valid
// (C16) and the controller sent evenly (R07). A run that does not count is
// excluded and redone.
func Counts(r results.Run) bool {
	return r.Valid && r.PacingP99Ms <= MaxPacingP99Ms && r.SkippedAUs == 0
}

// RunLossMs is a run's loss: the largest loss of its streams (G10).
func RunLossMs(r results.Run) int64 {
	var loss int64
	for _, s := range r.Streams {
		loss = max(loss, s.LossMs)
	}
	return loss
}

// Failures are why a run fails (G11), in Romanian; none when it passes. A
// stream fails above its StreamLimitMs, recomputed from its bitrate and
// settings; a run also fails when reconcile needed intervention, a file is
// unreadable, a torn fMP4 part remains, HUB-04 found clear bytes, or no
// stream was measured.
func Failures(r results.Run) []string { return failuresExcept(r, 0) }

// failuresExcept is Failures without the losses of streams whose limit is
// shownLimitMs, which the O17-off row already shows as its value and limit;
// 0 leaves none out.
func failuresExcept(r results.Run, shownLimitMs int64) []string {
	var out []string
	if len(r.Streams) == 0 {
		out = append(out, "niciun flux măsurat")
	}
	for _, s := range r.Streams {
		if limit := streamLimit(s); s.LossMs > limit && limit != shownLimitMs {
			out = append(out, fmt.Sprintf("%s a pierdut %s, peste limita de %s", s.Stream, secondsText(s.LossMs), wholeSecondsText(limit)))
		}
	}
	if !r.Reconciled {
		out = append(out, "reconcilierea a cerut intervenție")
	}
	if r.Unreadable > 0 {
		out = append(out, count(r.Unreadable, "fișier ilizibil", "fișiere ilizibile"))
	}
	if r.TornParts > 0 {
		out = append(out, count(r.TornParts, "parte fMP4 ruptă", "părți fMP4 rupte"))
	}
	if !r.Hub04Clear {
		out = append(out, "HUB-04: octeți video în clar pe disc")
	}
	return out
}

// streamLimit is s's StreamLimitMs, recomputed from what was recorded.
func streamLimit(s results.StreamLoss) int64 {
	return StreamLimitMs(s.BitrateKbps*1000, s.Encrypted, s.PadLowBitrate)
}

// setInfo is how Judge treats a set: where its runs come from, the OS whose
// failure rate it counts toward, and whether it is real cuts or the Pi's.
type setInfo struct {
	name, source, os string
	real, pi         bool
}

// sets are MI1's sets in the order of the report. The failure rate counts
// Linux = lab kill9 + lab qemu + pi kill9 + pi sysrq, Windows = lab kill9 +
// pc kill9 + pc bugcheck.
var sets = []setInfo{
	{name: SetLabLinuxKill9, source: "lab", os: "Linux"},
	{name: SetLabLinuxQEMUReset, source: "lab", os: "Linux"},
	{name: SetLabWindowsKill9, source: "lab", os: "Windows"},
	{name: SetPCKill9, source: "pc", os: "Windows"},
	{name: SetPCBugcheck, source: "pc", os: "Windows"},
	{name: SetPiKill9, source: "pi", os: "Linux", pi: true},
	{name: SetPiSysrq, source: "pi", os: "Linux", pi: true},
	{name: SetRealPC, source: "pc", real: true},
	{name: SetRealPi, source: "pi", real: true, pi: true},
}

// The states of a set's verdict.
type setState int

const (
	setPass setState = iota
	setFail
	setIncomplete
	setDeferred
)

// setResult is a set's verdict and its counts for the failure rate.
type setResult struct {
	state           setState
	counted, failed int
	reasons         []string
}

// Judge is G10 (C17). Each set counts its default runs that count (C16,
// R07), each run de-duplicated by its id (G15). A set passes with all of
// them in (20 automated, 5 real cuts), their median ≤ 1 s, their max ≤ 2 s
// and no failed run (G11); every stream's limit is recomputed with
// StreamLimitMs, never read from LimitMs. The O17-off run of an automated
// set is judged alone, outside the set's median and max and outside the
// verdict, in a row of its own that names why it failed; a failed O17-off
// run's failures are also in Reasons, marked «informativ, în afara G10»,
// and change neither the verdict nor PlanB. G10 passes only when every set
// passes; a failed run or set fails it with PlanB; a missing Pi set defers
// it to Faza 0-bis when the Pi is unsupported (an envelope with host "pi"
// says so), and otherwise, like any other missing run, leaves it without a
// verdict.
func Judge(envs []results.Envelope) results.Judgement {
	j := results.Judgement{Gate: "G10"}
	runs := map[string][]results.Run{}
	seen := map[string]bool{}
	piUnsupported := false
	for _, e := range envs {
		if e.Host == "pi" && e.PiModel != nil && e.PiModel.Known && !e.PiModel.Supported {
			piUnsupported = true
		}
		if e.Code != "MI1" {
			continue
		}
		for _, r := range e.Runs {
			if r.RunID != "" && seen[r.RunID] {
				continue
			}
			seen[r.RunID] = true
			runs[e.Set] = append(runs[e.Set], r)
		}
	}

	outcomes := map[string]setResult{}
	var failed, incomplete, deferred bool
	for _, s := range sets {
		res := judgeSet(&j, s, runs[s.name], piUnsupported)
		outcomes[s.name] = res
		j.Reasons = append(j.Reasons, res.reasons...)
		failed = failed || res.state == setFail
		incomplete = incomplete || res.state == setIncomplete
		deferred = deferred || res.state == setDeferred
	}
	failureRates(&j, outcomes)
	switch {
	case failed:
		j.Verdict, j.PlanB = results.Fail, PlanB
	case incomplete:
		j.Verdict = results.Incomplete
	case deferred:
		j.Verdict = results.Deferred(piDeferral)
	default:
		j.Verdict = results.Pass
	}
	return j
}

// judgeSet judges one set and adds its rows to j.
func judgeSet(j *results.Judgement, s setInfo, runs []results.Run, piUnsupported bool) setResult {
	var res setResult
	var losses []int64
	var o17 []results.Run
	for _, r := range runs {
		switch {
		case !Counts(r):
		case !r.PadLowBitrate && !s.real:
			o17 = append(o17, r)
		default:
			res.counted++
			losses = append(losses, RunLossMs(r))
			if f := Failures(r); len(f) > 0 {
				res.failed++
				res.reasons = append(res.reasons, fmt.Sprintf("%s: rularea %s: %s", s.name, r.RunID, strings.Join(f, "; ")))
			}
		}
	}
	required := defaultRuns
	if s.real {
		required = realSlots
	}
	complete := res.counted >= required
	slices.Sort(losses)
	median, maximum := medianMs(losses), int64(0)
	if len(losses) > 0 {
		maximum = losses[len(losses)-1]
	}
	switch {
	case res.failed > 0:
		res.state = setFail
	case !complete && s.pi && piUnsupported:
		res.state = setDeferred
		res.reasons = append(res.reasons, s.name+": amânat ("+piDeferral+")")
	case !complete && res.counted == 0:
		res.state = setIncomplete
		res.reasons = append(res.reasons, s.name+": lipsește")
	case !complete:
		res.state = setIncomplete
		res.reasons = append(res.reasons, fmt.Sprintf("%s: %d din %d rulări valide", s.name, res.counted, required))
	case median > GeneralMedianMs:
		res.state = setFail
		res.reasons = append(res.reasons, fmt.Sprintf("%s: mediana %s, peste %s", s.name, secondsText(median), wholeSecondsText(GeneralMedianMs)))
	case maximum > GeneralMaxMs:
		res.state = setFail
		res.reasons = append(res.reasons, fmt.Sprintf("%s: maximul %s, peste %s", s.name, secondsText(maximum), wholeSecondsText(GeneralMaxMs)))
	}

	prefix := "MI1 " + s.name + ": "
	if res.counted == 0 {
		absent := "lipsește"
		if res.state == setDeferred {
			absent = "amânat (" + piDeferral + ")"
		}
		for _, what := range []string{"mediana", "maximul", "rulări eșuate"} {
			j.Rows = append(j.Rows, results.Row{Code: prefix + what, Value: absent, Source: s.source})
		}
	} else {
		j.Rows = append(j.Rows,
			// An incomplete set's median decides nothing yet.
			results.Row{Code: prefix + "mediana", Value: secondsText(median), Limit: "≤ " + wholeSecondsText(GeneralMedianMs), Source: s.source,
				Pass: verdict(median <= GeneralMedianMs || !complete, complete)},
			results.Row{Code: prefix + "maximul", Value: secondsText(maximum), Limit: "≤ " + wholeSecondsText(GeneralMaxMs), Source: s.source,
				Pass: verdict(maximum <= GeneralMaxMs, complete)},
			results.Row{Code: prefix + "rulări eșuate", Value: fmt.Sprintf("%d din %d", res.failed, res.counted), Limit: "0", Source: s.source,
				Pass: verdict(res.failed == 0, complete)})
	}
	if !s.real {
		row, notes := o17Row(prefix, s.name, s.source, o17)
		j.Rows = append(j.Rows, row)
		// After the set's own reasons; they never change its state.
		res.reasons = append(res.reasons, notes...)
	}
	return res
}

// o17Informative marks the reasons of a failed O17-off run: reported for
// Task 14's decision on §4.5 (C1-R1), never part of G10's verdict.
const o17Informative = "informativ, în afara G10: "

// o17Row reports a set's O17-off runs, outside G10 (C1-R1): the loss of
// the stream with the largest limit, the 256 kbps one, against that limit,
// followed by every other failure of the runs, so that a failed row always
// names its cause; it passes when no run fails. notes are each failed run's
// Failures, marked with o17Informative, for Judgement.Reasons.
func o17Row(prefix, set, source string, runs []results.Run) (row results.Row, notes []string) {
	row = results.Row{Code: prefix + O17OffLabel, Source: source}
	nominal := StreamLimitMs(lowBitrateKbps*1000, true, false)
	if len(runs) == 0 {
		row.Value, row.Limit = "lipsește", "≤ "+wholeSecondsText(nominal)
		return row, nil
	}
	var loss, limit int64
	for _, r := range runs {
		for _, s := range r.Streams {
			switch l := streamLimit(s); {
			case l > limit:
				limit, loss = l, s.LossMs
			case l == limit:
				loss = max(loss, s.LossMs)
			}
		}
	}
	var value []string
	if limit > 0 {
		value = append(value, secondsText(loss))
	}
	pass := true
	for _, r := range runs {
		f := Failures(r)
		if len(f) == 0 {
			continue
		}
		pass = false
		notes = append(notes, fmt.Sprintf("%s%s: rularea %s, cu „Pierdere minimă la pană de curent” oprită: %s", o17Informative, set, r.RunID, strings.Join(f, "; ")))
		for _, other := range failuresExcept(r, limit) {
			if len(runs) > 1 {
				other = "rularea " + r.RunID + ": " + other
			}
			value = append(value, other)
		}
	}
	if limit == 0 { // no stream measured: the bound the run was planned for
		limit = nominal
	}
	row.Value, row.Limit, row.Pass = strings.Join(value, "; "), "≤ "+wholeSecondsText(limit), &pass
	return row, notes
}

// failureRates adds the failure-rate rows: the 95 % bound per OS over the
// automated sets' counted default runs, and over the real cuts.
func failureRates(j *results.Judgement, res map[string]setResult) {
	groups := []struct {
		code, source, limit, one, many string
		member                         func(setInfo) bool
	}{
		{"MI1 Linux", "lab, pi", ConfidenceText(ConfidenceUpperBound(0, 40)), "rulare automată", "rulări automate",
			func(s setInfo) bool { return s.os == "Linux" }},
		{"MI1 Windows", "lab, pc", ConfidenceText(ConfidenceUpperBound(0, 40)), "rulare automată", "rulări automate",
			func(s setInfo) bool { return s.os == "Windows" }},
		{"MI1 tăieri reale", "pc, pi", ConfidenceText(ConfidenceUpperBound(0, 10)), "tăiere reală", "tăieri reale",
			func(s setInfo) bool { return s.real }},
	}
	for _, g := range groups {
		var n, failures int
		for _, s := range sets {
			if g.member(s) {
				n += res[s.name].counted
				failures += res[s.name].failed
			}
		}
		value := "lipsește"
		if n > 0 {
			value = fmt.Sprintf("%s (%s în %s)", ConfidenceText(ConfidenceUpperBound(failures, n)),
				count(failures, "eșec", "eșecuri"), count(n, g.one, g.many))
		}
		j.Rows = append(j.Rows, results.Row{Code: g.code + ": rata de eșec (95 %)", Value: value, Limit: g.limit, Source: g.source})
	}
}

// verdict is a row's pass value: false when ok fails, true when ok holds
// on a complete set, and none while the set is incomplete.
func verdict(ok, complete bool) *bool {
	switch {
	case !ok:
		return new(false)
	case complete:
		return new(true)
	}
	return nil
}

// medianMs is the median of sorted values, 0 without any.
func medianMs(sorted []int64) int64 {
	n := len(sorted)
	switch {
	case n == 0:
		return 0
	case n%2 == 1:
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// secondsText renders a measured time in seconds with two decimals and a
// decimal comma: "1,90 s".
func secondsText(ms int64) string {
	return strings.Replace(strconv.FormatFloat(float64(ms)/1000, 'f', 2, 64), ".", ",", 1) + " s"
}

// wholeSecondsText renders a limit, without decimals when it is whole
// seconds: "2 s", "3,50 s".
func wholeSecondsText(ms int64) string {
	if ms%1000 == 0 {
		return fmt.Sprintf("%d s", ms/1000)
	}
	return secondsText(ms)
}

// count renders n things in Romanian: "1 eșec", "0 eșecuri", "80 de
// rulări".
func count(n int, one, many string) string {
	switch {
	case n == 1:
		return "1 " + one
	case n%100 >= 20 || (n >= 100 && n%100 == 0):
		return fmt.Sprintf("%d de %s", n, many)
	}
	return fmt.Sprintf("%d %s", n, many)
}
