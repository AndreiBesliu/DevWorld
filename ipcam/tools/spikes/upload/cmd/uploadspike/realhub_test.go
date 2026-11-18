package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
)

// realRig is a device with the real ipcam-hub, which starts the fork's
// MediaMTX (MEDIAMTX_BIN) even without jobs, on the fakes of a
// fakeproviders process; the test's own clients are registered there.
func realRig(t *testing.T, known ...string) (*rig, *fakes) {
	t.Helper()
	return realRigWith(t, nil, known...)
}

// realRigWith is realRig whose fakeproviders also takes args.
func realRigWith(t *testing.T, args []string, known ...string) (*rig, *fakes) {
	t.Helper()
	mediamtx := spiketest.Required(t, "MEDIAMTX_BIN", "RECSPIKE_REQUIRE_MEDIAMTX")
	hub := binary(t, "ipcam-hub", "github.com/AndreiBesliu/DevWorld/ipcam/hub/cmd/ipcam-hub")
	f := startFakes(t, args, known...)
	r := newRigWith(t, hub, "")
	r.env = append(r.env, envMediaMTX+"="+mediamtx, envFakes+"="+f.base)
	return r, f
}

// fakes is a fakeproviders process.
type fakes struct {
	base string // http://127.0.0.1:<port>
}

// The test's clients at the fakes, as clientsInput gives them to setup.
var fakeClients = []map[string]any{
	{"provider": "google", "type": "android", "id": "android-client-0001.apps.example", "project": "p1",
		"redirectUris": []string{"ro.ipcam.spike:/oauth2redirect"}},
	{"provider": "google", "type": "desktop", "id": "desktop-client-0001.apps.example", "secret": "SENT" + "INEL-desktop-secret-01", "project": "p1"},
	{"provider": "google", "type": "tv", "id": "tv-client-0001.apps.example", "secret": "SENT" + "INEL-tv-secret-000001", "project": "p1"},
	{"provider": "google", "type": "web", "id": "web-client-0001.apps.example", "secret": "SENT" + "INEL-web-secret-00001", "project": "p1",
		"redirectUris": []string{"http://localhost:7091"}},
}

// startFakes starts fakeproviders on 127.0.0.1 with extra args and the
// known values (-known kind=value) and registers the test's clients.
func startFakes(t *testing.T, extra []string, known ...string) *fakes {
	t.Helper()
	bin := binary(t, "fakeproviders", "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/cmd/fakeproviders")
	args := append([]string{"-listen", "127.0.0.1:0"}, extra...)
	for _, k := range known {
		args = append(args, "-known", k)
	}
	cmd := exec.Command(bin, args...) //nolint:gosec // the test's own build
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, out) }()
	var urls struct {
		OAuth string `json:"oauth"`
	}
	if err := json.Unmarshal([]byte(line), &urls); err != nil {
		t.Fatal(err)
	}
	f := &fakes{base: strings.TrimSuffix(urls.OAuth, "/oauth")}
	for _, c := range fakeClients {
		b, _ := json.Marshal(c)
		resp, err := http.Post(urls.OAuth+"/fake/clients", "application/json", bytes.NewReader(b)) //nolint:noctx // the test's fake
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("registering a client: %d", resp.StatusCode)
		}
	}
	return f
}

// issued is what the fakes minted and the verifiers they received.
func (f *fakes) issued(t *testing.T) (minted map[string][]string, verifiers []string) {
	t.Helper()
	resp, err := http.Get(f.base + "/oauth/fake/issued") //nolint:noctx // the test's fake
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var v struct {
		Minted    map[string][]string `json:"minted"`
		Verifiers []string            `json:"verifiers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v.Minted, v.Verifiers
}

// app is the video app's OAUTH scenario on a platform, acting on the
// rig's handoff server: it asks for the awaited login, begins it, and
// for plan A lets the fakes' authorization page redirect with a code that
// it posts back; payloads are the bodies that it posted.
type app struct {
	r        *rig
	payloads []string
}

// next waits for the login awaited on platform.
func (a *app) next(t *testing.T, platform string) (dest, provider, plan string) {
	t.Helper()
	var n struct{ Dest, Provider, Plan string }
	until(t, 2*time.Minute, "the login awaited on "+platform, func() bool {
		code, body := a.r.get("/spike/v1/oauth/next?platform="+platform, "Origin", "http://localhost")
		return code == http.StatusOK && json.Unmarshal([]byte(body), &n) == nil
	})
	return n.Dest, n.Provider, n.Plan
}

// begin begins the awaited login on platform with port.
func (a *app) begin(t *testing.T, platform string, port int) map[string]any {
	t.Helper()
	dest, provider, _ := a.next(t, platform)
	q := url.Values{"dest": {dest}, "provider": {provider}, "platform": {platform}, "port": {fmt.Sprint(port)}}
	code, body := a.r.get("/spike/v1/oauth/begin?"+q.Encode(), "Origin", "http://localhost")
	var res map[string]any
	if code != http.StatusOK || json.Unmarshal([]byte(body), &res) != nil {
		t.Fatalf("begin on %s: %d %s", platform, code, body)
	}
	return res
}

// loginA runs a plan A login on platform: the fakes approve at once; the
// code goes back as text/plain JSON with exactly code and state (a wrong
// code when refuse is set).
func (a *app) loginA(t *testing.T, platform string, port int, refuse bool) {
	t.Helper()
	res := a.begin(t, platform, port)
	authURL, _ := res["authUrl"].(string)
	if authURL == "" {
		t.Fatalf("begin on %s gave no authorization URL: %v", platform, res)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(authURL) //nolint:noctx // the test's fake
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || loc.Query().Get("code") == "" {
		t.Fatalf("the fakes' authorization page: %d, %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	code := loc.Query().Get("code")
	if refuse {
		code = "a-code-the-fakes-never-issued"
	}
	body, _ := json.Marshal(map[string]string{"code": code, "state": loc.Query().Get("state")})
	a.payloads = append(a.payloads, string(body))
	status, answer := a.r.do(http.MethodPost, "/spike/v1/oauth/complete", string(body), "Origin", "http://localhost", "Content-Type", "text/plain")
	if refuse != (status != http.StatusOK) {
		t.Fatalf("complete on %s: %d %s", platform, status, answer)
	}
}

// loginB1 begins a device login on platform; the fakes approve it at once,
// and uploadspike's status polls complete it.
func (a *app) loginB1(t *testing.T, platform string) {
	t.Helper()
	if res := a.begin(t, platform, 0); res["userCode"] == nil {
		t.Fatalf("a B1 login on %s gave no user code: %v", platform, res)
	}
}

// webCells posts the web reader's cells, all listed and read.
func (a *app) webCells(t *testing.T, creators ...string) {
	t.Helper()
	origin := fmt.Sprintf("http://localhost:%d", a.r.port)
	for _, c := range creators {
		until(t, 3*time.Minute, "the web reader's turn", func() bool {
			code, _ := a.r.do(http.MethodPost, "/spike/v1/visibility/web", `{"creator":"`+c+`","listed":true,"read":true}`, "Origin", origin)
			return code == http.StatusNoContent
		})
	}
}

// filesUnder are the contents of every file under dir.
func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // the test's own files
		if err == nil {
			out[p] = string(b)
		}
		return nil
	})
	return out
}
