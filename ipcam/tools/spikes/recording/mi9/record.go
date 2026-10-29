// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi9

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// The camera that MI9 plays: the controller's 4 Mbps main stream, recorded
// encrypted into one 5-min segment, as MI4's (G26).
const (
	Camera      = "cam01"
	Profile     = "main"
	SegmentSecs = 300
)

// Where a recording lies in its folder: its data folder, for ipcam-hub,
// and the file of RI_g's identity, which decrypts it.
const (
	recordingDir = "data"
	identityFile = "ri_g.key"
)

// The waits of a recording.
const (
	// recordingWait bounds the wait for the camera's first part.
	recordingWait = 2 * time.Minute
	// closeSlack is how long after its length a segment may take to close
	// at the next keyframe: 5 min 10 s for a 5-min segment.
	closeSlack = 10 * time.Second
)

// Recording is a closed encrypted segment and the file of RI_g's identity,
// which decrypts it.
type Recording struct{ Path, Identity string }

// RecordOptions are what a recording needs: ipcam-hub and the fork
// (MediaMTX, "" for the one next to Hub), the camera's RTSP address with
// its credentials (they go only to the private run.json), the recording's
// own folder, the segment's length, the SD-card confirmation (G16), and
// the owner's screen, nil for none.
type RecordOptions struct {
	Hub, MediaMTX   string
	CameraURL       string
	Dir             string
	SegmentSecs     int
	SDCardConfirmed bool
	Out             io.Writer
}

// Record records the camera at CameraURL, encrypted for an ephemeral RI_g
// and RK (PH0-22: RK's identity is never kept), through ipcam-hub (C01),
// into one segment of SegmentSecs, and returns that segment once it has
// closed. Dir holds the recording, run.json, RI_g's identity and
// ipcam-hub's log.
func Record(ctx context.Context, o RecordOptions) (Recording, error) {
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return Recording{}, err
	}
	riG, err := age.GenerateX25519Identity()
	if err != nil {
		return Recording{}, err
	}
	rk, err := age.GenerateX25519Identity()
	if err != nil {
		return Recording{}, err
	}
	identity := filepath.Join(o.Dir, identityFile)
	if err := os.WriteFile(identity, []byte(riG.String()+"\n"), 0o600); err != nil {
		return Recording{}, err
	}
	config := filepath.Join(o.Dir, "run.json")
	err = hubproc.WriteConfig(config, hubproc.Config{DataDir: filepath.Join(o.Dir, recordingDir), MediaMTX: o.MediaMTX, IdentityFile: identity,
		SDCardConfirmed: o.SDCardConfirmed, Jobs: []hubproc.Job{{Cam: Camera, Profile: Profile, RestreamURL: o.CameraURL,
			SegmentSecs: o.SegmentSecs, Recipients: []string{riG.Recipient().String(), rk.Recipient().String()}}}})
	if err != nil {
		return Recording{}, err
	}
	logPath := filepath.Join(o.Dir, "ipcam-hub.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the recording's own folder
	if err != nil {
		return Recording{}, err
	}
	defer func() { _ = logFile.Close() }()
	hub, err := hubproc.Start(hubproc.Options{Binary: o.Hub, Config: config, Stderr: logFile})
	if err != nil {
		return Recording{}, fmt.Errorf("nu pot porni ipcam-hub: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mi1.StopTimeout)
		defer cancel()
		if err := hub.Stop(stopCtx); err != nil && o.Out != nil {
			_, _ = fmt.Fprintf(o.Out, "ipcam-hub nu s-a oprit curat: %v\n", err)
		}
	}()
	waitCtx, cancel := context.WithTimeout(ctx, recordingWait)
	_, err = hub.WaitRecording(waitCtx)
	cancel()
	if err != nil {
		return Recording{}, fmt.Errorf("ipcam-hub nu a început să înregistreze camera: %w; jurnalul este %s", err, logPath)
	}
	closeWait := time.Duration(o.SegmentSecs)*time.Second + closeSlack
	waitCtx, cancel = context.WithTimeout(ctx, closeWait)
	e, err := hub.WaitEvent(waitCtx, "segment_closed", func(e hubproc.Event) bool { return e.Cam == Camera && e.Profile == Profile })
	cancel()
	if err != nil {
		return Recording{}, fmt.Errorf("segmentul nu s-a închis în %v: %w; jurnalul este %s", closeWait, err, logPath)
	}
	return Recording{Path: e.Path, Identity: identity}, nil
}

// ReadIdentity reads the RI_g identity in the file at path.
func ReadIdentity(path string) (age.Identity, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the recording's own identity file
	if err != nil {
		return nil, err
	}
	return age.ParseX25519Identity(strings.TrimSpace(string(b)))
}

// removeRecordings removes the recordings under root's run folders,
// <run>/<segment>/data, with RI_g's identity file beside each, and keeps
// the rest: the logs.
func removeRecordings(root string) error {
	var errs []error
	for _, name := range []string{recordingDir, identityFile} {
		paths, err := filepath.Glob(filepath.Join(root, "*", "*", name))
		if err != nil {
			return err
		}
		for _, path := range paths {
			errs = append(errs, os.RemoveAll(path))
		}
	}
	return errors.Join(errs...)
}
