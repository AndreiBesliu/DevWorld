package source

import (
	"slices"
	"sync"
)

const (
	// subscriberBuffer is how many access units a subscriber may fall behind:
	// about 6 s at 20 fps.
	subscriberBuffer = 128
	// maxGOPFrames bounds the cache against a stream that never sends an IDR.
	maxGOPFrames = 1024
)

// gop caches the access units since the last IDR of one source and fans every
// new access unit out to the subscribers.
type gop struct {
	mu     sync.Mutex
	aus    []AccessUnit
	subs   map[*subscriber]struct{}
	closed bool
}

type subscriber struct {
	ch chan AccessUnit
	// skipping is set after an access unit did not fit in ch: everything up to
	// the next IDR that fits is dropped, so the subscriber's decoder never sees
	// a frame whose reference it missed.
	skipping bool
}

func newGOP() *gop {
	return &gop{subs: make(map[*subscriber]struct{})}
}

// push adds the next access unit of the source. Until the first IDR it drops
// everything: such frames cannot be decoded. It never blocks.
func (g *gop) push(au AccessUnit) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	switch {
	case au.IDR:
		g.aus = append(g.aus[:0], au)
	case len(g.aus) == 0:
		return // no IDR yet, or the last GOP never ended and was dropped
	case len(g.aus) >= maxGOPFrames:
		g.aus = g.aus[:0]
		return
	default:
		g.aus = append(g.aus, au)
	}
	for s := range g.subs {
		if s.skipping && !au.IDR {
			continue
		}
		select {
		case s.ch <- au:
			s.skipping = false
		default:
			s.skipping = true
		}
	}
}

// subscribe returns the cached GOP and a channel with every access unit pushed
// after it. Both come from one critical section, so no access unit is missed or
// repeated between them.
func (g *gop) subscribe() (cached []AccessUnit, live <-chan AccessUnit, cancel func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cached = slices.Clone(g.aus)
	s := &subscriber{ch: make(chan AccessUnit, subscriberBuffer)}
	if g.closed {
		close(s.ch)
		return cached, s.ch, func() {}
	}
	g.subs[s] = struct{}{}
	cancel = func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if _, ok := g.subs[s]; ok {
			delete(g.subs, s)
			close(s.ch)
		}
	}
	return cached, s.ch, cancel
}

// close ends the source: every subscriber's channel is closed, and later
// subscribers get the last GOP and a closed channel.
func (g *gop) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	g.closed = true
	for s := range g.subs {
		close(s.ch)
	}
	clear(g.subs)
}
