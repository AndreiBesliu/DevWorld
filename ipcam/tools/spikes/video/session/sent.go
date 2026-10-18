package session

import (
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/source"
)

const (
	// sentInterval is how often a session reports the frames that its bound
	// simulators sent.
	sentInterval = 500 * time.Millisecond
	// jitterInterval is how often a session reports the hub's pacing jitter.
	jitterInterval = 10 * time.Second
)

// A simulator's sent log has one consumer, but several sessions may have the
// same simulator bound. The process-wide fan-out reads each sent log once and
// hands every record to each session that has the source bound at that moment
// (R22).

// sentSink receives the sent records of the sources it subscribed to.
type sentSink interface {
	addSent(src source.Source, r source.SentRecord)
}

// fanout is the process-wide fan-out of the simulators' sent logs.
var fanout = &sentFanout{sinks: map[source.Source]map[sentSink]struct{}{}}

type sentFanout struct {
	mu    sync.Mutex
	sinks map[source.Source]map[sentSink]struct{}
}

// subscribe delivers the sent records of src to sink from now on. The first
// subscription to a source starts the goroutine that reads its sent log, after
// it has discarded the records that the source logged before: they belong to
// frames that were sent, and cached, before any viewer bound the source, so a
// latency measured on them would include the age of the cache (R26). A source
// without a sent log is ignored.
func (f *sentFanout) subscribe(src source.Source, sink sentSink) {
	log := src.SentLog()
	if log == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sinks, reading := f.sinks[src]
	if !reading {
		sinks = map[sentSink]struct{}{}
		f.sinks[src] = sinks
		discardBacklog(log)
		go f.forward(src, log)
	}
	sinks[sink] = struct{}{}
}

// discardBacklog drops the records that are buffered in log, without waiting
// for more.
func discardBacklog(log <-chan source.SentRecord) {
	for {
		select {
		case _, ok := <-log:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// unsubscribe stops the delivery of the sent records of src to sink.
func (f *sentFanout) unsubscribe(src source.Source, sink sentSink) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sinks[src], sink)
}

// forward reads the sent log of src for as long as the source runs.
func (f *sentFanout) forward(src source.Source, log <-chan source.SentRecord) {
	for r := range log {
		f.mu.Lock()
		for sink := range f.sinks[src] {
			sink.addSent(src, r)
		}
		f.mu.Unlock()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sinks, src)
}

// boundSource is a source that one or more slots of a session carry, with the
// sent records not reported yet.
type boundSource struct {
	profile string // the stream profile of the bind key
	slots   int
	cam     uint8
	recs    [][3]float64
	warned  bool // the stream profile mismatch has been logged
}

// retain notes that one more slot carries src, bound as profile.
func (s *Session) retain(src source.Source, profile string) {
	s.sentMu.Lock()
	if s.closed {
		s.sentMu.Unlock()
		return
	}
	b, ok := s.bound[src]
	if !ok {
		b = &boundSource{profile: profile}
		s.bound[src] = b
	}
	b.slots++
	s.sentMu.Unlock()
	if !ok {
		fanout.subscribe(src, s)
	}
}

// release notes that one slot less carries src.
func (s *Session) release(src source.Source) {
	s.sentMu.Lock()
	b, ok := s.bound[src]
	last := ok && b.slots == 1
	switch {
	case last:
		delete(s.bound, src)
	case ok:
		b.slots--
	}
	s.sentMu.Unlock()
	if last {
		fanout.unsubscribe(src, s)
	}
}

// releaseAll releases every source; later retains do nothing.
func (s *Session) releaseAll() {
	s.sentMu.Lock()
	s.closed = true
	srcs := make([]source.Source, 0, len(s.bound))
	for src := range s.bound {
		srcs = append(srcs, src)
	}
	clear(s.bound)
	s.sentMu.Unlock()
	for _, src := range srcs {
		fanout.unsubscribe(src, s)
	}
}

// addSent keeps a sent record for the next sent message. The message's
// profile is the bind key's; a simulator that names another stream is
// misconfigured, which is logged once.
func (s *Session) addSent(src source.Source, r source.SentRecord) {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	b, ok := s.bound[src]
	if !ok {
		return
	}
	if r.Profile != "" && r.Profile != b.profile && !b.warned {
		b.warned = true
		s.log.Warn("a simulator sends another stream than the one it is bound as; sent messages name the bound one",
			"camera", src.ID(), "stream", r.Profile, "profile", b.profile)
	}
	b.cam = r.Camera
	b.recs = append(b.recs, [3]float64{float64(r.Frame), float64(r.Loop), unixMs(r.At)})
}

// flushSent sends one sent message per bound source that sent frames.
func (s *Session) flushSent() {
	s.sentMu.Lock()
	var msgs []sentMsg
	for _, b := range s.bound {
		if len(b.recs) > 0 {
			msgs = append(msgs, sentMsg{T: "sent", Cam: b.cam, P: b.profile, R: b.recs})
			b.recs = nil
		}
	}
	s.sentMu.Unlock()
	for _, m := range msgs {
		s.notify(m)
	}
}

// reportJitter sends the worst pacing jitter of the bound sources, if any.
func (s *Session) reportJitter() {
	s.sentMu.Lock()
	var worst time.Duration
	bound := len(s.bound) > 0
	for src := range s.bound {
		worst = max(worst, src.Jitter())
	}
	s.sentMu.Unlock()
	if bound {
		s.notify(jitterMsg{T: "jitter", MS: durationMs(worst)})
	}
}
