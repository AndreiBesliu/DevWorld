package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// controlTokenEnv names the control endpoint's bearer token in ipcam-hub
// run's environment (P01 item 3, P46): 32 hex characters or more. uploadspike
// sets it; without it, run serves no control endpoint.
const controlTokenEnv = "IPCAM_CONTROL_TOKEN" //nolint:gosec // G101: the name of a variable

// The control endpoint's limits.
const (
	controlMaxBody     = 64 << 10
	controlReadTimeout = 10 * time.Second
	defaultTestingDays = 7 // Phase 0's Google project is in Testing (P53)
)

// controlTokenPattern is a valid control token.
var controlTokenPattern = regexp.MustCompile(`^[0-9A-Fa-f]{32,}$`)

// oauthSection is run.json's oauth section (Task 9): the providers'
// endpoints, the live ones when absent (the fakes' in the lab and the
// tests), and the Google refresh token's life in Testing mode, 7 days when
// absent.
type oauthSection struct {
	Endpoints         *oauth.Endpoints `json:"endpoints"`
	TestingExpiryDays *int             `json:"testingExpiryDays"`
}

// validate checks the section: every endpoint an http(s) URL, and no
// negative life.
func (o *oauthSection) validate() error {
	if o.TestingExpiryDays != nil && *o.TestingExpiryDays < 0 {
		return fmt.Errorf("oauth: a negative testingExpiryDays %d", *o.TestingExpiryDays)
	}
	if e := o.Endpoints; e != nil {
		for name, v := range map[string]string{
			"googleAuth": e.GoogleAuth, "googleToken": e.GoogleToken, "googleDevice": e.GoogleDevice, "googleRevoke": e.GoogleRevoke,
			"microsoftAuth": e.MicrosoftAuth, "microsoftToken": e.MicrosoftToken, "microsoftDevice": e.MicrosoftDevice,
		} {
			if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return fmt.Errorf("oauth: endpoints.%s is not an http(s) URL", name)
			}
		}
	}
	return nil
}

// newOAuthStore is the token store of the data directory of cfg on secrets,
// the catalog, calling the providers through client.
func newOAuthStore(cfg runConfig, secrets oauth.SecretStore, client *http.Client) (*oauth.Store, error) {
	sealer, err := crypto.NewSealer(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	endpoints, days := oauth.DefaultEndpoints(), defaultTestingDays
	if o := cfg.OAuth; o != nil {
		if o.Endpoints != nil {
			endpoints = *o.Endpoints
		}
		if o.TestingExpiryDays != nil {
			days = *o.TestingExpiryDays
		}
	}
	return oauth.New(oauth.Config{Endpoints: endpoints, Sealer: sealer, HTTP: client, Secrets: secrets,
		TestingExpiry: time.Duration(days) * 24 * time.Hour})
}

// readControlToken reads and unsets the control token of the environment,
// so that no child inherits it: "" when it is not set. Its error never
// quotes it.
func readControlToken() (string, error) {
	token, ok := os.LookupEnv(controlTokenEnv)
	if !ok {
		return "", nil
	}
	_ = os.Unsetenv(controlTokenEnv)
	if !controlTokenPattern.MatchString(token) {
		return "", fmt.Errorf("%s is not 32 or more hex characters", controlTokenEnv)
	}
	return token, nil
}

// control is ipcam-hub run's control endpoint on 127.0.0.1 (P01 item 3,
// P46): uploadspike's way into the token store and the uploads. Every
// request needs Authorization: Bearer <token>; no answer, event or error
// holds a token, a secret, a code or a verifier.
type control struct {
	token string
	store *oauth.Store
	up    *uploads // nil without an uploads section
	uc    *uploadsConfig
	cat   *catalog.DB
	ev    *events
	conns *connTracker

	mu       sync.Mutex
	reported map[string]bool // device logins whose end went out as an event
}

// serve listens on 127.0.0.1:0 and serves c until ctx ends; it returns the
// address it took and a stop that waits for it.
func (c *control) serve(ctx context.Context) (string, func(), error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("the control endpoint: %w", err)
	}
	srv := &http.Server{Handler: c.handler(), ReadHeaderTimeout: controlReadTimeout, ReadTimeout: controlReadTimeout}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ln)
	}()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-done
	}
	return ln.Addr().String(), stop, nil
}

func (c *control) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /control/v1/clients", c.clients)
	mux.HandleFunc("POST /control/v1/oauth/begin", c.begin)
	mux.HandleFunc("POST /control/v1/oauth/complete", c.complete)
	mux.HandleFunc("GET /control/v1/oauth/status", c.status)
	mux.HandleFunc("POST /control/v1/oauth/refresh", c.refresh)
	mux.HandleFunc("GET /control/v1/visibility/targets", c.targets)
	mux.HandleFunc("POST /control/v1/uploads/reset-connections", c.resetConnections)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !c.authorized(r) {
			writeControl(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, controlMaxBody)
		mux.ServeHTTP(w, r)
	})
}

// authorized reports whether r carries the control token, compared in
// constant time.
func (c *control) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(prefix) || h[:len(prefix)] != prefix {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(h[len(prefix):]), []byte(c.token)) == 1
}

// decode reads r's JSON body into v, refusing unknown fields.
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("the body is not the endpoint's JSON")
	}
	return nil
}

func writeControl(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeFailure answers err, which holds no secret, with its oauth reason.
func writeFailure(w http.ResponseWriter, status int, err error) {
	writeControl(w, status, map[string]string{"error": err.Error(), "reason": oauth.Reason(err)})
}

// clients seals the OAuth clients (P46).
func (c *control) clients(w http.ResponseWriter, r *http.Request) {
	var cl oauth.Clients
	if err := decode(r, &cl); err != nil {
		writeFailure(w, http.StatusBadRequest, err)
		return
	}
	if err := c.store.SetClients(r.Context(), cl); err != nil {
		writeFailure(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// begin starts a login.
func (c *control) begin(w http.ResponseWriter, r *http.Request) {
	var req oauth.BeginRequest
	if err := decode(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, err)
		return
	}
	res, err := c.store.Begin(r.Context(), req)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, err)
		return
	}
	c.ev.emit("oauth_begin", map[string]any{"destId": req.DestID, "provider": req.Provider, "platform": req.Platform, "plan": res.Plan})
	writeControl(w, http.StatusOK, res)
}

// complete redeems a plan A login's code; the destinations of its tokenRef
// are unblocked.
func (c *control) complete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		State string `json:"state"`
		Code  string `json:"code"`
	}
	if err := decode(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, err)
		return
	}
	done, err := c.store.Complete(r.Context(), req.State, req.Code)
	c.firstReport(req.State) // its end goes out here: a later status reports it no more
	if err != nil {
		c.failed(done.DestID, "complete", err)
		writeFailure(w, http.StatusBadRequest, err)
		return
	}
	c.completed(done)
	writeControl(w, http.StatusOK, done)
}

// status reports a login: pending, completed, expired or failed; a device
// login is polled on the way. A login's end goes out as an event once,
// here or at its complete.
func (c *control) status(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	status, done, err := c.store.Status(r.Context(), state)
	stage := "complete"
	if done.Plan == oauth.PlanB1 {
		stage = "device"
	}
	switch {
	case errors.Is(err, oauth.ErrStateUnknown):
		writeFailure(w, http.StatusNotFound, err)
		return
	case status == oauth.StatusCompleted && c.firstReport(state):
		c.completed(done)
	case status == oauth.StatusFailed && c.firstReport(state):
		c.failed(done.DestID, stage, err)
	case status == oauth.StatusExpired && c.firstReport(state):
		c.failed(done.DestID, stage, oauth.ErrStateExpired)
	}
	answer := map[string]any{"status": status}
	if status == oauth.StatusCompleted {
		answer["completion"] = done
	}
	if err != nil {
		answer["reason"] = oauth.Reason(err)
	}
	writeControl(w, http.StatusOK, answer)
}

// firstReport reports whether state's end goes out now, once.
func (c *control) firstReport(state string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reported[state] {
		return false
	}
	c.reported[state] = true
	return true
}

// refresh refreshes a login now.
func (c *control) refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DestID string `json:"destId"`
	}
	if err := decode(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, err)
		return
	}
	res, err := c.store.Refresh(r.Context(), req.DestID)
	if err != nil {
		c.failed(req.DestID, "refresh", err)
		writeFailure(w, http.StatusBadGateway, err)
		return
	}
	writeControl(w, http.StatusOK, res)
}

// visibilityTarget is a creator's folder for the web reader (P21).
type visibilityTarget struct {
	Creator  oauth.ClientType `json:"creator"`
	FolderID string           `json:"folderId"`
}

// targets lists each login's own Drive destination whose root folder the
// hub knows, with the login's client type as its creator: the folders the
// web reader lists. The IDs are the catalog's; no provider is called.
func (c *control) targets(w http.ResponseWriter, r *http.Request) {
	out := []visibilityTarget{}
	if c.uc != nil {
		for _, d := range c.uc.Destinations {
			if d.Kind != storage.KindGoogleDrive || d.TokenRef != d.ID {
				continue
			}
			login, ok, err := c.store.Login(r.Context(), d.TokenRef)
			if err != nil || !ok {
				continue
			}
			folder, ok, err := c.cat.Folder(r.Context(), d.ID, driveRoot(d))
			if err != nil || !ok {
				continue
			}
			out = append(out, visibilityTarget{Creator: login.ClientType, FolderID: folder})
		}
	}
	writeControl(w, http.StatusOK, out)
}

// resetConnections closes the drivers' and the token store's connections,
// idle and in flight: each request in flight fails as transient and is
// retried on a new connection (MI5 calls it right after its block).
func (c *control) resetConnections(w http.ResponseWriter, _ *http.Request) {
	c.conns.reset()
	w.WriteHeader(http.StatusNoContent)
}

// completed reports a completed login and unblocks the destinations of its
// tokenRef.
func (c *control) completed(done oauth.Completion) {
	c.ev.emit("oauth_completed", map[string]any{"destId": done.DestID, "provider": done.Provider, "platform": done.Platform,
		"plan": done.Plan, "clientType": done.ClientType, "issuedAtMs": done.IssuedAt.UnixMilli()})
	if c.up != nil {
		c.up.unblock(done.DestID)
	}
}

// failed reports a login's or a refresh's failure, by its reason only.
func (c *control) failed(destID, stage string, err error) {
	c.ev.emit("oauth_failed", map[string]any{"destId": destID, "stage": stage, "reason": oauth.Reason(err)})
}

// driveRoot is a Drive destination's root folder in My Drive.
func driveRoot(d destDef) string {
	if d.Folder == "" {
		return "ipcam"
	}
	return d.Folder
}

// runDaily runs the store's daily refresh (P53) until stop, its results as
// oauth_refresh events.
func runDaily(store *oauth.Store, ev *events) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = store.RunDaily(ctx, func(r oauth.DailyResult) {
			ev.emit("oauth_refresh", map[string]any{"destId": r.DestID, "day": r.Day, "attempt": r.Attempt,
				"outcome": r.Outcome, "rotated": r.Rotated})
		})
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(shutdownWait):
		}
	}
}
