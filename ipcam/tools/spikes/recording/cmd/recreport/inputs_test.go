package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
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
	for _, want := range []string{"renumerotat", "`MI4 pi-mi4`", "după momentul lor",
		"G13 judecă rularea cu numărul cel mai mare, `pi-mi4-4`, cea mai nouă."} {
		if !strings.Contains(input, want) {
			t.Errorf("the input section lacks «%s»:\n%s", want, input)
		}
	}
}

// TestInputs_RenumberedTextNamesTheGateThatJudges (Task 12b review): the
// renumbering names the gate that judges the run with the highest number
// by the set: G12 for each MI3 set, G13 for the Pi's MI4 set alone (the
// PC's is information only); for MI2 it says that G11 does not depend on
// the order, since it judges the worst run.
func TestInputs_RenumberedTextNamesTheGateThatJudges(t *testing.T) {
	for _, c := range []struct {
		code, set  string
		untimed    bool
		has, lacks []string
	}{
		{"MI4", "pi-mi4", false, []string{"G13 judecă rularea cu numărul cel mai mare, `pi-mi4-2`, cea mai nouă."}, nil},
		{"MI4", "pc-mi4", false, nil, []string{"judecă"}},
		{"MI4", "pc-mi4", true, []string{"nesigur"}, []string{"judecă", "verificați"}},
		{"MI3", "pc-mi3", true, []string{"G12 judecă rularea cu numărul cel mai mare, `pc-mi3-2`", "verificați"}, nil},
		{"MI2", "pi-72h", true, []string{"nesigur", "G11 nu depinde de ordine"}, []string{"verificați"}},
		{"MI2", "pi-72h", false, []string{"după momentul lor", "G11 nu depinde de ordine"}, nil},
	} {
		n := renumbering{code: c.code, set: c.set, from: []string{c.set + "-1", c.set + "-1"}, to: []string{c.set + "-1", c.set + "-2"}}
		if c.untimed {
			n.untimed = []string{c.set + "-2"}
		}
		text := renumberedText(n)
		for _, want := range c.has {
			if !strings.Contains(text, want) {
				t.Errorf("%s %s (untimed %v): %q lacks «%s»", c.code, c.set, c.untimed, text, want)
			}
		}
		for _, bad := range c.lacks {
			if strings.Contains(text, bad) {
				t.Errorf("%s %s (untimed %v): %q holds «%s»", c.code, c.set, c.untimed, text, bad)
			}
		}
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

// TestInputs_ConflictNamesWhatG10Counts (Task 12b review): the report
// names the copies of a conflicting set that G10 counts by G10's own
// selection (mi1.CountedRuns), run by run: of two copies of an envelope
// whose runs count in different copies, each run counts from the copy
// where it counts.
func TestInputs_ConflictNamesWhatG10Counts(t *testing.T) {
	run := func(id string, counts bool) results.Run {
		r := results.Run{RunID: id, Attempt: 1, Valid: counts, ShutdownKind: "power", PacingP99Ms: 6}
		if !counts {
			r.InvalidReason = "oprire în afara ferestrei"
		}
		return r
	}
	a := envelope("MI1", mi1.SetRealPC, "real-pc-99", "pc")
	b := a
	a.Runs = []results.Run{run("real-pc-98", true), run("real-pc-99", false)}
	b.Runs = []results.Run{run("real-pc-98", false), run("real-pc-99", true)}
	in, err := load([]string{(&fixture{envs: []results.Envelope{a}}).folder(t), (&fixture{envs: []results.Envelope{b}}).folder(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []conflict{{set: mi1.SetRealPC, runs: 2, counted: []originCount{{origin{folder: 1}, 1}, {origin{folder: 2}, 1}}}}
	if fmt.Sprint(in.conflicts) != fmt.Sprint(want) {
		t.Errorf("conflicts %+v, want %+v: a run from each folder, as G10 counts them", in.conflicts, want)
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

// jobDone are when the fixture's jobs completed (testdata/annotations).
var jobDone = map[int64]time.Time{
	1001: time.Date(2026, 10, 26, 7, 44, 19, 0, time.UTC),
	1002: time.Date(2026, 10, 26, 8, 6, 38, 0, time.UTC),
	1003: time.Date(2026, 10, 26, 7, 51, 18, 0, time.UTC),
	2002: time.Date(2026, 10, 29, 20, 25, 40, 0, time.UTC),
}

// TestInputs_AnnotationEnvelopesCarryTheirJobsTime (Task 12 review, M1):
// an envelope that a CI run's annotations carry is stamped with the time
// its job completed, as the ENC-06 envelope is, so that one set read from
// two runs orders by time: the lab's runs, CI's MI9 legs and the Chrome
// line's envelope.
func TestInputs_AnnotationEnvelopesCarryTheirJobsTime(t *testing.T) {
	installFakeGH(t, annotationsDir)
	in, err := load(nil, []string{labRun, ciRun})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range in.envs {
		var want []time.Time
		switch e.Set {
		case "lab-linux-kill9":
			want = []time.Time{jobDone[1001], jobDone[1002]}
		case "lab-windows-kill9":
			want = []time.Time{jobDone[1003]}
		case "ci-relay", "ci-loopback", "ci-chrome":
			want = []time.Time{jobDone[2002]}
		default:
			continue
		}
		if !slices.ContainsFunc(want, func(at time.Time) bool { return e.AtMs == at.UnixMilli() }) {
			t.Errorf("%s %s: AtMs %d, want its job's completion, one of %v", e.Code, e.RunID, e.AtMs, want)
		}
	}
}

// relayNotice is CI's ci-relay-1 leg at mbps, as the mi9 job's notice.
func relayNotice(t *testing.T, mbps float64) results.Notice {
	t.Helper()
	e := mi9Envelope("lab-linux", "ci-relay", "ci-relay-1", results.Mi9Leg{Transport: "turns443", ThroughputMbps: mbps, SeekMs: seeks(20, 300),
		RTTMs: 50.5, Source: "ci-relay"})
	n := notice(t, e)
	n.Title = "mi9"
	return n
}

// TestInputs_CIRunsRenumberedByTheirTime (Task 12 review, M1): CI's MI9
// legs have the same run id in every CI run (ci-relay-1); read from two
// runs, in either order, they are renumbered by the time their jobs
// completed, the newer last, and the report does not call the order
// uncertain.
func TestInputs_CIRunsRenumberedByTheirTime(t *testing.T) {
	installFakeGH(t, ghRuns(t, map[string][]ghJob{
		"501": {{id: 5001, completed: time.Date(2026, 10, 27, 20, 0, 0, 0, time.UTC), notices: []results.Notice{relayNotice(t, 17.5)}}},
		"502": {{id: 5002, completed: time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC), notices: []results.Notice{relayNotice(t, 15.2)}}},
	}))
	for _, order := range [][]string{{"501", "502"}, {"502", "501"}} {
		t.Run(strings.Join(order, " then "), func(t *testing.T) {
			in, err := load(nil, order)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]float64{}
			for _, e := range in.envs {
				if e.Set == "ci-relay" {
					got[e.RunID] = e.MI9.Legs[0].ThroughputMbps
				}
			}
			if got["ci-relay-1"] != 17.5 || got["ci-relay-2"] != 15.2 || len(got) != 2 {
				t.Errorf("ci-relay legs %v, want the older run's 17.5 as ci-relay-1 and the newer's 15.2 as ci-relay-2", got)
			}
			if input := section(t, newReport(in, goldenDate).markdown(), "## Intrarea raportului"); strings.Contains(input, "nesigur") ||
				!strings.Contains(input, "`MI9 ci-relay`") {
				t.Errorf("the input section, want the renumbering by time without an uncertain order:\n%s", input)
			}
		})
	}
}

// TestInputs_RenumberWithoutTimesSaysTheOrderIsUncertain (Task 12 review,
// M1): envelopes without a time (AtMs 0, as from an older recspike) that
// share a run id are renumbered all the same, but the report says that
// their order is uncertain, names the run that G12 judges, the highest
// number, and asks for a check by hand.
func TestInputs_RenumberWithoutTimesSaysTheOrderIsUncertain(t *testing.T) {
	other := mi3Envelope("pi", "H1b", 4)
	other.MI3.CPUPctTotal = 50.1
	in, err := load([]string{passing(t).folder(t), (&fixture{envs: []results.Envelope{other}}).folder(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := runIDs(in.envs, "MI3"); !slices.Equal(got, []string{"pc-mi3-1", "pi-mi3-1", "pi-mi3-2"}) {
		t.Errorf("MI3 run ids %q, want the Pi's two renumbered", got)
	}
	input := section(t, newReport(in, goldenDate).markdown(), "## Intrarea raportului")
	for _, want := range []string{"`MI3 pi-mi3`", "nesigur", "G12 judecă rularea cu numărul cel mai mare, `pi-mi3-2`", "verificați"} {
		if !strings.Contains(input, want) {
			t.Errorf("the input section lacks «%s»:\n%s", want, input)
		}
	}
	if strings.Contains(input, "după momentul lor") {
		t.Errorf("the input section claims an order by time that the envelopes do not have:\n%s", input)
	}
}

// labNotices are lab-linux-kill9's 21 runs as a lab run's notices; with
// fail, its third run loses 2.5 s, a measured failure.
func labNotices(t *testing.T, fail bool) []results.Notice {
	t.Helper()
	envs := labEnvelopes(t, mi1.SetLabLinuxKill9, "lab-linux")
	if fail {
		r := &envs[2].Runs[0]
		r.Streams[0].LossMs = 2500
		r.LossMs = mi1.RunLossMs(*r)
	}
	return notices(t, envs)
}

// TestInputs_NewestLabRunCounts (Task 12 review, M2): with two lab runs,
// recreport reads the newer first, whatever the order of -annotations, so
// that G10 counts the newer run's result, here a measured failure; the
// report names the CI run that G10 counted.
func TestInputs_NewestLabRunCounts(t *testing.T) {
	installFakeGH(t, ghRuns(t, map[string][]ghJob{
		"601": {{id: 6001, completed: time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC), notices: labNotices(t, false)}},
		"602": {{id: 6002, completed: time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC), notices: labNotices(t, true)}},
	}))
	for _, order := range [][]string{{"601", "602"}, {"602", "601"}} {
		t.Run(strings.Join(order, " then "), func(t *testing.T) {
			in, err := load(nil, order)
			if err != nil {
				t.Fatal(err)
			}
			r := newReport(in, goldenDate)
			if j := r.g10(); j.Verdict != results.Fail {
				t.Errorf("G10 = %q, reasons %q; want the newer run's measured failure", j.Verdict, j.Reasons)
			}
			if len(in.conflicts) != 1 || in.conflicts[0].set != mi1.SetLabLinuxKill9 {
				t.Errorf("conflicts %+v, want lab-linux-kill9", in.conflicts)
			}
			if input := section(t, r.markdown(), "## Intrarea raportului"); !strings.Contains(input, "rularea CI `602`") ||
				strings.Contains(input, "`601`") {
				t.Errorf("the input section does not name the CI run that G10 counted, 602, alone:\n%s", input)
			}
		})
	}
}

// TestInputs_SmokeConflictsNotNamed (Task 12 review, M2): two recorder
// workflow runs carry different smoke runs under the same run ids; G10
// does not judge them, so they are counted apart, not named as runs that
// G10 counts one of.
func TestInputs_SmokeConflictsNotNamed(t *testing.T) {
	smoke := func(lossMs int64) []results.Notice {
		envs := labEnvelopes(t, mi1.SetLabLinuxKill9, "lab-linux")[:2]
		for i := range envs {
			e := &envs[i]
			e.Set, e.RunID = "lab-linux-kill9-smoke", strings.Replace(e.RunID, "kill9", "kill9-smoke", 1)
			e.Runs[0].RunID, e.Runs[0].Streams[0].LossMs = e.RunID, lossMs
			e.Runs[0].LossMs = mi1.RunLossMs(e.Runs[0])
		}
		return notices(t, envs)
	}
	installFakeGH(t, ghRuns(t, map[string][]ghJob{
		"701": {{id: 7001, completed: time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC), notices: smoke(400)}},
		"702": {{id: 7002, completed: time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC), notices: smoke(700)}},
	}))
	in, err := load(nil, []string{"701", "702"})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.conflicts) != 0 {
		t.Errorf("conflicts %+v, want none: G10 does not judge a smoke set", in.conflicts)
	}
	if in.unjudged["lab-linux-kill9-smoke"] != 4 {
		t.Errorf("unjudged %v, want the 4 smoke envelopes", in.unjudged)
	}
}

// chromeLine is a Chrome line of the mi9 job with its keys reordered and
// spaced, as a change of mi9.spec.ts could print it.
var chromeLine = `{ "seekMs": [21.7, 31.3, 78.3, 25, 25.1, 7.9, 18.2, 30.8, 24.8, 31.7, 14.1, 44.2, 41.6, 13.1, 4.8, 15.3, 35.8, 22.8, 17.3, 36.6],
  "displayMs": [115.6, 39.1, 115.6, 65.6, 49.1, 32.3, 65.7, 65.7, 101.5, 66.7, 49.3, 82.6, 82.4, 49, 15.7, 49.1, 82.6, 65.4, 65.6, 82.3],
  "transport" : "chrome" }`

// TestInputs_ChromeLineByItsTransport (Task 12 review, M3): an mi9 notice
// that is no envelope is told by its transport: "chrome" decodes, strictly,
// into the Chrome line's envelope whatever the order of its keys and its
// spaces; any other shape is an error, as for ENC-06 and mtx2-loss, never
// an information notice that leaves G18 without its Chrome part.
func TestInputs_ChromeLineByItsTransport(t *testing.T) {
	at := time.Date(2026, 10, 29, 20, 0, 0, 0, time.UTC)
	line := func(id int64, message string) []ghJob {
		return []ghJob{{id: id, completed: at, notices: []results.Notice{{Title: "mi9", Message: message}}}}
	}
	installFakeGH(t, ghRuns(t, map[string][]ghJob{
		"801": line(8001, chromeLine),
		"802": line(8002, `{"transport":"firefox","displayMs":[1],"seekMs":[1]}`),
		"803": line(8003, `{"transport":"chrome","displayMs":[1],"seekMs":[1],"fps":30}`),
		"804": line(8004, "mi9: gata"),
	}))
	in, err := load(nil, []string{"801"})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(in.envs, func(e results.Envelope) bool { return e.Set == "ci-chrome" })
	if i < 0 || len(in.envs[i].MI9.Legs[0].DisplayMs) != 20 || len(in.envs[i].MI9.Legs[0].SeekMs) != 20 {
		t.Errorf("envelopes %+v, want the Chrome line's with 20 frames shown and 20 seeks", in.envs)
	}
	if len(in.ci.other) != 0 {
		t.Errorf("other notices %v, want none: the Chrome line is no information notice", in.ci.other)
	}
	for _, id := range []string{"802", "803", "804"} {
		if _, err := load(nil, []string{id}); err == nil {
			t.Errorf("run %s: an mi9 line of an unknown shape was read without an error", id)
		}
	}
}

// TestInputs_JobWithoutCompletionHasNoTime (Task 12 review, M4): a job
// whose completed_at is null, as one still running, gives its envelopes no
// time (AtMs 0), never the zero time's -62135596800000.
func TestInputs_JobWithoutCompletionHasNoTime(t *testing.T) {
	enc06 := results.Notice{Title: "ENC-06", Message: `{"code":"ENC-06","tools":["age v1.3.2","rage 0.12.1"],"files":7,"pass":true}`}
	lab := notice(t, labEnvelopes(t, mi1.SetLabLinuxKill9, "lab-linux")[0])
	installFakeGH(t, ghRuns(t, map[string][]ghJob{"901": {{id: 9001, notices: []results.Notice{enc06, lab}}}}))
	in, err := load(nil, []string{"901"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range in.envs {
		if e.AtMs != 0 {
			t.Errorf("%s %s: AtMs %d, want 0 without the job's completion", e.Code, e.RunID, e.AtMs)
		}
	}
	if len(in.envs) != 2 {
		t.Errorf("envelopes %+v, want the ENC-06 check and the lab run", in.envs)
	}
}

// TestInputs_UnreadAppFilesListed (Task 12 review, M5): an app result that
// is no JSON, and an AGE result whose name lacks the time that reccontroller
// stored it at, are not read: the report lists them under «Fișiere
// necitite», each with why in Romanian, so that a truncated AGE result does
// not read as no AGE run.
func TestInputs_UnreadAppFilesListed(t *testing.T) {
	f := passing(t)
	const truncated, misnamed = "android-samsung-sm-a245f-20261030-123000-age.json", "android-age.json"
	f.app = append(f.app, appResult{name: truncated, data: []byte(`{"scenario":"AGE","mbps":3`)},
		appResult{name: misnamed, data: ageFile(t, "android", time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), 5).data})
	in, err := load([]string{f.folder(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, s := range in.skipped {
		files = append(files, s.file)
	}
	slices.Sort(files)
	if want := []string{"app/" + misnamed, "app/" + truncated}; !slices.Equal(files, want) {
		t.Errorf("files not read %q, want %q", files, want)
	}
	if in.ageRuns != 1 {
		t.Errorf("AGE runs %d, want the fixture's one: a file not read is no run", in.ageRuns)
	}
	input := section(t, newReport(in, goldenDate).markdown(), "## Intrarea raportului")
	for _, want := range []string{"Fișiere necitite", "`app/" + truncated + "` (rezultat al aplicației care nu e JSON valid)",
		"`app/" + misnamed + "` (rezultat AGE fără ora salvării în nume)"} {
		if !strings.Contains(input, want) {
			t.Errorf("the input section lacks «%s»:\n%s", want, input)
		}
	}
}
