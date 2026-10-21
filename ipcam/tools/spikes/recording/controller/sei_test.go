package controller

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

// startCodeEmulation reports whether nalu holds 00 00 00, 00 00 01 or
// 00 00 02, which emulation prevention must never leave in a NAL unit.
func startCodeEmulation(nalu []byte) bool {
	for i := 0; i+2 < len(nalu); i++ {
		if nalu[i] == 0 && nalu[i+1] == 0 && nalu[i+2] <= 2 {
			return true
		}
	}
	return false
}

func TestSEI_StampAndRead_RoundTrip(t *testing.T) {
	sps := []byte{0x67, 0x4d, 0x00, 0x28, 0xac, 0xd9}
	pps := []byte{0x68, 0xee, 0x3c, 0x80}
	idr := []byte{0x65, 0x88, 0x84, 0x00, 0x21}
	for _, index := range []uint64{0, 3, 0x0000000000000300, 1 << 63} {
		in := [][]byte{sps, pps, idr}
		out := StampIndex(in, index)

		if len(in) != 3 || !bytes.Equal(in[0], sps) {
			t.Fatalf("index %#x: StampIndex changed its input", index)
		}
		if len(out) != 4 || !slices.EqualFunc(out[1:], in, bytes.Equal) {
			t.Fatalf("index %#x: StampIndex = %d NAL units, want the SEI then the input", index, len(out))
		}
		sei := out[0]
		if h264.NALUType(sei[0]&0x1f) != h264.NALUTypeSEI || sei[0]&0x80 != 0 {
			t.Fatalf("index %#x: first NAL unit header %#x is not an SEI", index, sei[0])
		}
		if startCodeEmulation(sei) {
			t.Errorf("index %#x: the SEI % x emulates a start code", index, sei)
		}
		if sei[len(sei)-1] != 0x80 {
			t.Errorf("index %#x: the SEI ends in %#x, want the rbsp stop bit 0x80", index, sei[len(sei)-1])
		}
		rbsp := h264.EmulationPreventionRemove(sei[1:])
		wantRBSP := append(binary.BigEndian.AppendUint64(append([]byte{5, 24}, SEIUUID[:]...), index), 0x80)
		if !bytes.Equal(rbsp, wantRBSP) {
			t.Errorf("index %#x: SEI payload % x, want % x", index, rbsp, wantRBSP)
		}

		got, ok := ReadIndex(out)
		if !ok || got != index {
			t.Errorf("ReadIndex(StampIndex(au, %#x)) = %#x, %v", index, got, ok)
		}
		if h264.IsRandomAccess(out) != h264.IsRandomAccess(in) {
			t.Errorf("index %#x: the SEI changed whether the access unit is a keyframe", index)
		}
	}

	t.Run("emulation prevention bytes are inserted", func(t *testing.T) {
		// 0x300 is 00 00 00 00 00 00 03 00: the zeros need 03 escapes.
		sei := StampIndex(nil, 0x300)[0]
		if !bytes.Contains(sei, []byte{0, 0, 3}) {
			t.Errorf("the SEI of index 0x300 % x has no emulation prevention byte", sei)
		}
	})

	t.Run("an access unit without the index", func(t *testing.T) {
		// x264 writes its own user_data_unregistered SEI with another UUID.
		x264 := append([]byte{0x06, 0x05, 0x11}, bytes.Repeat([]byte{0xdc}, 17)...)
		x264 = append(x264, 0x80)
		for name, au := range map[string][][]byte{
			"no SEI":        {sps, pps, idr},
			"another UUID":  {x264, idr},
			"empty":         nil,
			"truncated SEI": {StampIndex(nil, 7)[0][:12]},
		} {
			if got, ok := ReadIndex(au); ok {
				t.Errorf("%s: ReadIndex = %d, true; want false", name, got)
			}
		}
		if got, ok := ReadIndex(append([][]byte{x264}, StampIndex([][]byte{idr}, 42)...)); !ok || got != 42 {
			t.Errorf("after an x264 SEI: ReadIndex = %d, %v; want 42, true", got, ok)
		}
	})
}
