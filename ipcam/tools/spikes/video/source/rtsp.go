package source

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph265"
	"github.com/pion/rtp"
)

// rtspSource reads one video stream from a real camera over RTSP.
type rtspSource struct {
	id    string
	codec Codec
	log   *slog.Logger
	gop   *gop

	mu      sync.Mutex
	profile string
	frames  int           // access units decoded so far
	firstAt time.Duration // PTS of the first one
	lastAt  time.Duration // PTS of the latest one

	// Used only by the packet callback, which the client runs on one goroutine.
	decode    func(*rtp.Packet) ([][]byte, error)
	benign    []error // decoder results that are not faults: more packets are needed
	clockRate int
	params    map[paramKind][]byte // latest parameter sets: from the SDP, then from the stream
	counter   uint16
	prevPTS   time.Duration
	synced    bool
}

// NewRTSP connects to the camera at rawURL, which may carry credentials, and
// returns once it plays. It reads the first H.264 or H.265 video track.
//
// The URL is the owner's secret and is only kept in memory: it is never
// logged, and no error that NewRTSP returns or logs contains it, its
// credentials or its host. The source is known by id in the logs.
//
// Frames come over TCP, so none is lost on the way. When the connection ends or
// ctx is done, the source ends: subscriptions close, and the cached GOP stays.
// An IDR that lacks parameter sets gets the ones from the SDP or from earlier in
// the stream, because some cameras send them only once.
func NewRTSP(ctx context.Context, id string, rawURL string) (Source, error) {
	u, err := base.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("rtsp %s: the camera URL is not valid", id)
	}
	scrub := newScrubber(rawURL, u)
	fail := func(err error) (Source, error) {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("rtsp %s: %w", id, ctx.Err())
		}
		return nil, fmt.Errorf("rtsp %s: %s", id, scrub.text(err))
	}

	log := slog.Default().With("source", id)
	tcp := gortsplib.ProtocolTCP
	// The client logs through the standard logger when no callback is set.
	c := &gortsplib.Client{
		Scheme:   u.Scheme,
		Host:     u.Host,
		Protocol: &tcp,
		OnDecodeError: func(err error) {
			log.Debug("rtsp decode error", "err", scrub.text(err))
		},
		OnPacketsLost: func(lost uint64) {
			log.Debug("rtsp packets lost", "count", lost)
		},
		OnTransportSwitch: func(err error) {
			log.Debug("rtsp transport switch", "err", scrub.text(err))
		},
	}
	if err := c.Start(); err != nil {
		return fail(err)
	}
	// Closing the client also unblocks the calls below, so ctx can cancel them.
	stopWatching := context.AfterFunc(ctx, c.Close)
	abort := func(err error) (Source, error) {
		stopWatching()
		c.Close()
		return fail(err)
	}

	desc, _, err := c.Describe(u)
	if err != nil {
		return abort(err)
	}
	s, medi, forma, err := newRTSPSource(id, log, desc)
	if err != nil {
		return abort(err)
	}
	if _, err := c.Setup(desc.BaseURL, medi, 0, 0); err != nil {
		return abort(err)
	}
	c.OnPacketRTP(medi, forma, func(pkt *rtp.Packet) {
		pts, ok := c.PacketPTS(medi, pkt)
		if !ok {
			return // the client cannot place the packet in time yet
		}
		s.onPacket(pkt, ptsToDuration(pts, s.clockRate))
	})
	if _, err := c.Play(nil); err != nil {
		return abort(err)
	}

	go func() {
		err := c.Wait()
		stopWatching()
		level := slog.LevelWarn
		if ctx.Err() != nil {
			level = slog.LevelInfo
		}
		log.Log(ctx, level, "rtsp source stopped", "reason", scrub.text(err))
		s.gop.close()
	}()
	log.Info("rtsp source playing", "codec", s.codec, "profile", s.Profile())
	return s, nil
}

// newRTSPSource picks the first H.264 or H.265 video track of desc.
func newRTSPSource(id string, log *slog.Logger, desc *description.Session) (*rtspSource, *description.Media, format.Format, error) {
	s := &rtspSource{id: id, log: log, gop: newGOP(), params: map[paramKind][]byte{}}
	for _, medi := range desc.Medias {
		if medi.Type != description.MediaTypeVideo {
			continue
		}
		for _, forma := range medi.Formats {
			switch f := forma.(type) {
			case *format.H264:
				dec, err := f.CreateDecoder()
				if err != nil {
					return nil, nil, nil, err
				}
				s.codec, s.decode, s.clockRate = CodecH264, dec.Decode, f.ClockRate()
				s.benign = []error{rtph264.ErrMorePacketsNeeded, rtph264.ErrNonStartingPacketAndNoPrevious}
				s.remember(f.SPS, f.PPS)
				return s, medi, forma, nil
			case *format.H265:
				dec, err := f.CreateDecoder()
				if err != nil {
					return nil, nil, nil, err
				}
				s.codec, s.decode, s.clockRate = CodecH265, dec.Decode, f.ClockRate()
				s.benign = []error{rtph265.ErrMorePacketsNeeded, rtph265.ErrNonStartingPacketAndNoPrevious}
				s.remember(f.VPS, f.SPS, f.PPS)
				return s, medi, forma, nil
			}
		}
	}
	return nil, nil, nil, errors.New("the camera has no H.264 or H.265 video")
}

// remember stores parameter sets, in the order VPS (H.265 only), SPS, PPS, and
// takes the profile from an SPS.
func (s *rtspSource) remember(sets ...[]byte) {
	for _, set := range sets {
		if len(set) == 0 {
			continue
		}
		kind := paramKindOf(s.codec, set)
		if kind == paramNone {
			continue
		}
		s.params[kind] = set
		if kind == paramSPS {
			if profile := profileOf(s.codec, set); profile != "" {
				s.mu.Lock()
				s.profile = profile
				s.mu.Unlock()
			}
		}
	}
}

// onPacket depacketises one RTP packet and pushes the access unit it completes.
func (s *rtspSource) onPacket(pkt *rtp.Packet, pts time.Duration) {
	prevPTS := s.prevPTS
	s.prevPTS = pts

	nalus, err := s.decode(pkt)
	if err != nil {
		if !s.isBenign(err) {
			s.log.Debug("rtsp frame dropped", "err", err)
		}
		return
	}
	// The stream is joined at any packet, so the first access unit may lack its
	// first NAL units. Every client of the cached GOP would show the damage.
	if !s.synced {
		s.synced = true
		return
	}
	nalus = ownedNALUs(nalus)
	if len(nalus) == 0 {
		return
	}
	// Without a marker bit, the decoder completes an access unit when a packet
	// with another timestamp arrives, and the unit belongs to the packet before.
	auPTS := pts
	if !pkt.Marker {
		auPTS = prevPTS
	}

	idr := isRandomAccess(s.codec, nalus)
	s.remember(nalus...)
	if idr {
		nalus = s.withParameterSets(nalus)
	}
	au := AccessUnit{NALUs: nalus, IDR: idr, Frame: s.counter, PTS: auPTS}
	s.counter++

	s.mu.Lock()
	if s.frames == 0 {
		s.firstAt = auPTS
	}
	s.frames++
	s.lastAt = auPTS
	s.mu.Unlock()

	s.gop.push(au)
}

func (s *rtspSource) isBenign(err error) bool {
	return slices.ContainsFunc(s.benign, func(b error) bool { return errors.Is(err, b) })
}

// withParameterSets puts the latest parameter sets that the access unit lacks in
// front of it.
func (s *rtspSource) withParameterSets(nalus [][]byte) [][]byte {
	has := map[paramKind]bool{}
	for _, n := range nalus {
		has[paramKindOf(s.codec, n)] = true
	}
	var missing [][]byte
	for _, kind := range []paramKind{paramVPS, paramSPS, paramPPS} {
		if set := s.params[kind]; set != nil && !has[kind] {
			missing = append(missing, set)
		}
	}
	return append(missing, nalus...)
}

// ownedNALUs copies the NAL units into one new buffer, because the depacketiser
// returns slices of the RTP packets, which the client may reuse.
func ownedNALUs(nalus [][]byte) [][]byte {
	total := 0
	for _, n := range nalus {
		total += len(n)
	}
	buf := make([]byte, 0, total)
	out := make([][]byte, 0, len(nalus))
	for _, n := range nalus {
		if len(n) == 0 {
			continue
		}
		start := len(buf)
		buf = append(buf, n...)
		out = append(out, buf[start:len(buf):len(buf)])
	}
	return out
}

// ptsToDuration converts a timestamp in units of 1/rate s without overflowing
// for streams that run for days.
func ptsToDuration(pts int64, rate int) time.Duration {
	r := int64(rate)
	return time.Duration(pts/r)*time.Second + time.Duration(pts%r)*time.Second/time.Duration(r)
}

func (s *rtspSource) ID() string                 { return s.id }
func (s *rtspSource) Codec() Codec               { return s.codec }
func (s *rtspSource) SentLog() <-chan SentRecord { return nil }
func (s *rtspSource) Jitter() time.Duration      { return 0 }

func (s *rtspSource) Profile() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.profile
}

// FPS is the mean frame rate since the first frame, rounded.
func (s *rtspSource) FPS() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frames < 2 || s.lastAt <= s.firstAt {
		return 0
	}
	return int(math.Round(float64(s.frames-1) / (s.lastAt - s.firstAt).Seconds()))
}

func (s *rtspSource) Subscribe() ([]AccessUnit, <-chan AccessUnit, func()) {
	return s.gop.subscribe()
}

// scrubber removes the parts of a camera URL from error text.
type scrubber struct{ replacer *strings.Replacer }

func newScrubber(rawURL string, u *base.URL) *scrubber {
	secrets := []string{rawURL, u.String(), u.Host, u.Hostname(), u.Port(), strings.Trim(u.Path, "/"), u.RawQuery}
	if u.User != nil {
		pass, _ := u.User.Password()
		secrets = append(secrets, u.User.Username(), pass)
	}
	// strings.Replacer prefers the earlier of two matches at one position.
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	var oldnew []string
	for _, secret := range secrets {
		if secret != "" {
			oldnew = append(oldnew, secret, "<redacted>")
		}
	}
	return &scrubber{replacer: strings.NewReplacer(oldnew...)}
}

func (s *scrubber) text(err error) string {
	if err == nil {
		return "no error"
	}
	return s.replacer.Replace(err.Error())
}
