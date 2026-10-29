package main

import (
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

var update = flag.Bool("update", false, "rewrite testdata/golden.md")

// goldenFixture is the golden report's input: the owner's two devices,
// the Pi on an SD card and recording at 3 cameras after MI3 (G12's plan
// B), the PC with its 16-camera hour, and CI's two runs read through gh,
// the lab without lab-linux-qemu-reset and with lab-windows-kill9 short of
// a part.
func goldenFixture(t *testing.T) *fixture {
	t.Helper()
	f := passing(t).without(func(e results.Envelope) bool {
		return strings.HasPrefix(e.Set, "lab-") || strings.HasPrefix(e.Set, "ci-")
	})
	confirmed := &results.SdConfirmation{Text: "DA", AtMs: time.Date(2026, 10, 27, 18, 4, 0, 0, time.UTC).UnixMilli()}
	f.each(onHost("pi"), func(e *results.Envelope) { e.Storage, e.SdConfirmation = "sd", confirmed })
	f.each(ofCode("MI2", "pi"), func(e *results.Envelope) {
		e.MI2.OnSdCard = true
		e.MI2.CardStates = []results.CardState{{State: "ok", AtMs: 1}, {State: "degraded", AtMs: 2}}
	})
	f.each(ofCode("MI3", "pi"), func(e *results.Envelope) {
		e.MI3.CPUPctTotal = 75.2
		e.MI3.Budgets["cpuTotal"] = results.Budget{Value: 75.2, Limit: 60}
		e.MI3.Steps = []results.Mi3Step{
			{Cameras: 4, Minutes: 60, Attempt: 1, Valid: true, PacingP99Ms: 6},
			{Cameras: 2, Minutes: 15, Attempt: 1, Valid: true, Pass: true, PacingP99Ms: 5},
			{Cameras: 3, Minutes: 15, Attempt: 1, Valid: true, Pass: true, PacingP99Ms: 5},
			{Cameras: 3, Minutes: 60, Attempt: 1, Valid: true, Pass: true, PacingP99Ms: 6},
		}
	})
	f.each(ofCode("MI3", "pc"), func(e *results.Envelope) {
		e.MI3.Sixteen = &results.Mi3Step{Cameras: 16, Minutes: 60, Attempt: 1, Valid: true, Pass: true, PacingP99Ms: 9}
	})
	return f
}

// goldenReport is the golden fixture's report, with CI's runs from the
// fake gh.
func goldenReport(t *testing.T) *report {
	t.Helper()
	installFakeGH(t, annotationsDir)
	in, err := load([]string{goldenFixture(t).folder(t)}, []string{labRun, ciRun})
	if err != nil {
		t.Fatal(err)
	}
	return newReport(in, goldenDate)
}

func TestRender_GoldenMarkdown(t *testing.T) {
	got := goldenReport(t).markdown()
	path := filepath.Join("testdata", "golden.md")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // the test's golden file
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("the report differs from %s (go test -update rewrites it):\n%s", path, got)
	}
	for _, c := range []string{"ş", "ţ", "Ş", "Ţ"} {
		if strings.Contains(got, c) {
			t.Errorf("the report writes %q with a cedilla, not a comma below", c)
		}
	}
}

// TestRender_NoRedactionMatches (G28, PH0-22): a run's InvalidReason
// reaches the MI1 CSV, and the Pi model's Raw the report. Each seed of the
// video spike's dirt list, alone in either field, makes recreport write
// nothing and fail, naming the file, the line and the kind of the match,
// never its text; the clean fixture writes the report and the five CSVs.
func TestRender_NoRedactionMatches(t *testing.T) {
	clean := passing(t)
	out := t.TempDir()
	if code, text := runReport("-in", clean.folder(t), "-out", out, "-date", goldenDate); code != 0 {
		t.Fatalf("recreport on clean results = %d:\n%s", code, text)
	}
	for _, name := range []string{goldenDate + "-phase0c-recorder-results.md", "phase0-data/phase0c-recorder-mi1.csv",
		"phase0-data/phase0c-recorder-mi2.csv", "phase0-data/phase0c-recorder-mi3.csv", "phase0-data/phase0c-recorder-mi4.csv",
		"phase0-data/phase0c-recorder-mi9.csv"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Errorf("recreport did not write %s: %v", name, err)
		}
	}

	dirt := map[string]string{ //nolint:gosec // the video spike's dirt list, credentials made up for the scanner
		"IPv4":      "conexiunea la 192.168.1.20 a căzut",
		"IPv6":      "de la fe80::1ff:fe23:4567:890a",
		"MAC":       "placa aa:bb:cc:dd:ee:ff",
		"e-mail":    "scris de tester@firma.ro",
		"rtsp URL":  "camera rtsp://admin:hunter2@cam/stream1",
		".local":    "gazda living-pc.local",
		"user path": `în C:\Users\Maria\AppData\Local`,
		"serial":    "seria R58M12ABCDE",
	}
	for kind, seed := range dirt {
		for _, field := range []struct {
			name, file string
			plant      func(f *fixture)
		}{
			{"InvalidReason", "phase0c-recorder-mi1.csv", func(f *fixture) {
				f.envs = append(f.envs, invalidRun(t, seed))
			}},
			{"PiModel.Raw", "phase0c-recorder-results.md", func(f *fixture) {
				f.each(onHost("pi"), func(e *results.Envelope) {
					m := *e.PiModel
					m.Raw = "Raspberry Pi 4 Model B " + seed
					e.PiModel = &m
				})
			}},
		} {
			t.Run(kind+" in "+field.name, func(t *testing.T) {
				f := passing(t)
				field.plant(f)
				out := t.TempDir()
				code, text := runReport("-in", f.folder(t), "-out", out, "-date", goldenDate)
				if code != 1 || !strings.Contains(text, field.file+":") {
					t.Errorf("recreport = %d, want 1 and the match named in %s:\n%s", code, field.file, text)
				}
				if strings.Contains(text, seed) {
					t.Errorf("the error repeats what it found:\n%s", text)
				}
				if entries, _ := os.ReadDir(out); len(entries) != 0 {
					t.Errorf("recreport wrote %v", entries)
				}
			})
		}
	}
}

// invalidRun is an invalid run of pc-kill9 whose InvalidReason holds why.
func invalidRun(t *testing.T, why string) results.Envelope {
	t.Helper()
	r := mi1Runs(t, mi1.SetPCKill9, 8, 600)[0]
	r.RunID, r.Attempt, r.Valid, r.InvalidReason, r.Streams = "pc-kill9-01-2", 2, false, why, nil
	e := envelope("MI1", mi1.SetPCKill9, r.RunID, "pc")
	e.Runs = []results.Run{r}
	return e
}

// verdictOf is the verdict cell of gate in md's verdict table.
func verdictOf(t *testing.T, md, gate string) string {
	t.Helper()
	return tableRow(t, section(t, md, "## Verdicte și decizii declanșate (PH0-30)"), gate+":")[1]
}

// TestRender_VerdictIncompleteAndDeferred (G27): an unsupported Pi without
// results moves the Pi's parts to Faza 0-bis, «amânat în Faza 0-bis: Pi
// armv6»; a missing measurement without one, here the phone's AGE run,
// reads «fără verdict: lipsesc măsurători»; a measured failure reads
// «pică → <plan B>» even with other parts missing.
func TestRender_VerdictIncompleteAndDeferred(t *testing.T) {
	f := armv6(t)
	f.app = nil
	md := markdownOf(t, f)
	for gate, want := range map[string]string{
		"G10": "amânat în Faza 0-bis: Pi armv6",
		"G11": "amânat în Faza 0-bis: Pi armv6",
		"G12": "amânat în Faza 0-bis: Pi armv6",
		"G13": "fără verdict: lipsesc măsurători",
		"G18": "trece",
	} {
		if got := verdictOf(t, md, gate); got != want {
			t.Errorf("%s: verdict «%s», want «%s»", gate, got, want)
		}
	}
	g13 := section(t, md, "### G13: `age` cu acces aleator (MI4)")
	if !strings.Contains(g13, "lipsește rularea AGE din aplicația Android a telefonului") {
		t.Errorf("G13's section does not say what is missing:\n%s", g13)
	}

	f = armv6(t)
	f.each(ofCode("MI2", "pc"), func(e *results.Envelope) { e.MI2.Crashes72h = 1 })
	if got := verdictOf(t, markdownOf(t, f), "G11"); got != "pică → ingest inversat (MediaMTX trage camera)" {
		t.Errorf("G11 with a crash on the PC and the Pi deferred: «%s», want the measured failure", got)
	}
}

// armv6 is the passing fixture with an armv6 Pi: no measurement on it, and
// the probe's envelope that names its model.
func armv6(t *testing.T) *fixture {
	t.Helper()
	f := passing(t).without(func(e results.Envelope) bool { return e.Host == "pi" })
	e := envelope("MI1", "pi-probe", "pi-probe-1", "pi")
	e.PiModel = &results.PiModelInfo{Raw: "Raspberry Pi Zero W Rev 1.1", Known: true, Supported: false, Source: "device-tree"}
	return &fixture{envs: append(f.envs, e), app: f.app}
}

// TestRender_Faza0bisStaticTable (G33, §16.9): the table of what moves to
// Faza 0-bis is static, row for row; the armv6 row is there only with an
// unsupported Pi.
func TestRender_Faza0bisStaticTable(t *testing.T) {
	rows := []string{
		"| Element | Poartă | Cost estimat | Termen |",
		"| --- | --- | --- | --- |",
		"| H1 (Pi 5) și H2 (N100): G10, G12 și G13 pe aceste gazde; până atunci, ≈ 8 și 16 camere sunt „estimare, nemăsurată” | G10, G12, G13 | OWN-15 (opțional) | fără termen |",
		"| `agerange` pe iOS-1 (MI4, punctul 3 din ENC-08) | G13 (partea 3) | un iPhone împrumutat; Apple Developer, 99 USD/an (OWN-05) | înainte de Faza 4 |",
	}
	const armv6Row = "| Pi armv6: G10–G13 pe un Pi suportat | G10–G13 | un Pi suportat (OWN-15) | înainte de Faza 4 |"
	for _, c := range []struct {
		name string
		f    *fixture
		want []string
	}{
		{"a supported Pi", passing(t), rows},
		{"an armv6 Pi", armv6(t), append(slices.Clone(rows), armv6Row)},
	} {
		t.Run(c.name, func(t *testing.T) {
			var table []string
			for _, l := range strings.Split(section(t, markdownOf(t, c.f), "## Rămas pentru Faza 0-bis"), "\n") {
				if strings.HasPrefix(l, "|") {
					table = append(table, l)
				}
			}
			if !slices.Equal(table, c.want) {
				t.Errorf("Faza 0-bis table:\n%s\nwant:\n%s", strings.Join(table, "\n"), strings.Join(c.want, "\n"))
			}
		})
	}
}

// TestRender_EstimatesMarked (G31): §4.8's H1 and H2 numbers, not measured
// in Phase 0, read «estimare, nemăsurată»; a Pi 5 of the owner measures H1.
func TestRender_EstimatesMarked(t *testing.T) {
	models := section(t, markdownOf(t, passing(t)), "## Modelul Pi-ului și numerele recomandate (§4.8)")
	for _, want := range []string{"| H1 (Pi 5) | ≈ 8 | — | estimare, nemăsurată |", "| H2 (N100) | 16 | — | estimare, nemăsurată |"} {
		if !strings.Contains(models, want) {
			t.Errorf("the §4.8 section lacks «%s»:\n%s", want, models)
		}
	}
	pi5 := passing(t).each(onHost("pi"), func(e *results.Envelope) {
		e.PiModel = &results.PiModelInfo{Raw: "Raspberry Pi 5 Model B Rev 1.0", Tier: "H1", Supported: true, Known: true, RecommendedMin: 8, RecommendedMax: 8, Source: "cpuinfo"}
	})
	models = section(t, markdownOf(t, pi5), "## Modelul Pi-ului și numerele recomandate (§4.8)")
	if row := tableRow(t, models, "H1 (Pi 5)"); row[1] != "≈ 8" || row[3] != "măsurat pe Pi-ul dvs. (G12, G13)" {
		t.Errorf("with a Pi 5, H1 is measured: %q", row)
	}
}

// TestRender_ArmV6StatesLabBasis (G31): with an unsupported Pi, the report
// says that G10's Linux part rests on the lab until a supported Pi is
// measured, and that the Linux parts of G11, G12 and G13, which have no lab
// legs, read «amânat în Faza 0-bis: Pi armv6».
func TestRender_ArmV6StatesLabBasis(t *testing.T) {
	md := markdownOf(t, armv6(t))
	bis := section(t, md, "## Rămas pentru Faza 0-bis")
	for _, want := range []string{
		"partea Linux a lui G10 se bazează pe laborator (`lab-linux-kill9`, `lab-linux-qemu-reset`) până la măsurarea unui Pi suportat, înainte de Faza 4",
		"părțile Linux ale lui G11, G12 și G13 nu au rulări de laborator, deci citesc „amânat în Faza 0-bis: Pi armv6”",
	} {
		if !strings.Contains(bis, want) {
			t.Errorf("the Faza 0-bis section lacks «%s»:\n%s", want, bis)
		}
	}
	if supported := section(t, markdownOf(t, passing(t)), "## Rămas pentru Faza 0-bis"); strings.Contains(supported, "armv6") {
		t.Errorf("a supported Pi's report speaks of armv6:\n%s", supported)
	}
}

// TestRender_O17BothSettingsShown (O17, C1-R1, C1-R13): each automated set
// has, beside its default-setting rows, its O17-off row «Pierdere minimă
// la pană de curent: oprită (256 kbps, criptat)» with §4.5's limit, 4,000
// ms, and the report says that it does not count in G10.
func TestRender_O17BothSettingsShown(t *testing.T) {
	md := markdownOf(t, passing(t))
	for _, set := range []string{mi1.SetLabLinuxKill9, mi1.SetLabLinuxQEMUReset, mi1.SetLabWindowsKill9, mi1.SetPCKill9, mi1.SetPCBugcheck,
		mi1.SetPiKill9, mi1.SetPiSysrq} {
		row := tableRow(t, md, "MI1 "+set+": Pierdere minimă la pană de curent: oprită (256 kbps, criptat)")
		if row[1] != "1,80 s" || row[2] != "≤ 4 s" || row[3] != "✓" {
			t.Errorf("%s: O17-off row %q, want 1,80 s against ≤ 4 s, ✓", set, row)
		}
		tableRow(t, md, "MI1 "+set+": mediana")
	}
	if strings.Contains(md, "MI1 real-pc: Pierdere minimă") {
		t.Error("a real-cut set has an O17-off row")
	}
	notes := section(t, md, "## Rezultate pe gazdă")
	for _, want := range []string{"4 s (4.000 ms la 256 kbps)", "nu contează în G10"} {
		if !strings.Contains(notes, want) {
			t.Errorf("the notes of the tables lack «%s»", want)
		}
	}
}

// TestRender_O17RowFailsForAMarkedRun (Task 7.1b review, M1 and M4): a set
// that fails only through a run marked O17-off without cam99_main shows
// the failure in its O17-off row, ✗ with the cause, and names the run once.
func TestRender_O17RowFailsForAMarkedRun(t *testing.T) {
	f := passing(t).each(func(e results.Envelope) bool { return e.RunID == "pc-bugcheck-21-1" }, func(e *results.Envelope) {
		e.Runs[0].Streams = e.Runs[0].Streams[:1]
		e.Runs[0].Hub04Clear = false
	})
	md := markdownOf(t, f)
	row := tableRow(t, md, "MI1 pc-bugcheck: Pierdere minimă la pană de curent: oprită")
	if row[3] != "✗" || !strings.Contains(row[1], "rularea pc-bugcheck-21-1: nu înregistrează cam99_main cu setarea oprită; HUB-04") {
		t.Errorf("O17-off row %q, want ✗ with the run's cause", row)
	}
	g10 := section(t, md, "### G10: Pană de curent (MI1)")
	if n := strings.Count(g10, "pc-bugcheck-21-1"); n != 1 {
		t.Errorf("G10's section names the run %d times, want once:\n%s", n, g10)
	}
}

// TestRender_SixteenMeasuredOrEstimate (R07): the PC's 16-camera row reads
// «măsurat» with the hour's figures when MI3 ran it, else «estimare,
// nemăsurată».
func TestRender_SixteenMeasuredOrEstimate(t *testing.T) {
	const code = "MI3 pc: 16 camere (≥ 6 nuclee)"
	if row := tableRow(t, markdownOf(t, passing(t)), code); row[1] != "estimare, nemăsurată" || row[3] != "–" {
		t.Errorf("without the hour: %q, want «estimare, nemăsurată»", row)
	}
	f := passing(t).each(ofCode("MI3", "pc"), func(e *results.Envelope) {
		e.MI3.Sixteen = &results.Mi3Step{Cameras: 16, Minutes: 60, Attempt: 1, Valid: true, Pass: true, PacingP99Ms: 9}
	})
	if row := tableRow(t, markdownOf(t, f), code); row[1] != "măsurat: 16 camere 1 h: trece" {
		t.Errorf("with the hour: %q, want «măsurat: 16 camere 1 h: trece»", row)
	}
}

// TestRender_ColdBootAndTimelineRows (§4.8, G20; Task 9): the cold-boot
// row «pornire la rece până la înregistrare» is judged on the PC (≤ 20 s)
// and information on the Pi, names its one camera and says that the time
// includes the OS's boot; the timeline row reads «amânat: HUB-20, Faza 1».
func TestRender_ColdBootAndTimelineRows(t *testing.T) {
	md := markdownOf(t, passing(t))
	pc := tableRow(t, md, "MI3 pc: pornire la rece până la înregistrare")
	if pc[1] != "15,00 s (20 din 20)" || pc[2] != "≤ 20 s" || pc[3] != "✓" || !strings.Contains(pc[0], "1 cameră") || !strings.Contains(pc[0], "include pornirea Windows") {
		t.Errorf("PC cold-boot row %q", pc)
	}
	pi := tableRow(t, md, "MI3 pi: pornire la rece până la înregistrare")
	if pi[2] != "" || pi[3] != "–" || !strings.Contains(pi[0], "informativ") || !strings.Contains(pi[0], "include pornirea Linux") {
		t.Errorf("Pi cold-boot row %q, want information", pi)
	}
	if tl := tableRow(t, md, "MI3: timeline 24 h"); tl[1] != "amânat: HUB-20, Faza 1" || tl[2] != "≤ 50 ms" {
		t.Errorf("timeline row %q", tl)
	}
	if !strings.Contains(section(t, md, "## Limitări ale măsurătorilor"), "pornire la rece") {
		t.Error("the limits do not say where the cold-boot row comes from")
	}
}

// TestRender_SdSetsSeparate (G16, T40; Task 8): the Pi's results on an SD
// card have a table of their own, with the owner's confirmation, and the
// resumes next to the card's states with what a card's fsync latency means
// for G11; the PC's table, on an SSD, has none of it.
func TestRender_SdSetsSeparate(t *testing.T) {
	md := markdownOf(t, goldenFixture(t))
	sd := section(t, md, "### Raspberry Pi (`pi`) pe card SD (G16)")
	for _, want := range []string{"MI1 pi-kill9: mediana", "MI3 pi: CPU total", "confirmat de proprietar: «DA»", "13–37 s"} {
		if !strings.Contains(sd, want) {
			t.Errorf("the SD section lacks «%s»:\n%s", want, sd)
		}
	}
	lines := strings.Split(sd, "\n")
	resume := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "| MI2 pi: reluarea după kill -9 |") })
	if resume < 0 || resume+1 >= len(lines) || !strings.HasPrefix(lines[resume+1], "| MI2 pi: starea cardului SD | OK → DEGRADAT |") {
		t.Errorf("the card's states do not follow the resumes:\n%s", sd)
	}
	pc := section(t, md, "### PC (`pc`, H3)")
	if strings.Contains(pc, "card SD") || strings.Contains(pc, "pi-kill9") {
		t.Errorf("the PC's table holds SD rows:\n%s", pc)
	}
	if strings.Contains(md, "### Raspberry Pi (`pi`)\n") {
		t.Error("the Pi has a second table besides its SD one")
	}
}

// TestRender_MI3InformationShown (C1-R17, C1-R19, G20; Task 9): the
// report shows the disk writers' queue peak next to the RAM, each load's
// CPU, the export note when the export alone fails the CPU, and the
// proxies, among them the page-cached playback fixture and the fsync
// count of closed parts.
func TestRender_MI3InformationShown(t *testing.T) {
	f := passing(t).each(ofCode("MI3", "pc"), func(e *results.Envelope) {
		e.MI3.CPUPctTotal = 64
		e.MI3.Budgets["cpuTotal"] = results.Budget{Value: 64, Limit: 60}
		e.MI3.LoadCPUPct["export"] = 12.4
		e.MI3.StepsStopped = "8 camere 15 min: nu s-a putut măsura; proprietarul a oprit proba"
	})
	md := markdownOf(t, f)
	pc := section(t, md, "### PC (`pc`, H3)")
	lines := strings.Split(pc, "\n")
	ram := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "| MI3 pc: RAM |") })
	if ram < 0 || ram+1 >= len(lines) || !strings.Contains(lines[ram+1], "coada de scriere pe disc") || !strings.Contains(lines[ram+1], "3,4 MB") {
		t.Errorf("the queue peak does not follow the RAM:\n%s", pc)
	}
	for _, want := range []string{"CPU-ul încărcării de export", "12,4 %", "proprietarul a oprit proba"} {
		if !strings.Contains(pc, want) {
			t.Errorf("the PC's table lacks «%s»", want)
		}
	}
	g12 := section(t, md, "### G12: Niveluri hardware (MI3)")
	for _, want := range []string{"doar prin export", "cache-ul de pagini", "(părți − 1) ÷ durata lor"} {
		if !strings.Contains(g12, want) {
			t.Errorf("G12's notes lack «%s»:\n%s", want, g12)
		}
	}
}

// TestRender_MI9LowerBoundAndFinding (G27, §7.6; Tasks 11 and 11b): a seek
// that the measurement's wait cut reads «≥» as the lower bound it is, a
// partial leg says how far it got, and the report carries §7.6's finding
// on who closes a bulk channel.
func TestRender_MI9LowerBoundAndFinding(t *testing.T) {
	f := passing(t).each(ofCode("MI9", "pi"), func(e *results.Envelope) {
		e.MI9.Legs[0].SeekMs = []float64{300, 400, 36000}
		e.MI9.Legs[0].Partial = &results.Mi9Partial{ReceivedBytes: 150e6, SizeBytes: 150e6, ElapsedMs: 120e3, LastSeekCut: true}
	})
	md := markdownOf(t, f)
	if row := tableRow(t, md, "(lan, pi)"); row[1] != "max ≥ 36000 ms (3 salturi)" || row[3] != "✗" || row[4] != "lan" {
		t.Errorf("the Pi's seek row %q, want the lower bound ≥ 36000 ms, ✗, from lan", row)
	}
	if row := tableRow(t, md, "MI9 LAN (pi): proba încheiată de așteptare"); !strings.Contains(row[1], "s-au măsurat doar 2 din 20 de salturi") {
		t.Errorf("the partial row %q", row)
	}
	if verdictOf(t, md, "G18") != "pică → redare ca track WebRTC 1×, salt prin RPC" {
		t.Errorf("G18 = %q, want the measured failure", verdictOf(t, md, "G18"))
	}
	finding := section(t, md, "## Constatare pentru Faza 1: cine închide canalul bulk (§7.6)")
	for _, want := range []string{"1 rulare din 7", "clientul", "`bulk/<id>`"} {
		if !strings.Contains(finding, want) {
			t.Errorf("the §7.6 finding lacks «%s»:\n%s", want, finding)
		}
	}
}

// TestRender_SourcesPerHost (G26): each host's table names the source of
// each value: lab, pc, pi, ci-relay, ci-chrome, lan.
func TestRender_SourcesPerHost(t *testing.T) {
	md := markdownOf(t, passing(t))
	for _, c := range []struct{ heading, code, source string }{
		{"### PC (`pc`, H3)", "MI1 pc-kill9: mediana", "pc"},
		{"### PC (`pc`, H3)", "MI9 LAN (pc): debit canal bulk", "lan"},
		{"### Raspberry Pi (`pi`)", "MI9 LAN (pi): debit canal bulk", "lan"},
		{"### Laborator și CI", "MI1 lab-linux-kill9: mediana", "lab"},
		{"### Laborator și CI", "MI9 TURN-TLS/443 la 50 ms RTT (lab-linux): debit canal bulk", "ci-relay"},
		{"### Laborator și CI", "MI9 Chrome: primul cadru afișat după salt", "ci-chrome"},
		{"### Telefonul (Galaxy A24, AND-1)", "MI4: agerange în WebView-ul telefonului", "telefon"},
	} {
		if row := tableRow(t, section(t, md, c.heading), c.code); row[4] != c.source {
			t.Errorf("%s: row %q, want source %q", c.heading, row, c.source)
		}
	}
}

// TestRender_DecisionsInPH0Format (PH0-30, PH0-31): each gate has its
// verdict, the decision it triggers and what changes in §2 or the
// requirements; a failure names its plan B as a §2.2 Technical Decision.
func TestRender_DecisionsInPH0Format(t *testing.T) {
	md := goldenReport(t).markdown()
	g12 := section(t, md, "### G12: Niveluri hardware (MI3)")
	for _, want := range []string{
		"- **Verdict:** pică → pi: numărul recomandat devine 3",
		"- **Decizie declanșată:**",
		"- **Ce se schimbă în §2 sau în cerințe:** §2.2: Decizie tehnică „pi: numărul recomandat devine 3”, cu trimitere la G12 (PH0-31); §4.8: numerele recomandate măsurate.",
	} {
		if !strings.Contains(g12, want) {
			t.Errorf("G12's section lacks «%s»:\n%s", want, g12)
		}
	}
	if g13 := section(t, md, "### G13: `age` cu acces aleator (MI4)"); !strings.Contains(g13, "- **Ce se schimbă în §2 sau în cerințe:** Nimic în §2 sau în cerințe.") {
		t.Errorf("a passing gate changes nothing:\n%s", g13)
	}
}

// TestRender_ModelTableShowsTheResultingNumber (Task 12 review, M7): the
// §4.8 table gives, next to each host's starting number, the number that
// the measurements leave: G12's, or MI4's when the Pi's encryption rate
// lowers it more (G13), named by the gate that lowered it; a host whose
// steps have not settled its number says so. The summary sends the owner
// there for the resulting number.
func TestRender_ModelTableShowsTheResultingNumber(t *testing.T) {
	const heading = "## Modelul Pi-ului și numerele recomandate (§4.8)"
	golden := goldenReport(t).markdown()
	models := section(t, golden, heading)
	for _, want := range []string{
		"| Nivel | Camere recomandate (§4.8) | Rezultat | Măsurat |",
		"| H3: PC-ul dvs. | 8 (16 cu ≥ 6 nuclee) | 8 camere | măsurat pe PC-ul dvs. (G12, G13) |",
		"| H1b: Pi-ul dvs. | 4 camere | 3 camere (G12) | măsurat pe Pi-ul dvs. (G12, G13) |",
		"| H1 (Pi 5) | ≈ 8 | — | estimare, nemăsurată |",
	} {
		if !strings.Contains(models, want) {
			t.Errorf("the §4.8 section lacks «%s»:\n%s", want, models)
		}
	}
	if !strings.Contains(section(t, golden, "## Rezumat pentru proprietar"), "Numărul rezultat e la „Modelul Pi-ului și numerele recomandate”") {
		t.Error("the summary does not send the owner to the resulting number")
	}

	slow := passing(t).each(ofCode("MI4", "pi"), func(e *results.Envelope) { e.MI4.CryptoMBs = 12 })
	if models := section(t, markdownOf(t, slow), heading); !strings.Contains(models, "| H1b: Pi-ul dvs. | 4 camere | 2 camere (G13) |") {
		t.Errorf("with 12 MB/s on one core, MI4 lowers the Pi to 2 cameras:\n%s", models)
	}
	open := passing(t).each(ofCode("MI3", "pi"), func(e *results.Envelope) {
		e.MI3.Budgets["cpuTotal"] = results.Budget{Value: 75.2, Limit: 60}
		e.MI3.Steps = []results.Mi3Step{{Cameras: 4, Minutes: 60, Attempt: 1, Valid: true, PacingP99Ms: 6}}
	})
	if models := section(t, markdownOf(t, open), heading); !strings.Contains(models, "| H1b: Pi-ul dvs. | 4 camere | nestabilit (G12) |") {
		t.Errorf("with G12's steps open, the Pi's number is not settled:\n%s", models)
	}
	// G12's steps open, and G13's rate already caps the Pi lower (Task 12b
	// review): the cell says both.
	open.each(ofCode("MI4", "pi"), func(e *results.Envelope) { e.MI4.CryptoMBs = 12 })
	if models := section(t, markdownOf(t, open), heading); !strings.Contains(models,
		"| H1b: Pi-ul dvs. | 4 camere | nestabilit (G12); cel mult 2 camere (G13) |") {
		t.Errorf("with G12's steps open and G13 at 2 cameras, the cell does not show G13's cap:\n%s", models)
	}
}

// TestRender_MixedStorageNamesSdSets (G16; Task 12 review, M11): a Pi with
// sets on an SD card and on another storage has one table, since the
// judges give their rows per host, not per storage; the line above it says
// that the table holds both and names the sets on each storage.
func TestRender_MixedStorageNamesSdSets(t *testing.T) {
	confirmed := &results.SdConfirmation{Text: "DA", AtMs: time.Date(2026, 10, 27, 18, 4, 0, 0, time.UTC).UnixMilli()}
	f := passing(t).each(func(e results.Envelope) bool { return e.Host == "pi" && e.Set == mi1.SetPiKill9 }, func(e *results.Envelope) {
		e.Storage, e.SdConfirmation = "sd", confirmed
	})
	pi := section(t, markdownOf(t, f), "### Raspberry Pi (`pi`), pe mai multe stocări (G16)")
	for _, want := range []string{
		"Seturile de pe card SD: `MI1 pi-kill9`.",
		"Pe SSD: `MI1 pi-sysrq`, `MI1 real-pi`, `MI2 pi-72h`, `MI3 pi-mi3`, `MI4 pi-mi4`, `MI9 lan`.",
		"confirmat de proprietar: «DA»",
		"| MI1 pi-kill9: mediana |",
		"| MI1 pi-sysrq: mediana |",
	} {
		if !strings.Contains(pi, want) {
			t.Errorf("the Pi's table lacks «%s»:\n%s", want, pi)
		}
	}
	if strings.Contains(pi, "separat de celelalte") {
		t.Errorf("the Pi's table claims its SD results apart, though it holds both storages:\n%s", pi)
	}
}

// TestRender_O17ProposalOnlyOnItsSignal (Task 12 review, M8): the O17
// proposal for the report's PR follows G10's own signal that an O17-off
// loss went over §4.5's bound (O17OffOverLimit), not any note of G10; G10's
// section says once that it is no decision triggered.
func TestRender_O17ProposalOnlyOnItsSignal(t *testing.T) {
	r := reportOf(t, passing(t))
	r.gates[0].j.Notes = append(r.gates[0].j.Notes, "o altă notă a lui G10, fără legătură cu O17")
	if md := r.markdown(); strings.Contains(md, "De propus în PR") {
		t.Errorf("a G10 note that is no O17 loss triggers the O17 proposal:\n%s", section(t, md, "### G10: Pană de curent (MI1)"))
	}

	over := passing(t).each(func(e results.Envelope) bool { return e.RunID == "pc-kill9-21-1" }, func(e *results.Envelope) {
		i := slices.IndexFunc(e.Runs[0].Streams, func(s results.StreamLoss) bool { return s.Stream == mi1.LowBitrateStream })
		e.Runs[0].Streams[i].LossMs = 4500
	})
	md := markdownOf(t, over)
	if !strings.Contains(section(t, md, "## Rezumat pentru proprietar"), "**De propus în PR:**") {
		t.Errorf("an O17-off loss over its limit does not bring the proposal into the summary:\n%s", md)
	}
	g10 := section(t, md, "### G10: Pană de curent (MI1)")
	if !strings.Contains(g10, "**De propus în PR:**") || strings.Count(g10, "nu e o decizie declanșată") != 1 {
		t.Errorf("G10's section, want the proposal saying once that it is no decision triggered:\n%s", g10)
	}
}
