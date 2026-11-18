package recorder

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	amp4 "github.com/abema/go-mp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/google/uuid"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/logger"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/recordstore"
)

func writeInit(
	f io.Writer,
	streamID uuid.UUID,
	segmentNumber uint64,
	dts time.Duration,
	ntp time.Time,
	tracks []*formatFMP4Track,
) error {
	fmp4Tracks := make([]*fmp4.InitTrack, len(tracks))
	for i, track := range tracks {
		fmp4Tracks[i] = track.initTrack
	}

	init := fmp4.Init{
		Tracks: fmp4Tracks,
		UserData: []amp4.IBox{
			&recordstore.Mtxi{
				FullBox: amp4.FullBox{
					Version: 0,
				},
				StreamID:      streamID,
				SegmentNumber: segmentNumber,
				DTS:           int64(dts),
				NTP:           ntp.UnixNano(),
			},
		},
	}

	var buf seekablebuffer.Buffer
	err := init.Marshal(&buf)
	if err != nil {
		return err
	}

	_, err = f.Write(buf.Bytes())
	return err
}

func writeDuration(f io.ReadWriteSeeker, d time.Duration) error {
	_, err := f.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	// check and skip ftyp header and content

	buf := make([]byte, 8)
	_, err = io.ReadFull(f, buf)
	if err != nil {
		return err
	}

	if !bytes.Equal(buf[4:], []byte{'f', 't', 'y', 'p'}) {
		return fmt.Errorf("ftyp box not found")
	}

	ftypSize := uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])

	_, err = f.Seek(int64(ftypSize), io.SeekStart)
	if err != nil {
		return err
	}

	// check and skip moov header

	_, err = io.ReadFull(f, buf)
	if err != nil {
		return err
	}

	if !bytes.Equal(buf[4:], []byte{'m', 'o', 'o', 'v'}) {
		return fmt.Errorf("moov box not found")
	}

	moovSize := uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])

	moovPos, err := f.Seek(8, io.SeekCurrent)
	if err != nil {
		return err
	}

	var mvhd amp4.Mvhd
	_, err = amp4.Unmarshal(f, uint64(moovSize-8), &mvhd, amp4.Context{})
	if err != nil {
		return err
	}

	mvhd.DurationV0 = uint32(d / time.Millisecond)

	_, err = f.Seek(moovPos, io.SeekStart)
	if err != nil {
		return err
	}

	_, err = amp4.Marshal(f, &mvhd, amp4.Context{})
	if err != nil {
		return err
	}

	return nil
}

type formatFMP4Segment struct {
	f        *formatFMP4
	startDTS time.Duration
	startNTP time.Time
	number   uint64

	path           string
	curPart        *formatFMP4Part
	endDTS         time.Duration
	nextPartNumber uint32

	// MTX-1: what the recording has handed to the recorder's disk writer
	// (diskwriter.go): the init, with the first part, and the close.
	initSent  bool
	closeSent bool

	// MTX-1: the segment's file and what writes to it, which only the disk
	// writer touches.
	fi segmentFile
	// MTX-2: the writer of an encrypted segment's plaintext, nil for a clear
	// segment, and where the segment's fMP4 bytes go: age, or fi itself.
	age *ageSegmentWriter
	w   io.Writer
	// MTX-1: the first failed write or sync of the segment, which then keeps
	// its .part name.
	partErr error
	// MTX-1: the directory sync of the creation and the parts' fsyncs, beside
	// the writes.
	syncer *partSyncer
}

func (s *formatFMP4Segment) initialize() {
	s.endDTS = s.startDTS
}

// close ends the segment: it hands the last part and the close to the disk
// writer, which closes the segment's file in MTX-1's order (finish), and
// returns the first failure of the disk so far. It does not wait for the
// disk: formatFMP4.close does, when the recorder stops. It runs once.
func (s *formatFMP4Segment) close() error {
	if s.closeSent {
		return nil
	}
	s.closeSent = true

	var err error
	if s.curPart != nil {
		err = s.closeCurPart()
	}
	err2 := s.f.writer().send(diskOp{seg: s, close: true, duration: s.endDTS - s.startDTS})
	if err == nil {
		err = err2
	}
	return err
}

// closeCurPart marshals the current part, with the segment's init when it is
// the first, and hands it to the disk writer. It returns a marshalling error,
// or else the first failure of the disk so far.
func (s *formatFMP4Segment) closeCurPart() error {
	op := diskOp{seg: s}
	if !s.initSent {
		s.initSent = true
		// MTX-4: the name is the start in UTC, one instant also in the hour that
		// repeats when daylight saving time ends.
		s.path = recordstore.Path{Start: s.startNTP.UTC()}.Encode(s.f.ri.pathFormat2)
		if s.f.ri.ageScope != nil {
			// MTX-2: an encrypted segment is <name>.mp4.age.
			s.path += ageSuffix
		}
		var init bytes.Buffer
		op.err = writeInit(
			&init,
			s.f.ri.streamID,
			s.number,
			s.startDTS,
			s.startNTP,
			s.f.tracks)
		op.init = init.Bytes()
	}

	var part bytes.Buffer
	if err := s.curPart.close(&part); err != nil && op.err == nil {
		op.err = err
	}
	op.part = part.Bytes()

	err := s.f.writer().send(op)
	if op.err != nil {
		return op.err
	}
	return err
}

// writePartToDisk writes a part of the segment, creating the segment's file
// with the first one, and asks for the part's fsync. It runs on the disk
// writer. A segment that failed gets nothing more.
func (s *formatFMP4Segment) writePartToDisk(op diskOp) {
	if op.err != nil {
		s.fail(op.err)
		return
	}
	if op.init != nil {
		if err := s.create(op.init); err != nil {
			s.fail(err)
			return
		}
	}
	if s.fi == nil || s.partErr != nil {
		return
	}
	if err := s.syncer.failed(); err != nil {
		s.fail(err)
		return
	}

	var partStart int64
	if s.age != nil {
		partStart = s.age.size()
	}
	_, err := s.w.Write(op.part)
	if err == nil && s.age != nil {
		// MTX-2: pad the part when §4.5's rule says so, and seal its complete
		// chunks before the fsync.
		err = s.age.endPart(partStart)
	}
	if err != nil {
		s.fail(err)
		return
	}

	// MTX-1: every part's fsync is asked for once the part is written, before
	// the next part's first write, and runs on the segment's syncer.
	syncAsked(s.fi)
	s.syncer.askFileSync(s.fi)
}

// create creates the segment's file under its .part name and writes init
// into it. It runs on the disk writer, once the segment before is closed.
func (s *formatFMP4Segment) create(init []byte) error {
	s.f.ri.Log(logger.Debug, "creating segment %s", s.path)

	err := os.MkdirAll(filepath.Dir(s.path), 0o755)
	if err != nil {
		return err
	}

	// MTX-1: the segment is written under a .part name until it is complete.
	fi, err := createFile(s.path + partSuffix)
	if err != nil {
		return err
	}

	// MTX-1: make the new name durable, on the segment's syncer, ahead of every
	// part's fsync.
	syncer := newPartSyncer(s.f.disk.fail)
	syncer.askDirSync(filepath.Dir(s.path))

	s.f.ri.onSegmentCreate(fi.Name())

	// MTX-2: every byte of an encrypted segment goes through the age
	// writer, which writes the age header first. No clear byte reaches
	// the file (HUB-04).
	var w io.Writer = fi
	var age *ageSegmentWriter
	if scope := s.f.ri.ageScope; scope != nil {
		age, err = newAgeSegmentWriter(fi, *scope, s.f.ri.agePadLowBitrate)
		if err != nil {
			syncer.wait() //nolint:errcheck
			fi.Close()
			return err
		}
		w = age
	}

	_, err = w.Write(init)
	if err != nil {
		syncer.wait() //nolint:errcheck
		fi.Close()
		return err
	}

	s.fi = fi
	s.age = age
	s.w = w
	s.syncer = syncer
	return nil
}

// fail notes the segment's first failure, which keeps its .part name, and
// tells the disk writer. It runs on the disk writer.
func (s *formatFMP4Segment) fail(err error) {
	if s.partErr == nil {
		s.partErr = err
	}
	s.f.disk.fail(err)
}

// finish closes the segment's file, after its last part, in MTX-1's order:
// the syncer's last sync, the last write, one more fsync, Close, the rename
// and the directory sync, then MTX-3's hook; and it logs the disk writer's
// peak (C1-R17). It runs on the disk writer, which creates the next
// segment's file only after it.
func (s *formatFMP4Segment) finish(duration time.Duration) {
	if s.fi == nil {
		return
	}
	s.f.ri.Log(logger.Debug, "closing segment %s", s.path)

	// MTX-1: every sync of the segment has run before the close's last
	// writes, and a part that failed earlier, or a sync, is a failure of the
	// close.
	err := s.partErr
	syncErr := s.syncer.wait()
	if err == nil {
		err = syncErr
	}

	var err2 error
	if s.age != nil {
		// MTX-2: an encrypted segment is written strictly in order, so its
		// mvhd keeps no duration (C13): the hub's catalog has it. The last
		// write is the final age chunk.
		err2 = s.age.close()
	} else {
		// write overall duration in the header to speed up the playback server
		err2 = writeDuration(s.fi, duration)
	}
	if err == nil {
		err = err2
	}

	// MTX-1: the last write is durable before the file gets its final name.
	if err == nil {
		err = syncFile(s.fi)
	}

	err2 = s.fi.Close()
	if err == nil {
		err = err2
	}

	// MTX-1: only a segment whose every write and sync succeeded gets its
	// final name, and the hook (MTX-3) runs only once that name is durable.
	// Otherwise the .part stays for the hub's reconcile.
	if err == nil {
		err = renameFinal(s.fi.Name(), s.path)
		if err == nil {
			err = syncDir(filepath.Dir(s.path))
		}
	}

	if err == nil {
		s.f.ri.onSegmentComplete(s.path, s.startNTP, duration)
	} else {
		s.f.disk.fail(err)
	}
	s.f.disk.logPeak()
}

func (s *formatFMP4Segment) write(track *formatFMP4Track, sample *formatFMP4Sample, dts time.Duration) error {
	endDTS := dts + timestampToDuration(int64(sample.Duration), int(track.initTrack.TimeScale))
	if endDTS > s.endDTS {
		s.endDTS = endDTS
	}

	if s.curPart == nil {
		s.curPart = &formatFMP4Part{
			maxPartSize:     s.f.ri.maxPartSize,
			segmentStartDTS: s.startDTS,
			number:          s.nextPartNumber,
			startDTS:        dts,
		}
		s.curPart.initialize()
		s.nextPartNumber++
	} else if s.curPart.duration() >= s.f.ri.partDuration {
		err := s.closeCurPart()
		s.curPart = nil

		if err != nil {
			return err
		}

		s.curPart = &formatFMP4Part{
			maxPartSize:     s.f.ri.maxPartSize,
			segmentStartDTS: s.startDTS,
			number:          s.nextPartNumber,
			startDTS:        dts,
		}
		s.curPart.initialize()
		s.nextPartNumber++
	}

	return s.curPart.write(track, sample, dts)
}
