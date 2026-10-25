package mi1

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// hostOf is the pseudonym of the host under test in each set.
var hostOf = map[string]string{
	SetLabLinuxKill9: "lab-linux", SetLabLinuxQEMUReset: "lab-qemu", SetLabWindowsKill9: "lab-windows",
	SetPCKill9: "pc", SetPCBugcheck: "pc", SetRealPC: "pc", SetPiKill9: "pi", SetPiSysrq: "pi", SetRealPi: "pi",
}

var supportedPi = &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Tier: "H1b", Source: "cpuinfo", Supported: true, Known: true, RecommendedMin: 4, RecommendedMax: 4}

// runOf is a counted run of p, every stream at lossMs; an O17-off run's
// 256 kbps stream loses 3 s, within its 4 s bound.
func runOf(set string, p RunPlan, lossMs int64) results.Run {
	r := results.Run{
		RunID: fmt.Sprintf("%s-%02d", set, p.Index), Valid: true, Attempt: 1, ShutdownKind: "kill9",
		Encrypted: p.Encrypted, PadLowBitrate: p.PadLowBitrate, Reconciled: true, Hub04Clear: true, PacingP99Ms: 6, Cameras: len(p.Streams),
	}
	if strings.HasPrefix(set, "real-") {
		r.Slot, r.ShutdownKind = p.Index, "power"
	}
	for _, s := range p.Streams {
		loss := lossMs
		if !p.PadLowBitrate && s == LowBitrateStream {
			loss = 3000
		}
		r.Streams = append(r.Streams, results.StreamLoss{
			Stream: s, BitrateKbps: BitrateKbps(s), Encrypted: p.Encrypted, PadLowBitrate: p.PadLowBitrate, LossMs: loss,
			LimitMs: StreamLimitMs(BitrateKbps(s)*1000, p.Encrypted, p.PadLowBitrate),
		})
	}
	r.LossMs = RunLossMs(r)
	return r
}

// allSets holds the runs of every set: the default runs lose 0.8 s except
// one at 1.9 s, so each set has median 0.8 s and max 1.9 s.
func allSets(t *testing.T) map[string][]results.Run {
	t.Helper()
	runs := map[string][]results.Run{}
	for set := range hostOf {
		plan, err := Plan(set, 2)
		if err != nil {
			t.Fatal(err)
		}
		for i, p := range plan {
			loss := int64(800)
			if i == 1 {
				loss = 1900
			}
			runs[set] = append(runs[set], runOf(set, p, loss))
		}
	}
	return runs
}

// envelopes puts each run in a one-run envelope, as reccontroller writes
// them; the Pi's envelopes carry its model.
func envelopes(runs map[string][]results.Run) []results.Envelope {
	var out []results.Envelope
	for set, rs := range runs {
		for _, r := range rs {
			e := results.Envelope{Schema: results.Schema, Code: "MI1", RunID: r.RunID, Set: set, Host: hostOf[set], Storage: "ssd", Runs: []results.Run{r}}
			if e.Host == "pi" {
				e.PiModel = supportedPi
			}
			out = append(out, e)
		}
	}
	return out
}

func row(t *testing.T, j results.Judgement, prefix, contains string) results.Row {
	t.Helper()
	for _, r := range j.Rows {
		if strings.HasPrefix(r.Code, prefix) && strings.Contains(r.Code, contains) {
			return r
		}
	}
	t.Fatalf("no row %q … %q in %+v", prefix, contains, j.Rows)
	return results.Row{}
}

func passes(r results.Row) bool { return r.Pass != nil && *r.Pass }

// o17RowOf is the O17-off row of set.
func o17RowOf(t *testing.T, j results.Judgement, set string) results.Row {
	t.Helper()
	return row(t, j, "MI1 "+set+":", "oprită")
}

func wantVerdict(t *testing.T, j results.Judgement, want results.Verdict) {
	t.Helper()
	if j.Gate != "G10" {
		t.Errorf("Gate = %q, want G10", j.Gate)
	}
	if j.Verdict != want {
		t.Errorf("Verdict = %q, want %q (reasons %q)", j.Verdict, want, j.Reasons)
	}
	if wantPlanB := want == results.Fail; (j.PlanB == PlanB) != wantPlanB || (j.PlanB == "") == wantPlanB {
		t.Errorf("PlanB = %q with verdict %q", j.PlanB, j.Verdict)
	}
}

func reasonsMention(t *testing.T, j results.Judgement, words ...string) {
	t.Helper()
	all := strings.Join(j.Reasons, "\n")
	for _, w := range words {
		if !strings.Contains(all, w) {
			t.Errorf("reasons %q do not mention %q", j.Reasons, w)
		}
	}
}

// TestMI1_Verdict_MedianAndMax: median 0.8 s and max 1.9 s in every set
// pass; one run at 2.1 s fails G10 with plan B, and so does a median above
// 1 s with every run within 2 s.
func TestMI1_Verdict_MedianAndMax(t *testing.T) {
	runs := allSets(t)
	j := Judge(envelopes(runs))
	wantVerdict(t, j, results.Pass)
	for set := range hostOf {
		med, mx := row(t, j, "MI1 "+set+":", "mediana"), row(t, j, "MI1 "+set+":", "maximul")
		if !passes(med) || med.Value != "0,80 s" || med.Limit != "≤ 1 s" || !passes(mx) || mx.Value != "1,90 s" || mx.Limit != "≤ 2 s" {
			t.Errorf("%s: rows %+v and %+v, want 0,80 s ≤ 1 s and 1,90 s ≤ 2 s, passed", set, med, mx)
		}
		if wantSource := map[bool]string{true: "lab", false: hostOf[set]}[strings.HasPrefix(set, "lab-")]; med.Source != wantSource {
			t.Errorf("%s: Source = %q, want %q", set, med.Source, wantSource)
		}
	}

	t.Run("max 2.1 s fails", func(t *testing.T) {
		runs := allSets(t)
		r := &runs[SetPCKill9][1]
		for i := range r.Streams {
			r.Streams[i].LossMs = 2100
		}
		j := Judge(envelopes(runs))
		wantVerdict(t, j, results.Fail)
		reasonsMention(t, j, SetPCKill9)
		if mx := row(t, j, "MI1 "+SetPCKill9+":", "maximul"); passes(mx) || mx.Value != "2,10 s" {
			t.Errorf("max row = %+v, want 2,10 s failed", mx)
		}
	})

	t.Run("median 1.1 s fails", func(t *testing.T) {
		runs := allSets(t)
		for i := range 11 {
			r := &runs[SetLabLinuxKill9][i]
			for k := range r.Streams {
				r.Streams[k].LossMs = 1100
			}
		}
		j := Judge(envelopes(runs))
		wantVerdict(t, j, results.Fail)
		reasonsMention(t, j, SetLabLinuxKill9, "mediana")
	})

	t.Run("a real set with max 2.1 s fails", func(t *testing.T) {
		runs := allSets(t)
		runs[SetRealPi][4].Streams[0].LossMs = 2100
		wantVerdict(t, Judge(envelopes(runs)), results.Fail)
	})
}

// lowBitrateRunOf is the default run of set with the 256 kbps stream.
func lowBitrateRunOf(t *testing.T, runs map[string][]results.Run, set string) *results.Run {
	t.Helper()
	for i, r := range runs[set] {
		if r.PadLowBitrate && slices.ContainsFunc(r.Streams, func(s results.StreamLoss) bool { return s.Stream == LowBitrateStream }) {
			return &runs[set][i]
		}
	}
	t.Fatalf("%s has no 256 kbps default run", set)
	return nil
}

func setStreamLoss(r *results.Run, stream string, lossMs int64) {
	for i := range r.Streams {
		if r.Streams[i].Stream == stream {
			r.Streams[i].LossMs = lossMs
		}
	}
}

// TestMI1_LowBitrateEncrypted_DefaultPadding_Uses2s (O17-A): with the
// setting on, a 256 kbps encrypted stream has the general 2 s limit.
func TestMI1_LowBitrateEncrypted_DefaultPadding_Uses2s(t *testing.T) {
	if got := StreamLimitMs(256000, true, true); got != 2000 {
		t.Errorf("StreamLimitMs(256 kbps, encrypted, padding on) = %d, want 2000", got)
	}
	if got := StreamLimitMs(256000, false, false); got != 2000 {
		t.Errorf("StreamLimitMs(256 kbps, clear, padding off) = %d, want 2000", got)
	}
	for loss, want := range map[int64]results.Verdict{1900: results.Pass, 2100: results.Fail} {
		runs := allSets(t)
		setStreamLoss(lowBitrateRunOf(t, runs, SetPiSysrq), LowBitrateStream, loss)
		if j := Judge(envelopes(runs)); j.Verdict != want {
			t.Errorf("256 kbps stream at %d ms: Verdict = %q, want %q", loss, j.Verdict, want)
		}
	}
}

// o17Run is the O17-off run of set.
func o17Run(t *testing.T, runs map[string][]results.Run, set string) *results.Run {
	t.Helper()
	for i, r := range runs[set] {
		if !r.PadLowBitrate {
			return &runs[set][i]
		}
	}
	t.Fatalf("%s has no O17-off run", set)
	return nil
}

// TestMI1_PaddingOff_UsesUnpaddedBound (O17-B): with the setting off, a
// stream whose parts are under 64 KiB has §4.5's bound; the O17-off run is
// judged alone, outside its set's median and max and outside G10's verdict.
func TestMI1_PaddingOff_UsesUnpaddedBound(t *testing.T) {
	for bps, want := range map[int]int64{256000: 4000, 128000: 6000, 524287: 3000, 64000: 10000} {
		if got := UnpaddedLimitMs(bps); got != want {
			t.Errorf("UnpaddedLimitMs(%d) = %d, want %d", bps, got, want)
		}
	}
	for _, c := range []struct {
		bps  int
		want int64
	}{{256000, 4000}, {524287, 3000}, {524288, 2000}, {4000000, 2000}} {
		if got := StreamLimitMs(c.bps, true, false); got != c.want {
			t.Errorf("StreamLimitMs(%d, encrypted, padding off) = %d, want %d", c.bps, got, c.want)
		}
	}

	for loss, wantPass := range map[int64]bool{3900: true, 4100: false} {
		runs := allSets(t)
		setStreamLoss(o17Run(t, runs, SetLabWindowsKill9), LowBitrateStream, loss)
		j := Judge(envelopes(runs))
		wantVerdict(t, j, results.Pass) // neither the set's max nor G10 sees the run
		r := o17RowOf(t, j, SetLabWindowsKill9)
		if passes(r) != wantPass || r.Limit != "≤ 4 s" || r.Value != fmt.Sprintf("%d,%02d s", loss/1000, loss%1000/10) {
			t.Errorf("O17-off at %d ms: row %+v, want pass %v with limit ≤ 4 s", loss, r, wantPass)
		}
		if mx := row(t, j, "MI1 "+SetLabWindowsKill9+":", "maximul"); mx.Value != "1,90 s" {
			t.Errorf("O17-off at %d ms: the set's max is %s, want 1,90 s", loss, mx.Value)
		}
	}

	t.Run("a missing O17-off run decides nothing", func(t *testing.T) {
		runs := allSets(t)
		runs[SetPCBugcheck] = slices.DeleteFunc(runs[SetPCBugcheck], func(r results.Run) bool { return !r.PadLowBitrate })
		j := Judge(envelopes(runs))
		wantVerdict(t, j, results.Pass)
		if r := o17RowOf(t, j, SetPCBugcheck); r.Pass != nil {
			t.Errorf("row %+v, want no pass value", r)
		}
	})
}

// TestMI1_Judge_RecomputesStoredLimit: Judge recomputes every stream's
// limit and never trusts the stored LimitMs.
func TestMI1_Judge_RecomputesStoredLimit(t *testing.T) {
	runs := allSets(t)
	r := o17Run(t, runs, SetPiKill9)
	setStreamLoss(r, LowBitrateStream, 4020)
	for i := range r.Streams {
		if r.Streams[i].Stream == LowBitrateStream {
			r.Streams[i].LimitMs = 4048 // stale: from 64 KiB per part rounded otherwise
		}
	}
	j := Judge(envelopes(runs))
	if row := o17RowOf(t, j, SetPiKill9); passes(row) {
		t.Errorf("O17-off at 4020 ms with a stored limit of 4048: row %+v, want failed", row)
	}

	t.Run("a stored limit below the rule", func(t *testing.T) {
		runs := allSets(t)
		for i := range runs[SetPCKill9][0].Streams {
			runs[SetPCKill9][0].Streams[i].LossMs = 1500
			runs[SetPCKill9][0].Streams[i].LimitMs = 1000
		}
		wantVerdict(t, Judge(envelopes(runs)), results.Pass)
	})
}

// TestMI1_RunFailureConditions (G11): each of the five conditions alone
// fails a run, and a failed run fails its set.
func TestMI1_RunFailureConditions(t *testing.T) {
	plan, err := Plan(SetPCKill9, 2)
	if err != nil {
		t.Fatal(err)
	}
	good := runOf(SetPCKill9, plan[0], 900)
	if f := Failures(good); len(f) != 0 {
		t.Fatalf("Failures(a good run) = %q", f)
	}
	for name, mutate := range map[string]func(*results.Run){
		"a stream over its limit":   func(r *results.Run) { r.Streams[1].LossMs = 2001 },
		"reconcile not clean":       func(r *results.Run) { r.Reconciled = false },
		"an unreadable file":        func(r *results.Run) { r.Unreadable = 1 },
		"a torn fMP4 part":          func(r *results.Run) { r.TornParts = 1 },
		"clear bytes on the disk":   func(r *results.Run) { r.Hub04Clear = false },
		"a run without any streams": func(r *results.Run) { r.Streams = nil },
	} {
		t.Run(name, func(t *testing.T) {
			r := runOf(SetPCKill9, plan[0], 900)
			mutate(&r)
			if f := Failures(r); len(f) == 0 {
				t.Error("Failures = none")
			}
			runs := allSets(t)
			runs[SetPCKill9][0] = r
			j := Judge(envelopes(runs))
			wantVerdict(t, j, results.Fail)
			reasonsMention(t, j, SetPCKill9, r.RunID)
			if f := row(t, j, "MI1 "+SetPCKill9+":", "eșuate"); passes(f) || f.Value != "1 din 20" {
				t.Errorf("failed-runs row = %+v, want 1 din 20, failed", f)
			}
		})
	}
}

// TestMI1_RunLossIsMaxOverStreams (G10): a run's loss is the maximum over
// its streams, whatever the stored LossMs says.
func TestMI1_RunLossIsMaxOverStreams(t *testing.T) {
	r := results.Run{LossMs: 300, Streams: []results.StreamLoss{{Stream: "cam01_main", LossMs: 300}, {Stream: "cam02_main", LossMs: 1700}, {Stream: "cam03_main", LossMs: 900}}}
	if got := RunLossMs(r); got != 1700 {
		t.Errorf("RunLossMs = %d, want 1700", got)
	}
	runs := allSets(t)
	for i := range 11 {
		r := &runs[SetPiKill9][i]
		r.Streams[1].LossMs = 1200 // the second camera
		r.LossMs = 800
	}
	j := Judge(envelopes(runs))
	wantVerdict(t, j, results.Fail)
	if med := row(t, j, "MI1 "+SetPiKill9+":", "mediana"); med.Value != "1,20 s" {
		t.Errorf("median row = %+v, want 1,20 s", med)
	}
}

// TestMI1_InvalidRunsExcludedAndRedone (C16, R07): a C16-invalid run, a run
// with pacing p99 110 ms and one with a skipped AU each count for nothing
// however large their loss, and the same run is redone; a set short of 20
// counted runs has no verdict.
func TestMI1_InvalidRunsExcludedAndRedone(t *testing.T) {
	for name, mutate := range map[string]func(*results.Run){
		"C16-invalid":  func(r *results.Run) { r.Valid, r.InvalidReason = false, "oprire în afara ferestrei" },
		"pacing 110ms": func(r *results.Run) { r.PacingP99Ms = 110 },
		"a skipped AU": func(r *results.Run) { r.SkippedAUs = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := Plan(SetPCKill9, 2)
			if err != nil {
				t.Fatal(err)
			}
			bad := runOf(SetPCKill9, plan[5], 9000)
			bad.RunID += "-a"
			bad.TornParts = 3
			mutate(&bad)
			if Counts(bad) {
				t.Fatal("Counts(invalid run) = true")
			}
			runs := allSets(t)
			done := slices.Clone(runs[SetPCKill9][:5])
			if next, ok := NextRun(plan, append(done, bad)); !ok || next.Index != 6 {
				t.Errorf("NextRun after the invalid run 6 = %+v, %v; want run 6 again", next, ok)
			}
			runs[SetPCKill9] = append(runs[SetPCKill9], bad)
			wantVerdict(t, Judge(envelopes(runs)), results.Pass)

			runs[SetPCKill9] = slices.Delete(runs[SetPCKill9], 5, 6) // run 6 only invalid: 19 counted default runs
			j := Judge(envelopes(runs))
			wantVerdict(t, j, results.Incomplete)
			reasonsMention(t, j, SetPCKill9, "19 din 20")
		})
	}
	edge := runOf(SetPCKill9, RunPlan{Index: 1, PadLowBitrate: true, Streams: []string{MainStream}}, 500)
	edge.PacingP99Ms = 100
	if !Counts(edge) {
		t.Error("Counts(pacing p99 100 ms) = false, want true: R07 refuses above 100 ms")
	}
}

// TestMI1_Judge_DedupsRunIDs (G15): an envelope that arrived twice, from
// the PC's folder and from the Pi's, counts once.
func TestMI1_Judge_DedupsRunIDs(t *testing.T) {
	runs := allSets(t)
	runs[SetPiSysrq][3] = runs[SetPiSysrq][2]
	j := Judge(envelopes(runs))
	wantVerdict(t, j, results.Incomplete)
	reasonsMention(t, j, SetPiSysrq)
}

// TestMI1_InvalidRealCut_IsRedoneWithSameSlot (Review Focus 1, G12): an
// invalid cut is redone in its slot with that slot's encryption; after 8
// attempts without 5 counted cuts the set stops, incomplete.
func TestMI1_InvalidRealCut_IsRedoneWithSameSlot(t *testing.T) {
	cut := func(slot int, valid bool) results.Run {
		return results.Run{RunID: fmt.Sprint("real-", slot, valid), Slot: slot, Valid: valid, Encrypted: slot%2 == 1, Reconciled: true, Hub04Clear: true}
	}
	next, status := NextRealCut(nil)
	if status != RealNext || next.Index != 1 || next.Attempt != 1 || !next.Encrypted || !slices.Equal(next.Streams, []string{MainStream}) {
		t.Errorf("first cut = %+v, %v; want slot 1, attempt 1, encrypted", next, status)
	}
	done := []results.Run{cut(1, true), cut(2, true), cut(3, false)}
	next, status = NextRealCut(done)
	if status != RealNext || next.Index != 3 || !next.Encrypted || next.Attempt != 4 {
		t.Errorf("after an invalid slot 3: %+v, %v; want slot 3 again, encrypted, attempt 4", next, status)
	}
	done = append(done, cut(3, true))
	if next, status = NextRealCut(done); status != RealNext || next.Index != 4 || next.Encrypted || next.Attempt != 5 {
		t.Errorf("after slot 3: %+v, %v; want slot 4, clear, attempt 5", next, status)
	}
	if _, status = NextRealCut(append(slices.Clone(done), cut(4, true), cut(5, true))); status != RealComplete {
		t.Errorf("after 5 counted cuts: status %v, want RealComplete", status)
	}
	stuck := append(slices.Clone(done), cut(4, false), cut(4, false), cut(4, false), cut(4, false))
	if next, status = NextRealCut(stuck); status != RealIncomplete {
		t.Errorf("after 8 attempts with 3 counted cuts: %+v, %v; want RealIncomplete", next, status)
	}
	paced := cut(4, true)
	paced.PacingP99Ms = 110
	if next, _ = NextRealCut(append(slices.Clone(done), paced)); next.Index != 4 {
		t.Errorf("after a cut with pacing p99 110 ms: slot %d, want 4 again", next.Index)
	}

	t.Run("judged without the invalid cut", func(t *testing.T) {
		runs := allSets(t)
		invalid := runs[SetRealPC][2]
		invalid.RunID, invalid.Valid = "real-pc-03-a", false
		invalid.Streams = slices.Clone(invalid.Streams)
		invalid.Streams[0].LossMs = 7000
		runs[SetRealPC] = append(runs[SetRealPC], invalid)
		wantVerdict(t, Judge(envelopes(runs)), results.Pass)
	})
}

// TestMI1_ConfidenceFromCounts: 0 failures in 40 automated runs put the
// failure rate under ≈ 7 %, 0 in 10 real cuts under ≈ 26 % (95 %); Judge
// reports the bound per OS and for the real cuts.
func TestMI1_ConfidenceFromCounts(t *testing.T) {
	for _, c := range []struct {
		failures, n int
		want        float64
		text        string
	}{{0, 40, 0.072, "< ≈ 7 %"}, {0, 10, 0.259, "< ≈ 26 %"}} {
		got := ConfidenceUpperBound(c.failures, c.n)
		if math.Abs(got-c.want) > 0.001 {
			t.Errorf("ConfidenceUpperBound(%d, %d) = %.4f, want %.3f ± 0.001", c.failures, c.n, got, c.want)
		}
		if text := ConfidenceText(got); text != c.text {
			t.Errorf("ConfidenceText(%.4f) = %q, want %q", got, text, c.text)
		}
	}
	// One failure in 40: the exact (Clopper-Pearson) one-sided bound, 11.3 %.
	if got := ConfidenceUpperBound(1, 40); math.Abs(got-0.113) > 0.001 {
		t.Errorf("ConfidenceUpperBound(1, 40) = %.4f, want 0.113 ± 0.001", got)
	}
	if got := ConfidenceUpperBound(0, 0); got != 1 {
		t.Errorf("ConfidenceUpperBound(0, 0) = %v, want 1", got)
	}

	j := Judge(envelopes(allSets(t)))
	for _, c := range []struct{ code, value string }{
		{"MI1 Linux", "< ≈ 4 % (0 eșecuri în 80 de rulări automate)"},
		{"MI1 Windows", "< ≈ 5 % (0 eșecuri în 60 de rulări automate)"},
		{"MI1 tăieri reale", "< ≈ 26 % (0 eșecuri în 10 tăieri reale)"},
	} {
		if r := row(t, j, c.code, "rata de eșec"); r.Value != c.value || r.Pass != nil {
			t.Errorf("row %+v, want value %q and no pass value", r, c.value)
		}
	}
}

// TestMI1_ArmV6PiSetsDeferred (G31): with an unsupported Pi, its missing
// sets defer G10 to Faza 0-bis; with a supported or unknown Pi they leave
// it without a verdict; a measured failure still decides.
func TestMI1_ArmV6PiSetsDeferred(t *testing.T) {
	withoutPi := func(t *testing.T) map[string][]results.Run {
		runs := allSets(t)
		for _, set := range []string{SetPiKill9, SetPiSysrq, SetRealPi} {
			delete(runs, set)
		}
		return runs
	}
	armv6 := results.Envelope{
		Schema: results.Schema, Code: "MI2", RunID: "mi2-pi", Set: "72h", Host: "pi",
		PiModel: &results.PiModelInfo{Raw: "Raspberry Pi Model B Rev 2", Tier: "unsupported", Source: "device-tree", Known: true},
	}

	j := Judge(append(envelopes(withoutPi(t)), armv6))
	wantVerdict(t, j, results.Deferred("Pi armv6"))
	reasonsMention(t, j, SetPiKill9, SetPiSysrq, SetRealPi)

	wantVerdict(t, Judge(envelopes(withoutPi(t))), results.Incomplete)
	unknown := armv6
	unknown.PiModel = &results.PiModelInfo{Tier: "unknown", Estimate: true, RecommendedMin: 1, RecommendedMax: 1}
	wantVerdict(t, Judge(append(envelopes(withoutPi(t)), unknown)), results.Incomplete)

	t.Run("a measured failure decides", func(t *testing.T) {
		runs := withoutPi(t)
		runs[SetPCBugcheck][0].Unreadable = 2
		wantVerdict(t, Judge(append(envelopes(runs), armv6)), results.Fail)
	})
	t.Run("a missing set of another host leaves no verdict", func(t *testing.T) {
		runs := withoutPi(t)
		delete(runs, SetLabWindowsKill9)
		wantVerdict(t, Judge(append(envelopes(runs), armv6)), results.Incomplete)
	})
}
