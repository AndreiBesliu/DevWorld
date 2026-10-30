// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
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
// queue peak from ipcam-hub's log; the controller's pacing and latency;
// and why the step's load did not run for its whole window, if it did not
// (shortLoad).
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
		Invalid:            s.shortLoad(res.LatencySamples, disk.seconds),
	}, nil
}

// minLoadShare is the share of its window that a step's load must cover
// to count (mis-2).
const minLoadShare = 0.95

// shortLoad says, in Romanian, why the step's load did not run for its
// whole window (mis-2), or "": the live sessions received fewer than 95 %
// of their tiles' frames over the window (received, the controller's
// count), a camera recorded parts for less than 95 % of it (recorded, by
// stream), or MediaMTX restarted. A tile that stops, a camera that
// MediaMTX dropped, or a restart leaves a lighter load than HUB-32 asks
// for, so the step is redone rather than judged.
func (s *stepRun) shortLoad(received int, recorded map[string]float64) string {
	window := s.r.o.window(s.minutes).Seconds()
	mains, tiles := streams(s.cameras)
	fps := map[string]int{}
	for _, st := range controller.Streams("") {
		fps[st.Path] = st.FPS
	}
	frames := 0
	for _, tile := range tiles {
		frames += fps[tile]
	}
	var why []string
	if want := int(float64(Sessions(s.r.tier)*frames) * window); float64(received) < minLoadShare*float64(want) {
		why = append(why, fmt.Sprintf("sesiunile live au primit %d de cadre din cele %d ale ferestrei pasului (sub 95 %%)", received, want))
	}
	for _, cam := range mains {
		if got := recorded[cam]; got < minLoadShare*window {
			why = append(why, fmt.Sprintf("camera %s a înregistrat %s s din cele %s s ale ferestrei pasului (sub 95 %%)",
				cam, results.Decimal(got, 0), results.Decimal(window, 0)))
		}
	}
	switch n := s.childRestarts.Load(); {
	case n == 1:
		why = append(why, "MediaMTX a repornit în timpul pasului")
	case n > 1:
		why = append(why, fmt.Sprintf("MediaMTX a repornit de %d ori în timpul pasului", n))
	}
	return strings.Join(why, "; ")
}
