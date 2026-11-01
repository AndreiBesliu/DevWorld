// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"sync"
	"time"
)

// liveGap is the longest pause between two frames of a live tile that
// still counts as receiving (mis-2): two GOPs of the simulated cameras,
// whose keyframe comes every second. It covers the start, where a tile's
// frames reach the controller from its next keyframe after the binds (0.3
// to 0.8 s on the smoke), and the frames that a LAN loses at random, which
// the live peers do not ask again (24 lost in a row at 12 fps). A tile
// whose source stopped, an RTSP pull without reconnect, leaves a longer
// pause.
const liveGap = 2 * time.Second

// Coverage is how long each live tile of a step's sessions received frames
// (mis-2): from Start, when every tile was bound, to the step's end, the
// time without a pause longer than liveGap between two of its frames, or
// between Start and its first frame, or its last frame and the end. A tile
// that stops at minute 10 covers 10 min of the hour; one that loses frames
// at random covers it all, which a count of its frames would not (Final fix
// C: the frames' count rejected healthy loads). It is safe for concurrent
// use.
type Coverage struct {
	mu    sync.Mutex
	order []tileKey
	tiles map[tileKey]*tileCoverage
}

// tileKey is a tile of a live session, counted from 1.
type tileKey struct {
	session int
	tile    string
}

// tileCoverage is a tile's covered time so far, and its last frame: a zero
// time until Start.
type tileCoverage struct {
	last    time.Time
	covered time.Duration
}

// LeastCovered is the tile of a session, counted from 1, that received
// frames for the fewest seconds.
type LeastCovered struct {
	Seconds float64
	Session int
	Tile    string
}

// NewCoverage returns the coverage of sessions live sessions of tiles,
// which counts from Start.
func NewCoverage(sessions int, tiles []string) *Coverage {
	c := &Coverage{tiles: map[tileKey]*tileCoverage{}}
	for session := 1; session <= sessions; session++ {
		for _, tile := range tiles {
			k := tileKey{session, tile}
			c.order = append(c.order, k)
			c.tiles[k] = &tileCoverage{}
		}
	}
	return c
}

// Start starts the coverage of every tile at at, when every tile is bound;
// the frames before it do not count.
func (c *Coverage) Start(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.tiles {
		t.last, t.covered = at, 0
	}
}

// Arrived takes a frame of tile in session, received at at.
func (c *Coverage) Arrived(session int, tile string, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tiles[tileKey{session, tile}]
	if t == nil || t.last.IsZero() || at.Before(t.last) {
		return
	}
	t.covered += covers(at.Sub(t.last))
	t.last = at
}

// Least is the least covered tile at end, the first in the order of the
// sessions and their tiles among equals.
func (c *Coverage) Least(end time.Time) LeastCovered {
	c.mu.Lock()
	defer c.mu.Unlock()
	var least LeastCovered
	for i, k := range c.order {
		t := c.tiles[k]
		covered := t.covered
		if !t.last.IsZero() && end.After(t.last) {
			covered += covers(end.Sub(t.last))
		}
		if s := covered.Seconds(); i == 0 || s < least.Seconds {
			least = LeastCovered{Seconds: s, Session: k.session, Tile: k.tile}
		}
	}
	return least
}

// covers is how much of a pause between two frames counts as receiving:
// all of it up to liveGap, nothing beyond.
func covers(pause time.Duration) time.Duration {
	if pause > liveGap {
		return 0
	}
	return pause
}
