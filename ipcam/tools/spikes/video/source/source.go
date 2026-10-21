// Package source holds the camera sources of the Phase 0 (b) video spike hub:
// simulated cameras that replay a pre-encoded Annex B file in a loop, and an
// optional source that reads one real camera over RTSP.
//
// Every source produces access units (AUs), one per video frame. A source keeps
// the GOP since its last IDR, so that a new subscriber starts at a keyframe and
// reaches the current frame without waiting for the next one.
//
// Invariant: an AU is never delivered, cached or live, before the first IDR of
// the source, so the first AU a subscriber sees is always an IDR.
package source

import "time"

// Codec is the video codec of a source.
type Codec string

// The codecs a source can carry.
const (
	CodecH264 Codec = "H264"
	CodecH265 Codec = "H265"
)

// AccessUnit is one coded video frame.
//
// NALUs holds the NAL units without start codes. Consumers must treat them as
// read-only: they are shared between the GOP cache, all subscribers and, for
// simulated cameras, all simulators that replay the same file.
type AccessUnit struct {
	NALUs [][]byte
	IDR   bool
	// Frame is the index of the frame within the file for a simulator, and a
	// wrapping counter of received frames for an RTSP source.
	Frame uint16
	// Loop is the hub's replay counter: it increments each time a simulator
	// wraps to the first frame of its file. It is always 0 for an RTSP source.
	Loop uint16
	// PTS is the presentation time since the source started. It keeps
	// increasing across loops.
	PTS time.Duration
}

// SentRecord says that a simulator handed one frame to its subscribers.
type SentRecord struct {
	// Camera is the simulated camera index (1..16), not the media file id that
	// the pixel band carries.
	Camera uint8
	// Profile is the name of the stream of that camera, "sub" or "main". It is
	// not the codec profile that Source.Profile reports.
	Profile string
	// Frame is the index within the file and Loop the replay counter of the
	// frame, as in AccessUnit.
	Frame, Loop uint16
	// At is the hub clock reading when the frame was sent.
	At time.Time
}

// Source is a camera that the hub can serve.
type Source interface {
	ID() string
	Codec() Codec
	// Profile is the codec profile of the stream. For H.264 it is the
	// profile-level-id, the three bytes after the NAL header of the SPS as hex,
	// for example "4d0028" for Main at level 4.0. For H.265 it is the
	// general_profile_idc (with profile space and tier) and general_level_idc as
	// two hex bytes, for example "017b" for Main at level 4.1. It is empty until
	// the source has seen a parameter set.
	Profile() string
	// FPS is the frame rate. An RTSP source estimates it from the stream
	// timestamps and reports 0 until it has seen two frames.
	FPS() int
	// Subscribe returns the GOP since the last IDR, oldest first and ending at
	// the newest frame, and a channel with every following frame. cancel stops
	// the subscription and closes the channel; it is safe to call more than
	// once. The channel is also closed when the source ends.
	//
	// A subscriber that does not keep up never blocks the source: frames that
	// do not fit in its channel are dropped until the next IDR, so the decoder
	// only ever sees whole GOPs after a gap.
	Subscribe() (cached []AccessUnit, live <-chan AccessUnit, cancel func())
	// SentLog returns the records of the frames a simulator sent, or nil for
	// a source that is not a simulator. The channel has one consumer, and when
	// it is full the oldest record is dropped.
	SentLog() <-chan SentRecord
	// Jitter is the p99 deviation of the frame send interval from the frame
	// period since the source started. Only a simulator paces its sends; any
	// other source reports 0.
	Jitter() time.Duration
	// JitterSnapshot captures the pacing statistics at this moment. The jitter
	// of an interval is the later snapshot's P99Since the earlier one, so that a
	// run can be measured on its own although the source outlives it. A source
	// that does not pace its sends returns the zero snapshot.
	JitterSnapshot() JitterSnapshot
}
