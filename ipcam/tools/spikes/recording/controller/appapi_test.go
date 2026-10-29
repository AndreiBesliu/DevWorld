package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// ageResult is the AGE scenario's result as the spike app posts it.
const ageResult = `{"scenario":"AGE","device":{"platform":"android","model":"SM-A245F","ua":"Mozilla/5.0 (Linux; Android 14)"},` +
	`"startedAt":"2026-10-29T12:00:00.000Z","durationS":14.2,"interrupted":false,"hubJitterMs":[],` +
	`"samples":{"mbps":[85.5],"bytes":[33554432],"ms":[3139.5]},"notes":["x < y & z"]}`

// post sends body to the API's results endpoint from origin, as the app's
// fetch does (text/plain needs no preflight).
func post(t *testing.T, url, origin, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/spike/v1/results", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestAppEndpoints_AcceptAgeResult (G24): reccontroller answers the spike
// app on :7080 as spikehub does for what MI4's phone step needs: the camera
// list, empty, and the results, kept under results/app/ as
// <device>-<yyyymmdd-hhmmss>-<scenario>.json, re-encoded on one line, which
// the final collection counts as the AGE result. The shells' origins may
// call it and read the answers; another origin may not post.
func TestAppEndpoints_AcceptAgeResult(t *testing.T) {
	root := t.TempDir()
	api := NewAppAPI(filepath.Join(root, "app"))
	api.now = func() time.Time { return time.Date(2026, 10, 29, 12, 0, 1, 0, time.UTC) }
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/spike/v1/cameras", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://localhost")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	cameras, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(cameras)) != "[]" || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost" {
		t.Errorf("GET cameras = %d %q, origin %q; want 200 [] for the Android shell", resp.StatusCode, cameras, resp.Header.Get("Access-Control-Allow-Origin"))
	}

	resp = post(t, srv.URL, "http://localhost", ageResult)
	var created struct {
		File string `json:"file"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST results = %d, %v", resp.StatusCode, err)
	}
	if created.File != "android-sm-a245f-20261029-120001-age.json" || resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost" {
		t.Errorf("stored as %q, origin %q", created.File, resp.Header.Get("Access-Control-Allow-Origin"))
	}
	stored, err := os.ReadFile(filepath.Join(root, "app", created.File)) //nolint:gosec // the test's own folder
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(stored, &got); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(ageResult), &want)
	if !reflect.DeepEqual(got, want) || strings.Count(string(stored), "\n") != 1 || !strings.Contains(string(stored), `"x < y & z"`) {
		t.Errorf("stored %s, want the result on one line, unescaped", stored)
	}
	if present, _ := results.Inventory(root, false); !slices.Contains(present, "AGE") {
		t.Errorf("the final collection finds %q, without the AGE result", present)
	}
	if resp := post(t, srv.URL, "http://localhost", ageResult); resp.StatusCode != http.StatusCreated {
		t.Errorf("a second result in the same second = %d, want it kept apart", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "app", "android-sm-a245f-20261029-120001-age-2.json")); err != nil {
		t.Errorf("the second result: %v", err)
	}

	for _, c := range []struct {
		name, origin, body string
		status             int
	}{
		{"not an object", "http://localhost", `[1]`, http.StatusBadRequest},
		{"larger than 2 MiB", "http://localhost", `{"a":"` + strings.Repeat("x", 2<<20) + `"}`, http.StatusRequestEntityTooLarge},
		{"another site", "https://example.com", ageResult, http.StatusForbidden},
	} {
		if resp := post(t, srv.URL, c.origin, c.body); resp.StatusCode != c.status {
			t.Errorf("%s: POST = %d, want %d", c.name, resp.StatusCode, c.status)
		}
	}
}

// TestAppEndpoints_FolderCaps (hub-8): as spikehub's, the results folder
// holds at most 5000 files and 500 MiB; a result past either cap is refused
// with 507 and nothing is written.
func TestAppEndpoints_FolderCaps(t *testing.T) {
	if api := NewAppAPI(t.TempDir()); api.fileCap != 5000 || api.byteCap != 500<<20 {
		t.Errorf("caps %d files, %d bytes; want spikehub's 5000 and 500 MiB", api.fileCap, api.byteCap)
	}
	for _, c := range []struct {
		name    string
		files   int
		byteCap int64
	}{{"files", 1, 500 << 20}, {"bytes", 5000, int64(len(ageResult)) + 100}} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			api := NewAppAPI(dir)
			api.fileCap, api.byteCap = c.files, c.byteCap
			srv := httptest.NewServer(api)
			t.Cleanup(srv.Close)
			if resp := post(t, srv.URL, "", ageResult); resp.StatusCode != http.StatusCreated {
				t.Fatalf("the first result = %d", resp.StatusCode)
			}
			if resp := post(t, srv.URL, "", ageResult); resp.StatusCode != http.StatusInsufficientStorage {
				t.Errorf("past the cap = %d, want 507", resp.StatusCode)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("the folder holds %d files, want 1", len(entries))
			}
		})
	}
}
