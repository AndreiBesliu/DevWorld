// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package results

// Verdict is a gate's outcome, in the report's Romanian (G27, R39).
type Verdict string

// The verdicts. A failed gate's plan B is in Judgement.PlanB; the report
// shows "pică → <plan B>". A measured failure decides even when other
// measurements are missing.
const (
	Pass       Verdict = "trece"
	Fail       Verdict = "pică"
	Incomplete Verdict = "fără verdict: lipsesc măsurători"
)

// Deferred is the verdict of a gate whose measurements move to Faza 0-bis.
func Deferred(reason string) Verdict { return Verdict("amânat în Faza 0-bis: " + reason) }

// Row is one line of the report's table: Cod | Valoare | Prag | Trece, with
// the source of the value (lab, pc, pi, …). Pass is nil for a row that is
// information only or has no value.
type Row struct {
	Code, Value, Limit, Source string
	Pass                       *bool
}

// Judgement is a gate's verdict, its plan B when it fails, the reasons in
// Romanian, and the rows behind it (C17). A reason that starts with
// «informativ, în afara <gate>» reports a measurement outside the verdict
// (MI1's O17-off runs) and never explains the verdict itself.
type Judgement struct {
	Gate    string
	Verdict Verdict
	PlanB   string
	Reasons []string
	Rows    []Row
}
