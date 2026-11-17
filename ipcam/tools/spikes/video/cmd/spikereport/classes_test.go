package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
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
