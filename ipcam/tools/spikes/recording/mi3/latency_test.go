package mi3

import (
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/controller"
)

// TestLatency_FromSEIIndexOnControllerClock (§4.8 latency row): the added
// latency of a tile is the controller's receive time of an access unit
// minus the send time of its SEI index in the controller's send log, both
// on the controller's clock: nothing of the host's clock or of the RTP
// timestamps counts. An access unit without the index, or one whose index
// the stream's log does not hold, adds no sample.
func TestLatency_FromSEIIndexOnControllerClock(t *testing.T) {
	t0 := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	log := controller.NewSendLog()
	slice := [][]byte{{0x41, 0x9a, 0x01}}
	for i := range 100 {
		log.Record("cam01_sub", uint64(i), t0.Add(time.Duration(i)*time.Second))
	}
	l := NewLatencies(log)
	// Each index arrives 1 to 100 ms after it was sent.
	for i := range 100 {
		l.Arrived("cam01_sub", controller.StampIndex(slice, uint64(i)), t0.Add(time.Duration(i)*time.Second+time.Duration(i+1)*time.Millisecond))
	}
	l.Arrived("cam01_sub", slice, t0.Add(time.Hour))                                         // no SEI index
	l.Arrived("cam01_sub", controller.StampIndex(slice, 500), t0.Add(time.Hour))             // never sent
	l.Arrived("cam02_sub", controller.StampIndex(slice, 3), t0.Add(3*time.Second+time.Hour)) // another stream's log
	p95, n := l.P95()
	if n != 100 || p95 != 95*time.Millisecond {
		t.Errorf("P95 = %v over %d samples, want 95ms over 100", p95, n)
	}

	one := NewLatencies(log)
	one.Arrived("cam01_sub", controller.StampIndex(slice, 41), t0.Add(41*time.Second+35*time.Millisecond))
	if p95, n := one.P95(); n != 1 || p95 != 35*time.Millisecond {
		t.Errorf("one sample: P95 = %v over %d, want 35ms over 1", p95, n)
	}
	if p95, n := NewLatencies(log).P95(); n != 0 || p95 != 0 {
		t.Errorf("no sample: P95 = %v over %d, want 0 over 0", p95, n)
	}
}
