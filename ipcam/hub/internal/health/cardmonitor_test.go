package health

import (
	"testing"
	"time"
)

// t0 is the fake clock's start.
var t0 = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

// fsyncs observes one probe fsync of latency every 10 s, as the sampler
// does, from from for d, and returns the last state and the time after it.
func fsyncs(m *CardMonitor, from time.Time, d, latency time.Duration) (CardHealth, time.Time) {
	state := CardOK
	at := from
	for ; at.Before(from.Add(d)); at = at.Add(10 * time.Second) {
		state, _ = m.Observe(IoEvent{Kind: IoFsync, Latency: latency}, at)
	}
	return state, at
}

// TestCardMonitor_Thresholds (§13.1 „Card SD”, T40, G17), on a fake clock:
// OK is 0 I/O errors in 24 h and fsync p99 ≤ 500 ms over 10 min; DEGRADED
// is one I/O error in 24 h, or fsync p99 > 500 ms over 10 min; FAILED is 3
// I/O errors in 60 s, or a read-only remount. The alert (§13.3) is raised
// once, when the row leaves OK, and not again while it stays raised.
func TestCardMonitor_Thresholds(t *testing.T) {
	t.Run("one EIO is DEGRADED and raises the alert", func(t *testing.T) {
		m := NewCardMonitor()
		if state, alert := m.Observe(IoEvent{Kind: IoFsync, Latency: 20 * time.Millisecond}, t0); state != CardOK || alert {
			t.Fatalf("a fast fsync: %v, alert %v; want OK, no alert", state, alert)
		}
		state, alert := m.Observe(IoEvent{Kind: IoEIO}, t0.Add(time.Second))
		if state != CardDegraded || !alert {
			t.Errorf("one EIO: %v, alert %v; want DEGRADED and the alert", state, alert)
		}
		if s := m.Stats(t0.Add(time.Second)); s.IOErrors24h != 1 || s.IOErrors60s != 1 || s.ReadOnly {
			t.Errorf("stats %+v, want one error in 24 h and in 60 s", s)
		}
	})

	t.Run("three EIO within 60 s are FAILED, without a second alert", func(t *testing.T) {
		m := NewCardMonitor()
		var states []CardHealth
		alerts := 0
		for _, at := range []time.Duration{0, 30 * time.Second, 59 * time.Second} {
			state, alert := m.Observe(IoEvent{Kind: IoEIO}, t0.Add(at))
			states = append(states, state)
			if alert {
				alerts++
			}
		}
		if want := []CardHealth{CardDegraded, CardDegraded, CardFailed}; !equal(states, want) {
			t.Errorf("states %v, want %v", states, want)
		}
		if alerts != 1 {
			t.Errorf("%d alerts, want 1", alerts)
		}
		if s := m.Stats(t0.Add(59 * time.Second)); s.IOErrors60s != 3 {
			t.Errorf("stats %+v, want 3 errors in 60 s", s)
		}
	})

	t.Run("three EIO spread over more than 60 s stay DEGRADED", func(t *testing.T) {
		m := NewCardMonitor()
		var state CardHealth
		for _, at := range []time.Duration{0, 40 * time.Second, 80 * time.Second} {
			state, _ = m.Observe(IoEvent{Kind: IoEIO}, t0.Add(at))
		}
		if state != CardDegraded {
			t.Errorf("state %v, want DEGRADED: no 60 s holds 3 errors", state)
		}
	})

	t.Run("erofs is FAILED at once", func(t *testing.T) {
		m := NewCardMonitor()
		state, alert := m.Observe(IoEvent{Kind: IoEROFS}, t0)
		if state != CardFailed || !alert {
			t.Errorf("erofs: %v, alert %v; want FAILED and the alert", state, alert)
		}
		if s := m.Stats(t0); !s.ReadOnly {
			t.Errorf("stats %+v, want read-only", s)
		}
	})

	t.Run("FAILED stays FAILED", func(t *testing.T) {
		m := NewCardMonitor()
		m.Observe(IoEvent{Kind: IoEROFS}, t0)
		if state, alert := m.Observe(IoEvent{Kind: IoFsync, Latency: time.Millisecond}, t0.Add(25*time.Hour)); state != CardFailed || alert {
			t.Errorf("a day later: %v, alert %v; want FAILED, no new alert", state, alert)
		}
	})

	t.Run("fsync p99 600 ms over 10 min is DEGRADED", func(t *testing.T) {
		m := NewCardMonitor()
		state, at := fsyncs(m, t0, 10*time.Minute, 600*time.Millisecond)
		if state != CardDegraded {
			t.Errorf("state %v, want DEGRADED", state)
		}
		if s := m.Stats(at); s.FsyncP99_10m != 600*time.Millisecond || s.IOErrors24h != 0 {
			t.Errorf("stats %+v, want p99 600 ms and no error", s)
		}
	})

	t.Run("fsync p99 500 ms over 10 min is OK", func(t *testing.T) {
		m := NewCardMonitor()
		if state, _ := fsyncs(m, t0, 10*time.Minute, 500*time.Millisecond); state != CardOK {
			t.Errorf("state %v, want OK: the bound is ≤ 500 ms", state)
		}
	})

	t.Run("a slow fsync older than 10 min no longer counts", func(t *testing.T) {
		m := NewCardMonitor()
		if state, _ := m.Observe(IoEvent{Kind: IoFsync, Latency: 2 * time.Second}, t0); state != CardDegraded {
			t.Fatalf("a 2 s fsync: %v, want DEGRADED", state)
		}
		if state, _ := fsyncs(m, t0.Add(10*time.Minute+time.Second), time.Minute, 50*time.Millisecond); state != CardOK {
			t.Errorf("10 min later with fast fsyncs: %v, want OK", state)
		}
	})

	t.Run("24 h without errors and p99 ≤ 500 ms is OK again", func(t *testing.T) {
		m := NewCardMonitor()
		if state, _ := m.Observe(IoEvent{Kind: IoEIO}, t0); state != CardDegraded {
			t.Fatalf("one EIO: %v, want DEGRADED", state)
		}
		if state, _ := fsyncs(m, t0.Add(23*time.Hour), 59*time.Minute, 100*time.Millisecond); state != CardDegraded {
			t.Errorf("23 h 59 min after the error: %v, want DEGRADED", state)
		}
		state, at := fsyncs(m, t0.Add(24*time.Hour+time.Second), 10*time.Minute, 100*time.Millisecond)
		if state != CardOK {
			t.Errorf("24 h after the error: %v, want OK", state)
		}
		if s := m.Stats(at); s.IOErrors24h != 0 || s.FsyncP99_10m != 100*time.Millisecond {
			t.Errorf("stats %+v, want no error and p99 100 ms", s)
		}
		// The alert closed after 2 min of OK (§13.3): a new error raises it again.
		if _, alert := m.Observe(IoEvent{Kind: IoEIO}, at); !alert {
			t.Error("a new error after the card was OK again raised no alert")
		}
	})

	t.Run("the alert stays raised through a short OK", func(t *testing.T) {
		m := NewCardMonitor()
		m.Observe(IoEvent{Kind: IoFsync, Latency: time.Second}, t0)
		if state, _ := m.Observe(IoEvent{Kind: IoFsync, Latency: 10 * time.Millisecond}, t0.Add(10*time.Minute+time.Second)); state != CardOK {
			t.Fatalf("state %v, want OK once the slow fsync is older than 10 min", state)
		}
		if _, alert := m.Observe(IoEvent{Kind: IoFsync, Latency: time.Second}, t0.Add(11*time.Minute)); alert {
			t.Error("the alert was raised again after less than 2 min of OK")
		}
	})
}

func equal(a, b []CardHealth) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestCardHealth_String: the states as the card event names them.
func TestCardHealth_String(t *testing.T) {
	for state, want := range map[CardHealth]string{CardOK: "ok", CardDegraded: "degraded", CardFailed: "failed"} {
		if got := state.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(state), got, want)
		}
	}
}
