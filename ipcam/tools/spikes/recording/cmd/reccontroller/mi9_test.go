package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/link"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi9"
)

// TestMI9_ControllerPlaysTheHostsRecording (G26 leg 3): when a host starts
// MI9's LAN run, the controller's playback peer offers; with the host's
// hub-side answer it plays the host's recording over the data channel and
// answers with the measurement: the throughput, 20 seeks and the pair's
// round trip.
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

	conn := r.connect("pi")
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
