package mi3

import (
	"testing"
	"time"
)

// frames feeds c the frames of one tile of session at fps from first to
// last, but every drop-th frame (none when drop is 0), as a lossy link
// delivers them.
func frames(c *Coverage, session int, tile string, fps int, first, last time.Time, drop int) {
	period := time.Second / time.Duration(fps)
	for i, at := 0, first; !at.After(last); i, at = i+1, at.Add(period) {
		if drop > 0 && i%drop == drop-1 {
			continue
		}
		c.Arrived(session, tile, at)
	}
}

// TestCoverage_HealthyTilesCoverTheWindow (mis-2, Final fix C): a live
// tile counts from the moment every tile was bound to the step's end, for
// as long as no pause between its frames is longer than 2 s. The frames
// that reach the controller only from the tiles' next keyframe, up to a
// second after the binds (measured on the smoke: 0.3 to 0.8 s), and the
// frames a LAN loses at random (the live peers ask for no
// retransmission), leave a healthy tile its whole window; frames from
// before the binds do not count.
func TestCoverage_HealthyTilesCoverTheWindow(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	tiles := []string{"cam01_sub", "cam02_sub", "cam01_main"}
	c := NewCoverage(2, tiles)
	c.Arrived(1, "cam01_sub", t0.Add(-500*time.Millisecond)) // before the binds: ignored
	c.Start(t0)
	end := t0.Add(12*time.Second + 130*time.Millisecond)
	for session := 1; session <= 2; session++ {
		frames(c, session, "cam01_sub", 12, t0.Add(825*time.Millisecond), end.Add(-50*time.Millisecond), 10)
		frames(c, session, "cam02_sub", 12, t0.Add(825*time.Millisecond), end.Add(-50*time.Millisecond), 7)
		frames(c, session, "cam01_main", 20, t0.Add(322*time.Millisecond), end.Add(-50*time.Millisecond), 5)
	}
	least := c.Least(end)
	if want := end.Sub(t0).Seconds(); least.Seconds < want-0.001 || least.Seconds > want+0.001 {
		t.Errorf("Least = %+v, want every tile covering the %.3f s from the binds to the end", least, want)
	}
}

// TestCoverage_AStoppedTileCoversOnlyUntilItStopped (mis-2): a tile whose
// source stops (an RTSP pull without reconnect) covers only the time until
// its last frame, and a pause longer than 2 s in the middle of the window
// is not covered either; Least names the least covered tile and its
// session, counted from 1. A tile that received nothing after the binds
// covers nothing.
func TestCoverage_AStoppedTileCoversOnlyUntilItStopped(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	tiles := []string{"cam01_sub", "cam02_sub", "cam01_main"}
	end := t0.Add(time.Hour)
	full := func(c *Coverage, session int) {
		for _, tile := range tiles {
			frames(c, session, tile, 12, t0, end, 0)
		}
	}

	stopped := NewCoverage(3, tiles)
	stopped.Start(t0)
	full(stopped, 1)
	full(stopped, 3)
	frames(stopped, 2, "cam01_sub", 12, t0, end, 0)
	frames(stopped, 2, "cam01_main", 12, t0, end, 0)
	frames(stopped, 2, "cam02_sub", 12, t0, t0.Add(10*time.Minute), 0) // stops at minute 10
	if got := stopped.Least(end); got.Session != 2 || got.Tile != "cam02_sub" || got.Seconds < 599.9 || got.Seconds > 600.1 {
		t.Errorf("a tile that stops at minute 10: Least = %+v, want cam02_sub of session 2 at 600 s", got)
	}

	paused := NewCoverage(1, tiles)
	paused.Start(t0)
	frames(paused, 1, "cam01_sub", 12, t0, end, 0)
	frames(paused, 1, "cam02_sub", 12, t0, end, 0)
	frames(paused, 1, "cam01_main", 20, t0, t0.Add(20*time.Minute), 0)
	frames(paused, 1, "cam01_main", 20, t0.Add(25*time.Minute), end, 0) // 5 min without a frame
	if got := paused.Least(end); got.Session != 1 || got.Tile != "cam01_main" || got.Seconds < 3299.9 || got.Seconds > 3300.1 {
		t.Errorf("a tile paused for 5 min: Least = %+v, want cam01_main of session 1 at 3300 s", got)
	}

	silent := NewCoverage(1, tiles)
	silent.Arrived(1, "cam01_main", t0.Add(-time.Second))
	silent.Start(t0)
	frames(silent, 1, "cam01_sub", 12, t0, end, 0)
	frames(silent, 1, "cam02_sub", 12, t0, end, 0)
	if got := silent.Least(end); got.Tile != "cam01_main" || got.Seconds != 0 {
		t.Errorf("a tile without a frame after the binds: Least = %+v, want cam01_main at 0 s", got)
	}
}
