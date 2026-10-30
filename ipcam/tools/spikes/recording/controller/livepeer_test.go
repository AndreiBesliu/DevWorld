package controller

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/session"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// stampedAUs are n H.264 Main access units with an IDR every gop frames,
// each with base+i in its SEI index, as the RTSP server stamps them.
func stampedAUs(base uint64, n, gop int) []source.AccessUnit {
	sps := []byte{0x67, 0x4d, 0x00, 0x1f, 0xac, 0xd9}
	pps := []byte{0x68, 0xee, 0x3c, 0x80}
	aus := make([]source.AccessUnit, n)
	for i := range aus {
		au := source.AccessUnit{Frame: uint16(i)} //nolint:gosec // a few frames
		if i%gop == 0 {
			au.IDR = true
			au.NALUs = [][]byte{sps, pps, {0x65, 0x88, byte(i), 0xff}}
		} else {
			au.NALUs = [][]byte{{0x41, 0x9a, byte(i), 0xff}}
		}
		au.NALUs = StampIndex(au.NALUs, base+uint64(i)) //nolint:gosec // i < n
		aus[i] = au
	}
	return aus
}

// arrivals collects what a live peer received, per stream.
type arrivals struct {
	mu      sync.Mutex
	indexes map[string][]uint64
	last    time.Time
}

func (a *arrivals) add(stream string, nalus [][]byte, at time.Time) {
	index, ok := ReadIndex(nalus)
	if !ok {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.indexes[stream] = append(a.indexes[stream], index)
	a.last = at
}

func (a *arrivals) snapshot() map[string][]uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string][]uint64{}
	for k, v := range a.indexes {
		out[k] = slices.Clone(v)
	}
	return out
}

// TestLivePeer_ReceivesEachTileOfTheGrid (G20): a live peer offers one
// receive-only video m-line per tile and the rpc channel; answered by the
// video spike's session, it binds each tile's stream, every camera's sub
// then one main, and hands each access unit it receives to the callback
// under that tile's stream, with the controller's time, so that its SEI
// index can be looked up in the send log.
func TestLivePeer_ReceivesEachTileOfTheGrid(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	clock := ctxClock{ctx: ctx, Clock: source.SystemClock{}}
	sources := map[string]source.Source{
		"cam01/sub":  source.NewSim("cam01_sub", 1, stampedAUs(1000, 240, 12), 12, 0, clock),
		"cam02/sub":  source.NewSim("cam02_sub", 2, stampedAUs(2000, 240, 12), 12, 0, clock),
		"cam01/main": source.NewSim("cam01_main", 1, stampedAUs(3000, 400, 20), 20, 0, clock),
	}
	api, err := NewLiveAPI()
	if err != nil {
		t.Fatal(err)
	}
	got := &arrivals{indexes: map[string][]uint64{}}
	peer, err := NewLivePeer(api, []string{"cam01_sub", "cam02_sub", "cam01_main"}, source.SystemClock{}, got.add)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	hubAPI, _, err := session.NewAPI(false)
	if err != nil {
		t.Fatal(err)
	}
	s, answer, err := session.Answer(hubAPI, peer.Offer(), sources)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	acceptCtx, cancelAccept := context.WithTimeout(ctx, 20*time.Second)
	defer cancelAccept()
	if err := peer.Accept(acceptCtx, answer); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	want := map[string]uint64{"cam01_sub": 1000, "cam02_sub": 2000, "cam01_main": 3000}
	deadline := time.Now().Add(20 * time.Second)
	for {
		snap := got.snapshot()
		complete := true
		for stream := range want {
			complete = complete && len(snap[stream]) >= 5
		}
		if complete {
			for stream, base := range want {
				for _, index := range snap[stream] {
					if index < base || index >= base+1000 {
						t.Errorf("%s received index %d of another stream", stream, index)
					}
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after 20 s: %v, want 5 access units of each tile", snap)
		}
		time.Sleep(50 * time.Millisecond)
	}

	peer.Close()
	before := got.snapshot()
	time.Sleep(300 * time.Millisecond)
	for stream, indexes := range got.snapshot() {
		if len(indexes) != len(before[stream]) {
			t.Errorf("%s still received after Close", stream)
		}
	}
}

// TestLivePeer_RefusesABindTheHubCannotServe: a tile whose stream the hub
// does not have fails Accept, with the slot's error.
func TestLivePeer_RefusesABindTheHubCannotServe(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	clock := ctxClock{ctx: ctx, Clock: source.SystemClock{}}
	sources := map[string]source.Source{"cam01/sub": source.NewSim("cam01_sub", 1, stampedAUs(0, 24, 12), 12, 0, clock)}
	api, err := NewLiveAPI()
	if err != nil {
		t.Fatal(err)
	}
	peer, err := NewLivePeer(api, []string{"cam01_sub", "cam09_sub"}, source.SystemClock{}, func(string, [][]byte, time.Time) {})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	hubAPI, _, err := session.NewAPI(false)
	if err != nil {
		t.Fatal(err)
	}
	s, answer, err := session.Answer(hubAPI, peer.Offer(), sources)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	acceptCtx, cancelAccept := context.WithTimeout(ctx, 20*time.Second)
	defer cancelAccept()
	if err := peer.Accept(acceptCtx, answer); err == nil || !strings.Contains(err.Error(), "cam09_sub") || !strings.Contains(err.Error(), "UNKNOWN_CAMERA") {
		t.Errorf("Accept = %v, want the unknown camera's error", err)
	}
}
