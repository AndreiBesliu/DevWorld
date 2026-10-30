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

// expected is one result that the final collection must hold (G15).
type expected struct {
	label, host string
	match       func(Envelope) bool
}

// expectations are the six MI1 sets, MI2, MI3 and MI9 lan per device, and
// MI4 on the Pi, the PC's first; the app's AGE result is checked apart.
var expectations = []expected{
	mi1Set("pc-kill9", "pc"), mi1Set("pc-bugcheck", "pc"), mi1Set("real-pc", "pc"),
	onHost("MI2", "", "pc"), onHost("MI3", "", "pc"), onHost("MI9", "lan", "pc"),
	mi1Set("pi-kill9", "pi"), mi1Set("pi-sysrq", "pi"), mi1Set("real-pi", "pi"),
	onHost("MI2", "", "pi"), onHost("MI3", "", "pi"), onHost("MI9", "lan", "pi"), onHost("MI4", "", "pi"),
}

// mi1Set expects an MI1 set, which names its host.
func mi1Set(set, host string) expected {
	return expected{label: label("MI1", set, host), host: host, match: func(e Envelope) bool { return e.Code == "MI1" && e.Set == set }}
}

// onHost expects code on host, in set, or in any set when set is empty.
func onHost(code, set, host string) expected {
	return expected{label: label(code, set, host), host: host, match: func(e Envelope) bool {
		return e.Code == code && e.Host == host && (set == "" || e.Set == set)
	}}
}

// label names a code and set on a host, such as "MI1 pc-kill9 (pc)".
func label(code, set, host string) string {
	return strings.Join(slices.DeleteFunc([]string{code, set, "(" + host + ")"}, func(s string) bool { return s == "" }), " ")
}

// Inventory lists what the results folder dir holds, the Pi's folder
// pulled into it included (G15): present is every code, set and host found
// in an envelope, as "MI1 pc-kill9 (pc)", plus "AGE" for the app's result,
// sorted; missing is each expected result that is absent, in the order of
// the guide. With piSupported false nothing is expected with the Pi as
// host. Files that are not envelopes, or not valid ones, are skipped.
func Inventory(dir string, piSupported bool) (present, missing []string) {
	var envs []Envelope
	age := false
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
	for _, e := range envs {
		present = append(present, label(e.Code, e.Set, e.Host))
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

// isAgeResult reports whether an app result is the AGE scenario's.
func isAgeResult(data []byte) bool {
	var r struct {
		Scenario string `json:"scenario"`
	}
	return json.Unmarshal(data, &r) == nil && r.Scenario == ageResult
}

// InventoryText is what the final collection prints in Romanian: what is
// there and, when something is missing, «Lipsesc: …».
func InventoryText(present, missing []string) string {
	var b strings.Builder
	if len(present) == 0 {
		b.WriteString("Prezente: nimic.\n")
	} else {
		fmt.Fprintf(&b, "Prezente: %s.\n", strings.Join(present, ", "))
	}
	if len(missing) == 0 {
		b.WriteString("Nu lipsește nimic.\n")
	} else {
		fmt.Fprintf(&b, "Lipsesc: %s.\n", strings.Join(missing, ", "))
	}
	return b.String()
}
