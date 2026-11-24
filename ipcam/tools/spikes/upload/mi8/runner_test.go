package mi8

import (
	"bytes"
	"crypto/md5" //nolint:gosec // G501: the test's digest server
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// jpegAt is a 320 × 180 JPEG with a bright square at x.
func jpegAt(t *testing.T, x int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 320, 180))
	for y := 40; y < 120; y++ {
		for i := x; i < x+80 && i < 320; i++ {
			img.SetGray(i, y, color.Gray{Y: 250})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// snapshotCam serves snapshots behind basic auth; scene picks the picture.
type snapshotCam struct {
	*httptest.Server
	scene   atomic.Int32
	fetches atomic.Int32
}

func startSnapshotCam(t *testing.T, user, pass string) *snapshotCam {
	t.Helper()
	pics := [][]byte{jpegAt(t, 0), jpegAt(t, 200)}
	c := &snapshotCam{}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="cam"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		c.fetches.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(pics[c.scene.Load()%2])
	}))
	t.Cleanup(c.Close)
	return c
}

// session is a run state with a session begun now.
func session(target time.Duration) *State {
	s := &State{RunID: "mi8-test", StartMs: time.Now().UnixMilli(), TargetMs: target.Milliseconds()}
	s.BeginSession(time.Now().UnixMilli(), "boot", func(a, b string) bool { return a == b })
	return s
}

// TestMI8_SnapshotFallbackOnArmv7 (P41): armv7 has no pinned FFmpeg, so MI8
// runs on the ONVIF snapshot diff; a substream that does not decode falls
// back to it too. The snapshot reference finds the episodes, and the
// camera's motion events, through the real ipcam-hub diag onvif, match them.
func TestMI8_SnapshotFallbackOnArmv7(t *testing.T) {
	if _, ok := PinFor("linux", "arm"); ok {
		t.Fatal("armv7 has a pinned FFmpeg")
	}
	if _, err := (Fetcher{}).Fetch(t.Context()); err != ErrNoPin {
		t.Errorf("a fetch without a pin = %v, want ErrNoPin", err)
	}
	for _, tc := range []struct{ name, ffmpeg string }{{"armv7", ""}, {"substream that does not decode", "silent"}} {
		t.Run(tc.name, func(t *testing.T) {
			snap := startSnapshotCam(t, testUser, testPass)
			args := []string{"-snapshot", snap.URL + "/snap.jpg"}
			ffmpeg := ""
			if tc.ffmpeg != "" {
				rtsp := startRTSPCam(t, testUser, testPass)
				args = append(args, "-stream", rtsp.URL())
				ffmpeg, _ = installFakeFFmpeg(t, tc.ffmpeg)
			}
			s := startSim(t, testUser, testPass, args, "Hikvision/DS-2CD2043")
			st := session(12 * time.Second)
			st.Cameras = []CameraState{{Camera: "real-1", Brand: "Hikvision", Model: "DS-2CD2043"}}
			var mu sync.Mutex
			var last State
			cfg := Mi8Config{Hub: Hub{Bin: hubBin(t)}, Cameras: []CameraRef{{ID: "real-1", XAddr: s.XAddrs[0], User: testUser, Pass: testPass}},
				State: st, FFmpeg: ffmpeg, Params: EpisodeParams{QuietGap: time.Second}, ProgressEvery: 200 * time.Millisecond,
				FirstFrame: 2 * time.Second, Resubscribe: time.Second,
				Progress: func(s State) error { mu.Lock(); last = s; mu.Unlock(); return nil }}
			done := make(chan results.Envelope, 1)
			go func() {
				env, err := Run(t.Context(), cfg)
				if err != nil {
					t.Errorf("Run: %v", err)
				}
				done <- env
			}()
			until(t, 20*time.Second, "the subscription", func() bool { return s.counts()[0].Subscriptions == 1 })
			until(t, 20*time.Second, "the first snapshots", func() bool { return snap.fetches.Load() > 2 })
			// Two scene changes, 3 s apart, each with the camera's motion event.
			for range 2 {
				time.Sleep(3 * time.Second)
				snap.scene.Add(1)
				s.do("emit 0 1")
			}
			env := <-done
			c := env.MI8.Cameras[0]
			if c.ReferenceMethod != results.ReferenceSnapshot {
				t.Errorf("reference = %q, want snapshot", c.ReferenceMethod)
			}
			if c.Episodes < 1 || c.Matched != c.Episodes {
				t.Errorf("episodes %d, matched %d: want at least 1, all matched", c.Episodes, c.Matched)
			}
			if c.RefLagMs < 500 || c.RefLagMs > 3000 {
				t.Errorf("L = %d ms, want 0.5–3 s", c.RefLagMs)
			}
			mu.Lock()
			if last.HeartbeatMs == 0 || last.ObservedMs() < 12000 {
				t.Errorf("the last progress: heartbeat %d, observed %d ms", last.HeartbeatMs, last.ObservedMs())
			}
			mu.Unlock()
			until(t, 20*time.Second, "the unsubscribe", func() bool { return s.counts()[0].Subscriptions == 0 })
		})
	}
}

// TestMI8_SubscribeFailedReportedSeparately: a camera whose subscription
// cannot be created (here: its password refused) is reported as
// «abonare eșuată (configurare)», apart from coverage, while the other
// camera runs.
func TestMI8_SubscribeFailedReportedSeparately(t *testing.T) {
	s := startSim(t, testUser, testPass, nil, "Hikvision/A", "Dahua/B")
	st := session(6 * time.Second)
	st.Cameras = []CameraState{{Camera: "real-1", Brand: "Hikvision", Model: "A"}, {Camera: "real-2", Brand: "Dahua", Model: "B"}}
	cfg := Mi8Config{Hub: Hub{Bin: hubBin(t)}, State: st, ProgressEvery: 200 * time.Millisecond, Resubscribe: time.Second,
		Cameras: []CameraRef{{ID: "real-1", XAddr: s.XAddrs[0], User: testUser, Pass: testPass},
			{ID: "real-2", XAddr: s.XAddrs[1], User: testUser, Pass: "wrong-password"}}}
	env, err := Run(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if env.MI8.Cameras[0].SubscribeFailed || !env.MI8.Cameras[1].SubscribeFailed {
		t.Fatalf("subscribe failed = %v, %v; want false, true", env.MI8.Cameras[0].SubscribeFailed, env.MI8.Cameras[1].SubscribeFailed)
	}
	env.Host, env.Set, env.OS, env.Storage = "pi", "pi", "linux", "ssd"
	j := Judge([]results.Envelope{env})
	var row string
	for _, r := range j.Rows {
		if strings.HasPrefix(r.Value, "real-2") {
			row = r.Value
		}
	}
	if !strings.Contains(row, "abonare eșuată (configurare)") {
		t.Errorf("real-2's row = %q", row)
	}
	if j.Verdict != results.Incomplete {
		t.Errorf("verdict %q, want fără verdict (no camera with 20 episodes)", j.Verdict)
	}
	if c := s.counts(); c[1].Creates != 0 || c[1].Subscriptions != 0 {
		t.Errorf("the refused camera holds %+v", c[1])
	}
}

// TestSnapshot_DigestAuth: the snapshot fetch answers an HTTP digest
// challenge (qop auth) with the camera's credentials.
func TestSnapshot_DigestAuth(t *testing.T) {
	pic := jpegAt(t, 10)
	const realm, nonce = "cam", "abc123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := r.Header.Get("Authorization")
		p := map[string]string{}
		for _, m := range digestParam.FindAllStringSubmatch(strings.TrimPrefix(a, "Digest "), -1) {
			v := m[2]
			if v == "" {
				v = m[3]
			}
			p[m[1]] = v
		}
		h := func(s string) string { d := md5.Sum([]byte(s)); return hex.EncodeToString(d[:]) } //nolint:gosec // digest
		want := h(fmt.Sprintf("%s:%s:%s:%s:auth:%s", h(testUser+":"+realm+":"+testPass), nonce, p["nc"], p["cnonce"], h("GET:"+r.URL.RequestURI())))
		if p["response"] != want {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm="%s", nonce="%s", qop="auth", algorithm=MD5`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(pic)
	}))
	defer srv.Close()
	src := &SnapshotSource{URI: srv.URL + "/onvif/snapshot?ch=1", User: testUser, Pass: testPass}
	frames, err := src.Frames(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f := <-frames
	if len(f.Gray) != FrameSize || f.Gray[40*FrameWidth+10] < 200 {
		t.Errorf("the frame is not the snapshot's picture")
	}
}

// TestMI8_LaterSubscribeFailedKeepsSeries: a camera that subscribed once in
// the run, shown by its subscribed line or by any event, keeps its series
// when a later subscription fails (a camera still booting after a power
// cut): it is judged on its episodes, never «abonare eșuată».
func TestMI8_LaterSubscribeFailedKeepsSeries(t *testing.T) {
	for _, first := range []string{KindSubscribed, KindEvent} {
		t.Run(first, func(t *testing.T) {
			st := CameraState{Camera: "real-1", Brand: "Hikvision", Model: "X"}
			for i := range 30 {
				at := int64(i) * time.Hour.Milliseconds()
				st.Episodes = append(st.Episodes, results.EpisodeTime{StartMs: at, EndMs: at + 5000, Matched: true})
			}
			tr := &tracker{st: &st, lag: func() time.Duration { return 0 }}
			if first == KindEvent {
				tr.onEvent(time.Now().UnixMilli(), false)
			} else {
				tr.onSignal(first)
			}
			tr.onSignal(KindSubscribeFailed)
			c := tr.snapshot().Result()
			if c.SubscribeFailed || c.Episodes != 30 {
				t.Fatalf("result = failed %v, %d episodes; want the 30 episodes judged", c.SubscribeFailed, c.Episodes)
			}
			s := &State{RunID: "r", Sessions: []Session{{StartMs: 0, EndMs: 73 * time.Hour.Milliseconds()}}, Cameras: []CameraState{tr.snapshot()}}
			env := s.Envelope(Meta{Host: "pi", Set: "pi", OS: "linux", Storage: "ssd"}, 73*time.Hour.Milliseconds())
			if j := Judge([]results.Envelope{env}); j.Verdict != results.Pass {
				t.Errorf("verdict %q, rows %+v; want trece", j.Verdict, j.Rows)
			}
		})
	}
	st := CameraState{Camera: "real-2"}
	tr := &tracker{st: &st, lag: func() time.Duration { return 0 }}
	tr.onSignal(KindSubscribeFailed)
	if !tr.snapshot().Result().SubscribeFailed {
		t.Error("a camera that never subscribed is not «abonare eșuată»")
	}
}

// connGate is a TCP front of an address that refuses every connection until
// opened, as a camera still booting does.
type connGate struct {
	ln       net.Listener
	open     atomic.Bool
	refusals atomic.Int32
}

func startConnGate(t *testing.T, target string) *connGate {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := &connGate{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if !g.open.Load() {
				g.refusals.Add(1)
				_ = c.Close()
				continue
			}
			go func() {
				defer func() { _ = c.Close() }()
				u, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer func() { _ = u.Close() }()
				go func() { _, _ = io.Copy(u, c) }()
				_, _ = io.Copy(c, u)
			}()
		}
	}()
	return g
}

// TestMI8_UnreachableAtStartIsAskedAgain: a camera that does not answer when
// the session starts (subscribe_failed unreachable, no URIs) is asked again
// after the pause, unlike one that refuses the login: once it answers it is
// subscribed and not «abonare eșuată», and its URIs are read again, so its
// reference runs.
func TestMI8_UnreachableAtStartIsAskedAgain(t *testing.T) {
	snap := startSnapshotCam(t, testUser, testPass)
	s := startSim(t, testUser, testPass, []string{"-snapshot", snap.URL + "/snap.jpg"}, "Hikvision/A")
	g := startConnGate(t, s.Hosts[0])
	xaddr := strings.Replace(s.XAddrs[0], s.Hosts[0], g.ln.Addr().String(), 1)
	st := session(8 * time.Second)
	st.Cameras = []CameraState{{Camera: "real-1", Brand: "Hikvision", Model: "A"}}
	var failedSeen atomic.Bool
	cfg := Mi8Config{Hub: Hub{Bin: hubBin(t)}, State: st, ProgressEvery: 100 * time.Millisecond, Resubscribe: 500 * time.Millisecond,
		Cameras: []CameraRef{{ID: "real-1", XAddr: xaddr, User: testUser, Pass: testPass}},
		Progress: func(s State) error {
			if s.Cameras[0].SubscribeFailed {
				failedSeen.Store(true)
			}
			return nil
		}}
	type ran struct {
		env results.Envelope
		err error
	}
	done := make(chan ran, 1)
	go func() {
		env, err := Run(t.Context(), cfg)
		done <- ran{env, err}
	}()
	until(t, 6*time.Second, "the failed subscription", failedSeen.Load)
	g.open.Store(true)
	until(t, 6*time.Second, "the subscription once the camera answers", func() bool { return s.counts()[0].Subscriptions == 1 })
	until(t, 6*time.Second, "the snapshots once the URIs are read again", func() bool { return snap.fetches.Load() > 0 })
	got := <-done
	if got.err != nil {
		t.Fatalf("Run: %v", got.err)
	}
	env := got.env
	if c := env.MI8.Cameras[0]; c.SubscribeFailed || c.ReferenceMethod != results.ReferenceSnapshot {
		t.Errorf("real-1 = failed %v, reference %q; want subscribed, on snapshots", c.SubscribeFailed, c.ReferenceMethod)
	}
	until(t, 20*time.Second, "the unsubscribe", func() bool { return s.counts()[0].Subscriptions == 0 })
}
