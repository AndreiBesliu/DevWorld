package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// The tests of the residuals of the Task 9A security round (C2-R20).

// stringsOf is a line's list field as strings.
func stringsOf(line map[string]any, field string) []string {
	list, _ := line[field].([]any)
	out := []string{}
	for _, v := range list {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

// writeTree writes files, by their paths relative to dir with "/", 0600.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDiagScanSecrets_QuarantinesAndRemovesNothing (C2-R20 (a), security
// review DIAG-4): a scan never removes a file. Each file that holds a value
// moves, its bytes unchanged, into the scan's own directory of the
// quarantine beside the results root (<results>-quarantine/<scan>/<its
// path>), outside the folder the owner sends, which only the scan's
// identity may read; the line names the moved paths and the scan's
// quarantine. A value that every envelope holds (a schema string that
// passes the value checks) steers the scan to every envelope, and destroys
// none. A second scan moves into a directory of its own.
func TestDiagScanSecrets_QuarantinesAndRemovesNothing(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "results")
	steer := "ipcam.uploadspike.mi6/1" // 16 bytes or more, no character the value checks refuse
	files := map[string]string{
		"MI6/pc/run1.json": `{"schema":"` + steer + `","verdict":"trece"}`,
		"MI5/pc/run2.json": `{"schema":"` + steer + `"}`,
		"notes.txt":        "no envelope here",
	}
	writeTree(t, dir, files)
	code, lines, out, stderr := runScan(t, stdinLine(steer), "--dir", dir)
	if code != exitOK || len(lines) != 1 {
		t.Fatalf("scan-secrets = %d, %q (%s); want one line, exit 0", code, out, stderr)
	}
	want := []string{"MI5/pc/run2.json", "MI6/pc/run1.json"}
	if got := stringsOf(lines[0], "moved"); !slices.Equal(got, want) {
		t.Errorf("moved %v, want %v (line %v)", got, want, lines[0])
	}
	q, _ := lines[0]["quarantine"].(string)
	if path.Dir(q) != "results-quarantine" || strings.Contains(q, "..") {
		t.Fatalf("quarantine %q, want results-quarantine/<scan>, relative to the results root's parent", q)
	}
	for _, rel := range want {
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s is still in the results (%v)", rel, err)
		}
		moved := filepath.Join(base, filepath.FromSlash(q), filepath.FromSlash(rel))
		if b, err := os.ReadFile(moved); err != nil || string(b) != files[rel] { //nolint:gosec // the test's file
			t.Errorf("the quarantined %s: %q, %v; want its bytes unchanged", rel, b, err)
		}
		checkQuarantined(t, moved, false)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Errorf("notes.txt left the results (%v)", err)
	}
	checkQuarantined(t, filepath.Join(base, "results-quarantine"), true)
	checkQuarantined(t, filepath.Join(base, filepath.FromSlash(q)), true)

	// A second scan: a directory of its own, the first one kept.
	writeTree(t, dir, map[string]string{"MI6/pc/run3.json": `{"schema":"` + steer + `"}`})
	code, lines, out, stderr = runScan(t, stdinLine(steer), "--dir", dir)
	if code != exitOK || len(lines) != 1 {
		t.Fatalf("the second scan = %d, %q (%s)", code, out, stderr)
	}
	q2, _ := lines[0]["quarantine"].(string)
	if got := stringsOf(lines[0], "moved"); !slices.Equal(got, []string{"MI6/pc/run3.json"}) || q2 == q || path.Dir(q2) != "results-quarantine" {
		t.Errorf("the second scan: moved %v into %q; want MI6/pc/run3.json in a directory other than %q", got, q2, q)
	}
	// Every file is still on the disk: 3 envelopes and the note.
	n := 0
	if err := filepath.WalkDir(base, func(_ string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			n++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("%d files under the results' parent after the scans, want 4: a scan removed a file", n)
	}
	// A clean scan creates nothing and names no quarantine.
	code, lines, _, _ = runScan(t, stdinLine("SENT"+"INEL-in-no-file-at-all"), "--dir", dir)
	if code != exitOK || len(lines) != 1 || lines[0]["quarantine"] != "" || len(stringsOf(lines[0], "moved")) != 0 {
		t.Errorf("a clean scan = %d, %v; want exit 0, nothing moved, no quarantine", code, lines)
	}
}

// TestControl_UnauthorizedAnswerClosesTheConnection (C2-R20 (b), security
// review CTL-1): an answer sent before the bearer token is checked closes
// its connection, so no local process keeps the endpoint's connections
// open through keep-alive and locks uploadspike out of the cap; an
// authorized answer keeps its connection.
func TestControl_UnauthorizedAnswerClosesTheConnection(t *testing.T) {
	c := &control{token: testControlToken(), conns: &connTracker{conns: map[*trackedConn]bool{}}, reported: map[string]bool{}}
	addr, stop, err := c.serve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	for _, tc := range []struct {
		name, auth string
		status     int
		closes     bool
	}{
		{"no token", "", http.StatusUnauthorized, true},
		{"another token", "Bearer " + strings.Repeat("f", 40), http.StatusUnauthorized, true},
		{"the token", "Bearer " + testControlToken(), http.StatusOK, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			req := "GET /control/v1/visibility/targets HTTP/1.1\r\nHost: h\r\nConnection: keep-alive\r\n"
			if tc.auth != "" {
				req += "Authorization: " + tc.auth + "\r\n"
			}
			if _, err := io.WriteString(conn, req+"\r\n"); err != nil {
				t.Fatal(err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			br := bufio.NewReader(conn)
			resp, err := http.ReadResponse(br, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != tc.status || resp.Close != tc.closes {
				t.Errorf("HTTP %d, Connection: close %v; want %d, %v", resp.StatusCode, resp.Close, tc.status, tc.closes)
			}
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			_, err = br.ReadByte()
			if closed := err == io.EOF; closed != tc.closes { //nolint:errorlint // a read's EOF is io.EOF itself
				t.Errorf("the connection after the answer: read %v; want closed %v", err, tc.closes)
			}
		})
	}
}

// TestHubClient_TokenCallsPoolNoConnection (C2-R20 (d), security review
// CTL-3): the token store's client dials a new connection for each call,
// over HTTP/1.1 and HTTP/2, so no pooled flow to a token endpoint that a
// call left open survives MI5's block (a Windows Firewall rule added under
// an established flow leaves it open).
func TestHubClient_TokenCallsPoolNoConnection(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("HTTP/2 %v", h2), func(t *testing.T) {
			var dialled atomic.Int32
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"access_token":"x"}`)
			}))
			srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
				if s == http.StateNew {
					dialled.Add(1)
				}
			}
			_, tokens, _ := newHubClient()
			if h2 {
				srv.EnableHTTP2 = true
				srv.StartTLS()
				tr := tokens.Transport.(*http.Transport) //nolint:forcetypeassert // newHubClient's own
				tr.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
				tr.TLSClientConfig.MinVersion = tls.VersionTLS12
			} else {
				srv.Start()
			}
			t.Cleanup(srv.Close)
			for range 3 {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/token", strings.NewReader("grant_type=x"))
				if err != nil {
					t.Fatal(err)
				}
				resp, err := tokens.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if h2 && resp.ProtoMajor != 2 {
					t.Fatalf("the call went over %s, want HTTP/2", resp.Proto)
				}
			}
			if n := dialled.Load(); n != 3 {
				t.Errorf("3 token calls used %d connections, want 3: a pooled connection outlives its call", n)
			}
		})
	}
}

// TestControl_RefreshOutlivesItsCaller (C2-R20 (g), security review
// CTL-4): the control endpoint's refresh goes on when its caller goes
// away while the provider answers, as complete and status do: the
// provider has rotated and revoked the old token by then, so a refresh cut
// there would leave the row with a revoked token.
func TestControl_RefreshOutlivesItsCaller(t *testing.T) {
	f := newOAuthFixture(t)
	f.logIn(t, "g-windows", oauth.PlatformWindows, "acct-1")
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	srv := interceptedLoginServer(t, f, func(w http.ResponseWriter, r *http.Request) bool {
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
		select {
		case <-release:
		case <-r.Context().Done(): // the hub gave up the call
			return true
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
		return true
	})
	_, tokens, _ := newHubClient()
	s := fixtureStore(t, f, srv.URL, tokens)
	var out syncBuffer
	c := &control{token: testControlToken(), store: s, cat: f.cat, ev: &events{out: &out}, conns: &connTracker{conns: map[*trackedConn]bool{}},
		reported: map[string]bool{}}
	hub := httptest.NewServer(c.handler())
	t.Cleanup(hub.Close)
	ctx, cancel := context.WithCancel(t.Context())
	called := make(chan error, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, hub.URL+"/control/v1/oauth/refresh", strings.NewReader(`{"destId":"g-windows"}`))
		if err != nil {
			called <- err
			return
		}
		req.Header.Set("Authorization", "Bearer "+testControlToken())
		resp, err := (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}).Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		called <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the refresh did not reach the token endpoint")
	}
	cancel() // the caller goes away while the provider answers
	<-called
	time.Sleep(500 * time.Millisecond) // the hub sees its caller gone
	close(release)
	if _, err := s.Refresh(t.Context(), "g-windows"); err != nil {
		t.Errorf("the refresh after one whose caller went away = %v; want the rotated token stored", err)
	}
	if n := countEvents(out.String(), "oauth_failed", "g-windows"); n != 0 {
		t.Errorf("%d oauth_failed events for the refresh whose caller went away, want 0:\n%s", n, out.String())
	}
}
