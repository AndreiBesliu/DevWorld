package onedrive

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
	// maxErrorBody is the most of an error's answer that the driver reads for
	// its code.
	maxErrorBody = 64 << 10
	// maxCode bounds the length of a code that an error's text names.
	maxCode = 64
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
		kind = storage.ErrRateLimited{RetryAfter: retryAfter(resp.Header, now)}
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
// and digits is dropped, so the text never carries what a server wrote
// there.
func errorCode(body io.Reader) (string, bool) {
	type inner struct {
		Code  string `json:"code"`
		Inner *inner `json:"innerError"`
	}
	var answer struct {
		Error inner `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(body, maxErrorBody)).Decode(&answer) != nil {
		return "", false
	}
	quota := false
	for e, depth := &answer.Error, 0; e != nil && depth < 8; e, depth = e.Inner, depth+1 {
		quota = quota || e.Code == codeQuota
	}
	code := answer.Error.Code
	if len(code) > maxCode {
		return "", quota
	}
	for _, r := range code {
		if !wordRune(r) {
			return "", quota
		}
	}
	return code, quota
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
// request's URL (g2-03): the uploadUrl, or the download URL that /content
// redirected to.
func networkError(method string, err error) error {
	return fmt.Errorf("onedrive: %s: %w: %w", method, storage.ErrTransient, scrub(err))
}

// scrub returns err without the *url.Error around it, whose text quotes the
// request's URL.
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
		return fmt.Errorf("onedrive: %s: token: %w", method, scrub(err))
	}
	return &tokenFailure{method: method, err: err}
}

// tokenFailure is a token source's error that wraps no storage error.
type tokenFailure struct {
	method string
	err    error
}

func (e *tokenFailure) Error() string {
	return fmt.Sprintf("onedrive: %s: the token source failed: %v", e.method, storage.ErrTransient)
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
