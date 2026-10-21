package controller

import (
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
