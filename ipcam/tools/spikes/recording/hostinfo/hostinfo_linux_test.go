package hostinfo

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// TestHostInfo_DeviceTreeFallback (G32): with no Model line in cpuinfo,
// the board's device-tree model goes to `ipcam-hub diag pimodel --raw`,
// whose row of §4.8 Read reports with Source "device-tree" and cpuinfo's
// revision; a device-tree model that the table does not know still asks the
// owner (C24).
func TestHostInfo_DeviceTreeFallback(t *testing.T) {
	tree := filepath.Join(t.TempDir(), "model")
	if err := os.WriteFile(tree, []byte("Raspberry Pi 4 Model B Rev 1.5\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := fake(t, noModelHost)
	t.Setenv(fakePimodel, pi4Pimodel)
	rec := t.TempDir()
	var out strings.Builder
	info, err := Read(t.Context(), Options{Hub: os.Args[0], RecDir: rec, DeviceTree: tree, In: bufio.NewReader(strings.NewReader("7\n")), Out: &out})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := &results.PiModelInfo{Raw: "Raspberry Pi 4 Model B Rev 1.5", Revision: "d03115", Tier: "H1b", Source: "device-tree", Supported: true, Known: true, RecommendedMin: 4, RecommendedMax: 4}
	if !reflect.DeepEqual(info.PiModel, want) || info.RecommendedCameras != 4 {
		t.Errorf("Read = %+v (model %+v), want %+v and 4 cameras", info, info.PiModel, want)
	}
	if got := calls(t, args); !reflect.DeepEqual(got, [][]string{{"diag", "host", "--rec", rec}, {"diag", "pimodel", "--raw", "Raspberry Pi 4 Model B Rev 1.5"}}) {
		t.Errorf("ipcam-hub ran with %q", got)
	}
	if out.Len() != 0 {
		t.Errorf("Read asked %q for a known model", out.String())
	}

	t.Run("an unknown device-tree model", func(t *testing.T) {
		fake(t, noModelHost)
		t.Setenv(fakePimodel, unknownPimodel)
		var out strings.Builder
		info, err := Read(t.Context(), Options{Hub: os.Args[0], RecDir: rec, DeviceTree: tree, In: bufio.NewReader(strings.NewReader("\n")), Out: &out})
		if err != nil {
			t.Fatal(err)
		}
		if m := info.PiModel; m == nil || m.Raw != "Acme Board 7" || m.Source != "device-tree" || !m.Estimate || m.RecommendedMax != 1 || info.RecommendedCameras != 1 {
			t.Errorf("Read = %+v (model %+v), want Acme Board 7 from the device tree and an estimate of 1", info, m)
		}
	})

	t.Run("a failing diag pimodel", func(t *testing.T) {
		fake(t, noModelHost)
		t.Setenv(fakePimodel, "not json")
		if _, err := Read(t.Context(), Options{Hub: os.Args[0], RecDir: rec, DeviceTree: tree}); err == nil {
			t.Error("Read = nil error")
		}
	})
}
