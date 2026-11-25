package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi5"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The golden fixture: the PC's results zip, the Pi's, and the lab's
// workflow run, whose mi5-lab notice comes in the second of two pages.
const (
	goldenPC    = "testdata/pc"
	goldenPi    = "testdata/pi"
	goldenGH    = "testdata/gh"
	goldenRun   = "111"
	goldenFile  = "testdata/golden.md"
	goldenDate  = "2026-11-24"
	goldenUnits = "812"
)

// labDone is when the fixture's lab job completed.
var labDone = time.Date(2026, 11, 19, 7, 45, 0, 0, time.UTC)

// writeFixtures writes the golden fixture under testdata/ (go test -update).
func writeFixtures(t *testing.T) {
	t.Helper()
	for _, d := range []string{goldenPC, goldenPi, goldenGH} {
		if err := os.RemoveAll(d); err != nil {
			t.Fatal(err)
		}
	}
	writeEnv(t, goldenPC, mi5Env("pc", "ssd"))
	writeEnv(t, goldenPC, mi6Env())
	writeEnv(t, goldenPC, mi8Env())
	writeFile(t, filepath.Join(goldenPC, "app", "android-sm-a546b-20261118-101500-oauth.json"),
		[]byte(`{"scenario":"OAUTH","device":{"platform":"android","model":"SM-A546B"}}`+"\n"))
	writeEnv(t, goldenPi, mi5Env("pi", "sd"))
	lab := labEnv()
	writeGHRun(t, goldenGH, goldenRun,
		[]ghJob{{id: 9001, completed: labDone.Add(-20 * time.Minute), pages: [][]ghAnnotation{{{"notice", "test-times", "ok mi5 12s"}}}}},
		[]ghJob{{id: 9002, completed: labDone, pages: [][]ghAnnotation{
			{{"failure", "build", "nothing"}},
			{{"notice", titleMI5Lab, string(mustJSON(t, lab))}},
		}}})
}

// TestRender_GoldenMarkdown: the fixture's report matches golden.md.
func TestRender_GoldenMarkdown(t *testing.T) {
	if *update {
		writeFixtures(t)
	}
	installFakeGH(t, goldenGH)
	out := t.TempDir()
	code, _, stderr := runCmd("-in", goldenPC, "-in", goldenPi, "-annotations", goldenRun, "-onedrive", oneDriveUnknown,
		"-console-units", goldenUnits, "-date", goldenDate, "-out", out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got, err := os.ReadFile(filepath.Join(out, goldenDate+"-phase0c-upload-results.md")) //nolint:gosec // the test's output
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		writeFile(t, goldenFile, got)
	}
	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("the report differs from %s (go test -update rewrites it):\n%s", goldenFile, got)
	}
}

// TestRender_NoSecretInOutput (Review Focus 5): each kind of secret,
// assembled from parts and planted alone into a field that the report
// renders, fails the run, writes nothing and never reaches stderr.
func TestRender_NoSecretInOutput(t *testing.T) {
	cases := []struct {
		name, secret string
		plant        func(*[]results.Envelope, string)
	}{
		{"email in a run's invalid reason", "ion.pop" + "escu@" + "gmail.com", func(es *[]results.Envelope, s string) {
			(*es)[0].Runs[0].InvalidReason = "scanarea a găsit " + s
		}},
		{"display name as a plan A failure", "Ion " + "Popescu", func(es *[]results.Envelope, s string) {
			(*es)[1].MI6.Pairs[0].PlanAFailure = s
		}},
		{"Drive file ID as a camera model", "1" + "Zx9Qw" + "Er7Ty6Ui5Op4As3Df2Gh1Jk0Lz", func(es *[]results.Envelope, s string) {
			(*es)[2].MI8.Cameras[0].Model = s
		}},
		{"access token in a deferred destination", "ya" + "29." + "a0AfB_byC1d2E3f4G5h6I7j8K9", func(es *[]results.Envelope, s string) {
			(*es)[0].MI5.Dests[1].Deferred = "amânat în Faza 0-bis: " + s
		}},
		{"refresh token as a camera brand", "1/" + "/0gAbCdEfGhIjKlMnOpQr", func(es *[]results.Envelope, s string) {
			(*es)[2].MI8.Cameras[1].Brand = s
		}},
		{"camera password as a camera model", "Kx9#" + "mQ2!vR", func(es *[]results.Envelope, s string) {
			(*es)[2].MI8.Cameras[1].Model = s
		}},
		{"camera IP as a camera brand", "192.168." + "1.64", func(es *[]results.Envelope, s string) {
			(*es)[2].MI8.Cameras[0].Brand = s
		}},
		{"camera IP as the SD confirmation", "10.0." + "0.23", func(es *[]results.Envelope, s string) {
			(*es)[3].SdConfirmation.Text = s
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			envs := []results.Envelope{mi5Env("pc", "ssd"), mi6Env(), mi8Env(), mi5Env("pi", "sd")}
			c.plant(&envs, c.secret)
			in := t.TempDir()
			for _, e := range envs {
				writeEnv(t, in, e)
			}
			out := filepath.Join(t.TempDir(), "out")
			code, stdout, stderr := runCmd("-in", in, "-onedrive", oneDriveUnknown, "-out", out)
			if code != 1 {
				t.Fatalf("exit %d, want 1: %s", code, stderr)
			}
			if files := filesIn(t, out); len(files) > 0 {
				t.Fatalf("wrote %v", files)
			}
			if strings.Contains(stdout+stderr, c.secret) {
				t.Fatalf("the output repeats the secret: %s", stderr)
			}
		})
	}
	// The output scanners alone catch what the field checks would let
	// through: each kind in a rendered free text.
	for _, s := range []string{"ion.pop" + "escu@" + "gmail.com", "ya" + "29." + "a0AfB_byC1d2E3f4G5h6I7j8K9",
		"1/" + "/0gAbCdEfGhIjKlMnOpQr", "1" + "Zx9Qw" + "Er7Ty6Ui5Op4As3Df2Gh1Jk0Lz", "192.168." + "1.64"} {
		e := mi8Env()
		e.MI8.Cameras[0].Model = s
		if len(scanText(renderOf([]results.Envelope{e}, defaultOpts()))) == 0 {
			t.Errorf("the scanners miss a planted %d-byte value", len(s))
		}
	}
}

// faza0bisAll are the static table's rows 1–9, exactly (P23, P28, P55).
var faza0bisAll = []string{
	"G14 – OneDrive business (pana de 6 h), cu tenantul MI7: Entra ID P1, OWN-09, înainte de Faza 3",
	"G14 și G15 – OneDrive personal: înregistrarea Microsoft cu date de plată, OWN-08, înainte de Faza 3",
	"G15 – combinațiile iOS: Apple Developer (OWN-05, 99 USD/an) și un iPhone împrumutat, înainte de Faza 3",
	"G15 – refresh-ul Drive din ziua 7 și de după, cu proiectul în Production: domeniul pentru verificarea brandului (OWN-04, OWN-07), înainte de Faza 3",
	"G15 – OneDrive business: tenantul de test (OWN-09), înainte de Faza 3",
	"G16 (MI7) – Conditional Access și `AppFolder` pe conturi de firmă: Entra ID P1 (OWN-09, ≈ 6–7 USD/lună), înainte de Faza 3",
	"ipcam-lab: TST-07 de 6 h pe conturi reale (PH3-02) și stabilitatea de 72 h, OWN-22 ≈ 15 €/lună, înainte de Faza 3",
	"redirect-ul OneDrive de pe `<domeniu>`, după domeniu (OWN-04), înainte de Faza 3",
	"STO-01 nightly pe conturi reale (§14.5): Drive după trecerea proiectului în Production (OWN-07, înainte de Faza 3); OneDrive personal e gratuit dacă înregistrarea e gratuită (OWN-08) și se programează după rotația săptămânală a secretelor (OWN-23, Faza 1); OneDrive business (OWN-09), B2, R2, Wasabi și AWS SSE-KMS: Faza 0-bis, cost, înainte de Faza 3",
}

// TestRender_Faza0bisStaticTable: -onedrive free gives rows 1 and 3–9;
// paid and unknown give rows 1–9; the texts are exact.
func TestRender_Faza0bisStaticTable(t *testing.T) {
	for _, c := range []struct {
		oneDrive string
		rows     []int
	}{{oneDriveFree, []int{1, 3, 4, 5, 6, 7, 8, 9}}, {oneDrivePaid, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}}, {oneDriveUnknown, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}}} {
		opts := defaultOpts()
		opts.oneDrive = c.oneDrive
		md := renderOf([]results.Envelope{mi5Env("pc", "ssd")}, opts)
		section := md[strings.Index(md, headingFaza0bis):]
		section = section[:strings.Index(section[1:], "\n## ")+1]
		var got []string
		for _, line := range strings.Split(section, "\n") {
			if strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| Nr.") && !strings.HasPrefix(line, "| ---") {
				cells := strings.Split(strings.Trim(line, "| "), " | ")
				got = append(got, cells[len(cells)-1])
			}
		}
		var want []string
		for _, n := range c.rows {
			want = append(want, faza0bisAll[n-1])
		}
		if !slices.Equal(got, want) {
			t.Errorf("-onedrive %s: rows\n%q\nwant\n%q", c.oneDrive, got, want)
		}
	}
}

// TestRender_VerdictStrings: each of P22's four strings renders from a
// Judge result, in the verdict table and in the gate's section.
func TestRender_VerdictStrings(t *testing.T) {
	missing := mi5Env("pc", "ssd")
	missing.MI5.Dests[0].Missing, missing.MI5.Dests[0].Verified = 1, 287
	deferred := results.Judgement{Gate: "G14", Verdict: results.Deferred("Pi armv6"), Reasons: []string{}}
	for i, j := range []results.Judgement{mi5.Judge([]results.Envelope{mi5Env("pc", "ssd")}), mi5.Judge([]results.Envelope{missing}),
		mi5.Judge(nil), deferred} {
		if want := []results.Verdict{results.Pass, results.FailWith(mi5.PlanBOutbox), results.Incomplete, results.Deferred("Pi armv6")}[i]; j.Verdict != want {
			t.Fatalf("judgement %d: %q, want %q", i, j.Verdict, want)
		}
		r := newReport(inputsOf(), defaultOpts())
		r.gates[0].j = j
		md := r.markdown()
		mustContain(t, md, "| "+r.gates[0].title()+" | "+string(j.Verdict)+" |", "- **Verdict:** "+string(j.Verdict)+"\n")
	}
}

// TestRender_DriveUnitsOnlyForPricedMethods: files.get at 2 per segment is
// 10 units; upload.put at 3 has its units from the console (P18).
func TestRender_DriveUnitsOnlyForPricedMethods(t *testing.T) {
	opts := defaultOpts()
	opts.consoleUnits = 812
	md := renderOf([]results.Envelope{mi5Env("pc", "ssd")}, opts)
	mustContain(t, md, "| dst-1 | files.get | 2 apeluri, 10 unități |", "| dst-1 | upload.put | 3 apeluri, unități: din consolă |",
		"| dst-1 | files.create.resumable | 1 apel, unități: din consolă |", "812 unități pe segment")
	if strings.Contains(md, "upload.put | 3 apeluri, 15") {
		t.Error("upload.put priced")
	}
	md = renderOf([]results.Envelope{mi5Env("pc", "ssd")}, defaultOpts())
	mustContain(t, md, msgConsoleUnitsMissing)
}

// TestRender_G15DecisionsLines: the G15 judge's §2.2 lines (B1, F6) render
// under the gate's decisions.
func TestRender_G15DecisionsLines(t *testing.T) {
	e := mi6Env()
	e.MI6.Pairs[0].Plan, e.MI6.Pairs[0].PlanAFailure = "B1", "access_denied"
	for i := range e.MI6.Visibility {
		if c := &e.MI6.Visibility[i]; c.Creator == "tv" && c.Reader == "android" {
			c.Read = false
		}
	}
	md := renderOf([]results.Envelope{e}, defaultOpts())
	section := md[strings.Index(md, "### G15"):]
	section = section[:strings.Index(section[1:], "\n### ")+1]
	mustContain(t, section, "**Decizii (§2.2):**", "- G15: Android × Google Drive folosește B1 (device flow pe hub).",
		"- F6 pe Android folosește tipul de client al hub-ului (creatori: tv).", "pică → F6 folosește tipul de client al hub-ului")
}

// TestRender_G17GroupedByBrand: MI8 renders by brand, in order, then per
// camera, with the reference method and the 10 s quiet gap.
func TestRender_G17GroupedByBrand(t *testing.T) {
	e := mi8Env()
	start := time.UnixMilli(e.Runs[0].StartMs)
	e.MI8.Cameras = append(e.MI8.Cameras, mi8Camera("real-3", "Hikvision", "DS-2CD1023G0E-I", 21, 21, start))
	e.MI8.Cameras[2].ReferenceMethod = results.ReferenceSnapshot
	md := renderOf([]results.Envelope{e}, defaultOpts())
	section := md[strings.Index(md, headingMI8):]
	hik, reo := strings.Index(section, "#### Hikvision"), strings.Index(section, "#### Reolink")
	if hik < 0 || reo < 0 || hik > reo {
		t.Fatalf("brands not grouped in order:\n%s", section)
	}
	hikPart := section[hik:reo]
	mustContain(t, hikPart, "| real-1 | DS-2CD2143G2-I |", "| real-3 | DS-2CD1023G0E-I |", "instantanee ONVIF")
	mustContain(t, section[reo:], "| real-2 | RLC-510A |", "substream")
	mustContain(t, section, "10 s fără mișcare", "73.0 h")
}

// TestRender_SdResultsSeparate: the Pi's results on an SD card get their
// own table, apart from the SSD's (P17).
func TestRender_SdResultsSeparate(t *testing.T) {
	md := renderOf([]results.Envelope{mi5Env("pc", "ssd"), mi5Env("pi", "sd")}, defaultOpts())
	section := md[strings.Index(md, headingMI5):]
	ssd, sd := strings.Index(section, tableSSD), strings.Index(section, tableSD)
	if ssd < 0 || sd < 0 {
		t.Fatalf("missing a table:\n%s", section)
	}
	pcRun, piRun := displayRunID(mi5Env("pc", "ssd").RunID), displayRunID(mi5Env("pi", "sd").RunID)
	first, second := section[ssd:sd], section[sd:]
	if !strings.Contains(first, pcRun) || strings.Contains(first, piRun) {
		t.Errorf("SSD table:\n%s", first)
	}
	if !strings.Contains(second[:strings.Index(second, "\n\n#")], piRun) {
		t.Errorf("SD table:\n%s", second)
	}
	mustContain(t, second, "«DA»")
}

// TestRender_LimitariListed: every limit is listed, P40's LAN race
// included; the pi-probe line only with a pi-probe envelope.
func TestRender_LimitariListed(t *testing.T) {
	md := renderOf([]results.Envelope{mi5Env("pc", "ssd")}, defaultOpts())
	section := md[strings.Index(md, headingLimits):]
	for _, l := range limitItems {
		mustContain(t, section, "- "+l+"\n")
	}
	mustContain(t, section, "un dispozitiv din rețeaua de acasă ar putea citi adresa de autentificare",
		"P40", "P17", "P09", "P15", "P14", "P03", "P19", "P26", "P18", "P34")
	if strings.Contains(md, piProbeLimit) {
		t.Error("the pi-probe line without a pi-probe envelope")
	}
}

// TestRender_PiProbeMarkerInLimitari: a pi-probe envelope adds its line to
// «Limitări» and enters no Judge.
func TestRender_PiProbeMarkerInLimitari(t *testing.T) {
	probe := results.PiProbeMarker("Raspberry Pi Zero W Rev 1.1", "9000c1", "cpuinfo", ms(mi5End))
	r := newReport(inputsOf(probe), defaultOpts())
	md := r.markdown()
	mustContain(t, md[strings.Index(md, headingLimits):], "- "+piProbeLimit+"\n")
	if piProbeLimit != "Pi armv6: proba pe Pi trece în Faza 0-bis" {
		t.Errorf("pi-probe line %q", piProbeLimit)
	}
	for _, g := range r.gates {
		if g.j.Verdict != results.Incomplete || slices.ContainsFunc(g.j.Rows, func(row results.Row) bool { return strings.Contains(row.Source, "pi-probe") }) {
			t.Errorf("%s judged the marker: %+v", g.id, g.j)
		}
	}
	if got := r.judged(); len(got) != 0 {
		t.Errorf("judged set %v", got)
	}
}

// TestRender_G17_D3C_Note: -d3 c without an MI8 envelope renders G17 as
// fără verdict with D3-C's note.
func TestRender_G17_D3C_Note(t *testing.T) {
	opts := defaultOpts()
	opts.d3 = d3AfterPhase
	r := newReport(inputsOf(mi5Env("pc", "ssd")), opts)
	md := r.markdown()
	g17 := r.gates[2]
	mustContain(t, md, "| "+g17.title()+" | fără verdict: lipsesc măsurători |", "MI8 rulează după detectorul din Faza 0 (a) (D3-C)")
	if g17.j.Verdict != results.Incomplete {
		t.Errorf("G17 %q", g17.j.Verdict)
	}
}

// TestRender_LabEnvelopeIsTST07Row: a lab-linux envelope renders as a
// TST-07 row and leaves G14 at fără verdict without an owner envelope.
func TestRender_LabEnvelopeIsTST07Row(t *testing.T) {
	r := newReport(inputsOf(labEnv()), defaultOpts())
	md := r.markdown()
	if r.gates[0].j.Verdict != results.Incomplete {
		t.Errorf("G14 %q", r.gates[0].j.Verdict)
	}
	mustContain(t, md, "| TST-07 | dst-1: 16/16 verificate, 0 lipsă, 0 duplicate, 0 hash-uri greșite", "| ✓ | MI5/lab-linux/")
	for _, row := range r.gates[0].j.Rows {
		if strings.Contains(row.Source, "lab-linux") {
			t.Errorf("G14 row from the lab: %+v", row)
		}
	}
}
