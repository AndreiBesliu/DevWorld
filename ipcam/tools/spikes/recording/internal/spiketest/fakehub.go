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
)

// FakeHub is the fake ipcam-hub's main; it returns the exit code. "diag
// host" prints a host on an SSD with no Pi model; "run --config <run.json>
// --stop-on-stdin-eof" appends each job of run.json to the log, writes a
// 10-s encrypted test recording for the first job's recipients
// (WriteSegment) where MediaMTX would, under run.json's data folder, prints
// reconcile, ready, the job's recording and its segment_closed, and exits
// at the end of its stdin.
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
	dir := filepath.Join(cfg.DataDir, "rec", j.Cam, j.Profile, "2026-10-29")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fail(err)
	}
	segment := filepath.Join(dir, "120000_000000.mp4.age")
	if _, err := WriteSegment(segment, 10, recipients...); err != nil {
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
	emit(map[string]any{"type": "segment_closed", "cam": j.Cam, "profile": j.Profile, "path": segment})
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
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
