// Package oauth is the hub's OAuth store (§8.7, T11, T34, P19, P27): the
// logins of the cloud destinations, kept by ipcam-hub alone.
//
//   - Begin makes the login's state and PKCE verifier (plan A) or starts the
//     provider's device flow (plan B1, P39), and keeps them in RAM for
//     10 min, single-use (FLW-22). Apps hand back only code and state;
//     Complete redeems the code with the exact redirect URI of Begin.
//   - The refresh token is sealed (crypto.Sealer) under the label
//     oauth:<ref> and kept as the catalog's secrets row oauth:<ref>, kind
//     oauth, where ref is the login's destId and run.json's tokenRef.
//     Access tokens stay in RAM: one TokenSource per ref, shared by every
//     destination that names it.
//   - A refresh reads the sealed row, calls the token endpoint outside any
//     transaction within 5 s, and writes a rotated refresh token with
//     SwapSecret before it returns the access token, so two processes never
//     keep a revoked token and the catalog's other writers never wait on a
//     token endpoint that a WAN block has silenced.
//   - invalid_grant on an unchanged row, invalid_client and the other
//     refusals that need a new login, and an OAuth error code that the hub
//     does not know at a 4xx, wrap storage.ErrAuthExpired; a network
//     error, a timeout, a 408, a 429, a 5xx, a transient OAuth code at any
//     status and an answer without an OAuth error code (a proxy's page)
//     wrap storage.ErrTransient. A fault of the sealer's key is neither: a
//     new login would not mend it (crypto.ErrKeyUnavailable).
//   - RunDaily refreshes each login once a day, counted from its issuance
//     (P53), and records each daily success in the login's sealed row, so
//     a restart neither attempts nor reports again a day that was ok.
//
// No error text, and nothing this package returns, holds a token, a client
// secret, a code, a verifier, a request URL or a provider's own words
// (PH0-22): a provider's error is reduced to its OAuth error code.
package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// Platform is where a login runs.
type Platform string

// The platforms of Phase 0 (P39).
const (
	PlatformAndroid Platform = "android"
	PlatformWindows Platform = "windows"
	PlatformWeb     Platform = "web"
)

// ClientType is an OAuth client's type: Google's Android, Desktop, TV and
// Web clients, and Microsoft's one Entra public client.
type ClientType string

// The client types.
const (
	ClientAndroid ClientType = "android"
	ClientDesktop ClientType = "desktop"
	ClientTV      ClientType = "tv"
	ClientWeb     ClientType = "web"
	ClientEntra   ClientType = "entra"
)

// The plans of a login (P39): A, the app's own login handed to the hub, and
// B1, the hub's device flow.
const (
	PlanA  = "A"
	PlanB1 = "B1"
)

// Client is an OAuth client. Secret is only the Desktop and TV clients',
// which are not confidential (§8.7).
type Client struct {
	ID     string `json:"id"`
	Secret string `json:"secret,omitempty"`
}

// Clients are the hub's OAuth clients. Microsoft is nil unless A5 passed.
type Clients struct {
	Google    map[ClientType]Client `json:"google,omitempty"`
	Microsoft *Client               `json:"microsoft,omitempty"`
}

// Endpoints are the providers' OAuth endpoints; run.json may name the
// fakes' (Task 9).
type Endpoints struct {
	GoogleAuth      string `json:"googleAuth"`
	GoogleToken     string `json:"googleToken"`
	GoogleDevice    string `json:"googleDevice"`
	GoogleRevoke    string `json:"googleRevoke"`
	MicrosoftAuth   string `json:"microsoftAuth"`
	MicrosoftToken  string `json:"microsoftToken"`
	MicrosoftDevice string `json:"microsoftDevice"`
}

// DefaultEndpoints are the live providers' endpoints: Google's, and
// Microsoft's for personal accounts (authority consumers, P36).
func DefaultEndpoints() Endpoints {
	const ms = "https://login.microsoftonline.com/consumers/oauth2/v2.0/"
	return Endpoints{ //nolint:gosec // G101: public endpoint URLs, no credential
		GoogleAuth:      "https://accounts.google.com/o/oauth2/v2/auth",
		GoogleToken:     "https://oauth2.googleapis.com/token",
		GoogleDevice:    "https://oauth2.googleapis.com/device/code",
		GoogleRevoke:    "https://oauth2.googleapis.com/revoke",
		MicrosoftAuth:   ms + "authorize",
		MicrosoftToken:  ms + "token",
		MicrosoftDevice: ms + "devicecode",
	}
}

// SecretStore keeps the sealed rows: catalog.DB is one. No method holds a
// transaction across its caller's I/O.
type SecretStore interface {
	PutSecret(ctx context.Context, id, kind string, sealed []byte, sealer string) error
	Secret(ctx context.Context, id string) (sealed []byte, sealer string, ok bool, err error)
	// SwapSecret writes next only while the row still holds old
	// (compare-and-swap in one short transaction); swapped is false when
	// another writer changed the row first.
	SwapSecret(ctx context.Context, id string, old, next []byte, sealer string) (swapped bool, err error)
	// ReplaceSecrets writes rows and deletes every other row of prefix, in
	// one transaction: SetClients replaces the client rows with it (Task 9A
	// review M7). SecretIDs serves RunDaily, which lists the logins.
	ReplaceSecrets(ctx context.Context, prefix, kind string, rows map[string][]byte, sealer string) error
	SecretIDs(ctx context.Context, prefix string) ([]string, error)
}

// Config configures a Store.
type Config struct {
	// Clients are clients in RAM, ahead of the sealed client rows; tests
	// set them. ipcam-hub starts with none and reads the rows that
	// SetClients wrote.
	Clients   Clients
	Endpoints Endpoints        // DefaultEndpoints() when every field is empty
	Clock     func() time.Time // time.Now by default
	Sealer    crypto.Sealer
	HTTP      *http.Client // the drivers' plain client, with their timeouts (Task 5)
	Secrets   SecretStore
	Rand      io.Reader // crypto/rand by default: states and verifiers
	// TestingExpiry is a Google refresh token's life in Testing mode: 7×24 h
	// in Phase 0. An invalid_grant at or after it is expected (P53).
	TestingExpiry time.Duration
}

// Route is a provider × platform pair's client type and exact redirect URI
// under a plan; B1 has no redirect URI.
type Route struct {
	ClientType  ClientType
	RedirectURI string
	Plan        string
}

// BeginRequest starts a login of DestID, its tokenRef. Plan is "A" (the
// default) or "B1"; LoopbackPort is the Windows app's loopback port.
type BeginRequest struct {
	DestID       string       `json:"destId"`
	Provider     storage.Kind `json:"provider"`
	Platform     Platform     `json:"platform"`
	LoopbackPort int          `json:"loopbackPort,omitempty"`
	Plan         string       `json:"plan,omitempty"`
}

// BeginResult is what the app needs to log in: plan A's authorization URL
// and redirect URI, or B1's verification URL and user code. It never holds
// the verifier, which stays in the hub.
type BeginResult struct {
	State           string    `json:"state"`
	Plan            string    `json:"plan"`
	AuthURL         string    `json:"authUrl,omitempty"`
	RedirectURI     string    `json:"redirectUri,omitempty"`
	VerificationURL string    `json:"verificationUrl,omitempty"`
	UserCode        string    `json:"userCode,omitempty"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

// Completion is a login that completed.
type Completion struct {
	DestID     string       `json:"destId"`
	Provider   storage.Kind `json:"provider"`
	Platform   Platform     `json:"platform"`
	Plan       string       `json:"plan"`
	ClientType ClientType   `json:"clientType"`
	IssuedAt   time.Time    `json:"issuedAt"`
}

// RefreshResult is a refresh that succeeded; Rotated when the provider gave
// a new refresh token, which the row now holds.
type RefreshResult struct {
	At      time.Time `json:"at"`
	Rotated bool      `json:"rotated"`
}

// The outcomes of a day of the daily refresh (P53).
const (
	OutcomeOK             = "ok"
	OutcomeFailed         = "failed"
	OutcomeExpectedExpiry = "expected-expiry"
)

// DailyResult is a day of a login's daily refresh, once it is decided: ok
// at its first success, failed when every attempt of its 24 h failed, or
// expected-expiry for Google's invalid_grant past TestingExpiry.
type DailyResult struct {
	DestID  string
	Day     int // 1…7
	Attempt int // the attempts of the day so far
	Outcome string
	Rotated bool
}

// The errors of a login's state (FLW-22).
var (
	ErrStateUnknown         = errors.New("oauth: no login has this state")
	ErrStateExpired         = errors.New("oauth: the login's state expired")
	ErrStateUsed            = errors.New("oauth: the login's state was used")
	ErrAuthorizationPending = errors.New("oauth: the device login waits for the user")
)

// The OAuth error codes that ProviderError keeps; any other is
// codeUnknown, and an answer without one has none ("").
var providerCodes = map[string]bool{
	"invalid_grant": true, "invalid_client": true, "unauthorized_client": true, "access_denied": true,
	"redirect_uri_mismatch": true, "expired_token": true, "invalid_request": true, "invalid_scope": true,
	"unsupported_grant_type": true, "authorization_pending": true, "slow_down": true,
	"temporarily_unavailable": true, "server_error": true, "rate_limit_exceeded": true,
}

// terminalCodes are the OAuth refusals that end a login: only a new login,
// or a new setup of the clients, mends them.
var terminalCodes = map[string]bool{
	"invalid_grant": true, "invalid_client": true, "unauthorized_client": true, "access_denied": true,
	"expired_token": true, "redirect_uri_mismatch": true, "invalid_scope": true, "unsupported_grant_type": true,
	"invalid_request": true,
}

// codeUnknown is ProviderError's code for an OAuth error code that the hub
// does not know, which it never quotes.
const codeUnknown = "error"

// ProviderError is a provider's refusal: its OAuth error code (one of
// providerCodes, codeUnknown, or "" when the answer's body holds no OAuth
// error code) and HTTP status, never its own words. Unwrap classifies it,
// the one classifier of the token, device and refresh calls (security
// review STORE-3).
type ProviderError struct {
	Code   string
	Status int
}

func (e *ProviderError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("oauth: the provider's endpoint answered with no OAuth error code (HTTP %d)", e.Status)
	}
	return fmt.Sprintf("oauth: the provider refused: %s (HTTP %d)", e.Code, e.Status)
}

// Unwrap is the storage error that the refusal is: storage.ErrTransient
// for a 408, a 429 or a 5xx whatever its code; storage.ErrAuthExpired for a
// refusal that ends the login (terminalCodes), and for an OAuth error code
// that the hub does not know at any other 4xx: the provider's own refusal
// (C2-R20 (e)); and ErrTransient for any other answer: a transient code
// (temporarily_unavailable, server_error, rate_limit_exceeded) at any
// status, a device flow's authorization_pending or slow_down, and an
// answer without an OAuth error code, such as a proxy's HTML 403 or 407,
// which must not force a new login (Task 9A review M3).
func (e *ProviderError) Unwrap() error {
	switch {
	case e.Status == http.StatusRequestTimeout || e.Status == http.StatusTooManyRequests || e.Status >= http.StatusInternalServerError:
		return storage.ErrTransient
	case terminalCodes[e.Code]:
		return storage.ErrAuthExpired
	case e.Code == codeUnknown && e.Status >= http.StatusBadRequest:
		return storage.ErrAuthExpired
	}
	return storage.ErrTransient
}

// endsLogin reports whether err is a provider's refusal that ends a login.
func endsLogin(err error) bool {
	var pe *ProviderError
	return errors.As(err, &pe) && errors.Is(pe, storage.ErrAuthExpired)
}

// Reason is err's reason for the oauth_failed event: invalid_grant,
// access_denied, redirect_uri_mismatch, invalid_client or expired_token
// from the provider, state for a state that is unknown, expired or used,
// transient when the provider did not answer, and error otherwise.
func Reason(err error) string {
	if errors.Is(err, ErrStateUnknown) || errors.Is(err, ErrStateExpired) || errors.Is(err, ErrStateUsed) {
		return "state"
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "invalid_grant", "access_denied", "redirect_uri_mismatch", "invalid_client", "expired_token":
			return pe.Code
		}
	}
	if errors.Is(err, storage.ErrTransient) {
		return "transient"
	}
	return "error"
}
