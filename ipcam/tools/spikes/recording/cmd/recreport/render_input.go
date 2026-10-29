// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi3"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi4"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

func (r *report) input(b *strings.Builder) {
	b.WriteString("## Intrarea raportului\n\n")
	in := r.in
	fmt.Fprintf(b, "- Plicuri de rezultate judecate: %d, din %s și %s; dintre ele, rezultate AGE ale aplicației: %d.\n", len(in.envs),
		count(in.folders, "dosar de rezultate", "dosare de rezultate"), count(in.ciRuns, "rulare CI", "rulări CI"), in.ageRuns)
	if in.ageUnmeasured > 0 {
		fmt.Fprintf(b, "- Rezultate AGE fără măsurătoare (întrerupte sau fără debit), care nu intră în G13: %d.\n", in.ageUnmeasured)
	}
	if in.copies > 0 {
		fmt.Fprintf(b, "- Copii identice, citite o singură dată (G15): %d.\n", in.copies)
	}
	for _, s := range in.incomplete {
		fmt.Fprintf(b, "- Set de laborator incomplet (C32): %s; partea lui din G10 rămâne fără verdict.\n", missingParts(s))
	}
	for _, n := range in.renumbered {
		fmt.Fprintf(b, "- %s\n", renumberedText(n))
	}
	for _, c := range in.conflicts {
		from := make([]string, len(c.counted))
		for i, o := range c.counted {
			from[i] = count(o.runs, "rulare", "rulări") + " din " + originText(o.from)
		}
		fmt.Fprintf(b, "- Rulări MI1 cu rezultate diferite în intrare (de exemplu, din două rulări de laborator): `%s`: %s; G10 numără, din "+
			"fiecare, prima copie citită care contează, rulările CI fiind citite de la cea mai nouă: %s.\n", c.set, count(c.runs, "rulare", "rulări"),
			strings.Join(from, ", "))
	}
	if in.piProbe {
		fmt.Fprintf(b, "- Marcajul lui recspike-probe: Pi-ul dvs. este armv6 (`MI1 %s`), deci nu are măsurători; nu e o rulare, ci ce "+
			"mută părțile Pi-ului în Faza 0-bis (G31, G32).\n", results.PiProbeSet)
	}
	if len(in.unjudged) > 0 {
		fmt.Fprintf(b, "- Plicuri MI1 din seturi pe care G10 nu le judecă (rulările smoke din CI): %s.\n", counted(in.unjudged))
	}
	if len(in.skipped) > 0 {
		names := make([]string, len(in.skipped))
		for i, s := range in.skipped {
			names[i] = fmt.Sprintf("`%s` (%s)", s.file, s.reason)
		}
		fmt.Fprintf(b, "- Fișiere necitite: %s.\n", strings.Join(names, ", "))
	}
	if len(in.ci.other) > 0 {
		fmt.Fprintf(b, "- Alte notificări din CI, informative: %s.\n", counted(in.ci.other))
	}
	b.WriteString("\n")
}

// latestGate is the gate whose Judge takes, of a set, the run with the
// highest number, by the code and set of its envelopes: G12 each host's
// MI3 run, G13 the Pi's MI4 run (the PC's is information only).
var latestGate = map[string]string{
	"MI3 " + mi3.SetFor("pc"): "G12", "MI3 " + mi3.SetFor("pi"): "G12", "MI4 " + mi4.SetFor("pi"): "G13",
}

// worstRunNote is what a renumbering of MI2 means for G11, which judges a
// host's run with the most crashes, or of runs without any the longest.
const worstRunNote = " G11 nu depinde de ordine: judecă rularea cu cele mai multe crash-uri ale ipcam-hub sau, fără crash-uri, pe cea mai lungă."

// renumberedText says how a set's envelopes that shared a run id were
// renumbered (Task 10b): by their time, or, when some have none (named by
// their new run ids), in an uncertain order, which the owner checks by hand
// where a gate judges the highest number (Task 12 review, M1); for MI2,
// that G11 does not depend on the order.
func renumberedText(n renumbering) string {
	text := renumberedOrder(n)
	if n.code == "MI2" {
		text += worstRunNote
	}
	return text
}

// renumberedOrder is renumberedText without what MI2's order means.
func renumberedOrder(n renumbering) string {
	pairs := make([]string, len(n.from))
	for i := range n.from {
		pairs[i] = fmt.Sprintf("`%s` → `%s`", n.from[i], n.to[i])
	}
	text := fmt.Sprintf("Intrare renumerotată: în `%s %s`, plicuri diferite aveau același număr de rulare, din dosare sau rulări CI diferite; ",
		n.code, n.set)
	last := len(n.to) - 1
	gate, judged := latestGate[n.code+" "+n.set]
	if len(n.untimed) == 0 {
		text += "au fost renumerotate după momentul lor, de la cea mai veche la cea mai nouă: " + strings.Join(pairs, ", ") + "."
		if judged {
			text += fmt.Sprintf(" %s judecă rularea cu numărul cel mai mare, `%s`, cea mai nouă.", gate, n.to[last])
		}
		return text
	}
	untimed := make([]string, len(n.untimed))
	for i, id := range n.untimed {
		untimed[i] = "`" + id + "`"
	}
	without := strings.Join(untimed, ", ") + " nu spun când s-au făcut"
	if len(untimed) == 1 {
		without = untimed[0] + " nu spune când s-a făcut"
	}
	text += fmt.Sprintf("au fost renumerotate, dar ordinea lor e nesigură, fiindcă %s: %s.", without, strings.Join(pairs, ", "))
	if judged {
		text += fmt.Sprintf(" %s judecă rularea cu numărul cel mai mare, `%s` (fost `%s`), care poate să nu fie cea mai nouă: verificați de mână "+
			"care e cea mai nouă.", gate, n.to[last], n.from[last])
	}
	return text
}

// originText names where an envelope was read: "rularea CI `123`", or
// "dosarul de rezultate nr. 1", by its place among -in.
func originText(o origin) string {
	if o.ciRun != "" {
		return "rularea CI `" + o.ciRun + "`"
	}
	return fmt.Sprintf("dosarul de rezultate nr. %d", o.folder)
}

// counted lists names with their counts, sorted: "`a` (2), `b` (1)".
func counted(m map[string]int) string {
	names := slices.Sorted(maps.Keys(m))
	for i, name := range names {
		names[i] = fmt.Sprintf("`%s` (%d)", name, m[name])
	}
	return strings.Join(names, ", ")
}

// missingParts names a lab set's missing parts: "`set`: lipsește partea 7
// din 21".
func missingParts(s labSet) string {
	parts := make([]string, len(s.missing))
	for i, p := range s.missing {
		parts[i] = fmt.Sprint(p)
	}
	if len(parts) == 1 {
		return fmt.Sprintf("`%s`: lipsește partea %s din %d", s.set, parts[0], s.parts)
	}
	return fmt.Sprintf("`%s`: lipsesc părțile %s din %d", s.set, strings.Join(parts, ", "), s.parts)
}
