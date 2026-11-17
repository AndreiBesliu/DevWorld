// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

import (
	"fmt"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The G14 judge's names and texts.
const (
	gate = "G14"
	// OwnerBlock is MI5's block: 6 h (P09).
	OwnerBlock = 6 * time.Hour
	// kindLocal is the LOCAL destination's kind, which no block cuts.
	kindLocal = "LOCAL"

	reasonNoEnvelope   = "nicio rulare MI5 de 6 h a proprietarului (laboratorul și rezultatele cu variabile de test nu se judecă)"
	reasonNoSegments   = "%s: niciun segment înregistrat în timpul blocării"
	reasonNoResume     = "repornirea nu a reluat o încărcare de la un offset > 0"
	reasonNotVerified  = "%s: %d din %d segmente verificate"
	reasonMissing      = "%s: %d lipsă, %d duplicate, %d hash-uri greșite"
	reasonReadOpenFail = "citirea segmentului deschis: %d din %d reușite"
	limitG14           = "0 lipsă, 0 duplicate, fiecare hash verificat; citirea segmentului deschis reușită"
)

// Judge is G14 (P22): over the owner's MI5 envelopes with a block of at
// least 6 h (a lab envelope is TST-07's row and never decides G14, nor
// does one written with test seams, C2-R22 (3)), each judged envelope
// must have every non-deferred destination with segments recorded during
// the block, a restart that resumed above 0, and the hub's own evidence
// that the block held: between Block + 2 min and Unblock no upload
// verified at a provider, and a transient failure or an UNREACHABLE
// destination. It passes when one judged envelope holds every floor with
// every segment verified, none missing, duplicated or mismatched, and
// every open-segment read ok. A missing, duplicate or mismatched segment
// fails it (the outbox is fixed before Faza 3), and a failed open-segment
// read too (2-min segments): a measured failure decides even when a floor
// is missed. Anything else is fără verdict.
func Judge(envs []results.Envelope) results.Judgement {
	j := results.Judgement{Gate: gate, Reasons: []string{}, Rows: []results.Row{}, Decisions: []string{}}
	var outbox, twoMin, passed bool
	var incomplete []string
	judged := 0
	for _, e := range envs {
		if e.Code != "MI5" || e.MI5 == nil || e.MI5.Lab || e.TestSeams || e.MI5.BlockSecs < OwnerBlock.Seconds() || !valid(e) {
			continue
		}
		judged++
		d := e.MI5
		var floors []string
		measuredFail, allVerified := false, true
		for _, dest := range d.Dests {
			if dest.Deferred != "" {
				j.Rows = append(j.Rows, results.Row{Code: "MI5", Value: dest.Dest + ": " + string(results.Deferred(stripDeferred(dest.Deferred))),
					Limit: limitG14, Source: source(e)})
				continue
			}
			row := results.Row{Code: "MI5", Value: fmt.Sprintf("%s: %d/%d verificate, %d lipsă, %d duplicate, %d hash-uri greșite",
				dest.Dest, dest.Verified, dest.Segments, dest.Missing, dest.Duplicates, dest.HashMismatches), Limit: limitG14, Source: source(e)}
			if dest.Missing > 0 || dest.Duplicates > 0 || dest.HashMismatches > 0 {
				outbox, measuredFail = true, true
				row.Pass = boolPtr(false)
				j.Reasons = append(j.Reasons, fmt.Sprintf(reasonMissing, dest.Dest, dest.Missing, dest.Duplicates, dest.HashMismatches))
			}
			if dest.SegmentsDuringBlock <= 0 {
				floors = append(floors, fmt.Sprintf(reasonNoSegments, dest.Dest))
			}
			if dest.Kind != kindLocal && (dest.VerifiedDuringBlock > 0 || dest.FailedDuringBlock == 0 && !dest.UnreachableDuringBlock) {
				floors = append(floors, dest.Dest+": "+ReasonBlockLeaked)
			}
			if dest.Verified != dest.Segments || dest.Segments == 0 {
				allVerified = false
				floors = append(floors, fmt.Sprintf(reasonNotVerified, dest.Dest, dest.Verified, dest.Segments))
			}
			if row.Pass == nil && allVerified {
				row.Pass = boolPtr(true)
			}
			j.Rows = append(j.Rows, row)
		}
		if d.Restart == nil || d.Restart.ResumedOffset <= 0 {
			floors = append(floors, reasonNoResume)
		}
		calls, ok := 0, 0
		for _, r := range d.ReadOpen {
			calls, ok = calls+r.Calls, ok+r.OK
		}
		if ok < calls {
			twoMin, measuredFail = true, true
			j.Reasons = append(j.Reasons, fmt.Sprintf(reasonReadOpenFail, ok, calls))
		}
		if calls == 0 {
			floors = append(floors, fmt.Sprintf(reasonReadOpenFail, 0, 0))
		}
		if !measuredFail && len(floors) == 0 {
			passed = true
		}
		incomplete = append(incomplete, floors...)
	}
	switch {
	case outbox:
		j.Verdict, j.PlanB = results.FailWith(PlanBOutbox), PlanBOutbox
	case twoMin:
		j.Verdict, j.PlanB = results.FailWith(PlanBTwoMinSegs), PlanBTwoMinSegs
	case passed:
		j.Verdict = results.Pass
	case judged == 0:
		j.Verdict = results.Incomplete
		j.Reasons = append(j.Reasons, reasonNoEnvelope)
	default:
		j.Verdict = results.Incomplete
		j.Reasons = append(j.Reasons, incomplete...)
	}
	return j
}

// valid reports whether e's run is valid: a cut run is run again another
// day, and is never judged.
func valid(e results.Envelope) bool {
	if len(e.Runs) == 0 {
		return false
	}
	for _, r := range e.Runs {
		if !r.Valid {
			return false
		}
	}
	return true
}

// source names an envelope in the judgement's rows.
func source(e results.Envelope) string { return "MI5/" + e.Set + "/" + e.RunID }

// stripDeferred is a Deferred line without its prefix, which
// results.Deferred adds again.
func stripDeferred(s string) string {
	const prefix = "amânat în Faza 0-bis: "
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func boolPtr(b bool) *bool { return &b }
