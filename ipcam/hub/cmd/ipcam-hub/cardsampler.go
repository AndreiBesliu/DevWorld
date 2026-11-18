package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/health"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/recorder"
)

// probeEvery is how often the card sampler writes and fsyncs its probe, and
// mountEvery how often it reads the mount table (T40, §13.1). The tests of
// ipcam-hub's process set probeEvery from TestMain.
var probeEvery = 10 * time.Second

const mountEvery = 5 * time.Second

// shutdownWait bounds how long ipcam-hub's stop waits for work that the card
// can hold: the card sampler's goroutines, and the lines of MediaMTX's log
// that wait for stderr.
const shutdownWait = 5 * time.Second

// probeHung is how many intervals in a row must find a probe still in
// flight before that interval, and each later one, also counts as an I/O
// error (Task 8 review M2): the first one counts the probe as slow only.
// The intervals are counted, not timed, so a tick handled late, behind
// pollMount on the same loop or by the ticker's jitter, does not move the
// bound (Task 8b review). With 10 s intervals, a card whose fsync hangs for
// good has its third error, so is FAILED (3 I/O errors in 60 s), at the 4th
// interval after its probe began: about 40 s after it, and at most about
// 50 s after the hang began, since a probe starts up to one interval after.
const probeHung = 2

// probePath is the probe file, under rec/.
var probePath = filepath.Join(".health", "probe")

// probeData is what each probe writes: one page.
var probeData = make([]byte, 4096)

// cardRecorder is what the card sampler needs of the recorder.
type cardRecorder interface {
	Stop(ctx context.Context, cam recorder.CameraID, p recorder.Profile, reason catalog.GapReason) error
	WriteFailed(cam recorder.CameraID, p recorder.Profile, reason catalog.GapReason)
}

// cardSampler watches the SD card under rec/ while ipcam-hub records on it
// (T40, HUB-17, G17), with health.CardMonitor's §13.1 thresholds. Its
// inputs: every probeEvery it writes and fsyncs rec/.health/probe, on a
// goroutine of its own, and times it, and an EIO or EROFS of the probe is
// an I/O error or a read-only card, and a probe that hangs counts as slow,
// then as I/O errors (probeTick); every mountEvery it reads the read-only
// flag of rec/'s mount; a line of
// MediaMTX's log, or an error of ipcam-hub's own, that holds «input/output
// error» or «read-only file system» is an I/O error (the recorder reports a
// directory that stays unreadable once, at the start's reconcile or at the
// first verification pass that meets it, so it is one). Each change of the
// card's state is a card event, and the card alert an alert event.
//
// Below FAILED, recording goes on: a write that MediaMTX could not make
// becomes a card gap, from the end of the last good part to the start of
// the stream's next segment (recorder.WriteFailed, which returns at once:
// the goroutine that reads MediaMTX's log never touches the card). At
// FAILED, every job stops with a card gap (recorder.Stop), at once:
// recording stops with a gap, never with a silent loss (Review Focus 4).
type cardSampler struct {
	recDir, mountInfo string
	ev                *events
	now               func() time.Time
	// probeFile writes and fsyncs the probe at path and returns how long it
	// took; tests replace it.
	probeFile func(path string) (time.Duration, error)
	// probeEvery and mountEvery are the sampler's pace: the package's, which
	// tests change.
	probeEvery, mountEvery time.Duration
	// joinWait bounds run's wait for its goroutines once ctx is done.
	joinWait time.Duration
	// running counts the goroutines that run waits for: the stop at FAILED
	// and the probe in flight.
	running sync.WaitGroup

	mu       sync.Mutex
	mon      *health.CardMonitor
	state    health.CardHealth
	rec      cardRecorder
	failed   chan struct{} // closed once the card has failed
	probeErr string        // the probe's last error that is not the card's
	// probing is when the probe that has not returned yet began, by now,
	// and stalled how many intervals since have found it still in flight;
	// both zero while none runs.
	probing time.Time
	stalled int
	// ended is set once run has returned: nothing goes out after.
	ended bool
}

func newCardSampler(recDir, mountInfo string, ev *events) *cardSampler {
	return &cardSampler{recDir: recDir, mountInfo: mountInfo, ev: ev, now: time.Now, probeFile: writeProbe,
		probeEvery: probeEvery, mountEvery: mountEvery, joinWait: shutdownWait,
		mon: health.NewCardMonitor(), state: health.CardOK, failed: make(chan struct{})}
}

// begin reports the card's state as ipcam-hub starts: OK, or FAILED when
// its mount is read-only already.
func (s *cardSampler) begin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if health.IsReadOnly(s.mountInfo, s.recDir) {
		s.observe(health.IoEvent{Kind: health.IoEROFS})
		return
	}
	s.ev.emit("card", map[string]any{"state": s.state.String()})
}

// attach gives the sampler the recorder, which a failed write and a failed
// card go to.
func (s *cardSampler) attach(rec cardRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec = rec
}

// run probes the card and reads the mount table until ctx is done, and
// stops every job of jobs once the card has failed. The stop waits on a
// goroutine of its own, and each probe runs on one of its own: a probe that
// hangs on a dying card holds neither the stop nor the next intervals
// (Task 8 review M2). The stop's own writes, MediaMTX's configuration and
// the gaps' rows, are on the card: on a read-only card they fail at once,
// and the gaps still go out as events (recorder.Stop); a card that hangs
// holds them. Once ctx is done, run returns when those goroutines have
// ended (join).
func (s *cardSampler) run(ctx context.Context, jobs []recorder.Job) {
	defer s.join()
	s.running.Add(1)
	go func() {
		defer s.running.Done()
		select {
		case <-ctx.Done():
		case <-s.failed:
			s.stopAll(ctx, jobs)
		}
	}()
	probe := time.NewTicker(s.probeEvery)
	defer probe.Stop()
	mount := time.NewTicker(s.mountEvery)
	defer mount.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-probe.C:
			s.probeTick()
		case <-mount.C:
			s.pollMount()
		}
	}
}

// join waits until the goroutines that run started have ended, so that
// nothing that they find goes out after ipcam-hub's stop (Task 8b review
// M4): the stop at FAILED, whose writes are on the card, and the probe in
// flight, whose result is still observed. It waits at most joinWait, since
// a card that hangs holds either for good and must not hold ipcam-hub's
// stop; what they find after that goes nowhere.
func (s *cardSampler) join() {
	joined := make(chan struct{})
	go func() {
		s.running.Wait()
		close(joined)
	}()
	select {
	case <-joined:
	case <-time.After(s.joinWait):
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = true
}

// stopAll stops every job with a card gap.
func (s *cardSampler) stopAll(ctx context.Context, jobs []recorder.Job) {
	s.mu.Lock()
	rec := s.rec
	s.mu.Unlock()
	for _, j := range jobs {
		if err := rec.Stop(ctx, j.Cam, j.Profile, catalog.GapCard); err != nil {
			s.mu.Lock()
			if !s.ended {
				s.ev.emit("error", map[string]any{"message": err.Error()})
			}
			s.mu.Unlock()
		}
	}
}

// childLog takes a line of MediaMTX's log: one that holds an I/O error is
// one of the card's, and when it is a stream's recorder that failed, the
// hole that it leaves gets a card gap (recorder.WriteFailed). It runs on
// the goroutine that reads MediaMTX's log, so it does no disk I/O. The
// failed write goes to the recorder before the card's new state goes out.
func (s *cardSampler) childLog(line string) {
	if !ioErrorText(line) {
		return
	}
	s.mu.Lock()
	rec := s.rec
	s.mu.Unlock()
	if cam, p, ok := recorder.LogStream(line); ok && rec != nil && strings.Contains(line, "] [recorder] ") {
		rec.WriteFailed(cam, p, catalog.GapCard)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(health.IoEvent{Kind: health.IoEIO})
}

// hubError takes an error of ipcam-hub's own: one that holds an I/O error
// is one of the card's.
func (s *cardSampler) hubError(err error) {
	if !ioErrorText(err.Error()) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(health.IoEvent{Kind: health.IoEIO})
}

// ioErrorText reports whether text holds an I/O error of the card.
func ioErrorText(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "input/output error") || strings.Contains(text, "read-only file system")
}

// probeTick starts a probe on a goroutine of its own. While the last one
// has not returned, it starts none: it counts that probe's time so far as
// an fsync's, and, from the probeHung-th interval that finds it still in
// flight, as an I/O error too: a probe that hangs on a dying card is never
// invisible.
func (s *cardSampler) probeTick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.probing.IsZero() {
		s.probing, s.stalled = now, 0
		s.running.Add(1)
		go func() {
			defer s.running.Done()
			s.probe()
		}()
		return
	}
	s.stalled++
	s.observe(health.IoEvent{Kind: health.IoFsync, Latency: now.Sub(s.probing)})
	if s.stalled >= probeHung {
		s.observe(health.IoEvent{Kind: health.IoEIO})
	}
}

// probe writes and fsyncs the probe and observes its time, or its error.
func (s *cardSampler) probe() {
	took, err := s.probeFile(filepath.Join(s.recDir, probePath))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probing, s.stalled = time.Time{}, 0
	if s.ended {
		return
	}
	switch {
	case errors.Is(err, syscall.EROFS):
		s.observe(health.IoEvent{Kind: health.IoEROFS})
	case errors.Is(err, syscall.EIO):
		s.observe(health.IoEvent{Kind: health.IoEIO})
	case err != nil:
		// Not the card's, such as a full disk: reported once while it lasts.
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = errors.New(pe.Op + ": " + pe.Err.Error()) // without the data directory's path
		}
		if msg := "ipcam-hub: the SD-card probe: " + err.Error(); msg != s.probeErr {
			s.probeErr = msg
			s.ev.emit("error", map[string]any{"message": msg})
		}
	default:
		s.probeErr = ""
		s.observe(health.IoEvent{Kind: health.IoFsync, Latency: took})
	}
}

// pollMount observes a read-only mount of rec/.
func (s *cardSampler) pollMount() {
	if !health.IsReadOnly(s.mountInfo, s.recDir) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe(health.IoEvent{Kind: health.IoEROFS})
}

// stats are the card monitor's figures now.
func (s *cardSampler) stats() health.IoStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mon.Stats(s.now())
}

// observe gives ev to the monitor and reports what changed: a new state, the
// alert, and the failure, once; nothing once run has returned. s.mu is
// held.
func (s *cardSampler) observe(ev health.IoEvent) {
	if s.ended {
		return
	}
	state, raised := s.mon.Observe(ev, s.now())
	if state != s.state {
		s.state = state
		s.ev.emit("card", map[string]any{"state": state.String()})
		if state == health.CardFailed {
			close(s.failed) // FAILED lasts: this happens once
		}
	}
	if raised {
		s.ev.emit("alert", map[string]any{"kind": "card"})
	}
}

// writeProbe writes probeData to path, fsyncs it, and returns how long both
// took.
func writeProbe(path string) (time.Duration, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	start := time.Now()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // the probe under rec/
	if err != nil {
		return 0, err
	}
	_, err = f.Write(probeData)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return time.Since(start), err
}
