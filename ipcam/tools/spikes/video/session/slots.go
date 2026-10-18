package session

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	// burstStep is the RTP timestamp step between the cached frames that a
	// bind sends at once. Compressed timestamps let the decoder catch up with
	// the current frame without adding the age of the GOP to the latency
	// (spec §6.3 step 6, HUB-06).
	burstStep = time.Millisecond
	// fallbackFrameInterval is the frame period of a source that does not
	// know its frame rate yet: an RTSP source before its second frame.
	fallbackFrameInterval = 50 * time.Millisecond
	// requestQueue is how many bind and unbind requests may wait for a slot.
	requestQueue = 16
)

var errNoSendableCodec = errors.New("the offer has no H.264 in packetization mode 1 and no H.265")

// binding asks a slot to carry a source, or to carry nothing when src is nil.
// fallback is the offered profile-level-id that a substream goes out in when
// the offer lacks its own profile (R23).
type binding struct {
	src      source.Source
	codec    webrtc.RTPCodecParameters
	fallback string
	epoch    int64
}

// slot is one recvonly video m-line of the client's offer. It writes the
// camera bound to it, first the cached GOP and then the live frames, into
// one RTP stream: a rebind or a change of codec keeps the SSRC and continues
// the sequence numbers and timestamps (spec §6.3 step 6).
type slot struct {
	index   int
	offered []webrtc.RTPCodecParameters // the codecs the answer lists for the slot
	sender  *webrtc.RTPSender
	reqs    chan binding
	notify  func(any)
	log     *slog.Logger

	// Owned by the writer goroutine once it runs.
	track     *webrtc.TrackLocalStaticSample
	codec     webrtc.RTPCodecParameters
	seq       uint16  // the sequence number of the next RTP packet
	ts        uint32  // the timestamp of the next sample
	remainder float64 // the fraction of a tick that the next sample inherits
}

// newSlot creates a slot and its first track, created at answer time in the
// first codec of the answer that the hub can send.
func newSlot(index int, offered []webrtc.RTPCodecParameters, notify func(any), log *slog.Logger) (*slot, error) {
	sl := &slot{index: index, offered: offered, reqs: make(chan binding, requestQueue), notify: notify, log: log}
	var b [6]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error
	sl.seq, sl.ts = binary.BigEndian.Uint16(b[:2]), binary.BigEndian.Uint32(b[2:])
	for _, c := range offered {
		if sendable(c) {
			track, err := sl.newTrack(c)
			if err != nil {
				return nil, err
			}
			sl.track, sl.codec = track, c
			return sl, nil
		}
	}
	return nil, fmt.Errorf("slot %d: %w", index, errNoSendableCodec)
}

// newTrack creates a track that continues the slot's RTP stream in codec c.
func (sl *slot) newTrack(c webrtc.RTPCodecParameters) (*webrtc.TrackLocalStaticSample, error) {
	track, err := webrtc.NewTrackLocalStaticSample(c.RTPCodecCapability, fmt.Sprintf("slot-%d", sl.index), "spikehub",
		webrtc.WithRTPSequenceNumber(sl.seq), webrtc.WithRTPTimestamp(sl.ts), webrtc.WithPayloader(sl.payloader))
	if err != nil {
		return nil, fmt.Errorf("slot %d: create track: %w", sl.index, err)
	}
	return track, nil
}

// payloader returns the payloader of a track, which counts the packets it
// makes so that the slot knows its next sequence number.
func (sl *slot) payloader(c webrtc.RTPCodecCapability) (rtp.Payloader, error) {
	p, err := payloaderFor(c)
	if err != nil {
		return nil, err
	}
	return countingPayloader{Payloader: p, next: &sl.seq}, nil
}

// countingPayloader advances a sequence number by one per payload: the
// packetizer gives each payload one RTP packet and one sequence number.
type countingPayloader struct {
	rtp.Payloader
	next *uint16
}

func (p countingPayloader) Payload(mtu uint16, payload []byte) [][]byte {
	out := p.Payloader.Payload(mtu, payload)
	*p.next += uint16(len(out)) //nolint:gosec // a frame never takes 65536 packets
	return out
}

// run is the slot's writer. It takes requests and writes frames until done
// is closed.
func (sl *slot) run(done <-chan struct{}) {
	cur := feed{}
	defer func() { cur.stop() }()
	for {
		// A request that is ready goes before a frame that is ready, so that
		// nothing of the old camera is written once an unbind or a rebind has
		// been taken.
		select {
		case <-done:
			return
		case b := <-sl.reqs:
			cur = sl.apply(b, cur)
			continue
		default:
		}
		select {
		case <-done:
			return
		case b := <-sl.reqs:
			cur = sl.apply(b, cur)
		case au, ok := <-cur.live:
			if !ok {
				cur = feed{} // the source ended
				continue
			}
			sl.write(au, frameInterval(cur.src))
		}
	}
}

// feed is the subscription a slot writes from.
type feed struct {
	src    source.Source
	live   <-chan source.AccessUnit
	cancel func()
}

func (f feed) stop() {
	if f.cancel != nil {
		f.cancel()
	}
}

// apply stops the current feed and starts the one b asks for: it switches the
// track to b's codec, announces the binding and writes the source's cached
// GOP, which starts at its last IDR.
func (sl *slot) apply(b binding, cur feed) feed {
	cur.stop()
	if b.src == nil {
		return feed{}
	}
	if err := sl.useCodec(b.codec); err != nil {
		sl.log.Error("cannot switch the codec of a slot", "slot", sl.index, "codec", b.codec.MimeType, "err", err)
		sl.notify(errorMsg{T: "error", Slot: sl.index, Epoch: b.epoch, Code: codeCodecUnsupported})
		return feed{}
	}
	cached, live, cancel := b.src.Subscribe()
	sl.notify(boundMsg{T: "bound", Slot: sl.index, Epoch: b.epoch, Codec: string(b.src.Codec()), Profile: b.src.Profile(), Fallback: b.fallback})
	for i, au := range cached {
		step := burstStep
		if i == len(cached)-1 {
			step = frameInterval(b.src)
		}
		sl.write(au, step)
	}
	return feed{src: b.src, live: live, cancel: cancel}
}

// useCodec makes the slot send in codec c. A new codec needs a new track,
// which continues the sequence numbers and timestamps of the old one.
func (sl *slot) useCodec(c webrtc.RTPCodecParameters) error {
	if c.PayloadType == sl.codec.PayloadType {
		return nil
	}
	track, err := sl.newTrack(c)
	if err != nil {
		return err
	}
	if err := sl.sender.ReplaceTrack(track); err != nil {
		return fmt.Errorf("replace track: %w", err)
	}
	sl.track, sl.codec, sl.remainder = track, c, 0
	return nil
}

// write sends one access unit; step is the time until the next one.
func (sl *slot) write(au source.AccessUnit, step time.Duration) {
	if err := sl.track.WriteSample(media.Sample{Data: annexB(au.NALUs), Duration: step}); err != nil {
		sl.log.Debug("write a frame", "slot", sl.index, "err", err)
	}
	// TrackLocalStaticSample turns the step into RTP ticks with this
	// arithmetic; repeating it gives the timestamp of the next sample.
	ticks := step.Seconds()*float64(sl.codec.ClockRate) + sl.remainder
	whole := uint32(ticks)
	sl.remainder = ticks - float64(whole)
	sl.ts += whole
}

// annexB joins NAL units with 4-byte start codes, the form the payloaders
// split.
func annexB(nalus [][]byte) []byte {
	size := 0
	for _, nalu := range nalus {
		size += 4 + len(nalu)
	}
	out := make([]byte, 0, size)
	for _, nalu := range nalus {
		out = append(out, 0, 0, 0, 1)
		out = append(out, nalu...)
	}
	return out
}

// frameInterval is the frame period of a source.
func frameInterval(src source.Source) time.Duration {
	if fps := src.FPS(); fps > 0 {
		return time.Second / time.Duration(fps)
	}
	return fallbackFrameInterval
}
