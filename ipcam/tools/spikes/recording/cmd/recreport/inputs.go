// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi1"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/mi4"
	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// maxFileBytes bounds a results file that is read: an envelope or an app
// result is far smaller (reccontroller keeps app results under 2 MiB).
const maxFileBytes = 8 << 20

// inputs are the results that the report judges and how they were read.
type inputs struct {
	// envs are the envelopes, each once (G15), renumbered where merged
	// folders shared a run id (renumbered): what the judges read.
	envs []results.Envelope
	// folders and ciRuns are what was read: results folders and workflow
	// runs.
	folders, ciRuns int
	// copies is how many envelopes came more than once, identical, and
	// were read once.
	copies int
	// renumbered are the sets whose run ids recreport renumbered.
	renumbered []renumbering
	// conflicts are MI1 sets with runs that came with different results.
	conflicts []conflict
	// incomplete are the lab sets that lack parts.
	incomplete []labSet
	// unjudged counts the MI1 envelopes of sets that G10 does not judge,
	// by set: CI's smoke runs.
	unjudged map[string]int
	// ageRuns counts the app's AGE results, and ageUnmeasured those that
	// measured nothing (interrupted, or without mbps).
	ageRuns, ageUnmeasured int
	// ci holds what the annotations carried besides envelopes.
	ci ciNotices
	// skipped are the files that are not results.
	skipped []skipped
}

// skipped is a file of a results folder that is not a result, by its path
// in the folder, never the folder's own path, which may name the owner.
type skipped struct {
	file string
	err  error
}

// renumbering is a set whose envelopes shared a run id: their run ids
// before and after, in the order of their time.
type renumbering struct {
	code, set string
	from, to  []string
}

// conflict is an MI1 set with runs whose results differ between copies.
type conflict struct {
	set  string
	runs int
}

// labSet is a lab set read from annotations, one run per part, that lacks
// some of its parts.
type labSet struct {
	set     string
	parts   int
	missing []int
}

// ageResult is the app's AGE result as reccontroller stored it under app/.
type ageResult struct {
	name   string
	stored time.Time
	result struct {
		Scenario    string   `json:"scenario"`
		Interrupted bool     `json:"interrupted"`
		Mbps        *float64 `json:"mbps"`
		Device      struct {
			Platform string `json:"platform"`
		} `json:"device"`
	}
}

// ageName is the name reccontroller gives an AGE result:
// <device>-<yyyymmdd-hhmmss>-age.json in UTC, -2, -3… for one of the same
// second.
var ageName = regexp.MustCompile(`-(\d{8}-\d{6})-age(?:-\d+)?\.json$`)

// load reads the results folders dirs and the annotations of the workflow
// runs, then makes each envelope one: identical copies are read once, and
// sets whose different envelopes share a run id are renumbered by time.
func load(dirs, runs []string) (*inputs, error) {
	in := &inputs{folders: len(dirs), ciRuns: len(runs)}
	var envs []results.Envelope
	var ages []ageResult
	for _, dir := range dirs {
		e, a, s, err := readFolder(dir)
		if err != nil {
			return nil, err
		}
		envs, ages, in.skipped = append(envs, e...), append(ages, a...), append(in.skipped, s...)
	}
	for _, id := range runs {
		e, notices, err := readRun(id)
		if err != nil {
			return nil, err
		}
		envs = append(envs, e...)
		in.ci.add(notices)
	}
	envs = append(envs, in.ageEnvelopes(ages)...)
	envs, in.copies = dedup(envs)
	in.conflicts = conflicts(envs)
	envs, in.renumbered = renumber(envs)
	in.incomplete = labParts(envs)
	in.unjudged = unjudged(envs)
	in.envs = envs
	return in, nil
}

// stateDir is where recspike keeps its own state in a results folder (the
// run state, MI2's runs): no result.
const stateDir = "state"

// readFolder reads the envelopes of the results folder dir, at any depth,
// and the AGE results in its app/ folders; recspike's state/ folders are
// left out. A JSON file that is neither is skipped, named by its path in
// dir.
func readFolder(dir string) ([]results.Envelope, []ageResult, []skipped, error) {
	var envs []results.Envelope
	var ages []ageResult
	var skip []skipped
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		switch {
		case walkErr != nil:
			return walkErr
		case d.IsDir() && d.Name() == stateDir:
			return filepath.SkipDir
		case d.IsDir() || filepath.Ext(path) != ".json":
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := readSmall(path)
		if err != nil {
			skip = append(skip, skipped{rel, err})
			return nil
		}
		if filepath.Base(filepath.Dir(path)) == "app" {
			a := ageResult{name: d.Name()}
			if json.Unmarshal(data, &a.result) == nil && a.result.Scenario == "AGE" {
				if m := ageName.FindStringSubmatch(d.Name()); m != nil {
					a.stored, _ = time.Parse("20060102-150405", m[1])
				}
				ages = append(ages, a)
			}
			return nil
		}
		e, err := results.Parse(data)
		if err != nil {
			skip = append(skip, skipped{rel, err})
			return nil
		}
		envs = append(envs, e)
		return nil
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("cannot read a results folder: %w", err)
	}
	return envs, ages, skip, nil
}

// readSmall reads a results file of at most maxFileBytes.
func readSmall(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileBytes {
		return nil, fmt.Errorf("larger than %d MiB", maxFileBytes>>20)
	}
	return os.ReadFile(path) //nolint:gosec // a file of the results folder that the owner named
}

// ageEnvelopes turns the app's AGE results into MI4 envelopes with
// mi4.AgerangeRun (G24): the same file from two folders once, numbered
// from 1 in the order reccontroller stored them, each stamped with the time
// in its name. A result that measured nothing has none.
func (in *inputs) ageEnvelopes(ages []ageResult) []results.Envelope {
	slices.SortFunc(ages, func(a, b ageResult) int {
		if c := a.stored.Compare(b.stored); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	})
	ages = slices.CompactFunc(ages, func(a, b ageResult) bool { return a.name == b.name })
	var out []results.Envelope
	for _, a := range ages {
		if a.result.Interrupted || a.result.Mbps == nil {
			in.ageUnmeasured++
			continue
		}
		in.ageRuns++
		out = append(out, mi4.AgerangeRun(in.ageRuns, a.stored, a.result.Device.Platform, *a.result.Mbps))
	}
	return out
}

// identity is what names an envelope: its code, set and run id.
type identity struct{ code, set, runID string }

func idOf(e results.Envelope) identity { return identity{e.Code, e.Set, e.RunID} }

// dedup keeps each envelope once (G15): a copy identical to one already
// read, as when the Pi's folder is read both in the PC's and on its own,
// is dropped and counted. Different envelopes with one identity stay.
func dedup(envs []results.Envelope) ([]results.Envelope, int) {
	seen := map[identity][]string{}
	var out []results.Envelope
	copies := 0
	for _, e := range envs {
		key := stableKey(e)
		if slices.Contains(seen[idOf(e)], key) {
			copies++
			continue
		}
		seen[idOf(e)] = append(seen[idOf(e)], key)
		out = append(out, e)
	}
	return out, copies
}

// stableKey is e's JSON encoding.
func stableKey(e results.Envelope) string {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Sprintf("%+v", e)
	}
	return string(b)
}

// conflicts are the MI1 sets with different envelopes of one run id, as
// from two lab runs. They are not renumbered, since an MI1 run id names its
// position and attempt: G10 counts one of them (mi1.Judge), and the report
// names the set.
func conflicts(envs []results.Envelope) []conflict {
	count := map[identity]int{}
	for _, e := range envs {
		if e.Code == "MI1" {
			count[idOf(e)]++
		}
	}
	per := map[string]int{}
	for id, n := range count {
		if n > 1 {
			per[id.set]++
		}
	}
	var out []conflict
	for set, n := range per {
		out = append(out, conflict{set, n})
	}
	slices.SortFunc(out, func(a, b conflict) int { return strings.Compare(a.set, b.set) })
	return out
}

// renumber gives the envelopes of a set new run ids when two different
// ones share a run id (Task 10b): the judges take a set's latest run by
// the number that ends its run id before its time, so in merged folders an
// older run of a higher number would win. The set's envelopes are numbered
// from 1 in the order of their time (AtMs), then of their old number, then
// of their JSON, each keeping its run id's prefix. MI1 is left as it is
// (conflicts).
func renumber(envs []results.Envelope) ([]results.Envelope, []renumbering) {
	type group struct{ code, set string }
	members := map[group][]int{}
	shared := map[group]bool{}
	ids := map[identity]bool{}
	for i, e := range envs {
		g := group{e.Code, e.Set}
		members[g] = append(members[g], i)
		if ids[idOf(e)] && e.Code != "MI1" {
			shared[g] = true
		}
		ids[idOf(e)] = true
	}
	var out []renumbering
	for g := range shared {
		idx := members[g]
		slices.SortFunc(idx, func(a, b int) int {
			ea, eb := envs[a], envs[b]
			switch {
			case ea.AtMs != eb.AtMs:
				return compareInt(ea.AtMs, eb.AtMs)
			case runNumber(ea.RunID) != runNumber(eb.RunID):
				return compareInt(int64(runNumber(ea.RunID)), int64(runNumber(eb.RunID)))
			}
			return strings.Compare(stableKey(ea), stableKey(eb))
		})
		r := renumbering{code: g.code, set: g.set}
		for n, i := range idx {
			old := envs[i].RunID
			envs[i].RunID = fmt.Sprintf("%s-%d", runPrefix(old), n+1)
			r.from, r.to = append(r.from, old), append(r.to, envs[i].RunID)
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b renumbering) int { return strings.Compare(a.code+" "+a.set, b.code+" "+b.set) })
	return envs, out
}

func compareInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// runNumber is the number that ends a run id, 0 when none does.
func runNumber(id string) int {
	n, _ := strconv.Atoi(id[strings.LastIndexByte(id, '-')+1:])
	return n
}

// runPrefix is a run id without the number that ends it.
func runPrefix(id string) string {
	if i := strings.LastIndexByte(id, '-'); i >= 0 {
		if _, err := strconv.Atoi(id[i+1:]); err == nil {
			return id[:i]
		}
	}
	return id
}

// labSets are the lab sets that G10 judges; CI's smoke sets are not.
var labSets = []string{mi1.SetLabLinuxKill9, mi1.SetLabLinuxQEMUReset, mi1.SetLabWindowsKill9}

// g10Sets are every set that G10 judges.
var g10Sets = append(slices.Clone(labSets), mi1.SetPCKill9, mi1.SetPCBugcheck, mi1.SetPiKill9, mi1.SetPiSysrq, mi1.SetRealPC, mi1.SetRealPi)

// unjudged counts the MI1 envelopes of sets that G10 does not judge, such
// as CI's smoke runs (<set>-smoke), by set.
func unjudged(envs []results.Envelope) map[string]int {
	out := map[string]int{}
	for _, e := range envs {
		if e.Code == "MI1" && !slices.Contains(g10Sets, e.Set) {
			out[e.Set]++
		}
	}
	return out
}

// labParts lists the lab sets of G10, read as one-run parts of Parts (C32),
// that lack some of their parts: G10 judges such a set incomplete, and the
// report names it with the parts.
func labParts(envs []results.Envelope) []labSet {
	parts := map[string]int{}
	seen := map[string]map[int]bool{}
	for _, e := range envs {
		if e.Code != "MI1" || e.Parts == 0 || !slices.Contains(labSets, e.Set) {
			continue
		}
		if seen[e.Set] == nil {
			seen[e.Set] = map[int]bool{}
		}
		parts[e.Set] = max(parts[e.Set], e.Parts)
		seen[e.Set][e.Part] = true
	}
	var out []labSet
	for set, n := range parts {
		s := labSet{set: set, parts: n}
		for p := 1; p <= n; p++ {
			if !seen[set][p] {
				s.missing = append(s.missing, p)
			}
		}
		if len(s.missing) > 0 {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b labSet) int { return strings.Compare(a.set, b.set) })
	return out
}

// newest is the time of the newest result that says when it was made, zero
// when none does.
func (in *inputs) newest() time.Time {
	var ms int64
	for _, e := range in.envs {
		ms = max(ms, e.AtMs)
	}
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}
