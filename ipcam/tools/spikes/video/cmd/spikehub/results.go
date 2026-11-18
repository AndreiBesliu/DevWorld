package main

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
)

// maxResultsBytes is the largest results body the hub accepts.
const maxResultsBytes = 2 << 20

// The caps of the results folder (hub-8, privacy-2): a run of the guide stores
// a few hundred files of at most a few hundred kB, and any LAN client may
// post, so the caps keep a stray or hostile one from filling the PC's disk.
const (
	defaultResultsFileCap = 5000
	defaultResultsByteCap = 500 << 20 // 500 MiB
)

// maxNameLength is the longest device or scenario part of a results file name.
const maxNameLength = 40

// sanitiseName turns a device or scenario field into a file name part of
// [a-z0-9-]{1,40}: lower case, every other run of characters becomes one hyphen,
// no hyphen at either end, "unknown" when nothing is left. A path such as
// "../../x" becomes "x", so a client cannot name a file outside the results folder.
func sanitiseName(s string) string {
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
	if len(name) > maxNameLength {
		name = strings.TrimRight(name[:maxNameLength], "-")
	}
	if name == "" {
		return "unknown"
	}
	return name
}

// deviceName names the device of a result. The "device" field is a label, or
// the object of a scenario result: its platform and model.
func deviceName(raw json.RawMessage) string {
	var label string
	if json.Unmarshal(raw, &label) == nil {
		return sanitiseName(label)
	}
	var device struct {
		Platform string `json:"platform"`
		Model    string `json:"model"`
	}
	if json.Unmarshal(raw, &device) == nil && device.Platform != "" {
		return sanitiseName(device.Platform + " " + device.Model)
	}
	return sanitiseName("")
}

// scenarioName names the scenario of a result, a string such as "S3-4".
func scenarioName(raw json.RawMessage) string {
	var scenario string
	_ = json.Unmarshal(raw, &scenario)
	return sanitiseName(scenario)
}

// handleResults stores a result as results/<device>-<yyyymmdd-hhmmss>-<scenario>.json,
// with the time in UTC. The body is a JSON object of at most 2 MiB. The hub adds
// the field "mdnsUnavailable", whether it could not resolve the .local candidates
// of browsers, to every file, over any value that the client sent. The object
// is stored re-encoded, on one line with its keys sorted and no white space
// between tokens. The values keep their meaning and their numbers keep their
// digits; strings are not HTML-escaped, so <, > and & stay as they came. Nothing
// in a file comes from the hub's cameras, so the URL of a real camera cannot be
// in it (PH0-22).
//
// A second result of one device and scenario in the same second gets the
// suffix -2, -3 and so on: a run is never lost to an overwrite.
//
// A result that would take the folder over resultsFileCap files or
// resultsByteCap bytes, counting the files of earlier runs, is refused with 507
// and a JSON error, and nothing is written; the app keeps it as unsent. The
// hub window says so once.
func (h *hub) handleResults(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxResultsBytes))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, "the result is larger than 2 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "the request body cannot be read", http.StatusBadRequest)
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		http.Error(w, "the result must be a JSON object", http.StatusBadRequest)
		return
	}
	fields["mdnsUnavailable"] = json.RawMessage(fmt.Sprint(h.mdnsUnavailable))
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(fields); err != nil { // Encode ends the line
		http.Error(w, "the result cannot be encoded", http.StatusInternalServerError)
		return
	}

	stem := fmt.Sprintf("%s-%s-%s", deviceName(fields["device"]), h.now().UTC().Format("20060102-150405"), scenarioName(fields["scenario"]))
	h.resultsMu.Lock()
	defer h.resultsMu.Unlock()
	files, size, err := folderUsage(h.resultsDir)
	if err != nil {
		h.log.Error("cannot read the results folder", "err", err)
		http.Error(w, "the result cannot be stored", http.StatusInternalServerError)
		return
	}
	if files+1 > h.resultsFileCap || size+int64(out.Len()) > h.resultsByteCap {
		h.refuseFull(w)
		return
	}
	name, err := h.writeResult(stem, out.Bytes())
	if err != nil {
		h.log.Error("cannot store a result", "err", err)
		http.Error(w, "the result cannot be stored", http.StatusInternalServerError)
		return
	}
	h.log.Info("result stored", "file", name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"file": name})
}

// refuseFull answers a result that does not fit in the results folder, and
// tells the owner once, in the hub window.
func (h *hub) refuseFull(w http.ResponseWriter) {
	h.log.Debug("a result was refused: the results folder is full")
	if !h.toldFull {
		h.toldFull = true
		_, _ = fmt.Fprintf(h.console, "Atenție: folderul de rezultate a atins limita de %s sau %d MB, deci spikehub "+
			"nu mai salvează rezultate noi. Scrieți-ne în chat.\n", romanianCount(h.resultsFileCap, "fișier", "fișiere"), h.resultsByteCap>>20)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInsufficientStorage)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": fmt.Sprintf("the results folder is full: at most %d files and %d MiB", h.resultsFileCap, h.resultsByteCap>>20),
	})
}

// romanianCount writes n and the noun in Romanian: "1 fișier", "3 fișiere",
// "20 de fișiere"; "de" goes before the plural when the last two digits of n
// are 00 or 20 to 99, except for n under 20.
func romanianCount(n int, singular, plural string) string {
	switch rest := n % 100; {
	case n == 1:
		return "1 " + singular
	case n >= 20 && (rest == 0 || rest >= 20):
		return fmt.Sprintf("%d de %s", n, plural)
	default:
		return fmt.Sprintf("%d %s", n, plural)
	}
}

// folderUsage returns the number of files in dir and their total size. A
// folder that does not exist yet holds nothing.
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
			continue // removed since the listing
		}
		if err != nil {
			return 0, 0, err
		}
		files++
		size += info.Size()
	}
	return files, size, nil
}

// writeResult creates results/<stem>.json, or <stem>-2.json and so on when that
// exists, and returns the name of the file.
func (h *hub) writeResult(stem string, data []byte) (string, error) {
	if err := os.MkdirAll(h.resultsDir, 0o750); err != nil {
		return "", err
	}
	for n := 1; ; n++ {
		name := stem + ".json"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.json", stem, n)
		}
		f, err := os.OpenFile(filepath.Join(h.resultsDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the name is sanitised
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
