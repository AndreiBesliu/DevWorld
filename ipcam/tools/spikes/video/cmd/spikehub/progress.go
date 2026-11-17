package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/deviceplan"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/video/resultrules"
)

// The automatic run (post-release task A) asks the hub, before it starts,
// which scenarios this device has already completed, so that it skips them;
// and the page shows «Rămase pe acest PC:», the devices of the guide that
// still lack results. Both come from GET /spike/v1/progress, which reads the
// results folder: the files of every release, v0.1.0's included. A scenario
// is complete when the report can use its run, and a result belongs to the
// device that the report keys it to: both are spikereport's own rules
// (resultrules), so that the run skips nothing that the report cannot judge.

// scenarioOrder is the order of the scenario ids in the answers (results.ts SCENARIO_IDS).
var scenarioOrder = []string{"S1", "S2", "S3-4", "S3-9", "S3-16", "S4", "S5", "S6", "S7", "S8", "S9", "S10"}

// maxProgressFileBytes is the largest results file that the progress reads;
// the hub stores at most 2 MiB, which its re-encoding hardly changes.
const maxProgressFileBytes = 8 << 20

// progressGroup is what one device has completed. The phone and the Windows
// shell are told apart by their model too; the browsers report none.
type progressGroup struct {
	Key       string   `json:"key"`
	Platform  string   `json:"platform"`
	Model     string   `json:"model,omitempty"`
	Scenarios []string `json:"scenarios"`
}

// progressDevice is a device of the guide and the scenarios of its run that no
// result has completed, in the order of the run.
type progressDevice struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	PC        bool     `json:"pc"`
	Remaining []string `json:"remaining"`
}

// progressAnswer is the body of GET /spike/v1/progress.
type progressAnswer struct {
	// Version is the hub's release.
	Version string `json:"version"`
	// Complete lists, per device key and model, the scenarios with a complete result.
	Complete []progressGroup `json:"complete"`
	// Devices are the guide's devices (deviceplan) with what each still lacks;
	// a device is its platform's key, whatever the model.
	Devices []progressDevice `json:"devices"`
}

// cachedFacts is a results file as read: the facts, or nothing for a file that
// is not a result. Files are written once and never changed (writeResult), so
// the name, size and time say whether a file was read already.
type cachedFacts struct {
	size    int64
	modTime time.Time
	facts   *resultrules.Facts
}

// readFacts reads the facts of the results file name, or nil when it is not a
// result of a known scenario and platform.
func readFacts(path string) *resultrules.Facts {
	data, err := os.ReadFile(path) //nolint:gosec // a file of the results folder
	if err != nil {
		return nil
	}
	var r resultrules.Facts
	if json.Unmarshal(data, &r) != nil || !slices.Contains(scenarioOrder, r.Scenario) || r.Device.Platform == "" {
		return nil
	}
	return &r
}

// progress reads the results folder. Files that are not results are passed over.
func (h *hub) progress() (progressAnswer, error) {
	entries, err := os.ReadDir(h.resultsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return progressAnswer{}, err
	}
	h.progressMu.Lock()
	defer h.progressMu.Unlock()
	if h.progressCache == nil {
		h.progressCache = map[string]cachedFacts{}
	}
	type groupID struct{ key, platform, model string }
	done := map[groupID]map[string]bool{}
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() > maxProgressFileBytes {
			continue
		}
		seen[name] = true
		cached, ok := h.progressCache[name]
		if !ok || cached.size != info.Size() || !cached.modTime.Equal(info.ModTime()) {
			cached = cachedFacts{size: info.Size(), modTime: info.ModTime(), facts: readFacts(filepath.Join(h.resultsDir, name))}
			h.progressCache[name] = cached
		}
		r := cached.facts
		if r == nil || r.Class() != resultrules.Usable {
			continue
		}
		id := groupID{r.Key(), r.Device.Platform, r.Device.Model}
		if done[id] == nil {
			done[id] = map[string]bool{}
		}
		done[id][r.Scenario] = true
	}
	for name := range h.progressCache {
		if !seen[name] {
			delete(h.progressCache, name)
		}
	}

	answer := progressAnswer{Version: deviceplan.Version(), Complete: []progressGroup{}, Devices: []progressDevice{}}
	inOrder := func(set map[string]bool) []string {
		return slices.DeleteFunc(slices.Clone(scenarioOrder), func(s string) bool { return !set[s] })
	}
	byKey := map[string]map[string]bool{}
	for id, set := range done {
		answer.Complete = append(answer.Complete, progressGroup{Key: id.key, Platform: id.platform, Model: id.model, Scenarios: inOrder(set)})
		if byKey[id.key] == nil {
			byKey[id.key] = map[string]bool{}
		}
		for s := range set {
			byKey[id.key][s] = true
		}
	}
	slices.SortFunc(answer.Complete, func(a, b progressGroup) int {
		if c := strings.Compare(a.Key, b.Key); c != 0 {
			return c
		}
		return strings.Compare(a.Model, b.Model)
	})
	for _, d := range deviceplan.Devices() {
		completed := byKey[resultrules.Key(d.Platform, "")]
		remaining := slices.DeleteFunc(d.Scenarios(), func(s string) bool { return completed[s] })
		answer.Devices = append(answer.Devices, progressDevice{ID: d.ID, Label: d.Label, PC: d.PC, Remaining: remaining})
	}
	return answer, nil
}

// handleProgress answers GET /spike/v1/progress (progressAnswer).
func (h *hub) handleProgress(w http.ResponseWriter, _ *http.Request) {
	answer, err := h.progress()
	if err != nil {
		h.log.Error("cannot read the results folder", "err", err)
		http.Error(w, "the results folder cannot be read", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(answer); err != nil {
		h.log.Debug("send the progress", "err", err)
	}
}
