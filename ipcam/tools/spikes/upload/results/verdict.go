// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package results

// Verdict is a gate's verdict, in Romanian, exactly one of P22's four
// forms: Pass, FailWith's, Incomplete and Deferred's.
type Verdict string

// The verdicts without a plan B or a reason.
const (
	Pass       Verdict = "trece"
	Incomplete Verdict = "fără verdict: lipsesc măsurători"
)

// FailWith is the verdict of a gate that failed, naming its plan B.
func FailWith(planB string) Verdict { return Verdict("pică → " + planB) }

// Deferred is the verdict of a gate, or a part of one, that Faza 0-bis
// measures, with the reason.
func Deferred(reason string) Verdict { return Verdict("amânat în Faza 0-bis: " + reason) }

// Row is one line of a gate's table: what was measured, against which
// limit, where; Pass is nil when the row judges nothing.
type Row struct {
	Code   string `json:"code"`
	Value  string `json:"value"`
	Limit  string `json:"limit"`
	Source string `json:"source"`
	Pass   *bool  `json:"pass"`
}

// Judgement is a gate's verdict, its plan B on failure, the reasons behind
// it, its table and its §2.2 decision lines.
type Judgement struct {
	Gate      string   `json:"gate"`
	Verdict   Verdict  `json:"verdict"`
	PlanB     string   `json:"planB,omitempty"`
	Reasons   []string `json:"reasons"`
	Rows      []Row    `json:"rows"`
	Decisions []string `json:"decisions"`
}
