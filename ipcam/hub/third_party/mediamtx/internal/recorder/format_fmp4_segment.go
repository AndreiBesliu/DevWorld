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
	fi             segmentFile
	curPart        *formatFMP4Part
	endDTS         time.Duration
	nextPartNumber uint32

	// MTX-2: the writer of an encrypted segment's plaintext, nil for a clear
	// segment, and where the segment's fMP4 bytes go: age, or fi itself.
	age *ageSegmentWriter
	w   io.Writer

	// MTX-1: the first failed write or fsync of a part. The segment then keeps
	// its .part name.
	partErr error
}

func (s *formatFMP4Segment) initialize() {
	s.endDTS = s.startDTS
}

func (s *formatFMP4Segment) close() error {
	var err error

	if s.curPart != nil {
		err = s.closeCurPart()
	}

	if s.fi != nil {
		s.f.ri.Log(logger.Debug, "closing segment %s", s.path)

		// MTX-1: a part that failed earlier is a failure of the close.
		if err == nil {
			err = s.partErr
		}

		duration := s.endDTS - s.startDTS
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
		}
	}

	return err
}

func (s *formatFMP4Segment) closeCurPart() error {
	if s.fi == nil {
		s.path = recordstore.Path{Start: s.startNTP}.Encode(s.f.ri.pathFormat2)
		if s.f.ri.ageScope != nil {
			// MTX-2: an encrypted segment is <name>.mp4.age.
			s.path += ageSuffix
		}
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

		// MTX-1: make the new name durable.
		err = syncDir(filepath.Dir(s.path))
		if err != nil {
			fi.Close()
			return err
		}

		s.f.ri.onSegmentCreate(fi.Name())

		// MTX-2: every byte of an encrypted segment goes through the age
		// writer, which writes the age header first. No clear byte reaches
		// the file (HUB-04).
		var w io.Writer = fi
		var age *ageSegmentWriter
		if scope := s.f.ri.ageScope; scope != nil {
			age, err = newAgeSegmentWriter(fi, *scope, s.f.ri.agePadLowBitrate)
			if err != nil {
				fi.Close()
				return err
			}
			w = age
		}

		err = writeInit(
			w,
			s.f.ri.streamID,
			s.number,
			s.startDTS,
			s.startNTP,
			s.f.tracks)
		if err != nil {
			fi.Close()
			return err
		}

		s.fi = fi
		s.age = age
		s.w = w
	}

	var partStart int64
	if s.age != nil {
		partStart = s.age.size()
	}
	err := s.curPart.close(s.w)
	if err == nil && s.age != nil {
		// MTX-2: pad the part when §4.5's rule says so, and seal its complete
		// chunks before the fsync.
		err = s.age.endPart(partStart)
	}
	if err == nil {
		// MTX-1: every part is durable once it is closed.
		err = syncFile(s.fi)
	}
	if err != nil && s.partErr == nil {
		s.partErr = err
	}
	return err
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
