// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package mi1 is the power-cut measurement MI1 (G10, §16.4, T43): the
// composition of its sets, the rules that redo a run that does not count,
// and Judge, the one rule of the gate (C17).
package mi1

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The sets of MI1: three in the lab, four automated on the owner's devices
// and the two sets of real cuts.
const (
	SetLabLinuxKill9     = "lab-linux-kill9"
	SetLabLinuxQEMUReset = "lab-linux-qemu-reset"
	SetLabWindowsKill9   = "lab-windows-kill9"
	SetPCKill9           = "pc-kill9"
	SetPCBugcheck        = "pc-bugcheck"
	SetPiKill9           = "pi-kill9"
	SetPiSysrq           = "pi-sysrq"
	SetRealPC            = "real-pc"
	SetRealPi            = "real-pi"
)

// The streams that MI1 records, as reccontroller serves them: every run
// records cam01_main (4 Mbps); cam99_main is the 256 kbps camera of
// HUB-07's low-bitrate runs.
const (
	MainStream       = "cam01_main"
	LowBitrateStream = "cam99_main"
)

// The bitrates of the streams, in kbps.
const (
	mainKbps       = 4000
	lowBitrateKbps = 256
)

const (
	// defaultRuns is the number of default runs of an automated set; the
	// O17-off run comes after them.
	defaultRuns = 20
	// lowBitrateRun is the default run that also records the 256 kbps
	// stream: encrypted, in the lab's first shard.
	lowBitrateRun = 9
	// labShard is the number of runs of a lab set's first shard; the
	// second has the rest, the O17-off run included.
	labShard = 10
	// realSlots is the number of valid real cuts per set.
	realSlots = 5
	// MaxRealAttempts bounds the attempts of a real set (G12).
	MaxRealAttempts = 8
	// maxCameras is the number of cameras that reccontroller serves.
	maxCameras = 16
)

// BitrateKbps is the bitrate of a stream that MI1 records: 4,000 for
// cam01_main to cam16_main, 256 for cam99_main, 0 for any other.
func BitrateKbps(stream string) int {
	if stream == LowBitrateStream {
		return lowBitrateKbps
	}
	num, isCam := strings.CutPrefix(stream, "cam")
	num, isMain := strings.CutSuffix(num, "_main")
	if n, err := strconv.Atoi(num); isCam && isMain && len(num) == 2 && err == nil && n >= 1 && n <= maxCameras {
		return mainKbps
	}
	return 0
}

// RunPlan is one run of a set as the driver records it. Index is the run's
// 1-based position: its part number in an automated set, where 21 is the
// O17-off run, or a real cut's slot. A stream is encrypted when its job has
// a scope; PadLowBitrate is the per-camera setting „Pierdere minimă la
// pană de curent” (O17), on except in the O17-off run.
type RunPlan struct {
	Index         int
	Encrypted     bool
	PadLowBitrate bool
	Streams       []string
}

// Plan is the composition of set (G10, G12, O17). An automated set has 20
// default runs, the odd ones encrypted, run 9 also with cam99_main, then
// the O17-off run 21: cam01_main and cam99_main, encrypted, the setting
// off. Its default runs record cam01_main, or, in pc-kill9 and pi-kill9,
// the host's cameras main streams (cameras, its recommended count, 1 to
// 16), so that MI2 reuses their resumes (G13). A real set is slots 1 to 5
// of cam01_main, slots 1, 3 and 5 encrypted.
func Plan(set string, cameras int) ([]RunPlan, error) {
	switch set {
	case SetRealPC, SetRealPi:
		return realPlan(), nil
	case SetPCKill9, SetPiKill9:
		if cameras < 1 || cameras > maxCameras {
			return nil, fmt.Errorf("mi1: %s records 1 to %d cameras, not %d", set, maxCameras, cameras)
		}
	case SetLabLinuxKill9, SetLabLinuxQEMUReset, SetLabWindowsKill9, SetPCBugcheck, SetPiSysrq:
		cameras = 1
	default:
		return nil, fmt.Errorf("mi1: unknown set %q", set)
	}
	plan := make([]RunPlan, 0, defaultRuns+1)
	for i := 1; i <= defaultRuns; i++ {
		var streams []string
		for cam := 1; cam <= cameras; cam++ {
			streams = append(streams, fmt.Sprintf("cam%02d_main", cam))
		}
		if i == lowBitrateRun {
			streams = append(streams, LowBitrateStream)
		}
		plan = append(plan, RunPlan{Index: i, Encrypted: i%2 == 1, PadLowBitrate: true, Streams: streams})
	}
	return append(plan, RunPlan{Index: defaultRuns + 1, Encrypted: true, Streams: []string{MainStream, LowBitrateStream}}), nil
}

func realPlan() []RunPlan {
	plan := make([]RunPlan, 0, realSlots)
	for slot := 1; slot <= realSlots; slot++ {
		plan = append(plan, RunPlan{Index: slot, Encrypted: slot%2 == 1, PadLowBitrate: true, Streams: []string{MainStream}})
	}
	return plan
}

// Shard is a lab job's part of an automated set's plan (R03): shard 1 is
// runs 1 to 10, the 256 kbps run among them; shard 2 is runs 11 to 21, the
// O17-off run included.
func Shard(plan []RunPlan, shard int) ([]RunPlan, error) {
	if len(plan) != defaultRuns+1 {
		return nil, fmt.Errorf("mi1: a plan of %d runs is not an automated set", len(plan))
	}
	switch shard {
	case 1:
		return plan[:labShard], nil
	case 2:
		return plan[labShard:], nil
	}
	return nil, fmt.Errorf("mi1: shard %d, want 1 or 2", shard)
}

// NextRun is the run of plan that an automated driver records next, after
// the runs done, in order: each run that counts moves to the next
// position, and one that does not (C16, R07) is redone in its position.
// ok is false when every position has a run that counts.
func NextRun(plan []RunPlan, done []results.Run) (next RunPlan, ok bool) {
	pos := 0
	for _, r := range done {
		if Counts(r) {
			pos++
		}
	}
	if pos >= len(plan) {
		return RunPlan{}, false
	}
	return plan[pos], true
}

// RealCut is the real cut that the owner makes next, and its attempt: the
// set's attempts, counted from 1.
type RealCut struct {
	RunPlan
	Attempt int
}

// RealStatus is where a real set stands.
type RealStatus int

// The states of a real set.
const (
	// RealNext: the owner makes the cut that NextRealCut returns.
	RealNext RealStatus = iota
	// RealComplete: every slot has a cut that counts.
	RealComplete
	// RealIncomplete: 8 attempts are spent without 5 cuts that count; the
	// set stops and its verdict is incomplete.
	RealIncomplete
)

// NextRealCut is the next real cut after the cuts done (Review Focus 1,
// G12): the lowest slot without a cut that counts, with that slot's
// encryption, so that a redo keeps its slot; a set stops at 8 attempts.
func NextRealCut(done []results.Run) (RealCut, RealStatus) {
	filled := make([]bool, realSlots+1)
	for _, r := range done {
		if Counts(r) && r.Slot >= 1 && r.Slot <= realSlots {
			filled[r.Slot] = true
		}
	}
	slot := slices.Index(filled[1:], false) + 1
	switch {
	case slot == 0:
		return RealCut{}, RealComplete
	case len(done) >= MaxRealAttempts:
		return RealCut{}, RealIncomplete
	}
	return RealCut{RunPlan: realPlan()[slot-1], Attempt: len(done) + 1}, RealNext
}
