package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/mi6"
)

// TestPages_RenderTheirLabels: each embedded page renders with labels.go's
// constants (Task 12 checks the guide against them), a reader page carries
// its client ID as a JS string, and the plan's marker comment stays out of
// the answers.
func TestPages_RenderTheirLabels(t *testing.T) {
	for _, tc := range []struct {
		page string
		data pageData
		want []string
	}{
		{"setup.html", pageData{Title: pageSetupTitle, Lines: googleTypes}, []string{pageSetupTitle, pageSetupHelp, pageSetupAutoButton, `name="desktop-secret"`, `name="tv-secret"`}},
		{"device.html", pageData{Title: pageDeviceTitle, Device: hubproc.BeginResult{VerificationURL: "https://example.invalid/device", UserCode: "ABCD-EFGH"}},
			[]string{pageDeviceHelp, "ABCD-EFGH", "https://example.invalid/device"}},
		{"reader.html", pageData{Title: pageReaderTitle, Reader: readerConfig{WebClientID: "web-client-0001.apps.example"}},
			[]string{pageReaderHelp, pageVisibleWarning, `"webClientId":"web-client-0001.apps.example"`}},
		{"status.html", pageData{Title: pageStatusTitle, Lines: []string{"MI6: ziua 1 din 7"}}, []string{pageStatusTitle, "MI6: ziua 1 din 7"}},
	} {
		w := httptest.NewRecorder()
		renderPage(w, tc.page, tc.data)
		body := w.Body.String()
		if w.Code != 200 || strings.Contains(body, "SPIKE"+"-ONLY") {
			t.Errorf("%s: %d, marker kept %v", tc.page, w.Code, strings.Contains(body, "SPIKE"+"-ONLY"))
		}
		for _, s := range tc.want {
			if !strings.Contains(body, s) {
				t.Errorf("%s lacks %q", tc.page, s)
			}
		}
		if strings.Contains(tc.page, "setup") && strings.Contains(body, `name="web-secret"`) {
			t.Errorf("setup.html asks the web client's secret")
		}
	}
}

// TestReaderPage_RestartsAfterInterruption (C2-R18 (d)): the reader page
// that goes to the background during a reading posts the interruption,
// once, and restarts the reading by itself when it comes back, also when
// it comes back before the interrupted attempt's fetch ended; the cells
// of every target are then posted and the page says «Gata». The page's
// own script runs in Node with stand-ins for the DOM, Google Identity
// Services and fetch (testdata/reader-harness.mjs).
func TestReaderPage_RestartsAfterInterruption(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("UPLOADSPIKE_REQUIRE_NODE") == "1" {
			t.Fatalf("UPLOADSPIKE_REQUIRE_NODE=1, but node is missing: %v", err)
		}
		t.Skip("node is missing")
	}
	w := httptest.NewRecorder()
	renderPage(w, "reader.html", pageData{Title: pageReaderTitle, Reader: readerConfig{WebClientID: "web-client-0001.apps.example"}})
	page := filepath.Join(t.TempDir(), "reader.html")
	if err := os.WriteFile(page, w.Body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"in-flight", "after"} {
		t.Run(scenario, func(t *testing.T) {
			out, err := exec.Command(node, filepath.Join("testdata", "reader-harness.mjs"), page, scenario).CombinedOutput() //nolint:gosec // the test's own harness
			if err != nil {
				t.Fatalf("the harness: %v\n%s", err, out)
			}
			var got struct {
				Cells         []string `json:"cells"`
				Interruptions int      `json:"interruptions"`
				TargetCalls   int      `json:"targetCalls"`
				Done, Resumed bool
				Error         string `json:"error"`
			}
			if err := json.Unmarshal(out, &got); err != nil || got.Error != "" {
				t.Fatalf("the harness said %s (%v)", out, err)
			}
			if !slices.Equal(got.Cells, []string{"android", "tv"}) || got.Interruptions != 1 || got.TargetCalls != 2 || !got.Done || !got.Resumed {
				t.Errorf("after an interruption: %+v; want the two cells posted by a second attempt, one interruption posted, «Gata» and the restart line shown", got)
			}
		})
	}
}

// TestReaderInterruptions_InTheEnvelope (C2-R18 (d)): each interruption
// that the reader page posts is counted in the open MI6 run, said on the
// holder's terminal, and carried into the envelope, so an interrupted
// attempt stays recorded after its restart succeeded.
func TestReaderInterruptions_InTheEnvelope(t *testing.T) {
	r := newRig(t)
	r.openMI6("android")
	var out bytes.Buffer
	h := &holder{e: &env{base: r.base, out: &out, errOut: io.Discard, now: time.Now}}
	h.WebInterrupted()
	h.WebInterrupted()
	run, err := mi6.Decode(r.state().Open["MI6"])
	if err != nil || run.ReaderInterruptions != 2 {
		t.Fatalf("the run: %+v, %v; want 2 reader interruptions", run, err)
	}
	if e := run.Envelope(mi6.Meta{Host: "pc", Set: "pc", OS: "windows", Storage: "ssd"}, nil, nil, time.Now().UnixMilli()); e.MI6.ReaderInterruptions != 2 {
		t.Errorf("the envelope's reader interruptions: %d; want 2", e.MI6.ReaderInterruptions)
	}
	if n := strings.Count(out.String(), msgReaderInterrupted); n != 2 {
		t.Errorf("the holder said %q %d times; want 2\n%s", msgReaderInterrupted, n, out.String())
	}
}
