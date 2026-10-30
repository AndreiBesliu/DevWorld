package main

import (
	"bytes"
	"encoding/binary"
	"maps"
	"math"
	"runtime"
	"slices"
	"testing"
)

// The fMP4 of these tests is written box by box, so that a box can carry
// what no muxer writes: a corrupt sample count, a cut, an offset that
// overflows.

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
	mdhd := fullBox("mdhd", 0, u32(0), u32(0), u32(benchTimeScale), make([]byte, 8))
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

// corruptRecordings are plaintexts whose index the benches must refuse.
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

// TestFMP4_ManyTrunsPerMoofAreBounded (Task 10b review): maxTrunSamples
// caps each trun, so a moof of many truns of the video track, each counting
// 1 Mi samples without per-sample fields (20 bytes a trun), would allocate
// and walk 1 Mi samples per trun. MediaMTX writes one trun per traf, so
// the walker refuses a second one, after the first trun's cap at most.
func TestFMP4_ManyTrunsPerMoofAreBounded(t *testing.T) {
	const truns = 8
	trun := box("trun", trunBody(trunDataOffset, maxTrunSamples))
	traf := box("traf", fullBox("tfhd", 0, u32(1)), fullBox("tfdt", 0, u32(0)), bytes.Repeat(trun, truns))
	plain := slices.Concat(testInit(), box("moof", traf), box("mdat", make([]byte, 16)))
	var err error
	n := allocated(func() { err = walkFMP4(plain) })
	// One trun's samples: 1 Mi of 32 bytes, plus the walk's own few KB.
	if err == nil || n > 33<<20 {
		t.Errorf("walk of a moof of %d truns of %d samples = %v after %d MiB allocated, want an error and at most one trun's 32 MiB", truns, maxTrunSamples, err, n>>20)
	}
}

// TestFMP4_CorruptIndexFailsWithBoundedMemory (Task 10 review): the
// benches' fMP4 walker refuses a corrupt recording with an error and a
// bounded allocation: a trun whose sample count its box cannot hold, with
// per-sample fields or without (where 1 Mi samples is the cap), a box cut
// short, and a sample outside the plaintext, its offset overflowing or not.
// It never allocates one sample per counted entry, nor a sample's bytes
// before they are known to lie in the plaintext.
func TestFMP4_CorruptIndexFailsWithBoundedMemory(t *testing.T) {
	for name, plain := range corruptRecordings() {
		t.Run(name, func(t *testing.T) {
			var err error
			n := allocated(func() { err = walkFMP4(plain) })
			if err == nil || n > 1<<20 {
				t.Errorf("walk = %v after %d bytes allocated, want an error and at most 1 MiB", err, n)
			}
		})
	}
	valid := slices.Concat(testInit(), testPart(trunBody(trunDataOffset|trunSize, 2, 100, 100), nil, 200))
	if err := walkFMP4(valid); err != nil {
		t.Errorf("walk of a valid recording = %v", err)
	}
}

// walkFMP4 indexes plain as the benches do: its init, each part's moof, and
// the first IDR.
func walkFMP4(plain []byte) error {
	r, size := bytes.NewReader(plain), int64(len(plain))
	m, err := openFMP4(r, size)
	if err != nil {
		return err
	}
	if err := m.eachPart(func(span, []fmp4Sample) (bool, error) { return true, nil }); err != nil {
		return err
	}
	_, err = firstIDR(r, size, 0)
	return err
}

// FuzzFMP4Index walks any plaintext as the benches do; whatever it holds,
// the walk returns, with an error or not, and never panics. Each box's
// allocation has its cap (TestFMP4_CorruptIndexFailsWithBoundedMemory). The
// seeds, a valid recording and the corrupt ones, run in every go test.
func FuzzFMP4Index(f *testing.F) {
	f.Add(slices.Concat(testInit(), testPart(trunBody(trunDataOffset|trunSize, 2, 100, 100), nil, 200)))
	corrupt := corruptRecordings()
	for _, name := range slices.Sorted(maps.Keys(corrupt)) {
		f.Add(corrupt[name])
	}
	f.Fuzz(func(_ *testing.T, plain []byte) { _ = walkFMP4(plain) })
}
