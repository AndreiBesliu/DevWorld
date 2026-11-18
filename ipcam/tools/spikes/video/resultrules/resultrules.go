// Package resultrules holds what spikehub and spikereport both decide about a
// results file (app/src/results.ts ScenarioResult): the device it belongs to
// (Key) and what the report can use it for (Class). spikehub's progress (GET
// /spike/v1/progress) counts a scenario as complete exactly when the report
// judges its run, so the automatic run (post-release task A of
// ipcam/docs/superpowers/plans/2026-10-18-phase0b-video-spike.md) never skips a
// scenario that the report would leave without a verdict.
//
// The spike app keeps a copy of Key in TypeScript (app/src/deviceplan.ts
// deviceKey), pinned by its tests against the cases of this package's.
package resultrules

import (
	"slices"
	"strings"
)

// browsers are the web platforms (app/src/device.ts). The guide runs them on
// the owner's Windows PC (spec §16.3, WEB), so a browser run on another OS
// family is a device of its own (hub-4).
var browsers = []string{"chrome", "edge", "firefox"}

// expectedBrowserOS is the OS family of the browsers' runs that the verdicts read.
const expectedBrowserOS = "windows"

// PlatformAndroid is the platform of the phone's results: the Android shell.
const PlatformAndroid = "android"

// DesktopGridTiles is the grid that MV7 needs on a desktop path (R45, spec G6).
const DesktopGridTiles = 16

// IsBrowser says whether platform is one of the browsers.
func IsBrowser(platform string) bool { return slices.Contains(browsers, platform) }

// OSFamily is the OS family of a result's device in lower case ("windows",
// "android", "linux", "macos"), as device.ts names it; empty when the result
// does not say. It names a CSV file of the report, so it keeps only letters
// and digits of the first word, and at most 16 of them.
func OSFamily(os string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(os), " ")
	family := strings.Map(func(c rune) rune {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			return c
		}
		return -1
	}, strings.ToLower(first))
	return family[:min(len(family), 16)]
}

// Key is the device that a result belongs to: its platform, except for a
// browser on an OS family other than Windows, whose key adds the family
// ("chrome-android"). A run of Chrome on the phone then never replaces nor
// merges with the PC's Chrome (hub-4). A browser result that names no OS stays
// with its platform.
func Key(platform, os string) string {
	family := OSFamily(os)
	if !IsBrowser(platform) || family == "" || family == expectedBrowserOS {
		return platform
	}
	return platform + "-" + family
}

// BasePlatform is the platform of a device key: "chrome" for "chrome-android".
func BasePlatform(key string) string {
	for _, p := range browsers {
		if strings.HasPrefix(key, p+"-") {
			return p
		}
	}
	return key
}

// Device is what the rules read of a result's device.
type Device struct {
	Platform string `json:"platform"`
	Model    string `json:"model"`
	OS       string `json:"os"`
}

// Readings are the samples that the rules read.
type Readings struct {
	// Charging and MainCharging are S3's charger readings, 1 while charging:
	// in the grid and in the fullscreen part (R48).
	Charging     []float64 `json:"charging"`
	MainCharging []float64 `json:"mainCharging"`
	// Tiles is the grid that an S4 run paged.
	Tiles []float64 `json:"tiles"`
}

// Facts is what the rules read of a results file. It decodes from the file's
// JSON and passes over everything else in it.
type Facts struct {
	Scenario    string   `json:"scenario"`
	Device      Device   `json:"device"`
	Interrupted bool     `json:"interrupted"`
	Notes       []string `json:"notes"`
	Samples     Readings `json:"samples"`
}

// Key is the device of the result (Key).
func (f Facts) Key() string { return Key(f.Device.Platform, f.Device.OS) }

// Class is what a run counts for in the report.
type Class int

// The classes of a run.
const (
	// Usable: the report judges the run (the newest usable run of its scenario on its device).
	Usable Class = iota
	// Interrupted: the app ended the run early, for any reason.
	Interrupted
	// Failed: the scenario stopped with an error (a `failed:` note), so its readings are incomplete.
	Failed
	// OnCharger: an S3 run with a reading that says the phone was charging (R48), although the
	// app did not end it: battery and heat cannot be judged.
	OnCharger
	// Informative: an S4 run of a desktop path that did not page 16 tiles (R45). The report
	// lists it with its values, but judges MV7 there only on 16 tiles.
	Informative
)

// String is the class as testdata/classes.json names it.
func (c Class) String() string {
	switch c {
	case Usable:
		return "usable"
	case Interrupted:
		return "interrupted"
	case Failed:
		return "failed"
	case OnCharger:
		return "on-charger"
	case Informative:
		return "informative"
	}
	return "unknown"
}

// Class says what the run counts for. The checks go in this order, so a run
// that was interrupted on the charger is interrupted.
func (f Facts) Class() Class {
	switch {
	case f.Interrupted:
		return Interrupted
	case slices.ContainsFunc(f.Notes, func(n string) bool { return strings.HasPrefix(n, "failed:") }):
		return Failed
	case strings.HasPrefix(f.Scenario, "S3-") && (slices.Contains(f.Samples.Charging, 1) || slices.Contains(f.Samples.MainCharging, 1)):
		return OnCharger
	case f.Scenario == "S4" && f.Device.Platform != PlatformAndroid && DesktopTiles(f.Samples.Tiles) != DesktopGridTiles:
		return Informative
	}
	return Usable
}

// DesktopTiles is the grid of an S4 run: its first tiles reading, 0 when it has none.
func DesktopTiles(tiles []float64) int {
	if len(tiles) > 0 {
		return int(tiles[0])
	}
	return 0
}
