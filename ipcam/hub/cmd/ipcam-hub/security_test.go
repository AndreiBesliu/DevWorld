package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	fakeoauth "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/oauth"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// The tests of the Task 9A security review's findings on ipcam-hub's
// control endpoint and diag scan-secrets.

// TestControl_HeaderLimits (security review CTL-1): before the bearer token
// is checked, a request's header may hold 8 KiB, and must arrive within
// 2 s: a longer header is answered 431 at once, and a header that never
// ends is closed within the 2 s, so a local process cannot make the hub
// hold about 1 MB per connection.
func TestControl_HeaderLimits(t *testing.T) {
	c := &control{token: testControlToken(), conns: &connTracker{conns: map[*trackedConn]bool{}}, reported: map[string]bool{}}
	addr, stop, err := c.serve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	long, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = long.Close() }()
	if _, err := io.WriteString(long, "GET /control/v1/visibility/targets HTTP/1.1\r\nHost: h\r\nX-Pad: "+strings.Repeat("a", 64<<10)); err != nil {
		t.Fatal(err)
	}
	_ = long.SetReadDeadline(time.Now().Add(3 * time.Second))
	if status, err := bufio.NewReader(long).ReadString('\n'); err != nil || !strings.Contains(status, " 431 ") {
		t.Errorf("a 64 KiB header: %q, %v; want 431 at once", status, err)
	}
	slow, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = slow.Close() }()
	if _, err := io.WriteString(slow, "GET /control/v1/visibility/targets HTTP/1.1\r\nHost: h\r\n"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = slow.SetReadDeadline(time.Now().Add(6 * time.Second))
	_, err = io.ReadAll(slow)
	if took := time.Since(start); err != nil || took > 4*time.Second {
		t.Errorf("a header that never ends: closed after %v (%v); want closed within the 2 s header timeout", took.Round(time.Millisecond), err)
	}
}

// TestControl_ConnectionCap (security review CTL-1): the control endpoint
// serves a bounded number of connections at once; one more waits until a
// connection closes.
func TestControl_ConnectionCap(t *testing.T) {
	c := &control{token: testControlToken(), conns: &connTracker{conns: map[*trackedConn]bool{}}, reported: map[string]bool{}, maxConns: 2}
	addr, stop, err := c.serve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	var held []net.Conn
	for range 2 {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, conn)
	}
	get := func(timeout time.Duration) error {
		client := &http.Client{Timeout: timeout, Transport: &http.Transport{DisableKeepAlives: true}}
		req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/control/v1/visibility/targets", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+testControlToken())
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		return nil
	}
	time.Sleep(100 * time.Millisecond) // the two are accepted
	if err := get(500 * time.Millisecond); err == nil {
		t.Error("a third connection was served while two held the cap of 2")
	}
	for _, conn := range held {
		_ = conn.Close()
	}
	if err := get(5 * time.Second); err != nil {
		t.Errorf("a connection once the two closed: %v; want it served", err)
	}
}

// interceptedLoginServer is a server in front of the fixture's fake
// provider that offers each token request to answer first.
func interceptedLoginServer(t *testing.T, f *oauthFixture, answer func(w http.ResponseWriter, r *http.Request) bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") && answer(w, r) {
			return
		}
		f.login.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// grantType peeks at a token request's grant type and puts its body back.
func grantType(r *http.Request) string {
	b, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(b))
	form, _ := url.ParseQuery(string(b))
	return form.Get("grant_type")
}

// fixtureStore is another store on the fixture's catalog and data
// directory, calling the provider at loginURL through client.
func fixtureStore(t *testing.T, f *oauthFixture, loginURL string, client *http.Client) *oauth.Store {
	t.Helper()
	sealer, err := crypto.NewSealer(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := oauth.New(oauth.Config{Endpoints: oauth.Endpoints(fakeoauth.URLsAt(loginURL)), Clock: f.now, Sealer: sealer, HTTP: client, Secrets: f.cat})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestControl_OneEndReportPerLogin (security review CTL-2, Task 9A review
// M1): a login's end goes out as one event, at its complete or at a
// status: a second complete of a used state, or a status after a failed
// complete, reports nothing more; a complete that names a device login's
// state reports nothing and leaves that login's report to its completion.
// Every completion that the endpoint answers unblocks the destinations of
// its tokenRef, whether or not its event went out before.
func TestControl_OneEndReportPerLogin(t *testing.T) {
	f := newOAuthFixture(t)
	var out syncBuffer
	var mu sync.Mutex
	unblocked := map[string]int{}
	c := &control{token: testControlToken(), store: f.store, cat: f.cat, ev: &events{out: &out}, conns: &connTracker{conns: map[*trackedConn]bool{}},
		reported: map[string]bool{}, unblock: func(ref string) {
			mu.Lock()
			unblocked[ref]++
			mu.Unlock()
		}}
	srv := httptest.NewServer(c.handler())
	t.Cleanup(srv.Close)
	cc := &controlClient{t: t, addr: strings.TrimPrefix(srv.URL, "http://"), token: testControlToken()}
	begin := func(dest string, platform oauth.Platform) map[string]any {
		status, b := cc.do(http.MethodPost, "/control/v1/oauth/begin", oauth.BeginRequest{DestID: dest, Provider: "GOOGLE_DRIVE", Platform: platform, LoopbackPort: 53123})
		if status != http.StatusOK {
			t.Fatalf("begin %s: HTTP %d %v", dest, status, b)
		}
		return b
	}
	codeFor := func(b map[string]any) string {
		redirect, err := f.login.Authorize(b["authUrl"].(string))
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(redirect)
		if err != nil {
			t.Fatal(err)
		}
		return u.Query().Get("code")
	}
	statusOf := func(b map[string]any) map[string]any {
		_, s := cc.do(http.MethodGet, "/control/v1/oauth/status?state="+url.QueryEscape(b["state"].(string)), nil)
		return s
	}
	complete := func(b map[string]any, code string) (int, map[string]any) {
		return cc.do(http.MethodPost, "/control/v1/oauth/complete", map[string]string{"state": b["state"].(string), "code": code})
	}

	// Plan A that completes, then a second complete and a status.
	win := begin("g-windows", oauth.PlatformWindows)
	code := codeFor(win)
	if status, done := complete(win, code); status != http.StatusOK {
		t.Fatalf("complete: HTTP %d %v", status, done)
	}
	if status, again := complete(win, code); status != http.StatusBadRequest || again["reason"] != "state" {
		t.Errorf("a second complete: HTTP %d %v; want 400, reason state", status, again)
	}
	if s := statusOf(win); s["status"] != "completed" {
		t.Errorf("status after the complete: %v; want completed", s)
	}
	// Plan A whose complete fails, then a second complete and a status.
	android := begin("g-android", oauth.PlatformAndroid)
	code = codeFor(android)
	f.login.FailNext(fakeoauth.PathGoogleToken, http.StatusBadRequest, "access_denied")
	if status, _ := complete(android, code); status != http.StatusBadRequest {
		t.Errorf("a refused complete: HTTP %d; want 400", status)
	}
	_, _ = complete(android, code)
	if s := statusOf(android); s["status"] != "failed" || s["reason"] != "access_denied" {
		t.Errorf("status after a refused complete: %v; want failed, access_denied", s)
	}
	// A device login, first named by a complete.
	web := begin("g-web", oauth.PlatformWeb)
	if status, b := complete(web, "a-code"); status != http.StatusBadRequest || b["reason"] != "state" {
		t.Errorf("a complete of a device login's state: HTTP %d %v; want 400, reason state", status, b)
	}
	if err := f.login.ApproveDevice(web["userCode"].(string), "acct-1"); err != nil {
		t.Fatal(err)
	}
	f.advance(time.Minute)
	if s := statusOf(web); s["status"] != "completed" {
		t.Errorf("the device login's status: %v; want completed", s)
	}

	events := out.String()
	for dest, want := range map[string][2]int{"g-windows": {1, 0}, "g-android": {0, 1}, "g-web": {1, 0}} {
		nc := countEvents(events, "oauth_completed", dest)
		nf := countEvents(events, "oauth_failed", dest)
		if nc != want[0] || nf != want[1] {
			t.Errorf("%s: %d oauth_completed and %d oauth_failed; want %d and %d:\n%s", dest, nc, nf, want[0], want[1], events)
		}
	}
	if n := countEvents(events, "oauth_failed", ""); n != 0 {
		t.Errorf("%d oauth_failed events name no login:\n%s", n, events)
	}
	mu.Lock()
	defer mu.Unlock()
	if unblocked["g-windows"] != 2 || unblocked["g-web"] != 1 || unblocked["g-android"] != 0 {
		t.Errorf("unblocked %v; want g-windows at its complete and its completed status, g-web once, g-android never", unblocked)
	}
}

// countEvents counts the events of type typ whose destId is dest.
func countEvents(out, typ, dest string) int {
	n := 0
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, `"type":"`+typ+`"`) && strings.Contains(line, `"destId":"`+dest+`"`) {
			n++
		}
	}
	return n
}

// TestControl_ResetKeepsTokenCalls (security review CTL-3):
// reset-connections cuts the drivers' connections, not a call of the
// token store in flight: a refresh whose rotated answer is on its way
// back still stores the rotation, which the provider revoked the old
// token for. The token store's idle connections are closed, so no pooled
// flow outlives the block.
func TestControl_ResetKeepsTokenCalls(t *testing.T) {
	f := newOAuthFixture(t)
	f.logIn(t, "g-windows", oauth.PlatformWindows, "acct-1")
	_, tokens, conns := newHubClient()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	remotes := map[string]bool{}
	srv := interceptedLoginServer(t, f, func(w http.ResponseWriter, r *http.Request) bool {
		mu.Lock()
		remotes[r.RemoteAddr] = true
		mu.Unlock()
		if grantType(r) != "refresh_token" {
			return false
		}
		held := false
		once.Do(func() { held = true })
		if !held {
			return false
		}
		rec := httptest.NewRecorder()
		f.login.Handler().ServeHTTP(rec, r) // the fake rotates and revokes now
		entered <- struct{}{}
		<-release
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
		return true
	})
	s := fixtureStore(t, f, srv.URL, tokens)
	done := make(chan error, 1)
	go func() {
		_, err := s.Refresh(context.Background(), "g-windows")
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the refresh did not reach the token endpoint")
	}
	c := &control{conns: conns}
	rec := httptest.NewRecorder()
	c.resetConnections(rec, httptest.NewRequest(http.MethodPost, "/control/v1/uploads/reset-connections", nil))
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the refresh in flight at the reset = %v; want its rotation stored", err)
	}
	mu.Lock()
	before := len(remotes)
	mu.Unlock()
	c.resetConnections(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/control/v1/uploads/reset-connections", nil))
	if _, err := s.Refresh(t.Context(), "g-windows"); err != nil {
		t.Errorf("the next refresh = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(remotes) <= before {
		t.Errorf("%d connections before the reset and %d after: the token store's idle connection outlived it", before, len(remotes))
	}
}

// TestDiagScanSecrets_VanishedAndUnreadableFiles (security review DIAG-1):
// a file that vanishes between the directory's read and its open (a
// results writer's temporary file) cannot leak and is skipped; a file that
// cannot be read is named in unreadable, and the scan goes on, prints its
// line with every file it removed, and exits 1.
func TestDiagScanSecrets_VanishedAndUnreadableFiles(t *testing.T) {
	value := "SENT" + "INEL-leaked-value"
	dir := t.TempDir()
	for name, body := range map[string]string{"a-leak.json": value, "b.json.tmp0": "{}", "c-unreadable.json": "{}", "d-leak.json": value, "e-clean.json": "{}"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	open := func(root *os.Root, rel string) (*os.File, error) {
		switch rel {
		case "b.json.tmp0":
			_ = root.Remove(rel) // renamed away by its writer
		case "c-unreadable.json":
			return nil, &fs.PathError{Op: "openat", Path: rel, Err: syscall.EIO}
		}
		return root.Open(rel)
	}
	res, err := scanAndRemove(dir, encodedNeedles([]string{value}), open)
	if err != nil {
		t.Fatalf("scanAndRemove = %v; want the scan to go on", err)
	}
	if !slices.Equal(res.Removed, []string{"a-leak.json", "d-leak.json"}) || !slices.Equal(res.Unreadable, []string{"c-unreadable.json"}) {
		t.Errorf("removed %v, unreadable %v; want both leaks removed and the unreadable file named", res.Removed, res.Unreadable)
	}
	// Through the command: the line is printed, and the exit is 1.
	_ = os.WriteFile(filepath.Join(dir, "f-leak.json"), []byte(value), 0o600)
	scanOpen = open
	t.Cleanup(func() { scanOpen = (*os.Root).Open })
	code, lines, out, _ := runScan(t, stdinLine(value), "--dir", dir)
	if code != exitFatal || len(lines) != 1 || !strings.Contains(out, "f-leak.json") || !strings.Contains(out, "c-unreadable.json") {
		t.Errorf("scan-secrets with an unreadable file = %d, %q; want exit 1 after a line naming the removed and the unreadable files", code, out)
	}
}

// TestDiagScanSecrets_MoreEncodings (security review DIAG-2): a value is
// also found in a URL's userinfo, as an HTTP Basic credential and inside a
// base64 blob at any byte offset, in unpadded base64url, and in
// lowercase percent-escapes; an ordinary file stays.
func TestDiagScanSecrets_MoreEncodings(t *testing.T) {
	value := "SENT" + "INEL-Pa$s@wo/rd+=?zz>?>?abc" // 31 bytes: its base64 ends in a partial character
	b64 := base64.StdEncoding.EncodeToString
	forms := map[string]string{
		"userinfo-password": "rtsp://" + url.UserPassword("admin", value).String() + "@192.0.2.10/stream1",
		"userinfo-user":     "rtsp://" + url.User(value).String() + "@192.0.2.10/",
		"basic":             "Authorization: Basic " + b64([]byte("camera1:"+value)),
		"blob-offset-0":     b64([]byte(value + `","x":1}`)),
		"blob-offset-1":     b64([]byte(`{"secret":"` + value + `"}`)),
		"blob-offset-2":     base64.URLEncoding.EncodeToString([]byte(`{"s":"` + value + `"}`)),
		"rawurl":            base64.RawURLEncoding.EncodeToString([]byte(value)),
		"lower-query":       lowerEscapes(url.QueryEscape(value)),
		"lower-path":        lowerEscapes(url.PathEscape(value)),
	}
	dir := t.TempDir()
	var want []string
	for name, body := range forms {
		if strings.Contains(body, value) {
			t.Fatalf("the %s form holds the value raw: the test would prove nothing", name)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(`{"note":"`+body+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		want = append(want, name+".json")
	}
	if err := os.WriteFile(filepath.Join(dir, "clean.json"), []byte(`{"verdict":"trece","uri":"rtsp://admin:x@192.0.2.10/"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	slices.Sort(want)
	code, lines, out, stderr := runScan(t, stdinLine(value), "--dir", dir)
	if code != exitOK || len(lines) != 1 {
		t.Fatalf("scan-secrets = %d, %q (%s)", code, out, stderr)
	}
	var got []string
	for _, r := range lines[0]["removed"].([]any) {
		got = append(got, fmt.Sprint(r))
	}
	if !slices.Equal(got, want) {
		t.Errorf("removed %v, want %v", got, want)
	}
}

// TestDiagScanSecrets_RefusesCatalogValuesThatAreNoSecrets (security review
// DIAG-4): a catalog row whose value no OAuth secret takes, as the hub's
// identity could seal (a string that every envelope holds), refuses the
// scan before it removes anything, naming the row and never the value.
func TestDiagScanSecrets_RefusesCatalogValuesThatAreNoSecrets(t *testing.T) {
	f := newOAuthFixture(t)
	sealer, err := crypto.NewSealer(f.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealer.Seal(t.Context(), "client:google:android", []byte(`{"id":"{\"schema\":\"ipcam."}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.cat.PutSecret(t.Context(), "client:google:android", "oauth", sealed, sealer.Backend()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	envelope := filepath.Join(dir, "run1.json")
	if err := os.WriteFile(envelope, []byte(`{"schema":"ipcam.mi6/1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, out, stderr := runScan(t, "", "--config", f.config, "--dir", dir)
	if code != exitFatal || out != "" || !strings.Contains(stderr, "client:google:android") || strings.Contains(stderr, "schema") {
		t.Errorf("scan-secrets = %d, %q, %q; want exit 1, no line, a refusal naming the row only", code, out, stderr)
	}
	if _, err := os.Stat(envelope); err != nil {
		t.Errorf("the envelope was removed (%v)", err)
	}
}

// TestDiagScanSecrets_StdinOneValuePerLine (security review DIAG-5): a
// stdin line holds one {"value":…} object and nothing else; a second
// object or trailing data on a line refuses the scan, without quoting it.
func TestDiagScanSecrets_StdinOneValuePerLine(t *testing.T) {
	dir := t.TempDir()
	for _, stdin := range []string{
		`{"value":"first-value-123"}{"value":"second-value-456"}` + "\n",
		`{"value":"third-value-789"} trailing junk` + "\n",
		`{"value":"fourth-value-012"}}` + "\n",
	} {
		if code, _, out, stderr := runScan(t, stdin, "--dir", dir); code != exitFatal || out != "" || strings.Contains(stderr, "value-") {
			t.Errorf("stdin %q: %d, %q, %q; want exit 1, no line, no echo", stdin, code, out, stderr)
		}
	}
}
