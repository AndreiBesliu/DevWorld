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
// inputs: every probeEvery it writes and fsyncs rec/.health/probe and times
// it, and an EIO or EROFS of the probe is an I/O error or a read-only card;
// every mountEvery it reads the read-only flag of rec/'s mount; a line of
// MediaMTX's log, or an error of ipcam-hub's own, that holds «input/output
// error» or «read-only file system» is an I/O error (the recorder reports a
// directory that stays unreadable once, so it is one). Each change of the
// card's state is a card event, and the card alert an alert event.
//
// Below FAILED, recording goes on: a write that MediaMTX could not make
// becomes a card gap, from the end of the file's last good part to the
// start of the stream's next segment (recorder.WriteFailed). At FAILED,
// every job stops with a card gap (recorder.Stop), at once: recording stops
// with a gap, never with a silent loss (Review Focus 4).
type cardSampler struct {
	recDir, mountInfo string
	ev                *events
	now               func() time.Time
	// probeFile writes and fsyncs the probe at path and returns how long it
	// took; tests replace it.
	probeFile func(path string) (time.Duration, error)

	mu       sync.Mutex
	mon      *health.CardMonitor
	state    health.CardHealth
	rec      cardRecorder
	failed   chan struct{} // closed once the card has failed
	probeErr string        // the probe's last error that is not the card's
}

func newCardSampler(recDir, mountInfo string, ev *events) *cardSampler {
	return &cardSampler{recDir: recDir, mountInfo: mountInfo, ev: ev, now: time.Now, probeFile: writeProbe,
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
// stops every job of jobs once the card has failed.
func (s *cardSampler) run(ctx context.Context, jobs []recorder.Job) {
	probe := time.NewTicker(probeEvery)
	defer probe.Stop()
	mount := time.NewTicker(mountEvery)
	defer mount.Stop()
	failed := s.failed
	for {
		select {
		case <-ctx.Done():
			return
		case <-probe.C:
			s.probe()
		case <-mount.C:
			s.pollMount()
		case <-failed:
			failed = nil
			s.stopAll(ctx, jobs)
		}
	}
}

// stopAll stops every job with a card gap.
func (s *cardSampler) stopAll(ctx context.Context, jobs []recorder.Job) {
	s.mu.Lock()
	rec := s.rec
	s.mu.Unlock()
	for _, j := range jobs {
		if err := rec.Stop(ctx, j.Cam, j.Profile, catalog.GapCard); err != nil {
			s.ev.emit("error", map[string]any{"message": err.Error()})
		}
	}
}

// childLog takes a line of MediaMTX's log: one that holds an I/O error is
// one of the card's, and when it is a stream's recorder that failed, the
// tail of the file that it was writing gets a card gap.
func (s *cardSampler) childLog(line string) {
	if !ioErrorText(line) {
		return
	}
	s.mu.Lock()
	s.observe(health.IoEvent{Kind: health.IoEIO})
	rec := s.rec
	s.mu.Unlock()
	if cam, p, ok := recorder.LogStream(line); ok && rec != nil && strings.Contains(line, "] [recorder] ") {
		rec.WriteFailed(cam, p, catalog.GapCard)
	}
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

// probe writes and fsyncs the probe and observes its time, or its error.
func (s *cardSampler) probe() {
	took, err := s.probeFile(filepath.Join(s.recDir, probePath))
	s.mu.Lock()
	defer s.mu.Unlock()
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
// alert, and the failure, once. s.mu is held.
func (s *cardSampler) observe(ev health.IoEvent) {
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
