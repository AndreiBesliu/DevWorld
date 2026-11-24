package events

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: HTTP digest's MD5 is the camera's choice (RFC 7616)
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// The login methods, in the order the client tries them (§8.2): the
// WS-UsernameToken digest, HTTP digest (SHA-256, else MD5), HTTP basic.
// AuthNone is a camera that asked for no login.
const (
	AuthWSSEDigest = "wsse-digest"
	AuthHTTPDigest = "http-digest"
	AuthBasic      = "basic"
	AuthNone       = "none"
)

// authOrder is the order of the login methods.
var authOrder = []string{AuthWSSEDigest, AuthHTTPDigest, AuthBasic}

// maxAuthFailures is how many refused logins start the pause (§8.2).
const maxAuthFailures = 3

// client sends a camera's SOAP requests and keeps what its logins learned:
// the method that worked, the camera's clock offset, its HTTP challenges,
// and the refusals that pause it.
type client struct {
	cam  CameraRef
	opts Options

	mu           sync.Mutex
	method       string        // the method that last worked, "" before
	offset       time.Duration // the camera's clock minus the hub's
	failures     int           // refused logins since the last success or pause
	pausedUntil  time.Time
	digest       *digestChallenge
	basicOffered bool
}

// newClient is a client of cam with opts' defaults.
func newClient(cam CameraRef, opts Options) *client {
	return &client{cam: cam, opts: opts.withDefaults()}
}

// now is the hub's time.
func (c *client) now() time.Time { return c.opts.Clock() }

// methods is the order of the methods to try: the one that worked first,
// then those after it.
func (c *client) methods() []string {
	i := slices.Index(authOrder, c.method)
	if i < 0 {
		return authOrder
	}
	return authOrder[i:]
}

// authMethod is the method that worked last, "" before.
func (c *client) authMethod() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.method
}

// clockOffset is the camera's clock minus the hub's.
func (c *client) clockOffset() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offset
}

// anon sends a request without a login.
func (c *client) anon(ctx context.Context, endpoint, action, body string, out any) (soapResult, error) {
	return post(ctx, c.opts.HTTP, endpoint, action, envelope("", body), nil, out)
}

// call sends a request with a login, header being the SOAP header's other
// elements, and decodes the response's body into out. It tries the methods
// in turn; each refusal counts, and the third starts a pause of
// Options.AuthPause during which no request is sent (ErrAuthPaused).
func (c *client) call(ctx context.Context, endpoint, action, header, body string, out any) error {
	c.mu.Lock()
	if c.now().Before(c.pausedUntil) {
		c.mu.Unlock()
		return ErrAuthPaused
	}
	methods := c.methods()
	c.mu.Unlock()
	for _, m := range methods {
		res, tried, err := c.attempt(ctx, m, endpoint, action, header, body, out)
		if err != nil {
			return err
		}
		if !tried {
			continue
		}
		if !res.refused() {
			c.mu.Lock()
			c.method, c.failures = m, 0
			c.mu.Unlock()
			return res.err
		}
		if c.refusal(res.header) {
			return ErrAuth
		}
	}
	return ErrAuth
}

// refusal counts a refused login and notes the challenges that came with it;
// true when it started the pause.
func (c *client) refusal(h http.Header) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.noteChallenges(h)
	c.failures++
	if c.failures < maxAuthFailures {
		return false
	}
	c.failures = 0
	c.pausedUntil = c.now().Add(c.opts.AuthPause)
	return true
}

// noteChallenges keeps the HTTP challenges of a 401's header. c.mu is held.
func (c *client) noteChallenges(h http.Header) {
	if ch := parseDigestChallenges(h.Values("WWW-Authenticate")); ch != nil {
		c.digest = ch
	}
	for _, v := range h.Values("WWW-Authenticate") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "basic") {
			c.basicOffered = true
		}
	}
}

// attempt sends the request with method m. tried is false when m cannot be
// tried: no HTTP challenge offers it.
func (c *client) attempt(ctx context.Context, m, endpoint, action, header, body string, out any) (res soapResult, tried bool, err error) {
	switch m {
	case AuthWSSEDigest:
		c.mu.Lock()
		sec := wsseHeader(c.cam.User, c.cam.Pass, c.now(), c.offset)
		c.mu.Unlock()
		res, err = post(ctx, c.opts.HTTP, endpoint, action, envelope(sec+header, body), nil, out)
		return res, true, err
	case AuthHTTPDigest:
		return c.attemptDigest(ctx, endpoint, action, header, body, out)
	case AuthBasic:
		c.mu.Lock()
		offered := c.basicOffered
		c.mu.Unlock()
		if !offered {
			return soapResult{}, false, nil
		}
		res, err = post(ctx, c.opts.HTTP, endpoint, action, envelope(header, body), func(r *http.Request) {
			r.SetBasicAuth(c.cam.User, c.cam.Pass)
		}, out)
		return res, true, err
	}
	return soapResult{}, false, nil
}

// attemptDigest sends the request with HTTP digest. Without a challenge it
// first sends the request without a login to get one. A refusal of a
// challenge already used, with a new challenge, is a stale nonce: it is
// tried again once with the new one, and only that refusal counts.
func (c *client) attemptDigest(ctx context.Context, endpoint, action, header, body string, out any) (soapResult, bool, error) {
	for try := 0; ; try++ {
		c.mu.Lock()
		ch := c.digest
		c.mu.Unlock()
		if ch == nil {
			res, err := post(ctx, c.opts.HTTP, endpoint, action, envelope(header, body), nil, out)
			if err != nil {
				return res, false, err
			}
			if !res.refused() {
				c.mu.Lock()
				c.method = AuthNone
				c.mu.Unlock()
				return res, true, nil
			}
			c.mu.Lock()
			c.noteChallenges(res.header)
			ch = c.digest
			c.mu.Unlock()
			if ch == nil {
				return soapResult{}, false, nil
			}
		}
		used := ch.used()
		res, err := post(ctx, c.opts.HTTP, endpoint, action, envelope(header, body), func(r *http.Request) {
			r.Header.Set("Authorization", ch.authorization(c.cam.User, c.cam.Pass, r.Method, r.URL.RequestURI()))
		}, out)
		if err != nil || !res.refused() || try > 0 || !used {
			return res, true, err
		}
		fresh := parseDigestChallenges(res.header.Values("WWW-Authenticate"))
		if fresh == nil {
			return res, true, nil
		}
		c.mu.Lock()
		c.digest = fresh
		c.mu.Unlock()
	}
}

// digestChallenge is an HTTP digest challenge (RFC 7616) and the count of
// the requests that used its nonce.
type digestChallenge struct {
	realm, nonce, opaque, algorithm string
	qopAuth                         bool

	mu sync.Mutex
	nc int
}

// parseDigestChallenges picks among the WWW-Authenticate values the digest
// challenge to answer: SHA-256 when offered, else MD5; nil when none.
func parseDigestChallenges(values []string) *digestChallenge {
	var best *digestChallenge
	for _, v := range values {
		scheme, rest, _ := strings.Cut(strings.TrimSpace(v), " ")
		if !strings.EqualFold(scheme, "Digest") {
			continue
		}
		params := parseAuthParams(rest)
		alg := strings.ToUpper(params["algorithm"])
		if alg == "" {
			alg = "MD5"
		}
		if alg != "MD5" && alg != "SHA-256" {
			continue
		}
		ch := &digestChallenge{realm: params["realm"], nonce: params["nonce"], opaque: params["opaque"], algorithm: alg}
		for q := range strings.SplitSeq(params["qop"], ",") {
			if strings.TrimSpace(q) == "auth" {
				ch.qopAuth = true
			}
		}
		if ch.nonce == "" {
			continue
		}
		if best == nil || (best.algorithm == "MD5" && alg == "SHA-256") {
			best = ch
		}
	}
	return best
}

// parseAuthParams parses a challenge's comma-separated k=v and k="v" pairs.
func parseAuthParams(s string) map[string]string {
	params := map[string]string{}
	for {
		s = strings.TrimLeft(s, " \t,")
		key, rest, ok := strings.Cut(s, "=")
		if !ok {
			return params
		}
		key = strings.ToLower(strings.TrimSpace(key))
		rest = strings.TrimLeft(rest, " \t")
		var val string
		if strings.HasPrefix(rest, `"`) {
			var b strings.Builder
			i := 1
			for ; i < len(rest) && rest[i] != '"'; i++ {
				if rest[i] == '\\' && i+1 < len(rest) {
					i++
				}
				b.WriteByte(rest[i])
			}
			val, s = b.String(), rest[min(i+1, len(rest)):]
		} else {
			val, s, _ = strings.Cut(rest, ",")
			val = strings.TrimSpace(val)
		}
		params[key] = val
	}
}

// used reports whether a request already answered the challenge.
func (ch *digestChallenge) used() bool {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.nc > 0
}

// hash is the challenge's algorithm.
func (ch *digestChallenge) hash() hash.Hash {
	if ch.algorithm == "SHA-256" {
		return sha256.New()
	}
	return md5.New() //nolint:gosec // G401: see the import
}

// h is the hex digest of the parts joined by ':'.
func (ch *digestChallenge) h(parts ...string) string {
	hh := ch.hash()
	hh.Write([]byte(strings.Join(parts, ":")))
	return hex.EncodeToString(hh.Sum(nil))
}

// authorization is the Authorization header that answers the challenge for
// a request of method to uri.
func (ch *digestChallenge) authorization(user, pass, method, uri string) string {
	ch.mu.Lock()
	ch.nc++
	nc := fmt.Sprintf("%08x", ch.nc)
	ch.mu.Unlock()
	cnonceRaw := make([]byte, 12)
	_, _ = rand.Read(cnonceRaw)
	cnonce := hex.EncodeToString(cnonceRaw)
	ha1 := ch.h(user, ch.realm, pass)
	ha2 := ch.h(method, uri)
	q := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	var b strings.Builder
	b.WriteString("Digest username=" + q(user) + ", realm=" + q(ch.realm) + ", nonce=" + q(ch.nonce) +
		", uri=" + q(uri) + ", algorithm=" + ch.algorithm)
	if ch.qopAuth {
		b.WriteString(", response=" + q(ch.h(ha1, ch.nonce, nc, cnonce, "auth", ha2)) +
			", qop=auth, nc=" + nc + ", cnonce=" + q(cnonce))
	} else {
		b.WriteString(", response=" + q(ch.h(ha1, ch.nonce, ha2)))
	}
	if ch.opaque != "" {
		b.WriteString(", opaque=" + q(ch.opaque))
	}
	return b.String()
}
