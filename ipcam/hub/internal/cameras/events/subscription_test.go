package events

import (
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/cameras/events/onviftest"
)

// The tests run the subscription's minutes as fractions of a second: one
// minute of §8.2's timing is minute below.
const minute = 250 * time.Millisecond

// fastOptions are Options with every duration scaled so that one minute is
// minute: a 10-min term renewed at 8 min, unhealthy after 60 s, lost after
// 5 min, a 60-s pause after 3 refused logins.
func fastOptions() Options {
	return Options{
		Term:           10 * minute,
		RenewAt:        8 * minute,
		UnhealthyAfter: minute,
		LostAfter:      5 * minute,
		AuthPause:      minute,
	}
}

// newSim starts a simulated camera with the sentinel credentials, and the
// camera that reaches it.
func newSim(t *testing.T, cfg onviftest.Config) (*onviftest.Sim, CameraRef) {
	t.Helper()
	cam := sentinelCamera()
	cfg.User, cfg.Pass = cam.User, cam.Pass
	sim := onviftest.New(cfg)
	t.Cleanup(sim.Close)
	cam.XAddr = sim.XAddr()
	return sim, cam
}

// closedXAddr is a device service XAddr on a loopback port that nothing
// listens on.
func closedXAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return "http://" + addr + "/onvif/device_service"
}

// hostPort is the host:port of rawURL.
func hostPort(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// subscribe subscribes to cam, and closes the subscription at the test's end.
func subscribe(t *testing.T, cam CameraRef, opts Options) Subscription {
	t.Helper()
	sub, err := Subscribe(t.Context(), cam, opts)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sub.Close(ctx)
	})
	return sub
}

// nextSignal waits up to d for sub's next signal.
func nextSignal(t *testing.T, sub Subscription, d time.Duration) (Signal, bool) {
	t.Helper()
	select {
	case s, ok := <-sub.Signals():
		return s, ok
	case <-time.After(d):
		return Signal{}, false
	}
}

// schedulingSlack is what a signal's deadline may be missed by on a busy
// runner: 3 s of §8.2's time, never a health tick (5 s).
const schedulingSlack = minute / 20

// faultProxy is a camera in front of sim: answer gives the reply to a
// request whose body it matches (status and body; ok false forwards it).
func faultProxy(t *testing.T, sim *onviftest.Sim, cam CameraRef,
	answer func(body string) (status int, reply string, ok bool),
) CameraRef {
	t.Helper()
	target, err := url.Parse(sim.XAddr())
	if err != nil {
		t.Fatal(err)
	}
	rp := &httputil.ReverseProxy{
		Rewrite:  func(r *httputil.ProxyRequest) { r.SetURL(&url.URL{Scheme: target.Scheme, Host: target.Host}) },
		ErrorLog: log.New(io.Discard, "", 0),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}
		if status, reply, ok := answer(string(body)); ok {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, reply)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	cam.XAddr = srv.URL + target.Path
	return cam
}

// soapFaultReply is a SOAP 1.2 fault with the subcode.
func soapFaultReply(subcode string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" ` +
		`xmlns:ter="http://www.onvif.org/ver10/error" xmlns:wsrf-rw="http://docs.oasis-open.org/wsrf/rw-2">` +
		`<s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>` + subcode +
		`</s:Value></s:Subcode></s:Code><s:Reason><s:Text xml:lang="en">fault</s:Text></s:Reason></s:Fault></s:Body></s:Envelope>`
}

// waitFor polls cond every 10 ms for up to d.
func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// TestEvents_RenewsBefore10Min (HUB-11, §8.2): the subscription renews its
// 10-min term at 8 min, so it never lapses and never leaks a PullPoint:
// after 10 rounds the camera has created one subscription, renewed it at
// least 10 times, and holds none once the subscription is closed, with
// MaxPullPoints=5.
func TestEvents_RenewsBefore10Min(t *testing.T) {
	sim, cam := newSim(t, onviftest.Config{MaxPullPoints: 5})
	opts := fastOptions()
	// Health is not under test here: a slow runner's late answer must not
	// count as a loss that creates the subscription again.
	opts.UnhealthyAfter = 4 * minute
	sub, err := Subscribe(t.Context(), cam, opts)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if !waitFor(10*opts.RenewAt+opts.Term, func() bool { return sim.Counts().Renews >= 10 }) {
		t.Fatalf("renews = %d after 10 rounds; want 10", sim.Counts().Renews)
	}
	if got := sim.Counts(); got.Creates != 1 || got.Expired != 0 {
		t.Errorf("creates = %d, expired = %d; want 1 subscription that never lapsed", got.Creates, got.Expired)
	}
	if n := sim.Subscriptions(); n != 1 {
		t.Errorf("the camera holds %d subscriptions during the run; want 1", n)
	}
	if !sub.Healthy() {
		t.Error("Healthy() = false on a renewed subscription")
	}
	if err := sub.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n := sim.Subscriptions(); n != 0 {
		t.Errorf("the camera holds %d leftover subscriptions; want 0", n)
	}
	for _, at := range sim.RenewOffsets() {
		if at < 7*minute || at > 9*minute {
			t.Errorf("a renew came %v after the term began; want about 8 min (%v)", at, 8*minute)
		}
	}
}

// TestEvents_HealthyFalseAfterLoss (HUB-11, P44): a camera that stops
// answering makes Healthy() false within 60 s and emits events_unhealthy;
// once it answers again, events_healthy follows.
func TestEvents_HealthyFalseAfterLoss(t *testing.T) {
	sim, cam := newSim(t, onviftest.Config{})
	opts := fastOptions()
	sub := subscribe(t, cam, opts)
	if !sub.Healthy() {
		t.Fatal("Healthy() = false after Subscribe")
	}
	lostAt := time.Now()
	sim.SetSilent(true)
	// The signal is due at the last success + UnhealthyAfter, which is
	// before lostAt + UnhealthyAfter; schedulingSlack covers a busy runner.
	s, ok := nextSignal(t, sub, opts.UnhealthyAfter+schedulingSlack)
	if !ok || s.Kind != SignalUnhealthy || s.Cam != cam.ID {
		t.Fatalf("signal = %+v, %v; want %s for %s", s, ok, SignalUnhealthy, cam.ID)
	}
	// The signal's own time, not its receipt, so that a busy runner's
	// delay in reading the channel does not count against the hub.
	if after := time.UnixMilli(s.AtMs).Sub(lostAt); after > opts.UnhealthyAfter+schedulingSlack {
		t.Errorf("events_unhealthy came %v after the loss; want within %v (+%v slack)", after, opts.UnhealthyAfter, schedulingSlack)
	}
	if sub.Healthy() {
		t.Error("Healthy() = true after events_unhealthy")
	}
	sim.SetSilent(false)
	if s, ok = nextSignal(t, sub, 3*opts.UnhealthyAfter); !ok || s.Kind != SignalHealthy {
		t.Fatalf("signal = %+v, %v; want %s once the camera answers", s, ok, SignalHealthy)
	}
	if !sub.Healthy() {
		t.Error("Healthy() = false after events_healthy")
	}
}

// TestEvents_LostAfter5MinWithoutSuccess (HUB-11, MI8): more than 5 min
// without a successful Renew or PullMessages emits events_lost, once.
func TestEvents_LostAfter5MinWithoutSuccess(t *testing.T) {
	sim, cam := newSim(t, onviftest.Config{})
	opts := fastOptions()
	sub := subscribe(t, cam, opts)
	lostAt := time.Now()
	sim.SetSilent(true)
	var kinds []string
	for len(kinds) < 2 {
		s, ok := nextSignal(t, sub, opts.LostAfter+opts.UnhealthyAfter)
		if !ok {
			t.Fatalf("signals = %v; want events_unhealthy, then events_lost", kinds)
		}
		kinds = append(kinds, s.Kind)
	}
	if kinds[0] != SignalUnhealthy || kinds[1] != SignalLost {
		t.Fatalf("signals = %v; want [%s %s]", kinds, SignalUnhealthy, SignalLost)
	}
	if after := time.Since(lostAt); after < opts.LostAfter {
		t.Errorf("events_lost came %v after the loss; want after %v", after, opts.LostAfter)
	}
	if s, ok := nextSignal(t, sub, 2*opts.UnhealthyAfter); ok {
		t.Errorf("a further signal %+v while the camera stays silent; want one events_lost", s)
	}
}

// TestEvents_QuietSceneStaysHealthy (HUB-11, P44): 10 min of successful
// empty PullMessages keep Healthy() true and emit nothing: the subscription's
// state decides, never motion.
func TestEvents_QuietSceneStaysHealthy(t *testing.T) {
	sim, cam := newSim(t, onviftest.Config{})
	opts := fastOptions()
	sub := subscribe(t, cam, opts)
	end := time.Now().Add(10 * minute)
	for time.Now().Before(end) {
		if !sub.Healthy() {
			t.Fatal("Healthy() = false in a quiet scene")
		}
		select {
		case s := <-sub.Signals():
			t.Fatalf("signal %+v in a quiet scene", s)
		case e := <-sub.Events():
			t.Fatalf("event %+v in a quiet scene", e)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if sim.Counts().Pulls < 10 {
		t.Errorf("pulls = %d in 10 min; want the subscription pulling", sim.Counts().Pulls)
	}
}

// TestEvents_ExpiredSubscriptionResubscribes: a PullPoint that the camera
// dropped (a reboot, an expiry) is created again, and its events arrive with
// the hub's receipt time and the motion flag.
func TestEvents_ExpiredSubscriptionResubscribes(t *testing.T) {
	sim, cam := newSim(t, onviftest.Config{})
	opts := fastOptions()
	sub := subscribe(t, cam, opts)
	sim.ExpireAll()
	if !waitFor(3*opts.UnhealthyAfter, func() bool { return sim.Counts().Creates == 2 && sim.Subscriptions() == 1 }) {
		t.Fatalf("creates = %d, subscriptions = %d after the expiry; want a new subscription", sim.Counts().Creates, sim.Subscriptions())
	}
	before := time.Now().UnixMilli()
	sim.Emit("tns1:RuleEngine/CellMotionDetector/Motion", true)
	select {
	case e := <-sub.Events():
		if e.Cam != cam.ID || !e.Motion || e.Topic != "tns1:RuleEngine/CellMotionDetector/Motion" {
			t.Errorf("event = %+v; want a motion event of %s", e, cam.ID)
		}
		if e.AtMs < before || e.AtMs > time.Now().UnixMilli() {
			t.Errorf("event at %d; want the hub's receipt time, in [%d, now]", e.AtMs, before)
		}
	case <-time.After(3 * opts.UnhealthyAfter):
		t.Fatal("no event after the resubscription")
	}
	sim.Emit("tns1:RuleEngine/CellMotionDetector/Motion", false)
	select {
	case e := <-sub.Events():
		if e.Motion {
			t.Errorf("event = %+v; want motion false", e)
		}
	case <-time.After(3 * opts.UnhealthyAfter):
		t.Fatal("no second event")
	}
	select {
	case s := <-sub.Signals():
		if s.Kind == SignalLost {
			t.Errorf("signal %+v: an expiry that was resubscribed at once is not a silent loss", s)
		}
	default:
	}
}

// TestEvents_SubscribeFailedReasons: Subscribe's errors give diag onvif's
// subscribe_failed reasons.
func TestEvents_SubscribeFailedReasons(t *testing.T) {
	_, cam := newSim(t, onviftest.Config{})
	wrong := cam
	wrong.Pass = "SENT" + "INEL-wrong"
	_, noPP := newSim(t, onviftest.Config{NoPullPoint: true})
	closed := cam
	closed.XAddr = closedXAddr(t)
	for _, tc := range []struct {
		cam  CameraRef
		want string
	}{{wrong, ReasonAuth}, {noPP, ReasonNoPullPoint}, {closed, ReasonOnvifPort}} {
		_, err := Subscribe(t.Context(), tc.cam, fastOptions())
		if got := Reason(err); got != tc.want {
			t.Errorf("Reason(%v) = %q; want %q", err, got, tc.want)
		}
	}
}

// TestEvents_StreamAndSnapshotURI: the media calls pick the sub profile by
// its resolution, and return the camera's URIs as they are.
func TestEvents_StreamAndSnapshotURI(t *testing.T) {
	_, cam := newSim(t, onviftest.Config{
		StreamURIs:  map[string]string{"main": "rtsp://cam/main", "sub": "rtsp://cam/sub"},
		SnapshotURI: "http://cam/snap.jpg",
	})
	for profile, want := range map[string]string{"sub": "rtsp://cam/sub", "main": "rtsp://cam/main"} {
		got, err := StreamURI(t.Context(), cam, profile, fastOptions())
		if err != nil || got != want {
			t.Errorf("StreamURI(%s) = %q, %v; want %q", profile, got, err, want)
		}
	}
	if got, err := SnapshotURI(t.Context(), cam, "sub", fastOptions()); err != nil || got != "http://cam/snap.jpg" {
		t.Errorf("SnapshotURI = %q, %v", got, err)
	}
}

// TestEvents_PullFaultsAreALoss (HUB-11, P44, MI8): a camera that takes
// every CreatePullPointSubscription but fails every PullMessages delivers no
// event, so it is a loss: a create again is no success for the health clock.
// Healthy() turns false, events_unhealthy comes within 60 s of the last
// success and events_lost after 5 min, whether the pulls answer a generic
// fault, an HTTP 500 or a fault that the subscription is unknown.
func TestEvents_PullFaultsAreALoss(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		reply  string
	}{
		{"generic fault", http.StatusBadRequest, soapFaultReply("ter:ActionNotSupported")},
		{"HTTP 500", http.StatusInternalServerError, "internal error"},
		{"unknown subscription", http.StatusBadRequest, soapFaultReply("wsrf-rw:ResourceUnknownFault")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sim, cam := newSim(t, onviftest.Config{})
			var faulting atomic.Bool
			cam = faultProxy(t, sim, cam, func(body string) (int, string, bool) {
				if faulting.Load() && strings.Contains(body, "PullMessages>") {
					return tc.status, tc.reply, true
				}
				return 0, "", false
			})
			opts := fastOptions()
			start := time.Now()
			sub := subscribe(t, cam, opts)
			faulting.Store(true)
			lostAt := time.Now()
			s, ok := nextSignal(t, sub, opts.UnhealthyAfter+schedulingSlack)
			if !ok || s.Kind != SignalUnhealthy {
				t.Fatalf("signal = %+v, %v; want %s within %v of the faults", s, ok, SignalUnhealthy, opts.UnhealthyAfter)
			}
			if sub.Healthy() {
				t.Error("Healthy() = true while every PullMessages fails")
			}
			s, ok = nextSignal(t, sub, opts.LostAfter)
			if !ok || s.Kind != SignalLost {
				t.Fatalf("signal = %+v, %v; want %s", s, ok, SignalLost)
			}
			if after := time.Since(lostAt); after < opts.LostAfter-opts.UnhealthyAfter/4 {
				t.Errorf("events_lost came %v after the faults; want about %v", after, opts.LostAfter)
			}
			// Each create again follows a retryDelay at least.
			if c, d := sim.Counts(), time.Since(start); c.Creates > 2+int(d/opts.retryDelay()) {
				t.Errorf("creates = %d in %v; want at most one per %v", c.Creates, d, opts.retryDelay())
			}
			faulting.Store(false)
			if s, ok = nextSignal(t, sub, 3*opts.UnhealthyAfter); !ok || s.Kind != SignalHealthy {
				t.Fatalf("signal = %+v, %v; want %s once PullMessages succeeds", s, ok, SignalHealthy)
			}
		})
	}
}

// TestEvents_RenewFailureRecreates: once the renew is due, a Renew that
// fails with anything but "unknown subscription" (here a plain HTTP 404,
// as a camera that rebooted answers for an address it forgot) is retried,
// then the subscription is created again, instead of renewing forever with
// no PullMessages in between.
func TestEvents_RenewFailureRecreates(t *testing.T) {
	sim, cam := newSim(t, onviftest.Config{})
	var renews atomic.Int32
	cam = faultProxy(t, sim, cam, func(body string) (int, string, bool) {
		if strings.Contains(body, ":Renew>") && renews.Load() == 0 {
			return http.StatusNotFound, "not found", true
		}
		return 0, "", false
	})
	opts := fastOptions()
	sub := subscribe(t, cam, opts)
	if !waitFor(opts.RenewAt+4*opts.UnhealthyAfter, func() bool { return sim.Counts().Creates >= 2 }) {
		t.Fatalf("counts = %+v after a renew that answered 404; want the subscription created again", sim.Counts())
	}
	renews.Store(1)
	pulls := sim.Counts().Pulls
	if !waitFor(2*opts.UnhealthyAfter, func() bool { return sim.Counts().Pulls > pulls+2 }) {
		t.Errorf("pulls %d → %d after the create; want the subscription pulling", pulls, sim.Counts().Pulls)
	}
	if !waitFor(2*opts.UnhealthyAfter, sub.Healthy) {
		t.Error("Healthy() = false after the subscription was created again")
	}
	if n := sim.Subscriptions(); n != 1 {
		t.Errorf("the camera holds %d subscriptions; want 1", n)
	}
}
