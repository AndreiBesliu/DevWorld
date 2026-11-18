package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestProbe_MarkerRoundTripsThroughRecreport (G31, G32; Task 13 review,
// M2): what the probe writes on an armv6 Pi, where it writes it, is what
// recreport reads. The probe runs on a fake armv6 Pi, then recreport, built
// from its source, reads the probe's results folder alone, and the report
// renders the Pi as armv6 and names the probe's marker.
func TestProbe_MarkerRoundTripsThroughRecreport(t *testing.T) {
	p := fakePi(t, cpuinfoNoModel, "Raspberry Pi Zero W Rev 1.1", "armv6l")
	if out, err := probeOut(t, p); err != nil {
		t.Fatalf("the probe: %v\n%s", err, out)
	}

	bin := filepath.Join(t.TempDir(), "recreport")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// go test puts its own toolchain first on the PATH of the tests.
	build := exec.Command("go", "build", "-o", bin, "../recreport") //nolint:gosec // recreport, built into the test's folder
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build recreport: %v\n%s", err, out)
	}
	reports := t.TempDir()
	report := exec.Command(bin, "-in", p.results, "-out", reports, "-date", "2026-10-30") //nolint:gosec // the binary that the test built
	if out, err := report.CombinedOutput(); err != nil {
		t.Fatalf("recreport on the probe's folder: %v\n%s", err, out)
	}
	md, err := os.ReadFile(filepath.Join(reports, "2026-10-30-phase0c-recorder-results.md")) //nolint:gosec // the report that recreport wrote in the test's folder
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"- **Pi-ul dvs.** este armv6, deci nesuportat: măsurătorile lui trec în Faza 0-bis (vezi mai jos).",
		"| nesuportat: Pi-ul dvs. | — | — | amânat în Faza 0-bis: Pi armv6 |",
		"Pi-ul dvs.: Raspberry Pi Zero W Rev 1.1 (citit din device-tree).",
		"Marcajul lui recspike-probe: Pi-ul dvs. este armv6 (`MI1 pi-probe`)",
	} {
		if !strings.Contains(string(md), want) {
			t.Errorf("the report of the probe's folder lacks «%s»", want)
		}
	}
}
