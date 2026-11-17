// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package handoff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The caps of the app's results, c1's controller.AppAPI's and spikehub's
// (hub-8): any LAN client may post while the LAN listener is open, so
// they keep a stray or hostile one from filling the disk.
const (
	maxAppResultBytes = 2 << 20
	appResultsFileCap = 5000
	appResultsByteCap = 500 << 20
	maxAppNameLength  = 40
)

// appResults keeps the video app's results under <results>/app, as c1's
// controller.AppAPI keeps them (a copy, P01).
type appResults struct {
	dir     string
	fileCap int
	byteCap int64
	now     func() time.Time
	mu      sync.Mutex // the caps' check and the write are one step
}

func newAppResults(resultsDir string, now func() time.Time) *appResults {
	return &appResults{dir: filepath.Join(resultsDir, "app"), fileCap: appResultsFileCap, byteCap: appResultsByteCap, now: now}
}

// handle stores a result, a JSON object of at most 2 MiB, as
// <device>-<yyyymmdd-hhmmss>-<scenario>.json in UTC, re-encoded on one line
// with its keys sorted and without HTML escapes; a second one of the same
// device, scenario and second gets -2, -3 and so on. A result that would
// take the folder past 5,000 files or 500 MiB is refused with 507.
func (a *appResults) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAppResultBytes))
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "the result is larger than 2 MiB"})
		return
	case err != nil:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the request body cannot be read"})
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the result must be a JSON object"})
		return
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(fields); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the result cannot be encoded"})
		return
	}
	stem := fmt.Sprintf("%s-%s-%s", deviceName(fields["device"]), a.now().UTC().Format("20060102-150405"), fileNamePart(scenario(fields["scenario"])))
	a.mu.Lock()
	defer a.mu.Unlock()
	files, size, err := folderUsage(a.dir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the result cannot be stored"})
		return
	}
	if files+1 > a.fileCap || size+int64(out.Len()) > a.byteCap {
		writeJSON(w, http.StatusInsufficientStorage, map[string]string{
			"error": fmt.Sprintf("the results folder is full: at most %d files and %d MiB", a.fileCap, a.byteCap>>20)})
		return
	}
	name, err := writeNew(a.dir, stem, out.Bytes())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the result cannot be stored"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"file": name})
}

// fileNamePart turns a field into a file name part of [a-z0-9-]{1,40}:
// lower case, every other run of characters one hyphen, none at either
// end, "unknown" when nothing is left.
func fileNamePart(s string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			hyphen = false
			b.WriteRune(r)
		} else {
			hyphen = true
		}
	}
	name := b.String()
	if len(name) > maxAppNameLength {
		name = strings.TrimRight(name[:maxAppNameLength], "-")
	}
	if name == "" {
		return "unknown"
	}
	return name
}

// deviceName names a result's device: a label, or the scenario result's
// object, its platform and model.
func deviceName(raw json.RawMessage) string {
	var label string
	if json.Unmarshal(raw, &label) == nil {
		return fileNamePart(label)
	}
	var device struct {
		Platform string `json:"platform"`
		Model    string `json:"model"`
	}
	if json.Unmarshal(raw, &device) == nil && device.Platform != "" {
		return fileNamePart(device.Platform + " " + device.Model)
	}
	return fileNamePart("")
}

// scenario is a result's scenario, a string such as "OAUTH".
func scenario(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// folderUsage is the number of files in dir and their total size; a folder
// that does not exist yet holds nothing.
func folderUsage(dir string) (files int, size int64, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		files++
		size += info.Size()
	}
	return files, size, nil
}

// writeNew creates dir/<stem>.json, or <stem>-2.json and so on when that
// exists, 0600, and returns the file's name.
func writeNew(dir, stem string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	for n := 1; ; n++ {
		name := stem + ".json"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.json", stem, n)
		}
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the name is sanitised
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return "", err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(f.Name())
			return "", err
		}
		return name, nil
	}
}
