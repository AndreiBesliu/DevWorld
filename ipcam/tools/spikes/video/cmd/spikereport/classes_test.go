package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClassify_TheSharedFixtures reads testdata/classes, results files of every
// class that the report knows, against testdata/classes.json: the same
// fixtures and expectations that spikehub's
// TestProgress_CountsTheRunsThatTheReportUses reads, so that the automatic run
// skips a scenario exactly when the report can use its run.
func TestClassify_TheSharedFixtures(t *testing.T) {
	data, err := fs.ReadFile(os.DirFS(filepath.Join("..", "..", "testdata")), "classes.json")
	if err != nil {
		t.Fatal(err)
	}
	var classes map[string]struct {
		Key   string `json:"key"`
		Class string `json:"class"`
	}
	if err := json.Unmarshal(data, &classes); err != nil {
		t.Fatal(err)
	}
	results, skipped, err := loadResults(filepath.Join("..", "..", "testdata", "classes"))
	if err != nil || len(skipped) != 0 || len(results) != len(classes) || len(results) == 0 {
		t.Fatalf("results = %d (classes.json lists %d), skipped = %+v, err = %v", len(results), len(classes), skipped, err)
	}
	statusOf := map[string]RunStatus{
		"usable":      RunUsed,
		"interrupted": RunInterrupted,
		"failed":      RunFailed,
		"on-charger":  RunInvalid,
		"informative": RunInformative,
	}
	for _, run := range classify(results) {
		want, ok := classes[run.File]
		if !ok {
			t.Errorf("%s: not in classes.json", run.File)
			continue
		}
		if run.Device.Platform != want.Key {
			t.Errorf("%s: device %q, want %q", run.File, run.Device.Platform, want.Key)
		}
		got := run.Status
		if got == RunSuperseded {
			got = RunUsed // usable, and a newer run of the same scenario is used
		}
		if wantStatus, known := statusOf[want.Class]; !known || got != wantStatus {
			t.Errorf("%s: status %v, want %s", run.File, run.Status, want.Class)
		}
	}
}

// TestMV7_ThePhonesS4OnBattery_IsJudged: the automatic run keeps the phone off
// the charger from its start to «Gata» (S3 refuses the charger, R48, and the run
// is left alone), so the phone's S4 runs on battery. v0.1.0's guide put S4 on
// the charger (the plan's Task 14, guide step 5). Ruling B-R4 accepts S4 on
// battery only if the report judges MV7 on such a run; otherwise the run would
// have to call the person to plug the charger in before S4 and out before S5.
// testdata/classes/android-s4-battery.json is the phone's S4 as v0.1.1 writes
// it: 30 min, memory every 30 s, 9 tiles at most, and no charger reading,
// because S4 reads none (app/src/scenarios/s4.ts).
func TestMV7_ThePhonesS4OnBattery_IsJudged(t *testing.T) {
	const file = "android-s4-battery.json"
	in := t.TempDir()
	write(t, in, file, readText(t, filepath.Join("..", "..", "testdata", "classes", file)))
	results, skipped, err := loadResults(in)
	if err != nil || len(skipped) != 0 || len(results) != 1 {
		t.Fatalf("results = %d, skipped = %+v, err = %v", len(results), skipped, err)
	}
	if run := classify(results)[0]; run.Device.Platform != androidDevice || run.Status != RunUsed {
		t.Fatalf("the phone's S4 on battery: device %q, status %s; want android, used", run.Device.Platform, statusWord[run.Status])
	}
	if v, ok := ComputeMV(results)[mvKey(androidDevice, "MV7")]; !ok || !v.Pass {
		t.Fatalf("the phone's MV7 = %+v (judged: %v), want a pass", v, ok)
	}

	out := t.TempDir()
	if code, _, stderr := runMain(t, "-in", in, "-out", out, "-date", "2026-11-18"); code != 0 || stderr != "" {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	md := readText(t, filepath.Join(out, "2026-11-18-phase0b-results.md"))
	if row := rowOf(t, md, "| MV7 |"); !strings.HasSuffix(row, "| Da |") || !strings.Contains(row, "reporniri neprovocate: 0") {
		t.Errorf("the phone's MV7 row does not judge the run on battery:\n%s", row)
	}
	if row := rowOf(t, md, "| "+file+" |"); !strings.Contains(row, statusLabel[RunUsed]) {
		t.Errorf("the runs table does not use the run on battery:\n%s", row)
	}
	if strings.Contains(md, "**Rulări invalide**") {
		t.Error("the report lists an invalid run, but the only run is the phone's S4 on battery")
	}
}
