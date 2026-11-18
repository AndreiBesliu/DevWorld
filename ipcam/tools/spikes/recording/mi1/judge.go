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

// o17Setting is O17's per-camera setting, as the app names it.
const o17Setting = "Pierdere minimă la pană de curent"

// O17OffLabel names the O17-off run in its row, its reasons and its notes.
// Its 256 kbps loss is reported apart and never decides G10; every other
// failure of the run does (C1-R13).
const O17OffLabel = o17Setting + ": oprită (256 kbps, criptat)"

// o17OnLabel names the default run with the 256 kbps stream, encrypted with
// the setting on (O17-A).
const o17OnLabel = o17Setting + ": pornită (256 kbps, criptat)"

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

// OverLimit reports whether s lost more than its limit, as the verdict gave
// it: such a stream fails its run, and a lab run explains it (Task 7.2d).
func OverLimit(s results.StreamLoss) bool { return s.LossMs > s.LimitMs }

// Failures are why a run fails (G11), in Romanian; none when it passes. A
// stream fails above its StreamLimitMs, recomputed from its bitrate and
// settings; a run also fails when reconcile needed intervention, a file is
// unreadable, a torn fMP4 part remains, HUB-04 found clear bytes, or no
// stream was measured.
func Failures(r results.Run) []string { return failuresExcept(r, nil) }

// failuresExcept is Failures without the losses of the streams that
// excused matches (nil: none): in the O17-off run, the 256 kbps stream's,
// which is judged apart (C1-R13).
func failuresExcept(r results.Run, excused func(results.StreamLoss) bool) []string {
	var out []string
	if len(r.Streams) == 0 {
		out = append(out, "niciun flux măsurat")
	}
	return append(out, measuredFailures(r, excused)...)
}

// measuredFailures is failuresExcept without «niciun flux măsurat»: only
// what the run measured to fail.
func measuredFailures(r results.Run, excused func(results.StreamLoss) bool) []string {
	var out []string
	for _, s := range r.Streams {
		if limit := streamLimit(s); s.LossMs > limit && (excused == nil || !excused(s)) {
			out = append(out, lossFailure(s, limit))
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

// lossFailure says that s lost more than its limit.
func lossFailure(s results.StreamLoss, limit int64) string {
	return fmt.Sprintf("%s a pierdut %s, peste limita de %s", s.Stream, SecondsText(s.LossMs), WholeSecondsText(limit))
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

// Judge is G10 (C17). Each set counts its runs that count (C16, R07), each
// run once (G15, CountedRuns). A set is complete when they have its plan's
// composition (Plan: 20 default runs and an O17-off run, or 5 real cuts);
// it passes complete with its default runs' median ≤ 1 s, their max ≤ 2 s
// and no failed run (G11). Every stream's limit is recomputed with
// StreamLimitMs, never read from LimitMs. In the O17-off run of an
// automated set, only the 256 kbps stream's loss is judged apart (C1-R13):
// in a row of its own, and, over its limit, in Notes and O17OffOverLimit,
// outside the verdict and its plan B; any other failure of that run fails
// the set like a default run's. The run stays outside the set's median
// and max. A counted
// run marked O17-off (encrypted, the setting off) that does not record
// cam99_main at the setting off leaves the set incomplete, and what it
// measured to fail fails the set the same way (G27). G10
// passes only when every set passes; a failed run or set fails it with
// PlanB, even with other sets incomplete; a missing Pi set defers it to
// Faza 0-bis when the Pi is unsupported (an envelope with host "pi" says
// so), and otherwise, like any other missing run, leaves it without a
// verdict.
func Judge(envs []results.Envelope) results.Judgement {
	j := results.Judgement{Gate: "G10"}
	piUnsupported := false
	for _, e := range envs {
		if e.Host == "pi" && e.PiModel != nil && e.PiModel.Known && !e.PiModel.Supported {
			piUnsupported = true
		}
	}
	runs := map[string][]results.Run{}
	for _, c := range CountedRuns(envs) {
		runs[c.Set] = append(runs[c.Set], c.Run)
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

// DefaultRuns are the counted default runs of set in envs, each once (G15):
// the runs that G10 judges for the set's median and maximum, without its
// O17-off run. MI2 takes its 20 resumes from the owner kill9 sets' (G13).
func DefaultRuns(envs []results.Envelope, set string) []results.Run {
	var out []results.Run
	for _, c := range CountedRuns(envs) {
		if c.Set == set && Counts(c.Run) && isDefault(c.Run) {
			out = append(out, c.Run)
		}
	}
	return out
}

// isDefault reports whether r is one of an automated set's default runs:
// not its O17-off run, encrypted with the setting off.
func isDefault(r results.Run) bool { return !r.Encrypted || r.PadLowBitrate }

// CountedRun is a run of an MI1 envelope as G10 judges it: the copy that
// counts of its copies, with the set of its envelope and Env, the index in
// the envelopes given of the envelope that it came from.
type CountedRun struct {
	Set string
	Run results.Run
	Env int
}

// CountedRuns is G10's selection of the runs of the MI1 envelopes in envs,
// in order, each once (G15): an envelope can arrive twice, from the PC's
// folder and from the Pi's. Two runs are the same when their id and
// attempt are; of the copies of a run, the first that counts is kept, else
// the first, whatever their order. A run without an id is always kept.
// recreport names, by it, where the runs that G10 counted came from.
func CountedRuns(envs []results.Envelope) []CountedRun {
	type key struct {
		id      string
		attempt int
	}
	var out []CountedRun
	at := map[key]int{}
	for n, e := range envs {
		if e.Code != "MI1" {
			continue
		}
		for _, r := range e.Runs {
			k := key{r.RunID, r.Attempt}
			switch i, seen := at[k]; {
			case r.RunID == "" || !seen:
				at[k] = len(out)
				out = append(out, CountedRun{e.Set, r, n})
			case !Counts(out[i].Run) && Counts(r):
				out[i] = CountedRun{e.Set, r, n}
			}
		}
	}
	return out
}

// judgeSet judges one set and adds its rows and notes to j.
func judgeSet(j *results.Judgement, s setInfo, runs []results.Run, piUnsupported bool) setResult {
	var res setResult
	var defaults, o17, marked []results.Run
	var missing []string // what keeps the set from its plan
	for _, r := range runs {
		switch {
		case !Counts(r):
		case s.real || isDefault(r):
			defaults = append(defaults, r)
		case slices.ContainsFunc(r.Streams, isO17Stream):
			o17 = append(o17, r)
		default: // marked O17-off, without what the O17-off run measures
			marked = append(marked, r)
			missing = append(missing, o17Text(s.name, r.RunID, notO17Run))
		}
	}
	if s.real {
		missing = append(missing, realComposition(s.name, runs, defaults)...)
	} else {
		missing = append(missing, automatedComposition(s.name, defaults, o17, marked)...)
	}
	complete := len(missing) == 0

	var losses []int64
	for _, r := range defaults {
		losses = append(losses, RunLossMs(r))
		if f := Failures(r); len(f) > 0 {
			res.failed++
			res.reasons = append(res.reasons, fmt.Sprintf("%s: rularea %s: %s", s.name, r.RunID, strings.Join(f, "; ")))
		}
	}
	res.counted = len(defaults)
	// C1-R13: only the 256 kbps loss of the O17-off run is judged apart
	// (o17Row); anything else that fails it fails the set.
	o17Failed := false
	for _, r := range o17 {
		if f := failuresExcept(r, isO17Stream); len(f) > 0 {
			o17Failed = true
			res.reasons = append(res.reasons, o17Text(s.name, r.RunID, strings.Join(f, "; ")))
		}
	}
	// A counted run marked O17-off without cam99_main at the setting off is
	// an invalid composition (missing), but what it measured still decides
	// (G27): its failures fail the set like the O17-off run's. It has no
	// stream to excuse, and its lack of any stream is notO17Run itself, so
	// its one reason starts with that.
	for _, r := range marked {
		if f := measuredFailures(r, nil); len(f) > 0 {
			o17Failed = true
			res.reasons = append(res.reasons, o17Text(s.name, r.RunID, strings.Join(append([]string{notO17Run}, f...), "; ")))
		}
	}

	slices.Sort(losses)
	median, maximum := medianMs(losses), int64(0)
	if len(losses) > 0 {
		maximum = losses[len(losses)-1]
	}
	switch {
	case res.failed > 0 || o17Failed:
		res.state = setFail
	case !complete && s.pi && piUnsupported:
		res.state = setDeferred
		res.reasons = append(res.reasons, s.name+": amânat ("+piDeferral+")")
	case !complete && res.counted == 0:
		res.state = setIncomplete
		res.reasons = append(res.reasons, s.name+": lipsește")
		openSet(j, s, missing)
	case !complete:
		res.state = setIncomplete
		res.reasons = append(res.reasons, missing...)
		openSet(j, s, missing)
	case median > GeneralMedianMs:
		res.state = setFail
		res.reasons = append(res.reasons, fmt.Sprintf("%s: mediana %s, peste %s", s.name, SecondsText(median), WholeSecondsText(GeneralMedianMs)))
	case maximum > GeneralMaxMs:
		res.state = setFail
		res.reasons = append(res.reasons, fmt.Sprintf("%s: maximul %s, peste %s", s.name, SecondsText(maximum), WholeSecondsText(GeneralMaxMs)))
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
			results.Row{Code: prefix + "mediana", Value: SecondsText(median), Limit: "≤ " + WholeSecondsText(GeneralMedianMs), Source: s.source,
				Pass: results.RowPass(median <= GeneralMedianMs || !complete, complete)},
			results.Row{Code: prefix + "maximul", Value: SecondsText(maximum), Limit: "≤ " + WholeSecondsText(GeneralMaxMs), Source: s.source,
				Pass: results.RowPass(maximum <= GeneralMaxMs, complete)},
			results.Row{Code: prefix + "rulări eșuate", Value: fmt.Sprintf("%d din %d", res.failed, res.counted), Limit: "0", Source: s.source,
				Pass: results.RowPass(res.failed == 0, complete)})
	}
	if !s.real {
		j.Rows = append(j.Rows, o17Row(j, prefix, s.name, s.source, o17, marked))
	}
	return res
}

// openSet records, for a set of an owner device, what keeps it from its
// plan (missing) as its open part (results.OpenPart); the lab's sets are
// no owner's.
func openSet(j *results.Judgement, s setInfo, missing []string) {
	if s.source != "pc" && s.source != "pi" {
		return
	}
	for _, m := range missing {
		j.AddOpen(results.Label("MI1", s.name, s.source), strings.TrimPrefix(m, s.name+": "))
	}
}

// notO17Run is why a run marked O17-off (encrypted, the setting off) is not
// the O17-off run.
const notO17Run = "nu înregistrează " + LowBitrateStream + " cu setarea oprită"

// o17Text is a reason or a note about the O17-off run id of set.
func o17Text(set, id, what string) string {
	return fmt.Sprintf("%s: %s: rularea %s: %s", set, O17OffLabel, id, what)
}

// o17Row reports a set's O17-off runs (C1-R1, C1-R13): the loss of the
// 256 kbps stream recorded with the setting off against its limit, for the
// run furthest over it, named when there are several, then every other
// failure of the runs, so that a failed row always names its cause; it
// passes when no run fails. A 256 kbps loss over its limit is also one of
// j's Notes, outside G10's verdict; the other failures are the set's
// reasons (judgeSet). Each run records an isO17Stream, as judgeSet sorts
// them; a run without one adds only its other failures, and when no run has
// one, the loss reads «lipsește». A marked run, counted and marked O17-off
// without recording cam99_main at the setting off (judgeSet), that measured
// a failure fails the row too, named with why it is not the O17-off run, so
// that a set failed only through it shows that failure in a row (Task 7.1b
// review, M1); without a failure it leaves the row as it is.
func o17Row(j *results.Judgement, prefix, set, source string, runs, marked []results.Run) results.Row {
	row := results.Row{Code: prefix + O17OffLabel, Source: source}
	var markedFailures []string
	for _, r := range marked {
		if f := measuredFailures(r, nil); len(f) > 0 {
			markedFailures = append(markedFailures, "rularea "+r.RunID+": "+strings.Join(append([]string{notO17Run}, f...), "; "))
		}
	}
	if len(runs) == 0 {
		row.Value, row.Limit = "lipsește", "≤ "+WholeSecondsText(StreamLimitMs(lowBitrateKbps*1000, true, false))
		if len(markedFailures) > 0 {
			row.Value = strings.Join(append([]string{row.Value}, markedFailures...), "; ")
			row.Pass = new(false)
		}
		return row
	}
	var shown results.StreamLoss
	var shownRun string
	var others []string
	pass, found := true, false
	for _, r := range runs {
		if i := slices.IndexFunc(r.Streams, isO17Stream); i >= 0 {
			s := r.Streams[i]
			limit := streamLimit(s)
			if !found || s.LossMs-limit > shown.LossMs-streamLimit(shown) {
				shown, shownRun, found = s, r.RunID, true
			}
			if s.LossMs > limit {
				pass, j.O17OffOverLimit = false, true
				j.Notes = append(j.Notes, o17Text(set, r.RunID, lossFailure(s, limit)))
			}
		}
		for _, f := range failuresExcept(r, isO17Stream) {
			pass = false
			if len(runs) > 1 {
				f = "rularea " + r.RunID + ": " + f
			}
			others = append(others, f)
		}
	}
	value, limit := "lipsește", StreamLimitMs(lowBitrateKbps*1000, true, false)
	if found {
		value, limit = SecondsText(shown.LossMs), streamLimit(shown)
		if len(runs) > 1 {
			value += " (rularea " + shownRun + ")"
		}
	} else {
		pass = false
	}
	if len(markedFailures) > 0 {
		pass = false
		others = append(others, markedFailures...)
	}
	row.Value = strings.Join(append([]string{value}, others...), "; ")
	row.Limit, row.Pass = "≤ "+WholeSecondsText(limit), &pass
	return row
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

// SecondsText renders a measured time in seconds with two decimals and a
// decimal comma: "1,90 s".
func SecondsText(ms int64) string {
	return strings.Replace(strconv.FormatFloat(float64(ms)/1000, 'f', 2, 64), ".", ",", 1) + " s"
}

// WholeSecondsText renders a limit, without decimals when it is whole
// seconds: "2 s", "3,50 s".
func WholeSecondsText(ms int64) string {
	if ms%1000 == 0 {
		return fmt.Sprintf("%d s", ms/1000)
	}
	return SecondsText(ms)
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
