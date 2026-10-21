package health

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeSys builds the part of /sys that Storage reads: each disk's queue
// files, a partition directory under it, and /sys/dev/block/<major:minor>
// links to the partition, as the kernel lays them out.
func fakeSys(t *testing.T) string {
	t.Helper()
	sys := t.TempDir()
	write := func(rel, data string) {
		t.Helper()
		path := filepath.Join(sys, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := func(devnum, target string) {
		t.Helper()
		dir := filepath.Join(sys, "dev", "block")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.FromSlash("../../"+target), filepath.Join(dir, devnum)); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []struct{ disk, part, devnum, rotational, writeCache string }{
		{"devices/platform/emmc2bus/fe340000.mmc/mmc_host/mmc0/mmc0:aaaa/block/mmcblk0", "mmcblk0p2", "179:2", "0", "write through"},
		{"devices/platform/scb/fd500000.pcie/usb2/2-1/host0/target0:0:0/0:0:0:0/block/sda", "sda1", "8:1", "1", "write back"},
		{"devices/platform/axi/1000110000.pcie/nvme/nvme0/nvme0n1", "nvme0n1p2", "259:2", "0", "write back"},
	} {
		write(d.disk+"/queue/rotational", d.rotational+"\n")
		write(d.disk+"/queue/write_cache", d.writeCache+"\n")
		write(d.disk+"/"+d.part+"/partition", "2\n")
		link(d.devnum, d.disk+"/"+d.part)
	}
	return sys
}

// TestStorage_ReadsTheDiskUnderRec (G09, §4.8): the storage under rec/ is
// "sd" on a card (T40); otherwise its disk's queue/rotational says "hdd"
// (1) or "ssd" (0). Its queue/write_cache says "on" ("write back") or
// "off" ("write through"). A mount without a block device, such as tmpfs,
// is "unknown" for both.
func TestStorage_ReadsTheDiskUnderRec(t *testing.T) {
	sys := fakeSys(t)
	for _, tc := range []struct {
		name, mountinfo, recDir string
		kind, writeCache        string
	}{
		{"SD card", "mountinfo-pi-sd", "/home/pi/ipcam/data/rec", "sd", "off"},
		{"USB disk", "mountinfo-pi-sd", "/mnt/ssd/ipcam/data/rec", "hdd", "on"},
		{"NVMe", "mountinfo-nvme", "/home/pi/ipcam/data/rec", "ssd", "on"},
		{"tmpfs", "mountinfo-nvme", "/tmp/ipcam/data/rec", "unknown", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Roots{MountInfo: filepath.Join("testdata", tc.mountinfo), Sys: sys}
			if kind, writeCache := Storage(r, tc.recDir); kind != tc.kind || writeCache != tc.writeCache {
				t.Errorf("Storage(%s, %s) = %s, %s; want %s, %s", tc.mountinfo, tc.recDir, kind, writeCache, tc.kind, tc.writeCache)
			}
		})
	}
	t.Run("no mount table", func(t *testing.T) {
		r := Roots{MountInfo: filepath.Join(t.TempDir(), "mountinfo"), Sys: sys}
		if kind, writeCache := Storage(r, "/home/pi/ipcam/data/rec"); kind != "unknown" || writeCache != "unknown" {
			t.Errorf("Storage = %s, %s; want unknown, unknown", kind, writeCache)
		}
	})
	t.Run("no sysfs", func(t *testing.T) {
		r := Roots{MountInfo: filepath.Join("testdata", "mountinfo-pi-sd"), Sys: t.TempDir()}
		if kind, writeCache := Storage(r, "/home/pi/ipcam/data/rec"); kind != "sd" || writeCache != "unknown" {
			t.Errorf("Storage = %s, %s; want sd from the mount table, and an unknown write cache", kind, writeCache)
		}
	})
}
