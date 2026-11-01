package rest_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/rest"
)

// sentinel marks secretURL, a request URL that no error's text may quote
// (g2-03): it stands for a session URI, an uploadUrl or a download link.
var (
	sentinel  = "SENT" + "INEL"
	secretURL = "https://host.invalid/s/" + sentinel + "-session-url?k=" + sentinel + "-query"
)

// leaks reports whether text quotes any part of secretURL.
func leaks(text string) bool {
	return strings.Contains(text, sentinel) || strings.Contains(text, "host.invalid") || strings.Contains(text, "http")
}

// urlError is the error that net/http's client returns for secretURL.
func urlError(cause error) error {
	return &url.Error{Op: "Put", URL: secretURL, Err: cause}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		header string
		want   time.Duration
	}{
		{"", 0},
		{"12", 12 * time.Second},
		{"0", 0},
		{"-5", 0},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{"soon", 0},
	}
	for _, tt := range tests {
		h := http.Header{}
		if tt.header != "" {
			h.Set("Retry-After", tt.header)
		}
		if got := rest.RetryAfter(h, now); got != tt.want {
			t.Errorf("RetryAfter(%q) = %s, want %s", tt.header, got, tt.want)
		}
	}
}

func TestWord(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "",
		"quotaLimitReached":      "quotaLimitReached",
		"rate_limit_2":           "rate_limit_2",
		strings.Repeat("a", 64):  strings.Repeat("a", 64),
		strings.Repeat("a", 65):  "",
		"bad reason":             "",
		"see https://x.invalid/": "",
		"ăă":                     "",
	} {
		if got := rest.Word(in); got != want {
			t.Errorf("Word(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestScrubURLError: every *url.Error around a cause goes, however deep, and
// one with no cause becomes its operation; any other error stays as it is.
func TestScrubURLError(t *testing.T) {
	cause := errors.New("connection reset by peer")
	if got := rest.ScrubURLError(urlError(urlError(cause))); got != cause {
		t.Errorf("ScrubURLError of two url.Errors = %v, want the cause", got)
	}
	if got := rest.ScrubURLError(urlError(nil)); got == nil || got.Error() != "Put failed" {
		t.Errorf("ScrubURLError of a url.Error with no cause = %v, want \"Put failed\"", got)
	}
	if got := rest.ScrubURLError(cause); got != cause {
		t.Errorf("ScrubURLError(%v) = %v, want it unchanged", cause, got)
	}
	if got := rest.ScrubURLError(nil); got != nil {
		t.Errorf("ScrubURLError(nil) = %v, want nil", got)
	}
}

// TestErrors_NameDriverMethodAndNoURL: NetworkError, RequestError and
// TokenError name the driver and the method, wrap their kind, and quote no
// part of a request's URL.
func TestErrors_NameDriverMethodAndNoURL(t *testing.T) {
	reset := errors.New("connection reset by peer")
	tests := []struct {
		name     string
		err      error
		kind     error
		wantText string
	}{
		{"a request with no answer", rest.NetworkError("drv", "upload.put", urlError(reset)), storage.ErrTransient,
			"drv: upload.put: storage: transient error: connection reset by peer"},
		{"a request that cannot be made", rest.RequestError("drv", "upload.put"), storage.ErrPermanent,
			"drv: upload.put: the request cannot be made: storage: permanent error"},
		{"a token error of a kind", rest.TokenError("drv", "files.get", urlError(fmt.Errorf("refresh: %w", storage.ErrAuthExpired))),
			storage.ErrAuthExpired, "drv: files.get: token: refresh: storage: authorization expired"},
		{"a token error rate limited", rest.TokenError("drv", "files.get", storage.ErrRateLimited{RetryAfter: time.Second}),
			storage.ErrRateLimited{RetryAfter: time.Second}, "drv: files.get: token: storage: rate limited, retry after 1s"},
		{"a token error of no kind", rest.TokenError("drv", "files.get", urlError(errors.New("oauth2: cannot fetch token"))),
			storage.ErrTransient, "drv: files.get: the token source failed: storage: transient error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.kind) {
				t.Errorf("%q does not wrap %v", tt.err, tt.kind)
			}
			if got := tt.err.Error(); got != tt.wantText || leaks(got) {
				t.Errorf("the text %q, want %q", got, tt.wantText)
			}
		})
	}
	// The token source's own error stays in the chain, for errors.As.
	cause := errors.New("oauth2: cannot fetch token")
	if err := rest.TokenError("drv", "files.get", cause); !errors.Is(err, cause) {
		t.Errorf("TokenError(%v) does not wrap its cause", cause)
	}
}

// body is an answer's body that holds data, then fails with err, or ends;
// it records its Close.
type body struct {
	r      io.Reader
	err    error
	closed bool
}

func (b *body) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if errors.Is(err, io.EOF) && b.err != nil {
		err = b.err
	}
	return n, err
}

func (b *body) Close() error {
	b.closed = true
	return nil
}

// answer is an Answer of status whose body holds data, then fails with
// readErr when it is not nil; the returned func reports whether the answer
// was closed and its deadline ended.
func answer(status int, data string, readErr error) (*rest.Answer, func() bool) {
	b := &body{r: strings.NewReader(data), err: readErr}
	ctx, cancel := context.WithCancel(context.Background())
	a := rest.NewAnswer("drv", &http.Response{StatusCode: status, Body: b}, cancel)
	return a, func() bool { return b.closed && ctx.Err() != nil }
}

func TestAnswer_Decode(t *testing.T) {
	a, closed := answer(http.StatusOK, `{"id":"x1","size":3}`, nil)
	var v struct {
		ID   string `json:"id"`
		Size int64  `json:"size"`
	}
	if err := a.Decode("items.get", &v); err != nil || v.ID != "x1" || v.Size != 3 {
		t.Errorf("Decode = %v, %+v, want x1 of 3 bytes", err, v)
	}
	if !closed() {
		t.Error("Decode left the answer open")
	}

	a, closed = answer(http.StatusOK, `{"id":`, urlError(errors.New("unexpected EOF")))
	err := a.Decode("items.get", &v)
	if !errors.Is(err, storage.ErrTransient) || !strings.HasPrefix(err.Error(), "drv: items.get: the answer: ") || leaks(err.Error()) {
		t.Errorf("Decode of a broken answer = %v, want ErrTransient named for drv and items.get, with no URL", err)
	}
	if !closed() {
		t.Error("Decode of a broken answer left it open")
	}
}

// TestRangeBody: a 206 is the range; a 200, the whole object, is skipped to
// the offset; an object that ends before the offset reads as empty; a body
// that breaks off fails as ErrTransient with no URL; Close closes the answer.
func TestRangeBody(t *testing.T) {
	const object = "0123456789"
	tests := []struct {
		name    string
		status  int
		data    string
		off, n  int64
		want    string
		readErr error
	}{
		{name: "206", status: http.StatusPartialContent, data: "3456", off: 3, n: 4, want: "3456"},
		{name: "206 longer than asked", status: http.StatusPartialContent, data: "34567", off: 3, n: 4, want: "3456"},
		{name: "200 skips to off", status: http.StatusOK, data: object, off: 3, n: 4, want: "3456"},
		{name: "200 that ends sooner", status: http.StatusOK, data: object, off: 8, n: 4, want: "89"},
		{name: "200 that ends before off", status: http.StatusOK, data: object, off: 12, n: 4, want: ""},
		{name: "a body that breaks off", status: http.StatusPartialContent, data: "34", off: 3, n: 4, want: "34",
			readErr: urlError(errors.New("connection reset by peer"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, closed := answer(tt.status, tt.data, tt.readErr)
			r, err := rest.RangeBody(a, "files.get.media", tt.off, tt.n)
			if err != nil {
				t.Fatalf("RangeBody: %v", err)
			}
			got, err := io.ReadAll(r)
			if string(got) != tt.want {
				t.Errorf("read %q, want %q", got, tt.want)
			}
			switch {
			case tt.readErr == nil && err != nil:
				t.Errorf("read: %v", err)
			case tt.readErr != nil && (!errors.Is(err, storage.ErrTransient) || leaks(err.Error()) ||
				err.Error() != "drv: files.get.media: storage: transient error: connection reset by peer"):
				t.Errorf("read = %v, want ErrTransient named for drv and files.get.media, with no URL", err)
			}
			if err := r.Close(); err != nil || !closed() {
				t.Errorf("Close = %v, closed %v, want the answer closed", err, closed())
			}
		})
	}

	// A 200 that breaks off before off fails at once, and is closed.
	a, closed := answer(http.StatusOK, "012", urlError(errors.New("connection reset by peer")))
	if _, err := rest.RangeBody(a, "items.content", 5, 4); !errors.Is(err, storage.ErrTransient) || leaks(err.Error()) || !closed() {
		t.Errorf("RangeBody of a 200 that breaks off before off = %v, closed %v, want ErrTransient with no URL, closed", err, closed())
	}
}
