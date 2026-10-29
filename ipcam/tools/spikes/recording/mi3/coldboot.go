// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"fmt"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The cold-boot row of §4.8: boot to every camera recording, at most 20 s,
// judged on H3 from the maximum of the 20 runs of the PC's pc-bugcheck set
// (Task 7), which record one camera.
const (
	MaxColdBootMs = 20_000
	coldBootRuns  = 20
)

// ColdBootPlanB is the cold-boot row's plan B: the camera number does not
// change a boot's time, so a failure leaves the PC's recommended number
// and the Task 14 PR proposes the fix as a Technical Decision, as R02 does
// for the encrypted disk ratio.
const ColdBootPlanB = "pornirea la rece: Decizie tehnică în PR"

// coldBootWhat is what the row measured: the run's one camera, and a boot
// that includes the OS's.
const coldBootWhat = "1 cameră, cam01_main; uptime la evenimentul recording, deci include pornirea "

// coldBoots are the ColdBootMs of set's counted default runs, each once
// (mi1.DefaultRuns), and the run of the largest.
func coldBoots(envs []results.Envelope, set string) (measured int, slowest int64, slowestRun string) {
	for _, r := range mi1.DefaultRuns(envs, set) {
		if r.ColdBootMs == nil {
			continue
		}
		measured++
		if *r.ColdBootMs > slowest || slowestRun == "" {
			slowest, slowestRun = *r.ColdBootMs, r.RunID
		}
	}
	return measured, slowest, slowestRun
}

// coldBootValue is the row's value: the maximum and how many runs it is of.
func coldBootValue(measured int, slowest int64) string {
	if measured == 0 {
		return "lipsește"
	}
	return fmt.Sprintf("%s (%d din %d)", mi1.SecondsText(slowest), measured, coldBootRuns)
}

// judgeColdBoot judges the PC's cold-boot row (§4.8, judged on H3) from the
// owner's pc-bugcheck runs: it fails above 20 s on the maximum, also with
// runs missing (G27), and is complete with the set's 20 runs.
func judgeColdBoot(j *results.Judgement, envs []results.Envelope) hostState {
	measured, slowest, run := coldBoots(envs, mi1.SetPCBugcheck)
	failed := measured > 0 && slowest > MaxColdBootMs
	complete := measured >= coldBootRuns
	j.Rows = append(j.Rows, results.Row{
		Code:  "MI3 pc: pornire la rece (" + coldBootWhat + "Windows)",
		Value: coldBootValue(measured, slowest), Limit: "≤ " + mi1.WholeSecondsText(MaxColdBootMs), Source: "pc",
		Pass: verdict(!failed, complete),
	})
	switch {
	case failed:
		j.Reasons = append(j.Reasons, fmt.Sprintf("%s: rularea %s: pornirea la rece a durat %s, peste %s",
			mi1.SetPCBugcheck, run, mi1.SecondsText(slowest), mi1.WholeSecondsText(MaxColdBootMs)))
		return hostFail
	case !complete:
		j.Reasons = append(j.Reasons, fmt.Sprintf("pc: %d din %d porniri la rece măsurate în %s", measured, coldBootRuns, mi1.SetPCBugcheck))
		return hostIncomplete
	}
	return hostPass
}

// coldBootInfo adds the Pi's cold boot from its pi-sysrq runs, information
// only: G20 judges only CPU and RAM on the Pi.
func coldBootInfo(j *results.Judgement, envs []results.Envelope) {
	measured, slowest, _ := coldBoots(envs, mi1.SetPiSysrq)
	j.Rows = append(j.Rows, results.Row{
		Code:  "MI3 pi: pornire la rece (" + coldBootWhat + "Linux; informativ)",
		Value: coldBootValue(measured, slowest), Source: "pi",
	})
}
