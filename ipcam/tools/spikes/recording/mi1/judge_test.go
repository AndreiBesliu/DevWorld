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

	t.Run("a missing O17-off run leaves its set incomplete", func(t *testing.T) {
		runs := allSets(t)
		runs[SetPCBugcheck] = slices.DeleteFunc(runs[SetPCBugcheck], func(r results.Run) bool { return !r.PadLowBitrate })
		j := Judge(envelopes(runs))
		wantVerdict(t, j, results.Incomplete)
		if want := []string{SetPCBugcheck + ": " + O17OffLabel + ": lipsește"}; !slices.Equal(j.Reasons, want) {
			t.Errorf("Reasons = %q, want %q", j.Reasons, want)
		}
		if r := o17RowOf(t, j, SetPCBugcheck); r.Pass != nil || r.Value != "lipsește" || r.Limit != "≤ 4 s" {
			t.Errorf("row %+v, want lipsește ≤ 4 s and no pass value", r)
		}
	})
}

// TestMI1_O17OffRunIsEncryptedWith256kbpsSettingOff (O17): a run is the
// O17-off run only when it is encrypted with the setting off and records
// cam99_main with the setting off. A clear default run whose setting is
// left at its zero value stays a default run; a run marked O17-off without
// that stream is an invalid composition, reported as such and never judged
// against cam01_main's 2 s limit as the O17-off row, while what it
// measured to fail still fails G10.
func TestMI1_O17OffRunIsEncryptedWith256kbpsSettingOff(t *testing.T) {
	t.Run("a clear default run with the setting at its zero value", func(t *testing.T) {
		runs := allSets(t)
		r := &runs[SetLabLinuxQEMUReset][1] // run 2: clear, the set's 1.9 s
		if r.Encrypted || r.LossMs != 1900 {
			t.Fatalf("run 2 = %+v, want the clear run at 1.9 s", r)
		}
		r.PadLowBitrate = false
		for i := range r.Streams {
			r.Streams[i].PadLowBitrate = false
		}
		j := Judge(envelopes(runs))
		wantVerdict(t, j, results.Pass)
		if mx := row(t, j, "MI1 "+SetLabLinuxQEMUReset+":", "maximul"); mx.Value != "1,90 s" || !passes(mx) {
			t.Errorf("max row = %+v, want 1,90 s from the clear run 2, passed", mx)
		}
		if f := row(t, j, "MI1 "+SetLabLinuxQEMUReset+":", "eșuate"); f.Value != "0 din 20" {
			t.Errorf("failed-runs row = %+v, want 0 din 20", f)
		}
		if o := o17RowOf(t, j, SetLabLinuxQEMUReset); o.Value != "3,00 s" || !passes(o) {
			t.Errorf("O17-off row = %+v, want run 21's 3,00 s, passed", o)
		}
	})

	for name, mutate := range map[string]func(*results.Run){
		"without cam99_main": func(r *results.Run) {
			r.Streams = slices.DeleteFunc(slices.Clone(r.Streams), func(s results.StreamLoss) bool { return s.Stream == LowBitrateStream })
		},
		"without any stream": func(r *results.Run) { r.Streams = nil },
		"with cam99_main at the setting on": func(r *results.Run) {
			r.Streams = slices.Clone(r.Streams)
			for i := range r.Streams {
				r.Streams[i].PadLowBitrate = r.Streams[i].Stream == LowBitrateStream
			}
		},
	} {
		t.Run("a run marked O17-off "+name, func(t *testing.T) {
			runs := allSets(t)
			r := o17Run(t, runs, SetPCKill9)
			mutate(r)
			for i := range r.Streams {
				r.Streams[i].LossMs = 1500 // within 2 s: it must neither fail nor pass as the O17-off row
			}
			j := Judge(envelopes(runs))
			wantVerdict(t, j, results.Incomplete)
			// One reason names the run; «<O17OffLabel>: lipsește» would say
			// it twice (Task 7.1b review, M4).
			if want := []string{SetPCKill9 + ": " + O17OffLabel + ": rularea " + r.RunID + ": nu înregistrează cam99_main cu setarea oprită"}; !slices.Equal(j.Reasons, want) {
				t.Errorf("Reasons = %q, want %q", j.Reasons, want)
			}
			if o := o17RowOf(t, j, SetPCKill9); o.Pass != nil || o.Value != "lipsește" || o.Limit != "≤ 4 s" {
				t.Errorf("O17-off row = %+v, want lipsește ≤ 4 s and no pass value", o)
			}
		})
	}

	// What such a run measured still decides (G27, C1-R13): a failure fails
	// the set and G10 with plan B, and its one reason names the run, why it
	// is not the O17-off run, and the failure. A run without any stream
	// gets that first part only, never «niciun flux măsurat» as well.
	withoutCam99 := func(r *results.Run) {
		r.Streams = slices.DeleteFunc(slices.Clone(r.Streams), func(s results.StreamLoss) bool { return s.Stream == LowBitrateStream })
	}
	for _, c := range []struct {
		name   string
		mutate func(*results.Run)
		reason string // after «nu înregistrează cam99_main cu setarea oprită; »
	}{
		{"clear bytes on the disk", func(r *results.Run) { withoutCam99(r); r.Hub04Clear = false },
			"HUB-04: octeți video în clar pe disc"},
		{"the 4 Mbps stream over 2 s", func(r *results.Run) { withoutCam99(r); setStreamLoss(r, MainStream, 2500) },
			"cam01_main a pierdut 2,50 s, peste limita de 2 s"},
		{"reconcile not clean", func(r *results.Run) { withoutCam99(r); r.Reconciled = false },
			"reconcilierea a cerut intervenție"},
		{"cam99_main's file unreadable and clear bytes", func(r *results.Run) { withoutCam99(r); r.Unreadable = 1; r.Hub04Clear = false },
			"1 fișier ilizibil; HUB-04: octeți video în clar pe disc"},
		{"no stream and a torn fMP4 part", func(r *results.Run) { r.Streams = nil; r.TornParts = 1 },
			"1 parte fMP4 ruptă"},
		{"cam99_main at the setting on over 2 s", func(r *results.Run) {
			r.Streams = slices.Clone(r.Streams)
			for i := range r.Streams {
				r.Streams[i].PadLowBitrate = r.Streams[i].Stream == LowBitrateStream
			}
			setStreamLoss(r, LowBitrateStream, 2500)
		}, "cam99_main a pierdut 2,50 s, peste limita de 2 s"},
	} {
		t.Run("a run marked O17-off with "+c.name, func(t *testing.T) {
			runs := allSets(t)
			r := o17Run(t, runs, SetPCKill9)
			c.mutate(r)
			j := Judge(envelopes(runs))
			wantVerdict(t, j, results.Fail)
			want := []string{SetPCKill9 + ": " + O17OffLabel + ": rularea " + r.RunID + ": nu înregistrează cam99_main cu setarea oprită; " + c.reason}
			if !slices.Equal(j.Reasons, want) {
				t.Errorf("Reasons = %q, want %q", j.Reasons, want)
			}
			if len(j.Notes) != 0 {
				t.Errorf("Notes = %q, want none: no 256 kbps loss was measured with the setting off", j.Notes)
			}
			// Still no O17-off measurement, and still not a default run: the
			// O17-off row fails with the run's cause, so that the set's
			// failure shows in a row (Task 7.1b review, M1).
			wantRow := "lipsește; rularea " + r.RunID + ": nu înregistrează cam99_main cu setarea oprită; " + c.reason
			if o := o17RowOf(t, j, SetPCKill9); o.Pass == nil || *o.Pass || o.Value != wantRow || o.Limit != "≤ 4 s" {
				t.Errorf("O17-off row = %+v, want «%s» ≤ 4 s, failed", o, wantRow)
			}
			if f := row(t, j, "MI1 "+SetPCKill9+":", "eșuate"); f.Value != "0 din 20" {
				t.Errorf("failed-runs row = %+v, want 0 din 20", f)
			}
		})
	}
}

// TestMI1_Judge_ChecksSetComposition (G10, G12, O17): a set is complete only
// with the composition of its plan: an automated set's 20 default runs, 10
// of them encrypted and exactly one with the 256 kbps stream encrypted and
// the setting on, plus an O17-off run; a real set's 5 valid cuts, one per
// slot 1 to 5, slots 1, 3 and 5 encrypted, in at most 8 attempts. Otherwise
// it has no verdict, and the reason names what is wrong.
func TestMI1_Judge_ChecksSetComposition(t *testing.T) {
	const set = SetPiSysrq
	on := set + ": Pierdere minimă la pană de curent: pornită (256 kbps, criptat): "
	encrypt := func(r *results.Run, encrypted bool) {
		r.Encrypted = encrypted
		r.Streams = slices.Clone(r.Streams)
		for i := range r.Streams {
			r.Streams[i].Encrypted = encrypted
		}
	}
	for _, c := range []struct {
		name    string
		set     string
		mutate  func(runs []results.Run) []results.Run
		reasons []string
	}{
		{"every default run clear", set, func(runs []results.Run) []results.Run {
			for i := range 20 {
				encrypt(&runs[i], false)
			}
			return runs
		}, []string{set + ": 0 din 10 rulări criptate", on + "lipsește"}},
		{"no 256 kbps run", set, func(runs []results.Run) []results.Run {
			runs[8].Streams = runs[8].Streams[:1]
			return runs
		}, []string{on + "lipsește"}},
		{"two 256 kbps runs", set, func(runs []results.Run) []results.Run {
			runs[10].Streams = append(slices.Clone(runs[10].Streams), runs[8].Streams[1])
			return runs
		}, []string{on + "2 rulări, nu una"}},
		{"11 encrypted runs", set, func(runs []results.Run) []results.Run {
			encrypt(&runs[1], true)
			return runs
		}, []string{set + ": 11 rulări criptate, nu 10"}},
		{"21 default runs", set, func(runs []results.Run) []results.Run {
			extra := runs[1]
			extra.RunID += "-x"
			return append(runs, extra)
		}, []string{set + ": 21 de rulări valide, nu 20"}},
		{"a real slot twice and one missing", SetRealPC, func(runs []results.Run) []results.Run {
			runs[3].Slot = 2 // slot 4's clear cut, as slot 2
			return runs
		}, []string{SetRealPC + ": tăierea 2 apare de 2 ori", SetRealPC + ": lipsește tăierea validă 4"}},
		{"slot 3 clear", SetRealPC, func(runs []results.Run) []results.Run {
			encrypt(&runs[2], false)
			return runs
		}, []string{SetRealPC + ": tăierea 3 este în clar, nu criptată"}},
		{"slot 2 encrypted", SetRealPC, func(runs []results.Run) []results.Run {
			encrypt(&runs[1], true)
			return runs
		}, []string{SetRealPC + ": tăierea 2 este criptată, nu în clar"}},
		{"a slot outside 1 to 5", SetRealPC, func(runs []results.Run) []results.Run {
			runs[4].Slot = 6
			return runs
		}, []string{SetRealPC + ": rularea real-pc-05 are tăierea 6, în afara 1–5", SetRealPC + ": lipsește tăierea validă 5"}},
		{"9 attempts", SetRealPC, func(runs []results.Run) []results.Run {
			for i := range 4 {
				bad := runs[i]
				bad.RunID, bad.Attempt, bad.Valid = fmt.Sprint(bad.RunID, "-redo"), i+6, false
				runs = append(runs, bad)
			}
			return runs
		}, []string{SetRealPC + ": 9 încercări, peste cele 8 permise"}},
		{"an attempt numbered 9", SetRealPC, func(runs []results.Run) []results.Run {
			runs[4].Attempt = 9
			return runs
		}, []string{SetRealPC + ": 9 încercări, peste cele 8 permise"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			runs := allSets(t)
			runs[c.set] = c.mutate(runs[c.set])
			j := Judge(envelopes(runs))
			wantVerdict(t, j, results.Incomplete)
			if !slices.Equal(j.Reasons, c.reasons) {
				t.Errorf("Reasons = %q, want %q", j.Reasons, c.reasons)
			}
			if m := row(t, j, "MI1 "+c.set+":", "mediana"); m.Pass != nil {
				t.Errorf("median row = %+v, want no pass value on an incomplete set", m)
			}
		})
	}
}

// TestMI1_O17OffRunFailuresReported (C1-R1, C1-R13): in the O17-off run,
// only the loss of the 256 kbps stream recorded with the setting off is
// reported apart: in its row and in Notes, outside G10's verdict, with no
// plan B. Every other G11 condition of the run fails it and G10 with plan B,
// like a default run, with a reason named after O17OffLabel; the row names
// every cause, and the run stays outside its set's median and max.
func TestMI1_O17OffRunFailuresReported(t *testing.T) {
	const set = SetLabLinuxKill9
	if want := "Pierdere minimă la pană de curent: oprită (256 kbps, criptat)"; O17OffLabel != want {
		t.Errorf("O17OffLabel = %q, want %q", O17OffLabel, want)
	}
	j := Judge(envelopes(allSets(t)))
	if len(j.Reasons) != 0 || len(j.Notes) != 0 {
		t.Errorf("Reasons = %q, Notes = %q, want none when every run passes", j.Reasons, j.Notes)
	}
	if r := o17RowOf(t, j, set); !passes(r) || r.Value != "3,00 s" || r.Limit != "≤ 4 s" {
		t.Errorf("passing O17-off row = %+v, want 3,00 s ≤ 4 s, passed", r)
	}

	about := set + ": " + O17OffLabel + ": rularea " + set + "-21: "
	for _, c := range []struct {
		name    string
		mutate  func(*results.Run)
		verdict results.Verdict
		reason  string // the run's failures that fail G10, as Reasons gives them
		note    string // the 256 kbps loss, as Notes gives it
		value   string // the row's value
	}{
		{"the 4 Mbps stream over 2 s", func(r *results.Run) { setStreamLoss(r, MainStream, 2500) }, results.Fail,
			"cam01_main a pierdut 2,50 s, peste limita de 2 s", "", "3,00 s; cam01_main a pierdut 2,50 s, peste limita de 2 s"},
		{"reconcile not clean", func(r *results.Run) { r.Reconciled = false }, results.Fail,
			"reconcilierea a cerut intervenție", "", "3,00 s; reconcilierea a cerut intervenție"},
		{"unreadable files", func(r *results.Run) { r.Unreadable = 2 }, results.Fail,
			"2 fișiere ilizibile", "", "3,00 s; 2 fișiere ilizibile"},
		{"a torn fMP4 part", func(r *results.Run) { r.TornParts = 1 }, results.Fail,
			"1 parte fMP4 ruptă", "", "3,00 s; 1 parte fMP4 ruptă"},
		{"clear bytes on the disk", func(r *results.Run) { r.Hub04Clear = false }, results.Fail,
			"HUB-04: octeți video în clar pe disc", "", "3,00 s; HUB-04: octeți video în clar pe disc"},
		// The row's own value and limit show this one, so it is not repeated.
		{"the 256 kbps stream over 4 s", func(r *results.Run) { setStreamLoss(r, LowBitrateStream, 4100) }, results.Pass,
			"", "cam99_main a pierdut 4,10 s, peste limita de 4 s", "4,10 s"},
		{"the 256 kbps stream over 4 s and torn parts", func(r *results.Run) { setStreamLoss(r, LowBitrateStream, 4100); r.TornParts = 2 }, results.Fail,
			"2 părți fMP4 rupte", "cam99_main a pierdut 4,10 s, peste limita de 4 s", "4,10 s; 2 părți fMP4 rupte"},
	} {
		t.Run(c.name, func(t *testing.T) {
			runs := allSets(t)
			c.mutate(o17Run(t, runs, set))
			j := Judge(envelopes(runs))
			wantVerdict(t, j, c.verdict)
			if r := o17RowOf(t, j, set); r.Pass == nil || *r.Pass || r.Value != c.value || r.Limit != "≤ 4 s" {
				t.Errorf("O17-off row = %+v, want %q ≤ 4 s, failed", r, c.value)
			}
			var reasons, notes []string
			if c.reason != "" {
				reasons = []string{about + c.reason}
			}
			if c.note != "" {
				notes = []string{about + c.note}
			}
			if !slices.Equal(j.Reasons, reasons) {
				t.Errorf("Reasons = %q, want %q", j.Reasons, reasons)
			}
			if !slices.Equal(j.Notes, notes) {
				t.Errorf("Notes = %q, want %q", j.Notes, notes)
			}
			if mx := row(t, j, "MI1 "+set+":", "maximul"); mx.Value != "1,90 s" {
				t.Errorf("the set's max is %s, want 1,90 s: the O17-off run stays outside it", mx.Value)
			}
		})
	}

	t.Run("two O17-off runs name the run of the loss shown", func(t *testing.T) {
		runs := allSets(t)
		again := *o17Run(t, runs, set)
		again.RunID += "-b"
		again.Streams = slices.Clone(again.Streams)
		setStreamLoss(&again, LowBitrateStream, 3500)
		again.Hub04Clear = false
		runs[set] = append(runs[set], again)
		j := Judge(envelopes(runs))
		wantVerdict(t, j, results.Fail)
		want := "3,50 s (rularea " + set + "-21-b); rularea " + set + "-21-b: HUB-04: octeți video în clar pe disc"
		if r := o17RowOf(t, j, set); passes(r) || r.Value != want || r.Limit != "≤ 4 s" {
			t.Errorf("O17-off row = %+v, want %q ≤ 4 s, failed", r, want)
		}
		if want := []string{set + ": " + O17OffLabel + ": rularea " + set + "-21-b: HUB-04: octeți video în clar pe disc"}; !slices.Equal(j.Reasons, want) {
			t.Errorf("Reasons = %q, want %q", j.Reasons, want)
		}

		setStreamLoss(o17Run(t, runs, set), LowBitrateStream, 4200)
		j = Judge(envelopes(runs))
		if r, want := o17RowOf(t, j, set), "4,20 s (rularea "+set+"-21); rularea "+set+"-21-b: HUB-04"; passes(r) || !strings.HasPrefix(r.Value, want) {
			t.Errorf("O17-off row = %+v, want it to start with %q", r, want)
		}
		if want := []string{set + ": " + O17OffLabel + ": rularea " + set + "-21: cam99_main a pierdut 4,20 s, peste limita de 4 s"}; !slices.Equal(j.Notes, want) {
			t.Errorf("Notes = %q, want %q", j.Notes, want)
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

// TestMI1_Judge_DedupKeepsTheCopyThatCounts (G15): runs are the same when
// their id and attempt are; of two copies, the one that counts is kept over
// an invalid one, whatever their order. A redo under the same id with
// another attempt is another run.
func TestMI1_Judge_DedupKeepsTheCopyThatCounts(t *testing.T) {
	for _, c := range []struct {
		name    string
		attempt int
	}{{"a copy of the same attempt", 1}, {"a redo under the same id", 2}} {
		for _, invalidFirst := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, invalid first %v", c.name, invalidFirst), func(t *testing.T) {
				runs := allSets(t)
				valid := &runs[SetPCKill9][5]
				invalid := *valid
				invalid.Valid, invalid.InvalidReason = false, "oprire în afara ferestrei"
				valid.Attempt = c.attempt
				at := 6
				if invalidFirst {
					at = 5
				}
				runs[SetPCKill9] = slices.Insert(runs[SetPCKill9], at, invalid)
				j := Judge(envelopes(runs))
				wantVerdict(t, j, results.Pass)
				if f := row(t, j, "MI1 "+SetPCKill9+":", "eșuate"); f.Value != "0 din 20" {
					t.Errorf("failed-runs row = %+v, want 0 din 20", f)
				}
			})
		}
	}

	t.Run("two counted attempts under one id are two runs", func(t *testing.T) {
		runs := allSets(t)
		again := runs[SetPCKill9][5]
		again.Attempt = 2
		runs[SetPCKill9] = append(runs[SetPCKill9], again)
		j := Judge(envelopes(runs))
		wantVerdict(t, j, results.Incomplete)
		if want := []string{SetPCKill9 + ": 21 de rulări valide, nu 20"}; !slices.Equal(j.Reasons, want) {
			t.Errorf("Reasons = %q, want %q", j.Reasons, want)
		}
	})
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

// TestMI1_O17Row_RunWithoutTheO17Stream: o17Row's precondition is that
// each run records cam99_main encrypted with the setting off, as judgeSet
// sorts them; a run that breaks it does not panic the judge, and the row
// still names that run's failures with its loss «lipsește».
func TestMI1_O17Row_RunWithoutTheO17Stream(t *testing.T) {
	plan, err := Plan(SetLabLinuxKill9, 1)
	if err != nil {
		t.Fatal(err)
	}
	r := runOf(SetLabLinuxKill9, plan[defaultRuns], 500)
	r.Streams = r.Streams[:1] // cam01_main only
	r.TornParts = 1
	var j results.Judgement
	got := o17Row(&j, "MI1 "+SetLabLinuxKill9+": ", SetLabLinuxKill9, "lab", []results.Run{r}, nil)
	if !strings.HasPrefix(got.Value, "lipsește") || !strings.Contains(got.Value, "1 parte fMP4 ruptă") || passes(got) {
		t.Errorf("row = %+v, want «lipsește», the torn part, and no pass", got)
	}
}
