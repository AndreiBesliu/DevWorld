package mi6

import (
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// days is a refresh series of days 1 to 7 with the outcomes given, in
// order.
func days(outcomes ...string) []results.RefreshDay {
	out := make([]results.RefreshDay, 0, len(outcomes))
	for i, o := range outcomes {
		out = append(out, results.RefreshDay{Day: i + 1, Attempts: 1, Outcome: o})
	}
	return out
}

const ok, failed, expiry, blocked = results.OutcomeOK, results.OutcomeFailed, results.OutcomeExpectedExpiry, results.OutcomeBlockedByTest

// okWeek is days 1–6 ok and day 7's expected expiry.
func okWeek() []results.RefreshDay { return days(ok, ok, ok, ok, ok, ok, expiry) }

// allCells is the visibility matrix with every cell listed and read.
func allCells() []results.VisibilityCell {
	var out []results.VisibilityCell
	for _, c := range Matrix() {
		c.Listed, c.Read = true, true
		out = append(out, c)
	}
	return out
}

// passingEnvelope is a Phase 0 MI6 run that passes: the three Drive pairs,
// android and windows under plan A, web under its B1, with days 1–6 ok,
// and the whole visibility matrix.
func passingEnvelope() results.Envelope {
	return results.Envelope{Schema: results.Schema, Code: "MI6", RunID: "mi6-1", Set: "pc", Host: "pc", Storage: "ssd", AtMs: 10,
		MI6: &results.Mi6Data{
			Pairs: []results.Mi6Pair{
				{Provider: ProviderDrive, Platform: "android", Dest: "g-android", Plan: "A", HandoffOK: true, IssuedAtMs: 1, Refresh: okWeek()},
				{Provider: ProviderDrive, Platform: "windows", Dest: "g-windows", Plan: "A", HandoffOK: true, IssuedAtMs: 1, Refresh: okWeek()},
				{Provider: ProviderDrive, Platform: "web", Dest: "g-web", Plan: "B1", HandoffOK: true, IssuedAtMs: 1, Refresh: okWeek()},
			},
			Visibility: allCells(),
		}}
}

// pair returns the pair of dest in e.
func pair(e results.Envelope, dest string) *results.Mi6Pair {
	for i := range e.MI6.Pairs {
		if e.MI6.Pairs[i].Dest == dest {
			return &e.MI6.Pairs[i]
		}
	}
	return nil
}

func TestG15_AllPairsOk_Trece(t *testing.T) {
	j := Judge([]results.Envelope{passingEnvelope()})
	if j.Gate != "G15" || j.Verdict != results.Pass || j.PlanB != "" {
		t.Fatalf("Judge = %+v; want G15 trece", j)
	}
	if len(j.Decisions) != 0 {
		t.Errorf("decisions %q; want none (web × Google Drive has no plan A, so its B1 decides nothing)", j.Decisions)
	}
	for _, r := range j.Rows {
		if r.Pass != nil && !*r.Pass {
			t.Errorf("row %+v fails", r)
		}
	}
}

func TestG15_BlockedDay_FaraVerdict(t *testing.T) {
	e := passingEnvelope()
	pair(e, "g-windows").Refresh = days(ok, ok, blocked, ok, ok, ok, expiry)
	if j := Judge([]results.Envelope{e}); j.Verdict != results.Incomplete {
		t.Errorf("a blocked-by-test day: %+v; want %q", j, results.Incomplete)
	}
	// A missing day is no measurement either.
	e = passingEnvelope()
	pair(e, "g-android").Refresh = days(ok, ok, ok, ok, ok)
	if j := Judge([]results.Envelope{e}); j.Verdict != results.Incomplete {
		t.Errorf("days 1–5 only: %+v; want %q", j, results.Incomplete)
	}
}

func TestG15_EmptySet_FaraVerdict(t *testing.T) {
	for name, envs := range map[string][]results.Envelope{
		"no envelope":   nil,
		"no MI6 one":    {{Code: "MI5"}},
		"no pair":       {{Code: "MI6", MI6: &results.Mi6Data{Visibility: allCells()}}},
		"no login done": {{Code: "MI6", MI6: &results.Mi6Data{Pairs: []results.Mi6Pair{{Provider: ProviderDrive, Platform: "android", Dest: "g-android", Plan: "none"}}}}},
	} {
		if j := Judge(envs); j.Verdict != results.Incomplete {
			t.Errorf("%s: %+v; want %q", name, j, results.Incomplete)
		}
	}
}

func TestG15_B1Fails_PicaB2(t *testing.T) {
	e := passingEnvelope()
	p := pair(e, "g-android")
	p.Plan, p.HandoffOK, p.PlanAFailure, p.B1Failure, p.Refresh = "B1", false, "invalid_grant", "access_denied", nil
	j := Judge([]results.Envelope{e})
	if j.Verdict != results.FailWith("B2 (proxy fără stare în cloud, §5.6)") || j.PlanB != "B2 (proxy fără stare în cloud, §5.6)" {
		t.Fatalf("Judge = %+v; want pică → B2", j)
	}
	want := "G15: Android × Google Drive trece pe B2: proxy fără stare în cloud (§5.6), construit cu cloud-ul în Faza 2; " +
		"rândurile din harta datelor și din politica de confidențialitate se actualizează întâi."
	if !slices.Contains(j.Decisions, want) {
		t.Errorf("decisions %q; want %q", j.Decisions, want)
	}
}

func TestG15_FailedDayUnderB1_PicaB2(t *testing.T) {
	e := passingEnvelope()
	p := pair(e, "g-windows")
	p.Plan, p.PlanAFailure, p.RefreshFrom = "B1", "invalid_grant", "g-web"
	pair(e, "g-web").Refresh = days(ok, ok, ok, failed, ok, ok, expiry)
	j := Judge([]results.Envelope{e})
	if j.Verdict != results.FailWith("B2 (proxy fără stare în cloud, §5.6)") {
		t.Fatalf("a failed day of g-web's series, the evidence of a B1 pair: %+v; want pică → B2", j)
	}
	if !slices.ContainsFunc(j.Decisions, func(d string) bool { return strings.HasPrefix(d, "G15: Windows × Google Drive trece pe B2") }) {
		t.Errorf("decisions %q; want Windows's B2 line", j.Decisions)
	}
	// B1 that holds: a decision, and the gate passes.
	e = passingEnvelope()
	p = pair(e, "g-android")
	p.Plan, p.PlanAFailure, p.RefreshFrom, p.Refresh = "B1", "invalid_grant", "g-web", days(ok, ok)
	j = Judge([]results.Envelope{e})
	if j.Verdict != results.Pass || !slices.Equal(j.Decisions, []string{"G15: Android × Google Drive folosește B1 (device flow pe hub)."}) {
		t.Errorf("B1 after a plan A failure, g-web's days ok: %+v; want trece and the B1 decision", j)
	}
	// A failed day under plan A is never final: without its B1, no verdict.
	e = passingEnvelope()
	pair(e, "g-android").Refresh = days(ok, failed, ok, ok, ok, ok, expiry)
	if j := Judge([]results.Envelope{e}); j.Verdict != results.Incomplete {
		t.Errorf("a failed day under plan A, no B1: %+v; want %q", j, results.Incomplete)
	}
}

func TestG15_VisibilityFails_HubClientType(t *testing.T) {
	e := passingEnvelope()
	for i, c := range e.MI6.Visibility {
		if c.Creator == "desktop" && c.Reader == "web" {
			e.MI6.Visibility[i].Listed, e.MI6.Visibility[i].Read = false, false
		}
		if c.Creator == "android" && c.Reader == "android" { // Self: never judged
			e.MI6.Visibility[i].Read = false
		}
	}
	j := Judge([]results.Envelope{e})
	if j.Verdict != results.FailWith("F6 folosește tipul de client al hub-ului") {
		t.Fatalf("Judge = %+v; want pică → F6", j)
	}
	if !slices.Equal(j.Decisions, []string{"F6 pe web folosește tipul de client al hub-ului (creatori: desktop)."}) {
		t.Errorf("decisions %q; want the F6 line for web naming desktop", j.Decisions)
	}
	// A failed tv-reader cell is web's too; android's reader is Android.
	e = passingEnvelope()
	for i, c := range e.MI6.Visibility {
		if c.Creator == "android" && c.Reader == "tv" || c.Creator == "tv" && (c.Reader == "web" || c.Reader == "android") {
			e.MI6.Visibility[i].Read = false
		}
	}
	j = Judge([]results.Envelope{e})
	want := []string{"F6 pe web folosește tipul de client al hub-ului (creatori: android, tv).",
		"F6 pe Android folosește tipul de client al hub-ului (creatori: tv)."}
	if !slices.Equal(j.Decisions, want) {
		t.Errorf("decisions %q; want %q", j.Decisions, want)
	}
}

func TestG15_Day7Expiry_NotFailure(t *testing.T) {
	e := passingEnvelope()
	for _, d := range []string{"g-android", "g-windows", "g-web"} {
		pair(e, d).Refresh = days(ok, ok, ok, ok, ok, ok, expiry)
	}
	if j := Judge([]results.Envelope{e}); j.Verdict != results.Pass {
		t.Errorf("day 7's expected expiry: %+v; want trece", j)
	}
	pair(e, "g-web").Refresh = days(ok, ok, ok, ok, ok, ok, failed)
	if j := Judge([]results.Envelope{e}); j.Verdict != results.Pass {
		t.Errorf("day 7 is not judged: %+v; want trece", j)
	}
}

func TestG15_OneDriveDeferredWithoutA5(t *testing.T) {
	e := passingEnvelope()
	j := Judge([]results.Envelope{e})
	deferred := string(results.Deferred("înregistrarea Microsoft cere un director plătit (A5)"))
	var rows int
	for _, r := range j.Rows {
		if strings.Contains(r.Value, deferred) {
			rows++
			if r.Pass != nil {
				t.Errorf("deferred row %+v judges", r)
			}
		}
	}
	if j.Verdict != results.Pass || rows != 3 {
		t.Errorf("Judge = %+v; want trece with the three OneDrive rows deferred", j)
	}
	// With A5, a OneDrive pair without its login has no verdict.
	e.MI6.OneDriveEnabled = true
	if j := Judge([]results.Envelope{e}); j.Verdict != results.Incomplete {
		t.Errorf("OneDrive enabled, no OneDrive login: %+v; want %q", j, results.Incomplete)
	}
	for _, p := range []string{"android", "windows", "web"} {
		e.MI6.Pairs = append(e.MI6.Pairs, results.Mi6Pair{Provider: ProviderOneDrive, Platform: p, Dest: "m-" + p, Plan: "A",
			HandoffOK: true, IssuedAtMs: 1, Refresh: okWeek()})
	}
	if j := Judge([]results.Envelope{e}); j.Verdict != results.Pass {
		t.Errorf("OneDrive enabled, its pairs ok: %+v; want trece", j)
	}
}

// TestMI6_VisibilityMatrixShape (P21): creators android, desktop and tv ×
// readers web, tv, android and desktop: 12 cells, the 3 of one client
// Self, 9 judged.
func TestMI6_VisibilityMatrixShape(t *testing.T) {
	cells := Matrix()
	self, judged := 0, 0
	seen := map[[2]string]bool{}
	for _, c := range cells {
		seen[[2]string{c.Creator, c.Reader}] = true
		if c.Provider != ProviderDrive {
			t.Errorf("cell %+v; want Google Drive's", c)
		}
		if c.Self != (c.Creator == c.Reader) {
			t.Errorf("cell %+v: Self %v", c, c.Self)
		}
		if c.Self {
			self++
		} else {
			judged++
		}
	}
	if len(cells) != 12 || len(seen) != 12 || self != 3 || judged != 9 {
		t.Errorf("%d cells (%d distinct), %d Self, %d judged; want 12, 3, 9", len(cells), len(seen), self, judged)
	}
	for _, cr := range []string{"android", "desktop", "tv"} {
		for _, rd := range []string{"web", "tv", "android", "desktop"} {
			if !seen[[2]string{cr, rd}] {
				t.Errorf("no cell %s → %s", cr, rd)
			}
		}
	}
}
