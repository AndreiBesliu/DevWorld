package rest

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	// MaxErrorBody is the most of an error's answer that a driver reads for
	// the provider's reason or code.
	MaxErrorBody = 64 << 10
	// maxWord bounds the length of a word that Word keeps.
	maxWord = 64
)

// Word returns s when it is a short word, at most 64 ASCII letters, digits
// and underscores, as a provider's reason or error code is, and "" when it
// is not, so that an error's text never carries what a server wrote there.
func Word(s string) string {
	if len(s) > maxWord {
		return ""
	}
	for _, r := range s {
		if !wordRune(r) {
			return ""
		}
	}
	return s
}

// wordRune reports whether r is an ASCII letter, digit or underscore.
func wordRune(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '_'
}

// RetryAfter is the wait that a Retry-After header asks for, in seconds or
// as an HTTP date after now; 0 without one, or with one that is neither.
func RetryAfter(h http.Header, now time.Time) time.Duration {
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

// NetworkError is a request of driver's method that got no answer, or whose
// answer's body broke off: ErrTransient, with the cause that the *url.Error
// wraps and not its text, which quotes the request's URL (g2-03).
func NetworkError(driver, method string, err error) error {
	return fmt.Errorf("%s: %s: %w: %w", driver, method, storage.ErrTransient, ScrubURLError(err))
}

// RequestError is a request of driver's method that net/http cannot make:
// ErrPermanent, without the cause, since url.Parse's error quotes parts of
// the URL.
func RequestError(driver, method string) error {
	return fmt.Errorf("%s: %s: the request cannot be made: %w", driver, method, storage.ErrPermanent)
}

// ScrubURLError returns err without the *url.Error around it, whose text
// quotes the request's URL: for a chunk, the session URI or the uploadUrl
// (g2-03). A *url.Error that wraps nothing becomes its operation.
func ScrubURLError(err error) error {
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

// TokenError is an error of driver's token source, at method. One that
// wraps a typed storage error (storage.IsTyped) passes through, scrubbed:
// Task 9's source returns ErrAuthExpired for invalid_grant and ErrTransient
// for a token endpoint that does not answer. Any other is ErrTransient,
// never ErrPermanent, and its text is not kept, since it may quote the token
// endpoint's request.
func TokenError(driver, method string, err error) error {
	if storage.IsTyped(err) {
		return fmt.Errorf("%s: %s: token: %w", driver, method, ScrubURLError(err))
	}
	return &tokenFailure{driver: driver, method: method, err: err}
}

// tokenFailure is a token source's error that wraps no typed storage error.
type tokenFailure struct {
	driver, method string
	err            error
}

func (e *tokenFailure) Error() string {
	return fmt.Sprintf("%s: %s: the token source failed: %v", e.driver, e.method, storage.ErrTransient)
}

func (e *tokenFailure) Unwrap() []error { return []error{storage.ErrTransient, e.err} }
