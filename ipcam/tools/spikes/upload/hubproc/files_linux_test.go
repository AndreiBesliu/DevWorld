package hubproc

import (
	"errors"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// seamUID is the hub identity's uid and gid in these tests: nobody's, a
// non-root uid.
const seamUID = 65534

// asSpikeUser runs the test as root with the seam's identity as the hub's:
// it skips without root, and fails instead when UPLOADSPIKE_REQUIRE_ROOT is
// "1", as the upload job's root step sets it. With missing set, the user
// does not exist.
func asSpikeUser(t *testing.T, missing bool) {
	t.Helper()
	if os.Geteuid() != 0 {
		if os.Getenv("UPLOADSPIKE_REQUIRE_ROOT") == "1" {
			t.Fatal("UPLOADSPIKE_REQUIRE_ROOT=1, but the test does not run as root")
		}
		t.Skip("needs root: the files are given to another uid")
	}
	saved := lookupSpikeUser
	lookupSpikeUser = func(name string) (int, int, error) {
		if missing || name != SpikeUser {
			return 0, 0, user.UnknownUserError(name)
		}
		return seamUID, seamUID, nil
	}
	t.Cleanup(func() { lookupSpikeUser, betweenLstatAndOpen = saved, nil })
}

// ownerOf is the uid of the file at p, not following a link.
func ownerOf(t *testing.T, p string) (uint32, fs.FileMode) {
	t.Helper()
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Uid, info.Mode() //nolint:forcetypeassert // Linux's
}

// spikeConfig is a start in base with run.json and an identity file.
func spikeConfig(base string) Config {
	return Config{Bin: "/nonexistent", RunJSON: filepath.Join(base, "hub", "spike", "run.json"), RunAsUser: SpikeUser,
		Files: map[string][]byte{"run.json": []byte(`{"jobs":[]}`), "identity.txt": []byte("AGE-SECRET-KEY-stand-in")}}
}

// TestHubproc_LinuxFilesOwnedBySpikeUser (Files for the hub): run.json and
// the identity file are in <base>/hub/spike/, 0600 and the seam's uid's;
// every directory from <base>/hub down to them is the seam's uid's with
// search permission, so the hub can open the files, not only own them;
// the files that a root-run diag left under <dataDir>/state/ (catalog.db's
// -wal and -shm, the inner hub.lock, hub_id) are the seam's uid's again
// before the start. A missing user gives ErrNoSpikeUser.
func TestHubproc_LinuxFilesOwnedBySpikeUser(t *testing.T) {
	asSpikeUser(t, false)
	base := t.TempDir()
	state := filepath.Join(base, "hub", "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"catalog.db-wal", "catalog.db-shm", "hub.lock", "hub_id"} {
		if err := os.WriteFile(filepath.Join(state, name), []byte("root's"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := spikeConfig(base)
	owner, err := spikeOwner(cfg.RunAsUser)
	if err != nil || owner == nil || owner.uid != seamUID || !owner.switchID {
		t.Fatalf("spikeOwner = %+v, %v; want the seam's uid", owner, err)
	}
	if err := prepareFiles(cfg, owner); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"run.json", "identity.txt"} {
		p := filepath.Join(base, "hub", "spike", name)
		uid, mode := ownerOf(t, p)
		if uid != seamUID || mode != 0o600 {
			t.Errorf("%s: uid %d, mode %v; want %d, 0600", name, uid, mode, seamUID)
		}
		if _, err := os.Stat(p + ".tmp"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s.tmp is left: %v", name, err)
		}
	}
	for _, dir := range []string{filepath.Join(base, "hub"), filepath.Join(base, "hub", "spike"), state} {
		uid, mode := ownerOf(t, dir)
		if uid != seamUID || !mode.IsDir() || mode.Perm()&0o100 == 0 {
			t.Errorf("%s: uid %d, mode %v; want the seam's uid's with search permission", dir, uid, mode)
		}
	}
	for _, name := range []string{"catalog.db-wal", "catalog.db-shm", "hub.lock", "hub_id"} {
		if uid, _ := ownerOf(t, filepath.Join(state, name)); uid != seamUID {
			t.Errorf("state/%s: uid %d; want %d", name, uid, seamUID)
		}
	}
	asSpikeUser(t, true)
	if _, err := Start(t.Context(), spikeConfig(t.TempDir())); !errors.Is(err, ErrNoSpikeUser) {
		t.Errorf("Start without the user: %v; want ErrNoSpikeUser", err)
	}
	if ErrNoSpikeUser.Error() != "Rulați întâi sudo ./uploadspike install." {
		t.Errorf("ErrNoSpikeUser reads %q", ErrNoSpikeUser)
	}
}

// TestHubproc_PlantedLinksNeverFollowed (Files for the hub): a process of
// the hub's uid may plant links under <base>/hub. One case at a time: a
// symlink at hub/spike/run.json or at hub/spike/run.json.tmp, or at
// hub/state/catalog.db-wal, or in place of hub/spike, pointing outside
// <base>/hub; a hard link at hub/state/catalog.db-shm; a regular
// catalog.db-shm swapped for a hard link to an outside file between Lstat
// and the open. Every outside file keeps its bytes, mode and owner; in the
// first two cases the start goes on with a regular 0600 run.json, in the
// others prepareFiles fails with an error naming the path.
func TestHubproc_PlantedLinksNeverFollowed(t *testing.T) {
	asSpikeUser(t, false)
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, hub, outsideFile, outsideDir string)
		fails string // the path that the error names, relative to hub/; "" when the start goes on
	}{
		{"symlink at run.json", func(t *testing.T, hub, file, _ string) {
			symlink(t, file, filepath.Join(hub, "spike", "run.json"))
		}, ""},
		{"symlink at run.json.tmp", func(t *testing.T, hub, file, _ string) {
			symlink(t, file, filepath.Join(hub, "spike", "run.json.tmp"))
		}, ""},
		{"symlink at catalog.db-wal", func(t *testing.T, hub, file, _ string) {
			symlink(t, file, filepath.Join(hub, "state", "catalog.db-wal"))
		}, "state/catalog.db-wal"},
		{"symlink in place of spike", func(t *testing.T, hub, _, dir string) {
			if err := os.Remove(filepath.Join(hub, "spike")); err != nil {
				t.Fatal(err)
			}
			symlink(t, dir, filepath.Join(hub, "spike"))
		}, "spike"},
		{"hard link at catalog.db-shm", func(t *testing.T, hub, file, _ string) {
			if err := os.Link(file, filepath.Join(hub, "state", "catalog.db-shm")); err != nil {
				t.Fatal(err)
			}
		}, "state/catalog.db-shm"},
		{"catalog.db-shm swapped for a hard link after Lstat", func(t *testing.T, hub, file, _ string) {
			shm := filepath.Join(hub, "state", "catalog.db-shm")
			if err := os.WriteFile(shm, []byte("shm"), 0o600); err != nil {
				t.Fatal(err)
			}
			betweenLstatAndOpen = func(rel string) {
				if rel != filepath.Join("state", "catalog.db-shm") {
					return
				}
				if err := os.Remove(shm); err != nil {
					t.Error(err)
				}
				if err := os.Link(file, shm); err != nil {
					t.Error(err)
				}
			}
		}, "state/catalog.db-shm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { betweenLstatAndOpen = nil })
			base := t.TempDir()
			hub := filepath.Join(base, "hub")
			for _, d := range []string{filepath.Join(hub, "spike"), filepath.Join(hub, "state")} {
				if err := os.MkdirAll(d, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			outside := t.TempDir()
			file := filepath.Join(outside, "victim")
			if err := os.WriteFile(file, []byte("outside"), 0o644); err != nil { //nolint:gosec // G306: the mode under test
				t.Fatal(err)
			}
			if err := os.Chmod(file, 0o644); err != nil { //nolint:gosec // G302: the mode under test
				t.Fatal(err)
			}
			dir := filepath.Join(outside, "dir")
			if err := os.Mkdir(dir, 0o755); err != nil { //nolint:gosec // G301: the mode under test
				t.Fatal(err)
			}
			tc.plant(t, hub, file, dir)
			err := prepareFiles(spikeConfig(base), &fileOwner{uid: seamUID, gid: seamUID, switchID: true})
			if tc.fails == "" {
				if err != nil {
					t.Fatalf("prepareFiles: %v; want the start to go on", err)
				}
				uid, mode := ownerOf(t, filepath.Join(hub, "spike", "run.json"))
				if uid != seamUID || mode != 0o600 {
					t.Errorf("run.json: uid %d, mode %v; want a regular 0600 file of %d", uid, mode, seamUID)
				}
			} else if err == nil || !strings.Contains(err.Error(), filepath.Join(hub, tc.fails)) {
				t.Errorf("prepareFiles: %v; want an error naming %s", err, filepath.Join(hub, tc.fails))
			}
			if b, err := os.ReadFile(file); err != nil || string(b) != "outside" { //nolint:gosec // the test's own file
				t.Errorf("the outside file: %q, %v; want its bytes", b, err)
			}
			if uid, mode := ownerOf(t, file); uid != 0 || mode != 0o644 {
				t.Errorf("the outside file: uid %d, mode %v; want root's, 0644", uid, mode)
			}
			if uid, mode := ownerOf(t, dir); uid != 0 || mode.Perm() != 0o755 {
				t.Errorf("the outside folder: uid %d, mode %v; want root's, 0755", uid, mode)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("the outside folder holds %v", entries)
			}
		})
	}
}

// symlink makes link point at target.
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
