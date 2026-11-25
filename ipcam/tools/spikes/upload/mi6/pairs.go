// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package mi6 is MI6's driver logic (G15, P21, P39, P53): the pairs, the
// run's state as uploadspike keeps it in the run state, its envelope, the
// visibility matrix, the owner's status lines, and the G15 judge.
package mi6

import "github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"

// The providers as run.json names them.
const (
	ProviderDrive    = "GOOGLE_DRIVE"
	ProviderOneDrive = "ONEDRIVE_PERSONAL"
)

// The plans of a pair (P39): none while no login completed, A, the app's
// own login handed to the hub, and B1, the hub's device flow.
const (
	PlanNone = "none"
	PlanA    = "A"
	PlanB1   = "B1"
)

// PairDef is a platform × provider pair and its destination's pseudonym.
type PairDef struct {
	Dest, Provider, Platform string
}

// The Phase 0 pairs: Drive on Android, Windows and web, and OneDrive on the
// same platforms when A5 passed (C2-R4).
var (
	Drive = []PairDef{
		{Dest: "g-android", Provider: ProviderDrive, Platform: "android"},
		{Dest: "g-windows", Provider: ProviderDrive, Platform: "windows"},
		{Dest: "g-web", Provider: ProviderDrive, Platform: "web"},
	}
	OneDrive = []PairDef{
		{Dest: "m-android", Provider: ProviderOneDrive, Platform: "android"},
		{Dest: "m-windows", Provider: ProviderOneDrive, Platform: "windows"},
		{Dest: "m-web", Provider: ProviderOneDrive, Platform: "web"},
	}
)

// HasPlanA reports whether the pair has a plan A: every pair but web ×
// Google Drive, which always uses the hub's device flow (FLW-31).
func (d PairDef) HasPlanA() bool { return d.Provider != ProviderDrive || d.Platform != "web" }

// label is the pair in Romanian, such as «Android × Google Drive».
func (d PairDef) label() string { return PlatformName(d.Platform) + " × " + ProviderName(d.Provider) }

// PlatformName is a platform's Romanian name: Android, Windows, web.
func PlatformName(p string) string {
	switch p {
	case "android":
		return "Android"
	case "windows":
		return "Windows"
	}
	return p
}

// ProviderName is a provider's name for the owner.
func ProviderName(p string) string {
	if p == ProviderOneDrive {
		return "OneDrive personal"
	}
	return "Google Drive"
}

// The visibility matrix's client types (P21): the hub clients that create,
// and the readers, the web page's client first.
var (
	creators3 = []string{"android", "desktop", "tv"}
	readers4  = []string{"web", "tv", "android", "desktop"}
)

// Matrix is the visibility matrix: each creator × each reader, 12 cells,
// the 3 of one client Self and never judged.
func Matrix() []results.VisibilityCell {
	out := make([]results.VisibilityCell, 0, len(creators3)*len(readers4))
	for _, c := range creators3 {
		for _, r := range readers4 {
			out = append(out, results.VisibilityCell{Provider: ProviderDrive, Creator: c, Reader: r, Self: c == r})
		}
	}
	return out
}
