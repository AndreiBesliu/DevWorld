package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/session"
	"github.com/pion/webrtc/v4"
)

// SPIKE-ONLY (phase0b video spike plan, Global Constraints): the hub speaks
// plain HTTP on the LAN, port 7080, so that a phone can reach it without a
// certificate. Production signalling does not.

// maxOfferBytes is the largest offer the hub accepts; the offer of 16 tiles
// from a browser is about 50 kB.
const maxOfferBytes = 256 << 10

// allowedOrigins are the origins of the shells that may call the hub from
// another origin than its own (CORS): the web app on the PC (7080 and 80), the
// Android WebView with androidScheme "http" and the Windows WebView2.
var allowedOrigins = []string{
	"http://localhost:7080",
	"http://localhost",
	"http://tauri.localhost",
	"https://tauri.localhost",
}

// hubConfig is what a hub is built from.
type hubConfig struct {
	cat *catalog
	api *webrtc.API
	// mdnsUnavailable is the flag from session.NewAPI: the hub cannot resolve
	// the .local candidates of browsers. Every results file carries it.
	mdnsUnavailable bool
	// hlsDir is the directory of the recording (media/hls).
	hlsDir string
	// resultsDir is where the results files go.
	resultsDir string
	// web is the spike app, as the files of its build; the root holds index.html
	// and assets/.
	web fs.FS
	now func() time.Time
	log *slog.Logger
}

// hub is the spike hub's HTTP side: the camera list, the WebRTC offers, the
// recording, the results and the app.
type hub struct {
	hubConfig

	mu       sync.Mutex
	sessions map[*session.Session]struct{}
	closed   bool
}

func newHub(cfg hubConfig) *hub {
	return &hub{hubConfig: cfg, sessions: map[*session.Session]struct{}{}}
}

// Handler returns the hub's routes.
func (h *hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /spike/v1/cameras", h.handleCameras)
	mux.HandleFunc("POST /spike/v1/offer", h.handleOffer)
	mux.HandleFunc("POST /spike/v1/results", h.handleResults)
	mux.Handle("GET /media/hls/", http.StripPrefix("/media/hls/", h.hlsHandler()))
	mux.Handle("GET /", h.appHandler())
	return h.withCORS(mux)
}

// withCORS lets the listed origins call the hub from their pages, and no other
// (the browser enforces the answer to the preflight). A POST from another
// origin is refused as well: a page of another origin can send a POST that
// needs no preflight, and it must not write a results file or open a session.
// The app that the hub serves itself is not another origin.
func (h *hub) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		switch {
		case origin == "":
		case slices.Contains(allowedOrigins, origin):
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Range")
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		case r.Method == http.MethodPost && origin != "http://"+r.Host:
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleCameras lists the cameras and their profiles.
func (h *hub) handleCameras(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(h.cat.cameras); err != nil {
		h.log.Debug("send the camera list", "err", err)
	}
}

// handleOffer answers a viewer's SDP offer with a new session (spec §6.3). The
// session lives until the viewer leaves, not until the request ends.
func (h *hub) handleOffer(w http.ResponseWriter, r *http.Request) {
	offer, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxOfferBytes))
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		http.Error(w, "the offer is larger than 256 KiB", http.StatusRequestEntityTooLarge)
		return
	case err != nil:
		http.Error(w, "the request body cannot be read", http.StatusBadRequest)
		return
	case len(offer) == 0:
		http.Error(w, "the offer is empty", http.StatusBadRequest)
		return
	}
	if h.isClosed() {
		http.Error(w, "the hub is shutting down", http.StatusServiceUnavailable)
		return
	}
	s, answer, err := session.Answer(h.api, string(offer), h.cat.sources)
	if err != nil {
		h.log.Warn("an offer was refused", "err", err)
		http.Error(w, "the offer cannot be answered: "+err.Error(), http.StatusBadRequest)
		return
	}
	h.track(s)
	w.Header().Set("Content-Type", "application/sdp")
	_, _ = io.WriteString(w, answer)
}

func (h *hub) isClosed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

// track keeps a session in the list until it closes. A session that arrives
// after Close is closed at once.
func (h *hub) track(s *session.Session) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = s.Close()
		return
	}
	h.sessions[s] = struct{}{}
	n := len(h.sessions)
	h.mu.Unlock()
	h.log.Info("a viewer connected", "sessions", n)

	go func() {
		<-s.Done()
		h.mu.Lock()
		delete(h.sessions, s)
		n := len(h.sessions)
		h.mu.Unlock()
		h.log.Info("a viewer left", "sessions", n)
	}()
}

// activeSessions is the number of sessions that are open.
func (h *hub) activeSessions() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

// Close closes every session and refuses new offers. The sources are not
// closed: they live for the process (R28).
func (h *hub) Close() {
	h.mu.Lock()
	h.closed = true
	open := make([]*session.Session, 0, len(h.sessions))
	for s := range h.sessions {
		open = append(open, s)
	}
	h.mu.Unlock()
	for _, s := range open {
		_ = s.Close()
	}
}

// hlsTypes are the content types of the recording's files: Go's table has none
// of them, and Safari plays the playlist only when it is served as one.
var hlsTypes = map[string]string{
	".m3u8": "application/vnd.apple.mpegurl",
	".m4s":  "video/iso.segment",
	".mp4":  "video/mp4",
}

// hlsHandler serves the recording with byte ranges. It sends no-store, because
// a browser that kept a segment would start the next run faster than a
// viewer's first start, which MV10 measures.
func (h *hub) hlsHandler() http.Handler {
	files := http.FileServer(http.Dir(h.hlsDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if typ, ok := hlsTypes[strings.ToLower(path.Ext(r.URL.Path))]; ok {
			w.Header().Set("Content-Type", typ)
		}
		files.ServeHTTP(w, r)
	})
}

// appHandler serves the embedded spike app: index.html at / and its files, such
// as assets/*. The repository embeds only a .gitkeep; a release build copies the
// app's dist/ next to it.
func (h *hub) appHandler() http.Handler {
	files := http.FileServerFS(h.web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(h.web, "index.html"); err != nil {
			http.Error(w, "Aplicația de test nu este inclusă în acest build de spikehub. "+
				"Folosiți arhiva de lansare sau copiați dist/ al aplicației în cmd/spikehub/web și construiți din nou.",
				http.StatusNotFound)
			return
		}
		files.ServeHTTP(w, r)
	})
}
