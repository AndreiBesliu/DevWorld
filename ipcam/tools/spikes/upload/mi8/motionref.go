// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi8

import (
	"context"
	"slices"
	"time"
)

// The reference frames' size: 160 × 90 gray, one byte per pixel.
const (
	FrameWidth  = 160
	FrameHeight = 90
	FrameSize   = FrameWidth * FrameHeight
)

// Frame is a reference frame, in RAM only: the hub's receipt time of the
// frame (Unix ms; never the camera's clock) and its 160 × 90 gray pixels.
type Frame struct {
	AtMs int64
	Gray []byte
}

// FrameSource gives a camera's reference frames until ctx ends or the
// source fails; the channel closes then.
type FrameSource interface {
	Frames(ctx context.Context) (<-chan Frame, error)
}

// EpisodeParams are the frame diff's thresholds (P41): a pixel changes when
// its gray level moves by more than PixelDelta, a frame is motion when at
// least AreaFraction of its pixels change, and an episode ends after
// QuietGap without a motion frame.
type EpisodeParams struct {
	PixelDelta   int
	AreaFraction float64
	QuietGap     time.Duration
}

// DefaultEpisodeParams are MI8's: 25 gray levels, 1 %, 10 s.
var DefaultEpisodeParams = EpisodeParams{PixelDelta: 25, AreaFraction: 0.01, QuietGap: 10 * time.Second}

// Episode is a motion episode of the reference: the receipt times of its
// first and last motion frames, Unix ms.
type Episode struct{ StartMs, EndMs int64 }

// withDefaults fills p's zero fields from DefaultEpisodeParams.
func (p EpisodeParams) withDefaults() EpisodeParams {
	if p.PixelDelta <= 0 {
		p.PixelDelta = DefaultEpisodeParams.PixelDelta
	}
	if p.AreaFraction <= 0 {
		p.AreaFraction = DefaultEpisodeParams.AreaFraction
	}
	if p.QuietGap <= 0 {
		p.QuietGap = DefaultEpisodeParams.QuietGap
	}
	return p
}

// Motion reports whether cur differs from prev as motion: at least
// AreaFraction of the pixels changed by more than PixelDelta.
func (p EpisodeParams) Motion(prev, cur []byte) bool {
	if len(prev) != len(cur) || len(cur) == 0 {
		return false
	}
	need := int(p.AreaFraction*float64(len(cur)) + 0.999999)
	changed := 0
	for i := range cur {
		d := int(cur[i]) - int(prev[i])
		if d > p.PixelDelta || -d > p.PixelDelta {
			changed++
			if changed >= need {
				return true
			}
		}
	}
	return false
}

// Episodes turns frames into motion episodes as they come.
type Episodes struct {
	p      EpisodeParams
	prev   []byte
	open   *Episode
	lastMs int64
}

// NewEpisodes is an episode tracker with p (zero fields take MI8's values).
func NewEpisodes(p EpisodeParams) *Episodes { return &Episodes{p: p.withDefaults()} }

// Add takes the next frame and returns the episode that it closed, if any:
// an episode closes once a frame arrives QuietGap after its last motion
// frame.
func (t *Episodes) Add(f Frame) (closed *Episode, started bool) {
	prev := t.prev
	t.prev = slices.Clone(f.Gray)
	t.lastMs = f.AtMs
	if t.open != nil && f.AtMs-t.open.EndMs >= t.p.QuietGap.Milliseconds() {
		e := *t.open
		t.open = nil
		closed = &e
	}
	if prev != nil && t.p.Motion(prev, f.Gray) {
		if t.open == nil {
			t.open = &Episode{StartMs: f.AtMs, EndMs: f.AtMs}
			started = true
		} else {
			t.open.EndMs = f.AtMs
		}
	}
	return closed, started
}

// Open is the episode in progress, if any.
func (t *Episodes) Open() *Episode {
	if t.open == nil {
		return nil
	}
	e := *t.open
	return &e
}

// Flush closes the episode in progress at the source's end.
func (t *Episodes) Flush() *Episode {
	e := t.Open()
	t.open = nil
	return e
}

// MotionEpisodes reads src's frames and sends each motion episode once it
// closed, the last one at the source's end; the channel closes then.
func MotionEpisodes(ctx context.Context, src FrameSource, p EpisodeParams) (<-chan Episode, error) {
	frames, err := src.Frames(ctx)
	if err != nil {
		return nil, err
	}
	out := make(chan Episode, 16)
	go func() {
		defer close(out)
		t := NewEpisodes(p)
		send := func(e *Episode) bool {
			if e == nil {
				return true
			}
			select {
			case out <- *e:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for f := range frames {
			if closed, _ := t.Add(f); !send(closed) {
				return
			}
		}
		send(t.Flush())
	}()
	return out, nil
}

// MatchWindow is how long after an episode's start a motion event still
// matches it: MI8's 5 s.
const MatchWindow = 5 * time.Second

// Matches reports whether an event received at atMs matches an episode
// that started at startMs, for a reference whose own delay is lag (P41):
// atMs in [start − lag, start + window].
func Matches(startMs, atMs int64, lag, window time.Duration) bool {
	return atMs >= startMs-lag.Milliseconds() && atMs <= startMs+window.Milliseconds()
}

// Match counts the episodes that a motion event's receipt time matches
// within window of their start, for a reference without delay.
func Match(episodes []Episode, events []int64, window time.Duration) (matched int) {
	return MatchLag(episodes, events, 0, window)
}

// MatchLag counts the episodes that a motion event's receipt time matches
// in [start − lag, start + window].
func MatchLag(episodes []Episode, events []int64, lag, window time.Duration) (matched int) {
	for _, e := range episodes {
		for _, at := range events {
			if Matches(e.StartMs, at, lag, window) {
				matched++
				break
			}
		}
	}
	return matched
}

// The reference's delay (P41): one frame interval at 2 fps, and the cap.
const (
	frameInterval = 500 * time.Millisecond
	maxLag        = 3 * time.Second
)

// Lag is the reference's delay L from its samples: the p90 of the samples
// (decode time − the access unit's receipt time; or a snapshot's fetch
// time) plus one frame interval (0.5 s), capped at 3 s. Without samples it
// is the interval alone.
func Lag(samples []time.Duration) time.Duration {
	l := P90(samples) + frameInterval
	return min(l, maxLag)
}

// P90 is the 90th percentile of samples (nearest rank), 0 without any.
func P90(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	s := slices.Clone(samples)
	slices.Sort(s)
	i := (len(s)*9 + 9) / 10 // ceil(0.9 n)
	return s[max(i-1, 0)]
}

// lagSamples keeps the last lagWindow samples of a reference's delay.
type lagSamples struct {
	s []time.Duration
}

const lagWindow = 2000

// add records a sample.
func (l *lagSamples) add(d time.Duration) {
	if d < 0 {
		d = 0
	}
	l.s = append(l.s, d)
	if len(l.s) > lagWindow {
		l.s = slices.Delete(l.s, 0, len(l.s)-lagWindow)
	}
}

// lag is the reference's L.
func (l *lagSamples) lag() time.Duration { return Lag(l.s) }
