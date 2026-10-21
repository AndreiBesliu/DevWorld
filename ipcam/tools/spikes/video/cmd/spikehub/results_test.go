package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (h *testHub) postResults(t *testing.T, body string) (*http.Response, []byte) {
	t.Helper()
	return h.do(t, http.MethodPost, "/spike/v1/results", strings.NewReader(body), http.Header{"Content-Type": {"application/json"}})
}

func TestResults_SanitisesNameAndSize(t *testing.T) {
	hub := newTestHub(t, 1)

	resp, body := hub.postResults(t, `{"device":"../../x","scenario":"S1","samples":{"a":[1,2]}}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if want := "x-20261018-125100-s1.json"; strings.TrimSpace(string(body)) != `{"file":"`+want+`"}` {
		t.Errorf("response = %s, want the file name %s", body, want)
	}
	files := resultFiles(t, hub.resultsDir)
	if len(files) != 1 || files[0] != "x-20261018-125100-s1.json" {
		t.Fatalf("results folder holds %v, want x-20261018-125100-s1.json", files)
	}
	// Nothing went above the results folder.
	if above := resultFiles(t, filepath.Dir(hub.resultsDir)); len(above) != 1 || above[0] != "results" {
		t.Errorf("the folder above the results holds %v", above)
	}

	// 3 MiB is over the limit of 2 MiB.
	huge := `{"device":"big","scenario":"S1","pad":"` + strings.Repeat("a", 3<<20) + `"}`
	if resp, _ := hub.postResults(t, huge); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("3 MiB: status %d, want 413", resp.StatusCode)
	}
	if files := resultFiles(t, hub.resultsDir); len(files) != 1 {
		t.Errorf("the refused result left %v", files)
	}

	// Just under the limit is accepted.
	almost := `{"device":"big","scenario":"S2","pad":"` + strings.Repeat("a", 2<<20-100) + `"}`
	if resp, body := hub.postResults(t, almost); resp.StatusCode != http.StatusCreated {
		t.Errorf("2 MiB less 100 bytes: status %d: %s", resp.StatusCode, body)
	}
}

func TestResults_NamesTheFileAfterTheDeviceAndTheScenario(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"a label with spaces and capitals", `{"device":"Pixel 8 Pro","scenario":"S3-4"}`, "pixel-8-pro-20261018-125100-s3-4.json"},
		{"the device object of a scenario result", `{"device":{"platform":"android","model":"SM-A536B","ua":"Mozilla/5.0"},"scenario":"S1"}`, "android-sm-a536b-20261018-125100-s1.json"},
		{"a device object without a model", `{"device":{"platform":"windows-tauri"},"scenario":"S2"}`, "windows-tauri-20261018-125100-s2.json"},
		{"no device and no scenario", `{"x":1}`, "unknown-20261018-125100-unknown.json"},
		{"names of the wrong type", `{"device":42,"scenario":["S1"]}`, "unknown-20261018-125100-unknown.json"},
		{"nothing usable in the names", `{"device":"///...","scenario":"é"}`, "unknown-20261018-125100-unknown.json"},
		{"a long name", `{"device":"` + strings.Repeat("abcd-", 20) + `","scenario":"S1"}`, strings.Repeat("abcd-", 8)[:39] + "-20261018-125100-s1.json"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hub := newTestHub(t, 1)
			if resp, body := hub.postResults(t, tc.body); resp.StatusCode != http.StatusCreated {
				t.Fatalf("status %d: %s", resp.StatusCode, body)
			}
			if files := resultFiles(t, hub.resultsDir); len(files) != 1 || files[0] != tc.want {
				t.Errorf("results folder holds %v, want %s", files, tc.want)
			}
		})
	}
}

func TestSanitiseName(t *testing.T) {
	for in, want := range map[string]string{
		"../../x":                      "x",
		"Pixel 8 Pro":                  "pixel-8-pro",
		"a_b.c":                        "a-b-c",
		"--a--b--":                     "a-b",
		"":                             "unknown",
		"///":                          "unknown",
		"S3-9":                         "s3-9",
		strings.Repeat("a", 50):        strings.Repeat("a", 40),
		strings.Repeat("a", 39) + "-b": strings.Repeat("a", 39), // no hyphen at the end after the cut
	} {
		if got := sanitiseName(in); got != want {
			t.Errorf("sanitiseName(%q) = %q, want %q", in, got, want)
		}
	}
}

// The flag from NewAPI is part of every results file, whatever the client sent.
func TestResults_ReportsWhetherMDNSWasUnavailable(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		hub := newTestHub(t, 1, func(c *hubConfig) { c.mdnsUnavailable = unavailable })
		const body = `{"device":"x","scenario":"S10","mdnsUnavailable":"client-value","samples":{"a":[1.50,2e3]},"n":12345678901234567890}`
		if resp, b := hub.postResults(t, body); resp.StatusCode != http.StatusCreated {
			t.Fatalf("status %d: %s", resp.StatusCode, b)
		}
		files := resultFiles(t, hub.resultsDir)
		if len(files) != 1 {
			t.Fatalf("results folder holds %v", files)
		}
		data, err := os.ReadFile(filepath.Join(hub.resultsDir, files[0]))
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("%v: %s", err, data)
		}
		want := "false"
		if unavailable {
			want = "true"
		}
		if string(got["mdnsUnavailable"]) != want {
			t.Errorf("mdnsUnavailable = %s, want %s (the hub's, not the client's)", got["mdnsUnavailable"], want)
		}
		// The values the client sent are kept as they were sent.
		if string(got["samples"]) != `{"a":[1.50,2e3]}` || string(got["n"]) != "12345678901234567890" || string(got["device"]) != `"x"` {
			t.Errorf("the other fields changed: %s", data)
		}
	}
}

// The stored file is the object as the client sent it, re-encoded: the keys
// sorted and the white space between tokens dropped, the values unchanged. A
// user agent string with < > & stays readable instead of turning into \u003c.
func TestResults_StoresTheValuesUnchanged(t *testing.T) {
	hub := newTestHub(t, 1)
	const body = `{"scenario":"S1",  "device":{"platform":"chrome","ua":"Mozilla <x> & co"},` +
		`"notes":["a < b", "tab\\tescape"], "samples":{"firstFrameMs":[310.25, 1e3]}}`
	if resp, b := hub.postResults(t, body); resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	files := resultFiles(t, hub.resultsDir)
	if len(files) != 1 {
		t.Fatalf("results folder holds %v", files)
	}
	data, err := os.ReadFile(filepath.Join(hub.resultsDir, files[0]))
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"device":{"platform":"chrome","ua":"Mozilla <x> & co"},"mdnsUnavailable":false,` +
		`"notes":["a < b","tab\\tescape"],"samples":{"firstFrameMs":[310.25,1e3]},"scenario":"S1"}` + "\n"
	if string(data) != want {
		t.Errorf("stored\n%s\nwant\n%s", data, want)
	}
}

func TestResults_RejectsABodyThatIsNotAJSONObject(t *testing.T) {
	hub := newTestHub(t, 1)
	for _, body := range []string{``, `nope`, `[1,2]`, `"text"`, `null`, `{"device":`} {
		if resp, _ := hub.postResults(t, body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %q: status %d, want 400", body, resp.StatusCode)
		}
	}
	if files := resultFiles(t, hub.resultsDir); len(files) != 0 {
		t.Errorf("rejected bodies left %v", files)
	}
}

// A result that arrives in the same second as another of the same device and
// scenario does not replace it: a run is never lost.
func TestResults_NeverOverwritesAFile(t *testing.T) {
	hub := newTestHub(t, 1)
	for i := range 3 {
		body := `{"device":"x","scenario":"S1","run":` + string(rune('0'+i)) + `}`
		if resp, b := hub.postResults(t, body); resp.StatusCode != http.StatusCreated {
			t.Fatalf("POST %d: status %d: %s", i, resp.StatusCode, b)
		}
	}
	want := []string{"x-20261018-125100-s1-2.json", "x-20261018-125100-s1-3.json", "x-20261018-125100-s1.json"}
	if files := resultFiles(t, hub.resultsDir); strings.Join(files, ",") != strings.Join(want, ",") {
		t.Errorf("results folder holds %v, want %v", files, want)
	}
	data, err := os.ReadFile(filepath.Join(hub.resultsDir, "x-20261018-125100-s1.json"))
	if err != nil || !strings.Contains(string(data), `"run":0`) {
		t.Errorf("the first file was replaced: %s (%v)", data, err)
	}
}

func TestResults_ReportsAFolderThatCannotBeWritten(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	hub := newTestHub(t, 1, func(c *hubConfig) { c.resultsDir = filepath.Join(blocker, "results") })

	resp, body := hub.postResults(t, `{"device":"x","scenario":"S1"}`)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
	if strings.Contains(string(body), blocker) {
		t.Errorf("the response names the folder: %s", body)
	}
	if !strings.Contains(hub.logs.String(), "level=ERROR") {
		t.Errorf("the failure is not logged:\n%s", hub.logs.String())
	}
}

// fullFolderLine is the start of the line that the hub window shows when the
// results folder is full.
const fullFolderLine = "Atenție: folderul de rezultate a atins limita"

// checkFull fails unless a refused result got 507 with a JSON error.
func checkFull(t *testing.T, resp *http.Response, body []byte) {
	t.Helper()
	if resp.StatusCode != http.StatusInsufficientStorage {
		t.Fatalf("status %d: %s, want 507", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %q, want application/json", ct)
	}
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil || e.Error == "" {
		t.Errorf("body %s is not a JSON error (%v)", body, err)
	}
}

// The results folder holds at most 5000 files and 500 MB (hub-8, privacy-2):
// a LAN client cannot fill the PC's disk. A refused result is not written, the
// app keeps it as «Rezultate netrimise», and the hub window says so once.
func TestResults_RefusesAResultOverTheFileCap(t *testing.T) {
	hub := newTestHub(t, 1, func(c *hubConfig) { c.resultsFileCap = 3 })
	for i := range 3 {
		if resp, body := hub.postResults(t, fmt.Sprintf(`{"device":"x","scenario":"S%d"}`, i+1)); resp.StatusCode != http.StatusCreated {
			t.Fatalf("result %d: status %d: %s", i+1, resp.StatusCode, body)
		}
	}

	for range 2 {
		resp, body := hub.postResults(t, `{"device":"x","scenario":"S4"}`)
		checkFull(t, resp, body)
	}
	if files := resultFiles(t, hub.resultsDir); len(files) != 3 {
		t.Errorf("results folder holds %v, want the 3 files from before the cap", files)
	}
	console := hub.console.String()
	if n := strings.Count(console, fullFolderLine); n != 1 {
		t.Errorf("the hub window has %d lines about the full folder, want 1:\n%s", n, console)
	}
	if !strings.Contains(console, "3 fișiere sau 500 MB") {
		t.Errorf("the hub window does not name the limits:\n%s", console)
	}
}

func TestResults_RefusesAResultOverTheByteCap(t *testing.T) {
	hub := newTestHub(t, 1, func(c *hubConfig) { c.resultsByteCap = 1000 })
	pad := strings.Repeat("a", 500)
	if resp, body := hub.postResults(t, `{"device":"x","scenario":"S1","pad":"`+pad+`"}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}

	resp, body := hub.postResults(t, `{"device":"x","scenario":"S2","pad":"`+pad+`"}`)
	checkFull(t, resp, body)
	if files := resultFiles(t, hub.resultsDir); len(files) != 1 {
		t.Errorf("results folder holds %v, want the one file from before the cap", files)
	}
	// A small result still fits.
	if resp, body := hub.postResults(t, `{"device":"x","scenario":"S3"}`); resp.StatusCode != http.StatusCreated {
		t.Errorf("a result that fits: status %d: %s", resp.StatusCode, body)
	}
	if n := strings.Count(hub.console.String(), fullFolderLine); n != 1 {
		t.Errorf("the hub window has %d lines about the full folder, want 1:\n%s", n, hub.console.String())
	}
}

// The files of earlier runs count: the folder is the one next to spikehub.exe.
func TestResults_CountsTheFilesAlreadyInTheFolder(t *testing.T) {
	hub := newTestHub(t, 1, func(c *hubConfig) { c.resultsFileCap = 2 })
	if err := os.MkdirAll(hub.resultsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old-1.json", "old-2.json"} {
		if err := os.WriteFile(filepath.Join(hub.resultsDir, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	resp, body := hub.postResults(t, `{"device":"x","scenario":"S1"}`)
	checkFull(t, resp, body)
}

func TestResults_CapsAre5000FilesAnd500MB(t *testing.T) {
	h := newHub(hubConfig{})
	if h.resultsFileCap != 5000 || h.resultsByteCap != 500<<20 {
		t.Errorf("caps = %d files and %d bytes, want 5000 and 500 MiB", h.resultsFileCap, h.resultsByteCap)
	}
}

func TestRomanianCount(t *testing.T) {
	for n, want := range map[int]string{
		1:    "1 fișier",
		3:    "3 fișiere",
		19:   "19 fișiere",
		20:   "20 de fișiere",
		101:  "101 fișiere",
		120:  "120 de fișiere",
		5000: "5000 de fișiere",
	} {
		if got := romanianCount(n, "fișier", "fișiere"); got != want {
			t.Errorf("romanianCount(%d) = %q, want %q", n, got, want)
		}
	}
}
