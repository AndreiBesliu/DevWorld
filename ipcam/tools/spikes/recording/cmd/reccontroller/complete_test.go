package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi3"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// ownerPi is the owner's Pi in these fixtures: a Pi 4, H1b, 4 cameras.
func ownerPi() *results.PiModelInfo {
	return &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Revision: "d03115", Tier: "H1b", Supported: true, Known: true,
		RecommendedMin: 4, RecommendedMax: 4, Source: "cpuinfo"}
}

// ownerEnvelope is an envelope of code in set with runID on host.
func ownerEnvelope(code, set, runID, host string) results.Envelope {
	e := results.Envelope{Schema: results.Schema, Code: code, RunID: runID, Set: set, Host: host, Storage: "ssd", OS: "linux"}
	if host == "pc" {
		e.OS = "windows"
	} else {
		e.PiModel = ownerPi()
	}
	return e
}

// ownerSet is set's runs on host as reccontroller writes them, one
// envelope per run, as its plan lays them out at cameras: every run
// counts and passes, kill9 runs resume and reboot runs record their cold
// boot.
func ownerSet(t *testing.T, set, host string, cameras int) []results.Envelope {
	t.Helper()
	plan, err := mi1.Plan(set, cameras)
	if err != nil {
		t.Fatal(err)
	}
	var out []results.Envelope
	for _, p := range plan {
		r := results.Run{RunID: fmt.Sprintf("%s-%02d-1", set, p.Index), Valid: true, Attempt: 1, Encrypted: p.Encrypted,
			PadLowBitrate: p.PadLowBitrate, Reconciled: true, Hub04Clear: true, PacingP99Ms: 5, Cameras: len(p.Streams), NtpSynced: true}
		if set == mi1.SetRealPC || set == mi1.SetRealPi {
			r.Slot = p.Index
		}
		for _, s := range p.Streams {
			r.Streams = append(r.Streams, results.StreamLoss{Stream: s, BitrateKbps: mi1.BitrateKbps(s), Encrypted: p.Encrypted,
				PadLowBitrate: p.PadLowBitrate, LossMs: 600, LimitMs: mi1.StreamLimitMs(mi1.BitrateKbps(s)*1000, p.Encrypted, p.PadLowBitrate)})
		}
		r.LossMs = mi1.RunLossMs(r)
		switch set {
		case mi1.SetPCKill9, mi1.SetPiKill9:
			r.ResumeMs = new(int64(2500))
		case mi1.SetPCBugcheck, mi1.SetPiSysrq:
			r.ColdBootMs = new(int64(14_000))
		}
		e := ownerEnvelope("MI1", set, r.RunID, host)
		e.Runs = []results.Run{r}
		out = append(out, e)
	}
	return out
}

// ownerMI3 is host's MI3 run whose first hour at cameras passes.
func ownerMI3(host, tier string, cameras int) results.Envelope {
	e := ownerEnvelope("MI3", mi3.SetFor(host), mi3.SetFor(host)+"-1", host)
	all := map[string]results.Budget{
		"cpuTotal": {Value: 41.5}, "tileSub": {Value: 0.4}, "tileMain": {Value: 1.6}, "encUpload": {Value: 2.1},
		"ram": {Value: float64(300 + 10*cameras)}, "disk": {Value: 1.008}, "fsyncPerFile": {Value: 1},
		"latencyP95": {Value: 72}, "ingestPerCamera": {Value: 2.2},
	}
	budgets := map[string]results.Budget{}
	for _, key := range mi3.BudgetKeys(tier) {
		budgets[key] = all[key]
	}
	e.MI3 = &results.Mi3Data{Tier: tier, Cameras: cameras, Budgets: budgets, PacingP99Ms: 6,
		Steps: []results.Mi3Step{{Cameras: cameras, Minutes: 60, Attempt: 1, Pass: true, Valid: true, PacingP99Ms: 6}}}
	return e
}

// ownerMI9 is host's MI9 run on the LAN, with its 20 seeks.
func ownerMI9(host string) results.Envelope {
	e := ownerEnvelope("MI9", mi9.LANSet, host+"-mi9-1", host)
	e.MI9 = &results.Mi9Data{Legs: []results.Mi9Leg{{Transport: "lan", ThroughputMbps: 40, SeekMs: slices.Repeat([]float64{300}, mi9.Seeks), Source: mi9.SourceLAN}}}
	return e
}

// ownerResults is what the PC's results folder holds once both owner
// devices have run every part and every part counts: the Pi's folder
// pulled under pi/, the phone's AGE result under app/.
type ownerResults struct {
	envs []results.Envelope
	age  map[string]any
}

func completeOwnerResults(t *testing.T) *ownerResults {
	t.Helper()
	o := &ownerResults{}
	for _, s := range []struct {
		set, host string
		cameras   int
	}{
		{mi1.SetPCKill9, "pc", 8}, {mi1.SetPCBugcheck, "pc", 1}, {mi1.SetRealPC, "pc", 1},
		{mi1.SetPiKill9, "pi", 4}, {mi1.SetPiSysrq, "pi", 1}, {mi1.SetRealPi, "pi", 1},
	} {
		o.envs = append(o.envs, ownerSet(t, s.set, s.host, s.cameras)...)
	}
	for _, host := range []string{"pc", "pi"} {
		e := ownerEnvelope("MI2", host+"-72h", host+"-72h-1", host)
		e.MI2 = &results.Mi2Data{Hours: 72.3, Cameras: map[string]int{"pc": 8, "pi": 4}[host], WindowHours: 6}
		o.envs = append(o.envs, e, ownerMI9(host))
	}
	mi4 := ownerEnvelope("MI4", "pi-mi4", "pi-mi4-1", "pi")
	mi4.MI4 = &results.Mi4Data{ReadMs: slices.Repeat([]float64{12}, 1000), SeekMs: slices.Repeat([]float64{410}, 20), CryptoMBs: 46.5,
		ThresholdMBs: 20, RecommendedAfter: 4}
	o.envs = append(o.envs, ownerMI3("pc", mi3.TierH3, 8), ownerMI3("pi", mi3.TierH1b, 4), mi4)
	o.age = map[string]any{"scenario": "AGE", "device": map[string]any{"platform": "android", "model": "SM-A245F"},
		"interrupted": false, "mbps": 31.5, "bytes": 33554432, "ms": 8200}
	return o
}

// each changes the envelopes that match.
func (o *ownerResults) each(match func(results.Envelope) bool, change func(*results.Envelope)) *ownerResults {
	for i := range o.envs {
		if match(o.envs[i]) {
			change(&o.envs[i])
		}
	}
	return o
}

// without drops the envelopes that drop matches.
func (o *ownerResults) without(drop func(results.Envelope) bool) *ownerResults {
	o.envs = slices.DeleteFunc(o.envs, drop)
	return o
}

// folder writes the results as the PC holds them at guide step 13: the
// PC's envelopes in its folder, the Pi's under pi/, the AGE result under
// app/.
func (o *ownerResults) folder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, e := range o.envs {
		where := dir
		if e.Host == "pi" && e.Code != "MI1" {
			where = filepath.Join(dir, "pi")
		}
		if err := results.Write(where, e); err != nil {
			t.Fatalf("write %s: %v", e.RunID, err)
		}
	}
	if o.age != nil {
		data, err := json.Marshal(o.age)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "app", "android-samsung-sm-a245f-20261101-101500-age.json"), data)
	}
	return dir
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// inSet matches the envelopes of set.
func inSet(set string) func(results.Envelope) bool {
	return func(e results.Envelope) bool { return e.Set == set }
}

// TestCollection_ListsWhatTheJudgesCannotDecide (mi1-4, mis-4, report-4):
// the final collection judges what it holds, not only what is there: each
// part of an owner device that a gate would leave «fără verdict» gets a
// line with why and the guide step that completes it, and «Nu lipsește
// nimic» comes only when no part is missing or incomplete.
func TestCollection_ListsWhatTheJudgesCannotDecide(t *testing.T) {
	t.Run("complete", func(t *testing.T) {
		got := collection(completeOwnerResults(t).folder(t), true)
		if !strings.Contains(got, "Nu lipsește nimic.") || strings.Contains(got, "Lipsesc") || strings.Contains(got, "incomplete") {
			t.Errorf("collection =\n%s\nwant «Nu lipsește nimic.» and nothing else missing", got)
		}
	})

	for _, tc := range []struct {
		name   string
		damage func(*ownerResults)
		want   []string
	}{
		{"an MI1 set short of valid runs", func(o *ownerResults) {
			// Six clear default runs of pc-bugcheck did not count: the window
			// closed after 14 valid ones.
			dropped := 0
			o.each(inSet(mi1.SetPCBugcheck), func(e *results.Envelope) {
				if r := &e.Runs[0]; !r.Encrypted && dropped < 6 {
					r.Valid, dropped = false, dropped+1
				}
			})
		}, []string{"MI1 pc-bugcheck (pc): 14 din 20 rulări valide", "→ pasul 6, porniți din nou aceeași comandă."}},
		{"a real set with an invalid cut", func(o *ownerResults) {
			o.each(inSet(mi1.SetRealPi), func(e *results.Envelope) {
				if e.Runs[0].Slot == 3 {
					e.Runs[0].Valid = false
				}
			})
		}, []string{"MI1 real-pi (pi): lipsește tăierea validă 3 → pasul 7, porniți din nou aceeași comandă."}},
		{"an MI3 run without a valid hour", func(o *ownerResults) {
			o.each(func(e results.Envelope) bool { return e.Code == "MI3" && e.Host == "pc" }, func(e *results.Envelope) {
				e.MI3.Steps[0].Valid, e.MI3.InvalidReason = false, "o încărcare live s-a oprit"
				e.MI3.Steps[0].InvalidReason = e.MI3.InvalidReason
			})
		}, []string{"MI3 (pc): ora la 8 camere nu are nicio rulare validă", "→ pasul 9, porniți din nou aceeași comandă."}},
		{"a partial MI9 leg", func(o *ownerResults) {
			o.each(func(e results.Envelope) bool { return e.Code == "MI9" && e.Host == "pi" }, func(e *results.Envelope) {
				l := &e.MI9.Legs[0]
				l.SeekMs, l.Partial = l.SeekMs[:14], &results.Mi9Partial{ReceivedBytes: 1 << 20}
			})
		}, []string{"MI9 lan (pi): s-au măsurat doar 14 din 20 de salturi → pasul 11, porniți din nou aceeași comandă."}},
		{"a short MI2 run", func(o *ownerResults) {
			o.each(func(e results.Envelope) bool { return e.Code == "MI2" && e.Host == "pi" }, func(e *results.Envelope) { e.MI2.Hours = 30 })
		}, []string{"MI2 (pi): proba de 72 h a rulat doar 30,0 h → pasul 8, porniți din nou ambele comenzi."}},
		{"an MI4 run without its seeks", func(o *ownerResults) {
			o.each(func(e results.Envelope) bool { return e.Code == "MI4" }, func(e *results.Envelope) { e.MI4.SeekMs = nil })
		}, []string{"MI4 (pi): lipsesc salturile → pasul 10, porniți din nou aceeași comandă."}},
		{"an interrupted AGE result", func(o *ownerResults) {
			o.age["interrupted"], o.age["mbps"] = true, nil
		}, []string{"Lipsesc: AGE."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := completeOwnerResults(t)
			tc.damage(o)
			got := collection(o.folder(t), true)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("collection =\n%s\nwant it to hold %q", got, want)
				}
			}
			if strings.Contains(got, "Nu lipsește nimic") {
				t.Errorf("collection =\n%s\nwant no «Nu lipsește nimic»", got)
			}
		})
	}

	t.Run("a failed part decides and is not redone", func(t *testing.T) {
		// A crash of the 72 h run fails G11 (G27): nothing to redo.
		o := completeOwnerResults(t).each(func(e results.Envelope) bool { return e.Code == "MI2" && e.Host == "pc" },
			func(e *results.Envelope) { e.MI2.Hours, e.MI2.Crashes72h = 20, 1 })
		if got := collection(o.folder(t), true); !strings.Contains(got, "Nu lipsește nimic.") {
			t.Errorf("collection =\n%s\nwant «Nu lipsește nimic.»", got)
		}
	})

	t.Run("an absent part is only missing", func(t *testing.T) {
		got := collection(completeOwnerResults(t).without(inSet(mi1.SetPiSysrq)).folder(t), true)
		if !strings.Contains(got, "Lipsesc: MI1 pi-sysrq (pi).") || strings.Contains(got, "MI1 pi-sysrq (pi):") {
			t.Errorf("collection =\n%s\nwant MI1 pi-sysrq (pi) under «Lipsesc» alone", got)
		}
	})

	t.Run("an unsupported Pi", func(t *testing.T) {
		o := completeOwnerResults(t).without(func(e results.Envelope) bool { return e.Host == "pi" })
		o.envs = append(o.envs, results.PiProbeMarker("Raspberry Pi Zero W Rev 1.1", "9000c1", "device-tree", 1))
		if got := collection(o.folder(t), false); !strings.Contains(got, "Nu lipsește nimic.") {
			t.Errorf("collection =\n%s\nwant nothing expected of the Pi", got)
		}
	})
}
