// Package rest holds what the hub's REST storage drivers, gdrive and
// onedrive, share around net/http: an accepted answer and its bounded
// reads, a range read's body, the wait that Retry-After asks for, and the
// errors of a request that got no answer, of a request that cannot be made
// and of a token source.
//
// Every error built here names the driver, the API method and the status or
// cause, and never quotes a request's URL, query or header (g2-03, PH0-22):
// a chunk's URL is the provider's pre-authenticated session URI or
// uploadUrl, and a download's may be a pre-authenticated link. A
// *url.Error is unwrapped (ScrubURLError), never printed. The driver names
// passed in ("gdrive", "onedrive") start each error's text, as the drivers'
// own errors do.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	// maxAnswer bounds the JSON answer that Decode reads.
	maxAnswer = 1 << 20
	// maxDrain is the most of an answer's rest that Close reads, so that the
	// connection can serve the next request.
	maxDrain = 64 << 10
)

// Answer is a response that a driver accepted, within the request's
// deadline. Close releases it.
type Answer struct {
	*http.Response
	driver string
	cancel context.CancelFunc
}

// NewAnswer is resp, the answer to a request of driver whose deadline cancel
// ends.
func NewAnswer(driver string, resp *http.Response, cancel context.CancelFunc) *Answer {
	return &Answer{Response: resp, driver: driver, cancel: cancel}
}

// Close reads a little of the body's rest, closes it and ends the request's
// deadline.
func (a *Answer) Close() {
	_, _ = io.Copy(io.Discard, io.LimitReader(a.Body, maxDrain))
	_ = a.Body.Close()
	a.cancel()
}

// Decode reads the answer's JSON, at most 1 MiB, into v and closes the
// answer. An answer that is no such JSON is ErrTransient for method, with
// the cause scrubbed.
func (a *Answer) Decode(method string, v any) error {
	defer a.Close()
	if err := json.NewDecoder(io.LimitReader(a.Body, maxAnswer)).Decode(v); err != nil {
		return fmt.Errorf("%s: %s: the answer: %w: %w", a.driver, method, storage.ErrTransient, ScrubURLError(err))
	}
	return nil
}

// RangeBody is the reader of the n bytes from off that a, the answer of
// method's ranged read, holds, or of fewer where the object ends sooner. A
// 206's body is the range itself; a 200's is the whole object, which
// RangeBody skips to off. Its read errors are NetworkError's, with no URL in
// their text, and its Close closes a.
func RangeBody(a *Answer, method string, off, n int64) (io.ReadCloser, error) {
	m := &media{r: a.Body, a: a, method: method}
	if a.StatusCode == http.StatusOK { // the whole object: skip to off
		switch _, err := io.CopyN(io.Discard, m, off); {
		case errors.Is(err, io.EOF): // the object ends before off
			n = 0
		case err != nil:
			a.Close()
			return nil, err
		}
	}
	m.r = io.LimitReader(a.Body, n)
	return m, nil
}

// media is an object's bytes as a ranged read's answer holds them.
type media struct {
	r      io.Reader
	a      *Answer
	method string
}

func (m *media) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = NetworkError(m.a.driver, m.method, err)
	}
	return n, err
}

func (m *media) Close() error {
	m.a.Close()
	return nil
}
