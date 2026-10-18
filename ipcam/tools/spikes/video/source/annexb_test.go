package source

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestReadAnnexB_SplitsAccessUnits(t *testing.T) {
	aus, err := ReadAnnexB(mediaFile(t, "sub.h264"))
	if err != nil {
		t.Fatal(err)
	}
	if len(aus) != 720 {
		t.Fatalf("got %d access units, want 720", len(aus))
	}
	for i, au := range aus {
		if want := i%12 == 0; au.IDR != want {
			t.Fatalf("frame %d: IDR = %v, want %v", i, au.IDR, want)
		}
		if int(au.Frame) != i || au.Loop != 0 {
			t.Fatalf("frame %d: Frame = %d, Loop = %d", i, au.Frame, au.Loop)
		}
		if au.IDR && au.NALUs[0][0]&0x1f != 7 {
			t.Fatalf("frame %d: an IDR access unit must start with its SPS, got NAL type %d", i, au.NALUs[0][0]&0x1f)
		}
	}
}

func TestReadAnnexB_MediaFiles(t *testing.T) {
	tests := []struct {
		file   string
		frames int
		gop    int
	}{
		{"sub.h264", 720, 12},
		{"main.h264", 1200, 20},
		{"main-high51.h264", 400, 20},
		{"main-h265.h265", 1200, 20},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			aus, err := ReadAnnexB(mediaFile(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if len(aus) != tc.frames {
				t.Fatalf("got %d access units, want %d", len(aus), tc.frames)
			}
			for i, au := range aus {
				if want := i%tc.gop == 0; au.IDR != want {
					t.Fatalf("frame %d: IDR = %v, want %v", i, au.IDR, want)
				}
			}
		})
	}
}

// annexB joins NAL units with start codes, alternating the 3-byte and 4-byte form.
func annexB(nalus ...[]byte) []byte {
	var out []byte
	for i, n := range nalus {
		if i%2 == 0 {
			out = append(out, 0, 0, 0, 1)
		} else {
			out = append(out, 0, 0, 1)
		}
		out = append(out, n...)
	}
	return out
}

func writeStream(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadAnnexB_GroupsH264NALUnits(t *testing.T) {
	var (
		sps       = []byte{0x67, 0x4d, 0x00, 0x1f, 0xac}
		pps       = []byte{0x68, 0xee, 0x3c, 0x80}
		sei       = []byte{0x06, 0x05, 0x01, 0x80}
		idr1      = []byte{0x65, 0x88, 0x84}
		idr2      = []byte{0x65, 0x04, 0x11} // second slice of the same picture: first_mb_in_slice != 0
		p1        = []byte{0x41, 0x9a, 0x01}
		p2        = []byte{0x41, 0x9a, 0x02}
		aud       = []byte{0x09, 0xf0}
		p3        = []byte{0x41, 0x9a, 0x03}
		endOfSeq  = []byte{0x0a}
		trailing0 = []byte{0, 0, 0, 0} // trailing_zero_8bits before the next start code
	)
	stream := annexB(sps, pps, sei, idr1, idr2, p1)
	stream = append(stream, trailing0...)
	stream = append(stream, annexB(p2, aud, p3, endOfSeq)...)

	aus, err := ReadAnnexB(writeStream(t, "multi.h264", stream))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		idr   bool
		nalus [][]byte
	}{
		{true, [][]byte{sps, pps, sei, idr1, idr2}},
		{false, [][]byte{p1}},
		{false, [][]byte{p2}},
		{false, [][]byte{aud, p3, endOfSeq}},
	}
	if len(aus) != len(want) {
		t.Fatalf("got %d access units, want %d", len(aus), len(want))
	}
	for i, w := range want {
		got := aus[i]
		if got.IDR != w.idr || int(got.Frame) != i {
			t.Errorf("access unit %d: IDR = %v, Frame = %d", i, got.IDR, got.Frame)
		}
		if !equalNALUs(got.NALUs, w.nalus) {
			t.Errorf("access unit %d: NAL units differ\n got %x\nwant %x", i, got.NALUs, w.nalus)
		}
	}
}

func TestReadAnnexB_GroupsH265NALUnits(t *testing.T) {
	var (
		vps       = []byte{0x40, 0x01, 0x0c, 0x01}
		sps       = []byte{0x42, 0x01, 0x01, 0x01}
		pps       = []byte{0x44, 0x01, 0xc1, 0x72}
		idr       = []byte{0x26, 0x01, 0xaf, 0x11} // IDR_W_RADL, first_slice_segment_in_pic_flag set
		trail1    = []byte{0x02, 0x01, 0xd0, 0x01}
		prefixSEI = []byte{0x4e, 0x01, 0x05, 0x80}
		trail2    = []byte{0x02, 0x01, 0xd0, 0x02}
		suffixSEI = []byte{0x50, 0x01, 0x05, 0x80}
	)
	aus, err := ReadAnnexB(writeStream(t, "multi.h265", annexB(vps, sps, pps, idr, trail1, prefixSEI, trail2, suffixSEI)))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		idr   bool
		nalus [][]byte
	}{
		{true, [][]byte{vps, sps, pps, idr}},
		{false, [][]byte{trail1}},
		{false, [][]byte{prefixSEI, trail2, suffixSEI}},
	}
	if len(aus) != len(want) {
		t.Fatalf("got %d access units, want %d", len(aus), len(want))
	}
	for i, w := range want {
		if aus[i].IDR != w.idr || !equalNALUs(aus[i].NALUs, w.nalus) {
			t.Errorf("access unit %d: IDR = %v, NAL units %x, want IDR = %v, NAL units %x",
				i, aus[i].IDR, aus[i].NALUs, w.idr, w.nalus)
		}
	}
}

func TestReadAnnexB_RejectsBadInput(t *testing.T) {
	sps := []byte{0x67, 0x4d, 0x00, 0x1f}
	pps := []byte{0x68, 0xee, 0x3c, 0x80}
	tests := []struct {
		name string
		data []byte
	}{
		{"empty file", nil},
		{"no start code", []byte{0x65, 0x88, 0x84, 0x00}},
		{"data before the first start code", append([]byte{0xff}, annexB(sps, pps, []byte{0x65, 0x88})...)},
		{"unrecognised first NAL unit", annexB([]byte{0x7f, 0x01, 0x02})},
		{"forbidden_zero_bit set", annexB([]byte{0xe7, 0x4d, 0x00, 0x1f})},
		{"no picture in the stream", annexB(sps, pps)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if aus, err := ReadAnnexB(writeStream(t, "bad.h264", tc.data)); err == nil {
				t.Fatalf("got %d access units and no error", len(aus))
			}
		})
	}
	t.Run("missing file", func(t *testing.T) {
		if _, err := ReadAnnexB(filepath.Join(t.TempDir(), "absent.h264")); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func equalNALUs(a, b [][]byte) bool {
	return slices.EqualFunc(a, b, bytes.Equal)
}
