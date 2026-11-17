package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
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
