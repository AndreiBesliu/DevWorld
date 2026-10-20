package recorder

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// The fMP4 recordings that MediaMTX writes (internal/recorder/format_fmp4_*.go
// in the fork) are an init, an ftyp and a moov, then one moof and one mdat per
// part; MTX-2 adds free boxes after a part, as padding, and at the close.
// walk reads that layout from a recording's plaintext without trusting it:
// a cut leaves the last box torn, and a filesystem may leave junk or zeros
// after the last durable byte.

// errNotRecording is a file that does not start as an fMP4 recording does,
// which reconcile leaves alone and verification counts as unreadable.
var errNotRecording = errors.New("not an fMP4 recording")

const (
	maxInitBox = 1 << 20  // an ftyp or moov larger than this is not MediaMTX's
	maxMoofBox = 16 << 20 // MediaMTX's moof of a part is a few KB
	maxPart    = 64 << 20 // MediaMTX's recordMaxPartSize is 50 MiB
)

// box is an ISO BMFF box header.
type box struct {
	off, size int64 // where the box starts, its whole size
	hdr       int64 // 8, or 16 with a 64-bit size
	typ       string
}

func (b box) end() int64 { return b.off + b.size }

// readBox reads the header of the box at off in data of size bytes. ok is
// false when no box header fits before size, or the header is not one: a
// size of 0 (to the end of the file, which MediaMTX never writes) or one
// smaller than the header.
func readBox(r io.ReaderAt, off, size int64) (b box, ok bool, err error) {
	if size-off < 8 {
		return box{}, false, nil
	}
	var h [16]byte
	if err := readAt(r, h[:8], off); err != nil {
		return box{}, false, err
	}
	b = box{off: off, size: int64(binary.BigEndian.Uint32(h[:4])), hdr: 8, typ: string(h[4:8])}
	if b.size == 1 {
		if size-off < 16 {
			return box{}, false, nil
		}
		if err := readAt(r, h[8:16], off+8); err != nil {
			return box{}, false, err
		}
		b.size, b.hdr = int64(binary.BigEndian.Uint64(h[8:16])), 16 //nolint:gosec // checked below
	}
	if b.size < b.hdr {
		return box{}, false, nil
	}
	return b, true, nil
}

// readAt reads len(p) bytes at off.
func readAt(r io.ReaderAt, p []byte, off int64) error {
	n, err := r.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return err
}

// trackFrag is a traf of a moof: one track's samples in a part.
type trackFrag struct {
	trackID   uint32
	baseTime  uint64
	trunOff   int64 // where its trun box starts (a trun with an 8-byte header)
	trunSize  int64
	count     int64 // samples
	entrySize int64 // bytes of each sample's entry in the trun
	dataOff   int64 // where the first sample's data is; -1 when the trun gives no data_offset
	durations []uint32
	sizes     []uint32
}

// part is a moof and its mdat.
type part struct {
	moof, mdat box
	frags      []trackFrag
}

// recording is what walk found in a recording.
type recording struct {
	// initEnd is the end of the moov; 0 when the init is not whole, which
	// leaves no sample.
	initEnd    int64
	mvhdDurOff int64 // where the mvhd's duration is
	mvhdDurLen int   // 4 or 8 bytes
	mvhdScale  uint32
	timescales map[uint32]uint32 // track ID → timescale
	parts      []part            // the complete parts
	end        int64             // the end of the last complete box
	torn       *part             // a part whose moof is whole and whose mdat is cut
	tail       bool              // bytes after end that are no complete box
}

// walk reads the layout of the size bytes of plaintext in r. It returns
// errNotRecording for data that is not an fMP4 recording at all.
func walk(r io.ReaderAt, size int64) (*recording, error) {
	rec := &recording{timescales: map[uint32]uint32{}}
	ftyp, ok, err := readBox(r, 0, size)
	if err != nil {
		return nil, err
	}
	if !ok && size < 8 {
		return rec, nil // nothing durable yet
	}
	if !ok || ftyp.typ != "ftyp" || ftyp.size > maxInitBox {
		return nil, errNotRecording
	}
	moov, ok, err := readBox(r, ftyp.end(), size)
	if err != nil {
		return nil, err
	}
	if !ok && size-ftyp.end() >= 8 {
		return nil, errNotRecording // no box header after the ftyp
	}
	if ftyp.end() > size || !ok || moov.end() > size {
		return rec, nil // the init itself is torn
	}
	if moov.typ != "moov" || moov.size > maxInitBox {
		return nil, errNotRecording
	}
	if err := rec.readMoov(r, moov); err != nil {
		return nil, err
	}
	rec.initEnd, rec.end = moov.end(), moov.end()
	for pos := rec.end; pos < size; {
		b, ok, err := readBox(r, pos, size)
		if err != nil {
			return nil, err
		}
		if !ok || b.end() > size && b.typ != "moof" {
			rec.tail = true
			return rec, nil
		}
		switch b.typ {
		case "free", "skip":
		case "moof":
			if b.end() > size || b.size > maxMoofBox {
				rec.tail = true
				return rec, nil
			}
			p := part{moof: b}
			mdat, ok, err := readBox(r, b.end(), size)
			if err != nil {
				return nil, err
			}
			if !ok || mdat.typ != "mdat" {
				rec.tail = true // its mdat header is torn, or what follows is junk
				return rec, nil
			}
			p.mdat = mdat
			if p.frags, err = readMoof(r, b); err != nil {
				return nil, err
			}
			if mdat.end() > size {
				rec.torn = &p
				return rec, nil
			}
			if mdat.end()-b.off > maxPart {
				return nil, fmt.Errorf("a part of %d bytes is larger than MediaMTX writes", mdat.end()-b.off)
			}
			rec.parts = append(rec.parts, p)
			b = mdat
		default:
			rec.tail = true
			return rec, nil
		}
		pos = b.end()
		rec.end = pos
	}
	return rec, nil
}

// children calls f for each box in data[from:to], data being the bytes of a
// box at off in the file.
func children(data []byte, off int64, from, to int, f func(b box, body []byte) error) error {
	for pos := from; pos < to; {
		if to-pos < 8 {
			return errNotRecording
		}
		size := int(binary.BigEndian.Uint32(data[pos:]))
		if size < 8 || pos+size > to {
			return errNotRecording
		}
		if err := f(box{off: off + int64(pos), size: int64(size), hdr: 8, typ: string(data[pos+4 : pos+8])}, data[pos+8:pos+size]); err != nil {
			return err
		}
		pos += size
	}
	return nil
}

// readMoov finds the mvhd's duration and the timescale of each track.
func (rec *recording) readMoov(r io.ReaderAt, moov box) error {
	data := make([]byte, moov.size)
	if err := readAt(r, data, moov.off); err != nil {
		return err
	}
	err := children(data, moov.off, int(moov.hdr), len(data), func(b box, body []byte) error {
		switch b.typ {
		case "mvhd":
			// version, flags, then creation and modification times, the
			// timescale and the duration: 4 bytes each, or 8 in version 1.
			if len(body) < 20 {
				return errNotRecording
			}
			if body[0] == 1 {
				if len(body) < 32 {
					return errNotRecording
				}
				rec.mvhdScale = binary.BigEndian.Uint32(body[20:])
				rec.mvhdDurOff, rec.mvhdDurLen = b.off+8+24, 8
			} else {
				rec.mvhdScale = binary.BigEndian.Uint32(body[12:])
				rec.mvhdDurOff, rec.mvhdDurLen = b.off+8+16, 4
			}
		case "trak":
			id, scale, err := readTrak(body, b.off)
			if err != nil {
				return err
			}
			rec.timescales[id] = scale
		}
		return nil
	})
	if err != nil {
		return err
	}
	if rec.mvhdDurLen == 0 || rec.mvhdScale == 0 || len(rec.timescales) == 0 {
		return errNotRecording
	}
	return nil
}

// readTrak returns the track ID from the tkhd and the timescale from the
// mdhd of a trak whose payload is body.
func readTrak(body []byte, off int64) (id, scale uint32, err error) {
	err = children(body, off+8, 0, len(body), func(b box, payload []byte) error {
		switch b.typ {
		case "tkhd":
			// creation and modification times, then the track ID.
			at := 12
			if len(payload) > 0 && payload[0] == 1 {
				at = 20
			}
			if len(payload) < at+4 {
				return errNotRecording
			}
			id = binary.BigEndian.Uint32(payload[at:])
		case "mdia":
			return children(payload, b.off+8, 0, len(payload), func(c box, p []byte) error {
				if c.typ != "mdhd" {
					return nil
				}
				at := 12
				if len(p) > 0 && p[0] == 1 {
					at = 20
				}
				if len(p) < at+4 {
					return errNotRecording
				}
				scale = binary.BigEndian.Uint32(p[at:])
				return nil
			})
		}
		return nil
	})
	if err == nil && (id == 0 || scale == 0) {
		err = errNotRecording
	}
	return id, scale, err
}

// The trun flags and the tfhd flags that walk reads.
const (
	trunDataOffset     = 0x001
	trunFirstFlags     = 0x004
	trunDuration       = 0x100
	trunSize           = 0x200
	trunFlags          = 0x400
	trunCTO            = 0x800
	tfhdBaseDataOffset = 0x01
	tfhdDescription    = 0x02
	tfhdDuration       = 0x08
	tfhdSize           = 0x10
	tfhdSampleFlags    = 0x20
)

// readMoof reads the trafs of a whole moof.
func readMoof(r io.ReaderAt, moof box) ([]trackFrag, error) {
	data := make([]byte, moof.size)
	if err := readAt(r, data, moof.off); err != nil {
		return nil, err
	}
	var frags []trackFrag
	err := children(data, moof.off, int(moof.hdr), len(data), func(b box, body []byte) error {
		if b.typ != "traf" {
			return nil
		}
		f, err := readTraf(body, b.off, moof.off)
		if err != nil {
			return err
		}
		frags = append(frags, f)
		return nil
	})
	return frags, err
}

func readTraf(body []byte, off, moofOff int64) (trackFrag, error) {
	f := trackFrag{dataOff: -1}
	var defaultDuration, defaultSize uint32
	base := moofOff
	var haveTrun bool
	err := children(body, off+8, 0, len(body), func(b box, p []byte) error {
		switch b.typ {
		case "tfhd":
			if len(p) < 8 {
				return errNotRecording
			}
			flags := uint32(p[1])<<16 | uint32(p[2])<<8 | uint32(p[3])
			f.trackID = binary.BigEndian.Uint32(p[4:])
			at := 8
			field := func(n int) ([]byte, error) {
				if len(p) < at+n {
					return nil, errNotRecording
				}
				v := p[at : at+n]
				at += n
				return v, nil
			}
			for _, opt := range []struct {
				flag uint32
				n    int
				set  func([]byte)
			}{
				{tfhdBaseDataOffset, 8, func(v []byte) { base = int64(binary.BigEndian.Uint64(v)) }}, //nolint:gosec // an offset in the file
				{tfhdDescription, 4, func([]byte) {}},
				{tfhdDuration, 4, func(v []byte) { defaultDuration = binary.BigEndian.Uint32(v) }},
				{tfhdSize, 4, func(v []byte) { defaultSize = binary.BigEndian.Uint32(v) }},
				{tfhdSampleFlags, 4, func([]byte) {}},
			} {
				if flags&opt.flag != 0 {
					v, err := field(opt.n)
					if err != nil {
						return err
					}
					opt.set(v)
				}
			}
		case "tfdt":
			if len(p) < 8 {
				return errNotRecording
			}
			if p[0] == 1 {
				if len(p) < 12 {
					return errNotRecording
				}
				f.baseTime = binary.BigEndian.Uint64(p[4:])
			} else {
				f.baseTime = uint64(binary.BigEndian.Uint32(p[4:]))
			}
		case "trun":
			if haveTrun {
				return errors.New("a traf with two truns, which MediaMTX never writes")
			}
			haveTrun = true
			return f.readTrun(b, p, base, defaultDuration, defaultSize)
		}
		return nil
	})
	if err == nil && !haveTrun {
		err = errNotRecording
	}
	return f, err
}

func (f *trackFrag) readTrun(b box, p []byte, base int64, defaultDuration, defaultSize uint32) error {
	if len(p) < 8 {
		return errNotRecording
	}
	flags := uint32(p[1])<<16 | uint32(p[2])<<8 | uint32(p[3])
	count := int64(binary.BigEndian.Uint32(p[4:]))
	at := 8
	if flags&trunDataOffset != 0 {
		if len(p) < at+4 {
			return errNotRecording
		}
		f.dataOff = base + int64(int32(binary.BigEndian.Uint32(p[at:]))) //nolint:gosec // a signed offset by the spec
		at += 4
	}
	if flags&trunFirstFlags != 0 {
		at += 4
	}
	for _, flag := range []uint32{trunDuration, trunSize, trunFlags, trunCTO} {
		if flags&flag != 0 {
			f.entrySize += 4
		}
	}
	if int64(len(p)) < int64(at)+count*f.entrySize {
		return errNotRecording
	}
	f.trunOff, f.trunSize, f.count = b.off, b.size, count
	f.durations, f.sizes = make([]uint32, count), make([]uint32, count)
	for i := range count {
		e := p[int64(at)+i*f.entrySize:]
		f.durations[i], f.sizes[i] = defaultDuration, defaultSize
		if flags&trunDuration != 0 {
			f.durations[i], e = binary.BigEndian.Uint32(e), e[4:]
		}
		if flags&trunSize != 0 {
			f.sizes[i] = binary.BigEndian.Uint32(e)
		}
	}
	return nil
}

// endTime is the end of a fragment's first n samples, in its track's units.
func (f trackFrag) endTime(n int64) uint64 {
	end := f.baseTime
	for _, d := range f.durations[:n] {
		end += uint64(d)
	}
	return end
}

// durationMs is the duration of the complete parts, and of the first
// kept[i] samples of frag i of the torn part when kept is not nil, in ms:
// the latest end of any track (MediaMTX's own mvhd duration is the same).
func (rec *recording) durationMs(kept []int64) int64 {
	var ms int64
	end := func(f trackFrag, n int64) {
		if scale := rec.timescales[f.trackID]; scale != 0 {
			ms = max(ms, int64(f.endTime(n)*1000/uint64(scale))) //nolint:gosec // milliseconds of a recording
		}
	}
	for _, p := range rec.parts {
		for _, f := range p.frags {
			end(f, f.count)
		}
	}
	if rec.torn != nil && kept != nil {
		for i, f := range rec.torn.frags {
			end(f, kept[i])
		}
	}
	return ms
}

// salvage is what reconcile keeps of a recording (§4.5, R01): every
// complete part, plus the whole samples of a torn last part, whose moof
// sample counts and mdat size are rewritten in place so that the part is
// complete.
type salvage struct {
	keep       int64          // the plaintext bytes to keep
	patches    []crypto.Patch // the in-place changes inside [0, keep)
	durationMs int64          // of what is kept
	// partEnd is the end of the last complete part (or free box after it),
	// and partEndMs its duration: the interim state of C1-R9.
	partEnd, partEndMs int64
	cut                bool // something after keep is lost
	empty              bool // no sample is kept
}

// planSalvage plans the salvage of rec, whose durable plaintext is size
// bytes long. It changes nothing.
func (rec *recording) planSalvage(size int64) salvage {
	s := salvage{keep: rec.end, partEnd: rec.end, durationMs: rec.durationMs(nil)}
	s.partEndMs = s.durationMs
	s.cut = rec.tail || rec.torn != nil || rec.end < size
	if rec.initEnd == 0 {
		s.keep, s.partEnd, s.cut, s.empty = 0, 0, true, true
		return s
	}
	if p := rec.torn; p != nil {
		kept := make([]int64, len(p.frags))
		dataEnd := int64(-1)
		for i, f := range p.frags {
			if f.dataOff < p.mdat.off+p.mdat.hdr {
				continue // its samples are not in the mdat: none is kept
			}
			pos := f.dataOff
			for n := range f.count {
				pos += int64(f.sizes[n])
				if pos > size || pos > p.mdat.end() {
					break
				}
				kept[i] = n + 1
				dataEnd = max(dataEnd, pos)
			}
		}
		if dataEnd > 0 {
			s.keep = dataEnd
			s.durationMs = rec.durationMs(kept)
			for i, f := range p.frags {
				s.patches = append(s.patches, f.cutTo(kept[i])...)
			}
			if p.mdat.hdr == 16 { // the 64-bit largesize
				field := binary.BigEndian.AppendUint64(nil, uint64(dataEnd-p.mdat.off)) //nolint:gosec // a size in the file
				s.patches = append(s.patches, crypto.Patch{Off: p.mdat.off + 8, Data: field})
			} else {
				field := binary.BigEndian.AppendUint32(nil, uint32(dataEnd-p.mdat.off)) //nolint:gosec // a part is far below 4 GiB
				s.patches = append(s.patches, crypto.Patch{Off: p.mdat.off, Data: field})
			}
		}
	}
	s.empty = s.keep <= rec.initEnd
	return s
}

// cutTo returns the patches that leave the first n samples in f's trun:
// the sample count, and, when that drops whole entries, a trun shorter by
// them followed by a free box over them. The moof keeps its size, so the
// data offsets stay right.
func (f trackFrag) cutTo(n int64) []crypto.Patch {
	if n == f.count {
		return nil
	}
	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, uint32(n)) //nolint:gosec // n is at most the old count
	patches := []crypto.Patch{{Off: f.trunOff + 12, Data: count}}
	gap := (f.count - n) * f.entrySize
	if gap < 8 {
		return patches // too short for a free box: the entries stay after the count
	}
	trunSize := make([]byte, 4)
	binary.BigEndian.PutUint32(trunSize, uint32(f.trunSize-gap)) //nolint:gosec // smaller than the old size
	free := make([]byte, 8)
	binary.BigEndian.PutUint32(free, uint32(gap)) //nolint:gosec // smaller than the old size
	copy(free[4:], "free")
	return append(patches, crypto.Patch{Off: f.trunOff, Data: trunSize}, crypto.Patch{Off: f.trunOff + f.trunSize - gap, Data: free})
}

// mvhdPatch writes durationMs into the mvhd of a clear recording.
func (rec *recording) mvhdPatch(durationMs int64) crypto.Patch {
	v := uint64(durationMs) * uint64(rec.mvhdScale) / 1000 //nolint:gosec // a duration in ms
	b := make([]byte, rec.mvhdDurLen)
	if rec.mvhdDurLen == 8 {
		binary.BigEndian.PutUint64(b, v)
	} else {
		binary.BigEndian.PutUint32(b, uint32(min(v, 0xffffffff)))
	}
	return crypto.Patch{Off: rec.mvhdDurOff, Data: b}
}
