package fakeoauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// clock is a fake time that tests move.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// The tests' clients, their secrets assembled at run time (P46).
func desktop() Client {
	return Client{Provider: Google, Type: TypeDesktop, ID: "desktop.apps", Secret: "SENT" + "INEL-desktop", Project: "p1"}
}
func android() Client {
	return Client{Provider: Google, Type: TypeAndroid, ID: "android.apps", Project: "p1", RedirectURIs: []string{"ro.ipcam.spike:/oauth2redirect"}}
}
func tv() Client {
	return Client{Provider: Google, Type: TypeTV, ID: "tv.apps", Secret: "SENT" + "INEL-tv", Project: "p1"}
}
func web() Client {
	return Client{Provider: Google, Type: TypeWeb, ID: "web.apps", Secret: "SENT" + "INEL-web", Project: "p1",
		RedirectURIs: []string{"http://localhost:7091/oauth/callback"}}
}
func entra() Client {
	return Client{Provider: Microsoft, Type: TypeEntra, ID: "entra-id",
		RedirectURIs: []string{"ro.ipcam.spike://oauth/callback", "http://localhost/oauth/callback"}}
}

const verifier = "a-verifier-of-at-least-forty-three-characters-0123456789"

// authURL is an authorization request of c with redirect.
func authURL(c Client, redirect string) string {
	q := url.Values{"client_id": {c.ID}, "redirect_uri": {redirect}, "response_type": {"code"}, "state": {"st"},
		"code_challenge": {s256(verifier)}, "code_challenge_method": {"S256"}}
	path := PathGoogleAuth
	if c.Provider == Google {
		q.Set("scope", ScopeDriveFile)
		q.Set("access_type", "offline")
	} else {
		path = PathMicrosoftAuth
		q.Set("scope", ScopeFilesAppFolder+" "+ScopeOffline)
		q.Set("response_mode", "query")
	}
	return "http://fake" + path + "?" + q.Encode()
}

// newFake serves a fake with every test client on a clock.
func newFake(t *testing.T, opts Options) (*Fake, *httptest.Server, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 11, 1, 23, 50, 0, 0, time.UTC)}
	opts.Clock = c.Now
	f := New(opts)
	for _, cl := range []Client{desktop(), android(), tv(), web(), entra()} {
		f.AddClient(cl)
	}
	srv := httptest.NewServer(f.Handler())
	t.Cleanup(srv.Close)
	return f, srv, c
}

// post sends form to path and returns the status and JSON answer.
func post(t *testing.T, srv *httptest.Server, path string, form url.Values) (int, map[string]any) {
	t.Helper()
	resp, err := http.PostForm(srv.URL+path, form) //nolint:noctx // a test of the fake
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// codeOf returns the code of a redirect URI.
func codeOf(t *testing.T, redirect string) string {
	t.Helper()
	u, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("state") != "st" {
		t.Errorf("redirect %s lost the state", redirect)
	}
	return u.Query().Get("code")
}

func redeem(t *testing.T, srv *httptest.Server, c Client, code, redirect, verifier string) (int, map[string]any) {
	t.Helper()
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {c.ID}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier}}
	if c.Secret != "" {
		form.Set("client_secret", c.Secret)
	}
	path := PathGoogleToken
	if c.Provider == Microsoft {
		path = PathMicrosoftToken
	}
	return post(t, srv, path, form)
}

// TestFake_CodeFlowIsStrict: a code redeems once, with its client's
// secret, its request's redirect URI byte for byte and its verifier; every
// other redemption is invalid_grant or invalid_client.
func TestFake_CodeFlowIsStrict(t *testing.T) {
	f, srv, clk := newFake(t, Options{})
	const redirect = "http://127.0.0.1:53123/"
	auth := func() string {
		r, err := f.Authorize(authURL(desktop(), redirect))
		if err != nil {
			t.Fatal(err)
		}
		return codeOf(t, r)
	}
	code := auth()
	noSecret := desktop()
	noSecret.Secret = ""
	if status, body := redeem(t, srv, noSecret, code, redirect, verifier); status != http.StatusUnauthorized || body["error"] != "invalid_client" {
		t.Errorf("no secret: %d %v, want invalid_client", status, body)
	}
	if _, body := redeem(t, srv, desktop(), code, "http://127.0.0.1:53124/", verifier); body["error"] != "invalid_grant" {
		t.Errorf("another redirect_uri: %v, want invalid_grant", body)
	}
	if _, body := redeem(t, srv, desktop(), code, redirect, "another-verifier-another-verifier-another-v"); body["error"] != "invalid_grant" {
		t.Errorf("another verifier: %v, want invalid_grant", body)
	}
	status, body := redeem(t, srv, desktop(), code, redirect, verifier)
	if status != http.StatusOK || body["refresh_token"] == nil || body["access_token"] == nil {
		t.Fatalf("the right redemption: %d %v", status, body)
	}
	if _, body := redeem(t, srv, desktop(), code, redirect, verifier); body["error"] != "invalid_grant" {
		t.Errorf("a second redemption: %v, want invalid_grant", body)
	}
	late := auth()
	clk.add(codeLife + time.Second)
	if _, body := redeem(t, srv, desktop(), late, redirect, verifier); body["error"] != "invalid_grant" {
		t.Errorf("a code after 10 min: %v, want invalid_grant", body)
	}
	if tok, ok := f.Lookup(body0(t, status, body)); !ok || tok.ClientID != desktop().ID || tok.Account != defaultAccount || tok.Project != "p1" {
		t.Errorf("Lookup = %+v, %v", tok, ok)
	}
}

func body0(t *testing.T, status int, body map[string]any) string {
	t.Helper()
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	return body["access_token"].(string)
}

// TestFake_ClientTypeRules (FLW-31): each type's flow and redirect URIs.
func TestFake_ClientTypeRules(t *testing.T) {
	f, srv, _ := newFake(t, Options{})
	for _, tc := range []struct {
		name     string
		c        Client
		redirect string
		want     string // "" when the page consents
	}{
		{"Android, its scheme", android(), "ro.ipcam.spike:/oauth2redirect", ""},
		{"Android, another scheme", android(), "ro.other:/oauth2redirect", "redirect_uri_mismatch"},
		{"Desktop, a loopback port", desktop(), "http://127.0.0.1:40000/", ""},
		{"Desktop, localhost", desktop(), "http://localhost:40000/", "redirect_uri_mismatch"},
		{"TV, no code flow", tv(), "http://127.0.0.1:40000/", "unauthorized_client"},
		{"Web, its URI", web(), "http://localhost:7091/oauth/callback", ""},
		{"Entra, Android", entra(), "ro.ipcam.spike://oauth/callback", ""},
		{"Entra, localhost any port", entra(), "http://localhost:51234/oauth/callback", ""},
		{"Entra, another path", entra(), "http://localhost:51234/other", "redirect_uri_mismatch"},
	} {
		_, err := f.Authorize(authURL(tc.c, tc.redirect))
		if got := ""; err != nil {
			got = errCode(err)
			if got != tc.want {
				t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
			}
		} else if tc.want != "" {
			t.Errorf("%s: consented, want %q", tc.name, tc.want)
		}
	}
	// A Web client redeems only with its secret; a public client never
	// sends one.
	r, err := f.Authorize(authURL(web(), "http://localhost:7091/oauth/callback"))
	if err != nil {
		t.Fatal(err)
	}
	noSecret := web()
	noSecret.Secret = ""
	if _, body := redeem(t, srv, noSecret, codeOf(t, r), "http://localhost:7091/oauth/callback", verifier); body["error"] != "invalid_client" {
		t.Errorf("a Web client without its secret: %v, want invalid_client", body)
	}
	r, err = f.Authorize(authURL(android(), "ro.ipcam.spike:/oauth2redirect"))
	if err != nil {
		t.Fatal(err)
	}
	withSecret := android()
	withSecret.Secret = "SENT" + "INEL-x"
	if _, body := redeem(t, srv, withSecret, codeOf(t, r), "ro.ipcam.spike:/oauth2redirect", verifier); body["error"] != "invalid_client" {
		t.Errorf("an Android client with a secret: %v, want invalid_client", body)
	}
	// No S256 challenge, no consent.
	plain := strings.Replace(authURL(desktop(), "http://127.0.0.1:1/"), "code_challenge_method=S256", "code_challenge_method=plain", 1)
	if _, err := f.Authorize(plain); err == nil || errCode(err) != "invalid_request" {
		t.Errorf("a plain challenge: %v, want invalid_request", err)
	}
}

// TestFake_RefreshRotatesAndRevokes (P27): each refresh returns a new
// refresh token and revokes the one it took; in Testing mode a Google
// refresh token dies TestingExpiry after the consent, rotations included.
func TestFake_RefreshRotatesAndRevokes(t *testing.T) {
	f, srv, clk := newFake(t, Options{TestingExpiry: 7 * 24 * time.Hour})
	r, err := f.Authorize(authURL(desktop(), "http://127.0.0.1:1/"))
	if err != nil {
		t.Fatal(err)
	}
	_, body := redeem(t, srv, desktop(), codeOf(t, r), "http://127.0.0.1:1/", verifier)
	rt := body["refresh_token"].(string)
	refresh := func(token string) map[string]any {
		_, body := post(t, srv, PathGoogleToken, url.Values{"grant_type": {"refresh_token"}, "client_id": {desktop().ID},
			"client_secret": {desktop().Secret}, "refresh_token": {token}})
		return body
	}
	for day := 1; day <= 6; day++ {
		clk.add(24 * time.Hour)
		body := refresh(rt)
		next, _ := body["refresh_token"].(string)
		if next == "" || next == rt {
			t.Fatalf("day %d: %v, want a new refresh token", day, body)
		}
		if again := refresh(rt); again["error"] != "invalid_grant" {
			t.Fatalf("day %d: the old refresh token: %v, want invalid_grant", day, again)
		}
		rt = next
	}
	clk.add(24 * time.Hour)
	if body := refresh(rt); body["error"] != "invalid_grant" {
		t.Errorf("day 7 in Testing mode: %v, want invalid_grant", body)
	}
}

// TestFake_DeviceFlow (P39): the device flow is pending until the user
// approves, slow_down when polled faster than its interval, and
// expired_token after its life; Google's takes a TV client only.
func TestFake_DeviceFlow(t *testing.T) {
	f, srv, clk := newFake(t, Options{})
	if status, body := post(t, srv, PathGoogleDevice, url.Values{"client_id": {desktop().ID}, "scope": {ScopeDriveFile}}); status != http.StatusUnauthorized {
		t.Errorf("a Desktop client's device code: %d %v, want 401", status, body)
	}
	_, dev := post(t, srv, PathGoogleDevice, url.Values{"client_id": {tv().ID}, "scope": {ScopeDriveFile}})
	poll := func() map[string]any {
		_, body := post(t, srv, PathGoogleToken, url.Values{"grant_type": {GrantDeviceCode}, "client_id": {tv().ID},
			"client_secret": {tv().Secret}, "device_code": {dev["device_code"].(string)}})
		return body
	}
	if body := poll(); body["error"] != "authorization_pending" {
		t.Errorf("before the approval: %v", body)
	}
	if body := poll(); body["error"] != "slow_down" {
		t.Errorf("polled at once: %v, want slow_down", body)
	}
	clk.add(deviceInterval)
	if err := f.ApproveDevice(dev["user_code"].(string), "acct-1"); err != nil {
		t.Fatal(err)
	}
	if body := poll(); body["refresh_token"] == nil {
		t.Errorf("after the approval: %v", body)
	}
	_, late := post(t, srv, PathMicrosoftDevice, url.Values{"client_id": {entra().ID}, "scope": {ScopeFilesAppFolder + " " + ScopeOffline}})
	clk.add(msDevLife)
	if _, body := post(t, srv, PathMicrosoftToken, url.Values{"grant_type": {GrantDeviceCode}, "client_id": {entra().ID},
		"device_code": {late["device_code"].(string)}}); body["error"] != "expired_token" {
		t.Errorf("after its life: %v, want expired_token", body)
	}
}

// TestFake_AutoApprove: with AutoApprove the authorization page redirects
// with a code at once, and a device code is approved.
func TestFake_AutoApprove(t *testing.T) {
	_, srv, _ := newFake(t, Options{AutoApprove: true})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	u := strings.Replace(authURL(desktop(), "http://127.0.0.1:1/"), "http://fake", srv.URL, 1)
	resp, err := client.Get(u) //nolint:noctx // a test of the fake
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || codeOf(t, resp.Header.Get("Location")) == "" {
		t.Errorf("the page: %d %q, want a redirect with a code", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, dev := post(t, srv, PathGoogleDevice, url.Values{"client_id": {tv().ID}, "scope": {ScopeDriveFile}})
	if _, body := post(t, srv, PathGoogleToken, url.Values{"grant_type": {GrantDeviceCode}, "client_id": {tv().ID},
		"client_secret": {tv().Secret}, "device_code": {dev["device_code"].(string)}}); body["access_token"] == nil {
		t.Errorf("an auto-approved device code: %v", body)
	}
}
