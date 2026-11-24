// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi8

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// Target is MI8's observed time: 72 h.
const Target = 72 * time.Hour

// Mi8Config is one MI8 session.
type Mi8Config struct { //nolint:revive // the plan's name (Task 10)
	// Hub runs ipcam-hub diag onvif; Start starts its subscriber, FFmpeg
	// and anything else that must end with uploadspike.
	Hub   Hub
	Start Starter
	// Cameras are the session's cameras, in RAM only.
	Cameras []CameraRef
	// State is the run's state, with a session begun for this one; Run
	// updates it in place, under its lock, and hands copies to Progress.
	State *State
	// FFmpeg is the pinned ffmpeg's path; empty: the snapshot diff
	// (armv7, a failed download, a missing tar or xz).
	FFmpeg string
	// Params are the frame diff's thresholds; zero fields take MI8's.
	Params EpisodeParams
	// Progress receives the run's state every ProgressEvery (1 min by
	// default) and at the end: uploadspike writes it into the run state,
	// with the heartbeat, and the envelope into results/.
	Progress      func(State) error
	ProgressEvery time.Duration
	// SnapshotClient fetches the snapshots; nil: SnapshotSource's.
	SnapshotClient *http.Client
	// FirstFrame bounds the wait for a substream's first decoded frame.
	FirstFrame time.Duration
	// Resubscribe is the pause before diag onvif runs again after it ended
	// on its own: 10 s.
	Resubscribe time.Duration
	Now         func() time.Time
}

// Run runs an MI8 session until the run's observed time reaches its target
// (72 h over every session) or ctx ends, and returns the envelope of the run
// so far. Every camera is subscribed to through ipcam-hub diag onvif, and
// measured against its own reference: its substream decoded by FFmpeg, or,
// without FFmpeg or when the substream does not decode, its snapshots.
func Run(ctx context.Context, cfg Mi8Config) (results.Envelope, error) {
	r, err := newRunner(cfg)
	if err != nil {
		return results.Envelope{}, err
	}
	return r.run(ctx)
}

// runner is a running session.
type runner struct {
	cfg      Mi8Config
	now      func() time.Time
	mu       sync.Mutex // guards cfg.State
	trackers map[string]*tracker
}

func newRunner(cfg Mi8Config) (*runner, error) {
	if cfg.State == nil || len(cfg.State.Sessions) == 0 {
		return nil, errors.New("mi8: the run has no session")
	}
	r := &runner{cfg: cfg, now: cfg.Now, trackers: map[string]*tracker{}}
	if r.now == nil {
		r.now = time.Now
	}
	if r.cfg.ProgressEvery <= 0 {
		r.cfg.ProgressEvery = time.Minute
	}
	if r.cfg.Resubscribe <= 0 {
		r.cfg.Resubscribe = 10 * time.Second
	}
	if r.cfg.State.TargetMs <= 0 {
		r.cfg.State.TargetMs = Target.Milliseconds()
	}
	p := cfg.Params.withDefaults()
	r.cfg.Params = p
	r.cfg.State.QuietGapSecs = int(p.QuietGap / time.Second)
	for _, c := range cfg.Cameras {
		st := r.cfg.State.Camera(c.ID)
		if st == nil {
			r.cfg.State.Cameras = append(r.cfg.State.Cameras, CameraState{Camera: c.ID})
		}
	}
	for _, c := range cfg.Cameras {
		// Each tracker owns its copy; tick copies it back under r.mu.
		st := *r.cfg.State.Camera(c.ID)
		r.trackers[c.ID] = &tracker{st: &st, lag: func() time.Duration { return 0 }}
	}
	return r, nil
}

// done reports whether the run reached its target.
func (r *runner) done() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.State.ObservedMs() >= r.cfg.State.TargetMs
}

// tick extends the session to now and hands the state to Progress.
func (r *runner) tick() error {
	r.mu.Lock()
	st := r.cfg.State
	now := r.now().UnixMilli()
	st.Sessions[len(st.Sessions)-1].EndMs = now
	st.HeartbeatMs = now
	for id, t := range r.trackers {
		snap := t.snapshot()
		*st.Camera(id) = snap
	}
	copyState := *st
	copyState.Cameras = append([]CameraState(nil), st.Cameras...)
	r.mu.Unlock()
	if r.cfg.Progress != nil {
		return r.cfg.Progress(copyState)
	}
	return nil
}

func (r *runner) run(ctx context.Context) (results.Envelope, error) {
	sessCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	// Each camera's reference.
	uris, _ := r.cfg.Hub.URIs(sessCtx, r.cfg.Cameras)
	for _, c := range r.cfg.Cameras {
		t := r.trackers[c.ID]
		u := uris[c.ID]
		wg.Go(func() { r.reference(sessCtx, c, u, t) })
	}
	// The subscriptions, run again if diag onvif ends on its own.
	wg.Go(func() { r.subscribe(sessCtx) })
	ticker := time.NewTicker(r.cfg.ProgressEvery)
	defer ticker.Stop()
	var progressErr error
	if err := r.tick(); err != nil {
		progressErr = err
	}
loop:
	for !r.done() {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			if err := r.tick(); err != nil {
				progressErr = err
			}
		}
	}
	cancel()
	wg.Wait()
	if err := r.tick(); err != nil {
		progressErr = err
	}
	r.mu.Lock()
	env := r.cfg.State.Envelope(Meta{}, r.now().UnixMilli())
	r.mu.Unlock()
	if ctx.Err() != nil {
		return env, ctx.Err()
	}
	return env, progressErr
}

// subscribe runs diag onvif's subscriptions until ctx ends, and passes their
// lines to the cameras' trackers.
func (r *runner) subscribe(ctx context.Context) {
	hub := r.cfg.Hub
	if hub.Start == nil {
		hub.Start = r.cfg.Start
	}
	failed := map[string]bool{}
	for ctx.Err() == nil {
		var cams []CameraRef
		for _, c := range r.cfg.Cameras {
			if !failed[c.ID] {
				cams = append(cams, c)
			}
		}
		if len(cams) == 0 {
			<-ctx.Done()
			return
		}
		sub, err := hub.Subscribe(ctx, cams)
		if err == nil {
			for l := range sub.Lines() {
				t := r.trackers[l.Cam]
				if t == nil {
					continue
				}
				switch l.Type {
				case KindEvent:
					t.onEvent(l.AtMs, l.Motion)
				case KindSubscribeFailed:
					// A configuration fault: the camera is not asked again in
					// this session, apart from coverage.
					failed[l.Cam] = true
					t.onSignal(l.Type)
				default:
					t.onSignal(l.Type)
				}
			}
			closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
			_ = sub.Wait(closeCtx)
			cancel()
		}
		select {
		case <-ctx.Done():
		case <-time.After(r.cfg.Resubscribe):
		}
	}
}

// lagger is a reference with a delay.
type lagger interface {
	FrameSource
	Lag() time.Duration
}

// reference runs a camera's motion reference until ctx ends: the substream
// through FFmpeg when it decodes, else the snapshots; a reference that ends
// on its own starts again.
func (r *runner) reference(ctx context.Context, c CameraRef, u URIs, t *tracker) {
	for ctx.Err() == nil {
		var src lagger
		method := ""
		if r.cfg.FFmpeg != "" && u.Stream != "" {
			rs := &RTSPSource{URI: u.Stream, User: c.User, Pass: c.Pass, FFmpeg: r.cfg.FFmpeg, Start: r.cfg.Start,
				FirstFrame: r.cfg.FirstFrame, Now: r.now}
			if frames, err := rs.Frames(ctx); err == nil {
				src, method = rs, results.ReferenceSubstream
				r.consume(ctx, frames, src, method, t)
				continue
			}
		}
		if u.Snapshot != "" && ctx.Err() == nil {
			ss := &SnapshotSource{URI: u.Snapshot, User: c.User, Pass: c.Pass, Client: r.cfg.SnapshotClient, Now: r.now}
			if frames, err := ss.Frames(ctx); err == nil {
				src, method = ss, results.ReferenceSnapshot
				r.consume(ctx, frames, src, method, t)
				continue
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(r.cfg.Resubscribe):
		}
	}
}

// consume feeds a reference's frames to the camera's episodes until the
// source ends.
func (r *runner) consume(ctx context.Context, frames <-chan Frame, src lagger, method string, t *tracker) {
	t.mu.Lock()
	t.lag = src.Lag
	t.st.ReferenceMethod = method
	t.mu.Unlock()
	eps := NewEpisodes(r.cfg.Params)
	for f := range frames {
		closed, started := eps.Add(f)
		if closed != nil {
			t.onEpisodeEnd(*closed)
		}
		if started {
			t.onEpisodeStart(eps.Open().StartMs)
		}
	}
	// An episode open when the session ends is kept: its start has been seen,
	// its end is the last motion frame.
	if e := eps.Flush(); e != nil && ctx.Err() != nil {
		t.onEpisodeEnd(*e)
	}
}
