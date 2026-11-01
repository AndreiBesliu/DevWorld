// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi3"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// piModel is the owner's Pi as its envelopes name it, nil without any.
func (r *report) piModel() *results.PiModelInfo {
	for _, e := range r.in.envs {
		if e.Host == "pi" && e.PiModel != nil {
			return e.PiModel
		}
	}
	return nil
}

// has reports whether an envelope of code exists on host.
func (r *report) has(code, host string) bool {
	return slices.ContainsFunc(r.in.envs, func(e results.Envelope) bool { return e.Code == code && e.Host == host })
}

// measuredOn says whether host's G12 and G13 were measured, by MI3.
func (r *report) measuredOn(host, whose string) string {
	if !r.has("MI3", host) {
		return "lipsește proba MI3"
	}
	return "măsurat pe " + whose + " (G12, G13)"
}

func (r *report) models(b *strings.Builder) {
	b.WriteString("## Modelul Pi-ului și numerele recomandate (§4.8)\n\n")
	m := r.piModel()
	if m == nil {
		b.WriteString("Niciun rezultat nu numește modelul Pi-ului dvs.\n\n")
	} else {
		fmt.Fprintf(b, "Pi-ul dvs.: %s (citit din %s). %s\n\n", m.Raw, cmp.Or(m.Source, "cpuinfo"), modelText(m))
	}
	b.WriteString("| Nivel | Camere recomandate (§4.8) | Rezultat | Măsurat |\n| --- | --- | --- | --- |\n")
	fmt.Fprintf(b, "| H3: PC-ul dvs. | 8 (16 cu ≥ 6 nuclee) | %s | %s |\n", r.resultText("pc", h3Cameras), r.measuredOn("pc", "PC-ul dvs."))
	h1, h1Result := "estimare, nemăsurată", "—"
	switch {
	case m == nil:
	case m.Known && !m.Supported:
		fmt.Fprintf(b, "| nesuportat: Pi-ul dvs. | — | — | %s |\n", results.Deferred("Pi armv6"))
	case m.Tier == mi3.TierH1:
		h1, h1Result = r.measuredOn("pi", "Pi-ul dvs."), r.resultText("pi", m.RecommendedMax)
	default:
		fmt.Fprintf(b, "| %s: Pi-ul dvs. | %s | %s | %s |\n", cmp.Or(m.Tier, "necunoscut"), recommendedText(m), r.resultText("pi", m.RecommendedMax),
			r.measuredOn("pi", "Pi-ul dvs."))
	}
	fmt.Fprintf(b, "| H1 (Pi 5) | ≈ 8 | %s | %s |\n| H2 (N100) | 16 | — | estimare, nemăsurată |\n\n", h1Result, h1)
	fmt.Fprintf(b, "Numerele rezultate: G12 — %s; G13 — %s. Un plan B al lor coboară numărul recomandat al gazdei (minimum 1, O16), "+
		"iar PR-ul raportului scrie numerele măsurate în §4.8. H1 și H2 se măsoară doar în Faza 0-bis, dacă doriți (G31).\n\n",
		verdictText(r.g12()), verdictText(r.g13()))
}

// h3Cameras is H3's recommended number (§4.8), the PC's starting number.
const h3Cameras = 8

// resultText is host's recommended number after G12 and G13 (§4.8), from
// start: the lower of the numbers that their judgements leave
// (Recommended), named by the gates that lowered it below start. Without
// G12's number, as while its steps are open, it is not settled, and G13's
// number below start, when there is one, caps it.
func (r *report) resultText(host string, start int) string {
	g12, ok := r.g12().Recommended[host]
	g13, ok13 := r.g13().Recommended[host]
	if !ok {
		if ok13 && g13 < start {
			return "nestabilit (G12); cel mult " + count(g13, "cameră", "camere") + " (G13)"
		}
		return "nestabilit (G12)"
	}
	n := g12
	if ok13 {
		n = min(n, g13)
	}
	var by []string
	if n < start && g12 == n {
		by = append(by, "G12")
	}
	if n < start && ok13 && g13 == n {
		by = append(by, "G13")
	}
	text := count(n, "cameră", "camere")
	if len(by) > 0 {
		text += " (" + strings.Join(by, ", ") + ")"
	}
	return text
}

// modelText says what §4.8 makes of the Pi's model.
func modelText(m *results.PiModelInfo) string {
	switch {
	case m.Known && !m.Supported:
		return "Modelul e armv6, nesuportat (T39): Pi-ul servește doar ca martor, iar măsurătorile lui trec în Faza 0-bis."
	case !m.Known:
		return fmt.Sprintf("Modelul nu e în tabelul din §4.8; numărul recomandat de pornire e %s.", recommendedText(m))
	}
	return fmt.Sprintf("Nivelul %s din §4.8, cu %s.", m.Tier, recommendedText(m))
}

// recommendedText is the Pi model's recommended number: "4 camere",
// "1–2 camere", marked when the owner typed it (C24).
func recommendedText(m *results.PiModelInfo) string {
	text := count(m.RecommendedMax, "cameră", "camere")
	if m.RecommendedMin != m.RecommendedMax {
		text = fmt.Sprintf("%d–%d camere", m.RecommendedMin, m.RecommendedMax)
	}
	if m.Estimate {
		text += " (estimare introdusă de dvs., C24)"
	}
	return text
}
