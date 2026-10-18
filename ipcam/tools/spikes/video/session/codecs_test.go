package session

import (
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/webrtc/v4"
)

// profiledSource is a Source with a fixed codec and profile; only Codec and
// Profile are used.
type profiledSource struct {
	source.Source
	codec   source.Codec
	profile string
}

func (p profiledSource) Codec() source.Codec { return p.codec }
func (p profiledSource) Profile() string     { return p.profile }

func h264Codec(pt webrtc.PayloadType, fmtp string) webrtc.RTPCodecParameters {
	return webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: fmtp},
		PayloadType:        pt,
	}
}

func TestChooseCodec_MatchesProfileIDCAndPrefersTheHighestLevel(t *testing.T) {
	offered := []webrtc.RTPCodecParameters{
		{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, PayloadType: 96},
		h264Codec(98, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"),
		h264Codec(100, "level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=4d0033"),
		h264Codec(102, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=4d001f"),
		h264Codec(104, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=64001f"),
		h264Codec(106, "profile-level-id=640C34;packetization-mode=1"),
		h264Codec(108, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=640c34"),
		{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH265, ClockRate: 90000, SDPFmtpLine: "level-id=93;profile-id=1"}, PayloadType: 110},
	}
	tests := []struct {
		name   string
		src    profiledSource
		wantPT webrtc.PayloadType
		wantOK bool
	}{
		// sub (Main 3.1): only 4d001f has profile_idc 0x4d in packetization mode 1.
		{"sub", profiledSource{codec: source.CodecH264, profile: "4d401f"}, 102, true},
		// main (Main 4.0): the level is not enforced.
		{"main", profiledSource{codec: source.CodecH264, profile: "4d4028"}, 102, true},
		// main-high51: the High entry with the highest level, the first of equals.
		{"high", profiledSource{codec: source.CodecH264, profile: "640033"}, 106, true},
		{"baseline", profiledSource{codec: source.CodecH264, profile: "42c01e"}, 98, true},
		{"h265", profiledSource{codec: source.CodecH265, profile: "017b"}, 110, true},
		{"high 4:4:4 not offered", profiledSource{codec: source.CodecH264, profile: "f40028"}, 0, false},
		{"profile unknown yet", profiledSource{codec: source.CodecH264, profile: ""}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fallback, ok := chooseCodec(tt.src, false, offered)
			if ok != tt.wantOK || got.PayloadType != tt.wantPT || fallback != "" {
				t.Errorf("chooseCodec = %d, %q, %v; want %d, \"\", %v", got.PayloadType, fallback, ok, tt.wantPT, tt.wantOK)
			}
		})
	}

	if _, _, ok := chooseCodec(profiledSource{codec: source.CodecH265, profile: "017b"}, true, offered[:7]); ok {
		t.Error("chose a codec for H.265 from an offer without H.265")
	}
}

func TestChooseCodec_SubFallsBackToTheClosestOfferedProfile(t *testing.T) {
	mainSrc := profiledSource{codec: source.CodecH264, profile: "4d401f"}
	highSrc := profiledSource{codec: source.CodecH264, profile: "640028"}
	pm1 := func(pt webrtc.PayloadType, plid string) webrtc.RTPCodecParameters {
		return h264Codec(pt, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id="+plid)
	}
	firefox := []webrtc.RTPCodecParameters{
		pm1(126, "42e01f"),
		h264Codec(97, "level-asymmetry-allowed=1;profile-level-id=42e01f"),
		pm1(98, "42001f"),
	}
	tests := []struct {
		name         string
		src          profiledSource
		offered      []webrtc.RTPCodecParameters
		wantPT       webrtc.PayloadType
		wantFallback string
	}{
		// Firefox: no richer profile, so the next poorer one, the first of equals.
		{"main on Baseline", mainSrc, firefox, 126, "42e01f"},
		// Safari: a Constrained High decoder decodes Main without B-frames.
		{"main on Constrained High", mainSrc, []webrtc.RTPCodecParameters{pm1(96, "42e01f"), pm1(98, "640c1f")}, 98, "640c1f"},
		// The highest level of the closest profile; the fallback is lowercase.
		{"highest level", mainSrc, []webrtc.RTPCodecParameters{pm1(96, "42e01f"), pm1(98, "42E034"), pm1(100, "42e028")}, 98, "42e034"},
		// The next richer profile goes before a poorer one...
		{"high on High 4:4:4", highSrc, []webrtc.RTPCodecParameters{pm1(96, "4d001f"), pm1(98, "f4001f")}, 98, "f4001f"},
		{"main on High", mainSrc, []webrtc.RTPCodecParameters{pm1(96, "f4001f"), pm1(98, "64001f"), pm1(100, "42e01f")}, 98, "64001f"},
		// ...and among poorer ones, the richest.
		{"high on Main", highSrc, []webrtc.RTPCodecParameters{pm1(96, "42e01f"), pm1(98, "4d001f")}, 98, "4d001f"},
		// A matching profile is no fallback.
		{"match", mainSrc, []webrtc.RTPCodecParameters{pm1(96, "640c1f"), pm1(98, "4d001f")}, 98, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, fallback, ok := chooseCodec(tt.src, true, tt.offered)
			if !ok || got.PayloadType != tt.wantPT || fallback != tt.wantFallback {
				t.Errorf("chooseCodec = %d, %q, %v; want %d, %q, true", got.PayloadType, fallback, ok, tt.wantPT, tt.wantFallback)
			}
		})
	}

	none := []struct {
		name    string
		src     profiledSource
		sub     bool
		offered []webrtc.RTPCodecParameters
	}{
		{"main stays strict", mainSrc, false, firefox},
		{"no H.264 in packetization mode 1", mainSrc, true, []webrtc.RTPCodecParameters{firefox[1]}},
		{"profile unknown yet", profiledSource{codec: source.CodecH264}, true, firefox},
		{"profile outside the ladder", profiledSource{codec: source.CodecH264, profile: "58001f"}, true, firefox},
	}
	for _, tt := range none {
		t.Run(tt.name, func(t *testing.T) {
			if got, fallback, ok := chooseCodec(tt.src, tt.sub, tt.offered); ok {
				t.Errorf("chooseCodec = %d, %q, true; want no codec", got.PayloadType, fallback)
			}
		})
	}
}

func TestBind_H265NotOfferedReturnsError(t *testing.T) {
	sim := newTestSim(t, "mx-h265", 17, "main", h265AUs(17, 40, 20), 20)
	sim.emit(t, 1)
	offer := codecOffer{h264: chromeLike.h264}
	c := newClient(t, clientConfig{slots: 2, offer: offer})
	connect(t, testAPI(t), c, map[string]source.Source{"mx-h265/main": sim}, testOptions(source.SystemClock{}, nil))

	c.bind(t, 1, "mx-h265", "main", 9)
	m := c.next(t, "error")
	if want := `{"t":"error","slot":1,"epoch":9,"code":"CODEC_UNSUPPORTED"}`; m.raw != want {
		t.Errorf("error = %s, want %s", m.raw, want)
	}
	sim.emit(t, 1)
	c.quiet(t, 1, 100*time.Millisecond)
}

func TestBind_HighProfileNotOfferedReturnsError(t *testing.T) {
	high := newTestSim(t, "mx-high51", 18, "main", h264AUs("640033", 18, 40, 20), 20)
	baseline := newTestSim(t, "sim-01", 1, "sub", h264AUs("42c01e", 1, 24, 12), 12)
	high.emit(t, 1)
	baseline.emit(t, 1)
	c := newClient(t, clientConfig{slots: 1, offer: codecOffer{h264: []string{"42e01f"}}})
	connect(t, testAPI(t), c, map[string]source.Source{"mx-high51/main": high, "sim-01/sub": baseline}, testOptions(source.SystemClock{}, nil))

	c.bind(t, 0, "mx-high51", "main", 4)
	if m := c.next(t, "error"); m.Slot != 0 || m.Epoch != 4 || m.Code != "CODEC_UNSUPPORTED" {
		t.Errorf("High@5.1 on a Constrained Baseline offer: got %s, want CODEC_UNSUPPORTED", m.raw)
	}

	// The same offer carries a Baseline camera.
	c.bind(t, 0, "sim-01", "sub", 5)
	if m := c.next(t, "bound"); m.Epoch != 5 || m.Profile != "42c01e" {
		t.Errorf("bound = %s, want epoch 5 and profile 42c01e", m.raw)
	}
	if au := c.au(t, 0); !au.idr || au.camera != 1 {
		t.Errorf("first access unit = %v, want the IDR of camera 1", au)
	}
}

// firefoxLike is what Firefox offers: Constrained Baseline and Baseline only.
var firefoxLike = codecOffer{h264: []string{"42e01f", "42001f"}}

func TestBind_SubFallsBackToTheClosestOfferedProfile(t *testing.T) {
	subSim := newTestSim(t, "sim-01", 1, "sub", h264AUs("4d401f", 1, 24, 12), 12)
	mainSim := newTestSim(t, "sim-01", 1, "main", h264AUs("4d4028", 1, 40, 20), 20)
	subSim.emit(t, 1)
	mainSim.emit(t, 1)
	c := newClient(t, clientConfig{slots: 1, offer: firefoxLike})
	connect(t, testAPI(t), c, map[string]source.Source{"sim-01/sub": subSim, "sim-01/main": mainSim}, testOptions(source.SystemClock{}, nil))

	// The main stream keeps the strict profile match.
	c.bind(t, 0, "sim-01", "main", 3)
	if m := c.next(t, "error"); m.raw != `{"t":"error","slot":0,"epoch":3,"code":"CODEC_UNSUPPORTED"}` {
		t.Errorf("Main@4.0 main stream on a Baseline offer: got %s, want CODEC_UNSUPPORTED", m.raw)
	}

	// The substream goes out in the closest offered profile, and "bound" says so.
	c.bind(t, 0, "sim-01", "sub", 4)
	m := c.next(t, "bound")
	if want := `{"t":"bound","slot":0,"epoch":4,"codec":"H264","profile":"4d401f","fallback":"42e01f"}`; m.raw != want {
		t.Errorf("bound = %s, want %s", m.raw, want)
	}
	au := c.au(t, 0)
	if !au.idr || au.camera != 1 || au.frame != 0 {
		t.Errorf("first access unit = %v, want the IDR of camera 1", au)
	}
	var fmtp string
	for _, codec := range c.pc.GetTransceivers()[0].Receiver().GetParameters().Codecs {
		if uint8(codec.PayloadType) == au.pt {
			fmtp = codec.SDPFmtpLine
		}
	}
	if fmtpValue(fmtp, "profile-level-id") != "42e01f" || fmtpValue(fmtp, "packetization-mode") != "1" {
		t.Errorf("the substream came in payload type %d (%q), want the offered 42e01f in packetization mode 1", au.pt, fmtp)
	}
}
