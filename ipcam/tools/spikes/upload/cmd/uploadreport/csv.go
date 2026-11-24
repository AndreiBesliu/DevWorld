// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package main

import (
	"bytes"
	"cmp"
	"encoding/csv"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The CSVs of the report, by their paths in the output folder (PH0-30).
const (
	csvMI5 = "phase0-data/phase0c-upload-mi5.csv"
	csvMI6 = "phase0-data/phase0c-upload-mi6.csv"
	csvMI8 = "phase0-data/phase0c-upload-mi8.csv"
)

// The columns: MI5 a row per destination of each run (the finest sample
// that the envelope keeps), MI6 a row per pair-day or visibility cell, MI8
// a row per reference episode.
var (
	mi5Header = []string{"host", "os", "set", "runId", "storage", "lab", "valid", "blockSecs", "recoverySecs", "uplinkMbps",
		"dest", "kind", "account", "cameras", "segments", "segmentsDuringBlock", "verified", "missing", "duplicates", "hashMismatches",
		"verifiedDuringBlock", "failedDuringBlock", "rateLimitedDuringBlock", "unreachableDuringBlock", "deferred", "callsPerSegment"}
	mi6Header = []string{"host", "set", "runId", "sample", "provider", "platform", "dest", "plan", "day", "attempts", "outcome",
		"creator", "reader", "listed", "read", "self"}
	mi8Header = []string{"host", "os", "set", "runId", "camera", "brand", "model", "referenceMethod", "refLagMs", "startMs", "endMs",
		"matched"}
)

// csvs are the report's CSV files, every sample of every envelope, in the
// order of their host, set and run id; run ids in lower case.
func (r *report) csvs() map[string]string {
	envs := slices.Clone(r.in.envs)
	slices.SortStableFunc(envs, func(a, b results.Envelope) int {
		return cmp.Or(cmp.Compare(a.Host, b.Host), cmp.Compare(a.Set, b.Set), cmp.Compare(a.RunID, b.RunID))
	})
	m5, m6, m8 := newTable(mi5Header), newTable(mi6Header), newTable(mi8Header)
	for _, e := range envs {
		id := displayRunID(e.RunID)
		switch {
		case e.MI5 != nil:
			d := e.MI5
			for _, dest := range d.Dests {
				var calls []string
				for _, k := range slices.Sorted(maps.Keys(dest.CallsPerSegment)) {
					calls = append(calls, k+"="+fl(dest.CallsPerSegment[k]))
				}
				m5.row(e.Host, e.OS, e.Set, id, e.Storage, bl(isLab(e)), bl(allValid(e)), fl(d.BlockSecs), fl(d.RecoverySecs),
					fl(d.UplinkMbps), dest.Dest, dest.Kind, dest.Account, it(dest.Cameras), it(dest.Segments), it(dest.SegmentsDuringBlock),
					it(dest.Verified), it(dest.Missing), it(dest.Duplicates), it(dest.HashMismatches), it(dest.VerifiedDuringBlock),
					it(dest.FailedDuringBlock), it(dest.RateLimitedDuringBlock), bl(dest.UnreachableDuringBlock), dest.Deferred,
					strings.Join(calls, ";"))
			}
		case e.MI6 != nil:
			for _, p := range e.MI6.Pairs {
				for _, d := range p.Refresh {
					m6.row(e.Host, e.Set, id, "day", p.Provider, p.Platform, p.Dest, p.Plan, it(d.Day), it(d.Attempts), d.Outcome,
						"", "", "", "", "")
				}
			}
			for _, c := range e.MI6.Visibility {
				m6.row(e.Host, e.Set, id, "cell", c.Provider, "", "", "", "", "", "", c.Creator, c.Reader, bl(c.Listed), bl(c.Read), bl(c.Self))
			}
		case e.MI8 != nil:
			for _, c := range e.MI8.Cameras {
				for _, ep := range c.EpisodeTimes {
					m8.row(e.Host, e.OS, e.Set, id, c.Camera, c.Brand, c.Model, c.ReferenceMethod, strconv.FormatInt(c.RefLagMs, 10),
						strconv.FormatInt(ep.StartMs, 10), strconv.FormatInt(ep.EndMs, 10), bl(ep.Matched))
				}
			}
		}
	}
	return map[string]string{csvMI5: m5.String(), csvMI6: m6.String(), csvMI8: m8.String()}
}

// allValid reports whether every run of e is valid.
func allValid(e results.Envelope) bool {
	return len(e.Runs) > 0 && !slices.ContainsFunc(e.Runs, func(r results.Run) bool { return !r.Valid })
}

// csvTable is a CSV being written.
type csvTable struct {
	buf bytes.Buffer
	w   *csv.Writer
}

func newTable(header []string) *csvTable {
	t := &csvTable{}
	t.w = csv.NewWriter(&t.buf)
	t.row(header...)
	return t
}

// row writes one record; a bytes.Buffer never fails a write.
func (t *csvTable) row(fields ...string) { _ = t.w.Write(fields) }

func (t *csvTable) String() string {
	t.w.Flush()
	return t.buf.String()
}

func it(n int) string     { return strconv.Itoa(n) }
func fl(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
func bl(b bool) string    { return strconv.FormatBool(b) }
