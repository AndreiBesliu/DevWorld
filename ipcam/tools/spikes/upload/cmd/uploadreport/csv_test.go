package main

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// TestCSV_OneRowPerSample: MI5 has a row per destination of each run, MI6
// one per pair-day and one per visibility cell, MI8 one per episode.
func TestCSV_OneRowPerSample(t *testing.T) {
	envs := []results.Envelope{mi5Env("pc", "ssd"), labEnv(), mi6Env(), mi8Env()}
	files := newReport(inputsOf(envs...), defaultOpts()).csvs()
	count := func(name string) [][]string {
		t.Helper()
		rows, err := csv.NewReader(strings.NewReader(files[name])).ReadAll()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return rows
	}
	mi5Rows := count(csvMI5)
	if len(mi5Rows) != 1+3 {
		t.Errorf("MI5: %d rows, want header + 3", len(mi5Rows))
	}
	mi6Rows := count(csvMI6)
	if len(mi6Rows) != 1+3*7+12 {
		t.Errorf("MI6: %d rows, want header + 21 days + 12 cells", len(mi6Rows))
	}
	mi8Rows := count(csvMI8)
	if len(mi8Rows) != 1+22+24 {
		t.Errorf("MI8: %d rows, want header + 46 episodes", len(mi8Rows))
	}
	for name, rows := range map[string][][]string{csvMI5: mi5Rows, csvMI6: mi6Rows, csvMI8: mi8Rows} {
		for i, r := range rows {
			if len(r) != len(rows[0]) {
				t.Errorf("%s row %d: %d columns, want %d", name, i, len(r), len(rows[0]))
			}
		}
	}
	if !strings.Contains(files[csvMI8], "real-1,Hikvision,DS-2CD2143G2-I,substream,600,") {
		t.Errorf("MI8 csv:\n%s", files[csvMI8][:200])
	}
	for name, text := range files {
		if strings.Contains(text, "20261113T090000Z") {
			t.Errorf("%s: a run id in upper case, which the redaction scanner takes for a serial", name)
		}
	}
}
