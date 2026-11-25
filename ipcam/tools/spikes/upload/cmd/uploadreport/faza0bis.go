// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"fmt"
	"strings"
)

// faza0bisRows are §16.9's rows that this spike leaves (P23, P28, P55),
// each its gate, cost and deadline: static, so that no hand edit goes into
// the generated report. Row 2 (index 1) is left out with -onedrive free.
var faza0bisRows = []string{
	"G14 – OneDrive business (pana de 6 h), cu tenantul MI7: Entra ID P1, OWN-09, înainte de Faza 3",
	"G14 și G15 – OneDrive personal: înregistrarea Microsoft cu date de plată, OWN-08, înainte de Faza 3",
	"G15 – combinațiile iOS: Apple Developer (OWN-05, 99 USD/an) și un iPhone împrumutat, înainte de Faza 3",
	"G15 – refresh-ul Drive din ziua 7 și de după, cu proiectul în Production: domeniul pentru verificarea brandului (OWN-04, OWN-07), înainte de Faza 3",
	"G15 – OneDrive business: tenantul de test (OWN-09), înainte de Faza 3",
	"G16 (MI7) – Conditional Access și `AppFolder` pe conturi de firmă: Entra ID P1 (OWN-09, ≈ 6–7 USD/lună), înainte de Faza 3",
	"ipcam-lab: TST-07 de 6 h pe conturi reale (PH3-02) și stabilitatea de 72 h, OWN-22 ≈ 15 €/lună, înainte de Faza 3",
	"redirect-ul OneDrive de pe `<domeniu>`, după domeniu (OWN-04), înainte de Faza 3",
	"STO-01 nightly pe conturi reale (§14.5): Drive după trecerea proiectului în Production (OWN-07, înainte de Faza 3); " +
		"OneDrive personal e gratuit dacă înregistrarea e gratuită (OWN-08) și se programează după rotația săptămânală a " +
		"secretelor (OWN-23, Faza 1); OneDrive business (OWN-09), B2, R2, Wasabi și AWS SSE-KMS: Faza 0-bis, cost, înainte de Faza 3",
}

// oneDrivePersonalRow is the index of the row that A5's pass removes.
const oneDrivePersonalRow = 1

// faza0bis writes what moves to Faza 0-bis (§16.9).
func (r *report) faza0bis(b *strings.Builder) {
	b.WriteString(headingFaza0bis + "\n\n")
	b.WriteString("Fiecare element pornește când aprobați bugetul pentru el. Până atunci costă 0 €. Fiecare are verdict înainte " +
		"de faza care îl cere (§16.9).\n\n| Nr. | Poartă, cost și termen |\n| --- | --- |\n")
	for i, row := range faza0bisRows {
		if i == oneDrivePersonalRow && r.opts.oneDrive == oneDriveFree {
			continue
		}
		fmt.Fprintf(b, "| %d | %s |\n", i+1, row)
	}
	b.WriteString("\n")
}
