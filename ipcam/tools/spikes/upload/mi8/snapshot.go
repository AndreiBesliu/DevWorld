// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi8

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // G501: HTTP digest's MD5, as cameras offer it
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"image"
	_ "image/jpeg" // the cameras' snapshots
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrSnapshot is a snapshot that could not be fetched or decoded; it names no
// URI, address or credential.
var ErrSnapshot = errors.New("mi8: the camera's snapshot could not be read")

// maxSnapshot bounds a snapshot's size.
const maxSnapshot = 8 << 20

// SnapshotSource is the fallback reference (P41): the camera's ONVIF
// snapshots (GetSnapshotUri), at most 2 per second as the camera allows,
// each JPEG decoded in Go to 160 × 90 gray, in RAM only. The URI and the
// credentials are never logged.
type SnapshotSource struct {
	URI, User, Pass string
	// Client fetches the snapshots; nil: a client without a proxy that
	// follows no redirect, 10 s per snapshot.
	Client *http.Client
	// Interval is the time between two fetches' starts: 500 ms.
	Interval time.Duration
	Now      func() time.Time

	mu      sync.Mutex
	samples lagSamples
	auth    string // the WWW-Authenticate challenge last answered
	nc      int
}

// Lag is the reference's delay L: the p90 snapshot fetch time plus 0.5 s,
// capped at 3 s.
func (s *SnapshotSource) Lag() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.samples.lag()
}

func (s *SnapshotSource) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// client is the snapshots' HTTP client.
func (s *SnapshotSource) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Frames fetches the first snapshot, then one per Interval, until ctx ends.
// A snapshot that fails is skipped; the first must succeed.
func (s *SnapshotSource) Frames(ctx context.Context) (<-chan Frame, error) {
	interval := s.Interval
	if interval <= 0 {
		interval = frameInterval
	}
	client := s.client()
	first, err := s.fetch(ctx, client)
	if err != nil {
		return nil, err
	}
	out := make(chan Frame, 4)
	go func() {
		defer close(out)
		f := first
		for {
			if f.Gray != nil {
				select {
				case out <- f:
				case <-ctx.Done():
					return
				}
			}
			next := time.UnixMilli(f.AtMs).Add(interval)
			if wait := next.Sub(s.now()); wait > 0 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return
				}
			}
			start := s.now()
			var err error
			f, err = s.fetch(ctx, client)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				f = Frame{AtMs: start.UnixMilli()} // skipped
			}
		}
	}()
	return out, nil
}

// fetch fetches and decodes one snapshot, and records its fetch time.
func (s *SnapshotSource) fetch(ctx context.Context, client *http.Client) (Frame, error) {
	start := s.now()
	body, err := s.get(ctx, client)
	if err != nil {
		return Frame{}, err
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return Frame{}, ErrSnapshot
	}
	at := s.now()
	s.mu.Lock()
	s.samples.add(at.Sub(start))
	s.mu.Unlock()
	return Frame{AtMs: at.UnixMilli(), Gray: Gray160x90(img)}, nil
}

// get fetches the snapshot's bytes, answering an HTTP digest or basic
// challenge with the camera's credentials.
func (s *SnapshotSource) get(ctx context.Context, client *http.Client) ([]byte, error) {
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URI, nil)
		if err != nil {
			return nil, ErrSnapshot
		}
		req.Header.Set("User-Agent", UserAgent)
		s.mu.Lock()
		challenge := s.auth
		s.mu.Unlock()
		if challenge != "" {
			req.Header.Set("Authorization", s.authorization(challenge, req))
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, ErrSnapshot
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxSnapshot))
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			next := pickChallenge(resp.Header.Values("WWW-Authenticate"))
			if next == "" || (next == challenge && !strings.Contains(next, "stale=true")) {
				return nil, ErrSnapshot
			}
			s.mu.Lock()
			s.auth, s.nc = next, 0
			s.mu.Unlock()
			continue
		case resp.StatusCode != http.StatusOK || err != nil:
			return nil, ErrSnapshot
		}
		return body, nil
	}
	return nil, ErrSnapshot
}

// pickChallenge is the strongest challenge offered: digest SHA-256, digest
// MD5, then basic.
func pickChallenge(values []string) string {
	var md5c, basic string
	for _, v := range values {
		low := strings.ToLower(v)
		switch {
		case strings.HasPrefix(low, "digest") && strings.Contains(low, "sha-256"):
			return v
		case strings.HasPrefix(low, "digest"):
			md5c = v
		case strings.HasPrefix(low, "basic"):
			basic = v
		}
	}
	if md5c != "" {
		return md5c
	}
	return basic
}

var digestParam = regexp.MustCompile(`(\w+)=(?:"((?:[^"\\]|\\.)*)"|([^,\s]*))`)

// authorization answers challenge for req.
func (s *SnapshotSource) authorization(challenge string, req *http.Request) string {
	if strings.HasPrefix(strings.ToLower(challenge), "basic") {
		req.SetBasicAuth(s.User, s.Pass)
		return req.Header.Get("Authorization")
	}
	p := map[string]string{}
	for _, m := range digestParam.FindAllStringSubmatch(challenge[len("digest"):], -1) {
		v := m[2]
		if v == "" {
			v = m[3]
		}
		p[strings.ToLower(m[1])] = v
	}
	h := md5.New
	algo := "MD5"
	if strings.EqualFold(p["algorithm"], "SHA-256") {
		h, algo = func() hash.Hash { return sha256.New() }, "SHA-256"
	}
	hx := func(parts ...string) string {
		d := h()
		_, _ = io.WriteString(d, strings.Join(parts, ":"))
		return hex.EncodeToString(d.Sum(nil))
	}
	uri := req.URL.RequestURI()
	ha1 := hx(s.User, p["realm"], s.Pass)
	ha2 := hx(req.Method, uri)
	s.mu.Lock()
	s.nc++
	nc := fmt.Sprintf("%08x", s.nc)
	s.mu.Unlock()
	cb := make([]byte, 8)
	_, _ = rand.Read(cb)
	cnonce := hex.EncodeToString(cb)
	q := ""
	for qop := range strings.SplitSeq(p["qop"], ",") {
		if strings.TrimSpace(qop) == "auth" {
			q = "auth"
		}
	}
	var resp string
	if q == "auth" {
		resp = hx(ha1, p["nonce"], nc, cnonce, "auth", ha2)
	} else {
		resp = hx(ha1, p["nonce"], ha2)
	}
	out := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", algorithm=%s, response="%s"`,
		quote(s.User), quote(p["realm"]), quote(p["nonce"]), quote(uri), algo, resp)
	if p["opaque"] != "" {
		out += fmt.Sprintf(`, opaque="%s"`, quote(p["opaque"]))
	}
	if q == "auth" {
		out += fmt.Sprintf(`, qop=auth, nc=%s, cnonce="%s"`, nc, cnonce)
	}
	return out
}

// quote escapes a digest parameter's value.
func quote(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) }

// Gray160x90 scales img to 160 × 90 gray (BT.601 luma), each pixel the mean
// of its source area.
func Gray160x90(img image.Image) []byte {
	b := img.Bounds()
	out := make([]byte, FrameSize)
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return out
	}
	for y := range FrameHeight {
		y0, y1 := b.Min.Y+y*h/FrameHeight, b.Min.Y+(y+1)*h/FrameHeight
		y1 = max(y1, y0+1)
		for x := range FrameWidth {
			x0, x1 := b.Min.X+x*w/FrameWidth, b.Min.X+(x+1)*w/FrameWidth
			x1 = max(x1, x0+1)
			// At most 4 × 4 samples per pixel keep a 4K snapshot cheap.
			sx, sy := max((x1-x0)/4, 1), max((y1-y0)/4, 1)
			var sum, n uint64
			for yy := y0; yy < y1; yy += sy {
				for xx := x0; xx < x1; xx += sx {
					r, g, bl, _ := img.At(xx, yy).RGBA()
					sum += (19595*uint64(r) + 38470*uint64(g) + 7471*uint64(bl) + 1<<15) >> 24
					n++
				}
			}
			out[y*FrameWidth+x] = byte(sum / n) //nolint:gosec // a mean of bytes
		}
	}
	return out
}
