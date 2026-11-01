// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package spiketest

import (
	"encoding/binary"
	"errors"
	"os"

	"filippo.io/age"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
)

// The test recording's layout: 20 frames a second in 1-s parts at 90 kHz,
// as MediaMTX records the simulated cameras, with an IDR every 30 frames,
// so that some IDRs open a part and others fall inside one.
const (
	TimeScale   = 90000
	frameTicks  = TimeScale / 20
	partFrames  = 20
	idrInterval = 30
)

// SPS is the test recording's sequence parameter set: Constrained Baseline,
// level 4.0, so its codec is avc1.42c028.
var SPS = []byte{0x67, 0x42, 0xc0, 0x28, 0xd9, 0x00, 0x78, 0x02, 0x27, 0xe5, 0x84, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0xf0, 0x3c, 0x60, 0xc9, 0x20}

// Sample is one frame of a test recording: its AVCC payload, its decode
// time in TimeScale ticks from the recording's first frame, and whether it
// is an IDR.
type Sample struct {
	Payload []byte
	Time    uint64
	IDR     bool
}

// WriteSegment writes an encrypted fMP4 recording of secs seconds to path
// for recipients, laid out as MediaMTX writes one (§4.2): an init (ftyp,
// moov), then a moof and an mdat per 1-s part, with a free box after every
// other part as MTX-2's padding. Each frame's payload is distinct. It
// returns the frames in order.
func WriteSegment(path string, secs int, recipients ...age.Recipient) ([]Sample, error) {
	if secs < 1 {
		return nil, errors.New("spiketest: a recording of at least 1 s")
	}
	var plain seekablebuffer.Buffer
	init := fmp4.Init{Tracks: []*fmp4.InitTrack{{ID: 1, TimeScale: TimeScale, Codec: &codecs.H264{SPS: SPS, PPS: []byte{0x68, 0xce, 0x3c, 0x80}}}}}
	if err := init.Marshal(&plain); err != nil {
		return nil, err
	}
	var samples []Sample
	for p := range secs {
		track := &fmp4.PartTrack{ID: 1, BaseTime: uint64(p * partFrames * frameTicks)} //nolint:gosec // a few seconds
		for i := range partFrames {
			n := p*partFrames + i
			s := Sample{Payload: frame(n), Time: uint64(n * frameTicks), IDR: n%idrInterval == 0} //nolint:gosec // a few seconds
			samples = append(samples, s)
			track.Samples = append(track.Samples, &fmp4.Sample{Duration: frameTicks, IsNonSyncSample: !s.IDR, Payload: s.Payload})
		}
		var part seekablebuffer.Buffer
		if err := (fmp4.Part{SequenceNumber: uint32(p), Tracks: []*fmp4.PartTrack{track}}).Marshal(&part); err != nil { //nolint:gosec // a few seconds
			return nil, err
		}
		if _, err := plain.Write(part.Bytes()); err != nil {
			return nil, err
		}
		if p%2 == 1 {
			if _, err := plain.Write([]byte{0, 0, 0, 16, 'f', 'r', 'e', 'e', 0, 0, 0, 0, 0, 0, 0, 0}); err != nil {
				return nil, err
			}
		}
	}
	f, err := os.Create(path) //nolint:gosec // the test's own file
	if err != nil {
		return nil, err
	}
	w, err := age.Encrypt(f, recipients...)
	if err == nil {
		_, err = w.Write(plain.Bytes())
		err = errors.Join(err, w.Close())
	}
	return samples, errors.Join(err, f.Close())
}

// frame is the AVCC payload of frame n: one NAL unit, an IDR slice every
// idrInterval frames and a non-IDR slice otherwise, 6000 or 1500 bytes long,
// whose bytes name n.
func frame(n int) []byte {
	size, header := 1500, byte(0x41)
	if n%idrInterval == 0 {
		size, header = 6000, 0x65
	}
	b := make([]byte, 4+size)
	binary.BigEndian.PutUint32(b, uint32(size)) //nolint:gosec // small
	b[4] = header
	for i := 5; i < len(b); i++ {
		b[i] = byte(n + i) //nolint:gosec // the low byte, on purpose
	}
	return b
}
