package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	// tokenTimeout bounds each call of a token or device endpoint (P27), so
	// a WAN block ends a refresh as ErrTransient within it.
	tokenTimeout = 5 * time.Second
	// rotationGrace: see Store.rotationGrace.
	rotationGrace = 2 * time.Second
	// expiryMargin is how long before its expiry an access token is
	// refreshed, so no request carries one that expires on its way.
	expiryMargin = time.Minute
	// maxAnswer bounds an endpoint's answer.
	maxAnswer = 1 << 20
	// grantDeviceCode is the device code grant (RFC 8628).
	grantDeviceCode = "urn:ietf:params:oauth:grant-type:device_code"
)

// login is the plaintext of a login's sealed row (P19, P27).
type login struct {
	RefreshToken string       `json:"refreshToken"`
	Provider     storage.Kind `json:"provider"`
	Platform     Platform     `json:"platform"`
	ClientType   ClientType   `json:"clientType"`
	Plan         string       `json:"plan"`
	IssuedAtMs   int64        `json:"issuedAtMs"`
	// DailyRefreshedAtMs is the time of the login's last successful daily
	// refresh (P53), which RunDaily writes with it, so that a day that was
	// ok stays decided across a restart (C2-R16 (d′)); 0: none yet. Only
	// RunDaily's refreshes write it: a day is decided where its outcome went
	// out as an event.
	DailyRefreshedAtMs int64 `json:"dailyRefreshedAtMs,omitempty"`
}

// sealLogin seals l under ref's label.
func (s *Store) sealLogin(ctx context.Context, ref string, l login) ([]byte, error) {
	plain, err := json.Marshal(l) //nolint:gosec // G117: the plaintext that Seal seals, cleared after it
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	sealed, err := s.cfg.Sealer.Seal(ctx, loginPrefix+ref, plain)
	if err != nil {
		return nil, fmt.Errorf("oauth: seal the login %s: %w", ref, err)
	}
	return sealed, nil
}

// putLogin writes l as ref's row, replacing an earlier login of ref.
func (s *Store) putLogin(ctx context.Context, ref string, l login) error {
	sealed, err := s.sealLogin(ctx, ref, l)
	if err != nil {
		return err
	}
	return s.cfg.Secrets.PutSecret(ctx, loginPrefix+ref, secretKind, sealed, s.cfg.Sealer.Backend())
}

// readLogin returns ref's sealed row and its login, and whether there is
// one. A catalog that fails to read is ErrTransient; a row that this hub
// cannot open, ErrAuthExpired: only a new login mends it.
func (s *Store) readLogin(ctx context.Context, ref string) ([]byte, login, bool, error) {
	id := loginPrefix + ref
	sealed, backend, ok, err := s.cfg.Secrets.Secret(ctx, id)
	switch {
	case err != nil:
		return nil, login{}, false, fmt.Errorf("oauth: read the login %s: %w", ref, storage.ErrTransient)
	case !ok:
		return nil, login{}, false, nil
	case backend != s.cfg.Sealer.Backend():
		return nil, login{}, false, fmt.Errorf("oauth: the login %s was sealed by %s, not this hub's %s: %w",
			ref, backend, s.cfg.Sealer.Backend(), storage.ErrAuthExpired)
	}
	plain, err := s.cfg.Sealer.Unseal(ctx, id, sealed)
	if err != nil {
		return nil, login{}, false, fmt.Errorf("oauth: unseal the login %s: %w: %w", ref, err, storage.ErrAuthExpired)
	}
	defer clear(plain)
	var l login
	if err := json.Unmarshal(plain, &l); err != nil || l.RefreshToken == "" {
		return nil, login{}, false, fmt.Errorf("oauth: the row of the login %s holds no login: %w", ref, storage.ErrAuthExpired)
	}
	return sealed, l, true, nil
}

// tokenAnswer is a token endpoint's answer.
type tokenAnswer struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// deviceAnswer is a device endpoint's answer: Google names its URL
// verification_url, Microsoft verification_uri.
type deviceAnswer struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
}

func (d deviceAnswer) verification() string {
	if d.VerificationURL != "" {
		return d.VerificationURL
	}
	return d.VerificationURI
}

func (d deviceAnswer) life() time.Duration {
	if d.ExpiresIn <= 0 {
		return stateLife
	}
	return time.Duration(d.ExpiresIn) * time.Second
}

func (d deviceAnswer) interval() time.Duration {
	if d.Interval <= 0 {
		return 5 * time.Second
	}
	return time.Duration(d.Interval) * time.Second
}

// tokenCall sends form, with client's credentials, to provider p's token
// endpoint within tokenTimeout.
func (s *Store) tokenCall(ctx context.Context, p storage.Kind, client Client, form url.Values) (tokenAnswer, error) {
	endpoint := s.cfg.Endpoints.GoogleToken
	if p == storage.KindOneDrivePersonal {
		endpoint = s.cfg.Endpoints.MicrosoftToken
	}
	form.Set("client_id", client.ID)
	if client.Secret != "" {
		form.Set("client_secret", client.Secret)
	}
	var tok tokenAnswer
	if err := s.post(ctx, endpoint, form, &tok); err != nil {
		return tokenAnswer{}, err
	}
	if tok.AccessToken == "" {
		return tokenAnswer{}, errors.New("oauth: the token endpoint gave no access token")
	}
	return tok, nil
}

// deviceAuthorization asks provider p's device endpoint for a user code
// (RFC 8628), as client.
func (s *Store) deviceAuthorization(ctx context.Context, p storage.Kind, client Client) (deviceAnswer, error) {
	endpoint, scope := s.cfg.Endpoints.GoogleDevice, scopeGoogle
	if p == storage.KindOneDrivePersonal {
		endpoint, scope = s.cfg.Endpoints.MicrosoftDevice, scopeMicrosoft
	}
	var dev deviceAnswer
	if err := s.post(ctx, endpoint, url.Values{"client_id": {client.ID}, "scope": {scope}}, &dev); err != nil {
		return deviceAnswer{}, err
	}
	if dev.DeviceCode == "" || dev.UserCode == "" {
		return deviceAnswer{}, errors.New("oauth: the device endpoint gave no device code")
	}
	return dev, nil
}

// post sends form to endpoint within tokenTimeout and decodes a 200's JSON
// into v. A request with no answer is ErrTransient, and a refusal is a
// ProviderError; neither text quotes the request, its URL or the answer.
func (s *Store) post(ctx context.Context, endpoint string, form url.Values, v any) error {
	ctx, cancel := context.WithTimeout(ctx, s.tokenTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("oauth: a request to an endpoint that is not a URL")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.cfg.HTTP.Do(req)
	if err != nil {
		what := "failed"
		if ctx.Err() != nil {
			what = "timed out"
		}
		return fmt.Errorf("oauth: the provider's endpoint %s: %w", what, storage.ErrTransient)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return fmt.Errorf("oauth: the provider's answer was cut: %w", storage.ErrTransient)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		code := e.Error
		if !providerCodes[code] {
			code = "error"
		}
		return &ProviderError{Code: code, Status: resp.StatusCode}
	}
	if err := json.Unmarshal(body, v); err != nil {
		return errors.New("oauth: the provider's answer is not its JSON")
	}
	return nil
}

// isInvalidGrant reports a provider's invalid_grant.
func isInvalidGrant(err error) bool {
	var pe *ProviderError
	return errors.As(err, &pe) && pe.Code == "invalid_grant"
}

// Refresh refreshes the login of ref now, whatever its access token's
// expiry: the daily refresh (P53) and the control endpoint's.
func (s *Store) Refresh(ctx context.Context, ref string) (RefreshResult, error) {
	return s.refresh(ctx, ref, false)
}

// refresh refreshes ref now; daily records the success in ref's row as its
// last daily refresh (RunDaily's).
func (s *Store) refresh(ctx context.Context, ref string, daily bool) (RefreshResult, error) {
	src := s.source(ref)
	src.mu.Lock()
	defer src.mu.Unlock()
	return s.refreshLocked(ctx, ref, src, daily)
}

// refreshLocked is the refresh of ref (P27), with src.mu held:
//  1. read the sealed row and unseal its refresh token;
//  2. call the token endpoint, in no transaction, within tokenTimeout;
//  3. when the provider rotated the token, or the refresh is daily (its
//     time is recorded), seal the row anew and write it with SwapSecret
//     before the access token is used.
//
// An invalid_grant, or a swap that another writer won, re-reads the row
// once: a token that another process rotated meanwhile is refreshed again,
// so no process keeps a revoked one. An invalid_grant on a row that stays
// as it was, also rotationGrace later, is ErrAuthExpired. A row written
// anew keeps every field it does not change, so a rotation keeps the last
// daily refresh's time, and a daily refresh keeps the token.
func (s *Store) refreshLocked(ctx context.Context, ref string, src *tokenSource, daily bool) (RefreshResult, error) {
	row, l, ok, err := s.readLogin(ctx, ref)
	switch {
	case err != nil:
		return RefreshResult{}, err
	case !ok:
		return RefreshResult{}, fmt.Errorf("oauth: no login for %s: %w", ref, storage.ErrAuthExpired)
	}
	client, err := s.client(ctx, l.Provider, l.ClientType)
	if err != nil {
		return RefreshResult{}, err
	}
	for again := false; ; again = true {
		if s.beforeCall != nil {
			s.beforeCall()
		}
		tok, err := s.tokenCall(ctx, l.Provider, client, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {l.RefreshToken}})
		if isInvalidGrant(err) && !again {
			if row, l, ok, err = s.rereadChanged(ctx, ref, row); err != nil || !ok {
				return RefreshResult{}, invalidGrant(ref, err)
			}
			continue
		}
		if err != nil {
			return RefreshResult{}, err
		}
		now := s.cfg.Clock()
		rotated := tok.RefreshToken != "" && tok.RefreshToken != l.RefreshToken
		if rotated || daily {
			next := l
			if rotated {
				next.RefreshToken = tok.RefreshToken
			}
			if daily {
				next.DailyRefreshedAtMs = now.UnixMilli()
			}
			sealed, err := s.sealLogin(ctx, ref, next)
			if err != nil {
				return RefreshResult{}, err
			}
			swapped, err := s.cfg.Secrets.SwapSecret(ctx, loginPrefix+ref, row, sealed, s.cfg.Sealer.Backend())
			switch {
			case err != nil:
				return RefreshResult{}, fmt.Errorf("oauth: store the refreshed login %s: %w", ref, storage.ErrTransient)
			case !swapped && !again:
				// Another writer changed the row: refresh with its token.
				if row, l, ok, err = s.readLogin(ctx, ref); err != nil {
					return RefreshResult{}, err
				} else if !ok {
					return RefreshResult{}, fmt.Errorf("oauth: the login %s is gone: %w", ref, storage.ErrAuthExpired)
				}
				continue
			case !swapped:
				return RefreshResult{}, fmt.Errorf("oauth: the login %s changed twice during a refresh: %w", ref, storage.ErrTransient)
			}
		}
		src.set(tok, now)
		return RefreshResult{At: now, Rotated: rotated}, nil
	}
}

// rereadChanged re-reads ref's row after an invalid_grant: a row that
// another process rotated meanwhile is returned with ok. A row that is
// still old is read again rotationGrace later, the time another process's
// refresh takes from its answer to its write.
func (s *Store) rereadChanged(ctx context.Context, ref string, old []byte) ([]byte, login, bool, error) {
	for wait := time.Duration(0); ; wait = s.rotationGrace {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil, login{}, false, ctx.Err()
			case <-time.After(wait):
			}
		}
		row, l, ok, err := s.readLogin(ctx, ref)
		if err != nil || !ok {
			return nil, login{}, false, err
		}
		if !bytes.Equal(row, old) {
			return row, l, true, nil
		}
		if wait > 0 {
			return nil, login{}, false, nil
		}
	}
}

// invalidGrant is the error of a refresh token that the provider refused
// with invalid_grant, its row unchanged (ErrAuthExpired), or of the re-read
// that err is.
func invalidGrant(ref string, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("oauth: the login %s: %w", ref, &ProviderError{Code: "invalid_grant", Status: http.StatusBadRequest})
}

// tokenSource is the access token of one ref, in RAM, shared by every
// destination that names the ref.
type tokenSource struct {
	s   *Store
	ctx context.Context // its refreshes', TokenSource's first caller's; nil: Background
	ref string

	mu     sync.Mutex // one refresh at a time; held across the call
	access string
	expiry time.Time
}

// TokenSource returns ref's token source, the same for every call: its
// Token refreshes the access token through the sealed row when it expires
// within a minute, within the first caller's ctx (ipcam-hub's run).
func (s *Store) TokenSource(ctx context.Context, ref string) oauth2.TokenSource {
	src := s.source(ref)
	s.mu.Lock()
	defer s.mu.Unlock()
	if src.ctx == nil {
		src.ctx = ctx
	}
	return src
}

// source returns ref's tokenSource, made at its first call.
func (s *Store) source(ref string) *tokenSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := s.sources[ref]
	if src == nil {
		src = &tokenSource{s: s, ref: ref}
		s.sources[ref] = src
	}
	return src
}

// Token returns the access token, refreshed first when it expires within
// expiryMargin. Its errors wrap storage.ErrAuthExpired or ErrTransient.
func (t *tokenSource) Token() (*oauth2.Token, error) {
	t.s.mu.Lock()
	ctx := t.ctx
	t.s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.access == "" || !t.s.cfg.Clock().Before(t.expiry.Add(-expiryMargin)) {
		if _, err := t.s.refreshLocked(ctx, t.ref, t, false); err != nil {
			return nil, err
		}
	}
	return &oauth2.Token{AccessToken: t.access, TokenType: "Bearer", Expiry: t.expiry}, nil
}

// set keeps tok's access token, which expires ExpiresIn after now. The
// caller holds t.mu, or t is new to its login.
func (t *tokenSource) set(tok tokenAnswer, now time.Time) {
	life := time.Duration(tok.ExpiresIn) * time.Second
	if life <= 0 {
		life = time.Hour
	}
	t.access, t.expiry = tok.AccessToken, now.Add(life)
}
