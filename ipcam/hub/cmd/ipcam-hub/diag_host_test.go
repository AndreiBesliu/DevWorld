package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/health"
)

// maxAnnotationLength is the most bytes a notice line may have after
// annotate.sh's escaping (C32).
const maxAnnotationLength = 3500

// annotationLength is the length of line once annotate.sh has escaped it:
// each "%" becomes "%25" (a single line has no line break to escape).
func annotationLength(line string) int { return len(line) + 2*strings.Count(line, "%") }

// setHostProbe makes diag host read probe instead of the host.
func setHostProbe(probe func(recDir string) hostFacts) (restore func()) {
	previous := hostProbe
	hostProbe = probe
	return func() { hostProbe = previous }
}

// diag runs ipcam-hub with args and returns its exit code and its stdout,
// which must be one line; stderr is logged.
func diag(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, nil, &stdout, &stderr)
	if stderr.Len() > 0 {
		t.Logf("stderr: %s", stderr.String())
	}
	out := stdout.String()
	if code == exitOK && (strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n")) {
		t.Fatalf("ipcam-hub %s printed %q, want one line", strings.Join(args, " "), out)
	}
	return code, strings.TrimSuffix(out, "\n")
}

// diagHost runs "ipcam-hub diag host" with args and decodes its object.
func diagHost(t *testing.T, args ...string) (map[string]any, string) {
	t.Helper()
	code, line := diag(t, append([]string{"diag", "host"}, args...)...)
	if code != exitOK {
		t.Fatalf("diag host exited %d", code)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("diag host printed %q: %v", line, err)
	}
	return got, line
}

// fixtureRoots are a Raspberry Pi 4 whose root is its SD card; /sys holds
// nothing, so the write cache is unknown.
func fixtureRoots(t *testing.T) health.Roots {
	testdata := filepath.Join("..", "..", "internal", "health", "testdata")
	return health.Roots{
		CPUInfo:   filepath.Join(testdata, "cpuinfo-pi4"),
		MemInfo:   filepath.Join(testdata, "meminfo-pi4"),
		MountInfo: filepath.Join(testdata, "mountinfo-pi-sd"),
		Sys:       t.TempDir(),
	}
}

// TestDiagHost_JSONShape (R07, G09, C24): diag host prints one JSON object
// with exactly the fields of ipcam.diag.host/1, each of its type, in a line
// that stays within a notice annotation once annotate.sh has escaped it
// (C32). With the fixtures of a Raspberry Pi 4 on its SD card, the values
// are the fixtures'; on Windows the readers ask the system, which the
// windows CI job also prints as a notice.
func TestDiagHost_JSONShape(t *testing.T) {
	roots := fixtureRoots(t)
	defer setHostProbe(func(recDir string) hostFacts { return probeHost(roots, recDir) })()
	got, line := diagHost(t, "--rec", "/home/pi/ipcam/data/rec")

	keys := []string{"schema", "os", "arch", "physicalCores", "logicalCpus", "ramMb", "piModel", "storage", "writeCache", "recommendedCameras", "cores6Plus"}
	gotKeys := make([]string, 0, len(got))
	for k := range got {
		gotKeys = append(gotKeys, k)
	}
	slices.Sort(keys)
	slices.Sort(gotKeys)
	if !slices.Equal(gotKeys, keys) {
		t.Errorf("fields %q, want %q", gotKeys, keys)
	}
	if got["schema"] != "ipcam.diag.host/1" || got["os"] != runtime.GOOS || got["arch"] != runtime.GOARCH {
		t.Errorf("schema, os, arch = %v, %v, %v", got["schema"], got["os"], got["arch"])
	}
	for _, k := range []string{"physicalCores", "logicalCpus", "ramMb"} {
		if n, ok := got[k].(float64); !ok || n < 1 {
			t.Errorf("%s = %v, want a positive number", k, got[k])
		}
	}
	if n, ok := got["recommendedCameras"].(float64); !ok || n < 0 {
		t.Errorf("recommendedCameras = %v, want a number", got["recommendedCameras"])
	}
	if _, ok := got["cores6Plus"].(bool); !ok {
		t.Errorf("cores6Plus = %v, want a bool", got["cores6Plus"])
	}
	if s := got["storage"]; s != "sd" && s != "ssd" && s != "hdd" && s != "unknown" {
		t.Errorf("storage = %v", s)
	}
	if w := got["writeCache"]; w != "on" && w != "off" && w != "unknown" {
		t.Errorf("writeCache = %v", w)
	}
	if n := annotationLength(line); n > maxAnnotationLength {
		t.Errorf("the line has %d bytes after annotate.sh's escaping, more than %d", n, maxAnnotationLength)
	}

	switch runtime.GOOS {
	case "windows":
		if got["piModel"] != nil || got["recommendedCameras"] != float64(8) {
			t.Errorf("piModel %v, recommendedCameras %v; want null and 8 (H3)", got["piModel"], got["recommendedCameras"])
		}
	default:
		want := map[string]any{"physicalCores": 4.0, "logicalCpus": 4.0, "ramMb": 3790.0, "storage": "sd", "writeCache": "unknown",
			"recommendedCameras": 4.0, "cores6Plus": false}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s = %v, want %v", k, got[k], v)
			}
		}
		pi, ok := got["piModel"].(map[string]any)
		wantPi := map[string]any{"raw": "Raspberry Pi 4 Model B Rev 1.5", "revision": "c03115", "tier": "H1b", "supported": true, "known": true,
			"recommendedMin": 4.0, "recommendedMax": 4.0, "source": "cpuinfo"}
		if !ok || len(pi) != len(wantPi) {
			t.Fatalf("piModel = %v, want %v", got["piModel"], wantPi)
		}
		for k, v := range wantPi {
			if pi[k] != v {
				t.Errorf("piModel.%s = %v, want %v", k, pi[k], v)
			}
		}
	}
}

// TestDiagHost_RecommendedForPC (§4.8, R07): a Windows PC (H3) gets 8
// cameras whatever its cores; with 6 physical cores or more, cores6Plus
// says that 16 may be measured, as information.
func TestDiagHost_RecommendedForPC(t *testing.T) {
	for _, tc := range []struct {
		cores      int
		cores6Plus bool
	}{{4, false}, {6, true}, {16, true}} {
		restore := setHostProbe(func(string) hostFacts {
			return hostFacts{OS: "windows", Arch: "amd64", PhysicalCores: tc.cores, LogicalCPUs: 2 * tc.cores, RAMMb: 16384, Storage: "ssd", WriteCache: "on"}
		})
		got, _ := diagHost(t)
		restore()
		if got["recommendedCameras"] != float64(8) || got["cores6Plus"] != tc.cores6Plus || got["piModel"] != nil {
			t.Errorf("%d cores: recommendedCameras %v, cores6Plus %v, piModel %v; want 8, %v, null",
				tc.cores, got["recommendedCameras"], got["cores6Plus"], got["piModel"], tc.cores6Plus)
		}
	}
}

// TestDiagHost_UnknownPiHasNoNumber (C24): a board that is not in §4.8's
// table, and an armv6 Pi, get no recommended camera count; the owner gives
// one in the spike.
func TestDiagHost_UnknownPiHasNoNumber(t *testing.T) {
	for _, raw := range []string{"", "Banana Pi M5", "Raspberry Pi Zero W Rev 1.1"} {
		restore := setHostProbe(func(string) hostFacts {
			m := health.ParsePiModel(raw, "")
			return hostFacts{OS: "linux", Arch: "arm64", PhysicalCores: 4, LogicalCPUs: 4, RAMMb: 1024, PiModel: &m, Storage: "sd", WriteCache: "off"}
		})
		got, _ := diagHost(t)
		restore()
		pi, _ := got["piModel"].(map[string]any)
		if got["recommendedCameras"] != float64(0) || pi == nil || pi["supported"] != false {
			t.Errorf("%q: recommendedCameras %v, piModel %v; want 0 and an unsupported model", raw, got["recommendedCameras"], got["piModel"])
		}
	}
}

// TestDiag_Usage: an unknown diag subcommand or argument is a usage error.
func TestDiag_Usage(t *testing.T) {
	defer setHostProbe(func(string) hostFacts { t.Error("a usage error read the host"); return hostFacts{} })()
	for _, args := range [][]string{{"diag"}, {"diag", "disk"}, {"diag", "host", "extra"}, {"diag", "pimodel"}, {"diag", "pimodel", "--raw", "x", "extra"}} {
		if code, out := diag(t, args...); code != exitFatal || out != "" {
			t.Errorf("ipcam-hub %s: exit %d, stdout %q; want exit 1 and nothing", strings.Join(args, " "), code, out)
		}
	}
}
