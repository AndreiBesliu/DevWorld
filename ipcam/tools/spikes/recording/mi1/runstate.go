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
	"slices"
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

// RunState is where a set stands, in results/state/run-state.json (G14),
// so that `recspike resume` continues it after each reboot. The file keeps
// the state of every set that ran in the results folder, by results set
// (ResultsSet), and which one is active: the set begun last, which resume
// continues. A set that is complete stays complete there when another set
// runs, so that it never starts again under the run ids of its counted
// envelopes; a lab set's two shards share their set's state, as their
// runs share its plan. The file lives in the published results folder, so
// it names no address and no absolute path (PH0-22): run folders are found
// by run id under recspike's private work folder, and Before's files are
// relative to the run's data directory.
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
	// Shard is the lab shard that the set runs now; Smoke marks a lab
	// smoke run's set.
	Shard int  `json:"shard,omitempty"`
	Smoke bool `json:"smoke,omitempty"`
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
	// Cores6Plus is diag host's: 6 physical cores or more, with which a PC
	// also runs MI3 at 16 cameras (R07).
	Cores6Plus bool `json:"cores6Plus,omitempty"`
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
	// ResumeMs is the kill to the restarted ipcam-hub's recording (kill9),
	// without the harness's raw HUB-04 scan at the stop.
	ResumeMs *int64 `json:"resumeMs,omitempty"`
	// StopBoot is the host's boot when recspike asked the OS to stop
	// (sysrq, bugcheck): another boot after it proves the reboot.
	StopBoot *Boot `json:"stopBoot,omitempty"`
	// Hub04FoundAtStop is true when the raw scan of the run's own device
	// right after the stop, before ipcam-hub reconciled, found clear video
	// or failed (G08).
	Hub04FoundAtStop bool `json:"hub04FoundAtStop,omitempty"`

	// trace is what this recspike saw of ipcam-hub around the stop, for the
	// diagnostics of a loss over the limit; log is the run's open
	// ipcam-hub log. Neither is kept across a reboot.
	trace *cutTrace
	log   *os.File
}

// runStates is the run state file: each set's state by results set, and
// the active one's results set.
type runStates struct {
	Active string               `json:"active"`
	Sets   map[string]*RunState `json:"sets"`
}

// key is the results set that st's state is kept under.
func (st *RunState) key() string { return ResultsSet(st.Set, st.Smoke) }

// LoadRunState reads the active set's state in the run state file at path;
// nil when there is none.
func LoadRunState(path string) (*RunState, error) {
	states, err := loadRunStates(path)
	if err != nil {
		return nil, err
	}
	return states.Sets[states.Active], nil
}

// loadRunStates reads the run state file at path; empty when there is
// none.
func loadRunStates(path string) (*runStates, error) {
	states := &runStates{Sets: map[string]*RunState{}}
	data, err := os.ReadFile(path) //nolint:gosec // recspike's own run state
	if errors.Is(err, os.ErrNotExist) {
		return states, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(states); err != nil {
		// The file of an older recspike, one set's state, or a damaged one:
		// every load and save refuses it, so the owner hears how to start over.
		return nil, fmt.Errorf("starea probelor nu se poate citi (%s: %w); poate a rămas de la o versiune mai veche a recspike. "+
			"Ca să începeți probele de la capăt, ștergeți dosarul %s", filepath.Base(path), err, filepath.Dir(path))
	}
	if states.Sets == nil {
		states.Sets = map[string]*RunState{}
	}
	return states, nil
}

// SaveRunState writes st as the active set's state into the run state file
// at path, beside the other sets' states, which it keeps. The file is
// written whole and durably: beside its final name, synced, renamed over
// it, and the folder synced, so that a power cut leaves the old file or
// the new one.
func SaveRunState(path string, st *RunState) error {
	states, err := loadRunStates(path)
	if err != nil {
		return err
	}
	states.Active = st.key()
	states.Sets[states.Active] = st
	data, err := json.MarshalIndent(states, "", "  ")
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
// (NTFS journals its entries). Tests replace it, and syncPath, to see what
// recspike syncs.
var syncDir = func(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir) //nolint:gosec // recspike's own folder
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

// syncPath makes the file at path durable: its bytes and its size reach
// the disk (fsync, FlushFileBuffers).
var syncPath = func(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // recspike's own file
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

// writeSynced writes data to the file at path and syncs it.
func writeSynced(path string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}
	return syncPath(path)
}

// WriteDurably writes a file that recspike reads again after a reboot,
// such as the controller's address: the file is synced, and so is its
// folder's entry, so that a stop that comes before the system writes them
// out leaves neither empty nor gone.
func WriteDurably(path string, data []byte, perm os.FileMode) error {
	if err := writeSynced(path, data, perm); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// runs is the runs done, as NextRun and NextRealCut take them.
func (st *RunState) runs() []results.Run {
	out := make([]results.Run, len(st.Done))
	for i, d := range st.Done {
		out[i] = d.Run
	}
	return out
}

// nextAutomated is plan's next run after the runs done at its positions
// (NextRun) and its attempt at that position, counted from 1; ok is false
// when every position has a run that counts. The runs done at other
// positions, the other lab shard's, do not move it.
func (st *RunState) nextAutomated(plan []RunPlan) (next RunPlan, attempt int, ok bool) {
	var runs []results.Run
	for _, d := range st.Done {
		if slices.ContainsFunc(plan, func(p RunPlan) bool { return p.Index == d.Index }) {
			runs = append(runs, d.Run)
		}
	}
	next, ok = NextRun(plan, runs)
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
