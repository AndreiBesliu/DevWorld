package health

import (
	"path/filepath"
	"testing"
)

// TestCores_CountsPhysicalCores (R07): the physical cores are the distinct
// (physical id, core id) pairs of cpuinfo, so neither hyperthreads nor a
// second socket's repeated core ids are miscounted; a CPU that lists no
// core ids, as ARM does, has one core per logical CPU.
func TestCores_CountsPhysicalCores(t *testing.T) {
	for _, tc := range []struct {
		fixture           string
		physical, logical int
	}{
		{"cpuinfo-x86-6core", 6, 12},
		{"cpuinfo-x86-2socket", 4, 8},
		{"cpuinfo-pi4", 4, 4},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			physical, logical, err := Cores(Roots{CPUInfo: filepath.Join("testdata", tc.fixture)})
			if err != nil {
				t.Fatalf("Cores: %v", err)
			}
			if physical != tc.physical || logical != tc.logical {
				t.Errorf("Cores = %d physical, %d logical; want %d, %d", physical, logical, tc.physical, tc.logical)
			}
		})
	}
	if _, _, err := Cores(Roots{CPUInfo: filepath.Join(t.TempDir(), "cpuinfo")}); err == nil {
		t.Error("Cores of a missing cpuinfo: no error")
	}
}

// TestRAMMb_ReadsMemTotal: the RAM is meminfo's MemTotal, in MiB.
func TestRAMMb_ReadsMemTotal(t *testing.T) {
	mb, err := RAMMb(Roots{MemInfo: filepath.Join("testdata", "meminfo-pi4")})
	if err != nil {
		t.Fatalf("RAMMb: %v", err)
	}
	if mb != 3790 {
		t.Errorf("RAMMb = %d, want 3790 (3881864 kB)", mb)
	}
	if _, err := RAMMb(Roots{MemInfo: filepath.Join("testdata", "cpuinfo-pi4")}); err == nil {
		t.Error("RAMMb of a file without MemTotal: no error")
	}
}
