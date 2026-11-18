package recorder

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// The segment-closed hook (G05). MediaMTX runs "ipcam-hub hook
// segment-complete" when a segment is complete (MTX-3); that command posts
// the segment to the recorder's endpoint on 127.0.0.1, whose address and
// token MediaMTX's environment carries. Each MediaMTX child gets a token of
// its own.
const (
	HookAddrEnv  = "IPCAM_HOOK_ADDR"  // host:port of the endpoint
	HookTokenEnv = "IPCAM_HOOK_TOKEN" //nolint:gosec // the variable's name; its bearer token is new for every child
	HookPath     = "/hook/segment-complete"
)

// HookRequest is the body of a hook post.
type HookRequest struct {
	Path       string `json:"path"` // the final path, MTX_SEGMENT_PATH
	StartMs    int64  `json:"startMs"`
	DurationMs int64  `json:"durationMs"`
	Cam        string `json:"cam"`
	Profile    string `json:"profile"`
	Encrypted  bool   `json:"encrypted"`
}

// maxHookBody bounds a hook post.
const maxHookBody = 64 << 10

// PostSegmentComplete is the hook command: it reads the segment from the
// environment that MediaMTX gives it, MTX_PATH (<cam>/<profile>),
// MTX_SEGMENT_PATH, MTX_SEGMENT_START (Unix ms) and MTX_SEGMENT_DURATION (s),
// and posts it with the token to the address in getenv.
func PostSegmentComplete(ctx context.Context, getenv func(string) string) error {
	addr, token := getenv(HookAddrEnv), getenv(HookTokenEnv)
	if addr == "" || token == "" {
		return fmt.Errorf("hook: %s or %s is not set", HookAddrEnv, HookTokenEnv)
	}
	cam, profile, ok := strings.Cut(getenv("MTX_PATH"), "/")
	if !ok || cam == "" || profile == "" {
		return fmt.Errorf("hook: MTX_PATH %q is not <cam>/<profile>", getenv("MTX_PATH"))
	}
	path := getenv("MTX_SEGMENT_PATH")
	if path == "" {
		return errors.New("hook: MTX_SEGMENT_PATH is not set")
	}
	start, err := strconv.ParseInt(getenv("MTX_SEGMENT_START"), 10, 64)
	if err != nil {
		return fmt.Errorf("hook: MTX_SEGMENT_START: %w", err)
	}
	seconds, err := strconv.ParseFloat(getenv("MTX_SEGMENT_DURATION"), 64)
	if err != nil || seconds < 0 || math.IsInf(seconds, 0) {
		return fmt.Errorf("hook: MTX_SEGMENT_DURATION %q is not a duration", getenv("MTX_SEGMENT_DURATION"))
	}
	body, err := json.Marshal(HookRequest{Path: path, StartMs: start, DurationMs: int64(math.Round(seconds * 1000)),
		Cam: cam, Profile: profile, Encrypted: strings.HasSuffix(path, suffixMP4+suffixAge)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+HookPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	// A loopback endpoint: no proxy, whatever the environment says.
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("hook: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("hook: HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// hookServer is the recorder's endpoint.
type hookServer struct {
	ln     net.Listener
	srv    *http.Server
	token  atomic.Pointer[string] // the running child's; nil before the first
	handle func(ctx context.Context, req HookRequest) (status int, err error)
}

// startHookServer listens on 127.0.0.1 at a random port. It accepts no
// token until newChild gives one out.
func startHookServer(handle func(ctx context.Context, req HookRequest) (int, error)) (*hookServer, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("recorder: the hook endpoint: %w", err)
	}
	h := &hookServer{ln: ln, handle: handle}
	mux := http.NewServeMux()
	mux.HandleFunc(HookPath, h.serve)
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second}
	go func() { _ = h.srv.Serve(ln) }()
	return h, nil
}

// newChild makes a new random token, from now on the only one accepted: a
// hook with an earlier child's token gets HTTP 401. It returns what the new
// child's environment needs for the hook command.
func (h *hookServer) newChild() []string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	token := hex.EncodeToString(b)
	h.token.Store(&token)
	return []string{HookAddrEnv + "=" + h.ln.Addr().String(), HookTokenEnv + "=" + token}
}

func (h *hookServer) shutdown(ctx context.Context) { _ = h.srv.Shutdown(ctx) }

func (h *hookServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	want := h.token.Load()
	if !ok || want == nil || subtle.ConstantTimeCompare([]byte(got), []byte(*want)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxHookBody))
	dec.DisallowUnknownFields()
	var req HookRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "the body is not a hook request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if status, err := h.handle(r.Context(), req); err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
