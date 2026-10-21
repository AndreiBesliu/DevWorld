package health

import (
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
		{"the mount point itself", "mountinfo-pi-sd", "/mnt/ssd", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSdCard(filepath.Join("testdata", tc.mountinfo), tc.recDir); got != tc.want {
				t.Errorf("IsSdCard(%s, %s) = %v, want %v", tc.mountinfo, tc.recDir, got, tc.want)
			}
		})
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
