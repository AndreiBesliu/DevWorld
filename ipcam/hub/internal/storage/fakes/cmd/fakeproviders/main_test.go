package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakegdrive "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/gdrive"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/gdrive"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/onedrive"
)

const (
	kib = 1 << 10
	mib = 1 << 20
)

// mainEnv makes the test binary run as fakeproviders, with its arguments.
const mainEnv = "FAKEPROVIDERS_TEST_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(mainEnv) == "1" {
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// providers is a fakeproviders process that a test started, with the URLs
// of its schema line.
type providers struct {
	cmd    *exec.Cmd
	urls   map[string]string
	stderr *syncBuffer
	done   chan error
}

// syncBuffer is a bytes.Buffer that a process writes while a test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// start runs fakeproviders with args and reads its schema line. The process
// is killed when the test ends, unless the test stopped it.
func start(t *testing.T, args ...string) *providers {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...) //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), mainEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &providers{cmd: cmd, stderr: &syncBuffer{}, done: make(chan error, 1)}
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		if sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
		_, _ = io.Copy(io.Discard, stdout)
		p.done <- cmd.Wait()
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-p.done
	})
	select {
	case line, ok := <-lines:
		if !ok {
			t.Fatalf("fakeproviders %q printed no line; stderr:\n%s", args, p.stderr)
		}
		if err := json.Unmarshal([]byte(line), &p.urls); err != nil {
			t.Fatalf("fakeproviders printed %q, not a JSON object of strings: %v", line, err)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("fakeproviders %q printed no line in 30 s; stderr:\n%s", args, p.stderr)
	}
	return p
}

// tokens is the token source of the fakes, which accept any token.
func tokens() oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "tok-fakeproviders", TokenType: "Bearer"})
}

// drive is a Drive driver of p's fakegdrive, with chunks of part bytes.
func (p *providers) drive(t *testing.T, part int64) *gdrive.Drive {
	t.Helper()
	d, err := gdrive.New(gdrive.Options{
		DestID: "dst-1", HubID: "H", HTTPClient: &http.Client{}, Tokens: tokens(),
		BaseURL: p.urls["gdrive"], UploadBaseURL: p.urls["gdriveUpload"], PartSize: part, Folders: fakegdrive.NewFolderStore(),
	})
	if err != nil {
		t.Fatalf("gdrive.New: %v", err)
	}
	return d
}

// graph is a OneDrive driver of p's fakegraph, with fragments of 320 KiB.
func (p *providers) graph(t *testing.T) *onedrive.OneDrive {
	t.Helper()
	d, err := onedrive.New(onedrive.Options{
		DestID: "dst-od", HubID: "H", HTTPClient: &http.Client{}, Tokens: tokens(), BaseURL: p.urls["graph"], PartSize: 320 * kib,
	})
	if err != nil {
		t.Fatalf("onedrive.New: %v", err)
	}
	return d
}

// uploadResumable uploads data to key through d's resumable session in
// chunks of part bytes, asking the session for its offset after the first,
// and checks Commit's size and hash.
func uploadResumable(t *testing.T, d storage.Destination, key storage.ObjectKey, data []byte, part int64) {
	t.Helper()
	ctx := t.Context()
	size := int64(len(data))
	st, err := d.BeginUpload(ctx, key, size, storage.Meta{ContentType: "video/mp4"})
	if err != nil {
		t.Fatalf("%s: BeginUpload: %v", d.Kind(), err)
	}
	for off := int64(0); off < size; off = st.Offset {
		n := min(part, size-off)
		if err := d.PutChunk(ctx, &st, off, bytes.NewReader(data[off:off+n]), n); err != nil {
			t.Fatalf("%s: PutChunk at %d: %v", d.Kind(), off, err)
		}
		if off == 0 {
			if next, err := d.Resume(ctx, &st); err != nil || next != n {
				t.Fatalf("%s: Resume after the first chunk = %d, %v; want %d", d.Kind(), next, err, n)
			}
		}
	}
	ref, err := d.Commit(ctx, &st)
	if err != nil {
		t.Fatalf("%s: Commit: %v", d.Kind(), err)
	}
	kind := d.Capabilities().Hashes[0]
	want, err := storage.Digest(kind, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Size != size || ref.HashKind != kind || ref.Hash != want {
		t.Errorf("%s: Commit = %d bytes, %s %s; want %d bytes, %s %s", d.Kind(), ref.Size, ref.HashKind, ref.Hash, size, kind, want)
	}
}

// stop sends p SIGTERM and returns its exit code, -1 when the signal killed
// it. Where a process cannot be sent a signal (Windows), it kills p and
// returns false.
func (p *providers) stop(t *testing.T) (int, bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		_ = p.cmd.Process.Kill()
		p.done <- <-p.done // for the cleanup
		return 0, false
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	select {
	case err := <-p.done:
		p.done <- err // for the cleanup
		var exit *exec.ExitError
		switch {
		case err == nil:
			return 0, true
		case errors.As(err, &exit):
			return exit.ExitCode(), true
		}
		t.Fatalf("fakeproviders after SIGTERM: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("fakeproviders did not exit in 30 s after SIGTERM")
	}
	return 0, false
}

func randomBytes(n int, seed byte) []byte {
	b := make([]byte, n)
	_, _ = rand.NewChaCha8([32]byte{seed}).Read(b)
	return b
}

// TestFakeProviders_ServesBothAndPrintsURLs: started on 127.0.0.1:0,
// fakeproviders prints its schema line with the URLs of the port it took,
// a resumable upload succeeds through each, with the Drive API and its
// upload URL apart as the live API's are, fakeoauth answers under /oauth
// (Task 9, which uploadspike mi5 --fakes reads) and auto-approves a device
// login of a client registered over HTTP, whose access token fakegdrive
// then scopes, and SIGTERM ends it with exit 0.
func TestFakeProviders_ServesBothAndPrintsURLs(t *testing.T) {
	p := start(t, "-listen", "127.0.0.1:0")
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range p.urls {
			if !yield(k) {
				return
			}
		}
	})
	if want := []string{"gdrive", "gdriveUpload", "graph", "oauth", "schema"}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("the schema line's keys %q, want %q", keys, want)
	}
	if p.urls["schema"] != "ipcam.fakeproviders/1" {
		t.Errorf("schema %q, want ipcam.fakeproviders/1", p.urls["schema"])
	}
	base := strings.TrimSuffix(p.urls["graph"], "/graph")
	if !strings.HasPrefix(base, "http://127.0.0.1:") || strings.HasSuffix(base, ":0") {
		t.Fatalf("graph URL %q, want http://127.0.0.1:<port>/graph", p.urls["graph"])
	}
	for name, path := range map[string]string{"gdrive": "/gdrive", "gdriveUpload": "/gdrive-upload", "graph": "/graph", "oauth": "/oauth"} {
		if p.urls[name] != base+path {
			t.Errorf("%s URL %q, want %s%s", name, p.urls[name], base, path)
		}
	}
	// A new Drive driver's first upload makes the root folder: a key in it
	// keeps its creates within the driver's 2 a second.
	uploadResumable(t, p.drive(t, 256*kib), "a.mp4", randomBytes(600*kib, 1), 256*kib)
	uploadResumable(t, p.graph(t), "c1/a.mp4", randomBytes(700*kib, 2), 320*kib)
	access := deviceLogin(t, p.urls["oauth"])
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, p.urls["gdrive"]+"/drive/v3/files?q="+
		url.QueryEscape("'root' in parents and trashed = false"), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	listed, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.Contains(string(listed), "ipcam") {
		t.Errorf("fakeoauth's token listed My Drive with HTTP %d: %s; want 200 and none of the static token's files", resp.StatusCode, listed)
	}
	if code, signalled := p.stop(t); signalled && code != 0 {
		t.Errorf("fakeproviders exited %d after SIGTERM, want 0; stderr:\n%s", code, p.stderr)
	}
}

// TestFakeProviders_Throttle: with -throttle 1048576, fakeproviders reads a
// request's body at 1 MiB/s, so a 2 MiB chunk takes 2 s, at least 1.9 s.
func TestFakeProviders_Throttle(t *testing.T) {
	p := start(t, "-listen", "127.0.0.1:0", "-throttle", "1048576")
	d := p.drive(t, 2*mib)
	data := randomBytes(2*mib, 3)
	st, err := d.BeginUpload(t.Context(), "a.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}
	began := time.Now()
	if err := d.PutChunk(t.Context(), &st, 0, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("PutChunk: %v", err)
	}
	if took := time.Since(began); took < 1900*time.Millisecond {
		t.Errorf("a 2 MiB chunk took %s at -throttle 1048576, want 1.9 s at least", took)
	}
	if _, err := d.Commit(t.Context(), &st); err != nil {
		t.Errorf("Commit: %v", err)
	}
}

// TestFakeProviders_Usage: fakeproviders refuses a missing -listen, a
// negative -throttle and stray arguments with exit 2, printing nothing on
// stdout.
func TestFakeProviders_Usage(t *testing.T) {
	for _, args := range [][]string{{}, {"-listen", "127.0.0.1:0", "-throttle", "-1"}, {"-listen", "127.0.0.1:0", "extra"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Errorf("run(%q) = %d with stdout %q, want 2 and nothing", args, code, stdout.String())
		}
	}
}

// deviceLogin registers a TV client with the fakeoauth at base, runs its
// device flow, which fakeproviders approves at once, and returns the access
// token.
func deviceLogin(t *testing.T, base string) string {
	t.Helper()
	secret := "SENT" + "INEL-tv"
	client, err := json.Marshal(map[string]any{"provider": "google", "type": "tv", "id": "tv.apps", "secret": secret, "project": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(base+"/fake/clients", "application/json", bytes.NewReader(client)) //nolint:noctx // a test of the fake
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("registering a client: HTTP %d", resp.StatusCode)
	}
	form := func(path string, v url.Values) map[string]any {
		t.Helper()
		resp, err := http.PostForm(base+path, v) //nolint:noctx // a test of the fake
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	dev := form("/google/device/code", url.Values{"client_id": {"tv.apps"}, "scope": {"https://www.googleapis.com/auth/drive.file"}})
	tok := form("/google/token", url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "client_id": {"tv.apps"},
		"client_secret": {secret}, "device_code": {fmt.Sprint(dev["device_code"])}})
	access, _ := tok["access_token"].(string)
	if access == "" {
		t.Fatalf("the device flow answered %v, want an access token", tok)
	}
	return access
}
