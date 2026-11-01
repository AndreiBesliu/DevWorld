// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi2

import (
	"fmt"
	"math"
	"slices"
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

// maxNotRecordedShare is how much of a 72 h run a camera may go without a
// new part (mis-1): more, and the run measured an ipcam-hub that did not
// record, which says nothing of its stability.
const maxNotRecordedShare = 0.01

// restartNote is C07's caveat, which the report states.
const restartNote = "Repornirea ipcam-hub făcută de recspike este o limită inferioară pentru recuperarea prin SCM (Windows) și systemd (Linux) din Faza 1 (C07)."

// piDeferral is why the Pi's part moves to Faza 0-bis (G31).
const piDeferral = "Pi armv6"

// ResumeRowCode is the code of host's resume row, and CardRowCode that of
// its SD card's states, which the report shows next to it (G16).
func ResumeRowCode(host string) string { return "MI2 " + host + ": reluarea după kill -9" }

// CardRowCode is the code of host's SD card row (ResumeRowCode).
func CardRowCode(host string) string { return "MI2 " + host + ": starea cardului SD" }

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
	row := results.Row{Code: ResumeRowCode(host), Value: "lipsește", Limit: "≤ " + mi1.WholeSecondsText(MaxResumeMs), Source: host}
	switch {
	case measured > Kill9Runs:
		// «21 din 20» would read as a contradiction; the note says why.
		row.Value = fmt.Sprintf("%s (%d de reluări)", mi1.SecondsText(slowest), measured)
	case measured > 0:
		row.Value = fmt.Sprintf("%s (%d din %d)", mi1.SecondsText(slowest), measured, Kill9Runs)
	}
	row.Pass = results.RowPass(!failed, complete)
	j.Rows = append(j.Rows, row)
	switch {
	case failed:
		return hostFail
	case !complete:
		j.Reasons = append(j.Reasons, fmt.Sprintf("%s: %d din %d reluări măsurate în %s", host, measured, Kill9Runs, kill9))
		j.AddOpen(results.Label("MI1", kill9, host), fmt.Sprintf("%d din %d reluări măsurate", measured, Kill9Runs))
		return hostIncomplete
	}
	return hostPass
}

// judgeRun judges host's 72 h run: the one with the most crashes, its own
// and its redone runs' (G27), or, of runs with as many, one that recorded
// (mis-1), then the longest.
func judgeRun(j *results.Judgement, host string, envs []results.Envelope) hostState {
	var run *results.Envelope
	for i, e := range envs {
		if e.Code != "MI2" || e.Host != host || e.MI2 == nil {
			continue
		}
		if run == nil || betterRun(e.MI2, run.MI2) {
			run = &envs[i]
		}
	}
	prefix, part := "MI2 "+host+": ", results.Label("MI2", "", host)
	crashRow := results.Row{Code: prefix + "crash-uri ipcam-hub în 72 h", Value: "lipsește", Limit: "0", Source: host}
	if run == nil {
		j.Rows = append(j.Rows, crashRow)
		j.Reasons = append(j.Reasons, host+": lipsește proba de 72 h")
		j.AddOpen(part, "lipsește proba de 72 h")
		return hostIncomplete
	}
	d := run.MI2
	complete := d.Hours >= RunHours
	crashRow.Value = fmt.Sprintf("%d (în %s h)", d.Crashes72h, results.Decimal(d.Hours, 1))
	if d.CrashesInvalidated > 0 {
		crashRow.Value += ", plus " + CrashText(d.CrashesInvalidated) + " în rulările refăcute"
	}
	crashRow.Pass = results.RowPass(crashes(d) == 0, complete)
	maxNotRecorded := maxNotRecordedMinutes(d)
	recorded := d.NotRecordingMinutes <= maxNotRecorded
	j.Rows = append(j.Rows, crashRow,
		results.Row{Code: prefix + "timp fără înregistrare (camera cea mai afectată)", Value: results.Decimal(d.NotRecordingMinutes, 1) + " min",
			Limit: "≤ 1 % din rulare (" + results.Decimal(maxNotRecorded, 1) + " min)", Source: host, Pass: results.RowPass(recorded, complete)},
		results.Row{Code: prefix + "reporniri MediaMTX (HUB-13)", Value: strconv.Itoa(d.ChildRestarts), Source: host},
		results.Row{Code: prefix + "fereastra păstrată", Value: results.Decimal(d.WindowHours, 2) + " h", Source: host})
	if d.OnSdCard {
		j.Rows = append(j.Rows, results.Row{Code: CardRowCode(host), Value: cardText(d.CardStates), Source: host})
		if crashes(d) == 0 && slices.ContainsFunc(d.CardStates, func(s results.CardState) bool { return s.State == "failed" }) {
			j.Notes = append(j.Notes, host+": cardul SD a ajuns EȘUAT în proba de 72 h, iar de atunci ipcam-hub a oprit înregistrarea pe toate "+
				"camerele (T40): „0 crash-uri” acoperă și orele de după, măsurate fără înregistrare.")
		}
	}
	if len(d.Invalidated) > 0 {
		j.Rows = append(j.Rows, results.Row{Code: prefix + "rulări refăcute", Value: strings.Join(d.Invalidated, "; "), Source: host})
	}
	if d.Crashes72h > 0 {
		j.Reasons = append(j.Reasons, fmt.Sprintf("%s: rularea %s: %d crash-uri ipcam-hub în %s h", run.Set, run.RunID, d.Crashes72h, results.Decimal(d.Hours, 1)))
	}
	if d.CrashesInvalidated > 0 {
		j.Reasons = append(j.Reasons, fmt.Sprintf("%s: %s ipcam-hub în rulările refăcute înainte de %s", run.Set, CrashText(d.CrashesInvalidated), run.RunID))
	}
	switch {
	case crashes(d) > 0:
		return hostFail
	case !recorded:
		j.Reasons = append(j.Reasons, fmt.Sprintf("%s: rularea %s: o cameră nu s-a înregistrat %s min din %s h, peste 1 %% din rulare: "+
			"proba a măsurat un ipcam-hub care nu înregistra. Refaceți proba cu reccontroller -local pornit tot timpul",
			run.Set, run.RunID, results.Decimal(d.NotRecordingMinutes, 1), results.Decimal(d.Hours, 1)))
		j.AddOpen(part, fmt.Sprintf("o cameră nu s-a înregistrat %s min din %s h, peste 1 %% din rulare: lăsați reccontroller -local pornit tot timpul",
			results.Decimal(d.NotRecordingMinutes, 1), results.Decimal(d.Hours, 1)))
		return hostIncomplete
	case !complete:
		j.Reasons = append(j.Reasons, fmt.Sprintf("%s: proba de 72 h a rulat doar %s h", host, results.Decimal(d.Hours, 1)))
		j.AddOpen(part, fmt.Sprintf("proba de 72 h a rulat doar %s h", results.Decimal(d.Hours, 1)))
		return hostIncomplete
	}
	return hostPass
}

// crashes is a 72 h run's crashes and its redone runs' (mis-3).
func crashes(d *results.Mi2Data) int { return d.Crashes72h + d.CrashesInvalidated }

// betterRun reports whether the 72 h run d judges G11 before the run best
// (judgeRun): more crashes, or as many and recorded while best did not
// record (mis-1), or then longer.
func betterRun(d, best *results.Mi2Data) bool {
	if c, cb := crashes(d), crashes(best); c != cb {
		return c > cb
	}
	if r, rb := d.NotRecordingMinutes <= maxNotRecordedMinutes(d), best.NotRecordingMinutes <= maxNotRecordedMinutes(best); r != rb {
		return r
	}
	return d.Hours > best.Hours
}

// maxNotRecordedMinutes is 1 % of the run d, in minutes to a tenth, as the
// envelope's NotRecordingMinutes.
func maxNotRecordedMinutes(d *results.Mi2Data) float64 {
	return math.Round(maxNotRecordedShare*d.Hours*60*10) / 10
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
