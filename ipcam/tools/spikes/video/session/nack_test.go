package session

import (
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// The hub sends again a packet that the viewer reports lost (R50, spec §6.3
// step 10). Pion's NACK responder resends only while someone reads the
// sender's RTCP; without a reader, the browser waits in vain for the
// retransmission and the tile freezes until the next keyframe.
func TestSession_AnswersANACKWithTheLostPacket(t *testing.T) {
	sim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 24, 12), 12)
	c := newClient(t, clientConfig{slots: 1, offer: chromeLike, nackProbe: true})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": sim}, testOptions(source.SystemClock{}, nil))
	c.bind(t, 0, "sim-01", "sub", 1)
	c.next(t, "bound")

	sim.emit(t, 3)
	lost := waitFor(t, c.rtp[0])
	// The test reports the slot's first packet as lost, a few times, as a
	// receiver does until the packet comes.
	nack := []rtcp.Packet{&rtcp.TransportLayerNack{
		SenderSSRC: 1,
		MediaSSRC:  lost.SSRC,
		Nacks:      rtcp.NackPairsFromSequenceNumbers([]uint16{lost.SequenceNumber}),
	}}
	deadline := time.After(5 * time.Second)
	retry := time.NewTicker(200 * time.Millisecond)
	defer retry.Stop()
	for {
		if err := c.pc.WriteRTCP(nack); err != nil {
			t.Fatalf("write the NACK: %v", err)
		}
		if resent(t, c.rtp[0], lost, retry.C, deadline) {
			return
		}
	}
}

// resent reads packets until one is lost sent again, which it reports, or
// until retry ticks (false). It fails the test at the deadline.
func resent(t *testing.T, packets <-chan *rtp.Packet, lost *rtp.Packet, retry <-chan time.Time, deadline <-chan time.Time) bool {
	t.Helper()
	for {
		select {
		case p := <-packets:
			if p.SSRC == lost.SSRC && p.SequenceNumber == lost.SequenceNumber {
				if string(p.Payload) != string(lost.Payload) || p.Timestamp != lost.Timestamp {
					t.Fatalf("packet %d came again with another payload or timestamp", lost.SequenceNumber)
				}
				return true
			}
		case <-retry:
			return false
		case <-deadline:
			t.Fatalf("the hub did not send packet %d again within 5 s of the NACKs", lost.SequenceNumber)
		}
	}
}
