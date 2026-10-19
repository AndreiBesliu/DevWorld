package main

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The platforms of a result (app/src/device.ts). A device here is a platform:
// the owner has one phone (AND-1) and one PC (WIN-1, and its browsers).
const (
	platformAndroid = "android"
	platformWindows = "windows-tauri"
)

// Device is the device of a result. Nothing in it names an address, a network
// or a serial number (PH0-22); the user agent is not read at all.
type Device struct {
	Platform string `json:"platform"`
	Model    string `json:"model"`
	OS       string `json:"os"`
	WebView  string `json:"webview"`
}

// Result is one results file as spikehub stores it: the app's ScenarioResult
// (app/src/results.ts) and the hub's mdnsUnavailable.
type Result struct {
	Scenario  string  `json:"scenario"`
	Device    Device  `json:"device"`
	StartedAt string  `json:"startedAt"`
	DurationS float64 `json:"durationS"`
	// Interrupted: the page was hidden during the run (Review Focus 2).
	Interrupted bool `json:"interrupted"`
	// HubJitterMs holds the p99 send deviation of each 10 s window (R28).
	HubJitterMs []float64            `json:"hubJitterMs"`
	Samples     map[string][]float64 `json:"samples"`
	Notes       []string             `json:"notes"`
	// MDNSUnavailable: the hub could not resolve the .local candidates of browsers.
	MDNSUnavailable bool `json:"mdnsUnavailable"`
	// File is the base name of the results file.
	File string `json:"-"`
}

// MV is the outcome of one threshold on one device.
type MV struct {
	// Code is MV1 to MV15 or G9, with the grid size after a slash where it matters: MV3/9.
	Code string
	// Value is the number the threshold is judged on (NaN when it has none).
	Value     float64
	Unit      string
	Threshold string
	Pass      bool
	// Device is the platform of the results: android, windows-tauri, chrome, edge, firefox.
	Device string
	// Text is the "Valoare" cell of the report; Value and Unit when empty.
	Text string
	// Notes are details for the lines under the device's table.
	Notes []string
}

// mvKey is the key of a threshold of a device in the map that ComputeMV returns.
func mvKey(device, code string) string { return device + "/" + code }

// The thresholds (spec 16.3, Global Constraints), each with the reading that is judged.
const (
	subFPS = 12.0 // the sources of the simulated cameras (Global Constraints)

	mv1P90MaxMs      = 1500.0
	mv1Starts        = 20
	mv2P95MaxMs      = 1000.0
	mv2Samples       = 100
	mv3MinSubFPS     = 0.9 * subFPS // 10.8
	mv3MaxDroppedPct = 2.0
	mv5MaxThermal    = 2 // MODERATE
	mv5MaxBatteryPh  = 15.0
	mv6GridMaxPct    = 25.0
	mv6MainMaxPct    = 10.0
	mv7MaxGrowthPct  = 10.0
	mv8MaxGapMs      = 2000.0
	mv8Seconds       = 300.0
	mv9MaxResumeMs   = 3000.0
	mv9BackgroundS   = 120.0
	mv10MaxStartMs   = 2000.0
	mv10MaxSeekMs    = 1500.0
	mv10Runs         = 20
	mv15MaxMs        = 3000.0
	mv15Runs         = 10

	// Review Focus 4: a run whose hub jitter is over this is flagged.
	jitterFlagMs = 20.0

	// The shell reads its metrics every 10 s (scenarios/s3.ts).
	nativeEverySeconds = 10.0
	// MV5 judges the fps of the last 2 minutes: 60 windows of 2 s.
	heatWindows = 60
	// MV7 compares two windows of 12 readings (6 min, a multiple of the 2 min paging and
	// the 3 min fullscreen cycle of S4, so that the layout phases cancel), the first one
	// after memWarmup readings (1 min, while the tiles start).
	memWarmup = 2
	memWindow = 12
)

var thermalLevels = []string{"NONE", "LIGHT", "MODERATE", "SEVERE", "CRITICAL", "EMERGENCY", "SHUTDOWN"}

// gridTiles is the number of tiles of the grid scenarios.
var gridTiles = map[string]int{"S3-4": 4, "S3-9": 9, "S3-16": 16}

// RunStatus says what a run counts for.
type RunStatus int

// The statuses of a run.
const (
	// RunUsed: the newest valid run of its scenario on its device.
	RunUsed RunStatus = iota
	// RunSuperseded: valid, but a newer valid run of the same scenario exists.
	RunSuperseded
	// RunInterrupted: the page was hidden during the run (Review Focus 2).
	RunInterrupted
	// RunFailed: the scenario stopped with an error, so its readings are incomplete.
	RunFailed
)

// Run is a result and what it counts for.
type Run struct {
	Result
	Status RunStatus
}

// startedAt is the time of a run on the device's clock; the zero time when it cannot be read.
func (r Result) startedAt() time.Time {
	t, err := time.Parse(time.RFC3339Nano, r.StartedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// failed: the scenario ended with an error (runner.ts notes `failed: ...`).
func (r Result) failed() bool {
	for _, n := range r.Notes {
		if strings.HasPrefix(n, "failed:") {
			return true
		}
	}
	return false
}

func (r Result) hubJitterMax() float64 {
	m := 0.0
	for _, v := range r.HubJitterMs {
		m = math.Max(m, v)
	}
	return m
}

// scenarioOrder is the order of the scenarios in the report.
var scenarioOrder = map[string]int{"S1": 1, "S2": 2, "S3-4": 3, "S3-9": 4, "S3-16": 5, "S4": 6, "S5": 7, "S6": 8, "S7": 9, "S8": 10, "S9": 11, "S10": 12}

// platformOrder is the order of the devices in the report: the two targets, then the browsers.
var platformOrder = map[string]int{platformAndroid: 1, platformWindows: 2, "chrome": 3, "edge": 4, "firefox": 5}

func orderOf(order map[string]int, name string) int {
	if n, ok := order[name]; ok {
		return n
	}
	return len(order) + 1
}

// classify sorts the results (device, scenario, start time) and says what each counts for:
// interrupted and failed runs are left out, and of the valid runs of one scenario on one
// device only the newest counts.
func classify(results []Result) []Run {
	runs := make([]Run, len(results))
	for i, r := range results {
		runs[i] = Run{Result: r}
	}
	sort.SliceStable(runs, func(i, j int) bool {
		a, b := runs[i], runs[j]
		if pa, pb := orderOf(platformOrder, a.Device.Platform), orderOf(platformOrder, b.Device.Platform); pa != pb {
			return pa < pb
		}
		if a.Device.Platform != b.Device.Platform {
			return a.Device.Platform < b.Device.Platform
		}
		if sa, sb := orderOf(scenarioOrder, a.Scenario), orderOf(scenarioOrder, b.Scenario); sa != sb {
			return sa < sb
		}
		if ta, tb := a.startedAt(), b.startedAt(); !ta.Equal(tb) {
			return ta.Before(tb)
		}
		if a.StartedAt != b.StartedAt {
			return a.StartedAt < b.StartedAt
		}
		return a.File < b.File
	})
	newest := map[string]int{} // platform + scenario -> index of the newest valid run
	for i := range runs {
		switch {
		case runs[i].Interrupted:
			runs[i].Status = RunInterrupted
		case runs[i].failed():
			runs[i].Status = RunFailed
		default:
			key := runs[i].Device.Platform + "|" + runs[i].Scenario
			if j, ok := newest[key]; ok {
				runs[j].Status = RunSuperseded
			}
			newest[key] = i
			runs[i].Status = RunUsed
		}
	}
	return runs
}

// JitterFlag is a run whose hub jitter went over the limit (Review Focus 4).
type JitterFlag struct {
	File     string
	Scenario string
	MaxMs    float64
}

// HubJitterFlags lists the runs whose hub jitter, the p99 of a 10 s window (R28), went
// over 20 ms in some window: the hub PC was busy, so the client's numbers look worse than they are.
func HubJitterFlags(results []Result) []JitterFlag {
	var flags []JitterFlag
	for _, r := range results {
		if m := r.hubJitterMax(); m > jitterFlagMs {
			flags = append(flags, JitterFlag{File: r.File, Scenario: r.Scenario, MaxMs: m})
		}
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].File < flags[j].File })
	return flags
}

// evaluation is everything the thresholds say about a set of results.
type evaluation struct {
	Runs []Run
	// MV holds the thresholds that could be judged, by mvKey.
	MV map[string]MV
	// Missing says, for a threshold that was expected but could not be judged, why not.
	Missing map[string]string
}

// ComputeMV judges the thresholds of every device in `results`. The map is keyed by
// mvKey(device, code), for example "android/MV9". A threshold that the results cannot
// judge (its scenario did not run, was interrupted, or lacks readings) is not in the map.
// Interrupted runs and runs that failed do not count; of several valid runs of one
// scenario on one device the newest counts.
func ComputeMV(results []Result) map[string]MV { return evaluate(results).MV }

func evaluate(results []Result) evaluation {
	ev := evaluation{Runs: classify(results), MV: map[string]MV{}, Missing: map[string]string{}}
	byDevice := map[string]map[string]*Run{}
	for i := range ev.Runs {
		r := &ev.Runs[i]
		if r.Status != RunUsed {
			continue
		}
		if byDevice[r.Device.Platform] == nil {
			byDevice[r.Device.Platform] = map[string]*Run{}
		}
		byDevice[r.Device.Platform][r.Scenario] = r
	}
	for platform, used := range byDevice {
		e := &evaluator{platform: platform, runs: used, ev: &ev}
		e.all()
	}
	return ev
}

// tri is the state of one condition of a threshold.
type tri int

const (
	unknown tri = iota
	passed
	failed
)

// check is one condition of a threshold: what it says in the value cell, and whether it holds.
type check struct {
	state tri
	text  string
}

func holds(ok bool, text string) check {
	if ok {
		return check{passed, text}
	}
	return check{failed, text}
}

func lacking(why string) check { return check{unknown, why} }

// combine: failed when any condition failed (a missing reading cannot undo that), passed
// when all passed, unknown otherwise.
func combine(checks []check) tri {
	state := passed
	for _, c := range checks {
		switch c.state {
		case failed:
			return failed
		case unknown:
			state = unknown
		case passed:
		}
	}
	return state
}

func joinTexts(checks []check, only ...tri) string {
	var parts []string
	for _, c := range checks {
		if c.text == "" {
			continue
		}
		if len(only) > 0 && c.state != only[0] {
			continue
		}
		parts = append(parts, c.text)
	}
	return strings.Join(parts, "; ")
}

type evaluator struct {
	platform string
	runs     map[string]*Run
	ev       *evaluation
}

// finish records a threshold, or why it could not be judged.
func (e *evaluator) finish(code string, value float64, unit string, checks []check, notes []string) {
	key := mvKey(e.platform, code)
	switch combine(checks) {
	case unknown:
		e.ev.Missing[key] = joinTexts(checks, unknown)
	default:
		e.ev.MV[key] = MV{
			Code: code, Value: value, Unit: unit, Threshold: thresholdOf(code), Device: e.platform,
			Pass: combine(checks) == passed, Text: joinTexts(checks), Notes: notes,
		}
	}
}

// missing records why a threshold could not be judged, when its run exists but lacks readings.
func (e *evaluator) missing(code, why string) {
	e.ev.Missing[mvKey(e.platform, code)] = why
}

func (e *evaluator) all() {
	e.mv1()
	e.mv2()
	for _, scenario := range []string{"S3-4", "S3-9", "S3-16"} {
		e.grid(scenario)
	}
	e.mv6()
	e.mv7()
	e.mv8()
	e.mv9()
	e.mv10()
	e.pair()
	e.mv15()
	e.g9()
}

// runNotes are the notes every threshold of a run carries: the hub jitter, when over the limit.
func runNotes(r *Run) []string {
	if m := r.hubJitterMax(); m > jitterFlagMs {
		return []string{fmt.Sprintf("jitter hub: %s ms în rularea %s (peste 20 ms): PC-ul hub-ului poate fi ocupat, deci cifrele clientului pot fi mai proaste decât în realitate", number(m, 1), r.Scenario)}
	}
	return nil
}

// notesWithPrefix returns the notes of a run that start with prefix.
func notesWithPrefix(r *Run, prefix string) []string {
	var out []string
	for _, n := range r.Notes {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out
}

func hasNote(r *Run, note string) bool {
	for _, n := range r.Notes {
		if n == note {
			return true
		}
	}
	return false
}

// ---- statistics ----

// percentile is the nearest-rank percentile at quantile q (0 to 1), as the app counts it
// (app/src/percentile.ts): the smallest sample with at least a fraction q of the samples at
// or below it. NaN for no samples.
func percentile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	rank := int(math.Ceil(q*float64(len(sorted)) - 1e-9))
	rank = min(len(sorted), max(1, rank))
	return sorted[rank-1]
}

// percentileOf is the percentile of `expected` readings of which only `values` came: a
// reading that did not come (a start without a first frame) counts as infinitely slow.
func percentileOf(values []float64, expected int, q float64) float64 {
	padded := append([]float64(nil), values...)
	for len(padded) < expected {
		padded = append(padded, math.Inf(1))
	}
	return percentile(padded, q)
}

// tolerance absorbs the rounding of a mean: the mean of sixty times 10.8 is not 10.8 to the last digit.
const tolerance = 1e-9

// atLeast and atMost compare a computed value (a mean, a growth, a rate) with its threshold.
func atLeast(v, limit float64) bool { return v >= limit-tolerance }
func atMost(v, limit float64) bool  { return v <= limit+tolerance }

func mean(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func minOf(values []float64) float64 {
	m := math.Inf(1)
	for _, v := range values {
		m = math.Min(m, v)
	}
	return m
}

func maxOf(values []float64) float64 {
	m := math.Inf(-1)
	for _, v := range values {
		m = math.Max(m, v)
	}
	return m
}

// number writes v with a decimal comma, rounded to `decimals` places without trailing zeros.
func number(v float64, decimals int) string {
	switch {
	case math.IsInf(v, 1):
		return "∞"
	case math.IsNaN(v):
		return "—"
	}
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		s = "0"
	}
	return strings.Replace(s, ".", ",", 1)
}

func ms(v float64) string { return number(v, 0) + " ms" }

// ---- MV1, MV2 ----

func (e *evaluator) mv1() {
	r := e.runs["S1"]
	if r == nil {
		return
	}
	got := r.Samples["firstFrameMs"]
	p90 := percentileOf(got, mv1Starts, 0.9)
	var text string
	if math.IsInf(p90, 1) {
		text = fmt.Sprintf("p90 nu există: doar %d din %d porniri au arătat un prim cadru", len(got), mv1Starts)
	} else {
		text = fmt.Sprintf("p90 %s (mediana %s, max %s; %d din %d porniri)", ms(p90), ms(percentile(got, 0.5)), ms(maxOf(got)), len(got), mv1Starts)
	}
	notes := runNotes(r)
	for _, n := range notesWithPrefix(r, "sub: ") {
		notes = append(notes, "substream: "+describeCodec(strings.TrimPrefix(n, "sub: ")))
	}
	e.finish("MV1", p90, "ms", []check{holds(p90 <= mv1P90MaxMs, text)}, notes)
}

// describeCodec turns the app's `H264, fallback profile 4d001f` into a sentence part.
func describeCodec(s string) string {
	codec, profile, found := strings.Cut(s, ", fallback profile ")
	if !found {
		return codec
	}
	return codec + ", în profilul de rezervă " + profile + " (R23: browserul nu oferă profilul sursei)"
}

func (e *evaluator) mv2() {
	r := e.runs["S2"]
	if r == nil {
		return
	}
	var checks []check
	notes := runNotes(r)
	worst := math.Inf(-1)
	for _, p := range []struct{ key, name string }{{"sub", "sub"}, {"main", "principal"}} {
		got := r.Samples[p.key+"LatencyMs"]
		if len(got) < mv2Samples {
			checks = append(checks, lacking(fmt.Sprintf("%s: %d din %d mostre", p.name, len(got), mv2Samples)))
			continue
		}
		p95 := percentile(got, 0.95)
		worst = math.Max(worst, p95)
		checks = append(checks, holds(p95 < mv2P95MaxMs, fmt.Sprintf("p95 %s %s (%d mostre)", p.name, ms(p95), len(got))))
		if bad := countValue(r.Samples[p.key+"Reliable"], 0); bad > 0 {
			notes = append(notes, fmt.Sprintf("%s: %d din %d mostre cu ceas nesigur (RTT > 50 ms sau offset mutat cu > 20 ms); intră în calcul", p.name, bad, len(r.Samples[p.key+"Reliable"])))
		}
	}
	if math.IsInf(worst, -1) {
		worst = math.NaN()
	}
	if rtt := r.Samples["clockRttMs"]; len(rtt) > 0 {
		notes = append(notes, fmt.Sprintf("RTT al ceasului: max %s (eroarea offsetului e cel mult jumătate din el)", ms(maxOf(rtt))))
	}
	e.finish("MV2", worst, "ms", checks, notes)
}

func countValue(values []float64, v float64) int {
	n := 0
	for _, x := range values {
		if x == v {
			n++
		}
	}
	return n
}

// ---- MV3, MV4, MV5: the grids ----

// complete: the readings of a per-tile series are all there.
func complete(values []float64, tiles int) bool { return len(values) == tiles }

func (e *evaluator) grid(scenario string) {
	r := e.runs[scenario]
	if r == nil {
		return
	}
	tiles := gridTiles[scenario]
	e.mv3(r, tiles)
	e.mv4(r, tiles)
	if e.platform == platformAndroid {
		e.mv5(r, tiles)
	}
}

func tileCode(code string, tiles int) string { return code + "/" + strconv.Itoa(tiles) }

func (e *evaluator) mv3(r *Run, tiles int) {
	fps, dropped := r.Samples["tileFps"], r.Samples["tileDroppedPct"]
	var checks []check
	switch {
	case len(fps) > 0 && minOf(fps) < mv3MinSubFPS:
		checks = append(checks, holds(false, "fps minim "+number(minOf(fps), 2)))
	case !complete(fps, tiles):
		checks = append(checks, lacking(fmt.Sprintf("fps: %d din %d plăci în rezultat", len(fps), tiles)))
	default:
		checks = append(checks, holds(true, "fps minim "+number(minOf(fps), 2)))
	}
	switch {
	case len(dropped) > 0 && maxOf(dropped) > mv3MaxDroppedPct:
		checks = append(checks, holds(false, "pierdute max "+number(maxOf(dropped), 1)+" %"))
	case !complete(dropped, tiles):
		checks = append(checks, lacking(fmt.Sprintf("pierdute: %d din %d plăci în rezultat", len(dropped), tiles)))
	default:
		checks = append(checks, holds(true, "pierdute max "+number(maxOf(dropped), 1)+" %"))
	}
	value := math.NaN()
	if len(fps) > 0 {
		value = minOf(fps)
	}
	notes := runNotes(r)
	if main := r.Samples["mainFps"]; len(main) > 0 {
		line := "ecran complet (flux principal, informativ): " + number(mean(main), 2) + " fps"
		if d := r.Samples["mainDroppedPct"]; len(d) > 0 {
			line += ", pierdute " + number(d[0], 1) + " %"
		}
		notes = append(notes, line)
	}
	e.finish(tileCode("MV3", tiles), value, "fps", checks, notes)
}

func (e *evaluator) mv4(r *Run, tiles int) {
	hw, main := r.Samples["tileHardware"], r.Samples["mainHardware"]
	var checks []check
	ones := countValue(hw, 1)
	value := math.NaN()
	switch {
	case len(hw) > 0 && countValue(hw, 0) > 0:
		checks = append(checks, holds(false, fmt.Sprintf("plăci hardware: %d din %d", ones, tiles)))
	case !complete(hw, tiles):
		checks = append(checks, lacking(fmt.Sprintf("decodorul plăcilor: %d din %d în rezultat", len(hw), tiles)))
	default:
		checks = append(checks, holds(true, fmt.Sprintf("plăci hardware: %d din %d", ones, tiles)))
	}
	if len(hw) > 0 {
		value = 100 * float64(ones) / float64(len(hw))
	}
	switch {
	case len(main) == 0:
		checks = append(checks, lacking("ecran complet: fără informație despre decodor"))
	case countValue(main, 1) == len(main):
		checks = append(checks, holds(true, "ecran complet: hardware"))
	default:
		checks = append(checks, holds(false, "ecran complet: software"))
	}
	e.finish(tileCode("MV4", tiles), value, "%", checks, runNotes(r))
}

func thermalName(index float64) string {
	i := int(index)
	if i < 0 || i >= len(thermalLevels) {
		return "necunoscut"
	}
	return thermalLevels[i]
}

func (e *evaluator) mv5(r *Run, tiles int) {
	var checks []check
	if thermal := r.Samples["thermal"]; len(thermal) == 0 {
		checks = append(checks, lacking("termic: fără metrici de la shell"))
	} else {
		top := maxOf(thermal)
		checks = append(checks, holds(top <= mv5MaxThermal, "termic max "+thermalName(top)))
	}
	if grid := r.Samples["gridFps"]; len(grid) == 0 {
		checks = append(checks, lacking("fps final: fără mostre"))
	} else {
		last := mean(grid[max(0, len(grid)-heatWindows):])
		checks = append(checks, holds(atLeast(last, mv3MinSubFPS), "fps în ultimele 2 min "+number(last, 2)))
	}
	rate := math.NaN()
	switch bat := r.Samples["batteryPct"]; {
	case len(bat) < 2:
		checks = append(checks, lacking("baterie: fără citiri"))
	case bat[len(bat)-1] > bat[0]:
		checks = append(checks, lacking("baterie: a crescut în timpul măsurătorii (încărcător conectat?), ritmul nu se poate judeca"))
	default:
		// Multiplied first, so that a drop of exactly 15 %/h is not 15.000000000000002.
		rate = (bat[0] - bat[len(bat)-1]) * 3600 / (float64(len(bat)-1) * nativeEverySeconds)
		checks = append(checks, holds(atMost(rate, mv5MaxBatteryPh), "baterie "+number(rate, 1)+" %/h"))
	}
	e.finish(tileCode("MV5", tiles), rate, "%/h", checks, runNotes(r))
}

// ---- MV6, MV7: the PC and the long run ----

func (e *evaluator) mv6() {
	if e.platform != platformWindows {
		return
	}
	var checks []check
	var notes []string
	value := math.NaN()
	if r := e.runs["S3-16"]; r != nil {
		notes = runNotes(r)
		if cpu := r.Samples["cpuPctApp"]; len(cpu) > 0 {
			value = mean(cpu)
			checks = append(checks, holds(atMost(value, mv6GridMaxPct), fmt.Sprintf("16 plăci: %s %% (max %s %%)", number(value, 1), number(maxOf(cpu), 1))))
			if total := r.Samples["cpuPctTotal"]; len(total) > 0 {
				notes = append(notes, "CPU total al PC-ului, cu hub-ul pe același PC: "+number(mean(total), 1)+" %")
			}
		} else {
			checks = append(checks, lacking("16 plăci: fără metrici de la shell"))
		}
	} else {
		checks = append(checks, lacking("16 plăci: S3-16 nu a rulat"))
	}
	// The main stream is judged on the first grid run that measured it: it does not depend on the grid.
	main := lacking("flux principal: fără metrici de la shell")
	for _, scenario := range []string{"S3-16", "S3-9", "S3-4"} {
		if r := e.runs[scenario]; r != nil && len(r.Samples["mainCpuPctApp"]) > 0 {
			cpu := mean(r.Samples["mainCpuPctApp"])
			main = holds(atMost(cpu, mv6MainMaxPct), "flux principal 1080p: "+number(cpu, 1)+" %")
			break
		}
	}
	checks = append(checks, main)
	e.finish("MV6", value, "%", checks, notes)
}

// memoryGrowth is how much the mean of the last window is above the mean of the first
// window, in percent; false without enough readings.
func memoryGrowth(series []float64) (float64, bool) {
	if len(series) < memWarmup+2*memWindow {
		return 0, false
	}
	base := mean(series[memWarmup : memWarmup+memWindow])
	if base <= 0 {
		return 0, false
	}
	return (mean(series[len(series)-memWindow:]) - base) / base * 100, true
}

func signed(v float64, decimals int) string {
	s := number(v, decimals)
	if v > 0 && !strings.HasPrefix(s, "-") {
		return "+" + s
	}
	return s
}

func (e *evaluator) mv7() {
	r := e.runs["S4"]
	if r == nil {
		return
	}
	var checks []check
	worst := math.Inf(-1)
	haveSeries := false
	for _, s := range []struct{ key, name string }{{"memMb", "memorie"}, {"jsHeapMb", "heap JS"}} {
		series := r.Samples[s.key]
		if len(series) == 0 {
			continue
		}
		haveSeries = true
		growth, ok := memoryGrowth(series)
		if !ok {
			checks = append(checks, lacking(fmt.Sprintf("%s: %d citiri, prea puține", s.name, len(series))))
			continue
		}
		worst = math.Max(worst, growth)
		checks = append(checks, holds(atMost(growth, mv7MaxGrowthPct), fmt.Sprintf("%s %s %%", s.name, signed(growth, 1))))
	}
	if !haveSeries {
		checks = append(checks, lacking("fără citiri de memorie"))
	}
	if math.IsInf(worst, -1) {
		worst = math.NaN()
	}
	if errs := r.Samples["playerErrors"]; len(errs) == 0 {
		checks = append(checks, lacking("reporniri neprovocate: lipsesc"))
	} else {
		checks = append(checks, holds(errs[0] == 0, "reporniri neprovocate: "+number(errs[0], 0)))
	}
	notes := runNotes(r)
	switch e.platform {
	case platformWindows:
		notes = append(notes, "memMb = memorie privată (private bytes, R37) a aplicației și a proceselor WebView2; nu include secțiunile partajate și memoria GPU, unde Chromium ține cadrele decodate, deci o scurgere acolo nu apare în MV7")
	case platformAndroid:
		notes = append(notes, "memMb = PSS al procesului aplicației; randerul WebView e alt proces și nu e inclus, de aceea se citește și heap-ul JS")
	default:
		notes = append(notes, "memoria = heap-ul JS al paginii (Chromium); memoria procesului browserului nu e măsurată")
	}
	notes = append(notes, "creștere = media ultimelor 6 min față de media a 6 min de după primul minut, ferestre care acoperă întregi ciclurile de paginare (2 min) și de ecran complet (3 min)")
	e.finish("MV7", worst, "%", checks, notes)
}

// ---- MV8, MV9, MV10: Android behaviour and recordings ----

func (e *evaluator) mv8() {
	r := e.runs["S6"]
	if r == nil {
		return
	}
	frames, gap, secs := r.Samples["pipFrames"], r.Samples["pipMaxGapMs"], r.Samples["pipS"]
	if len(frames) == 0 || len(gap) == 0 || len(secs) == 0 {
		e.missing("MV8", whyNoReading("S6", r))
		return
	}
	checks := []check{
		holds(secs[0] >= mv8Seconds, fmt.Sprintf("%s s în PiP", number(secs[0], 0))),
		holds(gap[0] <= mv8MaxGapMs, "pauză maximă între cadre "+ms(gap[0])),
		holds(frames[0] > 0, fmt.Sprintf("%s cadre", number(frames[0], 0))),
	}
	e.finish("MV8", gap[0], "ms", checks, runNotes(r))
}

// whyNoReading says why a run gave no reading: the first note that explains it, else a general sentence.
func whyNoReading(scenario string, r *Run) string {
	for _, n := range r.Notes {
		if strings.HasPrefix(n, "n/a") || strings.HasPrefix(n, "start") || strings.HasPrefix(n, "resume: n/a") || strings.HasPrefix(n, "the app did not") {
			return fmt.Sprintf("%s a notat „%s”", scenario, n)
		}
	}
	return scenario + " nu a dat citirile așteptate"
}

func (e *evaluator) mv9() {
	r := e.runs["S5"]
	if r == nil {
		return
	}
	resume, background := r.Samples["resumeMs"], r.Samples["backgroundS"]
	notes := runNotes(r)
	switch {
	case hasNote(r, "resume: n/a (no requestVideoFrameCallback)"):
		e.missing("MV9", "browserul nu are requestVideoFrameCallback (R31): reluarea live nu se poate măsura")
	case hasNote(r, "no frame within 30 s of the return"):
		e.finish("MV9", math.Inf(1), "ms", []check{holds(false, "niciun cadru live în 30 s după întoarcere")}, notes)
	case hasNote(r, "no steady live frames within 30 s of the return"):
		e.finish("MV9", math.Inf(1), "ms", []check{holds(false, "cadre live, dar neregulate, în 30 s după întoarcere")}, notes)
	case len(notesWithPrefix(r, "resume: ")) > 0:
		e.finish("MV9", math.Inf(1), "ms", []check{holds(false, "playerul a dat eroare la întoarcere")}, notes)
	case len(resume) == 0:
		e.missing("MV9", whyNoReading("S5", r))
	case len(background) == 0 || background[0] < mv9BackgroundS:
		e.missing("MV9", "aplicația a stat în fundal mai puțin de 2 min: repetați S5")
	default:
		e.finish("MV9", resume[0], "ms", []check{holds(resume[0] <= mv9MaxResumeMs, fmt.Sprintf("reluare în %s după %s s în fundal", ms(resume[0]), number(background[0], 0)))}, notes)
	}
}

// allWithin judges `got` of `expected` readings on every one of them being within `limit`, like
// MV15: a reading that did not come (no first frame, no `seeked` within 10 s) counts as infinitely
// slow, so one timeout fails it. It returns the slowest reading.
func allWithin(name string, got []float64, expected int, limit float64) (check, float64) {
	worst := percentileOf(got, expected, 1)
	if math.IsInf(worst, 1) {
		return holds(false, fmt.Sprintf("%s: doar %d din %d reușite", name, len(got), expected)), worst
	}
	return holds(worst <= limit, fmt.Sprintf("%s max %s (mediana %s; %d din %d)", name, ms(worst), ms(percentile(got, 0.5)), len(got), expected)), worst
}

// mv10 judges every one of the 20 starts and 20 seeks, not a percentile: the spec gives none
// ("start ≤ 2 s; seek ≤ 1.5 s"), and MV10 is a critical item of AND-1, so a recording that
// sometimes never plays or never seeks must not pass.
func (e *evaluator) mv10() {
	r := e.runs["S7"]
	if r == nil {
		return
	}
	start, slowestStart := allWithin("pornire", r.Samples["startMs"], mv10Runs, mv10MaxStartMs)
	seek, _ := allWithin("salt", r.Samples["seekMs"], mv10Runs, mv10MaxSeekMs)
	e.finish("MV10", slowestStart, "ms", []check{start, seek}, runNotes(r))
}

// ---- MV12, MV13: the ICE pair ----

var (
	pairNote   = regexp.MustCompile(`^pair: ([a-z]+)/([a-z]+)$`)
	promptNote = regexp.MustCompile(`^prompt: (yes|no|n/a)$`)
)

var promptText = map[string]string{"yes": "da", "no": "nu", "n/a": "necunoscut (n/a)"}

func (e *evaluator) pair() {
	r := e.runs["S10"]
	if r == nil {
		return
	}
	var code string
	switch e.platform {
	case platformAndroid, platformWindows:
		code = "MV12"
	case "chrome", "edge", "firefox":
		code = "MV13"
	default:
		return
	}
	local, remote, prompt := "", "", "necunoscut"
	for _, n := range r.Notes {
		if m := pairNote.FindStringSubmatch(n); m != nil {
			local, remote = m[1], m[2]
		}
		if m := promptNote.FindStringSubmatch(n); m != nil {
			prompt = promptText[m[1]]
		}
	}
	notes := append(runNotes(r), "hub-ul ascultă doar pe IPv4 (R25, R38): perechile IPv6 nu se măsoară")
	if r.MDNSUnavailable {
		notes = append(notes, "mDNS indisponibil pe hub: n-a putut rezolva candidații .local ai browserelor; un filtru de multicast nu pică MV13 dacă perechea e prflx")
	}
	known := map[string]bool{"host": true, "srflx": true, "prflx": true, "relay": true}
	if !known[local] || !known[remote] {
		e.missing(code, "hub-ul nu a raportat perechea ICE (pair: n/a) în 10 s")
		return
	}
	text := fmt.Sprintf("pereche %s/%s; cerere de permisiune: %s", local, remote, prompt)
	ok := local != "relay" && remote != "relay"
	if code == "MV13" {
		isLAN := func(t string) bool { return t == "host" || t == "prflx" }
		ok = isLAN(local) && isLAN(remote)
	}
	e.finish(code, math.NaN(), "", []check{holds(ok, text)}, notes)
}

// ---- MV15, G9 ----

func (e *evaluator) mv15() {
	r := e.runs["S8"]
	if r == nil {
		return
	}
	got := r.Samples["argon2idMs"]
	worst := percentileOf(got, mv15Runs, 1)
	text := fmt.Sprintf("doar %d din %d rulări s-au terminat", len(got), mv15Runs)
	if !math.IsInf(worst, 1) {
		text = fmt.Sprintf("max %s (mediana %s, min %s; %d rulări)", ms(worst), ms(percentile(got, 0.5)), ms(minOf(got)), len(got))
	}
	e.finish("MV15", worst, "ms", []check{holds(worst <= mv15MaxMs, text)}, runNotes(r))
}

var outcomeNote = regexp.MustCompile(`^(mx-[a-z0-9]+): (ok|fallback|unsupported-profile|fallback-failed)$`)

// g9Cameras are the two paths of the codec matrix, in the order of the report.
var g9Cameras = []string{"mx-h265", "mx-high51"}

func (e *evaluator) g9() {
	r := e.runs["S9"]
	if r == nil {
		return
	}
	outcomes := map[string]string{}
	for _, n := range r.Notes {
		if m := outcomeNote.FindStringSubmatch(n); m != nil {
			outcomes[m[1]] = m[2]
		}
	}
	var firstFrames []string
	var checks []check
	for _, cam := range g9Cameras {
		o, ok := outcomes[cam]
		if !ok {
			checks = append(checks, lacking(cam+": fără rezultat"))
			continue
		}
		checks = append(checks, holds(o != "fallback-failed", cam+": "+o))
		switch o {
		case "ok":
			if v := r.Samples[cam+".firstFrameMs"]; len(v) > 0 {
				firstFrames = append(firstFrames, cam+" pe fluxul principal "+ms(v[0]))
			}
		case "fallback", "unsupported-profile":
			if v := r.Samples[cam+".subFirstFrameMs"]; len(v) > 0 {
				firstFrames = append(firstFrames, cam+" pe substream "+ms(v[0]))
			}
		}
	}
	notes := runNotes(r)
	if len(firstFrames) > 0 {
		notes = append(notes, "primul cadru: "+strings.Join(firstFrames, ", "))
	}
	for _, n := range r.Notes {
		if strings.Contains(n, ", fallback profile ") {
			cam, rest, _ := strings.Cut(n, ": bound ")
			notes = append(notes, cam+": "+describeCodec(rest))
		}
	}
	e.finish("G9", math.NaN(), "", checks, notes)
}

// ---- the rows a device is expected to have ----

// expectedCodes are the thresholds that the owner's guide has each device measure, in report order.
func expectedCodes(platform string) []string {
	switch platform {
	case platformAndroid:
		return []string{"MV1", "MV2", "MV3/4", "MV3/9", "MV4/9", "MV5/9", "MV7", "MV8", "MV9", "MV10", "MV12", "MV15", "G9"}
	case platformWindows:
		return []string{"MV3/16", "MV6", "MV7", "MV12"}
	case "chrome":
		return []string{"MV1", "MV2", "MV3/9", "MV3/16", "MV7", "MV10", "MV13", "G9"}
	case "edge", "firefox":
		return []string{"MV1", "MV2", "MV3/9", "MV13", "G9"}
	}
	return nil
}

// scenarioOf is the scenario that measures a threshold.
func scenarioOf(code string) string {
	base, tiles, _ := strings.Cut(code, "/")
	switch base {
	case "MV1":
		return "S1"
	case "MV2":
		return "S2"
	case "MV3", "MV4", "MV5":
		return "S3-" + tiles
	case "MV6":
		return "S3-16"
	case "MV7":
		return "S4"
	case "MV8":
		return "S6"
	case "MV9":
		return "S5"
	case "MV10":
		return "S7"
	case "MV12", "MV13":
		return "S10"
	case "MV15":
		return "S8"
	case "G9":
		return "S9"
	}
	return ""
}

// reasonMissing says why an expected threshold of a device has no value.
func (ev evaluation) reasonMissing(platform, code string) string {
	if why := ev.Missing[mvKey(platform, code)]; why != "" {
		return why
	}
	scenario := scenarioOf(code)
	excluded := 0
	for _, r := range ev.Runs {
		if r.Device.Platform != platform || r.Scenario != scenario {
			continue
		}
		switch r.Status {
		case RunInterrupted, RunFailed:
			excluded++
		case RunUsed, RunSuperseded:
			return scenario + " nu are citirile așteptate"
		}
	}
	if excluded > 0 {
		return fmt.Sprintf("%s: toate rulările (%d) sunt întrerupte sau eșuate", scenario, excluded)
	}
	return scenario + " nu a rulat"
}

// codeRank orders the codes of a device's table: MV1 to MV15, then G9; the grid sizes in order.
func codeRank(code string) (int, int) {
	base, tiles, _ := strings.Cut(code, "/")
	order := []string{"MV1", "MV2", "MV3", "MV4", "MV5", "MV6", "MV7", "MV8", "MV9", "MV10", "MV12", "MV13", "MV15", "G9"}
	n, _ := strconv.Atoi(tiles)
	for i, c := range order {
		if c == base {
			return i, n
		}
	}
	return len(order), n
}

// codeLess is the order of the rows of a table.
func codeLess(a, b string) bool {
	ai, an := codeRank(a)
	bi, bn := codeRank(b)
	if ai != bi {
		return ai < bi
	}
	if an != bn {
		return an < bn
	}
	return a < b
}

// thresholdOf is the threshold of a code as the report writes it (spec 16.3).
func thresholdOf(code string) string {
	base, _, _ := strings.Cut(code, "/")
	switch base {
	case "MV1":
		return "p90 ≤ 1,5 s (20 porniri)"
	case "MV2":
		return "p95 < 1 s (100 mostre)"
	case "MV3":
		return "fps ≥ 90 % din sursă (≥ 10,8 la 12 fps); pierdute ≤ 2 %"
	case "MV4":
		return "ecran complet hardware; toate plăcile grilei hardware"
	case "MV5":
		return "termic ≤ MODERATE; fps final ≥ 10,8; baterie ≤ 15 %/h"
	case "MV6":
		return "16 plăci ≤ 25 % CPU; flux principal ≤ 10 % CPU"
	case "MV7":
		return "creștere memorie ≤ 10 %; 0 reporniri neprovocate"
	case "MV8":
		return "5 min în PiP fără pauze de peste 2 s"
	case "MV9":
		return "live reluat în ≤ 3 s după 2 min în fundal"
	case "MV10":
		return "pornire ≤ 2 s; salt ≤ 1,5 s (toate cele 20)"
	case "MV12":
		return "perechea ICE aleasă nu e relay"
	case "MV13":
		return "ambele capete host sau prflx, fără relay"
	case "MV15":
		return "≤ 3 s pe AND-1 (10 rulări)"
	case "G9":
		return "fiecare cale: ok sau retragere pe substream etichetată; fără fallback-failed"
	}
	return ""
}
