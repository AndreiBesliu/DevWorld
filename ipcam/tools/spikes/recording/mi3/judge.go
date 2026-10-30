// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// SetFor is MI3's set on host: pc-mi3 or pi-mi3.
func SetFor(host string) string { return host + "-mi3" }

// piDeferral is why the Pi's part moves to Faza 0-bis (G31).
const piDeferral = "Pi armv6"

// The notes of every MI3 judgement: what §4.8 asks that MI3 does not
// judge (R02, O17, the timeline row).
const (
	encryptedDiskNote = "Disc ÷ bitrate la camerele criptate se raportează, nu se judecă (R02): umplutura de la sfârșitul părților ține de format, iar " +
		"PR-ul raportului propune formularea „≤ 1,03 × în clar; ≤ 1,05 × criptat”. La o cameră criptată sub 512 kbps, cu „Pierdere minimă la pană de " +
		"curent” pornită (O17), fiecare parte ocupă o bucată age de 64 KiB: ≤ 5,7 GB pe zi la părți de 1 s."
	timelineNote = "Rândul de timeline din §4.8 (24 h dintr-un catalog de 1 an × 16 camere, ≤ 50 ms) nu se judecă aici: cere part_index și un catalog " +
		"de 1 an (HUB-20, Faza 1), iar catalogul minim din c1 are doar segmente și goluri pe câteva zile."
)

// timelineRow is §4.8's timeline row, which c1 does not measure
// (timelineNote).
const timelineRow = "timeline 24 h (catalog de 1 an × 16 camere)"

// sixteenRow adds the PC's row of 16 cameras (R07, information): measured,
// with its hour, when MI3 ran it on a PC with 6 cores or more (pc, the PC's
// latest MI3 envelope); else an estimate, with why the hour has no result
// when it stopped.
func sixteenRow(j *results.Judgement, pc *results.Envelope) {
	r := results.Row{Code: "MI3 pc: 16 camere (≥ 6 nuclee)", Value: "estimare, nemăsurată", Source: "pc"}
	switch {
	case pc == nil:
	case pc.MI3.Sixteen != nil:
		r.Value = "măsurat: " + stepText(*pc.MI3.Sixteen)
	case pc.MI3.SixteenStopped != "":
		r.Value += " (" + pc.MI3.SixteenStopped + ")"
	}
	j.Rows = append(j.Rows, r)
}

// The states of a part of G12.
type hostState int

const (
	hostPass hostState = iota
	hostFail
	hostIncomplete
)

// loadNames are the loads in the report's Romanian (G20).
var loadNames = map[string]string{LoadLive: "live", LoadPlayback: "de redare", LoadExport: "de export", LoadUpload: "de upload"}

// rowNames are the budgets' rows in the report's Romanian (§4.8).
var rowNames = map[string]string{
	keyCPUTotal:        "CPU total",
	keyTileSub:         "CPU per placă trimisă (sub)",
	keyTileMain:        "CPU per placă trimisă (main)",
	keyEncUpload:       "CPU criptare + upload, per 10 Mbps",
	keyIngestPerCamera: "CPU ingest + înregistrare per cameră, dintr-un nucleu",
	keyRAM:             "RAM",
	keyDisk:            "disc ÷ bitrate, camere în clar",
	keyFsyncPerFile:    "părți închise pe secundă per fișier deschis (MTX-1: un fsync la fiecare parte)",
	keyLatencyP95:      "latența adăugată, p95",
}

// Judge is G12 (C17, HUB-32). On each owner device, its latest MI3
// envelope: the first valid hour at the recommended count passes when every
// budget of its tier (BudgetKeys) is met, each limit recomputed, never read;
// when one is not, G12 fails and the number drops to the count that the
// steps confirmed with a full hour, or to 1 (O16): "<host>: numărul
// recomandat devine <n>", or, with the steps still open, "<host>: numărul
// recomandat coboară". A first hour without a valid attempt (R07) leaves
// the host without a verdict, as does a missing envelope; a missing Pi part
// defers G12 to Faza 0-bis when the Pi is unsupported. The PC's cold boot
// (coldboot.go) fails G12 above 20 s with its Technical Decision, the number
// unchanged; the Pi's is information. A measured failure decides even with
// other parts missing (G27), as when a step after it could not be measured
// (StepsStopped). Each host's count that G12 leaves is in Recommended: the
// first hour's when it passes, the steps' when they settled after it
// failed. The steps, with the one that stopped them, the disk writers'
// queue peak next to the RAM (C1-R17), each load's CPU (C1-R19), the disk
// ratio of each mode that no budget judges (the encrypted one, R02, and on
// H1b and H1c the clear one), the pacing, the storage, the PC's 16
// cameras, measured or an estimate (R07), and §4.8's timeline row,
// deferred to HUB-20, are information rows; the proxies (G20) and what
// §4.8 asks that MI3 does not judge are Notes.
func Judge(envs []results.Envelope) results.Judgement {
	j := results.Judgement{Gate: "G12"}
	piUnsupported := false
	for _, e := range envs {
		if e.Host == "pi" && e.PiModel != nil && e.PiModel.Known && !e.PiModel.Supported {
			piUnsupported = true
		}
	}
	var planB []string
	var failed, incomplete, deferred bool
	for _, host := range []string{"pc", "pi"} {
		state, hostPlanB := judgeHost(&j, host, envs)
		switch {
		case state == hostFail:
			failed = true
			planB = append(planB, hostPlanB)
		case state == hostIncomplete && host == "pi" && piUnsupported:
			deferred = true
			j.Reasons = append(j.Reasons, "pi: amânat ("+piDeferral+")")
		case state == hostIncomplete:
			incomplete = true
		}
	}
	sixteenRow(&j, latest(envs, "pc"))
	switch judgeColdBoot(&j, envs) {
	case hostFail:
		failed = true
		planB = append(planB, ColdBootPlanB)
	case hostIncomplete:
		incomplete = true
	}
	coldBootInfo(&j, envs)
	j.Rows = append(j.Rows, results.Row{Code: "MI3: " + timelineRow, Value: "amânat: HUB-20, Faza 1", Limit: "≤ 50 ms"})
	j.Notes = append(j.Notes, encryptedDiskNote, timelineNote)
	switch {
	case failed:
		j.Verdict, j.PlanB = results.Fail, strings.Join(planB, "; ")
	case incomplete:
		j.Verdict = results.Incomplete
	case deferred:
		j.Verdict = results.Deferred(piDeferral)
	default:
		j.Verdict = results.Pass
	}
	return j
}

// latest is host's latest MI3 envelope, by its run id's number, or nil.
func latest(envs []results.Envelope, host string) *results.Envelope {
	var out *results.Envelope
	best := -1
	for i, e := range envs {
		if e.Code != "MI3" || e.Host != host || e.MI3 == nil {
			continue
		}
		n, _ := strconv.Atoi(e.RunID[strings.LastIndexByte(e.RunID, '-')+1:])
		if n >= best {
			best, out = n, &envs[i]
		}
	}
	return out
}

// judgeHost judges host's MI3 envelope and adds its rows, reasons and
// notes to j; a failure comes with its plan B.
func judgeHost(j *results.Judgement, host string, envs []results.Envelope) (hostState, string) {
	prefix := "MI3 " + host + ": "
	e := latest(envs, host)
	if e == nil {
		j.Rows = append(j.Rows, results.Row{Code: prefix + "rezultat", Value: "lipsește", Source: host})
		j.Reasons = append(j.Reasons, host+": lipsește proba MI3")
		return hostIncomplete, ""
	}
	d := e.MI3
	j.Rows = append(j.Rows, results.Row{Code: prefix + "nivel și camere", Source: host,
		Value: fmt.Sprintf("%s, %s, %s", d.Tier, camerasText(d.Cameras), SessionsText(Sessions(d.Tier)))})
	valid := validPacing(d.PacingP99Ms, d.SkippedAUs) && d.InvalidReason == ""
	failed, missing := false, false
	for _, key := range BudgetKeys(d.Tier) {
		lim := limit(key, d.Tier, d.Cameras)
		r := results.Row{Code: prefix + rowNames[key], Value: "lipsește", Limit: limitText(key, lim), Source: host}
		b, ok := d.Budgets[key]
		switch {
		case ok:
			r.Value = valueText(key, b.Value)
			if valid {
				r.Pass = new(b.Value <= lim)
				if b.Value > lim {
					failed = true
					j.Reasons = append(j.Reasons, fmt.Sprintf("%s: %s %s la %s (limita %s)", host, rowNames[key], r.Value, camerasText(d.Cameras), r.Limit))
				}
			}
		case valid:
			missing = true
		}
		j.Rows = append(j.Rows, r)
		if key == keyRAM && d.DiskQueuePeakBytes > 0 {
			j.Rows = append(j.Rows, results.Row{Code: prefix + "coada de scriere pe disc a MediaMTX, vârf (C1-R17, informativ)", Source: host,
				Value: results.Decimal(float64(d.DiskQueuePeakBytes)/1e6, 1) + " MB, suma vârfurilor fiecărei înregistrări (o limită superioară)"})
		}
	}
	infoRows(j, prefix, host, e)
	if note, ok := exportNote(host, d); ok && valid {
		j.Notes = append(j.Notes, note)
	}
	switch {
	case !valid:
		j.Reasons = append(j.Reasons, fmt.Sprintf("%s: ora la %s nu are nicio rulare validă (%s)", host, camerasText(d.Cameras), invalidText(d)))
		return hostIncomplete, ""
	case failed:
		n, settled := recommended(d.Steps)
		if !settled {
			reason := host + ": pașii de coborâre nu s-au încheiat"
			if d.StepsStopped != "" {
				reason += " (" + d.StepsStopped + ")"
			}
			j.Reasons = append(j.Reasons, reason)
			return hostFail, host + ": numărul recomandat coboară; pașii nu s-au încheiat"
		}
		j.Recommend(host, n)
		return hostFail, fmt.Sprintf("%s: numărul recomandat devine %d", host, n)
	case missing:
		j.Reasons = append(j.Reasons, host+": lipsesc bugete ale nivelului "+d.Tier)
		return hostIncomplete, ""
	}
	j.Recommend(host, d.Cameras)
	return hostPass, ""
}

// exportNote is the Note of C1-R19 on host's first hour d: total CPU fails,
// but would pass without the export's share. The export runs at full speed
// for the whole step, a floor of CPU that no camera count lowers, so the
// result comes from the export, and the report's PR (Task 14) proposes a
// Technical Decision instead of the lower count alone.
func exportNote(host string, d *results.Mi3Data) (string, bool) {
	b, measured := d.Budgets[keyCPUTotal]
	export, ran := d.LoadCPUPct[LoadExport]
	lim := limit(keyCPUTotal, d.Tier, d.Cameras)
	if !measured || !ran || b.Value <= lim || b.Value-export > lim {
		return "", false
	}
	return fmt.Sprintf("%s: CPU-ul total de %s %% depășește limita de %s %% doar prin export, care ia %s %% din mașină: fără el ar fi %s %%. "+
		"Exportul rulează la viteză maximă tot timpul probei (C1-R19), deci rezultatul îl dă exportul continuu; PR-ul raportului (Task 14) "+
		"propune o Decizie tehnică, de exemplu un export cu ritm fix sau cu prioritate mică.", host, results.Decimal(b.Value, 1),
		results.Decimal(lim, -1), results.Decimal(export, 1), results.Decimal(b.Value-export, 1)), true
}

// recommended is the count that the steps after a failed first hour settled
// on: the count of a valid hour that passed, or 1 when a run at 1 camera
// failed; settled is false while the steps are open, as after a step's
// last invalid attempt.
func recommended(steps []results.Mi3Step) (n int, settled bool) {
	if len(steps) == 0 {
		return 0, false
	}
	last := steps[len(steps)-1]
	switch {
	case last.Valid && last.Pass && last.Minutes == FullMinutes:
		return last.Cameras, true
	case last.Valid && !last.Pass && last.Cameras == 1:
		return 1, true
	}
	return 0, false
}

// invalidText lists why the first hour's attempts are invalid.
func invalidText(d *results.Mi3Data) string {
	var parts []string
	for _, s := range d.Steps {
		if s.Valid || s.Cameras != d.Cameras || s.Minutes != FullMinutes {
			break
		}
		parts = append(parts, fmt.Sprintf("încercarea %d: %s", s.Attempt, invalidWhy(s.PacingP99Ms, s.SkippedAUs, s.InvalidReason)))
	}
	if len(parts) == 0 {
		return invalidWhy(d.PacingP99Ms, d.SkippedAUs, d.InvalidReason)
	}
	return strings.Join(parts, "; ")
}

// invalidWhy is why a step does not count: its pacing, when the controller
// sent unevenly (R07), and the reason why its load did not run its whole
// window (mis-2).
func invalidWhy(p99 int64, skipped int, reason string) string {
	var why []string
	if !validPacing(p99, skipped) || reason == "" {
		why = append(why, pacingText(p99, skipped))
	}
	if reason != "" {
		why = append(why, reason)
	}
	return strings.Join(why, "; ")
}

// infoRows adds the rows and notes of host's run that are no gate: the
// steps, with the one that stopped them, if any; each load's CPU (C1-R19);
// the disk ratio of each mode that no budget of the tier judges (the
// encrypted one, R02, and on H1b and H1c the clear one), the pacing, the
// storage and the proxies.
func infoRows(j *results.Judgement, prefix, host string, e *results.Envelope) {
	d := e.MI3
	steps := make([]string, len(d.Steps))
	for i, s := range d.Steps {
		steps[i] = stepText(s)
	}
	if d.StepsStopped != "" {
		steps = append(steps, d.StepsStopped)
	}
	j.Rows = append(j.Rows, results.Row{Code: prefix + "pașii", Value: strings.Join(steps, "; "), Source: host})
	for _, kind := range Loads {
		if pct, ok := d.LoadCPUPct[kind]; ok {
			j.Rows = append(j.Rows, results.Row{Code: prefix + "CPU-ul încărcării " + loadNames[kind] + " (proxy, informativ, C1-R19)",
				Value: results.Decimal(pct, 1) + " %", Source: host})
		}
	}
	if ratio, ok := d.DiskRatio[modeClear]; ok && !slices.Contains(BudgetKeys(d.Tier), keyDisk) {
		j.Rows = append(j.Rows, results.Row{Code: prefix + rowNames[keyDisk] + " (informativ)", Value: valueText(keyDisk, ratio), Source: host})
	}
	if ratio, ok := d.DiskRatio[modeEncrypted]; ok {
		j.Rows = append(j.Rows, results.Row{Code: prefix + "disc ÷ bitrate, camere criptate (R02, informativ)", Value: results.Decimal(ratio, 3), Source: host})
	}
	j.Rows = append(j.Rows,
		results.Row{Code: prefix + "ritmul controlorului (R07)", Value: pacingText(d.PacingP99Ms, d.SkippedAUs),
			Limit: fmt.Sprintf("≤ %d ms, cadre sărite 0", mi1.MaxPacingP99Ms), Source: host},
		results.Row{Code: prefix + "stocare", Value: storageText(e.Storage), Source: host})
	for _, p := range d.Proxies {
		if !slices.Contains(j.Notes, p) {
			j.Notes = append(j.Notes, p)
		}
	}
}

// stepHead names step s as the report shows it: "8 camere 1 h",
// "4 camere 15 min, încercarea 2".
func stepHead(s results.Mi3Step) string {
	length := "1 h"
	if s.Minutes != FullMinutes {
		length = fmt.Sprintf("%d min", s.Minutes)
	}
	head := camerasText(s.Cameras) + " " + length
	if s.Attempt > 1 {
		head += fmt.Sprintf(", încercarea %d", s.Attempt)
	}
	return head
}

// stepText is a step as the report shows it: "8 camere 1 h: pică".
func stepText(s results.Mi3Step) string {
	head := stepHead(s)
	switch {
	case !s.Valid:
		return head + ": nevalidă (" + invalidWhy(s.PacingP99Ms, s.SkippedAUs, s.InvalidReason) + ")"
	case s.Pass:
		return head + ": trece"
	}
	return head + ": pică"
}

// pacingText is the controller's pacing of a run (R07).
func pacingText(p99 int64, skipped int) string {
	return fmt.Sprintf("ritmul controlorului p99 %d ms, cadre sărite %d", p99, skipped)
}

// storageText is the storage under rec/ in Romanian; on an SD card, the
// owner confirmed its warning before the run (G16), as in MI1.
func storageText(storage string) string {
	switch storage {
	case "ssd":
		return "SSD"
	case "hdd":
		return "HDD"
	case "sd":
		return "card SD, cu avertismentul confirmat de proprietar (G16)"
	}
	return "necunoscută"
}

// camerasText is a camera count in Romanian: "1 cameră", "8 camere".
func camerasText(n int) string {
	if n == 1 {
		return "1 cameră"
	}
	return fmt.Sprintf("%d camere", n)
}

// SessionsText is n live sessions, singular for one: «1 sesiune live»,
// «3 sesiuni live».
func SessionsText(n int) string {
	if n == 1 {
		return "1 sesiune live"
	}
	return fmt.Sprintf("%d sesiuni live", n)
}

// valueText is a budget's value as its row shows it.
func valueText(key string, v float64) string {
	switch key {
	case keyCPUTotal:
		return results.Decimal(v, 1) + " %"
	case keyTileSub, keyTileMain, keyEncUpload, keyIngestPerCamera:
		return results.Decimal(v, 2) + " %"
	case keyRAM:
		return results.Decimal(v, 0) + " MB"
	case keyDisk:
		return results.Decimal(v, 3)
	case keyFsyncPerFile:
		return results.Decimal(v, 2)
	case keyLatencyP95:
		return results.Decimal(v, 0) + " ms"
	}
	return results.Decimal(v, -1)
}

// limitText is a budget's limit as its row shows it.
func limitText(key string, lim float64) string {
	switch key {
	case keyCPUTotal, keyTileSub, keyTileMain, keyEncUpload, keyIngestPerCamera:
		return "≤ " + results.Decimal(lim, -1) + " %"
	case keyRAM:
		return "≤ " + results.Decimal(lim, 0) + " MB"
	case keyLatencyP95:
		return "≤ " + results.Decimal(lim, 0) + " ms"
	}
	return "≤ " + results.Decimal(lim, -1)
}
