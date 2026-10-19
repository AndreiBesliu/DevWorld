package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// The synthetic result sets of the tests. Every set below passes every
// threshold; a test changes the one value it is about.

const (
	androidDevice = "android"
	windowsDevice = "windows-tauri"
)

// fixture collects the runs of one device. Each run gets a start time one
// minute after the previous one and a file name like the hub's.
type fixture struct {
	device  Device
	results []*Result
}

func newFixture(device Device) *fixture { return &fixture{device: device} }

func (f *fixture) add(scenario string, samples map[string][]float64, notes ...string) *Result {
	n := len(f.results) + 1
	r := &Result{
		Scenario:  scenario,
		Device:    f.device,
		StartedAt: fmt.Sprintf("2026-10-18T10:%02d:00.000Z", n),
		DurationS: 60,
		Samples:   samples,
		Notes:     notes,
		File:      fmt.Sprintf("%s-20261018-10%02d00-%s.json", f.device.Platform, n, strings.ToLower(scenario)),
	}
	f.results = append(f.results, r)
	return r
}

func (f *fixture) list() []Result {
	out := make([]Result, len(f.results))
	for i, r := range f.results {
		out[i] = *r
	}
	return out
}

// rep is n copies of v.
func rep(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// ramp is n values from `from` to `to`, both included.
func ramp(from, to float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = from + (to-from)*float64(i)/float64(n-1)
	}
	return out
}

// battery is 60 readings, one per 10 s, that start at `start` and end at `start-drop`.
func battery(start, drop float64) []float64 {
	out := rep(start, 60)
	for i := 30; i < 60; i++ {
		out[i] = start - drop
	}
	return out
}

var (
	androidModel = Device{Platform: androidDevice, Model: "samsung SM-A245F", OS: "Android 14 (API 34)", WebView: "com.google.android.webview 120.0.6099.230"}
	windowsModel = Device{Platform: windowsDevice, Model: "AMD Ryzen 5 5600G with Radeon Graphics", OS: "Windows 11 Pro", WebView: "WebView2 154.0.4258.62"}
)

func browserDevice(platform string) Device {
	return Device{Platform: platform, OS: "Windows", WebView: map[string]string{"chrome": "Chrome/141.0.0.0", "edge": "Edg/141.0.0.0", "firefox": "Firefox/132.0"}[platform]}
}

// gridSamples is a passing S3 run of `tiles` substream tiles.
func gridSamples(tiles int) map[string][]float64 {
	return map[string][]float64{
		"gridFps":        rep(11.95, 300),
		"tileFps":        rep(11.9, tiles),
		"tileDroppedPct": rep(0.2, tiles),
		"mainFps":        rep(19.8, 60),
		"mainDroppedPct": {0.1},
	}
}

// androidGrid is a passing S3 run on the phone: hardware decoders, cool, light on battery.
func androidGrid(tiles int) map[string][]float64 {
	s := gridSamples(tiles)
	s["tileHardware"] = rep(1, tiles)
	s["mainHardware"] = []float64{1}
	s["thermal"] = rep(1, 60)
	s["batteryPct"] = battery(80, 1)
	return s
}

// windowsGrid is a passing S3 run on the PC: 18 % CPU for the grid, 6 % for the main stream.
func windowsGrid(tiles int) map[string][]float64 {
	s := gridSamples(tiles)
	s["cpuPctApp"] = rep(18, 60)
	s["cpuPctTotal"] = rep(35, 60)
	s["mainCpuPctApp"] = rep(6, 12)
	s["memMb"] = rep(900, 60)
	return s
}

// stability is a passing S4 run with 61 readings each of the shell's memory and the JS heap.
func stability() map[string][]float64 {
	return map[string][]float64{
		"memMb":             rep(400, 61),
		"jsHeapMb":          rep(30, 61),
		"playerErrors":      {0},
		"pageSwitches":      {29},
		"fullscreenToggles": {20},
	}
}

func commonRuns(f *fixture) {
	f.add("S1", map[string][]float64{"firstFrameMs": ramp(300, 900, 20)}, "sub: H264")
	f.add("S2", map[string][]float64{
		"subLatencyMs":  ramp(80, 300, 100),
		"mainLatencyMs": ramp(100, 400, 100),
		"subReliable":   rep(1, 100),
		"mainReliable":  rep(1, 100),
		"clockRttMs":    {3, 4},
	})
	f.add("S9", map[string][]float64{"mx-h265.subFirstFrameMs": {20}, "mx-high51.firstFrameMs": {120}},
		"mx-h265/main: error CODEC_UNSUPPORTED", "mx-h265/sub: bound H264", "mx-h265: fallback",
		"mx-high51/main: bound H264", "mx-high51: ok")
	f.add("S10", nil, "pair: host/host", "prompt: no")
}

// androidRuns is a passing Android set: every scenario the owner's guide has for the phone.
func androidRuns() []Result {
	f := newFixture(androidModel)
	commonRuns(f)
	f.add("S3-4", androidGrid(4))
	f.add("S3-9", androidGrid(9))
	f.add("S4", stability())
	f.add("S5", map[string][]float64{"resumeMs": {1200}, "backgroundS": {121}})
	f.add("S6", map[string][]float64{"pipFrames": {3000}, "pipMaxGapMs": {150}, "pipS": {300}})
	f.add("S7", map[string][]float64{"startMs": rep(900, 20), "seekMs": rep(700, 20)})
	f.add("S8", map[string][]float64{"argon2idMs": rep(2100, 10)})
	return f.list()
}

// windowsRuns is a passing Windows set.
func windowsRuns() []Result {
	f := newFixture(windowsModel)
	f.add("S3-16", windowsGrid(16))
	f.add("S4", stability())
	f.add("S10", nil, "pair: host/host", "prompt: no")
	return f.list()
}

// webRuns is a passing set for Chrome, Edge and Firefox: S10 on each.
func webRuns() []Result {
	var out []Result
	for _, platform := range []string{"chrome", "edge", "firefox"} {
		f := newFixture(browserDevice(platform))
		f.add("S10", nil, "pair: host/prflx", "prompt: n/a")
		out = append(out, f.list()...)
	}
	return out
}

func allRuns() []Result {
	var out []Result
	out = append(out, androidRuns()...)
	out = append(out, windowsRuns()...)
	out = append(out, webRuns()...)
	return out
}

// find returns the run of `platform` and `scenario` that has the given index among such runs.
func find(t *testing.T, results []Result, platform, scenario string) *Result {
	t.Helper()
	for i := range results {
		if results[i].Device.Platform == platform && results[i].Scenario == scenario {
			return &results[i]
		}
	}
	t.Fatalf("the set has no %s run of %s", scenario, platform)
	return nil
}

// outcome is "pass", "fail" or "absent".
func outcome(mv map[string]MV, device, code string) string {
	v, ok := mv[mvKey(device, code)]
	switch {
	case !ok:
		return "absent"
	case v.Pass:
		return "pass"
	default:
		return "fail"
	}
}

// mvSet is a map with every key passing except those in `failing`, which fail, and those in `absent`.
func mvSet(keys []string, failing, absent []string) map[string]MV {
	skip := map[string]bool{}
	for _, k := range absent {
		skip[k] = true
	}
	bad := map[string]bool{}
	for _, k := range failing {
		bad[k] = true
	}
	out := map[string]MV{}
	for _, k := range keys {
		if skip[k] {
			continue
		}
		out[k] = MV{Pass: !bad[k]}
	}
	return out
}

// readText reads a file that a test wrote or that testdata holds.
func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a path of the test's own temporary or testdata folders
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// rowOf is the first line of the Markdown report that starts with prefix, for example "| MV1 |".
func rowOf(t *testing.T, md, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("the report has no line that starts with %q", prefix)
	return ""
}
