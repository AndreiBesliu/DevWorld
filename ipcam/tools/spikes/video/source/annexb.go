package source

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
)

// maxAccessUnits is how many frames a file may hold: AccessUnit.Frame is a uint16.
const maxAccessUnits = 1 << 16

var startCode = []byte{0, 0, 1}

// ReadAnnexB reads an Annex B elementary stream (NAL units separated by 3-byte
// or 4-byte start codes) and splits it into access units.
//
// The codec is detected from the first NAL unit: H.265 streams must start with
// a VPS, H.264 streams with an SPS, an access unit delimiter, an SEI or a
// slice. An access unit is the run of NAL units that ends with the last slice
// of one picture, so parameter sets and SEI belong to the picture that follows
// them. Frame is the index of the access unit, and Loop and PTS stay 0.
func ReadAnnexB(path string) ([]AccessUnit, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a media file that the caller chose
	if err != nil {
		return nil, fmt.Errorf("read Annex B stream: %w", err)
	}
	nalus, err := splitNALUs(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	codec, ok := detectCodec(nalus[0])
	if !ok {
		return nil, fmt.Errorf("%s: cannot tell the codec from a first NAL unit with header byte 0x%02x", path, nalus[0][0])
	}
	aus, err := groupAccessUnits(codec, nalus)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return aus, nil
}

// splitNALUs returns the NAL units of an Annex B byte stream, without start
// codes and without the zero bytes that precede the next start code. The
// result aliases data.
func splitNALUs(data []byte) ([][]byte, error) {
	first := bytes.Index(data, startCode)
	if first < 0 {
		return nil, errors.New("no Annex B start code found")
	}
	if len(bytes.Trim(data[:first], "\x00")) != 0 {
		return nil, errors.New("data before the first start code")
	}

	var nalus [][]byte
	for pos := first + len(startCode); pos <= len(data); {
		end := len(data)
		next := bytes.Index(data[pos:], startCode)
		if next >= 0 {
			end = pos + next
		}
		// trailing_zero_8bits and the first zero of a 4-byte start code belong
		// to no NAL unit; a NAL unit never ends in a zero byte.
		if nalu := bytes.TrimRight(data[pos:end], "\x00"); len(nalu) > 0 {
			if nalu[0]&0x80 != 0 {
				return nil, fmt.Errorf("NAL unit %d has forbidden_zero_bit set", len(nalus))
			}
			nalus = append(nalus, nalu)
		}
		if next < 0 {
			break
		}
		pos = end + len(startCode)
	}
	if len(nalus) == 0 {
		return nil, errors.New("no NAL units found")
	}
	return nalus, nil
}

// detectCodec tells the codec of a stream from its first NAL unit. A header
// byte of 0x40 is an H.265 VPS in layer 0, and as an H.264 header it would be
// the unspecified NAL unit type 0, so the two cannot be confused.
func detectCodec(first []byte) (Codec, bool) {
	switch b := first[0]; {
	case b == 0x40:
		return CodecH265, true
	case b&0x80 != 0:
		return "", false
	default:
		switch h264.NALUType(b & 0x1f) {
		case h264.NALUTypeNonIDR, h264.NALUTypeIDR, h264.NALUTypeSEI, h264.NALUTypeSPS,
			h264.NALUTypePPS, h264.NALUTypeAccessUnitDelimiter:
			return CodecH264, true
		}
		return "", false
	}
}

func groupAccessUnits(codec Codec, nalus [][]byte) ([]AccessUnit, error) {
	var (
		aus        []AccessUnit
		current    [][]byte
		hasPicture bool
	)
	flush := func() error {
		defer func() { current, hasPicture = nil, false }()
		if !hasPicture {
			return nil // parameter sets or SEI at the end of the stream, with no picture
		}
		if len(aus) == maxAccessUnits {
			return fmt.Errorf("more than %d access units", maxAccessUnits)
		}
		aus = append(aus, AccessUnit{
			NALUs: current,
			IDR:   isRandomAccess(codec, current),
			Frame: uint16(len(aus)), //nolint:gosec // len(aus) is below maxAccessUnits
		})
		return nil
	}
	for _, nalu := range nalus {
		if hasPicture && startsAccessUnit(codec, nalu) {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		current = append(current, nalu)
		hasPicture = hasPicture || isSlice(codec, nalu)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(aus) == 0 {
		return nil, errors.New("the stream holds no picture")
	}
	return aus, nil
}

// isSlice reports whether the NAL unit carries picture data.
func isSlice(codec Codec, nalu []byte) bool {
	if codec == CodecH265 {
		return (nalu[0]>>1)&0x3f <= 31
	}
	t := h264.NALUType(nalu[0] & 0x1f)
	return t == h264.NALUTypeNonIDR || t == h264.NALUTypeIDR
}

// startsAccessUnit reports whether nalu, arriving after the slice of a picture,
// begins the next access unit: a delimiter, parameter set or SEI that precedes
// a picture, or the first slice of the next picture.
func startsAccessUnit(codec Codec, nalu []byte) bool {
	if codec == CodecH265 {
		t := h265.NALUType((nalu[0] >> 1) & 0x3f)
		if t <= 31 {
			return len(nalu) > 2 && nalu[2]&0x80 != 0 // first_slice_segment_in_pic_flag
		}
		return t == h265.NALUType_VPS_NUT || t == h265.NALUType_SPS_NUT || t == h265.NALUType_PPS_NUT ||
			t == h265.NALUType_AUD_NUT || t == h265.NALUType_PREFIX_SEI_NUT
	}
	switch h264.NALUType(nalu[0] & 0x1f) {
	case h264.NALUTypeNonIDR, h264.NALUTypeIDR:
		return len(nalu) > 1 && nalu[1]&0x80 != 0 // first_mb_in_slice == 0 is the single bit 1
	case h264.NALUTypeSEI, h264.NALUTypeSPS, h264.NALUTypePPS, h264.NALUTypeAccessUnitDelimiter:
		return true
	}
	return false
}

// isRandomAccess reports whether a decoder can start at the access unit.
func isRandomAccess(codec Codec, nalus [][]byte) bool {
	if codec == CodecH265 {
		return h265.IsRandomAccess(nalus)
	}
	return h264.IsRandomAccess(nalus)
}

// paramKind classifies the NAL units that a decoder needs before an IDR.
type paramKind int

const (
	paramNone paramKind = iota
	paramVPS
	paramSPS
	paramPPS
)

func paramKindOf(codec Codec, nalu []byte) paramKind {
	if codec == CodecH265 {
		switch h265.NALUType((nalu[0] >> 1) & 0x3f) {
		case h265.NALUType_VPS_NUT:
			return paramVPS
		case h265.NALUType_SPS_NUT:
			return paramSPS
		case h265.NALUType_PPS_NUT:
			return paramPPS
		}
		return paramNone
	}
	switch h264.NALUType(nalu[0] & 0x1f) {
	case h264.NALUTypeSPS:
		return paramSPS
	case h264.NALUTypePPS:
		return paramPPS
	}
	return paramNone
}

// profileOf returns the Source.Profile string for an SPS, or "" when the SPS is
// too short to hold one.
func profileOf(codec Codec, sps []byte) string {
	if codec == CodecH265 {
		// After the 2-byte NAL header: one byte of VPS id, sub-layer count and
		// nesting flag, then general_profile_space/tier/profile_idc, 4 bytes of
		// compatibility flags, 6 bytes of constraint flags and general_level_idc.
		if len(sps) < 2 {
			return ""
		}
		rbsp := h264.EmulationPreventionRemove(sps[2:])
		if len(rbsp) < 13 {
			return ""
		}
		return hex.EncodeToString([]byte{rbsp[1], rbsp[12]})
	}
	if len(sps) < 4 {
		return ""
	}
	return hex.EncodeToString(sps[1:4])
}

// firstProfile returns the profile of the first SPS in the access units.
func firstProfile(codec Codec, aus []AccessUnit) string {
	for _, au := range aus {
		for _, nalu := range au.NALUs {
			if paramKindOf(codec, nalu) == paramSPS {
				return profileOf(codec, nalu)
			}
		}
	}
	return ""
}
