// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import (
	"context"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

// sendLogKeep is how many of each stream's newest indexes the send log
// keeps: an hour at 20 fps. A run's report normally comes after the host's
// reboot, the NTP wait (at most 10 min) and reconcile, so its last
// recorded index was sent well within the hour; a later one is judged
// from the run's own copy (SendTimes). A 72 h run keeps bounded memory.
const sendLogKeep = 20 * 3600

// SendLog maps each sent access unit, by stream and SEI index, to the time
// when the controller sent it, on the controller's clock (C16). It is safe
// for concurrent use.
type SendLog struct {
	mu      sync.Mutex
	base    time.Time // the first record's time; entries are offsets from it
	streams map[string]*sendRing
	// fed is closed and dropped at the next record, for WaitFed; nil while
	// nobody waits.
	fed chan struct{}
}

// sendRing holds the newest sendLogKeep entries of one stream, at index mod
// sendLogKeep, and the latest send recorded: the feed's high-water mark.
type sendRing struct {
	entries []sendEntry
	newest  time.Duration // since SendLog.base
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
	entry := sendEntry{tag: index + 1, off: at.Sub(l.base)}
	ring := l.streams[stream]
	if ring == nil {
		ring = &sendRing{newest: entry.off}
		l.streams[stream] = ring
	}
	slot := int(index % sendLogKeep)
	for len(ring.entries) <= slot && len(ring.entries) < sendLogKeep {
		ring.entries = append(ring.entries, sendEntry{})
	}
	ring.entries[slot] = entry
	ring.newest = max(ring.newest, entry.off)
	if l.fed != nil {
		close(l.fed)
		l.fed = nil
	}
}

// WaitFed waits until the log has recorded, for each of streams, a send at
// or after t: its feed is asynchronous (each simulator buffers about 12 s
// of sends, source.SentLog), so a frame sent before t can reach the log
// after t. It reports false when ctx ends first.
func (l *SendLog) WaitFed(ctx context.Context, streams []string, t time.Time) bool {
	for {
		l.mu.Lock()
		fedPast := !l.base.IsZero()
		for _, stream := range streams {
			ring := l.streams[stream]
			fedPast = fedPast && ring != nil && !l.base.Add(ring.newest).Before(t)
		}
		if !fedPast && l.fed == nil {
			l.fed = make(chan struct{})
		}
		fed := l.fed
		l.mu.Unlock()
		if fedPast {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-fed:
		}
	}
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

// SendTimer gives the time when a stream's index was sent: the send log,
// or a run's copy of it (SendTimes).
type SendTimer interface {
	SendTime(stream string, index uint64) (time.Time, bool)
}

// SendTimers asks each of its timers in turn and gives the first send time
// found: a run's copy, then the live log, which also holds the frames that
// reached it after the copy.
type SendTimers []SendTimer

// SendTime returns the first of the timers' send times of index of stream.
func (ts SendTimers) SendTime(stream string, index uint64) (time.Time, bool) {
	for _, t := range ts {
		if at, ok := t.SendTime(stream, index); ok {
			return at, true
		}
	}
	return time.Time{}, false
}

// SendTimes is a copy of the send log's times of some streams within a
// window: a run's, from its first beacon to just after its stop, taken
// when its beacons go silent. The log keeps an hour, and a run's report
// can come later than that (mi1-1): from the copy, a late report gets the
// loss of a prompt one.
type SendTimes struct {
	base    time.Time
	streams map[string]*sendSpan
}

// sendSpan is one stream's copied entries, from index first on.
type sendSpan struct {
	first   uint64
	entries []sendEntry
}

// Times copies the send times of streams that fall within [from, to].
func (l *SendLog) Times(streams []string, from, to time.Time) *SendTimes {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := &SendTimes{base: l.base, streams: map[string]*sendSpan{}}
	lo, hi := from.Sub(l.base), to.Sub(l.base)
	for _, stream := range streams {
		ring := l.streams[stream]
		if ring == nil {
			continue
		}
		in := func(e sendEntry) bool { return e.tag != 0 && lo <= e.off && e.off <= hi }
		// A stream's indexes only grow, so the ring holds a span of at most
		// sendLogKeep of them, and so does the window.
		var first, last uint64
		found := false
		for _, e := range ring.entries {
			if !in(e) {
				continue
			}
			if index := e.tag - 1; !found {
				first, last, found = index, index, true
			} else {
				first, last = min(first, index), max(last, index)
			}
		}
		if !found {
			continue
		}
		span := &sendSpan{first: first, entries: make([]sendEntry, last-first+1)}
		for _, e := range ring.entries {
			if in(e) {
				span.entries[e.tag-1-first] = e
			}
		}
		t.streams[stream] = span
	}
	return t
}

// SendTime returns when index of stream was sent; ok is false when the
// copy does not hold it.
func (t *SendTimes) SendTime(stream string, index uint64) (time.Time, bool) {
	span := t.streams[stream]
	if span == nil || index < span.first || index-span.first >= uint64(len(span.entries)) {
		return time.Time{}, false
	}
	if e := span.entries[index-span.first]; e.tag == index+1 {
		return t.base.Add(e.off), true
	}
	return time.Time{}, false
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
