package hostinfo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/internal/spiketest"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// pi is a Pi of the model raw and tier, with diag pimodel's numbers.
func pi(raw, tier string, maxCams int) *results.PiModelInfo {
	return &results.PiModelInfo{Raw: raw, Tier: tier, Known: true, Supported: tier != "unsupported", RecommendedMin: maxCams, RecommendedMax: maxCams}
}

// TestHost_D2Recommendation (C2-R2): B for a Pi 4, 5, 400 or 500 on arm64;
// A for a Pi 4 on armv7, a Pi 3, an unknown host and a PC; an armv6 Pi is
// A and says the Pi cannot run the hub.
func TestHost_D2Recommendation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		host   Host
		letter string
		says   string
	}{
		{"Pi 4 on arm64", Host{Arch: "arm64", PiModel: pi("Raspberry Pi 4 Model B Rev 1.5", "H1b", 4)}, "B", "64 de biți"},
		{"Pi 5 on arm64", Host{Arch: "arm64", PiModel: pi("Raspberry Pi 5 Model B Rev 1.0", "H1", 8)}, "B", "D2-B"},
		{"Pi 400 on arm64", Host{Arch: "arm64", PiModel: pi("Raspberry Pi 400 Rev 1.0", "H1b", 4)}, "B", "D2-B"},
		{"Pi 500 on arm64", Host{Arch: "arm64", PiModel: pi("Raspberry Pi 500 Rev 1.0", "H1", 8)}, "B", "D2-B"},
		{"Pi 4 on armv7", Host{Arch: "arm", PiModel: pi("Raspberry Pi 4 Model B Rev 1.5", "H1b", 4)}, "A", "32 de biți"},
		{"Pi 3", Host{Arch: "arm64", PiModel: pi("Raspberry Pi 3 Model B Rev 1.2", "H1c", 2)}, "A", "prea slab"},
		{"unknown", Host{Arch: "arm64", PiModel: &results.PiModelInfo{Raw: "Some Board", Tier: "unknown"}}, "A", "nu este un Raspberry Pi"},
		{"a PC", Host{OS: "windows", Arch: "amd64"}, "A", "D2-A"},
		{"armv6 Pi", Host{Arch: "arm", PiModel: pi("Raspberry Pi Model B Rev 2", "unsupported", 0)}, "A", "Pi-ul nu poate rula hub-ul"},
	} {
		letter, reason := RecommendD2(tc.host)
		if letter != tc.letter || !strings.Contains(reason, tc.says) {
			t.Errorf("%s: %s, %q; want %s and a reason with %q", tc.name, letter, reason, tc.letter, tc.says)
		}
	}
}

// TestHost_UnknownModelUsesDeviceTree (c1 Task 7a): ipcam-hub's diag host
// on a host without a cpuinfo model, and a device-tree model of a Pi 4,
// give tier H1b, RecommendedMax 4 and Source "device-tree", through the
// real ipcam-hub's diag pimodel; with no device-tree model, the host is no
// Pi.
func TestHost_UnknownModelUsesDeviceTree(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux has a device tree")
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil && strings.Contains(string(b), "Raspberry Pi") {
		t.Skip("this host names a Pi model in cpuinfo")
	}
	dir := t.TempDir()
	hub := spiketest.BuildBinary(t, dir, "ipcam-hub", "github.com/AndreiBesliu/DevWorld/ipcam/hub/cmd/ipcam-hub")
	model := filepath.Join(dir, "model")
	if err := os.WriteFile(model, []byte("Raspberry Pi 4 Model B Rev 1.4\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	deviceTreeModelPath = model
	t.Cleanup(func() { deviceTreeModelPath = "/proc/device-tree/model" })
	h, err := Read(t.Context(), hub, dir)
	if err != nil {
		t.Fatal(err)
	}
	m := h.PiModel
	if m == nil || m.Tier != "H1b" || m.RecommendedMax != 4 || m.Source != "device-tree" || h.RecommendedCameras != 4 {
		t.Fatalf("Read = %+v, model %+v; want H1b, max 4, from the device tree", h, m)
	}
	deviceTreeModelPath = filepath.Join(dir, "missing")
	if h, err := Read(t.Context(), hub, dir); err != nil || h.PiModel != nil || h.Storage == "" {
		t.Errorf("no device-tree model: %+v, %v; want no Pi and a storage", h, err)
	}
}
