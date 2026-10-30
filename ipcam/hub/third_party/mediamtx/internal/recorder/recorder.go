// Package recorder contains the recorder.
package recorder

import (
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/conf"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/logger"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/third_party/mediamtx/internal/stream"
)

const (
	ntpDriftTolerance = 5 * time.Second
)

// OnSegmentCreateFunc is the prototype of the function passed as OnSegmentCreate
type OnSegmentCreateFunc = func(path string)

// OnSegmentCompleteFunc is the prototype of the function passed as OnSegmentComplete.
// MTX-3 (hub/third_party/PATCHES.md): it also receives the segment's start, the
// wall time of its first sample.
type OnSegmentCompleteFunc = func(path string, start time.Time, duration time.Duration)

// Recorder writes recordings to disk.
type Recorder struct {
	PathFormat        string
	Format            conf.RecordFormat
	PartDuration      time.Duration
	MaxPartSize       conf.StringSize
	SegmentDuration   time.Duration
	PathName          string
	Stream            *stream.Stream
	OnSegmentCreate   OnSegmentCreateFunc
	OnSegmentComplete OnSegmentCompleteFunc
	Parent            logger.Writer

	// MTX-2 (hub/third_party/PATCHES.md): the recipients that fMP4 segments
	// are encrypted to, or nil to record them in clear.
	AgeScope *crypto.KeyScope
	// MTX-2: whether an encrypted part smaller than one age chunk is always
	// padded to the chunk boundary (the per-camera setting O17).
	AgePadLowBitrate bool

	restartPause time.Duration

	currentInstance *recorderInstance

	terminate chan struct{}
	done      chan struct{}
}

// Initialize initializes Recorder.
func (r *Recorder) Initialize() {
	if r.OnSegmentCreate == nil {
		r.OnSegmentCreate = func(string) {
		}
	}
	if r.OnSegmentComplete == nil {
		r.OnSegmentComplete = func(string, time.Time, time.Duration) {
		}
	}
	if r.restartPause == 0 {
		r.restartPause = 2 * time.Second
	}

	r.terminate = make(chan struct{})
	r.done = make(chan struct{})

	r.currentInstance = &recorderInstance{
		pathFormat:        r.PathFormat,
		format:            r.Format,
		partDuration:      r.PartDuration,
		maxPartSize:       r.MaxPartSize,
		segmentDuration:   r.SegmentDuration,
		pathName:          r.PathName,
		stream:            r.Stream,
		onSegmentCreate:   r.OnSegmentCreate,
		onSegmentComplete: r.OnSegmentComplete,
		parent:            r,
		ageScope:          r.AgeScope,
		agePadLowBitrate:  r.AgePadLowBitrate,
	}
	r.currentInstance.initialize()

	go r.run()
}

// Log implements logger.Writer.
func (r *Recorder) Log(level logger.Level, format string, args ...any) {
	r.Parent.Log(level, "[recorder] "+format, args...)
}

// Close closes the agent.
func (r *Recorder) Close() {
	r.Log(logger.Info, "recording stopped")
	close(r.terminate)
	<-r.done
}

func (r *Recorder) run() {
	defer close(r.done)

	for {
		select {
		case <-r.currentInstance.done:
			r.currentInstance.close()
		case <-r.terminate:
			r.currentInstance.close()
			return
		}

		select {
		case <-time.After(r.restartPause):
		case <-r.terminate:
			return
		}

		r.currentInstance = &recorderInstance{
			pathFormat:        r.PathFormat,
			format:            r.Format,
			partDuration:      r.PartDuration,
			maxPartSize:       r.MaxPartSize,
			segmentDuration:   r.SegmentDuration,
			pathName:          r.PathName,
			stream:            r.Stream,
			onSegmentCreate:   r.OnSegmentCreate,
			onSegmentComplete: r.OnSegmentComplete,
			parent:            r,
			ageScope:          r.AgeScope,
			agePadLowBitrate:  r.AgePadLowBitrate,
		}
		r.currentInstance.initialize()
	}
}
