// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi2"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi3"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi4"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// gate is one gate of the report: its judgement, made once (C17), and
// what its decision says (PH0-30).
type gate struct {
	id, name, measure string
	j                 results.Judgement
	// keep is what stays when the gate passes.
	keep string
	// alsoChanges is what else a failure changes, after its §2.2 decision.
	alsoChanges string
}

// report is the report of a run of the spike: its inputs and the gates'
// judgements.
type report struct {
	date  string
	in    *inputs
	gates []gate
}

// newReport judges in's envelopes, each gate once by its Judge (C17), for
// the report of date.
func newReport(in *inputs, date string) *report {
	return &report{date: date, in: in, gates: []gate{
		{id: "G10", name: "Pană de curent", measure: "MI1", j: mi1.Judge(in.envs),
			keep: "recorderul rămâne MediaMTX cu patch-urile MTX-1…MTX-3 (§4.2)."},
		{id: "G11", name: "Independența înregistrării", measure: "MI2", j: mi2.Judge(in.envs),
			keep: "ingestul rămâne cel din §4.2."},
		{id: "G12", name: "Niveluri hardware", measure: "MI3", j: mi3.Judge(in.envs),
			keep:        "numerele recomandate din §4.8 rămân; PR-ul raportului scrie în §4.8 modelul Pi-ului dvs. și numărul lui (PH0-30).",
			alsoChanges: "§4.8: numerele recomandate măsurate"},
		{id: "G13", name: "`age` cu acces aleator", measure: "MI4", j: mi4.Judge(in.envs),
			keep: "redarea citește în continuare segmentele age cu acces aleator (§8.5); formatul nu se schimbă."},
		{id: "G18", name: "Redare prin canalul de date", measure: "MI9", j: mi9.Judge(in.envs),
			keep: "redarea rămâne prin canalul de date (CTR-12)."},
	}}
}

func (r *report) g10() results.Judgement { return r.gates[0].j }

func (r *report) g12() results.Judgement { return r.gates[2].j }

func (r *report) g13() results.Judgement { return r.gates[3].j }

// fileName is the report's name in the output folder.
func (r *report) fileName() string { return r.date + "-phase0c-recorder-results.md" }

// files are what the run writes: the report and the CSVs, by their paths
// in the output folder.
func (r *report) files() map[string]string {
	files := r.csvs()
	files[r.fileName()] = r.markdown()
	return files
}

// title is gate's heading: "G10: Pană de curent (MI1)".
func (g gate) title() string { return fmt.Sprintf("%s: %s (%s)", g.id, g.name, g.measure) }

// verdictText is a verdict as the report writes it (G27).
func verdictText(j results.Judgement) string {
	if j.Verdict == results.Fail {
		return string(results.Fail) + " → " + j.PlanB
	}
	return string(j.Verdict)
}

// markdown is the report.
func (r *report) markdown() string {
	var b strings.Builder
	r.intro(&b)
	r.summary(&b)
	r.decisions(&b)
	r.models(&b)
	r.hosts(&b)
	r.input(&b)
	r.mtx2(&b)
	finding(&b)
	r.faza0bis(&b)
	limits(&b)
	rawData(&b)
	return b.String()
}

func (r *report) intro(b *strings.Builder) {
	b.WriteString("# Rezultatele Fazei 0 (c1): spike-ul de înregistrare\n\n")
	fmt.Fprintf(b, "Raport din %s, generat de `recreport` din rezultatele spike-ului (PH0-30). Fiecare poartă e judecată o singură dată, "+
		"de regula ei din spike (C17); raportul arată verdictele, cu motivele lor, și cifrele din spatele lor. Gazdele apar doar cu pseudonime: "+
		"`pc` (PC-ul dvs. cu Windows), `pi` (Raspberry Pi-ul dvs.), iar `lab-linux`, `lab-windows` și `lab-qemu` sunt runner-ele CI. "+
		"Raportul se scrie doar dacă scanerul de redactare nu găsește nimic în el (PH0-22, G28); îl citiți înainte de commit. "+
		"Acesta e raportul spike-ului c1; raportul Fazei 0, cu tabelul G1–G19, îl scrie ultimul plan al Fazei 0 (G34).\n\n", r.date)
}

func (r *report) summary(b *strings.Builder) {
	b.WriteString("## Rezumat pentru proprietar\n\n")
	for _, g := range r.gates {
		text := verdictText(g.j)
		if g.j.Verdict != results.Pass && len(g.j.Reasons) > 0 {
			text += " — " + g.j.Reasons[0]
			if more := len(g.j.Reasons) - 1; more > 0 {
				text += fmt.Sprintf(" (și încă %s, mai jos)", count(more, "motiv", "motive"))
			}
		}
		fmt.Fprintf(b, "- **%s:** %s.\n", g.title(), text)
	}
	switch m := r.piModel(); {
	case m == nil:
	case m.Known && !m.Supported:
		b.WriteString("- **Pi-ul dvs.** este armv6, deci nesuportat: măsurătorile lui trec în Faza 0-bis (vezi mai jos).\n")
	default:
		fmt.Fprintf(b, "- **Pi-ul dvs.:** %s. %s Numărul rezultat e la „Modelul Pi-ului și numerele recomandate”.\n", m.Raw, modelText(m))
	}
	for _, s := range r.in.incomplete {
		fmt.Fprintf(b, "- **Set de laborator incomplet:** %s; G10 nu are verdict pentru el până la o nouă rulare din laborator.\n", missingParts(s))
	}
	if r.g10().O17OffOverLimit {
		fmt.Fprintf(b, "- **De propus în PR:** %s\n", o17Proposal)
	}
	b.WriteString("\nMotivele fiecărui verdict sunt la „Verdicte și decizii declanșate”; cifrele, pe gazdă, la „Rezultate pe gazdă”.\n\n")
}

// o17Proposal is what an O17-off loss over §4.5's bound calls for, which
// G10 signals (O17OffOverLimit): a proposal, not a decision (Task 14).
const o17Proposal = "rularea „Pierdere minimă la pană de curent” oprită a pierdut mai mult decât limita din §4.5; nu e o decizie declanșată " +
	"(planul B al lui G10 nu se aplică), iar PR-ul raportului propune o Decizie tehnică pentru limita sau pentru nota din §4.5, cu cifrele din notele lui G10."

func (r *report) decisions(b *strings.Builder) {
	b.WriteString("## Verdicte și decizii declanșate (PH0-30)\n\n| Poartă | Verdict |\n| --- | --- |\n")
	for _, g := range r.gates {
		fmt.Fprintf(b, "| %s | %s |\n", cell(g.title()), cell(verdictText(g.j)))
	}
	b.WriteString("\n")
	for _, g := range r.gates {
		fmt.Fprintf(b, "### %s\n\n", g.title())
		decision, changes := g.decision()
		fmt.Fprintf(b, "- **Verdict:** %s\n- **Decizie declanșată:** %s\n- **Ce se schimbă în §2 sau în cerințe:** %s\n", verdictText(g.j), decision, changes)
		if g.j.O17OffOverLimit {
			fmt.Fprintf(b, "- **De propus în PR:** %s\n", o17Proposal)
		}
		bullets(b, "Motive", g.j.Reasons)
		bullets(b, "Note (în afara verdictului)", g.j.Notes)
		b.WriteString("\n")
	}
}

// decision is what g's verdict triggers and changes (PH0-30, PH0-31).
func (g gate) decision() (decision, changes string) {
	switch g.j.Verdict {
	case results.Pass:
		return "Nicio decizie: " + g.keep, "Nimic în §2 sau în cerințe."
	case results.Fail:
		changes = fmt.Sprintf("§2.2: Decizie tehnică „%s”, cu trimitere la %s (PH0-31)", g.j.PlanB, g.id)
		if g.alsoChanges != "" {
			changes += "; " + g.alsoChanges
		}
		return fmt.Sprintf("Planul B al lui %s: %s.", g.id, g.j.PlanB), changes + "."
	case results.Incomplete:
		return "Fără decizie până nu există toate măsurătorile; motivele sunt mai jos.", "Nimic până atunci."
	}
	return "Partea Pi-ului trece în Faza 0-bis, cu termenul din tabelul de mai jos; până atunci nu se declanșează nicio decizie.",
		"Nimic în §2; se aplică rândul „Pi-ul proprietarului e armv6” din §16.9."
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

// The tables of "Rezultate pe gazdă", in order.
const (
	tablePC      = "### PC (`pc`, H3)"
	tablePi      = "### Raspberry Pi (`pi`)"
	tablePiSD    = "### Raspberry Pi (`pi`) pe card SD (G16)"
	tablePiMixed = "### Raspberry Pi (`pi`), pe mai multe stocări (G16)"
	tableLab     = "### Laborator și CI"
	tablePhone   = "### Telefonul (Galaxy A24, AND-1)"
	tableAll     = "### Pe ansamblu"
)

// tableOf is the table that shows row: its host's (Row.Host), else the one
// that its source names; a row over several hosts, or none, is in tableAll.
func tableOf(row results.Row) string {
	host := cmp.Or(row.Host, row.Source)
	switch host {
	case "pc":
		return tablePC
	case "pi":
		return tablePi
	case "lab", "ci", "lab-linux", "lab-windows", "lab-qemu", mi9.SourceRelay, mi9.SourceChrome, mi9.SourceLoopback:
		return tableLab
	case "telefon":
		return tablePhone
	}
	return tableAll
}

// storages are the storages of host's envelopes, sorted.
func (r *report) storages(host string) []string {
	var out []string
	for _, e := range r.in.envs {
		if e.Host == host && e.Storage != "" && !slices.Contains(out, e.Storage) {
			out = append(out, e.Storage)
		}
	}
	slices.Sort(out)
	return out
}

func (r *report) hosts(b *strings.Builder) {
	b.WriteString("## Rezultate pe gazdă\n\n")
	b.WriteString("Fiecare rând vine din judecata porții lui; „Sursa” spune unde s-a măsurat valoarea: `pc`, `pi`, `lab` (runner-ele CI), " +
		"`lan` (proba MI9 între dispozitivele dvs.), `ci-relay`, `ci-chrome`, `ci-loopback`, `ci` sau `telefon`. " +
		"„✓” trece, „✗” pică, „–” e informativ sau fără verdict încă.\n\n")
	tables := map[string][]results.Row{}
	for _, g := range r.gates {
		for _, row := range g.j.Rows {
			tables[tableOf(row)] = append(tables[tableOf(row)], row)
		}
	}
	piTable := tablePi
	switch s := r.storages("pi"); {
	case slices.Equal(s, []string{"sd"}):
		piTable = tablePiSD
	case slices.Contains(s, "sd"):
		piTable = tablePiMixed
	}
	for _, t := range []string{tablePC, tablePi, tableLab, tablePhone, tableAll} {
		rows := tables[t]
		if len(rows) == 0 {
			continue
		}
		heading := t
		if t == tablePi {
			heading = piTable
		}
		fmt.Fprintf(b, "%s\n\n", heading)
		switch {
		case t == tablePi && heading != tablePi:
			r.sdIntro(b, heading == tablePiMixed)
			rows = nextToResumes(rows, "pi")
		case t == tableLab:
			b.WriteString("Rulările din laborator (`lab-linux`, `lab-windows`, `lab-qemu`) și din CI, citite din adnotările rulărilor (C32).\n\n")
		case t == tableAll:
			b.WriteString("Rânduri care adună mai multe gazde, sau părți fără nicio măsurătoare.\n\n")
		}
		table(b, rows)
		if t == tablePi && heading != tablePi {
			fmt.Fprintf(b, "\n%s\n", sdResumeNote)
		}
		b.WriteString("\n")
	}
	tableNotes(b, tables)
}

// sdResumeNote is what an SD card means for G11's resumes (Task 8, 7.2d).
const sdResumeNote = "Reluările de după kill -9 (MI2) stau lângă stările cardului: pe un disc încetinit, care ține locul unui card SD sub " +
	"încărcare, laboratorul spike-ului a măsurat reluări de 13–37 s, din fsync-urile reconcilierii și ale stării rulării. Limita de 10 s a " +
	"lui G11 poate pica din latența fsync a cardului, pe care planul B al lui G11 (ingestul inversat) nu o schimbă."

// storageNames are the storages other than an SD card in the report's
// Romanian (G16).
var storageNames = map[string]string{"ssd": "SSD", "hdd": "HDD", "unknown": "o stocare necunoscută"}

// sdIntro says which of the Pi's sets were on an SD card, and the owner's
// confirmation of the card's warning (G16, T40). On mixed storage, the
// judges' rows being per host, one table holds every storage: the intro
// says so and names the sets on each.
func (r *report) sdIntro(b *strings.Builder, mixed bool) {
	sets := map[string][]string{}
	var confirmations []string
	for _, e := range r.in.envs {
		if e.Host != "pi" {
			continue
		}
		storage := cmp.Or(e.Storage, "unknown")
		if label := "`" + e.Code + " " + e.Set + "`"; !slices.Contains(sets[storage], label) {
			sets[storage] = append(sets[storage], label)
		}
		if c := e.SdConfirmation; c != nil && storage == "sd" {
			text := fmt.Sprintf("«%s», la %s", c.Text, time.UnixMilli(c.AtMs).UTC().Format("2006-01-02 15:04 UTC"))
			if !slices.Contains(confirmations, text) {
				confirmations = append(confirmations, text)
			}
		}
	}
	for _, s := range sets {
		slices.Sort(s)
	}
	if !mixed {
		confirmed := "fără confirmarea proprietarului în rezultate"
		if len(confirmations) > 0 {
			confirmed = "avertismentul cardului SD confirmat de proprietar: " + strings.Join(confirmations, "; ")
		}
		fmt.Fprintf(b, "Rezultatele Pi-ului de pe card SD, separat de celelalte (G16): %s. Seturile de pe card SD: %s.\n\n", confirmed, strings.Join(sets["sd"], ", "))
		return
	}
	confirmed := "Avertismentul cardului SD nu are confirmarea proprietarului în rezultate."
	if len(confirmations) > 0 {
		confirmed = "Avertismentul cardului SD e confirmat de proprietar: " + strings.Join(confirmations, "; ") + "."
	}
	var others []string
	for _, storage := range slices.Sorted(maps.Keys(sets)) {
		if storage != "sd" {
			others = append(others, fmt.Sprintf("Pe %s: %s.", cmp.Or(storageNames[storage], storage), strings.Join(sets[storage], ", ")))
		}
	}
	fmt.Fprintf(b, "Pi-ul are rezultate pe mai multe stocări (G16). Judecata dă rândurile pe gazdă, nu pe stocare, deci tabelul de mai jos le "+
		"cuprinde pe toate: citiți fiecare rând după setul lui. Seturile de pe card SD: %s. %s %s\n\n",
		strings.Join(sets["sd"], ", "), strings.Join(others, " "), confirmed)
}

// nextToResumes moves host's card row to right after its resume row.
func nextToResumes(rows []results.Row, host string) []results.Row {
	card := slices.IndexFunc(rows, func(r results.Row) bool { return r.Code == mi2.CardRowCode(host) })
	if card < 0 {
		return rows
	}
	row := rows[card]
	rows = slices.Delete(slices.Clone(rows), card, card+1)
	if resume := slices.IndexFunc(rows, func(r results.Row) bool { return r.Code == mi2.ResumeRowCode(host) }); resume >= 0 {
		return slices.Insert(rows, resume+1, row)
	}
	return append(rows, row)
}

// table writes rows as Cod | Valoare | Prag | Trece | Sursa.
func table(b *strings.Builder, rows []results.Row) {
	b.WriteString("| Cod | Valoare | Prag | Trece | Sursa |\n| --- | --- | --- | --- | --- |\n")
	for _, row := range rows {
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n", cell(row.Code), cell(row.Value), cell(row.Limit), passText(row.Pass), cell(row.Source))
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

// o17Note says what the O17-off rows are (O17, C1-R1, C1-R13).
var o17Note = "Rândurile „" + mi1.O17OffLabel + "” sunt rularea cu setarea oprită din fiecare set automat. Limita lor e cea din §4.5: o parte " +
	"plus 64 KiB ÷ bitrate, rotunjit în sus la părți întregi, adică 4 s (4.000 ms la 256 kbps). Pierderea fluxului de 256 kbps din această " +
	"rulare nu contează în G10 (O17, C1-R13): peste limită, apare doar aici și în notele lui G10, fără planul B; orice altă eroare a rulării " +
	"contează ca la o rulare obișnuită."

// tableNotes explains what the tables show beyond their rows.
func tableNotes(b *strings.Builder, tables map[string][]results.Row) {
	var notes []string
	all := slices.Concat(tables[tablePC], tables[tablePi], tables[tableLab], tables[tablePhone], tables[tableAll])
	if slices.ContainsFunc(all, func(row results.Row) bool { return strings.HasSuffix(row.Code, mi1.O17OffLabel) }) {
		notes = append(notes, o17Note)
	}
	if slices.ContainsFunc(all, func(row results.Row) bool {
		return strings.HasPrefix(row.Code, "MI9") && strings.Contains(row.Value, "≥")
	}) {
		notes = append(notes, "La MI9, „≥” marchează saltul pe care sfârșitul probei l-a întrerupt: timpul lui e o limită inferioară, "+
			"care pică pragul (G27).")
	}
	if len(notes) == 0 {
		return
	}
	b.WriteString("**Note la tabele:**\n\n")
	for _, n := range notes {
		fmt.Fprintf(b, "- %s\n", n)
	}
	b.WriteString("\n")
}

func (r *report) mtx2(b *strings.Builder) {
	if len(r.in.ci.mtx2) == 0 {
		return
	}
	b.WriteString("## Pentru decizia R01: pierderea MTX-2 (mtx2-loss)\n\n")
	b.WriteString("Testul TestMTX2_WorstCaseLoss din CI taie fluxuri criptate la momente aleatoare și măsoară cât se pierde și cât disc " +
		"în plus ocupă criptarea, cu umplutura de la bitrate mic pornită și oprită (O17). Cifrele sprijină Decizia tehnică R01 din PR-ul " +
		"raportului: reconcilierea păstrează eșantioanele întregi ale ultimei părți parțial scrise.\n\n")
	for _, m := range r.in.ci.mtx2 {
		pass := "da"
		if !m.Pass {
			pass = "nu"
		}
		fmt.Fprintf(b, "%s; testul trece: %s.\n\n| kbps | Umplutura (O17) | Mediana | Maximul | Disc în plus |\n| --- | --- | --- | --- | --- |\n",
			count(m.Cuts, "tăietură", "tăieturi"), pass)
		for _, run := range m.Runs {
			pad := "oprită"
			if run.Pad {
				pad = "pornită"
			}
			fmt.Fprintf(b, "| %d | %s | %s | %s | %s %% |\n", run.Kbps, pad, mi1.SecondsText(run.MedianMs), mi1.SecondsText(run.MaxMs),
				results.Decimal(run.OverheadPct, 1))
		}
		b.WriteString("\n")
	}
}

// finding is §7.6's finding of MI9 (Task 11), for Faza 1's playback.
func finding(b *strings.Builder) {
	b.WriteString("## Constatare pentru Faza 1: cine închide canalul bulk (§7.6)\n\n")
	b.WriteString("În proba MI9 din Chrome, când hub-ul închidea canalul `bulk/<id>` imediat după ultimul lui octet, Chrome pierdea uneori " +
		"octeții canalului următor, deschis cât închiderea era în curs (1 rulare din 7, local, cu Chrome 155). De aceea, în spike, clientul " +
		"închide canalul după ce a primit tot, iar hub-ul îl închide singur doar după 10 s. E o informație pentru proiectarea redării din " +
		"Faza 1 (§7.6), nu un verdict.\n\n")
}

// limits are the measurements' limits that the report states.
func limits(b *strings.Builder) {
	b.WriteString("## Limitări ale măsurătorilor\n\n")
	for _, l := range []string{
		"Pasul de restream al hubd (`live`, go2rtc) nu e măsurat: controlorul servește camerele simulate direct prin RTSP, iar MediaMTX le " +
			"citește cu serverele lui oprite; MI1 nu include bufferul din memorie al restream-ului (milisecunde), iar MI3 nici CPU-ul lui (C06).",
		"Nu s-a înregistrat nicio cameră reală: c1 înregistrează doar camere simulate, deci comportamentul la înregistrarea unei camere " +
			"reale (VBR, GOP neregulat) nu e acoperit (C28).",
		"SCTP-ul din Android WebView nu e măsurat: canalul de date se măsoară în Go, pe LAN și prin TURN-TLS/443, și în Chrome, în CI (G26).",
		"Încărcările MI3 sunt proxy-uri ale funcțiilor hub-ului (live, redare, export, upload); notele lui G12 spun ce ține locul " +
			"fiecăreia (G20).",
		"Repornirea ipcam-hub făcută de recspike e o limită inferioară pentru recuperarea prin SCM (Windows) și systemd (Linux) din Faza 1 (C07).",
		"„Primul cadru” (MI4 și MI9) e timpul de la cererea de salt până la sosirea octeților primului eșantion IDR de la țintă sau de după " +
			"ea, pe calea de citire a hub-ului; în Chrome se adaugă primul cadru afișat (requestVideoFrameCallback) (G21, G26).",
		"Rândul de pornire la rece e măsurat pe rulările MI1 cu repornire (pc-bugcheck pe PC, pi-sysrq pe Pi), cu camera lor, una singură " +
			"(cam01_main), și include pornirea sistemului de operare.",
	} {
		fmt.Fprintf(b, "- %s\n", l)
	}
	b.WriteString("\n")
}

// rawData lists the CSVs and their columns.
func rawData(b *strings.Builder) {
	b.WriteString("## Date brute\n\nCâte un fișier CSV pe măsurătoare, lângă acest raport, cu o linie pe mostră (PH0-30):\n\n")
	fmt.Fprintf(b, "- `%s`: o linie pe fluxul fiecărei rulări, și pentru rulările care nu contează, cu motivul lor (`invalidReason`); "+
		"coloanele: `%s`.\n", csvMI1, strings.Join(mi1Header, ","))
	for _, c := range []struct{ name, what string }{
		{csvMI2, "proba de 72 h a fiecărei gazde, cu stările cardului SD"},
		{csvMI3, "prima oră, bugetele, încărcările, pașii și ora la 16 camere"},
		{csvMI4, "fiecare citire și salt, criptarea, rulările AGE ale aplicației și verificarea ENC-06"},
	} {
		fmt.Fprintf(b, "- `%s`: %s; coloanele: `%s`.\n", c.name, c.what, strings.Join(longHeader, ","))
	}
	fmt.Fprintf(b, "- `%s`: debitul, fiecare salt și fiecare cadru afișat al fiecărui segment al probei; `bound` e „≥” la saltul întrerupt de "+
		"sfârșitul probei, o limită inferioară; coloanele: `%s`.\n", csvMI9, strings.Join(mi9Header, ","))
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
