package oauth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	// stateLife is how long a login's state and verifier live (FLW-22).
	stateLife = 10 * time.Minute
	// pendingKept is how long a used or expired state stays known after
	// its life, so that it is told apart from an unknown one.
	pendingKept = time.Hour
	// secretKind is the secrets rows' kind: Task 4's CHECK list.
	secretKind = "oauth"
	// The rows' ids and the sealer's labels.
	loginPrefix  = "oauth:"
	clientPrefix = "client:"
)

// The scopes that the hub asks for (§9.2, P36).
const (
	scopeGoogle    = "https://www.googleapis.com/auth/drive.file"
	scopeMicrosoft = "Files.ReadWrite.AppFolder offline_access"
)

// refPattern is a login's ref, its destId: a row id and a label.
var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Store is the hub's OAuth store. Its methods are safe for concurrent use.
type Store struct {
	cfg Config
	// tokenTimeout bounds each call of a token or device endpoint (P27);
	// rotationGrace is how long a refresh that got invalid_grant on an
	// unchanged row waits for another process's rotation to land before it
	// re-reads the row a last time. Tests shorten both.
	tokenTimeout, rotationGrace time.Duration
	// beforeCall, when a test sets it, runs after a refresh read the row
	// and before it calls the token endpoint.
	beforeCall func()

	mu      sync.Mutex
	clients Clients
	pending map[string]*pending     // by state
	sources map[string]*tokenSource // by ref
	daily   map[string]*dailyState  // by ref
	wake    chan struct{}           // a login completed: RunDaily looks again
}

// pending is a login between Begin and its completion.
type pending struct {
	req     BeginRequest
	route   Route
	client  Client
	expires time.Time

	// Plan A: the PKCE verifier, wiped once used or expired.
	verifier string
	// Plan B1: the device code, the poll interval and the next poll's time.
	device   string
	interval time.Duration
	nextPoll time.Time
	polling  bool

	used       bool
	completion *Completion
	failed     error // a device login that the provider ended
}

// New returns a store on cfg. It touches no file and makes no call.
func New(cfg Config) (*Store, error) {
	switch {
	case cfg.Sealer == nil:
		return nil, errors.New("oauth: Config.Sealer is nil")
	case cfg.Secrets == nil:
		return nil, errors.New("oauth: Config.Secrets is nil")
	case cfg.HTTP == nil:
		return nil, errors.New("oauth: Config.HTTP is nil")
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	if cfg.Endpoints == (Endpoints{}) {
		cfg.Endpoints = DefaultEndpoints()
	}
	return &Store{
		cfg: cfg, tokenTimeout: tokenTimeout, rotationGrace: rotationGrace, clients: cfg.Clients,
		pending: map[string]*pending{}, sources: map[string]*tokenSource{}, daily: map[string]*dailyState{},
		wake: make(chan struct{}, 1),
	}, nil
}

// clientRow is a client's sealed row: client:google:<type> or
// client:microsoft.
func clientRow(p storage.Kind, t ClientType) string {
	if p == storage.KindOneDrivePersonal {
		return clientPrefix + "microsoft"
	}
	return clientPrefix + "google:" + string(t)
}

// SetClients seals c's clients as the rows client:google:<type> and
// client:microsoft, each of kind oauth, replaces every other client row,
// and keeps c in RAM. The Desktop and TV clients need their secrets; the
// Android, Web and Entra clients take none (the Web client's secret never
// reaches the hub).
func (s *Store) SetClients(ctx context.Context, c Clients) error {
	rows := map[string]Client{}
	for t, cl := range c.Google {
		switch {
		case t != ClientAndroid && t != ClientDesktop && t != ClientTV && t != ClientWeb:
			return fmt.Errorf("oauth: unknown Google client type %q", t)
		case cl.ID == "":
			return fmt.Errorf("oauth: the Google %s client has no ID", t)
		case (t == ClientDesktop || t == ClientTV) && cl.Secret == "":
			return fmt.Errorf("oauth: the Google %s client needs its secret", t)
		case (t == ClientAndroid || t == ClientWeb) && cl.Secret != "":
			return fmt.Errorf("oauth: the Google %s client takes no secret", t)
		}
		rows[clientRow(storage.KindGoogleDrive, t)] = cl
	}
	if m := c.Microsoft; m != nil {
		switch {
		case m.ID == "":
			return errors.New("oauth: the Microsoft client has no ID")
		case m.Secret != "":
			return errors.New("oauth: the Microsoft client is public and takes no secret")
		}
		rows[clientRow(storage.KindOneDrivePersonal, ClientEntra)] = *m
	}
	for id, cl := range rows {
		plain, err := json.Marshal(cl) //nolint:gosec // G117: the plaintext that Seal seals, cleared after it
		if err != nil {
			return err
		}
		sealed, err := s.cfg.Sealer.Seal(ctx, id, plain)
		clear(plain)
		if err != nil {
			return fmt.Errorf("oauth: seal %s: %w", id, err)
		}
		if err := s.cfg.Secrets.PutSecret(ctx, id, secretKind, sealed, s.cfg.Sealer.Backend()); err != nil {
			return err
		}
	}
	ids, err := s.cfg.Secrets.SecretIDs(ctx, clientPrefix)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, keep := rows[id]; !keep {
			if err := s.cfg.Secrets.DeleteSecret(ctx, id); err != nil {
				return err
			}
		}
	}
	s.mu.Lock()
	s.clients = c
	s.mu.Unlock()
	return nil
}

// client returns the client of type t of provider p: the one in RAM, or
// else its sealed row.
func (s *Store) client(ctx context.Context, p storage.Kind, t ClientType) (Client, error) {
	s.mu.Lock()
	var cl Client
	var ok bool
	if p == storage.KindOneDrivePersonal {
		if s.clients.Microsoft != nil {
			cl, ok = *s.clients.Microsoft, true
		}
	} else {
		cl, ok = s.clients.Google[t]
	}
	s.mu.Unlock()
	if ok {
		return cl, nil
	}
	id := clientRow(p, t)
	plain, found, err := s.unsealRow(ctx, id)
	switch {
	case err != nil:
		return Client{}, err
	case !found:
		return Client{}, fmt.Errorf("oauth: no %s client: the setup has not given it: %w", t, storage.ErrAuthExpired)
	}
	defer clear(plain)
	if err := json.Unmarshal(plain, &cl); err != nil {
		return Client{}, fmt.Errorf("oauth: the row %s holds no client: %w", id, storage.ErrAuthExpired)
	}
	return cl, nil
}

// unsealRow returns the plaintext of the sealed row id, and whether there
// is one. A catalog that fails to read is ErrTransient; a row of another
// sealer's backend, or one that does not open, ErrAuthExpired: only the
// setup or a new login mends it.
func (s *Store) unsealRow(ctx context.Context, id string) ([]byte, bool, error) {
	sealed, backend, ok, err := s.cfg.Secrets.Secret(ctx, id)
	switch {
	case err != nil:
		return nil, false, fmt.Errorf("oauth: read %s: %w", id, storage.ErrTransient)
	case !ok:
		return nil, false, nil
	case backend != s.cfg.Sealer.Backend():
		return nil, false, fmt.Errorf("oauth: the row %s was sealed by %s, not this hub's %s: %w", id, backend, s.cfg.Sealer.Backend(), storage.ErrAuthExpired)
	}
	plain, err := s.cfg.Sealer.Unseal(ctx, id, sealed)
	if err != nil {
		return nil, false, fmt.Errorf("oauth: unseal %s: %w: %w", id, err, storage.ErrAuthExpired)
	}
	return plain, true, nil
}

// Begin starts a login of r.DestID: plan A returns the authorization URL
// with a new state and the S256 challenge of a new verifier; B1 asks the
// provider's device endpoint for a user code. The state lives 10 min (a
// device login, the device code's life) and is single-use.
func (s *Store) Begin(ctx context.Context, r BeginRequest) (BeginResult, error) {
	if !refPattern.MatchString(r.DestID) {
		return BeginResult{}, fmt.Errorf("oauth: destId %q is not a ref: letters, digits, '.', '_' and '-', 64 at most", r.DestID)
	}
	route, err := RouteFor(r.Provider, r.Platform, r.Plan, r.LoopbackPort)
	if err != nil {
		return BeginResult{}, err
	}
	client, err := s.client(ctx, r.Provider, route.ClientType)
	if err != nil {
		return BeginResult{}, err
	}
	state, err := randomToken(s.cfg.Rand)
	if err != nil {
		return BeginResult{}, err
	}
	p := &pending{req: r, route: route, client: client}
	result := BeginResult{State: state, Plan: route.Plan, RedirectURI: route.RedirectURI}
	now := s.cfg.Clock()
	if route.Plan == PlanB1 {
		dev, err := s.deviceAuthorization(ctx, r.Provider, client)
		if err != nil {
			return BeginResult{}, err
		}
		p.device, p.interval, p.expires = dev.DeviceCode, dev.interval(), now.Add(dev.life())
		result.VerificationURL, result.UserCode = dev.verification(), dev.UserCode
	} else {
		if p.verifier, err = randomToken(s.cfg.Rand); err != nil {
			return BeginResult{}, err
		}
		p.expires = now.Add(stateLife)
		result.AuthURL = s.authURL(r.Provider, route, client, state, challengeS256(p.verifier))
	}
	result.ExpiresAt = p.expires
	s.mu.Lock()
	defer s.mu.Unlock()
	for st, old := range s.pending {
		if now.After(old.expires.Add(pendingKept)) {
			delete(s.pending, st)
		}
	}
	s.pending[state] = p
	return result, nil
}

// authURL is plan A's authorization request (P36): Google's with
// drive.file, offline access and consent; Microsoft's for personal
// accounts with the app folder, offline access and a query response.
func (s *Store) authURL(p storage.Kind, route Route, client Client, state, challenge string) string {
	q := url.Values{
		"client_id": {client.ID}, "redirect_uri": {route.RedirectURI}, "response_type": {"code"}, "state": {state},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	base := s.cfg.Endpoints.GoogleAuth
	if p == storage.KindOneDrivePersonal {
		base = s.cfg.Endpoints.MicrosoftAuth
		q.Set("scope", scopeMicrosoft)
		q.Set("response_mode", "query")
	} else {
		q.Set("scope", scopeGoogle)
		q.Set("access_type", "offline")
		q.Set("prompt", "consent")
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + q.Encode()
}

// Complete redeems code for the plan A login of state, with Begin's exact
// redirect URI and the state's verifier, seals the refresh token as the
// login's row and keeps the access token in RAM. The state is used up
// whatever the provider answers; an expired one is ErrStateExpired, and its
// verifier is wiped.
func (s *Store) Complete(ctx context.Context, state, code string) (Completion, error) {
	s.mu.Lock()
	p := s.pending[state]
	switch {
	case p == nil || p.device != "":
		s.mu.Unlock()
		return Completion{}, ErrStateUnknown
	case p.used:
		s.mu.Unlock()
		return Completion{}, ErrStateUsed
	case !s.cfg.Clock().Before(p.expires):
		p.used, p.verifier = true, ""
		s.mu.Unlock()
		return Completion{}, ErrStateExpired
	}
	p.used = true
	verifier := p.verifier
	p.verifier = ""
	s.mu.Unlock()
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {p.route.RedirectURI},
		"code_verifier": {verifier}}
	tok, err := s.tokenCall(ctx, p.req.Provider, p.client, form)
	if err != nil {
		return Completion{}, err
	}
	return s.completed(ctx, p, tok)
}

// DeviceStatus is the B1 login of state: its Completion once the user
// approved, ErrAuthorizationPending until then, while the hub polls the
// provider's token endpoint no faster than its interval; the provider's
// refusal (access_denied, expired_token) ends it.
func (s *Store) DeviceStatus(ctx context.Context, state string) (Completion, error) {
	now := s.cfg.Clock()
	s.mu.Lock()
	p := s.pending[state]
	switch {
	case p == nil || p.device == "":
		s.mu.Unlock()
		return Completion{}, ErrStateUnknown
	case p.completion != nil:
		c := *p.completion
		s.mu.Unlock()
		return c, nil
	case p.failed != nil:
		err := p.failed
		s.mu.Unlock()
		return Completion{}, err
	case !now.Before(p.expires):
		s.mu.Unlock()
		return Completion{}, ErrStateExpired
	case p.polling || now.Before(p.nextPoll):
		s.mu.Unlock()
		return Completion{}, ErrAuthorizationPending
	}
	p.polling = true
	s.mu.Unlock()
	form := url.Values{"grant_type": {grantDeviceCode}, "device_code": {p.device}}
	tok, err := s.tokenCall(ctx, p.req.Provider, p.client, form)
	s.mu.Lock()
	p.polling = false
	var pe *ProviderError
	switch {
	case errors.As(err, &pe) && pe.Code == "authorization_pending":
		p.nextPoll = now.Add(p.interval)
		s.mu.Unlock()
		return Completion{}, ErrAuthorizationPending
	case errors.As(err, &pe) && pe.Code == "slow_down":
		p.interval += 5 * time.Second
		p.nextPoll = now.Add(p.interval)
		s.mu.Unlock()
		return Completion{}, ErrAuthorizationPending
	case errors.As(err, &pe) && pe.Status < 500 && pe.Status != 429:
		p.failed = err
		s.mu.Unlock()
		return Completion{}, err
	case err != nil:
		p.nextPoll = now.Add(p.interval)
		s.mu.Unlock()
		return Completion{}, err
	}
	p.used = true
	s.mu.Unlock()
	return s.completed(ctx, p, tok)
}

// The statuses of a login that Status reports.
const (
	StatusPending   = "pending"
	StatusCompleted = "completed"
	StatusExpired   = "expired"
	StatusFailed    = "failed"
)

// Status is the login of state: pending, completed with its Completion,
// expired, or failed when the provider ended a device login. A device
// login is polled as DeviceStatus does.
func (s *Store) Status(ctx context.Context, state string) (string, Completion, error) {
	s.mu.Lock()
	p := s.pending[state]
	s.mu.Unlock()
	if p == nil {
		return "", Completion{}, ErrStateUnknown
	}
	if p.device != "" {
		c, err := s.DeviceStatus(ctx, state)
		switch {
		case err == nil:
			return StatusCompleted, c, nil
		case errors.Is(err, ErrAuthorizationPending), errors.Is(err, storage.ErrTransient):
			return StatusPending, Completion{}, nil
		case errors.Is(err, ErrStateExpired):
			return StatusExpired, Completion{}, nil
		}
		return StatusFailed, Completion{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case p.completion != nil:
		return StatusCompleted, *p.completion, nil
	case !s.cfg.Clock().Before(p.expires):
		return StatusExpired, Completion{}, nil
	}
	return StatusPending, Completion{}, nil
}

// completed stores the login that tok completes: the sealed row of its
// refresh token, the access token in RAM, and its Completion.
func (s *Store) completed(ctx context.Context, p *pending, tok tokenAnswer) (Completion, error) {
	if tok.RefreshToken == "" {
		return Completion{}, errors.New("oauth: the provider gave no refresh token")
	}
	now := s.cfg.Clock()
	l := login{RefreshToken: tok.RefreshToken, Provider: p.req.Provider, Platform: p.req.Platform,
		ClientType: p.route.ClientType, Plan: p.route.Plan, IssuedAtMs: now.UnixMilli()}
	if err := s.putLogin(ctx, p.req.DestID, l); err != nil {
		return Completion{}, err
	}
	src := s.source(p.req.DestID)
	src.mu.Lock()
	src.set(tok, now)
	src.mu.Unlock()
	c := Completion{DestID: p.req.DestID, Provider: p.req.Provider, Platform: p.req.Platform, Plan: p.route.Plan,
		ClientType: p.route.ClientType, IssuedAt: time.UnixMilli(l.IssuedAtMs).UTC()}
	s.mu.Lock()
	p.completion = &c
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return c, nil
}

// Login is the login of ref as its row holds it, without its refresh token,
// and whether there is one.
func (s *Store) Login(ctx context.Context, ref string) (Completion, bool, error) {
	_, l, ok, err := s.readLogin(ctx, ref)
	if err != nil || !ok {
		return Completion{}, ok, err
	}
	return Completion{DestID: ref, Provider: l.Provider, Platform: l.Platform, Plan: l.Plan, ClientType: l.ClientType,
		IssuedAt: time.UnixMilli(l.IssuedAtMs).UTC()}, true, nil
}
