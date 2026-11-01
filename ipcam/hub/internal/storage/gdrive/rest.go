package gdrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

const (
	// metadataTimeout is the deadline of each metadata call, and
	// chunkTimeout that of each chunk PUT and media read (P26): with
	// ipcam-hub's dial, TLS and response-header timeouts, a route that
	// drops ends a call as ErrTransient instead of holding a worker.
	metadataTimeout = 30 * time.Second
	chunkTimeout    = 10 * time.Minute
	// maxAnswer bounds the JSON answer that the driver reads.
	maxAnswer = 1 << 20
	// maxDrain is the most of an answer's rest that Close reads, so that the
	// connection can serve the next request.
	maxDrain = 64 << 10
)

// call is one request of the API.
type call struct {
	method string // the API method: the call log's name, and the errors'
	verb   string // the HTTP method
	url    string
	header map[string]string
	body   io.Reader
	length int64 // the body's length
	// create takes a slot of the create bucket before anything else
	// (STO-07).
	create bool
	// session marks a request to a session URI, where 404 and 410 mean the
	// session is gone.
	session bool
	timeout time.Duration      // metadataTimeout when 0
	calls   storage.CallCounts // the counts of the upload that the call serves, or nil
}

// answer is a response that send accepted. Close releases it.
type answer struct {
	*http.Response
	cancel context.CancelFunc
}

// Close reads a little of the body's rest, closes it and ends the request's
// deadline.
func (a *answer) Close() {
	_, _ = io.Copy(io.Discard, io.LimitReader(a.Body, maxDrain))
	_ = a.Body.Close()
	a.cancel()
}

// decode reads the answer's JSON into v and closes the answer.
func (a *answer) decode(method string, v any) error {
	defer a.Close()
	if err := json.NewDecoder(io.LimitReader(a.Body, maxAnswer)).Decode(v); err != nil {
		return fmt.Errorf("gdrive: %s: the answer: %w: %w", method, storage.ErrTransient, scrub(err))
	}
	return nil
}

// send makes c's request on the plain client with the bearer token that
// Tokens gives now (P27), and returns the answer when its status is one of
// want. A create first takes a slot of the bucket and, without one, returns
// ErrRateLimited before any call. Any other status is an error as
// statusError maps it, and a request that gets no answer is ErrTransient;
// neither text quotes the request (g2-03). Every request sent is counted.
func (d *Drive) send(ctx context.Context, c call, want ...int) (*answer, error) {
	if c.create {
		if wait, ok := d.bucket.take(d.clock()); !ok {
			return nil, rateLimited(c.method, wait)
		}
	}
	tok, err := d.tokens.Token()
	if err != nil {
		return nil, tokenError(c.method, err)
	}
	timeout := c.timeout
	if timeout == 0 {
		timeout = metadataTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(ctx, c.verb, c.url, c.body)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gdrive: %s: %w: %w", c.method, storage.ErrPermanent, scrub(err))
	}
	if c.body != nil {
		req.ContentLength = c.length
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	d.calls.add(c.method, c.calls)
	resp, err := d.client.Do(req)
	if err != nil {
		cancel()
		return nil, networkError(c.method, err)
	}
	a := &answer{Response: resp, cancel: cancel}
	if !slices.Contains(want, resp.StatusCode) {
		err := statusError(&c, resp, d.clock())
		a.Close()
		return nil, err
	}
	return a, nil
}

// createBucket holds the driver's creates to createsPerSec in any second
// (STO-07, ≈ 2 creates/s): it keeps the times of the last createsPerSec
// creates, and the next may go once the oldest of them is a second old.
type createBucket struct {
	mu    sync.Mutex
	taken []time.Time // oldest first
}

// take takes a slot at now and returns true, or returns the wait until the
// next slot and false.
func (b *createBucket) take(now time.Time) (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, t := range b.taken {
		if t.After(now) { // the clock went back
			b.taken[i] = now
		}
	}
	if len(b.taken) == createsPerSec {
		if wait := b.taken[0].Add(time.Second).Sub(now); wait > 0 {
			return wait, false
		}
		b.taken = b.taken[1:]
	}
	b.taken = append(b.taken, now)
	return 0, true
}
