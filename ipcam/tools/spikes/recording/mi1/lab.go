// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"context"
	"os"
	"path/filepath"
)

// The lab's sets run on GitHub's runners (R03): `recspike mi1 --lab --kind
// kill9 --shard 1|2 [--smoke]` runs one shard of lab-linux-kill9 or
// lab-windows-kill9 against reccontroller -local on loopback, with no
// window, preflight or owner; on Linux each encrypted run records on an
// image of its own (Images, G08). Its envelopes are reccontroller's, which
// the lab workflow turns into annotations (C32, `recspike annotate`). A
// qemu-reset shard runs the same driver in a guest, which mi1.QEMU boots on
// the runner and resets at each cut.

// smokeRuns is how many runs of its shard a smoke run records: the first
// two, one encrypted and one in clear.
const smokeRuns = 2

// Volume is where one run records: its data directory, and, for a run on
// an image of its own, a raw scan of that image for HUB-04 (G08).
type Volume interface {
	// DataDir is the run's ipcam-hub data directory.
	DataDir() string
	// ScanRaw scans the run's own device for the needles, raw; a plain
	// folder has none: no finding.
	ScanRaw(needles [][]byte) ([]Finding, error)
	// Release frees the volume once the run is reported: its recordings
	// would fill the disk.
	Release() error
}

// Volumes gives each run its volume. Prepare makes a fresh one at the run's
// start; Reopen finds it again after a reboot.
type Volumes interface {
	Prepare(ctx context.Context, runID string, encrypted bool) (Volume, error)
	Reopen(ctx context.Context, runID string, encrypted bool) (Volume, error)
}

// LabVolumes is where a lab shard of kind records its runs under root: on
// a Linux kill9 shard each encrypted run on an image of its own (Images),
// every other run in a plain folder (Dirs).
func LabVolumes(kind, root string) Volumes { return labVolumes(kind, root) }

// Dirs gives each run a plain folder under Root: <Root>/<runId>/data. The
// owner's devices and the lab's clear runs record there.
type Dirs struct{ Root string }

// Prepare creates the run's folder, empty.
func (d Dirs) Prepare(_ context.Context, runID string, _ bool) (Volume, error) {
	dir := filepath.Join(d.Root, runID, "data")
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return dirVolume(dir), nil
}

// Reopen finds the run's folder.
func (d Dirs) Reopen(_ context.Context, runID string, _ bool) (Volume, error) {
	dir := filepath.Join(d.Root, runID, "data")
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	return dirVolume(dir), nil
}

// dirVolume is a run's plain data folder.
type dirVolume string

func (v dirVolume) DataDir() string                   { return string(v) }
func (dirVolume) ScanRaw([][]byte) ([]Finding, error) { return nil, nil }
func (v dirVolume) Release() error                    { return os.RemoveAll(string(v)) }
