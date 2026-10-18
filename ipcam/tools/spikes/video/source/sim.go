package source

import (
	"fmt"
	"time"
)

// sentLogSize is how many records SentLog buffers: about 12 s at 20 fps.
const sentLogSize = 256

// sim is a simulated camera. It replays the access units of one file in a loop,
// one per tick of its clock, from the moment it is created.
type sim struct {
	id      string
	camera  uint8
	stream  string
	codec   Codec
	profile string
	fps     int
	aus     []AccessUnit

	pacer *pacer
	gop   *gop
	sent  chan SentRecord

	// Owned by the goroutine that runs the pacer.
	next    int
	loop    uint16
	emitted int64
}

// SimOption customises a simulator.
type SimOption func(*sim)

// WithStreamProfile names the stream the simulator plays, "sub" or "main". The
// name is reported as SentRecord.Profile.
func WithStreamProfile(name string) SimOption {
	return func(s *sim) { s.stream = name }
}

// NewSim starts a simulated camera that replays aus, as read by ReadAnnexB, at
// fps frames per second for as long as the process runs. camera is the index of
// the simulated camera that its SentRecords report. phase is the number of
// frames the simulator is ahead of the start of the file when it starts, so that
// cameras that replay the same file do not show the same frame at the same
// moment; a phase beyond the file wraps, and a negative one counts from the end.
//
// The codec comes from the NAL units, as in ReadAnnexB. Several simulators may
// replay the same aus. NewSim panics when aus is empty or holds more than 65536
// frames, when the codec cannot be told, or when fps is not positive.
func NewSim(id string, camera uint8, aus []AccessUnit, fps, phase int, clock Clock, opts ...SimOption) Source {
	if len(aus) == 0 || len(aus) > maxAccessUnits || len(aus[0].NALUs) == 0 {
		panic(fmt.Sprintf("source: NewSim %s: %d access units", id, len(aus)))
	}
	if fps <= 0 {
		panic(fmt.Sprintf("source: NewSim %s: fps %d", id, fps))
	}
	codec, ok := detectCodec(aus[0].NALUs[0])
	if !ok {
		panic(fmt.Sprintf("source: NewSim %s: cannot tell the codec from the first NAL unit", id))
	}
	s := &sim{
		id:      id,
		camera:  camera,
		codec:   codec,
		profile: firstProfile(codec, aus),
		fps:     fps,
		aus:     aus,
		pacer:   newPacer(clock, time.Second/time.Duration(fps)),
		gop:     newGOP(),
		sent:    make(chan SentRecord, sentLogSize),
		next:    (phase%len(aus) + len(aus)) % len(aus),
	}
	for _, opt := range opts {
		opt(s)
	}
	go func() {
		s.pacer.run(s.emit)
		s.gop.close()
	}()
	return s
}

// emit sends the next frame at the hub time at.
func (s *sim) emit(at time.Time) {
	au := s.aus[s.next]
	au.Frame = uint16(s.next) //nolint:gosec // NewSim checked that the file has at most 65536 frames
	au.Loop = s.loop
	au.PTS = time.Duration(s.emitted) * time.Second / time.Duration(s.fps)
	s.gop.push(au)
	s.record(SentRecord{Camera: s.camera, Profile: s.stream, Frame: au.Frame, Loop: au.Loop, At: at})

	s.emitted++
	s.next++
	if s.next == len(s.aus) {
		s.next = 0
		s.loop++
	}
}

// record appends to the sent log, dropping the oldest record when it is full.
// emit is the only writer, so the second send cannot block.
func (s *sim) record(r SentRecord) {
	select {
	case s.sent <- r:
	default:
		select {
		case <-s.sent:
		default:
		}
		s.sent <- r
	}
}

func (s *sim) ID() string                 { return s.id }
func (s *sim) Codec() Codec               { return s.codec }
func (s *sim) Profile() string            { return s.profile }
func (s *sim) FPS() int                   { return s.fps }
func (s *sim) SentLog() <-chan SentRecord { return s.sent }
func (s *sim) Jitter() time.Duration      { return s.pacer.jitter() }

func (s *sim) Subscribe() ([]AccessUnit, <-chan AccessUnit, func()) {
	return s.gop.subscribe()
}
