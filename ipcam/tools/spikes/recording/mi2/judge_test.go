package mi2

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

var supportedPi = &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Tier: "H1b", Source: "cpuinfo", Supported: true, Known: true, RecommendedMin: 4, RecommendedMax: 4}

// kill9Envelopes are the owner's kill9 set of host, as reccontroller
// writes it: one envelope per run, every run counted, the 20 default runs
// resuming in resumeMs[i] (or 3 s past its end) and the O17-off run in
// o17ResumeMs.
func kill9Envelopes(t *testing.T, host string, resumeMs []int64, o17ResumeMs int64) []results.Envelope {
	t.Helper()
	set := host + "-kill9"
	plan, err := mi1.Plan(set, 2)
	if err != nil {
		t.Fatal(err)
	}
	var out []results.Envelope
	for i, p := range plan {
		resume := int64(3000)
		switch {
		case p.Index == len(plan):
			resume = o17ResumeMs
		case i < len(resumeMs):
			resume = resumeMs[i]
		}
		r := results.Run{RunID: fmt.Sprintf("%s-%02d-1", set, p.Index), Valid: true, Attempt: 1, ShutdownKind: "kill9",
			Encrypted: p.Encrypted, PadLowBitrate: p.PadLowBitrate, Reconciled: true, Hub04Clear: true, PacingP99Ms: 5,
			Cameras: len(p.Streams), ResumeMs: &resume}
		out = append(out, envelope("MI1", set, r.RunID, host, []results.Run{r}, nil))
	}
	return out
}

// mi2Envelope is a 72 h run of host.
func mi2Envelope(host string, hours float64, crashes int) results.Envelope {
	set := host + "-72h"
	return envelope("MI2", set, set+"-1", host, nil, &results.Mi2Data{Hours: hours, Cameras: 2, Crashes72h: crashes, WindowHours: 6})
}

func envelope(code, set, runID, host string, runs []results.Run, data *results.Mi2Data) results.Envelope {
	e := results.Envelope{Schema: results.Schema, Code: code, RunID: runID, Set: set, Host: host, Storage: "ssd", Runs: runs, MI2: data}
	if host == "pi" {
		e.PiModel = supportedPi
	}
	return e
}

// passing is both hosts' 20 resumes within 10 s and 72 h without a crash.
func passing(t *testing.T) []results.Envelope {
	t.Helper()
	envs := append(kill9Envelopes(t, "pc", nil, 3000), kill9Envelopes(t, "pi", nil, 3000)...)
	return append(envs, mi2Envelope("pc", 72, 0), mi2Envelope("pi", 72.01, 0))
}

// TestMI2_Verdict (G11, HUB-31): G11 passes when each owner device's 20
// resumes are each ≤ 10 s and its 72 h ran with 0 ipcam-hub crashes; one
// resume of 12 s, or one crash, fails it with the reverse-ingest plan B. A
// missing 72 h leaves it without a verdict, and the report says that
// recspike's restart is a lower bound for Faza 1's SCM and systemd (C07).
func TestMI2_Verdict(t *testing.T) {
	t.Run("20 resumes within 10 s and no crash", func(t *testing.T) {
		j := Judge(passing(t))
		if j.Gate != "G11" || j.Verdict != results.Pass || j.PlanB != "" {
			t.Errorf("Judge = %s %q, plan B %q, reasons %q; want G11 trece", j.Gate, j.Verdict, j.PlanB, j.Reasons)
		}
		if !slices.ContainsFunc(j.Notes, func(n string) bool { return strings.Contains(n, "limită inferioară") }) {
			t.Errorf("notes %q, want the lower bound of C07", j.Notes)
		}
		row := rowOf(t, j, "MI2 pc: reluarea după kill -9")
		if row.Value != "3,00 s (20 din 20)" || row.Limit != "≤ 10 s" || row.Pass == nil || !*row.Pass {
			t.Errorf("resume row %+v, want 3,00 s (20 din 20), ≤ 10 s, passed", row)
		}
	})

	t.Run("one resume of 12 s", func(t *testing.T) {
		envs := passing(t)
		envs = slices.DeleteFunc(envs, func(e results.Envelope) bool { return e.Set == "pi-kill9" })
		envs = append(envs, kill9Envelopes(t, "pi", []int64{3000, 3000, 3000, 12000}, 3000)...)
		j := Judge(envs)
		if j.Verdict != results.Fail || j.PlanB != PlanB || PlanB != "ingest inversat (MediaMTX trage camera)" {
			t.Errorf("Judge = %q, plan B %q; want pică → ingest inversat (MediaMTX trage camera)", j.Verdict, j.PlanB)
		}
		if !slices.ContainsFunc(j.Reasons, func(r string) bool { return strings.Contains(r, "pi-kill9-04-1") && strings.Contains(r, "12,00 s") }) {
			t.Errorf("reasons %q, want the 12 s resume of pi-kill9-04-1", j.Reasons)
		}
	})

	t.Run("one crash in 72 h", func(t *testing.T) {
		envs := passing(t)
		envs[len(envs)-2] = mi2Envelope("pc", 72, 1)
		if j := Judge(envs); j.Verdict != results.Fail || j.PlanB != PlanB {
			t.Errorf("Judge = %q, plan B %q; want pică with the plan B", j.Verdict, j.PlanB)
		}
	})

	t.Run("a crash decides before the 72 h end", func(t *testing.T) {
		envs := passing(t)
		envs[len(envs)-2] = mi2Envelope("pc", 30, 2)
		if j := Judge(envs); j.Verdict != results.Fail {
			t.Errorf("Judge = %q, want pică: a measured crash decides (G27)", j.Verdict)
		}
	})

	t.Run("the Pi's 72 h missing", func(t *testing.T) {
		envs := passing(t)
		if j := Judge(envs[:len(envs)-1]); j.Verdict != results.Incomplete {
			t.Errorf("Judge = %q, want fără verdict", j.Verdict)
		}
	})

	t.Run("a run shorter than 72 h", func(t *testing.T) {
		envs := passing(t)
		envs[len(envs)-1] = mi2Envelope("pi", 71.5, 0)
		if j := Judge(envs); j.Verdict != results.Incomplete {
			t.Errorf("Judge = %q, want fără verdict: 71,5 h are not 72 h", j.Verdict)
		}
	})

	t.Run("an armv6 Pi", func(t *testing.T) {
		envs := slices.DeleteFunc(passing(t), func(e results.Envelope) bool { return e.Host == "pi" })
		armv6 := envelope("MI1", "pi-kill9", "pi-preflight", "pi", nil, nil)
		armv6.PiModel = &results.PiModelInfo{Raw: "Raspberry Pi Zero W Rev 1.1", Known: true, Supported: false}
		if j := Judge(append(envs, armv6)); j.Verdict != results.Deferred("Pi armv6") {
			t.Errorf("Judge = %q, want amânat în Faza 0-bis: Pi armv6", j.Verdict)
		}
	})
}

// TestMI2_ResumeFromKill9Runs (G13, C07): MI2's 20 resumes are the
// ResumeMs of the owner kill9 set's 20 counted default runs, each once:
// neither a run that does not count, nor the O17-off run, nor another set's
// run, nor a second copy of an envelope (the PC's folder and the Pi's)
// changes them. A set with fewer than 20 leaves G11 without a verdict.
func TestMI2_ResumeFromKill9Runs(t *testing.T) {
	envs := passing(t)
	slow := int64(50_000)
	others := []results.Envelope{
		// A cut that did not count, a lab run and a bugcheck run, all slow.
		envelope("MI1", "pc-kill9", "pc-kill9-05-2", "pc", []results.Run{{RunID: "pc-kill9-05-2", Attempt: 2, Valid: false, ResumeMs: &slow}}, nil),
		envelope("MI1", "lab-linux-kill9", "lab-linux-kill9-01-1", "lab-linux", []results.Run{{RunID: "lab-linux-kill9-01-1", Valid: true, Attempt: 1, ResumeMs: &slow}}, nil),
		envelope("MI1", "pc-bugcheck", "pc-bugcheck-01-1", "pc", []results.Run{{RunID: "pc-bugcheck-01-1", Valid: true, Attempt: 1, ResumeMs: &slow}}, nil),
	}
	envs = append(envs, others...)
	// The O17-off run resumed in 15 s: it is not one of the 20.
	envs = slices.DeleteFunc(envs, func(e results.Envelope) bool { return e.Set == "pc-kill9" })
	resumes := []int64{1200, 9999, 10_000, 2500}
	envs = append(envs, kill9Envelopes(t, "pc", resumes, 15_000)...)
	envs = append(envs, kill9Envelopes(t, "pc", resumes, 15_000)[:5]...) // copies from the Pi's folder
	j := Judge(envs)
	if j.Verdict != results.Pass {
		t.Fatalf("Judge = %q, reasons %q; want trece", j.Verdict, j.Reasons)
	}
	if row := rowOf(t, j, "MI2 pc: reluarea după kill -9"); row.Value != "10,00 s (20 din 20)" {
		t.Errorf("resume row %q, want the default runs' maximum, 10,00 s, of 20", row.Value)
	}

	short := slices.DeleteFunc(slices.Clone(envs), func(e results.Envelope) bool { return e.RunID == "pc-kill9-07-1" })
	if j := Judge(short); j.Verdict != results.Incomplete {
		t.Errorf("19 resumes: Judge = %q, want fără verdict", j.Verdict)
	}
	none := slices.Clone(envs)
	for i := range none {
		if none[i].RunID == "pc-kill9-07-1" {
			run := none[i].Runs[0]
			run.ResumeMs = nil
			none[i].Runs = []results.Run{run}
		}
	}
	if j := Judge(none); j.Verdict != results.Incomplete {
		t.Errorf("a run without its resume: Judge = %q, want fără verdict", j.Verdict)
	}
}

// TestMI2_MoreThan20Resumes (G11; Task 8 review M4): a kill9 set whose
// counted default runs give more than the 20 resumes of MI1's composition,
// such as a run redone under another attempt when both count, still gets a
// verdict: every resume is judged against 10 s, and a note names the
// excess. One of them over 10 s fails G11.
func TestMI2_MoreThan20Resumes(t *testing.T) {
	extra := func(resumeMs int64) results.Envelope {
		r := results.Run{RunID: "pc-kill9-05-2", Valid: true, Attempt: 2, ShutdownKind: "kill9", Encrypted: true, PadLowBitrate: true,
			Reconciled: true, Hub04Clear: true, PacingP99Ms: 5, Cameras: 2, ResumeMs: &resumeMs}
		return envelope("MI1", "pc-kill9", r.RunID, "pc", []results.Run{r}, nil)
	}
	j := Judge(append(passing(t), extra(4000)))
	if j.Verdict != results.Pass {
		t.Errorf("21 resumes within 10 s: Judge = %q, reasons %q; want trece", j.Verdict, j.Reasons)
	}
	// The count without «din 20», which would read as a contradiction (Task
	// 8b review M6): the note says why there are more.
	if row := rowOf(t, j, "MI2 pc: reluarea după kill -9"); row.Value != "4,00 s (21 de reluări)" || row.Pass == nil || !*row.Pass {
		t.Errorf("resume row %+v, want 4,00 s (21 de reluări), passed", row)
	}
	if !slices.ContainsFunc(j.Notes, func(n string) bool {
		return strings.HasPrefix(n, "pc-kill9: 21 de reluări numărate, cu 1 peste cele 20")
	}) {
		t.Errorf("notes %q, want the excess of pc-kill9", j.Notes)
	}
	if j := Judge(append(passing(t), extra(12_000))); j.Verdict != results.Fail || j.PlanB != PlanB {
		t.Errorf("21 resumes, one of 12 s: Judge = %q, plan B %q; want pică with the plan B", j.Verdict, j.PlanB)
	}
}

func rowOf(t *testing.T, j results.Judgement, code string) results.Row {
	t.Helper()
	for _, r := range j.Rows {
		if r.Code == code {
			return r
		}
	}
	t.Fatalf("no row %q in %+v", code, j.Rows)
	return results.Row{}
}

// TestMI2_CardFailedNotesIdleHub (Task 8 review, M9; T40): when the SD
// card reached EȘUAT in a 72 h run without a crash, recording stopped on
// every camera, so the run's later hours measured an ipcam-hub without
// recording. G11 keeps its verdict, and a note says so; a card that only
// degraded adds none. The report shows the card's states next to the
// resumes, by the row codes ResumeRowCode and CardRowCode.
func TestMI2_CardFailedNotesIdleHub(t *testing.T) {
	for _, c := range []struct {
		name   string
		states []results.CardState
		note   bool
	}{
		{"the card failed", []results.CardState{{State: "ok", AtMs: 1}, {State: "degraded", AtMs: 2}, {State: "failed", AtMs: 3}}, true},
		{"the card degraded", []results.CardState{{State: "ok", AtMs: 1}, {State: "degraded", AtMs: 2}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			envs := passing(t)
			pi := &envs[len(envs)-1]
			pi.Storage = "sd"
			pi.MI2.OnSdCard, pi.MI2.CardStates = true, c.states
			j := Judge(envs)
			if j.Verdict != results.Pass {
				t.Errorf("Judge = %q, reasons %q; want trece: the card's state does not change G11", j.Verdict, j.Reasons)
			}
			noted := slices.ContainsFunc(j.Notes, func(n string) bool {
				return strings.HasPrefix(n, "pi: ") && strings.Contains(n, "EȘUAT") && strings.Contains(n, "fără înregistrare")
			})
			if noted != c.note {
				t.Errorf("notes %q: the idle-hub note %v, want %v", j.Notes, noted, c.note)
			}
			rowOf(t, j, ResumeRowCode("pi"))
			if r := rowOf(t, j, CardRowCode("pi")); !strings.Contains(r.Value, "OK → DEGRADAT") {
				t.Errorf("card row %+v, want the states in order", r)
			}
		})
	}
}
