package health

import (
	"runtime"
	"testing"
)

// TestCores_FromTheSystem (R07): GetLogicalProcessorInformationEx gives at
// least one physical core, and no more than the logical processors, which
// include those outside the process's affinity.
func TestCores_FromTheSystem(t *testing.T) {
	physical, logical, err := Cores(Roots{})
	if err != nil {
		t.Fatalf("Cores: %v", err)
	}
	if physical < 1 || logical < physical || logical < runtime.NumCPU() {
		t.Errorf("Cores = %d physical, %d logical, with %d CPUs for the process", physical, logical, runtime.NumCPU())
	}
}

// TestRAMMb_FromTheSystem: GlobalMemoryStatusEx gives the RAM.
func TestRAMMb_FromTheSystem(t *testing.T) {
	mb, err := RAMMb(Roots{})
	if err != nil {
		t.Fatalf("RAMMb: %v", err)
	}
	if mb < 512 {
		t.Errorf("RAMMb = %d", mb)
	}
}
