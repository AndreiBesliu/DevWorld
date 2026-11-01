package onedrive

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
	driverName = "onedrive"
	// codeQuota is Graph's code for an account out of storage.
	codeQuota = "quotaLimitReached"
)

// statusError maps an answer with a status that c does not expect to a typed
// error of package storage (Task 6's mapping, after Graph's error codes):
//   - 401 is ErrAuthExpired;
//   - 507, or the code quotaLimitReached, is ErrQuotaFull;
//   - 429 is ErrRateLimited, after Retry-After (0 without one);
//   - 404 or 410 on an uploadUrl is ErrSessionExpired (Resume then asks
//     whether the item at the key completed), and 404 on anything else
//     ErrNotFound;
//   - 416 on an uploadUrl is ErrTransient: Graph answers it to a fragment
//     that the session holds already, and the next attempt's Resume asks the
//     session where to go on, where ErrPermanent would fail the placement
//     for good;
//   - 5xx is ErrTransient, and any other 4xx ErrPermanent;
//   - a status below 400 that c does not expect is ErrTransient.
//
// The text names c's method, the status and Graph's code only: never the
// request's URL, which for a fragment is the pre-authenticated uploadUrl,
// nor Graph's message (g2-03).
func statusError(c *call, resp *http.Response, now time.Time) error {
	status := resp.StatusCode
	code, quota := errorCode(resp.Body)
	text := fmt.Sprintf("onedrive: %s: HTTP %d", c.method, status)
	if code != "" {
		text += " " + code
	}
	var kind error
	switch {
	case status == http.StatusUnauthorized:
		kind = storage.ErrAuthExpired
	case status == http.StatusInsufficientStorage || quota:
		kind = storage.ErrQuotaFull
	case status == http.StatusTooManyRequests:
		kind = storage.ErrRateLimited{RetryAfter: rest.RetryAfter(resp.Header, now)}
	case c.upload && (status == http.StatusNotFound || status == http.StatusGone):
		kind = storage.ErrSessionExpired
	case status == http.StatusNotFound:
		kind = storage.ErrNotFound
	case c.upload && status == http.StatusRequestedRangeNotSatisfiable:
		kind = storage.ErrTransient
	case status >= http.StatusInternalServerError:
		kind = storage.ErrTransient
	case status >= http.StatusBadRequest:
		kind = storage.ErrPermanent
	default:
		kind = storage.ErrTransient
	}
	return fmt.Errorf("%s: %w", text, kind)
}

// errorCode returns the code of Graph's error body, {"error":{"code":…,
// "innerError":{"code":…}}}, "" when it has none, and whether the code or any
// inner one is quotaLimitReached. A code that is not a short word of letters
// and digits is dropped (rest.Word), so the text never carries what a server
// wrote there.
func errorCode(body io.Reader) (string, bool) {
	type inner struct {
		Code  string `json:"code"`
		Inner *inner `json:"innerError"`
	}
	var answer struct {
		Error inner `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(body, rest.MaxErrorBody)).Decode(&answer) != nil {
		return "", false
	}
	quota := false
	for e, depth := &answer.Error, 0; e != nil && depth < 8; e, depth = e.Inner, depth+1 {
		quota = quota || e.Code == codeQuota
	}
	return rest.Word(answer.Error.Code), quota
}
