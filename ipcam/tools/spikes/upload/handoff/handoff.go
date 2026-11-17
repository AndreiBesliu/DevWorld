// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package handoff is uploadspike's HTTP server on port 7091 (P40), served
// only by the process that holds the device's hub.lock: the login handoff
// that the video app calls (next, begin, complete), the device-flow page,
// the web reader page and its visibility endpoints (P21), the setup page,
// the status page, the handover endpoint, and the app's own camera and
// results endpoints (C2-R7 (b)). It listens on 127.0.0.1:7091 (and
// [::1]:7091) for as long as its process holds the lock, and on each of
// the host's RFC 1918 IPv4 addresses only while a login of another device
// is open (OpenLAN), never on the wildcard. Plain HTTP; every answer sets
// Referrer-Policy: no-referrer; nothing is logged, a query string least of
// all.
package handoff

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
)

// HandoffPort is uploadspike's port.
const HandoffPort = 7091

// The bounds of the server.
const (
	lanLife        = 10 * time.Minute // a login's state lives 10 min (T11)
	bindRetry      = 10 * time.Second // a handover's previous holder may still hold the port
	maxSmallBody   = 16 << 10
	readHeaderWait = 10 * time.Second
	hubCallTimeout = 40 * time.Second
	lanDrain       = 5 * time.Second
)

// shellOrigins are the video app's shells (spikehub's withCORS): the
// Android WebView (androidScheme http) and the Tauri WebView2.
var shellOrigins = []string{"http://localhost", "http://tauri.localhost", "https://tauri.localhost"}

// Next is the login that uploadspike waits for on a platform.
type Next struct {
	Dest     string `json:"dest"`
	Provider string `json:"provider"`
	Plan     string `json:"plan"`
}

// Driver is the holder's side of the server: the logins it waits for, the
// handoff cap, the visibility cells, the handover, and the pages, which it
// renders from its Go constants.
type Driver interface {
	// Next is the login awaited on platform, if any.
	Next(platform string) (Next, bool)
	// AllowBegin refuses a login past the handoff cap, with the owner's
	// Romanian text; nil allows it.
	AllowBegin(dest, platform, plan string) error
	// Begun tells the driver that a login began.
	Begun(dest string, res hubproc.BeginResult)
	// WebCell records a cell of the web reader; WebClientID is the web
	// reader's client ID, which a reader page on another device reads.
	WebCell(creator string, listed, read bool) error
	WebClientID() string
	// HandoverToken is the run state's; Handover is the holder's answer to
	// a request that carries it: 204, or 409 while an MI5 run is open.
	HandoverToken() string
	Handover() int
	// The pages: the device flow's code, the web reader (also the F6
	// reader's redirect), OneDrive's web callback's result, the setup
	// (GET and POST) and the status (the page, or its JSON).
	DevicePage(w http.ResponseWriter, res hubproc.BeginResult)
	ReaderPage(w http.ResponseWriter, r *http.Request)
	CallbackPage(w http.ResponseWriter, ok bool)
	Setup(w http.ResponseWriter, r *http.Request)
	Status(w http.ResponseWriter, r *http.Request)
}

// Options configure a Server.
type Options struct {
	Clock func() time.Time
	// RemoteHub is `mi6 reader --hub`'s Pi address; "" otherwise.
	RemoteHub string
	// Port is 0 for HandoffPort; tests pass a free port.
	Port int
	// ResultsDir is the results root: the app's results go to its app/.
	ResultsDir string
	// Driver is the holder's side; nil serves no login, page or handover.
	Driver Driver
	// OneDrive serves OneDrive's web callback (only if A5 passed).
	OneDrive bool
	// LANLife bounds a LAN session; 0 is 10 min. Tests shorten it.
	LANLife time.Duration
}

// Server is the handoff server.
type Server struct {
	hub  *hubproc.ControlClient
	opts Options
	port int
	app  *appResults

	mu    sync.Mutex
	begun map[string]begun // by state
	loop  []*http.Server
	lan   *lanSession
}

// begun is a login that began through the server.
type begun struct {
	dest string
	res  hubproc.BeginResult
}

// lanSession is the LAN listener of a remote login or reader session.
type lanSession struct {
	key   string
	srvs  []*http.Server
	timer *time.Timer
}

// lanAddrs lists the host's RFC 1918 IPv4 addresses; tests replace it.
var lanAddrs = RFC1918Addrs

// New is the server of hub's control endpoint (nil: none, as under `mi6
// reader --hub`).
func New(hub *hubproc.ControlClient, opts Options) *Server {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	port := opts.Port
	if port == 0 {
		port = HandoffPort
	}
	s := &Server{hub: hub, opts: opts, port: port, begun: map[string]begun{}}
	if opts.ResultsDir != "" {
		s.app = newAppResults(opts.ResultsDir, opts.Clock)
	}
	return s
}

// Port is the server's port.
func (s *Server) Port() int { return s.port }

// SetHub points the server at a restarted hub's control endpoint.
func (s *Server) SetHub(hub *hubproc.ControlClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hub = hub
}

// control is the current hub's control client, nil without one.
func (s *Server) control() *hubproc.ControlClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hub
}

// ServeLoopback listens on 127.0.0.1:<port>, and [::1]:<port> when the
// host has IPv6 loopback, retrying the bind for 10 s while the port is in
// use (a handover's previous holder may still hold it), and serves there
// until ctx ends or Close. It returns once it listens.
func (s *Server) ServeLoopback(ctx context.Context) error {
	ln4, err := listenRetry(ctx, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port)))
	if err != nil {
		return err
	}
	listeners := []net.Listener{ln4}
	if ln6, err := listenRetry(ctx, "tcp6", net.JoinHostPort("::1", strconv.Itoa(s.port))); err == nil {
		listeners = append(listeners, ln6)
	}
	handler := s.handler(true)
	s.mu.Lock()
	for _, ln := range listeners {
		srv := newHTTPServer(handler)
		s.loop = append(s.loop, srv)
		go func() { _ = srv.Serve(ln) }()
	}
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.Close()
	}()
	return nil
}

// listenRetry listens at addr, retrying for bindRetry while it is in use;
// a host without the family fails at once.
func listenRetry(ctx context.Context, network, addr string) (net.Listener, error) {
	deadline := time.Now().Add(bindRetry)
	for {
		var lc net.ListenConfig
		ln, err := lc.Listen(ctx, network, addr)
		if err == nil || !errors.Is(err, syscall.EADDRINUSE) && !isAddrInUse(err) || time.Now().After(deadline) {
			return ln, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// isAddrInUse reports Windows' WSAEADDRINUSE too.
func isAddrInUse(err error) bool {
	return strings.Contains(err.Error(), "address already in use") || strings.Contains(err.Error(), "Only one usage of each socket address")
}

// newHTTPServer serves h; it logs nothing, so no request line reaches a
// log.
func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{Handler: h, ReadHeaderTimeout: readHeaderWait, ErrorLog: log.New(io.Discard, "", 0)}
}

// Close stops every listener, the LAN one included.
func (s *Server) Close() {
	s.mu.Lock()
	loop := s.loop
	s.loop = nil
	s.mu.Unlock()
	for _, srv := range loop {
		_ = srv.Close()
	}
	s.CloseLAN()
}

// OpenLAN listens on <lan-ip>:<port> on each RFC 1918 IPv4 address of the
// host, never the wildcard, for a login or a reader session of another
// device, named key, until a login completes through the server, CloseLAN,
// LANLife (10 min, a login state's life) or ctx's end. An open session is
// replaced. A host without such an address is an error.
func (s *Server) OpenLAN(ctx context.Context, key string) error {
	addrs, err := lanAddrs()
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return errors.New("handoff: the host has no RFC 1918 IPv4 address for the other device to reach")
	}
	s.CloseLAN()
	sess := &lanSession{key: key}
	handler := s.handler(false)
	for _, ip := range addrs {
		ln, err := listenRetry(ctx, "tcp4", net.JoinHostPort(ip.String(), strconv.Itoa(s.port)))
		if err != nil {
			for _, srv := range sess.srvs {
				_ = srv.Close()
			}
			return err
		}
		srv := newHTTPServer(handler)
		sess.srvs = append(sess.srvs, srv)
		go func() { _ = srv.Serve(ln) }()
	}
	life := s.opts.LANLife
	if life <= 0 {
		life = lanLife
	}
	sess.timer = time.AfterFunc(life, func() { s.closeSession(sess) })
	go func() {
		<-ctx.Done()
		s.closeSession(sess)
	}()
	s.mu.Lock()
	s.lan = sess
	s.mu.Unlock()
	return nil
}

// LANOpen reports whether a LAN session is open, and its key.
func (s *Server) LANOpen() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lan == nil {
		return "", false
	}
	return s.lan.key, true
}

// CloseLAN closes the LAN session, if one is open.
func (s *Server) CloseLAN() {
	s.mu.Lock()
	sess := s.lan
	s.mu.Unlock()
	if sess != nil {
		s.closeSession(sess)
	}
}

// closeSession closes sess, once: its listeners at once, its connections
// once their answers are out, within lanDrain.
func (s *Server) closeSession(sess *lanSession) {
	s.mu.Lock()
	if s.lan != sess {
		s.mu.Unlock()
		return
	}
	s.lan = nil
	s.mu.Unlock()
	sess.timer.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), lanDrain)
	defer cancel()
	for _, srv := range sess.srvs {
		if srv.Shutdown(ctx) != nil {
			_ = srv.Close()
		}
	}
}

// RFC1918Addrs lists the host's IPv4 addresses in 10/8, 172.16/12 and
// 192.168/16.
func RFC1918Addrs() ([]net.IP, error) {
	ifaces, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var out []net.IP
	for _, a := range ifaces {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ip4 := n.IP.To4(); ip4 != nil && ip4.IsPrivate() && !slices.ContainsFunc(out, ip4.Equal) {
			out = append(out, ip4)
		}
	}
	return out, nil
}

// handler is the server's routes: on the loopback listener all of them, on
// the LAN one only what the other device needs (the login handoff, the
// device page, OneDrive's callback, the app's endpoints and the
// visibility endpoints); the rest answers 404 there.
func (s *Server) handler(loopback bool) http.Handler {
	mux := http.NewServeMux()
	shell := func(h http.HandlerFunc) http.Handler { return s.cors(shellOrigins, h) }
	reader := func(h http.HandlerFunc) http.Handler { return s.cors([]string{s.readerOrigin()}, h) }
	mux.Handle("/spike/v1/oauth/next", shell(s.next))
	mux.Handle("/spike/v1/oauth/begin", shell(s.beginLogin))
	mux.Handle("/spike/v1/oauth/complete", shell(s.complete))
	mux.Handle("/spike/v1/cameras", shell(s.cameras))
	mux.Handle("/spike/v1/results", shell(s.results))
	mux.Handle("/spike/v1/visibility/targets", reader(s.targets))
	mux.Handle("/spike/v1/visibility/web", reader(s.webCell))
	mux.Handle("/spike/v1/visibility/config", reader(s.readerConfig))
	mux.HandleFunc("GET /oauth/device", s.devicePage)
	if s.opts.OneDrive {
		mux.HandleFunc("GET /oauth/callback", s.callback)
	}
	if loopback {
		mux.HandleFunc("GET /reader", s.readerPage)
		if s.opts.OneDrive {
			mux.HandleFunc("GET /oauth/f6", s.readerPage)
		}
		mux.HandleFunc("/setup", s.setup)
		mux.HandleFunc("GET /status", s.status)
		mux.HandleFunc("GET /spike/v1/status", s.status)
		mux.HandleFunc("POST /spike/v1/run/handover", s.handover)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

// readerOrigin is the reader page's origin, the web client's registered
// one: http://localhost:7091.
func (s *Server) readerOrigin() string { return "http://localhost:" + strconv.Itoa(s.port) }

// cors lets the allowed origins call h and read its answers, as spikehub's
// withCORS: their preflight gets 204, with private network access when it
// asks for it; a request of another origin gets no CORS header and 403, a
// GET too, since a begin starts a login. A request without Origin, or of
// the server's own origin, goes on.
func (s *Server) cors(allowed []string, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		switch {
		case origin == "":
		case slices.Contains(allowed, origin):
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.Header().Set("Access-Control-Max-Age", "600")
				if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
					w.Header().Set("Access-Control-Allow-Private-Network", "true")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
		case origin == "http://"+r.Host && r.Method != http.MethodOptions:
		default:
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin not allowed"})
			return
		}
		h(w, r)
	})
}

// writeJSON answers v with status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// noLogin answers a request that needs the hub or the driver when the
// server has none.
func noLogin(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no login is served here"})
}

// next answers the login awaited on ?platform=.
func (s *Server) next(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	if s.opts.Driver == nil {
		noLogin(w)
		return
	}
	n, ok := s.opts.Driver.Next(r.URL.Query().Get("platform"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no login is awaited on this platform"})
		return
	}
	writeJSON(w, http.StatusOK, n)
}

// beginLogin begins the awaited login of ?dest=&provider=&platform=&port=
// on the hub and answers the hub's BeginResult.
func (s *Server) beginLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	hub := s.control()
	if s.opts.Driver == nil || hub == nil {
		noLogin(w)
		return
	}
	q := r.URL.Query()
	dest, provider, platform := q.Get("dest"), q.Get("provider"), q.Get("platform")
	n, ok := s.opts.Driver.Next(platform)
	if !ok || n.Dest != dest || n.Provider != provider {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "this login is not the one awaited"})
		return
	}
	port := 0
	if p := q.Get("port"); p != "" {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || v > 65535 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "port is not a port"})
			return
		}
		port = v
	}
	if err := s.opts.Driver.AllowBegin(dest, platform, n.Plan); err != nil {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), hubCallTimeout)
	defer cancel()
	res, err := hub.Begin(ctx, hubproc.BeginRequest{DestID: dest, Provider: provider, Platform: platform, LoopbackPort: port, Plan: n.Plan})
	if err != nil {
		writeHubFailure(w, err)
		return
	}
	s.mu.Lock()
	s.begun[res.State] = begun{dest: dest, res: res}
	s.mu.Unlock()
	s.opts.Driver.Begun(dest, res)
	writeJSON(w, http.StatusOK, res)
}

// writeHubFailure answers a failed control call by its reason only.
func writeHubFailure(w http.ResponseWriter, err error) {
	var f *hubproc.FailureError
	if errors.As(err, &f) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the hub refused", "reason": f.Reason})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the hub did not answer"})
}

// complete forwards a plan A login's code to the hub: the body must be a
// JSON object with exactly state and code, both set (the app sends it as
// text/plain, as results.ts does, so it needs no preflight). A login that
// completes ends the LAN session.
func (s *Server) complete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	hub := s.control()
	if hub == nil {
		noLogin(w)
		return
	}
	var req struct {
		State string `json:"state"`
		Code  string `json:"code"`
	}
	if !decodeExact(w, r, &req) || req.State == "" || req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body must hold exactly state and code"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), hubCallTimeout)
	defer cancel()
	done, err := hub.Complete(ctx, req.State, req.Code)
	if err != nil {
		writeHubFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, done)
	// The session ends once this answer is out: its listener closes at
	// once, and its connections after their answers (closeSession).
	go s.CloseLAN()
}

// decodeExact decodes r's body, at most maxSmallBody bytes, into v,
// refusing unknown fields and anything after the object.
func decodeExact(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSmallBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return false
	}
	var extra json.RawMessage
	return errors.Is(dec.Decode(&extra), io.EOF)
}

// devicePage shows a device login's verification URL and user code.
func (s *Server) devicePage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	b, ok := s.begun[r.URL.Query().Get("state")]
	s.mu.Unlock()
	if !ok || b.res.UserCode == "" || s.opts.Driver == nil {
		http.NotFound(w, r)
		return
	}
	s.opts.Driver.DevicePage(w, b.res)
}

// callback is OneDrive's web redirect (only if A5 passed): it forwards the
// code and state to the hub and shows the result.
func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	hub := s.control()
	if hub == nil || s.opts.Driver == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	ok := false
	if q.Get("state") != "" && q.Get("code") != "" {
		ctx, cancel := context.WithTimeout(r.Context(), hubCallTimeout)
		defer cancel()
		_, err := hub.Complete(ctx, q.Get("state"), q.Get("code"))
		ok = err == nil
	}
	s.opts.Driver.CallbackPage(w, ok)
}

// readerPage serves the web reader (loopback only).
func (s *Server) readerPage(w http.ResponseWriter, r *http.Request) {
	if s.opts.Driver == nil {
		http.NotFound(w, r)
		return
	}
	s.opts.Driver.ReaderPage(w, r)
}

// targets lists the creators' folders for the web reader.
func (s *Server) targets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	hub := s.control()
	if hub == nil {
		noLogin(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), hubCallTimeout)
	defer cancel()
	t, err := hub.Targets(ctx)
	if err != nil {
		writeHubFailure(w, err)
		return
	}
	if t == nil {
		t = []hubproc.Target{}
	}
	writeJSON(w, http.StatusOK, t)
}

// readerConfig answers the web reader's client ID (no secret), for a
// reader page that `mi6 reader --hub` serves on another device.
func (s *Server) readerConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || s.opts.Driver == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "none"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"webClientId": s.opts.Driver.WebClientID()})
}

// webCell records a web reader's cell: exactly creator, listed and read.
func (s *Server) webCell(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	if s.opts.Driver == nil {
		noLogin(w)
		return
	}
	var cell struct {
		Creator string `json:"creator"`
		Listed  *bool  `json:"listed"`
		Read    *bool  `json:"read"`
	}
	if !decodeExact(w, r, &cell) || cell.Listed == nil || cell.Read == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body must hold exactly creator, listed and read"})
		return
	}
	if err := s.opts.Driver.WebCell(cell.Creator, *cell.Listed, *cell.Read); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setup serves the setup page (loopback only).
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if s.opts.Driver == nil || r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	s.opts.Driver.Setup(w, r)
}

// status serves the status page or its JSON (loopback only).
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	if s.opts.Driver == nil {
		http.NotFound(w, r)
		return
	}
	s.opts.Driver.Status(w, r)
}

// handover is another uploadspike's request for the hub (loopback only):
// it needs the run state's handover token, else 401.
func (s *Server) handover(w http.ResponseWriter, r *http.Request) {
	if s.opts.Driver == nil {
		http.NotFound(w, r)
		return
	}
	token := s.opts.Driver.HandoverToken()
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	w.WriteHeader(s.opts.Driver.Handover())
}

// cameras lists no camera: uploadspike serves the app's OAUTH scenario,
// whose environment() needs the list.
func (s *Server) cameras(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET only"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, "[]\n")
}

// results stores the app's result under results/app.
func (s *Server) results(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	if s.app == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no results folder"})
		return
	}
	s.app.handle(w, r)
}

// HandoverURL is the holder's handover endpoint on this device.
func HandoverURL(port int) string {
	if port == 0 {
		port = HandoffPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/spike/v1/run/handover", port)
}
