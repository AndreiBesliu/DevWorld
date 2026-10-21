package health

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Storage reports the storage under recDir and its write cache (G09):
//
//   - kind is "sd" when IsSdCard says so; otherwise the disk's
//     queue/rotational gives "hdd" (1) or "ssd" (0);
//   - writeCache is "on" when the disk's queue/write_cache says "write
//     back", "off" for "write through".
//
// The disk is the one that holds the mount of recDir: r.Sys's
// dev/block/<major>:<minor> resolved, or its parent for a partition, which
// is the directory that /sys/block/<disk> names. Each is "unknown" when its
// file cannot be read, as for tmpfs or overlay, which have no block device.
func Storage(r Roots, recDir string) (kind, writeCache string) {
	m, ok := mountOf(r.MountInfo, recDir)
	if !ok {
		return unknown, unknown
	}
	disk := diskDir(r.Sys, m)
	writeCache = unknown
	switch readSysfs(disk, "queue", "write_cache") {
	case "write back":
		writeCache = "on"
	case "write through":
		writeCache = "off"
	}
	if m.sdCard() {
		return "sd", writeCache
	}
	switch readSysfs(disk, "queue", "rotational") {
	case "1":
		return "hdd", writeCache
	case "0":
		return "ssd", writeCache
	}
	return unknown, writeCache
}

// diskDir returns the sysfs directory of the disk that holds m, or "" when
// sys has none.
func diskDir(sys string, m mount) string {
	dir, err := filepath.EvalSymlinks(filepath.Join(sys, "dev", "block", strconv.FormatUint(m.major, 10)+":"+strconv.FormatUint(m.minor, 10)))
	if err != nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, "partition")); err == nil {
		return filepath.Dir(dir)
	}
	return dir
}

// readSysfs returns the trimmed contents of a sysfs file under dir, or ""
// when there is none.
func readSysfs(dir string, elem ...string) string {
	if dir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(append([]string{dir}, elem...)...)) //nolint:gosec // a file of /sys, or of a test's fixture
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
