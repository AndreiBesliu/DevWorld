// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import (
	"context"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// sendLogKeep is how many of each stream's newest indexes the send log
// keeps: an hour at 20 fps. A run's report comes after the host's reboot,
// the NTP wait (at most 10 min) and reconcile, so its last recorded index
// was sent well within the hour; a 72 h run keeps bounded memory.
const sendLogKeep = 20 * 3600

// SendLog maps each sent access unit, by stream and SEI index, to the time
// when the controller sent it, on the controller's clock (C16). It is safe
// for concurrent use.
type SendLog struct {
	mu      sync.Mutex
	base    time.Time // the first record's time; entries are offsets from it
	streams map[string]*sendRing
}

// sendRing holds the newest sendLogKeep entries of one stream, at index mod
// sendLogKeep.
type sendRing struct {
	entries []sendEntry
}

type sendEntry struct {
	tag uint64        // index + 1; 0 is an empty slot
	off time.Duration // since SendLog.base
}

// NewSendLog returns an empty send log.
func NewSendLog() *SendLog {
	return &SendLog{streams: map[string]*sendRing{}}
}

// Record notes that index of stream was sent at at. The offsets from the
// first record keep the monotonic clock reading of the times that the
// controller's clock gives, so a step of the wall clock during a run does
// not change a loss.
func (l *SendLog) Record(stream string, index uint64, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.base.IsZero() {
		l.base = at
	}
	ring := l.streams[stream]
	if ring == nil {
		ring = &sendRing{}
		l.streams[stream] = ring
	}
	entry := sendEntry{tag: index + 1, off: at.Sub(l.base)}
	slot := int(index % sendLogKeep)
	for len(ring.entries) <= slot && len(ring.entries) < sendLogKeep {
		ring.entries = append(ring.entries, sendEntry{})
	}
	ring.entries[slot] = entry
}

// SendTime returns when index of stream was sent; ok is false when it was
// never sent or is older than the log keeps.
func (l *SendLog) SendTime(stream string, index uint64) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ring := l.streams[stream]
	if ring == nil {
		return time.Time{}, false
	}
	slot := int(index % sendLogKeep)
	if slot >= len(ring.entries) || ring.entries[slot].tag != index+1 {
		return time.Time{}, false
	}
	return l.base.Add(ring.entries[slot].off), true
}

// feedSendLog records every frame that a simulator sends of stream, a file
// of frames access units, until ctx is done. The index of a frame is
// Loop·frames + Frame, as the server stamps it.
func feedSendLog(ctx context.Context, stream string, frames int, sent <-chan source.SentRecord, log *SendLog) {
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-sent:
			log.Record(stream, frameIndex(r.Loop, r.Frame, frames), r.At)
		}
	}
}

// frameIndex is the absolute index of a simulator's frame.
func frameIndex(loop, frame uint16, frames int) uint64 {
	return uint64(loop)*uint64(frames) + uint64(frame) //nolint:gosec // frames is a file's positive frame count
}
