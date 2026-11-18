package controller

import (
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// TestPacing_P99AndSkipped (R07): a run's pacing is the largest p99 send
// jitter of the streams since the run's mark, and the AUs that the server
// skipped between the mark and the stop; what happened before the mark or
// after the stop belongs to no run.
func TestPacing_P99AndSkipped(t *testing.T) {
	const fps = 20
	period := time.Second / fps
	clockA, clockB := newFakeClock(), newFakeClock()
	simA := source.NewSim("cam01_main", 1, synthAUs(60, fps), fps, 0, clockA)
	simB := source.NewSim("cam02_main", 2, synthAUs(60, fps), fps, 0, clockB)
	p := newPacing(map[string]source.Source{"cam01_main": simA, "cam02_main": simB})

	// tick sends one frame of a simulator at the given time and waits until it is sent.
	tick := func(clock *fakeClock, sim source.Source, at time.Time) {
		t.Helper()
		clock.tickAt(t, at)
		select {
		case <-sim.SentLog():
		case <-time.After(10 * time.Second):
			t.Fatal("the simulator sent nothing")
		}
	}
	atA, atB := clockA.Now(), clockB.Now()
	// Before the mark, cam02 sends one frame 1 s late, and the server skips AUs.
	for i := range 20 {
		atA = atA.Add(period)
		atB = atB.Add(period)
		if i == 10 {
			atB = atB.Add(time.Second)
		}
		tick(clockA, simA, atA)
		tick(clockB, simB, atB)
	}
	p.skipped(atA, 5)

	markAt := atA.Add(time.Millisecond)
	mark := p.mark(markAt)
	// During the run, 2 of cam01's 100 intervals are 120 ms late: p99 120 ms.
	// cam02 stays on time.
	for i := range 100 {
		atA = atA.Add(period)
		if i == 30 || i == 60 {
			atA = atA.Add(120 * time.Millisecond)
		}
		atB = atB.Add(period)
		tick(clockA, simA, atA)
		tick(clockB, simB, atB)
	}
	p.skipped(markAt.Add(time.Second), 1)
	p.skipped(markAt.Add(2*time.Second), 2)
	stop := markAt.Add(3 * time.Second)
	// The controller marks the end when the host's beacons have gone silent,
	// shortly after the stop.
	end := p.mark(stop.Add(BeaconSilence))
	// After the stop, a host that lost power fills the write queue, and the
	// controller, busy with the reboot's reconnect, sends cam02 five frames
	// 1 s late each, over 1 % of its intervals: none of it belongs to the run.
	p.skipped(stop.Add(4*time.Second), 40)
	for range 5 {
		atB = atB.Add(period + time.Second)
		tick(clockB, simB, atB)
	}

	got := p.between(mark, end, stop)
	if got.P99 != 120*time.Millisecond {
		t.Errorf("P99 = %v, want 120 ms (cam01's run, without cam02's delays before the mark and after the end)", got.P99)
	}
	if got.SkippedAUs != 3 {
		t.Errorf("SkippedAUs = %d, want 3 (between the mark and the stop)", got.SkippedAUs)
	}

	now := p.mark(atA)
	if quiet := p.between(now, p.mark(atA.Add(time.Minute)), atA.Add(time.Minute)); quiet.P99 != 0 || quiet.SkippedAUs != 40 {
		t.Errorf("a run that starts now: %+v, want P99 0 and the 40 later skips", quiet)
	}
}
