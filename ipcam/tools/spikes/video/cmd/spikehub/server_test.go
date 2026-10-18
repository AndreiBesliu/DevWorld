package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

func TestCameras_ListsSimsAndCodecs(t *testing.T) {
	hub := newTestHub(t, 16)

	resp, body := hub.do(t, http.MethodGet, "/spike/v1/cameras", nil, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, content type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var got []struct {
		ID           string   `json:"id"`
		Camera       int      `json:"camera"`
		Codec        string   `json:"codec"`
		ProfileLevel string   `json:"profileLevel"`
		Profiles     []string `json:"profiles"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	if len(got) != 18 {
		t.Fatalf("%d cameras, want 18: %s", len(got), body)
	}
	for i := range 16 {
		c := got[i]
		if want := fmt.Sprintf("sim-%02d", i+1); c.ID != want || c.Camera != i+1 || c.Codec != "H264" ||
			c.ProfileLevel != "" || strings.Join(c.Profiles, ",") != "sub,main" {
			t.Errorf("camera %d = %+v, want %s, camera %d, H264, profiles sub and main", i, c, want, i+1)
		}
	}
	if c := got[16]; c.ID != "mx-h265" || c.Camera != 17 || c.Codec != "H265" || c.ProfileLevel != "" || strings.Join(c.Profiles, ",") != "main,sub" {
		t.Errorf("camera 16 = %+v, want mx-h265, camera 17, H265, profiles main and sub", c)
	}
	if c := got[17]; c.ID != "mx-high51" || c.Camera != 18 || c.Codec != "H264" || c.ProfileLevel != "640033" || strings.Join(c.Profiles, ",") != "main,sub" {
		t.Errorf("camera 17 = %+v, want mx-high51, camera 18, H264, profile level 640033, profiles main and sub", c)
	}

	// The wire form is the one of the plan: this field order, profileLevel only on mx-high51.
	for _, want := range []string{
		`{"id":"sim-01","camera":1,"codec":"H264","profiles":["sub","main"]}`,
		`{"id":"mx-h265","camera":17,"codec":"H265","profiles":["main","sub"]}`,
		`{"id":"mx-high51","camera":18,"codec":"H264","profileLevel":"640033","profiles":["main","sub"]}`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("cameras lack %s:\n%s", want, body)
		}
	}
	if n := strings.Count(string(body), "profileLevel"); n != 1 {
		t.Errorf("%d cameras carry a profileLevel, want 1", n)
	}
}

func TestCameras_SimsFlagSetsTheNumberOfSimCameras(t *testing.T) {
	hub := newTestHub(t, 4)

	_, body := hub.do(t, http.MethodGet, "/spike/v1/cameras", nil, nil)
	var got []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	if want := "sim-01,sim-02,sim-03,sim-04,mx-h265,mx-high51"; strings.Join(ids, ",") != want {
		t.Errorf("cameras = %v, want %s", ids, want)
	}
}

// offerClient plays the spike app: it offers recvonly video transceivers and the
// rpc data channel.
type offerClient struct {
	pc    *webrtc.PeerConnection
	dc    *webrtc.DataChannel
	offer string
	open  chan struct{}
	msgs  chan string
}

func newOfferClient(t *testing.T, slots int) *offerClient {
	t.Helper()
	me := &webrtc.MediaEngine{}
	if err := me.RegisterDefaultCodecs(); err != nil {
		t.Fatal(err)
	}
	var se webrtc.SettingEngine
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	for range slots {
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
			t.Fatal(err)
		}
	}
	c := &offerClient{pc: pc, open: make(chan struct{}), msgs: make(chan string, 64)}
	if c.dc, err = pc.CreateDataChannel("rpc", nil); err != nil {
		t.Fatal(err)
	}
	c.dc.OnOpen(func() { close(c.open) })
	c.dc.OnMessage(func(m webrtc.DataChannelMessage) { c.msgs <- string(m.Data) })

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	c.offer = pc.LocalDescription().SDP
	return c
}

// connect posts the offer to the hub and applies the answer.
func (c *offerClient) connect(t *testing.T, hub *testHub) {
	t.Helper()
	resp, body := hub.do(t, http.MethodPost, "/spike/v1/offer", strings.NewReader(c.offer), http.Header{"Content-Type": {"application/sdp"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("offer: status %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/sdp" {
		t.Errorf("offer: content type %q, want application/sdp", ct)
	}
	if err := c.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(body)}); err != nil {
		t.Fatalf("the answer does not apply: %v\n%s", err, body)
	}
	if n := strings.Count(string(body), "\r\nm=video "); n != len(c.pc.GetTransceivers()) {
		t.Errorf("answer has %d video m-lines, want %d", n, len(c.pc.GetTransceivers()))
	}
	select {
	case <-c.open:
	case <-time.After(30 * time.Second):
		t.Fatal("the rpc channel did not open")
	}
}

// next returns the next rpc message of the given type.
func (c *offerClient) next(t *testing.T, typ string) string {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case m := <-c.msgs:
			var head struct {
				T string `json:"t"`
			}
			if err := json.Unmarshal([]byte(m), &head); err != nil {
				t.Fatalf("%v: %s", err, m)
			}
			if head.T == typ {
				return m
			}
		case <-deadline:
			t.Fatalf("no %q message within 10 s", typ)
		}
	}
}

func TestOffer_ReturnsSDPAnswer(t *testing.T) {
	hub := newTestHub(t, 4)
	c := newOfferClient(t, 2)

	c.connect(t, hub)

	// The sources map has the keys "<camera>/<profile>" (R2): binds by those names work.
	if err := c.dc.SendText(`{"t":"bind","slot":0,"cam":"sim-01","profile":"sub","epoch":7}`); err != nil {
		t.Fatal(err)
	}
	if m := c.next(t, "bound"); !strings.HasPrefix(m, `{"t":"bound","slot":0,"epoch":7,"codec":"H264","profile":"4d401f"`) {
		t.Errorf("bound = %s", m)
	}
	if err := c.dc.SendText(`{"t":"bind","slot":1,"cam":"mx-h265","profile":"main","epoch":8}`); err != nil {
		t.Fatal(err)
	}
	if m := c.next(t, "bound"); !strings.HasPrefix(m, `{"t":"bound","slot":1,"epoch":8,"codec":"H265","profile":"017b"`) {
		t.Errorf("bound = %s", m)
	}
}

func TestOffer_RejectsBadRequests(t *testing.T) {
	hub := newTestHub(t, 1)
	post := func(body string) int {
		t.Helper()
		resp, _ := hub.do(t, http.MethodPost, "/spike/v1/offer", strings.NewReader(body), http.Header{"Content-Type": {"application/sdp"}})
		return resp.StatusCode
	}

	if got := post(""); got != http.StatusBadRequest {
		t.Errorf("empty offer: status %d, want 400", got)
	}
	if got := post("this is not an SDP"); got != http.StatusBadRequest {
		t.Errorf("garbage offer: status %d, want 400", got)
	}
	if got := post(strings.Repeat("a=x\r\n", 100_000)); got != http.StatusRequestEntityTooLarge {
		t.Errorf("500 kB offer: status %d, want 413", got)
	}
	// The offer is POST only; a GET falls through to the app, which has no such file.
	if resp, _ := hub.do(t, http.MethodGet, "/spike/v1/offer", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET offer: status %d, want 404", resp.StatusCode)
	}
	if resp, _ := hub.do(t, http.MethodPost, "/spike/v1/cameras", nil, nil); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST cameras: status %d, want 405", resp.StatusCode)
	}
	if n := hub.activeSessions(); n != 0 {
		t.Errorf("%d sessions after rejected offers, want 0", n)
	}
}

// The server keeps the sessions it answered and drops each one when it closes
// (Session.Done).
func TestSessions_AreDroppedWhenTheyClose(t *testing.T) {
	hub := newTestHub(t, 1)
	first, second := newOfferClient(t, 1), newOfferClient(t, 1)
	first.connect(t, hub)
	second.connect(t, hub)
	if n := hub.activeSessions(); n != 2 {
		t.Fatalf("%d sessions after two offers, want 2", n)
	}

	if err := first.pc.Close(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the server drops the session whose viewer left", func() bool { return hub.activeSessions() == 1 })

	hub.Close()
	waitUntil(t, "the server closes the remaining session", func() bool { return hub.activeSessions() == 0 })
	if resp, _ := hub.do(t, http.MethodPost, "/spike/v1/offer", strings.NewReader(second.offer), nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("offer after Close: status %d, want 503", resp.StatusCode)
	}
}

func TestCORS_AllowsOnlyListedOrigins(t *testing.T) {
	hub := newTestHub(t, 1)

	for _, origin := range []string{"http://localhost:7080", "http://localhost", "http://tauri.localhost", "https://tauri.localhost"} {
		resp, _ := hub.do(t, http.MethodGet, "/spike/v1/cameras", nil, http.Header{"Origin": {origin}})
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("GET from %s: Access-Control-Allow-Origin = %q, want the origin", origin, got)
		}
		if !strings.Contains(resp.Header.Get("Vary"), "Origin") {
			t.Errorf("GET from %s: Vary = %q, want Origin", origin, resp.Header.Get("Vary"))
		}

		resp, _ = hub.do(t, http.MethodOptions, "/spike/v1/offer", nil, http.Header{
			"Origin":                         {origin},
			"Access-Control-Request-Method":  {"POST"},
			"Access-Control-Request-Headers": {"content-type"},
		})
		if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != origin ||
			!strings.Contains(resp.Header.Get("Access-Control-Allow-Methods"), "POST") ||
			!strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Content-Type") {
			t.Errorf("preflight from %s: status %d, headers %v", origin, resp.StatusCode, resp.Header)
		}
	}

	for _, origin := range []string{
		"http://evil.example", "https://localhost", "http://localhost:7081", "http://localhost.evil.example", "null",
	} {
		resp, _ := hub.do(t, http.MethodGet, "/spike/v1/cameras", nil, http.Header{"Origin": {origin}})
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("GET from %s: Access-Control-Allow-Origin = %q, want none", origin, got)
		}
		resp, _ = hub.do(t, http.MethodOptions, "/spike/v1/offer", nil, http.Header{
			"Origin":                        {origin},
			"Access-Control-Request-Method": {"POST"},
		})
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" || resp.StatusCode < 400 {
			t.Errorf("preflight from %s: status %d, Access-Control-Allow-Origin %q, want a refusal", origin, resp.StatusCode, got)
		}
	}
}

// A page of another origin cannot write a results file or open a session with a
// request that a browser sends without a preflight.
func TestCORS_RefusesAPostFromAnotherOrigin(t *testing.T) {
	hub := newTestHub(t, 1)
	const body = `{"device":"x","scenario":"S1"}`

	resp, _ := hub.do(t, http.MethodPost, "/spike/v1/results", strings.NewReader(body),
		http.Header{"Origin": {"http://evil.example"}, "Content-Type": {"text/plain"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST from http://evil.example: status %d, want 403", resp.StatusCode)
	}
	if files := resultFiles(t, hub.resultsDir); len(files) != 0 {
		t.Errorf("the refused POST wrote %v", files)
	}

	// The app served by the hub itself is not another origin, at whatever address
	// the phone reached it.
	u, err := url.Parse(hub.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = hub.do(t, http.MethodPost, "/spike/v1/results", strings.NewReader(body),
		http.Header{"Origin": {"http://" + u.Host}})
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("POST from the hub's own origin: status %d, want 201", resp.StatusCode)
	}
	resp, _ = hub.do(t, http.MethodPost, "/spike/v1/results", strings.NewReader(body),
		http.Header{"Origin": {"http://tauri.localhost"}})
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("POST from http://tauri.localhost: status %d, want 201", resp.StatusCode)
	}
}

func TestHLS_ServesRanges(t *testing.T) {
	hub := newTestHub(t, 1)

	resp, body := hub.do(t, http.MethodGet, "/media/hls/seg-00000.m4s", nil, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Accept-Ranges") != "bytes" || len(body) != len(hlsSegment) {
		t.Fatalf("whole segment: status %d, Accept-Ranges %q, %d bytes", resp.StatusCode, resp.Header.Get("Accept-Ranges"), len(body))
	}
	if got := resp.Header.Get("Content-Type"); got != "video/iso.segment" {
		t.Errorf("segment content type = %q, want video/iso.segment", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store: a cached start would shorten the HLS start time that MV10 measures", got)
	}

	resp, body = hub.do(t, http.MethodGet, "/media/hls/seg-00000.m4s", nil, http.Header{"Range": {"bytes=100-199"}})
	if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Range") != fmt.Sprintf("bytes 100-199/%d", len(hlsSegment)) {
		t.Fatalf("range: status %d, Content-Range %q", resp.StatusCode, resp.Header.Get("Content-Range"))
	}
	if string(body) != string(hlsSegment[100:200]) {
		t.Error("range: wrong bytes")
	}

	resp, body = hub.do(t, http.MethodGet, "/media/hls/seg-00000.m4s", nil, http.Header{"Range": {"bytes=-16"}})
	if resp.StatusCode != http.StatusPartialContent || string(body) != string(hlsSegment[len(hlsSegment)-16:]) {
		t.Errorf("suffix range: status %d, %d bytes", resp.StatusCode, len(body))
	}

	resp, _ = hub.do(t, http.MethodGet, "/media/hls/index.m3u8", nil, nil)
	if got := resp.Header.Get("Content-Type"); got != "application/vnd.apple.mpegurl" {
		t.Errorf("playlist content type = %q", got)
	}
	resp, _ = hub.do(t, http.MethodGet, "/media/hls/init.mp4", nil, nil)
	if got := resp.Header.Get("Content-Type"); got != "video/mp4" {
		t.Errorf("init segment content type = %q", got)
	}
	if resp, _ = hub.do(t, http.MethodGet, "/media/hls/nope.m4s", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing segment: status %d, want 404", resp.StatusCode)
	}
}

func TestHLS_ServesNothingOutsideTheRecording(t *testing.T) {
	hub := newTestHub(t, 1)
	for _, path := range []string{"/media/hls/../sub.h264", "/media/hls/%2e%2e/sub.h264", "/media/hls/..%2fsub.h264", "/media/sub.h264"} {
		resp, body := hub.do(t, http.MethodGet, path, nil, nil)
		if resp.StatusCode == http.StatusOK || strings.Contains(string(body), "\x00\x00\x00\x01") {
			t.Errorf("GET %s: status %d, served a media file", path, resp.StatusCode)
		}
	}
}

func TestApp_ServesTheEmbeddedFiles(t *testing.T) {
	hub := newTestHub(t, 1)

	resp, body := hub.do(t, http.MethodGet, "/", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "<title>spike</title>") {
		t.Errorf("GET /: status %d, body %q", resp.StatusCode, body)
	}
	resp, body = hub.do(t, http.MethodGet, "/assets/app.js", nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "console.log") {
		t.Errorf("GET /assets/app.js: status %d, body %q", resp.StatusCode, body)
	}
	if resp, _ = hub.do(t, http.MethodGet, "/assets/missing.js", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /assets/missing.js: status %d, want 404", resp.StatusCode)
	}
	if resp, _ = hub.do(t, http.MethodGet, "/spike/v1/unknown", nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /spike/v1/unknown: status %d, want 404", resp.StatusCode)
	}
}

func TestApp_WithoutABuildSaysSo(t *testing.T) {
	hub := newTestHub(t, 1, func(c *hubConfig) {
		c.web = fstest.MapFS{".gitkeep": {}} // what the repository embeds
	})

	resp, body := hub.do(t, http.MethodGet, "/", nil, nil)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "Aplicația") {
		t.Errorf("GET /: status %d, body %q, want 404 and a Romanian explanation", resp.StatusCode, body)
	}
	// The API works without the app.
	if resp, _ = hub.do(t, http.MethodGet, "/spike/v1/cameras", nil, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("GET cameras: status %d", resp.StatusCode)
	}
}

// resultFiles lists the files in dir, none when it does not exist.
func resultFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
