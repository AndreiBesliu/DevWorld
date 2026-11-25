// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi5"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi8"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The report's headings that tests and readers find it by.
const (
	headingSummary  = "## Rezumat pentru proprietar"
	headingInputs   = "## Ce a citit raportul"
	headingVerdicts = "## Verdicte și decizii declanșate (PH0-30)"
	headingMI5      = "## Rezultate MI5 (G14)"
	headingMI6      = "## Rezultate MI6 (G15)"
	headingMI8      = "## Rezultate MI8 (G17)"
	headingFaza0bis = "## Rămas pentru Faza 0-bis"
	headingLimits   = "## Limitări"
	headingRawData  = "## Date brute"
	tableJudged     = "### Rândurile judecății"
	tableSSD        = "### Pe SSD sau HDD"
	tableSD         = "### Pe card SD (P17)"
)

// The texts of the report that tests check.
const (
	msgConsoleUnitsMissing = "Unitățile de cotă din consola Google Cloud nu sunt încă în raport (-console-units)."
	noteD3C                = "MI8 rulează după detectorul din Faza 0 (a) (D3-C)"
	piProbeLimit           = "Pi armv6: proba pe Pi trece în Faza 0-bis"
)

// limitItems are the measurements' limits that «Limitări» states (P40,
// P17, P38, P09, P15, P14, P03, P19, P26, P41, P18, P34).
var limitItems = []string{
	"Canalul probei dintre aplicație și hub e HTTP simplu, prin uploadspike: nu e criptat și nu e autentificat. " +
		"Astfel, un dispozitiv din rețeaua de acasă ar putea citi adresa de autentificare și ar putea strecura un cod din alt cont, " +
		"cu aceeași stare (`state`). Canalul criptat din T11 (lanapi cu HIK, T42) previne asta. Până în Faza 1, această protecție " +
		"rămâne netestată (P40).",
	"Pe un Pi, MI5 înregistrează min(4, numărul recomandat pentru modelul lui) camere. Un Pi mai slab înregistrează deci mai " +
		"puține camere decât cele 4 din specificație (P17, P38).",
	"O rulare din laborator e varianta de 15 minute a TST-07, pe serviciile simulate. Ea nu decide niciodată G14 (P09).",
	"O regulă de firewall poate lăsa deschisă o conexiune deja stabilită. De aceea blocarea se verifică și din evenimentele de " +
		"încărcare ale hub-ului: nicio încărcare verificată în timpul blocării (P15).",
	"Clipurile pe evenimente (EVENT_CLIP) sunt simulate: camera `cam01` ține locul lor (P14).",
	"O copie verificată nu se înlocuiește niciodată. De aceea MI5 înregistrează necriptat (P03).",
	"În timpul blocării, numele serviciilor se rezolvau în continuare (DNS). Blocarea taie doar conexiunile (P15).",
	"Pe Windows, hub-ul sigilează secretele cu `dpapi-machine`. E mai slab decât cere specificația, până în Faza 1 (P19).",
	"Bucla de retenție din STO-13 se testează în Faza 1 (P26).",
	"Referința de mișcare e a probei, nu a camerei: o diferență de cadre (FFmpeg pe substream, sau instantanee ONVIF). Ea are " +
		"propria întârziere L, scrisă pe fiecare cameră, de care ține seama potrivirea (P41).",
	"La o continuare MI8 (`--continue`), două camere de aceeași marcă și de același model nu se pot deosebi (Task 10).",
	"Unitățile de cotă Drive vin din consola Google Cloud, citite de pe captura dvs. Proba numără doar apelurile (P18).",
	"Outbox-ul numără în memorie cele 3 încărcări cu hash greșit permise unei plasări. Astfel, fiecare repornire a hub-ului mai " +
		"permite unei plasări până la 2 încărcări greșite, și ștergerile lor (P34).",
}

// gate is one gate of the report: its judgement, made once by its Judge.
type gate struct {
	id, name, measure string
	j                 results.Judgement
	// keep is what stays when the gate passes.
	keep string
}

// title is gate's heading: "G14: Încărcarea în timpul unei pene (MI5)".
func (g gate) title() string { return fmt.Sprintf("%s: %s (%s)", g.id, g.name, g.measure) }

// report is the report of the spike's results.
type report struct {
	opts  options
	in    *inputs
	gates []gate
}

// newReport judges in's envelopes, each gate once by its Judge, for the
// report of opts.date. Lab envelopes and the pi-probe marker enter no
// Judge; without -onedrive free the OneDrive pairs are deferred (C2-R4);
// with -d3 c and no MI8 envelope G17 has D3-C's note.
func newReport(in *inputs, opts options) *report {
	r := &report{opts: opts, in: in}
	judged := r.judged()
	mi6Input := judged
	if opts.oneDrive != oneDriveFree {
		mi6Input = make([]results.Envelope, len(judged))
		for i, e := range judged {
			if e.MI6 != nil {
				d := *e.MI6
				d.OneDriveEnabled = false
				e.MI6 = &d
			}
			mi6Input[i] = e
		}
	}
	g15 := mi6.Judge(mi6Input)
	if opts.oneDrive == oneDriveUnknown {
		g15.Rows = slices.Clone(g15.Rows)
		for i, row := range g15.Rows {
			g15.Rows[i].Value = strings.Replace(row.Value, string(results.Deferred(mi6.OneDriveA5)),
				string(results.Deferred(oneDriveNoAnswer)), 1)
		}
	}
	g17 := mi8.Judge(judged)
	if opts.d3 == d3AfterPhase && !in.hasMI8() {
		g17 = results.Judgement{Gate: "G17", Verdict: results.Incomplete, Reasons: []string{noteD3C}, Rows: []results.Row{}, Decisions: []string{}}
	}
	r.gates = []gate{
		{id: "G14", name: "Încărcarea în timpul unei pene de internet", measure: "MI5", j: mi5.Judge(judged),
			keep: "outbox-ul și încărcarea rămân cele din §9.7–§9.9."},
		{id: "G15", name: "Conectarea hub-ului la cloud, partea din Faza 0", measure: "MI6", j: g15,
			keep: "autentificarea rămâne cea din §3.3 și §8.7."},
		{id: "G17", name: "Evenimentele ONVIF ale camerelor", measure: "MI8", j: g17,
			keep: "evenimentele vin prin ONVIF PullPoint (HUB-11)."},
	}
	return r
}

// oneDriveNoAnswer is the OneDrive pairs' deferral reason with
// -onedrive unknown: the owner has not answered A5 yet (C2-R4).
const oneDriveNoAnswer = "nu avem încă răspunsul verificării A5 (C2-R4)"

// judged are the envelopes that the Judges read: not the lab's, not the
// pi-probe marker and not one written with test seams.
func (r *report) judged() []results.Envelope {
	var out []results.Envelope
	for _, e := range r.in.envs {
		if judgedEnv(e) {
			out = append(out, e)
		}
	}
	return out
}

// fileName is the report's name in the output folder.
func (r *report) fileName() string { return r.opts.date + "-phase0c-upload-results.md" }

// files are what the run writes, by their paths in the output folder.
func (r *report) files() map[string]string {
	files := r.csvs()
	files[r.fileName()] = r.markdown()
	return files
}

// markdown is the report.
func (r *report) markdown() string {
	var b strings.Builder
	r.intro(&b)
	r.summary(&b)
	r.inputsRead(&b)
	r.verdicts(&b)
	r.mi5(&b)
	r.mi6(&b)
	r.mi8(&b)
	r.faza0bis(&b)
	r.limits(&b)
	rawData(&b)
	return b.String()
}

func (r *report) intro(b *strings.Builder) {
	b.WriteString("# Rezultatele Fazei 0 (c2): proba de încărcare și conturi\n\n")
	fmt.Fprintf(b, "Raport din %s, generat de `uploadreport` din rezultatele probei (PH0-30). Fiecare poartă e judecată o singură "+
		"dată, de regula ei din probă. Raportul arată verdictele, motivele lor și cifrele din spatele lor. Gazdele apar doar cu "+
		"pseudonime: `pc` (PC-ul dvs. cu Windows), `pi` (Raspberry Pi-ul dvs.), `lab-linux` și `lab-windows` (runner-ele CI). "+
		"Telefonul dvs. (Galaxy A54, AND-1) apare doar în perechile MI6. Camerele apar ca `real-1`, `real-2`…, conturile ca "+
		"`acct-1`, `acct-2`. Raportul se scrie doar dacă nu conține nimic de ascuns (PH0-22). Acesta e raportul probei c2; "+
		"tabelul Fazei 0 îl scrie ultimul plan al fazei (G34).\n\n", r.opts.date)
}

func (r *report) summary(b *strings.Builder) {
	b.WriteString(headingSummary + "\n\n")
	for _, g := range r.gates {
		text := string(g.j.Verdict)
		if g.j.Verdict != results.Pass && len(g.j.Reasons) > 0 {
			text += " — " + g.j.Reasons[0]
			if more := len(g.j.Reasons) - 1; more > 0 {
				text += fmt.Sprintf(" (și încă %s, mai jos)", count(more, "motiv", "motive"))
			}
		}
		fmt.Fprintf(b, "- **%s:** %s.\n", g.title(), strings.TrimSuffix(text, "."))
	}
	switch r.opts.oneDrive {
	case oneDriveFree:
		b.WriteString("- **OneDrive personal:** verificarea Microsoft de 2 minute (A5) a trecut. OneDrive personal se măsoară în Faza 0.\n")
	case oneDrivePaid:
		b.WriteString("- **OneDrive personal:** Microsoft cere un director plătit (A5). OneDrive personal trece în Faza 0-bis.\n")
	default:
		b.WriteString("- **OneDrive personal:** nu avem încă răspunsul verificării A5. OneDrive personal trece în Faza 0-bis (C2-R4).\n")
	}
	switch r.opts.d3 {
	case d3OnePer:
		b.WriteString("- **Camerele pentru MI8 (D3):** câte o cameră de fiecare marcă (D3-B).\n")
	case d3AfterPhase:
		b.WriteString("- **Camerele pentru MI8 (D3):** " + noteD3C + ".\n")
	default:
		b.WriteString("- **Camerele pentru MI8 (D3):** toate camerele cu evenimente ONVIF (D3-A).\n")
	}
	b.WriteString("\nMotivele fiecărui verdict sunt la „Verdicte și decizii declanșate”. Cifrele sunt la secțiunile MI5, MI6 și MI8.\n\n")
}

func (r *report) inputsRead(b *strings.Builder) {
	b.WriteString(headingInputs + "\n\n")
	fmt.Fprintf(b, "- Rezultate citite: %s, din %s și %s.\n", count(len(r.in.envs), "plic", "plicuri"),
		count(len(r.in.sourceFolders()), "folder", "foldere"), count(r.in.ciRuns, "rulare CI", "rulări CI"))
	apps := fmt.Sprintf("Rezultatele aplicației: %d", len(r.in.appResults))
	if len(r.in.appResults) > 0 {
		apps += " (" + strings.Join(r.in.appResults, ", ") + ")"
	}
	fmt.Fprintf(b, "- %s. Ele nu sunt plicuri și nu intră în verdicte.\n", apps)
	if len(r.in.dropped) > 0 {
		b.WriteString("- Copii ale aceleiași rulări, lăsate deoparte (rămâne copia salvată ultima):\n")
		for _, d := range r.in.dropped {
			fmt.Fprintf(b, "  - %s (păstrat: %s)\n", d.from, d.kept)
		}
	}
	if len(r.in.unreadable) > 0 {
		b.WriteString("- Fișiere necitite:\n")
		for _, s := range r.in.unreadable {
			fmt.Fprintf(b, "  - %s: %s\n", s.file, s.reason)
		}
	}
	var invalid []string
	for _, e := range r.in.envs {
		for _, run := range e.Runs {
			if !run.Valid {
				invalid = append(invalid, fmt.Sprintf("%s: %s", displaySource(e.Code+"/"+e.Set+"/"+run.RunID), cmp.Or(run.InvalidReason, "fără motiv")))
			}
		}
	}
	if len(invalid) > 0 {
		b.WriteString("- Rulări întrerupte, care nu se judecă (se repetă altă zi):\n")
		for _, l := range invalid {
			fmt.Fprintf(b, "  - %s\n", l)
		}
	}
	b.WriteString("\n")
}

// sourceFolders are the -in folders that gave a result, by their number.
func (in *inputs) sourceFolders() []string {
	var out []string
	add := func(from string) {
		if n, _, ok := strings.Cut(from, ": "); ok && strings.HasPrefix(n, "-in #") && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	for _, f := range slices.Sorted(maps.Values(in.from)) {
		add(f)
	}
	for _, d := range in.dropped {
		add(d.from)
	}
	return out
}

// verdictKind is the form of a verdict (P22).
type verdictKind int

const (
	kindPass verdictKind = iota
	kindFail
	kindIncomplete
	kindDeferred
)

func kindOf(v results.Verdict) verdictKind {
	switch {
	case v == results.Pass:
		return kindPass
	case strings.HasPrefix(string(v), string(results.FailWith(""))):
		return kindFail
	case strings.HasPrefix(string(v), string(results.Deferred(""))):
		return kindDeferred
	}
	return kindIncomplete
}

// decision is what g's verdict triggers and changes (PH0-30, PH0-31).
func (g gate) decision() (decision, changes string) {
	switch kindOf(g.j.Verdict) {
	case kindPass:
		return "Nicio decizie: " + g.keep, "Nimic în §2 sau în cerințe."
	case kindFail:
		return fmt.Sprintf("Planul B al lui %s: %s.", g.id, g.j.PlanB),
			fmt.Sprintf("§2.2: Decizie tehnică „%s”, cu trimitere la %s (PH0-31).", g.j.PlanB, g.id)
	case kindDeferred:
		return "Partea amânată trece în Faza 0-bis, cu termenul din tabelul de mai jos.", "Nimic în §2 până atunci."
	}
	return "Fără decizie până nu există toate măsurătorile; motivele sunt mai jos.", "Nimic până atunci."
}

func (r *report) verdicts(b *strings.Builder) {
	b.WriteString(headingVerdicts + "\n\n| Poartă | Verdict |\n| --- | --- |\n")
	for _, g := range r.gates {
		fmt.Fprintf(b, "| %s | %s |\n", cell(g.title()), cell(string(g.j.Verdict)))
	}
	b.WriteString("\n")
	for _, g := range r.gates {
		fmt.Fprintf(b, "### %s\n\n", g.title())
		decision, changes := g.decision()
		fmt.Fprintf(b, "- **Verdict:** %s\n- **Decizie declanșată:** %s\n- **Ce se schimbă în §2 sau în cerințe:** %s\n",
			g.j.Verdict, decision, changes)
		bullets(b, "Motive", g.j.Reasons)
		bullets(b, "Decizii (§2.2)", g.j.Decisions)
		b.WriteString("\n")
	}
}

// bullets writes items under a bold heading, none when there are none.
func bullets(b *strings.Builder, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n**%s:**\n\n", heading)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", item)
	}
}

// envOf is the envelope that a row's source names (<code>/<set>/<runId>),
// if any.
func (r *report) envOf(source string) (results.Envelope, bool) {
	parts := strings.Split(source, "/")
	if len(parts) != 3 {
		return results.Envelope{}, false
	}
	i := slices.IndexFunc(r.in.envs, func(e results.Envelope) bool {
		return e.Code == parts[0] && e.Set == parts[1] && e.RunID == parts[2]
	})
	if i < 0 {
		return results.Envelope{}, false
	}
	return r.in.envs[i], true
}

// gateRows writes a gate's rows: one table, or, when some come from an SD
// card, the SSD's and the SD card's apart (P17).
func (r *report) gateRows(b *strings.Builder, rows []results.Row) {
	var ssd, sd []results.Row
	for _, row := range rows {
		if e, ok := r.envOf(row.Source); ok && e.Storage == "sd" {
			sd = append(sd, row)
		} else {
			ssd = append(ssd, row)
		}
	}
	if len(sd) == 0 {
		b.WriteString(tableJudged + "\n\n")
		table(b, ssd)
		b.WriteString("\n")
		return
	}
	if len(ssd) > 0 {
		b.WriteString(tableSSD + "\n\n")
		table(b, ssd)
		b.WriteString("\n")
	}
	b.WriteString(tableSD + "\n\n")
	r.sdIntro(b, sd)
	table(b, sd)
	b.WriteString("\n")
}

// sdIntro names the runs on an SD card and the owner's confirmation of the
// card's warning (P02).
func (r *report) sdIntro(b *strings.Builder, rows []results.Row) {
	var runs, confirmations []string
	for _, row := range rows {
		e, _ := r.envOf(row.Source)
		if label := "`" + displaySource(row.Source) + "`"; !slices.Contains(runs, label) {
			runs = append(runs, label)
		}
		if c := e.SdConfirmation; c != nil {
			text := fmt.Sprintf("«%s», la %s", c.Text, time.UnixMilli(c.AtMs).UTC().Format("2006-01-02 15:04 UTC"))
			if !slices.Contains(confirmations, text) {
				confirmations = append(confirmations, text)
			}
		}
	}
	confirmed := "Avertismentul cardului SD nu are confirmarea dvs. în rezultate."
	if len(confirmations) > 0 {
		confirmed = "Ați confirmat avertismentul cardului SD: " + strings.Join(confirmations, "; ") + "."
	}
	fmt.Fprintf(b, "Rezultatele de pe card SD stau separat de celelalte. Rulările: %s. %s\n\n", strings.Join(runs, ", "), confirmed)
}

// table writes rows as Cod | Valoare | Prag | Trece | Sursă.
func table(b *strings.Builder, rows []results.Row) {
	b.WriteString("| Cod | Valoare | Prag | Trece | Sursă |\n| --- | --- | --- | --- | --- |\n")
	for _, row := range rows {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n", cell(row.Code), cell(row.Value), cell(row.Limit), passText(row.Pass),
			cell(displaySource(row.Source)))
	}
}

// passText is a row's Trece: ✓, ✗, or – for information or no verdict yet.
func passText(pass *bool) string {
	switch {
	case pass == nil:
		return "–"
	case *pass:
		return "✓"
	}
	return "✗"
}

// cell makes text one table cell: no line break, no bare "|".
func cell(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\n", " "), "|", "\\|")
}

// displayRunID is a run id as the report shows it, in lower case:
// uploadspike's ids end in a UTC time such as 20261124T101010Z, which the
// redaction scanner would take for a serial.
func displayRunID(id string) string { return strings.ToLower(id) }

// displaySource is a row's source with its run id in lower case.
func displaySource(source string) string {
	i := strings.LastIndexByte(source, '/')
	return source[:i+1] + displayRunID(source[i+1:])
}

// The quota units of the Drive methods that §9.2 prices (P18); the others'
// units come from the Cloud console.
var pricedUnits = map[string]float64{"files.get": 5, "files.list": 100, "files.get.media": 200}

// storageNames are the storages in Romanian.
var storageNames = map[string]string{"ssd": "SSD", "sd": "card SD", "hdd": "HDD", "unknown": "stocare necunoscută"}

func (r *report) mi5(b *strings.Builder) {
	b.WriteString(headingMI5 + "\n\n")
	b.WriteString("Hub-ul înregistrează și urcă, cu internetul tăiat doar lui timp de 6 ore. După deblocare, hub-ul e repornit în " +
		"mijlocul unei încărcări.\n\n")
	r.gateRows(b, r.gates[0].j.Rows)
	var owner, lab []results.Envelope
	for _, e := range r.in.envs {
		switch {
		case e.Code != "MI5" || e.MI5 == nil:
		case isLab(e):
			lab = append(lab, e)
		default:
			owner = append(owner, e)
		}
	}
	for _, e := range owner {
		mi5Details(b, e)
	}
	b.WriteString("### Unitățile de cotă Drive din consolă (P18)\n\n")
	if r.opts.consoleUnits >= 0 {
		fmt.Fprintf(b, "Din consola Google Cloud, citit de pe captura dvs.: %d unități pe segment.\n\n", r.opts.consoleUnits)
	} else {
		b.WriteString(msgConsoleUnitsMissing + "\n\n")
	}
	if len(lab) > 0 {
		b.WriteString("### Laborator: TST-07, varianta de 15 minute (P09)\n\n")
		b.WriteString("Rulările din laborator, pe serviciile simulate. Ele nu intră în G14.\n\n")
		var rows []results.Row
		for _, e := range lab {
			rows = append(rows, tst07Rows(e)...)
		}
		table(b, rows)
		b.WriteString("\n")
	}
}

// tst07Rows are a lab envelope's rows: one per destination, as TST-07's
// 15-min variant (P09).
func tst07Rows(e results.Envelope) []results.Row {
	var rows []results.Row
	for _, d := range e.MI5.Dests {
		row := results.Row{Code: "TST-07", Limit: "fiecare segment verificat; 0 lipsă, 0 duplicate (15 min, servicii simulate)",
			Source: "MI5/" + e.Set + "/" + e.RunID}
		if d.Deferred != "" {
			row.Value = d.Dest + ": " + d.Deferred
			rows = append(rows, row)
			continue
		}
		row.Value = fmt.Sprintf("%s: %d/%d verificate, %d lipsă, %d duplicate, %d hash-uri greșite; recuperare %s s",
			d.Dest, d.Verified, d.Segments, d.Missing, d.Duplicates, d.HashMismatches, num(e.MI5.RecoverySecs))
		ok := d.Segments > 0 && d.Verified == d.Segments && d.Missing == 0 && d.Duplicates == 0 && d.HashMismatches == 0
		row.Pass = &ok
		rows = append(rows, row)
	}
	return rows
}

// mi5Details writes an owner's MI5 run: the block, the recovery, the
// restart, the open-segment reads and the calls per segment.
func mi5Details(b *strings.Builder, e results.Envelope) {
	d := e.MI5
	fmt.Fprintf(b, "### `%s`, rularea `%s` (%s, %s)\n\n", e.Host, displayRunID(e.RunID), cmp.Or(storageNames[e.Storage], e.Storage),
		count(e.Cameras, "cameră", "camere"))
	blocked, open := true, true
	for _, c := range d.Reach {
		blocked = blocked && c.ProviderBlocked
		open = open && c.Lan && c.Loopback
	}
	fmt.Fprintf(b, "- Blocarea: %s h. Verificări ale blocării: %d; furnizorii blocați la toate: %s; LAN și loopback au răspuns: %s.\n",
		num(d.BlockSecs/3600), len(d.Reach), yesNo(blocked && len(d.Reach) > 0), yesNo(open && len(d.Reach) > 0))
	fmt.Fprintf(b, "- Numele serviciilor s-au rezolvat în timpul blocării (DNS): %s.\n", yesNo(d.DNSResolvedDuringBlock))
	fmt.Fprintf(b, "- Recuperarea: %s s după deblocare, la %s Mbps.\n", num(d.RecoverySecs), num(d.UplinkMbps))
	if rs := d.Restart; rs != nil {
		fmt.Fprintf(b, "- Repornirea: plasarea în curs avea %s MiB; ultimul offset confirmat %s MiB; reluată de la %s MiB. "+
			"Segmentul rupt de repornire a fost urcat: %s.\n", mib(rs.Size), mib(rs.LastConfirmedOffset), mib(rs.ResumedOffset),
			yesNo(rs.TornSegmentUploaded))
	} else {
		b.WriteString("- Repornirea: lipsește.\n")
	}
	for _, ro := range d.ReadOpen {
		kind := "necriptat"
		if ro.Encrypted {
			kind = "criptat"
		}
		line := fmt.Sprintf("- Citirea segmentului deschis (%s): %d/%d reușite, mediana %s ms, maxim %s ms", kind, ro.OK, ro.Calls,
			num(ro.P50Ms), num(ro.MaxMs))
		if ro.Retried > 0 {
			line += fmt.Sprintf("; %s după 5 s", count(ro.Retried, "citire repetată", "citiri repetate"))
		}
		for _, k := range slices.Sorted(maps.Keys(ro.Failures)) {
			line += fmt.Sprintf("; eșec „%s” × %d", k, ro.Failures[k])
		}
		b.WriteString(line + ".\n")
	}
	if d.InjectedGaps > 0 {
		fmt.Fprintf(b, "- Goluri de înregistrare cauzate de repornire (nu sunt segmente lipsă): %d.\n", d.InjectedGaps)
	}
	if d.HubCrashes > 0 {
		fmt.Fprintf(b, "- Hub-ul s-a oprit singur de %d ori. E un defect de cercetat.\n", d.HubCrashes)
	}
	for _, dest := range d.Dests {
		if n := len(dest.QuotaAfterDelete); n > 0 {
			q := dest.QuotaAfterDelete[n-1]
			fmt.Fprintf(b, "- Cota %s după ștergere (ultima citire): liber %s GiB, în coșul de reciclare %s GiB.\n", dest.Dest,
				num(float64(q.Free)/(1<<30)), num(float64(q.InBin)/(1<<30)))
		}
	}
	b.WriteString("\n")
	var rows []string
	for _, dest := range d.Dests {
		for _, method := range slices.Sorted(maps.Keys(dest.CallsPerSegment)) {
			rows = append(rows, fmt.Sprintf("| %s | %s | %s |", dest.Dest, method, callsText(dest.Kind, method, dest.CallsPerSegment[method])))
		}
	}
	if len(rows) > 0 {
		b.WriteString("Apelurile pe segment verificat, pe metodă. Unitățile sunt calculate doar pentru metodele cu preț în §9.2 " +
			"(get 5, list 100, download 200); celelalte vin din consolă.\n\n| Destinație | Metodă | Pe segment |\n| --- | --- | --- |\n")
		b.WriteString(strings.Join(rows, "\n") + "\n\n")
	}
}

// callsText is a method's calls per segment and, on Drive, its units:
// computed for a priced method, from the console for the others (P18).
func callsText(kind, method string, calls float64) string {
	text := num(calls) + " apeluri"
	if calls == 1 {
		text = "1 apel"
	}
	if kind != mi5.KindDrive {
		return text
	}
	if units, ok := pricedUnits[method]; ok {
		return text + ", " + num(calls*units) + " unități"
	}
	return text + ", unități: din consolă"
}

// The refresh outcomes in Romanian.
var outcomeNames = map[string]string{results.OutcomeOK: "ok", results.OutcomeFailed: "eșuat",
	results.OutcomeExpectedExpiry: "expirare așteptată", results.OutcomeBlockedByTest: "blocat de probă"}

func (r *report) mi6(b *strings.Builder) {
	b.WriteString(headingMI6 + "\n\n")
	b.WriteString("Hub-ul primește codul de autentificare de pe fiecare platformă și își reîmprospătează accesul zilnic. Proiectul " +
		"Google e în modul Testing, deci accesul expiră în ziua 7: acea zi nu se judecă.\n\n")
	r.gateRows(b, r.gates[1].j.Rows)
	pairs := map[string]results.Mi6Pair{}
	sources := map[string]string{}
	at := map[string]int64{}
	var cells []results.VisibilityCell
	var cellsAt int64 = -1
	var handoffs []results.Handoff
	for _, e := range r.judged() {
		if e.MI6 == nil {
			continue
		}
		for _, p := range e.MI6.Pairs {
			if prev, ok := at[p.Dest]; !ok || e.AtMs >= prev {
				pairs[p.Dest], at[p.Dest], sources[p.Dest] = p, e.AtMs, "MI6/"+e.Set+"/"+e.RunID
			}
		}
		if len(e.MI6.Visibility) > 0 && e.AtMs >= cellsAt {
			cells, cellsAt, handoffs = e.MI6.Visibility, e.AtMs, e.MI6.Handoffs
		}
	}
	if len(pairs) > 0 {
		b.WriteString("### Perechile\n\n| Pereche | Plan | Zilele 1–6 | Ziua 7 | Încercări fără răspuns | Sursă |\n" +
			"| --- | --- | --- | --- | --- | --- |\n")
		defs := slices.Clone(mi6.Drive)
		if r.opts.oneDrive == oneDriveFree {
			defs = append(defs, mi6.OneDrive...)
		}
		for _, def := range defs {
			p, ok := pairs[def.Dest]
			if !ok {
				continue
			}
			fmt.Fprintf(b, "| %s × %s | %s | %s | %s | %d | %s |\n", mi6.PlatformName(def.Platform), mi6.ProviderName(def.Provider),
				planText(p), daysText(p.Refresh, 1, 6), daysText(p.Refresh, 7, 7), p.Transient, displaySource(sources[def.Dest]))
		}
		b.WriteString("\n")
	}
	b.WriteString("### Vizibilitatea între clienți (3 × 4)\n\nUn fișier creat de fiecare tip de client al hub-ului (rânduri), " +
		"listat și citit de fiecare cititor (coloane). Celulele aceluiași client nu se judecă.\n\n" +
		"| Creator \\ Cititor | web | tv | android | desktop |\n| --- | --- | --- | --- | --- |\n")
	for _, creator := range []string{"android", "desktop", "tv"} {
		line := "| " + creator
		for _, reader := range []string{"web", "tv", "android", "desktop"} {
			line += " | " + cellText(cells, creator, reader)
		}
		b.WriteString(line + " |\n")
	}
	b.WriteString("\n")
	if len(handoffs) > 0 {
		b.WriteString("Autentificări predate hub-ului, pe cont și client: ")
		var parts []string
		for _, h := range handoffs {
			parts = append(parts, fmt.Sprintf("%s × %s: %d", h.Account, h.Client, h.Count))
		}
		b.WriteString(strings.Join(parts, "; ") + ".\n\n")
	}
}

// planText is a pair's plan as the table shows it.
func planText(p results.Mi6Pair) string {
	text := cmp.Or(p.Plan, mi6.PlanNone)
	if p.PlanAFailure != "" {
		text += " (planul A: " + p.PlanAFailure + ")"
	}
	if p.B1Failure != "" {
		text += " (B1: " + p.B1Failure + ")"
	}
	if p.RefreshFrom != "" {
		text += ", seria lui " + p.RefreshFrom
	}
	return text
}

// daysText is the outcome of the days from to to of a series: «toate ok»
// when every day of several is ok.
func daysText(series []results.RefreshDay, from, to int) string {
	var parts []string
	allOK := true
	for day := from; day <= to; day++ {
		outcome := ""
		for _, d := range series {
			if d.Day == day && (outcome == "" || d.Outcome == results.OutcomeOK) {
				outcome = d.Outcome
			}
		}
		allOK = allOK && outcome == results.OutcomeOK
		name := cmp.Or(outcomeNames[outcome], "–")
		if from == to {
			return name
		}
		parts = append(parts, fmt.Sprintf("%d: %s", day, name))
	}
	if allOK {
		return "toate ok"
	}
	return strings.Join(parts, ", ")
}

// cellText is a visibility cell as the matrix shows it.
func cellText(cells []results.VisibilityCell, creator, reader string) string {
	if creator == reader {
		return "același client"
	}
	i := slices.IndexFunc(cells, func(c results.VisibilityCell) bool { return c.Creator == creator && c.Reader == reader })
	switch {
	case i < 0:
		return "–"
	case cells[i].Listed && cells[i].Read:
		return "✓"
	}
	return fmt.Sprintf("✗ (listat: %s, citit: %s)", yesNo(cells[i].Listed), yesNo(cells[i].Read))
}

// referenceNames are the motion reference's methods in Romanian.
var referenceNames = map[string]string{results.ReferenceSubstream: "substream (FFmpeg)", results.ReferenceSnapshot: "instantanee ONVIF"}

func (r *report) mi8(b *strings.Builder) {
	b.WriteString(headingMI8 + "\n\n")
	b.WriteString("Hub-ul ascultă evenimentele de mișcare ale camerelor dvs. Proba le compară cu propria ei referință de mișcare, " +
		"o diferență de cadre.\n\n")
	r.gateRows(b, r.gates[2].j.Rows)
	type camera struct {
		c      results.Mi8Camera
		source string
	}
	brands := map[string][]camera{}
	var hours float64
	var gaps []int
	for _, e := range r.judged() {
		if e.Code != "MI8" || e.MI8 == nil {
			continue
		}
		hours += e.MI8.Hours
		if !slices.Contains(gaps, e.MI8.QuietGapSecs) {
			gaps = append(gaps, e.MI8.QuietGapSecs)
		}
		for _, c := range e.MI8.Cameras {
			brand := strings.TrimSpace(c.Brand)
			if brand == "" {
				brand = "marcă necunoscută"
			}
			brands[brand] = append(brands[brand], camera{c, "MI8/" + e.Set + "/" + e.RunID})
		}
	}
	if len(brands) == 0 {
		return
	}
	var gapText []string
	for _, g := range gaps {
		gapText = append(gapText, fmt.Sprintf("%d s", g))
	}
	fmt.Fprintf(b, "Ore observate: %.1f h. Un episod al referinței se încheie după %s fără mișcare. Un episod are eveniment "+
		"dacă o mișcare a camerei vine în cel mult 5 s de la începutul lui, ținând seama de întârzierea L a referinței.\n\n",
		hours, strings.Join(gapText, " / "))
	b.WriteString("### Pe marcă\n\n")
	for _, brand := range slices.SortedFunc(maps.Keys(brands), func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	}) {
		fmt.Fprintf(b, "#### %s\n\n| Cameră | Model | Episoade | Cu eveniment în 5 s | Acoperire | Pierderi tăcute | Referința | L | Sursă |\n"+
			"| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n", brand)
		cams := brands[brand]
		slices.SortStableFunc(cams, func(a, b camera) int { return strings.Compare(a.c.Camera, b.c.Camera) })
		for _, c := range cams {
			coverage := "–"
			if c.c.Episodes > 0 {
				coverage = fmt.Sprintf("%.1f %%", 100*c.c.Coverage)
			}
			if c.c.SubscribeFailed {
				coverage = "abonare eșuată (configurare)"
			}
			fmt.Fprintf(b, "| %s | %s | %d | %d | %s | %d | %s | %d ms | %s |\n", c.c.Camera, cell(c.c.Model), c.c.Episodes, c.c.Matched,
				coverage, c.c.SilentLosses, cmp.Or(referenceNames[c.c.ReferenceMethod], c.c.ReferenceMethod), c.c.RefLagMs, displaySource(c.source))
		}
		b.WriteString("\n")
	}
}

func (r *report) limits(b *strings.Builder) {
	b.WriteString(headingLimits + "\n\n")
	items := limitItems
	if r.in.piProbe {
		items = append(slices.Clone(items), piProbeLimit)
	}
	for _, l := range items {
		fmt.Fprintf(b, "- %s\n", l)
	}
	b.WriteString("\n")
}

// rawData lists the CSVs and their columns.
func rawData(b *strings.Builder) {
	b.WriteString(headingRawData + "\n\nCâte un fișier CSV pe măsurătoare, lângă acest raport, cu o linie pe mostră (PH0-30):\n\n")
	for _, c := range []struct {
		name, what string
		header     []string
	}{
		{csvMI5, "o linie pe destinația fiecărei rulări MI5, și pentru laborator", mi5Header},
		{csvMI6, "o linie pe ziua fiecărei perechi și una pe fiecare celulă de vizibilitate", mi6Header},
		{csvMI8, "o linie pe fiecare episod al referinței de mișcare", mi8Header},
	} {
		fmt.Fprintf(b, "- `%s`: %s; coloanele: `%s`.\n", c.name, c.what, strings.Join(c.header, ","))
	}
}

// num renders a number with at most two decimals.
func num(f float64) string {
	return strconv.FormatFloat(float64(int64(f*100+copySign(0.5, f)))/100, 'f', -1, 64)
}

func copySign(x, sign float64) float64 {
	if sign < 0 {
		return -x
	}
	return x
}

// mib renders bytes in MiB.
func mib(n int64) string { return num(float64(n) / (1 << 20)) }

func yesNo(b bool) string {
	if b {
		return "da"
	}
	return "nu"
}

// count renders n things in Romanian: "1 motiv", "3 motive", "21 de
// rulări".
func count(n int, one, many string) string {
	switch {
	case n == 1:
		return "1 " + one
	case n%100 >= 20 || (n >= 100 && n%100 == 0):
		return fmt.Sprintf("%d de %s", n, many)
	}
	return fmt.Sprintf("%d %s", n, many)
}
