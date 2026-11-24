// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package results

import "regexp"

// cameraPattern is a camera's pseudonym (PH0-22): real-1, real-2, …
var cameraPattern = regexp.MustCompile(`^real-[1-9][0-9]?$`)

// The motion reference's methods (P41): the camera's substream decoded by
// FFmpeg, or its ONVIF snapshots (the fallback).
const (
	ReferenceSubstream = "substream"
	ReferenceSnapshot  = "snapshot"
)

var referenceMethods = []string{ReferenceSubstream, ReferenceSnapshot}

// Mi8Data is MI8's run (G17, HUB-11): the owner's cameras subscribed to
// through ipcam-hub diag onvif for Hours observed (the sessions added up,
// P49), each against the harness's frame-diff motion reference, whose
// episodes end after QuietGapSecs without motion.
type Mi8Data struct {
	Hours        float64     `json:"hours"`
	QuietGapSecs int         `json:"quietGapSecs"`
	Cameras      []Mi8Camera `json:"cameras"`
}

// Mi8Camera is one camera's outcome:
//   - Brand and Model are the camera's GetDeviceInformation (never its
//     serial);
//   - Episodes are the reference's motion episodes, Matched those with a
//     motion event of the camera within [start − RefLagMs, start + 5 s],
//     and Coverage is Matched/Episodes (0 without episodes);
//   - SilentLosses counts the events_lost signals: more than 5 min
//     without a successful Renew or PullMessages;
//   - RefLagMs is the reference's own delay L (P41);
//   - SubscribeFailed is set when no subscription could be created (a
//     configuration fault, apart from coverage);
//   - ReferenceMethod is "substream" or "snapshot";
//   - EpisodeTimes are the episodes' hub receipt times, Unix ms.
type Mi8Camera struct {
	Camera          string        `json:"camera"`
	Brand           string        `json:"brand"`
	Model           string        `json:"model"`
	Episodes        int           `json:"episodes"`
	Matched         int           `json:"matched"`
	Coverage        float64       `json:"coverage"`
	SilentLosses    int           `json:"silentLosses"`
	RefLagMs        int64         `json:"refLagMs"`
	SubscribeFailed bool          `json:"subscribeFailed"`
	ReferenceMethod string        `json:"referenceMethod"`
	EpisodeTimes    []EpisodeTime `json:"episodeTimes"`
}

// EpisodeTime is one reference episode and whether an event matched it.
type EpisodeTime struct {
	StartMs int64 `json:"startMs"`
	EndMs   int64 `json:"endMs"`
	Matched bool  `json:"matched"`
}
