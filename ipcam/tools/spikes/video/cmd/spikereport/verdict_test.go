package main

import (
	"strings"
	"testing"
)

var androidCritical = []string{"MV1", "MV2", "MV3/4", "MV9", "MV10", "MV3/9", "MV4/9", "MV5/9"}

func androidKeys(codes ...string) []string {
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = mvKey(androidDevice, c)
	}
	return out
}

func TestAndroidVerdict_AllPass_GOProvizoriu(t *testing.T) {
	mv := mvSet(androidKeys(androidCritical...), nil, nil)
	if got := AndroidVerdict(mv); got != VerdictGOProvizoriu {
		t.Errorf("verdict = %q, want %q", got, VerdictGOProvizoriu)
	}
	if string(VerdictGOProvizoriu) != "GO provizoriu (fără iOS)" {
		t.Errorf("the verdict text is %q", VerdictGOProvizoriu)
	}
}

func TestAndroidVerdict_Only9TileFails_GOConditionat(t *testing.T) {
	for _, failing := range []string{"MV3/9", "MV4/9", "MV5/9"} {
		t.Run(failing, func(t *testing.T) {
			mv := mvSet(androidKeys(androidCritical...), androidKeys(failing), nil)
			if got := AndroidVerdict(mv); got != VerdictGOConditionat {
				t.Errorf("verdict = %q, want %q", got, VerdictGOConditionat)
			}
		})
	}
	t.Run("all three 9-tile items", func(t *testing.T) {
		mv := mvSet(androidKeys(androidCritical...), androidKeys("MV3/9", "MV4/9", "MV5/9"), nil)
		if got := AndroidVerdict(mv); got != VerdictGOConditionat {
			t.Errorf("verdict = %q, want %q", got, VerdictGOConditionat)
		}
	})
}

func TestAndroidVerdict_MV9Fails_NOGO(t *testing.T) {
	for _, failing := range []string{"MV9", "MV1", "MV2", "MV3/4", "MV10"} {
		t.Run(failing, func(t *testing.T) {
			mv := mvSet(androidKeys(androidCritical...), androidKeys(failing), nil)
			if got := AndroidVerdict(mv); got != VerdictNOGOMobil {
				t.Errorf("verdict = %q, want %q", got, VerdictNOGOMobil)
			}
		})
	}
	t.Run("a 4-tile failure with a 9-tile failure is still NO-GO", func(t *testing.T) {
		mv := mvSet(androidKeys(androidCritical...), androidKeys("MV3/4", "MV3/9"), nil)
		if got := AndroidVerdict(mv); got != VerdictNOGOMobil {
			t.Errorf("verdict = %q, want %q", got, VerdictNOGOMobil)
		}
	})
}

func TestAndroidVerdict_MissingMeasurementsGiveNoVerdict(t *testing.T) {
	t.Run("a critical item was not measured", func(t *testing.T) {
		mv := mvSet(androidKeys(androidCritical...), nil, androidKeys("MV9"))
		if got := AndroidVerdict(mv); got != VerdictIncomplete {
			t.Errorf("verdict = %q, want %q: no NO-GO and no GO on a missing item", got, VerdictIncomplete)
		}
	})
	t.Run("a 9-tile item was not measured", func(t *testing.T) {
		mv := mvSet(androidKeys(androidCritical...), nil, androidKeys("MV5/9"))
		if got := AndroidVerdict(mv); got != VerdictIncomplete {
			t.Errorf("verdict = %q, want %q", got, VerdictIncomplete)
		}
	})
	t.Run("a failed critical item decides even when others are missing", func(t *testing.T) {
		mv := mvSet(androidKeys(androidCritical...), androidKeys("MV2"), androidKeys("MV9", "MV5/9"))
		if got := AndroidVerdict(mv); got != VerdictNOGOMobil {
			t.Errorf("verdict = %q, want %q", got, VerdictNOGOMobil)
		}
	})
	t.Run("a failed 9-tile item with the 4-tile items passing decides GO condiționat", func(t *testing.T) {
		mv := mvSet(androidKeys(androidCritical...), androidKeys("MV3/9"), androidKeys("MV5/9"))
		if got := AndroidVerdict(mv); got != VerdictGOConditionat {
			t.Errorf("verdict = %q, want %q", got, VerdictGOConditionat)
		}
	})
	t.Run("a failed 9-tile item cannot be rescued by a missing 4-tile item", func(t *testing.T) {
		mv := mvSet(androidKeys(androidCritical...), androidKeys("MV3/9"), androidKeys("MV10"))
		if got := AndroidVerdict(mv); got != VerdictIncomplete {
			t.Errorf("verdict = %q, want %q", got, VerdictIncomplete)
		}
	})
	t.Run("nothing measured", func(t *testing.T) {
		if got := AndroidVerdict(map[string]MV{}); got != VerdictIncomplete {
			t.Errorf("verdict = %q, want %q", got, VerdictIncomplete)
		}
	})
	t.Run("the other devices' items do not count", func(t *testing.T) {
		mv := mvSet(androidKeys(androidCritical...), androidKeys("MV9"), nil)
		mv[mvKey("chrome", "MV9")] = MV{Pass: true}
		if got := AndroidVerdict(mv); got != VerdictNOGOMobil {
			t.Errorf("verdict = %q, want %q", got, VerdictNOGOMobil)
		}
	})
}

func TestAndroidVerdict_OnlyItsCriticalItemsDecide(t *testing.T) {
	keys := append(androidKeys(androidCritical...), androidKeys("MV7", "MV8", "MV15", "G9", "MV12", "MV3/16")...)
	mv := mvSet(keys, androidKeys("MV7", "MV8", "MV15", "G9", "MV12", "MV3/16"), nil)
	if got := AndroidVerdict(mv); got != VerdictGOProvizoriu {
		t.Errorf("verdict = %q, want %q: MV7, MV8, MV15, G9 and MV12 are reported, not critical", got, VerdictGOProvizoriu)
	}
}

func TestWindowsVerdict_MV6Fails_PlanB9(t *testing.T) {
	mv := mvSet([]string{mvKey(windowsDevice, "MV6")}, []string{mvKey(windowsDevice, "MV6")}, nil)
	if got := WindowsVerdict(mv); got != VerdictPlanBGrid9 {
		t.Errorf("verdict = %q, want %q", got, VerdictPlanBGrid9)
	}
	if string(VerdictPlanBGrid9) != "plan B: grilă desktop 9" {
		t.Errorf("the verdict text is %q", VerdictPlanBGrid9)
	}
}

func TestWindowsVerdict_OtherCases(t *testing.T) {
	key := mvKey(windowsDevice, "MV6")
	if got := WindowsVerdict(mvSet([]string{key}, nil, nil)); got != VerdictGO {
		t.Errorf("MV6 passing: verdict = %q, want %q", got, VerdictGO)
	}
	if got := WindowsVerdict(map[string]MV{}); got != VerdictIncomplete {
		t.Errorf("MV6 not measured: verdict = %q, want %q", got, VerdictIncomplete)
	}
	// Windows cannot cause NO-GO: no input gives a mobile verdict.
	for _, mv := range []map[string]MV{
		mvSet([]string{key, mvKey(windowsDevice, "MV7")}, []string{key, mvKey(windowsDevice, "MV7")}, nil),
		{},
	} {
		if got := WindowsVerdict(mv); got == VerdictNOGOMobil {
			t.Errorf("Windows gave %q", got)
		}
	}
	t.Run("MV7 is reported and does not change the verdict", func(t *testing.T) {
		mv := mvSet([]string{key, mvKey(windowsDevice, "MV7")}, []string{mvKey(windowsDevice, "MV7")}, nil)
		if got := WindowsVerdict(mv); got != VerdictGO {
			t.Errorf("verdict = %q, want %q", got, VerdictGO)
		}
	})
}

var requiredWebKeys = []string{
	mvKey("chrome", "MV13"), mvKey("edge", "MV13"), mvKey("firefox", "MV13"),
	mvKey(androidDevice, "MV12"), mvKey(windowsDevice, "MV12"),
}

func TestWebVerdict_MV13Relay_PlanB(t *testing.T) {
	for _, bad := range []string{mvKey("chrome", "MV13"), mvKey("edge", "MV13"), mvKey("firefox", "MV13")} {
		t.Run(bad, func(t *testing.T) {
			if got := WebVerdict(mvSet(requiredWebKeys, []string{bad}, nil)); got != VerdictPlanBRelay {
				t.Errorf("verdict = %q, want %q", got, VerdictPlanBRelay)
			}
		})
	}
	if string(VerdictPlanBRelay) != "plan B: relay în LAN + aplicația nativă" {
		t.Errorf("the verdict text is %q", VerdictPlanBRelay)
	}
}

func TestWebVerdict_MV12Relay_PlanB(t *testing.T) {
	for _, bad := range []string{mvKey(androidDevice, "MV12"), mvKey(windowsDevice, "MV12")} {
		t.Run(bad, func(t *testing.T) {
			if got := WebVerdict(mvSet(requiredWebKeys, []string{bad}, nil)); got != VerdictPlanBRelay {
				t.Errorf("verdict = %q, want %q", got, VerdictPlanBRelay)
			}
		})
	}
}

func TestWebVerdict_GOAndIncomplete(t *testing.T) {
	if got := WebVerdict(mvSet(requiredWebKeys, nil, nil)); got != VerdictGO {
		t.Errorf("all passing: verdict = %q, want %q", got, VerdictGO)
	}
	if got := WebVerdict(mvSet(requiredWebKeys, nil, []string{mvKey("edge", "MV13")})); got != VerdictIncomplete {
		t.Errorf("Edge missing: verdict = %q, want %q", got, VerdictIncomplete)
	}
	if got := WebVerdict(mvSet(requiredWebKeys, []string{mvKey("firefox", "MV13")}, []string{mvKey("edge", "MV13")})); got != VerdictPlanBRelay {
		t.Errorf("a failure outweighs a missing item: verdict = %q, want %q", got, VerdictPlanBRelay)
	}
	if got := WebVerdict(map[string]MV{}); got != VerdictIncomplete {
		t.Errorf("nothing measured: verdict = %q, want %q", got, VerdictIncomplete)
	}
}

func TestVerdicts_FromTheResults(t *testing.T) {
	t.Run("a passing set", func(t *testing.T) {
		mv := ComputeMV(allRuns())
		if a, w, web := AndroidVerdict(mv), WindowsVerdict(mv), WebVerdict(mv); a != VerdictGOProvizoriu || w != VerdictGO || web != VerdictGO {
			t.Errorf("verdicts = %q, %q, %q", a, w, web)
		}
	})
	t.Run("9 tiles that stutter", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, androidDevice, "S3-9").Samples["tileFps"][5] = 6
		if got := AndroidVerdict(ComputeMV(rs)); got != VerdictGOConditionat {
			t.Errorf("verdict = %q, want %q", got, VerdictGOConditionat)
		}
	})
	t.Run("an app that does not come back from the background", func(t *testing.T) {
		rs := allRuns()
		s5 := find(t, rs, androidDevice, "S5")
		s5.Samples = map[string][]float64{"backgroundS": {125}}
		s5.Notes = []string{"no frame within 30 s of the return"}
		if got := AndroidVerdict(ComputeMV(rs)); got != VerdictNOGOMobil {
			t.Errorf("verdict = %q, want %q", got, VerdictNOGOMobil)
		}
	})
	t.Run("HLS starts that sometimes never show a frame", func(t *testing.T) {
		rs := allRuns()
		s7 := find(t, rs, androidDevice, "S7")
		s7.Samples["startMs"] = s7.Samples["startMs"][:18]
		s7.Samples["seekMs"] = s7.Samples["seekMs"][:18]
		s7.Notes = append(s7.Notes, "start 3: no first frame within 10 s", "start 9: no first frame within 10 s")
		if got := AndroidVerdict(ComputeMV(rs)); got != VerdictNOGOMobil {
			t.Errorf("verdict = %q, want %q: MV10 is a critical item", got, VerdictNOGOMobil)
		}
	})
	t.Run("a PC that cannot decode 16 tiles", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, windowsDevice, "S3-16").Samples["cpuPctApp"] = rep(40, 60)
		if got := WindowsVerdict(ComputeMV(rs)); got != VerdictPlanBGrid9 {
			t.Errorf("verdict = %q, want %q", got, VerdictPlanBGrid9)
		}
	})
	t.Run("a browser whose pair goes through a relay", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, "firefox", "S10").Notes = []string{"pair: relay/host", "prompt: n/a"}
		if got := WebVerdict(ComputeMV(rs)); got != VerdictPlanBRelay {
			t.Errorf("verdict = %q, want %q", got, VerdictPlanBRelay)
		}
	})
	t.Run("an interrupted run leaves the verdict open", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, androidDevice, "S5").Interrupted = true
		if got := AndroidVerdict(ComputeMV(rs)); got != VerdictIncomplete || !strings.Contains(string(got), "fără verdict") {
			t.Errorf("verdict = %q, want %q", got, VerdictIncomplete)
		}
	})
}

func TestGates_TheDecisionOfEachVerdict(t *testing.T) {
	gate := func(t *testing.T, rs []Result, id string) Gate {
		t.Helper()
		for _, g := range buildGates(ComputeMV(rs)) {
			if g.ID == id {
				return g
			}
		}
		t.Fatalf("no gate %s", id)
		return Gate{}
	}
	t.Run("G5 GO condiționat asks for the owner's decision on A6", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, androidDevice, "S3-9").Samples["tileFps"][5] = 6
		g := gate(t, rs, "G5")
		text := g.Decision + " " + g.Change
		for _, want := range []string{"A6", "9", "aprobarea", "§2.1", "snapshot"} {
			if !strings.Contains(text, want) {
				t.Errorf("G5 decision %q lacks %q", text, want)
			}
		}
		if !strings.Contains(g.Change, "de completat") {
			t.Errorf("G5 change %q: the owner's answer must be a blank to fill in (PH0-30)", g.Change)
		}
	})
	t.Run("G5 NO-GO is T29", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, androidDevice, "S1").Samples["firstFrameMs"] = rep(5000, 20)
		g := gate(t, rs, "G5")
		if g.Verdict != VerdictNOGOMobil || !strings.Contains(g.Decision, "T29") || !strings.Contains(g.Decision, "Flutter") {
			t.Errorf("G5 = %+v", g)
		}
	})
	t.Run("G5 GO provizoriu waits for iOS", func(t *testing.T) {
		g := gate(t, allRuns(), "G5")
		if g.Verdict != VerdictGOProvizoriu || !strings.Contains(g.Decision, "iOS") {
			t.Errorf("G5 = %+v", g)
		}
	})
	t.Run("G6 plan B is a 9-tile desktop grid", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, windowsDevice, "S3-16").Samples["cpuPctApp"] = rep(40, 60)
		g := gate(t, rs, "G6")
		if g.Verdict != VerdictPlanBGrid9 || !strings.Contains(g.Decision, "9 plăci") {
			t.Errorf("G6 = %+v", g)
		}
	})
	t.Run("G7 plan B is relay", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, "chrome", "S10").Notes = []string{"pair: relay/relay", "prompt: n/a"}
		g := gate(t, rs, "G7")
		if g.Verdict != VerdictPlanBRelay || !strings.Contains(g.Decision, "APP-32") {
			t.Errorf("G7 = %+v", g)
		}
	})
	t.Run("G19 failing lowers the Argon2id parameters", func(t *testing.T) {
		rs := allRuns()
		find(t, rs, androidDevice, "S8").Samples["argon2idMs"] = rep(3500, 10)
		g := gate(t, rs, "G19")
		if !strings.Contains(g.Decision, "m=46 MiB, t=1, p=1") {
			t.Errorf("G19 = %+v", g)
		}
	})
	t.Run("G9 records each path", func(t *testing.T) {
		g := gate(t, allRuns(), "G9")
		if !strings.Contains(g.Result, "mx-h265") || !strings.Contains(g.Result, "fallback") {
			t.Errorf("G9 = %+v", g)
		}
	})
	t.Run("a gate without data says what is missing", func(t *testing.T) {
		rs := allRuns()
		var kept []Result
		for _, r := range rs {
			if r.Scenario != "S5" {
				kept = append(kept, r)
			}
		}
		g := gate(t, kept, "G5")
		if g.Verdict != VerdictIncomplete || !strings.Contains(g.Decision, "MV9") {
			t.Errorf("G5 = %+v, want the missing MV9 named", g)
		}
	})
}
