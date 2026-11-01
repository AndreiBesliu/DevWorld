package oauth

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakeoauth "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/oauth"
)

// testClock is a clock that tests move.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// The tests' OAuth clients. Their IDs and secrets are assembled at run time
// and never take a provider's shape (P46).
var (
	idAndroid, idDesktop, idTV, idWeb, idEntra = "android-client", "desktop-client", "tv-client", "web-client", "entra-client"
	secretDesktop                              = "SENT" + "INEL-desktop-secret"
	secretTV                                   = "SENT" + "INEL-tv-secret"
	secretWeb                                  = "SENT" + "INEL-web-secret"
)

// hubClients are the clients as the hub holds them: the Web client's secret
// never reaches the hub.
func hubClients() Clients {
	return Clients{
		Google: map[ClientType]Client{
			ClientAndroid: {ID: idAndroid}, ClientDesktop: {ID: idDesktop, Secret: secretDesktop},
			ClientTV: {ID: idTV, Secret: secretTV}, ClientWeb: {ID: idWeb},
		},
		Microsoft: &Client{ID: idEntra},
	}
}

// env is a fake provider, a catalog in a data directory, a sealer and a
// clock, for the tests of the store.
type env struct {
	t       *testing.T
	clk     *testClock
	fake    *fakeoauth.Fake
	srv     *httptest.Server
	dataDir string
	cat     *catalog.DB
	sealer  crypto.Sealer
	// tokenForms records the forms that reached the token endpoints.
	mu         sync.Mutex
	tokenForms []url.Values
}

func newEnv(t *testing.T, opts fakeoauth.Options) *env {
	t.Helper()
	e := &env{t: t, clk: &testClock{now: time.Date(2026, 11, 1, 23, 50, 0, 0, time.UTC)}}
	opts.Clock = e.clk.Now
	e.fake = fakeoauth.New(opts)
	for _, c := range []fakeoauth.Client{
		{Provider: fakeoauth.Google, Type: fakeoauth.TypeAndroid, ID: idAndroid, Project: "p1", RedirectURIs: []string{"ro.ipcam.spike:/oauth2redirect"}},
		{Provider: fakeoauth.Google, Type: fakeoauth.TypeDesktop, ID: idDesktop, Secret: secretDesktop, Project: "p1"},
		{Provider: fakeoauth.Google, Type: fakeoauth.TypeTV, ID: idTV, Secret: secretTV, Project: "p1"},
		{Provider: fakeoauth.Google, Type: fakeoauth.TypeWeb, ID: idWeb, Secret: secretWeb, Project: "p1",
			RedirectURIs: []string{"http://localhost:7091/reader"}},
		{Provider: fakeoauth.Microsoft, Type: fakeoauth.TypeEntra, ID: idEntra,
			RedirectURIs: []string{"ro.ipcam.spike://oauth/callback", "http://localhost/oauth/callback"}},
	} {
		e.fake.AddClient(c)
	}
	handler := e.fake.Handler()
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			if err := r.ParseForm(); err == nil {
				e.mu.Lock()
				e.tokenForms = append(e.tokenForms, r.PostForm)
				e.mu.Unlock()
			}
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(e.srv.Close)
	e.dataDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(e.dataDir, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	e.cat = e.openCatalog()
	var err error
	if e.sealer, err = crypto.NewSealer(e.dataDir); err != nil {
		t.Fatal(err)
	}
	return e
}

// openCatalog opens another handle of the env's catalog, as another
// process would.
func (e *env) openCatalog() *catalog.DB {
	e.t.Helper()
	cat, err := catalog.Open(e.t.Context(), filepath.Join(e.dataDir, "state", "catalog.db"))
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = cat.Close() })
	return cat
}

// config is a store's configuration on the env, with the hub's clients.
func (e *env) config() Config {
	return Config{
		Clients: hubClients(), Endpoints: Endpoints(fakeoauth.URLsAt(e.srv.URL)), Clock: e.clk.Now, Sealer: e.sealer,
		HTTP: e.srv.Client(), Secrets: e.cat, TestingExpiry: 7 * 24 * time.Hour,
	}
}

func (e *env) store(change ...func(*Config)) *Store {
	e.t.Helper()
	cfg := e.config()
	for _, c := range change {
		c(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

// login runs a whole login of destID on s: plan A through the fake's
// consent page, or B1 through its device page.
func (e *env) login(s *Store, destID string, provider storage.Kind, platform Platform, plan string) Completion {
	e.t.Helper()
	ctx := e.t.Context()
	begin, err := s.Begin(ctx, BeginRequest{DestID: destID, Provider: provider, Platform: platform, LoopbackPort: 53123, Plan: plan})
	if err != nil {
		e.t.Fatalf("Begin: %v", err)
	}
	if begin.Plan == PlanB1 {
		if err := e.fake.ApproveDevice(begin.UserCode, "acct-1"); err != nil {
			e.t.Fatal(err)
		}
		c, err := s.DeviceStatus(ctx, begin.State)
		if err != nil {
			e.t.Fatalf("DeviceStatus: %v", err)
		}
		return c
	}
	redirect, err := e.fake.Authorize(begin.AuthURL)
	if err != nil {
		e.t.Fatalf("the consent page: %v", err)
	}
	c, err := s.Complete(ctx, begin.State, codeOf(e.t, redirect, begin.State))
	if err != nil {
		e.t.Fatalf("Complete: %v", err)
	}
	return c
}

// codeOf is the code of a redirect, which must carry state.
func codeOf(t *testing.T, redirect, state string) string {
	t.Helper()
	u, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("state") != state {
		t.Fatalf("the redirect lost the state")
	}
	return u.Query().Get("code")
}

// TestMI6_ClientTypePerPlatform (FLW-31, the strict fake): the hub uses each
// platform's client type and repeats Begin's redirect URI byte for byte
// when it redeems; another redirect URI is invalid_grant; a Web client
// without its secret is refused; web × Google is the hub's device flow with
// the TV client.
func TestMI6_ClientTypePerPlatform(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	ctx := t.Context()
	for _, tc := range []struct {
		dest     string
		provider storage.Kind
		platform Platform
		client   ClientType
	}{
		{"g-android", storage.KindGoogleDrive, PlatformAndroid, ClientAndroid},
		{"g-windows", storage.KindGoogleDrive, PlatformWindows, ClientDesktop},
		{"m-android", storage.KindOneDrivePersonal, PlatformAndroid, ClientEntra},
		{"m-windows", storage.KindOneDrivePersonal, PlatformWindows, ClientEntra},
		{"m-web", storage.KindOneDrivePersonal, PlatformWeb, ClientEntra},
	} {
		begin, err := s.Begin(ctx, BeginRequest{DestID: tc.dest, Provider: tc.provider, Platform: tc.platform, LoopbackPort: 53123})
		if err != nil {
			t.Fatalf("%s: Begin: %v", tc.dest, err)
		}
		redirect, err := e.fake.Authorize(begin.AuthURL)
		if err != nil {
			t.Fatalf("%s: the consent page: %v", tc.dest, err)
		}
		e.mu.Lock()
		e.tokenForms = nil
		e.mu.Unlock()
		c, err := s.Complete(ctx, begin.State, codeOf(t, redirect, begin.State))
		if err != nil || c.ClientType != tc.client || c.Plan != PlanA || c.DestID != tc.dest {
			t.Errorf("%s: Complete = %+v, %v; want client type %s, plan A", tc.dest, c, err, tc.client)
		}
		e.mu.Lock()
		forms := e.tokenForms
		e.mu.Unlock()
		if len(forms) != 1 || forms[0].Get("redirect_uri") != begin.RedirectURI || forms[0].Get("code_verifier") == "" {
			t.Errorf("%s: the token requests %v, want one with Begin's redirect URI %q and the verifier", tc.dest, len(forms), begin.RedirectURI)
		}
	}
	// Another redirect URI than the authorization request's: invalid_grant.
	begin, err := s.Begin(ctx, BeginRequest{DestID: "g-windows", Provider: storage.KindGoogleDrive, Platform: PlatformWindows, LoopbackPort: 53123})
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := e.fake.Authorize(begin.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	verifier := s.pendingVerifier(begin.State)
	if status, body := redeemAt(t, e, idDesktop, secretDesktop, codeOf(t, redirect, begin.State), "http://127.0.0.1:53124/", verifier); body["error"] != "invalid_grant" {
		t.Errorf("another redirect_uri: HTTP %d %v, want invalid_grant", status, body)
	}
	// A Web client without its secret.
	if status, body := postForm(t, e.srv.URL+fakeoauth.PathGoogleToken, url.Values{"grant_type": {"refresh_token"},
		"client_id": {idWeb}, "refresh_token": {"x"}}); status != http.StatusUnauthorized || body["error"] != "invalid_client" {
		t.Errorf("a Web client without its secret: HTTP %d %v, want invalid_client", status, body)
	}
	// Web × Google: B1 with the TV client.
	c := e.login(s, "g-web", storage.KindGoogleDrive, PlatformWeb, "")
	if c.Plan != PlanB1 || c.ClientType != ClientTV || c.Platform != PlatformWeb {
		t.Errorf("web × Google: %+v, want B1 with the TV client", c)
	}
}

// redeemAt redeems code at the fake's Google token endpoint as client.
func redeemAt(t *testing.T, e *env, clientID, secret, code, redirect, verifier string) (int, map[string]any) {
	t.Helper()
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code}, "redirect_uri": {redirect}}
	if secret != "" {
		form.Set("client_secret", secret)
	}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return postForm(t, e.srv.URL+fakeoauth.PathGoogleToken, form)
}

func postForm(t *testing.T, u string, form url.Values) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// TestOAuth_StateSingleUseAndExpiry (P27, FLW-22): a state lives 10 min:
// Complete at 10 min + 1 s is ErrStateExpired and wipes its verifier; a
// second Complete of a used state is ErrStateUsed; an unknown one
// ErrStateUnknown.
func TestOAuth_StateSingleUseAndExpiry(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	ctx := t.Context()
	begin, err := s.Begin(ctx, BeginRequest{DestID: "g-android", Provider: storage.KindGoogleDrive, Platform: PlatformAndroid})
	if err != nil {
		t.Fatal(err)
	}
	if want := e.clk.Now().Add(10 * time.Minute); !begin.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt %v, want %v", begin.ExpiresAt, want)
	}
	redirect, err := e.fake.Authorize(begin.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	e.clk.Add(10*time.Minute + time.Second)
	if _, err := s.Complete(ctx, begin.State, codeOf(t, redirect, begin.State)); !errors.Is(err, ErrStateExpired) {
		t.Errorf("Complete at 10 min + 1 s = %v, want ErrStateExpired", err)
	}
	if v := s.pendingVerifier(begin.State); v != "" {
		t.Error("the expired state kept its verifier")
	}
	begin, err = s.Begin(ctx, BeginRequest{DestID: "g-android", Provider: storage.KindGoogleDrive, Platform: PlatformAndroid})
	if err != nil {
		t.Fatal(err)
	}
	redirect, err = e.fake.Authorize(begin.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	code := codeOf(t, redirect, begin.State)
	if _, err := s.Complete(ctx, begin.State, code); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(ctx, begin.State, code); !errors.Is(err, ErrStateUsed) {
		t.Errorf("a second Complete = %v, want ErrStateUsed", err)
	}
	if v := s.pendingVerifier(begin.State); v != "" {
		t.Error("the used state kept its verifier")
	}
	if _, err := s.Complete(ctx, "no-such-state", code); !errors.Is(err, ErrStateUnknown) {
		t.Errorf("an unknown state = %v, want ErrStateUnknown", err)
	}
}

// fixedRand is a Rand of known bytes, so that a test knows the verifier.
type fixedRand struct{ next byte }

func (r *fixedRand) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.next
		r.next++
	}
	return len(p), nil
}

// TestHandoff_VerifierNeverLeavesHub (Review Focus 5, FLW-22): the verifier
// that the hub makes from its Rand redeems the code, and only it: an
// attacker who holds the code but not the verifier gets invalid_grant. The
// verifier is in no BeginResult, Completion or error text.
func TestHandoff_VerifierNeverLeavesHub(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store(func(c *Config) { c.Rand = &fixedRand{} })
	ctx := t.Context()
	begin, err := s.Begin(ctx, BeginRequest{DestID: "g-windows", Provider: storage.KindGoogleDrive, Platform: PlatformWindows, LoopbackPort: 53123})
	if err != nil {
		t.Fatal(err)
	}
	verifier := s.pendingVerifier(begin.State)
	if len(verifier) < 43 {
		t.Fatalf("verifier of %d characters, want at least 43 (RFC 7636)", len(verifier))
	}
	redirect, err := e.fake.Authorize(begin.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	code := codeOf(t, redirect, begin.State)
	for _, v := range []string{"", strings.Repeat("A", 43)} {
		if _, body := redeemAt(t, e, idDesktop, secretDesktop, code, begin.RedirectURI, v); body["error"] != "invalid_grant" {
			t.Errorf("the code with verifier %q: %v, want invalid_grant", v, body)
		}
	}
	// The attacker's tries did not use the code up: the fake refuses a
	// redemption without the verifier before it marks the code used.
	c, err := s.Complete(ctx, begin.State, code)
	if err != nil {
		t.Fatalf("the hub's Complete: %v", err)
	}
	_, errAgain := s.Complete(ctx, begin.State, code)
	for name, v := range map[string]any{"BeginResult": begin, "Completion": c, "the second Complete's error": errAgain.Error()} {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), verifier) {
			t.Errorf("%s holds the verifier", name)
		}
	}
}

// TestTokenSource_InvalidGrantIsAuthExpired (P27): a refresh token that
// the provider revoked, with the row unchanged, is storage.ErrAuthExpired,
// through Refresh and through the TokenSource once its access token
// expired, so the outbox holds the destination BLOCKED_AUTH.
func TestTokenSource_InvalidGrantIsAuthExpired(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	s.rotationGrace = time.Millisecond
	e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
	for _, rt := range e.fake.RefreshTokens() {
		postForm(t, e.srv.URL+fakeoauth.PathGoogleRevoke, url.Values{"token": {rt}})
	}
	if _, err := s.Refresh(t.Context(), "g-windows"); !errors.Is(err, storage.ErrAuthExpired) || errors.Is(err, storage.ErrTransient) {
		t.Errorf("Refresh = %v, want ErrAuthExpired", err)
	}
	e.clk.Add(2 * time.Hour)
	if _, err := s.TokenSource(t.Context(), "g-windows").Token(); !errors.Is(err, storage.ErrAuthExpired) {
		t.Errorf("Token = %v, want ErrAuthExpired", err)
	}
	if _, err := s.TokenSource(t.Context(), "no-login").Token(); !errors.Is(err, storage.ErrAuthExpired) {
		t.Errorf("Token of a ref with no login = %v, want ErrAuthExpired", err)
	}
}

// TestTokenSource_NetworkErrorIsTransient (P26, P27): a refused connection,
// a timeout and a 503 of the token endpoint are storage.ErrTransient, so a
// blocked WAN ends in backoff and recovers with no new login; invalid_grant
// on an unchanged row is ErrAuthExpired. No error text holds the
// endpoint's URL.
func TestTokenSource_NetworkErrorIsTransient(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := "http://" + closed.Addr().String()
	_ = closed.Close()
	hold := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-hold:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(hold) }) // first: the server's Close waits for its handlers
	for _, tc := range []struct {
		name, tokenURL string
		prepare        func()
		want           error
	}{
		{"a refused connection", refused + "/google/token", nil, storage.ErrTransient},
		{"a timeout", slow.URL + "/google/token", nil, storage.ErrTransient},
		{"a 503", e.srv.URL + fakeoauth.PathGoogleToken, func() {
			e.fake.FailNext(fakeoauth.PathGoogleToken, http.StatusServiceUnavailable, "temporarily_unavailable")
		}, storage.ErrTransient},
		{"a 429", e.srv.URL + fakeoauth.PathGoogleToken, func() { e.fake.FailNext(fakeoauth.PathGoogleToken, http.StatusTooManyRequests, "rate_limit_exceeded") }, storage.ErrTransient},
		{"invalid_grant", e.srv.URL + fakeoauth.PathGoogleToken, func() { e.fake.FailNext(fakeoauth.PathGoogleToken, http.StatusBadRequest, "invalid_grant") }, storage.ErrAuthExpired},
		{"invalid_client", e.srv.URL + fakeoauth.PathGoogleToken, func() { e.fake.FailNext(fakeoauth.PathGoogleToken, http.StatusUnauthorized, "invalid_client") }, storage.ErrAuthExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := e.config()
			cfg.Endpoints.GoogleToken = tc.tokenURL
			st, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			st.tokenTimeout, st.rotationGrace = 300*time.Millisecond, time.Millisecond
			if tc.prepare != nil {
				tc.prepare()
			}
			start := time.Now()
			_, err = st.Refresh(t.Context(), "g-windows")
			if !errors.Is(err, tc.want) {
				t.Errorf("Refresh = %v, want %v", err, tc.want)
			}
			other := storage.ErrAuthExpired
			if tc.want == storage.ErrAuthExpired {
				other = storage.ErrTransient
			}
			if errors.Is(err, other) {
				t.Errorf("Refresh = %v, also %v", err, other)
			}
			if err != nil && (strings.Contains(err.Error(), "127.0.0.1") || strings.Contains(err.Error(), "/token")) {
				t.Errorf("the error %q names the endpoint", err)
			}
			if took := time.Since(start); took > 3*time.Second {
				t.Errorf("Refresh took %v", took)
			}
		})
	}
}

// TestStore_TwoStoresSameCatalog_NoStaleRefresh (P27): two stores on one
// catalog file, as two processes, alternate 10 refreshes against the
// rotating fake, which revokes each refresh token it replaces: every
// refresh succeeds, also when the other store rotates the token between a
// store's read of the row and its call.
func TestStore_TwoStoresSameCatalog_NoStaleRefresh(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	a := e.store()
	e.login(a, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
	b := e.store(func(c *Config) { c.Secrets = e.openCatalog() })
	ctx := t.Context()
	for i := range 10 {
		s, other := a, b
		if i%2 == 1 {
			s, other = b, a
		}
		if i == 4 {
			// The other store rotates the token after s read the row.
			s.beforeCall = func() {
				s.beforeCall = nil
				if _, err := other.Refresh(ctx, "g-windows"); err != nil {
					t.Errorf("the other store's refresh: %v", err)
				}
			}
		}
		if res, err := s.Refresh(ctx, "g-windows"); err != nil || !res.Rotated {
			t.Fatalf("refresh %d = %+v, %v; want a rotated token", i+1, res, err)
		}
	}
}

// TestOAuth_NoPlaintextRefreshTokenInDataDir (P19): after a login and a
// rotation, no file under the data directory holds a refresh token, an
// access token or a client secret: the catalog keeps them sealed.
func TestOAuth_NoPlaintextRefreshTokenInDataDir(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	if err := s.SetClients(t.Context(), hubClients()); err != nil {
		t.Fatal(err)
	}
	e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
	if _, err := s.Refresh(t.Context(), "g-windows"); err != nil {
		t.Fatal(err)
	}
	secrets := append(e.fake.RefreshTokens(), e.fake.IssuedAccessTokens()...)
	secrets = append(secrets, secretDesktop, secretTV)
	if len(secrets) < 6 {
		t.Fatalf("the fake issued %d tokens", len(secrets))
	}
	err := filepath.WalkDir(e.dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path) //nolint:gosec // the test's data directory
		if err != nil {
			return err
		}
		for _, secret := range secrets {
			if bytes.Contains(b, []byte(secret)) {
				t.Errorf("%s holds a secret in plaintext", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRefresh_SlowTokenEndpointDoesNotBlockCatalogWrites (P27): a token
// endpoint that holds the request 30 s makes Refresh return ErrTransient
// within 6 s, its 5 s deadline, while a PutSegment with its enqueue on the
// same catalog file commits within 1 s: no transaction spans the call.
func TestRefresh_SlowTokenEndpointDoesNotBlockCatalogWrites(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
	arrived, hold := make(chan struct{}, 1), make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		select {
		case <-hold:
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(hold) }) // first: the server's Close waits for its handlers
	cfg := e.config()
	cfg.Endpoints.GoogleToken = slow.URL
	st, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := catalog.OpenWith(t.Context(), filepath.Join(e.dataDir, "state", "catalog.db"), catalog.Options{
		OnSegmentComplete: func(ctx context.Context, tx *sql.Tx, seg catalog.Segment) error {
			_, err := catalog.PutPlacementTx(ctx, tx, catalog.Placement{ObjectKind: catalog.ObjectSegment, ObjectID: seg.ID,
				DestinationID: "g-windows", Class: catalog.ClassContinuous, State: catalog.PlacementPending, Bytes: seg.Bytes})
			return err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := st.Refresh(t.Context(), "g-windows")
		done <- err
	}()
	<-arrived
	putStart := time.Now()
	if err := writer.PutSegment(t.Context(), catalog.Segment{Cam: "c1", Profile: "main", Path: "c1/main/d/a.mp4",
		StartMs: 1000, EndMs: 2000, Bytes: 10, State: catalog.StateComplete}); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(putStart); took > time.Second {
		t.Errorf("PutSegment with its enqueue took %v while the token endpoint held a refresh, want under 1 s", took)
	}
	select {
	case err := <-done:
		if !errors.Is(err, storage.ErrTransient) {
			t.Errorf("Refresh = %v, want ErrTransient", err)
		}
		if took := time.Since(start); took > 6*time.Second {
			t.Errorf("Refresh took %v, want its 5-s deadline", took)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Refresh did not return within 10 s")
	}
}

// The helper process of TestOAuth_RefreshAfterKill9Restart: with
// helperEnv set, the test binary refreshes the login of helperRef once,
// prints "refreshed" and waits to be killed.
const (
	helperEnv     = "OAUTH_TEST_REFRESH_HELPER"
	helperDataEnv = "OAUTH_TEST_DATA_DIR"
	helperURLEnv  = "OAUTH_TEST_FAKE_URL"
	helperRef     = "g-windows"
)

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(refreshHelper())
	}
	os.Exit(m.Run())
}

// refreshHelper is the helper process: a store with no client in RAM, so
// it reads the sealed ones, refreshes, reports and hangs.
func refreshHelper() int {
	ctx := context.Background()
	dataDir := os.Getenv(helperDataEnv)
	cat, err := catalog.Open(ctx, filepath.Join(dataDir, "state", "catalog.db"))
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 1
	}
	sealer, err := crypto.NewSealer(dataDir)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 1
	}
	s, err := New(Config{Endpoints: Endpoints(fakeoauth.URLsAt(os.Getenv(helperURLEnv))), Sealer: sealer, HTTP: http.DefaultClient, Secrets: cat})
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		return 1
	}
	if res, err := s.Refresh(ctx, helperRef); err != nil || !res.Rotated {
		_, _ = os.Stderr.WriteString("refresh failed\n")
		return 1
	}
	_, _ = os.Stdout.WriteString("refreshed\n")
	select {} // killed here
}

// TestOAuth_RefreshAfterKill9Restart (P19, P27): a process refreshes (the
// fake rotates the refresh token and revokes the old one) and is killed; a
// new store on the same catalog unseals the rotated token and refreshes.
// The windows job runs it too, with DPAPI.
func TestOAuth_RefreshAfterKill9Restart(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	e.clk.now = time.Now() // the helper runs on the real clock
	s := e.store()
	if err := s.SetClients(t.Context(), hubClients()); err != nil {
		t.Fatal(err)
	}
	e.login(s, helperRef, storage.KindGoogleDrive, PlatformWindows, "")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), helperEnv+"=1", helperDataEnv+"="+e.dataDir, helperURLEnv+"="+e.srv.URL)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Scan()
		line <- sc.Text()
		_, _ = io.Copy(io.Discard, stdout)
	}()
	select {
	case l := <-line:
		if l != "refreshed" {
			t.Fatalf("the helper printed %q; stderr: %s", l, stderr.String())
		}
	case <-time.After(60 * time.Second):
		t.Fatalf("the helper did not refresh within 60 s; stderr: %s", stderr.String())
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	restarted := e.store(func(c *Config) { c.Clients = Clients{}; c.Secrets = e.openCatalog() })
	if res, err := restarted.Refresh(t.Context(), helperRef); err != nil || !res.Rotated {
		t.Errorf("the refresh after the kill = %+v, %v; want the rotated token to refresh", res, err)
	}
}

// pendingVerifier is the verifier that the hub keeps for state, "" once
// used, expired or for none.
func (s *Store) pendingVerifier(state string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.pending[state]; p != nil {
		return p.verifier
	}
	return ""
}
