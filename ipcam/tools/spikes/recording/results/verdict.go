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
// the source of the value (lab, pc, pi, ci-relay, ci-chrome, lan, …). Pass
// is nil for a row that is information only or has no value. Host is the
// host whose table shows the row when Source does not name it, as for
// MI9's legs, whose Source is where the leg was measured (G26); empty
// otherwise, the row then going to the table that Source names.
type Row struct {
	Code, Value, Limit, Source string
	Host                       string
	Pass                       *bool
}

// Judgement is a gate's verdict, its plan B when it fails, the reasons in
// Romanian, and the rows behind it (C17). Reasons explain the verdict;
// Notes, in Romanian too, report measurements outside it (MI1's O17-off
// loss, C1-R13), which neither change the verdict nor call for the plan B.
// O17OffOverLimit is G10's: an O17-off run's 256 kbps loss went over §4.5's
// bound, which calls for a proposal in the report's PR, not a decision.
// Recommended is, per host, the recommended camera count that the gate's
// measurements leave (§4.8): G12's count, measured or lowered, and G13's
// after the Pi's encryption rate; a host whose count the gate did not
// settle has none. Open is each part of an owner device that the gate
// still has no verdict for, which the owner completes by running its step
// again (the final collection, IncompleteParts).
type Judgement struct {
	Gate            string
	Verdict         Verdict
	PlanB           string
	Reasons         []string
	Notes           []string
	Rows            []Row
	O17OffOverLimit bool
	Recommended     map[string]int
	Open            []OpenPart
}

// OpenPart is a part of an owner device without a verdict yet (mi1-4,
// mis-4): Part names it as Inventory does ("MI1 pc-bugcheck (pc)",
// "MI3 (pi)"), and Why says, in Romanian, what it lacks ("14 din 20
// rulări valide"). A part that failed is decided (G27) and is never open.
type OpenPart struct{ Part, Why string }

// AddOpen records that part, of an owner device, lacks why (OpenPart).
func (j *Judgement) AddOpen(part, why string) {
	j.Open = append(j.Open, OpenPart{Part: part, Why: why})
}

// Recommend records n as host's recommended camera count in j.
func (j *Judgement) Recommend(host string, n int) {
	if j.Recommended == nil {
		j.Recommended = map[string]int{}
	}
	j.Recommended[host] = n
}
