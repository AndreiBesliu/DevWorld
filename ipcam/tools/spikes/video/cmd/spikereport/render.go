package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Report is the report of a set of results: what ComputeMV judged, the gates it triggers,
// and the files that were not usable.
type Report struct {
	date    string
	skipped []Skipped
	ev      evaluation
	gates   []Gate
	// devices are the platforms that have results, in the order of the report.
	devices []string
}

// BuildReport judges the results and prepares the report dated `date` (YYYY-MM-DD). It
// does not change `results`; every text from them passes through scrubText first (PH0-22).
func BuildReport(results []Result, skipped []Skipped, date string) Report {
	cleaned := make([]Result, len(results))
	for i, r := range results {
		cleaned[i] = cleanResult(r)
	}
	r := Report{date: date, ev: evaluate(cleaned)}
	r.gates = buildGates(r.ev.MV)
	for _, s := range skipped {
		r.skipped = append(r.skipped, Skipped{File: scrubText(s.File), Reason: scrubText(s.Reason)})
	}
	sort.SliceStable(r.skipped, func(i, j int) bool { return r.skipped[i].File < r.skipped[j].File })
	seen := map[string]bool{}
	for _, run := range r.ev.Runs { // sorted by device
		if p := run.Device.Platform; !seen[p] {
			seen[p] = true
			r.devices = append(r.devices, p)
		}
	}
	return r
}

// cleanResult is a copy of r in which every text has passed through scrubText.
func cleanResult(r Result) Result {
	r.Scenario = scrubText(r.Scenario)
	r.File = scrubText(r.File)
	clean := func(s string) string { return scrubText(shortVersions(s)) }
	r.Device = Device{Platform: scrubText(r.Device.Platform), Model: clean(r.Device.Model), OS: clean(r.Device.OS), WebView: clean(r.Device.WebView)}
	notes := make([]string, len(r.Notes))
	for i, n := range r.Notes {
		notes[i] = scrubText(n)
	}
	r.Notes = notes
	samples := make(map[string][]float64, len(r.Samples))
	for k, v := range r.Samples {
		samples[scrubText(k)] = v
	}
	r.Samples = samples
	return r
}

// FileName is the name of the Markdown report, <date>-phase0b-results.md.
func (r Report) FileName() string { return r.date + "-phase0b-results.md" }

// ---- CSV ----

var statusWord = map[RunStatus]string{RunUsed: "used", RunSuperseded: "superseded", RunInterrupted: "interrupted", RunFailed: "failed"}

// The CSV files go to <out>/phase0-data/ and are named phase0b-<device>.csv.
const (
	csvDir    = "phase0-data"
	csvPrefix = "phase0b-"
	csvSuffix = ".csv"
)

// csvName is the path of the CSV file of a device, relative to the output folder.
func csvName(platform string) string {
	slug := strings.Map(func(c rune) rune {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			return c
		}
		return '-'
	}, strings.ToLower(platform))
	return csvDir + "/" + csvPrefix + slug + csvSuffix
}

// CSVs are the raw samples, one file per device and one row per sample, keyed by the path
// relative to the output folder. Runs that did not count are in them too, with their status.
func (r Report) CSVs() map[string]string {
	files := map[string]string{}
	for _, platform := range r.devices {
		var buf bytes.Buffer
		w := csv.NewWriter(&buf)
		_ = w.Write([]string{"device", "scenario", "run", "status", "key", "index", "value"})
		for _, run := range r.ev.Runs {
			if run.Device.Platform != platform {
				continue
			}
			samples := make(map[string][]float64, len(run.Samples)+1)
			for k, v := range run.Samples {
				samples[k] = v
			}
			if len(run.HubJitterMs) > 0 {
				samples["hubJitterMs"] = run.HubJitterMs
			}
			keys := make([]string, 0, len(samples))
			for k := range samples {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				for i, v := range samples[k] {
					_ = w.Write([]string{platform, run.Scenario, run.File, statusWord[run.Status], k, strconv.Itoa(i), strconv.FormatFloat(v, 'f', -1, 64)})
				}
			}
		}
		w.Flush()
		files[csvName(platform)] = buf.String()
	}
	return files
}

// ---- Markdown ----

// doc builds the Markdown. text writes a line as it is; printf formats one (a percent sign is %%).
type doc struct{ strings.Builder }

func (d *doc) text(s string) { _, _ = d.WriteString(s + "\n") }

func (d *doc) printf(format string, args ...any) { fmt.Fprintf(&d.Builder, format+"\n", args...) }

func (d *doc) blank() { _, _ = d.WriteString("\n") }

// row writes a table row; a pipe or a line break in a cell would break the table.
func (d *doc) row(cells ...string) {
	escaped := make([]string, len(cells))
	for i, c := range cells {
		escaped[i] = strings.ReplaceAll(strings.ReplaceAll(c, "\n", " "), "|", `\|`)
	}
	d.text("| " + strings.Join(escaped, " | ") + " |")
}

func (d *doc) header(cells ...string) {
	d.row(cells...)
	d.row(strings.Split(strings.Repeat("---,", len(cells)), ",")[:len(cells)]...)
}

// Markdown is the owner's report in Romanian. It depends only on the results: the same
// results, in any order, give the same text.
func (r Report) Markdown() string {
	var d doc
	d.text("# Rezultatele Fazei 0 (b): spike-ul video")
	d.blank()
	used, excluded, superseded := r.counts()
	d.printf("Raport din %s, generat de `spikereport` din fișierele de rezultate ale spike-ului. Rulări citite: %d; folosite la praguri: %d; excluse (întrerupte sau eșuate): %d; înlocuite de o rulare mai nouă a aceluiași scenariu: %d. Cifrele brute sunt în fișierele CSV de la sfârșit (PH0-30).",
		r.date, len(r.ev.Runs), used, excluded, superseded)
	d.text("Textele din rezultate trec printr-un filtru care scoate adrese, URL-uri, adrese MAC, e-mailuri și valorile etichetate `serial` și `ssid` (PH0-22); proprietarul citește raportul înainte de commit.")
	d.blank()
	r.summary(&d)
	r.devicesSection(&d)
	r.warnings(&d)
	r.decisions(&d)
	r.leftovers(&d)
	r.limits(&d)
	r.rawData(&d)
	r.unreadable(&d)
	return d.String()
}

func (r Report) counts() (used, excluded, superseded int) {
	for _, run := range r.ev.Runs {
		switch run.Status {
		case RunUsed:
			used++
		case RunSuperseded:
			superseded++
		case RunInterrupted, RunFailed:
			excluded++
		}
	}
	return used, excluded, superseded
}

func (r Report) gate(id string) Gate {
	for _, g := range r.gates {
		if g.ID == id {
			return g
		}
	}
	return Gate{ID: id}
}

// explain is the one-sentence reason of a verdict, for the summary.
func explain(g Gate) string {
	switch g.Verdict {
	case VerdictGOProvizoriu:
		return "Toate cele 8 praguri critice trec (MV1, MV2, MV3 cu 4 plăci, MV9, MV10 și, cu 9 plăci, MV3, MV4, MV5). Verdictul e provizoriu: iOS se măsoară în Faza 0-bis."
	case VerdictGOConditionat:
		return fmt.Sprintf("Cu 4 plăci trece tot ce e critic, dar cu 9 plăci pică %s. Plafonul de 9 plăci pe telefon (A6) se schimbă doar cu aprobarea dvs.", labels(g.Failed, false))
	case VerdictNOGOMobil:
		return fmt.Sprintf("Au picat %s. Regula A3: ambele aplicații mobile trec pe Flutter (T29).", labels(g.Failed, false))
	case VerdictIncomplete:
		return fmt.Sprintf("Lipsesc măsurătorile %s.", labels(g.Missing, g.ID == "G7"))
	case VerdictGO:
		if g.ID == "G6" {
			return "MV6 trece: 16 plăci și un flux principal încap în pragurile de CPU."
		}
		return "Perechile ICE din Chrome, Edge, Firefox și din cele două WebView-uri nu trec prin relay."
	case VerdictPlanBGrid9:
		return "MV6 pică: grila desktop coboară la 9 plăci (APP-30). Windows nu poate da NO-GO."
	case VerdictPlanBRelay:
		return fmt.Sprintf("Au picat %s: web-ul folosește relay în LAN și recomandă aplicația nativă (APP-32).", labels(g.Failed, true))
	}
	return ""
}

func (r Report) summary(d *doc) {
	d.text("## Rezumat pentru proprietar")
	d.blank()
	g5, g6, g7 := r.gate("G5"), r.gate("G6"), r.gate("G7")
	phone := "AND-1"
	if m := r.describe(platformAndroid, modelOf); m != "" {
		phone += ", " + m
	}
	d.printf("- **Android (%s):** **%s**. %s", phone, g5.Verdict, explain(g5))
	d.printf("- **Windows (WIN-1):** **%s**. %s", g6.Verdict, explain(g6))
	d.printf("- **Web (Chrome, Edge, Firefox):** **%s**. %s", g7.Verdict, explain(g7))
	d.printf("- **Argon2id (G19, MV15):** %s.", r.gate("G19").Result)
	d.printf("- **Codecuri (G9):** %s.", r.gate("G9").Result)
	if missing := r.unmeasured(); len(missing) > 0 {
		var parts []string
		for _, device := range r.devices {
			if codes := missing[device]; len(codes) > 0 {
				parts = append(parts, deviceName(device)+": "+strings.Join(codes, ", "))
			}
		}
		d.printf("- **Nemăsurat încă:** %s (motivele sunt în tabele).", strings.Join(parts, "; "))
	} else {
		d.text("- **Nemăsurat încă:** nimic din ce cere ghidul.")
	}
	if g5.Verdict == VerdictGOConditionat {
		d.text("- **De decis de dvs.:** plafonul de plăci pe telefon (A6, acum 9), vezi G5 mai jos.")
	}
	if keys := r.earlierKeys(); len(keys) > 0 {
		d.printf("- **Atenție:** pentru %s, o rulare anterioară a aceluiași scenariu a picat pragul; valoarea o dă cea mai nouă rulare, dar eșecul rămâne în tabele, lângă valoare (R40).", labels(keys, true))
	}
	if others := otherFailures(r.ev.MV); len(others) > 0 {
		keys := make([]string, len(others))
		for i, v := range others {
			keys[i] = mvKey(v.Device, v.Code)
		}
		d.printf("- **Praguri picate fără plan B scris:** %s; nu schimbă niciun verdict și se discută în PR.", labels(keys, true))
	}
	if flags := HubJitterFlags(r.results()); len(flags) > 0 {
		d.printf("- **Atenție:** rulări cu jitter al hub-ului peste 20 ms: %d (vezi Avertismente); cifrele lor pot fi mai proaste decât în realitate.", len(flags))
	}
	d.blank()
}

// earlierKeys are the thresholds that some earlier valid run failed (R40).
func (r Report) earlierKeys() []string {
	var keys []string
	for key, list := range r.ev.Earlier {
		if len(list) > 0 {
			keys = append(keys, key)
		}
	}
	return keys
}

// results are the results behind the report, in the order of the report.
func (r Report) results() []Result {
	out := make([]Result, len(r.ev.Runs))
	for i, run := range r.ev.Runs {
		out[i] = run.Result
	}
	return out
}

// unmeasured are, per device with results, the expected thresholds that have no value.
func (r Report) unmeasured() map[string][]string {
	out := map[string][]string{}
	for _, device := range r.devices {
		for _, code := range expectedCodes(device) {
			if _, ok := r.ev.MV[mvKey(device, code)]; !ok {
				out[device] = append(out[device], code)
			}
		}
	}
	return out
}

// distinct are the different non-empty values of a field over the runs of a device, sorted.
func (r Report) distinct(platform string, field func(Device) string) []string {
	seen := map[string]bool{}
	var values []string
	for _, run := range r.ev.Runs {
		if v := field(run.Device); run.Device.Platform == platform && v != "" && !seen[v] {
			seen[v] = true
			values = append(values, v)
		}
	}
	sort.Strings(values)
	return values
}

// describe joins the distinct values of a field over the runs of a device.
func (r Report) describe(platform string, field func(Device) string) string {
	return strings.Join(r.distinct(platform, field), " / ")
}

func modelOf(dev Device) string { return dev.Model }

// machineOf is the model with the OS: what tells two machines of one platform apart. A browser
// result carries no model, so Chrome on the phone and Chrome on the PC differ only in the OS.
func machineOf(dev Device) string {
	var parts []string
	for _, p := range []string{dev.Model, dev.OS} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

var deviceTitle = map[string]string{
	platformAndroid: "AND-1: Android (Capacitor)",
	platformWindows: "WIN-1: Windows (Tauri, WebView2)",
	"chrome":        "Web: Chrome",
	"edge":          "Web: Edge",
	"firefox":       "Web: Firefox",
}

var statusLabel = map[RunStatus]string{
	RunUsed:        "folosită",
	RunSuperseded:  "înlocuită de una mai nouă",
	RunInterrupted: "întreruptă, exclusă",
	RunFailed:      "eșuată, exclusă",
}

func (r Report) devicesSection(d *doc) {
	d.text("## Rezultate pe dispozitiv")
	d.blank()
	for _, device := range r.devices {
		title, ok := deviceTitle[device]
		if !ok {
			title = "Alt dispozitiv: " + device
		}
		d.text("### " + title)
		d.blank()
		var facts []string
		for _, field := range []func(Device) string{
			modelOf,
			func(dev Device) string { return dev.OS },
			func(dev Device) string { return dev.WebView },
		} {
			if v := r.describe(device, field); v != "" {
				facts = append(facts, v)
			}
		}
		if len(facts) == 0 {
			facts = append(facts, "necunoscut (rezultatele nu spun modelul)")
		}
		d.text("**Dispozitiv:** " + strings.Join(facts, " · "))
		switch device {
		case platformAndroid:
			d.text("De completat de proprietar: memoria RAM a telefonului (nu apare în rezultate).")
		case platformWindows:
			d.text("De completat de proprietar: GPU-ul și memoria RAM ale PC-ului (nu apar în rezultate; procesorul e modelul de mai sus).")
		}
		d.blank()
		r.table(d, device)
		r.runsTable(d, device)
	}
}

func (r Report) table(d *doc, device string) {
	codes := map[string]bool{}
	for _, code := range expectedCodes(device) {
		codes[code] = true
	}
	for _, v := range r.ev.MV {
		if v.Device == device {
			codes[v.Code] = true
		}
	}
	for _, list := range r.ev.Earlier { // a failure of an earlier run needs its row, whatever the newest run says
		for _, earlier := range list {
			if earlier.MV.Device == device {
				codes[earlier.MV.Code] = true
			}
		}
	}
	ordered := make([]string, 0, len(codes))
	for code := range codes {
		ordered = append(ordered, code)
	}
	sort.Slice(ordered, func(i, j int) bool { return codeLess(ordered[i], ordered[j]) })

	d.header("Cod", "Valoare", "Prag", "Trece")
	var notes []string
	for _, code := range ordered {
		earlier := r.earlierWarning(device, code)
		v, ok := r.ev.MV[mvKey(device, code)]
		if !ok {
			d.row(code, withWarning("nemăsurat: "+r.ev.reasonMissing(device, code), earlier), thresholdOf(code), "—")
			continue
		}
		pass := "Nu"
		if v.Pass {
			pass = "Da"
		}
		d.row(code, withWarning(v.cell(), earlier), v.Threshold, pass)
		for _, n := range v.Notes {
			notes = append(notes, fmt.Sprintf("- **%s:** %s", code, n))
		}
	}
	d.blank()
	if len(notes) > 0 {
		d.text("**Note la praguri:**")
		d.blank()
		for _, n := range notes {
			d.text(n)
		}
		d.blank()
	}
}

// earlierWarning is the text beside the value of a threshold that earlier valid runs of its
// scenario failed (R40), one warning for each such run, with the value that run had.
func (r Report) earlierWarning(device, code string) string {
	var warnings []string
	for _, earlier := range r.ev.Earlier[mvKey(device, code)] {
		warnings = append(warnings, fmt.Sprintf("**atenție:** o rulare anterioară a picat pragul (`%s`): %s", earlier.File, earlier.MV.cell()))
	}
	return strings.Join(warnings, "; ")
}

func withWarning(cell, warning string) string {
	if warning == "" {
		return cell
	}
	return cell + "; " + warning
}

func (r Report) runsTable(d *doc, device string) {
	d.text("**Rulări:**")
	d.blank()
	d.header("Fișier", "Scenariu", "Durată", "Stare", "Jitter hub (max)")
	var notes []string
	for _, run := range r.ev.Runs {
		if run.Device.Platform != device {
			continue
		}
		jitter := "—"
		if len(run.HubJitterMs) > 0 {
			jitter = number(run.hubJitterMax(), 1) + " ms"
			if run.hubJitterMax() > jitterFlagMs {
				jitter += ", peste 20 ms"
			}
		}
		d.row(run.File, run.Scenario, number(math.Round(run.DurationS), 0)+" s", statusLabel[run.Status], jitter)
		if run.Status != RunSuperseded {
			for _, n := range run.Notes {
				notes = append(notes, fmt.Sprintf("- %s (%s): %s", run.Scenario, run.File, n))
			}
		}
	}
	d.blank()
	if len(notes) > 0 {
		d.text("**Note din rulări**, așa cum le-a scris aplicația:")
		d.blank()
		for _, n := range notes {
			d.text(n)
		}
		d.blank()
	}
}

func (r Report) warnings(d *doc) {
	d.text("## Avertismente")
	d.blank()
	flags := HubJitterFlags(r.results())
	if len(flags) == 0 {
		d.text("- Nicio rulare nu a depășit 20 ms jitter al hub-ului.")
	} else {
		d.text("- Jitter al hub-ului: rulările de mai jos au avut o fereastră de 10 s cu p99 al abaterii de trimitere de peste 20 ms. PC-ul hub-ului poate să fi fost ocupat (lucru în paralel, antivirus), deci cifrele clientului din aceste rulări pot fi mai proaste decât în realitate; repetați-le cu PC-ul liber.")
		for _, f := range flags {
			d.printf("  - `%s` (%s): jitter maxim %s ms, peste 20 ms.", f.File, f.Scenario, number(f.MaxMs, 1))
		}
	}
	var interrupted, failed []string
	for _, run := range r.ev.Runs {
		switch run.Status {
		case RunInterrupted:
			interrupted = append(interrupted, run.File)
		case RunFailed:
			failed = append(failed, run.File)
		case RunUsed, RunSuperseded:
		}
	}
	if len(interrupted) > 0 {
		d.printf("- Rulări întrerupte: %d (pagina a fost ascunsă: ecranul s-a stins sau aplicația a trecut în fundal). Nu intră în praguri, iar aplicația oferă să le repetați: %s.", len(interrupted), codeList(interrupted))
	}
	if len(failed) > 0 {
		d.printf("- Rulări eșuate: %d (scenariul s-a oprit cu o eroare, deci citirile sunt incomplete). Nu intră în praguri: %s.", len(failed), codeList(failed))
	}
	for _, device := range r.devices {
		if machines := r.distinct(device, machineOf); len(machines) > 1 {
			d.printf("- Mai multe modele sau sisteme de operare sub platforma %s (%s): rezultatele lor sunt tratate ca un singur dispozitiv. Dacă sunt dispozitive diferite, puneți-le în dosare separate și rulați raportul pentru fiecare.", deviceName(device), strings.Join(machines, " / "))
		}
	}
	var noMDNS []string
	for _, run := range r.ev.Runs {
		if run.MDNSUnavailable && run.Scenario == "S10" {
			noMDNS = append(noMDNS, run.File)
		}
	}
	if len(noMDNS) > 0 {
		d.printf("- mDNS indisponibil pe hub în rulările S10 %s: hub-ul nu a putut rezolva candidații .local ai browserelor.", codeList(noMDNS))
	}
	d.blank()
}

func codeList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = "`" + s + "`"
	}
	return strings.Join(quoted, ", ")
}

func (r Report) decisions(d *doc) {
	d.text("## Verdicte și decizii declanșate")
	d.blank()
	d.header("Poartă", "Verdict sau rezultat")
	for _, g := range r.gates {
		d.row(g.ID+": "+g.Title, g.Result)
	}
	d.blank()
	for _, g := range r.gates {
		d.text("### " + g.ID + ": " + g.Title)
		d.blank()
		label := "Rezultat"
		if g.Verdict != "" {
			label = "Verdict"
		}
		d.text("- **" + label + ":** " + g.Result)
		d.text("- **Decizie declanșată:** " + g.Decision)
		d.text("- **Ce se schimbă în §2 sau în cerințe:** " + g.Change)
		d.blank()
	}
	if others := otherFailures(r.ev.MV); len(others) > 0 {
		d.text("### Alte praguri picate")
		d.blank()
		d.text("Nu intră în nicio regulă de verdict și nu au plan B scris în spec; le discutăm în PR.")
		d.blank()
		for _, v := range others {
			d.printf("- %s %s: %s (prag: %s)", deviceName(v.Device), v.Code, v.Text, v.Threshold)
		}
		d.blank()
	}
}

func (r Report) leftovers(d *doc) {
	d.text("## Rămas pentru Faza 0-bis")
	d.blank()
	d.text("Fiecare element pornește când aprobați bugetul pentru el (OPN-09, OPN-16; până atunci, 0 €) și are verdict înainte de faza care îl cere (spec §16.9).")
	d.blank()
	d.header("Element", "Poartă", "Cost estimat", "Termen")
	d.row("Verdictul iOS: MV1–MV11 și MV14 pe iOS-1 (iPhone real, iOS ≥ 17.1), smoke automat pe iOS-2 în CI", "G2, G3, G4",
		"un iPhone împrumutat (clasa iPhone 12 sau mai nouă); Apple Developer, 99 USD/an (OWN-05)", "înainte de Faza 4; recomandat cât mai devreme")
	d.row("Părțile iOS din G7, G8, G9 și G13: MV12 și MV13 în Safari și WKWebView, MV11 pe iOS-1, matricea de codecuri pe căile iOS", "G7, G8, G9, G13",
		"ca la rândul de mai sus", "odată cu verdictul iOS, înainte de Faza 4")
	d.row("MV11 (TURN-TLS/443 pe operator mobil), criticele remote (MV1 și MV2 la distanță) și partea din MV12 care cere Chrome cu origine publică", "G8 complet, G7 (MV12 în Chrome)",
		"VPS UE (OWN-11), Cloudflare TURN (OWN-12), domeniul pentru originea publică (OWN-04); pentru al doilea operator mobil, un SIM preplătit (≈ 5 € [estimare]) sau telefonul cuiva din familie", "înainte de Faza 2")
	d.row("AND-2: Android de intrare (Android 10, 3–4 GB RAM) pe Firebase Test Lab, MV3 și MV5 cu 4 plăci (informativ: fixează plafonul implicit)", "informativ, legat de A6",
		"OWN-17", "înainte de Faza 4")
	d.blank()
}

func (r Report) limits(d *doc) {
	d.text("## Limitări ale măsurătorilor")
	d.blank()
	d.text("- Jitterul hub-ului (R28) e p99 al abaterii de trimitere pe ferestre de 10 s; o rulare e marcată când o fereastră depășește 20 ms.")
	d.text("- Dintre rulările valide ale aceluiași scenariu pe același dispozitiv, valoarea o dă cea mai nouă (după ceasul dispozitivului). O rulare anterioară care a picat pragul nu dispare: e arătată lângă valoare, cu avertisment (R40), ca repetarea până la o trecere să nu ascundă un eșec. Rulările întrerupte și cele eșuate sunt listate, dar nu intră în praguri.")
	d.text("- MV2 se calculează cu offsetul dintre ceasul dispozitivului și al hub-ului (ping pe canalul de date), nu cu ceasul dispozitivului; mostrele exclud cadrele din GOP-ul păstrat în cache la legarea unei camere, care au vârsta cache-ului în loc de întârzierea transportului (R26).")
	d.text("- MV2 păstrează mostrele cu ceas nesigur (RTT > 50 ms sau offset mutat cu > 20 ms) și le raportează în notele pragului, fără să le excludă: eroarea offsetului e cel mult jumătate din RTT-ul ceasului.")
	d.text("- Specificația nu dă un percentil pentru MV10 (este un prag critic al AND-1), așa că MV10 și MV15 judecă fiecare citire: toate cele 20 de porniri (≤ 2 s) și toate cele 20 de salturi (≤ 1,5 s) la MV10, toate cele 10 rulări (≤ 3 s) la MV15; nicio pornire, salt sau rulare nu poate expira. MV1 folosește p90 din 20, cum cere specificația. O pornire, un salt sau o rulare care nu s-a terminat (MV1, MV10, MV15) se numără ca infinit de lent.")
	d.text("- MV5 judecă fps-ul pe media ultimelor 2 minute din `gridFps` (60 de ferestre de 2 s), nu pe toată rularea: o scădere din cauza căldurii se vede la sfârșit.")
	d.text("- MV5 calculează ritmul bateriei din prima și ultima citire, raportat la durata măsurată a grilei (ferestrele de 2 s din `gridFps`), nu la numărul de citiri: o citire a shell-ului care a expirat nu îl umflă. Procentul bateriei e întreg, deci rezoluția ritmului e de circa 6 %/h (1 % în circa 10 min): două puncte dau circa 12 %/h (trece), trei circa 18 %/h (pică), iar pragul de 15 %/h cade între aceste trepte.")
	d.text("- MV6 judecă media lui `cpuPctApp` (procesul aplicației și procesele WebView2, ca pondere din tot PC-ul), nu maximul și nu `cpuPctTotal`, care include și hub-ul de pe același PC; fluxul principal se judecă pe media lui `mainCpuPctApp`, de la prima rulare a grilei care l-a măsurat.")
	d.text("- MV8: specificația cere 5 min în PiP cu video care avansează; raportul cere 300 s în PiP, cel puțin un cadru și nicio pauză între cadre de peste 2 s. Pragul de 2 s pentru pauză e alegerea raportului, nu a specificației.")
	d.text("- Pe WIN-1, `memMb` (MV7) e memorie privată (private bytes, R37) a aplicației și a proceselor WebView2: nu include secțiunile partajate și memoria GPU, unde Chromium ține cadrele decodate, deci o scurgere acolo nu apare în MV7.")
	d.text("- MV14 (linii Kotlin pentru PiP și rețeaua locală) nu apare în rezultate: îl numără `tools/scripts/count-native-lines.sh` în CI.")
	d.blank()
}

func (r Report) rawData(d *doc) {
	d.text("## Date brute")
	d.blank()
	d.text("Câte un fișier CSV pe dispozitiv, lângă acest raport, cu o linie pe mostră (coloanele: `device,scenario,run,status,key,index,value`; `status` e `used`, `superseded`, `interrupted` sau `failed`; `hubJitterMs` e jitterul hub-ului al rulării):")
	d.blank()
	names := make([]string, 0, len(r.devices))
	for _, device := range r.devices {
		names = append(names, csvName(device))
	}
	sort.Strings(names)
	for _, name := range names {
		d.text("- `" + name + "`")
	}
	d.blank()
}

func (r Report) unreadable(d *doc) {
	if len(r.skipped) == 0 {
		return
	}
	d.text("## Fișiere necitite")
	d.blank()
	d.text("Aceste fișiere din dosarul de rezultate nu sunt rezultate utilizabile și nu intră în raport:")
	d.blank()
	for _, s := range r.skipped {
		d.printf("- `%s`: %s", s.File, s.Reason)
	}
	d.blank()
}
