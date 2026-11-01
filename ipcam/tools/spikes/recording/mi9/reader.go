// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi9

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"filippo.io/age"
)

// The hub side reads a recording as hubd will (§8.5 step 4, C01's
// exception): its plaintext through age.DecryptReaderAt with the run's
// identity, and in it the fMP4 that MediaMTX writes (§4.2): an init (ftyp,
// moov), then one moof and one mdat per part, with free boxes as padding.
//
// Tools cannot import the hub (C01), so this is a copy of the walk of
// ipcam-hub's diag benches, hub/cmd/ipcam-hub/diag_fmp4.go, which names
// this file in turn. A fix to one goes into the other, since both must
// keep the same rules:
//   - the first-IDR rule of a seek: the first sync sample of the video
//     track whose decode time from the recording's first sample is the
//     target or later;
//   - the trun bounds: at most maxTrunSamples samples in a trun, whose
//     entries must fit its box, and one trun of the video track per traf,
//     so that a corrupt moof is refused before its samples are allocated;
//   - the first IDR lies inside the plaintext, checked without an
//     overflow.
//
// TestReader_TrunBoundsAsTheHubs pins here the corrupt recordings of the
// hub's TestFMP4_CorruptIndexFailsWithBoundedMemory and
// TestFMP4_ManyTrunsPerMoofAreBounded.

// span is a byte range of the plaintext.
type span struct{ off, n int64 }

// errNotFMP4 is a recording that is not laid out as MediaMTX writes one.
var errNotFMP4 = errors.New("the plaintext is not an fMP4 recording")

// Segment is a recording opened on the hub side: its plaintext, the length
// of its init, its video track and the track's RFC 6381 codec.
type Segment struct {
	f         *os.File
	r         io.ReaderAt
	size      int64
	init      int64 // the ftyp and the moov
	video     uint32
	timescale uint32
	defaults  sampleDefaults
	codec     string
}

// OpenSegment opens the encrypted recording at path with id, its RI_g, and
// reads its init.
func OpenSegment(path string, id age.Identity) (*Segment, error) {
	f, err := os.Open(path) //nolint:gosec // the recording that the run names
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil {
		var s *Segment
		if s, err = openPlaintext(f, info.Size(), id); err == nil {
			return s, nil
		}
	}
	return nil, errors.Join(err, f.Close())
}

func openPlaintext(f *os.File, encrypted int64, id age.Identity) (*Segment, error) {
	r, size, err := age.DecryptReaderAt(f, encrypted, id)
	if err != nil {
		return nil, err
	}
	s := &Segment{f: f, r: r, size: size}
	if err := s.readInit(); err != nil {
		return nil, err
	}
	return s, nil
}

// Size is the plaintext's length.
func (s *Segment) Size() int64 { return s.size }

// InitSize is the length of the init, the plaintext's first bytes.
func (s *Segment) InitSize() int64 { return s.init }

// Codec is the video track's codec as MSE names it ("avc1.4d401f"), "" for
// a codec other than H.264.
func (s *Segment) Codec() string { return s.codec }

// ReadAt reads the plaintext.
func (s *Segment) ReadAt(p []byte, off int64) (int, error) { return s.r.ReadAt(p, off) }

// Close closes the recording's file.
func (s *Segment) Close() error { return s.f.Close() }

// fmp4Box is an ISO BMFF box header.
type fmp4Box struct {
	off, size, hdr int64
	typ            string
}

func (b fmp4Box) end() int64 { return b.off + b.size }

// readBox reads the header of the box at off.
func (s *Segment) readBox(off int64) (fmp4Box, error) {
	var h [16]byte
	if s.size-off < 8 {
		return fmp4Box{}, fmt.Errorf("%w: a box header past the end, at %d", errNotFMP4, off)
	}
	if err := readFullAt(s.r, h[:8], off); err != nil {
		return fmp4Box{}, err
	}
	b := fmp4Box{off: off, size: int64(binary.BigEndian.Uint32(h[:4])), hdr: 8, typ: string(h[4:8])}
	if b.size == 1 {
		if s.size-off < 16 {
			return fmp4Box{}, fmt.Errorf("%w: a box header past the end, at %d", errNotFMP4, off)
		}
		if err := readFullAt(s.r, h[8:16], off+8); err != nil {
			return fmp4Box{}, err
		}
		b.size, b.hdr = int64(binary.BigEndian.Uint64(h[8:16])), 16 //nolint:gosec // checked below
	}
	if b.size < b.hdr || b.end() > s.size {
		return fmp4Box{}, fmt.Errorf("%w: a %q box of %d bytes at %d", errNotFMP4, b.typ, b.size, off)
	}
	return b, nil
}

// readBody reads a whole box, without its header.
func (s *Segment) readBody(b fmp4Box) ([]byte, error) {
	const maxBody = 16 << 20 // a moov or a moof is a few KB
	if b.size-b.hdr > maxBody {
		return nil, fmt.Errorf("%w: a %q box of %d bytes", errNotFMP4, b.typ, b.size)
	}
	body := make([]byte, b.size-b.hdr)
	if err := readFullAt(s.r, body, b.off+b.hdr); err != nil {
		return nil, err
	}
	return body, nil
}

// readFullAt reads len(p) bytes at off: an io.ReaderAt may give io.EOF
// with all of them when they end its data.
func readFullAt(r io.ReaderAt, p []byte, off int64) error {
	n, err := r.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return err
}

// eachChild calls f with the type and body of each box in data.
func eachChild(data []byte, f func(typ string, body []byte) error) error {
	for len(data) > 0 {
		if len(data) < 8 {
			return errNotFMP4
		}
		size := binary.BigEndian.Uint32(data)
		if size < 8 || uint64(size) > uint64(len(data)) {
			return errNotFMP4
		}
		if err := f(string(data[4:8]), data[8:size]); err != nil {
			return err
		}
		data = data[size:]
	}
	return nil
}

// sampleDefaults are a track's default sample duration, size and flags,
// from its trex, and a fragment's from its tfhd.
type sampleDefaults struct{ duration, size, flags uint32 }

// fmp4Sample is one sample of the video track: its decode time, in the
// track's timescale, where its bytes lie in the plaintext, and whether it is
// a sync sample.
type fmp4Sample struct {
	time      uint64
	off, size int64
	sync      bool
}

// readInit reads the ftyp and the moov: the video track, its timescale,
// its trex defaults and its codec.
func (s *Segment) readInit() error {
	ftyp, err := s.readBox(0)
	if err != nil {
		return err
	}
	moov, err := s.readBox(ftyp.end())
	if err != nil {
		return err
	}
	if ftyp.typ != "ftyp" || moov.typ != "moov" {
		return fmt.Errorf("%w: it starts with %q and %q", errNotFMP4, ftyp.typ, moov.typ)
	}
	body, err := s.readBody(moov)
	if err != nil {
		return err
	}
	s.init = moov.end()
	trex := map[uint32]sampleDefaults{}
	err = eachChild(body, func(typ string, b []byte) error {
		switch typ {
		case "trak":
			t, err := readTrak(b)
			if err != nil {
				return err
			}
			if t.video && s.video == 0 {
				s.video, s.timescale, s.codec = t.id, t.scale, t.codec
			}
		case "mvex":
			return eachChild(b, func(typ string, b []byte) error {
				if typ == "trex" {
					if len(b) < 24 {
						return errNotFMP4
					}
					trex[binary.BigEndian.Uint32(b[4:])] = sampleDefaults{binary.BigEndian.Uint32(b[12:]), binary.BigEndian.Uint32(b[16:]), binary.BigEndian.Uint32(b[20:])}
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if s.video == 0 || s.timescale == 0 {
		return fmt.Errorf("%w: no video track", errNotFMP4)
	}
	s.defaults = trex[s.video]
	return nil
}

// trak is what the hub side reads of a trak: its track ID (tkhd), its
// timescale (mdhd), whether it is video (hdlr "vide") and its codec.
type trak struct {
	id, scale uint32
	video     bool
	codec     string
}

func readTrak(b []byte) (trak, error) {
	var t trak
	// The tkhd's and the mdhd's field is after a version, flags and two
	// times: 4 bytes each in version 0, the times 8 bytes in version 1.
	field := func(b []byte) (uint32, error) {
		at := 12
		if len(b) > 0 && b[0] == 1 {
			at = 20
		}
		if len(b) < at+4 {
			return 0, errNotFMP4
		}
		return binary.BigEndian.Uint32(b[at:]), nil
	}
	err := eachChild(b, func(typ string, b []byte) error {
		var err error
		switch typ {
		case "tkhd":
			t.id, err = field(b)
		case "mdia":
			err = eachChild(b, func(typ string, b []byte) error {
				var err error
				switch typ {
				case "mdhd":
					t.scale, err = field(b)
				case "hdlr": // version and flags, pre_defined, then the handler type
					if len(b) < 12 {
						return errNotFMP4
					}
					t.video = string(b[8:12]) == "vide"
				case "minf":
					t.codec = codecOf(b)
				}
				return err
			})
		}
		return err
	})
	return t, err
}

// avc1Fields is the length of an avc1 sample entry's own fields, before its
// boxes (ISO/IEC 14496-12 VisualSampleEntry).
const avc1Fields = 78

// codecOf is the RFC 6381 codec of a minf's H.264 sample entry, avc1 or
// avc3, from its avcC ("avc1.PPCCLL"), or "".
func codecOf(minf []byte) string {
	codec := ""
	_ = eachChild(minf, func(typ string, b []byte) error {
		if typ != "stbl" {
			return nil
		}
		return eachChild(b, func(typ string, b []byte) error {
			if typ != "stsd" || len(b) < 8 { // version, flags and the entry count
				return nil
			}
			return eachChild(b[8:], func(typ string, b []byte) error {
				if (typ != "avc1" && typ != "avc3") || len(b) < avc1Fields {
					return nil
				}
				return eachChild(b[avc1Fields:], func(typ string, b []byte) error {
					if typ == "avcC" && len(b) >= 4 {
						codec = fmt.Sprintf("avc1.%02x%02x%02x", b[1], b[2], b[3])
					}
					return nil
				})
			})
		})
	})
	return codec
}

// The tfhd and trun flags that the walk reads.
const (
	tfhdBaseDataOffset = 0x01
	tfhdDescription    = 0x02
	tfhdDuration       = 0x08
	tfhdSize           = 0x10
	tfhdFlags          = 0x20
	trunDataOffset     = 0x001
	trunFirstFlags     = 0x004
	trunDuration       = 0x100
	trunSize           = 0x200
	trunFlags          = 0x400
	trunCTO            = 0x800
	nonSyncSample      = 0x10000
)

// maxTrunSamples bounds a trun's sample count, and a traf has one trun of
// the video track: a corrupt moof cannot make the walk allocate one sample
// per counted entry.
const maxTrunSamples = 1 << 20

// eachPart calls visit with each part of the recording, in order: its span,
// from its moof to the end of its mdat, and the video track's samples in
// it. It stops when visit returns false.
func (s *Segment) eachPart(visit func(part span, samples []fmp4Sample) (bool, error)) error {
	for pos := s.init; pos < s.size; {
		b, err := s.readBox(pos)
		if err != nil {
			return err
		}
		pos = b.end()
		switch b.typ {
		case "free", "skip":
			continue
		case "moof":
		default:
			return fmt.Errorf("%w: a %q box where a part should start, at %d", errNotFMP4, b.typ, b.off)
		}
		mdat, err := s.readBox(b.end())
		if err != nil {
			return err
		}
		if mdat.typ != "mdat" {
			return fmt.Errorf("%w: a moof without its mdat, at %d", errNotFMP4, b.off)
		}
		pos = mdat.end()
		body, err := s.readBody(b)
		if err != nil {
			return err
		}
		var samples []fmp4Sample
		err = eachChild(body, func(typ string, traf []byte) error {
			if typ != "traf" || samples != nil {
				return nil
			}
			var err error
			samples, err = s.readTraf(traf, b.off)
			return err
		})
		if err != nil {
			return err
		}
		more, err := visit(span{off: b.off, n: mdat.end() - b.off}, samples)
		if err != nil || !more {
			return err
		}
	}
	return nil
}

// readTraf returns the samples of a traf when it is the video track's, else
// nil; moof is where its moof starts. A second trun of the video track is
// refused, as MediaMTX writes one.
func (s *Segment) readTraf(traf []byte, moof int64) ([]fmp4Sample, error) {
	d := s.defaults
	base := moof
	var start uint64
	var samples []fmp4Sample
	video, seen := false, false
	err := eachChild(traf, func(typ string, b []byte) error {
		switch typ {
		case "tfhd":
			if len(b) < 8 {
				return errNotFMP4
			}
			flags := binary.BigEndian.Uint32(b) & 0xffffff
			video = binary.BigEndian.Uint32(b[4:]) == s.video
			at := 8
			for _, f := range []struct {
				flag uint32
				n    int
				set  func([]byte)
			}{
				{tfhdBaseDataOffset, 8, func(v []byte) { base = int64(binary.BigEndian.Uint64(v)) }}, //nolint:gosec // an offset in the file
				{tfhdDescription, 4, func([]byte) {}},
				{tfhdDuration, 4, func(v []byte) { d.duration = binary.BigEndian.Uint32(v) }},
				{tfhdSize, 4, func(v []byte) { d.size = binary.BigEndian.Uint32(v) }},
				{tfhdFlags, 4, func(v []byte) { d.flags = binary.BigEndian.Uint32(v) }},
			} {
				if flags&f.flag != 0 {
					if len(b) < at+f.n {
						return errNotFMP4
					}
					f.set(b[at : at+f.n])
					at += f.n
				}
			}
		case "tfdt":
			switch {
			case len(b) >= 12 && b[0] == 1:
				start = binary.BigEndian.Uint64(b[4:])
			case len(b) >= 8 && b[0] == 0:
				start = uint64(binary.BigEndian.Uint32(b[4:]))
			default:
				return errNotFMP4
			}
		case "trun":
			if !video {
				return nil
			}
			if seen {
				return fmt.Errorf("%w: a second trun of the video track in a traf, at the moof at %d", errNotFMP4, moof)
			}
			seen = true
			var err error
			samples, err = readTrun(b, base, start, d)
			return err
		}
		return nil
	})
	if !video {
		return nil, err
	}
	return samples, err
}

// readTrun reads a trun's samples, which start at decode time start, their
// data at base plus the trun's data offset. A count above maxTrunSamples,
// or one that the box cannot hold, is refused before anything is
// allocated.
func readTrun(b []byte, base int64, start uint64, d sampleDefaults) ([]fmp4Sample, error) {
	if len(b) < 8 {
		return nil, errNotFMP4
	}
	flags := binary.BigEndian.Uint32(b) & 0xffffff
	n := binary.BigEndian.Uint32(b[4:])
	if n > maxTrunSamples {
		return nil, fmt.Errorf("%w: a trun of %d samples", errNotFMP4, n)
	}
	count, at := int(n), 8
	if flags&trunDataOffset == 0 {
		return nil, fmt.Errorf("%w: a trun without a data offset", errNotFMP4)
	}
	if len(b) < at+4 {
		return nil, errNotFMP4
	}
	off := base + int64(int32(binary.BigEndian.Uint32(b[at:]))) //nolint:gosec // a signed offset by the spec
	at += 4
	first, firstFlags := d.flags, false
	if flags&trunFirstFlags != 0 {
		if len(b) < at+4 {
			return nil, errNotFMP4
		}
		first, firstFlags = binary.BigEndian.Uint32(b[at:]), true
		at += 4
	}
	entry := 0
	for _, f := range []uint32{trunDuration, trunSize, trunFlags, trunCTO} {
		if flags&f != 0 {
			entry += 4
		}
	}
	if entry > 0 && count > (len(b)-at)/entry {
		return nil, fmt.Errorf("%w: a trun of %d samples in %d bytes", errNotFMP4, count, len(b))
	}
	samples := make([]fmp4Sample, count)
	t := start
	for i := range samples {
		e := b[at+i*entry:]
		duration, size, sampleFlags := d.duration, d.size, d.flags
		if i == 0 && firstFlags {
			sampleFlags = first
		}
		if flags&trunDuration != 0 {
			duration, e = binary.BigEndian.Uint32(e), e[4:]
		}
		if flags&trunSize != 0 {
			size, e = binary.BigEndian.Uint32(e), e[4:]
		}
		if flags&trunFlags != 0 {
			sampleFlags = binary.BigEndian.Uint32(e)
		}
		samples[i] = fmp4Sample{time: t, off: off, size: int64(size), sync: sampleFlags&nonSyncSample == 0}
		t += uint64(duration)
		off += int64(size)
	}
	return samples, nil
}

// ticks is d in the recording's timescale.
func (s *Segment) ticks(d time.Duration) uint64 {
	return uint64(d.Nanoseconds()) * uint64(s.timescale) / uint64(time.Second) //nolint:gosec // a target inside a recording, never negative
}

// duration is ticks in the recording's timescale as a duration.
func (s *Segment) duration(ticks uint64) time.Duration {
	return time.Duration(ticks * uint64(time.Second) / uint64(s.timescale)) //nolint:gosec // minutes of a recording
}

// idr is where the first IDR at or after a seek's target lies: the part
// that holds it, from its moof to the end of its mdat, and the IDR's own
// bytes, in the plaintext.
type idr struct{ part, sample span }

// firstIDR finds the first sync sample of the video track whose decode
// time from the recording's first sample is target or later, walking the
// moofs from the first part as ipcam-hub's seekbench does.
func (s *Segment) firstIDR(target time.Duration) (idr, error) {
	var at uint64
	first := true
	var found *idr
	err := s.eachPart(func(part span, samples []fmp4Sample) (bool, error) {
		for _, smp := range samples {
			if first {
				at, first = smp.time+s.ticks(target), false
			}
			if smp.sync && smp.time >= at {
				found = &idr{part: part, sample: span{off: smp.off, n: smp.size}}
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return idr{}, err
	}
	if found == nil {
		return idr{}, fmt.Errorf("no IDR at or after %v", target)
	}
	// Checked without an overflow: a corrupt base data offset can put the
	// sample near the largest int64.
	if s, p := found.sample, found.part; s.off < p.off || s.n > p.n || s.off-p.off > p.n-s.n {
		return idr{}, fmt.Errorf("%w: an IDR outside its part", errNotFMP4)
	}
	return *found, nil
}

// lastIDR is the time of the recording's last IDR from its first sample:
// a seek's targets go up to it.
func (s *Segment) lastIDR() (time.Duration, error) {
	var first, last uint64
	seen, sync := false, false
	err := s.eachPart(func(_ span, samples []fmp4Sample) (bool, error) {
		for _, smp := range samples {
			if !seen {
				first, seen = smp.time, true
			}
			if smp.sync {
				last, sync = smp.time, true
			}
		}
		return true, nil
	})
	if err != nil {
		return 0, err
	}
	if !sync {
		return 0, fmt.Errorf("%w: no IDR", errNotFMP4)
	}
	return s.duration(last - first), nil
}
