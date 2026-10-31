package storage

import (
	"errors"
	"fmt"
	"time"
)

// The typed errors of a Destination (§9.1). A driver returns them wrapped
// with its context; errors.Is and errors.As find them, and Reason maps them
// to §7.7's reasons.
var (
	// ErrAuthExpired means the provider refused the credentials; the owner
	// must sign in again.
	ErrAuthExpired = errors.New("storage: authorization expired")
	// ErrQuotaFull means the destination has no room for the object.
	ErrQuotaFull = errors.New("storage: quota full")
	// ErrSessionExpired means the upload session is gone; the upload
	// restarts from 0 in a new session.
	ErrSessionExpired = errors.New("storage: upload session expired")
	// ErrNotFound means the object does not exist.
	ErrNotFound = errors.New("storage: object not found")
	// ErrTransient is a network error or a 5xx; the same call may succeed
	// later.
	ErrTransient = errors.New("storage: transient error")
	// ErrPermanent means the provider refused the call, and repeating it
	// will not help.
	ErrPermanent = errors.New("storage: permanent error")
	// ErrProxyRequired means PlaybackURL has no link to give; the hub serves
	// the object itself.
	ErrProxyRequired = errors.New("storage: playback needs the hub's proxy")
)

// ErrRateLimited means the provider asked to wait RetryAfter before the next
// call. Drivers return it without sleeping (P26); the outbox waits. Reason
// finds it as a value and as a pointer.
type ErrRateLimited struct{ RetryAfter time.Duration }

func (e ErrRateLimited) Error() string {
	return fmt.Sprintf("storage: rate limited, retry after %s", e.RetryAfter)
}

// ErrDailyCap means the account reached the provider's daily upload cap,
// which resets after ResetAfter (Drive: 750 GiB a day, a 24 h pause).
// Reason finds it as a value and as a pointer.
type ErrDailyCap struct{ ResetAfter time.Duration }

func (e ErrDailyCap) Error() string {
	return fmt.Sprintf("storage: daily upload cap reached, resets after %s", e.ResetAfter)
}

// The storage values of §7.7's ErrorReason.
const (
	ReasonUnspecified = 0    // ERROR_REASON_UNSPECIFIED: never shown as a storage reason
	ReasonAuthExpired = 2100 // STORAGE_AUTH_EXPIRED
	ReasonQuotaFull   = 2101 // STORAGE_QUOTA_FULL
	ReasonRateLimited = 2102 // STORAGE_RATE_LIMITED
)

// ReasonInfo is what §7.7's ErrorInfo carries for a storage error: the
// reason, the retry_after of the errors that have one, and whether a retry
// may succeed.
type ReasonInfo struct {
	Code       int
	RetryAfter time.Duration
	Retryable  bool
}

// Reason maps a driver error, wrapped or not, to its §7.7 reason (P24).
// ErrAuthExpired is 2100 and ErrQuotaFull 2101. ErrRateLimited and
// ErrDailyCap, as values or as non-nil pointers, are 2102, retryable after
// their wait. Every other error, nil included, is ReasonUnspecified, and of
// those only ErrTransient is retryable, as Faza 1's Connect mapping makes it
// `unavailable` (CTR-14).
func Reason(err error) ReasonInfo {
	var rateLimited ErrRateLimited
	var rateLimitedPtr *ErrRateLimited
	var dailyCap ErrDailyCap
	var dailyCapPtr *ErrDailyCap
	switch {
	case errors.Is(err, ErrAuthExpired):
		return ReasonInfo{Code: ReasonAuthExpired}
	case errors.Is(err, ErrQuotaFull):
		return ReasonInfo{Code: ReasonQuotaFull}
	case errors.As(err, &rateLimited):
		return ReasonInfo{Code: ReasonRateLimited, RetryAfter: rateLimited.RetryAfter, Retryable: true}
	case errors.As(err, &rateLimitedPtr) && rateLimitedPtr != nil:
		return ReasonInfo{Code: ReasonRateLimited, RetryAfter: rateLimitedPtr.RetryAfter, Retryable: true}
	case errors.As(err, &dailyCap):
		return ReasonInfo{Code: ReasonRateLimited, RetryAfter: dailyCap.ResetAfter, Retryable: true}
	case errors.As(err, &dailyCapPtr) && dailyCapPtr != nil:
		return ReasonInfo{Code: ReasonRateLimited, RetryAfter: dailyCapPtr.ResetAfter, Retryable: true}
	case errors.Is(err, ErrTransient):
		return ReasonInfo{Code: ReasonUnspecified, Retryable: true}
	}
	return ReasonInfo{Code: ReasonUnspecified}
}
