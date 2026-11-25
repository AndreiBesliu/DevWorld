package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// TestInputs_DedupKeepsLatestAtMs: of two envelopes of one run id the
// later AtMs stays; with equal AtMs the larger file; the dropped copy is
// named; a mi5-lab notice takes its job's completed_at, 0 when null.
func TestInputs_DedupKeepsLatestAtMs(t *testing.T) {
	early, late := mi6Env(), mi6Env()
	early.AtMs -= 1000
	early.MI6.Pairs[0].Refresh = early.MI6.Pairs[0].Refresh[:6]
	a, b := t.TempDir(), t.TempDir()
	writeEnv(t, a, late)
	writeEnv(t, b, early)

	small, large := mi8Env(), mi8Env()
	small.MI8.Cameras = small.MI8.Cameras[:1]
	writeEnv(t, a, small)
	writeEnv(t, b, large)

	gh := t.TempDir()
	lab := labEnv()
	other := labEnv()
	other.RunID, other.Runs[0].RunID = "mi5-20261119T090000Z", "mi5-20261119T090000Z"
	writeGHRun(t, gh, "201", []ghJob{{id: 7001, completed: labDone, pages: [][]ghAnnotation{{{"notice", titleMI5Lab, string(mustJSON(t, lab))}}}}})
	writeGHRun(t, gh, "202", []ghJob{{id: 7002, pages: [][]ghAnnotation{{{"notice", titleMI5Lab, string(mustJSON(t, lab))},
		{"notice", titleMI5Lab, string(mustJSON(t, other))}}}}})
	installFakeGH(t, gh)

	in, err := load([]string{a, b}, []string{"202", "201"})
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]results.Envelope{}
	for _, e := range in.envs {
		kept[e.RunID] = e
	}
	if len(in.envs) != 4 {
		t.Fatalf("%d envelopes kept, want 4", len(in.envs))
	}
	if got := kept[late.RunID]; got.AtMs != late.AtMs || len(got.MI6.Pairs[0].Refresh) != 7 {
		t.Errorf("MI6: kept the earlier copy")
	}
	if got := kept[large.RunID]; len(got.MI8.Cameras) != 2 {
		t.Errorf("MI8: kept the smaller file")
	}
	if got := kept[lab.RunID]; got.AtMs != ms(labDone) {
		t.Errorf("lab: AtMs %d, want the job's completed_at %d", got.AtMs, ms(labDone))
	}
	if got := kept[other.RunID]; got.AtMs != 0 {
		t.Errorf("lab without completed_at: AtMs %d, want 0", got.AtMs)
	}
	wantDropped := []dropped{
		{from: "-in #1: MI8/pc/" + displayRunID(small.RunID) + ".json", kept: "-in #2: MI8/pc/" + displayRunID(large.RunID) + ".json"},
		{from: "-in #2: MI6/pc/" + displayRunID(early.RunID) + ".json", kept: "-in #1: MI6/pc/" + displayRunID(late.RunID) + ".json"},
		{from: "rulare CI 202, job 7002", kept: "rulare CI 201, job 7001"},
	}
	if !reflect.DeepEqual(sortedDropped(in.dropped), sortedDropped(wantDropped)) {
		t.Errorf("dropped %+v\nwant %+v", in.dropped, wantDropped)
	}
	md := newReport(in, defaultOpts()).markdown()
	for _, d := range wantDropped {
		mustContain(t, md, d.from)
	}
}

// sortedDropped is ds in the order of their names.
func sortedDropped(ds []dropped) []dropped {
	out := append([]dropped(nil), ds...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j].from < out[i].from {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// TestInputs_DedupByRunID: an envelope read twice, from two folders, or
// from a folder and a notice, is judged once.
func TestInputs_DedupByRunID(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	e := mi5Env("pc", "ssd")
	writeEnv(t, a, e)
	writeEnv(t, b, e)
	lab := labEnv()
	lab.AtMs = 0
	writeEnv(t, a, lab)
	gh := t.TempDir()
	writeGHRun(t, gh, "301", []ghJob{{id: 7301, completed: labDone, pages: [][]ghAnnotation{{{"notice", titleMI5Lab, string(mustJSON(t, lab))}}}}})
	installFakeGH(t, gh)
	in, err := load([]string{a, b}, []string{"301"})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.envs) != 2 || len(in.dropped) != 2 {
		t.Fatalf("%d kept, %d dropped; want 2 and 2", len(in.envs), len(in.dropped))
	}
	for _, d := range in.dropped {
		if strings.HasPrefix(d.from, "rulare CI") {
			t.Errorf("dropped the notice's copy, which is newer: %+v", d)
		}
	}
}

// TestInputs_UnreadableFilesListed: an unparsable file and an invalid
// envelope are listed by name and reason, and the render goes on; an app
// result under app/ is counted and named, never listed as unreadable.
func TestInputs_UnreadableFilesListed(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, mi5Env("pc", "ssd"))
	writeFile(t, filepath.Join(dir, "MI5", "pc", "broken.json"), []byte("{not json"))
	bad := mi6Env()
	bad.Host = "laptop"
	writeEnv(t, dir, bad)
	writeFile(t, filepath.Join(dir, "app", "android-sm-a546b-20261118-101500-oauth.json"), []byte(`{"scenario":"OAUTH"}`))
	in, err := load([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []skipped{
		{file: "-in #1: MI5/pc/broken.json", reason: reasonNotJSON},
		{file: "-in #1: MI6/pc/" + displayRunID(bad.RunID) + ".json", reason: reasonNotEnvelope},
	}
	if !reflect.DeepEqual(in.unreadable, want) {
		t.Errorf("unreadable %+v\nwant %+v", in.unreadable, want)
	}
	if !reflect.DeepEqual(in.appResults, []string{"android-sm-a546b-20261118-101500-oauth.json"}) {
		t.Errorf("app results %v", in.appResults)
	}
	out := t.TempDir()
	code, _, stderr := runCmd("-in", dir, "-onedrive", oneDriveFree, "-out", out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	md, err := os.ReadFile(filepath.Join(out, "2026-11-20-phase0c-upload-results.md")) //nolint:gosec // the test's output
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, string(md), "Rezultatele aplicației: 1 (android-sm-a546b-20261118-101500-oauth.json)",
		"-in #1: MI5/pc/broken.json: "+reasonNotJSON, "-in #1: MI6/pc/"+displayRunID(bad.RunID)+".json: "+reasonNotEnvelope)
	mustContain(t, stderr, "broken.json")
	if strings.Contains(stderr, "laptop") || strings.Contains(string(md), "laptop") {
		t.Error("the invalid value is repeated")
	}
}

// escape is annotate.sh's escaping of a line; unescape the runner's,
// which the annotations API shows.
func escape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "%", "%25"), "\n", "%0A")
}

func unescape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "%0A", "\n"), "%25", "%")
}

// TestInputs_ReadsLabAnnotations: the jobs and the annotations come in two
// pages each; a mi5-lab notice whose message is annotate emit's line,
// escaped by annotate.sh and unescaped by the runner, reads back as the
// same lab-linux envelope; other notices and failures are no envelopes.
func TestInputs_ReadsLabAnnotations(t *testing.T) {
	lab := labEnv()
	lab.Runs[0].InvalidReason = "100 % din segmente" // a % that annotate.sh escapes
	line, err := json.Marshal(lab)
	if err != nil {
		t.Fatal(err)
	}
	message := unescape(escape(string(line)))
	if escape(string(line)) == string(line) {
		t.Fatal("the line has nothing to escape")
	}
	gh := t.TempDir()
	writeGHRun(t, gh, "401",
		[]ghJob{{id: 8001, completed: labDone.Add(-time.Hour), pages: [][]ghAnnotation{{{"notice", "test-times", "ok 1s"}}}}},
		[]ghJob{{id: 8002, completed: labDone, pages: [][]ghAnnotation{
			{{"failure", "mi5", "boom"}},
			{{"notice", titleMI5Lab, message}},
		}}})
	installFakeGH(t, gh)
	in, err := load(nil, []string{"401"})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.envs) != 1 {
		t.Fatalf("%d envelopes, want 1", len(in.envs))
	}
	want := lab
	want.AtMs = ms(labDone)
	if got := in.envs[0]; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if in.ciRuns != 1 || len(in.unreadable) != 0 {
		t.Errorf("runs %d, unreadable %+v", in.ciRuns, in.unreadable)
	}
	// A mi5-lab notice that holds no lab-linux MI5 envelope is listed.
	pc := mi5Env("pc", "ssd")
	writeGHRun(t, gh, "402", []ghJob{{id: 8003, completed: labDone, pages: [][]ghAnnotation{{{"notice", titleMI5Lab, string(mustJSON(t, pc))},
		{"notice", titleMI5Lab, "{cut"}}}}})
	in, err = load(nil, []string{"402"})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.envs) != 0 || len(in.unreadable) != 2 {
		t.Errorf("envs %d, unreadable %+v", len(in.envs), in.unreadable)
	}
}
