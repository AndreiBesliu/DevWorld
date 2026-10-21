package main

import (
	"fmt"
	"math"
	"slices"
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

// webPlatforms are the browsers. The guide runs them on the owner's Windows PC (spec §16.3,
// WEB), so a browser run on another OS family is a device of its own (deviceKey, hub-4).
var webPlatforms = []string{"chrome", "edge", "firefox"}

// expectedWebOS is the OS family of the browsers' runs that the verdicts read.
const expectedWebOS = "windows"

// osFamily is the OS family of a browser result in lower case ("windows", "android", "linux",
// "macos"), as device.ts names it; empty when the result does not say. It names a CSV file, so
// it keeps only letters and digits, and at most 16 of them.
func osFamily(os string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(os), " ")
	family := strings.Map(func(c rune) rune {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			return c
		}
		return -1
	}, strings.ToLower(first))
	return family[:min(len(family), 16)]
}

// deviceKey is the device that a result belongs to in the report: its platform, except for a
// browser on an OS family other than Windows, whose key adds the family ("chrome-android").
// A run of Chrome on the phone then never replaces nor merges with the PC's Chrome (hub-4).
// A browser result that names no OS stays with its platform.
func deviceKey(d Device) string {
	family := osFamily(d.OS)
	if !slices.Contains(webPlatforms, d.Platform) || family == "" || family == expectedWebOS {
		return d.Platform
	}
	return d.Platform + "-" + family
}

// basePlatform is the platform of a device key: "chrome" for "chrome-android".
func basePlatform(key string) string {
	for _, p := range webPlatforms {
		if strings.HasPrefix(key, p+"-") {
			return p
		}
	}
	return key
}

// unexpectedOS says whether a device key is a browser on an OS that the guide does not use.
func unexpectedOS(key string) bool { return basePlatform(key) != key }

// Device is the device of a result. Nothing in it names an address, a network
// or a serial number (PH0-22); the user agent is not read at all.
type Device struct {
	Platform string `json:"platform"`
	Model    string `json:"model"`
	OS       string `json:"os"`
	WebView  string `json:"webview"`
	// RAMMb is the physical memory that the shell reports, in MiB; 0 when it does not.
	RAMMb float64 `json:"ramMb"`
	// GPU names the display adapters (the Windows shell only).
	GPU string `json:"gpu"`
}

// Interruption says why a run ended as interrupted (app/src/results.ts InterruptReason):
// hidden (AtS), session-lost (AtS, How; R49), charging (AtS; R48), early-return
// (BackgroundS) or pip-left (PipS). Results written before the field existed have none.
type Interruption struct {
	Kind        string  `json:"kind"`
	AtS         float64 `json:"atS"`
	How         string  `json:"how"`
	BackgroundS float64 `json:"backgroundS"`
	PipS        float64 `json:"pipS"`
}

// Result is one results file as spikehub stores it: the app's ScenarioResult
// (app/src/results.ts) and the hub's mdnsUnavailable.
type Result struct {
	Scenario  string  `json:"scenario"`
	Device    Device  `json:"device"`
	StartedAt string  `json:"startedAt"`
	DurationS float64 `json:"durationS"`
	// Interrupted: the run ended before it measured what it should (Review Focus 2, R48, R49).
	Interrupted bool `json:"interrupted"`
	// Interruption says why, when Interrupted.
	Interruption *Interruption `json:"interruption"`
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
	// Device is the device key of the results (deviceKey): android, windows-tauri, chrome,
	// edge, firefox, or a browser on an unexpected OS such as chrome-android.
	Device string
	// Text is the "Valoare" cell of the report; Value and Unit when empty.
	Text string
	// Notes are details for the lines under the device's table.
	Notes []string
}

// cell is the "Valoare" cell of the report: Text, or the value with its unit when there is none.
func (v MV) cell() string {
	if v.Text != "" {
		return v.Text
	}
	return number(v.Value, 1) + " " + v.Unit
}

// mvKey is the key of a threshold of a device in the map that ComputeMV returns.
func mvKey(device, code string) string { return device + "/" + code }

// RunStatus says what a run counts for.
type RunStatus int

// The statuses of a run.
const (
	// RunUsed: the newest valid run of its scenario on its device.
	RunUsed RunStatus = iota
	// RunSuperseded: valid, but a newer valid run of the same scenario exists.
	RunSuperseded
	// RunInterrupted: the app ended the run early, for any reason (Interruption).
	RunInterrupted
	// RunFailed: the scenario stopped with an error, so its readings are incomplete.
	RunFailed
	// RunInvalid: an S3 run that a reading shows on the charger (R48), although the app
	// did not end it: battery and heat cannot be judged.
	RunInvalid
	// RunInformative: an S4 run of a desktop path that did not page 16 tiles (R45). It is
	// listed with its values, but MV7 there is judged only on 16 tiles.
	RunInformative
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

// onCharger: an S3 run with a reading that says the phone was charging (R48), in the grid
// (`charging`) or in the fullscreen part (`mainCharging`).
func (r Result) onCharger() bool {
	if !strings.HasPrefix(r.Scenario, "S3-") {
		return false
	}
	return slices.Contains(r.Samples["charging"], 1) || slices.Contains(r.Samples["mainCharging"], 1)
}

// desktopTiles is the number of tiles of an S4 run (its `tiles` sample), 0 when it has none.
func (r Result) desktopTiles() int {
	if t := r.Samples["tiles"]; len(t) > 0 {
		return int(t[0])
	}
	return 0
}

// desktopGridTiles is the grid that MV7 needs on a desktop path (R45, spec G6).
const desktopGridTiles = 16

// informativeOnly: an S4 run of a desktop path that did not page 16 tiles (R45).
func (r Result) informativeOnly() bool {
	return r.Scenario == "S4" && basePlatform(r.Device.Platform) != platformAndroid && r.desktopTiles() != desktopGridTiles
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
// interrupted, failed and invalid runs are left out, a desktop S4 without 16 tiles is only
// informative, and of the valid runs of one scenario on one device only the newest counts.
// The device of a run is its deviceKey, which it keeps in Device.Platform from here on.
func classify(results []Result) []Run {
	runs := make([]Run, len(results))
	for i, r := range results {
		runs[i] = Run{Result: r}
		runs[i].Device.Platform = deviceKey(r.Device)
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
		case runs[i].onCharger():
			runs[i].Status = RunInvalid
		case runs[i].informativeOnly():
			runs[i].Status = RunInformative
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

// jitterFlagMs: a run whose hub jitter is over this is flagged (Review Focus 4).
const jitterFlagMs = 20.0

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

// EarlierFailure is a run that does not decide a threshold, with what it said about it: a
// valid run that a newer valid run of the same scenario replaced and that failed a threshold
// of its scenario (R40), or an informative run (RunInformative).
type EarlierFailure struct {
	File string
	// MV is what that run said on its own: its value, with the text of the "Valoare" cell.
	MV MV
}

// evaluation is everything the thresholds say about a set of results.
type evaluation struct {
	Runs []Run
	// MV holds the thresholds that could be judged, by mvKey.
	MV map[string]MV
	// Missing says, for a threshold that was expected but could not be judged, why not.
	Missing map[string]string
	// Earlier holds, by mvKey, the superseded valid runs that failed the threshold, oldest first.
	// They do not decide anything; the report shows them beside the value, so that running a
	// scenario again until it passes cannot hide a failure (R40).
	Earlier map[string][]EarlierFailure
	// Informative holds, by mvKey, what the informative runs said (a desktop S4 without 16
	// tiles, R45): listed beside the row, never deciding it.
	Informative map[string][]EarlierFailure
}

// ComputeMV judges the thresholds of every device in `results`. The map is keyed by
// mvKey(device, code), for example "android/MV9". A threshold that the results cannot
// judge (its scenario did not run, was interrupted, or lacks readings) is not in the map.
// Interrupted runs and runs that failed do not count; of several valid runs of one
// scenario on one device the newest decides (R40: the report still shows the older ones
// that failed).
func ComputeMV(results []Result) map[string]MV { return evaluate(results).MV }

func evaluate(results []Result) evaluation {
	ev := evaluation{
		Runs: classify(results), MV: map[string]MV{}, Missing: map[string]string{},
		Earlier: map[string][]EarlierFailure{}, Informative: map[string][]EarlierFailure{},
	}
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
	ev.earlierFailures()
	return ev
}

// earlierFailures records, for every superseded run, the thresholds of its own scenario that it
// failed, and for every informative run what it said. Each run is judged alone: R40 asks what
// that run said, whatever the newest one says.
func (ev *evaluation) earlierFailures() {
	for i := range ev.Runs { // in the order of the report, so oldest first
		r := &ev.Runs[i]
		if r.Status != RunSuperseded && r.Status != RunInformative {
			continue
		}
		alone := evaluation{MV: map[string]MV{}, Missing: map[string]string{}}
		(&evaluator{platform: r.Device.Platform, runs: map[string]*Run{r.Scenario: r}, ev: &alone}).all()
		for key, v := range alone.MV {
			switch {
			case !measures(v.Code, r.Scenario):
			case r.Status == RunInformative:
				ev.Informative[key] = append(ev.Informative[key], EarlierFailure{File: r.File, MV: v})
			case !v.Pass:
				ev.Earlier[key] = append(ev.Earlier[key], EarlierFailure{File: r.File, MV: v})
			}
		}
	}
}

// tri is the state of one condition of a threshold.
type tri int

const (
	unknown tri = iota
	passed
	failed
	// notApplicable: a condition that does not apply on this device; its text is shown, and
	// it neither passes nor fails the threshold.
	notApplicable
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

func notApplying(why string) check { return check{notApplicable, why} }

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
		case passed, notApplicable:
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

// reasonMissing says why an expected threshold of a device has no value.
func (ev evaluation) reasonMissing(platform, code string) string {
	if why := ev.Missing[mvKey(platform, code)]; why != "" {
		return why
	}
	scenario := scenarioOf(code)
	excluded, informative := 0, 0
	for _, r := range ev.Runs {
		if r.Device.Platform != platform || r.Scenario != scenario {
			continue
		}
		switch r.Status {
		case RunInterrupted, RunFailed, RunInvalid:
			excluded++
		case RunInformative:
			informative++
		case RunUsed, RunSuperseded:
			return scenario + " nu are citirile așteptate"
		}
	}
	switch {
	case informative > 0:
		return fmt.Sprintf("%s a rulat fără 16 plăci; pe PC, MV7 se judecă doar pe o rulare S4 cu 16 plăci (G6, R45)", scenario)
	case excluded > 0:
		return fmt.Sprintf("%s: toate rulările (%d) sunt invalide (întrerupte, eșuate sau la încărcător; vezi «Rulări invalide»)", scenario, excluded)
	}
	return scenario + " nu a rulat"
}

// codeRank orders the codes of a device's table: MV1 to MV15, then G9; the grid sizes in order,
// and a part that is not a grid size (MV4/main) after them.
func codeRank(code string) (int, int) {
	base, tiles, _ := strings.Cut(code, "/")
	order := []string{"MV1", "MV2", "MV3", "MV4", "MV5", "MV6", "MV7", "MV8", "MV9", "MV10", "MV12", "MV13", "MV15", "G9"}
	n, err := strconv.Atoi(tiles)
	if tiles != "" && err != nil {
		n = math.MaxInt32
	}
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
