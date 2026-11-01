package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/pion/webrtc/v4"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
)

// TestMI9_ControllerPlaysTheHostsRecording (G26 leg 3): when a host on the
// other device starts MI9's LAN run, the controller's playback peer offers;
// with the host's hub-side answer it plays the host's recording over the
// data channel and answers with the measurement: the throughput, 20 seeks
// and the pair's round trip.
func TestMI9_ControllerPlaysTheHostsRecording(t *testing.T) {
	r := newRig(t)
	api, err := mi9.NewAPI(mi9.LAN)
	if err != nil {
		t.Fatal(err)
	}
	r.c.mi9API = api
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cam01_main.mp4.age")
	if _, err := spiketest.WriteSegment(path, 10, id.Recipient()); err != nil {
		t.Fatal(err)
	}
	seg, err := mi9.OpenSegment(path, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = seg.Close() }()
	hub, err := mi9.NewHub(api, mi9.Configuration(mi9.LAN, nil), seg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hub.Close() }()

	conn := r.connectFrom("pi", &net.TCPAddr{IP: net.IPv4(192, 168, 1, 10), Port: link.Port}, &net.TCPAddr{IP: net.IPv4(192, 168, 1, 20), Port: 51000})
	r.send(conn, link.Mi9Start{RunID: "mi9-1"})
	offer, ok := r.receive(conn).(link.Mi9Offer)
	if !ok || offer.RunID != "mi9-1" {
		t.Fatalf("got %#v, want the playback peer's offer", offer)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	answer, err := hub.Answer(ctx, offer.Offer)
	if err != nil {
		t.Fatal(err)
	}
	r.send(conn, link.Mi9Answer{RunID: "mi9-1", Answer: answer})
	res, ok := r.receive(conn).(link.Mi9Result)
	if !ok || res.RunID != "mi9-1" || res.ThroughputMbps <= 0 || len(res.SeekMs) != mi9.Seeks {
		t.Errorf("got %#v, want the measurement of the run", res)
	}
}

// TestMI9_LocalControllerRefusesARun: a -local controller has no playback
// peer: MI9's LAN run plays on the other device, and its Pion sockets would
// listen beyond 127.0.0.1.
func TestMI9_LocalControllerRefusesARun(t *testing.T) {
	r := newRig(t)
	conn := r.connect("pc")
	r.send(conn, link.Mi9Start{RunID: "mi9-1"})
	if refused, ok := r.receive(conn).(link.Refused); !ok || refused.RunID != "mi9-1" || refused.Reason != noLocalMI9 {
		t.Errorf("got %#v, want the run refused: %s", refused, noLocalMI9)
	}
}

// pipeAddr is the address of a link that is not TCP.
type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

// TestMI9_RefusesAHostOnTheSameDevice (G26 leg 3): a controller started
// without -local refuses an MI9 run whose host is on its own device, from
// a loopback address or from the very address that it reached (recspike
// mi9 --controller <this device's own address>), and a link that does not
// show two IP addresses: the run would measure the loopback, hundreds of
// Mbps, as the LAN's. The refusal comes at the run's start, before the
// host records, and keeps no playback peer.
func TestMI9_RefusesAHostOnTheSameDevice(t *testing.T) {
	lan := func(d byte, port int) *net.TCPAddr { return &net.TCPAddr{IP: net.IPv4(192, 168, 1, d), Port: port} }
	for name, addrs := range map[string][2]net.Addr{
		"loopback":         {&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: link.Port}, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50000}},
		"loopback to LAN":  {lan(10, link.Port), &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50000}},
		"own LAN address":  {lan(10, link.Port), lan(10, 51000)},
		"IPv4-mapped self": {lan(10, link.Port), &net.TCPAddr{IP: net.ParseIP("::ffff:192.168.1.10"), Port: 51000}},
		"not TCP":          {pipeAddr{}, pipeAddr{}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			api, err := mi9.NewAPI(mi9.LAN)
			if err != nil {
				t.Fatal(err)
			}
			r.c.mi9API = api
			conn := r.connectFrom("pc", addrs[0], addrs[1])
			r.send(conn, link.Mi9Start{RunID: "mi9-1"})
			if refused, ok := r.receive(conn).(link.Refused); !ok || refused.RunID != "mi9-1" || refused.Reason != sameDeviceMI9 {
				t.Errorf("got %#v, want the run refused: %s", refused, sameDeviceMI9)
			}
			if want := "Proba MI9 mi9-1 nu se măsoară: " + sameDeviceMI9 + "."; !strings.Contains(r.screen.String(), want) {
				t.Errorf("the screen does not say «%s»:\n%s", want, r.screen.String())
			}
			r.c.mu.Lock()
			kept := len(r.c.mi9Runs)
			r.c.mu.Unlock()
			if kept != 0 {
				t.Errorf("%d playback peers kept after the refusal", kept)
			}
		})
	}
}

// TestMI9_AnswerOfAHostThatLeftAtOnce (Windows CI, d4bd2ab, as MI3's): a
// host that disconnects right after it sent its answer, before the
// controller applied it, is a host that disconnected, whichever comes
// first, the controller's playback or the closing of the connection's
// playback peer: the controller says so, and refuses nothing.
func TestMI9_AnswerOfAHostThatLeftAtOnce(t *testing.T) {
	r := newRig(t)
	api, err := mi9.NewAPI(mi9.LAN)
	if err != nil {
		t.Fatal(err)
	}
	r.c.mi9API = api
	const runs = 20
	for i := range runs {
		runID := fmt.Sprintf("mi9-%d", i+1)
		conn := r.connectFrom("pi", &net.TCPAddr{IP: net.IPv4(192, 168, 1, 10), Port: link.Port}, &net.TCPAddr{IP: net.IPv4(192, 168, 1, 20), Port: 51000})
		r.send(conn, link.Mi9Start{RunID: runID})
		offer, ok := r.receive(conn).(link.Mi9Offer)
		if !ok || offer.RunID != runID {
			t.Fatalf("got %#v, want the playback peer's offer", offer)
		}
		r.send(conn, link.Mi9Answer{RunID: runID, Answer: deadAnswer(t, api, offer.Offer)})
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		left := "Proba MI9 " + runID + ": gazda s-a deconectat înainte de final."
		for deadline := time.Now().Add(10 * time.Second); !strings.Contains(r.screen.String(), left); time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("the screen does not say that the host of %s disconnected:\n%s", runID, r.screen.String())
			}
		}
	}
	if screen := r.screen.String(); strings.Contains(screen, "nu a fost anunțată") || strings.Contains(screen, "redarea nu a reușit") {
		t.Errorf("the controller refused a run of a host that left:\n%s", screen)
	}
}

// slowHub is a hub side too slow for the measurement's wait: it answers
// the playback peer's calls as mi9.Hub does, the info at once and a read
// with its range of size bytes, but sends only the range's first sent
// bytes on bulk/<call>, then nothing more. It returns its answer to offer.
func slowHub(t *testing.T, api *webrtc.API, offer string, size, sent int64) string {
	t.Helper()
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	pc.OnDataChannel(func(rpc *webrtc.DataChannel) {
		if rpc.Label() != "rpc" {
			return
		}
		rpc.OnMessage(func(m webrtc.DataChannelMessage) {
			var req struct {
				T    string `json:"t"`
				Call uint64 `json:"call"`
			}
			if json.Unmarshal(m.Data, &req) != nil {
				return
			}
			switch req.T {
			case "info":
				_ = rpc.SendText(fmt.Sprintf(`{"t":"info","call":%d,"size":%d,"initBytes":1000,"codec":"avc1.42c028","lastIdrMs":9000}`, req.Call, size))
			case "read":
				go func() {
					bulk, err := pc.CreateDataChannel(fmt.Sprintf("bulk/%d", req.Call), nil)
					if err != nil {
						return
					}
					bulk.OnOpen(func() { _ = bulk.Send(make([]byte, sent)) })
					_ = rpc.SendText(fmt.Sprintf(`{"t":"range","call":%d,"n":%d}`, req.Call, size))
				}()
			}
		})
	})
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		t.Fatal(err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gathered:
	case <-time.After(30 * time.Second):
		t.Fatal("the slow hub side gathered no candidates")
	}
	return pc.LocalDescription().SDP
}

// TestMI9_SlowRunGivesItsPartialMeasurement (G27): a run too slow for the
// measurement's wait still decides. At the wait the controller stops the
// transfer and answers with what it measured: the throughput of the bytes
// received by then, those bytes and the time elapsed, and no seek. It
// does not refuse the run, which would leave the LAN part without a
// verdict.
func TestMI9_SlowRunGivesItsPartialMeasurement(t *testing.T) {
	realWait := mi9MeasureWait
	t.Cleanup(func() { mi9MeasureWait = realWait })
	mi9MeasureWait = 8 * time.Second
	r := newRig(t)
	api, err := mi9.NewAPI(mi9.LAN)
	if err != nil {
		t.Fatal(err)
	}
	r.c.mi9API = api
	conn := r.connectFrom("pi", &net.TCPAddr{IP: net.IPv4(192, 168, 1, 10), Port: link.Port}, &net.TCPAddr{IP: net.IPv4(192, 168, 1, 20), Port: 51000})
	r.send(conn, link.Mi9Start{RunID: "mi9-1"})
	offer, ok := r.receive(conn).(link.Mi9Offer)
	if !ok {
		t.Fatalf("got %#v, want the playback peer's offer", offer)
	}
	const size, sent = 4 << 20, 32 << 10
	r.send(conn, link.Mi9Answer{RunID: "mi9-1", Answer: slowHub(t, api, offer.Offer, size, sent)})
	m := r.receiveWithin(conn, time.Minute)
	res, ok := m.(link.Mi9Result)
	switch p := res.Partial; {
	case !ok || res.RunID != "mi9-1" || p == nil:
		t.Fatalf("got %#v, want the run's partial measurement", m)
	case p.ReceivedBytes != sent || p.SizeBytes != size || p.ElapsedMs <= 0 || len(res.SeekMs) != 0:
		t.Errorf("partial %+v and %d seeks, want the %d bytes received of %d, the time elapsed and no seek", *p, len(res.SeekMs), sent, size)
	case res.ThroughputMbps <= 0 || res.ThroughputMbps >= mi9.MinLANMbps:
		t.Errorf("throughput %v Mbps, want that of %d bytes in %v ms", res.ThroughputMbps, sent, p.ElapsedMs)
	}
	for _, want := range []string{"Proba MI9 mi9-1: redarea nu s-a încheiat în timpul probei: au sosit 0,0 MB din 4,2 MB în ", "; salturile nu s-au măsurat."} {
		if !strings.Contains(r.screen.String(), want) {
			t.Errorf("the screen does not say «%s»:\n%s", want, r.screen.String())
		}
	}
}
