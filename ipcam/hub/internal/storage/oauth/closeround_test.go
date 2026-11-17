package oauth

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakeoauth "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/oauth"
)

// The tests of the residuals of the Task 9A security round (C2-R20).

// TestRefresh_KeptRotationCarriesNoDailyTime (C2-R20 (c), security review
// STORE-1, C2-R16 (d′)): a daily refresh whose rotated row fails to store
// is a failed day; the rotation that the store keeps for the next refresh
// carries the token change only, so writing it records no daily success:
// only RunDaily's successes write DailyRefreshedAtMs.
func TestRefresh_KeptRotationCarriesNoDailyTime(t *testing.T) {
	e := newEnv(t, fakeoauth.Options{})
	secrets := &failingSwaps{SecretStore: e.cat}
	s := e.store(func(c *Config) { c.Secrets = secrets })
	s.rotationGrace = time.Millisecond
	e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
	secrets.fail.Store(1)
	if _, err := s.refresh(t.Context(), "g-windows", true); !errors.Is(err, storage.ErrTransient) {
		t.Fatalf("the daily refresh whose swap failed = %v; want ErrTransient", err)
	}
	if res, err := s.Refresh(t.Context(), "g-windows"); err != nil || !res.Rotated {
		t.Fatalf("the next refresh = %+v, %v; want the kept rotation written and refreshed", res, err)
	}
	_, l, ok, err := s.readLogin(t.Context(), "g-windows")
	if err != nil || !ok {
		t.Fatalf("the login's row: %v, %v", ok, err)
	}
	if l.DailyRefreshedAtMs != 0 {
		t.Errorf("DailyRefreshedAtMs = %d after a failed daily refresh; want 0: no daily success was recorded", l.DailyRefreshedAtMs)
	}
	if len(l.PreviousRefreshTokens) != 2 {
		t.Errorf("the row keeps %d superseded tokens, want 2: the kept rotation's and the next refresh's", len(l.PreviousRefreshTokens))
	}
}

// TestProviderError_UnknownCodeAt4xxEndsTheLogin (C2-R20 (e), security
// review STORE-3): a 4xx answer whose JSON body carries an OAuth error code
// that the hub does not know is the provider's refusal, ErrAuthExpired; a
// 408, a 429, a 5xx, a known transient code, and an answer with no OAuth
// error code in a JSON body (a proxy's page, a JSON body without an error
// string) are ErrTransient. Neither text quotes the unknown code.
func TestProviderError_UnknownCodeAt4xxEndsTheLogin(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
		auth                    bool
	}{
		{"an unknown code at 400", "application/json", `{"error":"invalid_rapt"}`, http.StatusBadRequest, true},
		{"an unknown code at 401", "application/json", `{"error":"unknown_code_x"}`, http.StatusUnauthorized, true},
		{"an unknown code at 403", "application/json; charset=utf-8", `{"error":"policy_refused","error_description":"x"}`, http.StatusForbidden, true},
		{"an unknown code at 408", "application/json", `{"error":"invalid_rapt"}`, http.StatusRequestTimeout, false},
		{"an unknown code at 429", "application/json", `{"error":"invalid_rapt"}`, http.StatusTooManyRequests, false},
		{"an unknown code at 503", "application/json", `{"error":"invalid_rapt"}`, http.StatusServiceUnavailable, false},
		{"a transient code at 400", "application/json", `{"error":"temporarily_unavailable"}`, http.StatusBadRequest, false},
		{"a JSON body without an error", "application/json", `{"error_description":"denied"}`, http.StatusForbidden, false},
		{"a JSON error that is no string", "application/json", `{"error":{"code":"Forbidden"}}`, http.StatusForbidden, false},
		{"an empty error", "application/json", `{"error":""}`, http.StatusBadRequest, false},
		{"an HTML 403", "text/html", "<html>blocked by policy</html>", http.StatusForbidden, false},
		{"an empty 407", "", "", http.StatusProxyAuthRequired, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, fakeoauth.Options{})
			s := e.interceptedStore(func(w http.ResponseWriter, r *http.Request) bool {
				if grantOf(r) != "refresh_token" {
					return false
				}
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
				return true
			})
			s.rotationGrace = time.Millisecond
			e.login(s, "g-windows", storage.KindGoogleDrive, PlatformWindows, "")
			_, err := s.Refresh(t.Context(), "g-windows")
			auth, transient := errors.Is(err, storage.ErrAuthExpired), errors.Is(err, storage.ErrTransient)
			if auth != tc.auth || transient == tc.auth {
				t.Errorf("the refresh = %v: ErrAuthExpired %v, ErrTransient %v; want ErrAuthExpired %v", err, auth, transient, tc.auth)
			}
			for _, word := range []string{"invalid_rapt", "unknown_code_x", "policy_refused", "Forbidden", "denied"} {
				if err != nil && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(word)) {
					t.Errorf("the error %q quotes the provider's %q", err, word)
				}
			}
		})
	}
}
