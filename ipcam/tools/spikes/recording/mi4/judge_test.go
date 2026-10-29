package mi4

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The Pi models of the tests, as diag host gives them (§4.8).
var (
	pi4 = &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Tier: "H1b", Known: true, Supported: true, RecommendedMin: 4, RecommendedMax: 4}
	pi5 = &results.PiModelInfo{Raw: "Raspberry Pi 5 Model B Rev 1.0", Tier: "H1", Known: true, Supported: true, RecommendedMin: 8, RecommendedMax: 8}
	h1c = &results.PiModelInfo{Raw: "Raspberry Pi 3 Model B Rev 1.2", Tier: "H1c", Known: true, Supported: true, RecommendedMin: 1, RecommendedMax: 2}
	pi1 = &results.PiModelInfo{Raw: "Raspberry Pi Model B Rev 2", Tier: "unsupported", Known: true}
)

// repeat is n copies of ms.
func repeat(ms float64, n int) []float64 { return slices.Repeat([]float64{ms}, n) }

// passing is a Pi 4's MI4 run that meets every threshold: 1000 cold reads
// of a part at 10 ms, seeks at 1.2 s and 22 MB/s on one core.
func passing() results.Mi4Data {
	return results.Mi4Data{Model: pi4.Raw, ReadMs: repeat(10, 1000), ReadMaxMs: 10, ReadP50Ms: 10, ReadP99Ms: 10, ReadSize: 524288, Cold: true,
		SeekMs: repeat(1200, 20), CryptoMBs: 22, CryptoMBsAllCores: 70, ThresholdMBs: 20, RecommendedAfter: 4}
}

// onPi is d as the Pi's MI4 envelope, with model.
func onPi(d results.Mi4Data, model *results.PiModelInfo) results.Envelope {
	return results.Envelope{Schema: results.Schema, Code: "MI4", RunID: "pi-mi4-1", Set: "pi-mi4", Host: "pi", OS: "linux",
		Storage: "sd", PiModel: model, MI4: &d}
}

// stored is when reccontroller stored the tests' AGE results.
var stored = time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)

// agerangeRun is the phone's AGE result, from the app's Android shell.
func agerangeRun(mbps float64) results.Envelope { return AgerangeRun(1, stored, DeviceOS, mbps) }

// enc06Run is CI's ENC-06 result as recreport's envelope.
func enc06Run(pass bool) results.Envelope {
	return results.Envelope{Schema: results.Schema, Code: "ENC-06", RunID: "ci-enc06-1", Set: "ci-enc06", Host: "lab-linux", OS: "linux",
		MI4: &results.Mi4Data{Enc06: &pass}}
}

// judge judges the Pi's run d on a Pi 4 with a passing agerange and
// ENC-06.
func judge(d results.Mi4Data) results.Judgement {
	return Judge([]results.Envelope{onPi(d, pi4), agerangeRun(13), enc06Run(true)})
}

// row is j's row whose code contains part.
func row(t *testing.T, j results.Judgement, part string) results.Row {
	t.Helper()
	for _, r := range j.Rows {
		if strings.Contains(r.Code, part) {
			return r
		}
	}
	t.Fatalf("no row %q in %+v", part, j.Rows)
	return results.Row{}
}

func TestMI4_PassesWhenEveryPartMeetsItsThreshold(t *testing.T) {
	j := judge(passing())
	if j.Gate != "G13" || j.Verdict != results.Pass || j.PlanB != "" {
		t.Errorf("Judge = %s %q → %q, reasons %q; want trece", j.Gate, j.Verdict, j.PlanB, j.Reasons)
	}
}

// TestMI4_ReadLatency_UsesMaxNotP99 (C26): each of the 1000 reads must be
// within 50 ms: one read at 51 ms among 999 at 10 ms fails, though p99 is
// 10 ms, with HUB-33's plan B, the RAM buffer.
func TestMI4_ReadLatency_UsesMaxNotP99(t *testing.T) {
	slow := passing()
	slow.ReadMs = append(repeat(10, 999), 51)
	slow.ReadMaxMs, slow.ReadP99Ms = 51, 10
	j := judge(slow)
	if j.Verdict != results.Fail || j.PlanB != PlanBRAM {
		t.Errorf("one read at 51 ms: Judge = %q → %q; want pică → %s", j.Verdict, j.PlanB, PlanBRAM)
	}
	if r := row(t, j, "citiri"); r.Pass == nil || *r.Pass {
		t.Errorf("the reads' row %+v, want failed", r)
	}

	edge := passing()
	edge.ReadMs = append(repeat(10, 999), 50)
	edge.ReadMaxMs = 50
	if j := judge(edge); j.Verdict != results.Pass {
		t.Errorf("every read ≤ 50 ms: Judge = %q, reasons %q; want trece", j.Verdict, j.Reasons)
	}
}

// TestMI4_SeekFirstFrame (G21): the first frame after a seek in a 5-min
// segment within 1.5 s passes; beyond it, the plan B is the RAM buffer
// when its seeks are within 1.5 s, else 2-min segments when theirs are,
// else an escalation.
func TestMI4_SeekFirstFrame(t *testing.T) {
	for _, c := range []struct {
		name              string
		seek, ram, twoMin float64
		verdict           results.Verdict
		planB             string
	}{
		{"1.2 s passes", 1200, 0, 0, results.Pass, ""},
		{"1.7 s with the RAM buffer at 1.3 s", 1700, 1300, 900, results.Fail, PlanBRAM},
		{"RAM at 1.6 s, 2-min segments at 1.4 s", 1700, 1600, 1400, results.Fail, PlanBTwoMin},
		{"both above 1.5 s", 1700, 1600, 1550, results.Fail, PlanBEscalate},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := passing()
			d.SeekMs = append(repeat(500, 19), c.seek)
			if c.ram > 0 {
				d.SeekRAMMs, d.Seek2MinMs = append(repeat(400, 19), c.ram), append(repeat(300, 19), c.twoMin)
			}
			j := judge(d)
			if j.Verdict != c.verdict || j.PlanB != c.planB {
				t.Errorf("Judge = %q → %q, reasons %q; want %q → %q", j.Verdict, j.PlanB, j.Reasons, c.verdict, c.planB)
			}
		})
	}
}

// TestMI4_SeekPlanB_OnlyWhatWasMeasured (G21, G27): a seek beyond 1.5 s
// still fails G13, but its plan B is chosen only from measured stages: the
// RAM buffer first, then 2-min segments. When a stage that the choice needs
// was not measured, the plan B reads «planul B nemăsurat», never an
// escalation that nothing measured; the escalation needs both stages
// measured and both beyond 1.5 s.
func TestMI4_SeekPlanB_OnlyWhatWasMeasured(t *testing.T) {
	for _, c := range []struct {
		name        string
		ram, twoMin []float64
		planB       string
	}{
		{"no plan-B stage", nil, nil, PlanBUnmeasured},
		{"RAM at 1.6 s, no 2-min segments", repeat(1600, 20), nil, PlanBUnmeasured},
		{"no RAM, 2-min segments at 1.4 s", nil, repeat(1400, 20), PlanBUnmeasured},
		{"both measured above 1.5 s", repeat(1600, 20), repeat(1550, 20), PlanBEscalate},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := passing()
			d.SeekMs, d.SeekRAMMs, d.Seek2MinMs = append(repeat(500, 19), 1700), c.ram, c.twoMin
			j := judge(d)
			if j.Verdict != results.Fail || j.PlanB != c.planB {
				t.Errorf("Judge = %q → %q, reasons %q; want pică → %q", j.Verdict, j.PlanB, j.Reasons, c.planB)
			}
			unmeasured := slices.ContainsFunc(j.Reasons, func(r string) bool { return strings.Contains(r, "nemăsurat") })
			if unmeasured != (c.planB == PlanBUnmeasured) || (c.planB == PlanBUnmeasured && strings.Contains(j.PlanB, "escaladare")) {
				t.Errorf("reasons %q, plan B %q: want «nemăsurat» exactly when a stage is missing", j.Reasons, j.PlanB)
			}
		})
	}
}

// TestMI4_ThroughputPerModel (C25, G23, ENC-08 (1)): the single-core
// encryption rate must reach 50 MB/s on H1 (Pi 5) and 5 MB/s per
// recommended camera on the other models (Pi 4: 20, H1c: 10); a failure
// lowers the recommended number to ⌊rate ÷ 5⌋, at least 1 and always below
// the model's. On the PC (H3) the rate is information only.
func TestMI4_ThroughputPerModel(t *testing.T) {
	for _, c := range []struct {
		name      string
		model     *results.PiModelInfo
		mbs       float64
		threshold float64
		after     int
		pass      bool
	}{
		{"Pi 4 at 22 MB/s", pi4, 22, 20, 4, true},
		{"Pi 4 at 18 MB/s", pi4, 18, 20, 3, false},
		{"Pi 5 at 45 MB/s", pi5, 45, 50, 7, false},
		{"H1c at 8 MB/s", h1c, 8, 10, 1, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			threshold, judged, after := Throughput(c.model, c.mbs)
			if !judged || threshold != c.threshold || after != c.after {
				t.Errorf("Throughput = %v, %v, %d; want %v, judged, %d", threshold, judged, after, c.threshold, c.after)
			}
			d := passing()
			d.CryptoMBs = c.mbs
			j := Judge([]results.Envelope{onPi(d, c.model), agerangeRun(13), enc06Run(true)})
			want, planB := results.Pass, ""
			if !c.pass {
				want, planB = results.Fail, "pi: numărul recomandat devine "+strconv.Itoa(c.after)
			}
			if j.Verdict != want || j.PlanB != planB {
				t.Errorf("Judge = %q → %q, reasons %q; want %q → %q", j.Verdict, j.PlanB, j.Reasons, want, planB)
			}
		})
	}
	t.Run("PC", func(t *testing.T) {
		if _, judged, _ := Throughput(nil, 5); judged {
			t.Error("the PC's rate is judged")
		}
		pc := passing()
		pc.Model, pc.CryptoMBs, pc.Cold = "PC", 5, false
		e := results.Envelope{Schema: results.Schema, Code: "MI4", RunID: "pc-mi4-1", Set: "pc-mi4", Host: "pc", OS: "windows", Storage: "ssd", MI4: &pc}
		j := Judge([]results.Envelope{e, onPi(passing(), pi4), agerangeRun(13), enc06Run(true)})
		if j.Verdict != results.Pass {
			t.Errorf("Judge = %q, reasons %q; the PC's 5 MB/s must not decide", j.Verdict, j.Reasons)
		}
		if r := row(t, j, "MI4 pc: criptare"); r.Pass != nil {
			t.Errorf("the PC's rate row %+v, want information", r)
		}
	})
}

// TestMI4_AgerangeMissing_Incomplete (G24, G27): without the phone's run,
// agerange has no measurement and G13 no verdict; a measured failure
// elsewhere still decides. Below 12 Mbps it fails with F6's plan B.
func TestMI4_AgerangeMissing_Incomplete(t *testing.T) {
	j := Judge([]results.Envelope{onPi(passing(), pi4), enc06Run(true)})
	if j.Verdict != results.Incomplete {
		t.Errorf("without agerange: Judge = %q, want %q", j.Verdict, results.Incomplete)
	}
	if r := row(t, j, "agerange"); r.Value != "lipsește" || r.Pass != nil {
		t.Errorf("the agerange row %+v, want «lipsește»", r)
	}
	slow := passing()
	slow.ReadMs = append(repeat(10, 999), 80)
	if j := Judge([]results.Envelope{onPi(slow, pi4), enc06Run(true)}); j.Verdict != results.Fail {
		t.Errorf("a slow read without agerange: Judge = %q, want pică", j.Verdict)
	}
	j = Judge([]results.Envelope{onPi(passing(), pi4), agerangeRun(11), enc06Run(true)})
	if j.Verdict != results.Fail || j.PlanB != PlanBF6 {
		t.Errorf("agerange at 11 Mbps: Judge = %q → %q, want pică → %s", j.Verdict, j.PlanB, PlanBF6)
	}
}

// TestMI4_Agerange_FromTheDeviceRunOnly (G24, ENC-08 (3)): agerange is
// judged in the AND-1 WebView, so only an AGE run of the app's Android
// shell decides it, the latest by its run number whatever the order of the
// envelopes; a desktop shell's or a browser's AGE run, or a rate in another
// MI4 envelope, is not the device's: alone it leaves agerange missing, and
// beside the device's run it changes nothing.
func TestMI4_Agerange_FromTheDeviceRunOnly(t *testing.T) {
	// The envelope that recreport writes for the phone's result keeps its
	// device marker through a results file.
	data, err := json.Marshal(AgerangeRun(3, stored, DeviceOS, 12.5))
	if err != nil {
		t.Fatal(err)
	}
	if e, err := results.Parse(data); err != nil || e.Code != "MI4" || e.RunID != "app-age-3" || e.AtMs != stored.UnixMilli() || e.Set != "app-age" ||
		e.OS != "android" || e.MI4.AgerangeMbps == nil || *e.MI4.AgerangeMbps != 12.5 {
		t.Errorf("AgerangeRun's envelope read back as %+v, %v", e, err)
	}
	if e := AgerangeRun(3, time.Time{}, DeviceOS, 12.5); e.AtMs != 0 {
		t.Errorf("AgerangeRun without the time it was stored: AtMs = %d, want 0", e.AtMs)
	}

	pi, enc06 := onPi(passing(), pi4), enc06Run(true)
	misplaced := passing()
	misplaced.AgerangeMbps = new(30.0)
	notTheDevice := []results.Envelope{AgerangeRun(7, stored, "windows-tauri", 30), AgerangeRun(8, stored, "chrome", 30), onPi(misplaced, pi4)}
	for i, other := range notTheDevice {
		j := Judge([]results.Envelope{pi, other, enc06})
		if r := row(t, j, "agerange"); j.Verdict != results.Incomplete || r.Value != "lipsește" {
			t.Errorf("only %s %s (%d): Judge = %q, agerange %q; want %q, «lipsește»", other.Set, other.OS, i, j.Verdict, r.Value, results.Incomplete)
		}
		// The owner learns why a desktop AGE run did not count.
		if why := strings.Join(j.Reasons, "\n"); other.Set == AgerangeSet && !strings.Contains(why, "cele de pe "+other.OS+" nu contează") {
			t.Errorf("only %s %s: reasons %q do not say that its run does not count", other.Set, other.OS, j.Reasons)
		}
		for _, envs := range [][]results.Envelope{{pi, AgerangeRun(1, stored, "android", 11), other, enc06}, {pi, other, AgerangeRun(1, stored, "android", 11), enc06}} {
			if j := Judge(envs); j.Verdict != results.Fail || j.PlanB != PlanBF6 || row(t, j, "agerange").Value != "11,0 Mbps" {
				t.Errorf("the device at 11 Mbps beside %s %s: Judge = %q → %q, want pică → %s", other.Set, other.OS, j.Verdict, j.PlanB, PlanBF6)
			}
		}
	}
	// Run 10 is later than run 9, in either order.
	for _, c := range []struct {
		early, late float64
		want        results.Verdict
	}{{11, 13, results.Pass}, {13, 11, results.Fail}} {
		early, late := AgerangeRun(9, stored, "android", c.early), AgerangeRun(10, stored, "android", c.late)
		for _, envs := range [][]results.Envelope{{pi, early, late, enc06}, {pi, late, early, enc06}, {late, enc06, early, pi}} {
			if j := Judge(envs); j.Verdict != c.want {
				t.Errorf("run 9 at %v and run 10 at %v Mbps, in the order %s: Judge = %q, want %q (run 10 decides)", c.early, c.late, runIDs(envs), j.Verdict, c.want)
			}
		}
	}
}

// runIDs are the run ids of envs, in order.
func runIDs(envs []results.Envelope) string {
	ids := make([]string, len(envs))
	for i, e := range envs {
		ids[i] = e.RunID
	}
	return strings.Join(ids, ", ")
}

// TestMI4_LatestRun_LaterTimeOnTheSameNumber (G15): of two runs whose ids
// end in the same number, the one whose envelope is later (AtMs) decides,
// for the Pi's run, the phone's AGE run and CI's ENC-06 check alike, in
// either order; a greater number still beats a later time. The times are
// chosen so that, as text, the earlier one sorts after the later one:
// neither the envelopes' JSON nor their order can pick the later run.
func TestMI4_LatestRun_LaterTimeOnTheSameNumber(t *testing.T) {
	const early, late = 9_000, 10_000
	slow := passing()
	slow.ReadMs, slow.ReadMaxMs = append(repeat(10, 999), 51), 51
	pi := func(d results.Mi4Data, n int, atMs int64) results.Envelope {
		e := onPi(d, pi4)
		e.RunID, e.AtMs = "pi-mi4-"+strconv.Itoa(n), atMs
		return e
	}
	age := func(atMs int64, mbps float64) results.Envelope {
		return AgerangeRun(3, time.UnixMilli(atMs), DeviceOS, mbps)
	}
	enc06 := func(same bool, atMs int64) results.Envelope {
		e := enc06Run(same)
		e.AtMs = atMs
		return e
	}
	onePi, oneAge, oneEnc06 := onPi(passing(), pi4), agerangeRun(13), enc06Run(true)
	for _, c := range []struct {
		name         string
		a, b         results.Envelope
		others       []results.Envelope
		verdict      results.Verdict
		planB        string
		decidingPart string
	}{
		{"the Pi's slow run is later", pi(passing(), 2, early), pi(slow, 2, late), []results.Envelope{oneAge, oneEnc06}, results.Fail, PlanBRAM, "pi"},
		{"the Pi's passing run is later", pi(slow, 2, early), pi(passing(), 2, late), []results.Envelope{oneAge, oneEnc06}, results.Pass, "", "pi"},
		{"the Pi's greater number beats a later time", pi(passing(), 2, late), pi(slow, 3, early), []results.Envelope{oneAge, oneEnc06}, results.Fail, PlanBRAM, "pi"},
		{"the slow AGE run is later", age(early, 13), age(late, 11), []results.Envelope{onePi, oneEnc06}, results.Fail, PlanBF6, "agerange"},
		{"the fast AGE run is later", age(early, 11), age(late, 13), []results.Envelope{onePi, oneEnc06}, results.Pass, "", "agerange"},
		{"the failed ENC-06 check is later", enc06(true, early), enc06(false, late), []results.Envelope{onePi, oneAge}, results.Fail, PlanBRAM, "ENC-06"},
		{"the passed ENC-06 check is later", enc06(false, early), enc06(true, late), []results.Envelope{onePi, oneAge}, results.Pass, "", "ENC-06"},
	} {
		for _, envs := range [][]results.Envelope{append([]results.Envelope{c.a, c.b}, c.others...), append([]results.Envelope{c.b, c.a}, c.others...)} {
			if j := Judge(envs); j.Verdict != c.verdict || j.PlanB != c.planB {
				t.Errorf("%s, in the order %s: Judge = %q → %q, %s %q; want %q → %q", c.name, runIDs(envs),
					j.Verdict, j.PlanB, c.decidingPart, row(t, j, c.decidingPart).Value, c.verdict, c.planB)
			}
		}
	}
}

// TestMI4_LatestRun_WhateverTheOrder (G15): the Pi's latest run, the
// phone's latest AGE run and CI's latest ENC-06 check are picked by one
// rule, the run id's number, then the envelope's time, then, on the same
// number and time, a stable key, so that the order of the envelopes never
// changes the verdict: every order of two Pi runs, two AGE runs and two
// ENC-06 checks that share a number and a time gives the same judgement.
func TestMI4_LatestRun_WhateverTheOrder(t *testing.T) {
	slow := passing()
	slow.ReadMs, slow.ReadMaxMs = append(repeat(10, 999), 51), 51
	piA, piB, older := onPi(passing(), pi4), onPi(slow, pi4), onPi(slow, pi4)
	piA.RunID, piB.RunID = "pi-mi4-2", "pi-mi4-2"
	envs := []results.Envelope{piA, piB, older, AgerangeRun(3, stored, DeviceOS, 11), AgerangeRun(3, stored, DeviceOS, 13), enc06Run(true), enc06Run(false)}
	var first []byte
	for order := range permutations(len(envs)) {
		in := make([]results.Envelope, len(envs))
		for i, at := range order {
			in[i] = envs[at]
		}
		got, err := json.Marshal(Judge(in))
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = got
		} else if string(got) != string(first) {
			t.Fatalf("in the order %v, Judge =\n%s\nin the first order:\n%s", order, got, first)
		}
	}
}

// permutations yields every order of n items, as their indexes.
func permutations(n int) func(yield func([]int) bool) {
	return func(yield func([]int) bool) {
		order := make([]int, n)
		for i := range order {
			order[i] = i
		}
		var permute func(k int) bool
		permute = func(k int) bool {
			if k == n {
				return yield(slices.Clone(order))
			}
			for i := k; i < n; i++ {
				order[k], order[i] = order[i], order[k]
				if !permute(k + 1) {
					return false
				}
				order[k], order[i] = order[i], order[k]
			}
			return true
		}
		permute(0)
	}
}

// TestMI4_AgeMismatch_PlanBRamBuffer (ENC-06, G21): files that the age CLI
// and rage do not read to the same bytes fail G13 with the RAM buffer as
// plan B; one plan B is named once, also when the reads ask for it too.
func TestMI4_AgeMismatch_PlanBRamBuffer(t *testing.T) {
	j := Judge([]results.Envelope{onPi(passing(), pi4), agerangeRun(13), enc06Run(false)})
	if j.Verdict != results.Fail || j.PlanB != PlanBRAM {
		t.Errorf("ENC-06 mismatch: Judge = %q → %q, want pică → %s", j.Verdict, j.PlanB, PlanBRAM)
	}
	slow := passing()
	slow.ReadMs = append(repeat(10, 999), 80)
	j = Judge([]results.Envelope{onPi(slow, pi4), agerangeRun(13), enc06Run(false)})
	if j.PlanB != PlanBRAM {
		t.Errorf("a slow read and a mismatch: plan B %q, want %s once", j.PlanB, PlanBRAM)
	}
	if j := Judge([]results.Envelope{onPi(passing(), pi4), agerangeRun(13)}); j.Verdict != results.Incomplete {
		t.Errorf("without ENC-06: Judge = %q, want %q", j.Verdict, results.Incomplete)
	}
}

// TestMI4_PiMissingOrUnsupported (G27, G31): without the Pi's run G13 has
// no verdict; with an unsupported Pi it moves to Faza 0-bis; the PC's run
// is information, never the Pi's part.
func TestMI4_PiMissingOrUnsupported(t *testing.T) {
	pcOnly := passing()
	pc := results.Envelope{Schema: results.Schema, Code: "MI4", RunID: "pc-mi4-1", Set: "pc-mi4", Host: "pc", OS: "windows", Storage: "ssd", MI4: &pcOnly}
	if j := Judge([]results.Envelope{pc, agerangeRun(13), enc06Run(true)}); j.Verdict != results.Incomplete {
		t.Errorf("without the Pi's run: Judge = %q, want %q", j.Verdict, results.Incomplete)
	}
	probe := results.Envelope{Schema: results.Schema, Code: "MI3", RunID: "pi-probe-1", Set: "pi-probe", Host: "pi", OS: "linux", PiModel: pi1,
		MI3: &results.Mi3Data{}}
	if j := Judge([]results.Envelope{probe, agerangeRun(13), enc06Run(true)}); j.Verdict != results.Deferred("Pi armv6") {
		t.Errorf("an unsupported Pi: Judge = %q, want %q", j.Verdict, results.Deferred("Pi armv6"))
	}
}

// TestMI4_WarmReadsAreSaid (G22): reads that MI4 could not make cold are
// still judged, and their row says that they were warm.
func TestMI4_WarmReadsAreSaid(t *testing.T) {
	warm := passing()
	warm.Cold = false
	if r := row(t, judge(warm), "citiri"); !strings.Contains(r.Code, "la cald") {
		t.Errorf("the reads' row %q does not say that they were warm", r.Code)
	}
	if r := row(t, judge(passing()), "citiri"); !strings.Contains(r.Code, "la rece") {
		t.Errorf("the reads' row %q does not say that they were cold", r.Code)
	}
}
