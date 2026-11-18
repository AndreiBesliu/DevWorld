// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// Phase is which composition the hub runs with: the encrypted pre-run's
// one job, the main run's clear jobs, or the final drain's no job, whose
// hub uploads what the stop finalised and records nothing new.
type Phase string

// The phases of a run.
const (
	PhasePreRun Phase = "prerun"
	PhaseMain   Phase = "main"
	PhaseFinal  Phase = "final"
)

// Host is the holder's side of a run (the device's one hub, Task 9):
// uploadspike holds the hub and its composition; Run orders the steps.
type Host interface {
	// StartHub starts the hub with the composition of phase and waits
	// for its ready event.
	StartHub(ctx context.Context, phase Phase) error
	// StopHub stops it cleanly; KillHub kills it, MediaMTX with it.
	StopHub(ctx context.Context) error
	KillHub() error
	// ResetConnections closes the hub's upload connections (Task 9).
	ResetConnections(ctx context.Context) error
	// Diag runs `ipcam-hub diag <args>` as the catalog's owner.
	Diag(ctx context.Context, args []string) ([]json.RawMessage, error)
	// HubConfig is the hub's process configuration, as the blocked
	// identity runs it (diag reach); RunJSON its run.json.
	HubConfig() hubproc.Config
	RunJSON() string
	// Printf prints one of the owner's lines.
	Printf(format string, args ...any)
}

// Timing is a run's durations: the owner's (OwnerTiming), the lab's
// (LabTiming) or a test's.
type Timing struct {
	// PreRun is the encrypted pre-run, its reads every PreRunEvery.
	PreRun, PreRunEvery time.Duration
	// WarmUp is the recording before the block.
	WarmUp time.Duration
	// CheckEvery is the block's check's period; ReadOpenEvery the
	// block's open-segment reads' (BlockFor / 24, at least every 15 min).
	CheckEvery, ReadOpenEvery time.Duration
	// Floor is how long after the block's start the hub's events must
	// show no upload: Block + 2 min.
	Floor time.Duration
	// Quiet is how long the final drain waits with nothing pending and
	// no new event before it ends; DrainMax bounds each drain.
	Quiet, DrainMax time.Duration
}

// OwnerTiming is the owner's run: 10 min of encrypted pre-run (a read
// every 30 s), 10 min of warm-up, checks every 10 min, reads every
// BlockFor / 24 and at least every 15 min, the floor at Block + 2 min.
func OwnerTiming(blockFor time.Duration) Timing {
	return Timing{PreRun: 10 * time.Minute, PreRunEvery: 30 * time.Second, WarmUp: 10 * time.Minute, CheckEvery: 10 * time.Minute,
		ReadOpenEvery: min(blockFor/24, 15*time.Minute), Floor: 2 * time.Minute, Quiet: 30 * time.Second, DrainMax: 12 * time.Hour}
}

// LabTiming is the lab's: the owner's but a 2-min warm-up (30-s segments)
// and a 2-h drain bound.
func LabTiming(blockFor time.Duration) Timing {
	t := OwnerTiming(blockFor)
	t.WarmUp, t.Quiet, t.DrainMax = 2*time.Minute, 15*time.Second, 2*time.Hour
	return t
}

// Run is MI5's order (Order of the run): the encrypted pre-run with its
// open-segment reads; the recording and warm-up; the block (RemoveStale,
// Block, reset-connections, the cleanup at start + BlockFor + 30 min),
// checked by VerifyBlocked before the clock starts and every CheckEvery,
// with the open-segment reads on cam02 and whether DNS still resolves;
// the unblock; the restart in the middle of an upload (P16); the drain
// until every placement of the run's own segments is verified, then the
// final drain without recording; OneDrive's quota after a delete; the
// audit. It returns the run's MI5 data; the caller writes the envelope.
// A failed check unblocks and aborts with the owner's line.
func Run(ctx context.Context, cfg Mi5Config, opts RunOptions) (*results.Mi5Data, error) {
	r := newRun(cfg, opts)
	go r.consume(opts.Events)
	defer r.stopConsuming()
	return r.run(ctx)
}

// RunOptions are what Run takes beside the settings: the holder, the
// block, the run's start and destinations, its timing and the hub's
// events across its starts.
type RunOptions struct {
	Host     Host
	Firewall WanBlock
	StartMs  int64
	Dests    []DestConfig
	Timing   Timing
	Events   <-chan hubproc.Event
	// Clock and Sleep are the tests' seams; nil: the system's.
	Clock func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	// Resolve resolves a provider host (DNS during the block); nil: the
	// system's resolver.
	Resolve func(ctx context.Context, host string) error
	// Verify is the block's check; nil: VerifyBlocked on the host's hub.
	Verify func(ctx context.Context) (ReachResult, error)
}

// run is one MI5 run's state.
type run struct {
	cfg  Mi5Config
	opts RunOptions
	host Host
	now  func() time.Time
	data *results.Mi5Data

	mu      sync.Mutex
	tally   *tally
	stopped chan struct{}
	done    chan struct{}
}

func newRun(cfg Mi5Config, opts RunOptions) *run {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Sleep == nil {
		opts.Sleep = sleepCtx
	}
	if opts.Resolve == nil {
		opts.Resolve = resolve
	}
	return &run{cfg: cfg, opts: opts, host: opts.Host, now: opts.Clock, data: &results.Mi5Data{Lab: cfg.Lab, Reach: []results.ReachCheck{},
		ReadOpen: []results.ReadOpenRun{}, Dests: []results.Mi5Dest{}}, tally: newTally(opts.StartMs, opts.Dests), stopped: make(chan struct{}),
		done: make(chan struct{})}
}

// sleepCtx waits d or until ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// resolve resolves host (without its port) through the system's resolver.
func resolve(ctx context.Context, host string) error {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := net.DefaultResolver.LookupHost(ctx, host)
	return err
}

// consume feeds the hub's events to the tally until the run ends.
func (r *run) consume(events <-chan hubproc.Event) {
	defer close(r.done)
	for {
		select {
		case <-r.stopped:
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			r.mu.Lock()
			r.tally.observe(ev)
			r.mu.Unlock()
		}
	}
}

func (r *run) stopConsuming() {
	close(r.stopped)
	<-r.done
}

// withTally runs fn on the tally under the lock.
func (r *run) withTally(fn func(t *tally)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r.tally)
}

// sleep waits d.
func (r *run) sleep(ctx context.Context, d time.Duration) error { return r.opts.Sleep(ctx, d) }

// run is Run's body.
func (r *run) run(ctx context.Context) (*results.Mi5Data, error) {
	t := r.opts.Timing
	fw := r.opts.Firewall
	if err := fw.RemoveStale(ctx); err != nil {
		return nil, err
	}
	// 1. The encrypted pre-run.
	if err := r.host.StartHub(ctx, PhasePreRun); err != nil {
		return nil, err
	}
	pre, err := r.readOpenSeries(ctx, true, r.cfg.eventCamera(), ReadOpenCalls, t.PreRunEvery, t.PreRun)
	if err != nil {
		return nil, err
	}
	r.data.ReadOpen = append(r.data.ReadOpen, pre)
	// 2. The recording and warm-up.
	if err := r.host.StopHub(ctx); err != nil {
		return nil, err
	}
	if err := r.host.StartHub(ctx, PhaseMain); err != nil {
		return nil, err
	}
	if err := r.sleep(ctx, t.WarmUp); err != nil {
		return nil, err
	}
	// 3. The block.
	if err := r.block(ctx); err != nil {
		return nil, err
	}
	// 4. The unblock, 5. the restart, 6. the drain.
	if err := r.restartMidUpload(ctx); err != nil {
		return nil, err
	}
	if err := r.drain(ctx); err != nil {
		return nil, err
	}
	// 7. OneDrive's quota after a delete, then 8. the audit, with the hub
	// stopped (dest-probe refuses while a hub holds the catalog).
	if err := r.host.StopHub(ctx); err != nil {
		return nil, err
	}
	if err := r.audit(ctx); err != nil {
		return nil, err
	}
	return r.data, nil
}

// block blocks the hub's WAN for BlockFor: the rules, the connections'
// reset and the cleanup, the checks before the clock and every
// CheckEvery, the open-segment reads, DNS; then the unblock. A failed
// check unblocks and aborts.
func (r *run) block(ctx context.Context) error {
	t, fw := r.opts.Timing, r.opts.Firewall
	start := r.now()
	if err := fw.Block(ctx); err != nil {
		_ = fw.Unblock(context.WithoutCancel(ctx))
		return err
	}
	if err := r.host.ResetConnections(ctx); err != nil {
		_ = fw.Unblock(context.WithoutCancel(ctx))
		return err
	}
	if err := fw.ScheduleCleanup(ctx, CleanupAt(start, r.cfg.BlockFor)); err != nil {
		_ = fw.Unblock(context.WithoutCancel(ctx))
		return err
	}
	if err := r.check(ctx); err != nil {
		return err
	}
	start = r.now()
	r.withTally(func(tl *tally) { tl.blockStart, tl.floorStart = start.UnixMilli(), start.Add(t.Floor).UnixMilli() })
	end := start.Add(r.cfg.BlockFor)
	r.host.Printf(MsgBlockStarted, start.Format("15:04"), span(r.cfg.BlockFor), end.Format("15:04"))
	nextCheck, nextRead := start.Add(t.CheckEvery), start
	reads := results.ReadOpenRun{}
	var readMs []float64
	dns := true
	for {
		now := r.now()
		if !now.Before(end) {
			break
		}
		if !now.Before(nextRead) && reads.Calls < max(r.cfg.ReadOpenCalls, ReadOpenCalls)+4 {
			ok, ms, why := r.readOpen(ctx, "cam02")
			reads.Calls++
			if ok {
				reads.OK++
				readMs = append(readMs, ms)
			} else {
				failed(&reads, why)
			}
			for _, h := range r.cfg.ProviderHosts {
				dns = dns && r.opts.Resolve(ctx, h) == nil
			}
			nextRead = nextRead.Add(r.opts.Timing.ReadOpenEvery)
		}
		if !now.Before(nextCheck) {
			if err := r.check(ctx); err != nil {
				return err
			}
			nextCheck = nextCheck.Add(t.CheckEvery)
		}
		wait := min(end.Sub(now), nextRead.Sub(now), nextCheck.Sub(now))
		if err := r.sleep(ctx, max(wait, 10*time.Millisecond)); err != nil {
			_ = fw.Unblock(context.WithoutCancel(ctx))
			return err
		}
	}
	reads.P50Ms, reads.MaxMs = percentile(readMs, 0.5), percentile(readMs, 1)
	r.data.ReadOpen = append(r.data.ReadOpen, reads)
	r.data.DNSResolvedDuringBlock = dns
	if err := fw.Unblock(ctx); err != nil {
		return err
	}
	unblock := r.now()
	r.data.BlockSecs = unblock.Sub(start).Seconds()
	r.withTally(func(tl *tally) { tl.unblockAt = unblock.UnixMilli() })
	r.host.Printf("%s", MsgUnblocked)
	return nil
}

// check runs VerifyBlocked and records it; a failure unblocks and returns
// the owner's line.
func (r *run) check(ctx context.Context) error {
	verify := r.opts.Verify
	if verify == nil {
		verify = func(ctx context.Context) (ReachResult, error) {
			return VerifyBlocked(ctx, r.host.HubConfig(), r.cfg.ProviderHosts, r.cfg.LanProbe)
		}
	}
	res, err := verify(ctx)
	r.data.Reach = append(r.data.Reach, results.ReachCheck{AtMs: r.now().UnixMilli(), ProviderBlocked: res.ProviderBlockedV4 && res.ProviderBlockedV6,
		Lan: res.Lan, Loopback: res.Loopback})
	if err != nil {
		_ = r.opts.Firewall.Unblock(context.WithoutCancel(ctx))
		return err
	}
	return nil
}

// readOpenLine is diag readopen's line.
type readOpenLine struct {
	Schema string  `json:"schema"`
	OK     bool    `json:"ok"`
	ReadMs float64 `json:"readMs"`
	Error  string  `json:"error"`
}

// settle is how long after a camera's segment rotation a read waits: the
// camera has no open segment until MediaMTX opens the next .part with its
// first part, and a series whose period is the segment's (the lab's 30-s
// pre-run) would land on every rotation.
const settle = 5 * time.Second

// readOpen reads cam's open segment once (diag readopen --profile sub), at
// least settle after its last rotation, and once more settle later when
// it found no open segment (a rotation whose event had not reached the
// run yet): whether it read, how long it took, and why not (the diag's
// error, or its exit).
func (r *run) readOpen(ctx context.Context, cam string) (bool, float64, string) {
	var rotated int64
	r.withTally(func(t *tally) { rotated = t.rotated[cam] })
	if wait := time.UnixMilli(rotated).Add(settle).Sub(r.now()); rotated != 0 && wait > 0 {
		if err := r.sleep(ctx, wait); err != nil {
			return false, 0, "stopped"
		}
	}
	ok, ms, why := r.readOpenOnce(ctx, cam)
	if !ok && why == "no-open-segment" {
		if err := r.sleep(ctx, settle); err != nil {
			return false, 0, "stopped"
		}
		ok, ms, why = r.readOpenOnce(ctx, cam)
	}
	return ok, ms, why
}

// readOpenOnce is one diag readopen of cam's sub stream.
func (r *run) readOpenOnce(ctx context.Context, cam string) (bool, float64, string) {
	lines, err := r.host.Diag(ctx, []string{"readopen", "--config", r.host.RunJSON(), "--cam", cam, "--profile", "sub"})
	for _, raw := range lines {
		var l readOpenLine
		if json.Unmarshal(raw, &l) == nil && l.Schema == "ipcam.diag.readopen/1" {
			return l.OK, l.ReadMs, l.Error
		}
	}
	var de *hubproc.DiagError
	if errors.As(err, &de) {
		return false, 0, fmt.Sprintf("exit %d", de.Code)
	}
	return false, 0, "no line"
}

// failed counts a failed read's reason in res.
func failed(res *results.ReadOpenRun, why string) {
	if res.Failures == nil {
		res.Failures = map[string]int{}
	}
	res.Failures[why]++
}

// readOpenSeries reads cam's open segment calls times, one every every,
// for at most span: the first after one period, once the hub that just
// started has a segment open.
func (r *run) readOpenSeries(ctx context.Context, encrypted bool, cam string, calls int, every, span time.Duration) (results.ReadOpenRun, error) {
	res := results.ReadOpenRun{Encrypted: encrypted}
	var ms []float64
	end := r.now().Add(span)
	for range calls {
		if err := r.sleep(ctx, every); err != nil {
			return res, err
		}
		ok, took, why := r.readOpen(ctx, cam)
		res.Calls++
		if ok {
			res.OK++
			ms = append(ms, took)
		} else {
			failed(&res, why)
		}
		if r.now().After(end) && res.Calls >= calls {
			break
		}
	}
	res.P50Ms, res.MaxMs = percentile(ms, 0.5), percentile(ms, 1)
	return res, nil
}

// percentile is the q-quantile of v (nearest rank), 0 for none.
func percentile(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	sort.Float64s(s)
	i := int(q*float64(len(s))+0.5) - 1
	return s[min(max(i, 0), len(s)-1)]
}

// ErrNoUploadAfterUnblock is a run whose hub showed no upload in the
// middle of a placement after the unblock, so no restart could land
// between two parts.
var ErrNoUploadAfterUnblock = errors.New("mi5: no upload in the middle of a placement after the unblock")

// restartMidUpload is the restart (P16): after the unblock it waits for
// an upload_progress with 0 < offset < size, at least tearAfter after the
// cameras' last rotation (so the kill also tears an open segment), records
// that placement's last confirmed offset, kills the hub (MediaMTX dies
// with it) and starts it once more. It never kills during the block.
func (r *run) restartMidUpload(ctx context.Context) error {
	var target progress
	deadline := r.now().Add(r.opts.Timing.DrainMax)
	for {
		found := false
		r.withTally(func(tl *tally) { target, found = tl.midUpload(r.now().UnixMilli()) })
		if found {
			break
		}
		if r.now().After(deadline) {
			return ErrNoUploadAfterUnblock
		}
		if err := r.sleep(ctx, 50*time.Millisecond); err != nil {
			return err
		}
	}
	killedAt := r.now()
	if err := r.host.KillHub(); err != nil {
		return err
	}
	r.withTally(func(tl *tally) {
		tl.killedAt = killedAt.UnixMilli()
		tl.restartPlacement = target.placement
		tl.lastConfirmed = target.offset
	})
	r.data.Restart = &results.Mi5Restart{KilledAtMs: killedAt.UnixMilli(), Size: target.size, LastConfirmedOffset: target.offset}
	if err := r.host.StartHub(ctx, PhaseMain); err != nil {
		return err
	}
	r.host.Printf("%s", MsgRestarted)
	return nil
}

// drain waits until every placement of the run's own segments is
// verified (the backlog caught up), then stops the recording and drains
// what the stop finalised with no job; RecoverySecs and UplinkMbps are
// the first drain's.
func (r *run) drain(ctx context.Context) error {
	t := r.opts.Timing
	if err := r.waitDrained(ctx, 0); err != nil {
		return err
	}
	end := r.now()
	r.withTally(func(tl *tally) {
		r.data.RecoverySecs = float64(end.UnixMilli()-tl.unblockAt) / 1000
		if r.data.RecoverySecs > 0 {
			r.data.UplinkMbps = float64(tl.drainBytes(end.UnixMilli())) * 8 / 1e6 / r.data.RecoverySecs
		}
	})
	r.host.Printf(MsgDrained, elapsed(time.Duration(r.data.RecoverySecs*float64(time.Second))))
	if err := r.host.StopHub(ctx); err != nil {
		return err
	}
	if err := r.host.StartHub(ctx, PhaseFinal); err != nil {
		return err
	}
	return r.waitDrained(ctx, t.Quiet)
}

// waitDrained waits until the restart's placement is verified and no
// placement of the run is pending, nothing having happened for quiet.
func (r *run) waitDrained(ctx context.Context, quiet time.Duration) error {
	deadline := r.now().Add(r.opts.Timing.DrainMax)
	for {
		done := false
		r.withTally(func(tl *tally) { done = tl.drained(r.now().UnixMilli(), quiet.Milliseconds()) })
		if done {
			return nil
		}
		if r.now().After(deadline) {
			return errors.New("mi5: the drain did not end within its bound")
		}
		if err := r.sleep(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
}

// elapsed is a duration for the owner: "40 s", "12 min", "2 h 5 min".
func elapsed(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", d/time.Second)
	case d < time.Hour:
		return fmt.Sprintf("%d min", d/time.Minute)
	}
	return fmt.Sprintf("%d h %d min", d/time.Hour, d%time.Hour/time.Minute)
}
