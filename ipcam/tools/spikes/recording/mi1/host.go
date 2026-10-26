// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi1

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hostinfo"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/lastindex"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The pseudonyms of the hosts (PH0-22).
const (
	hostPC         = "pc"
	hostPi         = "pi"
	hostLabLinux   = "lab-linux"
	hostLabWindows = "lab-windows"
	hostLabQEMU    = "lab-qemu"
)

// SetFor is the owner's set for a host and a kind (G10): pc-kill9,
// pc-bugcheck, pi-kill9, pi-sysrq, or, for real cuts, real-pc and real-pi.
func SetFor(host, kind string, realCuts bool) (string, error) {
	switch {
	case realCuts && host == hostPC:
		return SetRealPC, nil
	case realCuts && host == hostPi:
		return SetRealPi, nil
	case host == hostPC && kind == "kill9":
		return SetPCKill9, nil
	case host == hostPC && kind == "bugcheck":
		return SetPCBugcheck, nil
	case host == hostPi && kind == "kill9":
		return SetPiKill9, nil
	case host == hostPi && kind == "sysrq":
		return SetPiSysrq, nil
	}
	return "", fmt.Errorf("gazda %s nu are proba automată %q: pe PC kill9 sau bugcheck, pe Pi kill9 sau sysrq", host, kind)
}

// LabSet is the lab's set for the runner's OS and a kind: lab-linux-kill9,
// lab-windows-kill9 or lab-linux-qemu-reset.
func LabSet(goos, kind string) string {
	switch {
	case kind == "qemu-reset":
		return SetLabLinuxQEMUReset
	case goos == "windows":
		return SetLabWindowsKill9
	}
	return SetLabLinuxKill9
}

// LabHost is the lab host for the runner's OS, as its envelopes name it.
func LabHost(goos string) HostState {
	h := HostState{Pseudonym: hostLabLinux, OS: goos, Storage: "unknown", WriteCache: "unknown", Cameras: 1}
	if goos == "windows" {
		h.Pseudonym = hostLabWindows
	}
	return h
}

// OwnerHost reads the owner's host (pc or pi, by OS) with ipcam-hub diag
// host, whose Pi model may need the device tree or the owner's camera count
// (C24, G32), and, when rec/ is on an SD card, asks for the owner's
// confirmation of T40's warning (G16).
func OwnerHost(ctx context.Context, hub, recDir, goos string, in *bufio.Reader, out io.Writer) (HostState, error) {
	info, err := hostinfo.Read(ctx, hostinfo.Options{Hub: hub, RecDir: recDir, In: in, Out: out})
	if err != nil {
		return HostState{}, err
	}
	h := HostState{Pseudonym: hostPC, OS: goos, Storage: info.Storage, WriteCache: info.WriteCache, PiModel: info.PiModel, Cameras: info.RecommendedCameras}
	if goos != "windows" {
		h.Pseudonym = hostPi
	}
	if h.Storage == "sd" {
		if h.SdConfirmation, err = ConfirmSD(in, out, time.Now); err != nil {
			return HostState{}, err
		}
	}
	return h, nil
}

// record records one run: it starts ipcam-hub with the run's beacon, tells
// the controller that it is ready, stops the host when the controller says
// (G09, C16), and finishes and reports the run; a sysrq or bugcheck run is
// finished after the reboot, by Resume. A lab smoke run's id names its own
// results set (ResultsSet), never one of the set's runs.
func (d *Driver) record(ctx context.Context, s *session, st *RunState, p RunPlan, attempt int) error {
	cur := &CurrentRun{
		RunID: fmt.Sprintf("%s-%02d-%d", ResultsSet(st.Set, st.Smoke), p.Index, attempt), Index: p.Index, Attempt: attempt,
		Encrypted: p.Encrypted, PadLowBitrate: p.PadLowBitrate, Streams: p.Streams, Phase: PhaseRecording,
	}
	if st.Mode == ModeReal {
		cur.Slot = p.Index
	}
	cur.trace = &cutTrace{}
	vol, err := d.volumes().Prepare(ctx, cur.RunID, cur.Encrypted)
	if err != nil {
		return err
	}
	config, err := d.writeRun(cur, vol, s)
	if err != nil {
		return err
	}
	st.Current, st.NextRun, st.Slot = cur, p.Index, cur.Slot
	st.Attempts++
	if err := SaveRunState(d.statePath(), st); err != nil {
		return err
	}
	if err := d.announce(ctx, s, st, cur, false); err != nil {
		return err
	}
	d.say("Rularea %s (%s) începe.", cur.RunID, encryptionText(cur.Encrypted))
	log, err := d.openLog(cur)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	hub, err := hubproc.Start(hubproc.Options{Binary: d.Hub, Config: config, Stderr: log, OnEvent: cur.trace.event,
		Beacon: &hubproc.Beacon{Addr: s.beaconAddr, RunID: cur.RunID}})
	if err != nil {
		return err
	}
	readyCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	_, err = hub.WaitEvent(readyCtx, "ready", nil)
	cancel()
	if err != nil {
		_, _ = hub.Kill()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ipcam-hub nu a pornit rularea %s: %w (jurnalul: %s)", cur.RunID, err, filepath.Join(d.runDir(cur.RunID), "ipcam-hub.log"))
	}
	if err := d.announce(ctx, s, st, cur, true); err != nil {
		stopHub(hub)
		return err
	}
	return d.awaitCut(ctx, s, st, cur, vol, hub)
}

// runStart is the announcement of cur to the controller.
func runStart(st *RunState, cur *CurrentRun) link.RunStart {
	return link.RunStart{
		RunID: cur.RunID, Set: st.Set, Kind: st.Kind, Encrypted: cur.Encrypted, PadLowBitrate: cur.PadLowBitrate, Streams: cur.Streams,
		Host: st.Host.Pseudonym, OS: st.Host.OS, Storage: st.Host.Storage, PiModel: st.Host.PiModel, SdConfirmation: st.Host.SdConfirmation,
		Slot: cur.Slot, Attempt: cur.Attempt, Smoke: st.Smoke,
	}
}

// announce tells the controller of cur: its run_start, or its ready once
// ipcam-hub is ready. A link that is down, or that drops as recspike
// sends, is connected again in an unattended set (deliver), and the new
// link hears the whole announcement, run_start then ready: the old link may
// have lost the run_start too. The controller keeps a run that it knows,
// with its schedule and its cut, and moves it to the new link.
func (d *Driver) announce(ctx context.Context, s *session, st *RunState, cur *CurrentRun, ready bool) error {
	start := runStart(st, cur)
	if !ready {
		return d.deliver(ctx, s, st, []link.Message{start}, []link.Message{start})
	}
	return d.deliver(ctx, s, st, []link.Message{link.Ready{RunID: cur.RunID}}, []link.Message{start, link.Ready{RunID: cur.RunID}})
}

// deliver sends msgs to the controller, in order. In an unattended set, a
// link that is down, or that drops as recspike sends, is connected again,
// at most maxReconnects times, and the new link hears again: what the old
// one may have lost. A real set's drop ends it, as before (reconnect).
func (d *Driver) deliver(ctx context.Context, s *session, st *RunState, msgs, again []link.Message) error {
	for reconnects := 0; ; reconnects++ {
		err := errLinkDown
		if s.up() {
			err = s.sendAll(msgs)
		}
		if err == nil || !errors.Is(err, errLinkDown) || reconnects == maxReconnects {
			return err
		}
		if err := d.reconnect(ctx, s, st, err); err != nil {
			return err
		}
		msgs = again
	}
}

// awaitCut waits for the controller's cut of cur and stops the host as its
// kind says. A cut or a refusal of another run, one that has ended, is
// ignored: it never stops this run (C16). In an unattended set, a link that
// drops meanwhile is connected again, and the run announced again on it,
// run_start then ready: the controller delivers the cut there, the same
// cut if it went out on the old link.
func (d *Driver) awaitCut(ctx context.Context, s *session, st *RunState, cur *CurrentRun, vol Volume, hub *hubproc.Hub) error {
	for reconnects := 0; ; {
		var m link.Message
		select {
		case <-ctx.Done():
			return ctx.Err() // a power cut, or the owner's Ctrl+C: ipcam-hub stops with recspike
		case <-hub.Exited():
			d.say("ipcam-hub s-a oprit singur în timpul rulării %s; rularea se reface.", cur.RunID)
			return d.afterStop(ctx, s, st, cur, vol, hub)
		case r, ok := <-s.in:
			if ok && r.err == nil {
				m = r.m
				break
			}
			cause := errLinkDown
			if ok {
				cause = r.err
			}
			if reconnects++; reconnects > maxReconnects {
				stopHub(hub)
				return cause
			}
			if err := d.reconnect(ctx, s, st, cause); err != nil {
				stopHub(hub)
				return err
			}
			start := runStart(st, cur)
			whole := []link.Message{start, link.Ready{RunID: cur.RunID}}
			if err := d.deliver(ctx, s, st, whole, whole); err != nil {
				stopHub(hub)
				return err
			}
			continue
		}
		switch m := m.(type) {
		case link.Refused:
			if m.RunID != cur.RunID {
				d.say("Controlorul refuză rularea %s, care nu este cea în curs (%s); nu țin seama.", m.RunID, cur.RunID)
				continue
			}
			d.say("Controlorul refuză rularea %s: %s", cur.RunID, m.Reason)
			stopHub(hub)
			return errors.New(m.Reason)
		case link.Cut:
			if m.RunID != cur.RunID {
				d.say("Controlorul a cerut oprirea rulării %s, care nu este cea în curs (%s); nu opresc gazda.", m.RunID, cur.RunID)
				continue
			}
			return d.stop(ctx, s, st, cur, vol, hub)
		default:
			stopHub(hub)
			return fmt.Errorf("controlorul a trimis %s în locul opririi", m.Type())
		}
	}
}

// stopHub stops ipcam-hub cleanly when a run is abandoned, killing it after
// stopTimeout.
func stopHub(hub *hubproc.Hub) {
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	_ = hub.Stop(ctx)
}

// stop stops the host for the cut of cur.
func (d *Driver) stop(ctx context.Context, s *session, st *RunState, cur *CurrentRun, vol Volume, hub *hubproc.Hub) error {
	kind := controller.ShutdownKind(st.Kind)
	if !StopAllowed(st.Mode, d.window(st), kind, d.clock().Now()) {
		return d.refuse(ctx, s, st, cur, vol, hub, "oprire automată cerută în afara ferestrei "+st.Window)
	}
	switch kind {
	case controller.KindPower, controller.KindForced:
		d.say(">>> %s <<<", RealCutPrompt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-hub.Exited():
		case <-time.After(realCutWait):
			d.say("Alimentarea nu a fost tăiată în 2 minute: tăierea %d se reface.", cur.Slot)
			stopHub(hub)
		}
		return d.afterStop(ctx, s, st, cur, vol, hub)
	case controller.KindKill9:
		killedAt, err := hub.Kill()
		if err != nil && !errors.Is(err, hubproc.ErrExited) {
			return err
		}
		if !killedAt.IsZero() && cur.trace != nil {
			logSize := int64(-1)
			if info, err := os.Stat(filepath.Join(d.runDir(cur.RunID), "ipcam-hub.log")); err == nil {
				logSize = info.Size()
			}
			cur.trace.kill(killedAt, logSize)
		}
		if err := d.listBefore(cur, vol); err != nil {
			return err
		}
		cur.Phase = PhaseStopped
		if err := SaveRunState(d.statePath(), st); err != nil {
			return err
		}
		again, err := hub.Restart()
		if err != nil {
			return err
		}
		if !killedAt.IsZero() {
			resumeCtx, cancel := context.WithTimeout(ctx, readyTimeout)
			ms, err := again.ResumeMs(resumeCtx, killedAt)
			cancel()
			if err == nil {
				cur.ResumeMs = &ms
			}
		}
		return d.finish(ctx, s, st, cur, vol, again, nil)
	}
	cur.Phase = PhaseStopping
	if boot, err := d.thisBoot(); err == nil {
		cur.StopBoot = &boot
	}
	if err := SaveRunState(d.statePath(), st); err != nil {
		return err
	}
	d.say("Gazda se oprește acum (%s).", kind)
	if err := d.osStop(ctx, kind); err != nil {
		stopHub(hub)
		return fmt.Errorf("oprirea %s nu a pornit: %w", kind, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(hostDownWait):
		return fmt.Errorf("gazda nu s-a oprit în %v după oprirea %s", hostDownWait, kind)
	}
}

// refuse ends a run without its stop: ipcam-hub stops cleanly, the
// controller hears why, and the run is redone.
func (d *Driver) refuse(ctx context.Context, s *session, st *RunState, cur *CurrentRun, vol Volume, hub *hubproc.Hub, reason string) error {
	d.say("Rularea %s: %s; nu opresc gazda, rularea se reface.", cur.RunID, reason)
	stopCtx, cancel := context.WithTimeout(ctx, stopTimeout)
	err := hub.Stop(stopCtx)
	cancel()
	if err != nil {
		return err
	}
	return d.dropRun(ctx, s, st, cur, vol, reason)
}

// dropRun ends a run that recspike will not report, with ipcam-hub
// stopped: the controller hears why, and the run is done as invalid for
// that reason, to be redone.
func (d *Driver) dropRun(ctx context.Context, s *session, st *RunState, cur *CurrentRun, vol Volume, reason string) error {
	refused := []link.Message{link.Refused{RunID: cur.RunID, Reason: reason}}
	if err := d.deliver(ctx, s, st, refused, refused); err != nil {
		return err
	}
	return d.done(st, cur, vol, results.Run{RunID: cur.RunID, InvalidReason: reason, Slot: cur.Slot, Attempt: cur.Attempt,
		ShutdownKind: st.Kind, Encrypted: cur.Encrypted, PadLowBitrate: cur.PadLowBitrate})
}

// finishAfterReboot finishes the run that a reboot, or an end of recspike,
// left: with ipcam-hub killed, it reads the files listed then; otherwise
// it lists the files now, before ipcam-hub starts again on the run's
// run.json, so that ipcam-hub reconciles them and records again (C07).
// An OS stop after which the host did not reboot ends its run as invalid.
func (d *Driver) finishAfterReboot(ctx context.Context, s *session, st *RunState) error {
	cur := st.Current
	vol, err := d.volumes().Reopen(ctx, cur.RunID, cur.Encrypted)
	if err != nil {
		return fmt.Errorf("rularea %s nu mai are dosarul de date: %w", cur.RunID, err)
	}
	// The host booted since the OS stop that recspike asked for: ColdBootMs
	// is measured on its boot clock.
	booted := cur.Phase == PhaseStopping
	if booted && !d.rebootedSince(cur) {
		return d.notRebooted(ctx, s, st, cur, vol)
	}
	if cur.Phase != PhaseStopped {
		if err := d.listBefore(cur, vol); err != nil {
			return err
		}
		cur.Phase = PhaseStopped
		if err := SaveRunState(d.statePath(), st); err != nil {
			return err
		}
	}
	log, err := d.openLog(cur)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	hub, err := hubproc.Start(hubproc.Options{Binary: d.Hub, Config: filepath.Join(d.runDir(cur.RunID), "run.json"), Stderr: log})
	if err != nil {
		return err
	}
	kind := controller.ShutdownKind(st.Kind)
	coldBoot := booted && (kind == controller.KindSysrq || kind == controller.KindBugcheck)
	var coldBootMs *int64
	if coldBoot {
		waitCtx, cancel := context.WithTimeout(ctx, eventTimeout)
		last, err := hub.WaitRecording(waitCtx)
		cancel()
		if err == nil {
			if up, err := d.uptime(); err == nil {
				ms := (up - time.Since(last)).Milliseconds()
				coldBootMs = &ms
			}
		}
	}
	return d.finish(ctx, s, st, cur, vol, hub, coldBootMs)
}

// rebootedSince reports whether the host booted after recspike asked the
// OS to stop for cur: its boot is not the stop's (Boot). A boot that is not
// known, at the stop or now, is no reboot.
func (d *Driver) rebootedSince(cur *CurrentRun) bool {
	boot, err := d.thisBoot()
	return err == nil && cur.StopBoot != nil && !boot.same(*cur.StopBoot)
}

// notRebooted ends a run whose OS stop did not reboot the host (the stop
// failed, or the host never went down): it is invalid, with no ColdBootMs
// and no report; the controller hears why, and the run is redone.
func (d *Driver) notRebooted(ctx context.Context, s *session, st *RunState, cur *CurrentRun, vol Volume) error {
	reason := fmt.Sprintf("gazda nu a repornit după oprirea %s", st.Kind)
	d.say("Rularea %s: %s; rularea se reface.", cur.RunID, reason)
	return d.dropRun(ctx, s, st, cur, vol, reason)
}

// afterStop finishes a run whose ipcam-hub stopped without a kill: the
// owner's cut that did not come, or ipcam-hub's own exit.
func (d *Driver) afterStop(ctx context.Context, s *session, st *RunState, cur *CurrentRun, vol Volume, hub *hubproc.Hub) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	<-hub.Exited()
	if err := d.listBefore(cur, vol); err != nil {
		return err
	}
	cur.Phase = PhaseStopped
	if err := SaveRunState(d.statePath(), st); err != nil {
		return err
	}
	again, err := hub.Restart()
	if err != nil {
		return err
	}
	return d.finish(ctx, s, st, cur, vol, again, nil)
}

// finish is the end of every run after its stop, with ipcam-hub running
// again (G09): the NTP wait, ipcam-hub's reconcile and verify, the last
// recorded index of each stream from the files of the stop, HUB-04, the
// clean stop of ipcam-hub, and the report, whose verdict says whether the
// run counts.
func (d *Driver) finish(ctx context.Context, s *session, st *RunState, cur *CurrentRun, vol Volume, hub *hubproc.Hub, coldBootMs *int64) error {
	defer stopHub(hub)
	ntpMs, synced := WaitNTP(ctx, d.ntp(), d.clock())
	if ctx.Err() != nil {
		return ctx.Err()
	}
	waitCtx, cancel := context.WithTimeout(ctx, eventTimeout)
	defer cancel()
	reconcile, rerr := hub.WaitEvent(waitCtx, "reconcile", func(e hubproc.Event) bool { return e.Final || e.Aborted })
	verify, verr := hub.WaitEvent(waitCtx, "verify", func(e hubproc.Event) bool { return !e.At.Before(reconcile.At) })
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if rerr != nil || verr != nil {
		d.say("Rularea %s: ipcam-hub nu a raportat reconcilierea și verificarea (%v).", cur.RunID, errors.Join(rerr, verr))
	}
	id, err := d.identity(cur)
	if err != nil {
		return err
	}
	report := link.Report{RunID: cur.RunID, Reconcile: reconcile.Line, Verify: verify.Line, ResumeMs: cur.ResumeMs, ColdBootMs: coldBootMs,
		NtpWaitMs: ntpMs, NtpSynced: synced, WriteCache: st.Host.WriteCache}
	for _, stream := range cur.Streams {
		var files lastindex.Before
		for _, rel := range cur.Before[stream] {
			files = append(files, filepath.Join(vol.DataDir(), filepath.FromSlash(rel)))
		}
		index, err := d.lastRecorded(ctx, files, id)
		if err != nil {
			d.say("Rularea %s: ultimul cadru înregistrat al %s nu se poate citi: %v", cur.RunID, stream, err)
			continue
		}
		report.Streams = append(report.Streams, link.StreamIndex{Stream: stream, LastRecordedIndex: index})
	}
	report.Hub04Clear = d.hub04(cur, vol, s.info.Needles)
	stopCtx, stopCancel := context.WithTimeout(ctx, stopTimeout)
	err = hub.Stop(stopCtx)
	stopCancel()
	if err != nil {
		return err
	}
	v, err := d.report(ctx, s, st, report)
	if err != nil {
		return err
	}
	// A lab run explains a stream that lost more than its limit (Task 7.2d).
	if st.Mode == ModeLab {
		for _, line := range d.lossDiags(cur, vol, v, id) {
			d.say("%s", line)
		}
	}
	return d.done(st, cur, vol, v.Run)
}

// report sends the run's report and returns its verdict. In an unattended
// set, a link that drops meanwhile is connected again and the report sent
// again: the controller answers a report sent again with the same verdict.
func (d *Driver) report(ctx context.Context, s *session, st *RunState, r link.Report) (link.Verdict, error) {
	for reconnects := 0; ; reconnects++ {
		v, err := s.report(ctx, r)
		if err == nil || !errors.Is(err, errLinkDown) || reconnects == maxReconnects {
			return v, err
		}
		if err := d.reconnect(ctx, s, st, err); err != nil {
			return link.Verdict{}, err
		}
	}
}

// done records the run's verdict in the run state and frees its folder.
func (d *Driver) done(st *RunState, cur *CurrentRun, vol Volume, run results.Run) error {
	st.Done = append(st.Done, DoneRun{Index: cur.Index, Run: run})
	st.Current = nil
	if err := SaveRunState(d.statePath(), st); err != nil {
		return err
	}
	if Counts(run) {
		d.say("Rularea %s: validă, pierdere %s.", run.RunID, secondsText(RunLossMs(run)))
	} else {
		d.say("Rularea %s: nu contează (%s); se reface.", run.RunID, invalidText(run))
	}
	return errors.Join(vol.Release(), os.RemoveAll(d.runDir(cur.RunID)))
}

// invalidText says why a run does not count.
func invalidText(r results.Run) string {
	switch {
	case r.InvalidReason != "":
		return r.InvalidReason
	case r.PacingP99Ms > MaxPacingP99Ms || r.SkippedAUs > 0:
		return "controlorul a trimis neuniform"
	}
	return "oprire nevalidă"
}

// hub04 scans the run's data directory (and its own device, if it has one)
// for clear video in the encrypted streams' files, tmp/ and the run
// directory (G08).
func (d *Driver) hub04(cur *CurrentRun, vol Volume, needles [][]byte) bool {
	var skip []string
	if !cur.Encrypted {
		for _, stream := range cur.Streams {
			cam, profile := jobOf(stream)
			skip = append(skip, filepath.Join("rec", cam, profile))
		}
	}
	found, err := ScanHub04(vol.DataDir(), skip, needles)
	if err != nil {
		d.say("Rularea %s: verificarea HUB-04 a eșuat: %v", cur.RunID, err)
		return false
	}
	raw, err := vol.ScanRaw(needles)
	if err != nil {
		d.say("Rularea %s: verificarea HUB-04 a discului a eșuat: %v", cur.RunID, err)
		return false
	}
	for _, f := range append(found, raw...) {
		d.say("Rularea %s: HUB-04: %s în clar în %s, la %d.", cur.RunID, f.What, f.Path, f.Offset)
	}
	return len(found) == 0 && len(raw) == 0
}

// jobOf is the ipcam-hub job of a stream: cam01_main is cam01, main.
func jobOf(stream string) (cam, profile string) {
	cam, profile, _ = strings.Cut(stream, "_")
	return cam, profile
}

// writeRun writes the run's identity and run.json: each stream is a job
// that pulls the controller's camera, encrypted to a fresh RI_g and RK
// when the run is (C11, PH0-22); RK's identity is never kept.
func (d *Driver) writeRun(cur *CurrentRun, vol Volume, s *session) (string, error) {
	dir := d.runDir(cur.RunID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// The owner's confirmation of the SD-card warning lets ipcam-hub record
	// on the card (G16).
	cfg := hubproc.Config{DataDir: vol.DataDir(), MediaMTX: d.MediaMTX, SDCardConfirmed: d.Host.SdConfirmation != nil}
	var recipients []string
	if cur.Encrypted {
		riG, err := age.GenerateX25519Identity()
		if err != nil {
			return "", err
		}
		rk, err := age.GenerateX25519Identity()
		if err != nil {
			return "", err
		}
		cfg.IdentityFile = filepath.Join(dir, "ri_g.key")
		if err := os.WriteFile(cfg.IdentityFile, []byte(riG.String()+"\n"), 0o600); err != nil {
			return "", err
		}
		recipients = []string{riG.Recipient().String(), rk.Recipient().String()}
	}
	base, err := url.Parse(s.info.RTSPBase)
	if err != nil {
		return "", fmt.Errorf("controlorul a trimis o adresă RTSP greșită: %w", err)
	}
	for _, stream := range cur.Streams {
		cam, profile := jobOf(stream)
		u := url.URL{Scheme: base.Scheme, User: url.UserPassword(s.info.User, s.info.Pass), Host: base.Host, Path: "/" + stream}
		pad := cur.PadLowBitrate
		cfg.Jobs = append(cfg.Jobs, hubproc.Job{Cam: cam, Profile: profile, RestreamURL: u.String(), Recipients: recipients, PadLowBitrate: &pad})
	}
	config := filepath.Join(dir, "run.json")
	return config, hubproc.WriteConfig(config, cfg)
}

// identity reads the run's RI_g; nil for a clear run.
func (d *Driver) identity(cur *CurrentRun) (age.Identity, error) {
	if !cur.Encrypted {
		return nil, nil
	}
	f, err := os.Open(filepath.Join(d.runDir(cur.RunID), "ri_g.key"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	ids, err := age.ParseIdentities(f)
	if err != nil || len(ids) != 1 {
		return nil, fmt.Errorf("rularea %s nu are o identitate citibilă", cur.RunID)
	}
	return ids[0], nil
}

// openLog opens the run's ipcam-hub log, beside its run.json: ipcam-hub's
// errors and MediaMTX's lines, of every start of the run. It stays open
// until the run is finished, as each restart writes to it too.
func (d *Driver) openLog(cur *CurrentRun) (*os.File, error) {
	return os.OpenFile(filepath.Join(d.runDir(cur.RunID), "ipcam-hub.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the run's own folder
}

// segmentSuffixes are the names of the segment files that ipcam-hub's
// MediaMTX writes (MTX-1, MTX-2).
var segmentSuffixes = []string{".mp4", ".mp4.part", ".mp4.age", ".mp4.age.part"}

// listBefore lists each stream's segment files, relative to the data
// directory: lastindex.Before, the files of the stop.
func (d *Driver) listBefore(cur *CurrentRun, vol Volume) error {
	cur.Before = map[string][]string{}
	for _, stream := range cur.Streams {
		cam, profile := jobOf(stream)
		root := filepath.Join(vol.DataDir(), "rec", cam, profile)
		err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if e.Type().IsRegular() && slices.ContainsFunc(segmentSuffixes, func(s string) bool { return strings.HasSuffix(path, s) }) {
				rel, err := filepath.Rel(vol.DataDir(), path)
				if err != nil {
					return err
				}
				cur.Before[stream] = append(cur.Before[stream], filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
