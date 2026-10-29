// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"fmt"
	"strings"
)

// faza0bisRow is a row of what moves to Faza 0-bis: the element, its
// gates, its cost and its deadline, from §16.9.
type faza0bisRow struct{ element, gates, cost, deadline string }

// faza0bisRows are §16.9's rows that this spike leaves (G33): static, so
// that no hand edit goes into the generated report; Task 14 only confirms
// them against §16.9.
var faza0bisRows = []faza0bisRow{
	{"H1 (Pi 5) și H2 (N100): G10, G12 și G13 pe aceste gazde; până atunci, ≈ 8 și 16 camere sunt „estimare, nemăsurată”",
		"G10, G12, G13", "OWN-15 (opțional)", "fără termen"},
	{"`agerange` pe iOS-1 (MI4, punctul 3 din ENC-08)", "G13 (partea 3)",
		"un iPhone împrumutat; Apple Developer, 99 USD/an (OWN-05)", "înainte de Faza 4"},
}

// armv6Row is §16.9's row for an owner's Pi that is armv6, there only then.
var armv6Row = faza0bisRow{"Pi armv6: G10–G13 pe un Pi suportat", "G10–G13", "un Pi suportat (OWN-15)", "înainte de Faza 4"}

// armv6Basis is what an armv6 Pi leaves to the lab, and what it defers
// (G31, narrowed to what is measured): G10's Linux part has lab legs, the
// Linux parts of G11–G13 have none.
const armv6Basis = "Pi-ul dvs. este armv6, deci nesuportat (T39): partea Linux a lui G10 se bazează pe laborator (`lab-linux-kill9`, " +
	"`lab-linux-qemu-reset`) până la măsurarea unui Pi suportat, înainte de Faza 4; părțile Linux ale lui G11, G12 și G13 nu au rulări de " +
	"laborator, deci citesc „amânat în Faza 0-bis: Pi armv6” (G31)."

// faza0bis writes what moves to Faza 0-bis (§16.9, G31, G33).
func (r *report) faza0bis(b *strings.Builder) {
	b.WriteString("## Rămas pentru Faza 0-bis\n\n")
	b.WriteString("Fiecare element pornește când aprobați bugetul pentru el (OPN-09, OPN-16; până atunci, 0 €) și are verdict înainte de " +
		"faza care îl cere (§16.9). Tabelul e cel din §16.9, pentru ce lasă acest spike (G33).\n\n")
	b.WriteString("| Element | Poartă | Cost estimat | Termen |\n| --- | --- | --- | --- |\n")
	rows := faza0bisRows
	m := r.piModel()
	unsupported := m != nil && m.Known && !m.Supported
	if unsupported {
		rows = append(rows[:len(rows):len(rows)], armv6Row)
	}
	for _, row := range rows {
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n", row.element, row.gates, row.cost, row.deadline)
	}
	if unsupported {
		fmt.Fprintf(b, "\n%s\n", armv6Basis)
	}
	b.WriteString("\n")
}
