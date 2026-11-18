package resultrules

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The device key is spikehub's and the report's, and the app's copy
// (app/src/deviceplan.ts deviceKey) is pinned against the same cases in
// app/src/deviceplan.test.ts.
func TestKey(t *testing.T) {
	for _, c := range []struct{ platform, os, want string }{
		{"chrome", "Windows", "chrome"},
		{"chrome", "Linux", "chrome-linux"},
		{"chrome", "macOS", "chrome-macos"},
		{"firefox", "", "firefox"},
		{"edge", "Android", "edge-android"},
		{"android", "Android 15 (API 35)", "android"},
		{"windows-tauri", "Windows 11", "windows-tauri"},
		// The family is the first word, in lower case, letters and digits only, at most 16 of them.
		{"chrome", "  Windows 11", "chrome"},
		{"edge", "Chrome_OS 141", "edge-chromeos"},
		{"chrome", "averyveryverylongosname 1", "chrome-averyveryverylon"},
		{"firefox", "--- 1", "firefox"},
	} {
		if got := Key(c.platform, c.os); got != c.want {
			t.Errorf("Key(%q, %q) = %q, want %q", c.platform, c.os, got, c.want)
		}
	}
}

func TestBasePlatform(t *testing.T) {
	for key, want := range map[string]string{
		"chrome-android": "chrome",
		"edge-linux":     "edge",
		"firefox":        "firefox",
		"android":        "android",
		"windows-tauri":  "windows-tauri",
	} {
		if got := BasePlatform(key); got != want {
			t.Errorf("BasePlatform(%q) = %q, want %q", key, got, want)
		}
	}
	if !IsBrowser("edge") || IsBrowser("android") || IsBrowser("windows-tauri") {
		t.Error("IsBrowser: the browsers are chrome, edge and firefox")
	}
}

// testdata/classes holds results files of every class, and classes.json what
// each counts for. spikehub and spikereport read the same fixtures in their
// own tests, through their own reading of the files.
func TestClass_TheSharedFixtures(t *testing.T) {
	testdata := os.DirFS(filepath.Join("..", "testdata"))
	data, err := fs.ReadFile(testdata, "classes.json")
	if err != nil {
		t.Fatal(err)
	}
	var classes map[string]struct {
		Key   string `json:"key"`
		Class string `json:"class"`
	}
	if err := json.Unmarshal(data, &classes); err != nil {
		t.Fatal(err)
	}
	seen := map[Class]bool{}
	for file, want := range classes {
		data, err := fs.ReadFile(testdata, "classes/"+file)
		if err != nil {
			t.Fatal(err)
		}
		var f Facts
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if got := f.Key(); got != want.Key {
			t.Errorf("%s: key %q, want %q", file, got, want.Key)
		}
		if got := f.Class(); got.String() != want.Class {
			t.Errorf("%s: class %v, want %s", file, got, want.Class)
		}
		seen[f.Class()] = true
	}
	for _, c := range []Class{Usable, Interrupted, Failed, OnCharger, Informative} {
		if !seen[c] {
			t.Errorf("no fixture of class %v", c)
		}
	}
}

// The checks go in the report's order: interrupted, failed, on the charger,
// informative.
func TestClass_Order(t *testing.T) {
	charging := Readings{Charging: []float64{1}}
	for _, c := range []struct {
		name string
		f    Facts
		want Class
	}{
		{"interrupted and failed", Facts{Scenario: "S1", Interrupted: true, Notes: []string{"failed: Error: x"}}, Interrupted},
		{"interrupted on the charger", Facts{Scenario: "S3-9", Interrupted: true, Samples: charging}, Interrupted},
		{"failed on the charger", Facts{Scenario: "S3-9", Notes: []string{"failed: Error: x"}, Samples: charging}, Failed},
		{"charging outside S3", Facts{Scenario: "S1", Device: Device{Platform: "android"}, Samples: charging}, Usable},
		{"a note that only starts like failed", Facts{Scenario: "S1", Notes: []string{"failedover: no"}}, Usable},
		{"desktop S4 without tiles", Facts{Scenario: "S4", Device: Device{Platform: "edge"}}, Informative},
		{"phone S4 without tiles", Facts{Scenario: "S4", Device: Device{Platform: "android"}}, Usable},
		{"desktop S4 with 16 tiles", Facts{Scenario: "S4", Device: Device{Platform: "windows-tauri"}, Samples: Readings{Tiles: []float64{16}}}, Usable},
	} {
		if got := c.f.Class(); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if DesktopTiles(nil) != 0 || DesktopTiles([]float64{9, 16}) != 9 {
		t.Error("DesktopTiles: the first tiles reading, 0 without one")
	}
}
