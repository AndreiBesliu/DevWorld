package health

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// TestStorage_FromTheVolume (G09): the volume under rec/ reports its seek
// penalty and its write cache, or "unknown", also for a directory that does
// not exist yet. An elevated process, such as GitHub's Windows runner,
// always knows the write cache. The runner's own values are logged.
func TestStorage_FromTheVolume(t *testing.T) {
	elevated := windows.GetCurrentProcessToken().IsElevated()
	for _, dir := range []string{t.TempDir(), filepath.Join(t.TempDir(), "data", "rec")} {
		kind, writeCache := Storage(Roots{}, dir)
		t.Logf("storage %s, write cache %s (elevated: %v)", kind, writeCache, elevated)
		if kind != "ssd" && kind != "hdd" && kind != "unknown" {
			t.Errorf("Storage kind %q", kind)
		}
		if writeCache != "on" && writeCache != "off" && writeCache != "unknown" {
			t.Errorf("Storage write cache %q", writeCache)
		}
		if elevated && writeCache == unknown {
			t.Errorf("Storage(%s): write cache unknown in an elevated process", dir)
		}
	}
}

// TestStorage_WriteCacheWithoutAccessRights (G09): the owner's unelevated
// diag host cannot open the disk for reading, which
// IOCTL_DISK_GET_CACHE_INFORMATION needs; StorageDeviceWriteCacheProperty,
// which needs no access right, still tells the write cache.
func TestStorage_WriteCacheWithoutAccessRights(t *testing.T) {
	disk, err := diskOf(t.TempDir())
	if err != nil {
		t.Fatalf("diskOf: %v", err)
	}
	got := writeCacheProperty(disk)
	t.Logf("%s: write cache %s by its property, %s by IOCTL_DISK_GET_CACHE_INFORMATION", disk, got, cacheInformation(disk))
	if got != "on" && got != "off" {
		t.Errorf("writeCacheProperty(%s) = %q, want on or off", disk, got)
	}
}

// TestStorage_WriteCachePropertyStates: STORAGE_WRITE_CACHE_PROPERTY's
// WriteCacheEnabled reads as on (WriteCacheEnabled), off
// (WriteCacheDisabled) or unknown (WriteCacheEnableUnknown); an answer too
// short to hold it is unknown.
func TestStorage_WriteCachePropertyStates(t *testing.T) {
	full := uint32(28)
	for _, tc := range []struct {
		enabled, returned uint32
		want              string
	}{
		{2, full, "on"},
		{1, full, "off"},
		{0, full, "unknown"},
		{2, 12, "unknown"},
		{7, full, "unknown"},
	} {
		if got := writeCacheOf(storageWriteCacheProperty{WriteCacheEnabled: tc.enabled}, tc.returned); got != tc.want {
			t.Errorf("WriteCacheEnabled %d in %d bytes = %q, want %q", tc.enabled, tc.returned, got, tc.want)
		}
	}
}
