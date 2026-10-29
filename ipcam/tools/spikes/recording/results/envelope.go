// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c1-recorder-spike.md

// Package results is the one results envelope of the recorder spike (C17):
// every measurement, on the owner's devices and in the lab, is written as an
// Envelope, and each gate's Judge reads them back. Results are public
// (PH0-22): an envelope names its host only by one of five pseudonyms, and
// carries no host name, address, user name, serial or camera credential.
package results

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Schema names the envelope format.
const Schema = "ipcam.phase0c1.result/1"

// hosts are the pseudonyms of the hosts under test (PH0-22).
var hosts = []string{"pc", "pi", "lab-linux", "lab-windows", "lab-qemu"}

// codes are the measurements that write envelopes.
var codes = []string{"MI1", "MI2", "MI3", "MI4", "MI9", "ENC-06"}

// storages are the kinds of storage under rec/, as diag host reports them.
var storages = []string{"ssd", "sd", "hdd", "unknown"}

// Envelope is one results file: the runs of one measurement, on one host,
// in one set.
type Envelope struct {
	Schema string `json:"schema"`
	// Code is "MI1" to "MI4", "MI9" or "ENC-06".
	Code string `json:"code"`
	// RunID names the file; for a one-run envelope it is the run's id.
	RunID string `json:"runId"`
	// Set is "lab-linux-kill9", "lab-linux-qemu-reset", "lab-windows-kill9",
	// "pc-kill9", "pc-bugcheck", "pi-kill9", "pi-sysrq", "real-pc",
	// "real-pi", or another measurement's set.
	Set string `json:"set"`
	// Host is "pc", "pi", "lab-linux", "lab-windows" or "lab-qemu".
	Host string `json:"host"`
	OS   string `json:"os"`
	// PiModel is the Pi's model (C24), nil on a PC.
	PiModel *PiModelInfo `json:"piModel,omitempty"`
	// Storage is "ssd", "sd", "hdd" or "unknown" (G16).
	Storage string `json:"storage"`
	// SdConfirmation is the owner's answer to the SD-card warning (G16).
	SdConfirmation *SdConfirmation `json:"sdConfirmation,omitempty"`
	// Part and Parts are, in a lab annotation, this run's 1-based position
	// in its set and the set's run count (21); 0 elsewhere.
	Part  int   `json:"part,omitempty"`
	Parts int   `json:"parts,omitempty"`
	Runs  []Run `json:"runs"`
	// MI2 is an MI2 envelope's 72 h run (G11), and only an MI2 envelope's.
	MI2 *Mi2Data `json:"mi2,omitempty"`
	// MI3 is an MI3 envelope's capacity run (G12), and only an MI3
	// envelope's.
	MI3 *Mi3Data `json:"mi3,omitempty"`
	// MI4 is an MI4 envelope's age rig run (G13), and only an MI4 or an
	// ENC-06 envelope's: recreport builds the ENC-06 envelope from CI's
	// annotation, with Enc06, and each AGE result of the app as an MI4
	// envelope with AgerangeMbps (mi4.AgerangeRun: set app-age, OS the
	// app's platform, "android" on the phone).
	MI4 *Mi4Data `json:"mi4,omitempty"`
}

// Mi4Data is MI4's age random-access rig on a host (G13, ENC-08, HUB-33):
//   - Model is the host's model, the Pi's as diag host reads it, or "PC".
//   - ReadMs are the 1000 timed reads, each as long as one fMP4 part at a
//     uniform random offset (G22), ReadSize their mean bytes; the max
//     decides (C26), p50 and p99 are information.
//     Cold is true when the page cache was dropped before each set (on the
//     Pi, as root), false for warm reads (Windows).
//   - InfoReads are the reads of 64 KiB and 4 MiB, information only.
//   - SeekMs are the seeks' first-frame times in a 5-min segment (G21);
//     after a failure, SeekRAMMs are those with the whole segment decrypted
//     into RAM and Seek2MinMs those in a 2-min segment, also from RAM.
//   - CryptoMBs is the encryption rate on one core (G23), CryptoMBsAllCores
//     on all of them (information); ThresholdMBs is the threshold of the
//     model (C25), 0 on the PC, where the rate is information, and
//     RecommendedAfter the recommended number that follows from the rate.
//   - AgerangeMbps is an AGE run of the app (G24), which decides only when
//     it ran on the phone, and Enc06 the ENC-06 check of CI (identical
//     bytes to the age CLI and rage), each in the envelope that recreport
//     builds for it.
type Mi4Data struct {
	Model             string     `json:"model,omitempty"`
	ReadMs            []float64  `json:"readMs,omitempty"`
	ReadMaxMs         float64    `json:"readMaxMs,omitempty"`
	ReadP50Ms         float64    `json:"readP50Ms,omitempty"`
	ReadP99Ms         float64    `json:"readP99Ms,omitempty"`
	ReadSize          int        `json:"readSize,omitempty"`
	Cold              bool       `json:"cold,omitempty"`
	InfoReads         []Mi4Reads `json:"infoReads,omitempty"`
	SeekMs            []float64  `json:"seekMs,omitempty"`
	SeekRAMMs         []float64  `json:"seekRamMs,omitempty"`
	Seek2MinMs        []float64  `json:"seek2MinMs,omitempty"`
	CryptoMBs         float64    `json:"cryptoMBs,omitempty"`
	CryptoMBsAllCores float64    `json:"cryptoMBsAllCores,omitempty"`
	ThresholdMBs      float64    `json:"thresholdMBs,omitempty"`
	RecommendedAfter  int        `json:"recommendedAfter,omitempty"`
	AgerangeMbps      *float64   `json:"agerangeMbps,omitempty"`
	Enc06             *bool      `json:"enc06,omitempty"`
}

// Mi4Reads is a set of MI4's information reads of Size bytes each.
type Mi4Reads struct {
	Size  int     `json:"size"`
	MaxMs float64 `json:"maxMs"`
	P50Ms float64 `json:"p50Ms"`
	P99Ms float64 `json:"p99Ms"`
}

// Mi3Data is MI3's capacity run on a host (G12, HUB-32, G20): its tier
// (H3, H1, H1b, H1c, or unknown for a Pi model that the table lacks), the
// recommended camera count that it ran first, and that first valid hour's
// figures: total CPU (% of the machine), RAM (MB), the §4.8 budgets of the
// tier, disk ÷ bitrate per mode ("clear", "encrypted", R02), fsyncs per
// second per file, the live tiles' added latency (p95, ms) and the
// controller's pacing (R07). Steps are every run of the step-down, the
// first hour and its invalid attempts included (G20); Sixteen is the extra
// hour at 16 cameras of a PC with 6 cores or more (R07), information only.
// Proxies say, in Romanian, which load stands for what (G20).
// DiskQueuePeakBytes is the sum of the MediaMTX disk writers' peaks, per
// recorder, in that hour (C1-R17): an upper bound, reported beside the RAM.
// StepsStopped says, in Romanian, which step after a failed first hour
// could not be measured, and why, when that error stopped the steps before
// they settled; SixteenStopped says the same of the hour at 16 cameras,
// which then has no Sixteen. Neither carries an error's own text, which may
// name a path or an address (PH0-22).
type Mi3Data struct {
	Tier               string             `json:"tier"`
	Cameras            int                `json:"cameras"`
	CPUPctTotal        float64            `json:"cpuPctTotal"`
	RAMMb              float64            `json:"ramMb"`
	Budgets            map[string]Budget  `json:"budgets"`
	DiskRatio          map[string]float64 `json:"diskRatio"`
	FsyncPerSecPerFile float64            `json:"fsyncPerSecPerFile"`
	LatencyP95Ms       float64            `json:"latencyP95Ms"`
	PacingP99Ms        int64              `json:"pacingP99Ms"`
	SkippedAUs         int                `json:"skippedAUs"`
	Steps              []Mi3Step          `json:"steps"`
	Sixteen            *Mi3Step           `json:"sixteen,omitempty"`
	Proxies            []string           `json:"proxies,omitempty"`
	DiskQueuePeakBytes int64              `json:"diskQueuePeakBytes"`
	StepsStopped       string             `json:"stepsStopped,omitempty"`
	SixteenStopped     string             `json:"sixteenStopped,omitempty"`
	LoadCPUPct         map[string]float64 `json:"loadCpuPct"`
}

// Budget is one §4.8 budget of a run: the value measured, its limit, and
// whether the value is within it. The judge recomputes Limit and Pass.
type Budget struct {
	Value float64 `json:"value"`
	Limit float64 `json:"limit"`
	Pass  bool    `json:"pass"`
}

// Mi3Step is one run of MI3: its cameras, its length (60 or 15 min), its
// attempt at that count and length, whether the controller sent evenly
// (Valid, R07, with the pacing that says so), and, when valid, whether
// every budget was met (Pass).
type Mi3Step struct {
	Cameras     int   `json:"cameras"`
	Minutes     int   `json:"minutes"`
	Attempt     int   `json:"attempt"`
	Pass        bool  `json:"pass"`
	Valid       bool  `json:"valid"`
	PacingP99Ms int64 `json:"pacingP99Ms"`
	SkippedAUs  int   `json:"skippedAUs"`
}

// Mi2Data is MI2's 72 h run on a host (G11, HUB-31, G19): the hours it
// ran, its cameras (the host's recommended count), the unexpected exits of
// ipcam-hub (crashes) and MediaMTX's restarts (HUB-13), the card monitor's
// states when rec/ was on an SD card (T40), the rolling window's hours, and
// the runs before it that a reboot or an interruption invalidated, each as
// "<run id>: <why>", in Romanian.
type Mi2Data struct {
	Hours         float64     `json:"hours"`
	Cameras       int         `json:"cameras"`
	Crashes72h    int         `json:"crashes72h"`
	ChildRestarts int         `json:"childRestarts"`
	OnSdCard      bool        `json:"onSdCard"`
	CardStates    []CardState `json:"cardStates,omitempty"`
	WindowHours   float64     `json:"windowHours"`
	Invalidated   []string    `json:"invalidated,omitempty"`
}

// CardState is one state of the card monitor, as ipcam-hub's card event
// gave it ("ok", "degraded", "failed"), and when (the host's Unix ms).
type CardState struct {
	State string `json:"state"`
	AtMs  int64  `json:"atMs"`
}

// PiModelInfo mirrors the piModel of `ipcam-hub diag host` (tools cannot
// import the hub's health package, C01). Estimate is true when the owner
// typed the camera count for an unknown model (C24).
type PiModelInfo struct {
	Raw            string `json:"raw"`
	Revision       string `json:"revision"`
	Tier           string `json:"tier"`
	Supported      bool   `json:"supported"`
	Known          bool   `json:"known"`
	RecommendedMin int    `json:"recommendedMin"`
	RecommendedMax int    `json:"recommendedMax"`
	Source         string `json:"source"`
	Estimate       bool   `json:"estimate"`
}

// SdConfirmation is the text that the owner typed to confirm the SD-card
// warning, and when (Unix ms), the audit stand-in of G16.
type SdConfirmation struct {
	Text string `json:"text"`
	AtMs int64  `json:"atMs"`
}

// Run is one stop of the host and what it lost.
type Run struct {
	RunID string `json:"runId"`
	// Valid is the controller's verdict on the cut (C16); InvalidReason says
	// why not.
	Valid         bool   `json:"valid"`
	InvalidReason string `json:"invalidReason,omitempty"`
	// Slot is a real cut's slot, 1 to 5; Attempt counts the set's attempts.
	Slot    int `json:"slot,omitempty"`
	Attempt int `json:"attempt,omitempty"`
	// ShutdownKind is kill9, sysrq, bugcheck, qemu-reset, power or forced.
	ShutdownKind string `json:"shutdownKind"`
	// Encrypted and PadLowBitrate are the run's jobs' settings; MI1's
	// O17-off run is encrypted with PadLowBitrate false and records
	// cam99_main encrypted with the setting off. A clear run's setting does
	// not matter.
	Encrypted     bool         `json:"encrypted"`
	PadLowBitrate bool         `json:"padLowBitrate"`
	Streams       []StreamLoss `json:"streams"`
	// LossMs is the largest stream loss, ErrBoundMs the bound of the stop
	// moment's error.
	LossMs     int64 `json:"lossMs"`
	ErrBoundMs int64 `json:"errBoundMs"`
	// Reconciled: the reconcile event has aborted false, the verify event
	// has catalogMismatches 0, and nothing outside ipcam-hub touched the
	// files (G07, C12).
	Reconciled bool `json:"reconciled"`
	Unreadable int  `json:"unreadable"`
	TornParts  int  `json:"tornParts"`
	Hub04Clear bool `json:"hub04Clear"`
	// PacingP99Ms and SkippedAUs are the controller's pacing (R07).
	PacingP99Ms int64 `json:"pacingP99Ms"`
	SkippedAUs  int   `json:"skippedAUs"`
	// ResumeMs is the kill to the last job's `recording` event (kill9),
	// without the harness's raw HUB-04 scan at the stop, which runs between
	// the kill and the restart (G08).
	ResumeMs *int64 `json:"resumeMs,omitempty"`
	Cameras  int    `json:"cameras"`
	// ColdBootMs is, on sysrq and bugcheck runs, the host's uptime at the
	// last job's `recording` event after the reboot (§4.8 cold-boot row).
	ColdBootMs *int64 `json:"coldBootMs,omitempty"`
	NtpWaitMs  int64  `json:"ntpWaitMs"`
	NtpSynced  bool   `json:"ntpSynced"`
	WriteCache string `json:"writeCache"`
}

// StreamLoss is what one stream lost. LimitMs is information only: the
// judges recompute it.
type StreamLoss struct {
	Stream        string `json:"stream"`
	BitrateKbps   int    `json:"bitrateKbps"`
	Encrypted     bool   `json:"encrypted"`
	PadLowBitrate bool   `json:"padLowBitrate"`
	LossMs        int64  `json:"lossMs"`
	LimitMs       int64  `json:"limitMs"`
}

// Validate checks the schema, the code, the host pseudonym, the storage,
// the lab part, and that the code, set and run id are each one file name.
func (e Envelope) Validate() error {
	switch {
	case e.Schema != Schema:
		return fmt.Errorf("results: schema %q, want %q", e.Schema, Schema)
	case !slices.Contains(codes, e.Code):
		return fmt.Errorf("results: unknown code %q", e.Code)
	case !slices.Contains(hosts, e.Host):
		return fmt.Errorf("results: host %q is not one of the pseudonyms %s", e.Host, strings.Join(hosts, ", "))
	case e.Storage != "" && !slices.Contains(storages, e.Storage):
		return fmt.Errorf("results: unknown storage %q", e.Storage)
	case e.Part < 0 || e.Part > e.Parts || (e.Parts > 0) != (e.Part > 0):
		return fmt.Errorf("results: part %d of %d", e.Part, e.Parts)
	case (e.Code == "MI2") != (e.MI2 != nil):
		return fmt.Errorf("results: an %s envelope with mi2 %v: only MI2's has it, always", e.Code, e.MI2 != nil)
	case (e.Code == "MI3") != (e.MI3 != nil):
		return fmt.Errorf("results: an %s envelope with mi3 %v: only MI3's has it, always", e.Code, e.MI3 != nil)
	case e.Code == "MI4" && e.MI4 == nil, e.MI4 != nil && e.Code != "MI4" && e.Code != "ENC-06":
		return fmt.Errorf("results: an %s envelope with mi4 %v: MI4's has it always, and ENC-06's may", e.Code, e.MI4 != nil)
	}
	if !fileName(e.Set) {
		return fmt.Errorf("results: set %q is not a file name", e.Set)
	}
	if !fileName(e.RunID) {
		return fmt.Errorf("results: run id %q is not a file name", e.RunID)
	}
	return nil
}

// fileName reports whether name is one local path element on every OS.
func fileName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\:`) && filepath.IsLocal(name)
}

// Parse decodes one envelope strictly, with no field that the schema lacks,
// and validates it.
func Parse(data []byte) (Envelope, error) {
	var e Envelope
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return Envelope{}, fmt.Errorf("results: %w", err)
	}
	if dec.More() {
		return Envelope{}, errors.New("results: data after the envelope")
	}
	if err := e.Validate(); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

// Read reads the envelope in the file at path.
func Read(path string) (Envelope, error) {
	data, err := os.ReadFile(path) //nolint:gosec // a results file that the caller names
	if err != nil {
		return Envelope{}, err
	}
	e, err := Parse(data)
	if err != nil {
		return Envelope{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return e, nil
}
