package controller

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// steadyBeacons returns n beacons that arrive every BeaconEvery from start,
// each up to 3 ms late, as on a LAN.
func steadyBeacons(start time.Time, n int) []Beacon {
	beacons := make([]Beacon, n)
	for i := range beacons {
		late := time.Duration(i*7%4) * time.Millisecond
		beacons[i] = Beacon{Seq: uint64(i), At: start.Add(time.Duration(i)*BeaconEvery + late)} //nolint:gosec // i >= 0
	}
	return beacons
}

// TestStopMoment_IsLastBeacon_RegardlessOfShutdownKind (Review Focus 5): a
// laptop whose battery holds power is stopped by a forced shutdown, not a
// plug pull; the stop moment is the last beacon's arrival for every kind,
// within 100 ms for a steady 50 ms stream.
func TestStopMoment_IsLastBeacon_RegardlessOfShutdownKind(t *testing.T) {
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	beacons := steadyBeacons(start, 200) // 10 s
	last := beacons[len(beacons)-1].At
	for _, kind := range append(append([]ShutdownKind{}, automatedKinds...), realKinds...) {
		at, errBound := StopMoment(kind, beacons)
		if !at.Equal(last) {
			t.Errorf("%s: StopMoment = %v, want the last arrival %v", kind, at, last)
		}
		if errBound < BeaconEvery || errBound > 100*time.Millisecond {
			t.Errorf("%s: errBound = %v, want within [%v, 100 ms]", kind, errBound, BeaconEvery)
		}
	}

	t.Run("a late beacon widens the bound", func(t *testing.T) {
		late := steadyBeacons(start, 200)
		late[195].At = late[195].At.Add(120 * time.Millisecond)
		_, errBound := StopMoment(KindPower, late)
		if errBound < BeaconEvery+120*time.Millisecond-3*time.Millisecond {
			t.Errorf("errBound = %v after a beacon 120 ms late, want at least %v", errBound, BeaconEvery+117*time.Millisecond)
		}
	})

	t.Run("lost beacons and reordering", func(t *testing.T) {
		b := steadyBeacons(start, 200)
		b = append(b[:150], b[153:]...) // three lost in the network
		b[190], b[191] = b[191], b[190] // two swapped near the end
		at, errBound := StopMoment(KindKill9, b)
		if want := b[len(b)-1].At; !at.Equal(want) || errBound > 100*time.Millisecond {
			t.Errorf("StopMoment = %v, %v; want %v within 100 ms", at, errBound, want)
		}
	})

	t.Run("one beacon or none", func(t *testing.T) {
		at, errBound := StopMoment(KindSysrq, beacons[:1])
		if !at.Equal(beacons[0].At) || errBound != BeaconEvery {
			t.Errorf("one beacon: StopMoment = %v, %v; want %v, %v", at, errBound, beacons[0].At, BeaconEvery)
		}
		if at, errBound := StopMoment(KindSysrq, nil); !at.IsZero() || errBound != 0 {
			t.Errorf("no beacon: StopMoment = %v, %v; want the zero time", at, errBound)
		}
	})
}

// TestBeaconLog_KeepsExpectedRunsOnly: the controller stamps each beacon of
// an expected run with its own clock; a beacon of another run, or a packet
// that is no beacon, is dropped.
func TestBeaconLog_KeepsExpectedRunsOnly(t *testing.T) {
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	beacons := NewBeaconLog()
	beacons.Expect("pc-kill9-3")
	done := make(chan error, 1)
	go func() { done <- ReceiveBeacons(t.Context(), conn, beacons, source.SystemClock{}) }()

	out, err := net.Dial("udp4", conn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = out.Close() })
	before := time.Now()
	for seq := range uint64(80) {
		for _, packet := range [][]byte{link.BeaconPacket("pc-kill9-3", seq), link.BeaconPacket("pc-kill9-2", seq), []byte("not a beacon")} {
			if _, err := out.Write(packet); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(time.Millisecond)
	}
	waitUntil(t, "80 beacons arrived", func() bool {
		got := beacons.Beacons("pc-kill9-3")
		return len(got) > 0 && got[len(got)-1].Seq == 79
	})
	got := beacons.Beacons("pc-kill9-3")
	if got[0].Seq != 0 || got[0].At.Before(before) || got[len(got)-1].At.After(time.Now()) {
		t.Errorf("beacons = first %+v, last %+v; want seq 0 first, stamped on arrival", got[0], got[len(got)-1])
	}
	for i := 1; i < len(got); i++ {
		if got[i].At.Before(got[i-1].At) {
			t.Fatalf("beacon %d arrived before beacon %d", i, i-1)
		}
	}
	if first, ok := beacons.First("pc-kill9-3"); !ok || first.Seq != 0 {
		t.Errorf("First = %+v, %v; want seq 0", first, ok)
	}
	if other := beacons.Beacons("pc-kill9-2"); len(other) != 0 {
		t.Errorf("kept %d beacons of a run that was not expected", len(other))
	}
	beacons.Forget("pc-kill9-3")
	if left := beacons.Beacons("pc-kill9-3"); len(left) != 0 {
		t.Errorf("kept %d beacons after Forget", len(left))
	}

	_ = conn.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ReceiveBeacons did not return after its connection closed")
	}
}

// TestBeaconLog_BoundedPerRun: a run that beacons for hours keeps its first
// beacon and its newest ones, which are all that BootAt and StopMoment use.
func TestBeaconLog_BoundedPerRun(t *testing.T) {
	beacons := NewBeaconLog()
	beacons.Expect("mi2")
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	const n = 72 * 3600 * 20 / 100 // a 72 h stream, thinned to keep the test fast
	for seq := range uint64(n) {
		beacons.add(link.RunHash("mi2"), seq, start.Add(time.Duration(seq)*BeaconEvery)) //nolint:gosec // seq < n
	}
	got := beacons.Beacons("mi2")
	if len(got) > beaconsKept+1 {
		t.Errorf("kept %d beacons, want at most %d", len(got), beaconsKept+1)
	}
	if got[0].Seq != 0 || got[len(got)-1].Seq != n-1 {
		t.Errorf("kept seq %d … %d, want 0 … %d", got[0].Seq, got[len(got)-1].Seq, n-1)
	}
	if _, errBound := StopMoment(KindKill9, got); errBound != BeaconEvery {
		t.Errorf("errBound over the kept beacons = %v, want %v", errBound, BeaconEvery)
	}
}

// TestBeaconLog_RejectsLateBeacons (C16): a run's beacons end at its first
// silence longer than BeaconSilence. recspike never beacons again under a
// run once its child has stopped, so a later beacon of the run is late, from
// a sender that must not move the stop: it is dropped, and so is every one
// after it.
func TestBeaconLog_RejectsLateBeacons(t *testing.T) {
	beacons := NewBeaconLog()
	beacons.Expect("pi-sysrq-04-1")
	hash := link.RunHash("pi-sysrq-04-1")
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	steady := steadyBeacons(start, 40)
	for _, b := range steady {
		beacons.add(hash, b.Seq, b.At)
	}
	last := steady[len(steady)-1]
	beacons.add(hash, 40, last.At.Add(BeaconSilence+time.Millisecond))
	beacons.add(hash, 41, last.At.Add(BeaconSilence+BeaconEvery))
	got := beacons.Beacons("pi-sysrq-04-1")
	if len(got) != len(steady) || got[len(got)-1] != last {
		t.Errorf("kept %d beacons ending with %+v, want the %d before the silence, ending with %+v", len(got), got[len(got)-1], len(steady), last)
	}
	if at, _ := StopMoment(KindSysrq, got); !at.Equal(last.At) {
		t.Errorf("StopMoment = %v, want the last beacon before the silence, %v", at, last.At)
	}

	t.Run("a pause shorter than the silence", func(t *testing.T) {
		beacons.Expect("pc-kill9-05-1")
		h := link.RunHash("pc-kill9-05-1")
		beacons.add(h, 0, start)
		beacons.add(h, 1, start.Add(BeaconSilence))
		if got := beacons.Beacons("pc-kill9-05-1"); len(got) != 2 {
			t.Errorf("kept %d beacons after a pause of exactly BeaconSilence, want 2", len(got))
		}
	})
}

// TestBeaconLog_WaitSilence: WaitSilence returns a run's last beacon once
// the controller's clock is more than BeaconSilence past it, so that the
// controller can mark the end of the run's pacing near its stop (R07).
func TestBeaconLog_WaitSilence(t *testing.T) {
	beacons := NewBeaconLog()
	beacons.Expect("lab-linux-kill9-01-1")
	clock := newFakeClock()
	start := clock.Now()
	type result struct {
		b   Beacon
		err error
	}
	done := make(chan result, 1)
	go func() {
		b, err := beacons.WaitSilence(t.Context(), "lab-linux-kill9-01-1", clock)
		done <- result{b, err}
	}()
	hash := link.RunHash("lab-linux-kill9-01-1")
	for seq := range uint64(10) {
		at := start.Add(time.Duration(seq) * BeaconEvery) //nolint:gosec // seq < 10
		beacons.add(hash, seq, at)
		clock.tickAt(t, at)
	}
	lastAt := start.Add(9 * BeaconEvery)
	clock.tickAt(t, lastAt.Add(BeaconSilence))
	select {
	case r := <-done:
		t.Fatalf("WaitSilence returned %+v, %v after a silence of only BeaconSilence", r.b, r.err)
	default:
	}
	clock.tickAt(t, lastAt.Add(BeaconSilence+BeaconEvery))
	select {
	case r := <-done:
		if r.err != nil || r.b.Seq != 9 || !r.b.At.Equal(lastAt) {
			t.Errorf("WaitSilence = %+v, %v; want seq 9 at %v", r.b, r.err, lastAt)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("WaitSilence did not return after the silence")
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := beacons.WaitSilence(ctx, "lab-linux-kill9-02-1", clock); !errors.Is(err, context.Canceled) {
		t.Errorf("WaitSilence on a cancelled context = %v, want context.Canceled", err)
	}
}
