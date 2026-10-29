// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi2

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// PlanB is G11's plan B (§16.4).
const PlanB = "ingest inversat (MediaMTX trage camera)"

// The thresholds of MI2 (HUB-31): on each owner device, 20 resumes after a
// kill -9 of ipcam-hub, each within 10 s, and 72 h without a crash.
const (
	MaxResumeMs = 10_000
	Kill9Runs   = 20
	RunHours    = 72
)

// restartNote is C07's caveat, which the report states.
const restartNote = "Repornirea ipcam-hub făcută de recspike este o limită inferioară pentru recuperarea prin SCM (Windows) și systemd (Linux) din Faza 1 (C07)."

// piDeferral is why the Pi's part moves to Faza 0-bis (G31).
const piDeferral = "Pi armv6"

// owners are the devices of MI2 and their kill9 sets of MI1 (G13).
var owners = []struct{ host, kill9 string }{
	{"pc", mi1.SetPCKill9},
	{"pi", mi1.SetPiKill9},
}

// The states of a host's part of G11.
type hostState int

const (
	hostPass hostState = iota
	hostFail
	hostIncomplete
)

// Judge is G11 (C17, HUB-31). On each owner device, the 20 resumes are the
// ResumeMs of its kill9 set's counted default runs, each once (G13,
// mi1.DefaultRuns), and the 72 h run is its MI2 envelope. More than 20
// counted resumes, as when a run is redone under another attempt and both
// count, are all judged, and a note names the excess. G11 passes when
// every resume is ≤ 10 s and the 72 h ran with 0 crashes of ipcam-hub;
// otherwise it fails with the reverse-ingest plan B, also with other parts
// missing (G27). A missing or short part leaves it without a verdict, and a
// missing Pi part defers it to Faza 0-bis when the Pi is unsupported. Its
// MediaMTX restarts (HUB-13), its window and, on an SD card, the card's
// states are information rows; its Notes say that recspike's restart is a
// lower bound (C07).
func Judge(envs []results.Envelope) results.Judgement {
	j := results.Judgement{Gate: "G11", Notes: []string{restartNote}}
	piUnsupported := false
	for _, e := range envs {
		if e.Host == "pi" && e.PiModel != nil && e.PiModel.Known && !e.PiModel.Supported {
			piUnsupported = true
		}
	}
	var failed, incomplete, deferred bool
	for _, o := range owners {
		switch judgeHost(&j, o.host, o.kill9, envs) {
		case hostFail:
			failed = true
		case hostIncomplete:
			if o.host == "pi" && piUnsupported {
				deferred = true
				j.Reasons = append(j.Reasons, "pi: amânat ("+piDeferral+")")
			} else {
				incomplete = true
			}
		}
	}
	switch {
	case failed:
		j.Verdict, j.PlanB = results.Fail, PlanB
	case incomplete:
		j.Verdict = results.Incomplete
	case deferred:
		j.Verdict = results.Deferred(piDeferral)
	default:
		j.Verdict = results.Pass
	}
	return j
}

// judgeHost judges host's resumes and its 72 h run, and adds their rows and
// reasons to j.
func judgeHost(j *results.Judgement, host, kill9 string, envs []results.Envelope) hostState {
	resumes := judgeResumes(j, host, kill9, envs)
	run := judgeRun(j, host, envs)
	switch {
	case resumes == hostFail || run == hostFail:
		return hostFail
	case resumes == hostIncomplete || run == hostIncomplete:
		return hostIncomplete
	}
	return hostPass
}

// judgeResumes judges the resumes of host's kill9 set.
func judgeResumes(j *results.Judgement, host, kill9 string, envs []results.Envelope) hostState {
	var measured int
	var slowest int64
	failed := false
	for _, r := range mi1.DefaultRuns(envs, kill9) {
		if r.ResumeMs == nil {
			continue
		}
		measured++
		slowest = max(slowest, *r.ResumeMs)
		if *r.ResumeMs > MaxResumeMs {
			failed = true
			j.Reasons = append(j.Reasons, fmt.Sprintf("%s: rularea %s: înregistrarea a reluat în %s, peste %s",
				kill9, r.RunID, mi1.SecondsText(*r.ResumeMs), mi1.WholeSecondsText(MaxResumeMs)))
		}
	}
	complete := measured >= Kill9Runs
	if excess := measured - Kill9Runs; excess > 0 {
		j.Notes = append(j.Notes, fmt.Sprintf("%s: %d de reluări numărate, cu %d peste cele %d ale setului; fiecare este judecată față de %s.",
			kill9, measured, excess, Kill9Runs, mi1.WholeSecondsText(MaxResumeMs)))
	}
	row := results.Row{Code: "MI2 " + host + ": reluarea după kill -9", Value: "lipsește", Limit: "≤ " + mi1.WholeSecondsText(MaxResumeMs), Source: host}
	switch {
	case measured > Kill9Runs:
		// «21 din 20» would read as a contradiction; the note says why.
		row.Value = fmt.Sprintf("%s (%d de reluări)", mi1.SecondsText(slowest), measured)
	case measured > 0:
		row.Value = fmt.Sprintf("%s (%d din %d)", mi1.SecondsText(slowest), measured, Kill9Runs)
	}
	row.Pass = verdict(!failed, complete)
	j.Rows = append(j.Rows, row)
	switch {
	case failed:
		return hostFail
	case !complete:
		j.Reasons = append(j.Reasons, fmt.Sprintf("%s: %d din %d reluări măsurate în %s", host, measured, Kill9Runs, kill9))
		return hostIncomplete
	}
	return hostPass
}

// judgeRun judges host's 72 h run: the one with the most crashes, or, of
// runs without any, the longest.
func judgeRun(j *results.Judgement, host string, envs []results.Envelope) hostState {
	var run *results.Envelope
	for i, e := range envs {
		if e.Code != "MI2" || e.Host != host || e.MI2 == nil {
			continue
		}
		if run == nil || e.MI2.Crashes72h > run.MI2.Crashes72h ||
			(e.MI2.Crashes72h == run.MI2.Crashes72h && e.MI2.Hours > run.MI2.Hours) {
			run = &envs[i]
		}
	}
	prefix := "MI2 " + host + ": "
	crashRow := results.Row{Code: prefix + "crash-uri ipcam-hub în 72 h", Value: "lipsește", Limit: "0", Source: host}
	if run == nil {
		j.Rows = append(j.Rows, crashRow)
		j.Reasons = append(j.Reasons, host+": lipsește proba de 72 h")
		return hostIncomplete
	}
	d := run.MI2
	complete := d.Hours >= RunHours
	crashRow.Value = fmt.Sprintf("%d (în %s h)", d.Crashes72h, decimal(d.Hours, 1))
	crashRow.Pass = verdict(d.Crashes72h == 0, complete)
	j.Rows = append(j.Rows, crashRow,
		results.Row{Code: prefix + "reporniri MediaMTX (HUB-13)", Value: strconv.Itoa(d.ChildRestarts), Source: host},
		results.Row{Code: prefix + "fereastra păstrată", Value: decimal(d.WindowHours, 2) + " h", Source: host})
	if d.OnSdCard {
		j.Rows = append(j.Rows, results.Row{Code: prefix + "starea cardului SD", Value: cardText(d.CardStates), Source: host})
	}
	if len(d.Invalidated) > 0 {
		j.Rows = append(j.Rows, results.Row{Code: prefix + "rulări refăcute", Value: strings.Join(d.Invalidated, "; "), Source: host})
	}
	switch {
	case d.Crashes72h > 0:
		j.Reasons = append(j.Reasons, fmt.Sprintf("%s: rularea %s: %d crash-uri ipcam-hub în %s h", run.Set, run.RunID, d.Crashes72h, decimal(d.Hours, 1)))
		return hostFail
	case !complete:
		j.Reasons = append(j.Reasons, fmt.Sprintf("%s: proba de 72 h a rulat doar %s h", host, decimal(d.Hours, 1)))
		return hostIncomplete
	}
	return hostPass
}

// cardNames are the card's states in the report's Romanian (§13.1).
var cardNames = map[string]string{"ok": "OK", "degraded": "DEGRADAT", "failed": "EȘUAT"}

// cardText is the card's states in order: "OK → DEGRADAT".
func cardText(states []results.CardState) string {
	if len(states) == 0 {
		return "nicio stare raportată"
	}
	names := make([]string, len(states))
	for i, s := range states {
		names[i] = cardNames[s.State]
		if names[i] == "" {
			names[i] = s.State
		}
	}
	return strings.Join(names, " → ")
}

// verdict is a row's pass value: false when ok fails, true when ok holds on
// a complete part, and none while it is incomplete.
func verdict(ok, complete bool) *bool {
	switch {
	case !ok:
		return new(false)
	case complete:
		return new(true)
	}
	return nil
}
