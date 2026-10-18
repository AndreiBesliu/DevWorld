package source

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"regexp"
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
	scrub *scrubber // for the text of errors, which may carry the camera's URL
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
	synced    bool // the first access unit has been decoded and judged
	warned    bool // the first decode error has been logged
}

// NewRTSP connects to the camera at rawURL, which may carry credentials, and
// returns once it plays. It reads the first H.264 or H.265 video track.
//
// The URL is the owner's secret and is only kept in memory: it is never
// logged, and no error that NewRTSP returns or logs contains it, its
// credentials, its host or any IP address (PH0-22). The source is known by id in
// the logs.
//
// Frames come over TCP, so none is lost on the way. When the connection ends or
// ctx is done, the source ends: subscriptions close, and the cached GOP stays.
// An IDR that lacks parameter sets gets the ones from the SDP or from earlier in
// the stream, because some cameras send them only once.
//
// The stream is joined at any packet, so the first access unit may lack its
// first NAL units: it is dropped, unless it is an IDR that begins with the
// parameter sets, which a camera that starts to play at a keyframe sends.
//
// A camera that plays but sends nothing that decodes leaves the source silent,
// with no frame rate and no profile. The first decode error is therefore logged
// at Warn, scrubbed; later ones at Debug.
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
	c := newRTSPClient(u, log, scrub)
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
	s, medi, forma, err := newRTSPSource(id, log, scrub, desc)
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

// newRTSPClient returns the RTSP client of a source, over TCP. Its callbacks log
// at Debug, with the text of errors scrubbed, to the logger of the source: the
// client would log to the standard logger, unscrubbed, when they are not set.
func newRTSPClient(u *base.URL, log *slog.Logger, scrub *scrubber) *gortsplib.Client {
	tcp := gortsplib.ProtocolTCP
	return &gortsplib.Client{
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
}

// newRTSPSource picks the first H.264 or H.265 video track of desc.
func newRTSPSource(id string, log *slog.Logger, scrub *scrubber, desc *description.Session) (*rtspSource, *description.Media, format.Format, error) {
	s := &rtspSource{id: id, log: log, scrub: scrub, gop: newGOP(), params: map[paramKind][]byte{}}
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
			s.logDecodeError(err)
		}
		return
	}
	nalus = ownedNALUs(nalus)
	first := !s.synced
	s.synced = true
	if len(nalus) == 0 {
		return
	}
	idr := isRandomAccess(s.codec, nalus)
	// The stream is joined at any packet, so the first access unit may lack its
	// first NAL units, and every client of the cached GOP would show the damage.
	// One that begins with the parameter sets is whole: a camera that starts to
	// play at a keyframe sends it, and it is worth a GOP of waiting.
	if first && (!idr || !s.beginsWithParameterSets(nalus)) {
		return
	}
	// Without a marker bit, the decoder completes an access unit when a packet
	// with another timestamp arrives, and the unit belongs to the packet before.
	auPTS := pts
	if !pkt.Marker {
		auPTS = prevPTS
	}

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

// logDecodeError logs the error of a packet that cannot be decoded. The first
// one of a source is a Warn, because a camera that plays but yields nothing
// decodable otherwise leaves no trace above Debug; the rest are Debug, so that
// a broken camera does not flood the log.
func (s *rtspSource) logDecodeError(err error) {
	if s.warned {
		s.log.Debug("rtsp frame dropped", "err", s.scrub.text(err))
		return
	}
	s.warned = true
	s.log.Warn("rtsp frame dropped; later ones are logged at debug level", "err", s.scrub.text(err))
}

// parameterSetKinds lists the parameter sets of a codec in the order a decoder
// needs them: the VPS, which only H.265 has, then the SPS and the PPS.
func (s *rtspSource) parameterSetKinds() []paramKind {
	if s.codec == CodecH265 {
		return []paramKind{paramVPS, paramSPS, paramPPS}
	}
	return []paramKind{paramSPS, paramPPS}
}

// beginsWithParameterSets reports whether the access unit starts, after an
// access unit delimiter if it has one, with all the parameter sets of its codec.
func (s *rtspSource) beginsWithParameterSets(nalus [][]byte) bool {
	leading := map[paramKind]bool{}
	for _, n := range nalus {
		kind := paramKindOf(s.codec, n)
		if kind == paramNone && len(leading) == 0 && isDelimiter(s.codec, n) {
			continue
		}
		if kind == paramNone {
			break
		}
		leading[kind] = true
	}
	for _, kind := range s.parameterSetKinds() {
		if !leading[kind] {
			return false
		}
	}
	return true
}

// withParameterSets puts the NAL units of an access unit in the order that a
// decoder parses them: the access unit delimiter, the VPS (H.265 only), the SPS
// and the PPS, then the rest as it was. A parameter set that the access unit
// carries stays as it is, and one that it lacks is the latest stored one.
func (s *rtspSource) withParameterSets(nalus [][]byte) [][]byte {
	var delimiters, rest [][]byte
	carried := map[paramKind][][]byte{}
	for _, n := range nalus {
		switch kind := paramKindOf(s.codec, n); {
		case kind != paramNone:
			carried[kind] = append(carried[kind], n)
		case isDelimiter(s.codec, n):
			delimiters = append(delimiters, n)
		default:
			rest = append(rest, n)
		}
	}
	out := make([][]byte, 0, len(nalus)+len(s.parameterSetKinds()))
	out = append(out, delimiters...)
	for _, kind := range s.parameterSetKinds() {
		switch {
		case len(carried[kind]) > 0:
			out = append(out, carried[kind]...)
		case s.params[kind] != nil:
			out = append(out, s.params[kind])
		}
	}
	return append(out, rest...)
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

// JitterSnapshot is the zero snapshot: the hub does not pace an RTSP source.
func (s *rtspSource) JitterSnapshot() JitterSnapshot { return JitterSnapshot{} }

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

// scrubber removes the parts of a camera URL from error text, and every IP
// address (PH0-22): when the URL names the camera by host name, Go's dial errors
// carry the address that the name resolved to, and a DNS error carries that of
// the server.
type scrubber struct{ replacer *strings.Replacer }

const redacted = "<redacted>"

var (
	ipv4Literal = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// ipv6Candidate matches a run of the characters of an IPv6 address, with an
	// optional zone, that holds a colon. redactIPv6 checks that it is one.
	ipv6Candidate = regexp.MustCompile(`[0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*(?:%[0-9A-Za-z_.-]+)?`)
)

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
			oldnew = append(oldnew, secret, redacted)
		}
	}
	return &scrubber{replacer: strings.NewReplacer(oldnew...)}
}

func (s *scrubber) text(err error) string {
	if err == nil {
		return "no error"
	}
	text := redactIPv6(s.replacer.Replace(err.Error()))
	return ipv4Literal.ReplaceAllString(text, redacted)
}

// redactIPv6 replaces the IPv6 addresses in text, which it finds as the longest
// prefix of each candidate that parses as one. IPv4-mapped addresses go with
// them, so it runs before the IPv4 pass.
func redactIPv6(text string) string {
	return ipv6Candidate.ReplaceAllStringFunc(text, func(candidate string) string {
		if strings.Count(candidate, ":") < 2 {
			return candidate
		}
		for end := len(candidate); end >= 2; end-- {
			if _, err := netip.ParseAddr(candidate[:end]); err == nil {
				return redacted + candidate[end:]
			}
		}
		return candidate
	})
}
