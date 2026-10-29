package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi3"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// fakeGHEnv names the folder that the test binary, run as gh, answers from.
const fakeGHEnv = "RECREPORT_FAKE_GH"

// TestMain lets the test binary stand in for gh (installFakeGH).
func TestMain(m *testing.M) {
	if dir := os.Getenv(fakeGHEnv); dir != "" {
		os.Exit(fakeGH(dir, os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// fakeGH answers `gh api repos/AndreiBesliu/DevWorld/<path> --paginate`
// with the file <dir>/<path>.json, the pages as gh prints them, and logs
// each call in <dir>/../calls.log of the test's copy.
func fakeGH(dir string, args []string, stdout, stderr io.Writer) int {
	const repo = "repos/AndreiBesliu/DevWorld/"
	if len(args) != 3 || args[0] != "api" || args[2] != "--paginate" || !strings.HasPrefix(args[1], repo) {
		_, _ = fmt.Fprintf(stderr, "fake gh: unexpected arguments %q\n", args)
		return 2
	}
	if log := os.Getenv(fakeGHEnv + "_LOG"); log != "" {
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // the test's log
		if err == nil {
			_, _ = fmt.Fprintln(f, strings.Join(args, " "))
			_ = f.Close()
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(args[1], repo))+".json")) //nolint:gosec // the test's fixture
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "gh: Not Found (HTTP 404)")
		return 1
	}
	_, _ = stdout.Write(data)
	return 0
}

// installFakeGH puts a gh on PATH that answers from dir (fakeGH), and
// returns the file that logs its calls.
func installFakeGH(t *testing.T, dir string) string {
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
	log := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv(fakeGHEnv, abs)
	t.Setenv(fakeGHEnv+"_LOG", log)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// ghJob is a job of a workflow run that the fake gh answers: its id, when
// it completed (zero: null, as for a job still running) and its notices.
type ghJob struct {
	id        int64
	completed time.Time
	notices   []results.Notice
}

// ghRuns writes workflow runs, by their ids, into a new folder that the
// fake gh answers from (installFakeGH): each run's jobs, and each job's
// notices as its check run's annotations.
func ghRuns(t *testing.T, runs map[string][]ghJob) string {
	t.Helper()
	dir := t.TempDir()
	for id, jobs := range runs {
		list := []map[string]any{}
		for _, j := range jobs {
			var completed any
			if !j.completed.IsZero() {
				completed = j.completed.Format(time.RFC3339)
			}
			list = append(list, map[string]any{"id": j.id, "name": fmt.Sprintf("job %d", j.id), "status": "completed", "conclusion": "success",
				"completed_at": completed})
			annotations := []map[string]any{}
			for _, n := range j.notices {
				annotations = append(annotations, map[string]any{"path": ".github", "start_line": 1, "end_line": 1, "annotation_level": "notice",
					"title": n.Title, "message": n.Message, "raw_details": nil})
			}
			writeJSON(t, filepath.Join(dir, "check-runs", fmt.Sprint(j.id), "annotations.json"), annotations)
		}
		writeJSON(t, filepath.Join(dir, "actions", "runs", id, "jobs.json"), map[string]any{"total_count": len(list), "jobs": list})
	}
	return dir
}

// writeJSON writes v as JSON to path, making its folder.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// notice is e as the notice that carries it in CI, titled with its set.
func notice(t *testing.T, e results.Envelope) results.Notice {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return results.Notice{Title: e.Set, Message: string(data)}
}

// notices are envs as their notices.
func notices(t *testing.T, envs []results.Envelope) []results.Notice {
	t.Helper()
	out := make([]results.Notice, len(envs))
	for i, e := range envs {
		out[i] = notice(t, e)
	}
	return out
}

// annotationsDir is the fixture of the CI runs: 101, the lab's, with the
// set lab-linux-kill9 whole and lab-windows-kill9 without its part 7, and
// 102, the recorder workflow's, with ENC-06, mtx2-loss and the MI9 lines.
const annotationsDir = "testdata/annotations"

// The CI runs of the fixture.
const (
	labRun    = "101"
	ciRun     = "102"
	enc06Job  = 2001
	chromeJob = 2002
)

// enc06Done is when the fixture's ENC-06 job (2001) completed.
var enc06Done = time.Date(2026, 10, 29, 20, 25, 35, 0, time.UTC)

// piModel is the owner's Pi in the fixtures: a Pi 4, H1b, 4 cameras.
func piModel() *results.PiModelInfo {
	return &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Revision: "d03115", Tier: "H1b", Supported: true, Known: true,
		RecommendedMin: 4, RecommendedMax: 4, Source: "cpuinfo"}
}

// envelope is an envelope of code on host in set.
func envelope(code, set, runID, host string) results.Envelope {
	e := results.Envelope{Schema: results.Schema, Code: code, RunID: runID, Set: set, Host: host, Storage: "ssd"}
	switch host {
	case "pc", "lab-windows":
		e.OS = "windows"
	default:
		e.OS = "linux"
	}
	if host == "pi" {
		e.PiModel = piModel()
	}
	return e
}

// mi1Runs are the counted runs of set as its plan lays them out, with
// cameras on a kill9 set of the owner's devices: each stream loses
// lossMs, but for one run of 1.5 s, and the O17-off run's cam99_main 1.8
// s; kill9 runs resume in 3 s and reboot runs record their cold boot.
func mi1Runs(t *testing.T, set string, cameras int, lossMs int64) []results.Run {
	t.Helper()
	plan, err := mi1.Plan(set, cameras)
	if err != nil {
		t.Fatal(err)
	}
	kind := map[string]string{mi1.SetLabLinuxKill9: "kill9", mi1.SetLabWindowsKill9: "kill9", mi1.SetLabLinuxQEMUReset: "qemu-reset",
		mi1.SetPCKill9: "kill9", mi1.SetPiKill9: "kill9", mi1.SetPCBugcheck: "bugcheck", mi1.SetPiSysrq: "sysrq",
		mi1.SetRealPC: "power", mi1.SetRealPi: "power"}[set]
	var runs []results.Run
	for _, p := range plan {
		r := results.Run{RunID: fmt.Sprintf("%s-%02d-1", set, p.Index), Valid: true, Attempt: 1, ShutdownKind: kind,
			Encrypted: p.Encrypted, PadLowBitrate: p.PadLowBitrate, ErrBoundMs: 150, Reconciled: true, Hub04Clear: true,
			PacingP99Ms: 5, Cameras: len(p.Streams), NtpSynced: true, WriteCache: "enabled"}
		if kind == "power" {
			r.Slot = p.Index
		}
		for _, s := range p.Streams {
			loss := lossMs
			switch {
			case !p.PadLowBitrate && s == mi1.LowBitrateStream:
				loss = 1800
			case p.Index == 4:
				loss = 1500
			}
			r.Streams = append(r.Streams, results.StreamLoss{Stream: s, BitrateKbps: mi1.BitrateKbps(s), Encrypted: p.Encrypted,
				PadLowBitrate: p.PadLowBitrate, LossMs: loss, LimitMs: mi1.StreamLimitMs(mi1.BitrateKbps(s)*1000, p.Encrypted, p.PadLowBitrate)})
		}
		r.LossMs = mi1.RunLossMs(r)
		switch kind {
		case "kill9":
			r.ResumeMs = new(int64(2500 + 25*p.Index))
		case "bugcheck", "sysrq":
			r.ColdBootMs = new(int64(14_000 + 50*p.Index))
		}
		runs = append(runs, r)
	}
	return runs
}

// mi1Set is set's runs as reccontroller writes them on the owner's
// devices: one envelope per run.
func mi1Set(t *testing.T, set, host string, cameras int) []results.Envelope {
	t.Helper()
	var out []results.Envelope
	for _, r := range mi1Runs(t, set, cameras, 600) {
		e := envelope("MI1", set, r.RunID, host)
		e.Runs = []results.Run{r}
		out = append(out, e)
	}
	return out
}

// labEnvelopes is a lab set as its annotations carry it: one envelope per run,
// with its part of the set's 21.
func labEnvelopes(t *testing.T, set, host string) []results.Envelope {
	t.Helper()
	runs := mi1Runs(t, set, 1, 500)
	out := make([]results.Envelope, len(runs))
	for i, r := range runs {
		e := envelope("MI1", set, r.RunID, host)
		e.Part, e.Parts, e.Runs = i+1, len(runs), []results.Run{r}
		out[i] = e
	}
	return out
}

// mi3Budgets are §4.8's budgets of tier at cameras, each within its limit.
func mi3Budgets(tier string, cameras int) map[string]results.Budget {
	all := map[string]results.Budget{
		"cpuTotal": {Value: 41.5, Limit: 60, Pass: true}, "tileSub": {Value: 0.4, Limit: 1, Pass: true},
		"tileMain": {Value: 1.6, Limit: 3, Pass: true}, "encUpload": {Value: 2.1, Limit: 5, Pass: true},
		"ram":  {Value: float64(300 + 10*cameras), Limit: float64(250 + 20*cameras + 15*mi3.Sessions(tier)), Pass: true},
		"disk": {Value: 1.008, Limit: 1.03, Pass: true}, "fsyncPerFile": {Value: 1, Limit: 1, Pass: true},
		"latencyP95": {Value: 72, Limit: 150, Pass: true}, "ingestPerCamera": {Value: 2.2, Limit: 3, Pass: true},
	}
	out := map[string]results.Budget{}
	for _, key := range mi3.BudgetKeys(tier) {
		out[key] = all[key]
	}
	return out
}

// mi3Envelope is host's MI3 run that passes its first hour at cameras.
func mi3Envelope(host, tier string, cameras int) results.Envelope {
	e := envelope("MI3", mi3.SetFor(host), mi3.SetFor(host)+"-1", host)
	e.MI3 = &results.Mi3Data{Tier: tier, Cameras: cameras, CPUPctTotal: 41.5, RAMMb: float64(300 + 10*cameras),
		Budgets: mi3Budgets(tier, cameras), DiskRatio: map[string]float64{"clear": 1.008, "encrypted": 1.041}, FsyncPerSecPerFile: 1,
		LatencyP95Ms: 72, PacingP99Ms: 6, Steps: []results.Mi3Step{{Cameras: cameras, Minutes: 60, Attempt: 1, Pass: true, Valid: true, PacingP99Ms: 6}},
		Proxies: mi3.Proxies, DiskQueuePeakBytes: 3_400_000,
		LoadCPUPct: map[string]float64{mi3.LoadLive: 9.5, mi3.LoadPlayback: 1.2, mi3.LoadExport: 12.4, mi3.LoadUpload: 2.3}}
	return e
}

// seeks are n seeks of ms each.
func seeks(n int, ms float64) []float64 { return slices.Repeat([]float64{ms}, n) }

// readsMs are 1000 reads, the slowest maxMs.
func readsMs(maxMs float64) []float64 {
	out := make([]float64, 1000)
	for i := range out {
		out[i] = 4 + float64(i%37)*0.5
	}
	out[500] = maxMs
	return out
}

// mi4Envelope is host's MI4 run, every figure within its threshold, at at.
func mi4Envelope(host string, at time.Time) results.Envelope {
	e := envelope("MI4", host+"-mi4", host+"-mi4-1", host)
	e.AtMs = at.UnixMilli()
	model := "PC"
	if host == "pi" {
		model = piModel().Raw
	}
	e.MI4 = &results.Mi4Data{Model: model, ReadMs: readsMs(31.5), ReadMaxMs: 31.5, ReadP50Ms: 12.5, ReadP99Ms: 22, ReadSize: 524288,
		Cold: host == "pi", InfoReads: []results.Mi4Reads{{Size: 65536, MaxMs: 9.5, P50Ms: 3.5, P99Ms: 7}, {Size: 4 << 20, MaxMs: 95, P50Ms: 70, P99Ms: 90}},
		SeekMs: seeks(20, 410), CryptoMBs: 46.5, CryptoMBsAllCores: 130}
	if host == "pi" {
		e.MI4.ThresholdMBs, e.MI4.RecommendedAfter = 20, 4
	}
	return e
}

// mi9Envelope is an MI9 envelope of host in set with one leg.
func mi9Envelope(host, set, runID string, leg results.Mi9Leg) results.Envelope {
	e := envelope("MI9", set, runID, host)
	e.MI9 = &results.Mi9Data{Legs: []results.Mi9Leg{leg}}
	return e
}

// appResult is a result file of the app: its name under results/app/ and
// its bytes.
type appResult struct {
	name string
	data []byte
}

// ageFile is an AGE result of the app on platform at mbps, stored at
// stored, as reccontroller names and writes it.
func ageFile(t *testing.T, platform string, stored time.Time, mbps float64) appResult {
	t.Helper()
	data, err := json.Marshal(map[string]any{"scenario": "AGE", "device": map[string]any{"platform": platform, "model": "SM-A245F"},
		"startedAt": stored.Add(-time.Minute).Format(time.RFC3339), "interrupted": false, "mbps": mbps, "bytes": 33554432, "ms": 8200})
	if err != nil {
		t.Fatal(err)
	}
	return appResult{name: fmt.Sprintf("%s-samsung-sm-a245f-%s-age.json", platform, stored.UTC().Format("20060102-150405")), data: data}
}

// fixture is a whole run of the spike, every gate passing: both owner
// devices, the lab sets, CI's ENC-06 check and MI9 legs, and the phone's
// AGE result.
type fixture struct {
	envs []results.Envelope
	app  []appResult
}

func passing(t *testing.T) *fixture {
	t.Helper()
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	f := &fixture{}
	for _, s := range []struct {
		set, host string
		cameras   int
	}{
		{mi1.SetPCKill9, "pc", 8}, {mi1.SetPCBugcheck, "pc", 1}, {mi1.SetRealPC, "pc", 1},
		{mi1.SetPiKill9, "pi", 4}, {mi1.SetPiSysrq, "pi", 1}, {mi1.SetRealPi, "pi", 1},
	} {
		f.envs = append(f.envs, mi1Set(t, s.set, s.host, s.cameras)...)
	}
	f.envs = append(f.envs, labEnvelopes(t, mi1.SetLabLinuxKill9, "lab-linux")...)
	f.envs = append(f.envs, labEnvelopes(t, mi1.SetLabLinuxQEMUReset, "lab-qemu")...)
	f.envs = append(f.envs, labEnvelopes(t, mi1.SetLabWindowsKill9, "lab-windows")...)
	for _, host := range []string{"pc", "pi"} {
		e := envelope("MI2", host+"-72h", host+"-72h-1", host)
		e.MI2 = &results.Mi2Data{Hours: 72.3, Cameras: map[string]int{"pc": 8, "pi": 4}[host], WindowHours: 6}
		f.envs = append(f.envs, e)
	}
	f.envs = append(f.envs, mi3Envelope("pc", mi3.TierH3, 8), mi3Envelope("pi", mi3.TierH1b, 4),
		mi4Envelope("pi", at), mi4Envelope("pc", at.Add(time.Hour)),
		mi9Envelope("pc", mi9.LANSet, "pc-mi9-1", results.Mi9Leg{Transport: "lan", ThroughputMbps: 92.4, SeekMs: seeks(20, 180), RTTMs: 1.2, Source: mi9.SourceLAN}),
		mi9Envelope("pi", mi9.LANSet, "pi-mi9-1", results.Mi9Leg{Transport: "lan", ThroughputMbps: 41.7, SeekMs: seeks(20, 520), RTTMs: 2.4, Source: mi9.SourceLAN}),
		mi9Envelope("lab-linux", "ci-relay", "ci-relay-1", results.Mi9Leg{Transport: "turns443", ThroughputMbps: 15.8, SeekMs: seeks(20, 300), RTTMs: 50.5, Source: mi9.SourceRelay}),
		mi9Envelope("lab-linux", "ci-chrome", "ci-chrome-1", results.Mi9Leg{Transport: "chrome", SeekMs: seeks(20, 60), DisplayMs: seeks(20, 120), Source: mi9.SourceChrome}),
		mi9Envelope("lab-linux", "ci-loopback", "ci-loopback-1", results.Mi9Leg{Transport: "lan", ThroughputMbps: 512, SeekMs: seeks(20, 40), RTTMs: 0.2, Source: mi9.SourceLoopback}),
	)
	pass := true
	enc06 := envelope("ENC-06", "ci-enc06", "ci-enc06-1", "lab-linux")
	enc06.AtMs, enc06.MI4 = enc06Done.UnixMilli(), &results.Mi4Data{Enc06: &pass}
	f.envs = append(f.envs, enc06)
	f.app = []appResult{ageFile(t, "android", at.Add(2*time.Hour), 31.5)}
	return f
}

// without drops the envelopes that drop matches.
func (f *fixture) without(drop func(e results.Envelope) bool) *fixture {
	f.envs = slices.DeleteFunc(f.envs, drop)
	return f
}

// each changes the envelopes that match.
func (f *fixture) each(match func(e results.Envelope) bool, change func(e *results.Envelope)) *fixture {
	for i := range f.envs {
		if match(f.envs[i]) {
			change(&f.envs[i])
		}
	}
	return f
}

// onHost matches host's envelopes.
func onHost(host string) func(e results.Envelope) bool {
	return func(e results.Envelope) bool { return e.Host == host }
}

// ofCode matches the envelopes of code on host.
func ofCode(code, host string) func(e results.Envelope) bool {
	return func(e results.Envelope) bool { return e.Code == code && e.Host == host }
}

// write writes the fixture as a results folder: each envelope where
// results.Write puts it, and the app's results under app/.
func (f *fixture) write(t *testing.T, dir string) {
	t.Helper()
	for _, e := range f.envs {
		if err := results.Write(dir, e); err != nil {
			t.Fatalf("write %s: %v", e.RunID, err)
		}
	}
	if len(f.app) > 0 {
		if err := os.MkdirAll(filepath.Join(dir, "app"), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range f.app {
		if err := os.WriteFile(filepath.Join(dir, "app", a.name), a.data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// folder writes the fixture into a folder of its own.
func (f *fixture) folder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	f.write(t, dir)
	return dir
}

// goldenDate is the date of the reports of the tests.
const goldenDate = "2026-10-30"

// reportOf is the report of the fixture's folder, judged.
func reportOf(t *testing.T, f *fixture) *report {
	t.Helper()
	in, err := load([]string{f.folder(t)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return newReport(in, goldenDate)
}

// markdownOf is the Markdown report of the fixture.
func markdownOf(t *testing.T, f *fixture) string {
	t.Helper()
	return reportOf(t, f).markdown()
}

// section is the part of md from the heading to the next heading of the
// same or a higher level.
func section(t *testing.T, md, heading string) string {
	t.Helper()
	at := strings.Index(md, "\n"+heading+"\n")
	if at < 0 {
		t.Fatalf("no section %q in:\n%s", heading, md)
	}
	level := strings.Index(heading, " ")
	rest := md[at+1+len(heading):]
	for _, l := range strings.SplitAfter(rest, "\n") {
		if strings.HasPrefix(l, "#") {
			if n := strings.Index(l, " "); n > 0 && n <= level && strings.Trim(l[:n], "#") == "" {
				return md[at+1 : at+1+len(heading)+strings.Index(rest, l)]
			}
		}
	}
	return md[at+1:]
}

// tableRow is the table row of md whose code cell holds code.
func tableRow(t *testing.T, md, code string) []string {
	t.Helper()
	for _, l := range strings.Split(md, "\n") {
		if !strings.HasPrefix(l, "| ") {
			continue
		}
		cells := strings.Split(strings.Trim(l, "| "), " | ")
		if strings.Contains(cells[0], code) {
			return cells
		}
	}
	t.Fatalf("no table row %q in:\n%s", code, md)
	return nil
}

// runReport runs recreport with args and returns its exit code and
// output.
func runReport(args ...string) (int, string) {
	var out bytes.Buffer
	code := run(args, &out, &out)
	return code, out.String()
}
