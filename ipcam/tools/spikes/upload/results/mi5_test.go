package results

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestMi5Data_WrittenAndValidated: an MI5 envelope carries its data
// through Write; MI5 data belongs to an MI5 envelope only; the pi-probe
// marker is an MI5 envelope of host pi, known and unsupported, with no
// run, as c1's.
func TestMi5Data_WrittenAndValidated(t *testing.T) {
	e := Envelope{Schema: Schema, Code: "MI5", RunID: "mi5-20261118T090000Z", Set: "pc", Host: "pc", OS: "windows", Storage: "ssd",
		Runs: []Run{{RunID: "mi5-20261118T090000Z", Valid: true}},
		MI5: &Mi5Data{BlockSecs: 21600, Restart: &Mi5Restart{ResumedOffset: 1 << 20}, Dests: []Mi5Dest{{Dest: "dst-1", Kind: "GOOGLE_DRIVE",
			Account: "acct-1", CallsPerSegment: map[string]float64{"put": 1}}}}}
	dir := t.TempDir()
	if err := Write(dir, e); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "MI5", "pc", e.RunID+".json")) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := json.Unmarshal(b, &got); err != nil || got.MI5 == nil || got.MI5.Restart.ResumedOffset != 1<<20 || got.MI5.Dests[0].CallsPerSegment["put"] != 1 {
		t.Fatalf("read back %+v, %v", got.MI5, err)
	}
	e.Code = "MI6"
	if e.Validate() == nil {
		t.Error("MI5 data in an MI6 envelope validates")
	}
	m := PiProbeMarker("Raspberry Pi Zero W Rev 1.1", "9000c1", "cpuinfo", 1)
	if err := m.Validate(); err != nil || m.Host != "pi" || m.Set != PiProbeSet || m.PiModel.Supported || !m.PiModel.Known || len(m.Runs) != 0 {
		t.Errorf("the marker %+v: %v", m, err)
	}
	if err := Write(dir, m); err != nil {
		t.Errorf("writing the marker: %v", err)
	}
}
