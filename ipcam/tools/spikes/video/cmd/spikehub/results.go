package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxResultsBytes is the largest results body the hub accepts.
const maxResultsBytes = 2 << 20

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
// of browsers, to every file, over any value that the client sent; it leaves
// the rest as it came. Nothing in a file comes from the hub's cameras, so the URL
// of a real camera cannot be in it (PH0-22).
//
// A second result of one device and scenario in the same second gets the
// suffix -2, -3 and so on: a run is never lost to an overwrite.
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
	out, err := json.Marshal(fields)
	if err != nil {
		http.Error(w, "the result cannot be encoded", http.StatusInternalServerError)
		return
	}

	stem := fmt.Sprintf("%s-%s-%s", deviceName(fields["device"]), h.now().UTC().Format("20060102-150405"), scenarioName(fields["scenario"]))
	name, err := h.writeResult(stem, append(out, '\n'))
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
