// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"context"
	"errors"
	"flag"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/cli"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// holdEnd is why a holder stopped holding.
type holdEnd int

const (
	holdSignal holdEnd = iota
	holdHandover
	holdNoRun
)

// runResume is the autostart's command (Self-healing autostart): it runs
// for the whole test. It waits until the run state has an open run, takes
// hub.lock (retrying every 5 s while another process holds it, and
// yielding to another live process's valid claim), waits for a previous
// holder's hub to end, and runs the hub with the open runs' composition.
// After a handover it gives the hub back and waits again; when no open run
// is left, it stops the hub and waits. A signal ends it; the autostart
// starts it again.
func runResume(ctx context.Context, e *env, args []string) error {
	if err := cli.ParseFlags(flag.NewFlagSet("resume", flag.ContinueOnError), args, e.out, usage); err != nil {
		return err
	}
	ctx, stop := signalContext(ctx)
	defer stop()
	told := false
	for ctx.Err() == nil {
		if !hasOpenRun(e) {
			if !told {
				e.printf(msgResumeWaiting)
				told = true
			}
			sleep(ctx, e.poll())
			continue
		}
		h, ok := takeForResume(ctx, e)
		if !ok {
			sleep(ctx, e.poll())
			continue
		}
		told = false
		if err := h.startHub(ctx); err != nil {
			h.giveBack()
			e.printf("%s %v", msgError, err)
			if errors.Is(err, hubproc.ErrSdCardUnconfirmed) || errors.Is(err, hubproc.ErrNoSpikeUser) {
				sleep(ctx, 30*time.Second)
			}
			continue
		}
		if err := h.serve(ctx); err != nil {
			h.giveBack()
			e.printf("%s %v", msgError, err)
			continue
		}
		switch h.hold(ctx) {
		case holdHandover:
			h.handBack(ctx)
		default:
			h.giveBack()
		}
	}
	return nil
}

// takeForResume takes hub.lock for resume, unless another process holds
// it or another live process's claim is valid (resume never overtakes a
// command that asked for the hub), and waits for the previous hub to end.
func takeForResume(ctx context.Context, e *env) (*holder, bool) {
	release, err := runstate.Lock(e.hubLock())
	if err != nil {
		return nil, false
	}
	st, err := runstate.Load(e.statePath())
	if err != nil || st.Claim.Valid(e.now()) && !st.Claim.Mine() {
		release()
		return nil, false
	}
	h := &holder{e: e, command: "uploadspike resume", release: release}
	if err := hubproc.WaitGone(ctx, e.pidFile(), e.goneGrace()); err != nil {
		release()
		return nil, false
	}
	if !hasOpenRun(e) {
		release()
		return nil, false
	}
	return h, true
}

// hasOpenRun reports whether the run state holds an open run.
func hasOpenRun(e *env) bool {
	st, err := runstate.Load(e.statePath())
	return err == nil && len(st.Open) > 0
}

// sleep waits d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// goneGrace is how long a new holder waits for the previous holder's hub
// before it kills it: 60 s, 3 s in the tests.
func (e *env) goneGrace() time.Duration {
	if e.fast {
		return 3 * time.Second
	}
	return waitGoneGrace
}

// heartbeatEvery is the heartbeat's period: a minute, a second in the tests.
func (e *env) heartbeatEvery() time.Duration {
	if e.fast {
		return time.Second
	}
	return heartbeatTick
}

// hold runs the open runs' hub until a signal, a handover, or no open run
// is left: a heartbeat every minute feeds the run state's gaps (P49), a
// hub that ends on its own restarts with a hub-crash gap, and MI6 closes
// at day 7.
func (h *holder) hold(ctx context.Context) holdEnd {
	tick := time.NewTicker(h.e.heartbeatEvery())
	defer tick.Stop()
	h.heartbeat()
	for {
		select {
		case <-ctx.Done():
			return holdSignal
		case <-h.handoverRequested():
			return holdHandover
		case <-h.gone():
			if h.isStopping() {
				return holdNoRun
			}
			h.restartAfterCrash(ctx)
		case <-tick.C:
			h.heartbeat()
			h.closeDueMI6(ctx)
			if !hasOpenRun(h.e) {
				return holdNoRun
			}
		}
	}
}

// closeDueMI6 closes the open MI6 run at day 7 (Close): its envelope, then
// the known-secrets scan with this process's control tokens; the run
// leaves the run state.
func (h *holder) closeDueMI6(ctx context.Context) {
	st, err := runstate.Load(h.e.statePath())
	if err != nil {
		return
	}
	raw, ok := st.Open["MI6"]
	if !ok {
		return
	}
	run, err := mi6.Decode(raw)
	if err != nil || h.e.now().Before(run.CloseAt()) {
		return
	}
	host, info := hostName(ctx, h.e)
	if err := writeMI6(ctx, h.e, run, st, host, info); err != nil {
		h.e.printf("%s %v", msgError, err)
		return
	}
	if err := scanResults(ctx, h.e, h.tokens); err != nil {
		h.e.printf("%s", err)
		failMI6(h.e, err)
		if st, err := runstate.Load(h.e.statePath()); err == nil {
			if run, err := mi6.Decode(st.Open["MI6"]); err == nil {
				_ = writeMI6(ctx, h.e, run, st, host, info)
			}
		}
	}
	_ = runstate.Update(h.e.statePath(), func(s *runstate.State) error {
		delete(s.Open, "MI6")
		return nil
	})
	h.e.printf(msgMI6Closed)
}
