package storage_test

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"slices"
	"strings"
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

// TestErrors_IsTypedKnowsEveryTypedError: IsTyped finds each typed error of
// errors.go, bare and wrapped, the struct errors as values and as non-nil
// pointers, and no other error. The table must name every Err sentinel and
// Err type that errors.go declares, so a typed error added there fails this
// test until the table, and with it IsTyped, knows it: a driver's token
// errors rely on IsTyped to keep their kind (rest.TokenError).
func TestErrors_IsTypedKnowsEveryTypedError(t *testing.T) {
	typed := map[string][]error{
		"ErrAuthExpired":    {storage.ErrAuthExpired},
		"ErrQuotaFull":      {storage.ErrQuotaFull},
		"ErrSessionExpired": {storage.ErrSessionExpired},
		"ErrNotFound":       {storage.ErrNotFound},
		"ErrTransient":      {storage.ErrTransient},
		"ErrPermanent":      {storage.ErrPermanent},
		"ErrProxyRequired":  {storage.ErrProxyRequired},
		"ErrRateLimited":    {storage.ErrRateLimited{}, &storage.ErrRateLimited{RetryAfter: time.Second}},
		"ErrDailyCap":       {storage.ErrDailyCap{ResetAfter: time.Hour}, &storage.ErrDailyCap{}},
	}
	declared := declaredErrors(t, "errors.go")
	var named []string
	for name := range typed {
		named = append(named, name)
	}
	slices.Sort(named)
	if !slices.Equal(named, declared) {
		t.Errorf("errors.go declares the typed errors %v, and this test names %v: add the new one here and to IsTyped", declared, named)
	}
	for name, errs := range typed {
		for _, err := range errs {
			for _, e := range []error{err, fmt.Errorf("onedrive: token: %w", err), fmt.Errorf("a: %w", fmt.Errorf("b: %w", err))} {
				if !storage.IsTyped(e) {
					t.Errorf("IsTyped(%q) = false, want true: it wraps %s", e, name)
				}
			}
		}
	}
	for _, err := range []error{
		nil, errors.New("storage: transient error"), io.EOF, context.DeadlineExceeded,
		(*storage.ErrRateLimited)(nil), (*storage.ErrDailyCap)(nil), fmt.Errorf("x: %w", io.ErrUnexpectedEOF),
	} {
		if storage.IsTyped(err) {
			t.Errorf("IsTyped(%#v) = true, want false", err)
		}
	}
}

// declaredErrors returns, sorted, the names of the package-level variables
// and types of file whose names start with Err.
func declaredErrors(t *testing.T, file string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var names []string
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			switch spec := spec.(type) {
			case *ast.ValueSpec:
				for _, id := range spec.Names {
					if strings.HasPrefix(id.Name, "Err") {
						names = append(names, id.Name)
					}
				}
			case *ast.TypeSpec:
				if strings.HasPrefix(spec.Name.Name, "Err") {
					names = append(names, spec.Name.Name)
				}
			}
		}
	}
	slices.Sort(names)
	return names
}
