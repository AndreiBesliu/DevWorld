package mi4

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
)

// TestMI4_RefusesWithoutRootOnLinux (G22): on Linux MI4 drops the page
// cache before its reads, which needs root: started without sudo, it
// refuses with its Romanian line before it reads the host or records
// anything, so no ipcam-hub starts, not even for diag host.
func TestMI4_RefusesWithoutRootOnLinux(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	hub := filepath.Join(dir, "ipcam-hub")
	if err := os.WriteFile(hub, []byte("#!/bin/sh\necho \"$@\" >> "+started+"\nexit 1\n"), 0o700); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	var out strings.Builder
	err := Run(t.Context(), Options{Controller: "192.0.2.1:7081", Hub: hub, Results: filepath.Join(dir, "results"), Work: work, Out: &out,
		Euid: func() int { return 1000 },
		Host: func(ctx context.Context) (mi1.HostState, error) {
			return mi1.OwnerHost(ctx, hub, work, "linux", nil, &out)
		}})
	if err == nil || err.Error() != "MI4 trebuie pornit cu sudo: ./recspike mi4 …" {
		t.Errorf("Run as uid 1000 = %v, want «MI4 trebuie pornit cu sudo: ./recspike mi4 …»", err)
	}
	if b, err := os.ReadFile(started); !errors.Is(err, fs.ErrNotExist) { //nolint:gosec // the test's own file
		t.Errorf("ipcam-hub started: %q", b)
	}
	if _, err := os.Stat(work); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the work folder was made: %v", err)
	}

	// As root the same Run gets as far as the host, through ipcam-hub.
	_ = Run(t.Context(), Options{Controller: "192.0.2.1:7081", Hub: hub, Results: filepath.Join(dir, "results"), Work: work, Out: &out,
		Euid: func() int { return 0 },
		Host: func(ctx context.Context) (mi1.HostState, error) {
			return mi1.OwnerHost(ctx, hub, work, "linux", nil, &out)
		}})
	if _, err := os.Stat(started); err != nil {
		t.Errorf("as root, ipcam-hub diag host did not start: %v", err)
	}
}
