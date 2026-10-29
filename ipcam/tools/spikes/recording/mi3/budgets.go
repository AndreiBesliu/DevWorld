// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"slices"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The tiers of §4.8 that MI3 runs on: the owner's PC is H3; a Pi is its
// model's tier, or TierUnknown for a model that the table lacks (C24),
// which is judged as H1b and H1c are.
const (
	TierH3      = "H3"
	TierH1      = "H1"
	TierH1b     = "H1b"
	TierH1c     = "H1c"
	TierUnknown = "unknown"
)

// The keys of Mi3Data.Budgets, one per row of §4.8 that MI3 judges.
const (
	keyCPUTotal        = "cpuTotal"
	keyTileSub         = "tileSub"
	keyTileMain        = "tileMain"
	keyEncUpload       = "encUpload"
	keyRAM             = "ram"
	keyDisk            = "disk"
	keyFsyncPerFile    = "fsyncPerFile"
	keyLatencyP95      = "latencyP95"
	keyIngestPerCamera = "ingestPerCamera"
)

// h3Keys are H3's budgets: every row of §4.8 that MI3 measures but the
// per-camera ingest, which has no H3 figure (G20).
var h3Keys = []string{keyCPUTotal, keyTileSub, keyTileMain, keyEncUpload, keyRAM, keyDisk, keyFsyncPerFile, keyLatencyP95}

// BudgetKeys are the budgets that tier is judged on (§4.8, G20): H3's; on
// H1 also the per-camera ingest; on H1b, H1c and an unknown model, total
// CPU and RAM only.
func BudgetKeys(tier string) []string {
	switch tier {
	case TierH3:
		return slices.Clone(h3Keys)
	case TierH1:
		return append(slices.Clone(h3Keys), keyIngestPerCamera)
	}
	return []string{keyCPUTotal, keyRAM}
}

// Sessions is the tier's simultaneous live sessions (§4.8): 3 on H3, 2 on
// H1, 1 on H1b, H1c and an unknown model.
func Sessions(tier string) int {
	switch tier {
	case TierH3:
		return 3
	case TierH1:
		return 2
	}
	return 1
}

// The limits of §4.8. CPU percentages are of the whole machine, all its
// logical CPUs, as the total is, except the ingest's, which is of one core.
const (
	maxCPUTotalPct        = 60
	maxTileSubPct         = 1
	maxTileMainPct        = 3
	maxEncUploadPct       = 5 // per 10 Mbps
	maxIngestPerCameraPct = 3
	ramBaseMb             = 250
	ramPerCameraMb        = 20
	ramPerSessionMb       = 15
	maxDiskRatio          = 1.03 // × bitrate, clear cameras only (R02)
	maxFsyncPerSec        = 1    // per open file
	maxLatencyP95Ms       = 150
)

// limit is key's limit on tier with cameras.
func limit(key, tier string, cameras int) float64 {
	switch key {
	case keyCPUTotal:
		return maxCPUTotalPct
	case keyTileSub:
		return maxTileSubPct
	case keyTileMain:
		return maxTileMainPct
	case keyEncUpload:
		return maxEncUploadPct
	case keyIngestPerCamera:
		return maxIngestPerCameraPct
	case keyRAM:
		return float64(ramBaseMb + ramPerCameraMb*cameras + ramPerSessionMb*Sessions(tier))
	case keyDisk:
		return maxDiskRatio
	case keyFsyncPerFile:
		return maxFsyncPerSec
	case keyLatencyP95:
		return maxLatencyP95Ms
	}
	return 0
}

// Figures are what one step of MI3 measured on the host, and the
// controller's pacing of it (R07). CPU is a percentage of the whole
// machine, as §4.8's total is: TileSubPct and TileMainPct per tile sent,
// EncUploadPct per 10 Mbps uploaded; IngestPerCameraPct is a percentage of
// one core per camera (§4.8: "dintr-un nucleu"). RAMMb is the peak of the
// hub's processes and the loads together, in MB of 10^6 bytes, §4.8's
// unit. DiskRatio is file bytes ÷ stream bytes, per mode, "clear" and
// "encrypted". LoadCPUPct is each load's CPU, with its children's, as a
// percentage of the whole machine (C1-R19).
type Figures struct {
	CPUPctTotal        float64
	TileSubPct         float64
	TileMainPct        float64
	EncUploadPct       float64
	IngestPerCameraPct float64
	RAMMb              float64
	DiskRatio          map[string]float64
	FsyncPerSecPerFile float64
	LatencyP95Ms       float64
	PacingP99Ms        int64
	SkippedAUs         int
	DiskQueuePeakBytes int64
	LoadCPUPct         map[string]float64
}

// value is f's measurement of key; ok is false when f has none, as for
// the disk without a clear camera.
func (f Figures) value(key string) (v float64, ok bool) {
	switch key {
	case keyCPUTotal:
		return f.CPUPctTotal, true
	case keyTileSub:
		return f.TileSubPct, true
	case keyTileMain:
		return f.TileMainPct, true
	case keyEncUpload:
		return f.EncUploadPct, true
	case keyIngestPerCamera:
		return f.IngestPerCameraPct, true
	case keyRAM:
		return f.RAMMb, true
	case keyDisk:
		v, ok = f.DiskRatio["clear"]
		return v, ok
	case keyFsyncPerFile:
		return f.FsyncPerSecPerFile, true
	case keyLatencyP95:
		return f.LatencyP95Ms, true
	}
	return 0, false
}

// Budgets are f's budgets on tier with cameras, each with its value, its
// limit and whether the value is within it; a budget that f did not
// measure is absent.
func Budgets(tier string, cameras int, f Figures) map[string]results.Budget {
	out := map[string]results.Budget{}
	for _, key := range BudgetKeys(tier) {
		if v, ok := f.value(key); ok {
			lim := limit(key, tier, cameras)
			out[key] = results.Budget{Value: v, Limit: lim, Pass: v <= lim}
		}
	}
	return out
}

// Valid reports whether a step counts (R07): the controller's send p99 at
// most 100 ms and no skipped access unit (validPacing). One that does not
// is redone and never judged.
func Valid(f Figures) bool { return validPacing(f.PacingP99Ms, f.SkippedAUs) }

// validPacing reports whether the controller sent a step evenly (R07): its
// p99 at most 100 ms and no skipped access unit.
func validPacing(p99Ms int64, skippedAUs int) bool {
	return p99Ms <= mi1.MaxPacingP99Ms && skippedAUs == 0
}

// passes reports whether a valid step met every budget of its tier.
func passes(tier string, cameras int, f Figures) bool {
	b := Budgets(tier, cameras, f)
	for _, key := range BudgetKeys(tier) {
		if !b[key].Pass {
			return false
		}
	}
	return true
}

// dataOf is the envelope's data of a first hour on tier at cameras that
// measured f.
func dataOf(tier string, cameras int, f Figures) results.Mi3Data {
	return results.Mi3Data{
		Tier: tier, Cameras: cameras, CPUPctTotal: f.CPUPctTotal, RAMMb: f.RAMMb, Budgets: Budgets(tier, cameras, f),
		DiskRatio: f.DiskRatio, FsyncPerSecPerFile: f.FsyncPerSecPerFile, LatencyP95Ms: f.LatencyP95Ms,
		PacingP99Ms: f.PacingP99Ms, SkippedAUs: f.SkippedAUs, DiskQueuePeakBytes: f.DiskQueuePeakBytes, LoadCPUPct: f.LoadCPUPct,
	}
}
