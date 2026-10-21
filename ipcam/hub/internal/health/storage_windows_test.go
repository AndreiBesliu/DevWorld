package health

import (
	"path/filepath"
	"testing"
)

// TestStorage_FromTheVolume (G09): the volume under rec/ reports its seek
// penalty and its write cache, or "unknown", also for a directory that does
// not exist yet. The runner's own values are logged.
func TestStorage_FromTheVolume(t *testing.T) {
	for _, dir := range []string{t.TempDir(), filepath.Join(t.TempDir(), "data", "rec")} {
		kind, writeCache := Storage(Roots{}, dir)
		t.Logf("storage %s, write cache %s", kind, writeCache)
		if kind != "ssd" && kind != "hdd" && kind != "unknown" {
			t.Errorf("Storage kind %q", kind)
		}
		if writeCache != "on" && writeCache != "off" && writeCache != "unknown" {
			t.Errorf("Storage write cache %q", writeCache)
		}
	}
}
