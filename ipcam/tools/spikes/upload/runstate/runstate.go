// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package runstate is uploadspike's run state (Task 9): one file per
// device, <base>/state/run-state.json, 0600, that every uploadspike command
// of the device reads and updates under a lock of its own, the device's
// hub lock (one hub per device, P19, P53), the claims of the handover, and
// the heartbeat that finds the host's reboots and sleeps (P49, P53).
package runstate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The files of <base>/state/, uploadspike's own (root, 0700, on Linux).
const (
	StateFile = "run-state.json"
	lockFile  = "run-state.lock"
	HubLock   = "hub.lock"
	HubPID    = "hub.pid"
)

// ClaimLife is how long a claim of the hub holds (One hub per device).
const ClaimLife = 90 * time.Second

// sleepSlack is how far the wall clock may run ahead of the monotonic one
// between two heartbeats before the difference counts as a sleep.
const sleepSlack = 2 * time.Minute

// State is a device's run state.
type State struct {
	// Mode is D2's choice for this device (C2-R2): "A", "B" or "C"; empty
	// while the owner took the recommendation.
	Mode string `json:"mode,omitempty"`
	// BootID, LastWallMs and LastMonoMs are the last heartbeat's boot,
	// wall clock (Unix ms) and monotonic clock (ms since boot, sleep
	// excluded).
	BootID     string `json:"bootId,omitempty"`
	LastWallMs int64  `json:"lastWallMs,omitempty"`
	LastMonoMs int64  `json:"lastMonoMs,omitempty"`
	// Gaps are the host's reboots and sleeps, and the hub's crashes.
	Gaps []results.Gap `json:"gaps,omitempty"`
	// Handoffs counts the logins handed to the hub, per account and
	// client ("acct-1/google:android"), over every command of the device.
	Handoffs map[string]int `json:"handoffs,omitempty"`
	// Pending holds what a command keeps between its steps.
	Pending map[string]json.RawMessage `json:"pending,omitempty"`
	// Sd is the owner's answer to the SD-card warning, asked once per
	// device (P02).
	Sd *results.SdConfirmation `json:"sd,omitempty"`
	// HandoverToken is the bearer of /spike/v1/run/handover: 32 random
	// bytes, hex.
	HandoverToken string `json:"handoverToken,omitempty"`
	// Claim is a command that is taking the hub; resume yields to it.
	Claim *Claim `json:"claim,omitempty"`
	// Open holds each open run's resumable state: "MI6", "MI5", "MI8".
	Open map[string]json.RawMessage `json:"open,omitempty"`
	// WebClientID and EntraClientID are the client IDs that the web reader
	// page needs (P21): no secret, never written into results.
	WebClientID   string `json:"webClientId,omitempty"`
	EntraClientID string `json:"entraClientId,omitempty"`
	// Autostart is set by Task 8's install: a resume waits to take the hub.
	Autostart bool `json:"autostart,omitempty"`
}

// Claim is a command that is taking the hub: valid while that process
// lives and UntilMs is ahead.
type Claim struct {
	PID         int   `json:"pid"`
	ProcStartMs int64 `json:"procStartMs"`
	UntilMs     int64 `json:"untilMs"`
}

// Valid reports whether the claim still holds at now.
func (c *Claim) Valid(now time.Time) bool {
	return c != nil && now.UnixMilli() < c.UntilMs && Alive(c.PID, c.ProcStartMs)
}

// Mine reports whether the claim is this process's.
func (c *Claim) Mine() bool {
	if c == nil || c.PID != os.Getpid() {
		return false
	}
	start, err := ProcStartMs(os.Getpid())
	return err == nil && start == c.ProcStartMs
}

// NewClaim is this process's claim, valid ClaimLife from now.
func NewClaim(now time.Time) (*Claim, error) {
	start, err := ProcStartMs(os.Getpid())
	if err != nil {
		return nil, err
	}
	return &Claim{PID: os.Getpid(), ProcStartMs: start, UntilMs: now.Add(ClaimLife).UnixMilli()}, nil
}

// init gives the maps of s a value.
func (s *State) init() {
	if s.Handoffs == nil {
		s.Handoffs = map[string]int{}
	}
	if s.Pending == nil {
		s.Pending = map[string]json.RawMessage{}
	}
	if s.Open == nil {
		s.Open = map[string]json.RawMessage{}
	}
}

// Load reads the run state at path; a missing file is an empty state.
func Load(path string) (*State, error) {
	s := &State{}
	b, err := readFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, s); err != nil {
			return nil, fmt.Errorf("runstate: %s: %w", filepath.Base(path), err)
		}
	}
	s.init()
	return s, nil
}

// Update loads the run state at path, applies fn, and writes the result
// through a temporary file, synced and renamed over it, all under
// <dir>/run-state.lock, which it holds only for the update: concurrent
// commands (MI8 and the hub's holder) keep each other's fields. An error
// of fn writes nothing. The handover token is drawn at the first update.
func Update(path string, fn func(*State) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, lockFile), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the run state's own lock
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := lockWait(f); err != nil {
		return err
	}
	defer func() { _ = unlock(f) }()
	s, err := Load(path)
	if err != nil {
		return err
	}
	if s.HandoverToken == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		s.HandoverToken = hex.EncodeToString(b)
	}
	if err := fn(s); err != nil {
		return err
	}
	return writeAtomic(path, s)
}

// writeAtomic writes s to path, 0600, through a synced temporary file.
func writeAtomic(path string, s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), StateFile+".*.tmp") // 0600
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tmp.Name(), path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// syncDir syncs the folder dir's entries, except on Windows.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir) //nolint:gosec // the state folder
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

// ErrLocked is Lock's error while another process holds the lock.
var ErrLocked = errors.New("runstate: another process holds the lock")

// Lock takes the lock at path, exclusive and without waiting (flock on
// Linux, LockFileEx on Windows): ErrLocked while another open file holds
// it. hub.lock is held for as long as a hub runs (One hub per device). The
// lock goes with the process: its death, however it dies, frees it. Its
// file is not inherited by the process's children.
func Lock(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { //nolint:gosec // G703: uploadspike's own state folder
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // uploadspike's own lock file
	if err != nil {
		return nil, err
	}
	ok, err := tryLock(f)
	switch {
	case err != nil:
		_ = f.Close()
		return nil, err
	case !ok:
		_ = f.Close()
		return nil, ErrLocked
	}
	return func() { _, _ = unlock(f), f.Close() }, nil
}

// MI8Lock is MI8's own lock, <base>/state/mi8.lock: one MI8 per device,
// held for the whole run (Task 10), apart from run-state.lock.
const MI8Lock = "mi8.lock"

// LockFile takes the lock at path as Lock does, and returns its open file,
// which holds the lock for as long as it, or a copy of its descriptor, stays
// open: on Linux uploadspike mi8 hands it to its detached copy as an
// inherited descriptor (exec.Cmd.ExtraFiles), so the lock never lapses
// between the two processes (flock belongs to the open file description,
// which the copy shares). ErrLocked while another open file holds it.
func LockFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { //nolint:gosec // G703: uploadspike's own state folder
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // uploadspike's own lock file
	if err != nil {
		return nil, err
	}
	ok, err := tryLock(f)
	switch {
	case err != nil:
		_ = f.Close()
		return nil, err
	case !ok:
		_ = f.Close()
		return nil, ErrLocked
	}
	return f, nil
}

// HoldsLock reports whether f, an inherited lock file, holds its lock: a
// lock of the same open file description is taken again without waiting,
// and any other holder refuses it.
func HoldsLock(f *os.File) bool {
	ok, err := tryLock(f)
	return err == nil && ok
}

// Heartbeat records a heartbeat at now (wall), mono (the host's monotonic
// clock, which stops while it sleeps) on boot bootID, and returns the gap
// since the last heartbeat, if there is one, which it also appends to
// Gaps: a new boot ID is a "reboot" from the last heartbeat on; the wall
// clock advancing more than 2 min beyond the monotonic one is a "sleep".
func (s *State) Heartbeat(now time.Time, mono time.Duration, bootID string) *results.Gap {
	var gap *results.Gap
	wall := now.UnixMilli()
	switch {
	case s.LastWallMs == 0:
	case s.BootID != "" && !SameBoot(s.BootID, bootID):
		gap = &results.Gap{StartMs: s.LastWallMs, EndMs: wall, Reason: results.GapReboot}
	case wall-s.LastWallMs-(mono.Milliseconds()-s.LastMonoMs) > sleepSlack.Milliseconds():
		gap = &results.Gap{StartMs: s.LastWallMs, EndMs: wall, Reason: results.GapSleep}
	}
	if gap != nil {
		s.Gaps = append(s.Gaps, *gap)
	}
	s.BootID, s.LastWallMs, s.LastMonoMs = bootID, wall, mono.Milliseconds()
	return gap
}
