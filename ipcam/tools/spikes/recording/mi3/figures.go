// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"errors"
	"os"
	"path/filepath"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
)

// ramMb is peak bytes in MB of 10^6 bytes: §4.8's unit, that of its RAM
// formula, and the stricter reading.
func ramMb(peak uint64) float64 { return float64(peak) / 1e6 }

// subBps is a camera's sub stream, 512 kbps.
const subBps = 512_000

// figures are the step's figures (budgets.go): the CPU of the machine and
// of each process from start to end, the live load's split over its tiles
// by their bitrates (Proxies), the upload's per 10 Mbps at the rate it
// reached, ipcam-hub's with MediaMTX's per camera and core; the memory's
// peak; the recordings' disk ratio and part fsyncs; the disk writers'
// queue peak from ipcam-hub's log; and the controller's pacing and latency.
func (s *stepRun) figures(start, end usage, peak uint64, res link.Mi3Result, upload LoadReport, riG age.Identity) (Figures, error) {
	if res.LatencySamples == 0 {
		return Figures{}, errors.New("sesiunile live nu au primit niciun cadru")
	}
	if upload.Seconds <= 0 || upload.Bytes <= 0 {
		return Figures{}, errors.New("upload-ul nu a trimis nimic")
	}
	disk, err := measureDisk(filepath.Join(s.dir, "data", "rec"), riG)
	if err != nil {
		return Figures{}, err
	}
	log, err := os.Open(filepath.Join(s.dir, "ipcam-hub.log"))
	if err != nil {
		return Figures{}, err
	}
	defer func() { _ = log.Close() }()
	queuePeak, err := diskQueuePeak(log)
	if err != nil {
		return Figures{}, err
	}
	machine := func(kind string) float64 { return end.pctOfMachine(start, end.cpuSince(start, kind)) }
	loads := map[string]float64{}
	for _, kind := range Loads {
		loads[kind] = machine(kind)
	}
	live := machine(LoadLive) / float64(Sessions(s.r.tier)*(s.cameras*subBps+cameraBps))
	uploaded := float64(upload.Bytes*8) / upload.Seconds
	return Figures{
		CPUPctTotal:        end.pctOfMachine(start, end.busy-start.busy),
		TileSubPct:         live * subBps,
		TileMainPct:        live * cameraBps,
		EncUploadPct:       machine(LoadUpload) * uploadBps / uploaded,
		IngestPerCameraPct: end.pctOfCore(start, end.cpuSince(start, "hub")) / float64(s.cameras),
		RAMMb:              ramMb(peak),
		DiskRatio:          disk.ratio,
		FsyncPerSecPerFile: disk.fsyncPerSec,
		LatencyP95Ms:       res.LatencyP95Ms,
		PacingP99Ms:        res.PacingP99Ms,
		SkippedAUs:         res.SkippedAUs,
		DiskQueuePeakBytes: queuePeak,
		LoadCPUPct:         loads,
	}, nil
}
