package handoff

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
)

// fakeDriver is a holder that awaits one login per platform.
type fakeDriver struct {
	mu      sync.Mutex
	awaited map[string]Next
	refuse  error
	cells   []string
	begun   []string
	// interruptions counts the reader page's interrupted attempts.
	interruptions int
}

func newFakeDriver() *fakeDriver {
	return &fakeDriver{awaited: map[string]Next{
		"android": {Dest: "g-android", Provider: "GOOGLE_DRIVE", Plan: "A"},
		"web":     {Dest: "g-web", Provider: "GOOGLE_DRIVE", Plan: "B1"},
	}}
}

func (d *fakeDriver) Next(p string) (Next, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n, ok := d.awaited[p]
	return n, ok
}
func (d *fakeDriver) AllowBegin(string, string, string) error { return d.refuse }
func (d *fakeDriver) Begun(dest string, _ hubproc.BeginResult) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.begun = append(d.begun, dest)
}
func (d *fakeDriver) WebCell(c string, l, r bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cells = append(d.cells, fmt.Sprintf("%s %v %v", c, l, r))
	return nil
}
func (d *fakeDriver) WebInterrupted() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.interruptions++
}
func (d *fakeDriver) WebClientID() string   { return "web-client.apps" }
func (d *fakeDriver) HandoverToken() string { return strings.Repeat("ab", 32) }
func (d *fakeDriver) Handover() int         { return http.StatusNoContent }
func (d *fakeDriver) DevicePage(w http.ResponseWriter, res hubproc.BeginResult) {
	_, _ = io.WriteString(w, "device "+res.UserCode)
}
func (d *fakeDriver) ReaderPage(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, "reader")
}
func (d *fakeDriver) CallbackPage(w http.ResponseWriter, ok bool) {
	_, _ = fmt.Fprint(w, "callback ", ok)
}
func (d *fakeDriver) Setup(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "setup") }
func (d *fakeDriver) Status(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, "status")
}

// fakeControl is a control endpoint that records each request's path and
// body.
type fakeControl struct {
	mu     sync.Mutex
	bodies map[string][]string
}

func startFakeControl(t *testing.T) (*hubproc.ControlClient, *fakeControl) {
	t.Helper()
	fc := &fakeControl{bodies: map[string][]string{}}
	token := strings.Repeat("cd", 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		fc.mu.Lock()
		fc.bodies[r.URL.Path] = append(fc.bodies[r.URL.Path], string(b))
		fc.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/control/v1/oauth/begin":
			_, _ = io.WriteString(w, `{"state":"st-1","plan":"B1","verificationUrl":"https://v.invalid","userCode":"ABCD-EFGH","expiresAt":"2026-11-17T12:10:00Z"}`)
		case "/control/v1/oauth/complete":
			_, _ = io.WriteString(w, `{"destId":"g-android","provider":"GOOGLE_DRIVE","platform":"android","plan":"A","clientType":"android","issuedAt":"2026-11-17T12:00:00Z"}`)
		case "/control/v1/visibility/targets":
			_, _ = io.WriteString(w, `[{"creator":"desktop","folderId":"f-1"}]`)
		}
	}))
	t.Cleanup(srv.Close)
	return hubproc.NewControlClient(strings.TrimPrefix(srv.URL, "http://"), token), fc
}

// freePort is a port that nothing listens on now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // a TCP listener's
}

// startServer serves a fake driver and a fake control endpoint on a free
// port's loopback, results in a temporary folder.
func startServer(t *testing.T, opts Options) (*Server, *fakeDriver, *fakeControl) {
	t.Helper()
	hub, fc := startFakeControl(t)
	d := newFakeDriver()
	opts.Driver, opts.Port = d, freePort(t)
	if opts.ResultsDir == "" {
		opts.ResultsDir = t.TempDir()
	}
	s := New(hub, opts)
	if err := s.ServeLoopback(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, d, fc
}

// do sends a request with the headers given as name, value pairs.
func do(t *testing.T, method, url, body string, headers ...string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestHandoffServer_CORSAllowsShellOriginsOnly (P40): the shells' origins
// get Access-Control-Allow-Origin on /spike/v1/oauth/*, and their
// preflight that asks for private network access gets it; another origin
// gets no CORS header and its POST 403; /spike/v1/visibility/* answers
// only the reader page's origin. Every answer sets Referrer-Policy.
func TestHandoffServer_CORSAllowsShellOriginsOnly(t *testing.T) {
	s, _, _ := startServer(t, Options{})
	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	for _, origin := range []string{"http://localhost", "http://tauri.localhost", "https://tauri.localhost"} {
		resp := do(t, http.MethodGet, base+"/spike/v1/oauth/next?platform=android", "", "Origin", origin)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != origin ||
			!slices.Contains(resp.Header.Values("Vary"), "Origin") || resp.Header.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("next from %s: %d, %v", origin, resp.StatusCode, resp.Header)
		}
		resp = do(t, http.MethodOptions, base+"/spike/v1/oauth/complete", "", "Origin", origin,
			"Access-Control-Request-Method", "POST", "Access-Control-Request-Private-Network", "true")
		if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != origin ||
			resp.Header.Get("Access-Control-Allow-Private-Network") != "true" {
			t.Errorf("preflight from %s: %d, %v", origin, resp.StatusCode, resp.Header)
		}
	}
	for _, path := range []string{"/spike/v1/oauth/next?platform=android", "/spike/v1/oauth/complete", "/spike/v1/results"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			resp := do(t, method, base+path, `{"state":"s","code":"c"}`, "Origin", "https://evil.example")
			if resp.Header.Get("Access-Control-Allow-Origin") != "" || method == http.MethodPost && resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s %s from another origin: %d, %v; want no CORS header and 403 for a POST", method, path, resp.StatusCode, resp.Header)
			}
		}
	}
	// The reader page's origin only on the visibility endpoints.
	reader := fmt.Sprintf("http://localhost:%d", s.Port())
	resp := do(t, http.MethodGet, base+"/spike/v1/visibility/targets", "", "Origin", reader)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != reader {
		t.Errorf("targets from the reader page: %d, %v", resp.StatusCode, resp.Header)
	}
	for _, origin := range []string{"http://localhost", "https://tauri.localhost"} {
		resp := do(t, http.MethodPost, base+"/spike/v1/visibility/web", `{"creator":"tv","listed":true,"read":true}`, "Origin", origin)
		if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("visibility from the shell %s: %d, %v; want 403 without CORS", origin, resp.StatusCode, resp.Header)
		}
	}
	// The app sends complete as text/plain JSON: a simple request.
	resp = do(t, http.MethodPost, base+"/spike/v1/oauth/complete", `{"state":"s","code":"c"}`, "Origin", "http://localhost",
		"Content-Type", "text/plain")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost" {
		t.Errorf("complete from the Android shell: %d, %v", resp.StatusCode, resp.Header)
	}
}

// TestHandoffServer_CORSOrigins_WebBuildAndShells (Task 9B's cannot-verify
// (a), checked in Task 9a): the video app's endpoints (/spike/v1/oauth/*,
// the cameras and the results) answer with CORS exactly the origins that the
// app runs from: the Android shell (http://localhost), the Windows shell
// (http://tauri.localhost, https://tauri.localhost) and the web build that
// spikehub or c1's reccontroller serves on the PC (http://localhost:7080,
// and http://127.0.0.1:7080, the address that spikehub opens). Their
// requests and their preflights get Access-Control-Allow-Origin; no other
// origin gets a CORS header, and each of its requests, a preflight
// included, is refused with 403. uploadspike's own pages need no CORS: their
// requests go on without a CORS header, and their preflight is refused.
func TestHandoffServer_CORSOrigins_WebBuildAndShells(t *testing.T) {
	s, _, _ := startServer(t, Options{})
	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	requests := []struct{ method, path, body string }{
		{http.MethodGet, "/spike/v1/oauth/next?platform=web", ""},
		{http.MethodGet, "/spike/v1/oauth/begin?dest=g-web&provider=GOOGLE_DRIVE&platform=web", ""},
		{http.MethodPost, "/spike/v1/oauth/complete", `{"state":"s","code":"c"}`},
		{http.MethodGet, "/spike/v1/cameras", ""},
		{http.MethodPost, "/spike/v1/results", `{"scenario":"OAUTH","device":{"platform":"chrome","ua":"x"}}`},
	}
	app := []string{"http://localhost", "http://tauri.localhost", "https://tauri.localhost", "http://localhost:7080", "http://127.0.0.1:7080"}
	for _, origin := range app {
		for _, r := range requests {
			resp := do(t, r.method, base+r.path, r.body, "Origin", origin, "Content-Type", "text/plain")
			if resp.StatusCode >= 300 || resp.Header.Get("Access-Control-Allow-Origin") != origin || !slices.Contains(resp.Header.Values("Vary"), "Origin") {
				t.Errorf("%s %s from %s: %d, %v; want its answer with CORS", r.method, r.path, origin, resp.StatusCode, resp.Header)
			}
			resp = do(t, http.MethodOptions, base+r.path, "", "Origin", origin,
				"Access-Control-Request-Method", r.method, "Access-Control-Request-Private-Network", "true")
			if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != origin ||
				resp.Header.Get("Access-Control-Allow-Private-Network") != "true" {
				t.Errorf("preflight of %s from %s: %d, %v", r.path, origin, resp.StatusCode, resp.Header)
			}
		}
	}
	others := []string{
		"https://evil.example", "null", "http://localhost:7081", "http://127.0.0.1", "https://localhost",
		"http://192.168.1.20:7080", "http://localhost:7080.evil.example", "http://tauri.localhost:7080", "https://localhost:7080",
	}
	for _, origin := range others {
		for _, r := range requests {
			for _, method := range []string{r.method, http.MethodOptions} {
				resp := do(t, method, base+r.path, r.body, "Origin", origin, "Content-Type", "text/plain",
					"Access-Control-Request-Method", r.method)
				if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
					t.Errorf("%s %s from %s: %d, %v; want 403 without CORS", method, r.path, origin, resp.StatusCode, resp.Header)
				}
			}
		}
	}
	for _, own := range []string{fmt.Sprintf("http://localhost:%d", s.Port()), base} {
		resp := do(t, http.MethodGet, base+"/spike/v1/cameras", "", "Origin", own)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("cameras from uploadspike's own page %s: %d, %v; want 200 without CORS", own, resp.StatusCode, resp.Header)
		}
		resp = do(t, http.MethodOptions, base+"/spike/v1/oauth/complete", "", "Origin", own, "Access-Control-Request-Method", "POST")
		if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("preflight from uploadspike's own page %s: %d, %v; want 403 without CORS", own, resp.StatusCode, resp.Header)
		}
	}
}

// TestHandoffServer_ForwardsCodeAndStateOnly (Review Focus 5): a complete
// whose body holds another field, a verifier, is refused with 400 and
// reaches no hub; the forwarded JSON holds exactly code and state.
func TestHandoffServer_ForwardsCodeAndStateOnly(t *testing.T) {
	s, _, fc := startServer(t, Options{})
	url := fmt.Sprintf("http://127.0.0.1:%d/spike/v1/oauth/complete", s.Port())
	for _, body := range []string{
		`{"state":"st-1","code":"c-1","verifier":"v-1"}`,
		`{"state":"st-1"}`,
		`{"state":"st-1","code":"c-1"} {"verifier":"v-1"}`,
		`not json`,
	} {
		if resp := do(t, http.MethodPost, url, body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("complete %s: %d; want 400", body, resp.StatusCode)
		}
	}
	if n := len(fc.bodies["/control/v1/oauth/complete"]); n != 0 {
		t.Fatalf("%d refused bodies reached the hub", n)
	}
	if resp := do(t, http.MethodPost, url, `{"code":"c-1","state":"st-1"}`, "Content-Type", "text/plain"); resp.StatusCode != http.StatusOK {
		t.Fatalf("a good complete: %d", resp.StatusCode)
	}
	bodies := fc.bodies["/control/v1/oauth/complete"]
	if len(bodies) != 1 {
		t.Fatalf("forwarded %v", bodies)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["state"] != "st-1" || got["code"] != "c-1" {
		t.Errorf("forwarded %v; want exactly code and state", got)
	}
}

// TestHandoffServer_SetupAndReaderOnlyOnLoopback (P40): the LAN listener
// answers 404 for /setup, /reader, the status and the handover, which the
// loopback listener serves; the login handoff is served on both.
func TestHandoffServer_SetupAndReaderOnlyOnLoopback(t *testing.T) {
	lanAddrs = func() ([]net.IP, error) { return []net.IP{net.IPv4(127, 0, 0, 2)}, nil }
	t.Cleanup(func() { lanAddrs = RFC1918Addrs })
	skipWithoutLoopbackAlias(t)
	s, _, _ := startServer(t, Options{})
	if err := s.OpenLAN(t.Context(), "android"); err != nil {
		t.Fatal(err)
	}
	loop, lan := fmt.Sprintf("http://127.0.0.1:%d", s.Port()), fmt.Sprintf("http://127.0.0.2:%d", s.Port())
	auth := []string{"Authorization", "Bearer " + strings.Repeat("ab", 32)}
	for _, tc := range []struct {
		method, path string
		headers      []string
		loopback     int
	}{
		{http.MethodGet, "/setup", nil, http.StatusOK},
		{http.MethodGet, "/reader", nil, http.StatusOK},
		{http.MethodGet, "/status", nil, http.StatusOK},
		{http.MethodGet, "/spike/v1/status", nil, http.StatusOK},
		{http.MethodPost, "/spike/v1/run/handover", auth, http.StatusNoContent},
	} {
		if resp := do(t, tc.method, loop+tc.path, "", tc.headers...); resp.StatusCode != tc.loopback {
			t.Errorf("loopback %s %s: %d; want %d", tc.method, tc.path, resp.StatusCode, tc.loopback)
		}
		if resp := do(t, tc.method, lan+tc.path, "", tc.headers...); resp.StatusCode != http.StatusNotFound {
			t.Errorf("LAN %s %s: %d; want 404", tc.method, tc.path, resp.StatusCode)
		}
	}
	for _, base := range []string{loop, lan} {
		if resp := do(t, http.MethodGet, base+"/spike/v1/oauth/next?platform=android", ""); resp.StatusCode != http.StatusOK {
			t.Errorf("%s next: %d", base, resp.StatusCode)
		}
	}
	// The handover needs the run state's token.
	for _, h := range [][]string{nil, {"Authorization", "Bearer " + strings.Repeat("00", 32)}, {"Authorization", strings.Repeat("ab", 32)}} {
		if resp := do(t, http.MethodPost, loop+"/spike/v1/run/handover", "", h...); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("handover with %v: %d; want 401", h, resp.StatusCode)
		}
	}
}

// skipWithoutLoopbackAlias skips where 127.0.0.2 cannot be bound.
func skipWithoutLoopbackAlias(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.2:0")
	if err != nil {
		t.Skipf("127.0.0.2 cannot be bound here: %v", err)
	}
	_ = ln.Close()
}

// TestHandoffServer_ServesAppCamerasAndResults (C2-R7 (b), g3-01): GET
// /spike/v1/cameras answers [] and POST /spike/v1/results stores the body
// under results/app/, on the loopback listener and on the open LAN
// listener, with the shells' CORS; a body over 2 MiB is refused, and so is
// a result past 5,000 files or 500 MiB in total.
func TestHandoffServer_ServesAppCamerasAndResults(t *testing.T) {
	lanAddrs = func() ([]net.IP, error) { return []net.IP{net.IPv4(127, 0, 0, 2)}, nil }
	t.Cleanup(func() { lanAddrs = RFC1918Addrs })
	skipWithoutLoopbackAlias(t)
	results := t.TempDir()
	s, _, _ := startServer(t, Options{ResultsDir: results})
	if err := s.OpenLAN(t.Context(), "android"); err != nil {
		t.Fatal(err)
	}
	for i, base := range []string{fmt.Sprintf("http://127.0.0.1:%d", s.Port()), fmt.Sprintf("http://127.0.0.2:%d", s.Port())} {
		resp := do(t, http.MethodGet, base+"/spike/v1/cameras", "", "Origin", "http://localhost")
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(b)) != "[]" || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost" {
			t.Errorf("%s cameras: %d %q %v", base, resp.StatusCode, b, resp.Header)
		}
		body := fmt.Sprintf(`{"scenario":"OAUTH","device":{"platform":"android","model":"A24"},"n":%d}`, i)
		resp = do(t, http.MethodPost, base+"/spike/v1/results", body, "Origin", "https://tauri.localhost", "Content-Type", "text/plain")
		if resp.StatusCode != http.StatusCreated || resp.Header.Get("Access-Control-Allow-Origin") != "https://tauri.localhost" {
			t.Errorf("%s results: %d %v", base, resp.StatusCode, resp.Header)
		}
	}
	entries, err := os.ReadDir(filepath.Join(results, "app"))
	if err != nil || len(entries) != 2 {
		t.Errorf("results/app holds %v (%v); want the two results", entries, err)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "android-a24-") || !strings.Contains(e.Name(), "-oauth") {
			t.Errorf("results/app holds %s; want <device>-<time>-<scenario>", e.Name())
		}
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	big := `{"x":"` + strings.Repeat("a", 2<<20) + `"}`
	if resp := do(t, http.MethodPost, base+"/spike/v1/results", big); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("a result over 2 MiB: %d; want 413", resp.StatusCode)
	}
	s.app.fileCap = 2
	if resp := do(t, http.MethodPost, base+"/spike/v1/results", `{"scenario":"OAUTH"}`); resp.StatusCode != http.StatusInsufficientStorage {
		t.Errorf("a result past the file cap: %d; want 507", resp.StatusCode)
	}
	s.app.fileCap, s.app.byteCap = appResultsFileCap, 10
	if resp := do(t, http.MethodPost, base+"/spike/v1/results", `{"scenario":"OAUTH"}`); resp.StatusCode != http.StatusInsufficientStorage {
		t.Errorf("a result past the byte cap: %d; want 507", resp.StatusCode)
	}
	if entries, _ := os.ReadDir(filepath.Join(results, "app")); len(entries) != 2 {
		t.Errorf("results/app holds %d files after the refusals; want 2", len(entries))
	}
	if appResultsFileCap != 5000 || appResultsByteCap != 500<<20 || maxAppResultBytes != 2<<20 {
		t.Error("the caps are not 5,000 files, 500 MiB and 2 MiB")
	}
}

// listening reports whether a TCP connect to addr succeeds.
func listening(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// wildcardListener reports whether /proc/net/tcp lists a socket that
// listens on 0.0.0.0:port.
func wildcardListener(t *testing.T, port int) bool {
	t.Helper()
	b, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("00000000:%04X", port)
	for line := range strings.SplitSeq(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) > 3 && f[1] == want && f[3] == "0A" {
			return true
		}
	}
	return false
}

// TestHandoffServer_LanListenerOnlyDuringRemoteLogin (P40), on Linux with
// the real port 7091 while 127.0.0.1:7091 listens, the only test that binds
// it: no LAN socket before; during an Android login a socket on each of
// the host's RFC 1918 addresses and none on 0.0.0.0; closed after the
// login's complete, and after LANLife without one (10 min in production).
// The upload job requires an RFC 1918 address (UPLOADSPIKE_REQUIRE_LAN=1).
func TestHandoffServer_LanListenerOnlyDuringRemoteLogin(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the real port's test runs on Linux")
	}
	addrs, err := RFC1918Addrs()
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) == 0 {
		if os.Getenv("UPLOADSPIKE_REQUIRE_LAN") == "1" {
			t.Fatal("UPLOADSPIKE_REQUIRE_LAN=1, but the host has no RFC 1918 address")
		}
		t.Skip("the host has no RFC 1918 address")
	}
	hub, _ := startFakeControl(t)
	d := newFakeDriver()
	s := New(hub, Options{Driver: d, ResultsDir: t.TempDir(), LANLife: 700 * time.Millisecond})
	if err := s.ServeLoopback(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	port := strconv.Itoa(HandoffPort)
	if !listening("127.0.0.1:" + port) {
		t.Fatal("127.0.0.1:7091 does not listen")
	}
	lanAll := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for _, ip := range addrs {
			addr := net.JoinHostPort(ip.String(), port)
			for listening(addr) != want && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			if got := listening(addr); got != want {
				t.Errorf("%s listening %v; want %v", addr, got, want)
			}
		}
		if wildcardListener(t, HandoffPort) {
			t.Error("a socket listens on 0.0.0.0:7091")
		}
	}
	lanAll(false)
	if err := s.OpenLAN(t.Context(), "android"); err != nil {
		t.Fatal(err)
	}
	lanAll(true)
	lan := "http://" + net.JoinHostPort(addrs[0].String(), port)
	if resp := do(t, http.MethodPost, lan+"/spike/v1/oauth/complete", `{"state":"st-1","code":"c-1"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("complete over the LAN: %d", resp.StatusCode)
	}
	lanAll(false)
	if !listening("127.0.0.1:" + port) {
		t.Error("the loopback listener closed with the LAN one")
	}
	// A login that never completes: closed after LANLife.
	if err := s.OpenLAN(t.Context(), "android"); err != nil {
		t.Fatal(err)
	}
	lanAll(true)
	time.Sleep(1500 * time.Millisecond)
	lanAll(false)
	if _, open := s.LANOpen(); open {
		t.Error("the session is still open after its life")
	}
}

// TestHandoffServer_BeginOnlyTheAwaitedLogin: begin starts on the hub only
// the login awaited on its platform, refuses the cap's 21st with the
// driver's Romanian text, and the device page then shows the code.
func TestHandoffServer_BeginOnlyTheAwaitedLogin(t *testing.T) {
	s, d, fc := startServer(t, Options{})
	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	if resp := do(t, http.MethodGet, base+"/spike/v1/oauth/begin?dest=g-windows&provider=GOOGLE_DRIVE&platform=web", ""); resp.StatusCode != http.StatusConflict {
		t.Errorf("begin of a login not awaited: %d; want 409", resp.StatusCode)
	}
	d.refuse = errors.New("ați atins limita de 20 de autentificări pentru acest client")
	resp := do(t, http.MethodGet, base+"/spike/v1/oauth/begin?dest=g-web&provider=GOOGLE_DRIVE&platform=web", "")
	var refusal map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&refusal)
	if resp.StatusCode != http.StatusTooManyRequests || refusal["error"] != d.refuse.Error() {
		t.Errorf("begin past the cap: %d %v", resp.StatusCode, refusal)
	}
	if n := len(fc.bodies["/control/v1/oauth/begin"]); n != 0 {
		t.Fatalf("%d refused begins reached the hub", n)
	}
	d.refuse = nil
	resp = do(t, http.MethodGet, base+"/spike/v1/oauth/begin?dest=g-web&provider=GOOGLE_DRIVE&platform=web", "")
	var res hubproc.BeginResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil || res.State != "st-1" || res.UserCode != "ABCD-EFGH" {
		t.Fatalf("begin: %d %+v %v", resp.StatusCode, res, err)
	}
	var sent hubproc.BeginRequest
	_ = json.Unmarshal([]byte(fc.bodies["/control/v1/oauth/begin"][0]), &sent)
	if sent != (hubproc.BeginRequest{DestID: "g-web", Provider: "GOOGLE_DRIVE", Platform: "web", Plan: "B1"}) {
		t.Errorf("the hub's begin: %+v", sent)
	}
	page := do(t, http.MethodGet, base+"/oauth/device?state=st-1", "")
	if b, _ := io.ReadAll(page.Body); string(b) != "device ABCD-EFGH" {
		t.Errorf("the device page: %q", b)
	}
	if resp := do(t, http.MethodGet, base+"/oauth/device?state=other", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("the device page of an unknown state: %d", resp.StatusCode)
	}
	cell := do(t, http.MethodPost, base+"/spike/v1/visibility/web", `{"creator":"desktop","listed":true,"read":false}`,
		"Origin", fmt.Sprintf("http://localhost:%d", s.Port()))
	if cell.StatusCode != http.StatusNoContent || !slices.Equal(d.cells, []string{"desktop true false"}) {
		t.Errorf("a web cell: %d, %v", cell.StatusCode, d.cells)
	}
	if resp := do(t, http.MethodPost, base+"/spike/v1/visibility/web", `{"creator":"desktop","listed":true,"read":false,"token":"x"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a cell with another field: %d", resp.StatusCode)
	}
}

// TestHandoffServer_RecordsReaderInterruptions (C2-R18 (d)): the reader
// page's POST /spike/v1/visibility/interrupted, without a body, reaches the
// driver, from the reader page's origin only and on POST only; a body is
// refused.
func TestHandoffServer_RecordsReaderInterruptions(t *testing.T) {
	d := newFakeDriver()
	s := New(nil, Options{Port: freePort(t), Driver: d})
	if err := s.ServeLoopback(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	reader := fmt.Sprintf("http://localhost:%d", s.Port())
	if resp := do(t, http.MethodPost, base+"/spike/v1/visibility/interrupted", "", "Origin", reader); resp.StatusCode != http.StatusNoContent ||
		resp.Header.Get("Access-Control-Allow-Origin") != reader {
		t.Errorf("an interruption from the reader page: %d %v", resp.StatusCode, resp.Header)
	}
	for _, tc := range []struct {
		method, body, origin string
		want                 int
	}{
		{http.MethodPost, "", "http://localhost", http.StatusForbidden},
		{http.MethodGet, "", reader, http.StatusMethodNotAllowed},
		{http.MethodPost, `{"creator":"tv"}`, reader, http.StatusBadRequest},
	} {
		if resp := do(t, tc.method, base+"/spike/v1/visibility/interrupted", tc.body, "Origin", tc.origin); resp.StatusCode != tc.want {
			t.Errorf("%s from %s with %q: %d; want %d", tc.method, tc.origin, tc.body, resp.StatusCode, tc.want)
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.interruptions != 1 {
		t.Errorf("the driver got %d interruptions; want 1", d.interruptions)
	}
}

// doHost sends a request to url with the Host header host and the headers
// given as name, value pairs.
func doHost(t *testing.T, method, url, host, body string, headers ...string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := (&http.Client{Transport: &http.Transport{Proxy: nil}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestHandoffServer_OwnHostsAndOriginsOnly (Task 9B review M1): a request
// whose Host is not one of the server's own names (localhost, 127.0.0.1,
// [::1] or an open LAN address, at its port) is refused, so a DNS
// rebinding page reaches nothing, a same-origin GET with no Origin
// included; an Origin that only echoes such a Host is no own origin; and
// /setup's POST refuses another site's origin, a text/plain form with the
// clients' JSON included.
func TestHandoffServer_OwnHostsAndOriginsOnly(t *testing.T) {
	s, d, _ := startServer(t, Options{})
	port := strconv.Itoa(s.Port())
	base := "http://127.0.0.1:" + port
	rebound := "rebind.example:" + port
	for _, tc := range []struct {
		method, path string
		headers      []string
	}{
		{http.MethodGet, "/spike/v1/oauth/next?platform=android", nil},
		{http.MethodGet, "/spike/v1/oauth/next?platform=android", []string{"Origin", "http://" + rebound}},
		{http.MethodGet, "/status", nil},
		{http.MethodGet, "/setup", nil},
		{http.MethodPost, "/spike/v1/visibility/web", []string{"Origin", "http://" + rebound}},
	} {
		resp := doHost(t, tc.method, base+tc.path, rebound, `{"creator":"tv","listed":true,"read":true}`, tc.headers...)
		if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s %s with Host %s %v: %d; want 403 without CORS", tc.method, tc.path, rebound, tc.headers, resp.StatusCode)
		}
	}
	if len(d.cells) != 0 {
		t.Errorf("a rebound page recorded cells %v", d.cells)
	}
	for _, host := range []string{"127.0.0.1:" + port, "localhost:" + port, "LOCALHOST:" + port, "[::1]:" + port} {
		if resp := doHost(t, http.MethodGet, base+"/spike/v1/oauth/next?platform=android", host, ""); resp.StatusCode != http.StatusOK {
			t.Errorf("next with Host %s: %d; want 200", host, resp.StatusCode)
		}
	}
	clients := `{"google":{"web":{"id":"w"}}}`
	for _, origin := range []string{"https://evil.example", "http://localhost", "null"} {
		resp := do(t, http.MethodPost, base+"/setup", clients, "Origin", origin, "Content-Type", "text/plain")
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("/setup POST from %s: %d; want 403", origin, resp.StatusCode)
		}
	}
	for _, origin := range []string{"http://localhost:" + port, "http://127.0.0.1:" + port, ""} {
		var h []string
		if origin != "" {
			h = []string{"Origin", origin}
		}
		if resp := do(t, http.MethodPost, base+"/setup", clients, h...); resp.StatusCode != http.StatusOK {
			t.Errorf("/setup POST from %q: %d; want 200", origin, resp.StatusCode)
		}
	}
}

// TestHandoffServer_CompleteClosesItsOwnSession (Task 9B review M5): a
// login that completes on the loopback listener leaves another device's
// open LAN session alone; one that completes on the LAN session closes
// that session.
func TestHandoffServer_CompleteClosesItsOwnSession(t *testing.T) {
	lanAddrs = func() ([]net.IP, error) { return []net.IP{net.IPv4(127, 0, 0, 2)}, nil }
	t.Cleanup(func() { lanAddrs = RFC1918Addrs })
	skipWithoutLoopbackAlias(t)
	s, _, _ := startServer(t, Options{})
	if err := s.OpenLAN(t.Context(), "g-android"); err != nil {
		t.Fatal(err)
	}
	loop, lan := fmt.Sprintf("http://127.0.0.1:%d", s.Port()), fmt.Sprintf("http://127.0.0.2:%d", s.Port())
	if resp := do(t, http.MethodPost, loop+"/spike/v1/oauth/complete", `{"state":"st-1","code":"c-1"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("complete on the loopback: %d", resp.StatusCode)
	}
	time.Sleep(300 * time.Millisecond)
	if key, open := s.LANOpen(); !open || key != "g-android" {
		t.Fatalf("a loopback login's complete closed the LAN session (open %v, key %q)", open, key)
	}
	if resp := do(t, http.MethodPost, lan+"/spike/v1/oauth/complete", `{"state":"st-1","code":"c-1"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("complete on the LAN: %d", resp.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, open := s.LANOpen(); open && time.Now().Before(deadline); _, open = s.LANOpen() {
		time.Sleep(50 * time.Millisecond)
	}
	if _, open := s.LANOpen(); open {
		t.Error("the LAN session stays open after its login completed on it")
	}
}
