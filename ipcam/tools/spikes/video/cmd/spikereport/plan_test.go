package main

import (
	"slices"
	"strings"
	"testing"
)

// The tests of final fix batch D: the device plan of spec §16.3 (R45), the memory sources of
// MV7 (R46), runs that are not valid (R48, R49), the WebView2 processes (R52), the loss
// counters (R50) and the report findings of the final review.

func gateOf(t *testing.T, rs []Result, id string) Gate {
	t.Helper()
	for _, g := range buildGates(evaluate(rs)) {
		if g.ID == id {
			return g
		}
	}
	t.Fatalf("no gate %s", id)
	return Gate{}
}

func mvOf(t *testing.T, rs []Result, device, code string) MV {
	t.Helper()
	v, ok := ComputeMV(rs)[mvKey(device, code)]
	if !ok {
		t.Fatalf("%s %s has no value (missing: %q)", device, code, evaluate(rs).Missing[mvKey(device, code)])
	}
	return v
}

// without returns the results without the runs of `scenario` on `platform`.
func without(rs []Result, platform, scenario string) []Result {
	var out []Result
	for _, r := range rs {
		if r.Device.Platform != platform || r.Scenario != scenario {
			out = append(out, r)
		}
	}
	return out
}

// grow is 61 memory readings at `base` whose last 12 are `last`.
func grow(base, last float64) []float64 {
	out := rep(base, 61)
	for i := 49; i < 61; i++ {
		out[i] = last
	}
	return out
}

// ---- Item 1: the device plan (R45) ----

func TestExpectedCodes_FollowTheDevicePlanOfR45(t *testing.T) {
	want := map[string][]string{
		androidDevice: {"MV1", "MV2", "MV3/4", "MV3/9", "MV4/9", "MV4/main", "MV5/9", "MV7", "MV8", "MV9", "MV10", "MV12", "MV15", "G9"},
		// WIN-1 runs MV1–MV7 with 16 tiles, MV10 and G9: S1, S2, S3-16, S4, S7, S9, S10.
		windowsDevice: {"MV1", "MV2", "MV3/16", "MV6", "MV7", "MV10", "MV12", "G9"},
		"chrome":      {"MV1", "MV2", "MV3/9", "MV3/16", "MV7", "MV10", "MV13", "G9"},
		"edge":        {"MV1", "MV2", "MV3/9", "MV13", "G9"},
		"firefox":     {"MV1", "MV2", "MV3/9", "MV13", "G9"},
	}
	for device, codes := range want {
		got := expectedCodes(device)
		sorted := append([]string(nil), codes...)
		slices.SortFunc(sorted, func(a, b string) int {
			if codeLess(a, b) {
				return -1
			}
			if codeLess(b, a) {
				return 1
			}
			return 0
		})
		if strings.Join(got, ",") != strings.Join(sorted, ",") {
			t.Errorf("expectedCodes(%s) = %v, want %v", device, got, sorted)
		}
	}
}

func TestMV7_OnADesktopPathOnlyA16TileRunDecides(t *testing.T) {
	for _, device := range []Device{windowsModel, browserDevice("chrome")} {
		t.Run(device.Platform, func(t *testing.T) {
			s := windowsStability()
			s["tiles"] = []float64{9}
			s["memMb"] = grow(400, 480) // a 20 % growth, which would fail
			f := newFixture(device)
			nine := f.add("S4", s)
			rs := f.list()
			ev := evaluate(rs)
			if _, ok := ev.MV[mvKey(device.Platform, "MV7")]; ok {
				t.Fatalf("MV7 was judged on a 9-tile S4 of a desktop path: %+v", ev.MV[mvKey(device.Platform, "MV7")])
			}
			if why := ev.reasonMissing(device.Platform, "MV7"); !strings.Contains(why, "16 plăci") {
				t.Errorf("why MV7 is missing = %q, want it to say that the PC needs 16 tiles", why)
			}
			md := render(t, rs)
			row := rowOf(t, md, "| MV7 |")
			for _, want := range []string{"nemăsurat", "informativ", nine.File, "+20 %"} {
				if !strings.Contains(row, want) {
					t.Errorf("the MV7 row lacks %q (the 9-tile run is listed, with its value):\n%s", want, row)
				}
			}
			if run := rowOf(t, md, "| "+nine.File+" |"); !strings.Contains(run, "informativă") {
				t.Errorf("the runs table does not say that the 9-tile run does not decide:\n%s", run)
			}
		})
	}
	t.Run("a newer 9-tile run does not replace an older 16-tile run", func(t *testing.T) {
		f := newFixture(windowsModel)
		sixteen := windowsStability()
		sixteen["memMb"] = grow(400, 460) // 15 %: fails
		f.add("S4", sixteen)
		nine := windowsStability()
		nine["tiles"] = []float64{9}
		f.add("S4", nine)
		if v := mvOf(t, f.list(), windowsDevice, "MV7"); v.Pass {
			t.Errorf("MV7 = %+v, want the 16-tile run's failure", v)
		}
	})
	t.Run("an S4 without the tiles sample does not decide on a desktop path", func(t *testing.T) {
		s := windowsStability()
		delete(s, "tiles")
		f := newFixture(windowsModel)
		f.add("S4", s)
		if got := outcome(ComputeMV(f.list()), windowsDevice, "MV7"); got != "absent" {
			t.Errorf("MV7 = %s, want absent", got)
		}
	})
	t.Run("the phone pages 9 tiles and that decides", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S4", stability())
		if got := outcome(ComputeMV(f.list()), androidDevice, "MV7"); got != "pass" {
			t.Errorf("Android MV7 = %s, want pass", got)
		}
	})
	t.Run("a 16-tile run decides", func(t *testing.T) {
		f := newFixture(windowsModel)
		f.add("S4", windowsStability())
		if v := mvOf(t, f.list(), windowsDevice, "MV7"); !v.Pass || !strings.Contains(strings.Join(v.Notes, " "), "16 plăci") {
			t.Errorf("MV7 = %+v, want a pass with a note on the 16 tiles", v)
		}
	})
}

func TestG6_ReadsMV6AndMV7With16Tiles(t *testing.T) {
	key6, key7 := mvKey(windowsDevice, "MV6"), mvKey(windowsDevice, "MV7")
	both := []string{key6, key7}
	for name, tc := range map[string]struct {
		failing, absent []string
		want            Verdict
	}{
		"both pass":                 {nil, nil, VerdictGO},
		"MV7 fails":                 {[]string{key7}, nil, VerdictPlanBGrid9},
		"MV6 fails":                 {[]string{key6}, nil, VerdictPlanBGrid9},
		"MV7 is missing":            {nil, []string{key7}, VerdictIncomplete},
		"MV6 fails, MV7 is missing": {[]string{key6}, []string{key7}, VerdictPlanBGrid9},
		"MV7 fails, MV6 is missing": {[]string{key7}, []string{key6}, VerdictPlanBGrid9},
	} {
		t.Run(name, func(t *testing.T) {
			if got := WindowsVerdict(mvSet(both, tc.failing, tc.absent)); got != tc.want {
				t.Errorf("verdict = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("from the results, with the report's text", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, windowsDevice, "S4").Samples["memMb"] = grow(1200, 1380) // +15 %
		g := gateOf(t, rs, "G6")
		if g.Verdict != VerdictPlanBGrid9 || !strings.Contains(g.Decision, "MV7") || !strings.Contains(g.Decision, "9 plăci") {
			t.Errorf("G6 = %+v, want plan B naming MV7", g)
		}
		md := render(t, rs)
		if strings.Contains(md, "### Alte praguri picate") {
			t.Errorf("WIN-1 MV7 is listed among the failures without a plan B:\n%s", section(t, md, "## Verdicte și decizii declanșate"))
		}
		summary := section(t, md, "## Rezumat pentru proprietar")
		if strings.Contains(summary, "fără plan B") {
			t.Errorf("the summary says WIN-1 MV7 has no plan B:\n%s", summary)
		}
		if !strings.Contains(summary, "MV7") || !strings.Contains(summary, string(VerdictPlanBGrid9)) {
			t.Errorf("the summary does not explain G6 by MV7:\n%s", summary)
		}
	})
}

// ---- Item 2: the memory of MV7 (R46) ----

func TestMV7_ABucketedJSHeapIsNotJudged(t *testing.T) {
	t.Run("on the phone the process memory alone decides", func(t *testing.T) {
		s := stability()
		s["jsHeapMb"] = append(rep(10, 49), rep(10.6, 12)...) // one bucket step: +6 %
		s["jsHeapMb"][60] = 12.7                              // and a jump that would fail
		f := newFixture(androidModel)
		f.add("S4", s, "jsHeap: bucketed")
		v := mvOf(t, f.list(), androidDevice, "MV7")
		if !v.Pass || !strings.Contains(v.Text, "memorie 0 %") {
			t.Errorf("MV7 = %+v, want a pass on memMb alone", v)
		}
		if !strings.Contains(v.Text, "bucketed") || !strings.Contains(v.Text, "R46") {
			t.Errorf("MV7 text %q does not say why the JS heap is left out", v.Text)
		}
		if notes := strings.Join(v.Notes, "\n"); !strings.Contains(notes, "PSS") || !strings.Contains(notes, "rander") {
			t.Errorf("MV7 notes = %q, want the PSS without the renderer named", notes)
		}
	})
	t.Run("without the note the JS heap is judged", func(t *testing.T) {
		s := stability()
		s["jsHeapMb"] = grow(30, 40)
		f := newFixture(androidModel)
		f.add("S4", s)
		if got := outcome(ComputeMV(f.list()), androidDevice, "MV7"); got != "fail" {
			t.Errorf("MV7 = %s, want fail", got)
		}
	})
	t.Run("a desktop path keeps the JS heap and the process memory", func(t *testing.T) {
		s := windowsStability()
		s["jsHeapMb"] = grow(30, 40)
		f := newFixture(windowsModel)
		f.add("S4", s)
		v := mvOf(t, f.list(), windowsDevice, "MV7")
		if v.Pass || !strings.Contains(v.Text, "heap JS") || !strings.Contains(v.Text, "memorie") {
			t.Errorf("MV7 = %+v, want a failure on the JS heap with the memory beside it", v)
		}
	})
	t.Run("a bucketed heap and no process memory is not a measurement", func(t *testing.T) {
		s := desktopStability()
		delete(s, "memMb")
		f := newFixture(browserDevice("chrome"))
		f.add("S4", s, "jsHeap: bucketed")
		if got := outcome(ComputeMV(f.list()), "chrome", "MV7"); got != "absent" {
			t.Errorf("Chrome MV7 = %s, want absent", got)
		}
	})
	t.Run("the limitations say what AND-1's figure is", func(t *testing.T) {
		limits := section(t, render(t, allRuns()), "## Limitări ale măsurătorilor")
		if !anyLineHasAll(limits, "AND-1", "PSS", "rander", "heap", "rotunjit", "R46") {
			t.Errorf("no limitation says that AND-1's MV7 is the app's PSS without the renderer and that the heap is bucketed:\n%s", limits)
		}
	})
}

// deviceSection is the text of a device's "### heading" section: up to the next heading.
func deviceSection(t *testing.T, md, heading string) string {
	t.Helper()
	i := strings.Index(md, heading)
	if i < 0 {
		t.Fatalf("the report has no %q", heading)
	}
	rest := md[i+len(heading):]
	for _, next := range []string{"\n### ", "\n## "} {
		if end := strings.Index(rest, next); end >= 0 {
			rest = rest[:end]
		}
	}
	return rest
}

// anyLineHasAll says whether one line of text holds every word.
func anyLineHasAll(text string, words ...string) bool {
	for _, line := range strings.Split(text, "\n") {
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(line, w)
		}
		if ok {
			return true
		}
	}
	return false
}

// ---- Item 3: runs that are not valid (R48, R49) ----

func TestRuns_OnTheChargerAreNotValid(t *testing.T) {
	f := newFixture(androidModel)
	f.add("S3-9", androidGrid(9))
	on := androidGrid(9)
	on["batteryPct"] = rep(100, 60) // flat at 100 %: 0 %/h would pass
	on["charging"] = append(rep(0, 30), rep(1, 30)...)
	charging := f.add("S3-9", on)
	rs := f.list()
	ev := evaluate(rs)
	for _, run := range ev.Runs {
		if run.File == charging.File && run.Status == RunUsed {
			t.Errorf("the run on the charger is used")
		}
	}
	if v := ev.MV[mvKey(androidDevice, "MV5/9")]; v.Text == "" || strings.Contains(v.Text, "0 %/h") {
		t.Errorf("MV5/9 = %+v, want the valid run's value, not the charger's 0 %%/h", v)
	}
	t.Run("in the fullscreen part too", func(t *testing.T) {
		f := newFixture(androidModel)
		s := androidGrid(9)
		s["mainCharging"] = []float64{0, 1}
		f.add("S3-9", s)
		if got := outcome(ComputeMV(f.list()), androidDevice, "MV5/9"); got != "absent" {
			t.Errorf("MV5/9 = %s, want absent", got)
		}
	})
	md := render(t, rs)
	device := deviceSection(t, md, "### AND-1")
	if !strings.Contains(device, "Rulări invalide") || !strings.Contains(device, charging.File) || !strings.Contains(device, "încărcător") {
		t.Errorf("the device does not list the run on the charger under «Rulări invalide» with the reason:\n%s", device)
	}
}

func TestRuns_InterruptedForAnyReasonAreListedWithTheReason(t *testing.T) {
	f := newFixture(androidModel)
	add := func(scenario string, why *Interruption, note string) *Result {
		r := f.add(scenario, map[string][]float64{"firstFrameMs": rep(300, 20)}, note)
		r.Interrupted = true
		r.Interruption = why
		return r
	}
	cases := []struct {
		run  *Result
		want []string
	}{
		{add("S1", &Interruption{Kind: "hidden", AtS: 12}, "interrupted: the page was hidden 12 s into the run"), []string{"ascunsă", "12 s"}},
		{add("S2", &Interruption{Kind: "session-lost", AtS: 60, How: "connection closed"}, "interrupted: the hub session was lost 60 s into the run (connection closed)"), []string{"hub", "60 s", "s-a închis"}},
		{add("S3-9", &Interruption{Kind: "charging", AtS: 40}, "interrupted: the phone was on the charger 40 s into the run"), []string{"încărcător", "40 s"}},
		{add("S5", &Interruption{Kind: "early-return", BackgroundS: 100}, "interrupted: back from the background after 100 s, at least 120 s are needed"), []string{"fundal", "100 s", "2 minute"}},
		{add("S6", &Interruption{Kind: "pip-left", PipS: 290}, "interrupted: picture-in-picture ended after 290 s, 300 s are needed"), []string{"290 s", "5 minute"}},
		// A result written before the field existed: read it as hidden (batch C).
		{add("S7", nil, "interrupted: the page was hidden 7 s into the run"), []string{"ascunsă", "7 s"}},
	}
	rs := f.list()
	mv := ComputeMV(rs)
	for _, code := range []string{"MV1", "MV2", "MV5/9", "MV9", "MV8", "MV10"} {
		if _, ok := mv[mvKey(androidDevice, code)]; ok {
			t.Errorf("%s has a value from an interrupted run", code)
		}
	}
	invalid := deviceSection(t, render(t, rs), "### AND-1")
	if !strings.Contains(invalid, "Rulări invalide") {
		t.Fatalf("no «Rulări invalide» list:\n%s", invalid)
	}
	for _, tc := range cases {
		line := ""
		for _, l := range strings.Split(invalid, "\n") {
			if strings.HasPrefix(l, "- `"+tc.run.File+"`") {
				line = l
			}
		}
		for _, want := range tc.want {
			if !strings.Contains(line, want) {
				t.Errorf("%s: the line %q lacks %q", tc.run.Scenario, line, want)
			}
		}
	}
}

// ---- Item 4: the WebView2 processes (R52) ----

func TestWebView2Procs_MV6AndMV7NeedThem(t *testing.T) {
	for name, tc := range map[string]struct {
		scenario, code string
		mut            func(s map[string][]float64)
		want           string
	}{
		"MV6: a grid reading without WebView2":  {"S3-16", "MV6", func(s map[string][]float64) { s["webview2Procs"][7] = 0 }, "absent"},
		"MV6: no WebView2 count at all":         {"S3-16", "MV6", func(s map[string][]float64) { delete(s, "webview2Procs") }, "absent"},
		"MV6: the main stream without WebView2": {"S3-16", "MV6", func(s map[string][]float64) { s["mainWebview2Procs"] = rep(0, 12) }, "absent"},
		"MV6: a measured failure still decides": {"S3-16", "MV6", func(s map[string][]float64) {
			s["cpuPctApp"] = rep(40, 60)
			s["mainWebview2Procs"] = rep(0, 12)
		}, "fail"},
		"MV6: every reading with WebView2":       {"S3-16", "MV6", func(map[string][]float64) {}, "pass"},
		"MV7: a memory reading without WebView2": {"S4", "MV7", func(s map[string][]float64) { s["webview2Procs"][30] = 0 }, "absent"},
		"MV7: no WebView2 count at all":          {"S4", "MV7", func(s map[string][]float64) { delete(s, "webview2Procs") }, "absent"},
		"MV7: every reading with WebView2":       {"S4", "MV7", func(map[string][]float64) {}, "pass"},
		// The fail-closed shell (R52): without a WebView2 process it leaves memMb out and
		// counts 0 processes; the JS heap alone must not judge MV7 on WIN-1 (R46).
		"MV7: the fail-closed shell dropped memMb": {"S4", "MV7", func(s map[string][]float64) {
			delete(s, "memMb")
			s["webview2Procs"] = rep(0, 61)
		}, "absent"},
		"MV7: neither memMb nor a WebView2 count": {"S4", "MV7", func(s map[string][]float64) {
			delete(s, "memMb")
			delete(s, "webview2Procs")
		}, "absent"},
	} {
		t.Run(name, func(t *testing.T) {
			s := windowsGrid(16)
			if tc.scenario == "S4" {
				s = windowsStability()
			}
			tc.mut(s)
			f := newFixture(windowsModel)
			f.add(tc.scenario, s)
			rs := f.list()
			if got := outcome(ComputeMV(rs), windowsDevice, tc.code); got != tc.want {
				t.Fatalf("%s = %s, want %s", tc.code, got, tc.want)
			}
			if tc.want == "absent" {
				if why := evaluate(rs).reasonMissing(windowsDevice, tc.code); !strings.Contains(why, "WebView2") || !strings.Contains(why, "R52") {
					t.Errorf("why %s is missing = %q, want the WebView2 processes named (R52)", tc.code, why)
				}
			}
		})
	}
	t.Run("G6 has no verdict then", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, windowsDevice, "S3-16").Samples["webview2Procs"] = rep(0, 60)
		if g := gateOf(t, rs, "G6"); g.Verdict != VerdictIncomplete {
			t.Errorf("G6 = %q, want %q", g.Verdict, VerdictIncomplete)
		}
	})
	t.Run("WIN-1 MV7 needs the process memory, also with WebView2 found", func(t *testing.T) {
		for name, mut := range map[string]func(s map[string][]float64){
			"no memMb":    func(s map[string][]float64) { delete(s, "memMb") },
			"empty memMb": func(s map[string][]float64) { s["memMb"] = nil },
		} {
			t.Run(name, func(t *testing.T) {
				s := windowsStability()
				mut(s)
				f := newFixture(windowsModel)
				f.add("S4", s)
				rs := f.list()
				if got := outcome(ComputeMV(rs), windowsDevice, "MV7"); got != "absent" {
					t.Fatalf("MV7 = %s, want absent: the JS heap alone does not judge WIN-1 (R46)", got)
				}
				if why := evaluate(rs).reasonMissing(windowsDevice, "MV7"); !strings.Contains(why, "memorie: fără citiri de la shell") {
					t.Errorf("why MV7 is missing = %q, want «memorie: fără citiri de la shell»", why)
				}
			})
		}
	})
	for name, mut := range map[string]func(s map[string][]float64){
		"G6 has no verdict when the fail-closed shell dropped memMb": func(s map[string][]float64) {
			delete(s, "memMb")
			s["webview2Procs"] = rep(0, 61)
		},
		"G6 has no verdict without memMb and without a WebView2 count": func(s map[string][]float64) {
			delete(s, "memMb")
			delete(s, "webview2Procs")
		},
	} {
		t.Run(name, func(t *testing.T) {
			rs := allRuns()
			mut(find(t, rs, windowsDevice, "S4").Samples)
			mv := ComputeMV(rs)
			if got := outcome(mv, windowsDevice, "MV6"); got != "pass" {
				t.Fatalf("MV6 = %s, want pass: G6 must rest on MV7 alone here", got)
			}
			if got := outcome(mv, windowsDevice, "MV7"); got != "absent" {
				t.Errorf("MV7 = %s, want absent", got)
			}
			if g := gateOf(t, rs, "G6"); g.Verdict != VerdictIncomplete {
				t.Errorf("G6 = %q (%s), want %q", g.Verdict, g.Decision, VerdictIncomplete)
			}
		})
	}
	t.Run("only the Windows app counts WebView2 processes", func(t *testing.T) {
		f := newFixture(browserDevice("chrome"))
		f.add("S4", desktopStability())
		if got := outcome(ComputeMV(f.list()), "chrome", "MV7"); got != "pass" {
			t.Errorf("Chrome MV7 = %s, want pass without a WebView2 count", got)
		}
	})
}

// ---- Item 5: the loss counters (R50) ----

func TestLossCounters_AreInformationBesideMV1AndMV3(t *testing.T) {
	loss := map[string][]float64{"rtpLost": {7}, "nack": {12}, "pli": {21}, "framesDropped": {3}}
	f := newFixture(androidModel)
	s1 := map[string][]float64{"firstFrameMs": rep(300, 20)}
	grid := androidGrid(9)
	for k, v := range loss {
		s1[k] = v
		grid[k] = []float64{v[0] * 1000} // large numbers change nothing: no threshold
	}
	delete(grid, "pli")
	f.add("S1", s1)
	f.add("S3-9", grid, "pli: n/a")
	rs := f.list()
	mv1 := mvOf(t, rs, androidDevice, "MV1")
	notes := strings.Join(mv1.Notes, "\n")
	for _, want := range []string{"RTP pierdute 7", "NACK 12", "PLI 21", "cadre aruncate 3", "fără prag"} {
		if !strings.Contains(notes, want) {
			t.Errorf("MV1 notes lack %q:\n%s", want, notes)
		}
	}
	mv3 := mvOf(t, rs, androidDevice, "MV3/9")
	if !mv3.Pass || !strings.Contains(strings.Join(mv3.Notes, "\n"), "RTP pierdute 7000") || !strings.Contains(strings.Join(mv3.Notes, "\n"), "PLI n/a") {
		t.Errorf("MV3/9 = %+v, want a pass with the counters as a note and PLI n/a", mv3)
	}
	limits := section(t, render(t, rs), "## Limitări ale măsurătorilor")
	if !anyLineHasAll(limits, "PLI", "GOP", "NACK", "R50") {
		t.Errorf("no limitation says that the hub answers NACK but not PLI (R50):\n%s", limits)
	}
	if !anyLineHasAll(limits, "MV12", "MV13", "srflx", "relay", "directă") {
		t.Errorf("no limitation says that MV12/MV13 'trece' means a direct LAN pair:\n%s", limits)
	}
}

// ---- Item 6: the report findings ----

// report-3: spec G7 also asks the hub to listen on 5353/udp next to the Windows DNS client.
func TestG7_NotGOWhenTheHubHadNoMDNS(t *testing.T) {
	rs := allRuns()
	s10 := find(t, rs, "firefox", "S10")
	s10.MDNSUnavailable = true
	g := gateOf(t, rs, "G7")
	if g.Verdict == VerdictGO {
		t.Fatalf("G7 = GO although the hub could not open 5353/udp")
	}
	if g.Verdict != VerdictIncomplete {
		t.Errorf("G7 = %q, want %q", g.Verdict, VerdictIncomplete)
	}
	text := g.Decision + " " + explain(g)
	for _, want := range []string{"5353", "mDNS", s10.File} {
		if !strings.Contains(text, want) {
			t.Errorf("G7 does not say what is missing: %q lacks %q", text, want)
		}
	}
	t.Run("a measured relay still decides", func(t *testing.T) {
		find(t, rs, "chrome", "S10").Notes = []string{"pair: relay/relay", "prompt: n/a"}
		if g := gateOf(t, rs, "G7"); g.Verdict != VerdictPlanBRelay {
			t.Errorf("G7 = %q, want %q", g.Verdict, VerdictPlanBRelay)
		}
	})
	t.Run("a run that G7 does not read does not count", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, androidDevice, "S1").MDNSUnavailable = true
		if g := gateOf(t, rs, "G7"); g.Verdict != VerdictGO {
			t.Errorf("G7 = %q, want GO: the S10 runs had mDNS", g.Verdict)
		}
	})
}

// report-5: G9 decides only from every expected path, or from a measured failure.
func TestG9_WaitsForEveryExpectedPath(t *testing.T) {
	t.Run("all five paths", func(t *testing.T) {
		g := gateOf(t, allRuns(), "G9")
		if strings.Contains(g.Result, "fără verdict") || !strings.HasPrefix(g.Change, "§2.2") {
			t.Errorf("G9 = %+v, want a decision with a §2.2 change", g)
		}
	})
	for _, missing := range []string{windowsDevice, "chrome", "edge", androidDevice} {
		t.Run(missing+" did not run S9", func(t *testing.T) {
			g := gateOf(t, without(allRuns(), missing, "S9"), "G9")
			if !strings.HasPrefix(g.Result, string(VerdictIncomplete)) {
				t.Errorf("G9 result = %q, want it to start with %q", g.Result, VerdictIncomplete)
			}
			if strings.Contains(g.Change, "§2.2") || !strings.Contains(g.Decision, deviceName(missing)) {
				t.Errorf("G9 = %+v: no §2.2 change, and the decision names %s", g, deviceName(missing))
			}
		})
	}
	t.Run("a measured failure decides while a path is missing", func(t *testing.T) {
		rs := without(allRuns(), "edge", "S9")
		find(t, rs, "firefox", "S9").Notes = []string{"mx-h265: fallback-failed", "mx-high51: ok"}
		g := gateOf(t, rs, "G9")
		if !strings.Contains(g.Decision, "fallback-failed") || !strings.HasPrefix(g.Change, "§2.2") {
			t.Errorf("G9 = %+v, want plan B with a §2.2 change", g)
		}
	})
	t.Run("a path on an unexpected OS does not decide", func(t *testing.T) {
		rs := allRuns()
		phone := find(t, rs, "chrome", "S9")
		phone.Device.OS = "Android"
		phone.Notes = []string{"mx-h265: fallback-failed", "mx-high51: ok"}
		g := gateOf(t, rs, "G9")
		if strings.Contains(g.Decision, "fallback-failed") || !strings.HasPrefix(g.Result, string(VerdictIncomplete)) {
			t.Errorf("G9 = %+v: a phone browser decided it", g)
		}
	})
}

// report-6: Firefox offers no H.264 Main, so S2's main stream cannot bind there.
func TestMV2_FirefoxWithoutH264MainIsJudgedOnTheSubstream(t *testing.T) {
	s2 := map[string][]float64{"subLatencyMs": ramp(40, 120, 100), "subReliable": rep(1, 100)}
	const note = "principal: n/a, browserul nu oferă H.264 Main (în produs, retragere pe substream, §6.3 pasul 7)"
	f := newFixture(browserDevice("firefox"))
	f.add("S2", s2, "main: bind error CODEC_UNSUPPORTED")
	v := mvOf(t, f.list(), "firefox", "MV2")
	if !v.Pass || !strings.Contains(v.Text, "p95 sub") || !strings.Contains(v.Text+" "+strings.Join(v.Notes, " "), note) {
		t.Errorf("Firefox MV2 = %+v, want a pass on the substream with %q", v, note)
	}
	t.Run("a slow substream still fails", func(t *testing.T) {
		f := newFixture(browserDevice("firefox"))
		f.add("S2", map[string][]float64{"subLatencyMs": rep(1500, 100)}, "main: bind error CODEC_UNSUPPORTED")
		if got := outcome(ComputeMV(f.list()), "firefox", "MV2"); got != "fail" {
			t.Errorf("MV2 = %s, want fail", got)
		}
	})
	t.Run("the phone keeps both streams: MV2 is critical there", func(t *testing.T) {
		f := newFixture(androidModel)
		f.add("S2", s2, "main: bind error CODEC_UNSUPPORTED")
		if got := outcome(ComputeMV(f.list()), androidDevice, "MV2"); got != "absent" {
			t.Errorf("Android MV2 = %s, want absent", got)
		}
	})
}

// report-7: the fullscreen decoder does not depend on the grid.
func TestMV4_TheFullscreenDecoderIsItsOwnItem(t *testing.T) {
	rs := allRuns()
	for _, scenario := range []string{"S3-4", "S3-9"} {
		find(t, rs, androidDevice, scenario).Samples["mainHardware"] = []float64{0}
	}
	mv := ComputeMV(rs)
	if got := outcome(mv, androidDevice, "MV4/9"); got != "pass" {
		t.Errorf("MV4/9 = %s, want pass: all 9 tiles decode in hardware", got)
	}
	if got := outcome(mv, androidDevice, "MV4/main"); got != "fail" {
		t.Errorf("MV4/main = %s, want fail", got)
	}
	// R54: the spec lists MV4 only among AND-1's 9-tile items, so alone it cannot give NO-GO.
	if got := AndroidVerdict(mv); got != VerdictGOConditionat {
		t.Errorf("verdict = %q, want %q (R54)", got, VerdictGOConditionat)
	}
	t.Run("hardware everywhere passes", func(t *testing.T) {
		if v := mvOf(t, allRuns(), androidDevice, "MV4/main"); !v.Pass || !strings.Contains(v.Text, "hardware") {
			t.Errorf("MV4/main = %+v", v)
		}
	})
	t.Run("one grid run in software is enough to fail", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, androidDevice, "S3-9").Samples["mainHardware"] = []float64{0}
		if got := outcome(ComputeMV(rs), androidDevice, "MV4/main"); got != "fail" {
			t.Errorf("MV4/main = %s, want fail", got)
		}
	})
	t.Run("no decoder information", func(t *testing.T) {
		rs := allRuns()
		for _, scenario := range []string{"S3-4", "S3-9"} {
			delete(find(t, rs, androidDevice, scenario).Samples, "mainHardware")
		}
		if got := AndroidVerdict(ComputeMV(rs)); got != VerdictIncomplete {
			t.Errorf("verdict = %q, want %q", got, VerdictIncomplete)
		}
	})
}

// R54: a software decode of the fullscreen main stream fails MV4, which the spec lists only
// among AND-1's extra 9-tile items. Alone it gives GO condiționat, with a reason of its own:
// fewer tiles would not change the fullscreen decoder, so the A6 cap is not the remedy.
func TestR54_ASoftwareFullscreenDecoderIsGOConditionatWithItsOwnReason(t *testing.T) {
	const reason = "ecranul complet e decodat software; plafonul de plăci nu rezolvă asta"
	software := func(t *testing.T, rs []Result) {
		t.Helper()
		for _, scenario := range []string{"S3-4", "S3-9"} {
			find(t, rs, androidDevice, scenario).Samples["mainHardware"] = []float64{0}
		}
	}
	summaryOf := func(t *testing.T, rs []Result) string {
		t.Helper()
		return section(t, render(t, rs), "## Rezumat pentru proprietar")
	}
	t.Run("alone", func(t *testing.T) {
		rs := allRuns()
		software(t, rs)
		g := gateOf(t, rs, "G5")
		if g.Verdict != VerdictGOConditionat {
			t.Fatalf("G5 = %q, want %q", g.Verdict, VerdictGOConditionat)
		}
		if !strings.Contains(g.Decision, "MV4/main") || !strings.Contains(g.Decision, reason) {
			t.Errorf("G5 decision %q, want MV4/main and «%s»", g.Decision, reason)
		}
		if strings.Contains(g.Decision, "cu 9 plăci pică") || strings.Contains(g.Decision, "snapshot") {
			t.Errorf("G5 decision %q offers the tile cap as the remedy of a fullscreen decoder", g.Decision)
		}
		if !strings.Contains(g.Change, "de completat") {
			t.Errorf("G5 change %q: the owner's answer must be a blank to fill in (PH0-30)", g.Change)
		}
		summary := summaryOf(t, rs)
		if !anyLineHasAll(summary, "**GO condiționat**", "MV4/main", reason) {
			t.Errorf("the summary does not give the fullscreen reason:\n%s", summary)
		}
		if anyLineHasAll(summary, "**GO condiționat**", "cu 9 plăci pică") {
			t.Errorf("the summary says a 9-tile item failed:\n%s", summary)
		}
		// MV4/main is itself critical (R54), so "everything critical passes" would contradict the
		// MV4/main row that shows a software decode in S3-4.
		if anyLineHasAll(summary, "**GO condiționat**", "trece tot ce e critic") || strings.Contains(g.Decision, "trece tot ce e critic") {
			t.Errorf("the summary or G5 says every critical item passes while MV4/main fails:\n%s\n%s", summary, g.Decision)
		}
		if !anyLineHasAll(summary, "**GO condiționat**", "Pragurile critice de bază (cu 4 plăci) trec") {
			t.Errorf("the summary does not name the basis that passes:\n%s", summary)
		}
		if !anyLineHasAll(summary, "De decis de dvs.", "ecranul complet", "MV4/main") || anyLineHasAll(summary, "De decis de dvs.", "A6") {
			t.Errorf("the summary asks the owner about the wrong decision:\n%s", summary)
		}
	})
	t.Run("with a 9-tile failure", func(t *testing.T) {
		rs := allRuns()
		software(t, rs)
		find(t, rs, androidDevice, "S3-9").Samples["tileFps"][5] = 6
		g := gateOf(t, rs, "G5")
		if g.Verdict != VerdictGOConditionat {
			t.Fatalf("G5 = %q, want %q", g.Verdict, VerdictGOConditionat)
		}
		for _, want := range []string{"cu 9 plăci pică MV3/9", "A6", reason} {
			if !strings.Contains(g.Decision, want) {
				t.Errorf("G5 decision %q lacks %q", g.Decision, want)
			}
		}
		summary := summaryOf(t, rs)
		if !anyLineHasAll(summary, "**GO condiționat**", "MV3/9", reason) || !anyLineHasAll(summary, "De decis de dvs.", "A6", "ecranul complet") {
			t.Errorf("the summary lacks one of the two decisions:\n%s", summary)
		}
	})
	t.Run("a 4-tile failure still decides NO-GO mobil", func(t *testing.T) {
		rs := allRuns()
		software(t, rs)
		find(t, rs, androidDevice, "S1").Samples["firstFrameMs"] = rep(5000, 20)
		if g := gateOf(t, rs, "G5"); g.Verdict != VerdictNOGOMobil {
			t.Errorf("G5 = %q, want %q", g.Verdict, VerdictNOGOMobil)
		}
	})
}

// report-9: a device without any result is unmeasured too.
func TestSummary_UnmeasuredListsDevicesWithoutResults(t *testing.T) {
	var rs []Result
	rs = append(rs, windowsRuns()...)
	rs = append(rs, webRuns()...)
	for _, platform := range []string{"chrome", "edge", "firefox"} {
		f := newFixture(browserDevice(platform))
		f.add("S1", map[string][]float64{"firstFrameMs": rep(300, 20)})
		f.add("S2", map[string][]float64{"subLatencyMs": rep(80, 100), "mainLatencyMs": rep(90, 100)})
		f.add("S3-9", gridSamples(9))
		if platform == "chrome" {
			f.add("S3-16", gridSamples(16))
			f.add("S4", desktopStability())
			f.add("S7", map[string][]float64{"startMs": rep(600, 20), "seekMs": rep(400, 20)})
		}
		rs = append(rs, f.list()...)
	}
	summary := section(t, render(t, rs), "## Rezumat pentru proprietar")
	line := rowOf(t, summary, "- **Nemăsurat încă:**")
	if strings.Contains(line, "nimic") || !strings.Contains(line, "AND-1: niciun rezultat") {
		t.Errorf("the summary does not name the phone without results:\n%s", line)
	}
	if strings.Contains(line, "WIN-1") || strings.Contains(line, "Chrome") {
		t.Errorf("complete devices are listed as unmeasured:\n%s", line)
	}
}

// report-10: the advice for several machines names a fresh -out.
func TestWarnings_SeveralMachinesAdviceNamesAFreshOut(t *testing.T) {
	rs := androidRuns()
	find(t, rs, androidDevice, "S1").Device.Model = "Google Pixel 8"
	warnings := section(t, render(t, rs), "## Avertismente")
	if !strings.Contains(warnings, "`-in`") || !strings.Contains(warnings, "`-out`") || !strings.Contains(warnings, "nou") {
		t.Errorf("the advice does not say to give each folder its own -out:\n%s", warnings)
	}
}

// hub-4: web runs are keyed by browser and OS family.
func TestWebRuns_AreKeyedByBrowserAndOSFamily(t *testing.T) {
	pc := newFixture(browserDevice("chrome"))
	pc.add("S1", map[string][]float64{"firstFrameMs": rep(200, 20)})
	pc.add("S10", nil, "pair: host/host", "prompt: n/a")
	// Chrome on the phone, later than the PC's runs.
	phone := newFixture(Device{Platform: "chrome", OS: "Android", WebView: "Chrome/141.0.0.0"})
	late := phone.add("S1", map[string][]float64{"firstFrameMs": rep(2500, 20)})
	late.StartedAt, late.File = "2026-10-18T11:00:00.000Z", "chrome-20261018-110000-s1.json"
	relay := phone.add("S10", nil, "pair: relay/relay", "prompt: n/a")
	relay.StartedAt, relay.File = "2026-10-18T11:05:00.000Z", "chrome-20261018-110500-s10.json"
	rs := append(pc.list(), phone.list()...)
	mv := ComputeMV(rs)
	if v := mv[mvKey("chrome", "MV1")]; !v.Pass || v.Value != 200 {
		t.Errorf("Chrome MV1 = %+v, want the PC's run: a phone run must not replace it", v)
	}
	if got := WebVerdict(mv); got == VerdictPlanBRelay {
		t.Errorf("the phone's relay pair decided the web verdict")
	}
	if _, ok := mv[mvKey("chrome-android", "MV1")]; !ok {
		t.Errorf("the phone's Chrome run has no row of its own: %v", keysOf(mv))
	}
	md := render(t, rs)
	if !strings.Contains(md, "### Web: Chrome pe Android") {
		t.Errorf("the phone's Chrome runs are not listed separately")
	}
	warnings := section(t, md, "## Avertismente")
	if !strings.Contains(warnings, "Chrome pe Android") || !strings.Contains(warnings, "nu intră în verdicte") {
		t.Errorf("no warning about the runs on an unexpected OS:\n%s", warnings)
	}
}

func keysOf(mv map[string]MV) []string {
	var out []string
	for k := range mv {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Task 13b deferred: an IPv6 literal has boundaries.
func TestScrubText_LeavesAScopeOperatorAlone(t *testing.T) {
	for _, s := range []string{"std::string", "Vec::new and Self::default", "std::fs::read"} {
		if got := scrubText(s); got != s {
			t.Errorf("scrubText(%q) = %q", s, got)
		}
	}
	if got := scrubText("from fe80::1 and ::1 and [2001:db8::7]:80 and ip:fe80::2"); got != "from [ip] and [ip] and [ip] and ip:[ip]" {
		t.Errorf("scrubText = %q", got)
	}
}

// ---- Item 7: the device facts ----

func TestDevices_FactsComeFromTheResults(t *testing.T) {
	rs := allRuns()
	for i := range rs {
		switch rs[i].Device.Platform {
		case androidDevice:
			rs[i].Device.RAMMb = 5482
		case windowsDevice:
			rs[i].Device.RAMMb = 16069
			rs[i].Device.GPU = "AMD Radeon(TM) Graphics"
		}
	}
	md := render(t, rs)
	phone, pc := deviceSection(t, md, "### AND-1"), deviceSection(t, md, "### WIN-1")
	for _, want := range []string{"Android 14 (API 34)", "aprox. 6 GB", "5482 MiB"} {
		if !strings.Contains(phone, want) {
			t.Errorf("AND-1 lacks %q:\n%s", want, phone)
		}
	}
	for _, want := range []string{"AMD Ryzen 5 5600G", "AMD Radeon(TM) Graphics", "aprox. 16 GB", "Windows 11 Pro"} {
		if !strings.Contains(pc, want) {
			t.Errorf("WIN-1 lacks %q:\n%s", want, pc)
		}
	}
	if strings.Contains(phone, "De completat") || strings.Contains(pc, "De completat") {
		t.Errorf("the owner is asked for facts that the results have:\n%s\n%s", phone, pc)
	}
	t.Run("only what is missing is asked for", func(t *testing.T) {
		rs := allRuns()
		for i := range rs {
			if rs[i].Device.Platform == windowsDevice {
				rs[i].Device.RAMMb = 16069
			}
		}
		md := render(t, rs)
		ask := rowOf(t, deviceSection(t, md, "### WIN-1"), "De completat")
		if !strings.Contains(ask, "GPU") || strings.Contains(ask, "RAM") || strings.Contains(ask, "procesor") {
			t.Errorf("WIN-1 asks %q, want the GPU only", ask)
		}
		phone := rowOf(t, deviceSection(t, md, "### AND-1"), "De completat")
		if !strings.Contains(phone, "RAM") {
			t.Errorf("AND-1 asks %q, want the RAM", phone)
		}
		// One missing fact takes the singular verb.
		for device, line := range map[string]string{"WIN-1": ask, "AND-1": phone} {
			if !strings.HasSuffix(line, "(nu apare în rezultate).") {
				t.Errorf("%s asks %q, want one fact with «(nu apare în rezultate).»", device, line)
			}
		}
	})
	t.Run("several missing facts take the plural verb", func(t *testing.T) {
		ask := rowOf(t, deviceSection(t, render(t, allRuns()), "### WIN-1"), "De completat")
		if !strings.Contains(ask, " și ") || !strings.HasSuffix(ask, "(nu apar în rezultate).") {
			t.Errorf("WIN-1 asks %q, want several facts with «(nu apar în rezultate).»", ask)
		}
	})
}
