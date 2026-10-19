package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestParseFlags_Defaults(t *testing.T) {
	cfg, err := parseFlags(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := config{media: "./media", sims: 16, port: 7080, results: "./results", mdns: true, real: false}
	if cfg != want {
		t.Errorf("defaults = %+v, want %+v", cfg, want)
	}
}

func TestParseFlags_Values(t *testing.T) {
	cfg, err := parseFlags([]string{"-media", "m", "-sims", "4", "-port", "0", "-results", "r", "-mdns=false", "-real"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := config{media: "m", sims: 4, port: 0, results: "r", mdns: false, real: true}
	if cfg != want {
		t.Errorf("config = %+v, want %+v", cfg, want)
	}
}

func TestParseFlags_RejectsWhatTheHubCannotServe(t *testing.T) {
	for _, args := range [][]string{
		{"-sims", "17"}, // the codec matrix cameras are 17 and 18
		{"-sims", "-1"},
		{"-port", "65536"},
		{"-port", "-1"},
		{"-sims", "many"},
		{"-bogus"},
		{"rtsp://user:pw@camera/stream"}, // URLs are typed at the prompt, hidden: arguments show in process lists
	} {
		if _, err := parseFlags(args, io.Discard); err == nil {
			t.Errorf("parseFlags(%v) accepted the arguments", args)
		}
	}
	// The usage goes where the caller says, and carries none of the arguments.
	var usage bytes.Buffer
	_, err := parseFlags([]string{"rtsp://user:pw@camera/stream"}, &usage)
	if err == nil || strings.Contains(err.Error(), "pw") || strings.Contains(usage.String(), "pw") {
		t.Errorf("the refusal of an argument echoes it: %v / %s", err, usage.String())
	}
}

// harness runs the hub as the binary does, on loopback with an ephemeral port.
type harness struct {
	url            string
	stdout, stderr *syncBuffer
	cancel         context.CancelFunc
	done           chan error
	resultsDir     string
	mediaDir       string
}

type harnessOptions struct {
	args      []string
	askSecret func(string) (string, error)
	newAPI    func(bool) (*webrtc.API, bool, error)
	// lan lists the addresses for the banner; nil gives one Wi-Fi address.
	lan func() ([]lanAddr, error)
}

// startHub runs the hub until the test ends or stop is called. It returns once
// the hub listens and has printed its banner.
func startHub(t *testing.T, opts harnessOptions) *harness {
	t.Helper()
	captureLogs(t) // restores the default logger, which run replaces
	h := &harness{
		stdout:     &syncBuffer{},
		stderr:     &syncBuffer{},
		done:       make(chan error, 1),
		resultsDir: filepath.Join(t.TempDir(), "results"),
		mediaDir:   writeMedia(t),
	}
	ctx, cancel := context.WithCancel(t.Context())
	h.cancel = cancel
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(10 * time.Second):
			t.Error("the hub did not stop after its context was cancelled")
		}
	})
	listening := make(chan string, 1)
	lan := opts.lan
	if lan == nil {
		lan = func() ([]lanAddr, error) { return []lanAddr{{Name: "Wi-Fi", IP: net.IPv4(192, 168, 1, 12)}}, nil }
	}
	e := env{
		stdout:    h.stdout,
		stderr:    h.stderr,
		askSecret: opts.askSecret,
		newAPI:    opts.newAPI,
		clock:     newFakeClock(), // the simulators do not run on their own
		lan:       lan,
		listen: func(int) (net.Listener, error) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err == nil {
				listening <- ln.Addr().String()
			}
			return ln, err
		},
	}
	args := append([]string{"-media", h.mediaDir, "-results", h.resultsDir, "-sims", "2", "-port", "0", "-mdns=false"}, opts.args...)
	go func() { h.done <- run(ctx, args, e) }()
	select {
	case addr := <-listening:
		h.url = "http://" + addr
		// The listener exists before the banner is printed.
		waitUntil(t, "the banner is printed", func() bool { return strings.Contains(h.stdout.String(), "Opriți cu Ctrl+C") })
	case err := <-h.done:
		t.Fatalf("run returned before it listened: %v\nstdout: %s\nstderr: %s", err, h.stdout, h.stderr)
	case <-time.After(30 * time.Second):
		t.Fatal("the hub did not start")
	}
	return h
}

// stop cancels the hub and returns what run returned.
func (h *harness) stop(t *testing.T) error {
	t.Helper()
	h.cancel()
	select {
	case err := <-h.done:
		h.done <- err // for the cleanup
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the hub did not stop")
		return nil
	}
}

func (h *harness) get(t *testing.T, path string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func TestRun_ServesAndStopsWhenCancelled(t *testing.T) {
	h := startHub(t, harnessOptions{})

	resp, body := h.get(t, "/spike/v1/cameras")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"id":"sim-02"`) || strings.Contains(string(body), `"id":"sim-03"`) {
		t.Errorf("cameras: status %d: %s", resp.StatusCode, body)
	}
	resp, body = h.get(t, "/media/hls/index.m3u8")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "#EXTM3U") {
		t.Errorf("playlist: status %d: %s", resp.StatusCode, body)
	}
	banner := h.stdout.String()
	for _, want := range []string{"http://192.168.1.12:" + strings.TrimPrefix(h.url, "http://127.0.0.1:"), "Rețele private"} {
		if !strings.Contains(banner, want) {
			t.Errorf("banner lacks %q:\n%s", want, banner)
		}
	}

	if err := h.stop(t); err != nil {
		t.Errorf("run returned %v after a clean shutdown", err)
	}
	if _, err := postJSON(t, h.url+"/spike/v1/results", `{}`); err == nil {
		t.Error("the hub still serves after it stopped")
	}
}

func TestRun_PrintsAnInterfaceListingErrorInTheBanner(t *testing.T) {
	h := startHub(t, harnessOptions{lan: func() ([]lanAddr, error) {
		return nil, errors.New("route ip+net: netlinkrib: permission denied")
	}})

	if banner := h.stdout.String(); !strings.Contains(banner, "route ip+net: netlinkrib: permission denied") {
		t.Errorf("banner does not show the error of the interface listing:\n%s", banner)
	}
}

func TestRun_ReportsUnavailableMDNSInTheBannerAndInEveryResultsFile(t *testing.T) {
	var asked []bool
	h := startHub(t, harnessOptions{
		args: []string{"-mdns=true"},
		newAPI: func(mdns bool) (*webrtc.API, bool, error) {
			asked = append(asked, mdns)
			api, _, err := newAPIForTest()
			return api, true, err // as when the mDNS port cannot be opened
		},
	})
	if len(asked) != 1 || !asked[0] {
		t.Errorf("NewAPI was called with mdns %v, want [true] for -mdns=true", asked)
	}
	if banner := h.stdout.String(); !strings.Contains(banner, "mDNS") {
		t.Errorf("banner does not report the unavailable mDNS:\n%s", banner)
	}

	for _, scenario := range []string{"S1", "S2"} {
		if body, err := postJSON(t, h.url+"/spike/v1/results", `{"device":"x","scenario":"`+scenario+`"}`); err != nil || !strings.Contains(body, `"file"`) {
			t.Fatalf("results: %v: %s", err, body)
		}
	}
	files := resultFiles(t, h.resultsDir)
	if len(files) != 2 {
		t.Fatalf("results folder holds %v, want 2 files", files)
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(h.resultsDir, name)) //nolint:gosec // a file in the test's results folder
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(data, &got); err != nil || string(got["mdnsUnavailable"]) != "true" {
			t.Errorf("%s: mdnsUnavailable = %s (%v), want true", name, got["mdnsUnavailable"], err)
		}
	}
}

func TestRun_PassesTheMDNSFlagToNewAPI(t *testing.T) {
	var asked []bool
	startHub(t, harnessOptions{
		newAPI: func(mdns bool) (*webrtc.API, bool, error) {
			asked = append(asked, mdns)
			return newAPIForTest()
		},
	}) // the harness passes -mdns=false
	if len(asked) != 1 || asked[0] {
		t.Errorf("NewAPI was called with mdns %v, want [false] for -mdns=false", asked)
	}
}

// The hub's HTTP port is IPv4 only, like its ICE (R25). Dual-stack, Firefox at
// http://localhost reaches it over [::1] and then, without a camera permission,
// looks for its default IPv6 address only; on a host without an IPv6 route it
// gathers no ICE candidate and never connects.
func TestDefaultListen_IsIPv4Only(t *testing.T) {
	ln, err := env{}.withDefaults().listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	addr := ln.Addr().(*net.TCPAddr)
	if addr.IP.To4() == nil {
		t.Errorf("the hub listens on %v, want an IPv4 address (0.0.0.0)", addr)
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("::1", strconv.Itoa(addr.Port)), time.Second)
	if err == nil {
		_ = conn.Close()
		t.Errorf("a client reached the hub over [::1]:%d, want it refused so that the browser falls back to 127.0.0.1", addr.Port)
	}
}

func TestRun_ExplainsMissingMediaInRomanian(t *testing.T) {
	captureLogs(t)
	err := run(t.Context(), []string{"-media", filepath.Join(t.TempDir(), "nope"), "-port", "0", "-mdns=false"}, env{stdout: io.Discard, stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "fișierele media") {
		t.Errorf("run with no media: error %v, want one in Romanian that names the media files", err)
	}
}

func TestRun_ExplainsABusyPortInRomanian(t *testing.T) {
	captureLogs(t)
	busy, err := net.Listen("tcp", ":0") //nolint:gosec // the hub listens on all interfaces too, and the ports must clash
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	port := busy.Addr().(*net.TCPAddr).Port

	err = run(t.Context(), []string{"-media", writeMedia(t), "-port", strconv.Itoa(port), "-mdns=false"}, env{stdout: io.Discard, stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "portul "+strconv.Itoa(port)) {
		t.Errorf("run on a busy port: error %v, want one in Romanian that names the port", err)
	}
}

func TestRun_FailsWhenNewAPIFails(t *testing.T) {
	captureLogs(t)
	boom := errors.New("boom")
	err := run(t.Context(), []string{"-media", writeMedia(t), "-port", "0", "-mdns=false"}, env{
		stdout: io.Discard, stderr: io.Discard,
		newAPI: func(bool) (*webrtc.API, bool, error) { return nil, false, boom },
	})
	if !errors.Is(err, boom) {
		t.Errorf("run = %v, want the error of NewAPI", err)
	}
}
