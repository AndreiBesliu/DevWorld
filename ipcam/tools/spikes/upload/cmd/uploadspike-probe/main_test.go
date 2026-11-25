package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// testProbe is a probe of a Pi whose cpuinfo and device tree are given.
func testProbe(t *testing.T, machine, cpuinfo, deviceTree string) probe {
	t.Helper()
	dir := t.TempDir()
	p := probe{cpuinfo: filepath.Join(dir, "cpuinfo"), deviceTree: filepath.Join(dir, "model"),
		machine: func() (string, error) { return machine, nil }, results: filepath.Join(dir, "results"), zipDir: t.TempDir(),
		now: func() time.Time { return time.UnixMilli(1_790_000_000_000) }}
	for path, content := range map[string]string{p.cpuinfo: cpuinfo, p.deviceTree: deviceTree} {
		if content != "" {
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return p
}

// TestProbe_ChoosesArchiveByMachine (P17): aarch64 and armv7l get their
// archive; armv6 is told Faza 0-bis, and its marker lands in
// results/MI5/pi-probe/ and in results-pi.zip.
func TestProbe_ChoosesArchiveByMachine(t *testing.T) {
	for machine, archive := range map[string]string{"aarch64": "spike-upload-linux-arm64.tar.gz", "armv7l": "spike-upload-linux-armv7.tar.gz"} {
		var out bytes.Buffer
		if err := run(testProbe(t, machine, "Model\t: Raspberry Pi 4 Model B Rev 1.4\n", ""), &out); err != nil ||
			!strings.Contains(out.String(), archive) {
			t.Errorf("%s: %v\n%s", machine, err, out.String())
		}
	}
	p := testProbe(t, "armv6l", "Revision\t: 9000c1\nModel\t: Raspberry Pi Zero W Rev 1.1\n", "")
	var out bytes.Buffer
	if err := run(p, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Acest Raspberry Pi (armv6) nu poate rula hub-ul. Proba de upload pe Pi trece în Faza 0-bis. Folosiți PC-ul.") {
		t.Errorf("armv6 said:\n%s", out.String())
	}
	files, _ := filepath.Glob(filepath.Join(p.results, "MI5", "pi-probe", "*.json"))
	if len(files) != 1 {
		t.Fatalf("markers %v", files)
	}
	zipPath := filepath.Join(p.zipDir, "results-pi.zip")
	if !strings.Contains(out.String(), zipPath) {
		t.Errorf("the zip's path is not printed:\n%s", out.String())
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	if len(zr.File) != 1 || !strings.HasPrefix(zr.File[0].Name, "results/MI5/pi-probe/") {
		t.Fatalf("the zip holds %v", zr.File)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	var e results.Envelope
	if err := json.Unmarshal(b, &e); err != nil || e.Code != "MI5" || e.Host != "pi" || e.PiModel == nil || !e.PiModel.Known ||
		e.PiModel.Supported || e.PiModel.Revision != "9000c1" || e.PiModel.Source != "cpuinfo" {
		t.Errorf("the zipped marker %+v (%v)", e, err)
	}
	if err := run(testProbe(t, "x86_64", "", ""), io.Discard); err == nil {
		t.Error("x86_64: no error")
	}
}

// TestProbe_DeviceTreeFallback: without cpuinfo's Model line, the device
// tree's model, without its trailing NUL.
func TestProbe_DeviceTreeFallback(t *testing.T) {
	p := testProbe(t, "armv6l", "Hardware\t: BCM2835\n", "Raspberry Pi Model B Rev 2\x00")
	var out bytes.Buffer
	if err := run(p, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Modelul: Raspberry Pi Model B Rev 2 (din /proc/device-tree/model)") {
		t.Errorf("said:\n%s", out.String())
	}
	if m := readModel(p.cpuinfo, p.deviceTree); m.source != "device-tree" || m.raw != "Raspberry Pi Model B Rev 2" {
		t.Errorf("model %+v", m)
	}
}
