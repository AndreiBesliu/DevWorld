package mi3

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// step is a valid step of MI3.
func step(cameras, minutes, attempt int, pass bool) results.Mi3Step {
	return results.Mi3Step{Cameras: cameras, Minutes: minutes, Attempt: attempt, Pass: pass, Valid: true, PacingP99Ms: 8}
}

// invalidStep is a step that the controller's pacing invalidated (R07).
func invalidStep(cameras, minutes, attempt int, p99 int64, skipped int) results.Mi3Step {
	return results.Mi3Step{Cameras: cameras, Minutes: minutes, Attempt: attempt, PacingP99Ms: p99, SkippedAUs: skipped}
}

// envelope is host's MI3 envelope: the first 1 h run at cameras measured f,
// and its steps.
func envelope(host, tier string, cameras int, f Figures, steps ...results.Mi3Step) results.Envelope {
	d := dataOf(tier, cameras, f)
	d.Steps = steps
	e := results.Envelope{Schema: results.Schema, Code: "MI3", RunID: SetFor(host) + "-1", Set: SetFor(host), Host: host, Storage: "ssd", MI3: &d}
	if host == "pi" {
		e.OS = "linux"
		e.PiModel = &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Tier: tier, Supported: true, Known: true,
			RecommendedMin: cameras, RecommendedMax: cameras, Source: "cpuinfo"}
	} else {
		e.OS = "windows"
	}
	return e
}

// passing is an envelope of host that passes at cameras in its first 1 h.
func passing(host, tier string, cameras int) results.Envelope {
	return envelope(host, tier, cameras, meeting(), step(cameras, 60, 1, true))
}

// reboots is an MI1 envelope of set with 20 counted default runs whose
// ColdBootMs grow up to maxMs.
func reboots(set, host string, maxMs int64) results.Envelope {
	e := results.Envelope{Schema: results.Schema, Code: "MI1", RunID: set + "-runs", Set: set, Host: host, OS: "windows", Storage: "ssd"}
	for i := range 20 {
		cold := maxMs - int64(19-i)*100
		e.Runs = append(e.Runs, results.Run{RunID: fmt.Sprintf("%s-%02d", set, i+1), Valid: true, ShutdownKind: "bugcheck", Encrypted: i%2 == 1,
			PadLowBitrate: true, Cameras: 1, ColdBootMs: &cold, PacingP99Ms: 5, Reconciled: true, Hub04Clear: true,
			Streams: []results.StreamLoss{{Stream: mi1.MainStream, BitrateKbps: 4000, LossMs: 500, LimitMs: 2000}}})
	}
	return e
}

// owners are both owner devices passing MI3 and the PC's cold boot at 15 s.
func owners() []results.Envelope {
	return []results.Envelope{passing("pc", "H3", 8), passing("pi", "H1b", 4), reboots(mi1.SetPCBugcheck, "pc", 15_000)}
}

// row returns j's row whose code holds part.
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

func passOf(r results.Row) string {
	if r.Pass == nil {
		return "nil"
	}
	return fmt.Sprint(*r.Pass)
}

// TestMI3_Verdict_BudgetsAndCpu (HUB-32): with every budget of §4.8 met and
// 55 % total CPU, G12 passes; at 65 % the PC fails its first hour, and the
// steps lower its recommended number: G12 fails with that number as its
// plan B.
func TestMI3_Verdict_BudgetsAndCpu(t *testing.T) {
	j := Judge(owners())
	if j.Gate != "G12" || j.Verdict != results.Pass {
		t.Fatalf("Judge = %s %q, reasons %q; want G12 trece", j.Gate, j.Verdict, j.Reasons)
	}
	if r := row(t, j, "MI3 pc: CPU total"); r.Value != "55,0 %" || r.Limit != "≤ 60 %" || passOf(r) != "true" {
		t.Errorf("CPU total row %+v", r)
	}

	f := meeting()
	f.CPUPctTotal = 65
	envs := owners()
	envs[0] = envelope("pc", "H3", 8, f, step(8, 60, 1, false), step(4, 15, 1, true), step(4, 60, 1, true))
	j = Judge(envs)
	if j.Verdict != results.Fail || !strings.Contains(j.PlanB, "pc: numărul recomandat devine 4") {
		t.Fatalf("at 65 %%: %q, plan B %q, reasons %q; want pică → pc: numărul recomandat devine 4", j.Verdict, j.PlanB, j.Reasons)
	}
	if r := row(t, j, "MI3 pc: CPU total"); r.Value != "65,0 %" || passOf(r) != "false" {
		t.Errorf("CPU total row %+v", r)
	}
	if !strings.Contains(strings.Join(j.Reasons, "\n"), "CPU total") {
		t.Errorf("reasons %q do not name the CPU", j.Reasons)
	}
	if r := row(t, j, "MI3 pc: pașii"); !strings.Contains(r.Value, "8 camere 1 h: pică") || !strings.Contains(r.Value, "4 camere 1 h: trece") {
		t.Errorf("steps row %+v", r)
	}

	t.Run("a failure decides with the steps still open", func(t *testing.T) {
		envs := owners()
		envs[0] = envelope("pc", "H3", 8, f, step(8, 60, 1, false), step(4, 15, 1, true))
		j := Judge(envs)
		if j.Verdict != results.Fail || !strings.Contains(j.PlanB, "pc: numărul recomandat coboară") {
			t.Errorf("Judge = %q, plan B %q; want pică with the number still going down", j.Verdict, j.PlanB)
		}
	})

	t.Run("a missing host leaves G12 without a verdict", func(t *testing.T) {
		j := Judge([]results.Envelope{passing("pc", "H3", 8), reboots(mi1.SetPCBugcheck, "pc", 15_000)})
		if j.Verdict != results.Incomplete {
			t.Errorf("without the Pi: %q, want %q", j.Verdict, results.Incomplete)
		}
	})

	t.Run("an unsupported Pi defers its part", func(t *testing.T) {
		armv6 := results.Envelope{Schema: results.Schema, Code: "MI1", RunID: "pi-probe", Set: "pi-probe", Host: "pi", OS: "linux",
			PiModel: &results.PiModelInfo{Raw: "Raspberry Pi Zero W Rev 1.1", Tier: "unsupported", Known: true}}
		j := Judge([]results.Envelope{passing("pc", "H3", 8), reboots(mi1.SetPCBugcheck, "pc", 15_000), armv6})
		if j.Verdict != results.Deferred("Pi armv6") {
			t.Errorf("Judge = %q, want deferred for the Pi", j.Verdict)
		}
	})
}

// TestMI3_InvalidPacing_JudgedOnlyWhenValid (R07): attempts that the
// controller's pacing invalidated are shown, never judged; a host whose
// step had three of them has no MI3 result, and the report shows the
// pacing.
func TestMI3_InvalidPacing_JudgedOnlyWhenValid(t *testing.T) {
	bad := meeting()
	bad.CPUPctTotal = 90
	envs := owners()
	envs[0] = envelope("pc", "H3", 8, meeting(), invalidStep(8, 60, 1, 110, 0), step(8, 60, 2, true))
	if j := Judge(envs); j.Verdict != results.Pass {
		t.Errorf("a redone attempt: %q, reasons %q; want trece", j.Verdict, j.Reasons)
	}

	bad.PacingP99Ms, bad.SkippedAUs = 110, 1
	envs[0] = envelope("pc", "H3", 8, bad, invalidStep(8, 60, 1, 120, 0), invalidStep(8, 60, 2, 8, 1), invalidStep(8, 60, 3, 110, 1))
	j := Judge(envs)
	if j.Verdict != results.Incomplete {
		t.Fatalf("three invalid attempts: %q, plan B %q; want %q", j.Verdict, j.PlanB, results.Incomplete)
	}
	reasons := strings.Join(j.Reasons, "\n")
	if !strings.Contains(reasons, "110 ms") || !strings.Contains(reasons, "cadre sărite 1") {
		t.Errorf("reasons %q do not show the pacing of the attempts", reasons)
	}
	if r := row(t, j, "MI3 pc: CPU total"); r.Pass != nil {
		t.Errorf("an invalid run's CPU was judged: %+v", r)
	}
}

// TestMI3_ColdBootRowFromRebootRuns (§4.8 cold-boot row, judged on H3): the
// maximum ColdBootMs of the PC's pc-bugcheck runs is judged against 20 s; a
// failure fails G12 with the Technical Decision as its plan B, and the PC's
// recommended number stays. The Pi's pi-sysrq figure is information only,
// and without the PC's reboot runs the row, and G12, have no verdict.
func TestMI3_ColdBootRowFromRebootRuns(t *testing.T) {
	j := Judge(owners())
	r := row(t, j, "MI3 pc: pornire la rece")
	if j.Verdict != results.Pass || r.Value != "15,00 s (20 din 20)" || r.Limit != "≤ 20 s" || passOf(r) != "true" {
		t.Errorf("15 s: %q, row %+v", j.Verdict, r)
	}
	if !strings.Contains(r.Code, "1 cameră, cam01_main") || !strings.Contains(r.Code, "include pornirea Windows") {
		t.Errorf("the row %q does not say what it measured", r.Code)
	}

	envs := owners()
	envs[2] = reboots(mi1.SetPCBugcheck, "pc", 18_000)
	if j := Judge(envs); j.Verdict != results.Pass {
		t.Errorf("18 s: %q, reasons %q; want trece", j.Verdict, j.Reasons)
	}

	envs[2] = reboots(mi1.SetPCBugcheck, "pc", 22_000)
	j = Judge(envs)
	if j.Verdict != results.Fail || !strings.Contains(j.PlanB, "pornirea la rece: Decizie tehnică în PR") || strings.Contains(j.PlanB, "numărul recomandat") {
		t.Errorf("22 s: %q, plan B %q; want pică → pornirea la rece: Decizie tehnică în PR, with the number unchanged", j.Verdict, j.PlanB)
	}
	if r := row(t, j, "MI3 pc: pornire la rece"); passOf(r) != "false" || r.Value != "22,00 s (20 din 20)" {
		t.Errorf("22 s: row %+v", r)
	}

	t.Run("the Pi's figure is information only", func(t *testing.T) {
		envs := append(owners(), reboots(mi1.SetPiSysrq, "pi", 40_000))
		j := Judge(envs)
		if j.Verdict != results.Pass {
			t.Errorf("a 40 s Pi cold boot: %q, reasons %q; want trece", j.Verdict, j.Reasons)
		}
		if r := row(t, j, "MI3 pi: pornire la rece"); r.Pass != nil || r.Value != "40,00 s (20 din 20)" {
			t.Errorf("Pi row %+v, want 40,00 s as information", r)
		}
	})

	t.Run("without the PC's reboot runs there is no verdict", func(t *testing.T) {
		j := Judge(owners()[:2])
		if j.Verdict != results.Incomplete {
			t.Errorf("Judge = %q, want %q", j.Verdict, results.Incomplete)
		}
		if r := row(t, j, "MI3 pc: pornire la rece"); r.Value != "lipsește" || r.Pass != nil {
			t.Errorf("row %+v", r)
		}
	})
}

// TestMI3_ReportsWhatIsNotAGate (R02, C1-R17, G20, §4.8): the encrypted
// cameras' disk ratio, the disk writer's queue peak, the proxies, the
// timeline row and an SD card are reported beside the verdict.
func TestMI3_ReportsWhatIsNotAGate(t *testing.T) {
	envs := owners()
	envs[1].Storage = "sd"
	envs[1].SdConfirmation = &results.SdConfirmation{Text: "DA", AtMs: 1}
	envs[0].MI3.DiskQueuePeakBytes = 6 << 20
	envs[0].MI3.Proxies = []string{"live: test"}
	j := Judge(envs)
	if j.Verdict != results.Pass {
		t.Fatalf("Judge = %q, reasons %q", j.Verdict, j.Reasons)
	}
	if r := row(t, j, "MI3 pc: disc ÷ bitrate, camere criptate"); r.Value != "1,040" || r.Pass != nil {
		t.Errorf("encrypted ratio row %+v", r)
	}
	if r := row(t, j, "MI3 pi: stocare"); !strings.Contains(r.Value, "card SD") {
		t.Errorf("storage row %+v", r)
	}
	notes := strings.Join(j.Notes, "\n")
	for _, want := range []string{"live: test", "timeline", "O17"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes %q lack %q", notes, want)
		}
	}
}

// TestMI3_InformationRows (C1-R17, C1-R19, R07, §4.8; Task 12): beside
// the budgets, the report shows the disk writers' queue peak in a row
// right after the RAM row, each load's CPU, the 16-camera hour of the PC as
// measured or as an estimate, with why it was not measured, the cold-boot
// row by §4.8's name, and §4.8's timeline row as deferred to HUB-20.
func TestMI3_InformationRows(t *testing.T) {
	envs := owners()
	envs[0].MI3.DiskQueuePeakBytes = 6 << 20
	envs[0].MI3.LoadCPUPct = map[string]float64{LoadLive: 6, LoadPlayback: 1.5, LoadExport: 12, LoadUpload: 2}
	j := Judge(envs)
	at := slices.IndexFunc(j.Rows, func(r results.Row) bool { return r.Code == "MI3 pc: RAM" })
	if at < 0 || at+1 >= len(j.Rows) {
		t.Fatalf("no RAM row in %+v", j.Rows)
	}
	if q := j.Rows[at+1]; !strings.Contains(q.Code, "coada de scriere pe disc") || !strings.HasPrefix(q.Value, "6,3 MB") || q.Pass != nil || q.Source != "pc" {
		t.Errorf("the row after RAM = %+v, want the queue peak of 6,3 MB, information", q)
	}
	for load, want := range map[string]string{LoadLive: "6,0 %", LoadPlayback: "1,5 %", LoadExport: "12,0 %", LoadUpload: "2,0 %"} {
		if r := row(t, j, "MI3 pc: CPU-ul încărcării "+loadNames[load]+" ("); r.Value != want || r.Pass != nil {
			t.Errorf("load %s row %+v, want %s, information", load, r, want)
		}
	}
	if r := row(t, j, "MI3 pc: pornire la rece până la înregistrare"); !strings.Contains(r.Code, "1 cameră") || !strings.Contains(r.Code, "include pornirea Windows") {
		t.Errorf("cold-boot row %+v, want the camera count and the OS boot", r)
	}
	if r := row(t, j, "timeline"); r.Value != "amânat: HUB-20, Faza 1" || r.Limit != "≤ 50 ms" || r.Pass != nil {
		t.Errorf("timeline row %+v, want amânat: HUB-20, Faza 1", r)
	}

	const sixteen = "MI3 pc: 16 camere (≥ 6 nuclee)"
	for _, c := range []struct {
		name   string
		mutate func(envs []results.Envelope) []results.Envelope
		want   string
	}{
		{"measured", func(envs []results.Envelope) []results.Envelope {
			envs[0].MI3.Sixteen = new(step(16, 60, 1, true))
			return envs
		}, "măsurat: 16 camere 1 h: trece"},
		{"not run", func(envs []results.Envelope) []results.Envelope { return envs }, "estimare, nemăsurată"},
		{"stopped", func(envs []results.Envelope) []results.Envelope {
			envs[0].MI3.SixteenStopped = "16 camere 1 h: nu s-a putut măsura; proprietarul a oprit proba"
			return envs
		}, "estimare, nemăsurată (16 camere 1 h: nu s-a putut măsura; proprietarul a oprit proba)"},
		{"no MI3 on the PC", func(envs []results.Envelope) []results.Envelope { return envs[1:] }, "estimare, nemăsurată"},
	} {
		t.Run("16 cameras "+c.name, func(t *testing.T) {
			j := Judge(c.mutate(owners()))
			if r := row(t, j, sixteen); r.Value != c.want || r.Pass != nil || r.Source != "pc" {
				t.Errorf("16-camera row %+v, want «%s», information", r, c.want)
			}
			if slices.ContainsFunc(j.Rows, func(r results.Row) bool { return strings.HasPrefix(r.Code, "MI3 pi: 16 camere") }) {
				t.Error("the Pi has a 16-camera row: only H3 has one (R07)")
			}
		})
	}
}

// TestMI3_RecommendedPerHost (§4.8; Task 12 review, M7): the judgement
// gives, per host, the recommended number that G12 leaves, for the
// report's «Rezultat» column: the count of a first hour that passed, or the
// count that the steps settled on after one that failed; none while the
// steps are open.
func TestMI3_RecommendedPerHost(t *testing.T) {
	if got := Judge(owners()).Recommended; !maps.Equal(got, map[string]int{"pc": 8, "pi": 4}) {
		t.Errorf("Recommended = %v, want pc 8 and pi 4, the counts measured", got)
	}
	f := meeting()
	f.CPUPctTotal = 65
	envs := owners()
	envs[0] = envelope("pc", "H3", 8, f, step(8, 60, 1, false), step(4, 15, 1, true), step(4, 60, 1, true))
	if got := Judge(envs).Recommended; !maps.Equal(got, map[string]int{"pc": 4, "pi": 4}) {
		t.Errorf("Recommended = %v, want pc 4, where the steps settled", got)
	}
	envs[0] = envelope("pc", "H3", 8, f, step(8, 60, 1, false))
	if got := Judge(envs).Recommended; !maps.Equal(got, map[string]int{"pi": 4}) {
		t.Errorf("Recommended = %v, want none for the PC while its steps are open", got)
	}
}

// TestMI3_ClearDiskRatioShownWithoutABudget (Task 12 review, M6): on a
// tier without a disk budget (H1b, H1c), the clear cameras' disk ÷ bitrate
// is an information row next to the encrypted cameras', so that the report
// shows both modes; on H3 and H1 it is the budget's own row, not repeated.
func TestMI3_ClearDiskRatioShownWithoutABudget(t *testing.T) {
	j := Judge(owners())
	const info = "disc ÷ bitrate, camere în clar (informativ)"
	at := slices.IndexFunc(j.Rows, func(r results.Row) bool { return r.Code == "MI3 pi: "+info })
	if at < 0 {
		t.Fatalf("no row «MI3 pi: %s» in %+v", info, j.Rows)
	}
	if r := j.Rows[at]; r.Value != "1,010" || r.Limit != "" || r.Pass != nil || r.Source != "pi" {
		t.Errorf("the Pi's clear ratio row %+v, want 1,010, information", r)
	}
	if at+1 >= len(j.Rows) || !strings.HasPrefix(j.Rows[at+1].Code, "MI3 pi: disc ÷ bitrate, camere criptate") {
		t.Errorf("the encrypted ratio does not follow the clear one: %+v", j.Rows)
	}
	if slices.ContainsFunc(j.Rows, func(r results.Row) bool { return r.Code == "MI3 pc: "+info }) {
		t.Error("H3 repeats its clear ratio as information beside its budget row")
	}
	if r := row(t, j, "MI3 pc: disc ÷ bitrate, camere în clar"); passOf(r) != "true" {
		t.Errorf("H3's clear ratio row %+v, want the judged budget", r)
	}
}

// TestMI3_FsyncRowCountsClosedParts (§4.8; Task 9 review): the fsync figure
// comes from the parts' count, (parts − 1) ÷ their seconds, so it sits near
// 1 by construction and cannot see an fsync more: its row says it counts
// closed parts, which MTX-1 fsyncs one each, and the proxies say so too.
func TestMI3_FsyncRowCountsClosedParts(t *testing.T) {
	j := Judge(owners())
	if r := row(t, j, "MI3 pc: părți închise pe secundă"); !strings.Contains(r.Code, "MTX-1") {
		t.Errorf("fsync row %q, want it named a count of closed parts, one fsync each (MTX-1)", r.Code)
	}
	var proxy string
	for _, p := range Proxies {
		if strings.HasPrefix(p, "Părțile închise pe secundă") {
			proxy = p
		}
	}
	if !strings.Contains(proxy, "(proxy)") || !strings.Contains(proxy, "fsync") || !strings.Contains(proxy, "în plus") {
		t.Errorf("proxies %q, want one that says the fsync figure counts closed parts and sees no fsync in plus", Proxies)
	}
}

// TestMI3_PlaybackProxyNamesItsFixture (G20; Task 9 review): the playback
// reads the synthetic 5-min fixture, which stays in the page cache, not a
// recorded closed segment: its proxy says so.
func TestMI3_PlaybackProxyNamesItsFixture(t *testing.T) {
	for _, p := range Proxies {
		if strings.HasPrefix(p, "Redarea (proxy)") {
			if !strings.Contains(p, "sintetic") || !strings.Contains(p, "cache") {
				t.Errorf("playback proxy %q, want it to say it reads the synthetic fixture from the page cache", p)
			}
			return
		}
	}
	t.Fatalf("no playback proxy in %q", Proxies)
}

// TestMI3_ExportShareNote (C1-R19): the export runs for the whole step, a
// floor of CPU that the camera count cannot lower. When cpuTotal fails but
// would pass without the export's share, Judge adds a Romanian Note that
// the result comes from the continuous export and that Task 14 proposes a
// Technical Decision; the verdict stays. Without that condition, on either
// side, there is no such Note.
func TestMI3_ExportShareNote(t *testing.T) {
	for _, c := range []struct {
		name          string
		total, export float64
		note          bool
	}{
		{"cpuTotal fails only with the export", 65, 10, true},
		{"cpuTotal fails without the export too", 75, 10, false},
		{"cpuTotal passes", 55, 10, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := meeting()
			f.CPUPctTotal = c.total
			f.LoadCPUPct = map[string]float64{LoadLive: 6, LoadPlayback: 1, LoadExport: c.export, LoadUpload: 2}
			envs := owners()
			envs[0] = envelope("pc", TierH3, 8, f, step(8, 60, 1, c.total <= 60), step(4, 15, 1, true), step(4, 60, 1, true))
			j := Judge(envs)
			var note string
			for _, n := range j.Notes {
				if strings.Contains(n, "exportul") && strings.Contains(n, "C1-R19") {
					note = n
				}
			}
			if (note != "") != c.note {
				t.Fatalf("export note %q, want one: %v; notes %q", note, c.note, j.Notes)
			}
			if c.note && (!strings.Contains(note, "Decizie tehnică") || !strings.Contains(note, "Task 14") || !strings.Contains(note, "10,0 %")) {
				t.Errorf("export note %q, want the export's share and Task 14's Technical Decision", note)
			}
			if want := map[bool]results.Verdict{true: results.Pass, false: results.Fail}[c.total <= 60]; j.Verdict != want {
				t.Errorf("Judge = %q, want %q: the note changes no verdict", j.Verdict, want)
			}
		})
	}
	if d := dataOf(TierH3, 8, Figures{LoadCPUPct: map[string]float64{LoadExport: 10}}); d.LoadCPUPct[LoadExport] != 10 {
		t.Errorf("the envelope's load CPU %v, want the step's", d.LoadCPUPct)
	}
}
