// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// maxFileBytes bounds a results file that is read: an envelope is far
// smaller.
const maxFileBytes = 8 << 20

// Why a file is not read, in the report's Romanian. The error behind it is
// never shown: Validate's errors quote the field's value.
const (
	reasonNotJSONFile = "nu e un fișier .json"
	reasonNotJSON     = "nu e JSON valid"
	reasonNotEnvelope = "nu e un plic de rezultate valid"
	reasonUnreadable  = "nu s-a putut citi"
	reasonTooLarge    = "mai mare de 8 MiB"
	reasonNotLab      = "notița mi5-lab nu e un plic MI5 de pe lab-linux"
)

// appDir is the folder of the video app's results that uploadspike stores
// (Task 9): no envelopes.
const appDir = "app"

// read is an envelope, where it was read (a results file by its place
// among -in and its path in the folder, or a CI job), and its size in
// bytes, which breaks a tie of AtMs.
type read struct {
	env  results.Envelope
	from string
	size int64
}

// skipped is a file, or a notice, that is not read, and why.
type skipped struct{ file, reason string }

// dropped is a copy of a run that another copy of it replaced.
type dropped struct{ from, kept string }

// inputs are the results that the report judges and how they were read.
type inputs struct {
	// envs are the envelopes, one per run (dedup), in the order read.
	envs []results.Envelope
	// from names where each kept envelope was read, by code and run id.
	from map[runKey]string
	// ciRuns counts the workflow runs read.
	ciRuns int
	// dropped are the copies left out; unreadable the files that are no
	// envelope; appResults the app's results, by name.
	dropped    []dropped
	unreadable []skipped
	appResults []string
	// piProbe is set when uploadspike-probe's armv6 marker is among the
	// envelopes.
	piProbe bool
}

// runKey names a run: its code and run id.
type runKey struct{ code, runID string }

func keyOf(e results.Envelope) runKey { return runKey{e.Code, e.RunID} }

// load reads the results folders dirs and the mi5-lab notices of the
// workflow runs, then keeps one envelope per run (assemble).
func load(dirs, runs []string) (*inputs, error) {
	var reads []read
	var unreadable []skipped
	var apps []string
	for n, dir := range dirs {
		r, s, a, err := readFolder(dir, fmt.Sprintf("-in #%d: ", n+1))
		if err != nil {
			return nil, err
		}
		reads, unreadable, apps = append(reads, r...), append(unreadable, s...), append(apps, a...)
	}
	for _, id := range runs {
		r, s, err := readRun(id)
		if err != nil {
			return nil, err
		}
		reads, unreadable = append(reads, r...), append(unreadable, s...)
	}
	in := assemble(reads)
	in.ciRuns = len(runs)
	in.unreadable = unreadable
	slices.Sort(apps)
	in.appResults = slices.Compact(apps)
	return in, nil
}

// assemble keeps one envelope per run (code and run id): the one with the
// highest AtMs, then the larger, then the first read; it names the copies
// that it drops. The MI6 envelope is written at uploadspike results and
// again at day 7, and MI8's as --continue sessions add hours, so two zips
// can hold different envelopes of one run.
func assemble(reads []read) *inputs {
	in := &inputs{from: map[runKey]string{}}
	best := map[runKey]int{}
	var order []runKey
	var losers []read
	for i, r := range reads {
		k := keyOf(r.env)
		j, ok := best[k]
		switch {
		case !ok:
			best[k] = i
			order = append(order, k)
		case r.env.AtMs > reads[j].env.AtMs || r.env.AtMs == reads[j].env.AtMs && r.size > reads[j].size:
			losers = append(losers, reads[j])
			best[k] = i
		default:
			losers = append(losers, r)
		}
	}
	for _, k := range order {
		r := reads[best[k]]
		in.envs = append(in.envs, r.env)
		in.from[k] = r.from
		in.piProbe = in.piProbe || r.env.Set == results.PiProbeSet
	}
	for _, l := range losers {
		in.dropped = append(in.dropped, dropped{from: l.from, kept: in.from[keyOf(l.env)]})
	}
	return in
}

// readFolder reads the envelopes of the results folder dir, at any depth,
// each named by prefix and its path in dir; app results (under app/) are
// counted by name; any other file is listed with why it was not read.
func readFolder(dir, prefix string) ([]read, []skipped, []string, error) {
	var reads []read
	var skip []skipped
	var apps []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = displayPath(filepath.ToSlash(rel))
		if slices.Contains(strings.Split(rel, "/")[:strings.Count(rel, "/")], appDir) {
			apps = append(apps, d.Name())
			return nil
		}
		if filepath.Ext(path) != ".json" {
			skip = append(skip, skipped{prefix + rel, reasonNotJSONFile})
			return nil
		}
		data, reason := readSmall(path)
		if reason != "" {
			skip = append(skip, skipped{prefix + rel, reason})
			return nil
		}
		e, reason := parseEnvelope(data)
		if reason != "" {
			skip = append(skip, skipped{prefix + rel, reason})
			return nil
		}
		reads = append(reads, read{env: e, from: prefix + rel, size: int64(len(data))})
		return nil
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("nu pot citi un folder de rezultate: %w", err)
	}
	return reads, skip, apps, nil
}

// displayPath is a file's path as the report names it: its name in lower
// case, since uploadspike's run ids end in a UTC time such as
// 20261124T101010Z, which the redaction scanner takes for a serial.
func displayPath(rel string) string {
	i := strings.LastIndexByte(rel, '/')
	return rel[:i+1] + strings.ToLower(rel[i+1:])
}

// readSmall reads a results file of at most maxFileBytes, or says why not.
func readSmall(path string) ([]byte, string) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, reasonUnreadable
	}
	if info.Size() > maxFileBytes {
		return nil, reasonTooLarge
	}
	data, err := os.ReadFile(path) //nolint:gosec // a file of the results folder that the caller named
	if err != nil {
		return nil, reasonUnreadable
	}
	return data, ""
}

// parseEnvelope decodes one envelope strictly, a field it lacks being an
// error, and validates it; or it says why not.
func parseEnvelope(data []byte) (results.Envelope, string) {
	var e results.Envelope
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return e, reasonNotJSON
		}
		return e, reasonNotEnvelope
	}
	if dec.More() {
		return e, reasonNotJSON
	}
	if e.Validate() != nil {
		return e, reasonNotEnvelope
	}
	return e, ""
}

// newest is the time of the newest envelope's AtMs, else of the newest
// run's end; zero when no envelope says when it was made.
func (in *inputs) newest() time.Time {
	var at, end int64
	for _, e := range in.envs {
		at = max(at, e.AtMs)
		for _, r := range e.Runs {
			end = max(end, r.EndMs)
		}
	}
	switch {
	case at > 0:
		return time.UnixMilli(at)
	case end > 0:
		return time.UnixMilli(end)
	}
	return time.Time{}
}

// hasMI8 reports whether an owner's MI8 envelope is among the inputs.
func (in *inputs) hasMI8() bool {
	return slices.ContainsFunc(in.envs, func(e results.Envelope) bool { return e.Code == "MI8" && judgedEnv(e) })
}

// judgedEnv reports whether a gate's Judge reads e: not the lab's, not
// the pi-probe marker and not one written with test seams.
func judgedEnv(e results.Envelope) bool {
	return !isLab(e) && e.Set != results.PiProbeSet && !e.TestSeams
}

// isLab reports whether e comes from the lab: TST-07's variant, never a
// gate's judged set.
func isLab(e results.Envelope) bool {
	return e.Host == "lab-linux" || e.Host == "lab-windows" || e.MI5 != nil && e.MI5.Lab
}

// badField is a field whose value does not have its shape.
type badField struct{ from, field string }

// The shapes of the fields that the report renders and that Validate
// leaves open. A value outside its shape fails the run before anything is
// rendered (Review Focus 5): free text that could carry a name, an
// address, a token or a password has no place in them.
var (
	// productText is a brand, a model or a Pi's model string.
	productText = regexp.MustCompile(`^[\p{L}\p{N} ._/+(),&-]{0,64}$`)
	// prose is a reason in Romanian, written by the spike.
	prose      = regexp.MustCompile(`^[\p{L}\p{N} .,:;()«»„”"'%/+=<>≥≤×→–—_-]{0,300}$`)
	slug       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
	reasonWord = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	callName   = regexp.MustCompile(`^[a-z][a-zA-Z.]{0,40}$`)
	failName   = regexp.MustCompile(`^[a-z0-9][a-z0-9 -]{0,39}$`)
	acctName   = regexp.MustCompile(`^(acct-[1-9][0-9]?)?$`)
)

// The closed lists of the fields that have one.
var (
	gapReasons = []string{results.GapReboot, results.GapSleep, results.GapHubCrash, results.GapMI5Block, results.GapMI8Stopped}
	osNames    = []string{"windows", "linux", "darwin", "android"}
	destKinds  = []string{"GOOGLE_DRIVE", "ONEDRIVE_PERSONAL", "LOCAL"}
	providers  = []string{"GOOGLE_DRIVE", "ONEDRIVE_PERSONAL"}
	platforms  = []string{"android", "windows", "web"}
	pairDests  = []string{"g-android", "g-windows", "g-web", "m-android", "m-windows", "m-web"}
	plans      = []string{"", "none", "A", "B1"}
	outcomes   = []string{results.OutcomeOK, results.OutcomeFailed, results.OutcomeExpectedExpiry, results.OutcomeBlockedByTest}
	clients    = []string{"android", "desktop", "tv", "web"}
	accounts   = []string{"", "test", "personal"}
)

// badFields lists every field of the kept envelopes whose value is
// outside its shape, by where the envelope came from and the field's
// path, never the value.
func (in *inputs) badFields() []badField {
	var out []badField
	for _, e := range in.envs {
		from := in.from[keyOf(e)]
		for _, f := range fieldsOutOfShape(e) {
			out = append(out, badField{from, f})
		}
	}
	return out
}

// fieldsOutOfShape lists the paths of e's fields that are out of shape.
func fieldsOutOfShape(e results.Envelope) []string {
	var bad []string
	check := func(ok bool, path string, args ...any) {
		if !ok {
			bad = append(bad, fmt.Sprintf(path, args...))
		}
	}
	check(slices.Contains(osNames, e.OS), "os")
	if m := e.PiModel; m != nil {
		for name, v := range map[string]string{"raw": m.Raw, "revision": m.Revision, "tier": m.Tier, "source": m.Source} {
			check(productText.MatchString(v), "piModel.%s", name)
		}
	}
	if c := e.SdConfirmation; c != nil {
		check(prose.MatchString(c.Text), "sdConfirmation.text")
	}
	for i, r := range e.Runs {
		check(prose.MatchString(r.InvalidReason), "runs[%d].invalidReason", i)
		for j, g := range r.Gaps {
			check(slices.Contains(gapReasons, g.Reason), "runs[%d].gaps[%d].reason", i, j)
		}
	}
	if d := e.MI5; d != nil {
		for i, dest := range d.Dests {
			check(slug.MatchString(dest.Dest), "mi5.dests[%d].dest", i)
			check(slices.Contains(destKinds, dest.Kind), "mi5.dests[%d].kind", i)
			check(acctName.MatchString(dest.Account), "mi5.dests[%d].account", i)
			check(slices.Contains(accounts, dest.AccountKind), "mi5.dests[%d].accountKind", i)
			check(dest.AuditFrom == "" || slug.MatchString(dest.AuditFrom), "mi5.dests[%d].auditFrom", i)
			check(prose.MatchString(dest.Deferred), "mi5.dests[%d].deferred", i)
			for k := range dest.CallsPerSegment {
				check(callName.MatchString(k), "mi5.dests[%d].callsPerSegment", i)
			}
		}
		for i, r := range d.ReadOpen {
			for k := range r.Failures {
				check(failName.MatchString(k), "mi5.readOpen[%d].failures", i)
			}
		}
	}
	if d := e.MI6; d != nil {
		for i, p := range d.Pairs {
			check(slices.Contains(providers, p.Provider), "mi6.pairs[%d].provider", i)
			check(slices.Contains(platforms, p.Platform), "mi6.pairs[%d].platform", i)
			check(slices.Contains(pairDests, p.Dest), "mi6.pairs[%d].dest", i)
			check(slices.Contains(plans, p.Plan), "mi6.pairs[%d].plan", i)
			check(p.PlanAFailure == "" || reasonWord.MatchString(p.PlanAFailure), "mi6.pairs[%d].planAFailure", i)
			check(p.B1Failure == "" || reasonWord.MatchString(p.B1Failure), "mi6.pairs[%d].b1Failure", i)
			check(p.RefreshFrom == "" || slices.Contains(pairDests, p.RefreshFrom), "mi6.pairs[%d].refreshFrom", i)
			for j, r := range p.Refresh {
				check(slices.Contains(outcomes, r.Outcome), "mi6.pairs[%d].refresh[%d].outcome", i, j)
			}
		}
		for i, c := range d.Visibility {
			check(slices.Contains(providers, c.Provider) && slices.Contains(clients, c.Creator) && slices.Contains(clients, c.Reader),
				"mi6.visibility[%d]", i)
		}
		for i, h := range d.Handoffs {
			check(acctName.MatchString(h.Account) && slug.MatchString(h.Client), "mi6.handoffs[%d]", i)
		}
	}
	if d := e.MI8; d != nil {
		for i, c := range d.Cameras {
			check(productText.MatchString(c.Brand), "mi8.cameras[%d].brand", i)
			check(productText.MatchString(c.Model), "mi8.cameras[%d].model", i)
		}
	}
	slices.Sort(bad)
	return bad
}
