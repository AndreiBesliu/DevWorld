//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/oauth"
)

// TestDiagRootCommands_FollowNoLinkAndNeverBlock (security review SEAL-1):
// on the Pi, root runs diag scan-secrets and diag visibility on a data
// directory and a run.json that the hub's identity owns, so neither
// follows a link as run.json, state/, catalog.db or hub_id, nor blocks on a
// FIFO in their place: each is refused at once, and nothing is removed.
func TestDiagRootCommands_FollowNoLinkAndNeverBlock(t *testing.T) {
	type plant func(t *testing.T, f *oauthFixture)
	moveAside := func(t *testing.T, path string) string {
		t.Helper()
		aside := filepath.Join(t.TempDir(), filepath.Base(path))
		if err := os.Rename(path, aside); err != nil {
			t.Fatal(err)
		}
		return aside
	}
	fifo := func(rel func(f *oauthFixture) string) plant {
		return func(t *testing.T, f *oauthFixture) {
			path := rel(f)
			moveAside(t, path)
			if err := unix.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				// Free a reader that the command left blocked in open(2).
				if w, err := os.OpenFile(path, os.O_WRONLY|unix.O_NONBLOCK, 0); err == nil { //nolint:gosec // G304: the test's FIFO
					_ = w.Close()
				}
			})
		}
	}
	link := func(rel func(f *oauthFixture) string) plant {
		return func(t *testing.T, f *oauthFixture) {
			path := rel(f)
			if err := os.Symlink(moveAside(t, path), path); err != nil {
				t.Fatal(err)
			}
		}
	}
	config := func(f *oauthFixture) string { return f.config }
	state := func(f *oauthFixture) string { return filepath.Join(f.dataDir, "state") }
	catalogDB := func(f *oauthFixture) string { return filepath.Join(f.dataDir, "state", "catalog.db") }
	hubID := func(f *oauthFixture) string { return filepath.Join(f.dataDir, "state", "hub_id") }
	for _, tc := range []struct {
		name     string
		plant    plant
		commands []string
	}{
		{"run.json as a FIFO", fifo(config), []string{"scan-secrets", "visibility"}},
		{"run.json as a link", link(config), []string{"scan-secrets", "visibility"}},
		{"state as a link", link(state), []string{"scan-secrets", "visibility"}},
		{"catalog.db as a FIFO", fifo(catalogDB), []string{"scan-secrets", "visibility"}},
		{"catalog.db as a link", link(catalogDB), []string{"scan-secrets", "visibility"}},
		{"hub_id as a FIFO", fifo(hubID), []string{"visibility"}},
		{"hub_id as a link", link(hubID), []string{"visibility"}},
	} {
		for _, command := range tc.commands {
			t.Run(tc.name+"/"+command, func(t *testing.T) {
				f := newOAuthFixture(t)
				f.logIn(t, "g-windows", oauth.PlatformWindows, "acct-1")
				f.logIn(t, "g-android", oauth.PlatformAndroid, "acct-1")
				dir := t.TempDir()
				leak := filepath.Join(dir, "leak.json")
				if err := os.WriteFile(leak, []byte(secretDesktop), 0o600); err != nil {
					t.Fatal(err)
				}
				tc.plant(t, f)
				args := []string{"--config", f.config, "--dir", dir}
				if command == "visibility" {
					args = []string{"--config", f.config, "--creator", "g-windows", "--reader", "g-android"}
				}
				type result struct {
					code   int
					stderr string
				}
				done := make(chan result, 1)
				go func() {
					var stdout, stderr strings.Builder
					code := diagCommand(append([]string{command}, args...), strings.NewReader(""), &stdout, &stderr)
					done <- result{code, stderr.String()}
				}()
				select {
				case r := <-done:
					if r.code == exitOK {
						t.Errorf("diag %s = exit 0; want it refused (%s)", command, r.stderr)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("diag %s blocked for 5 s", command)
				}
				if _, err := os.Stat(leak); err != nil {
					t.Errorf("a file was removed (%v)", err)
				}
			})
		}
	}
}
