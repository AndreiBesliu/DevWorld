package events

import (
	"errors"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/cameras/events/onviftest"
)

// TestEvents_ClockSkewUsesOffset (§8.2, P42): a camera whose clock is 2 h
// ahead still accepts the WS-UsernameToken digest, since Created is shifted
// by GetSystemDateAndTime's offset.
func TestEvents_ClockSkewUsesOffset(t *testing.T) {
	sim, cam := newSim(t, onviftest.Config{ClockSkew: 2 * time.Hour, WSSEOnly: true})
	res, err := Probe(t.Context(), cam, fastOptions())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if res.Auth != AuthWSSEDigest {
		t.Errorf("Auth = %q; want %q", res.Auth, AuthWSSEDigest)
	}
	if off := time.Duration(res.ClockOffsetMs) * time.Millisecond; off < 2*time.Hour-5*time.Second || off > 2*time.Hour+5*time.Second {
		t.Errorf("ClockOffsetMs = %d; want about 2 h", res.ClockOffsetMs)
	}
	sub := subscribe(t, cam, fastOptions())
	if !sub.Healthy() {
		t.Error("Healthy() = false on a skewed camera")
	}
	for _, r := range sim.Requests() {
		if r.Credentialed() && !r.Accepted {
			t.Errorf("request %s with %s was refused", r.Op, r.Auth)
		}
	}
}

// TestEvents_DigestOnlyCamera (§8.2): a camera that refuses the
// WS-UsernameToken falls back to HTTP digest, and keeps using it.
func TestEvents_DigestOnlyCamera(t *testing.T) {
	sim, cam := newSim(t, onviftest.Config{DigestOnly: true, Brand: "Acme", Model: "X1"})
	res, err := Probe(t.Context(), cam, fastOptions())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if res.Auth != AuthHTTPDigest || res.Brand != "Acme" || res.Model != "X1" || !res.EventsService || !res.PullPoint {
		t.Errorf("Probe = %+v; want http-digest, Acme X1, events with PullPoint", res)
	}
	sub := subscribe(t, cam, fastOptions())
	sim.Emit("tns1:VideoSource/MotionAlarm", true)
	select {
	case e := <-sub.Events():
		if !e.Motion {
			t.Errorf("event = %+v; want motion", e)
		}
	case <-time.After(time.Second):
		t.Fatal("no event from a digest-only camera")
	}
	refused := 0
	for _, r := range sim.Requests() {
		if r.Credentialed() && !r.Accepted {
			refused++
		}
	}
	// Each login (Probe's, Subscribe's) tries the WS-UsernameToken once;
	// its later requests go straight to digest.
	if refused > 2 {
		t.Errorf("%d refused requests; want the WS-UsernameToken tried once per login", refused)
	}
}

// TestEvents_NeverSendsPasswordText (§8.2): no request carries the
// WS-UsernameToken PasswordText, nor the password in clear, whatever the
// camera accepts or refuses.
func TestEvents_NeverSendsPasswordText(t *testing.T) {
	for _, cfg := range []onviftest.Config{{}, {DigestOnly: true}, {WSSEOnly: true}, {ClockSkew: time.Hour}} {
		sim, cam := newSim(t, cfg)
		_, _ = Probe(t.Context(), cam, fastOptions())
		_, _ = StreamURI(t.Context(), cam, "sub", fastOptions())
		if sub, err := Subscribe(t.Context(), cam, fastOptions()); err == nil {
			time.Sleep(2 * minute)
			_ = sub.Close(t.Context())
		}
		wrong := cam
		wrong.Pass = "SENT" + "INEL-wrong"
		_, _ = Probe(t.Context(), wrong, fastOptions())
		for _, r := range sim.Requests() {
			if r.Auth == onviftest.AuthWSSEText {
				t.Errorf("%+v: %s sent a PasswordText", cfg, r.Op)
			}
			for _, pass := range []string{cam.Pass, wrong.Pass} {
				if r.Contains(pass) {
					t.Errorf("%+v: %s carries the password in clear", cfg, r.Op)
				}
			}
		}
	}
}

// TestEvents_PausesAfter3AuthFailures (§8.2): after 3 refused logins the
// client sends nothing to the camera for 60 s, so the owner's camera account
// is not locked; then it tries again.
func TestEvents_PausesAfter3AuthFailures(t *testing.T) {
	sim, cam := newSim(t, onviftest.Config{})
	cam.Pass = "SENT" + "INEL-wrong"
	opts := fastOptions()
	opts.AuthPause = 5 * minute
	c := newClient(cam, opts)
	var err error
	for range 5 {
		if _, _, err = c.deviceInformation(t.Context()); errors.Is(err, ErrAuthPaused) {
			break
		}
		if !errors.Is(err, ErrAuth) {
			t.Fatalf("err = %v; want ErrAuth", err)
		}
	}
	if !errors.Is(err, ErrAuthPaused) {
		t.Fatalf("err = %v; want ErrAuthPaused after 3 refusals", err)
	}
	if got := sim.Refused(); got != 3 {
		t.Errorf("the camera refused %d logins; want 3 before the pause", got)
	}
	sent := len(sim.Requests())
	for range 3 {
		if _, _, err := c.deviceInformation(t.Context()); !errors.Is(err, ErrAuthPaused) {
			t.Errorf("err = %v during the pause; want ErrAuthPaused", err)
		}
	}
	if got := len(sim.Requests()); got != sent {
		t.Errorf("%d requests during the pause; want 0", got-sent)
	}
	if Reason(ErrAuthPaused) != ReasonAuth {
		t.Errorf("Reason(ErrAuthPaused) = %q; want %q", Reason(ErrAuthPaused), ReasonAuth)
	}
	time.Sleep(opts.AuthPause)
	if _, _, err := c.deviceInformation(t.Context()); !errors.Is(err, ErrAuth) || errors.Is(err, ErrAuthPaused) {
		t.Errorf("err = %v after the pause; want a new attempt refused with ErrAuth", err)
	}
	if got := len(sim.Requests()); got == sent {
		t.Error("no request after the pause")
	}
}
