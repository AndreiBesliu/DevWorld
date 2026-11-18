// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package hubproc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The control endpoint's answers, as ipcam-hub's oauth package encodes
// them (uploadspike reads them across the process boundary, P01).
type (
	// BeginRequest starts a login: Plan "A" (default) or "B1".
	BeginRequest struct {
		DestID       string `json:"destId"`
		Provider     string `json:"provider"`
		Platform     string `json:"platform"`
		LoopbackPort int    `json:"loopbackPort,omitempty"`
		Plan         string `json:"plan,omitempty"`
	}
	// BeginResult is what the app needs to log in; it never holds the
	// verifier, which stays in the hub.
	BeginResult struct {
		State           string    `json:"state"`
		Plan            string    `json:"plan"`
		AuthURL         string    `json:"authUrl,omitempty"`
		RedirectURI     string    `json:"redirectUri,omitempty"`
		VerificationURL string    `json:"verificationUrl,omitempty"`
		UserCode        string    `json:"userCode,omitempty"`
		ExpiresAt       time.Time `json:"expiresAt"`
	}
	// Completion is a login that completed.
	Completion struct {
		DestID     string    `json:"destId"`
		Provider   string    `json:"provider"`
		Platform   string    `json:"platform"`
		Plan       string    `json:"plan"`
		ClientType string    `json:"clientType"`
		IssuedAt   time.Time `json:"issuedAt"`
	}
	// Status is a login's state: "pending", "completed", "expired" or
	// "failed", with its reason.
	Status struct {
		Status     string      `json:"status"`
		Completion *Completion `json:"completion,omitempty"`
		Reason     string      `json:"reason,omitempty"`
	}
	// Target is a creator's folder for the web reader (P21).
	Target struct {
		Creator  string `json:"creator"`
		FolderID string `json:"folderId"`
	}
)

// FailureError is the control endpoint's refusal: its status and the
// oauth reason, which holds no secret.
type FailureError struct {
	HTTPStatus int
	Reason     string
}

func (e *FailureError) Error() string {
	return fmt.Sprintf("hubproc: the control endpoint answered %d (%s)", e.HTTPStatus, e.Reason)
}

// controlTimeout bounds a control call; a device login's status polls the
// provider once on the way.
const controlTimeout = 30 * time.Second

// ControlClient calls a hub's control endpoint on 127.0.0.1 with its
// token. Its errors never quote the token, a code or a body.
type ControlClient struct {
	base, token string
	http        *http.Client
}

// Control is the hub's control client, once its control event came.
func (h *Hub) Control(ctx context.Context) (*ControlClient, error) {
	e, err := h.WaitEvent(ctx, "control")
	if err != nil {
		return nil, err
	}
	var ev struct {
		Addr string `json:"addr"`
	}
	if err := json.Unmarshal(e.Raw, &ev); err != nil || ev.Addr == "" {
		return nil, errors.New("hubproc: a control event without an address")
	}
	return NewControlClient(ev.Addr, h.cfg.ControlToken), nil
}

// NewControlClient calls the control endpoint at addr (127.0.0.1:port).
func NewControlClient(addr, token string) *ControlClient {
	return &ControlClient{base: "http://" + addr, token: token,
		http: &http.Client{Timeout: controlTimeout, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}}
}

// Token is the control token: the closing scans search for it (P45).
func (c *ControlClient) Token() string { return c.token }

// call sends a request with body (nil: none) and decodes a 2xx answer into
// out (nil: none).
func (c *ControlClient) call(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return errors.New("hubproc: a control request could not be built")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hubproc: the control endpoint: %w", stripURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("hubproc: the control endpoint's answer: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		var f struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(data, &f)
		return &FailureError{HTTPStatus: resp.StatusCode, Reason: f.Reason}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("hubproc: the control endpoint's answer is not its JSON")
	}
	return nil
}

// stripURL drops the request URL from a client error: a status call's
// carries the login's state.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// SetClients seals the OAuth clients in the hub (P46): clients is the
// control endpoint's Clients JSON.
func (c *ControlClient) SetClients(ctx context.Context, clients json.RawMessage) error {
	return c.call(ctx, http.MethodPost, "/control/v1/clients", clients, nil)
}

// Begin starts a login.
func (c *ControlClient) Begin(ctx context.Context, req BeginRequest) (BeginResult, error) {
	var res BeginResult
	err := c.call(ctx, http.MethodPost, "/control/v1/oauth/begin", req, &res)
	return res, err
}

// Complete redeems a plan A login's code.
func (c *ControlClient) Complete(ctx context.Context, state, code string) (Completion, error) {
	var done Completion
	err := c.call(ctx, http.MethodPost, "/control/v1/oauth/complete", map[string]string{"state": state, "code": code}, &done)
	return done, err
}

// Status reports a login; a device login is polled on the way.
func (c *ControlClient) Status(ctx context.Context, state string) (Status, error) {
	var s Status
	err := c.call(ctx, http.MethodGet, "/control/v1/oauth/status?state="+url.QueryEscape(state), nil, &s)
	return s, err
}

// Targets lists the creators' folders for the web reader.
func (c *ControlClient) Targets(ctx context.Context) ([]Target, error) {
	var out []Target
	err := c.call(ctx, http.MethodGet, "/control/v1/visibility/targets", nil, &out)
	return out, err
}

// ResetConnections closes the hub's upload connections, idle and in
// flight (MI5 calls it right after its block, Task 9): each request in
// flight fails as transient and is retried on a new connection.
func (c *ControlClient) ResetConnections(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/control/v1/uploads/reset-connections", nil, nil)
}
