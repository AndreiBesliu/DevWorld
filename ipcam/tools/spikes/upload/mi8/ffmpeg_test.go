package mi8

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// archiveServer serves body and records each request's User-Agent.
type archiveServer struct {
	*httptest.Server
	mu  sync.Mutex
	uas []string
}

func serveArchive(t *testing.T, body []byte) *archiveServer {
	t.Helper()
	s := &archiveServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.uas = append(s.uas, r.Header.Get("User-Agent"))
		s.mu.Unlock()
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// zipWith is a zip archive with ffmpeg.exe under bin/, as BtbN's win64
// archive holds it.
func zipWith(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"ffmpeg-n9.0-win64-lgpl-9.0/bin/ffmpeg.exe": content, "ffmpeg-n9.0-win64-lgpl-9.0/LICENSE.txt": "LGPL"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestFFmpegFetch_PinnedHashAndNeutralUA (P41, C2-R5): the pinned archive is
// fetched by its exact URL with the User-Agent ipcam-spike/0.1; a wrong hash
// is refused and nothing is unpacked; a .zip gives ffmpeg.exe through
// archive/zip, and a .tar.xz is unpacked through the tar -xJf seam.
func TestFFmpegFetch_PinnedHashAndNeutralUA(t *testing.T) {
	t.Run("wrong hash", func(t *testing.T) {
		body := zipWith(t, "binary")
		srv := serveArchive(t, body)
		dir := t.TempDir()
		f := Fetcher{Dir: dir, Pin: Pin{URL: srv.URL + "/ffmpeg-win64-lgpl-9.0.zip", SHA256: sum([]byte("other"))}, Windows: true}
		if _, err := f.Fetch(t.Context()); !errors.Is(err, ErrHash) {
			t.Fatalf("Fetch = %v, want ErrHash", err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Errorf("a refused archive left %d files, want none", len(entries))
		}
		if len(srv.uas) != 1 || srv.uas[0] != "ipcam-spike/0.1" {
			t.Errorf("User-Agents = %q, want exactly ipcam-spike/0.1", srv.uas)
		}
	})
	t.Run("zip", func(t *testing.T) {
		body := zipWith(t, "the-binary")
		srv := serveArchive(t, body)
		dir := t.TempDir()
		f := Fetcher{Dir: dir, Pin: Pin{URL: srv.URL + "/ffmpeg-win64-lgpl-9.0.zip", SHA256: sum(body)}, Windows: true}
		path, err := f.Fetch(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if got := readFile(path); string(got) != "the-binary" || filepath.Base(path) != "ffmpeg.exe" {
			t.Errorf("unpacked %s = %q", path, got)
		}
		// A second fetch of the same pin downloads nothing.
		if _, err := f.Fetch(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(srv.uas) != 1 || srv.uas[0] != UserAgent {
			t.Errorf("requests' User-Agents = %q, want one ipcam-spike/0.1", srv.uas)
		}
	})
	t.Run("tar.xz seam", func(t *testing.T) {
		body := []byte("a .tar.xz archive")
		srv := serveArchive(t, body)
		dir := t.TempDir()
		var called []string
		f := Fetcher{Dir: dir, Pin: Pin{URL: srv.URL + "/ffmpeg-linuxarm64-lgpl-9.0.tar.xz", SHA256: sum(body)},
			Untar: func(_ context.Context, archive, into string) error {
				got, _ := os.ReadFile(archive) //nolint:gosec // the test's archive
				called = append(called, string(got))
				bin := filepath.Join(into, "ffmpeg-n9.0-linuxarm64-lgpl-9.0", "bin")
				if err := os.MkdirAll(bin, 0o700); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("arm64-binary"), 0o600)
			}}
		path, err := f.Fetch(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(called) != 1 || called[0] != string(body) {
			t.Errorf("the tar seam got %q", called)
		}
		if got := readFile(path); string(got) != "arm64-binary" || filepath.Base(path) != "ffmpeg" {
			t.Errorf("unpacked %s = %q", path, got)
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "ffmpeg-unpack-") || strings.HasPrefix(e.Name(), "ffmpeg-archive-") {
				t.Errorf("left behind: %s", e.Name())
			}
		}
	})
	t.Run("tar.xz with the system tar", func(t *testing.T) {
		if _, err := exec.LookPath("tar"); err != nil {
			t.Skip("no tar")
		}
		if _, err := exec.LookPath("xz"); err != nil {
			t.Skip("no xz")
		}
		src := t.TempDir()
		bin := filepath.Join(src, "ffmpeg-n9.0-linuxarm64-lgpl-9.0", "bin")
		if err := os.MkdirAll(bin, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("real-tar"), 0o600); err != nil {
			t.Fatal(err)
		}
		archive := filepath.Join(t.TempDir(), "a.tar.xz")
		if out, err := exec.Command("tar", "-cJf", archive, "-C", src, ".").CombinedOutput(); err != nil { //nolint:gosec // the test's own files
			t.Skipf("tar -cJf: %v %s", err, out)
		}
		body, err := os.ReadFile(archive) //nolint:gosec // the test's archive
		if err != nil {
			t.Fatal(err)
		}
		srv := serveArchive(t, body)
		f := Fetcher{Dir: t.TempDir(), Pin: Pin{URL: srv.URL + "/x-linuxarm64-lgpl-9.0.tar.xz", SHA256: sum(body)}}
		path, err := f.Fetch(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if got := readFile(path); string(got) != "real-tar" {
			t.Errorf("unpacked %q", got)
		}
	})
	t.Run("pins", func(t *testing.T) {
		for _, p := range []struct{ goos, goarch string }{{"windows", "amd64"}, {"linux", "arm64"}} {
			pin, ok := PinFor(p.goos, p.goarch)
			if !ok || !strings.HasPrefix(pin.URL, "https://") || len(pin.SHA256) != 64 || !strings.Contains(pin.URL, PinTag) ||
				!strings.Contains(pin.URL, "-lgpl-") {
				t.Errorf("PinFor(%s/%s) = %+v, %v", p.goos, p.goarch, pin, ok)
			}
		}
	})
}

// readFile reads an unpacked file, nil when it cannot.
func readFile(path string) []byte {
	b, _ := os.ReadFile(path) //nolint:gosec // the test's unpacked file
	return b
}
