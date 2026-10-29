package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi4"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// runIDs are the run ids of the envelopes of code, sorted.
func runIDs(envs []results.Envelope, code string) []string {
	var out []string
	for _, e := range envs {
		if e.Code == code {
			out = append(out, e.RunID)
		}
	}
	slices.Sort(out)
	return out
}

// TestInputs_DedupByRunID_TwoDevices (G15): the PC's results folder holds
// the Pi's, pulled over the link, and the Pi's own folder holds the same
// envelopes again: each is read once. Files that are not envelopes are
// listed as not read, and the report says so without the folder's path;
// recspike's own state under state/ is no result and is not listed.
func TestInputs_DedupByRunID_TwoDevices(t *testing.T) {
	in, err := load([]string{"testdata/two-device/pc", "testdata/two-device/pi"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"MI1": {"pc-kill9-01-1", "pi-kill9-01-1"},
		"MI2": {"pc-72h-1", "pi-72h-1"},
		"MI4": {"app-age-1", "pi-mi4-1"},
		"MI9": {"pc-mi9-1", "pi-mi9-1"},
	}
	for code, ids := range want {
		if got := runIDs(in.envs, code); !slices.Equal(got, ids) {
			t.Errorf("%s: run ids %q, want %q, each once", code, got, ids)
		}
	}
	if in.copies != 4 {
		t.Errorf("copies dropped: %d, want the Pi's 4 envelopes found in both folders", in.copies)
	}
	if len(in.renumbered) != 0 {
		t.Errorf("renumbered %+v: identical copies are not renumbered", in.renumbered)
	}
	if len(in.skipped) != 1 || in.skipped[0].file != "notes.json" {
		t.Errorf("skipped %+v, want notes.json, which is no envelope", in.skipped)
	}
	md := newReport(in, goldenDate).markdown()
	if !strings.Contains(md, "`notes.json`") || strings.Contains(md, "testdata") {
		t.Errorf("the report must name the file not read, without the folder's path:\n%s", md)
	}
}

// TestInputs_ReadsAnnotations (C32): for each CI run, recreport lists its
// jobs and reads each job's check-run annotations with gh, every call
// paginated, the fake gh here answering in two pages. The lab set
// lab-linux-kill9 comes back whole, its 21 one-run parts; lab-windows-kill9
// lacks part 7, so its part of G10 has no verdict, and the report names
// the set and the part. The ENC-06 line becomes the ENC-06 envelope, with
// the time its check ran (C1-R22); the Chrome line becomes the ci-chrome
// MI9 envelope; mtx2-loss is kept for the R01 decision.
func TestInputs_ReadsAnnotations(t *testing.T) {
	calls := installFakeGH(t, annotationsDir)
	in, err := load(nil, []string{labRun, ciRun})
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(calls) //nolint:gosec // the test's log
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"api repos/AndreiBesliu/DevWorld/actions/runs/101/jobs --paginate",
		"api repos/AndreiBesliu/DevWorld/check-runs/1001/annotations --paginate",
		"api repos/AndreiBesliu/DevWorld/check-runs/1003/annotations --paginate",
		"api repos/AndreiBesliu/DevWorld/actions/runs/102/jobs --paginate",
		"api repos/AndreiBesliu/DevWorld/check-runs/2002/annotations --paginate",
	} {
		if !strings.Contains(string(log), want+"\n") {
			t.Errorf("gh was not called as «gh %s»; calls:\n%s", want, log)
		}
	}

	parts := map[string][]int{}
	for _, e := range in.envs {
		if e.Code == "MI1" {
			parts[e.Set] = append(parts[e.Set], e.Part)
		}
	}
	if n := len(parts["lab-linux-kill9"]); n != 21 {
		t.Errorf("lab-linux-kill9: %d parts read, want 21", n)
	}
	if n := len(parts["lab-windows-kill9"]); n != 20 || slices.Contains(parts["lab-windows-kill9"], 7) {
		t.Errorf("lab-windows-kill9: parts %v, want the 20 without part 7", parts["lab-windows-kill9"])
	}
	if len(in.incomplete) != 1 || in.incomplete[0].set != "lab-windows-kill9" || !slices.Equal(in.incomplete[0].missing, []int{7}) {
		t.Errorf("incomplete lab sets %+v, want lab-windows-kill9 without part 7", in.incomplete)
	}

	var enc06, chrome *results.Envelope
	for i, e := range in.envs {
		switch {
		case e.Code == "ENC-06":
			enc06 = &in.envs[i]
		case e.Code == "MI9" && e.Set == "ci-chrome":
			chrome = &in.envs[i]
		}
	}
	if enc06 == nil || enc06.Host != "lab-linux" || enc06.Set != "ci-enc06" || enc06.MI4 == nil || enc06.MI4.Enc06 == nil || !*enc06.MI4.Enc06 ||
		enc06.AtMs != enc06Done.UnixMilli() {
		t.Errorf("ENC-06 envelope %+v, want lab-linux ci-enc06 passing, at the check's completion %v", enc06, enc06Done)
	}
	if chrome == nil || chrome.Host != "lab-linux" || len(chrome.MI9.Legs) != 1 || chrome.MI9.Legs[0].Source != "ci-chrome" ||
		len(chrome.MI9.Legs[0].DisplayMs) != 20 || len(chrome.MI9.Legs[0].SeekMs) != 20 {
		t.Errorf("Chrome envelope %+v, want lab-linux ci-chrome with 20 seeks and 20 frames shown", chrome)
	}
	if len(in.ci.mtx2) != 1 || len(in.ci.mtx2[0].Runs) != 10 {
		t.Errorf("mtx2-loss %+v, want the one notice with its 10 runs", in.ci.mtx2)
	}

	r := newReport(in, goldenDate)
	if r.g10().Verdict != results.Incomplete {
		t.Errorf("G10 = %q, want fără verdict", r.g10().Verdict)
	}
	md := r.markdown()
	if !strings.Contains(md, "`lab-windows-kill9`: lipsește partea 7 din 21") {
		t.Errorf("the report does not name the missing part:\n%s", md)
	}
	if row := tableRow(t, md, "MI1 lab-windows-kill9: mediana"); row[3] != "–" {
		t.Errorf("lab-windows-kill9's median row %q, want no pass value while the set is incomplete", row)
	}
	if row := tableRow(t, md, "MI1 lab-linux-kill9: mediana"); row[3] != "✓" {
		t.Errorf("lab-linux-kill9's median row %q, want ✓", row)
	}
}

// TestInputs_AgeResultsBecomeMI4 (G24; Tasks 10 and 10b): each AGE result
// of the app under results/app/ becomes an MI4 envelope through
// mi4.AgerangeRun, numbered in the order they were stored and stamped with
// the time in the file's name, so that G13's agerange part is judged; an
// interrupted result, or one without mbps, has none.
func TestInputs_AgeResultsBecomeMI4(t *testing.T) {
	f := passing(t)
	first := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	f.app = []appResult{
		ageFile(t, "android", first.Add(time.Hour), 9.5),
		ageFile(t, "android", first, 31.5),
		ageFile(t, "windows-tauri", first.Add(2*time.Hour), 400),
		{name: "android-samsung-sm-a245f-20261030-110000-age.json", data: []byte(`{"scenario":"AGE","device":{"platform":"android"},"interrupted":true}`)},
		{name: "android-samsung-sm-a245f-20261030-111500-s1.json", data: []byte(`{"scenario":"S1","device":{"platform":"android"}}`)},
	}
	in, err := load([]string{f.folder(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var age []results.Envelope
	for _, e := range in.envs {
		if e.Set == mi4.AgerangeSet {
			age = append(age, e)
		}
	}
	want := []results.Envelope{
		mi4.AgerangeRun(1, first, "android", 31.5),
		mi4.AgerangeRun(2, first.Add(time.Hour), "android", 9.5),
		mi4.AgerangeRun(3, first.Add(2*time.Hour), "windows-tauri", 400),
	}
	if len(age) != len(want) {
		t.Fatalf("AGE envelopes %+v, want %+v", age, want)
	}
	for i := range want {
		if age[i].RunID != want[i].RunID || age[i].AtMs != want[i].AtMs || age[i].OS != want[i].OS || *age[i].MI4.AgerangeMbps != *want[i].MI4.AgerangeMbps {
			t.Errorf("AGE envelope %d = %+v, want %+v", i+1, age[i], want[i])
		}
	}
	// The latest android run decides: 9.5 Mbps fails G13 with F6.
	if j := newReport(in, goldenDate).g13(); j.Verdict != results.Fail || j.PlanB != mi4.PlanBF6 {
		t.Errorf("G13 = %q → %q, want pică → %s from the later AGE run", j.Verdict, j.PlanB, mi4.PlanBF6)
	}
}

// TestInputs_RenumbersMergedFolders (G15, C1-R22; Task 10b): the judges
// take a set's latest run by its number before its time, so an older
// folder merged with a newer one could let an older, higher number win.
// When two different envelopes of a set share a run id, recreport
// renumbers the set's envelopes by their time before judging, and the
// report says so.
func TestInputs_RenumbersMergedFolders(t *testing.T) {
	newer := passing(t)
	older := &fixture{}
	day := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	for n := 1; n <= 3; n++ {
		e := mi4Envelope("pi", day.Add(time.Duration(n)*time.Hour))
		e.RunID = fmt.Sprintf("pi-mi4-%d", n)
		if n == 3 {
			e.MI4.ReadMs[0], e.MI4.ReadMaxMs = 80, 80 // the older folder's last run fails
		}
		older.envs = append(older.envs, e)
	}
	in, err := load([]string{newer.folder(t), older.folder(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := runIDs(in.envs, "MI4"); !slices.Equal(got, []string{"app-age-1", "pc-mi4-1", "pi-mi4-1", "pi-mi4-2", "pi-mi4-3", "pi-mi4-4"}) {
		t.Errorf("MI4 run ids %q, want the Pi's four runs renumbered", got)
	}
	for _, e := range in.envs {
		if e.RunID == "pi-mi4-4" && e.AtMs != newer.envs[slices.IndexFunc(newer.envs, func(e results.Envelope) bool { return e.Set == "pi-mi4" })].AtMs {
			t.Errorf("pi-mi4-4 is %+v, want the newest run", e)
		}
	}
	r := newReport(in, goldenDate)
	if j := r.g13(); j.Verdict != results.Pass {
		t.Errorf("G13 = %q, reasons %q; want the newest run's pass, not the older folder's failing third", j.Verdict, j.Reasons)
	}
	input := section(t, r.markdown(), "## Intrarea raportului")
	if !strings.Contains(input, "renumerotat") || !strings.Contains(input, "`MI4 pi-mi4`") {
		t.Errorf("the report does not say that the input was renumbered:\n%s", input)
	}
}

// TestInputs_ConflictingMI1RunsNamed (G15): two different results of the
// same MI1 run, as from two lab runs, are not renumbered: a run id names
// its position and attempt. Both reach G10, which counts one, and the
// report names the set.
func TestInputs_ConflictingMI1RunsNamed(t *testing.T) {
	a, b := passing(t), &fixture{}
	for _, e := range a.envs {
		if e.Set == "lab-linux-kill9" {
			c := e
			c.Runs = []results.Run{e.Runs[0]}
			c.Runs[0].PacingP99Ms = 7
			b.envs = append(b.envs, c)
		}
	}
	in, err := load([]string{a.folder(t), b.folder(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.conflicts) != 1 || in.conflicts[0].set != "lab-linux-kill9" || in.conflicts[0].runs != 21 {
		t.Errorf("conflicts %+v, want lab-linux-kill9's 21 runs", in.conflicts)
	}
	if md := newReport(in, goldenDate).markdown(); !strings.Contains(md, "`lab-linux-kill9`: 21 de rulări") {
		t.Errorf("the report does not name the set:\n%s", section(t, md, "## Intrarea raportului"))
	}
}

// TestInputs_WriteNothingWithoutInput: recreport needs a folder or a CI run.
func TestInputs_WriteNothingWithoutInput(t *testing.T) {
	out := t.TempDir()
	if code, text := runReport("-out", out, "-date", goldenDate); code != 2 || !strings.Contains(text, "-in") {
		t.Errorf("recreport without input = %d %q, want 2 and the usage", code, text)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Errorf("wrote %v", entries)
	}
	if code, _ := runReport("-annotations", "12a", "-out", filepath.Join(out, "x")); code != 2 {
		t.Errorf("a run id that is not a number = %d, want 2", code)
	}
}

// TestInputs_SmokeSetsNotJudged (C32): the recorder workflow's annotations
// carry the smoke runs of the lab sets, two parts of 21 under a -smoke set,
// which G10 does not judge: they are not named as incomplete lab sets, and
// the report counts them apart.
func TestInputs_SmokeSetsNotJudged(t *testing.T) {
	f := passing(t)
	for _, e := range labEnvelopes(t, "lab-linux-kill9", "lab-linux")[:2] {
		e.Set, e.RunID = "lab-linux-kill9-smoke", strings.Replace(e.RunID, "kill9", "kill9-smoke", 1)
		e.Runs[0].RunID = e.RunID
		f.envs = append(f.envs, e)
	}
	in, err := load([]string{f.folder(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.incomplete) != 0 {
		t.Errorf("incomplete lab sets %+v, want none: a smoke set is no set of G10", in.incomplete)
	}
	if md := newReport(in, goldenDate).markdown(); !strings.Contains(section(t, md, "## Intrarea raportului"), "`lab-linux-kill9-smoke` (2)") {
		t.Errorf("the input section does not count the smoke runs:\n%s", section(t, md, "## Intrarea raportului"))
	}
}
