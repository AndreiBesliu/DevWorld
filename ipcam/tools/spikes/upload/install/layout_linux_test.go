package install

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// owners are the test's owners: as root, root and nobody (65534); else
// this user for both (the non-root uid seam).
func owners() Owners {
	if os.Geteuid() == 0 {
		return Owners{SpikeUID: 65534, SpikeGID: 65534}
	}
	return Owners{RootUID: os.Getuid(), RootGID: os.Getgid(), SpikeUID: os.Getuid(), SpikeGID: os.Getgid()}
}

// ownerMode is a path's uid and mode, not following a link.
func ownerMode(t *testing.T, p string) (int, fs.FileMode) {
	t.Helper()
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return int(info.Sys().(*syscall.Stat_t).Uid), info.Mode() //nolint:forcetypeassert // Linux
}

// TestInstall_LinuxLayout (P49): /opt/ipcam-spike root-owned 0755 with the
// binaries; /var/lib/ipcam-spike/hub and hub/spike 0700 owned by the
// hub's identity; state/ and results/ root-owned 0700; a symlink planted
// in place of hub/spike before a second install is refused, and its
// target keeps its owner and mode.
func TestInstall_LinuxLayout(t *testing.T) {
	prefix, src := t.TempDir(), t.TempDir()
	for _, name := range []string{"uploadspike", "ipcam-hub", "mediamtx"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte("#!"+name), 0o700); err != nil { //nolint:gosec // G306: a test binary
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(src, "media"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "media", "sub.h264"), []byte("h264"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := owners()
	b := Bundle{SrcDir: src, Binaries: []string{"uploadspike", "ipcam-hub", "mediamtx", "reccontroller"}, Media: "media"}
	if err := Layout(prefix, b, o); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		uid  int
		mode fs.FileMode
	}{
		{OptDir, o.RootUID, fs.ModeDir | 0o755},
		{OptDir + "/ipcam-hub", o.RootUID, 0o755},
		{OptDir + "/media/sub.h264", o.RootUID, 0o644},
		{VarDir, o.RootUID, fs.ModeDir | 0o755},
		{VarDir + "/hub", o.SpikeUID, fs.ModeDir | 0o700},
		{VarDir + "/hub/spike", o.SpikeUID, fs.ModeDir | 0o700},
		{VarDir + "/state", o.RootUID, fs.ModeDir | 0o700},
		{VarDir + "/results", o.RootUID, fs.ModeDir | 0o700},
	} {
		if uid, mode := ownerMode(t, filepath.Join(prefix, tc.path)); uid != tc.uid || mode != tc.mode {
			t.Errorf("%s: uid %d, mode %v; want %d, %v", tc.path, uid, mode, tc.uid, tc.mode)
		}
	}
	// A symlink in place of hub/spike: refused, its target untouched.
	target := t.TempDir()
	if err := os.Chmod(target, 0o711); err != nil { //nolint:gosec // G302: a folder with a mode to keep
		t.Fatal(err)
	}
	tuid, tmode := ownerMode(t, target)
	spike := filepath.Join(prefix, VarDir, "hub", "spike")
	if err := os.Remove(spike); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, spike); err != nil {
		t.Fatal(err)
	}
	err := Layout(prefix, b, o)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("a second install over a planted link: %v", err)
	}
	if uid, mode := ownerMode(t, target); uid != tuid || mode != tmode {
		t.Errorf("the link's target became uid %d, %v; was %d, %v", uid, mode, tuid, tmode)
	}
}
