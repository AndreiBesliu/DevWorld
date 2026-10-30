// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The caps of the app's results, spikehub's (cmd/spikehub/results.go, hub-8):
// any LAN client may post, so they keep a stray or hostile one from filling
// the PC's disk.
const (
	maxAppResultBytes = 2 << 20
	appResultsFileCap = 5000
	appResultsByteCap = 500 << 20
	maxAppNameLength  = 40
)

// appOrigins are the spike app's shells, which may call the endpoints from
// their pages: the Android WebView (androidScheme "http") and the Windows
// WebView2, as spikehub lists them.
var appOrigins = []string{"http://localhost:7080", "http://localhost", "http://tauri.localhost", "https://tauri.localhost"}

// AppAPI is reccontroller's answer to the video spike's app on :7080 (G24):
// what MI4's phone step needs of spikehub's endpoints, the camera list,
// which is empty (the AGE scenario needs no camera), and the results, which
// it keeps under its folder as spikehub keeps them.
type AppAPI struct {
	dir     string
	fileCap int
	byteCap int64
	now     func() time.Time
	mu      sync.Mutex // the caps' check and the write are one step
	mux     *http.ServeMux
}

// NewAppAPI serves the app's endpoints, its results kept in dir (results/app).
func NewAppAPI(dir string) *AppAPI {
	a := &AppAPI{dir: dir, fileCap: appResultsFileCap, byteCap: appResultsByteCap, now: time.Now, mux: http.NewServeMux()}
	a.mux.HandleFunc("GET /spike/v1/cameras", a.handleCameras)
	a.mux.HandleFunc("POST /spike/v1/results", a.handleResults)
	return a
}

// ServeHTTP lets the shells' origins call the endpoints and read the
// answers, and refuses a POST from another origin (another site's page may
// post a form), as spikehub does; a preflight that asks for private network
// access gets it for a shell.
func (a *AppAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Origin")
	origin := r.Header.Get("Origin")
	switch {
	case origin == "":
	case slices.Contains(appOrigins, origin):
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
	case r.Method == http.MethodPost && origin != "http://"+r.Host:
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	a.mux.ServeHTTP(w, r)
}

// handleCameras lists no camera: reccontroller's cameras are RTSP, for the
// hosts.
func (a *AppAPI) handleCameras(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, "[]\n")
}

// handleResults stores a result, a JSON object of at most 2 MiB, as
// <device>-<yyyymmdd-hhmmss>-<scenario>.json in UTC, re-encoded on one line
// with its keys sorted and without HTML escapes; a second one of the same
// device, scenario and second gets -2, -3 and so on. A result that would
// take the folder over its file or byte cap, counting the files there, is
// refused with 507, and nothing is written.
func (a *AppAPI) handleResults(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAppResultBytes))
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		http.Error(w, "the result is larger than 2 MiB", http.StatusRequestEntityTooLarge)
		return
	case err != nil:
		http.Error(w, "the request body cannot be read", http.StatusBadRequest)
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		http.Error(w, "the result must be a JSON object", http.StatusBadRequest)
		return
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(fields); err != nil {
		http.Error(w, "the result cannot be encoded", http.StatusInternalServerError)
		return
	}
	stem := fmt.Sprintf("%s-%s-%s", deviceName(fields["device"]), a.now().UTC().Format("20060102-150405"), fileNamePart(scenario(fields["scenario"])))
	a.mu.Lock()
	defer a.mu.Unlock()
	files, size, err := folderUsage(a.dir)
	if err != nil {
		http.Error(w, "the result cannot be stored", http.StatusInternalServerError)
		return
	}
	if files+1 > a.fileCap || size+int64(out.Len()) > a.byteCap {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInsufficientStorage)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("the results folder is full: at most %d files and %d MiB", a.fileCap, a.byteCap>>20),
		})
		return
	}
	name, err := writeNew(a.dir, stem, out.Bytes())
	if err != nil {
		http.Error(w, "the result cannot be stored", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"file": name})
}

// fileNamePart turns a field into a file name part of [a-z0-9-]{1,40}, as
// spikehub's sanitiseName: lower case, every other run of characters one
// hyphen, none at either end, "unknown" when nothing is left.
func fileNamePart(s string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			hyphen = false
			b.WriteRune(r)
		} else {
			hyphen = true
		}
	}
	name := b.String()
	if len(name) > maxAppNameLength {
		name = strings.TrimRight(name[:maxAppNameLength], "-")
	}
	if name == "" {
		return "unknown"
	}
	return name
}

// deviceName names a result's device: a label, or the scenario result's
// object, its platform and model.
func deviceName(raw json.RawMessage) string {
	var label string
	if json.Unmarshal(raw, &label) == nil {
		return fileNamePart(label)
	}
	var device struct {
		Platform string `json:"platform"`
		Model    string `json:"model"`
	}
	if json.Unmarshal(raw, &device) == nil && device.Platform != "" {
		return fileNamePart(device.Platform + " " + device.Model)
	}
	return fileNamePart("")
}

// scenario is a result's scenario, a string such as "AGE".
func scenario(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// folderUsage is the number of files in dir and their total size; a folder
// that does not exist yet holds nothing.
func folderUsage(dir string) (files int, size int64, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue // removed since the listing
		}
		if err != nil {
			return 0, 0, err
		}
		files++
		size += info.Size()
	}
	return files, size, nil
}

// writeNew creates dir/<stem>.json, or <stem>-2.json and so on when that
// exists, and returns the file's name.
func writeNew(dir, stem string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	for n := 1; ; n++ {
		name := stem + ".json"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.json", stem, n)
		}
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the name is sanitised
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return "", err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(f.Name())
			return "", err
		}
		return name, nil
	}
}
