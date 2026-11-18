// Package deviceplan is the table of the automatic run (post-release task A of
// ipcam/docs/superpowers/plans/2026-10-18-phase0b-video-spike.md): for each
// device of the owner's guide, the scenarios it runs, in order, and the
// release they belong to. deviceplan.json is the table's single source: the
// spike app imports it (app/src/deviceplan.ts), spikehub reads it here to tell
// the owner what is left, and a test checks the guide against it.
package deviceplan

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
)

// Pause is the step of a cooling pause between two scenarios (the phone's
// between S3-9 and S3-4). It lasts PauseMinutes.
const Pause = "pause"

// Device is a device of the guide and its run.
type Device struct {
	// ID names the device in the app and in spikehub's answers: chrome, edge,
	// firefox, ipcam-desktop, phone.
	ID string `json:"id"`
	// Label is the name the owner reads.
	Label string `json:"label"`
	// Platform is the platform of its results (app/src/device.ts).
	Platform string `json:"platform"`
	// PC is set for the owner's PC (its browsers and ipcam-desktop), unset for the phone.
	PC bool `json:"pc"`
	// Steps are the scenario ids, in order, with Pause where the run pauses.
	Steps []string `json:"steps"`
}

// Scenarios are the device's steps without the pause.
func (d Device) Scenarios() []string {
	return slices.DeleteFunc(slices.Clone(d.Steps), func(step string) bool { return step == Pause })
}

type plan struct {
	Version      string   `json:"version"`
	PauseMinutes int      `json:"pauseMinutes"`
	Devices      []Device `json:"devices"`
}

//go:embed deviceplan.json
var raw []byte

var table = mustParse(raw)

func mustParse(data []byte) plan {
	var p plan
	if err := json.Unmarshal(data, &p); err != nil {
		panic(fmt.Sprintf("deviceplan.json: %v", err))
	}
	return p
}

// Version is the release that the app, spikehub and this table belong to,
// such as spike-video-v0.1.1.
func Version() string { return table.Version }

// PauseMinutes is how long a Pause step lasts.
func PauseMinutes() int { return table.PauseMinutes }

// Devices are the devices of the guide, in its order. The caller may not change them.
func Devices() []Device { return table.Devices }

// ByPlatform is the device whose results carry platform.
func ByPlatform(platform string) (Device, bool) {
	i := slices.IndexFunc(table.Devices, func(d Device) bool { return d.Platform == platform })
	if i < 0 {
		return Device{}, false
	}
	return table.Devices[i], true
}
