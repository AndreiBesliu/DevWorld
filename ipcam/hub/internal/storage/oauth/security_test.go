package oauth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakeoauth "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/oauth"
)

// The tests of the Task 9A security review's findings on the token store.

// interceptor answers a token endpoint request itself when it returns
// true; the request goes on to the fake otherwise. It must not read the
// body of a request it passes on.
type interceptor func(w http.ResponseWriter, r *http.Request) bool

// interceptedStore is a store of the env whose provider endpoints go
// through a server that offers each token request to answer first.
func (e *env) interceptedStore(answer interceptor, change ...func(*Config)) *Store {
	e.t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") && answer(w, r) {
			return
		}
		e.srv.Config.Handler.ServeHTTP(w, r)
	}))
	e.t.Cleanup(srv.Close)
	return e.store(append([]func(*Config){func(c *Config) { c.Endpoints = Endpoints(fakeoauth.URLsAt(srv.URL)) }}, change...)...)
}

// grantOf peeks at a token request's grant type and puts its body back.
func grantOf(r *http.Request) string {
	b, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(b))
	form, _ := url.ParseQuery(string(b))
	return form.Get("grant_type")
}

// recorded runs the fake on r into a recorder: the provider's answer,
// already decided (a code redeemed, a token rotated), not yet sent.
func (e *env) recorded(r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.srv.Config.Handler.ServeHTTP(rec, r)
	return rec
}

// send writes a recorded answer to w.
func send(w http.ResponseWriter, rec *httptest.ResponseRecorder) {
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

// TestStore_SealerFaultIsNotAuthExpired (security review SEAL-2): a key
// that the sealer refuses (a widened mode, as after a chmod -R or a
// restore) is a fault of the sealer, crypto.ErrKeyUnavailable, never
// storage.ErrAuthExpired: a new login would not mend it, and the outbox
// must not ask for one. Mending the key mends the store. A ctx that ends
// between the catalog's read and the unseal is ErrTransient.
func TestStore_SealerFaultIsNotAuthExpired(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	ctx := t.Context()
	if err := s.SetClients(ctx, hubClients()); err != nil {
		t.Fatal(err)
	}
	e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
	t.Run("a ctx that ends after the row's read", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		ended := e.store(func(c *Config) { c.Secrets = &cancelAfterRead{SecretStore: e.cat, cancel: cancel} })
		if _, err := ended.Refresh(cctx, "g-windows"); !errors.Is(err, storage.ErrTransient) || errors.Is(err, storage.ErrAuthExpired) {
			t.Errorf("Refresh whose ctx ends after the row's read = %v; want ErrTransient", err)
		}
	})
	if runtime.GOOS == "windows" {
		t.Skip("a refused key is the software sealer's: DPAPI keeps no key file")
	}
	key := filepath.Join(e.dataDir, "state", "keys", "sealer.key")
	if err := os.Chmod(key, 0o640); err != nil { //nolint:gosec // G302: the refused mode under test
		t.Fatal(err)
	}
	sealer, err := crypto.NewSealer(e.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	fresh := e.store(func(c *Config) { c.Clients, c.Sealer = Clients{}, sealer })
	_, tokErr := fresh.TokenSource(ctx, "g-windows").Token()
	_, beginErr := fresh.Begin(ctx, BeginRequest{DestID: "g-android", Provider: storage.KindGoogleDrive, Platform: PlatformAndroid})
	for name, err := range map[string]error{"Token": tokErr, "Begin": beginErr} {
		if !errors.Is(err, crypto.ErrKeyUnavailable) || errors.Is(err, storage.ErrAuthExpired) || Reason(err) != "error" {
			t.Errorf("%s with a refused key = %v; want crypto.ErrKeyUnavailable, not ErrAuthExpired, reason error", name, err)
		}
	}
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.TokenSource(ctx, "g-windows").Token(); err != nil {
		t.Errorf("Token once the key is 0600 again = %v; want the access token", err)
	}
}

// cancelAfterRead is a SecretStore that ends a ctx once it read a row.
type cancelAfterRead struct {
	SecretStore
	cancel func()
}

func (c *cancelAfterRead) Secret(ctx context.Context, id string) ([]byte, string, bool, error) {
	sealed, sealer, ok, err := c.SecretStore.Secret(ctx, id)
	c.cancel()
	return sealed, sealer, ok, err
}

// failingSwaps is a SecretStore whose first fail SwapSecret calls fail, as
// SQLITE_BUSY after the busy timeout does.
type failingSwaps struct {
	SecretStore
	fail atomic.Int32
}

func (f *failingSwaps) SwapSecret(ctx context.Context, id string, old, next []byte, sealer string) (bool, error) {
	if f.fail.Add(-1) >= 0 {
		return false, errors.New("database is locked (5) (SQLITE_BUSY)")
	}
	return f.SecretStore.SwapSecret(ctx, id, old, next, sealer)
}

// TestRefresh_RotationKeptWhenItsStoreFails (security review STORE-1): the
// provider rotates the refresh token and revokes the old one; when the
// swap of the rotated row then fails, the refresh is ErrTransient, the
// rotated token is kept in RAM and written before the next call, so the
// next refresh succeeds instead of meeting invalid_grant on the revoked
// one. A caller whose ctx ends right after the provider's answer still
// gets the rotation stored.
func TestRefresh_RotationKeptWhenItsStoreFails(t *testing.T) {
	t.Run("a swap that fails", func(t *testing.T) {
		e := newEnv(t, fakeoauth.Options{})
		secrets := &failingSwaps{SecretStore: e.cat}
		s := e.store(func(c *Config) { c.Secrets = secrets })
		s.rotationGrace = time.Millisecond
		e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
		secrets.fail.Store(1)
		if _, err := s.Refresh(t.Context(), "g-windows"); !errors.Is(err, storage.ErrTransient) {
			t.Fatalf("the refresh whose swap failed = %v; want ErrTransient", err)
		}
		if res, err := s.Refresh(t.Context(), "g-windows"); err != nil || !res.Rotated {
			t.Fatalf("the next refresh = %+v, %v; want the kept rotation written and refreshed", res, err)
		}
		other := e.store(func(c *Config) { c.Secrets = e.openCatalog() })
		if _, err := other.Refresh(t.Context(), "g-windows"); err != nil {
			t.Errorf("another store's refresh from the row = %v; want the row to hold the live token", err)
		}
	})
	t.Run("a caller gone after the answer", func(t *testing.T) {
		e := newEnv(t, fakeoauth.Options{})
		s := e.store()
		s.rotationGrace = time.Millisecond
		e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
		ctx, cancel := context.WithCancel(t.Context())
		gone := e.store(func(c *Config) {
			c.HTTP = &http.Client{Transport: cancelAfterAnswer{rt: e.srv.Client().Transport, cancel: cancel}}
		})
		_, _ = gone.Refresh(ctx, "g-windows")
		if _, err := s.Refresh(t.Context(), "g-windows"); err != nil {
			t.Errorf("the refresh after one whose caller went away = %v; want the rotated token stored", err)
		}
	})
}

// cancelAfterAnswer is a transport that reads the whole answer and then
// ends its caller's ctx: the caller goes away right after the provider
// answered.
type cancelAfterAnswer struct {
	rt     http.RoundTripper
	cancel func()
}

func (c cancelAfterAnswer) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := c.rt.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(b))
	c.cancel()
	return resp, nil
}

// TestProviderError_Classes (security review STORE-3, Task 9A review M3,
// C2-R20 (e)): one classifier: the refusals that need a new login, and an
// unknown OAuth code ("error") at a 4xx but 408 and 429, are
// ErrAuthExpired; the transient OAuth codes at any status, 408, 429, a 5xx
// and an answer without an OAuth code ("": a proxy's HTML 403 or 407) are
// ErrTransient.
func TestProviderError_Classes(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		auth   bool
	}{
		{400, "invalid_grant", true}, {401, "invalid_client", true}, {400, "unauthorized_client", true},
		{400, "access_denied", true}, {400, "expired_token", true}, {400, "redirect_uri_mismatch", true},
		{400, "invalid_scope", true}, {400, "unsupported_grant_type", true}, {400, "invalid_request", true},
		{400, "temporarily_unavailable", false}, {400, "server_error", false}, {400, "rate_limit_exceeded", false},
		{403, "", false}, {407, "", false}, {400, "", false}, {408, "", false}, {408, "invalid_grant", false},
		{403, "error", true}, {400, "error", true}, {401, "error", true}, {408, "error", false}, {429, "error", false},
		{429, "invalid_grant", false}, {503, "error", false}, {500, "invalid_grant", false},
	} {
		err := error(&ProviderError{Code: tc.code, Status: tc.status})
		if auth, transient := errors.Is(err, storage.ErrAuthExpired), errors.Is(err, storage.ErrTransient); auth != tc.auth || transient == tc.auth {
			t.Errorf("%s (HTTP %d): ErrAuthExpired %v, ErrTransient %v; want ErrAuthExpired %v", tc.code, tc.status, auth, transient, tc.auth)
		}
	}
	e := newEnv(t, fakeoauth.Options{})
	s := e.interceptedStore(func(w http.ResponseWriter, r *http.Request) bool {
		if grantOf(r) != "refresh_token" {
			return false
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "<html>blocked by policy</html>")
		return true
	})
	e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
	if _, err := s.Refresh(t.Context(), "g-windows"); !errors.Is(err, storage.ErrTransient) || errors.Is(err, storage.ErrAuthExpired) {
		t.Errorf("a refresh answered by a proxy's HTML 403 = %v; want ErrTransient", err)
	}
}

// TestDeviceStatus_AnswerThatEndsNothing (security review STORE-3, STORE-4):
// a device poll answered by a proxy's HTML 403, a 200 that is not JSON or
// a 200 without an access token ends no login: Status answers pending, and
// once the provider's own answer comes through the login completes.
func TestDeviceStatus_AnswerThatEndsNothing(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"an HTML 403", "text/html", "<html>blocked</html>", http.StatusForbidden},
		{"a 200 that is not JSON", "text/html", "<html>a portal</html>", http.StatusOK},
		{"a 200 without an access token", "application/json", `{"token_type":"Bearer"}`, http.StatusOK},
		{"a 400 with a transient code", "application/json", `{"error":"temporarily_unavailable"}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, fakeoauth.Options{})
			var once sync.Once
			s := e.interceptedStore(func(w http.ResponseWriter, r *http.Request) bool {
				if grantOf(r) != grantDeviceCode {
					return false
				}
				handled := false
				once.Do(func() {
					handled = true
					w.Header().Set("Content-Type", tc.contentType)
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
				})
				return handled
			})
			begin := e.beginDevice(s, "g-web")
			if st, _, err := s.Status(t.Context(), begin.State); st != StatusPending {
				t.Errorf("Status after %s = %s, %v; want pending", tc.name, st, err)
			}
			e.clk.Add(time.Minute)
			if st, c, err := s.Status(t.Context(), begin.State); st != StatusCompleted || err != nil || c.ClientType != ClientTV {
				t.Errorf("Status after the provider's own answer = %s, %+v, %v; want completed", st, c, err)
			}
		})
	}
}

// TestRefresh_ReReadUsesTheNewLoginsClient (security review STORE-5): a
// refresh that re-reads a row which a new login of another client type
// wrote meanwhile refreshes it with that login's client.
func TestRefresh_ReReadUsesTheNewLoginsClient(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
	other := e.store(func(c *Config) { c.Secrets = e.openCatalog() })
	s.beforeCall = func() {
		s.beforeCall = nil
		e.login(other, "g-windows", storage.KindGoogleDrive, PlatformWindows, PlanB1) // the TV client
	}
	if _, err := s.refresh(t.Context(), "g-windows", true); err != nil {
		t.Errorf("a daily refresh across a re-login with the TV client = %v; want it refreshed", err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	last := e.tokenForms[len(e.tokenForms)-1]
	if last.Get("grant_type") != "refresh_token" || last.Get("client_id") != idTV {
		t.Errorf("the last token request: grant %s, client %s; want a refresh by the TV client", last.Get("grant_type"), last.Get("client_id"))
	}
}

// TestCompletion_CallerGoneDuringTheRedemption (security review CTL-4):
// the code or device code that the provider redeemed is used up, so a
// caller (a control request) that goes away before the provider's answer
// arrives does not lose the login: the hub reads the answer and stores it.
func TestCompletion_CallerGoneDuringTheRedemption(t *testing.T) {
	for _, plan := range []string{PlanA, PlanB1} {
		t.Run(plan, func(t *testing.T) {
			e := newEnv(t, fakeoauth.Options{})
			ctx, cancel := context.WithCancel(t.Context())
			s := e.interceptedStore(func(w http.ResponseWriter, r *http.Request) bool {
				if g := grantOf(r); g != "authorization_code" && g != grantDeviceCode {
					return false
				}
				rec := e.recorded(r)
				cancel()
				select {
				case <-r.Context().Done():
				case <-time.After(300 * time.Millisecond):
				}
				send(w, rec)
				return true
			})
			begin, err := s.Begin(t.Context(), BeginRequest{DestID: "g-windows", Provider: storage.KindGoogleDrive, Platform: PlatformWindows,
				LoopbackPort: 53123, Plan: plan})
			if err != nil {
				t.Fatal(err)
			}
			if plan == PlanB1 {
				if err := e.fake.ApproveDevice(begin.UserCode, "acct-1"); err != nil {
					t.Fatal(err)
				}
				_, _ = s.DeviceStatus(ctx, begin.State)
			} else {
				redirect, err := e.fake.Authorize(begin.AuthURL)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = s.Complete(ctx, begin.State, codeOf(t, redirect, begin.State))
			}
			if st, c, err := s.Status(t.Context(), begin.State); st != StatusCompleted || err != nil {
				t.Errorf("Status = %s, %+v, %v; want completed", st, c, err)
			}
			if _, ok, err := s.Login(t.Context(), "g-windows"); !ok || err != nil {
				t.Errorf("the login's row: ok %v, %v; want stored", ok, err)
			}
		})
	}
}

// heldGrant is an interceptor that lets the fake decide the first token
// request of grant at once (a code or device code redeemed, its tokens
// issued), tells entered, and sends the answer once release closes.
type heldGrant struct {
	e       *env
	grant   string
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func newHeldGrant(e *env, grant string) *heldGrant {
	return &heldGrant{e: e, grant: grant, entered: make(chan struct{}), release: make(chan struct{})}
}

func (h *heldGrant) answer(w http.ResponseWriter, r *http.Request) bool {
	if grantOf(r) != h.grant {
		return false
	}
	held := false
	h.once.Do(func() { held = true })
	if !held {
		return false
	}
	rec := h.e.recorded(r)
	close(h.entered)
	<-h.release
	send(w, rec)
	return true
}

// TestStatus_NoExpiryWhileARedemptionIsInFlight (security review CTL-2):
// a state whose code or device code the hub is redeeming is pending, also
// past its life: an expired answer meanwhile would take the login's one
// end report from the completion that follows.
func TestStatus_NoExpiryWhileARedemptionIsInFlight(t *testing.T) {
	for _, plan := range []string{PlanA, PlanB1} {
		t.Run(plan, func(t *testing.T) {
			e := newEnv(t, fakeoauth.Options{})
			grant := "authorization_code"
			if plan == PlanB1 {
				grant = grantDeviceCode
			}
			held := newHeldGrant(e, grant)
			s := e.interceptedStore(held.answer)
			ctx := t.Context()
			begin, err := s.Begin(ctx, BeginRequest{DestID: "g-windows", Provider: storage.KindGoogleDrive, Platform: PlatformWindows,
				LoopbackPort: 53123, Plan: plan})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			if plan == PlanB1 {
				if err := e.fake.ApproveDevice(begin.UserCode, "acct-1"); err != nil {
					t.Fatal(err)
				}
				go func() { _, err := s.DeviceStatus(ctx, begin.State); done <- err }()
			} else {
				redirect, err := e.fake.Authorize(begin.AuthURL)
				if err != nil {
					t.Fatal(err)
				}
				code := codeOf(t, redirect, begin.State)
				go func() { _, err := s.Complete(ctx, begin.State, code); done <- err }()
			}
			select {
			case <-held.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("the redemption did not reach the provider")
			}
			e.clk.Add(begin.ExpiresAt.Sub(e.clk.Now()) + time.Second)
			if st, _, err := s.Status(ctx, begin.State); st != StatusPending {
				t.Errorf("Status past the state's life while its redemption is in flight = %s, %v; want pending", st, err)
			}
			close(held.release)
			if err := <-done; err != nil {
				t.Fatalf("the redemption: %v", err)
			}
			if st, _, err := s.Status(ctx, begin.State); st != StatusCompleted || err != nil {
				t.Errorf("Status once the redemption returned = %s, %v; want completed", st, err)
			}
		})
	}
}

// TestStatus_PlanAFailedComplete (Task 9A review M2): a plan A login
// whose Complete failed is failed, with its reason, not pending until it
// expires.
func TestStatus_PlanAFailedComplete(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	ctx := t.Context()
	begin, err := s.Begin(ctx, BeginRequest{DestID: "g-android", Provider: storage.KindGoogleDrive, Platform: PlatformAndroid})
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := e.fake.Authorize(begin.AuthURL)
	if err != nil {
		t.Fatal(err)
	}
	e.fake.FailNext(fakeoauth.PathGoogleToken, http.StatusBadRequest, "access_denied")
	if _, err := s.Complete(ctx, begin.State, codeOf(t, redirect, begin.State)); err == nil {
		t.Fatal("Complete succeeded, want the provider's refusal")
	}
	st, c, err := s.Status(ctx, begin.State)
	if st != StatusFailed || Reason(err) != "access_denied" || c.DestID != "g-android" {
		t.Errorf("Status = %s, %+v, %v; want failed, access_denied, naming g-android", st, c, err)
	}
}

// TestKnownSecrets_KeepsSupersededRefreshTokens (security review DIAG-3):
// the scan's values hold every refresh token that a login's row held this
// run, not only its current one: a rotation and a new login of the same
// ref keep the tokens they replace (Microsoft does not revoke a rotated
// one, Google not the one a new consent replaces), at most 64 of them.
func TestKnownSecrets_KeepsSupersededRefreshTokens(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	ctx := t.Context()
	if err := s.SetClients(ctx, hubClients()); err != nil {
		t.Fatal(err)
	}
	e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
	first := e.fake.RefreshTokens()
	if _, err := s.Refresh(ctx, "g-windows"); err != nil {
		t.Fatal(err)
	}
	e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, PlanB1)
	values, err := KnownSecrets(ctx, e.cat, e.sealer)
	if err != nil {
		t.Fatal(err)
	}
	for _, rt := range e.fake.RefreshTokens() {
		if !slices.Contains(values, rt) {
			t.Errorf("a refresh token of the login (first: %v) is not among the scan's values", slices.Contains(first, rt))
		}
	}
	for range 70 {
		if _, err := s.Refresh(ctx, "g-windows"); err != nil {
			t.Fatal(err)
		}
	}
	_, l, _, err := s.readLogin(ctx, "g-windows")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(l.PreviousRefreshTokens); n != maxPreviousTokens {
		t.Fatalf("the row keeps %d superseded tokens, want %d", n, maxPreviousTokens)
	}
	all := e.fake.RefreshTokens()
	if values, err = KnownSecrets(ctx, e.cat, e.sealer); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(values, l.RefreshToken) || !slices.Contains(values, l.PreviousRefreshTokens[maxPreviousTokens-1]) {
		t.Errorf("the scan misses the current or the last superseded token (%d issued)", len(all))
	}
}

// TestSecretValues_RefusedWhenNoSecret (security review DIAG-4): a value
// that root's scan would search for is at least 16 bytes of the
// characters that OAuth client IDs, secrets and refresh tokens use;
// SetClients refuses another, and KnownSecrets refuses a row that holds
// one, naming the row and never the value.
func TestSecretValues_RefusedWhenNoSecret(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	s := e.store()
	ctx := t.Context()
	bad := `{"schema":"ipcam.`
	for name, c := range map[string]Clients{
		"a short ID":            {Google: map[ClientType]Client{ClientAndroid: {ID: "short-id"}}},
		"an ID of JSON":         {Google: map[ClientType]Client{ClientAndroid: {ID: bad}}},
		"a secret with a space": {Google: map[ClientType]Client{ClientDesktop: {ID: idDesktop, Secret: "a secret with spaces"}}},
		"a short Entra ID":      {Microsoft: &Client{ID: "entra"}},
	} {
		if err := s.SetClients(ctx, c); err == nil {
			t.Errorf("SetClients with %s succeeded, want it refused", name)
		} else if strings.Contains(err.Error(), bad) || strings.Contains(err.Error(), "spaces") {
			t.Errorf("SetClients with %s: the refusal %q quotes the value", name, err)
		}
	}
	sealed, err := e.sealer.Seal(ctx, "client:google:android", []byte(`{"id":"{\"schema\":\"ipcam."}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.cat.PutSecret(ctx, "client:google:android", secretKind, sealed, e.sealer.Backend()); err != nil {
		t.Fatal(err)
	}
	_, err = KnownSecrets(ctx, e.cat, e.sealer)
	if err == nil || !strings.Contains(err.Error(), "client:google:android") || strings.Contains(err.Error(), "schema") {
		t.Errorf("KnownSecrets with a row of no secret = %v; want a refusal naming the row only", err)
	}
}
