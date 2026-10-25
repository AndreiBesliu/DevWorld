// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The phases of the run in progress, as a reboot finds them.
const (
	// PhaseRecording: ipcam-hub records the run; a reboot is the run's
	// stop (a real cut, or an unplanned one).
	PhaseRecording = "recording"
	// PhaseStopping: recspike stops the host now (sysrq, bugcheck).
	PhaseStopping = "stopping"
	// PhaseStopped: ipcam-hub was killed and Before is listed (kill9).
	PhaseStopped = "stopped"
)

// RunState is results/state/run-state.json (G14): where a set stands, so
// that `recspike resume` continues it after each reboot. It lives in the
// published results folder, so it names no address and no absolute path
// (PH0-22): run folders are found by run id under recspike's private work
// folder, and Before's files are relative to the run's data directory.
type RunState struct {
	Set  string `json:"set"`
	Mode string `json:"mode"`
	Kind string `json:"kind"`
	// NextRun is the plan position of the next run (a real cut's slot), and
	// Attempts counts the runs started; both are information: the runs
	// done decide.
	NextRun  int    `json:"nextRun"`
	Slot     int    `json:"slot,omitempty"`
	Attempts int    `json:"attempts"`
	Window   string `json:"window,omitempty"`
	Shard    int    `json:"shard,omitempty"`
	Smoke    bool   `json:"smoke,omitempty"`
	// Host is what the envelopes say of the host, as the set's start read
	// it; the owner's answers (an estimated camera count, the SD-card
	// confirmation) are not asked again after a reboot.
	Host    HostState   `json:"host"`
	Done    []DoneRun   `json:"done"`
	Current *CurrentRun `json:"current,omitempty"`
}

// HostState is the host as the envelopes describe it.
type HostState struct {
	Pseudonym      string                  `json:"pseudonym"`
	OS             string                  `json:"os"`
	Storage        string                  `json:"storage"`
	WriteCache     string                  `json:"writeCache"`
	PiModel        *results.PiModelInfo    `json:"piModel,omitempty"`
	SdConfirmation *results.SdConfirmation `json:"sdConfirmation,omitempty"`
	// Cameras is the recommended camera count, which kill9 runs record.
	Cameras int `json:"cameras"`
}

// DoneRun is a run that ended, as the controller judged it, at its plan
// position (a real cut's slot).
type DoneRun struct {
	Index int         `json:"index"`
	Run   results.Run `json:"run"`
}

// CurrentRun is the run in progress.
type CurrentRun struct {
	RunID         string   `json:"runId"`
	Index         int      `json:"index"`
	Slot          int      `json:"slot,omitempty"`
	Attempt       int      `json:"attempt"`
	Encrypted     bool     `json:"encrypted"`
	PadLowBitrate bool     `json:"padLowBitrate"`
	Streams       []string `json:"streams"`
	Phase         string   `json:"phase"`
	// Before is each stream's segment files at the stop, relative to the
	// run's data directory (lastindex.Before), once listed.
	Before map[string][]string `json:"before,omitempty"`
	// ResumeMs is the kill to the restarted ipcam-hub's recording (kill9).
	ResumeMs *int64 `json:"resumeMs,omitempty"`
}

// LoadRunState reads the run state at path; nil when there is none.
func LoadRunState(path string) (*RunState, error) {
	data, err := os.ReadFile(path) //nolint:gosec // recspike's own run state
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st RunState
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&st); err != nil {
		return nil, fmt.Errorf("mi1: %s: %w", filepath.Base(path), err)
	}
	return &st, nil
}

// SaveRunState writes st to path whole and durably: beside its final name,
// synced, renamed over it, and the folder synced, so that a power cut
// leaves the old state or the new one.
func SaveRunState(path string, st *RunState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "run-state.*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // gone after the rename
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir makes a folder's entries durable; Windows has no directory fsync
// (NTFS journals its entries).
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir) //nolint:gosec // recspike's own folder
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

// runs is the runs done, as NextRun and NextRealCut take them.
func (st *RunState) runs() []results.Run {
	out := make([]results.Run, len(st.Done))
	for i, d := range st.Done {
		out[i] = d.Run
	}
	return out
}

// nextAutomated is plan's next run after the runs done (NextRun) and its
// attempt at that position, counted from 1; ok is false when every
// position has a run that counts.
func (st *RunState) nextAutomated(plan []RunPlan) (next RunPlan, attempt int, ok bool) {
	next, ok = NextRun(plan, st.runs())
	if !ok {
		return RunPlan{}, 0, false
	}
	attempt = 1
	for _, d := range st.Done {
		if d.Index == next.Index {
			attempt++
		}
	}
	return next, attempt, true
}

// lockPoll is how often a waiting recspike tries the lock again.
const lockPoll = 200 * time.Millisecond

// LockRunState takes the exclusive lock on the run state's lock file at
// path (flock on Linux, LockFileEx on Windows), so that two recspikes never
// run two ipcam-hubs on one data directory and catalog (G14). When another
// recspike holds it, waiting is called once, and the lock is tried again
// until it is free or ctx ends. The lock goes with the process: a reboot or
// a crash frees it.
func LockRunState(ctx context.Context, path string, waiting func()) (unlock func() error, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // recspike's own lock file
	if err != nil {
		return nil, err
	}
	told := false
	for {
		ok, err := tryLock(f)
		switch {
		case err != nil:
			_ = f.Close()
			return nil, err
		case ok:
			return func() error { return errors.Join(unlockFile(f), f.Close()) }, nil
		case !told && waiting != nil:
			waiting()
			told = true
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(lockPoll):
		}
	}
}
