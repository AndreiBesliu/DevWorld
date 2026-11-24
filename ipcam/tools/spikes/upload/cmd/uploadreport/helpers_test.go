package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi5"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// update rewrites the fixtures under testdata/ and golden.md.
var update = flag.Bool("update", false, "rewrite testdata/ (fixtures and golden.md)")

// fakeGHEnv names the folder that the test binary, run as gh, answers from.
const fakeGHEnv = "UPLOADREPORT_FAKE_GH"

// TestMain lets the test binary stand in for gh (installFakeGH).
func TestMain(m *testing.M) {
	if dir := os.Getenv(fakeGHEnv); dir != "" {
		os.Exit(fakeGH(dir, os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// fakeGH answers `gh api repos/AndreiBesliu/DevWorld/<path> --paginate`
// with the file <dir>/<path>.json, the pages as gh prints them.
func fakeGH(dir string, args []string, stdout, stderr io.Writer) int {
	if len(args) != 3 || args[0] != "api" || args[2] != "--paginate" || !strings.HasPrefix(args[1], repo) {
		_, _ = fmt.Fprintf(stderr, "fake gh: unexpected arguments %q\n", args)
		return 2
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(args[1], repo))+".json")) //nolint:gosec // the test's fixture
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh: Not Found (HTTP 404)")
		return 1
	}
	_, _ = stdout.Write(data)
	return 0
}

// installFakeGH puts a gh on PATH that answers from dir (fakeGH).
func installFakeGH(t *testing.T, dir string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	name := "gh"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	data, err := os.ReadFile(exe) //nolint:gosec // the test binary
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, name), data, 0o700); err != nil { //nolint:gosec // an executable for the test
		t.Fatal(err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeGHEnv, abs)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// ghJob is a job of a workflow run that the fake gh answers: its id, when
// it completed (zero: null) and its annotations' pages.
type ghJob struct {
	id        int64
	completed time.Time
	pages     [][]ghAnnotation
}

// ghAnnotation is a check-run annotation.
type ghAnnotation struct{ level, title, message string }

// writeGHRun writes workflow run id's jobs, in jobPages pages, into dir,
// which the fake gh answers from.
func writeGHRun(t *testing.T, dir, id string, jobPages ...[]ghJob) {
	t.Helper()
	var jobsOut bytes.Buffer
	for _, page := range jobPages {
		list := []map[string]any{}
		for _, j := range page {
			var completed any
			if !j.completed.IsZero() {
				completed = j.completed.Format(time.RFC3339)
			}
			list = append(list, map[string]any{"id": j.id, "name": fmt.Sprintf("job %d", j.id), "status": "completed",
				"conclusion": "success", "completed_at": completed})
			var ann bytes.Buffer
			for _, p := range j.pages {
				as := []map[string]any{}
				for _, a := range p {
					as = append(as, map[string]any{"path": ".github", "start_line": 1, "end_line": 1, "annotation_level": a.level,
						"title": a.title, "message": a.message, "raw_details": nil})
				}
				ann.Write(mustJSON(t, as))
				ann.WriteByte('\n')
			}
			if len(j.pages) == 0 {
				ann.WriteString("[]\n")
			}
			writeFile(t, filepath.Join(dir, "check-runs", fmt.Sprint(j.id), "annotations.json"), ann.Bytes())
		}
		jobsOut.Write(mustJSON(t, map[string]any{"total_count": len(list), "jobs": list}))
		jobsOut.WriteByte('\n')
	}
	writeFile(t, filepath.Join(dir, "actions", "runs", id, "jobs.json"), jobsOut.Bytes())
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// writeFile writes data to path, making its folder.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil { //nolint:gosec // a path under the test's folders
		t.Fatal(err)
	}
}

// writeEnv writes e under dir as uploadspike stores it:
// <dir>/<code>/<set>/<runId>.json, keeping its AtMs.
func writeEnv(t *testing.T, dir string, e results.Envelope) string {
	t.Helper()
	path := filepath.Join(dir, e.Code, e.Set, e.RunID+".json")
	writeFile(t, path, append(mustJSON(t, e), '\n'))
	return path
}

// The fixtures' clock: the MI5 run of the PC ends on 20 November 2026.
var (
	day0   = time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	mi5End = time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC)
)

func ms(t time.Time) int64 { return t.UnixMilli() }

// mi5Env is an owner's MI5 envelope of host on storage that passes G14:
// 6 h blocked, every segment verified, a resumed restart, every open read
// ok, and the OneDrive destination deferred.
func mi5Env(host, storage string) results.Envelope {
	start := mi5End.Add(-8 * time.Hour)
	runID := "mi5-" + start.Format("20060102T150405Z")
	goos, cams, segs := "windows", 4, 288
	if host == "pi" {
		goos, cams, segs = "linux", 2, 144
		runID = "mi5-" + start.Add(-24*time.Hour).Format("20060102T150405Z")
	}
	e := results.Envelope{Schema: results.Schema, Code: "MI5", RunID: runID, Set: host, Host: host, OS: goos, Storage: storage,
		Sealer: "software", Account: "acct-1", AccountKind: "test", Cameras: cams, AtMs: ms(mi5End),
		Runs: []results.Run{{RunID: runID, StartMs: ms(start), EndMs: ms(mi5End), Valid: true}},
		MI5: &results.Mi5Data{BlockSecs: 21600, RecoverySecs: 1260, UplinkMbps: 18.5, DNSResolvedDuringBlock: true,
			Reach: []results.ReachCheck{{AtMs: ms(start.Add(time.Hour)), ProviderBlocked: true, Lan: true, Loopback: true}},
			Dests: []results.Mi5Dest{
				{Dest: "dst-1", Kind: mi5.KindDrive, Account: "acct-1", AccountKind: "test", Cameras: cams, Segments: segs,
					SegmentsDuringBlock: segs * 3 / 4, Verified: segs, FailedDuringBlock: 40, UnreachableDuringBlock: true, AuditFrom: "list-remote",
					CallsPerSegment: map[string]float64{"files.create.resumable": 1, "upload.put": 3, "files.get": 2}},
				{Dest: "dst-od", Kind: mi5.KindOneDrive, Account: "acct-1", Deferred: mi5.MsgOneDriveDeferred},
			},
			Restart: &results.Mi5Restart{KilledAtMs: ms(start.Add(7 * time.Hour)), Size: 32 << 20, LastConfirmedOffset: 16 << 20,
				ResumedOffset: 16 << 20, TornSegmentUploaded: true},
			ReadOpen: []results.ReadOpenRun{{Encrypted: true, Calls: 10, OK: 10, P50Ms: 42, MaxMs: 120},
				{Calls: 20, OK: 20, P50Ms: 30, MaxMs: 90, Retried: 1}}}}
	if host == "pc" {
		e.Sealer = "dpapi-machine"
	}
	if host == "pi" {
		e.PiModel = &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Revision: "d03115", Tier: "H1b", Source: "cpuinfo",
			Supported: true, Known: true, RecommendedMin: 2, RecommendedMax: 2}
		if storage == "sd" {
			e.SdConfirmation = &results.SdConfirmation{Text: "DA", AtMs: ms(start)}
		}
	}
	return e
}

// labEnv is the lab's MI5 envelope (TST-07's 15-min variant), as annotate
// emit prints it.
func labEnv() results.Envelope {
	start := time.Date(2026, 11, 19, 7, 0, 0, 0, time.UTC)
	runID := "mi5-" + start.Format("20060102T150405Z")
	return results.Envelope{Schema: results.Schema, Code: "MI5", RunID: runID, Set: "lab-linux", Host: "lab-linux", OS: "linux",
		Storage: "ssd", Sealer: "software", Account: "acct-1", AccountKind: "test", Cameras: 4, AtMs: ms(start.Add(40 * time.Minute)),
		Runs: []results.Run{{RunID: runID, StartMs: ms(start), EndMs: ms(start.Add(40 * time.Minute)), Valid: true}},
		MI5: &results.Mi5Data{BlockSecs: 900, RecoverySecs: 95, UplinkMbps: 240, Lab: true,
			Dests: []results.Mi5Dest{{Dest: "dst-1", Kind: mi5.KindDrive, Account: "acct-1", Cameras: 4, Segments: 16,
				SegmentsDuringBlock: 12, Verified: 16, FailedDuringBlock: 6, UnreachableDuringBlock: true,
				CallsPerSegment: map[string]float64{"files.create.resumable": 1, "upload.put": 1, "files.get": 1}}},
			Restart:  &results.Mi5Restart{Size: 4 << 20, LastConfirmedOffset: 2 << 20, ResumedOffset: 2 << 20},
			ReadOpen: []results.ReadOpenRun{{Calls: 5, OK: 5, P50Ms: 12, MaxMs: 30}}}}
}

// mi6Env is the PC's MI6 envelope: the three Drive pairs, Android and
// Windows under plan A and web under B1, days 1–6 ok and day 7 expired,
// and the visibility matrix, every judged cell listed and read.
func mi6Env() results.Envelope {
	runID := "mi6-" + day0.Format("20060102T150405Z")
	days := func() []results.RefreshDay {
		var out []results.RefreshDay
		for d := 1; d <= 6; d++ {
			out = append(out, results.RefreshDay{Day: d, Attempts: 1, Outcome: results.OutcomeOK})
		}
		return append(out, results.RefreshDay{Day: 7, Attempts: 1, Outcome: results.OutcomeExpectedExpiry})
	}
	var cells []results.VisibilityCell
	for _, c := range []string{"android", "desktop", "tv"} {
		for _, r := range []string{"web", "tv", "android", "desktop"} {
			cells = append(cells, results.VisibilityCell{Provider: "GOOGLE_DRIVE", Creator: c, Reader: r, Listed: true, Read: true, Self: c == r})
		}
	}
	end := day0.Add(7*24*time.Hour + time.Hour)
	return results.Envelope{Schema: results.Schema, Code: "MI6", RunID: runID, Set: "pc", Host: "pc", OS: "windows", Storage: "ssd",
		Sealer: "dpapi-machine", Account: "acct-1", AccountKind: "test", AtMs: ms(end),
		Runs: []results.Run{{RunID: runID, StartMs: ms(day0), EndMs: ms(end), Valid: true}},
		MI6: &results.Mi6Data{Pairs: []results.Mi6Pair{
			{Provider: "GOOGLE_DRIVE", Platform: "android", Dest: "g-android", Plan: "A", HandoffOK: true, IssuedAtMs: ms(day0), Refresh: days()},
			{Provider: "GOOGLE_DRIVE", Platform: "windows", Dest: "g-windows", Plan: "A", HandoffOK: true, IssuedAtMs: ms(day0), Refresh: days()},
			{Provider: "GOOGLE_DRIVE", Platform: "web", Dest: "g-web", Plan: "B1", HandoffOK: true, IssuedAtMs: ms(day0), Refresh: days(), Transient: 1},
		}, Visibility: cells, Handoffs: []results.Handoff{{Account: "acct-1", Client: "android", Count: 1}, {Account: "acct-1", Client: "desktop", Count: 1},
			{Account: "acct-1", Client: "tv", Count: 1}}}}
}

// mi8Camera is a camera with n episodes, matched of them matched.
func mi8Camera(name, brand, model string, n, matched int, start time.Time) results.Mi8Camera {
	c := results.Mi8Camera{Camera: name, Brand: brand, Model: model, Episodes: n, Matched: matched, RefLagMs: 600,
		ReferenceMethod: results.ReferenceSubstream}
	if n > 0 {
		c.Coverage = float64(matched) / float64(n)
	}
	for i := range n {
		s := start.Add(time.Duration(i) * 3 * time.Hour)
		c.EpisodeTimes = append(c.EpisodeTimes, results.EpisodeTime{StartMs: ms(s), EndMs: ms(s.Add(40 * time.Second)), Matched: i < matched})
	}
	return c
}

// mi8Env is the PC's MI8 envelope: 73 h, two brands, every camera over 95 %.
func mi8Env() results.Envelope {
	start := time.Date(2026, 11, 21, 6, 0, 0, 0, time.UTC)
	runID := "mi8-" + start.Format("20060102-150405")
	end := start.Add(73 * time.Hour)
	return results.Envelope{Schema: results.Schema, Code: "MI8", RunID: runID, Set: "pc", Host: "pc", OS: "windows", Storage: "ssd",
		AtMs: ms(end), Runs: []results.Run{{RunID: runID, StartMs: ms(start), EndMs: ms(end), Valid: true}},
		MI8: &results.Mi8Data{Hours: 73, QuietGapSecs: 10, Cameras: []results.Mi8Camera{
			mi8Camera("real-2", "Reolink", "RLC-510A", 22, 21, start),
			mi8Camera("real-1", "Hikvision", "DS-2CD2143G2-I", 24, 24, start.Add(time.Minute)),
		}}}
}

// read is e as read from from.
func readOf(e results.Envelope, from string) read {
	return read{env: e, from: from, size: int64(len(e.RunID))}
}

// inputsOf is the inputs of envs, as load assembles them.
func inputsOf(envs ...results.Envelope) *inputs {
	reads := make([]read, len(envs))
	for i, e := range envs {
		reads[i] = readOf(e, fmt.Sprintf("-in #1: %s/%s/%s.json", e.Code, e.Set, e.RunID))
	}
	return assemble(reads)
}

// defaultOpts are the options of most render tests.
func defaultOpts() options {
	return options{oneDrive: oneDriveUnknown, d3: d3All, date: "2026-11-24", consoleUnits: -1}
}

// renderOf renders envs with opts.
func renderOf(envs []results.Envelope, opts options) string {
	return newReport(inputsOf(envs...), opts).markdown()
}

// runCmd runs uploadreport with args and returns its exit code and output.
func runCmd(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// mustContain fails t for each of want that text lacks.
func mustContain(t *testing.T, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("missing %q", w)
		}
	}
}

// filesIn lists the files under dir, relative, slash-separated.
func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		out = append(out, filepath.ToSlash(rel))
		return err
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}
