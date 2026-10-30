// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package hubrec records a probe's encrypted segment through the ipcam-hub
// binary (C01) and owns where it lies: MI4's and MI9's recordings, and the
// CI segment of MI9's job. A probe keeps its runs under recspike's work
// folder, work/<probe>/<run>/, one folder per run named by its start; each
// recording of a run is <run>/<name>/, with ipcam-hub's data folder data/,
// RI_g's identity ri_g.key beside it (RK's is never kept, PH0-22), run.json
// and ipcam-hub's log.
package hubrec

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// The camera that the probes record: the controller's 4 Mbps main stream.
const (
	Camera  = "cam01"
	Profile = "main"
)

// FiveMin is the name of a run's 5-min recording, in MI4's runs and in
// MI9's: MI9 plays the one that MI4 left on the Pi.
const FiveMin = "5min"

// Where a recording lies in its folder: ipcam-hub's data folder, and the
// file of RI_g's identity, which decrypts it.
const (
	RecordingDir = "data"
	IdentityFile = "ri_g.key"
)

// The waits of a recording.
const (
	// recordingWait bounds the wait for the camera's first part.
	recordingWait = 2 * time.Minute
	// closeSlack is how long after its length a segment may take to close
	// at the next keyframe: 5 min 10 s for a 5-min segment.
	closeSlack = 10 * time.Second
)

// Segment is a closed encrypted segment and the file of RI_g's identity,
// which decrypts it.
type Segment struct{ Path, Identity string }

// Options are what a recording needs.
type Options struct {
	// Hub is ipcam-hub; MediaMTX the fork, "" for the one next to Hub.
	Hub, MediaMTX string
	// CameraURL is the camera's RTSP address with its credentials, which
	// go only to the private run.json.
	CameraURL string
	// Run is the run's folder (Work.Start) and Name the recording's in it:
	// the recording lies in Run/Name.
	Run, Name string
	// SegmentSecs is the segment's length.
	SegmentSecs int
	// SDCardConfirmed is the owner's SD-card confirmation (G16).
	SDCardConfirmed bool
	// Say shows a line to the owner, nil for none.
	Say func(format string, args ...any)
}

// Record records the camera at CameraURL, encrypted for an ephemeral RI_g
// and RK (PH0-22: RK's identity is never kept), through ipcam-hub (C01),
// into one segment of SegmentSecs, and returns that segment once it has
// closed. A segment that closed early (a source drop of more than 10 s, a
// restart of MediaMTX) is not one: Record then waits, up to one more
// segment length, for the next one of the full length, and fails with a
// Romanian line when that one closes early too (mis-6). Run/Name holds the
// recording, run.json, RI_g's identity and ipcam-hub's log.
func Record(ctx context.Context, o Options) (Segment, error) {
	dir := filepath.Join(o.Run, o.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Segment{}, err
	}
	riG, err := age.GenerateX25519Identity()
	if err != nil {
		return Segment{}, err
	}
	rk, err := age.GenerateX25519Identity()
	if err != nil {
		return Segment{}, err
	}
	identity := filepath.Join(dir, IdentityFile)
	if err := os.WriteFile(identity, []byte(riG.String()+"\n"), 0o600); err != nil {
		return Segment{}, err
	}
	config := filepath.Join(dir, "run.json")
	err = hubproc.WriteConfig(config, hubproc.Config{DataDir: filepath.Join(dir, RecordingDir), MediaMTX: o.MediaMTX, IdentityFile: identity,
		SDCardConfirmed: o.SDCardConfirmed, Jobs: []hubproc.Job{{Cam: Camera, Profile: Profile, RestreamURL: o.CameraURL,
			SegmentSecs: o.SegmentSecs, Recipients: []string{riG.Recipient().String(), rk.Recipient().String()}}}})
	if err != nil {
		return Segment{}, err
	}
	logPath := filepath.Join(dir, "ipcam-hub.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the recording's own folder
	if err != nil {
		return Segment{}, err
	}
	defer func() { _ = logFile.Close() }()
	closed := make(chan hubproc.Event, 8)
	onEvent := func(e hubproc.Event) {
		if e.Type == "segment_closed" && e.Cam == Camera && e.Profile == Profile {
			select {
			case closed <- e:
			default: // a segment every few minutes: never full
			}
		}
	}
	hub, err := hubproc.Start(hubproc.Options{Binary: o.Hub, Config: config, Stderr: logFile, OnEvent: onEvent})
	if err != nil {
		return Segment{}, fmt.Errorf("nu pot porni ipcam-hub: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mi1.StopTimeout)
		defer cancel()
		if err := hub.Stop(stopCtx); err != nil && o.Say != nil {
			o.Say("ipcam-hub nu s-a oprit curat: %v", err)
		}
	}()
	waitCtx, cancel := context.WithTimeout(ctx, recordingWait)
	_, err = hub.WaitRecording(waitCtx)
	cancel()
	if err != nil {
		return Segment{}, fmt.Errorf("ipcam-hub nu a început să înregistreze camera: %w; jurnalul este %s", err, logPath)
	}
	path, err := o.fullSegment(ctx, hub, closed)
	if err != nil {
		return Segment{}, fmt.Errorf("%w; jurnalul este %s", err, logPath)
	}
	return Segment{Path: path, Identity: identity}, nil
}

// fullSegment waits for the first segment in closed that lasted at least
// SegmentSecs − 1 s, and returns its path (mis-6): a segment's close waits
// for the next keyframe, so a full one lasts its length or a little more.
// After a shorter one, it waits up to one more segment length for the
// next; when that one is short too, or none closes, it fails with the
// Romanian line of the short one.
func (o Options) fullSegment(ctx context.Context, hub *hubproc.Hub, closed <-chan hubproc.Event) (string, error) {
	length := time.Duration(o.SegmentSecs) * time.Second
	closeWait := length + closeSlack
	timer := time.NewTimer(closeWait)
	defer timer.Stop()
	var short *hubproc.Event
	tooShort := func(e hubproc.Event) error {
		return fmt.Errorf("segmentul s-a închis după %d s, nu după %s: porniți din nou proba",
			(e.EndMs-e.StartMs+500)/1000, durationText(o.SegmentSecs))
	}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-hub.Exited():
			return "", fmt.Errorf("segmentul nu s-a închis: %w", hubproc.ErrExited)
		case <-timer.C:
			if short != nil {
				return "", tooShort(*short)
			}
			return "", fmt.Errorf("segmentul nu s-a închis în %v", closeWait)
		case e := <-closed:
			if time.Duration(e.EndMs-e.StartMs)*time.Millisecond >= length-time.Second {
				return e.Path, nil
			}
			if short != nil {
				return "", tooShort(e)
			}
			if o.Say != nil {
				o.Say("Segmentul s-a închis după %d s, mai devreme decât cele %s ale probei: aștept următorul segment.",
					(e.EndMs-e.StartMs+500)/1000, durationText(o.SegmentSecs))
			}
			short = &e
			timer.Reset(closeWait)
		}
	}
}

// durationText is secs in Romanian: «5 min», «2 min», «10 s».
func durationText(secs int) string {
	if secs%60 == 0 {
		return fmt.Sprintf("%d min", secs/60)
	}
	return fmt.Sprintf("%d s", secs)
}

// ReadIdentity reads the RI_g identity in the file at path.
func ReadIdentity(path string) (age.Identity, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the recording's own identity file
	if err != nil {
		return nil, err
	}
	return age.ParseX25519Identity(strings.TrimSpace(string(b)))
}

// Runs is the folder of runs of the probe whose code, in lower case, is
// probe ("mi4", "mi9") under recspike's work folder work: work/<probe>.
// MI9 plays the 5-min segment (FiveMin) that MI4 left in Runs(work, "mi4")
// on the Pi.
func Runs(work, probe string) Work {
	return Work{Root: filepath.Join(work, probe), Probe: strings.ToUpper(probe)}
}

// Work is a probe's folder of runs under recspike's work folder.
type Work struct {
	// Root is the probe's folder, work/<probe>.
	Root string
	// Probe is the probe's code, MI4 or MI9, as the owner reads it.
	Probe string
	// RemoveAll removes a recording or an identity file, nil for
	// os.RemoveAll; tests replace it.
	RemoveAll func(string) error
}

// Start removes the recordings of the probe's runs before, which would only
// take space and whose identities decrypt nothing any more (their logs
// stay, each run in its own folder), and makes the folder of a new run,
// named by now.
func (w Work) Start(now time.Time) (string, error) {
	if err := w.RemoveRecordings(); err != nil {
		return "", fmt.Errorf("nu pot șterge înregistrările probelor %s de dinainte: %w", w.Probe, err)
	}
	dir := filepath.Join(w.Root, now.UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("nu pot crea dosarul de lucru: %w", err)
	}
	return dir, nil
}

// End removes the run's recordings and their identities, however the run
// ended (PH0-22); a run defers it with its named error, err. When they
// cannot be removed, say (nil for no screen) tells the owner, in Romanian,
// which folder to delete by hand, and a run that would otherwise succeed
// fails with the removal's error; a failed run keeps its own.
func (w Work) End(err *error, say func(format string, args ...any)) {
	rmErr := w.RemoveRecordings()
	if rmErr == nil {
		return
	}
	if say != nil {
		say("Nu am putut șterge înregistrarea probei și cheia ei: ștergeți de mână dosarul %s.", w.Root)
	}
	if *err == nil {
		*err = fmt.Errorf("nu am putut șterge înregistrarea și cheia ei din %s: %w", w.Root, rmErr)
	}
}

// RemoveRecordings removes every run's recordings, <run>/<name>/data, with
// RI_g's identity file beside each, and keeps the rest: the logs.
func (w Work) RemoveRecordings() error {
	removeAll := w.RemoveAll
	if removeAll == nil {
		removeAll = os.RemoveAll
	}
	var errs []error
	for _, name := range []string{RecordingDir, IdentityFile} {
		paths, err := filepath.Glob(filepath.Join(w.Root, "*", "*", name))
		if err != nil {
			return err
		}
		for _, path := range paths {
			errs = append(errs, removeAll(path))
		}
	}
	return errors.Join(errs...)
}

// Latest is the recording name of the newest run that still has one, with
// its identity: the first segment that it closed. A probe removes its
// recordings when its run ends, so one is left only when that removal
// failed.
func (w Work) Latest(name string) (Segment, bool) {
	keys, err := filepath.Glob(filepath.Join(w.Root, "*", name, IdentityFile))
	if err != nil {
		return Segment{}, false
	}
	slices.Sort(keys) // the runs' folders are named by their start
	for _, key := range slices.Backward(keys) {
		var segments []string
		_ = filepath.WalkDir(filepath.Join(filepath.Dir(key), RecordingDir), func(path string, _ fs.DirEntry, err error) error {
			if err == nil && strings.HasSuffix(path, ".mp4.age") {
				segments = append(segments, path)
			}
			return nil
		})
		if len(segments) > 0 {
			slices.Sort(segments) // MediaMTX names a segment by its start: the first closed one
			return Segment{Path: segments[0], Identity: key}, true
		}
	}
	return Segment{}, false
}
