package main

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

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
