package hub

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// spikeOnlyMarker is the marker that every spike source file carries. It is
// built from two parts so that this file does not contain it.
const spikeOnlyMarker = "SPIKE" + "-ONLY"

// TestNoSpikeOnlyMarker keeps spike code out of the production module: no
// file under hub/, the vendored MediaMTX fork included, may carry the marker.
func TestNoSpikeOnlyMarker(t *testing.T) {
	var marked []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := os.ReadFile(path) //nolint:gosec // path comes from walking the module directory
		if err != nil {
			return err
		}
		if bytes.Contains(content, []byte(spikeOnlyMarker)) {
			marked = append(marked, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk hub/: %v", err)
	}
	for _, path := range marked {
		t.Errorf("%s contains %s; spike code does not belong in hub/", path, spikeOnlyMarker)
	}
}
