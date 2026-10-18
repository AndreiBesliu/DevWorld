package session

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
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
	// (spec §6.3 step 6, HUB-06). It is also the least step from the last
	// frame sent to a frame that the slot stamps at the hub clock (useTrack).
	burstStep = time.Millisecond
	// fallbackFrameInterval is the frame period of a source that does not
	// know its frame rate yet: an RTSP source before its second frame.
	fallbackFrameInterval = 50 * time.Millisecond
	// requestQueue is how many bind and unbind requests may wait for a slot.
	requestQueue = 16
	// maxClockSkip bounds how far the slot skips its timestamps forward at
	// once, past the timestamp that the old track would have used. A step of
	// half the 32-bit range (6.6 h at 90 kHz) or more would read as a step
	// backward.
	maxClockSkip = time.Hour
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
	// applied, when set, receives the outcome once the slot has handled the
	// request: nil when the slot carries src, or the error that left it as it
	// was. It has room for the one result, so the slot never waits for it.
	applied chan error
}

// slot is one recvonly video m-line of the client's offer. It writes the
// camera bound to it, first the cached GOP and then the live frames, into
// one RTP stream: a rebind or a change of codec keeps the SSRC and continues
// the sequence numbers and timestamps (spec §6.3 step 6). The timestamps keep
// to the hub clock: the slot stamps a bind's first frame, and its first live
// frame, at the hub clock (see the package doc and useTrack).
type slot struct {
	index   int
	offered []webrtc.RTPCodecParameters // the codecs the answer lists for the slot
	sender  *webrtc.RTPSender
	reqs    chan binding
	notify  func(any)
	log     *slog.Logger
	now     func() time.Time // the hub clock
	// replaceTrack switches the sender to another track: RTPSender.ReplaceTrack,
	// except in tests that make it fail.
	replaceTrack func(*webrtc.RTPSender, webrtc.TrackLocal) error

	// Owned by the writer goroutine once it runs.
	track     *webrtc.TrackLocalStaticSample
	codec     webrtc.RTPCodecParameters
	seq       uint16  // the sequence number of the next RTP packet
	ts        uint32  // the timestamp of the next sample
	remainder float64 // the fraction of a tick that the next sample inherits
	// due is the hub time that ts stands for: the RTP clock started at the hub
	// time of the first sample and advances by the ticks of each sample, and
	// useTrack moves it with ts. Zero until the slot has written a sample.
	due time.Time
	// lastTS is the timestamp of the last sample written and lastAt the hub
	// time it stands for; lastAt is zero until the slot has written a sample.
	lastTS uint32
	lastAt time.Time
}

// newSlot creates a slot and its first track, created at answer time in the
// first codec of the answer that the hub can send. now reads the hub clock.
func newSlot(index int, offered []webrtc.RTPCodecParameters, notify func(any), log *slog.Logger, now func() time.Time,
	replaceTrack func(*webrtc.RTPSender, webrtc.TrackLocal) error,
) (*slot, error) {
	sl := &slot{index: index, offered: offered, reqs: make(chan binding, requestQueue), notify: notify, log: log, now: now, replaceTrack: replaceTrack}
	var b [6]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error
	sl.seq, sl.ts = binary.BigEndian.Uint16(b[:2]), binary.BigEndian.Uint32(b[2:])
	for _, c := range offered {
		if sendable(c) {
			track, err := sl.newTrack(c, sl.ts)
			if err != nil {
				return nil, err
			}
			sl.track, sl.codec = track, c
			return sl, nil
		}
	}
	return nil, fmt.Errorf("slot %d: %w", index, errNoSendableCodec)
}

// newTrack creates a track that continues the slot's RTP stream in codec c,
// its next sample stamped ts.
func (sl *slot) newTrack(c webrtc.RTPCodecParameters, ts uint32) (*webrtc.TrackLocalStaticSample, error) {
	track, err := webrtc.NewTrackLocalStaticSample(c.RTPCodecCapability, fmt.Sprintf("slot-%d", sl.index), "spikehub",
		webrtc.WithRTPSequenceNumber(sl.seq), webrtc.WithRTPTimestamp(ts), webrtc.WithPayloader(sl.payloader))
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
			cur = sl.handle(b, cur)
			continue
		default:
		}
		select {
		case <-done:
			return
		case b := <-sl.reqs:
			cur = sl.handle(b, cur)
		case au, ok := <-cur.live:
			if !ok {
				cur = feed{} // the source ended
				continue
			}
			if cur.afterBurst {
				if err := sl.useTrack(sl.codec); err != nil {
					sl.log.Warn("cannot stamp a live frame at the hub clock; it goes out on the old track", "slot", sl.index, "err", err)
				}
				cur.afterBurst = false
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
	// afterBurst is set until the first live frame, which goes out at the hub
	// clock like the first frame of the bind.
	afterBurst bool
}

func (f feed) stop() {
	if f.cancel != nil {
		f.cancel()
	}
}

// handle applies a request and reports its outcome to whoever waits for it.
func (sl *slot) handle(b binding, cur feed) feed {
	next, err := sl.apply(b, cur)
	if b.applied != nil {
		b.applied <- err
	}
	return next
}

// apply switches the slot to the feed b asks for: it switches the track to b's
// codec and to the hub clock, stops the current feed, announces the binding
// and writes the source's cached GOP, which starts at its last IDR. The track
// comes first, because it is the step that can fail: then the slot is left as
// it was, still writing the current feed, and the client gets an error for the
// epoch of the bind.
func (sl *slot) apply(b binding, cur feed) (feed, error) {
	if b.src == nil {
		cur.stop()
		return feed{}, nil
	}
	if err := sl.useTrack(b.codec); err != nil {
		sl.log.Error("cannot switch the track of a slot", "slot", sl.index, "codec", b.codec.MimeType, "err", err)
		sl.notify(errorMsg{T: "error", Slot: sl.index, Epoch: b.epoch, Code: codeCodecUnsupported})
		return cur, err
	}
	cur.stop()
	cached, live, cancel := b.src.Subscribe()
	sl.notify(boundMsg{T: "bound", Slot: sl.index, Epoch: b.epoch, Codec: string(b.src.Codec()), Profile: b.src.Profile(), Fallback: b.fallback})
	// The first live frame comes up to a frame period after the burst, not a
	// frame period after its last frame: run stamps it at the hub clock.
	for _, au := range cached {
		sl.write(au, burstStep)
	}
	return feed{src: b.src, live: live, cancel: cancel, afterBurst: true}, nil
}

// useTrack makes the slot send its next frame, the first of a bind or the
// first live frame after the cached GOP, in codec c, stamped at the hub clock,
// or burstStep after the last frame sent when that is later, so that
// timestamps only increase; it skips at most maxClockSkip past the old track's
// next timestamp.
//
// Left to the old track, the timestamps drift from the hub clock at those two
// frames. TrackLocalStaticSample stamps a sample by the durations of the
// samples before it, which are guesses: the timestamps stand still while the
// slot is empty (Task 9b); after a live frame the old track stamps the bind's
// first frame a frame period later, however little of it has passed; and the
// first live frame comes any time within a frame period after the burst
// (Task 9c). A new track continues the sequence numbers and starts at the
// timestamp the slot chooses; the slot needs one for a new codec too.
func (sl *slot) useTrack(c webrtc.RTPCodecParameters) error {
	ts, due := sl.ts, sl.due
	if !sl.lastAt.IsZero() {
		at := sl.now()
		if floor := sl.lastAt.Add(burstStep); at.Before(floor) {
			at = floor
		}
		if limit := sl.due.Add(maxClockSkip); at.After(limit) {
			at = limit
		}
		ticks := uint32(math.Round(at.Sub(sl.lastAt).Seconds() * float64(c.ClockRate)))
		ts, due = sl.lastTS+ticks, sl.lastAt.Add(tickDuration(ticks, c.ClockRate))
	}
	if c.PayloadType == sl.codec.PayloadType && ts == sl.ts {
		return nil
	}
	track, err := sl.newTrack(c, ts)
	if err != nil {
		return err
	}
	if err := sl.replaceTrack(sl.sender, track); err != nil {
		return fmt.Errorf("replace track: %w", err)
	}
	sl.track, sl.codec, sl.ts, sl.due, sl.remainder = track, c, ts, due, 0
	return nil
}

// write sends one access unit; step is the time until the next one.
func (sl *slot) write(au source.AccessUnit, step time.Duration) {
	if sl.due.IsZero() {
		sl.due = sl.now()
	}
	sl.lastTS, sl.lastAt = sl.ts, sl.due
	if err := sl.track.WriteSample(media.Sample{Data: annexB(au.NALUs), Duration: step}); err != nil {
		sl.log.Debug("write a frame", "slot", sl.index, "err", err)
	}
	// TrackLocalStaticSample turns the step into RTP ticks with this
	// arithmetic; repeating it gives the timestamp of the next sample. due
	// follows the ticks, not the step, so that the fractions of a tick that a
	// new track drops do not set the slot's account of its RTP clock apart from
	// the clock itself, bind after bind.
	ticks := step.Seconds()*float64(sl.codec.ClockRate) + sl.remainder
	whole := uint32(ticks)
	sl.remainder = ticks - float64(whole)
	sl.ts += whole
	sl.due = sl.lastAt.Add(tickDuration(whole, sl.codec.ClockRate))
}

// tickDuration is the time that ticks of an RTP clock at clockRate span.
func tickDuration(ticks, clockRate uint32) time.Duration {
	return time.Duration(ticks) * time.Second / time.Duration(clockRate)
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
