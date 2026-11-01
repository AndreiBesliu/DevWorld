package gdrive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	// dailyCapPause is how long Drive's daily upload cap holds an account
	// (750 GiB a day per user, §9.2).
	dailyCapPause = 24 * time.Hour
	// maxErrorBody is the most of an error's answer that the driver reads for
	// its reason.
	maxErrorBody = 64 << 10
	// maxReason bounds the length of a reason that an error's text names.
	maxReason = 64
)

// statusError maps an answer with a status that c does not expect to a typed
// error of package storage (Task 5's mapping, after Drive's error
// reference):
//   - 401 is ErrAuthExpired;
//   - 403 storageQuotaExceeded is ErrQuotaFull;
//   - 403 userRateLimitExceeded or rateLimitExceeded, and 429, are
//     ErrRateLimited, after Retry-After (0 without one);
//   - 403 dailyLimitExceeded or uploadLimitExceeded is ErrDailyCap{24 h};
//   - 404 or 410 on a session URI is ErrSessionExpired, and 404 on anything
//     else ErrNotFound;
//   - 5xx is ErrTransient, and any other 4xx ErrPermanent;
//   - a status below 400 that c does not expect is ErrTransient.
//
// The text names c's method, the status and Drive's reason only: never the
// request's URL, which for a chunk is the session URI, nor Drive's message
// (g2-03).
func statusError(c *call, resp *http.Response, now time.Time) error {
	status := resp.StatusCode
	reason := errorReason(resp.Body)
	text := fmt.Sprintf("gdrive: %s: HTTP %d", c.method, status)
	if reason != "" {
		text += " " + reason
	}
	var kind error
	switch {
	case status == http.StatusUnauthorized:
		kind = storage.ErrAuthExpired
	case status == http.StatusTooManyRequests:
		kind = storage.ErrRateLimited{RetryAfter: retryAfter(resp.Header, now)}
	case status == http.StatusForbidden && reason == "storageQuotaExceeded":
		kind = storage.ErrQuotaFull
	case status == http.StatusForbidden && (reason == "userRateLimitExceeded" || reason == "rateLimitExceeded"):
		kind = storage.ErrRateLimited{RetryAfter: retryAfter(resp.Header, now)}
	case status == http.StatusForbidden && (reason == "dailyLimitExceeded" || reason == "uploadLimitExceeded"):
		kind = storage.ErrDailyCap{ResetAfter: dailyCapPause}
	case c.session && (status == http.StatusNotFound || status == http.StatusGone):
		kind = storage.ErrSessionExpired
	case status == http.StatusNotFound:
		kind = storage.ErrNotFound
	case status >= http.StatusInternalServerError:
		kind = storage.ErrTransient
	case status >= http.StatusBadRequest:
		kind = storage.ErrPermanent
	default:
		kind = storage.ErrTransient
	}
	return fmt.Errorf("%s: %w", text, kind)
}

// errorReason returns the reason of Drive's error body,
// {"error":{"errors":[{"reason":…}]}} or, in the newer shape, a detail's
// reason; "" when the body has none. A reason that is not a short word of
// letters and digits is dropped, so the text never carries what a server
// wrote there.
func errorReason(body io.Reader) string {
	var answer struct {
		Error struct {
			Errors  []struct{ Reason string } `json:"errors"`
			Details []struct{ Reason string } `json:"details"`
		} `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(body, maxErrorBody)).Decode(&answer) != nil {
		return ""
	}
	var reason string
	switch {
	case len(answer.Error.Errors) > 0:
		reason = answer.Error.Errors[0].Reason
	case len(answer.Error.Details) > 0:
		reason = answer.Error.Details[0].Reason
	}
	if len(reason) > maxReason {
		return ""
	}
	for _, r := range reason {
		if !wordRune(r) {
			return ""
		}
	}
	return reason
}

// wordRune reports whether r is an ASCII letter, digit or underscore.
func wordRune(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '_'
}

// retryAfter is the wait that a Retry-After header asks for, in seconds or
// as an HTTP date; 0 without one.
func retryAfter(h http.Header, now time.Time) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if s, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.Duration(max(s, 0)) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0)
	}
	return 0
}

// networkError is a request that got no answer: ErrTransient, with the
// cause that the *url.Error wraps and not its text, which quotes the
// request's URL (g2-03).
func networkError(method string, err error) error {
	return fmt.Errorf("gdrive: %s: %w: %w", method, storage.ErrTransient, scrub(err))
}

// rateLimited is the error of a create that the driver's own bucket holds
// back for wait (STO-07).
func rateLimited(method string, wait time.Duration) error {
	return fmt.Errorf("gdrive: %s: %w", method, storage.ErrRateLimited{RetryAfter: wait})
}

// sessionGone is the error of a call on a session that is not this
// driver's: a new upload starts.
func sessionGone(method string) error {
	return fmt.Errorf("gdrive: %s: not a session of this destination: %w", method, storage.ErrSessionExpired)
}

// scrub returns err without the *url.Error around it, whose text quotes the
// request's URL: for a chunk, the session URI with its upload_id (g2-03).
func scrub(err error) error {
	for {
		ue, ok := err.(*url.Error)
		if !ok {
			return err
		}
		if ue.Err == nil {
			return errors.New(ue.Op + " failed")
		}
		err = ue.Err
	}
}

// tokenError is an error of the token source. One that wraps a storage
// error passes through: Task 9's source returns ErrAuthExpired for
// invalid_grant and ErrTransient for a token endpoint that does not answer.
// Any other is ErrTransient, never ErrPermanent, and its text is not kept,
// since it may quote the token endpoint's request.
func tokenError(method string, err error) error {
	if isStorageError(err) {
		return fmt.Errorf("gdrive: %s: token: %w", method, scrub(err))
	}
	return &tokenFailure{method: method, err: err}
}

// tokenFailure is a token source's error that wraps no storage error.
type tokenFailure struct {
	method string
	err    error
}

func (e *tokenFailure) Error() string {
	return fmt.Sprintf("gdrive: %s: the token source failed: %v", e.method, storage.ErrTransient)
}

func (e *tokenFailure) Unwrap() []error { return []error{storage.ErrTransient, e.err} }

// isStorageError reports whether err wraps a typed error of package
// storage.
func isStorageError(err error) bool {
	for _, target := range []error{
		storage.ErrAuthExpired, storage.ErrQuotaFull, storage.ErrSessionExpired, storage.ErrNotFound,
		storage.ErrTransient, storage.ErrPermanent, storage.ErrProxyRequired,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return storage.Reason(err).Code == storage.ReasonRateLimited // ErrRateLimited and ErrDailyCap
}
