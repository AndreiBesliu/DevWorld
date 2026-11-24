package events

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The kinds of Signal (HUB-11, P44).
const (
	SignalUnhealthy = "events_unhealthy"
	SignalLost      = "events_lost"
	SignalHealthy   = "events_healthy"
)

// Event is a notification of the camera: its topic, whether it says motion
// is on, and the hub's receipt time in Unix ms, never the camera's UtcTime,
// whose clock may be skewed.
type Event struct {
	Cam, Topic string
	AtMs       int64
	Motion     bool
}

// Signal is a change of the subscription's state, decided by the state
// alone, never by motion (P44): SignalUnhealthy when no Renew or
// PullMessages succeeded for Options.UnhealthyAfter, SignalLost for
// Options.LostAfter (a silent loss), SignalHealthy when one succeeds again.
type Signal struct {
	Cam, Kind string
	AtMs      int64
}

// Options are a subscription's settings. A zero field takes its default.
type Options struct {
	// HTTP is the client of the SOAP requests; by default one that goes
	// through no proxy and follows no redirect.
	HTTP *http.Client
	// Clock is the hub's clock; time.Now by default.
	Clock func() time.Time
	// Term is the subscription's term (10 min), RenewAt the age of a term
	// at which it is renewed (8 min), UnhealthyAfter and LostAfter the time
	// without a successful Renew or PullMessages that signals
	// SignalUnhealthy (60 s) and SignalLost (5 min).
	Term, RenewAt, UnhealthyAfter, LostAfter time.Duration
	// AuthPause is the pause after 3 refused logins (60 s, §8.2).
	AuthPause time.Duration
}

// withDefaults is o with its zero fields set to their defaults.
func (o Options) withDefaults() Options {
	if o.HTTP == nil {
		o.HTTP = sharedHTTPClient()
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.Term <= 0 {
		o.Term = 10 * time.Minute
	}
	if o.RenewAt <= 0 || o.RenewAt >= o.Term {
		o.RenewAt = o.Term * 4 / 5
	}
	if o.UnhealthyAfter <= 0 {
		o.UnhealthyAfter = 60 * time.Second
	}
	if o.LostAfter <= 0 {
		o.LostAfter = 5 * time.Minute
	}
	if o.AuthPause <= 0 {
		o.AuthPause = 60 * time.Second
	}
	return o
}

// The timings derived from UnhealthyAfter: with its 60 s, a PullMessages
// waits 10 s at most for a message, any request gets 20 s more to answer
// (so a silent camera's PullMessages fails after 30 s), a failed request is
// retried after 5 s and the health is checked every 5 s.

// pullWait is the longest a PullMessages waits for a message.
func (o Options) pullWait() time.Duration { return o.UnhealthyAfter / 6 }

// callTimeout is the time a request gets to answer.
func (o Options) callTimeout() time.Duration { return o.UnhealthyAfter / 3 }

// retryDelay is the wait after a failed request.
func (o Options) retryDelay() time.Duration { return o.UnhealthyAfter / 12 }

// healthTick is the period of the health check.
func (o Options) healthTick() time.Duration { return o.UnhealthyAfter / 12 }

// sharedHTTPClient is the default client, one for every camera, so that
// their idle connections share one pool.
var sharedHTTPClient = sync.OnceValue(defaultHTTPClient)

// defaultHTTPClient reaches a camera on the LAN directly: no proxy of the
// environment, no redirect.
func defaultHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Subscription is a camera's PullPoint subscription, renewed before its
// term ends and created again when the camera drops it.
type Subscription interface {
	// Events are the camera's notifications. The channel closes when the
	// subscription stops.
	Events() <-chan Event
	// Signals are the subscription's state changes. The channel closes
	// when the subscription stops.
	Signals() <-chan Signal
	// Healthy reports whether a Renew or PullMessages succeeded within
	// Options.UnhealthyAfter.
	Healthy() bool
	// Close stops the subscription and unsubscribes it on the camera.
	Close(ctx context.Context) error
}

// Subscribe reads the camera's clock and services and creates a PullPoint
// subscription, then pulls its messages until Close or ctx's end. Its error's
// Reason is ReasonAuth, ReasonNoPullPoint, ReasonOnvifPort or
// ReasonUnreachable.
func Subscribe(ctx context.Context, cam CameraRef, opts Options) (Subscription, error) {
	c := newClient(cam, opts)
	if err := c.syncClock(ctx); err != nil {
		return nil, err
	}
	svc, err := c.services(ctx)
	if err != nil {
		return nil, err
	}
	if svc.events == "" || !svc.pullPoint {
		return nil, ErrNoPullPoint
	}
	p, err := c.createPullPoint(ctx, svc.events, c.opts.Term)
	if err != nil {
		if errors.Is(err, errFault) || errors.Is(err, errGone) {
			return nil, errors.Join(ErrNoPullPoint, err)
		}
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &subscription{
		c: c, events: svc.events, p: p, termStart: c.now(),
		eventsCh: make(chan Event, 256), signalsCh: make(chan Signal, 16),
		cancel: cancel, done: make(chan struct{}), watchDone: make(chan struct{}),
	}
	s.lastOK.Store(c.now())
	stopOnParent := context.AfterFunc(ctx, cancel)
	go func() {
		defer stopOnParent()
		s.loop(runCtx)
	}()
	go s.watch(runCtx)
	return s, nil
}

// subscription is Subscribe's Subscription.
type subscription struct {
	c         *client
	events    string // the events service's XAddr
	eventsCh  chan Event
	signalsCh chan Signal
	cancel    context.CancelFunc
	done      chan struct{} // closed when loop returns
	watchDone chan struct{} // closed when watch returns
	closeOnce sync.Once
	closeErr  error

	lastOK atomicTime // the last successful create, Renew or PullMessages

	// The loop's own.
	p         *pullPoint
	termStart time.Time
}

// atomicTime is a time read and written by several goroutines.
type atomicTime struct {
	mu sync.Mutex
	t  time.Time
}

// Store sets the time.
func (a *atomicTime) Store(t time.Time) {
	a.mu.Lock()
	a.t = t
	a.mu.Unlock()
}

// Load returns the time.
func (a *atomicTime) Load() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.t
}

// Events implements Subscription.
func (s *subscription) Events() <-chan Event { return s.eventsCh }

// Signals implements Subscription.
func (s *subscription) Signals() <-chan Signal { return s.signalsCh }

// Healthy implements Subscription.
func (s *subscription) Healthy() bool {
	return s.c.now().Sub(s.lastOK.Load()) <= s.c.opts.UnhealthyAfter
}

// Close implements Subscription: it stops the loop, then unsubscribes the
// subscription that the loop held.
func (s *subscription) Close(ctx context.Context) error {
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.done
		<-s.watchDone
		if s.p != nil {
			s.closeErr = s.c.unsubscribe(ctx, s.p)
			if errors.Is(s.closeErr, errGone) {
				s.closeErr = nil
			}
			s.p = nil
		}
	})
	return s.closeErr
}

// ok records a successful request.
func (s *subscription) ok() { s.lastOK.Store(s.c.now()) }

// sleep waits d, false when ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// maxPullFailures is how many failed PullMessages in a row make the loop
// drop the subscription and create another.
const maxPullFailures = 2

// loop pulls the subscription's messages, renews it at RenewAt, and creates
// it again when the camera dropped it or it stopped answering.
func (s *subscription) loop(ctx context.Context) {
	defer close(s.done)
	defer close(s.eventsCh)
	opts := s.c.opts
	failures := 0
	for ctx.Err() == nil {
		if s.p == nil {
			if !s.recreate(ctx) {
				if !sleep(ctx, opts.retryDelay()) {
					return
				}
				continue
			}
		}
		if renewIn := opts.RenewAt - s.c.now().Sub(s.termStart); renewIn <= 0 {
			err := s.c.renew(ctx, s.p, opts.Term)
			switch {
			case err == nil:
				s.ok()
				s.termStart = s.c.now()
			case errors.Is(err, errGone):
				s.p = nil
				continue
			case ctx.Err() != nil:
				return
			default:
				if !sleep(ctx, opts.retryDelay()) {
					return
				}
				continue
			}
		}
		wait := min(opts.pullWait(), max(opts.RenewAt-s.c.now().Sub(s.termStart), time.Millisecond))
		msgs, err := s.c.pull(ctx, s.p, wait)
		switch {
		case err == nil:
			failures = 0
			s.ok()
			if !s.deliver(ctx, msgs) {
				return
			}
		case ctx.Err() != nil:
			return
		case errors.Is(err, errGone):
			s.p = nil
		default:
			failures++
			if failures >= maxPullFailures {
				failures = 0
				s.drop(ctx)
			}
			if !sleep(ctx, opts.retryDelay()) {
				return
			}
		}
	}
}

// drop unsubscribes the subscription that stopped answering, as far as the
// camera answers, so that it does not hold one of the camera's PullPoints.
func (s *subscription) drop(ctx context.Context) {
	_ = s.c.unsubscribe(ctx, s.p)
	s.p = nil
}

// recreate creates the subscription again; false when it failed.
func (s *subscription) recreate(ctx context.Context) bool {
	if err := s.c.syncClock(ctx); err != nil {
		return false
	}
	p, err := s.c.createPullPoint(ctx, s.events, s.c.opts.Term)
	if err != nil {
		return false
	}
	s.p, s.termStart = p, s.c.now()
	s.ok()
	return true
}

// deliver sends the messages' events; false when ctx ended first.
func (s *subscription) deliver(ctx context.Context, msgs []eventMessage) bool {
	at := s.c.now().UnixMilli()
	for _, m := range msgs {
		topic := strings.TrimSpace(string(m.Topic.TopicKinds))
		e := Event{Cam: s.c.cam.ID, Topic: topic, AtMs: at, Motion: isMotion(topic, m)}
		select {
		case s.eventsCh <- e:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

// watch emits the signals from the time since the last success, every
// healthTick: SignalUnhealthy past UnhealthyAfter, SignalLost past
// LostAfter, each once a loss, and SignalHealthy when a request succeeds
// after SignalUnhealthy.
func (s *subscription) watch(ctx context.Context) {
	defer close(s.watchDone)
	defer close(s.signalsCh)
	opts := s.c.opts
	tick := time.NewTicker(opts.healthTick())
	defer tick.Stop()
	unhealthy, lost := false, false
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		now := s.c.now()
		age := now.Sub(s.lastOK.Load())
		var kinds []string
		switch {
		case age <= opts.UnhealthyAfter:
			if unhealthy {
				kinds = append(kinds, SignalHealthy)
			}
			unhealthy, lost = false, false
		default:
			if !unhealthy {
				unhealthy = true
				kinds = append(kinds, SignalUnhealthy)
			}
			if age > opts.LostAfter && !lost {
				lost = true
				kinds = append(kinds, SignalLost)
			}
		}
		for _, k := range kinds {
			select {
			case s.signalsCh <- Signal{Cam: s.c.cam.ID, Kind: k, AtMs: now.UnixMilli()}:
			case <-ctx.Done():
				return
			}
		}
	}
}
