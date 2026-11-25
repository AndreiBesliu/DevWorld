// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi8

import (
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// CameraState is a camera's series in MI8's run state: brand and model, the
// reference's method and delay, the episodes with their match, and the
// silent losses. It holds no address, user, password or URI, and outlives
// MI8's process: uploadspike mi8 --continue adds a new session's to it.
type CameraState struct {
	Camera          string `json:"camera"`
	Brand           string `json:"brand"`
	Model           string `json:"model"`
	ReferenceMethod string `json:"referenceMethod,omitempty"`
	RefLagMs        int64  `json:"refLagMs,omitempty"`
	SilentLosses    int    `json:"silentLosses,omitempty"`
	// SubscribeFailed is set by any failed subscription, Subscribed by any
	// that was created: the camera is reported as «abonare eșuată» only when
	// it never subscribed in the run, so that a brief failure (a camera
	// still booting) never hides its series.
	SubscribeFailed bool                  `json:"subscribeFailed,omitempty"`
	Subscribed      bool                  `json:"subscribed,omitempty"`
	Episodes        []results.EpisodeTime `json:"episodes,omitempty"`
}

// Matched counts the camera's matched episodes.
func (c CameraState) Matched() int {
	n := 0
	for _, e := range c.Episodes {
		if e.Matched {
			n++
		}
	}
	return n
}

// Result is the camera's envelope line.
func (c CameraState) Result() results.Mi8Camera {
	r := results.Mi8Camera{Camera: c.Camera, Brand: c.Brand, Model: c.Model, Episodes: len(c.Episodes), Matched: c.Matched(),
		SilentLosses: c.SilentLosses, RefLagMs: c.RefLagMs, SubscribeFailed: c.SubscribeFailed && !c.Subscribed,
		ReferenceMethod: c.ReferenceMethod, EpisodeTimes: slices.Clone(c.Episodes)}
	if r.EpisodeTimes == nil {
		r.EpisodeTimes = []results.EpisodeTime{}
	}
	if r.Episodes > 0 {
		r.Coverage = float64(r.Matched) / float64(r.Episodes)
	}
	return r
}

// Session is one MI8 process's stretch of observation, Unix ms.
type Session struct {
	StartMs int64 `json:"startMs"`
	EndMs   int64 `json:"endMs"`
}

// State is MI8's run state, run-state.json's Open["MI8"]: its sessions and
// the gaps between them, each camera's series, and the live process's
// heartbeat. It holds no camera address, user, password or URI.
type State struct {
	RunID        string        `json:"runId"`
	StartMs      int64         `json:"startMs"`
	QuietGapSecs int           `json:"quietGapSecs"`
	OnePerBrand  bool          `json:"onePerBrand,omitempty"`
	Sessions     []Session     `json:"sessions"`
	Gaps         []results.Gap `json:"gaps,omitempty"`
	Cameras      []CameraState `json:"cameras"`
	// TargetMs is the observed time that closes the run: 72 h.
	TargetMs int64 `json:"targetMs"`
	// The live process: its PID and start (runstate.ProcStartMs), its boot
	// and its last heartbeat, Unix ms.
	PID         int    `json:"pid,omitempty"`
	ProcStartMs int64  `json:"procStartMs,omitempty"`
	BootID      string `json:"bootId,omitempty"`
	HeartbeatMs int64  `json:"heartbeatMs,omitempty"`
	TestSeams   bool   `json:"testSeams,omitempty"`
}

// Decode reads MI8's run state.
func Decode(raw json.RawMessage) (*State, error) {
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Encode is the run state's JSON.
func (s *State) Encode() json.RawMessage {
	b, _ := json.Marshal(s) //nolint:errchkjson // plain fields
	return b
}

// ObservedMs is the observed time: the sessions added up.
func (s *State) ObservedMs() int64 {
	var ms int64
	for _, x := range s.Sessions {
		ms += max(x.EndMs-x.StartMs, 0)
	}
	return ms
}

// Hours is the observed hours.
func (s *State) Hours() float64 { return float64(s.ObservedMs()) / float64(time.Hour.Milliseconds()) }

// Camera is the camera id's series, nil when the run has none.
func (s *State) Camera(id string) *CameraState {
	for i := range s.Cameras {
		if s.Cameras[i].Camera == id {
			return &s.Cameras[i]
		}
	}
	return nil
}

// BeginSession opens a session at nowMs. When the run had one, the stretch
// since its end is a gap: a reboot when bootID differs from the last
// session's boot, else mi8-stopped.
func (s *State) BeginSession(nowMs int64, bootID string, sameBoot func(a, b string) bool) {
	if n := len(s.Sessions); n > 0 {
		last := s.Sessions[n-1].EndMs
		if s.HeartbeatMs > last {
			last = s.HeartbeatMs
		}
		reason := results.GapMI8Stopped
		if s.BootID != "" && bootID != "" && !sameBoot(s.BootID, bootID) {
			reason = results.GapReboot
		}
		if nowMs > last {
			s.Gaps = append(s.Gaps, results.Gap{StartMs: last, EndMs: nowMs, Reason: reason})
		}
	}
	s.Sessions = append(s.Sessions, Session{StartMs: nowMs, EndMs: nowMs})
	s.BootID = bootID
	s.HeartbeatMs = nowMs
}

// Meta is what an envelope says of its host.
type Meta struct {
	Host, Set, OS, Storage string
	PiModel                *results.PiModelInfo
	SdConfirmation         *results.SdConfirmation
	TestSeams              bool
}

// Envelope is the run's MI8 envelope at nowMs.
func (s *State) Envelope(meta Meta, nowMs int64) results.Envelope {
	data := &results.Mi8Data{Hours: s.Hours(), QuietGapSecs: s.QuietGapSecs, Cameras: []results.Mi8Camera{}}
	for _, c := range s.Cameras {
		data.Cameras = append(data.Cameras, c.Result())
	}
	end := s.StartMs
	if n := len(s.Sessions); n > 0 {
		end = s.Sessions[n-1].EndMs
	}
	return results.Envelope{Schema: results.Schema, Code: "MI8", RunID: s.RunID, Set: meta.Set, Host: meta.Host, OS: meta.OS,
		PiModel: meta.PiModel, Storage: meta.Storage, SdConfirmation: meta.SdConfirmation, Cameras: len(s.Cameras),
		TestSeams: meta.TestSeams || s.TestSeams, AtMs: nowMs,
		Runs: []results.Run{{RunID: s.RunID, StartMs: s.StartMs, EndMs: end, Valid: true, Gaps: slices.Clone(s.Gaps)}},
		MI8:  data}
}

// tracker follows one camera during a session: the motion events of the
// last moments, the reference's episodes, and the matches between them.
type tracker struct {
	mu     sync.Mutex
	st     *CameraState
	lag    func() time.Duration
	events []int64 // motion events' receipt times, the last eventKeep
	open   *results.EpisodeTime
}

// eventKeep is how long a motion event is kept for an episode that starts
// after it: the reference's delay is capped at 3 s.
const eventKeep = 10 * time.Second

// onEvent records a camera's notification at the hub's receipt time.
func (t *tracker) onEvent(atMs int64, motion bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.st.Subscribed = true // an event comes only through a subscription
	if !motion {
		return
	}
	t.events = append(t.events, atMs)
	t.events = slices.DeleteFunc(t.events, func(e int64) bool { return e < atMs-eventKeep.Milliseconds() })
	if t.open != nil && !t.open.Matched && Matches(t.open.StartMs, atMs, t.lag(), MatchWindow) {
		t.open.Matched = true
	}
}

// onSignal records a subscription's signal.
func (t *tracker) onSignal(kind string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch kind {
	case KindLost:
		t.st.SilentLosses++
	case KindSubscribeFailed:
		t.st.SubscribeFailed = true
	case KindSubscribed, KindHealthy, KindUnhealthy:
		t.st.Subscribed = true
	}
}

// onEpisodeStart opens an episode at the first motion frame's receipt time;
// an event already received within its window matches it.
func (t *tracker) onEpisodeStart(startMs int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := &results.EpisodeTime{StartMs: startMs, EndMs: startMs}
	for _, at := range t.events {
		if Matches(startMs, at, t.lag(), MatchWindow) {
			e.Matched = true
		}
	}
	t.open = e
}

// onEpisodeEnd closes the open episode at ep's end and adds it to the
// camera's series.
func (t *tracker) onEpisodeEnd(ep Episode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open == nil || t.open.StartMs != ep.StartMs {
		t.open = &results.EpisodeTime{StartMs: ep.StartMs}
		for _, at := range t.events {
			if Matches(ep.StartMs, at, t.lag(), MatchWindow) {
				t.open.Matched = true
			}
		}
	}
	t.open.EndMs = ep.EndMs
	t.st.Episodes = append(t.st.Episodes, *t.open)
	t.open = nil
}

// snapshot copies the camera's state, with its reference's delay now.
func (t *tracker) snapshot() CameraState {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := *t.st
	c.Episodes = slices.Clone(t.st.Episodes)
	if t.lag != nil && c.ReferenceMethod != "" {
		c.RefLagMs = t.lag().Milliseconds()
	}
	return c
}
