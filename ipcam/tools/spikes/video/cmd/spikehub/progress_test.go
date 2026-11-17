package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// getProgress asks the hub what is complete and what is left.
func (h *testHub) getProgress(t *testing.T) progressAnswer {
	t.Helper()
	resp, body := h.do(t, http.MethodGet, "/spike/v1/progress", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("progress: status %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("progress: Cache-Control %q, want no-store", got)
	}
	var p progressAnswer
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("progress: %v: %s", err, body)
	}
	return p
}

// writeResultFile puts a results file into dir as the hub would have stored it.
func writeResultFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// result is a results file of the app: a run of scenario on the device, with what else is given.
func result(scenario, device string, extra ...string) string {
	fields := append([]string{`"scenario":"` + scenario + `"`, `"device":` + device, `"interrupted":false`, `"notes":[]`, `"samples":{}`}, extra...)
	// A later key wins in encoding/json, as in the app's JSON.parse.
	return "{" + strings.Join(fields, ",") + "}"
}

const (
	chromeWindows = `{"platform":"chrome","os":"Windows","webview":"Chrome/141.0.0.0","ua":"Mozilla/5.0 (Windows NT 10.0; Win64; x64)"}`
	chromeLinux   = `{"platform":"chrome","os":"Linux","ua":"Mozilla/5.0 (X11; Linux x86_64)"}`
	edgeWindows   = `{"platform":"edge","os":"Windows","ua":"Mozilla/5.0 (Windows NT 10.0) Edg/141.0"}`
	phone         = `{"platform":"android","model":"samsung SM-A245F","os":"Android 15 (API 35)","ua":"Mozilla/5.0 (Linux; Android 15; wv)"}`
	otherPhone    = `{"platform":"android","model":"Google sdk_gphone64_x86_64","os":"Android 16 (API 36)","ua":"x"}`
	desktopShell  = `{"platform":"windows-tauri","model":"AMD Ryzen 5 5600G with Radeon Graphics","os":"Windows 11","ua":"x"}`
)

func groupOf(p progressAnswer, key, model string) (progressGroup, bool) {
	i := slices.IndexFunc(p.Complete, func(g progressGroup) bool { return g.Key == key && g.Model == model })
	if i < 0 {
		return progressGroup{}, false
	}
	return p.Complete[i], true
}

func deviceOf(t *testing.T, p progressAnswer, id string) progressDevice {
	t.Helper()
	i := slices.IndexFunc(p.Devices, func(d progressDevice) bool { return d.ID == id })
	if i < 0 {
		t.Fatalf("progress has no device %s: %+v", id, p.Devices)
	}
	return p.Devices[i]
}

// A scenario counts as done only with a result that the report can use: not
// interrupted, not failed, an S3 not on the charger (R48) and a desktop S4
// with 16 tiles (R45). Each device is its platform, a browser elsewhere than
// on Windows a device of its own (hub-4), and the phone its model too.
func TestProgress_CountsOnlyCompleteResultsPerDevice(t *testing.T) {
	hub := newTestHub(t, 1)
	dir := hub.resultsDir
	writeResultFile(t, dir, "chrome-20261117-090000-s10.json", result("S10", chromeWindows, `"notes":["pair: host/host","prompt: no"]`))
	writeResultFile(t, dir, "chrome-20261117-090100-s1.json", result("S1", chromeWindows, `"interrupted":true`, `"interruption":{"kind":"hidden","atS":12}`))
	writeResultFile(t, dir, "chrome-20261117-090200-s2.json", result("S2", chromeWindows, `"notes":["failed: Error: no track for this slot"]`))
	writeResultFile(t, dir, "chrome-20261117-090300-s9.json", result("S9", chromeWindows))
	writeResultFile(t, dir, "chrome-20261117-090400-s9.json", result("S9", chromeWindows, `"interrupted":true`))
	writeResultFile(t, dir, "chrome-20261117-090500-s4.json", result("S4", chromeWindows, `"samples":{"tiles":[16]}`))
	writeResultFile(t, dir, "chrome-20261117-090600-s7.json", result("S7", chromeLinux))
	writeResultFile(t, dir, "edge-20261117-100000-s3-9.json", result("S3-9", edgeWindows))
	writeResultFile(t, dir, "android-samsung-sm-a245f-20261117-110000-s3-9.json", result("S3-9", phone, `"samples":{"charging":[0,0,1]}`))
	writeResultFile(t, dir, "android-samsung-sm-a245f-20261117-110100-s3-4.json", result("S3-4", phone, `"samples":{"mainCharging":[1]}`))
	writeResultFile(t, dir, "android-samsung-sm-a245f-20261117-110200-s1.json", result("S1", phone))
	writeResultFile(t, dir, "android-google-sdk-gphone64-x86-64-20261117-110300-s2.json", result("S2", otherPhone))
	writeResultFile(t, dir, "windows-tauri-amd-ryzen-5-5600g-with-r-20261117-120000-s4.json", result("S4", desktopShell, `"samples":{"tiles":[9]}`))
	writeResultFile(t, dir, "windows-tauri-amd-ryzen-5-5600g-with-r-20261117-120100-s4.json", result("S4", desktopShell))
	writeResultFile(t, dir, "windows-tauri-amd-ryzen-5-5600g-with-r-20261117-120200-s7.json", result("S7", desktopShell))

	p := hub.getProgress(t)

	if p.Version != "spike-video-v0.1.1" {
		t.Errorf("version %q", p.Version)
	}
	want := []progressGroup{
		{Key: "android", Platform: "android", Model: "Google sdk_gphone64_x86_64", Scenarios: []string{"S2"}},
		{Key: "android", Platform: "android", Model: "samsung SM-A245F", Scenarios: []string{"S1"}},
		{Key: "chrome", Platform: "chrome", Scenarios: []string{"S4", "S9", "S10"}},
		{Key: "chrome-linux", Platform: "chrome", Scenarios: []string{"S7"}},
		{Key: "edge", Platform: "edge", Scenarios: []string{"S3-9"}},
		{Key: "windows-tauri", Platform: "windows-tauri", Model: "AMD Ryzen 5 5600G with Radeon Graphics", Scenarios: []string{"S7"}},
	}
	if got, _ := json.Marshal(p.Complete); string(got) != mustJSON(t, want) {
		t.Errorf("complete:\n got %s\nwant %s", got, mustJSON(t, want))
	}

	// «Rămase»: what each device of the guide still lacks, in the order of its run.
	for id, remaining := range map[string][]string{
		"chrome":        {"S1", "S2", "S3-9", "S3-16", "S7"},
		"edge":          {"S10", "S1", "S2", "S9"},
		"firefox":       {"S10", "S1", "S2", "S3-9", "S9"},
		"ipcam-desktop": {"S10", "S1", "S2", "S3-16", "S4", "S9"},
		"phone":         {"S10", "S3-9", "S3-4", "S4", "S5", "S6", "S7", "S8", "S9"},
	} {
		if got := deviceOf(t, p, id).Remaining; !slices.Equal(got, remaining) {
			t.Errorf("%s: remaining %v, want %v", id, got, remaining)
		}
	}
	if d := deviceOf(t, p, "chrome"); d.Label != "Chrome" || !d.PC {
		t.Errorf("chrome: %+v", d)
	}
	if d := deviceOf(t, p, "phone"); d.Label != "Galaxy A24" || d.PC {
		t.Errorf("phone: %+v", d)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Before any run there is no results folder: everything is left.
func TestProgress_WithoutAResultsFolderEverythingIsLeft(t *testing.T) {
	hub := newTestHub(t, 1)

	p := hub.getProgress(t)

	if len(p.Complete) != 0 {
		t.Errorf("complete: %+v", p.Complete)
	}
	if got := deviceOf(t, p, "edge").Remaining; !slices.Equal(got, []string{"S10", "S1", "S2", "S3-9", "S9"}) {
		t.Errorf("edge: remaining %v", got)
	}
	if got := deviceOf(t, p, "phone").Remaining; len(got) != 11 {
		t.Errorf("phone: remaining %v, want its 11 scenarios", got)
	}
	if len(p.Devices) != 5 {
		t.Errorf("%d devices, want the guide's 5", len(p.Devices))
	}
}

// Files that are not results, or not readable as one, are passed over.
func TestProgress_PassesOverFilesThatAreNotResults(t *testing.T) {
	hub := newTestHub(t, 1)
	dir := hub.resultsDir
	writeResultFile(t, dir, "notes.txt", result("S1", chromeWindows))
	writeResultFile(t, dir, "broken-20261018-155000-s1.json", `{"scenario":"S1","device":`)
	writeResultFile(t, dir, "label-20261018-155000-s1.json", `{"scenario":"S1","device":"Pixel 8"}`)
	writeResultFile(t, dir, "unknown-20261018-155000-s99.json", result("S99", chromeWindows))
	writeResultFile(t, dir, "array.json", `[1,2]`)
	if err := os.MkdirAll(filepath.Join(dir, "sub.json"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeResultFile(t, dir, "chrome-20261117-090300-s9.json", result("S9", chromeWindows))

	p := hub.getProgress(t)

	if want := `[{"key":"chrome","platform":"chrome","scenarios":["S9"]}]`; mustJSON(t, p.Complete) != want {
		t.Errorf("complete = %s, want %s", mustJSON(t, p.Complete), want)
	}
}

// A file read once is not parsed again unless it changes; a new one is read
// at the next request.
func TestProgress_SeesNewResults(t *testing.T) {
	hub := newTestHub(t, 1)

	if p := hub.getProgress(t); len(p.Complete) != 0 {
		t.Fatalf("complete: %+v", p.Complete)
	}
	if resp, body := hub.postResults(t, result("S10", edgeWindows)); resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	p := hub.getProgress(t)
	if g, ok := groupOf(p, "edge", ""); !ok || !slices.Equal(g.Scenarios, []string{"S10"}) {
		t.Errorf("after an upload: %+v", p.Complete)
	}
	for i := range 3 {
		writeResultFile(t, hub.resultsDir, fmt.Sprintf("edge-20261117-10000%d-s1.json", i), result("S1", edgeWindows))
	}
	p = hub.getProgress(t)
	if g, _ := groupOf(p, "edge", ""); !slices.Equal(g.Scenarios, []string{"S1", "S10"}) {
		t.Errorf("after three more: %+v", p.Complete)
	}
}

// The shells call the hub from their own origins (CORS, R29).
func TestProgress_AnswersTheShells(t *testing.T) {
	hub := newTestHub(t, 1)

	resp, _ := hub.do(t, http.MethodGet, "/spike/v1/progress", nil, http.Header{"Origin": {"http://tauri.localhost"}})

	if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != "http://tauri.localhost" {
		t.Errorf("status %d, Access-Control-Allow-Origin %q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}
}

// The v0.1.1 zip is extracted over v0.1.0's folder, and its results stay
// (requirement 9): the hub reads v0.1.0's files as they are, so Chrome's runs
// of 2026-11-17 count. testdata/results-v0.1.0 was written by v0.1.0's code.
func TestProgress_ReadsTheResultsOfV010(t *testing.T) {
	hub := newTestHub(t, 1)
	from := filepath.Join("..", "..", "testdata", "results-v0.1.0")
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeResultFile(t, hub.resultsDir, e.Name(), string(data))
	}

	p := hub.getProgress(t)

	if want := `[{"key":"chrome","platform":"chrome","scenarios":["S1","S7","S9","S10"]}]`; mustJSON(t, p.Complete) != want {
		t.Errorf("complete = %s, want %s", mustJSON(t, p.Complete), want)
	}
	if got := deviceOf(t, p, "chrome").Remaining; !slices.Equal(got, []string{"S2", "S3-9", "S3-16", "S4"}) {
		t.Errorf("chrome: remaining %v", got)
	}
}
