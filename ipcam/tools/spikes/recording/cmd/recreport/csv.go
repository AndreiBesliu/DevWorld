// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

package main

import (
	"bytes"
	"cmp"
	"encoding/csv"
	"maps"
	"slices"
	"strconv"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/recording/results"
)

// The CSVs of the report, by their paths in the output folder (PH0-30).
const (
	csvMI1 = "phase0-data/phase0c-recorder-mi1.csv"
	csvMI2 = "phase0-data/phase0c-recorder-mi2.csv"
	csvMI3 = "phase0-data/phase0c-recorder-mi3.csv"
	csvMI4 = "phase0-data/phase0c-recorder-mi4.csv"
	csvMI9 = "phase0-data/phase0c-recorder-mi9.csv"
)

// mi1Header is MI1's columns: a row per stream of each run.
var mi1Header = []string{"host", "set", "runId", "part", "attempt", "slot", "valid", "invalidReason", "shutdownKind", "encrypted",
	"padLowBitrate", "lossMs", "stream", "bitrateKbps", "streamEncrypted", "streamPadLowBitrate", "streamLossMs", "streamLimitMs",
	"errBoundMs", "reconciled", "unreadable", "tornParts", "hub04Clear", "pacingP99Ms", "skippedAUs", "resumeMs", "coldBootMs",
	"cameras", "ntpWaitMs", "ntpSynced", "writeCache", "storage"}

// longHeader is the columns of MI2, MI3 and MI4: a row per figure, index
// counting a list's samples.
var longHeader = []string{"host", "os", "set", "runId", "storage", "key", "index", "value"}

// mi9Header is MI9's columns: a row per figure of each leg; bound is "≥"
// on a seek that the measurement's wait cut, a lower bound (LastSeekCut).
var mi9Header = []string{"host", "os", "set", "runId", "storage", "source", "transport", "key", "index", "value", "bound"}

// csvs are the report's CSV files, every sample of every envelope, in the
// order of their code, host, set and run id.
func (r *report) csvs() map[string]string {
	envs := slices.Clone(r.in.envs)
	slices.SortStableFunc(envs, func(a, b results.Envelope) int {
		return cmp.Or(cmp.Compare(a.Host, b.Host), cmp.Compare(a.Set, b.Set), cmp.Compare(a.RunID, b.RunID))
	})
	mi1 := newCSVTable(mi1Header)
	long := map[string]*csvTable{csvMI2: newCSVTable(longHeader), csvMI3: newCSVTable(longHeader), csvMI4: newCSVTable(longHeader)}
	mi9 := newCSVTable(mi9Header)
	for _, e := range envs {
		switch {
		case e.Code == "MI1":
			mi1Rows(mi1, e)
		case e.MI2 != nil:
			mi2Rows(long[csvMI2].figures(e), e.MI2)
		case e.MI3 != nil:
			mi3Rows(long[csvMI3].figures(e), e.MI3)
		case e.MI4 != nil:
			mi4Rows(long[csvMI4].figures(e), e)
		case e.MI9 != nil:
			for _, l := range e.MI9.Legs {
				mi9Rows(mi9, e, l)
			}
		}
	}
	return map[string]string{csvMI1: mi1.String(), csvMI2: long[csvMI2].String(), csvMI3: long[csvMI3].String(),
		csvMI4: long[csvMI4].String(), csvMI9: mi9.String()}
}

// csvTable is a CSV being written.
type csvTable struct {
	buf bytes.Buffer
	w   *csv.Writer
}

func newCSVTable(header []string) *csvTable {
	t := &csvTable{}
	t.w = csv.NewWriter(&t.buf)
	t.row(header...)
	return t
}

// row writes one row; a bytes.Buffer does not fail.
func (t *csvTable) row(cells ...string) { _ = t.w.Write(cells) }

func (t *csvTable) String() string {
	t.w.Flush()
	return t.buf.String()
}

// figures writes e's figures in the long form: host, os, set, run id,
// storage, then a key, an index ("" for one figure) and its value.
func (t *csvTable) figures(e results.Envelope) func(key, index, value string) {
	return func(key, index, value string) { t.row(e.Host, e.OS, e.Set, e.RunID, e.Storage, key, index, value) }
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func integer[T int | int64](v T) string { return strconv.FormatInt(int64(v), 10) }

func boolean(v bool) string { return strconv.FormatBool(v) }

// optional is *v, or "" for none.
func optional(v *int64) string {
	if v == nil {
		return ""
	}
	return integer(*v)
}

// mi1Rows writes a row per stream of each of e's runs, one for a run
// without a stream, invalid runs included with their reason.
func mi1Rows(t *csvTable, e results.Envelope) {
	part := ""
	if e.Part > 0 {
		part = integer(e.Part)
	}
	for _, r := range e.Runs {
		streams := r.Streams
		if len(streams) == 0 {
			streams = []results.StreamLoss{{}}
		}
		for _, s := range streams {
			stream := []string{"", "", "", "", "", ""}
			if s.Stream != "" {
				stream = []string{s.Stream, integer(s.BitrateKbps), boolean(s.Encrypted), boolean(s.PadLowBitrate), integer(s.LossMs), integer(s.LimitMs)}
			}
			cells := []string{e.Host, e.Set, r.RunID, part, integer(r.Attempt), integer(r.Slot), boolean(r.Valid), r.InvalidReason, r.ShutdownKind,
				boolean(r.Encrypted), boolean(r.PadLowBitrate), integer(r.LossMs)}
			cells = append(cells, stream...)
			cells = append(cells, integer(r.ErrBoundMs), boolean(r.Reconciled), integer(r.Unreadable), integer(r.TornParts), boolean(r.Hub04Clear),
				integer(r.PacingP99Ms), integer(r.SkippedAUs), optional(r.ResumeMs), optional(r.ColdBootMs), integer(r.Cameras),
				integer(r.NtpWaitMs), boolean(r.NtpSynced), r.WriteCache, e.Storage)
			t.row(cells...)
		}
	}
}

// mi2Rows writes a 72 h run's figures, its card's states and its redone
// runs.
func mi2Rows(row func(key, index, value string), d *results.Mi2Data) {
	row("hours", "", num(d.Hours))
	row("cameras", "", integer(d.Cameras))
	row("crashes72h", "", integer(d.Crashes72h))
	row("childRestarts", "", integer(d.ChildRestarts))
	row("onSdCard", "", boolean(d.OnSdCard))
	row("windowHours", "", num(d.WindowHours))
	for i, s := range d.CardStates {
		row("cardState", integer(i), s.State)
		row("cardStateAtMs", integer(i), integer(s.AtMs))
	}
	for i, why := range d.Invalidated {
		row("invalidated", integer(i), why)
	}
}

// mi3Rows writes a capacity run's first hour, its budgets, its loads, its
// steps and its 16-camera hour.
func mi3Rows(row func(key, index, value string), d *results.Mi3Data) {
	row("tier", "", d.Tier)
	row("cameras", "", integer(d.Cameras))
	row("cpuPctTotal", "", num(d.CPUPctTotal))
	row("ramMb", "", num(d.RAMMb))
	for _, key := range slices.Sorted(maps.Keys(d.Budgets)) {
		row("budget."+key, "", num(d.Budgets[key].Value))
		row("budgetLimit."+key, "", num(d.Budgets[key].Limit))
	}
	for _, mode := range slices.Sorted(maps.Keys(d.DiskRatio)) {
		row("diskRatio."+mode, "", num(d.DiskRatio[mode]))
	}
	row("fsyncPerSecPerFile", "", num(d.FsyncPerSecPerFile))
	row("latencyP95Ms", "", num(d.LatencyP95Ms))
	row("pacingP99Ms", "", integer(d.PacingP99Ms))
	row("skippedAUs", "", integer(d.SkippedAUs))
	row("diskQueuePeakBytes", "", integer(d.DiskQueuePeakBytes))
	for _, load := range slices.Sorted(maps.Keys(d.LoadCPUPct)) {
		row("loadCpuPct."+load, "", num(d.LoadCPUPct[load]))
	}
	for i, s := range d.Steps {
		stepRows(row, "step.", integer(i), s)
	}
	if d.Sixteen != nil {
		stepRows(row, "sixteen.", "", *d.Sixteen)
	}
	if d.StepsStopped != "" {
		row("stepsStopped", "", d.StepsStopped)
	}
	if d.SixteenStopped != "" {
		row("sixteenStopped", "", d.SixteenStopped)
	}
}

// stepRows writes a step of MI3 under prefix.
func stepRows(row func(key, index, value string), prefix, index string, s results.Mi3Step) {
	row(prefix+"cameras", index, integer(s.Cameras))
	row(prefix+"minutes", index, integer(s.Minutes))
	row(prefix+"attempt", index, integer(s.Attempt))
	row(prefix+"valid", index, boolean(s.Valid))
	row(prefix+"pass", index, boolean(s.Pass))
	row(prefix+"pacingP99Ms", index, integer(s.PacingP99Ms))
	row(prefix+"skippedAUs", index, integer(s.SkippedAUs))
}

// mi4Rows writes the figures that e's MI4 data holds: a host's run, an AGE
// run of the app or CI's ENC-06 check.
func mi4Rows(row func(key, index, value string), e results.Envelope) {
	d := e.MI4
	if e.AtMs != 0 {
		row("atMs", "", integer(e.AtMs))
	}
	if d.Model != "" {
		row("model", "", d.Model)
	}
	samples := func(key string, values []float64) {
		for i, v := range values {
			row(key, integer(i), num(v))
		}
	}
	samples("readMs", d.ReadMs)
	if len(d.ReadMs) > 0 {
		row("readMaxMs", "", num(d.ReadMaxMs))
		row("readP50Ms", "", num(d.ReadP50Ms))
		row("readP99Ms", "", num(d.ReadP99Ms))
		row("readSize", "", integer(d.ReadSize))
		row("cold", "", boolean(d.Cold))
	}
	for _, r := range d.InfoReads {
		size := integer(r.Size)
		row("infoReads.maxMs", size, num(r.MaxMs))
		row("infoReads.p50Ms", size, num(r.P50Ms))
		row("infoReads.p99Ms", size, num(r.P99Ms))
	}
	samples("seekMs", d.SeekMs)
	samples("seekRamMs", d.SeekRAMMs)
	samples("seek2MinMs", d.Seek2MinMs)
	if d.CryptoMBs != 0 {
		row("cryptoMBs", "", num(d.CryptoMBs))
		row("cryptoMBsAllCores", "", num(d.CryptoMBsAllCores))
		row("thresholdMBs", "", num(d.ThresholdMBs))
		row("recommendedAfter", "", integer(d.RecommendedAfter))
	}
	if d.AgerangeMbps != nil {
		row("agerangeMbps", "", num(*d.AgerangeMbps))
	}
	if d.Enc06 != nil {
		row("enc06", "", boolean(*d.Enc06))
	}
}

// mi9Rows writes leg l of e: its throughput and round trip, each seek, the
// one that the wait cut marked «≥», each frame shown, and how far a leg
// that its wait ended got.
func mi9Rows(t *csvTable, e results.Envelope, l results.Mi9Leg) {
	row := func(key, index, value, bound string) {
		t.row(e.Host, e.OS, e.Set, e.RunID, e.Storage, l.Source, l.Transport, key, index, value, bound)
	}
	if l.ThroughputMbps != 0 {
		row("throughputMbps", "", num(l.ThroughputMbps), "")
	}
	if l.RTTMs != 0 {
		row("rttMs", "", num(l.RTTMs), "")
	}
	for i, v := range l.SeekMs {
		bound := ""
		if l.Partial != nil && l.Partial.LastSeekCut && i == len(l.SeekMs)-1 {
			bound = "≥"
		}
		row("seekMs", integer(i), num(v), bound)
	}
	for i, v := range l.DisplayMs {
		row("displayMs", integer(i), num(v), "")
	}
	if p := l.Partial; p != nil {
		row("partial.receivedBytes", "", integer(p.ReceivedBytes), "")
		row("partial.sizeBytes", "", integer(p.SizeBytes), "")
		row("partial.elapsedMs", "", num(p.ElapsedMs), "")
		row("partial.lastSeekCut", "", boolean(p.LastSeekCut), "")
	}
}
