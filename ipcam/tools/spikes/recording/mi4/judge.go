// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package mi4 is the age random-access measurement MI4 (G13, ENC-08,
// HUB-33): Judge, the gate's one rule (C17), and Run, recspike's rig on the
// host under test, which records an encrypted 5-min segment through
// ipcam-hub and times reads, seeks and encryption with ipcam-hub's diag
// benches (C01), the read path itself.
package mi4

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The thresholds of G13 (ENC-08, HUB-33, C25, C26, G21).
const (
	// MaxReadMs bounds each of the reads (C26).
	MaxReadMs = 50
	// MaxSeekMs bounds the first frame after a seek.
	MaxSeekMs = 1500
	// MinAgerangeMbps is agerange's floor in the phone's WebView.
	MinAgerangeMbps = 12
	// h1MBs is the encryption threshold of H1 (Pi 5), and mbsPerCamera
	// that of the other models per recommended camera (ENC-08 (1)).
	h1MBs        = 50
	mbsPerCamera = 5
)

// The plan B of each sub-failure (G21). The format stays as it is.
const (
	PlanBRAM      = "bufferul RAM: segmentul întreg decriptat în RAM (≤ 256 MiB)"
	PlanBTwoMin   = "segmente criptate de 2 min, citite din bufferul RAM"
	PlanBEscalate = "escaladare: nici bufferul RAM, nici segmentele de 2 min nu aduc primul cadru în 1,5 s"
	// PlanBUnmeasured is a slow seek's plan B when a stage that chooses it
	// was not measured (G27).
	PlanBUnmeasured = "planul B nemăsurat"
	PlanBF6         = "F6 descarcă și decriptează segmentul întreg pe telefon"
)

// piDeferral is why the Pi's part moves to Faza 0-bis (G31).
const piDeferral = "Pi armv6"

// The phone's AGE run (G24, ENC-08 (3)). The video spike app's AGE scenario
// posts its result to reccontroller, which keeps it under results/app/, and
// recreport turns each result into an envelope with AgerangeRun. agerange is
// judged in the AND-1 WebView, so Judge reads it only from the device's
// runs, those marked with AgerangeSet and DeviceOS; an AGE run of a desktop
// shell or a browser carries its own platform and is left out.
const (
	// AgerangeSet is the set of the app's AGE runs.
	AgerangeSet = "app-age"
	// DeviceOS is the platform of the device that decides agerange: the
	// app's Android shell, AND-1's WebView ("android" in the result's
	// device.platform).
	DeviceOS = "android"
)

// AgerangeRun is the envelope of the app's n-th AGE result (n from 1, in
// the order reccontroller stored them), on platform, the result's
// device.platform, at mbps: the set AgerangeSet, the run id app-age-<n>,
// the platform as its OS, and the host pc, whose reccontroller received
// it. Its AtMs is stored, the time that reccontroller stored the result
// (the UTC time in the result file's name), or 0 when stored is zero. An
// interrupted result, or one without mbps, measured nothing and has no
// envelope.
func AgerangeRun(n int, stored time.Time, platform string, mbps float64) results.Envelope {
	var atMs int64
	if !stored.IsZero() {
		atMs = stored.UnixMilli()
	}
	return results.Envelope{Schema: results.Schema, Code: "MI4", RunID: AgerangeSet + "-" + strconv.Itoa(n), AtMs: atMs,
		Set: AgerangeSet, Host: "pc", OS: platform, MI4: &results.Mi4Data{AgerangeMbps: &mbps}}
}

// SetFor is MI4's set on host: pi-mi4, or pc-mi4 for information.
func SetFor(host string) string { return host + "-mi4" }

// Throughput is a host's encryption threshold in MB/s (C25, G23): 50 on H1
// (Pi 5), else 5 per camera of the model's recommended number (Pi 4: 20,
// H1c: 10); judged is false on the PC (m nil), where ENC-08 (1) sets none,
// and for a model without a number. after is the recommended number that
// the rate measured leaves: the model's when it reaches the threshold,
// else ⌊measured ÷ 5⌋, at least 1 and always below the model's.
func Throughput(m *results.PiModelInfo, measured float64) (threshold float64, judged bool, after int) {
	if m == nil || m.RecommendedMax < 1 {
		return 0, false, 0
	}
	threshold = float64(mbsPerCamera * m.RecommendedMax)
	if m.Tier == "H1" {
		threshold = h1MBs
	}
	if measured >= threshold {
		return threshold, true, m.RecommendedMax
	}
	return threshold, true, max(1, min(m.RecommendedMax-1, int(math.Floor(measured/mbsPerCamera))))
}

// part is the state of one part of G13.
type part int

const (
	partPass part = iota
	partFail
	partMissing
	partDeferred
)

// verdict collects the parts of G13.
type verdict struct {
	j        results.Judgement
	planB    []string
	failed   bool
	missing  bool
	deferred bool
}

func (v *verdict) add(p part, planB string) {
	switch p {
	case partFail:
		v.failed = true
		if !slices.Contains(v.planB, planB) {
			v.planB = append(v.planB, planB)
		}
	case partMissing:
		v.missing = true
	case partDeferred:
		v.deferred = true
	}
}

func (v *verdict) row(code, value, limit, source string, pass *bool) {
	v.j.Rows = append(v.j.Rows, results.Row{Code: code, Value: value, Limit: limit, Source: source, Pass: pass})
}

// Judge is G13 (C17, ENC-08, HUB-33). The Pi's latest MI4 envelope decides
// the reads (each of 1000 within 50 ms, C26), the seek (the first frame
// within 1.5 s, G21) and the encryption rate (Throughput, C25); the PC's
// is information only. The phone's latest AGE run decides agerange (at
// least 12 Mbps, G24), only a run of the device (AgerangeRun on
// DeviceOS), and CI's latest ENC-06 check the identical bytes, each from
// the envelope that recreport builds for it; latest is by the run id's
// number, then the envelope's time, then a stable key (later), whatever
// the order of envs. Each failure names its plan B
// (G21): a slow read or an age mismatch the RAM buffer; a slow seek the
// RAM buffer, or 2-min segments, or an escalation, after what their own
// seeks measured, and PlanBUnmeasured when a stage that the choice needs
// was not measured; the rate a lower recommended number; agerange F6's
// whole-segment download. A measured failure decides even with other parts
// missing (G27); a missing Pi part on an unsupported Pi moves G13 to Faza
// 0-bis (G31).
func Judge(envs []results.Envelope) results.Judgement {
	v := &verdict{j: results.Judgement{Gate: "G13"}}
	if e := latest(envs, "pi"); e != nil {
		v.judgePi(e)
	} else {
		state := partMissing
		if piUnsupported(envs) {
			state = partDeferred
			v.j.Reasons = append(v.j.Reasons, "pi: amânat ("+piDeferral+")")
		} else {
			v.j.Reasons = append(v.j.Reasons, "pi: lipsește proba MI4")
		}
		v.row("MI4 pi: rezultat", "lipsește", "", "pi", nil)
		v.add(state, "")
	}
	if e := latest(envs, "pc"); e != nil {
		v.informPC(e)
	}
	v.judgeAgerange(envs)
	v.judgeEnc06(envs)
	switch {
	case v.failed:
		v.j.Verdict, v.j.PlanB = results.Fail, strings.Join(v.planB, "; ")
		v.j.Notes = append(v.j.Notes, "Planul B nu schimbă formatul fișierelor (G21).")
	case v.missing:
		v.j.Verdict = results.Incomplete
	case v.deferred:
		v.j.Verdict = results.Deferred(piDeferral)
	default:
		v.j.Verdict = results.Pass
	}
	return v.j
}

// judgePi judges the reads, the seek and the rate of the Pi's run.
func (v *verdict) judgePi(e *results.Envelope) {
	d := e.MI4
	if len(d.ReadMs) == 0 {
		v.row("MI4 pi: citiri aleatoare", "lipsește", "fiecare ≤ 50 ms", "pi", nil)
		v.add(partMissing, "")
	} else {
		readMax := slices.Max(d.ReadMs)
		ok := readMax <= MaxReadMs
		v.row(readsCode("pi", d), fmt.Sprintf("max %s ms (p50 %s, p99 %s)", ms(readMax), ms(d.ReadP50Ms), ms(d.ReadP99Ms)),
			"fiecare ≤ 50 ms", "pi", results.RowPass(ok, true))
		if !ok {
			v.j.Reasons = append(v.j.Reasons, fmt.Sprintf("pi: o citire a durat %s ms (limita ≤ 50 ms)", ms(readMax)))
			v.add(partFail, PlanBRAM)
		}
	}
	for _, r := range d.InfoReads {
		v.row(fmt.Sprintf("MI4 pi: citiri de %s (informativ)", sizeText(r.Size)), fmt.Sprintf("max %s ms (p50 %s, p99 %s)", ms(r.MaxMs), ms(r.P50Ms), ms(r.P99Ms)), "", "pi", nil)
	}
	v.judgeSeek(d)
	v.judgeRate(e)
}

// judgeSeek judges the first frame after a seek, with its plan B's stages.
func (v *verdict) judgeSeek(d *results.Mi4Data) {
	if len(d.SeekMs) == 0 {
		v.row("MI4 pi: primul cadru după salt", "lipsește", "≤ 1500 ms", "pi", nil)
		v.add(partMissing, "")
		return
	}
	seekMax := slices.Max(d.SeekMs)
	ok := seekMax <= MaxSeekMs
	v.row("MI4 pi: primul cadru după salt (segment de 5 min)", fmt.Sprintf("max %s ms (%d salturi)", ms(seekMax), len(d.SeekMs)), "≤ 1500 ms", "pi", results.RowPass(ok, true))
	var unmeasured []string
	// stage adds the row of a plan-B stage, and whether it was measured and
	// brings the first frame within 1.5 s.
	stage := func(code, what string, seeks []float64) (measured, pass bool) {
		if len(seeks) == 0 {
			v.row(code, "nemăsurat", "", "pi", nil)
			unmeasured = append(unmeasured, what)
			return false, false
		}
		v.row(code, fmt.Sprintf("max %s ms", ms(slices.Max(seeks))), "", "pi", nil)
		return true, slices.Max(seeks) <= MaxSeekMs
	}
	if ok {
		return
	}
	v.j.Reasons = append(v.j.Reasons, fmt.Sprintf("pi: primul cadru după salt a venit în %s ms (limita ≤ 1500 ms)", ms(seekMax)))
	ramMeasured, ram := stage("MI4 pi: salt cu segmentul întreg în RAM (planul B)", "cu segmentul întreg în RAM", d.SeekRAMMs)
	twoMinMeasured, twoMin := stage("MI4 pi: salt în segmente de 2 min, din RAM (planul B)", "în segmente de 2 min", d.Seek2MinMs)
	// The RAM buffer comes first, then 2-min segments (G21): each is chosen
	// only on what was measured, and the escalation needs both measured.
	switch {
	case ram:
		v.add(partFail, PlanBRAM)
	case ramMeasured && twoMin:
		v.add(partFail, PlanBTwoMin)
	case ramMeasured && twoMinMeasured:
		v.add(partFail, PlanBEscalate)
	default:
		// The seek failed, measured (G27); the choice of its plan B was not.
		v.j.Reasons = append(v.j.Reasons, "pi: planul B al saltului este nemăsurat: lipsesc salturile "+strings.Join(unmeasured, " și "))
		v.add(partFail, PlanBUnmeasured)
		v.add(partMissing, "")
	}
}

// judgeRate judges the Pi's encryption rate against its model's threshold.
func (v *verdict) judgeRate(e *results.Envelope) {
	d := e.MI4
	threshold, judged, after := Throughput(e.PiModel, d.CryptoMBs)
	switch {
	case d.CryptoMBs <= 0:
		v.row("MI4 pi: criptare pe un nucleu", "lipsește", "", "pi", nil)
		v.add(partMissing, "")
		return
	case !judged:
		v.row("MI4 pi: criptare pe un nucleu", mbs(d.CryptoMBs), "fără prag: modelul nu are un număr recomandat", "pi", nil)
		v.add(partMissing, "")
		return
	}
	ok := d.CryptoMBs >= threshold
	v.row("MI4 pi: criptare pe un nucleu", mbs(d.CryptoMBs), "≥ "+mbs(threshold), "pi", results.RowPass(ok, true))
	v.row("MI4 pi: criptare pe toate nucleele (informativ)", mbs(d.CryptoMBsAllCores), "", "pi", nil)
	if !ok {
		v.j.Reasons = append(v.j.Reasons, fmt.Sprintf("pi: criptarea pe un nucleu dă %s (pragul %s)", mbs(d.CryptoMBs), mbs(threshold)))
		v.add(partFail, "pi: numărul recomandat devine "+strconv.Itoa(after))
	}
}

// informPC adds the PC's run as information rows.
func (v *verdict) informPC(e *results.Envelope) {
	d := e.MI4
	if len(d.ReadMs) > 0 {
		v.row(readsCode("pc", d), fmt.Sprintf("max %s ms (p50 %s, p99 %s)", ms(slices.Max(d.ReadMs)), ms(d.ReadP50Ms), ms(d.ReadP99Ms)), "", "pc", nil)
	}
	if len(d.SeekMs) > 0 {
		v.row("MI4 pc: primul cadru după salt (informativ)", fmt.Sprintf("max %s ms", ms(slices.Max(d.SeekMs))), "", "pc", nil)
	}
	v.row("MI4 pc: criptare pe un nucleu (informativ, H3)", mbs(d.CryptoMBs), "", "pc", nil)
}

// judgeAgerange judges the phone's latest AGE run: the device's run that is
// later than its others (later). When the device has none, the reason
// names the other platforms whose AGE runs do not count.
func (v *verdict) judgeAgerange(envs []results.Envelope) {
	const code, limit = "MI4: agerange în WebView-ul telefonului", "≥ 12 Mbps"
	var run *results.Envelope
	var others []string
	for i, e := range envs {
		if e.Code != "MI4" || e.Set != AgerangeSet || e.MI4 == nil || e.MI4.AgerangeMbps == nil {
			continue
		}
		if e.OS != DeviceOS {
			if platform := cmp.Or(e.OS, "platformă necunoscută"); !slices.Contains(others, platform) {
				others = append(others, platform)
			}
			continue
		}
		if run == nil || later(&envs[i], run) {
			run = &envs[i]
		}
	}
	if run == nil {
		why := "lipsește rularea AGE din aplicația Android a telefonului"
		if len(others) > 0 {
			slices.Sort(others)
			why += " (cele de pe " + strings.Join(others, ", ") + " nu contează: agerange se judecă în WebView-ul telefonului)"
		}
		v.row(code, "lipsește", limit, "telefon", nil)
		v.j.Reasons = append(v.j.Reasons, why)
		v.add(partMissing, "")
		return
	}
	mbps := run.MI4.AgerangeMbps
	ok := *mbps >= MinAgerangeMbps
	v.row(code, results.Decimal(*mbps, 1)+" Mbps", limit, "telefon", results.RowPass(ok, true))
	if !ok {
		v.j.Reasons = append(v.j.Reasons, fmt.Sprintf("agerange dă %s Mbps pe telefon (pragul 12 Mbps)", results.Decimal(*mbps, 1)))
		v.add(partFail, PlanBF6)
	}
}

// judgeEnc06 judges CI's latest ENC-06 check (later).
func (v *verdict) judgeEnc06(envs []results.Envelope) {
	const code, limit = "ENC-06: aceiași octeți ca age și rage", "identici"
	var check *results.Envelope
	for i, e := range envs {
		if e.Code == "ENC-06" && e.MI4 != nil && e.MI4.Enc06 != nil && (check == nil || later(&envs[i], check)) {
			check = &envs[i]
		}
	}
	var same *bool
	if check != nil {
		same = check.MI4.Enc06
	}
	switch {
	case same == nil:
		v.row(code, "lipsește", limit, "ci", nil)
		v.j.Reasons = append(v.j.Reasons, "lipsește verificarea ENC-06 din CI")
		v.add(partMissing, "")
	case *same:
		v.row(code, "da", limit, "ci", results.RowPass(true, true))
	default:
		v.row(code, "nu", limit, "ci", results.RowPass(false, true))
		v.j.Reasons = append(v.j.Reasons, "ENC-06: age sau rage citesc alți octeți")
		v.add(partFail, PlanBRAM)
	}
}

// latest is host's latest MI4 run (later), or nil; an envelope of the
// phone's agerange alone is not a run.
func latest(envs []results.Envelope, host string) *results.Envelope {
	var out *results.Envelope
	for i, e := range envs {
		if e.Code != "MI4" || e.Host != host || e.MI4 == nil || e.Set != SetFor(host) {
			continue
		}
		if out == nil || later(&envs[i], out) {
			out = &envs[i]
		}
	}
	return out
}

// later reports whether run a is later than run b: by the number that ends
// its run id; on the same number, by the envelope's time (AtMs, 0 when its
// writer gave none); on the same time too, by a stable key, the greater of
// their JSON encodings. The run picked thus never depends on the order of
// the envelopes.
func later(a, b *results.Envelope) bool {
	if na, nb := runNumber(a.RunID), runNumber(b.RunID); na != nb {
		return na > nb
	}
	if a.AtMs != b.AtMs {
		return a.AtMs > b.AtMs
	}
	return stableKey(a) > stableKey(b)
}

// stableKey is e's JSON encoding, "" in the one case that has none, a
// figure that is not a number.
func stableKey(e *results.Envelope) string {
	b, err := json.Marshal(e)
	if err != nil {
		return ""
	}
	return string(b)
}

// runNumber is the number that ends a run id, as in pi-mi4-3 or
// app-age-12, or 0 when it does not end in one.
func runNumber(id string) int {
	n, _ := strconv.Atoi(id[strings.LastIndexByte(id, '-')+1:])
	return n
}

// piUnsupported reports whether an envelope names the Pi as unsupported.
func piUnsupported(envs []results.Envelope) bool {
	return slices.ContainsFunc(envs, func(e results.Envelope) bool {
		return e.Host == "pi" && e.PiModel != nil && e.PiModel.Known && !e.PiModel.Supported
	})
}

// readsCode is the reads' row, which says whether they were cold.
func readsCode(host string, d *results.Mi4Data) string {
	how := "la rece"
	if !d.Cold {
		how = "la cald"
	}
	return fmt.Sprintf("MI4 %s: citiri aleatoare de lungimea unei părți (%d, %s, ~%s)", host, len(d.ReadMs), how, sizeText(d.ReadSize))
}

func ms(v float64) string { return results.Decimal(v, 1) }

func mbs(v float64) string { return results.Decimal(v, 1) + " MB/s" }

// sizeText is a read's size: "64 KiB", "4 MiB", "512 KiB".
func sizeText(n int) string {
	if n >= 1<<20 {
		return results.Decimal(float64(n)/(1<<20), -1) + " MiB"
	}
	return results.Decimal(float64(n)/(1<<10), 0) + " KiB"
}
