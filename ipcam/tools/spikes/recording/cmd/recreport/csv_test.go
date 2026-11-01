package main

import (
	"encoding/csv"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// records parses a CSV of the report into its header and rows.
func records(t *testing.T, data string) ([]string, [][]string) {
	t.Helper()
	all, err := csv.NewReader(strings.NewReader(data)).ReadAll()
	if err != nil || len(all) == 0 {
		t.Fatalf("CSV %v:\n%s", err, data)
	}
	return all[0], all[1:]
}

// column is the index of name in header.
func column(t *testing.T, header []string, name string) int {
	t.Helper()
	i := slices.Index(header, name)
	if i < 0 {
		t.Fatalf("no column %q in %q", name, header)
	}
	return i
}

// TestCSV_OneRowPerSample (PH0-30): each CSV has one row per sample. The
// MI1 CSV has a row per stream of every run, invalid ones included with
// their InvalidReason (one row for a run without a stream); MI4 every read
// and seek; MI9 every seek and frame shown, a seek that the wait cut
// marked «≥», a lower bound.
func TestCSV_OneRowPerSample(t *testing.T) {
	f := passing(t)
	f.envs = append(f.envs, invalidRun(t, "pacing neuniform (p99 140 ms)"))
	f.each(ofCode("MI9", "pi"), func(e *results.Envelope) {
		e.MI9.Legs[0].SeekMs = []float64{300, 400, 36000}
		e.MI9.Legs[0].Partial = &results.Mi9Partial{ReceivedBytes: 150e6, SizeBytes: 150e6, ElapsedMs: 120e3, LastSeekCut: true}
	})
	csvs := reportOf(t, f).csvs()
	if names := slicesSorted(csvs); !slices.Equal(names, []string{
		"phase0-data/phase0c-recorder-mi1.csv", "phase0-data/phase0c-recorder-mi2.csv", "phase0-data/phase0c-recorder-mi3.csv",
		"phase0-data/phase0c-recorder-mi4.csv", "phase0-data/phase0c-recorder-mi9.csv",
	}) {
		t.Fatalf("CSV files %q", names)
	}

	header, rows := records(t, csvs["phase0-data/phase0c-recorder-mi1.csv"])
	var streams int
	for _, e := range f.envs {
		for _, r := range e.Runs {
			streams += max(1, len(r.Streams))
		}
	}
	if len(rows) != streams {
		t.Errorf("MI1: %d rows, want %d, one per stream of each run", len(rows), streams)
	}
	runID, reason, valid := column(t, header, "runId"), column(t, header, "invalidReason"), column(t, header, "valid")
	invalid := slices.IndexFunc(rows, func(r []string) bool { return r[runID] == "pc-kill9-01-2" })
	if invalid < 0 {
		t.Fatal("MI1: no row of the invalid run")
	}
	if rows[invalid][valid] != "false" || rows[invalid][reason] != "pacing neuniform (p99 140 ms)" {
		t.Errorf("MI1: the invalid run's row %q, want it with its reason", rows[invalid])
	}

	header, rows = records(t, csvs["phase0-data/phase0c-recorder-mi4.csv"])
	key := column(t, header, "key")
	count := func(rows [][]string, col int, value string) int {
		n := 0
		for _, r := range rows {
			if r[col] == value {
				n++
			}
		}
		return n
	}
	if n := count(rows, key, "readMs"); n != 2000 {
		t.Errorf("MI4: %d readMs rows, want 1000 per host", n)
	}
	if n := count(rows, key, "seekMs"); n != 40 {
		t.Errorf("MI4: %d seekMs rows, want 20 per host", n)
	}
	if n := count(rows, key, "agerangeMbps"); n != 1 {
		t.Errorf("MI4: %d agerangeMbps rows, want the AGE run's", n)
	}

	header, rows = records(t, csvs["phase0-data/phase0c-recorder-mi9.csv"])
	key, source, bound, value := column(t, header, "key"), column(t, header, "source"), column(t, header, "bound"), column(t, header, "value")
	if n := count(rows, key, "seekMs"); n != 20+3+20+20+20 {
		t.Errorf("MI9: %d seekMs rows, want every seek of every leg", n)
	}
	if n := count(rows, key, "displayMs"); n != 20 {
		t.Errorf("MI9: %d displayMs rows, want Chrome's 20", n)
	}
	cut := slices.IndexFunc(rows, func(r []string) bool { return r[key] == "seekMs" && r[value] == "36000" })
	if cut < 0 {
		t.Fatal("MI9: no row of the cut seek")
	}
	if rows[cut][bound] != "≥" || rows[cut][source] != "lan" {
		t.Errorf("MI9: the cut seek's row %q, want «≥»", rows[cut])
	}
	if n := count(rows, bound, "≥"); n != 1 {
		t.Errorf("MI9: %d rows marked ≥, want the cut seek alone", n)
	}

	for _, name := range []string{"phase0-data/phase0c-recorder-mi2.csv", "phase0-data/phase0c-recorder-mi3.csv"} {
		header, rows := records(t, csvs[name])
		if hosts := column(t, header, "host"); len(rows) == 0 || count(rows, hosts, "pc") == 0 || count(rows, hosts, "pi") == 0 {
			t.Errorf("%s: %d rows, want both devices' samples", name, len(rows))
		}
	}
}

// slicesSorted is the sorted keys of m.
func slicesSorted(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
