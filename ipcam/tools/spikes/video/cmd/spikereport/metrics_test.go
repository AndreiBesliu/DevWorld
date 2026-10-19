package main

import (
	"math"
	"strings"
	"testing"
)

func TestComputeMV_AllPassingSet(t *testing.T) {
	mv := ComputeMV(allRuns())
	want := map[string][]string{
		androidDevice: {"MV1", "MV2", "MV3/4", "MV3/9", "MV4/9", "MV5/9", "MV7", "MV8", "MV9", "MV10", "MV12", "MV15", "G9"},
		windowsDevice: {"MV3/16", "MV6", "MV7", "MV12"},
		"chrome":      {"MV13"},
		"edge":        {"MV13"},
		"firefox":     {"MV13"},
	}
	for device, codes := range want {
		for _, code := range codes {
			if got := outcome(mv, device, code); got != "pass" {
				t.Errorf("%s %s = %s, want pass (%+v)", device, code, got, mv[mvKey(device, code)])
			}
		}
	}
	for key, v := range mv {
		if v.Code == "" || v.Device == "" || v.Threshold == "" || mvKey(v.Device, v.Code) != key {
			t.Errorf("%s: incomplete MV %+v", key, v)
		}
	}
}

func TestComputeMV_ExcludesInterruptedRuns(t *testing.T) {
	t.Run("a newer interrupted run does not replace the valid one", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S1", map[string][]float64{"firstFrameMs": rep(500, 20)})
		bad := f.add("S1", map[string][]float64{"firstFrameMs": rep(9000, 20)}, "interrupted: the page was hidden 12 s into the run")
		bad.Interrupted = true
		mv := ComputeMV(f.list())
		if got := mv[mvKey(androidDevice, "MV1")]; !got.Pass || got.Value != 500 {
			t.Errorf("MV1 = %+v, want the valid run's p90 of 500 ms", got)
		}
	})
	t.Run("an interrupted run with good numbers does not rescue a bad one", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S1", map[string][]float64{"firstFrameMs": rep(9000, 20)})
		good := f.add("S1", map[string][]float64{"firstFrameMs": rep(500, 20)})
		good.Interrupted = true
		if got := outcome(ComputeMV(f.list()), androidDevice, "MV1"); got != "fail" {
			t.Errorf("MV1 = %s, want fail", got)
		}
	})
	t.Run("only an interrupted run gives no value", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S1", map[string][]float64{"firstFrameMs": rep(500, 20)}).Interrupted = true
		if got := outcome(ComputeMV(f.list()), androidDevice, "MV1"); got != "absent" {
			t.Errorf("MV1 = %s, want absent", got)
		}
	})
}

func TestComputeMV_ExcludesFailedRuns(t *testing.T) {
	f := newFixture(androidModel)
	s := stability()
	s["memMb"] = s["memMb"][:20] // the run stopped half way
	delete(s, "playerErrors")
	f.add("S4", s, "failed: Error: the hub closed the data channel")
	if got := outcome(ComputeMV(f.list()), androidDevice, "MV7"); got != "absent" {
		t.Errorf("MV7 = %s, want absent: a run that failed is not a measurement", got)
	}
}

func TestComputeMV_TheNewestValidRunOfAScenarioCounts(t *testing.T) {
	f := newFixture(androidModel)
	f.add("S1", map[string][]float64{"firstFrameMs": rep(9000, 20)})
	f.add("S1", map[string][]float64{"firstFrameMs": rep(500, 20)})
	if got := outcome(ComputeMV(f.list()), androidDevice, "MV1"); got != "pass" {
		t.Errorf("MV1 = %s, want pass from the newer run", got)
	}
	// The order in the slice does not matter: the start time does.
	rs := f.list()
	rs[0], rs[1] = rs[1], rs[0]
	if got := outcome(ComputeMV(rs), androidDevice, "MV1"); got != "pass" {
		t.Errorf("MV1 with the runs swapped = %s, want pass", got)
	}
}

func TestComputeMV_KeepsDevicesApart(t *testing.T) {
	phone := newFixture(androidModel)
	phone.add("S1", map[string][]float64{"firstFrameMs": rep(500, 20)})
	pc := newFixture(browserDevice("chrome"))
	pc.add("S1", map[string][]float64{"firstFrameMs": rep(5000, 20)})
	mv := ComputeMV(append(phone.list(), pc.list()...))
	if outcome(mv, androidDevice, "MV1") != "pass" || outcome(mv, "chrome", "MV1") != "fail" {
		t.Errorf("android MV1 = %s, chrome MV1 = %s, want pass and fail", outcome(mv, androidDevice, "MV1"), outcome(mv, "chrome", "MV1"))
	}
	if v := mv[mvKey("chrome", "MV1")]; v.Device != "chrome" || v.Code != "MV1" {
		t.Errorf("chrome MV1 = %+v", v)
	}
}

func TestPercentile(t *testing.T) {
	twenty := ramp(1, 20, 20)
	for _, tc := range []struct {
		q    float64
		want float64
	}{{0.9, 18}, {0.95, 19}, {0.5, 10}, {1, 20}, {0, 1}} {
		if got := percentile(twenty, tc.q); got != tc.want {
			t.Errorf("p%v of 1..20 = %v, want %v (nearest rank, like the app)", tc.q*100, got, tc.want)
		}
	}
	if got := percentile(nil, 0.9); !math.IsNaN(got) {
		t.Errorf("percentile of nothing = %v, want NaN", got)
	}
}

func TestComputeMV_MV1(t *testing.T) {
	slow := func(n int, ms float64) []float64 { return append(rep(500, 20-n), rep(ms, n)...) }
	tests := []struct {
		name    string
		samples []float64
		want    string
	}{
		{"all fast", rep(800, 20), "pass"},
		{"exactly 1500 ms", rep(1500, 20), "pass"},
		{"1501 ms", rep(1501, 20), "fail"},
		{"two slow starts are the tail above p90", slow(2, 9000), "pass"},
		{"three slow starts are not", slow(3, 9000), "fail"},
		{"two starts without a first frame are the tail", rep(500, 18), "pass"},
		{"three starts without a first frame are not", rep(500, 17), "fail"},
		{"no start showed a frame", nil, "fail"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(androidModel)
			f.add("S1", map[string][]float64{"firstFrameMs": tc.samples})
			if got := outcome(ComputeMV(f.list()), androidDevice, "MV1"); got != tc.want {
				t.Errorf("MV1 = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestComputeMV_MV2(t *testing.T) {
	run := func(sub, main []float64) []Result {
		f := newFixture(androidModel)
		f.add("S2", map[string][]float64{"subLatencyMs": sub, "mainLatencyMs": main, "subReliable": rep(1, len(sub)), "mainReliable": rep(1, len(main)), "clockRttMs": {3}})
		return f.list()
	}
	tail := func(n int, ms float64) []float64 { return append(rep(300, 100-n), rep(ms, n)...) }
	tests := []struct {
		name      string
		sub, main []float64
		want      string
	}{
		{"both fast", rep(300, 100), rep(400, 100), "pass"},
		{"999 ms is under a second", rep(999, 100), rep(300, 100), "pass"},
		{"1000 ms is not under a second", rep(1000, 100), rep(300, 100), "fail"},
		{"five slow samples are above p95", tail(5, 5000), rep(300, 100), "pass"},
		{"six are not", tail(6, 5000), rep(300, 100), "fail"},
		{"the main stream counts too", rep(300, 100), tail(6, 5000), "fail"},
		{"too few samples is no measurement", rep(300, 60), rep(300, 100), "absent"},
		{"too few samples does not hide a failure", rep(2000, 100), rep(300, 60), "fail"},
		{"a missing profile is no measurement", rep(300, 100), nil, "absent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcome(ComputeMV(run(tc.sub, tc.main)), androidDevice, "MV2"); got != tc.want {
				t.Errorf("MV2 = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestComputeMV_MV2_ReportsAnUnreliableClock(t *testing.T) {
	f := newFixture(androidModel)
	reliable := rep(1, 100)
	reliable[3], reliable[40] = 0, 0
	f.add("S2", map[string][]float64{
		"subLatencyMs": rep(300, 100), "mainLatencyMs": rep(300, 100),
		"subReliable": reliable, "mainReliable": rep(1, 100), "clockRttMs": {3, 80},
	})
	v := ComputeMV(f.list())[mvKey(androidDevice, "MV2")]
	if !v.Pass {
		t.Errorf("an unreliable clock estimate must not drop samples silently or fail MV2: %+v", v)
	}
	joined := strings.Join(v.Notes, "\n")
	if !strings.Contains(joined, "2 din 100") || !strings.Contains(joined, "80") {
		t.Errorf("notes = %q, want the 2 of 100 unreliable samples and the largest round trip (80 ms)", joined)
	}
}

func TestComputeMV_MV3(t *testing.T) {
	grid := func(mut func(s map[string][]float64)) []Result {
		f := newFixture(androidModel)
		s := androidGrid(9)
		mut(s)
		f.add("S3-9", s)
		return f.list()
	}
	tests := []struct {
		name string
		mut  func(s map[string][]float64)
		want string
	}{
		{"all tiles fluent", func(map[string][]float64) {}, "pass"},
		{"exactly 90 % of 12 fps", func(s map[string][]float64) { s["tileFps"][4] = 10.8 }, "pass"},
		{"just under", func(s map[string][]float64) { s["tileFps"][4] = 10.79 }, "fail"},
		{"one tile did not start", func(s map[string][]float64) { s["tileFps"][8] = 0 }, "fail"},
		{"exactly 2 % dropped", func(s map[string][]float64) { s["tileDroppedPct"][1] = 2 }, "pass"},
		{"just over 2 % dropped", func(s map[string][]float64) { s["tileDroppedPct"][1] = 2.01 }, "fail"},
		{"a tile missing from the result", func(s map[string][]float64) { s["tileFps"] = s["tileFps"][:8] }, "absent"},
		{"no per-tile numbers", func(s map[string][]float64) { delete(s, "tileFps"); delete(s, "tileDroppedPct") }, "absent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcome(ComputeMV(grid(tc.mut)), androidDevice, "MV3/9"); got != tc.want {
				t.Errorf("MV3/9 = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestComputeMV_MV3_IsPerGridSize(t *testing.T) {
	f := newFixture(androidModel)
	f.add("S3-4", androidGrid(4))
	bad := androidGrid(9)
	bad["tileFps"][0] = 3
	f.add("S3-9", bad)
	mv := ComputeMV(f.list())
	if outcome(mv, androidDevice, "MV3/4") != "pass" || outcome(mv, androidDevice, "MV3/9") != "fail" {
		t.Errorf("MV3/4 = %s, MV3/9 = %s, want pass and fail", outcome(mv, androidDevice, "MV3/4"), outcome(mv, androidDevice, "MV3/9"))
	}
}

func TestComputeMV_MV4(t *testing.T) {
	run := func(mut func(s map[string][]float64)) []Result {
		f := newFixture(androidModel)
		s := androidGrid(9)
		mut(s)
		f.add("S3-9", s, "decoders: c2.exynos.h264.decoder ×9")
		return f.list()
	}
	tests := []struct {
		name string
		mut  func(s map[string][]float64)
		want string
	}{
		{"all hardware", func(map[string][]float64) {}, "pass"},
		{"one tile in software", func(s map[string][]float64) { s["tileHardware"][2] = 0 }, "fail"},
		{"the main stream in software", func(s map[string][]float64) { s["mainHardware"][0] = 0 }, "fail"},
		{"a software tile outweighs a missing main reading", func(s map[string][]float64) { s["tileHardware"][2] = 0; delete(s, "mainHardware") }, "fail"},
		{"no decoder info", func(s map[string][]float64) { delete(s, "tileHardware"); delete(s, "mainHardware") }, "absent"},
		{"tiles without the main stream", func(s map[string][]float64) { delete(s, "mainHardware") }, "absent"},
		{"fewer tile readings than tiles", func(s map[string][]float64) { s["tileHardware"] = s["tileHardware"][:5] }, "absent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcome(ComputeMV(run(tc.mut)), androidDevice, "MV4/9"); got != tc.want {
				t.Errorf("MV4/9 = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestComputeMV_MV5(t *testing.T) {
	run := func(device Device, mut func(s map[string][]float64)) []Result {
		f := newFixture(device)
		s := androidGrid(9)
		mut(s)
		f.add("S3-9", s)
		return f.list()
	}
	hot := func(level float64) func(map[string][]float64) {
		return func(s map[string][]float64) { s["thermal"] = append(rep(1, 50), rep(level, 10)...) }
	}
	tests := []struct {
		name string
		mut  func(s map[string][]float64)
		want string
	}{
		{"cool, steady, light on battery", func(map[string][]float64) {}, "pass"},
		{"MODERATE is the limit", hot(2), "pass"},
		{"SEVERE is over it", hot(3), "fail"},
		{"fps falls in the last 2 minutes", func(s map[string][]float64) { s["gridFps"] = append(rep(11.95, 240), rep(8, 60)...) }, "fail"},
		{"a dip earlier in the run is not heat at the end", func(s map[string][]float64) {
			s["gridFps"] = append(append(rep(11.95, 100), rep(5, 20)...), rep(11.95, 180)...)
		}, "pass"},
		{"fps in the last 2 minutes exactly 90 % of 12", func(s map[string][]float64) { s["gridFps"] = rep(10.8, 300) }, "pass"},
		{"fps in the last 2 minutes just under", func(s map[string][]float64) { s["gridFps"] = rep(10.79, 300) }, "fail"},
		{"12 % an hour", func(s map[string][]float64) { s["batteryPct"] = battery(80, 2) }, "pass"},
		{"exactly 15 % an hour (3 points in 72 intervals of 10 s)", func(s map[string][]float64) {
			b := rep(80, 73)
			for i := 36; i < 73; i++ {
				b[i] = 77
			}
			s["batteryPct"] = b
		}, "pass"},
		{"just over 15 % an hour (3 points in 70 intervals)", func(s map[string][]float64) {
			b := rep(80, 71)
			for i := 35; i < 71; i++ {
				b[i] = 77
			}
			s["batteryPct"] = b
		}, "fail"},
		{"18 % an hour", func(s map[string][]float64) { s["batteryPct"] = battery(80, 3) }, "fail"},
		{"charging is not a measurement", func(s map[string][]float64) { s["batteryPct"] = battery(70, -3) }, "absent"},
		{"no shell metrics", func(s map[string][]float64) { delete(s, "thermal"); delete(s, "batteryPct") }, "absent"},
		{"a hot phone fails even without a battery reading", func(s map[string][]float64) { hot(4)(s); delete(s, "batteryPct") }, "fail"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcome(ComputeMV(run(androidModel, tc.mut)), androidDevice, "MV5/9"); got != tc.want {
				t.Errorf("MV5/9 = %s, want %s", got, tc.want)
			}
		})
	}
	t.Run("only the phone has a thermal limit", func(t *testing.T) {
		mv := ComputeMV(run(windowsModel, func(map[string][]float64) {}))
		if got := outcome(mv, windowsDevice, "MV5/9"); got != "absent" {
			t.Errorf("MV5/9 on Windows = %s, want absent", got)
		}
	})
}

func TestComputeMV_MV6(t *testing.T) {
	run := func(mut func(s map[string][]float64)) []Result {
		f := newFixture(windowsModel)
		s := windowsGrid(16)
		mut(s)
		f.add("S3-16", s)
		return f.list()
	}
	tests := []struct {
		name string
		mut  func(s map[string][]float64)
		want string
	}{
		{"18 % and 6 %", func(map[string][]float64) {}, "pass"},
		{"exactly 25 % and 10 %", func(s map[string][]float64) { s["cpuPctApp"] = rep(25, 60); s["mainCpuPctApp"] = rep(10, 12) }, "pass"},
		{"grid over 25 %", func(s map[string][]float64) { s["cpuPctApp"] = rep(26, 60) }, "fail"},
		{"main stream over 10 %", func(s map[string][]float64) { s["mainCpuPctApp"] = rep(10.5, 12) }, "fail"},
		{"no main stream reading", func(s map[string][]float64) { delete(s, "mainCpuPctApp") }, "absent"},
		{"a failing grid outweighs a missing main stream reading", func(s map[string][]float64) { s["cpuPctApp"] = rep(40, 60); delete(s, "mainCpuPctApp") }, "fail"},
		{"no shell metrics", func(s map[string][]float64) { delete(s, "cpuPctApp"); delete(s, "mainCpuPctApp") }, "absent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcome(ComputeMV(run(tc.mut)), windowsDevice, "MV6"); got != tc.want {
				t.Errorf("MV6 = %s, want %s", got, tc.want)
			}
		})
	}
	t.Run("the main stream may come from a smaller grid run", func(t *testing.T) {
		f := newFixture(windowsModel)
		s := windowsGrid(16)
		delete(s, "mainCpuPctApp")
		f.add("S3-16", s)
		f.add("S3-9", windowsGrid(9))
		if got := outcome(ComputeMV(f.list()), windowsDevice, "MV6"); got != "pass" {
			t.Errorf("MV6 = %s, want pass", got)
		}
	})
	t.Run("the grid CPU is not judged on a smaller grid", func(t *testing.T) {
		f := newFixture(windowsModel)
		f.add("S3-9", windowsGrid(9))
		if got := outcome(ComputeMV(f.list()), windowsDevice, "MV6"); got != "absent" {
			t.Errorf("MV6 from 9 tiles only = %s, want absent: the threshold is for 16 tiles", got)
		}
	})
	t.Run("only the PC has a CPU threshold", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S3-16", windowsGrid(16))
		if got := outcome(ComputeMV(f.list()), androidDevice, "MV6"); got != "absent" {
			t.Errorf("MV6 on Android = %s, want absent", got)
		}
	})
}

func TestComputeMV_MV7(t *testing.T) {
	// 61 readings, 30 s apart. The growth is the mean of the last 12 against the mean of 12 from the third on.
	grow := func(base, last float64) []float64 {
		out := rep(base, 61)
		for i := 49; i < 61; i++ {
			out[i] = last
		}
		return out
	}
	tests := []struct {
		name string
		mut  func(s map[string][]float64)
		want string
	}{
		{"flat", func(map[string][]float64) {}, "pass"},
		{"9 % more", func(s map[string][]float64) { s["memMb"] = grow(400, 436) }, "pass"},
		{"exactly 10 % more", func(s map[string][]float64) { s["memMb"] = grow(400, 440) }, "pass"},
		{"11 % more", func(s map[string][]float64) { s["memMb"] = grow(400, 444) }, "fail"},
		{"the JS heap grows", func(s map[string][]float64) { s["jsHeapMb"] = grow(30, 40) }, "fail"},
		{"the shell's memory alone, without a JS heap", func(s map[string][]float64) { delete(s, "jsHeapMb") }, "pass"},
		{"the JS heap alone, in a browser", func(s map[string][]float64) { delete(s, "memMb") }, "pass"},
		{"a warm-up in the first 2 minutes is not growth", func(s map[string][]float64) {
			m := rep(400, 61)
			m[0], m[1] = 150, 300
			s["memMb"] = m
		}, "pass"},
		{"a layout swing every 2 minutes is not growth", func(s map[string][]float64) {
			m := make([]float64, 61)
			for i := range m {
				m[i] = 300
				if (i/2)%2 == 1 {
					m[i] = 500
				}
			}
			s["memMb"] = m
		}, "pass"},
		{"one unprovoked restart", func(s map[string][]float64) { s["playerErrors"] = []float64{1} }, "fail"},
		{"no memory readings", func(s map[string][]float64) { delete(s, "memMb"); delete(s, "jsHeapMb") }, "absent"},
		{"too few readings", func(s map[string][]float64) { s["memMb"] = rep(400, 20); s["jsHeapMb"] = rep(30, 20) }, "absent"},
		{"no restart count", func(s map[string][]float64) { delete(s, "playerErrors") }, "absent"},
		{"memory growth outweighs a missing restart count", func(s map[string][]float64) { s["memMb"] = grow(400, 480); delete(s, "playerErrors") }, "fail"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(androidModel)
			s := stability()
			tc.mut(s)
			f.add("S4", s)
			if got := outcome(ComputeMV(f.list()), androidDevice, "MV7"); got != tc.want {
				t.Errorf("MV7 = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestComputeMV_MV7_SaysWhatTheMemoryNumberLeavesOut(t *testing.T) {
	for _, tc := range []struct {
		device Device
		want   string
	}{
		{windowsModel, "GPU"},
		{androidModel, "WebView"},
	} {
		f := newFixture(tc.device)
		f.add("S4", stability())
		v := ComputeMV(f.list())[mvKey(tc.device.Platform, "MV7")]
		if joined := strings.Join(v.Notes, "\n"); !strings.Contains(joined, tc.want) {
			t.Errorf("%s MV7 notes = %q, want a note that names %s", tc.device.Platform, joined, tc.want)
		}
	}
	f := newFixture(windowsModel)
	f.add("S4", stability())
	if joined := strings.Join(ComputeMV(f.list())[mvKey(windowsDevice, "MV7")].Notes, "\n"); !strings.Contains(joined, "privat") {
		t.Errorf("Windows MV7 notes = %q, want private memory named (R37)", joined)
	}
}

func TestComputeMV_MV8(t *testing.T) {
	run := func(samples map[string][]float64, notes ...string) []Result {
		f := newFixture(androidModel)
		f.add("S6", samples, notes...)
		return f.list()
	}
	pip := func(frames, gap, seconds float64) map[string][]float64 {
		return map[string][]float64{"pipFrames": {frames}, "pipMaxGapMs": {gap}, "pipS": {seconds}}
	}
	tests := []struct {
		name    string
		results []Result
		want    string
	}{
		{"5 minutes without a long gap", run(pip(3000, 150, 300)), "pass"},
		{"a gap of exactly 2 s", run(pip(3000, 2000, 300)), "pass"},
		{"a gap over 2 s", run(pip(3000, 2001, 300)), "fail"},
		{"left PiP after 2 minutes", run(pip(1200, 150, 120), "left picture-in-picture after 120 s"), "fail"},
		{"no frames at all", run(pip(0, 300000, 300)), "fail"},
		{"no PiP in the shell", run(nil, "n/a: picture-in-picture needs the Android shell"), "absent"},
		{"no requestVideoFrameCallback", run(nil, "n/a: requestVideoFrameCallback is not available"), "absent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcome(ComputeMV(tc.results), androidDevice, "MV8"); got != tc.want {
				t.Errorf("MV8 = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestComputeMV_MV9(t *testing.T) {
	run := func(samples map[string][]float64, notes ...string) []Result {
		f := newFixture(androidModel)
		f.add("S5", samples, notes...)
		return f.list()
	}
	tests := []struct {
		name    string
		results []Result
		want    string
	}{
		{"back in 1.2 s after 2 minutes", run(map[string][]float64{"resumeMs": {1200}, "backgroundS": {121}}), "pass"},
		{"exactly 3 s", run(map[string][]float64{"resumeMs": {3000}, "backgroundS": {120}}), "pass"},
		{"3.1 s", run(map[string][]float64{"resumeMs": {3100}, "backgroundS": {125}}), "fail"},
		{"no live frame within 30 s", run(map[string][]float64{"backgroundS": {125}}, "no frame within 30 s of the return"), "fail"},
		{"live frames that never steadied", run(map[string][]float64{"backgroundS": {125}}, "no steady live frames within 30 s of the return"), "fail"},
		{"the player failed on return", run(map[string][]float64{"backgroundS": {125}}, "resume: STALLED"), "fail"},
		{"fast, but the background was too short to count", run(map[string][]float64{"resumeMs": {500}, "backgroundS": {40}}, "background shorter than 2 min"), "absent"},
		{"no requestVideoFrameCallback", run(nil, "resume: n/a (no requestVideoFrameCallback)"), "absent"},
		{"the person never pressed Home", run(nil, "the app did not go to the background within 10 min"), "absent"},
		{"the stream did not start", run(nil, "start: no first frame"), "absent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcome(ComputeMV(tc.results), androidDevice, "MV9"); got != tc.want {
				t.Errorf("MV9 = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestComputeMV_MV10(t *testing.T) {
	run := func(start, seek []float64) []Result {
		f := newFixture(androidModel)
		f.add("S7", map[string][]float64{"startMs": start, "seekMs": seek})
		return f.list()
	}
	tail := func(n int, fast, slow float64) []float64 { return append(rep(fast, 20-n), rep(slow, n)...) }
	tests := []struct {
		name        string
		start, seek []float64
		want        string
	}{
		{"fast", rep(900, 20), rep(700, 20), "pass"},
		{"limits", rep(2000, 20), rep(1500, 20), "pass"},
		{"start over 2 s", rep(2001, 20), rep(700, 20), "fail"},
		{"seek over 1.5 s", rep(900, 20), rep(1501, 20), "fail"},
		{"two slow starts are the tail above p90", tail(2, 900, 9000), rep(700, 20), "pass"},
		{"three are not", tail(3, 900, 9000), rep(700, 20), "fail"},
		{"seeks that never finished count as slow", rep(900, 20), rep(700, 17), "fail"},
		{"no seek numbers at all", rep(900, 20), nil, "fail"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcome(ComputeMV(run(tc.start, tc.seek)), androidDevice, "MV10"); got != tc.want {
				t.Errorf("MV10 = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestComputeMV_MV12AndMV13(t *testing.T) {
	run := func(platform, pair string) []Result {
		f := newFixture(Device{Platform: platform})
		f.add("S10", nil, pair, "prompt: yes")
		return f.list()
	}
	tests := []struct {
		platform, pair string
		code, want     string
	}{
		{androidDevice, "pair: host/host", "MV12", "pass"},
		{androidDevice, "pair: srflx/host", "MV12", "pass"},
		{androidDevice, "pair: relay/host", "MV12", "fail"},
		{androidDevice, "pair: host/relay", "MV12", "fail"},
		{androidDevice, "pair: n/a", "MV12", "absent"},
		{windowsDevice, "pair: host/prflx", "MV12", "pass"},
		{"chrome", "pair: host/host", "MV13", "pass"},
		{"edge", "pair: prflx/host", "MV13", "pass"},
		{"firefox", "pair: host/prflx", "MV13", "pass"},
		{"firefox", "pair: srflx/host", "MV13", "fail"},
		{"chrome", "pair: relay/relay", "MV13", "fail"},
		{"chrome", "pair: n/a", "MV13", "absent"},
	}
	for _, tc := range tests {
		t.Run(tc.platform+" "+tc.pair, func(t *testing.T) {
			if got := outcome(ComputeMV(run(tc.platform, tc.pair)), tc.platform, tc.code); got != tc.want {
				t.Errorf("%s = %s, want %s", tc.code, got, tc.want)
			}
		})
	}
	t.Run("a browser has no MV12 and a shell has no MV13", func(t *testing.T) {
		if got := outcome(ComputeMV(run("chrome", "pair: host/host")), "chrome", "MV12"); got != "absent" {
			t.Errorf("Chrome MV12 = %s, want absent: the Chrome part of MV12 needs a public origin (Faza 0-bis)", got)
		}
		if got := outcome(ComputeMV(run(androidDevice, "pair: host/host")), androidDevice, "MV13"); got != "absent" {
			t.Errorf("Android MV13 = %s, want absent", got)
		}
	})
	t.Run("the prompt answer and a hub without mDNS are noted", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S10", nil, "pair: host/host", "prompt: yes").MDNSUnavailable = true
		v := ComputeMV(f.list())[mvKey(androidDevice, "MV12")]
		joined := v.Text + "\n" + strings.Join(v.Notes, "\n")
		if !strings.Contains(joined, "host/host") || !strings.Contains(joined, "mDNS") || !strings.Contains(joined, "IPv4") {
			t.Errorf("MV12 = %q: want the pair, the mDNS warning and the IPv4-only limit (R25, R38)", joined)
		}
	})
}

func TestComputeMV_MV15(t *testing.T) {
	tests := []struct {
		name    string
		samples []float64
		want    string
	}{
		{"10 runs of 2.1 s", rep(2100, 10), "pass"},
		{"exactly 3 s", rep(3000, 10), "pass"},
		{"one run over 3 s", append(rep(2100, 9), 3001), "fail"},
		{"a run that never finished", rep(2100, 9), "fail"},
		{"none finished", nil, "fail"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(androidModel)
			f.add("S8", map[string][]float64{"argon2idMs": tc.samples})
			if got := outcome(ComputeMV(f.list()), androidDevice, "MV15"); got != tc.want {
				t.Errorf("MV15 = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestComputeMV_G9(t *testing.T) {
	run := func(h265, high51 string) []Result {
		f := newFixture(androidModel)
		f.add("S9", map[string][]float64{}, "mx-h265: "+h265, "mx-high51: "+high51)
		return f.list()
	}
	tests := []struct {
		h265, high51, want string
	}{
		{"ok", "ok", "pass"},
		{"fallback", "ok", "pass"},
		{"fallback", "unsupported-profile", "pass"},
		{"ok", "unsupported-profile", "pass"},
		{"fallback-failed", "ok", "fail"},
		{"ok", "fallback-failed", "fail"},
	}
	for _, tc := range tests {
		t.Run(tc.h265+" "+tc.high51, func(t *testing.T) {
			if got := outcome(ComputeMV(run(tc.h265, tc.high51)), androidDevice, "G9"); got != tc.want {
				t.Errorf("G9 = %s, want %s", got, tc.want)
			}
		})
	}
	t.Run("a path without an outcome is not a result", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S9", nil, "mx-h265: ok")
		if got := outcome(ComputeMV(f.list()), androidDevice, "G9"); got != "absent" {
			t.Errorf("G9 with one path = %s, want absent", got)
		}
	})
	t.Run("the outcomes are in the value", func(t *testing.T) {
		v := ComputeMV(run("fallback", "unsupported-profile"))[mvKey(androidDevice, "G9")]
		if !strings.Contains(v.Text, "mx-h265") || !strings.Contains(v.Text, "fallback") || !strings.Contains(v.Text, "unsupported-profile") {
			t.Errorf("G9 text = %q", v.Text)
		}
	})
}

func TestHubJitterFlags(t *testing.T) {
	rs := androidRuns()
	find(t, rs, androidDevice, "S1").HubJitterMs = []float64{2, 20}
	find(t, rs, androidDevice, "S2").HubJitterMs = []float64{3, 24.5, 4}
	find(t, rs, androidDevice, "S3-4").HubJitterMs = nil
	flags := HubJitterFlags(rs)
	if len(flags) != 1 {
		t.Fatalf("flags = %+v, want only the S2 run: 20 ms is the limit, not over it", flags)
	}
	if f := flags[0]; f.Scenario != "S2" || f.MaxMs != 24.5 || !strings.Contains(f.File, "s2") {
		t.Errorf("flag = %+v", f)
	}
}

func TestComputeMV_NotesTheHubJitterOfTheRunTheyCameFrom(t *testing.T) {
	rs := androidRuns()
	find(t, rs, androidDevice, "S2").HubJitterMs = []float64{3, 24.5}
	mv := ComputeMV(rs)
	if joined := strings.Join(mv[mvKey(androidDevice, "MV2")].Notes, "\n"); !strings.Contains(joined, "24,5") {
		t.Errorf("MV2 notes = %q, want the hub jitter of 24,5 ms", joined)
	}
	if joined := strings.Join(mv[mvKey(androidDevice, "MV1")].Notes, "\n"); strings.Contains(joined, "jitter") {
		t.Errorf("MV1 notes = %q: its run had no jitter", joined)
	}
}

func TestMVKeyAndFormat(t *testing.T) {
	if got := mvKey("android", "MV3/9"); got != "android/MV3/9" {
		t.Errorf("mvKey = %q", got)
	}
	for _, tc := range []struct {
		v    float64
		d    int
		want string
	}{{11.95, 2, "11,95"}, {12, 2, "12"}, {10.8, 2, "10,8"}, {812.4, 0, "812"}, {0.04, 1, "0"}, {math.Inf(1), 0, "∞"}} {
		if got := number(tc.v, tc.d); got != tc.want {
			t.Errorf("number(%v, %d) = %q, want %q", tc.v, tc.d, got, tc.want)
		}
	}
}

// The mean of twelve times 10.7 is 10.700000000000001: a threshold must not fail on that.
func TestComparisonsAbsorbTheRoundingOfAMean(t *testing.T) {
	if !atMost(25.000000000000004, 25) || !atMost(25, 25) || atMost(25.001, 25) {
		t.Error("atMost is wrong around its limit")
	}
	if !atLeast(10.799999999999999, 10.8) || !atLeast(10.8, 10.8) || atLeast(10.79, 10.8) {
		t.Error("atLeast is wrong around its limit")
	}
	f := newFixture(windowsModel)
	s := windowsGrid(16)
	s["mainCpuPctApp"] = rep(10.7, 12) // above 10: fails
	f.add("S3-16", s)
	if got := outcome(ComputeMV(f.list()), windowsDevice, "MV6"); got != "fail" {
		t.Errorf("MV6 with 10.7 %% on the main stream = %s, want fail", got)
	}
}
