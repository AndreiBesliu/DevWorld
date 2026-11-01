package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cpuinfoWithModel is testdata/cpuinfo-pi4 with its Model line set to
// model, written to a file of the test.
func cpuinfoWithModel(t *testing.T, model string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "cpuinfo-pi4"))
	if err != nil {
		t.Fatal(err)
	}
	modelLine := regexp.MustCompile(`(?m)^Model\t\t: .*$`)
	if !modelLine.Match(b) {
		t.Fatal("testdata/cpuinfo-pi4 has no Model line")
	}
	path := filepath.Join(t.TempDir(), "cpuinfo")
	if err := os.WriteFile(path, modelLine.ReplaceAll(b, []byte("Model\t\t: "+model)), 0o600); err != nil { //nolint:gosec // a file of the test
		t.Fatal(err)
	}
	return path
}

// TestReadPiModel_EverySpecRow (C24, HUB-34): every model of §4.8's table
// gets its tier and recommended camera count, in match order: Pi 5, 500 and
// CM5 8; Pi 4, 400 and CM4 4; Zero 2 W before Zero; Pi 3, 3B+, 3A+, Pi 2
// v1.1 and v1.2, CM3 and CM3+ 1–2; Pi 1, Zero, Zero W and CM1 unsupported
// (armv6); any other board unknown, without a number.
func TestReadPiModel_EverySpecRow(t *testing.T) {
	for _, tc := range []struct {
		raw              string
		tier             string
		supported, known bool
		min, max         int
	}{
		{"Raspberry Pi 5 Model B Rev 1.0", "H1", true, true, 8, 8},
		{"Raspberry Pi 500 Rev 1.0", "H1", true, true, 8, 8},
		{"Raspberry Pi Compute Module 5 Rev 1.0", "H1", true, true, 8, 8},
		{"Raspberry Pi 4 Model B Rev 1.5", "H1b", true, true, 4, 4},
		{"Raspberry Pi 400 Rev 1.1", "H1b", true, true, 4, 4},
		{"Raspberry Pi Compute Module 4 Rev 1.1", "H1b", true, true, 4, 4},
		{"Raspberry Pi 3 Model B Rev 1.2", "H1c", true, true, 1, 2},
		{"Raspberry Pi 3 Model B Plus Rev 1.3", "H1c", true, true, 1, 2},
		{"Raspberry Pi 3 Model A Plus Rev 1.1", "H1c", true, true, 1, 2},
		{"Raspberry Pi Zero 2 W Rev 1.0", "H1c", true, true, 1, 2},
		{"Raspberry Pi 2 Model B Rev 1.1", "H1c", true, true, 1, 2},
		{"Raspberry Pi 2 Model B Rev 1.2", "H1c", true, true, 1, 2},
		{"Raspberry Pi Compute Module 3 Rev 1.0", "H1c", true, true, 1, 2},
		{"Raspberry Pi Compute Module 3 Plus Rev 1.0", "H1c", true, true, 1, 2},
		{"Raspberry Pi Model B Rev 2", "unsupported", false, true, 0, 0},
		{"Raspberry Pi Zero Rev 1.3", "unsupported", false, true, 0, 0},
		{"Raspberry Pi Zero W Rev 1.1", "unsupported", false, true, 0, 0},
		{"Raspberry Pi Compute Module Rev 1.0", "unsupported", false, true, 0, 0},
		{"Banana Pi M5", "unknown", false, false, 0, 0},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			m, err := ReadPiModel(cpuinfoWithModel(t, tc.raw))
			if err != nil {
				t.Fatalf("ReadPiModel: %v", err)
			}
			want := PiModel{Raw: tc.raw, Revision: "c03115", Tier: tc.tier, Supported: tc.supported, Known: tc.known,
				RecommendedMin: tc.min, RecommendedMax: tc.max, Source: "cpuinfo"}
			if m != want {
				t.Errorf("ReadPiModel = %+v, want %+v", m, want)
			}
			if p := ParsePiModel(tc.raw, "c03115"); p.Tier != m.Tier || p.RecommendedMin != m.RecommendedMin || p.RecommendedMax != m.RecommendedMax {
				t.Errorf("ParsePiModel = %+v, not the table that ReadPiModel applies", p)
			}
		})
	}
}

// TestReadPiModel_NeverReadsSerial (T39, PH0-22): the board's serial number,
// which cpuinfo lists next to its model, appears nowhere in the result.
func TestReadPiModel_NeverReadsSerial(t *testing.T) {
	const serial = "10000000a1b2c3d4"
	path := filepath.Join("testdata", "cpuinfo-pi4")
	b, err := os.ReadFile(path) //nolint:gosec // the test's fixture
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), serial) {
		t.Fatalf("%s does not list the serial %s", path, serial)
	}
	m, err := ReadPiModel(path)
	if err != nil {
		t.Fatalf("ReadPiModel: %v", err)
	}
	if m.Tier != "H1b" || m.Revision != "c03115" {
		t.Errorf("ReadPiModel = %+v, want the Pi 4 with revision c03115", m)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{serial, serial[8:], "Serial"} {
		if strings.Contains(string(out), needle) || strings.Contains(m.Raw+m.Revision+m.Source, needle) {
			t.Errorf("the result %s holds %q", out, needle)
		}
	}
}

// TestReadPiModel_NoModelLine_Unknown (G32): cpuinfo without a Model line,
// as a mainline kernel writes it, gives an unknown model read from cpuinfo,
// so that the spike falls back to the device tree.
func TestReadPiModel_NoModelLine_Unknown(t *testing.T) {
	for _, fixture := range []string{"cpuinfo-pi-mainline", "cpuinfo-x86-6core"} {
		t.Run(fixture, func(t *testing.T) {
			m, err := ReadPiModel(filepath.Join("testdata", fixture))
			if err != nil {
				t.Fatalf("ReadPiModel: %v", err)
			}
			want := PiModel{Tier: "unknown", Source: "cpuinfo"}
			if m != want {
				t.Errorf("ReadPiModel = %+v, want %+v: Raw empty, from cpuinfo", m, want)
			}
		})
	}
}

// TestReadPiModel_MissingFile: a cpuinfo that cannot be read is an error,
// with the unknown model from cpuinfo.
func TestReadPiModel_MissingFile(t *testing.T) {
	m, err := ReadPiModel(filepath.Join(t.TempDir(), "cpuinfo"))
	if err == nil {
		t.Fatal("ReadPiModel of a missing file: no error")
	}
	if want := (PiModel{Tier: "unknown", Source: "cpuinfo"}); m != want {
		t.Errorf("ReadPiModel = %+v, want %+v", m, want)
	}
}

// TestParsePiModel_TrimsTheDeviceTreeString: /proc/device-tree/model ends
// in a NUL byte, which the spike may pass on as it is (G32).
func TestParsePiModel_TrimsTheDeviceTreeString(t *testing.T) {
	m := ParsePiModel("Raspberry Pi 4 Model B Rev 1.5\x00", "")
	if m.Raw != "Raspberry Pi 4 Model B Rev 1.5" || m.Tier != "H1b" {
		t.Errorf("ParsePiModel = %+v, want the Pi 4 without the NUL", m)
	}
	if m.Source != "" {
		t.Errorf("Source %q: ParsePiModel reads no file and names no source", m.Source)
	}
}
