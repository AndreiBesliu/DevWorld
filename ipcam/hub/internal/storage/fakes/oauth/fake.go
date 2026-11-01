// Package fakeoauth is a strict fake of Google's and Microsoft's OAuth 2.0
// endpoints (P27, Task 9), for the hub's tests and the lab: the
// authorization endpoint, the token endpoint (authorization code with S256
// PKCE, refresh token, device code), the device authorization endpoint and
// Google's revocation endpoint.
//
// It is strict where the providers are, and stricter where the hub's
// mistakes would otherwise pass unseen:
//   - every client is registered with its provider, type, ID, secret and
//     redirect URIs; a Desktop, TV or Web client without its secret, a
//     public client (Android, Entra) with one, or another type's flow is
//     invalid_client or unauthorized_client;
//   - an authorization request needs response_type=code, the provider's
//     scope and an S256 code_challenge, and a redirect URI that the client
//     takes: a registered one, Desktop's http://127.0.0.1:<any port>/…, or
//     Entra's http://localhost:<any port>/… for a registered localhost URI;
//   - a code is single-use, lives 10 min, and redeems only with its
//     client, the redirect_uri of its request byte for byte and the
//     verifier of its challenge (invalid_grant otherwise);
//   - every refresh rotates the refresh token and revokes the old one, so a
//     caller that keeps an old token gets invalid_grant;
//   - in Testing mode (Options.TestingExpiry) a Google refresh token dies
//     that long after the user's consent, its rotations included.
//
// AutoApprove stands for the user on the authorization page and the device
// page, as the lab's login rig does; otherwise a test consents with
// Authorize or ApproveDevice. Lookup tells fakegdrive who an access token
// acts for. It is permanent test infrastructure, as the drivers' fakes are
// (P04), and no release ships it.
package fakeoauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The endpoints' paths under the fake's handler.
const (
	PathGoogleAuth      = "/google/auth"
	PathGoogleToken     = "/google/token"
	PathGoogleDevice    = "/google/device/code"
	PathGoogleRevoke    = "/google/revoke"
	PathMicrosoftAuth   = "/microsoft/authorize"
	PathMicrosoftToken  = "/microsoft/token"
	PathMicrosoftDevice = "/microsoft/devicecode"
	// PathClients registers a client (a Client as JSON) over HTTP, for the
	// lab's fakeproviders process.
	PathClients = "/fake/clients"
)

// The providers and the client types.
const (
	Google    = "google"
	Microsoft = "microsoft"

	TypeAndroid = "android"
	TypeDesktop = "desktop"
	TypeTV      = "tv"
	TypeWeb     = "web"
	TypeEntra   = "entra"
)

// The scopes that the fake requires: Google's drive.file (§9.2), and
// Graph's app folder with offline access (P36).
const (
	ScopeDriveFile      = "https://www.googleapis.com/auth/drive.file"
	ScopeFilesAppFolder = "Files.ReadWrite.AppFolder"
	ScopeOffline        = "offline_access"
)

// GrantDeviceCode is the device code grant's type (RFC 8628).
const GrantDeviceCode = "urn:ietf:params:oauth:grant-type:device_code"

const (
	codeLife       = 10 * time.Minute
	defaultAccess  = time.Hour
	googleDevLife  = 30 * time.Minute
	msDevLife      = 15 * time.Minute
	deviceInterval = 5 * time.Second
	defaultAccount = "acct-1"
)

// URLs are the fake's endpoints under a base URL. Their fields are
// oauth.Endpoints', in its order, so the hub's tests convert one into the
// other.
type URLs struct {
	GoogleAuth, GoogleToken, GoogleDevice, GoogleRevoke, MicrosoftAuth, MicrosoftToken, MicrosoftDevice string
}

// URLsAt returns the endpoints of a fake served at base.
func URLsAt(base string) URLs {
	base = strings.TrimSuffix(base, "/")
	return URLs{
		GoogleAuth: base + PathGoogleAuth, GoogleToken: base + PathGoogleToken, GoogleDevice: base + PathGoogleDevice,
		GoogleRevoke: base + PathGoogleRevoke, MicrosoftAuth: base + PathMicrosoftAuth, MicrosoftToken: base + PathMicrosoftToken,
		MicrosoftDevice: base + PathMicrosoftDevice,
	}
}

// Client is a registered OAuth client. Project is its Google Cloud project:
// fakegdrive lets the clients of one project see each other's drive.file
// files. RedirectURIs are the exact URIs that it takes, besides Desktop's
// and Entra's loopback rules.
type Client struct {
	Provider     string   `json:"provider"`
	Type         string   `json:"type"`
	ID           string   `json:"id"`
	Secret       string   `json:"secret,omitempty"`
	Project      string   `json:"project,omitempty"`
	RedirectURIs []string `json:"redirectUris,omitempty"`
}

// Options configure a Fake.
type Options struct {
	Clock         func() time.Time // time.Now by default
	AutoApprove   bool             // the user consents to every request at once
	Account       string           // the account that consents; "acct-1" by default
	TestingExpiry time.Duration    // a Google refresh token's life from consent; 0: none
	AccessLife    time.Duration    // an access token's life; 1 h by default
	// Mint returns a new value of kind ("code", "access", "refresh",
	// "device" or "user"); random by default. Tests mint known values.
	Mint func(kind string) string
}

// Token is what Lookup knows of an access token.
type Token struct {
	ClientID, Project, Account string
	Expired                    bool
}

// Fake is the fake provider. Its methods are safe for concurrent use.
type Fake struct {
	opts Options

	mu      sync.Mutex
	clients map[string]Client       // by ID
	codes   map[string]*codeGrant   // by code
	refresh map[string]*grant       // by refresh token, live or revoked
	access  map[string]*accessToken // by access token
	devices map[string]*deviceGrant // by device code
	users   map[string]*deviceGrant // by user code
	fails   map[string][]failure    // by path
	counts  map[string]int          // by grant type, and "device", "revoke"
	seq     int
}

type codeGrant struct {
	client                        Client
	redirectURI, challenge, scope string
	offline                       bool
	account                       string
	expires                       time.Time
	used                          bool
}

// grant is a refresh token: its client, account and scope, and the time of
// the consent that its chain of rotations started from.
type grant struct {
	client    Client
	account   string
	scope     string
	consentAt time.Time
	revoked   bool
}

type accessToken struct {
	client  Client
	account string
	expires time.Time
}

type deviceGrant struct {
	client         Client
	scope          string
	userCode       string
	expires        time.Time
	interval       time.Duration
	lastPoll       time.Time
	approved, done bool
	denied         bool
	account        string
}

type failure struct {
	status int
	code   string
}

// New returns a fake with no client.
func New(opts Options) *Fake {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.Account == "" {
		opts.Account = defaultAccount
	}
	if opts.AccessLife <= 0 {
		opts.AccessLife = defaultAccess
	}
	return &Fake{
		opts: opts, clients: map[string]Client{}, codes: map[string]*codeGrant{}, refresh: map[string]*grant{},
		access: map[string]*accessToken{}, devices: map[string]*deviceGrant{}, users: map[string]*deviceGrant{},
		fails: map[string][]failure{}, counts: map[string]int{},
	}
}

// AddClient registers c, replacing a client of the same ID.
func (f *Fake) AddClient(c Client) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients[c.ID] = c
}

// FailNext makes the next request to path (one of the Path constants)
// answer status with the OAuth error code. Several calls queue in order.
func (f *Fake) FailNext(path string, status int, code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails[path] = append(f.fails[path], failure{status: status, code: code})
}

// Counts returns how many token requests of each grant type, device
// authorizations ("device") and revocations ("revoke") arrived.
func (f *Fake) Counts() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]int, len(f.counts))
	for k, v := range f.counts {
		out[k] = v
	}
	return out
}

// Lookup returns what the fake knows of the access token, and whether it
// issued it.
func (f *Fake) Lookup(token string) (Token, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.access[token]
	if a == nil {
		return Token{}, false
	}
	return Token{ClientID: a.client.ID, Project: a.client.Project, Account: a.account, Expired: !f.opts.Clock().Before(a.expires)}, true
}

// RefreshTokens returns the refresh tokens that the fake issued, live and
// revoked; IssuedAccessTokens the access tokens. Tests look for them where
// they must not be.
func (f *Fake) RefreshTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.refresh))
	for t := range f.refresh {
		out = append(out, t)
	}
	return out
}

// IssuedAccessTokens returns every access token that the fake issued.
func (f *Fake) IssuedAccessTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.access))
	for t := range f.access {
		out = append(out, t)
	}
	return out
}

// Authorize is the user at the authorization page of authURL, consenting
// as the fake's account: it returns the URI that the provider redirects
// to, with code and state, or the error that the page shows.
func (f *Fake) Authorize(authURL string) (string, error) {
	return f.AuthorizeAs(authURL, f.opts.Account)
}

// AuthorizeAs is Authorize with the account that consents.
func (f *Fake) AuthorizeAs(authURL, account string) (string, error) {
	u, err := url.Parse(authURL)
	if err != nil {
		return "", errors.New("fakeoauth: not a URL")
	}
	provider := Google
	if u.Path == PathMicrosoftAuth || strings.HasSuffix(u.Path, PathMicrosoftAuth) {
		provider = Microsoft
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authorize(provider, u.Query(), account)
}

// ApproveDevice is the user entering userCode on the device page, as
// account.
func (f *Fake) ApproveDevice(userCode, account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.users[userCode]
	if d == nil || !f.opts.Clock().Before(d.expires) {
		return errors.New("fakeoauth: no such user code")
	}
	d.approved, d.account = true, account
	return nil
}

// DenyDevice is the user refusing the device request of userCode.
func (f *Fake) DenyDevice(userCode string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.users[userCode]
	if d == nil {
		return errors.New("fakeoauth: no such user code")
	}
	d.denied = true
	return nil
}

// Handler serves the endpoints at the Path constants.
func (f *Fake) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(PathGoogleAuth, func(w http.ResponseWriter, r *http.Request) { f.serveAuth(w, r, Google) })
	mux.HandleFunc(PathMicrosoftAuth, func(w http.ResponseWriter, r *http.Request) { f.serveAuth(w, r, Microsoft) })
	mux.HandleFunc(PathGoogleToken, func(w http.ResponseWriter, r *http.Request) { f.serveToken(w, r, Google, PathGoogleToken) })
	mux.HandleFunc(PathMicrosoftToken, func(w http.ResponseWriter, r *http.Request) { f.serveToken(w, r, Microsoft, PathMicrosoftToken) })
	mux.HandleFunc(PathGoogleDevice, func(w http.ResponseWriter, r *http.Request) { f.serveDevice(w, r, Google, PathGoogleDevice) })
	mux.HandleFunc(PathMicrosoftDevice, func(w http.ResponseWriter, r *http.Request) { f.serveDevice(w, r, Microsoft, PathMicrosoftDevice) })
	mux.HandleFunc(PathGoogleRevoke, f.serveRevoke)
	mux.HandleFunc(PathClients, f.serveClients)
	return mux
}

// serveAuth is the authorization page: with AutoApprove it redirects with a
// code at once; otherwise it answers 403, and a test consents with
// Authorize. A request that the page refuses answers 400 with the error.
func (f *Fake) serveAuth(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "invalid_request")
		return
	}
	if !f.opts.AutoApprove {
		writeError(w, http.StatusForbidden, "interaction_required")
		return
	}
	f.mu.Lock()
	redirect, err := f.authorize(provider, r.URL.Query(), f.opts.Account)
	f.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusBadRequest, errCode(err))
		return
	}
	http.Redirect(w, r, redirect, http.StatusFound) //nolint:gosec // G710: a redirect URI that authorize checked against the client's registered ones
}

// authorize checks an authorization request and issues its code. f.mu is
// held.
func (f *Fake) authorize(provider string, q url.Values, account string) (string, error) {
	c, ok := f.clients[q.Get("client_id")]
	switch {
	case !ok || c.Provider != provider:
		return "", oauthError("invalid_client")
	case c.Type == TypeTV:
		return "", oauthError("unauthorized_client") // a TV client has the device flow only
	case q.Get("response_type") != "code":
		return "", oauthError("unsupported_response_type")
	case !redirectAllowed(c, q.Get("redirect_uri")):
		return "", oauthError("redirect_uri_mismatch")
	case q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256":
		return "", oauthError("invalid_request")
	case !scopeAllowed(provider, q.Get("scope")):
		return "", oauthError("invalid_scope")
	case provider == Microsoft && q.Get("response_mode") != "" && q.Get("response_mode") != "query":
		return "", oauthError("invalid_request")
	}
	code := f.mint("code")
	offline := q.Get("access_type") == "offline"
	if provider == Microsoft {
		offline = hasScope(q.Get("scope"), ScopeOffline)
	}
	f.codes[code] = &codeGrant{client: c, redirectURI: q.Get("redirect_uri"), challenge: q.Get("code_challenge"),
		scope: q.Get("scope"), offline: offline, account: account, expires: f.opts.Clock().Add(codeLife)}
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		return "", oauthError("redirect_uri_mismatch")
	}
	rq := redirect.Query()
	rq.Set("code", code)
	if s := q.Get("state"); s != "" {
		rq.Set("state", s)
	}
	redirect.RawQuery = rq.Encode()
	return redirect.String(), nil
}

// serveToken is the token endpoint.
func (f *Fake) serveToken(w http.ResponseWriter, r *http.Request, provider, path string) {
	if r.Method != http.MethodPost || r.ParseForm() != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[r.PostForm.Get("grant_type")]++
	if fail, ok := f.nextFailure(path); ok {
		writeError(w, fail.status, fail.code)
		return
	}
	c, status, code := f.authenticate(provider, r.PostForm)
	if status != 0 {
		writeError(w, status, code)
		return
	}
	var answer map[string]any
	var err error
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		answer, err = f.redeemCode(c, r.PostForm)
	case "refresh_token":
		answer, err = f.refreshGrant(c, r.PostForm)
	case GrantDeviceCode:
		answer, err = f.pollDevice(c, r.PostForm)
	default:
		err = oauthError("unsupported_grant_type")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, errCode(err))
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

// authenticate returns the client of a token or device request, or the
// status and error that refuse it: an unknown client, a confidential one
// (Desktop, TV, Web) without its secret, a public one (Android, Entra)
// with a secret.
func (f *Fake) authenticate(provider string, form url.Values) (Client, int, string) {
	c, ok := f.clients[form.Get("client_id")]
	if !ok || c.Provider != provider {
		return Client{}, http.StatusUnauthorized, "invalid_client"
	}
	secret := form.Get("client_secret")
	switch c.Type {
	case TypeDesktop, TypeTV, TypeWeb:
		if secret == "" || secret != c.Secret {
			return Client{}, http.StatusUnauthorized, "invalid_client"
		}
	default:
		if secret != "" {
			return Client{}, http.StatusUnauthorized, "invalid_client"
		}
	}
	return c, 0, ""
}

// redeemCode redeems an authorization code. f.mu is held.
func (f *Fake) redeemCode(c Client, form url.Values) (map[string]any, error) {
	g := f.codes[form.Get("code")]
	switch {
	case g == nil || g.used || !f.opts.Clock().Before(g.expires) || g.client.ID != c.ID:
		return nil, oauthError("invalid_grant")
	case form.Get("redirect_uri") != g.redirectURI:
		return nil, oauthError("invalid_grant")
	case s256(form.Get("code_verifier")) != g.challenge:
		return nil, oauthError("invalid_grant")
	}
	g.used = true
	return f.issue(c, g.account, g.scope, g.offline, f.opts.Clock()), nil
}

// refreshGrant rotates a refresh token: the old one is revoked, and the
// new one keeps the time of the consent. f.mu is held.
func (f *Fake) refreshGrant(c Client, form url.Values) (map[string]any, error) {
	g := f.refresh[form.Get("refresh_token")]
	now := f.opts.Clock()
	switch {
	case g == nil || g.revoked || g.client.ID != c.ID:
		return nil, oauthError("invalid_grant")
	case c.Provider == Google && f.opts.TestingExpiry > 0 && !now.Before(g.consentAt.Add(f.opts.TestingExpiry)):
		g.revoked = true
		return nil, oauthError("invalid_grant")
	}
	g.revoked = true
	return f.issue(c, g.account, g.scope, true, g.consentAt), nil
}

// issue makes an access token and, when offline, a refresh token whose
// chain began at consentAt. f.mu is held.
func (f *Fake) issue(c Client, account, scope string, offline bool, consentAt time.Time) map[string]any {
	access := f.mint("access")
	f.access[access] = &accessToken{client: c, account: account, expires: f.opts.Clock().Add(f.opts.AccessLife)}
	answer := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": int(f.opts.AccessLife / time.Second), "scope": scope}
	if offline {
		refresh := f.mint("refresh")
		f.refresh[refresh] = &grant{client: c, account: account, scope: scope, consentAt: consentAt}
		answer["refresh_token"] = refresh
	}
	return answer
}

// serveDevice is the device authorization endpoint: Google's for a TV
// client, Microsoft's for the Entra client.
func (f *Fake) serveDevice(w http.ResponseWriter, r *http.Request, provider, path string) {
	if r.Method != http.MethodPost || r.ParseForm() != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts["device"]++
	if fail, ok := f.nextFailure(path); ok {
		writeError(w, fail.status, fail.code)
		return
	}
	c, ok := f.clients[r.PostForm.Get("client_id")]
	switch {
	case !ok || c.Provider != provider:
		writeError(w, http.StatusUnauthorized, "invalid_client")
		return
	case provider == Google && c.Type != TypeTV:
		writeError(w, http.StatusUnauthorized, "invalid_client") // Google's device flow takes a TV client only
		return
	case !scopeAllowed(provider, r.PostForm.Get("scope")):
		writeError(w, http.StatusBadRequest, "invalid_scope")
		return
	}
	life := googleDevLife
	if provider == Microsoft {
		life = msDevLife
	}
	d := &deviceGrant{client: c, scope: r.PostForm.Get("scope"), userCode: f.mint("user"), expires: f.opts.Clock().Add(life),
		interval: deviceInterval, approved: f.opts.AutoApprove, account: f.opts.Account}
	device := f.mint("device")
	f.devices[device], f.users[d.userCode] = d, d
	verification := "verification_url"
	if provider == Microsoft {
		verification = "verification_uri"
	}
	writeJSON(w, http.StatusOK, map[string]any{"device_code": device, "user_code": d.userCode, verification: "https://fake.invalid/device",
		"expires_in": int(life / time.Second), "interval": int(deviceInterval / time.Second)})
}

// pollDevice answers a device code grant. f.mu is held.
func (f *Fake) pollDevice(c Client, form url.Values) (map[string]any, error) {
	d := f.devices[form.Get("device_code")]
	now := f.opts.Clock()
	switch {
	case d == nil || d.done || d.client.ID != c.ID:
		return nil, oauthError("invalid_grant")
	case !now.Before(d.expires):
		return nil, oauthError("expired_token")
	case d.denied:
		return nil, oauthError("access_denied")
	case !d.lastPoll.IsZero() && now.Sub(d.lastPoll) < d.interval:
		d.lastPoll = now
		return nil, oauthError("slow_down")
	}
	d.lastPoll = now
	if !d.approved {
		return nil, oauthError("authorization_pending")
	}
	d.done = true
	return f.issue(c, d.account, d.scope, true, now), nil
}

// serveRevoke is Google's revocation endpoint: it revokes a refresh token,
// or an access token.
func (f *Fake) serveRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.ParseForm() != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts["revoke"]++
	token := r.PostForm.Get("token")
	if g := f.refresh[token]; g != nil {
		g.revoked = true
	} else if a := f.access[token]; a != nil {
		a.expires = time.Time{}
	} else {
		writeError(w, http.StatusBadRequest, "invalid_token")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// serveClients registers the client in a POST's JSON body.
func (f *Fake) serveClients(w http.ResponseWriter, r *http.Request) {
	var c Client
	if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&c) != nil || c.ID == "" ||
		(c.Provider != Google && c.Provider != Microsoft) {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	f.AddClient(c)
	w.WriteHeader(http.StatusNoContent)
}

// nextFailure takes the failure queued for path, if any. f.mu is held.
func (f *Fake) nextFailure(path string) (failure, bool) {
	queued := f.fails[path]
	if len(queued) == 0 {
		return failure{}, false
	}
	f.fails[path] = queued[1:]
	return queued[0], true
}

// mint returns a new value of kind. f.mu is held.
func (f *Fake) mint(kind string) string {
	if f.opts.Mint != nil {
		return f.opts.Mint(kind)
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		f.seq++
		return fmt.Sprintf("fake-%s-%d", kind, f.seq)
	}
	return "fake-" + kind + "-" + hex.EncodeToString(b)
}

// redirectAllowed reports whether c takes redirect as its redirect URI.
func redirectAllowed(c Client, redirect string) bool {
	for _, r := range c.RedirectURIs {
		if r == redirect {
			return true
		}
	}
	u, err := url.Parse(redirect)
	if err != nil || u.Scheme != "http" || u.Port() == "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	switch c.Type {
	case TypeDesktop: // Google's loopback IP redirect, any port and path
		return u.Hostname() == "127.0.0.1"
	case TypeEntra: // Entra ignores the port of a registered localhost URI
		for _, r := range c.RedirectURIs {
			if ru, err := url.Parse(r); err == nil && ru.Scheme == "http" && ru.Host == "localhost" && u.Hostname() == "localhost" && ru.Path == u.Path {
				return true
			}
		}
	}
	return false
}

// scopeAllowed reports whether scope holds the provider's required scopes.
func scopeAllowed(provider, scope string) bool {
	if provider == Google {
		return hasScope(scope, ScopeDriveFile)
	}
	return hasScope(scope, ScopeFilesAppFolder)
}

func hasScope(scope, want string) bool {
	for _, s := range strings.Fields(scope) {
		if s == want {
			return true
		}
	}
	return false
}

// s256 is PKCE's S256 challenge of verifier (RFC 7636).
func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// oauthError is an OAuth error code.
type oauthError string

func (e oauthError) Error() string { return "fakeoauth: " + string(e) }

func errCode(err error) string {
	var e oauthError
	if errors.As(err, &e) {
		return string(e)
	}
	return "server_error"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers the OAuth error code with status.
func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": "fakeoauth: " + code})
}
