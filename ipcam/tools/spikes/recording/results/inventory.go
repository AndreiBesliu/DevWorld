// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package results

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ageResult names the app's AGE result (Task 10), which reccontroller keeps
// under app/ as the app sent it, not as an envelope.
const ageResult = "AGE"

// expected is one result that the final collection must hold (G15), and
// the guide step that produces it.
type expected struct {
	label, host string
	step        int
	match       func(Envelope) bool
}

// The guide's steps that produce each result (spike-recording.md).
const (
	stepAutomated = 6
	stepReal      = 7
	stepMI2       = 8
	stepMI3       = 9
	stepMI4       = 10
	stepMI9       = 11
)

// expectations are the six MI1 sets, MI2, MI3 and MI9 lan per device, and
// MI4 on the Pi, the PC's first; the app's AGE result is checked apart.
var expectations = []expected{
	mi1Set("pc-kill9", "pc", stepAutomated), mi1Set("pc-bugcheck", "pc", stepAutomated), mi1Set("real-pc", "pc", stepReal),
	onHost("MI2", "", "pc", stepMI2), onHost("MI3", "", "pc", stepMI3), onHost("MI9", "lan", "pc", stepMI9),
	mi1Set("pi-kill9", "pi", stepAutomated), mi1Set("pi-sysrq", "pi", stepAutomated), mi1Set("real-pi", "pi", stepReal),
	onHost("MI2", "", "pi", stepMI2), onHost("MI3", "", "pi", stepMI3), onHost("MI9", "lan", "pi", stepMI9), onHost("MI4", "", "pi", stepMI4),
}

// mi1Set expects an MI1 set, which names its host, from step.
func mi1Set(set, host string, step int) expected {
	return expected{label: Label("MI1", set, host), host: host, step: step, match: func(e Envelope) bool { return e.Code == "MI1" && e.Set == set }}
}

// onHost expects code on host, in set, or in any set when set is empty,
// from step.
func onHost(code, set, host string, step int) expected {
	return expected{label: Label(code, set, host), host: host, step: step, match: func(e Envelope) bool {
		return e.Code == code && e.Host == host && (set == "" || e.Set == set)
	}}
}

// Label names a code and set on a host as the final collection does, such
// as "MI1 pc-kill9 (pc)", or "MI3 (pi)" without a set.
func Label(code, set, host string) string {
	return strings.Join(slices.DeleteFunc([]string{code, set, "(" + host + ")"}, func(s string) bool { return s == "" }), " ")
}

// redo is what the guide says to do again for an incomplete part of step:
// the same command, or MI2's two.
func (x expected) redo() string {
	if x.step == stepMI2 {
		return fmt.Sprintf("pasul %d, porniți din nou ambele comenzi", x.step)
	}
	return fmt.Sprintf("pasul %d, porniți din nou aceeași comandă", x.step)
}

// Inventory lists what the results folder dir holds, the Pi's folder
// pulled into it included (G15): present is every code, set and host found
// in an envelope, as "MI1 pc-kill9 (pc)", plus "AGE" for the app's result
// that measured, sorted; missing is each expected result that is absent,
// in the order of the guide. With piSupported false nothing is expected
// with the Pi as host. Files that are not envelopes, or not valid ones,
// are skipped, and an AGE result that was interrupted or has no mbps does
// not count (report-4).
func Inventory(dir string, piSupported bool) (present, missing []string) {
	envs, age := collect(dir)
	for _, e := range envs {
		present = append(present, Label(e.Code, e.Set, e.Host))
	}
	if age {
		present = append(present, ageResult)
	}
	slices.Sort(present)
	present = slices.Compact(present)
	for _, x := range expectations {
		if (piSupported || x.host != "pi") && !slices.ContainsFunc(envs, x.match) {
			missing = append(missing, x.label)
		}
	}
	if !age {
		missing = append(missing, ageResult)
	}
	return present, missing
}

// Envelopes are the valid envelopes of the results folder dir, at any
// depth, as Inventory reads them, for the gates' judges.
func Envelopes(dir string) []Envelope {
	envs, _ := collect(dir)
	return envs
}

// collect reads the results folder dir: its valid envelopes, and whether
// its app/ folders hold an AGE result that measured.
func collect(dir string) (envs []Envelope, age bool) {
	// An entry that cannot be read counts as absent, which missing shows.
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // a file of the results folder
		switch {
		case err != nil:
		case filepath.Base(filepath.Dir(path)) == "app":
			age = age || isAgeResult(data)
		default:
			if e, err := Parse(data); err == nil {
				envs = append(envs, e)
			}
		}
		return nil
	})
	return envs, age
}

// IncompleteParts is what the final collection lists of open, the parts of the
// owner devices that the gates' judges leave without a verdict
// (Judgement.Open; mi1-4, mis-4): a line per part, in the order of the
// guide, with what it lacks and the guide step that completes it, such as
// "MI1 pc-bugcheck (pc): 14 din 20 rulări valide → pasul 6, porniți din
// nou aceeași comandă.". A part that missing names, being absent, is
// left to «Lipsesc», and with piSupported false no part of the Pi is
// listed.
func IncompleteParts(open []OpenPart, missing []string, piSupported bool) []string {
	var out []string
	for _, x := range expectations {
		if (!piSupported && x.host == "pi") || slices.Contains(missing, x.label) {
			continue
		}
		var why []string
		for _, o := range open {
			if o.Part == x.label && !slices.Contains(why, o.Why) {
				why = append(why, o.Why)
			}
		}
		if len(why) > 0 {
			out = append(out, fmt.Sprintf("%s: %s → %s.", x.label, strings.Join(why, "; "), x.redo()))
		}
	}
	return out
}

// isAgeResult reports whether an app result is the AGE scenario's and
// measured: not interrupted, with its mbps.
func isAgeResult(data []byte) bool {
	var r struct {
		Scenario    string   `json:"scenario"`
		Interrupted bool     `json:"interrupted"`
		Mbps        *float64 `json:"mbps"`
	}
	return json.Unmarshal(data, &r) == nil && r.Scenario == ageResult && !r.Interrupted && r.Mbps != nil
}

// InventoryText is what the final collection prints in Romanian: what is
// there, «Lipsesc: …» when something is missing, the incomplete parts
// (IncompleteParts) under «Părți incomplete:», and «Nu lipsește nimic.» only
// when nothing is missing or incomplete.
func InventoryText(present, missing, incomplete []string) string {
	var b strings.Builder
	if len(present) == 0 {
		b.WriteString("Prezente: nimic.\n")
	} else {
		fmt.Fprintf(&b, "Prezente: %s.\n", strings.Join(present, ", "))
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "Lipsesc: %s.\n", strings.Join(missing, ", "))
	}
	if len(incomplete) > 0 {
		b.WriteString("Părți incomplete:\n")
		for _, line := range incomplete {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	if len(missing) == 0 && len(incomplete) == 0 {
		b.WriteString("Nu lipsește nimic.\n")
	}
	return b.String()
}
