package storage_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// TestErrors_MapToReasons (P24, §7.7): each driver error maps to its storage
// reason, as returned and wrapped once with %w, and the two struct errors as
// values and as pointers. A nil pointer is no reason, and does not panic.
// Only AuthExpired, QuotaFull, RateLimited and DailyCap have a storage reason
// (2100–2102); the others are ERROR_REASON_UNSPECIFIED (0), and of those only
// Transient is retryable, because a Faza 1 Connect mapping gives it
// `unavailable` (CTR-14).
func TestErrors_MapToReasons(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want storage.ReasonInfo
	}{
		{"ErrAuthExpired", storage.ErrAuthExpired, storage.ReasonInfo{Code: 2100}},
		{"ErrQuotaFull", storage.ErrQuotaFull, storage.ReasonInfo{Code: 2101}},
		{
			"ErrRateLimited", storage.ErrRateLimited{RetryAfter: 30 * time.Second},
			storage.ReasonInfo{Code: 2102, RetryAfter: 30 * time.Second, Retryable: true},
		},
		{
			"ErrDailyCap", storage.ErrDailyCap{ResetAfter: 24 * time.Hour},
			storage.ReasonInfo{Code: 2102, RetryAfter: 24 * time.Hour, Retryable: true},
		},
		{
			"*ErrRateLimited", &storage.ErrRateLimited{RetryAfter: 30 * time.Second},
			storage.ReasonInfo{Code: 2102, RetryAfter: 30 * time.Second, Retryable: true},
		},
		{
			"*ErrDailyCap", &storage.ErrDailyCap{ResetAfter: 24 * time.Hour},
			storage.ReasonInfo{Code: 2102, RetryAfter: 24 * time.Hour, Retryable: true},
		},
		{"nil *ErrRateLimited", (*storage.ErrRateLimited)(nil), storage.ReasonInfo{}},
		{"nil *ErrDailyCap", (*storage.ErrDailyCap)(nil), storage.ReasonInfo{}},
		{"ErrTransient", storage.ErrTransient, storage.ReasonInfo{Retryable: true}},
		{"ErrNotFound", storage.ErrNotFound, storage.ReasonInfo{}},
		{"ErrSessionExpired", storage.ErrSessionExpired, storage.ReasonInfo{}},
		{"ErrPermanent", storage.ErrPermanent, storage.ReasonInfo{}},
		{"ErrProxyRequired", storage.ErrProxyRequired, storage.ReasonInfo{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, err := range []error{tt.err, fmt.Errorf("upload chunk: %w", tt.err)} {
				if got := storage.Reason(err); got != tt.want {
					t.Errorf("Reason(%q) = %+v, want %+v", err, got, tt.want)
				}
			}
		})
	}
}

// TestErrors_ProxyRequiredIs: PlaybackURL's "use the hub's proxy" answer
// survives wrapping (§9.1).
func TestErrors_ProxyRequiredIs(t *testing.T) {
	if err := fmt.Errorf("x: %w", storage.ErrProxyRequired); !errors.Is(err, storage.ErrProxyRequired) {
		t.Errorf("errors.Is(%q, ErrProxyRequired) = false, want true", err)
	}
}
