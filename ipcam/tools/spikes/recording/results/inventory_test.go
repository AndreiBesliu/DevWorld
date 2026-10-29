package results

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// envelope is a one-run envelope of code and set on host.
func envelope(code, set, host string) Envelope {
	e := sample()
	e.Code, e.Set, e.Host = code, set, host
	e.RunID = set + "-" + host + "-01"
	if host != "pi" {
		e.PiModel, e.SdConfirmation, e.OS = nil, nil, "windows"
	}
	switch code {
	case "MI2": // its 72 h run, not runs
		e.Runs, e.MI2 = nil, &Mi2Data{Hours: 72, Cameras: 8, WindowHours: 6}
	case "MI3": // its capacity run, not runs
		e.Runs, e.MI3 = nil, &Mi3Data{Tier: "H3", Cameras: 8, Steps: []Mi3Step{{Cameras: 8, Minutes: 60, Attempt: 1, Valid: true, Pass: true}}}
	}
	return e
}

// pcResults writes what the PC's own results folder holds at the end: its
// MI1 sets, MI2, MI3 and MI9 lan, and the app's AGE result.
func pcResults(t *testing.T, dir string) {
	t.Helper()
	for _, e := range []Envelope{
		envelope("MI1", "pc-kill9", "pc"), envelope("MI1", "pc-bugcheck", "pc"), envelope("MI1", "real-pc", "pc"),
		envelope("MI2", "72h", "pc"), envelope("MI3", "capacity", "pc"), envelope("MI9", "lan", "pc"),
	} {
		if err := Write(dir, e); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(dir, "app", "android-samsung-sm-a245f-20261101-101500-age.json"),
		`{"bytes":33554432,"device":{"model":"SM-A245F","platform":"android"},"mbps":96.5,"mdnsUnavailable":false,"ms":2782,"scenario":"AGE"}`)
	writeFile(t, filepath.Join(dir, "app", "android-samsung-sm-a245f-20261101-101000-s1.json"), `{"scenario":"S1"}`)
	writeFile(t, filepath.Join(dir, "state", "run-state.json"), `{"set":"pc-kill9","nextRun":4}`)
	writeFile(t, filepath.Join(dir, "MI1", "pc-kill9", "broken.json"), `{"schema":"ipcam.phase0c1.result/1","code":"MI1"`)
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestInventory_ReportsMissingSets (G15): the PC's folder, with the Pi's
// folder pulled into pi/, misses the Pi's MI2 and MI4 envelopes; both are
// listed under «Lipsesc: …». With an unsupported Pi nothing is expected
// with the Pi as host.
func TestInventory_ReportsMissingSets(t *testing.T) {
	dir := t.TempDir()
	pcResults(t, dir)
	pi := filepath.Join(dir, "pi")
	for _, e := range []Envelope{
		envelope("MI1", "pi-kill9", "pi"), envelope("MI1", "pi-sysrq", "pi"), envelope("MI3", "capacity", "pi"), envelope("MI9", "lan", "pi"),
	} {
		if err := Write(pi, e); err != nil {
			t.Fatal(err)
		}
	}
	// The Pi's real cuts were judged by the PC's controller: they are in the
	// PC's own folder.
	if err := Write(dir, envelope("MI1", "real-pi", "pi")); err != nil {
		t.Fatal(err)
	}

	present, missing := Inventory(dir, true)
	if want := []string{"MI2 (pi)", "MI4 (pi)"}; !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %q, want %q", missing, want)
	}
	for _, want := range []string{"MI1 pc-kill9 (pc)", "MI1 real-pi (pi)", "MI1 pi-sysrq (pi)", "MI2 72h (pc)", "MI9 lan (pi)", "AGE"} {
		if !slices.Contains(present, want) {
			t.Errorf("present = %q, want it to hold %q", present, want)
		}
	}
	if !slices.IsSorted(present) || len(slices.Compact(slices.Clone(present))) != len(present) {
		t.Errorf("present = %q, want sorted and without repeats", present)
	}
	text := InventoryText(present, missing)
	if !strings.Contains(text, "Lipsesc: MI2 (pi), MI4 (pi)") {
		t.Errorf("InventoryText = %q, want «Lipsesc: MI2 (pi), MI4 (pi)»", text)
	}

	t.Run("with everything there", func(t *testing.T) {
		for _, e := range []Envelope{envelope("MI2", "72h", "pi"), envelope("MI4", "random-access", "pi")} {
			if err := Write(pi, e); err != nil {
				t.Fatal(err)
			}
		}
		present, missing := Inventory(dir, true)
		if len(missing) != 0 {
			t.Errorf("missing = %q, want none", missing)
		}
		if text := InventoryText(present, missing); strings.Contains(text, "Lipsesc") || !strings.Contains(text, "Nu lipsește nimic.") {
			t.Errorf("InventoryText = %q, want «Nu lipsește nimic.»", text)
		}
	})

	t.Run("an unsupported Pi", func(t *testing.T) {
		dir := t.TempDir()
		pcResults(t, dir)
		if _, missing := Inventory(dir, false); len(missing) != 0 {
			t.Errorf("missing = %q, want no Pi set", missing)
		}
		_, missing := Inventory(dir, true)
		want := []string{"MI1 pi-kill9 (pi)", "MI1 pi-sysrq (pi)", "MI1 real-pi (pi)", "MI2 (pi)", "MI3 (pi)", "MI9 lan (pi)", "MI4 (pi)"}
		if !reflect.DeepEqual(missing, want) {
			t.Errorf("with a supported Pi, missing = %q, want %q", missing, want)
		}
	})

	t.Run("an empty or absent folder", func(t *testing.T) {
		present, missing := Inventory(filepath.Join(t.TempDir(), "absent"), false)
		want := []string{"MI1 pc-kill9 (pc)", "MI1 pc-bugcheck (pc)", "MI1 real-pc (pc)", "MI2 (pc)", "MI3 (pc)", "MI9 lan (pc)", "AGE"}
		if len(present) != 0 || !reflect.DeepEqual(missing, want) {
			t.Errorf("Inventory = %q, %q; want nothing present and %q missing", present, missing, want)
		}
	})
}
