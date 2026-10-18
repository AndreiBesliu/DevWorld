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
			got, ok := chooseCodec(tt.src, offered)
			if ok != tt.wantOK || got.PayloadType != tt.wantPT {
				t.Errorf("chooseCodec = %d, %v; want %d, %v", got.PayloadType, ok, tt.wantPT, tt.wantOK)
			}
		})
	}

	if _, ok := chooseCodec(profiledSource{codec: source.CodecH265, profile: "017b"}, offered[:7]); ok {
		t.Error("chose a codec for H.265 from an offer without H.265")
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
