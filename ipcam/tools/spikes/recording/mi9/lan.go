// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi9

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// LANSet is the set of MI9's LAN runs, one envelope per device as host.
const LANSet = "lan"

// The waits of a LAN run on the controller's side: its offer, and its
// measurement, a 5-min segment's plaintext and 20 seeks.
const (
	offerWait   = 2 * time.Minute
	measureWait = 20 * time.Minute
)

// LANOptions are what recspike's LAN run needs.
type LANOptions struct {
	// Controller is the link address of reccontroller on the other device,
	// which serves the camera and plays the recording.
	Controller string
	// Hub is ipcam-hub; MediaMTX the fork, "" for the one next to Hub.
	Hub, MediaMTX string
	// Results is recspike's published results folder; Work its private
	// one, which holds the recording, its key and the logs.
	Results, Work string
	// Out is the owner's screen.
	Out io.Writer
	// Host reads the host: diag host and, on an SD card, the owner's
	// confirmation (mi1.OwnerHost).
	Host func(ctx context.Context) (mi1.HostState, error)
}

// RunLAN is `recspike mi9 --lan` (G26 leg 3), on each of the owner's
// devices as host: it takes MI4's 5-min segment when the Pi still has one,
// else records one through ipcam-hub from the other device's cam01_main
// (SegmentSecs, encrypted); then this host serves it with the hub side's
// Pion peer, which binds UDP sockets here (R08's exception), and the other
// device's reccontroller plays it and measures the bulk channel over the
// LAN. It writes the envelope MI9/lan/<host>-mi9-<n>.json, sends it to the
// controller (G15), says goodbye (a Pi's results go to the PC then), and
// removes its recording and RI_g's identity (PH0-22). Every line it prints
// is Romanian.
func RunLAN(ctx context.Context, o LANOptions) (err error) {
	host, err := o.Host(ctx)
	if err != nil {
		return err
	}
	if m := host.PiModel; m != nil && m.Known && !m.Supported {
		return errors.New("acest Raspberry Pi nu este suportat (armv6): rulați proba MI9 doar pe PC, cu reccontroller-linux-armv6 pe Pi")
	}
	root := filepath.Join(o.Work, "mi9")
	if err := removeRecordings(root); err != nil {
		return fmt.Errorf("nu pot șterge înregistrările probelor MI9 de dinainte: %w", err)
	}
	dir := filepath.Join(root, time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("nu pot crea dosarul de lucru: %w", err)
	}
	defer func() {
		if rmErr := removeRecordings(root); rmErr != nil {
			o.say("Nu am putut șterge înregistrarea probei și cheia ei: ștergeți de mână dosarul %s.", root)
			if err == nil {
				err = fmt.Errorf("nu am putut șterge înregistrarea și cheia ei din %s: %w", root, rmErr)
			}
		}
	}()
	release, err := mi1.KeepAwake()
	if err != nil {
		return fmt.Errorf("nu pot ține dispozitivul treaz: %w", err)
	}
	defer release()
	l, err := mi1.OpenHostLink(ctx, o.Controller, host.Pseudonym)
	if err != nil {
		return err
	}
	ended := false
	defer func() {
		if !ended {
			_ = l.End(context.WithoutCancel(ctx), o.Results, nil)
		}
	}()
	rec, err := o.segment(ctx, host, l, dir)
	if err != nil {
		return err
	}
	r, err := o.play(ctx, l, rec, filepath.Base(dir))
	if err != nil {
		return fmt.Errorf("proba MI9 s-a oprit fără rezultat: %w", err)
	}
	runID, data, err := o.write(host, r)
	if err != nil {
		return err
	}
	if err := l.Send(link.Results{Name: strings.Join([]string{"MI9", LANSet, runID + ".json"}, "/"), Data: data}); err != nil {
		o.say("Rezultatul nu a ajuns la controlor (%v): îl aduce colectarea de la final (recspike push sau reccontroller pull).", err)
	}
	verdict := "trece"
	if pass, reasons := JudgeBulk(r); !pass {
		verdict = strings.Join(reasons, "; ")
	}
	o.say("Proba MI9 s-a încheiat: rezultatul este %s. Debit %s Mbps; primul cadru întreg după salt: max %s ms; RTT %s ms. Pe LAN: %s.",
		runID, mbps(r.ThroughputMbps), maxOf(r.SeekMs), ms(r.RTTMs), verdict)
	ended = true
	if err := l.End(ctx, o.Results, o.Out); err != nil && host.Pseudonym == "pi" {
		o.say("Rezultatul nu a ajuns la PC (%v): trimiteți-l la final cu recspike push.", err)
	}
	return nil
}

// segment is the recording that the run plays: on the Pi, MI4's 5-min
// segment when one is left; else one recorded now from the controller's
// camera into dir.
func (o LANOptions) segment(ctx context.Context, host mi1.HostState, l *mi1.HostLink, dir string) (Recording, error) {
	if host.Pseudonym == "pi" {
		if rec, ok := mi4Segment(o.Work); ok {
			o.say("Proba MI9 începe cu segmentul de 5 min al probei MI4 (%s).", rec.Path)
			return rec, nil
		}
	}
	url, err := l.CameraURL(Camera + "_" + Profile)
	if err != nil {
		return Recording{}, err
	}
	o.say("Proba MI9 începe: înregistrez un segment criptat de %d min de la camera cam01 a controlorului, apoi controlorul îl redă "+
		"prin canalul de date. Durează în jur de %d min; lăsați ambele dispozitive pornite și nefolosite până la final. Jurnalele sunt în %s.",
		SegmentSecs/60, SegmentSecs/60+5, dir)
	return Record(ctx, RecordOptions{Hub: o.Hub, MediaMTX: o.MediaMTX, CameraURL: url, Dir: filepath.Join(dir, "5min"),
		SegmentSecs: SegmentSecs, SDCardConfirmed: host.SdConfirmation != nil, Out: o.Out})
}

// mi4Segment is the 5-min segment that MI4 left under work, with its
// identity, the newest run's: MI4 removes it at its end, so one is left
// only when that removal failed. Its layout is mi4's (hubrig.go): work/mi4/
// <run>/5min/{data/…/*.mp4.age, ri_g.key}.
func mi4Segment(work string) (Recording, bool) {
	keys, err := filepath.Glob(filepath.Join(work, "mi4", "*", "5min", identityFile))
	if err != nil || len(keys) == 0 {
		return Recording{}, false
	}
	slices.Sort(keys) // the runs' folders are named by their start
	key := keys[len(keys)-1]
	var segments []string
	_ = filepath.WalkDir(filepath.Join(filepath.Dir(key), recordingDir), func(path string, _ fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, ".mp4.age") {
			segments = append(segments, path)
		}
		return nil
	})
	if len(segments) == 0 {
		return Recording{}, false
	}
	slices.Sort(segments) // MediaMTX names a segment by its start: the first closed one
	return Recording{Path: segments[0], Identity: key}, true
}

// play serves rec to the controller's playback peer, which measures it:
// the controller offers, the hub side here answers.
func (o LANOptions) play(ctx context.Context, l *mi1.HostLink, rec Recording, runID string) (BulkResult, error) {
	id, err := ReadIdentity(rec.Identity)
	if err != nil {
		return BulkResult{}, err
	}
	seg, err := OpenSegment(rec.Path, id)
	if err != nil {
		return BulkResult{}, fmt.Errorf("nu pot citi segmentul: %w", err)
	}
	defer func() { _ = seg.Close() }()
	api, err := NewAPI(LAN)
	if err != nil {
		return BulkResult{}, err
	}
	hub, err := NewHub(api, Configuration(LAN, nil), seg)
	if err != nil {
		return BulkResult{}, err
	}
	defer func() { _ = hub.Close() }()
	o.say("Controlorul redă acum segmentul prin canalul de date: tot segmentul, apoi %d de salturi.", Seeks)
	if err := l.Send(link.Mi9Start{RunID: runID}); err != nil {
		return BulkResult{}, err
	}
	m, err := l.Await(ctx, offerWait, ofRun(runID))
	if err != nil {
		return BulkResult{}, err
	}
	if refused, ok := m.(link.Refused); ok {
		return BulkResult{}, fmt.Errorf("controlorul a refuzat proba: %s", refused.Reason)
	}
	answer, err := hub.Answer(ctx, m.(link.Mi9Offer).Offer)
	if err != nil {
		return BulkResult{}, err
	}
	if err := l.Send(link.Mi9Answer{RunID: runID, Answer: answer}); err != nil {
		return BulkResult{}, err
	}
	if m, err = l.Await(ctx, measureWait, ofRun(runID)); err != nil {
		return BulkResult{}, err
	}
	switch m := m.(type) {
	case link.Mi9Result:
		return BulkResult{Transport: LAN, ThroughputMbps: m.ThroughputMbps, SeekMs: m.SeekMs, RTTMs: m.RTTMs}, nil
	case link.Refused:
		return BulkResult{}, fmt.Errorf("controlorul nu a putut reda segmentul: %s", m.Reason)
	}
	return BulkResult{}, fmt.Errorf("controlorul a trimis %s în loc de offer sau rezultat", m.Type())
}

// ofRun accepts the controller's offer, result or refusal of the run runID.
func ofRun(runID string) func(link.Message) bool {
	return func(m link.Message) bool {
		switch m := m.(type) {
		case link.Mi9Offer:
			return m.RunID == runID
		case link.Mi9Result:
			return m.RunID == runID
		case link.Refused:
			return m.RunID == runID
		}
		return false
	}
}

// write writes r as the host's lan envelope and returns its run id and its
// bytes, which go to the controller too.
func (o LANOptions) write(host mi1.HostState, r BulkResult) (string, []byte, error) {
	e := results.Envelope{Schema: results.Schema, Code: "MI9", Set: LANSet, Host: host.Pseudonym, OS: host.OS,
		PiModel: host.PiModel, Storage: host.Storage, SdConfirmation: host.SdConfirmation,
		MI9: &results.Mi9Data{Legs: []results.Mi9Leg{Leg(r, SourceLAN)}}}
	for n := 1; ; n++ {
		e.RunID = fmt.Sprintf("%s-mi9-%d", host.Pseudonym, n)
		err := results.Create(o.Results, e)
		switch {
		case errors.Is(err, results.ErrExists):
			continue
		case err != nil:
			return "", nil, fmt.Errorf("nu pot scrie rezultatul probei: %w", err)
		}
		data, err := json.Marshal(e)
		return e.RunID, append(data, '\n'), err
	}
}

func (o LANOptions) say(format string, args ...any) {
	if o.Out != nil {
		_, _ = fmt.Fprintf(o.Out, format+"\n", args...)
	}
}
