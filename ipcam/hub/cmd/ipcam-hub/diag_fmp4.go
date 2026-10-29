package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// The diag benches read a recording as the hub's read path does: its
// plaintext through crypto's random-access reader, and in it the fMP4 that
// MediaMTX writes (§4.2): an init, an ftyp and a moov, then one moof and one
// mdat per part, with free boxes as padding. A part's moof indexes the
// samples of its mdat; a seek ends at the first sync sample (an IDR) of the
// video track at or after its target.

// span is a byte range of the plaintext.
type span struct{ off, n int64 }

// errNotFMP4 is a recording that is not laid out as MediaMTX writes one.
var errNotFMP4 = errors.New("the plaintext is not an fMP4 recording")

// fmp4Box is an ISO BMFF box header.
type fmp4Box struct {
	off, size, hdr int64
	typ            string
}

func (b fmp4Box) end() int64 { return b.off + b.size }

// readFMP4Box reads the header of the box at off, in the size bytes of r.
func readFMP4Box(r io.ReaderAt, off, size int64) (fmp4Box, error) {
	var h [16]byte
	if size-off < 8 {
		return fmp4Box{}, fmt.Errorf("%w: a box header past the end, at %d", errNotFMP4, off)
	}
	if err := readFullAt(r, h[:8], off); err != nil {
		return fmp4Box{}, err
	}
	b := fmp4Box{off: off, size: int64(binary.BigEndian.Uint32(h[:4])), hdr: 8, typ: string(h[4:8])}
	if b.size == 1 {
		if size-off < 16 {
			return fmp4Box{}, fmt.Errorf("%w: a box header past the end, at %d", errNotFMP4, off)
		}
		if err := readFullAt(r, h[8:16], off+8); err != nil {
			return fmp4Box{}, err
		}
		b.size, b.hdr = int64(binary.BigEndian.Uint64(h[8:16])), 16 //nolint:gosec // checked below
	}
	if b.size < b.hdr || b.end() > size {
		return fmp4Box{}, fmt.Errorf("%w: a %q box of %d bytes at %d", errNotFMP4, b.typ, b.size, off)
	}
	return b, nil
}

// readFMP4Body reads a whole box, without its header.
func readFMP4Body(r io.ReaderAt, b fmp4Box) ([]byte, error) {
	const maxBody = 16 << 20 // a moov or a moof is a few KB
	if b.size-b.hdr > maxBody {
		return nil, fmt.Errorf("%w: a %q box of %d bytes", errNotFMP4, b.typ, b.size)
	}
	body := make([]byte, b.size-b.hdr)
	if err := readFullAt(r, body, b.off+b.hdr); err != nil {
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

// fmp4Recording is the init of a recording: its video track, and where its
// parts start.
type fmp4Recording struct {
	r          io.ReaderAt
	size       int64
	video      uint32 // the track ID
	timescale  uint32
	defaults   sampleDefaults
	partsStart int64
}

// openFMP4 reads the init of the recording whose plaintext is the size
// bytes of r.
func openFMP4(r io.ReaderAt, size int64) (*fmp4Recording, error) {
	ftyp, err := readFMP4Box(r, 0, size)
	if err != nil {
		return nil, err
	}
	moov, err := readFMP4Box(r, ftyp.end(), size)
	if err != nil {
		return nil, err
	}
	if ftyp.typ != "ftyp" || moov.typ != "moov" {
		return nil, fmt.Errorf("%w: it starts with %q and %q", errNotFMP4, ftyp.typ, moov.typ)
	}
	body, err := readFMP4Body(r, moov)
	if err != nil {
		return nil, err
	}
	m := &fmp4Recording{r: r, size: size, partsStart: moov.end()}
	trex := map[uint32]sampleDefaults{}
	err = eachChild(body, func(typ string, b []byte) error {
		switch typ {
		case "trak":
			id, scale, video, err := readTrak(b)
			if err != nil {
				return err
			}
			if video && m.video == 0 {
				m.video, m.timescale = id, scale
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
		return nil, err
	}
	if m.video == 0 || m.timescale == 0 {
		return nil, fmt.Errorf("%w: no video track", errNotFMP4)
	}
	m.defaults = trex[m.video]
	return m, nil
}

// readTrak returns a trak's track ID (tkhd), timescale (mdhd) and whether
// it is a video track (hdlr "vide").
func readTrak(trak []byte) (id, scale uint32, video bool, err error) {
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
	err = eachChild(trak, func(typ string, b []byte) error {
		var err error
		switch typ {
		case "tkhd":
			id, err = field(b)
		case "mdia":
			err = eachChild(b, func(typ string, b []byte) error {
				var err error
				switch typ {
				case "mdhd":
					scale, err = field(b)
				case "hdlr": // version and flags, pre_defined, then the handler type
					if len(b) < 12 {
						return errNotFMP4
					}
					video = string(b[8:12]) == "vide"
				}
				return err
			})
		}
		return err
	})
	return id, scale, video, err
}

// The tfhd and trun flags that the benches read.
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

// eachPart calls visit with each part of the recording, in order: its span,
// from its moof to the end of its mdat, and the video track's samples in
// it. It stops when visit returns false.
func (m *fmp4Recording) eachPart(visit func(part span, samples []fmp4Sample) (bool, error)) error {
	for pos := m.partsStart; pos < m.size; {
		b, err := readFMP4Box(m.r, pos, m.size)
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
		mdat, err := readFMP4Box(m.r, b.end(), m.size)
		if err != nil {
			return err
		}
		if mdat.typ != "mdat" {
			return fmt.Errorf("%w: a moof without its mdat, at %d", errNotFMP4, b.off)
		}
		pos = mdat.end()
		body, err := readFMP4Body(m.r, b)
		if err != nil {
			return err
		}
		var samples []fmp4Sample
		err = eachChild(body, func(typ string, traf []byte) error {
			if typ != "traf" || samples != nil {
				return nil
			}
			s, err := m.readTraf(traf, b.off)
			samples = s
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
// nil; moof is where its moof starts.
func (m *fmp4Recording) readTraf(traf []byte, moof int64) ([]fmp4Sample, error) {
	d := m.defaults
	base := moof
	var start uint64
	var samples []fmp4Sample
	video := false
	err := eachChild(traf, func(typ string, b []byte) error {
		switch typ {
		case "tfhd":
			if len(b) < 8 {
				return errNotFMP4
			}
			flags := binary.BigEndian.Uint32(b) & 0xffffff
			video = binary.BigEndian.Uint32(b[4:]) == m.video
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
// data at base plus the trun's data offset.
func readTrun(b []byte, base int64, start uint64, d sampleDefaults) ([]fmp4Sample, error) {
	if len(b) < 8 {
		return nil, errNotFMP4
	}
	flags := binary.BigEndian.Uint32(b) & 0xffffff
	count := int(binary.BigEndian.Uint32(b[4:]))
	at := 8
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
	if count < 0 || len(b) < at+count*entry {
		return nil, errNotFMP4
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
func (m *fmp4Recording) ticks(d time.Duration) uint64 {
	return uint64(d.Nanoseconds()) * uint64(m.timescale) / uint64(time.Second) //nolint:gosec // a target inside a recording, never negative
}

// duration is ticks in the recording's timescale as a duration.
func (m *fmp4Recording) duration(ticks uint64) time.Duration {
	return time.Duration(ticks * uint64(time.Second) / uint64(m.timescale)) //nolint:gosec // minutes of a recording
}

// firstIDR is the plaintext bytes of the first sync sample of the video
// track at or after target, counted from the recording's first sample: it
// reads the recording's init, then each part's moof until that sample's
// part, then the sample, all from r, whose size bytes are the plaintext.
func firstIDR(r io.ReaderAt, size int64, target time.Duration) ([]byte, error) {
	m, err := openFMP4(r, size)
	if err != nil {
		return nil, err
	}
	var at uint64
	first := true
	var found *fmp4Sample
	err = m.eachPart(func(_ span, samples []fmp4Sample) (bool, error) {
		for i, s := range samples {
			if first {
				at, first = s.time+m.ticks(target), false
			}
			if s.sync && s.time >= at {
				found = &samples[i]
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("no IDR at or after %v", target)
	}
	if found.off < 0 || found.off+found.size > size {
		return nil, fmt.Errorf("%w: a sample outside the plaintext", errNotFMP4)
	}
	data := make([]byte, found.size)
	if err := readFullAt(r, data, found.off); err != nil {
		return nil, err
	}
	return data, nil
}
