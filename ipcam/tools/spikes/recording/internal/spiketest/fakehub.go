// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package spiketest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
)

// A test binary stands in for ipcam-hub when FakeHubEnv is "1" and its
// TestMain hands its arguments to FakeHub: a recording's ipcam-hub, for the
// tests of hubrec.Record and of the probes that record with it (MI9).
const (
	FakeHubEnv = "RECSPIKE_FAKE_HUB"
	// FakeHubLogEnv names the file to which the fake appends each job of
	// the run.json that it runs, as JSON, one line per job.
	FakeHubLogEnv = "RECSPIKE_FAKE_LOG"
	// FakeHubSegmentsEnv lists, comma-separated, the lengths in seconds of
	// the segments that the fake closes, in order; unset, it closes one of
	// the first job's segment length.
	FakeHubSegmentsEnv = "RECSPIKE_FAKE_SEGMENTS"
)

// fakeSegmentStart is when the fake's first segment starts.
var fakeSegmentStart = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

// fakeSegmentGap is how long the fake waits between two segments that it
// closes, so that a reader sees each segment_closed on its own.
const fakeSegmentGap = 200 * time.Millisecond

// FakeHub is the fake ipcam-hub's main; it returns the exit code. "diag
// host" prints a host on an SSD with no Pi model; "run --config <run.json>
// --stop-on-stdin-eof" appends each job of run.json to the log, prints
// reconcile, ready and the first job's recording, then, for each segment
// that FakeHubSegmentsEnv lists (one of the job's length when unset),
// writes a 10-s encrypted test recording for the job's recipients
// (WriteSegment) where MediaMTX would, under run.json's data folder, named
// by its start, and prints its segment_closed with its start and end; it
// exits at the end of its stdin.
func FakeHub(args []string) int {
	fail := func(err error) int {
		_, _ = fmt.Fprintln(os.Stderr, "fake ipcam-hub:", err)
		return 1
	}
	if len(args) >= 2 && args[0] == "diag" && args[1] == "host" {
		fmt.Println(`{"schema":"ipcam.diag.host/1","os":"` + runtime.GOOS + `","arch":"amd64","physicalCores":4,"logicalCpus":8,"ramMb":8000,"piModel":null,"storage":"ssd","writeCache":"on","recommendedCameras":4,"cores6Plus":false}`)
		return 0
	}
	if len(args) != 4 || args[0] != "run" || args[1] != "--config" || args[3] != "--stop-on-stdin-eof" {
		return fail(errors.New("usage: run --config <run.json> --stop-on-stdin-eof"))
	}
	cfg, err := hubproc.ReadConfig(args[2])
	if err != nil {
		return fail(err)
	}
	log, err := os.OpenFile(os.Getenv(FakeHubLogEnv), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own log
	if err != nil {
		return fail(err)
	}
	for _, j := range cfg.Jobs {
		b, _ := json.Marshal(j)
		_, _ = log.Write(append(b, '\n'))
	}
	_ = log.Close()
	if len(cfg.Jobs) == 0 {
		return fail(errors.New("no job"))
	}
	j := cfg.Jobs[0]
	var recipients []age.Recipient
	for _, r := range j.Recipients {
		rec, err := age.ParseX25519Recipient(r)
		if err != nil {
			return fail(err)
		}
		recipients = append(recipients, rec)
	}
	lengths, err := fakeSegments(j.SegmentSecs)
	if err != nil {
		return fail(err)
	}
	dir := filepath.Join(cfg.DataDir, "rec", j.Cam, j.Profile, "2026-10-29")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fail(err)
	}
	emit := func(fields map[string]any) {
		fields["atMs"] = time.Now().UnixMilli()
		b, _ := json.Marshal(fields)
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
	emit(map[string]any{"type": "reconcile", "seq": 1, "final": true, "aborted": false})
	emit(map[string]any{"type": "ready", "jobs": []any{}})
	emit(map[string]any{"type": "recording", "cam": j.Cam, "profile": j.Profile})
	start := fakeSegmentStart
	for i, secs := range lengths {
		if i > 0 {
			time.Sleep(fakeSegmentGap)
		}
		end := start.Add(time.Duration(secs) * time.Second)
		segment := filepath.Join(dir, start.Format("150405")+"_000000.mp4.age")
		if _, err := WriteSegment(segment, 10, recipients...); err != nil {
			return fail(err)
		}
		emit(map[string]any{"type": "segment_closed", "cam": j.Cam, "profile": j.Profile, "path": segment,
			"startMs": start.UnixMilli(), "endMs": end.UnixMilli(), "encrypted": true})
		start = end
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

// fakeSegments is the lengths, in seconds, of the segments that the fake
// closes: FakeHubSegmentsEnv's, or one of jobSecs (300 when 0).
func fakeSegments(jobSecs int) ([]int, error) {
	list := os.Getenv(FakeHubSegmentsEnv)
	if list == "" {
		if jobSecs == 0 {
			jobSecs = 300
		}
		return []int{jobSecs}, nil
	}
	var out []int
	for _, f := range strings.Split(list, ",") {
		n, err := strconv.Atoi(f)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%s: a segment of %q s", FakeHubSegmentsEnv, f)
		}
		out = append(out, n)
	}
	return out, nil
}

// InstallFakeHub copies the test binary into dir as ipcam-hub (with .exe on
// Windows), sets the environment that makes it the fake for the rest of the
// test, and returns its path and the file of its jobs.
func InstallFakeHub(t *testing.T, dir string) (hub, jobs string) {
	t.Helper()
	hub = filepath.Join(dir, "ipcam-hub")
	if runtime.GOOS == "windows" {
		hub += ".exe"
	}
	self, err := os.ReadFile(os.Args[0]) //nolint:gosec // the test binary, the fake ipcam-hub
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hub, self, 0o700); err != nil { //nolint:gosec // the test binary as the fake ipcam-hub
		t.Fatal(err)
	}
	jobs = filepath.Join(dir, "jobs.log")
	t.Setenv(FakeHubEnv, "1")
	t.Setenv(FakeHubLogEnv, jobs)
	return hub, jobs
}

// HubJobs reads the jobs that the fake ipcam-hub ran, from the file that
// InstallFakeHub named: none when it never ran.
func HubJobs(t *testing.T, jobs string) []hubproc.Job {
	t.Helper()
	b, err := os.ReadFile(jobs) //nolint:gosec // the test's own log
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []hubproc.Job
	dec := json.NewDecoder(bytes.NewReader(b))
	for {
		var j hubproc.Job
		if err := dec.Decode(&j); errors.Is(err, io.EOF) {
			return out
		} else if err != nil {
			t.Fatalf("the fake ipcam-hub's jobs: %v", err)
		}
		out = append(out, j)
	}
}
