package session

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

// offeredH264 are the H.264 profile-level-ids that WebRTC clients offer:
// libwebrtc (Chrome, Edge, WebView2, Android WebView) offers 42001f, 42e01f,
// 4d001f, 64001f, 640c1f and f4001f; Firefox 42e01f and 42001f; Safari 42e01f
// and 640c1f. Pion negotiates an offered H.264 format only when its
// profile_idc and constraint byte equal a registered one, and ignores the
// level, so these entries negotiate every level a client offers of them.
var offeredH264 = []string{"42001f", "42e01f", "4d001f", "64001f", "640c1f", "f4001f"}

// videoFeedback is the RTCP feedback the hub accepts: NACK, which the default
// interceptors answer from their send buffer, and PLI.
var videoFeedback = []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}}

// registerCodecs registers what the hub can send: H.264 in packetization
// mode 1 (FU-A fragments large NAL units) and H.265 Main.
func registerCodecs(me *webrtc.MediaEngine) error {
	pt := webrtc.PayloadType(96)
	register := func(mime, fmtp string) error {
		err := me.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: mime, ClockRate: 90000, SDPFmtpLine: fmtp, RTCPFeedback: videoFeedback},
			PayloadType:        pt,
		}, webrtc.RTPCodecTypeVideo)
		pt++
		if err != nil {
			return fmt.Errorf("register %s %s: %w", mime, fmtp, err)
		}
		return nil
	}
	for _, plid := range offeredH264 {
		if err := register(webrtc.MimeTypeH264, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id="+plid); err != nil {
			return err
		}
	}
	return register(webrtc.MimeTypeH265, "level-id=93;profile-id=1;tier-flag=0;tx-mode=SRST")
}

// fmtpValue returns the value of a parameter in an SDP fmtp line, or "".
func fmtpValue(fmtp, key string) string {
	for _, param := range strings.Split(fmtp, ";") {
		if k, v, ok := strings.Cut(param, "="); ok && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func isH264(c webrtc.RTPCodecParameters) bool {
	return strings.EqualFold(c.MimeType, webrtc.MimeTypeH264)
}

func isH265(c webrtc.RTPCodecParameters) bool {
	return strings.EqualFold(c.MimeType, webrtc.MimeTypeH265)
}

// sendable reports whether the hub can send a codec of an answer.
func sendable(c webrtc.RTPCodecParameters) bool {
	return isH265(c) || isH264(c) && fmtpValue(c.SDPFmtpLine, "packetization-mode") == "1"
}

// profileLevelID splits an H.264 profile-level-id ("4d401f") into its
// profile_idc and level_idc bytes.
func profileLevelID(s string) (profileIDC, levelIDC byte, ok bool) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 3 {
		return 0, 0, false
	}
	return b[0], b[2], true
}

// chooseCodec picks, from the codecs that an answer lists for a slot, the one
// that carries src (R20). For H.264 it is an entry in packetization mode 1
// whose profile_idc equals the source's, with the highest level and the first
// of equals; the level itself is not enforced. For H.265 it is the first H.265
// entry. ok is false when nothing fits, including an H.264 source whose
// profile is not known yet.
func chooseCodec(src source.Source, offered []webrtc.RTPCodecParameters) (webrtc.RTPCodecParameters, bool) {
	switch src.Codec() {
	case source.CodecH265:
		for _, c := range offered {
			if isH265(c) {
				return c, true
			}
		}
	case source.CodecH264:
		want, _, ok := profileLevelID(src.Profile())
		if !ok {
			return webrtc.RTPCodecParameters{}, false
		}
		best, bestLevel := -1, -1
		for i, c := range offered {
			if !isH264(c) || !sendable(c) {
				continue
			}
			idc, level, ok := profileLevelID(fmtpValue(c.SDPFmtpLine, "profile-level-id"))
			if ok && idc == want && int(level) > bestLevel {
				best, bestLevel = i, int(level)
			}
		}
		if best >= 0 {
			return offered[best], true
		}
	}
	return webrtc.RTPCodecParameters{}, false
}

// payloaderFor returns the RTP payloader of a codec the hub sends.
func payloaderFor(c webrtc.RTPCodecCapability) (rtp.Payloader, error) {
	switch {
	case strings.EqualFold(c.MimeType, webrtc.MimeTypeH264):
		return &codecs.H264Payloader{}, nil
	case strings.EqualFold(c.MimeType, webrtc.MimeTypeH265):
		return &codecs.H265Payloader{}, nil
	}
	return nil, fmt.Errorf("no payloader for %s", c.MimeType)
}
