package mi9

import (
	"bytes"
	"encoding/binary"
	"math"
	"runtime"
	"slices"
	"testing"
)

// The fMP4 of these tests is written box by box, so that a box can carry
// what no muxer writes, as the hub's own walker tests write it
// (hub/cmd/ipcam-hub/diag_fmp4_test.go): the same boxes and the same corrupt
// recordings, so that this walk and the hub's refuse the same ones (C01
// keeps them two copies).

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func u64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

// box is an ISO BMFF box of typ around payload.
func box(typ string, payload ...[]byte) []byte {
	body := slices.Concat(payload...)
	return slices.Concat(u32(uint32(8+len(body))), []byte(typ), body) //nolint:gosec // small test boxes
}

// fullBox is a box of version 0 with flags.
func fullBox(typ string, flags uint32, payload ...[]byte) []byte {
	return box(typ, append([][]byte{u32(flags)}, payload...)...)
}

// testInit is an init with one video track, ID 1 at 90 kHz, whose trex
// sets no default.
func testInit() []byte {
	tkhd := fullBox("tkhd", 0, u32(0), u32(0), u32(1), make([]byte, 68))
	mdhd := fullBox("mdhd", 0, u32(0), u32(0), u32(90000), make([]byte, 8))
	hdlr := fullBox("hdlr", 0, u32(0), []byte("vide"), make([]byte, 13))
	trex := fullBox("trex", 0, u32(1), u32(1), u32(0), u32(0), u32(0))
	return slices.Concat(box("ftyp", []byte("iso5"), u32(1)), box("moov", box("trak", tkhd, box("mdia", mdhd, hdlr)), box("mvex", trex)))
}

// testPart is a part of track 1: a moof with tfhd (and its base data
// offset, when base is set), tfdt and trun, then an mdat of n bytes.
func testPart(trun []byte, base *uint64, n int) []byte {
	tfhd := fullBox("tfhd", 0, u32(1))
	if base != nil {
		tfhd = fullBox("tfhd", tfhdBaseDataOffset, u32(1), u64(*base))
	}
	return slices.Concat(box("moof", box("traf", tfhd, fullBox("tfdt", 0, u32(0)), box("trun", trun))), box("mdat", make([]byte, n)))
}

// trunBody is a trun's body: its flags with a data offset of 0, its count,
// and the entries' fields.
func trunBody(flags, count uint32, fields ...uint32) []byte {
	b := slices.Concat(u32(flags), u32(count), u32(0))
	for _, f := range fields {
		b = append(b, u32(f)...)
	}
	return b
}

// allocated is the bytes that f allocates on the heap.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// corruptRecordings are the plaintexts whose index the hub's walker
// refuses, by the same names.
func corruptRecordings() map[string][]byte {
	far := uint64(math.MaxInt64 - 1<<20)
	valid := testPart(trunBody(trunDataOffset|trunSize, 2, 100, 100), nil, 200)
	return map[string][]byte{
		"a huge count of sized samples":                 slices.Concat(testInit(), testPart(trunBody(trunDataOffset|trunSize, math.MaxUint32), nil, 16)),
		"a huge count without per-sample fields":        slices.Concat(testInit(), testPart(trunBody(trunDataOffset, math.MaxUint32), nil, 16)),
		"4 Mi samples without per-sample fields":        slices.Concat(testInit(), testPart(trunBody(trunDataOffset, 1<<22), nil, 16)),
		"3 sized samples, 2 of them written":            slices.Concat(testInit(), testPart(trunBody(trunDataOffset|trunSize, 3, 100, 100), nil, 300)),
		"a trun cut before its count":                   slices.Concat(testInit(), testPart(u32(trunDataOffset), nil, 16)),
		"a part cut short":                              slices.Concat(testInit(), valid[:len(valid)-5]),
		"a 64 MiB sample whose offset overflows":        slices.Concat(testInit(), testPart(trunBody(trunDataOffset|trunSize, 1, 64<<20), &far, 16)),
		"a 64 MiB sample past the end of the plaintext": slices.Concat(testInit(), testPart(trunBody(trunDataOffset|trunSize, 1, 64<<20), nil, 16)),
	}
}

// walkSegment indexes plain as the hub side does: its init, each part's
// moof, and the first IDR.
func walkSegment(plain []byte) error {
	s := &Segment{r: bytes.NewReader(plain), size: int64(len(plain))}
	if err := s.readInit(); err != nil {
		return err
	}
	if err := s.eachPart(func(span, []fmp4Sample) (bool, error) { return true, nil }); err != nil {
		return err
	}
	_, err := s.firstIDR(0)
	return err
}

// TestReader_TrunBoundsAsTheHubs (Task 11 review): the hub side's walk of
// a recording keeps the hub's rules for a corrupt index, case by case as
// the hub's TestFMP4_CorruptIndexFailsWithBoundedMemory and
// TestFMP4_ManyTrunsPerMoofAreBounded pin them: a trun whose count its box
// cannot hold, or above maxTrunSamples, a box cut short and a first IDR
// outside its part, its offset overflowing or not, are refused with an
// error and a bounded allocation; so is a moof of many truns of the video
// track, after one trun's samples at most. A valid recording walks.
func TestReader_TrunBoundsAsTheHubs(t *testing.T) {
	for name, plain := range corruptRecordings() {
		t.Run(name, func(t *testing.T) {
			var err error
			n := allocated(func() { err = walkSegment(plain) })
			if err == nil || n > 1<<20 {
				t.Errorf("walk = %v after %d bytes allocated, want an error and at most 1 MiB", err, n)
			}
		})
	}
	t.Run("many truns in one moof", func(t *testing.T) {
		const truns = 8
		trun := box("trun", trunBody(trunDataOffset, maxTrunSamples))
		traf := box("traf", fullBox("tfhd", 0, u32(1)), fullBox("tfdt", 0, u32(0)), bytes.Repeat(trun, truns))
		plain := slices.Concat(testInit(), box("moof", traf), box("mdat", make([]byte, 16)))
		var err error
		n := allocated(func() { err = walkSegment(plain) })
		// One trun's samples: 1 Mi of 32 bytes, plus the walk's own few KB.
		if err == nil || n > 33<<20 {
			t.Errorf("walk of a moof of %d truns of %d samples = %v after %d MiB allocated, want an error and at most one trun's 32 MiB", truns, maxTrunSamples, err, n>>20)
		}
	})
	valid := slices.Concat(testInit(), testPart(trunBody(trunDataOffset|trunSize, 2, 100, 100), nil, 200))
	if err := walkSegment(valid); err != nil {
		t.Errorf("walk of a valid recording = %v", err)
	}
}
