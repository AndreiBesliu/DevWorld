package source

import (
	"testing"
	"time"
)

// runTicks fires n ticks on the clock, one frame period apart, and returns the
// record of each frame. It waits for each record before the next tick, so the
// clock reading that the simulator takes for a tick is the one the test set.
func runTicks(t *testing.T, clock *fakeClock, src Source, fps, n int) []SentRecord {
	t.Helper()
	period := time.Second / time.Duration(fps)
	start := clock.Now()
	recs := make([]SentRecord, 0, n)
	for k := 1; k <= n; k++ {
		clock.tickAt(t, start.Add(time.Duration(k)*period))
		recs = append(recs, waitFor(t, src.SentLog()))
	}
	return recs
}

func TestSim_PacesAtFPS(t *testing.T) {
	clock := newFakeClock()
	src := NewSim("sim-03/sub", 3, synthAUs(720, 12), 12, 0, clock, WithStreamProfile("sub"))

	if got := clock.periods(); len(got) != 1 || got[0] != time.Second/12 {
		t.Fatalf("tickers = %v, want one ticker of %v", got, time.Second/12)
	}
	_, live, cancel := src.Subscribe()
	defer cancel()

	recs := runTicks(t, clock, src, 12, 60)

	for i := 1; i < len(recs); i++ {
		d := recs[i].At.Sub(recs[i-1].At)
		if d < 82300*time.Microsecond || d > 84300*time.Microsecond {
			t.Fatalf("records %d and %d are %v apart, want 83.3 ms +/- 1 ms", i-1, i, d)
		}
	}
	for i := range recs {
		au := waitFor(t, live)
		if int(au.Frame) != i {
			t.Fatalf("access unit %d has Frame %d", i, au.Frame)
		}
	}
	select {
	case au := <-live:
		t.Fatalf("a 61st access unit arrived: frame %d", au.Frame)
	default:
	}
}

func TestSim_ReportsWhatItSent(t *testing.T) {
	clock := newFakeClock()
	src := NewSim("sim-04/main", 4, synthAUs(40, 20), 20, 0, clock, WithStreamProfile("main"))

	recs := runTicks(t, clock, src, 20, 3)

	for i, r := range recs {
		if r.Camera != 4 || r.Profile != "main" || int(r.Frame) != i || r.Loop != 0 {
			t.Errorf("record %d = %+v, want camera 4, profile main, frame %d, loop 0", i, r, i)
		}
	}
	if src.ID() != "sim-04/main" || src.FPS() != 20 {
		t.Errorf("ID = %q, FPS = %d", src.ID(), src.FPS())
	}
}

func TestSim_LoopCounterIncrements(t *testing.T) {
	clock := newFakeClock()
	src := NewSim("sim-01/sub", 1, synthAUs(720, 12), 12, 0, clock)
	_, live, cancel := src.Subscribe()
	defer cancel()

	var recs []SentRecord
	var last AccessUnit
	start := clock.Now()
	for k := 1; k <= 722; k++ {
		clock.tickAt(t, start.Add(time.Duration(k)*time.Second/12))
		recs = append(recs, waitFor(t, src.SentLog()))
		last = waitFor(t, live)
	}

	if r := recs[719]; r.Frame != 719 || r.Loop != 0 {
		t.Errorf("last frame of the first pass = %+v, want frame 719 loop 0", r)
	}
	if r := recs[720]; r.Frame != 0 || r.Loop != 1 {
		t.Errorf("first frame of the second pass = %+v, want frame 0 loop 1", r)
	}
	if last.Frame != 1 || last.Loop != 1 || !equalNALUs(last.NALUs, synthAUs(720, 12)[1].NALUs) {
		t.Errorf("live access unit = frame %d loop %d", last.Frame, last.Loop)
	}
	if want := 721 * time.Second / 12; last.PTS != want {
		t.Errorf("PTS after a wrap = %v, want %v: it must keep increasing across loops", last.PTS, want)
	}
}

func TestSim_PhaseOffsetsDifferPerCamera(t *testing.T) {
	aus := synthAUs(48, 12)
	tests := []struct {
		name      string
		phase     int
		wantFirst uint16
	}{
		{"no phase", 0, 0},
		{"within the file", 5, 5},
		{"beyond the file wraps", 48 + 7, 7},
		{"negative counts from the end", -1, 47},
	}
	first := map[uint16]bool{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clock := newFakeClock()
			src := NewSim("sim", 1, aus, 12, tc.phase, clock)
			rec := runTicks(t, clock, src, 12, 1)[0]
			if rec.Frame != tc.wantFirst {
				t.Fatalf("first frame = %d, want %d", rec.Frame, tc.wantFirst)
			}
			first[rec.Frame] = true
		})
	}
	if len(first) != len(tests) {
		t.Fatalf("the four phases start at %d distinct frames, want %d", len(first), len(tests))
	}
}

func TestSim_StartsAtItsPhaseAndWrapsOnce(t *testing.T) {
	clock := newFakeClock()
	src := NewSim("sim", 2, synthAUs(24, 12), 12, 20, clock)
	recs := runTicks(t, clock, src, 12, 6)

	want := []struct{ frame, loop uint16 }{{20, 0}, {21, 0}, {22, 0}, {23, 0}, {0, 1}, {1, 1}}
	for i, w := range want {
		if recs[i].Frame != w.frame || recs[i].Loop != w.loop {
			t.Errorf("record %d = frame %d loop %d, want frame %d loop %d",
				i, recs[i].Frame, recs[i].Loop, w.frame, w.loop)
		}
	}
}

func TestNewSim_RejectsBadArguments(t *testing.T) {
	tests := []struct {
		name string
		make func()
	}{
		{"no access units", func() { NewSim("x", 1, nil, 12, 0, newFakeClock()) }},
		{"zero fps", func() { NewSim("x", 1, synthAUs(12, 12), 0, 0, newFakeClock()) }},
		{"unknown codec", func() {
			NewSim("x", 1, []AccessUnit{{NALUs: [][]byte{{0x7f, 0x01}}}}, 12, 0, newFakeClock())
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			tc.make()
		})
	}
}

func TestSim_ReportsStreamFacts(t *testing.T) {
	tests := []struct {
		file    string
		codec   Codec
		profile string
		fps     int
	}{
		{"sub.h264", CodecH264, "4d401f", 12},
		{"main.h264", CodecH264, "4d4028", 20},
		{"main-high51.h264", CodecH264, "640033", 20},
		{"main-h265.h265", CodecH265, "017b", 20},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			aus, err := ReadAnnexB(mediaFile(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			src := NewSim("sim", 1, aus, tc.fps, 0, newFakeClock())
			if src.Codec() != tc.codec || src.Profile() != tc.profile || src.FPS() != tc.fps {
				t.Fatalf("codec %s, profile %q, fps %d; want %s, %q, %d",
					src.Codec(), src.Profile(), src.FPS(), tc.codec, tc.profile, tc.fps)
			}
		})
	}
}
