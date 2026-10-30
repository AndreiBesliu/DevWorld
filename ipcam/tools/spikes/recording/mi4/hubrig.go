// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi4

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubrec"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// endWait bounds the goodbye at the end of a run, with the PC's pull of a
// Pi's results.
const endWait = 2 * time.Minute

// The schemas of ipcam-hub's diag benches.
const (
	readbenchSchema   = "ipcam.diag.readbench/1"
	seekbenchSchema   = "ipcam.diag.seekbench/1"
	cryptobenchSchema = "ipcam.diag.cryptobench/1"
)

// hubRig records and measures through the ipcam-hub binary (C01): `run`
// records the controller's camera, and `diag readbench`, `diag seekbench`
// and `diag cryptobench` time the hub's own read path and age writer.
type hubRig struct {
	o    Options
	host mi1.HostState
	dir  string // the run's folder under work/mi4
	// link is the one link to the controller for the whole run, from the
	// first recording to the end (mi1.HostLink); url is its camera's RTSP
	// address.
	link *mi1.HostLink
	url  string
}

// record records the camera, encrypted, into a segment of segmentSecs
// through ipcam-hub (hubrec.Record) and returns that segment once it has
// closed: <run>/<name> holds the recording, run.json, RI_g's identity
// (RK's is never kept, PH0-22) and ipcam-hub's log.
func (h *hubRig) record(ctx context.Context, name string, segmentSecs int) (hubrec.Segment, error) {
	if h.link == nil {
		l, err := mi1.OpenHostLink(ctx, h.o.Controller, h.host.Pseudonym)
		if err != nil {
			return hubrec.Segment{}, err
		}
		h.link = l
		if h.url, err = l.CameraURL(hubrec.Camera + "_" + hubrec.Profile); err != nil {
			return hubrec.Segment{}, err
		}
	}
	return hubrec.Record(ctx, hubrec.Options{Hub: h.o.Hub, MediaMTX: h.o.MediaMTX, CameraURL: h.url, Run: h.dir, Name: name,
		SegmentSecs: segmentSecs, SDCardConfirmed: h.host.SdConfirmation != nil, Say: h.o.say})
}

func (h *hubRig) readbench(ctx context.Context, seg hubrec.Segment, size string, reads int) (readSet, error) {
	var s readSet
	err := h.diag(ctx, &s, "readbench", "--file", seg.Path, "--identity", seg.Identity, "--reads", strconv.Itoa(reads), "--size", size)
	if err == nil && (s.Schema != readbenchSchema || s.Reads != reads || len(s.ReadMs) != reads) {
		err = fmt.Errorf("ipcam-hub diag readbench a dat %s cu %d citiri, nu %s cu %d", s.Schema, len(s.ReadMs), readbenchSchema, reads)
	}
	return s, err
}

func (h *hubRig) seekbench(ctx context.Context, seg hubrec.Segment, seeks int, wholeRAM bool) ([]float64, error) {
	var s struct {
		Schema   string    `json:"schema"`
		WholeRAM bool      `json:"wholeRam"`
		SeekMs   []float64 `json:"seekMs"`
	}
	args := []string{"seekbench", "--file", seg.Path, "--identity", seg.Identity, "--seeks", strconv.Itoa(seeks)}
	if wholeRAM {
		args = append(args, "--whole-ram")
	}
	err := h.diag(ctx, &s, args...)
	if err == nil && (s.Schema != seekbenchSchema || s.WholeRAM != wholeRAM || len(s.SeekMs) != seeks) {
		err = fmt.Errorf("ipcam-hub diag seekbench a dat %s cu %d salturi, nu %s cu %d", s.Schema, len(s.SeekMs), seekbenchSchema, seeks)
	}
	return s.SeekMs, err
}

func (h *hubRig) cryptobench(ctx context.Context, seconds int) (cryptoRates, error) {
	var r cryptoRates
	err := h.diag(ctx, &r, "cryptobench", "--seconds", strconv.Itoa(seconds))
	if err == nil && (r.Schema != cryptobenchSchema || r.MBPerSecSingleCore <= 0) {
		err = fmt.Errorf("ipcam-hub diag cryptobench a dat %+v", r)
	}
	return r, err
}

// diag runs `ipcam-hub diag <args>` and decodes its one line into out; its
// stderr goes to diag.log in the run's folder.
func (h *hubRig) diag(ctx context.Context, out any, args ...string) error {
	logPath := filepath.Join(h.dir, "diag.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the run's own folder
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	cmd := exec.CommandContext(ctx, h.o.Hub, append([]string{"diag"}, args...)...) //nolint:gosec // ipcam-hub next to recspike
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, logFile
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ipcam-hub diag %s: %w; jurnalul este %s", args[0], err, logPath)
	}
	dec := json.NewDecoder(&stdout)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("ipcam-hub diag %s a dat o linie care nu se poate citi: %w", args[0], err)
	}
	return nil
}

// end says goodbye on the run's link, if it opened one, and answers the
// PC's reccontroller, which pulls a Pi's results folder then (G15). It
// waits for the pull even after the owner stopped the run, up to endWait.
func (h *hubRig) end(ctx context.Context) error {
	if h.link == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), endWait)
	defer cancel()
	return h.link.End(ctx, h.o.Results, h.o.Out)
}
