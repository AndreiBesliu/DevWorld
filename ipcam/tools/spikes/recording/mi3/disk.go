// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package mi3

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
)

// The modes of DiskRatio (R02).
const (
	modeClear     = "clear"
	modeEncrypted = "encrypted"
)

// maxBox bounds a box that the disk measurement reads into memory: a part
// (MediaMTX's recordMaxPartSize is 50 MiB), or the moov.
const maxBox = 64 << 20

// diskFigures are the disk budgets' measurements of a step's recordings
// (§4.8): file bytes ÷ stream bytes per mode, and fsyncs per second per
// open file; and the seconds of parts that each stream recorded, by its
// "<cam>_<profile>" name (mis-2).
type diskFigures struct {
	ratio       map[string]float64
	fsyncPerSec float64
	seconds     map[string]float64
}

// segmentStats is what one segment file holds: its size on disk, the
// stream bytes of its samples, and the length of each of its parts, in
// seconds.
type segmentStats struct {
	size, payload int64
	parts         []float64
}

// measureDisk measures the segments under rec, the step's recordings; an
// .age one is decrypted with id. Disk ÷ bitrate is, per mode, the files'
// bytes over their samples' bytes: what the stream needed. The fsync rate
// is the parts' closes per second of recording: MTX-1 fsyncs the file at
// each part's close, so a file of n parts closed n − 1 of them over its
// first n − 1 parts; its last part, which a segment's close or the stop
// may cut short, is left out. The fsync at a segment's close, one per 5
// min, is not in the rate. It counts closed parts, so it sits near 1 by
// construction and cannot see an fsync in plus: the report names it so
// (Proxies). Each stream's seconds are all of its parts', from the
// recordings' folders, rec/<cam>/<profile>/….
func measureDisk(rec string, id age.Identity) (diskFigures, error) {
	sizes, payloads := map[string]int64{}, map[string]int64{}
	var closes, seconds float64
	recorded := map[string]float64{}
	err := filepath.WalkDir(rec, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || !strings.Contains(d.Name(), ".mp4") {
			return err
		}
		mode := modeClear
		if strings.Contains(d.Name(), ".age") {
			mode = modeEncrypted
		}
		st, err := readSegment(path, id)
		if err != nil {
			return fmt.Errorf("%s: %w", d.Name(), err)
		}
		sizes[mode] += st.size
		payloads[mode] += st.payload
		if rel, err := filepath.Rel(rec, path); err == nil {
			if dirs := strings.Split(filepath.ToSlash(rel), "/"); len(dirs) > 2 {
				for _, p := range st.parts {
					recorded[dirs[0]+"_"+dirs[1]] += p
				}
			}
		}
		if n := len(st.parts); n >= 2 {
			closes += float64(n - 1)
			for _, p := range st.parts[:n-1] {
				seconds += p
			}
		}
		return nil
	})
	if err != nil {
		return diskFigures{}, fmt.Errorf("mi3: the recordings: %w", err)
	}
	f := diskFigures{ratio: map[string]float64{}, seconds: recorded}
	for mode, payload := range payloads {
		if payload > 0 {
			f.ratio[mode] = float64(sizes[mode]) / float64(payload)
		}
	}
	if seconds <= 0 {
		return f, errors.New("mi3: no recording has two parts")
	}
	f.fsyncPerSec = closes / seconds
	return f, nil
}

// readSegment reads the plaintext of the segment at path, decrypted with id
// when it is an .age file, box by box: the moov gives the track's
// timescale, and each moof with its mdat is a part.
func readSegment(path string, id age.Identity) (segmentStats, error) {
	var st segmentStats
	f, err := os.Open(path) //nolint:gosec // a recording of the step
	if err != nil {
		return st, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return st, err
	}
	st.size = info.Size()
	var r io.Reader = bufio.NewReaderSize(f, 1<<20)
	if strings.Contains(filepath.Base(path), ".age") {
		if r, err = age.Decrypt(r, id); err != nil {
			return st, err
		}
	}
	var moof []byte
	var timescale uint32
	for {
		typ, box, err := nextBox(r)
		if errors.Is(err, io.EOF) {
			return st, nil
		}
		if err != nil {
			return st, err
		}
		switch typ {
		case "moov":
			if timescale, err = mdhdTimescale(box); err != nil {
				return st, err
			}
		case "moof":
			moof = box
		case "mdat":
			if moof == nil || timescale == 0 {
				return st, errors.New("an mdat without its moof or the init")
			}
			var parts fmp4.Parts
			if err := parts.Unmarshal(append(moof, box...)); err != nil {
				return st, err
			}
			for _, p := range parts {
				var ticks uint64
				for _, track := range p.Tracks {
					for _, s := range track.Samples {
						st.payload += int64(len(s.Payload))
						ticks += uint64(s.Duration)
					}
				}
				st.parts = append(st.parts, float64(ticks)/float64(timescale))
			}
			moof = nil
		}
	}
}

// mdhdTimescale is the timescale of moov's first track, in its
// moov/trak/mdia/mdhd box. MediaMTX's moov also has a udta box, which
// mediacommon's Init does not read.
func mdhdTimescale(moov []byte) (uint32, error) {
	box := moov
	for _, typ := range []string{"trak", "mdia", "mdhd"} {
		var ok bool
		if box, ok = childBox(boxBody(box), typ); !ok {
			return 0, fmt.Errorf("the init has no %s box", typ)
		}
	}
	mdhd := boxBody(box)
	// version and flags, then the creation and modification times: 4 bytes
	// each in version 0, 8 in version 1.
	at := 12
	if len(mdhd) > 0 && mdhd[0] == 1 {
		at = 20
	}
	if len(mdhd) < at+4 || binary.BigEndian.Uint32(mdhd[at:]) == 0 {
		return 0, errors.New("the init's mdhd has no timescale")
	}
	return binary.BigEndian.Uint32(mdhd[at:]), nil
}

// boxBody is a whole box's content, after its header.
func boxBody(box []byte) []byte {
	if len(box) >= 16 && binary.BigEndian.Uint32(box) == 1 {
		return box[16:]
	}
	if len(box) < 8 {
		return nil
	}
	return box[8:]
}

// childBox is the first box of typ among children, whole.
func childBox(children []byte, typ string) ([]byte, bool) {
	for len(children) >= 8 {
		size, hdr := uint64(binary.BigEndian.Uint32(children)), uint64(8)
		if size == 1 && len(children) >= 16 {
			size, hdr = binary.BigEndian.Uint64(children[8:]), 16
		}
		if size < hdr || size > uint64(len(children)) {
			return nil, false
		}
		if string(children[4:8]) == typ {
			return children[:size], true
		}
		children = children[size:]
	}
	return nil, false
}

// nextBox reads the next top-level box: its type and, for the boxes that
// readSegment reads (moov, moof, mdat), its bytes, header included;
// any other box, as MTX-2's free padding, is skipped. io.EOF ends the file.
func nextBox(r io.Reader) (string, []byte, error) {
	var h [16]byte
	if _, err := io.ReadFull(r, h[:8]); err != nil {
		return "", nil, err
	}
	size, hdr := uint64(binary.BigEndian.Uint32(h[:4])), 8
	if size == 1 {
		if _, err := io.ReadFull(r, h[8:16]); err != nil {
			return "", nil, err
		}
		size, hdr = binary.BigEndian.Uint64(h[8:16]), 16
	}
	typ := string(h[4:8])
	if size < uint64(hdr) {
		return "", nil, fmt.Errorf("a %q box of %d bytes", typ, size)
	}
	switch typ {
	case "moov", "moof", "mdat":
		if size > maxBox {
			return "", nil, fmt.Errorf("a %q box of %d bytes", typ, size)
		}
		box := make([]byte, size)
		copy(box, h[:hdr])
		if _, err := io.ReadFull(r, box[hdr:]); err != nil {
			return "", nil, fmt.Errorf("a cut %q box: %w", typ, err)
		}
		return typ, box, nil
	}
	if _, err := io.CopyN(io.Discard, r, int64(size)-int64(hdr)); err != nil { //nolint:gosec // a box's size
		return "", nil, fmt.Errorf("a cut %q box: %w", typ, err)
	}
	return typ, nil, nil
}

// peakLine is the line of MediaMTX's disk writer at each segment it
// closes, as ipcam-hub forwards it on its stderr (C1-R17, Task 7.2e):
// the most bytes that the recorder of a path held since its last line.
// hub/cmd/ipcam-hub/run_test.go pins the same pattern against the real
// fork (diskQueuePeakPattern), and TestDiskQueuePeak_HubPinsTheSameLine
// keeps the two equal.
var peakLine = regexp.MustCompile(`^mediamtx: \S+ \S+ INF \[path ([^\]]+)\] \[recorder\] disk queue peak: (\d+) bytes$`)

// diskQueuePeak reads ipcam-hub's log and returns the sum, over the
// recorders (paths), of each one's largest peak: an upper bound of the
// disk writers' memory, since the maxima may come at different times. A
// killed MediaMTX logs no line for its last segment; a step stops it
// cleanly.
func diskQueuePeak(log io.Reader) (int64, error) {
	peaks := map[string]int64{}
	sc := bufio.NewScanner(log)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		m := peakLine.FindStringSubmatch(strings.TrimSuffix(sc.Text(), "\r"))
		if m == nil {
			continue
		}
		n, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("mi3: a disk queue peak of %s bytes: %w", m[2], err)
		}
		peaks[m[1]] = max(peaks[m[1]], n)
	}
	var sum int64
	for _, p := range peaks {
		sum += p
	}
	return sum, sc.Err()
}
