package mi3

import (
	"slices"
	"testing"
)

// meeting are the figures of a step that meets every budget of §4.8 with 8
// cameras on H3: 55 % total CPU.
func meeting() Figures {
	return Figures{
		CPUPctTotal: 55, TileSubPct: 0.4, TileMainPct: 1.5, EncUploadPct: 2, IngestPerCameraPct: 1.2,
		RAMMb: 320, DiskRatio: map[string]float64{"clear": 1.01, "encrypted": 1.04}, FsyncPerSecPerFile: 1,
		LatencyP95Ms: 60, PacingP99Ms: 8,
	}
}

// TestMI3_BudgetSetPerTier (§4.8, G20): each tier is judged on its own set
// of budgets: H3 on every row of §4.8 but the per-camera ingest, which has
// no H3 figure; H1 on those and the ingest; H1b and H1c on total CPU and RAM
// only. A step's budgets are exactly its tier's.
func TestMI3_BudgetSetPerTier(t *testing.T) {
	h3 := []string{"cpuTotal", "tileSub", "tileMain", "encUpload", "ram", "disk", "fsyncPerFile", "latencyP95"}
	cases := map[string][]string{
		"H3":  h3,
		"H1":  append(slices.Clone(h3), "ingestPerCamera"),
		"H1b": {"cpuTotal", "ram"},
		"H1c": {"cpuTotal", "ram"},
	}
	for tier, want := range cases {
		got := BudgetKeys(tier)
		if !sameSet(got, want) {
			t.Errorf("BudgetKeys(%s) = %v, want %v", tier, got, want)
		}
		var keys []string
		for k := range Budgets(tier, 4, meeting()) {
			keys = append(keys, k)
		}
		if !sameSet(keys, want) {
			t.Errorf("Budgets(%s) has %v, want %v", tier, keys, want)
		}
	}
}

// TestMI3_BudgetLimits (§4.8): the RAM budget grows with the cameras and the
// tier's live sessions (H3 3, H1 2, H1b and H1c 1), and a value at its limit
// passes.
func TestMI3_BudgetLimits(t *testing.T) {
	cases := []struct {
		tier    string
		cameras int
		ram     float64
	}{
		{"H3", 8, 250 + 20*8 + 15*3},
		{"H1", 8, 250 + 20*8 + 15*2},
		{"H1b", 4, 250 + 20*4 + 15},
		{"H1c", 2, 250 + 20*2 + 15},
	}
	for _, c := range cases {
		f := meeting()
		f.RAMMb = c.ram
		b := Budgets(c.tier, c.cameras, f)["ram"]
		if b.Limit != c.ram || !b.Pass {
			t.Errorf("%s with %d cameras: ram %+v, want the limit %v met", c.tier, c.cameras, b, c.ram)
		}
		f.RAMMb = c.ram + 1
		if Budgets(c.tier, c.cameras, f)["ram"].Pass {
			t.Errorf("%s with %d cameras: %v MB passes the RAM budget", c.tier, c.cameras, f.RAMMb)
		}
	}
	limits := map[string]float64{"cpuTotal": 60, "tileSub": 1, "tileMain": 3, "encUpload": 5, "disk": 1.03, "fsyncPerFile": 1, "latencyP95": 150, "ingestPerCamera": 3}
	for key, budget := range Budgets("H1", 8, meeting()) {
		if want, ok := limits[key]; ok && budget.Limit != want {
			t.Errorf("%s: limit %v, want %v", key, budget.Limit, want)
		}
		if !budget.Pass {
			t.Errorf("%s: %+v fails with the figures that meet every budget", key, budget)
		}
	}
}

// TestMI3_DiskBudgetJudgedOnClearCameras (R02): the disk budget is judged on
// the clear cameras alone; the encrypted cameras' padding is a property of
// the format, reported apart.
func TestMI3_DiskBudgetJudgedOnClearCameras(t *testing.T) {
	f := meeting()
	f.DiskRatio = map[string]float64{"clear": 1.02, "encrypted": 1.05}
	if b := Budgets("H3", 8, f)["disk"]; !b.Pass || b.Value != 1.02 {
		t.Errorf("clear 1.02, encrypted 1.05: disk %+v, want the clear ratio, passing", b)
	}
	f.DiskRatio = map[string]float64{"clear": 1.04, "encrypted": 1.01}
	if b := Budgets("H3", 8, f)["disk"]; b.Pass || b.Value != 1.04 {
		t.Errorf("clear 1.04: disk %+v, want it to fail", b)
	}
}

// TestMI3_ValidOnlyWithEvenPacing (R07): a step counts only when the
// controller sent evenly: p99 at most 100 ms and no skipped access unit.
func TestMI3_ValidOnlyWithEvenPacing(t *testing.T) {
	for _, c := range []struct {
		p99     int64
		skipped int
		valid   bool
	}{{100, 0, true}, {101, 0, false}, {110, 0, false}, {8, 1, false}} {
		f := meeting()
		f.PacingP99Ms, f.SkippedAUs = c.p99, c.skipped
		if got := Valid(f); got != c.valid {
			t.Errorf("p99 %d ms, %d skipped: Valid %v, want %v", c.p99, c.skipped, got, c.valid)
		}
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
