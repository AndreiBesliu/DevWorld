package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	fakegdrive "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/gdrive"
	fakeoauth "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/oauth"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// testControlToken is the control endpoint's token of the process tests,
// assembled at run time: 40 hex characters.
func testControlToken() string { return strings.Repeat("0123456789", 3) + "abcdefabcd" }

// controlClient calls a hub's control endpoint.
type controlClient struct {
	t     *testing.T
	addr  string
	token string
	// bodies are the answers' bodies, for the tests that look for a secret
	// in them.
	mu     sync.Mutex
	bodies []string
}

// startControlHub starts ipcam-hub on config with the control token and
// returns it and its control endpoint, from its control event.
func startControlHub(t *testing.T, config string) (*hub, *controlClient) {
	t.Helper()
	t.Setenv(controlTokenEnv, testControlToken())
	h := startHub(t, "run", "--config", config)
	e := h.next(t, 30*time.Second, "control event", ofType("control"))
	addr, _ := e["addr"].(string)
	return h, &controlClient{t: t, addr: addr, token: testControlToken()}
}

// do sends method path with body as JSON and the control token, and
// returns the status and the answer's JSON.
func (c *controlClient) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	status, raw := c.raw(method, path, body, "Bearer "+c.token)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return status, m
}

// raw sends the request with the Authorization header auth ("" for none).
func (c *controlClient) raw(method, path string, body any, auth string) (int, []byte) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(c.t.Context(), method, "http://"+c.addr+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	c.mu.Lock()
	c.bodies = append(c.bodies, string(raw))
	c.mu.Unlock()
	return resp.StatusCode, raw
}

// TestControl_RequiresBearer (P01 item 3, P46): with IPCAM_CONTROL_TOKEN,
// ipcam-hub run serves the control endpoint, announced by a control event
// with its address; every request without the token, with another one, or
// with it outside a Bearer header answers 401; the token is in no event and
// not on stderr. A token that is not 32 hex characters or more stops run
// with exit 1, its value unprinted.
func TestControl_RequiresBearer(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	h, c := startControlHub(t, uploadConfig(t, dataDir, nil))
	ready := h.next(t, 30*time.Second, "ready event", ofType("ready"))
	for _, path := range []string{"/control/v1/visibility/targets", "/control/v1/oauth/status?state=x"} {
		for _, auth := range []string{"", "Bearer " + strings.Repeat("f", 40), testControlToken(), "Basic " + testControlToken()} {
			if status, _ := c.raw(http.MethodGet, path, nil, auth); status != http.StatusUnauthorized {
				t.Errorf("GET %s with Authorization %q: HTTP %d, want 401", path, auth, status)
			}
		}
	}
	for _, path := range []string{"/control/v1/clients", "/control/v1/oauth/begin", "/control/v1/oauth/complete",
		"/control/v1/oauth/refresh", "/control/v1/uploads/reset-connections"} {
		if status, _ := c.raw(http.MethodPost, path, map[string]any{}, ""); status != http.StatusUnauthorized {
			t.Errorf("POST %s without the token: HTTP %d, want 401", path, status)
		}
	}
	if status, body := c.raw(http.MethodGet, "/control/v1/visibility/targets", nil, "Bearer "+c.token); status != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("targets with the token: HTTP %d %s, want 200 []", status, body)
	}
	all := append([]map[string]any{ready}, stopAndDrain(t, h)...)
	for _, e := range all {
		b, _ := json.Marshal(e)
		if strings.Contains(string(b), testControlToken()) {
			t.Errorf("event %s holds the control token", b)
		}
	}
	if strings.Contains(h.stderr.String(), testControlToken()) {
		t.Error("stderr holds the control token")
	}
	t.Setenv(controlTokenEnv, "SENT"+"INEL-short")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", "--config", uploadConfig(t, filepath.Join(t.TempDir(), "data"), nil)}, strings.NewReader(""), &stdout, &stderr); code != exitFatal ||
		!strings.Contains(stderr.String(), controlTokenEnv) || strings.Contains(stderr.String()+stdout.String(), "INEL-short") {
		t.Errorf("a short token: exit %d, stderr %q; want exit 1 naming %s, not the value", code, stderr.String(), controlTokenEnv)
	}
}

// TestControl_BindsLoopbackOnly (P40): the control endpoint listens on
// 127.0.0.1 alone: its port answers on no other address of the host.
func TestControl_BindsLoopbackOnly(t *testing.T) {
	h, c := startControlHub(t, uploadConfig(t, filepath.Join(t.TempDir(), "data"), nil))
	host, port, err := net.SplitHostPort(c.addr)
	if err != nil || host != "127.0.0.1" {
		t.Fatalf("the control address %q, want 127.0.0.1:<port>", c.addr)
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	tried := 0
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.IsLinkLocalUnicast() {
			continue
		}
		tried++
		if conn, err := net.DialTimeout("tcp", net.JoinHostPort(ipNet.IP.String(), port), time.Second); err == nil {
			_ = conn.Close()
			t.Errorf("the control port answers on %s", ipNet.IP)
		}
	}
	if conn, err := net.DialTimeout("tcp", c.addr, time.Second); err != nil {
		t.Errorf("the control port does not answer on 127.0.0.1: %v", err)
	} else {
		_ = conn.Close()
	}
	t.Logf("tried %d other addresses", tried)
	h.next(t, 30*time.Second, "ready event", ofType("ready"))
	stopAndDrain(t, h)
}

// TestControl_ClientsSealed (P46): POST /control/v1/clients answers 204 and
// seals the clients: the catalog's files hold no client secret, and every
// client:* row has kind oauth.
func TestControl_ClientsSealed(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	h, c := startControlHub(t, uploadConfig(t, dataDir, nil))
	if status, body := c.do(http.MethodPost, "/control/v1/clients", testClients()); status != http.StatusNoContent {
		t.Fatalf("POST clients: HTTP %d %v", status, body)
	}
	bad := testClients()
	bad.Google[oauth.ClientWeb] = oauth.Client{ID: clientWeb, Secret: "SENT" + "INEL-web-secret"}
	if status, body := c.do(http.MethodPost, "/control/v1/clients", bad); status != http.StatusBadRequest || strings.Contains(c.bodies[len(c.bodies)-1], "INEL-web") {
		t.Errorf("a Web client's secret: HTTP %d %v, want 400 without the secret", status, body)
	}
	h.next(t, 30*time.Second, "ready event", ofType("ready"))
	stopAndDrain(t, h)
	for _, name := range []string{"catalog.db", "catalog.db-wal"} {
		b, err := os.ReadFile(filepath.Join(dataDir, "state", name)) //nolint:gosec // the test's catalog
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		for _, secret := range []string{secretDesktop, secretTV} {
			if bytes.Contains(b, []byte(secret)) {
				t.Errorf("state/%s holds a client secret", name)
			}
		}
	}
	db := openCatalog(t, filepath.Join(dataDir, "state", "catalog.db"))
	rows, err := db.QueryContext(t.Context(), "SELECT id, kind FROM secrets ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			t.Fatal(err)
		}
		if kind != "oauth" {
			t.Errorf("the row %s has kind %q, want oauth", id, kind)
		}
		ids = append(ids, id)
	}
	if want := []string{"client:google:android", "client:google:desktop", "client:google:tv", "client:google:web"}; strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("rows %q, want %q", ids, want)
	}
}

// oauthServers serves a fakeoauth and a fakegdrive that takes fakeoauth's
// live tokens only, the token endpoint's forms recorded.
type oauthServers struct {
	login    *fakeoauth.Fake
	drive    *fakegdrive.Fake
	loginURL string
	driveURL string
	mu       sync.Mutex
	forms    []url.Values
}

func newOAuthServers(t *testing.T) *oauthServers {
	t.Helper()
	s := &oauthServers{login: fakeoauth.New(fakeoauth.Options{}), drive: fakegdrive.New(fakegdrive.Options{})}
	s.login.AddClient(fakeoauth.Client{Provider: fakeoauth.Google, Type: fakeoauth.TypeDesktop, ID: clientDesktop, Secret: secretDesktop, Project: "p1"})
	s.drive.IdentifyTokens(func(token string) (fakegdrive.Identity, bool) {
		tok, ok := s.login.Lookup(token)
		return fakegdrive.Identity{Client: tok.ClientID, Project: tok.Project, Account: tok.Account}, ok && !tok.Expired
	})
	handler := s.login.Handler()
	loginSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fakeoauth.PathGoogleToken && r.ParseForm() == nil {
			s.mu.Lock()
			s.forms = append(s.forms, r.PostForm)
			s.mu.Unlock()
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(loginSrv.Close)
	driveSrv := httptest.NewServer(s.drive.Handler())
	t.Cleanup(driveSrv.Close)
	s.loginURL, s.driveURL = loginSrv.URL, driveSrv.URL
	return s
}

// TestControl_CompleteUnblocksEveryDestinationOfItsTokenRef (P27, Task 8's
// MI5 reusing an MI6 login): two Drive destinations name the tokenRef
// g-windows and a third another; with no login they are BLOCKED_AUTH. A
// login of g-windows through the control endpoint (begin, the fake's
// consent, complete) makes both of its destinations OK, and their copies
// are verified, while the third stays BLOCKED_AUTH. The hub's verifier, the
// code and the tokens are in no event, stderr line or control answer.
func TestControl_CompleteUnblocksEveryDestinationOfItsTokenRef(t *testing.T) {
	requireLocalRoom(t)
	srv := newOAuthServers(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	placeTornSegment(t, dataDir, testCam, testProfile, time.Now().Add(-time.Hour), 3)
	dest := func(id, ref string) map[string]any {
		return map[string]any{"id": id, "kind": "GOOGLE_DRIVE", "folder": "", "tokenRef": ref, "baseUrl": srv.driveURL, "uploadBaseUrl": "", "partSize": 256 << 10}
	}
	uploads := map[string]any{"hubName": "hub", "destinations": []any{dest("dst-1", "g-windows"), dest("dst-2", "g-windows"), dest("dst-3", "g-other")},
		"routes": []any{map[string]any{"cam": testCam, "profile": testProfile, "class": catalog.ClassContinuous,
			"destinations": []string{"dst-1", "dst-2", "dst-3"}, "fromMs": 0, "untilMs": 0}}}
	config := uploadConfig(t, dataDir, uploads)
	var cfg map[string]any
	b, err := os.ReadFile(config) //nolint:gosec // the test's run.json
	if err != nil || json.Unmarshal(b, &cfg) != nil {
		t.Fatal(err)
	}
	cfg["oauth"] = map[string]any{"endpoints": oauth.Endpoints(fakeoauth.URLsAt(srv.loginURL))}
	writeJSON(t, config, cfg)
	h, c := startControlHub(t, config)
	if status, body := c.do(http.MethodPost, "/control/v1/clients", testClients()); status != http.StatusNoContent {
		t.Fatalf("POST clients: HTTP %d %v", status, body)
	}
	var ev cardEvents
	blocked := func(all []map[string]any, dest string) bool {
		return len(ofTypeIn(all, "dest_state", "dest", dest)) > 0 && ofTypeIn(all, "dest_state", "dest", dest)[0]["state"] == "BLOCKED_AUTH"
	}
	ev.until(t, h, 60*time.Second, "three BLOCKED_AUTH destinations", func(all []map[string]any) bool {
		return blocked(all, "dst-1") && blocked(all, "dst-2") && blocked(all, "dst-3")
	})
	status, begin := c.do(http.MethodPost, "/control/v1/oauth/begin", oauth.BeginRequest{DestID: "g-windows", Provider: "GOOGLE_DRIVE",
		Platform: oauth.PlatformWindows, LoopbackPort: 53123})
	if status != http.StatusOK || begin["plan"] != "A" || begin["redirectUri"] != "http://127.0.0.1:53123/" {
		t.Fatalf("begin: HTTP %d %v", status, begin)
	}
	redirect, err := srv.login.Authorize(begin["authUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	code := u.Query().Get("code")
	status, done := c.do(http.MethodPost, "/control/v1/oauth/complete", map[string]string{"state": begin["state"].(string), "code": code})
	if status != http.StatusOK || done["destId"] != "g-windows" || done["clientType"] != "desktop" {
		t.Fatalf("complete: HTTP %d %v", status, done)
	}
	if status, again := c.do(http.MethodPost, "/control/v1/oauth/complete", map[string]string{"state": begin["state"].(string), "code": code}); status != http.StatusBadRequest || again["reason"] != "state" {
		t.Errorf("a second complete: HTTP %d %v, want 400, reason state", status, again)
	}
	verified := func(all []map[string]any, dest string) bool {
		return len(ofTypeIn(all, "upload_verified", "dest", dest)) > 0
	}
	ev.until(t, h, 60*time.Second, "dst-1's and dst-2's copies", func(all []map[string]any) bool {
		return verified(all, "dst-1") && verified(all, "dst-2")
	})
	all := append(ev.all, stopAndDrain(t, h)...)
	for _, d := range []string{"dst-1", "dst-2"} {
		states := ofTypeIn(all, "dest_state", "dest", d)
		if last := states[len(states)-1]; last["state"] != "OK" {
			t.Errorf("%s's last state %v, want OK", d, last)
		}
	}
	if states := ofTypeIn(all, "dest_state", "dest", "dst-3"); len(states) != 1 || verified(all, "dst-3") {
		t.Errorf("dst-3: states %v, verified %v; want BLOCKED_AUTH alone", states, verified(all, "dst-3"))
	}
	if n := len(ofTypeIn(all, "oauth_begin", "destId", "g-windows")); n != 1 {
		t.Errorf("%d oauth_begin events, want 1", n)
	}
	completed := ofTypeIn(all, "oauth_completed", "destId", "g-windows")
	if len(completed) != 1 || completed[0]["clientType"] != "desktop" || completed[0]["issuedAtMs"] == nil {
		t.Errorf("oauth_completed %v", completed)
	}
	if failed := ofTypeIn(all, "oauth_failed", "destId", "g-windows"); len(failed) != 1 || failed[0]["stage"] != "complete" || failed[0]["reason"] != "state" {
		t.Errorf("oauth_failed %v, want the second complete's, reason state", failed)
	}
	srv.mu.Lock()
	var secrets []string
	for _, f := range srv.forms {
		secrets = append(secrets, f.Get("code_verifier"), f.Get("code"), f.Get("client_secret"))
	}
	srv.mu.Unlock()
	secrets = append(secrets, srv.login.RefreshTokens()...)
	secrets = append(secrets, srv.login.IssuedAccessTokens()...)
	var outputs []string
	for _, e := range all {
		b, _ := json.Marshal(e)
		outputs = append(outputs, string(b))
	}
	outputs = append(outputs, h.stderr.String())
	outputs = append(outputs, c.bodies...)
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, out := range outputs {
			if strings.Contains(out, secret) {
				t.Errorf("an output holds a verifier, code, secret or token: %.120s", out)
			}
		}
	}
}

// heldServer serves h, but holds the second chunk PUT that it sees until
// its client goes away, and records each request's remote address.
type heldServer struct {
	h       http.Handler
	held    chan struct{}
	puts    atomic.Int32
	mu      sync.Mutex
	remotes map[string]bool
	cut     atomic.Bool // the held request's connection closed
}

func (s *heldServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.remotes[r.RemoteAddr] = true
	s.mu.Unlock()
	if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/upload/") && s.puts.Add(1) == 2 {
		_, _ = io.Copy(io.Discard, r.Body)
		s.held <- struct{}{}
		<-r.Context().Done()
		s.cut.Store(true)
		return
	}
	s.h.ServeHTTP(w, r)
}

// TestControl_ResetConnections (Task 8's block): POST
// /control/v1/uploads/reset-connections answers 204 and closes the drivers'
// connections: a chunk in flight is cut, counted as a transient failure,
// and retried on a new connection, and the copy is verified.
func TestControl_ResetConnections(t *testing.T) {
	fake := fakegdrive.New(fakegdrive.Options{})
	held := &heldServer{h: fake.Handler(), held: make(chan struct{}, 1), remotes: map[string]bool{}}
	srv := httptest.NewServer(held)
	t.Cleanup(srv.Close)
	t.Setenv(testTokenEnv, testTokenFromParts())
	dataDir := filepath.Join(t.TempDir(), "data")
	placeTornSegment(t, dataDir, testCam, testProfile, time.Now().Add(-time.Hour), 120)
	dest := map[string]any{"id": "dst-1", "kind": "GOOGLE_DRIVE", "folder": "ipcam", "tokenRef": "dst-1", "baseUrl": srv.URL, "uploadBaseUrl": "", "partSize": 256 << 10}
	h, c := startControlHub(t, uploadConfig(t, dataDir, uploadsTo(dest, 0)))
	select {
	case <-held.held:
	case <-time.After(60 * time.Second):
		t.Fatal("no second chunk PUT after 60 s")
	}
	held.mu.Lock()
	before := len(held.remotes)
	held.mu.Unlock()
	if status, _ := c.do(http.MethodPost, "/control/v1/uploads/reset-connections", nil); status != http.StatusNoContent {
		t.Fatalf("reset-connections: HTTP %d", status)
	}
	var ev cardEvents
	ev.until(t, h, 60*time.Second, "the upload's verification", func(all []map[string]any) bool {
		return len(ofTypeIn(all, "upload_verified", "", nil)) == 1
	})
	all := append(ev.all, stopAndDrain(t, h)...)
	if !held.cut.Load() {
		t.Error("the held chunk's connection was not closed")
	}
	if fails := ofTypeIn(all, "upload_failed", "reason", "transient"); len(fails) == 0 {
		t.Errorf("no transient upload_failed after the reset; events %v", all)
	}
	held.mu.Lock()
	after := len(held.remotes)
	held.mu.Unlock()
	if after <= before {
		t.Errorf("%d connections before the reset and %d after, want a new one", before, after)
	}
}

// TestControl_DeviceLoginStatus (P39): a B1 login through the control
// endpoint (web × Google, the TV client): status answers pending until the
// user approves on the device page, then completed with the Completion;
// oauth_completed goes out once however often status is asked, and an
// unknown state answers 404.
func TestControl_DeviceLoginStatus(t *testing.T) {
	f := newOAuthFixture(t)
	var out syncBuffer
	c := &control{token: testControlToken(), store: f.store, cat: f.cat, ev: &events{out: &out}, conns: &connTracker{conns: map[*trackedConn]bool{}},
		reported: map[string]bool{}}
	srv := httptest.NewServer(c.handler())
	t.Cleanup(srv.Close)
	cc := &controlClient{t: t, addr: strings.TrimPrefix(srv.URL, "http://"), token: testControlToken()}
	status, begin := cc.do(http.MethodPost, "/control/v1/oauth/begin", oauth.BeginRequest{DestID: "g-web", Provider: "GOOGLE_DRIVE", Platform: oauth.PlatformWeb})
	if status != http.StatusOK || begin["plan"] != "B1" || begin["userCode"] == "" || begin["authUrl"] != nil {
		t.Fatalf("begin: HTTP %d %v, want B1 with a user code", status, begin)
	}
	state := url.QueryEscape(begin["state"].(string))
	if _, s := cc.do(http.MethodGet, "/control/v1/oauth/status?state="+state, nil); s["status"] != "pending" {
		t.Errorf("before the approval: %v, want pending", s)
	}
	if err := f.login.ApproveDevice(begin["userCode"].(string), "acct-1"); err != nil {
		t.Fatal(err)
	}
	f.advance(time.Minute) // past the device flow's poll interval
	for range 2 {
		_, s := cc.do(http.MethodGet, "/control/v1/oauth/status?state="+state, nil)
		done, _ := s["completion"].(map[string]any)
		if s["status"] != "completed" || done["clientType"] != "tv" || done["plan"] != "B1" {
			t.Errorf("after the approval: %v, want completed by the TV client", s)
		}
	}
	if n := strings.Count(out.String(), `"type":"oauth_completed"`); n != 1 {
		t.Errorf("%d oauth_completed events, want 1:\n%s", n, out.String())
	}
	if status, _ := cc.do(http.MethodGet, "/control/v1/oauth/status?state=unknown", nil); status != http.StatusNotFound {
		t.Errorf("an unknown state: HTTP %d, want 404", status)
	}
}
