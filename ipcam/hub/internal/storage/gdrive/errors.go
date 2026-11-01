package gdrive

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/rest"
)

const (
	// driverName starts every error's text.
	driverName = "gdrive"
	// dailyCapPause is how long Drive's daily upload cap holds an account
	// (750 GiB a day per user, §9.2).
	dailyCapPause = 24 * time.Hour
	// statusCancelled is what a cancelled session answers: 499 Client
	// Closed Request, in Google's resumable upload protocol.
	statusCancelled = 499
)

// statusError maps an answer with a status that c does not expect to a typed
// error of package storage (Task 5's mapping, after Drive's error
// reference):
//   - 401 is ErrAuthExpired;
//   - 403 storageQuotaExceeded is ErrQuotaFull;
//   - 403 userRateLimitExceeded or rateLimitExceeded, and 429, are
//     ErrRateLimited, after Retry-After (0 without one);
//   - 403 dailyLimitExceeded or uploadLimitExceeded is ErrDailyCap{24 h};
//   - 404, 410 or 499 (cancelled) on a session URI is ErrSessionExpired, and
//     404 on anything else ErrNotFound;
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
		kind = storage.ErrRateLimited{RetryAfter: rest.RetryAfter(resp.Header, now)}
	case status == http.StatusForbidden && reason == "storageQuotaExceeded":
		kind = storage.ErrQuotaFull
	case status == http.StatusForbidden && (reason == "userRateLimitExceeded" || reason == "rateLimitExceeded"):
		kind = storage.ErrRateLimited{RetryAfter: rest.RetryAfter(resp.Header, now)}
	case status == http.StatusForbidden && (reason == "dailyLimitExceeded" || reason == "uploadLimitExceeded"):
		kind = storage.ErrDailyCap{ResetAfter: dailyCapPause}
	case c.session && (status == http.StatusNotFound || status == http.StatusGone || status == statusCancelled):
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
// letters and digits is dropped (rest.Word), so the text never carries what
// a server wrote there.
func errorReason(body io.Reader) string {
	var answer struct {
		Error struct {
			Errors  []struct{ Reason string } `json:"errors"`
			Details []struct{ Reason string } `json:"details"`
		} `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(body, rest.MaxErrorBody)).Decode(&answer) != nil {
		return ""
	}
	switch {
	case len(answer.Error.Errors) > 0:
		return rest.Word(answer.Error.Errors[0].Reason)
	case len(answer.Error.Details) > 0:
		return rest.Word(answer.Error.Details[0].Reason)
	}
	return ""
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
