package mi9

import (
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// seeks is the 20 seeks of a leg, each ms long.
func seeks(ms float64) []float64 { return slices.Repeat([]float64{ms}, Seeks) }

// TestJudgeBulk (G25): a bulk channel sustains at least 8 Mbps on the LAN
// and 5 Mbps through TURN-TLS/443, and every seek shows its first frame
// within 1.5 s: LAN 9 passes and 7 fails, relay 6 passes and 4 fails, a
// seek of at most 1.3 s passes and one of 1.7 s fails, as does a Chrome
// frame shown after 1.7 s. A failure names the WebRTC-track plan B.
func TestJudgeBulk(t *testing.T) {
	for _, c := range []struct {
		name string
		r    BulkResult
		pass bool
	}{
		{"LAN at 9 Mbps", BulkResult{Transport: LAN, ThroughputMbps: 9, SeekMs: seeks(1300)}, true},
		{"LAN at 7 Mbps", BulkResult{Transport: LAN, ThroughputMbps: 7, SeekMs: seeks(1300)}, false},
		{"relay at 6 Mbps", BulkResult{Transport: TURNS443, ThroughputMbps: 6, SeekMs: seeks(1300)}, true},
		{"relay at 4 Mbps", BulkResult{Transport: TURNS443, ThroughputMbps: 4, SeekMs: seeks(1300)}, false},
		{"a seek of 1.7 s", BulkResult{Transport: LAN, ThroughputMbps: 9, SeekMs: append(seeks(1300)[1:], 1700)}, false},
		{"Chrome shows each first frame within 1.3 s", BulkResult{Transport: Chrome, SeekMs: seeks(200), DisplayMs: seeks(1300)}, true},
		{"Chrome shows a first frame after 1.7 s", BulkResult{Transport: Chrome, SeekMs: seeks(200), DisplayMs: append(seeks(1300)[1:], 1700)}, false},
	} {
		pass, reasons := JudgeBulk(c.r)
		if pass != c.pass {
			t.Errorf("%s: JudgeBulk = %v %q, want %v", c.name, pass, reasons, c.pass)
			continue
		}
		planB := "pică → " + PlanB
		if !pass && (len(reasons) < 2 || reasons[len(reasons)-1] != planB) {
			t.Errorf("%s: reasons %q, want why, then «%s»", c.name, reasons, planB)
		}
		if pass && len(reasons) != 0 {
			t.Errorf("%s: a pass with reasons %q", c.name, reasons)
		}
	}
	if PlanB != "redare ca track WebRTC 1×, salt prin RPC" {
		t.Errorf("PlanB = %q", PlanB)
	}
}

// TestJudgeBulk_SeekIsMaxOver20 (G25): CTR-12 holds per seek, so the
// slowest of the 20 decides: 19 seeks of 0.5 s and one of 1.6 s fail.
func TestJudgeBulk_SeekIsMaxOver20(t *testing.T) {
	r := BulkResult{Transport: LAN, ThroughputMbps: 50, SeekMs: append(seeks(500)[1:], 1600)}
	pass, reasons := JudgeBulk(r)
	if pass || !strings.Contains(strings.Join(reasons, "\n"), "1600 ms") {
		t.Errorf("JudgeBulk of 19 seeks of 0,5 s and one of 1,6 s = %v %q, want a failure that names 1600 ms", pass, reasons)
	}
}

// mi9Env is an MI9 envelope of host in set with one leg.
func mi9Env(host, set string, leg results.Mi9Leg) results.Envelope {
	return results.Envelope{Schema: results.Schema, Code: "MI9", RunID: host + "-" + set + "-1", Set: set, Host: host, OS: "linux",
		MI9: &results.Mi9Data{Legs: []results.Mi9Leg{leg}}}
}

// The legs of a passing G18: the LAN on each device, the relay and Chrome.
var (
	pcLAN  = mi9Env("pc", "lan", results.Mi9Leg{Transport: "lan", ThroughputMbps: 90, SeekMs: seeks(300), RTTMs: 2, Source: SourceLAN})
	piLAN  = mi9Env("pi", "lan", results.Mi9Leg{Transport: "lan", ThroughputMbps: 40, SeekMs: seeks(600), RTTMs: 3, Source: SourceLAN})
	relay  = mi9Env("lab-linux", "ci-relay", results.Mi9Leg{Transport: "turns443", ThroughputMbps: 6, SeekMs: seeks(900), RTTMs: 51, Source: SourceRelay})
	chrome = mi9Env("lab-linux", "ci-chrome", results.Mi9Leg{Transport: "chrome", SeekMs: seeks(100), DisplayMs: seeks(400), Source: SourceChrome})
	loop   = mi9Env("lab-linux", "ci-loopback", results.Mi9Leg{Transport: "lan", ThroughputMbps: 50, SeekMs: seeks(200), RTTMs: 0.2, Source: SourceLoopback})
)

// rowOf is j's row whose code contains part.
func rowOf(t *testing.T, j results.Judgement, part string) results.Row {
	t.Helper()
	for _, r := range j.Rows {
		if strings.Contains(r.Code, part) {
			return r
		}
	}
	t.Fatalf("no row %q in %+v", part, j.Rows)
	return results.Row{}
}

// TestMI9_Judge_LoopbackNeverDecidesLAN (G18, G26, G27): each part of G18
// is decided by its own source. The CI loopback leg is information only: at
// 50 Mbps alone it leaves the LAN part without a verdict. Every LAN leg of
// the owner's devices must pass: the PC at 9 Mbps and the Pi at 7 Mbps fail
// it. A seek is judged on the LAN and relay legs, and the first frame shown
// only on the Chrome leg: DisplayMs of another leg is ignored.
func TestMI9_Judge_LoopbackNeverDecidesLAN(t *testing.T) {
	if j := Judge([]results.Envelope{pcLAN, piLAN, relay, chrome, loop}); j.Verdict != results.Pass || j.Gate != "G18" {
		t.Errorf("every part measured and passing: Judge = %q (%s), reasons %q", j.Verdict, j.Gate, j.Reasons)
	}

	j := Judge([]results.Envelope{loop, relay, chrome})
	if j.Verdict != results.Incomplete || rowOf(t, j, "LAN").Value != "lipsește" {
		t.Errorf("only the loopback leg on the LAN: Judge = %q, LAN row %+v; want the LAN part without a verdict", j.Verdict, rowOf(t, j, "LAN"))
	}
	if r := rowOf(t, j, "loopback"); r.Pass != nil {
		t.Errorf("the loopback row decides: %+v", r)
	}

	slowPi := mi9Env("pi", "lan", results.Mi9Leg{Transport: "lan", ThroughputMbps: 7, SeekMs: seeks(600), Source: SourceLAN})
	fastPC := mi9Env("pc", "lan", results.Mi9Leg{Transport: "lan", ThroughputMbps: 9, SeekMs: seeks(300), Source: SourceLAN})
	j = Judge([]results.Envelope{fastPC, slowPi, loop})
	if j.Verdict != results.Fail || j.PlanB != PlanB {
		t.Errorf("PC at 9 Mbps, Pi at 7: Judge = %q → %q, want a failure, measured, with the relay and Chrome missing", j.Verdict, j.PlanB)
	}
	if r := rowOf(t, j, "MI9 LAN (pc)"); r.Pass == nil || !*r.Pass || r.Value != "9,0 Mbps (RTT 0 ms)" {
		t.Errorf("the PC's LAN row: %+v, want 9,0 Mbps passing", r)
	}
	if r := rowOf(t, j, "MI9 LAN (pi)"); r.Pass == nil || *r.Pass || r.Value != "7,0 Mbps (RTT 0 ms)" || r.Limit != "≥ 8 Mbps" {
		t.Errorf("the Pi's LAN row: %+v, want 7,0 Mbps failing ≥ 8 Mbps", r)
	}
	if want := "pi: MI9 LAN: canalul bulk a dus 7,0 Mbps (pragul ≥ 8 Mbps)"; !slices.Contains(j.Reasons, want) || slices.ContainsFunc(j.Reasons, func(r string) bool { return strings.HasPrefix(r, "pc:") }) {
		t.Errorf("reasons %q, want «%s» and none for the PC", j.Reasons, want)
	}

	lanDisplay := mi9Env("pc", "lan", results.Mi9Leg{Transport: "lan", ThroughputMbps: 90, SeekMs: seeks(300), DisplayMs: seeks(5000), Source: SourceLAN})
	if j := Judge([]results.Envelope{lanDisplay, relay, chrome}); j.Verdict != results.Pass {
		t.Errorf("a LAN leg's DisplayMs of 5 s decided: Judge = %q, reasons %q", j.Verdict, j.Reasons)
	}
	lanDisplay.MI9.Legs[0].DisplayMs = seeks(300)
	if j := Judge([]results.Envelope{lanDisplay, relay}); j.Verdict != results.Incomplete || rowOf(t, j, "afișat").Value != "lipsește" {
		t.Errorf("no Chrome leg, a LAN leg with DisplayMs: Judge = %q, row %+v; want the display part without a verdict", j.Verdict, rowOf(t, j, "afișat"))
	}

	slowRelaySeek := mi9Env("lab-linux", "ci-relay", results.Mi9Leg{Transport: "turns443", ThroughputMbps: 6, SeekMs: append(seeks(900)[1:], 1600), Source: SourceRelay})
	if j := Judge([]results.Envelope{pcLAN, slowRelaySeek, chrome}); j.Verdict != results.Fail {
		t.Errorf("a relay seek of 1.6 s: Judge = %q, want pică", j.Verdict)
	}
	slowLoopSeek := mi9Env("lab-linux", "ci-loopback", results.Mi9Leg{Transport: "lan", ThroughputMbps: 1, SeekMs: seeks(9000), Source: SourceLoopback})
	if j := Judge([]results.Envelope{pcLAN, relay, chrome, slowLoopSeek}); j.Verdict != results.Pass {
		t.Errorf("a slow loopback leg decided: Judge = %q, reasons %q", j.Verdict, j.Reasons)
	}
}

// TestMI9_Judge_PartialLeg (G27, G25): a lan leg that the measurement's
// wait ended is judged on what it measured. Cut during the transfer, its
// throughput is that of the bytes received by then: below 8 Mbps, a
// measured failure that decides. Cut during the seeks, it keeps the whole
// transfer's throughput and the seeks timed by then: one over 1.5 s
// decides, including the seek under way, timed until the wait ended (a
// lower bound, LastSeekCut). Fewer than 20 seeks all within 1.5 s, or
// none, measure nothing that decides, so the seeks part has no verdict,
// whether or not the leg is partial.
func TestMI9_Judge_PartialLeg(t *testing.T) {
	cut := func(mbps float64, received int64, seekMs []float64, lastSeekCut bool) results.Envelope {
		return mi9Env("pi", "lan", results.Mi9Leg{Transport: "lan", ThroughputMbps: mbps, SeekMs: seekMs, RTTMs: 3, Source: SourceLAN,
			Partial: &results.Mi9Partial{ReceivedBytes: received, SizeBytes: 150e6, ElapsedMs: 120e3, LastSeekCut: lastSeekCut}})
	}
	j := Judge([]results.Envelope{pcLAN, cut(1.1, 124e6, nil, false), relay, chrome})
	if j.Verdict != results.Fail || j.PlanB != PlanB || !slices.Contains(j.Reasons, "pi: MI9 LAN: canalul bulk a dus 1,1 Mbps (pragul ≥ 8 Mbps)") {
		t.Errorf("a Pi leg cut at 1,1 Mbps: Judge = %q → %q, reasons %q; want its measured failure", j.Verdict, j.PlanB, j.Reasons)
	}

	j = Judge([]results.Envelope{pcLAN, cut(50, 150e6, []float64{300, 400, 36000}, true), relay, chrome})
	if j.Verdict != results.Fail || j.PlanB != PlanB {
		t.Errorf("a Pi leg cut 36 s into its third seek: Judge = %q → %q, reasons %q; want pică", j.Verdict, j.PlanB, j.Reasons)
	}
	if r := rowOf(t, j, "(lan, pi)"); r.Pass == nil || *r.Pass || r.Value != "max ≥ 36000 ms (3 salturi)" {
		t.Errorf("the Pi's seek row: %+v, want «max ≥ 36000 ms (3 salturi)» failing", r)
	}
	if want := "pi (lan): un salt nu adusese primul IDR nici după 36000 ms, când proba s-a încheiat (limita ≤ 1500 ms)"; !slices.Contains(j.Reasons, want) {
		t.Errorf("reasons %q, want «%s»", j.Reasons, want)
	}

	j = Judge([]results.Envelope{pcLAN, cut(50, 150e6, []float64{300, 1600}, false), relay, chrome})
	if j.Verdict != results.Fail || !slices.Contains(j.Reasons, "pi (lan): un salt a adus primul IDR în 1600 ms (limita ≤ 1500 ms)") {
		t.Errorf("a Pi leg cut after a seek of 1,6 s: Judge = %q, reasons %q; want pică on that seek", j.Verdict, j.Reasons)
	}

	for _, c := range []struct {
		name  string
		leg   results.Envelope
		value string
	}{
		{"a Pi leg cut after 2 seeks within 1.5 s", cut(50, 150e6, []float64{300, 400}, false), "max 400 ms (2 din 20 de salturi)"},
		{"a Pi leg cut before its first seek", cut(50, 150e6, nil, false), "lipsește"},
		{"a Pi leg of 2 seeks within 1.5 s, not partial",
			mi9Env("pi", "lan", results.Mi9Leg{Transport: "lan", ThroughputMbps: 50, SeekMs: []float64{300, 400}, Source: SourceLAN}), "max 400 ms (2 din 20 de salturi)"},
	} {
		j := Judge([]results.Envelope{pcLAN, c.leg, relay, chrome})
		if r := rowOf(t, j, "(lan, pi)"); j.Verdict != results.Incomplete || r.Pass != nil || r.Value != c.value {
			t.Errorf("%s: Judge = %q, reasons %q, the Pi's seek row %+v; want no verdict, the row «%s» undecided", c.name, j.Verdict, j.Reasons, r, c.value)
		}
	}
}

// TestJudgeBulk_MissingIsNoVerdict (G27, G25): JudgeBulk says «pică» only
// on a measured failure, which decides even when other parts are missing;
// with nothing failing, a missing part says «fără verdict»: no seek, fewer
// than 20 seeks all within 1.5 s (a measurement that its wait cut), or, on
// Chrome, no first frame shown. A seek under way when the wait ended, over
// 1.5 s (LastSeekCut), fails, and its reason says it had not arrived by
// then.
func TestJudgeBulk_MissingIsNoVerdict(t *testing.T) {
	partial := func(lastSeekCut bool) *results.Mi9Partial {
		return &results.Mi9Partial{ReceivedBytes: 150e6, SizeBytes: 150e6, ElapsedMs: 120e3, LastSeekCut: lastSeekCut}
	}
	for _, c := range []struct {
		name, reason, last string
		r                  BulkResult
	}{
		{"the seek under way past 1.5 s", "un salt nu adusese primul cadru întreg (IDR) nici după 36000 ms, când proba s-a încheiat (limita ≤ 1500 ms)", "pică → " + PlanB,
			BulkResult{Transport: LAN, ThroughputMbps: 50, SeekMs: []float64{300, 400, 36000}, Partial: partial(true)}},
		{"2 seeks within 1.5 s", "s-au măsurat doar 2 din 20 de salturi", string(results.Incomplete),
			BulkResult{Transport: LAN, ThroughputMbps: 50, SeekMs: []float64{300, 400}, Partial: partial(false)}},
		{"no seek", "lipsesc salturile", string(results.Incomplete),
			BulkResult{Transport: LAN, ThroughputMbps: 50, Partial: partial(false)}},
		{"no seek at 7 Mbps", "canalul bulk a dus 7,0 Mbps pe LAN (pragul ≥ 8 Mbps)", "pică → " + PlanB,
			BulkResult{Transport: LAN, ThroughputMbps: 7, Partial: &results.Mi9Partial{ReceivedBytes: 1e6, SizeBytes: 150e6, ElapsedMs: 900e3}}},
		{"Chrome without a first frame shown", "lipsește primul cadru afișat după salt", string(results.Incomplete),
			BulkResult{Transport: Chrome, SeekMs: seeks(200)}},
	} {
		pass, reasons := JudgeBulk(c.r)
		if pass || len(reasons) < 2 || reasons[len(reasons)-1] != c.last || !slices.Contains(reasons, c.reason) {
			t.Errorf("%s: JudgeBulk = %v %q, want «%s», then «%s»", c.name, pass, reasons, c.reason, c.last)
		}
	}
}
