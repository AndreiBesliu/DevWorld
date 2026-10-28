package mi2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// fakeClock is a clock that moves only when the test advances it; each
// advance ticks every ticker once, and waits until the run took the tick.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []chan time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTicker(time.Duration) source.Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := make(chan time.Time)
	c.tickers = append(c.tickers, t)
	return fakeTicker(t)
}

type fakeTicker chan time.Time

func (t fakeTicker) C() <-chan time.Time { return t }
func (fakeTicker) Stop()                 {}

// advance moves the clock by d and ticks.
func (c *fakeClock) advance(t *testing.T, d time.Duration) {
	t.Helper()
	c.mu.Lock()
	c.now = c.now.Add(d)
	now, tickers := c.now, slices.Clone(c.tickers)
	c.mu.Unlock()
	for _, tick := range tickers {
		select {
		case tick <- now:
		case <-time.After(10 * time.Second):
			t.Fatal("the run took no tick")
		}
	}
}

// fakeHub is an ipcam-hub that the test crashes, or that the run stops.
type fakeHub struct {
	onEvent  func(hubproc.Event)
	exited   chan struct{}
	once     sync.Once
	stopped  atomic.Bool
	stopWait atomic.Int64 // how long Stop's context gave it, in ns
}

func (h *fakeHub) Exited() <-chan struct{} { return h.exited }
func (h *fakeHub) ExitCode() int           { return -1 }

func (h *fakeHub) Stop(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		h.stopWait.Store(int64(time.Until(deadline)))
	}
	h.stopped.Store(true)
	h.exit()
	return nil
}

func (h *fakeHub) exit() { h.once.Do(func() { close(h.exited) }) }

// emit sends ipcam-hub's events of type typ, as hubproc reads them.
func (h *fakeHub) emit(events ...hubproc.Event) {
	for _, e := range events {
		h.onEvent(e)
	}
}

// harness runs Run with fakes: a fake clock, fake ipcam-hubs, 500 GB free,
// the controller's cameras, and the boot that boot names.
type harness struct {
	t      *testing.T
	clock  *fakeClock
	hubs   chan *fakeHub
	opts   Options
	out    *bytes.Buffer
	done   chan error
	cancel context.CancelFunc
}

func newHarness(t *testing.T, dir string, host mi1.HostState, boot string) *harness {
	t.Helper()
	h := &harness{t: t, clock: &fakeClock{now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}, hubs: make(chan *fakeHub, 8), out: &bytes.Buffer{}}
	h.opts = Options{
		Hours: 72, Host: host, Hub: filepath.Join(dir, "ipcam-hub"),
		Results: filepath.Join(dir, "results"), Work: filepath.Join(dir, "work"), Out: h.out,
		FreeBytes: func() (uint64, error) { return 500e9, nil },
		CameraURLs: func(_ context.Context, streams []string) (map[string]string, error) {
			urls := map[string]string{}
			for _, s := range streams {
				urls[s] = "rtsp://user:pass@127.0.0.1:8554/" + s
			}
			return urls, nil
		},
		Clock:    h.clock,
		ThisBoot: func() (mi1.Boot, error) { return mi1.Boot{ID: boot}, nil },
		StartHub: func(_ string, _ io.Writer, onEvent func(hubproc.Event)) (Hub, error) {
			hub := &fakeHub{onEvent: onEvent, exited: make(chan struct{})}
			h.hubs <- hub
			return hub, nil
		},
	}
	return h
}

func (h *harness) start() {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel, h.done = cancel, make(chan error, 1)
	go func() { h.done <- Run(ctx, h.opts) }()
	h.t.Cleanup(cancel)
}

// nextHub waits for the next ipcam-hub that the run starts, and makes it
// ready.
func (h *harness) nextHub() *fakeHub {
	h.t.Helper()
	select {
	case hub := <-h.hubs:
		hub.emit(hubproc.Event{Type: "ready"})
		return hub
	case err := <-h.done:
		h.t.Fatalf("the run ended (%v) before it started ipcam-hub; it printed:\n%s", err, h.out)
	case <-time.After(10 * time.Second):
		h.t.Fatal("the run started no ipcam-hub")
	}
	return nil
}

// wait waits for the run's end.
func (h *harness) wait() error {
	h.t.Helper()
	select {
	case err := <-h.done:
		return err
	case <-time.After(10 * time.Second):
		h.t.Fatal("the run did not end")
	}
	return nil
}

// finish moves the clock past the run's hours and waits for its end.
func (h *harness) finish() {
	h.t.Helper()
	h.clock.advance(h.t, time.Duration(h.opts.Hours*float64(time.Hour)))
	if err := h.wait(); err != nil {
		h.t.Fatalf("Run: %v; it printed:\n%s", err, h.out)
	}
}

// envelope reads the run's envelope.
func (h *harness) envelope(runID string) results.Envelope {
	h.t.Helper()
	set := strings.TrimSuffix(runID, runID[strings.LastIndex(runID, "-"):])
	e, err := results.Read(filepath.Join(h.opts.Results, "MI2", set, runID+".json"))
	if err != nil {
		h.t.Fatal(err)
	}
	if e.MI2 == nil {
		h.t.Fatalf("envelope %s has no mi2", runID)
	}
	return e
}

var pcHost = mi1.HostState{Pseudonym: "pc", OS: "windows", Storage: "ssd", WriteCache: "on", Cameras: 8}

// TestMI2_CrashCountsUnexpectedExitsOnly (C07, HUB-31, HUB-13): over the
// 72 h, an exit of ipcam-hub that recspike did not ask for is a crash,
// after which recspike starts it again at once; MediaMTX's restarts are
// reported, not counted as crashes; recspike's own stop at the end is no
// crash, and gives ipcam-hub the time that a slow SD card needs (Task 7.2d
// measured more than 30 s), not a premature kill.
func TestMI2_CrashCountsUnexpectedExitsOnly(t *testing.T) {
	h := newHarness(t, t.TempDir(), pcHost, "boot-1")
	h.start()
	first := h.nextHub()
	first.exit() // ipcam-hub crashed
	second := h.nextHub()
	second.emit(hubproc.Event{Type: "child_restart"}, hubproc.Event{Type: "child_restart"})
	h.finish()

	d := h.envelope("pc-72h-1").MI2
	if d.Crashes72h != 1 || d.ChildRestarts != 2 || d.Hours != 72 || d.Cameras != 8 || d.OnSdCard || d.WindowHours != 6 {
		t.Errorf("mi2 %+v, want 1 crash, 2 MediaMTX restarts, 72 h, 8 cameras on no SD card, a 6 h window", *d)
	}
	if first.stopped.Load() || !second.stopped.Load() {
		t.Errorf("stopped: the crashed ipcam-hub %v, the running one %v; want only the running one", first.stopped.Load(), second.stopped.Load())
	}
	if wait := time.Duration(second.stopWait.Load()); wait < mi1.StopTimeout-time.Second || mi1.StopTimeout < 5*time.Minute {
		t.Errorf("the stop gave ipcam-hub %v (StopTimeout %v), want at least 5 min", wait, mi1.StopTimeout)
	}
	select {
	case hub := <-h.hubs:
		t.Errorf("ipcam-hub was started again after the end (%p)", hub)
	default:
	}
}

// TestStability_RebootInvalidatesRun (G19): a run that an OS reboot ended,
// told by the boot id, is invalid: the next `recspike mi2` redoes it under
// the next run id, lists it as invalidated, removes its recordings, and
// counts no crash for it. A run that ended without a reboot, recspike
// stopped, is redone the same way, with its own reason.
func TestStability_RebootInvalidatesRun(t *testing.T) {
	for _, tc := range []struct {
		name, nextBoot, reason string
	}{
		{"an OS reboot", "boot-2", "gazda a repornit"},
		{"recspike stopped", "boot-1", "proba s-a întrerupt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			h := newHarness(t, dir, pcHost, "boot-1")
			h.start()
			hub := h.nextHub()
			if _, err := os.Stat(filepath.Join(dir, "work", "mi2", "pc-72h-1")); err != nil {
				t.Fatalf("the first run has no folder: %v", err)
			}
			h.cancel() // the host goes down, and recspike with it
			if err := h.wait(); !errors.Is(err, context.Canceled) {
				t.Fatalf("the interrupted run: %v, want context.Canceled", err)
			}
			if !hub.stopped.Load() {
				t.Error("the interrupted run left ipcam-hub running")
			}

			again := newHarness(t, dir, pcHost, tc.nextBoot)
			again.start()
			again.nextHub()
			again.finish()
			d := again.envelope("pc-72h-2").MI2
			if len(d.Invalidated) != 1 || !strings.HasPrefix(d.Invalidated[0], "pc-72h-1: ") || !strings.Contains(d.Invalidated[0], tc.reason) {
				t.Errorf("invalidated %q, want pc-72h-1 with «%s»", d.Invalidated, tc.reason)
			}
			if d.Crashes72h != 0 {
				t.Errorf("%d crashes, want 0: an invalidated run counts none", d.Crashes72h)
			}
			if _, err := os.Stat(filepath.Join(dir, "results", "MI2", "pc-72h", "pc-72h-1.json")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the invalidated run has an envelope (%v)", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "work", "mi2", "pc-72h-1")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the invalidated run's recordings are still there (%v)", err)
			}

			// The run ended: a third start invalidates nothing.
			third := newHarness(t, dir, pcHost, tc.nextBoot)
			third.start()
			third.nextHub()
			third.finish()
			if inv := third.envelope("pc-72h-3").MI2.Invalidated; len(inv) != 0 {
				t.Errorf("after a complete run, invalidated %q, want none", inv)
			}
		})
	}
}

// TestStability_ReportsCardStates (T40, HUB-17): on an SD card, the 72 h
// run records each state of the card monitor that ipcam-hub reports, in
// order, with its time; elsewhere it records none.
func TestStability_ReportsCardStates(t *testing.T) {
	t.Run("on an SD card", func(t *testing.T) {
		pi := mi1.HostState{Pseudonym: "pi", OS: "linux", Storage: "sd", WriteCache: "unknown", Cameras: 4,
			SdConfirmation: &results.SdConfirmation{Text: "da", AtMs: 1}}
		h := newHarness(t, t.TempDir(), pi, "boot-1")
		h.start()
		hub := h.nextHub()
		hub.emit(
			hubproc.Event{Type: "card", State: "ok", AtMs: 1000},
			hubproc.Event{Type: "card", State: "degraded", AtMs: 2000},
			hubproc.Event{Type: "alert", Kind: "card", AtMs: 2001},
			hubproc.Event{Type: "card", State: "failed", AtMs: 3000},
		)
		h.finish()
		e := h.envelope("pi-72h-1")
		want := []results.CardState{{State: "ok", AtMs: 1000}, {State: "degraded", AtMs: 2000}, {State: "failed", AtMs: 3000}}
		if !e.MI2.OnSdCard || !slices.Equal(e.MI2.CardStates, want) {
			t.Errorf("onSdCard %v, card states %+v; want true, %+v", e.MI2.OnSdCard, e.MI2.CardStates, want)
		}
		if e.Host != "pi" || e.Storage != "sd" || e.SdConfirmation == nil {
			t.Errorf("envelope host %s, storage %s, SD confirmation %v; want pi on its confirmed SD card", e.Host, e.Storage, e.SdConfirmation)
		}
	})
	t.Run("on an SSD", func(t *testing.T) {
		h := newHarness(t, t.TempDir(), pcHost, "boot-1")
		h.start()
		h.nextHub()
		h.finish()
		if d := h.envelope("pc-72h-1").MI2; d.OnSdCard || len(d.CardStates) != 0 {
			t.Errorf("onSdCard %v, card states %+v; want neither", d.OnSdCard, d.CardStates)
		}
	})
}

// fakeDisk is the free space of a disk of capacity bytes that holds what
// is under work/mi2, each byte there standing for 1 MB: a test writes
// kilobytes for the gigabytes that a run records.
func fakeDisk(t *testing.T, work string, capacity uint64) func() (uint64, error) {
	t.Helper()
	return func() (uint64, error) {
		var used uint64
		err := filepath.WalkDir(filepath.Join(work, "mi2"), func(_ string, d fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil || d.IsDir() {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			used += uint64(info.Size()) * 1_000_000 //nolint:gosec // a test file's size
			return nil
		})
		return capacity - min(used, capacity), err
	}
}

// record writes gb gigabytes of recordings, on fakeDisk's scale, into the
// data folder of the run runID.
func record(t *testing.T, work, runID string, gb int) {
	t.Helper()
	path := filepath.Join(work, "mi2", runID, "data", "rec", "cam01", "main", "2026-10-28", "2026-10-28_12-00-00-000000.mp4")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, gb*1000), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestStability_StaleRecordingsDoNotShrinkTheWindow (G19; Task 8 review
// I2): the free space that sets the window is measured once the runs left
// in work/mi2 are gone, since recspike removes them anyway: a reboot's
// recordings never refuse, nor shrink, the redo. A run's recordings go when
// it ends, finished or interrupted, so that they keep no space from MI3 and
// MI4; its ipcam-hub log stays until the next run.
func TestStability_StaleRecordingsDoNotShrinkTheWindow(t *testing.T) {
	pi := mi1.HostState{Pseudonym: "pi", OS: "linux", Storage: "sd", WriteCache: "unknown", Cameras: 4,
		SdConfirmation: &results.SdConfirmation{Text: "da", AtMs: 1}}
	const capacity = 20e9 // 4 cameras: N = 1,39 h
	want, err := Window(capacity, pi.Cameras)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("the redo after a reboot", func(t *testing.T) {
		dir := t.TempDir()
		work := filepath.Join(dir, "work")
		// The reboot: pi-72h-1 is still open, and its recordings hold 10 GB.
		if _, err := beginRun(filepath.Join(dir, "results", "state", "mi2-pi-72h.json"), "pi-72h", mi1.Boot{ID: "boot-1"}, true); err != nil {
			t.Fatal(err)
		}
		record(t, work, "pi-72h-1", 10)
		disk := fakeDisk(t, work, capacity)
		if free, err := disk(); err != nil || free != 10e9 {
			t.Fatalf("the fake disk has %d bytes free (%v), want 10 GB", free, err)
		}
		if _, err := Window(10e9, pi.Cameras); !errors.Is(err, ErrWindowTooSmall) {
			t.Fatalf("Window(10 GB): %v, want ErrWindowTooSmall: the scenario needs a stale run that refuses the redo", err)
		}

		h := newHarness(t, dir, pi, "boot-2")
		h.opts.FreeBytes = disk
		h.start()
		h.nextHub()
		record(t, work, "pi-72h-2", 1)
		h.finish()
		e := h.envelope("pi-72h-2")
		if e.MI2.WindowHours != want.Hours() {
			t.Errorf("window %v h, want %v h: the free space without the stale run", e.MI2.WindowHours, want.Hours())
		}
		if inv := e.MI2.Invalidated; len(inv) != 1 || !strings.HasPrefix(inv[0], "pi-72h-1: gazda a repornit") {
			t.Errorf("invalidated %q, want pi-72h-1 after the reboot", inv)
		}
		if !strings.Contains(h.out.String(), "Spațiu liber aici: 20 GB") {
			t.Errorf("the run printed:\n%s\nwant «Spațiu liber aici: 20 GB»", h.out)
		}
		requireCleared(t, work, "pi-72h-2", disk)
	})

	t.Run("an interrupted run", func(t *testing.T) {
		dir := t.TempDir()
		work := filepath.Join(dir, "work")
		disk := fakeDisk(t, work, capacity)
		h := newHarness(t, dir, pi, "boot-1")
		h.opts.FreeBytes = disk
		h.start()
		hub := h.nextHub()
		record(t, work, "pi-72h-1", 1)
		h.cancel()
		if err := h.wait(); !errors.Is(err, context.Canceled) {
			t.Fatalf("the interrupted run: %v, want context.Canceled", err)
		}
		if !hub.stopped.Load() {
			t.Error("the interrupted run left ipcam-hub running")
		}
		requireCleared(t, work, "pi-72h-1", disk)
	})
}

// requireCleared fails t unless the run runID's folder holds only its
// ipcam-hub log, and the fake disk is empty again.
func requireCleared(t *testing.T, work, runID string, disk func() (uint64, error)) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(work, "mi2", runID))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"ipcam-hub.log"}) {
		t.Errorf("after the run, its folder holds %q, want only ipcam-hub.log", names)
	}
	if free, err := disk(); err != nil || free != 20e9 {
		t.Errorf("after the run, %d bytes free (%v), want all 20 GB", free, err)
	}
}
