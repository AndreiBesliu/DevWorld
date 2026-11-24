package onviftest

import (
	"crypto/sha1" //nolint:gosec // G505: the PasswordDigest under test
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The test camera's account.
const (
	testUser = "admin"
	testPass = "secret-1"
)

// wsse is a WS-UsernameToken header for testUser with password pass and
// Created at created.
func wsse(pass string, created time.Time) string {
	nonce := []byte("0123456789abcdef")
	c := created.UTC().Format("2006-01-02T15:04:05.000Z")
	h := sha1.New() //nolint:gosec // G401: see the import
	h.Write(nonce)
	h.Write([]byte(c))
	h.Write([]byte(pass))
	return `<s:Header><wsse:Security><wsse:UsernameToken><wsse:Username>` + testUser + `</wsse:Username>` +
		`<wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` +
		base64.StdEncoding.EncodeToString(h.Sum(nil)) + `</wsse:Password><wsse:Nonce>` + base64.StdEncoding.EncodeToString(nonce) +
		`</wsse:Nonce><wsu:Created>` + c + `</wsu:Created></wsse:UsernameToken></wsse:Security></s:Header>`
}

// send posts a SOAP request with header and body to url, and returns the
// status and the response's body.
func send(t *testing.T, url, header, body string) (int, string) {
	t.Helper()
	env := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" ` +
		`xmlns:wsse="x" xmlns:wsu="y" xmlns:tev="z" xmlns:wsnt="w" xmlns:tds="v">` + header + `<s:Body>` + body + `</s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(env))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

// newTestSim starts a camera with the test account.
func newTestSim(t *testing.T, cfg Config) *Sim {
	t.Helper()
	cfg.User, cfg.Pass = testUser, testPass
	s := New(cfg)
	t.Cleanup(s.Close)
	return s
}

// TestSim_MaxPullPointsAndExpiry: the camera holds MaxPullPoints
// subscriptions at most, and drops one at its term's end.
func TestSim_MaxPullPointsAndExpiry(t *testing.T) {
	s := newTestSim(t, Config{MaxPullPoints: 2})
	create := `<tev:CreatePullPointSubscription><tev:InitialTerminationTime>PT0.3S</tev:InitialTerminationTime></tev:CreatePullPointSubscription>`
	for i := range 3 {
		status, body := send(t, s.srv.URL+eventsPath, wsse(testPass, time.Now()), create)
		if ok := status == http.StatusOK; ok != (i < 2) {
			t.Fatalf("create %d: status %d %s", i+1, status, body)
		}
	}
	if n := s.Subscriptions(); n != 2 {
		t.Errorf("subscriptions = %d; want 2", n)
	}
	time.Sleep(400 * time.Millisecond)
	if n := s.Subscriptions(); n != 0 {
		t.Errorf("subscriptions = %d after their term; want 0", n)
	}
	if c := s.Counts(); c.Creates != 2 || c.Expired != 2 {
		t.Errorf("counts = %+v; want 2 creates, 2 expired", c)
	}
}

// TestSim_ClockSkewChecksCreated: a camera 2 h ahead refuses a Created on
// the real clock and accepts one on its own.
func TestSim_ClockSkewChecksCreated(t *testing.T) {
	s := newTestSim(t, Config{ClockSkew: 2 * time.Hour, WSSEOnly: true})
	info := `<tds:GetDeviceInformation/>`
	if status, _ := send(t, s.XAddr(), wsse(testPass, time.Now()), info); status != http.StatusBadRequest {
		t.Errorf("unshifted Created: status %d; want 400 NotAuthorized", status)
	}
	if status, body := send(t, s.XAddr(), wsse(testPass, time.Now().Add(2*time.Hour)), info); status != http.StatusOK {
		t.Errorf("shifted Created: status %d %s", status, body)
	}
	if status, _ := send(t, s.XAddr(), wsse("wrong", time.Now().Add(2*time.Hour)), info); status != http.StatusBadRequest {
		t.Errorf("wrong password: status %d; want 400", status)
	}
	got := s.Requests()
	if len(got) != 3 || got[0].Accepted || !got[1].Accepted || got[2].Accepted || got[1].Auth != AuthWSSEDigest {
		t.Errorf("requests = %+v", got)
	}
}

// TestSim_DigestOnlyRefusesWSSE: a digest-only camera refuses a valid
// WS-UsernameToken with a 401 that offers HTTP digest, and answers
// GetSystemDateAndTime without a login.
func TestSim_DigestOnlyRefusesWSSE(t *testing.T) {
	s := newTestSim(t, Config{DigestOnly: true})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.XAddr(),
		strings.NewReader(`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">`+wsse(testPass, time.Now())+
			`<s:Body><GetDeviceInformation/></s:Body></s:Envelope>`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || len(resp.Header.Values("WWW-Authenticate")) != 2 {
		t.Errorf("status %d, challenges %q; want 401 with MD5 and SHA-256 digest", resp.StatusCode, resp.Header.Values("WWW-Authenticate"))
	}
	if status, body := send(t, s.XAddr(), "", `<tds:GetSystemDateAndTime/>`); status != http.StatusOK || !strings.Contains(body, "UTCDateTime") {
		t.Errorf("GetSystemDateAndTime: status %d %s", status, body)
	}
}
