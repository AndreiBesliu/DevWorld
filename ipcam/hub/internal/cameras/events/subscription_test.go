package events

import (
	"context"
	"net"
	"net/url"
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
	s, ok := nextSignal(t, sub, opts.UnhealthyAfter+opts.UnhealthyAfter/2)
	if !ok || s.Kind != SignalUnhealthy || s.Cam != cam.ID {
		t.Fatalf("signal = %+v, %v; want %s for %s", s, ok, SignalUnhealthy, cam.ID)
	}
	if after := time.Since(lostAt); after > opts.UnhealthyAfter+opts.UnhealthyAfter/2 {
		t.Errorf("events_unhealthy came %v after the loss; want within %v", after, opts.UnhealthyAfter)
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
