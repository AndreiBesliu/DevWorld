package source

import "testing"

func TestGOP_SubscribeStartsAtLastIDR(t *testing.T) {
	clock := newFakeClock()
	src := NewSim("sim", 1, synthAUs(48, 12), 12, 0, clock)

	runTicks(t, clock, src, 12, 15) // frames 0..14; the last IDR is frame 12

	cached, live, cancel := src.Subscribe()
	defer cancel()

	if len(cached) != 3 || !cached[0].IDR {
		t.Fatalf("cached = %d access units, first IDR = %v; want frames 12..14 starting at an IDR",
			len(cached), len(cached) > 0 && cached[0].IDR)
	}
	for i, au := range cached {
		if au.Frame != uint16(12+i) {
			t.Errorf("cached[%d].Frame = %d, want %d", i, au.Frame, 12+i)
		}
	}

	// The next frame arrives live, exactly once and right after the cache.
	runTicks(t, clock, src, 12, 1)
	if au := waitFor(t, live); au.Frame != 15 {
		t.Errorf("first live frame = %d, want 15", au.Frame)
	}
}

func TestGOP_SubscribeOnAKeyframeCachesOnlyIt(t *testing.T) {
	clock := newFakeClock()
	src := NewSim("sim", 1, synthAUs(48, 12), 12, 0, clock)

	runTicks(t, clock, src, 12, 13) // the newest frame is the IDR at 12

	cached, _, cancel := src.Subscribe()
	defer cancel()
	if len(cached) != 1 || cached[0].Frame != 12 || !cached[0].IDR {
		t.Fatalf("cached = %+v, want only the IDR at frame 12", cached)
	}
}

func TestGOP_DeliversNothingBeforeTheFirstIDR(t *testing.T) {
	clock := newFakeClock()
	// Starts at frame 5 of a 12-frame GOP: frames 5..11 cannot be decoded.
	src := NewSim("sim", 1, synthAUs(48, 12), 12, 5, clock)
	cached, live, cancel := src.Subscribe()
	defer cancel()
	if len(cached) != 0 {
		t.Fatalf("cached = %d access units before any tick", len(cached))
	}

	runTicks(t, clock, src, 12, 8) // frames 5..12
	au := waitFor(t, live)
	if !au.IDR || au.Frame != 12 {
		t.Fatalf("first live access unit = frame %d, IDR %v; want the IDR at frame 12", au.Frame, au.IDR)
	}
	select {
	case extra := <-live:
		t.Fatalf("unexpected extra access unit: frame %d", extra.Frame)
	default:
	}
}

func TestGOP_SlowSubscriberDoesNotBlockAndResumesAtIDR(t *testing.T) {
	g := newGOP()
	_, live, cancel := g.subscribe()
	defer cancel()

	idr := func(f uint16) AccessUnit { return AccessUnit{Frame: f, IDR: true, NALUs: [][]byte{{0x65}}} }
	p := func(f uint16) AccessUnit { return AccessUnit{Frame: f, NALUs: [][]byte{{0x41}}} }

	// Nobody reads: push far more than the channel holds. This must not block.
	done := make(chan struct{}, 1)
	go func() {
		g.push(idr(0))
		for f := 1; f <= subscriberBuffer+50; f++ {
			g.push(p(uint16(f)))
		}
		g.push(idr(1000)) // the channel is still full
		done <- struct{}{}
	}()
	waitFor(t, done)

	// The subscriber now drains what fitted: the IDR and the P frames after it.
	for f := 0; f < subscriberBuffer; f++ {
		if au := waitFor(t, live); int(au.Frame) != f {
			t.Fatalf("queued frame %d has Frame %d", f, au.Frame)
		}
	}

	// It is in the gap. P frames are dropped until the next IDR.
	g.push(p(1001))
	g.push(idr(2000))
	g.push(p(2001))

	if au := waitFor(t, live); au.Frame != 2000 || !au.IDR {
		t.Fatalf("after the gap got frame %d (IDR %v), want the IDR at 2000", au.Frame, au.IDR)
	}
	if au := waitFor(t, live); au.Frame != 2001 {
		t.Fatalf("got frame %d, want 2001", au.Frame)
	}
}

func TestGOP_CancelClosesTheChannelAndIsRepeatable(t *testing.T) {
	g := newGOP()
	_, live, cancel := g.subscribe()
	cancel()
	cancel()
	waitClosed(t, live)

	g.push(AccessUnit{Frame: 1, IDR: true, NALUs: [][]byte{{0x65}}}) // must not panic on a closed channel
}

func TestGOP_CloseEndsEverySubscription(t *testing.T) {
	g := newGOP()
	g.push(AccessUnit{Frame: 7, IDR: true, NALUs: [][]byte{{0x65}}})
	_, a, cancelA := g.subscribe()
	_, b, cancelB := g.subscribe()
	defer cancelA()
	defer cancelB()

	g.close()
	waitClosed(t, a)
	waitClosed(t, b)

	cached, live, cancel := g.subscribe()
	defer cancel()
	if len(cached) != 1 || cached[0].Frame != 7 {
		t.Errorf("a closed source keeps its last GOP, got %+v", cached)
	}
	waitClosed(t, live)
}

func TestGOP_DropsAGOPThatNeverEnds(t *testing.T) {
	g := newGOP()
	g.push(AccessUnit{Frame: 0, IDR: true, NALUs: [][]byte{{0x65}}})
	for f := 1; f <= maxGOPFrames; f++ {
		g.push(AccessUnit{Frame: uint16(f), NALUs: [][]byte{{0x41}}})
	}
	if cached, _, cancel := g.subscribe(); len(cached) != 0 {
		t.Fatalf("a GOP of %d frames without a new IDR is still cached", len(cached))
	} else {
		cancel()
	}

	g.push(AccessUnit{Frame: 5000, IDR: true, NALUs: [][]byte{{0x65}}})
	if cached, _, cancel := g.subscribe(); len(cached) != 1 || cached[0].Frame != 5000 {
		t.Fatalf("after the next IDR cached = %+v", cached)
	} else {
		cancel()
	}
}
