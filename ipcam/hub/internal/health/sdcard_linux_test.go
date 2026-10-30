package health

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsSdCard_DetectsMmcblk (T40, HUB-17): rec/ is on an SD card when the
// mount that holds it, the longest mount point that prefixes it, comes from
// /dev/mmcblk* or has major 179. An NVMe or USB disk (major 259 or 8) is no
// card; a bind mount counts as the device it comes from, in either
// direction; a mount point holds only the paths under it, not those that
// merely share its first letters.
func TestIsSdCard_DetectsMmcblk(t *testing.T) {
	fixturePaths(t)
	for _, tc := range []struct {
		name, mountinfo, recDir string
		want                    bool
	}{
		{"mmcblk0p2 root, major 179", "mountinfo-pi-sd", "/home/pi/ipcam/data/rec", true},
		{"/dev/root, major 179", "mountinfo-pi-devroot", "/home/pi/ipcam/data/rec", true},
		{"nvme0n1p2, major 259", "mountinfo-nvme", "/home/pi/ipcam/data/rec", false},
		{"USB SSD over the card's root", "mountinfo-pi-sd", "/mnt/ssd/ipcam/data/rec", false},
		{"bind mount of the SSD", "mountinfo-pi-sd", "/srv/ipcam/data/rec", false},
		{"bind mount of the card under the SSD", "mountinfo-pi-sd", "/mnt/ssd/card/data/rec", true},
		{"a sibling of a mount point", "mountinfo-pi-sd", "/mnt/ssdx/data/rec", true},
		{"a mount point with a space", "mountinfo-pi-sd", "/mnt/my disk/data/rec", false},
		{"a relative recDir on the card's root", "mountinfo-pi-sd", "data/rec", true},
		{"a relative recDir that leads to the SSD", "mountinfo-pi-sd", "../../../mnt/ssd/rec", false},
		{"the mount point itself", "mountinfo-pi-sd", "/mnt/ssd", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSdCard(filepath.Join("testdata", tc.mountinfo), tc.recDir); got != tc.want {
				t.Errorf("IsSdCard(%s, %s) = %v, want %v", tc.mountinfo, tc.recDir, got, tc.want)
			}
		})
	}
}

// fixtureWorkDir is where fixturePaths resolves a relative path: ipcam-hub's
// folder on the fixtures' Raspberry Pi.
const fixtureWorkDir = "/home/pi/ipcam"

// fixturePaths makes the test resolve rec/ as the fixtures' host would,
// without the test host's directories: an absolute path stays as it is,
// and a relative one lies under fixtureWorkDir.
func fixturePaths(t *testing.T) {
	t.Helper()
	previous := resolvePath
	resolvePath = func(dir string) string {
		if filepath.IsAbs(dir) {
			return filepath.Clean(dir)
		}
		return filepath.Join(fixtureWorkDir, dir)
	}
	t.Cleanup(func() { resolvePath = previous })
}

// TestResolve_FollowsTheNearestExistingAncestorsSymlinks: rec/ need not
// exist yet; the symlinks of its nearest existing ancestor are resolved,
// and the rest is kept.
func TestResolve_FollowsTheNearestExistingAncestorsSymlinks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "ssd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "ssd"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if got, want := resolve(filepath.Join(root, "link", "data", "rec")), filepath.Join(root, "ssd", "data", "rec"); got != want {
		t.Errorf("resolve(link/data/rec) = %s, want %s", got, want)
	}
	t.Chdir(root)
	if got, want := resolve(filepath.Join("link", "rec")), filepath.Join(root, "ssd", "rec"); got != want {
		t.Errorf("resolve of a relative path = %s, want %s", got, want)
	}
}

// TestIsSdCard_UnreadableMountinfo: without a mount table there is no card
// to warn about, as on Windows.
func TestIsSdCard_UnreadableMountinfo(t *testing.T) {
	if IsSdCard(filepath.Join(t.TempDir(), "mountinfo"), "/home/pi/ipcam/data/rec") {
		t.Error("IsSdCard is true without a mount table")
	}
	if IsSdCard("", "/home/pi/ipcam/data/rec") {
		t.Error("IsSdCard is true for an empty mountinfo path")
	}
}

// TestIsReadOnly_MountOrSuperblock (T40, §13.1): the card monitor's
// read-only remount is the mount that holds rec/ read-only, by its own
// options (mount -o remount,ro) or by its superblock's, as after ext4's
// errors=remount-ro, which leaves the mount's own options rw.
func TestIsReadOnly_MountOrSuperblock(t *testing.T) {
	fixturePaths(t)
	for _, tc := range []struct {
		name, mountinfo, recDir string
		want                    bool
	}{
		{"the card's superblock remounted ro", "mountinfo-pi-sd-ro", "/home/pi/ipcam/data/rec", true},
		{"a mount ro by its own options", "mountinfo-pi-sd-ro", "/mnt/ssd/data/rec", true},
		{"a rw mount beside them", "mountinfo-pi-sd-ro", "/mnt/nvme/data/rec", false},
		{"the card rw", "mountinfo-pi-sd", "/home/pi/ipcam/data/rec", false},
		{"no mount table", "absent", "/home/pi/ipcam/data/rec", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsReadOnly(filepath.Join("testdata", tc.mountinfo), tc.recDir); got != tc.want {
				t.Errorf("IsReadOnly(%s, %s) = %v, want %v", tc.mountinfo, tc.recDir, got, tc.want)
			}
		})
	}
}
