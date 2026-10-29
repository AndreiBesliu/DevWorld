package mi4

import (
	"slices"
	"strconv"
	"strings"
	"testing"

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

// agerangeRun is the phone's AGE result as recreport's envelope.
func agerangeRun(mbps float64) results.Envelope {
	return results.Envelope{Schema: results.Schema, Code: "MI4", RunID: "app-age-1", Set: "app-age", Host: "pc", OS: "android",
		MI4: &results.Mi4Data{AgerangeMbps: &mbps}}
}

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
