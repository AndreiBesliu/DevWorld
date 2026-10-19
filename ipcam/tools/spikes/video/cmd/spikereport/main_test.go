package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// storedS1 is a result file as spikehub writes it: one line, keys sorted, with the hub's mdnsUnavailable.
const storedS1 = `{"device":{"model":"samsung SM-A245F","os":"Android 14 (API 34)","platform":"android","ua":"Mozilla/5.0 (Linux; Android 14; SM-A245F) Chrome/120.0.6099.230 Mobile","webview":"com.google.android.webview 120.0.6099.230"},` +
	`"durationS":61.5,"hubJitterMs":[1.5,22],"interrupted":false,"mdnsUnavailable":true,"notes":["sub: H264"],"samples":{"firstFrameMs":[310,420.5]},"scenario":"S1","startedAt":"2026-10-18T10:00:00.000Z"}` + "\n"

func TestLoadResults_ReadsTheHubEnvelope(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "android-samsung-sm-a245f-20261018-100000-s1.json", storedS1)
	results, skipped, err := loadResults(dir)
	if err != nil || len(skipped) != 0 || len(results) != 1 {
		t.Fatalf("results = %d, skipped = %+v, err = %v", len(results), skipped, err)
	}
	r := results[0]
	if r.Scenario != "S1" || r.Device.Platform != "android" || r.Device.Model != "samsung SM-A245F" || r.Device.OS != "Android 14 (API 34)" {
		t.Errorf("result = %+v", r)
	}
	if r.Device.WebView != "com.google.android.webview 120.0.6099.230" || r.StartedAt != "2026-10-18T10:00:00.000Z" || r.DurationS != 61.5 {
		t.Errorf("result = %+v", r)
	}
	if r.Interrupted || !r.MDNSUnavailable || len(r.HubJitterMs) != 2 || r.HubJitterMs[1] != 22 || len(r.Notes) != 1 {
		t.Errorf("result = %+v", r)
	}
	if got := r.Samples["firstFrameMs"]; len(got) != 2 || got[1] != 420.5 {
		t.Errorf("samples = %v", r.Samples)
	}
	if r.File != "android-samsung-sm-a245f-20261018-100000-s1.json" {
		t.Errorf("file = %q, want the base name", r.File)
	}
}

func TestLoadResults_SkipsWhatIsNotAResultAndSaysWhy(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "good-20261018-100000-s1.json", storedS1)
	write(t, dir, "broken-20261018-100001-s1.json", `{"device":`)
	write(t, dir, "array-20261018-100002-s1.json", `[1,2]`)
	write(t, dir, "unknown-scenario-20261018-100003-s99.json", `{"scenario":"S99","device":{"platform":"android"},"samples":{}}`)
	write(t, dir, "no-platform-20261018-100004-s1.json", `{"scenario":"S1","device":{},"samples":{}}`)
	write(t, dir, "label-20261018-100005-s1.json", `{"scenario":"S1","device":"Pixel 8","samples":{}}`)
	write(t, dir, "notes.txt", "not a result")
	if err := os.Mkdir(filepath.Join(dir, "sub.json"), 0o750); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "huge-20261018-100006-s1.json", `{"scenario":"S1","device":{"platform":"android"},"pad":"`+strings.Repeat("a", 9<<20)+`"}`)

	results, skipped, err := loadResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].File != "good-20261018-100000-s1.json" {
		t.Fatalf("results = %+v", results)
	}
	names := map[string]string{}
	for _, s := range skipped {
		names[s.File] = s.Reason
	}
	for _, name := range []string{"broken-20261018-100001-s1.json", "array-20261018-100002-s1.json", "unknown-scenario-20261018-100003-s99.json",
		"no-platform-20261018-100004-s1.json", "label-20261018-100005-s1.json", "huge-20261018-100006-s1.json"} {
		if names[name] == "" {
			t.Errorf("%s is not listed as skipped with a reason (skipped: %v)", name, names)
		}
	}
	if len(skipped) != 6 {
		t.Errorf("skipped %d files, want 6 (not notes.txt, not the folder): %v", len(skipped), names)
	}
	for i := 1; i < len(skipped); i++ {
		if skipped[i-1].File > skipped[i].File {
			t.Errorf("skipped files are not sorted: %v", names)
		}
	}
}

func TestLoadResults_MissingFolder(t *testing.T) {
	if _, _, err := loadResults(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing folder is not an error")
	}
}

func runMain(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRun_WritesTheReportAndTheCSVs(t *testing.T) {
	out := t.TempDir()
	code, stdout, stderr := runMain(t, "-in", filepath.Join("testdata", "golden-set"), "-out", out, "-date", "2026-10-19")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	md := readText(t, filepath.Join(out, "2026-10-19-phase0b-results.md"))
	if !strings.HasPrefix(md, "# Rezultatele Fazei 0 (b)") {
		t.Errorf("report starts with %.60q", md)
	}
	csvs, err := filepath.Glob(filepath.Join(out, "phase0-data", "phase0b-*.csv"))
	if err != nil || len(csvs) < 3 {
		t.Fatalf("CSV files = %v (%v)", csvs, err)
	}
	for _, want := range []string{"2026-10-19-phase0b-results.md", "phase0b-android.csv"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q does not name %s", stdout, want)
		}
	}
	// The same input gives the same files.
	again := t.TempDir()
	if code, _, stderr := runMain(t, "-in", filepath.Join("testdata", "golden-set"), "-out", again, "-date", "2026-10-19"); code != 0 {
		t.Fatalf("second run: exit %d: %s", code, stderr)
	}
	if md2 := readText(t, filepath.Join(again, "2026-10-19-phase0b-results.md")); md != md2 {
		t.Errorf("a second run gave another report:\n%s", firstDifference(md2, md))
	}
}

func TestRun_TheDateIsTheNewestRunUnlessGiven(t *testing.T) {
	in := t.TempDir()
	write(t, in, "a-20261018-100000-s1.json", strings.Replace(storedS1, "2026-10-18T10:00:00.000Z", "2026-10-18T23:59:00.000Z", 1))
	write(t, in, "b-20261018-100001-s1.json", strings.Replace(storedS1, "2026-10-18T10:00:00.000Z", "2026-10-20T00:01:00.000Z", 1))
	out := t.TempDir()
	if code, _, stderr := runMain(t, "-in", in, "-out", out); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "2026-10-20-phase0b-results.md")); err != nil {
		t.Errorf("no report dated by the newest run: %v", err)
	}
	if code, _, stderr := runMain(t, "-in", in, "-out", out, "-date", "2026-11-02"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "2026-11-02-phase0b-results.md")); err != nil {
		t.Errorf("no report dated by -date: %v", err)
	}
}

func TestRun_NeedsAnInputWithResults(t *testing.T) {
	empty := t.TempDir()
	write(t, empty, "broken.json", `{"device":`)
	for name, args := range map[string][]string{
		"an empty folder":     {"-in", t.TempDir(), "-out", t.TempDir()},
		"only broken files":   {"-in", empty, "-out", t.TempDir()},
		"a missing folder":    {"-in", filepath.Join(t.TempDir(), "nope"), "-out", t.TempDir()},
		"a date that is none": {"-in", filepath.Join("testdata", "golden-set"), "-out", t.TempDir(), "-date", "tomorrow"},
	} {
		t.Run(name, func(t *testing.T) {
			code, _, stderr := runMain(t, args...)
			if code == 0 || stderr == "" {
				t.Errorf("exit %d, stderr %q: want a failure with a message", code, stderr)
			}
		})
	}
	t.Run("nothing is written when there is nothing to report", func(t *testing.T) {
		out := t.TempDir()
		runMain(t, "-in", empty, "-out", out)
		if entries, _ := os.ReadDir(out); len(entries) != 0 {
			t.Errorf("wrote %d entries into the output folder", len(entries))
		}
	})
	t.Run("usage errors exit 2", func(t *testing.T) {
		if code, _, _ := runMain(t, "-nope"); code != 2 {
			t.Errorf("exit %d for an unknown flag, want 2", code)
		}
		if code, _, _ := runMain(t, "stray"); code != 2 {
			t.Errorf("exit %d for a stray argument, want 2", code)
		}
	})
}

// Writing into a folder that an earlier run used: a CSV of a device that the input no
// longer has would disagree with the report's list of raw data.
func TestRun_RemovesCSVsThatTheInputNoLongerProduces(t *testing.T) {
	out := t.TempDir()
	data := filepath.Join(out, "phase0-data")
	if err := os.MkdirAll(filepath.Join(data, "phase0b-folder.csv"), 0o750); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(data, "phase0b-oldphone.csv")
	write(t, data, "phase0b-oldphone.csv", "device,scenario,run,status,key,index,value\n")
	keep := []string{"notes.csv", "phase0b-readme.txt", "other-phase0b-android.csv"}
	for _, name := range keep {
		write(t, data, name, "not ours")
	}
	write(t, out, "phase0b-top.csv", "not in phase0-data")

	code, stdout, stderr := runMain(t, "-in", filepath.Join("testdata", "golden-set"), "-out", out, "-date", "2026-10-19")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the CSV of a device that is no longer in the input is still there (%v)", err)
	}
	if !strings.Contains(stdout, "removed") || !strings.Contains(stdout, "phase0b-oldphone.csv") {
		t.Errorf("stdout %q does not say that the stale CSV was removed", stdout)
	}
	for _, name := range keep {
		if _, err := os.Stat(filepath.Join(data, name)); err != nil {
			t.Errorf("%s was removed, but it is not a CSV of this tool: %v", name, err)
		}
	}
	if info, err := os.Stat(filepath.Join(data, "phase0b-folder.csv")); err != nil || !info.IsDir() {
		t.Errorf("a folder with a CSV name was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "phase0b-top.csv")); err != nil {
		t.Errorf("a file outside phase0-data was removed: %v", err)
	}
	for _, name := range []string{"phase0b-android.csv", "phase0b-windows-tauri.csv", "phase0b-chrome.csv"} {
		if _, err := os.Stat(filepath.Join(data, name)); err != nil {
			t.Errorf("the CSV of the input, %s, is missing: %v", name, err)
		}
	}
	// A second run has nothing left to remove.
	_, again, _ := runMain(t, "-in", filepath.Join("testdata", "golden-set"), "-out", out, "-date", "2026-10-19")
	if strings.Contains(again, "removed") {
		t.Errorf("the second run removed something: %q", again)
	}
}

func TestRun_KeepsTheOldCSVsWhenThereIsNothingToReport(t *testing.T) {
	out := t.TempDir()
	data := filepath.Join(out, "phase0-data")
	if err := os.MkdirAll(data, 0o750); err != nil {
		t.Fatal(err)
	}
	write(t, data, "phase0b-android.csv", "an earlier run")
	if code, _, _ := runMain(t, "-in", t.TempDir(), "-out", out); code == 0 {
		t.Fatal("an empty input is not an error")
	}
	if got := readText(t, filepath.Join(data, "phase0b-android.csv")); got != "an earlier run" {
		t.Errorf("a run without results changed the earlier CSV: %q", got)
	}
}

func TestRun_ReportsSkippedFiles(t *testing.T) {
	in := t.TempDir()
	write(t, in, "good-20261018-100000-s1.json", storedS1)
	write(t, in, "broken-20261018-100001-s1.json", `{"device":`)
	out := t.TempDir()
	code, _, stderr := runMain(t, "-in", in, "-out", out, "-date", "2026-10-19")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stderr, "broken-20261018-100001-s1.json") {
		t.Errorf("stderr %q does not name the skipped file", stderr)
	}
	md := readText(t, filepath.Join(out, "2026-10-19-phase0b-results.md"))
	if !strings.Contains(md, "broken-20261018-100001-s1.json") || !strings.Contains(md, "## Fișiere necitite") {
		t.Errorf("the report does not list the skipped file")
	}
}

// testdata/real-chrome holds the files that spikehub stored for a run of the real app in
// Chrome for Testing 141 (headless, on Linux) against the real hub with 4 simulated cameras:
// S1, S2, S3-4, S7, S8, S9 and S10, as they came. It holds no address, URL, serial number or SSID.
func TestRun_RealChromeRun(t *testing.T) {
	dir := filepath.Join("testdata", "real-chrome")
	results, skipped, err := loadResults(dir)
	if err != nil || len(skipped) != 0 || len(results) != 7 {
		t.Fatalf("results = %d, skipped = %+v, err = %v", len(results), skipped, err)
	}
	mv := ComputeMV(results)
	for _, code := range []string{"MV1", "MV2", "MV3/4", "MV10", "MV13", "MV15", "G9"} {
		if got := outcome(mv, "chrome", code); got != "pass" {
			t.Errorf("chrome %s = %s, want pass (%+v)", code, got, mv[mvKey("chrome", code)])
		}
	}
	if v := mv[mvKey("chrome", "MV1")]; v.Value < 1 || v.Value > 100 {
		t.Errorf("MV1 p90 = %v ms: a local run starts in tens of milliseconds", v.Value)
	}
	if got := AndroidVerdict(mv); got != VerdictIncomplete {
		t.Errorf("a Chrome-only run gave the phone the verdict %q", got)
	}
	flags := HubJitterFlags(results)
	if len(flags) != 1 || flags[0].Scenario != "S3-4" || flags[0].MaxMs != 56.6 {
		t.Errorf("flags = %+v, want the S3-4 run whose hub was busy (56.6 ms)", flags)
	}

	out := t.TempDir()
	if code, _, stderr := runMain(t, "-in", dir, "-out", out); code != 0 || stderr != "" {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	md := readText(t, filepath.Join(out, "2026-10-19-phase0b-results.md"))
	csv := readText(t, filepath.Join(out, "phase0-data", "phase0b-chrome.csv"))
	for name, text := range map[string]string{"report": md, "csv": csv} {
		for _, re := range []*regexp.Regexp{ipv4Literal, ipv6Literal} {
			if m := re.FindString(text); m != "" {
				t.Errorf("the %s holds the address-like text %q", name, m)
			}
		}
		for _, bad := range []string{"://", "@"} {
			if strings.Contains(strings.ToLower(text), bad) {
				t.Errorf("the %s holds %q", name, bad)
			}
		}
	}
	if !strings.Contains(csv, "chrome,S1,chrome-20261019-042329-s1.json,used,firstFrameMs,0,") {
		t.Errorf("the CSV has no S1 row:\n%.300s", csv)
	}
}
