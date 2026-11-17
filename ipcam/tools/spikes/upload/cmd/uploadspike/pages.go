// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/runstate"
)

// The pages, embedded; each starts with the plan's marker comment, which
// html/template drops from the answers.
//
//go:embed web/*.html
var webFS embed.FS

var pages = template.Must(template.ParseFS(webFS, "web/*.html"))

// labels are the pages' texts, from labels.go's constants (Task 12).
type labels struct {
	Title, SetupTitle, SetupHelp, SetupFiles, SetupSave, SetupSaved, SetupFailed, SetupMicrosoft, SetupID, SetupSecret     string
	DeviceTitle, DeviceHelp, ReaderTitle, ReaderHelp, ReaderStart, ReaderDone, ReaderResumed, ReaderSignIn, StatusTitle    string
	NoClient, VisibleWarning, ClientAndroid, ClientDesktop, ClientTV, ClientWeb, ClientUnknown, ClientDetected, AutoButton string
}

var pageLabels = labels{
	Title: pageTitle, SetupTitle: pageSetupTitle, SetupHelp: pageSetupHelp, SetupFiles: pageSetupFiles, SetupSave: pageSetupSave,
	SetupSaved: pageSetupSaved, SetupFailed: pageSetupFailed, SetupMicrosoft: pageSetupMicrosoft, SetupID: pageSetupID,
	SetupSecret: pageSetupSecret, DeviceTitle: pageDeviceTitle, DeviceHelp: pageDeviceHelp, ReaderTitle: pageReaderTitle,
	ReaderHelp: pageReaderHelp, ReaderStart: pageReaderStart, ReaderDone: pageReaderDone, ReaderResumed: pageReaderResumed,
	ReaderSignIn: pageReaderSignIn, StatusTitle: pageStatusTitle, NoClient: pageNoClient, VisibleWarning: pageVisibleWarning,
	ClientAndroid: pageClientAndroid, ClientDesktop: pageClientDesktop, ClientTV: pageClientTV, ClientWeb: pageClientWeb,
	ClientUnknown: pageClientUnknown, ClientDetected: pageClientDetected, AutoButton: pageSetupAutoButton,
}

// readerConfig is the web reader page's settings: the web client's ID (no
// secret), and the uploadspike whose targets it reads and to which it
// posts its cells: "" for this one, else the Pi's (`mi6 reader --hub`).
type readerConfig struct {
	WebClientID string `json:"webClientId"`
	Remote      string `json:"remote"`
}

// pageData is what a page shows.
type pageData struct {
	L      labels
	Title  string
	Lines  []string
	Device hubproc.BeginResult
	Reader readerConfig
}

// renderPage answers the page name with data.
func renderPage(w http.ResponseWriter, name string, data pageData) {
	data.L = pageLabels
	var b bytes.Buffer
	if err := pages.ExecuteTemplate(&b, name, data); err != nil {
		http.Error(w, "the page cannot be shown", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	_, _ = w.Write(b.Bytes())
}

// DevicePage shows a device login's verification URL and user code.
func (h *holder) DevicePage(w http.ResponseWriter, res hubproc.BeginResult) {
	renderPage(w, "device.html", pageData{Title: pageDeviceTitle, Device: res})
}

// ReaderPage serves the web reader (P21), with the web client's ID.
func (h *holder) ReaderPage(w http.ResponseWriter, _ *http.Request) {
	st, err := runstate.Load(h.e.statePath())
	if err != nil {
		http.Error(w, "the run state cannot be read", http.StatusInternalServerError)
		return
	}
	renderPage(w, "reader.html", pageData{Title: pageReaderTitle, Reader: readerConfig{WebClientID: st.WebClientID, Remote: h.remoteReader}})
}

// CallbackPage shows OneDrive's web login's result.
func (h *holder) CallbackPage(w http.ResponseWriter, ok bool) {
	line := pageCallbackFailed
	if ok {
		line = pageCallbackOK
	}
	renderPage(w, "status.html", pageData{Title: pageTitle, Lines: []string{line}})
}
