// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi6

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// Days is MI6's length: logins on day 0, the hub's daily refresh on days 1
// to 7 (P53), results on day 7.
const Days = 7

// day is 24 h.
const day = 24 * time.Hour

// minUptime is how long the host must have run in a day's window for its
// refresh to count: the hub retries every 15 min, so less is no attempt at
// all, and the day is the test's, not the hub's (P49).
const minUptime = 15 * time.Minute

// Account is MI6's one account's pseudonym (all logins use one Google and
// one Microsoft account).
const Account = "acct-1"

// HandoffCap is how many logins of one account and client MI6 hands to the
// hub at most (P39; Google keeps 100 refresh tokens per account and
// client).
const HandoffCap = 20

// CapRefused is the owner's text when a client reached HandoffCap.
const CapRefused = "Ați atins limita de 20 de autentificări pentru acest client. Spuneți-ne."

// The kinds of a login's failure: the provider refused it (plan A's
// refusal starts B1, B1's gives B2), it never reached the provider, which
// judges nothing (C2-R16 (c)), or its state expired, which the owner
// retries.
const (
	FailRefusal   = "refusal"
	FailTransient = "transient"
	FailExpired   = "expired"
)

// The hub's oauth_failed stages: plan A's complete, B1's device flow.
const (
	stageComplete = "complete"
	stageDevice   = "device"
)

// Run is an open MI6 run, as uploadspike keeps it in the run state's
// Open["MI6"].
type Run struct {
	RunID string `json:"runId"`
	// StartMs is day 0's start: the run's open.
	StartMs int64 `json:"startMs"`
	// Drive is D1's setting (C2-R1): "test", "test+personal" or "personal".
	Drive string `json:"drive"`
	// OneDrive is set when A5 passed and --onedrive was given.
	OneDrive  bool     `json:"oneDrive"`
	Platforms []string `json:"platforms"`
	Pairs     []*Pair  `json:"pairs"`
	// Visibility is the matrix's measured cells.
	Visibility []results.VisibilityCell `json:"visibility,omitempty"`
	// LoginsDone is set once day 0's logins and the matrix are over.
	LoginsDone bool `json:"loginsDone,omitempty"`
	// ScanFailed is why a scan of the results failed the run (C2-R20): a
	// fixed reason that names no path, since the envelope carries it
	// (PH0-22).
	ScanFailed string `json:"scanFailed,omitempty"`
}

// Pair is one pair's state.
type Pair struct {
	PairDef
	Plan         string    `json:"plan"`
	HandoffOK    bool      `json:"handoffOk"`
	PlanAFailure string    `json:"planAFailure,omitempty"`
	B1Failure    string    `json:"b1Failure,omitempty"`
	Transient    int       `json:"transient,omitempty"`
	IssuedAtMs   int64     `json:"issuedAtMs,omitempty"`
	ClientType   string    `json:"clientType,omitempty"`
	Attempts     []Attempt `json:"attempts,omitempty"`
}

// Attempt is one of the hub's oauth_refresh events: a decided day, its
// attempts, its outcome, and when it came (Unix ms).
type Attempt struct {
	Day     int    `json:"day"`
	Attempt int    `json:"attempt"`
	Outcome string `json:"outcome"`
	AtMs    int64  `json:"atMs"`
}

// NewRun opens an MI6 run at start: Drive's pairs on the platforms, and
// OneDrive's with oneDrive.
func NewRun(runID string, start time.Time, drive string, oneDrive bool, platforms []string) *Run {
	r := &Run{RunID: runID, StartMs: start.UnixMilli(), Drive: drive, OneDrive: oneDrive, Platforms: platforms}
	defs := slices.Clone(Drive)
	if oneDrive {
		defs = append(defs, OneDrive...)
	}
	for _, d := range defs {
		if slices.Contains(platforms, d.Platform) {
			r.Pairs = append(r.Pairs, &Pair{PairDef: d, Plan: PlanNone})
		}
	}
	return r
}

// Decode reads a run from the run state.
func Decode(raw json.RawMessage) (*Run, error) {
	var r Run
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("mi6: the run state's MI6 run: %w", err)
	}
	return &r, nil
}

// Encode is the run for the run state.
func (r *Run) Encode() json.RawMessage {
	b, _ := json.Marshal(r) //nolint:errchkjson // plain fields
	return b
}

// Pair is the pair of dest, nil when the run has none.
func (r *Run) Pair(dest string) *Pair {
	for _, p := range r.Pairs {
		if p.Dest == dest {
			return p
		}
	}
	return nil
}

// Day is the run's day at now: 0 on its first 24 h.
func (r *Run) Day(now time.Time) int {
	return int(now.Sub(time.UnixMilli(r.StartMs)) / day)
}

// CloseAt is when the run closes: day 7, after its refresh had 2 h to
// show the expected expiry.
func (r *Run) CloseAt() time.Time {
	return time.UnixMilli(r.StartMs).Add(Days*day + 2*time.Hour)
}

// Completed records a login that completed: its plan and client type, and
// its issuance, day 0 of its refresh days; a new login's series starts
// anew.
func (r *Run) Completed(dest, plan, clientType string, issuedAtMs int64) {
	p := r.Pair(dest)
	if p == nil {
		return
	}
	p.Plan, p.HandoffOK, p.ClientType, p.IssuedAtMs, p.Attempts = plan, true, clientType, issuedAtMs, nil
	if plan == PlanB1 {
		p.B1Failure = ""
	}
}

// FailKind is the kind of a login's failure reason.
func FailKind(reason string) string {
	switch reason {
	case "invalid_grant", "access_denied", "redirect_uri_mismatch", "invalid_client":
		return FailRefusal
	case "state", "expired_token":
		return FailExpired
	}
	return FailTransient // "transient", "error" and any other
}

// Failed records a login's failure at stage ("complete": plan A; "device":
// B1) and returns its kind: a refusal of plan A is the pair's plan A
// failure, which starts B1 the same day; a refusal of B1 is B1's failure
// (B2); a failure without the provider's answer counts apart; an expired
// state records nothing. A failed refresh of the control endpoint is no
// login's.
func (r *Run) Failed(dest, stage, reason string) string {
	kind := FailKind(reason)
	p := r.Pair(dest)
	if p == nil || stage != stageComplete && stage != stageDevice {
		return kind
	}
	switch {
	case kind == FailTransient:
		p.Transient++
	case kind == FailRefusal && stage == stageComplete:
		p.PlanAFailure = reason
	case kind == FailRefusal:
		p.Plan, p.HandoffOK, p.B1Failure = PlanB1, false, reason
	}
	return kind
}

// Refreshed records one of the hub's oauth_refresh events.
func (r *Run) Refreshed(dest string, day, attempt int, outcome string, atMs int64) {
	if p := r.Pair(dest); p != nil {
		p.Attempts = append(p.Attempts, Attempt{Day: day, Attempt: attempt, Outcome: outcome, AtMs: atMs})
	}
}

// Series is a pair's refresh days at nowMs, days 1 to 7 from its issuance,
// those that began: a day is ok when any of its events is ok (C2-R16
// (d′)), expected-expiry, or failed; a day without ok, whose window gave
// the hub less than 15 min of the host's time outside reboot and sleep
// gaps, is blocked-by-test. A hub crash is never relabelled, and a day
// still open without a result is left out.
func (p *Pair) Series(gaps []results.Gap, nowMs int64) []results.RefreshDay {
	if p.IssuedAtMs == 0 {
		return nil
	}
	var out []results.RefreshDay
	for d := 1; d <= Days; d++ {
		from := p.IssuedAtMs + int64(d)*day.Milliseconds()
		to := from + day.Milliseconds()
		if from > nowMs {
			break
		}
		attempts, okDay, expiry, failed := 0, false, false, false
		for _, a := range p.Attempts {
			if a.Day != d {
				continue
			}
			attempts = max(attempts, a.Attempt)
			switch a.Outcome {
			case results.OutcomeOK:
				okDay = true
			case results.OutcomeExpectedExpiry:
				expiry = true
			case results.OutcomeFailed:
				failed = true
			}
		}
		outcome := ""
		switch {
		case okDay:
			outcome = results.OutcomeOK
		case expiry:
			outcome = results.OutcomeExpectedExpiry
		case (failed || to <= nowMs) && uptime(gaps, from, min(to, nowMs)) < minUptime.Milliseconds():
			outcome = results.OutcomeBlockedByTest
		case failed:
			outcome = results.OutcomeFailed
		default:
			continue // no result yet, or none came
		}
		out = append(out, results.RefreshDay{Day: d, Attempts: attempts, Outcome: outcome})
	}
	return out
}

// uptime is how much of [from, to) lies outside the reboot and sleep gaps.
func uptime(gaps []results.Gap, from, to int64) int64 {
	var spans [][2]int64
	for _, g := range gaps {
		if g.Reason != results.GapReboot && g.Reason != results.GapSleep {
			continue
		}
		if s, e := max(g.StartMs, from), min(g.EndMs, to); s < e {
			spans = append(spans, [2]int64{s, e})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	covered, end := int64(0), from
	for _, s := range spans {
		start := max(s[0], end)
		if s[1] > start {
			covered += s[1] - start
			end = s[1]
		}
	}
	return to - from - covered
}

// refreshFrom is the pair whose series is the evidence of a B1 pair after
// a plan A failure (P39): the pair that shares its client, Drive's g-web,
// or OneDrive's first other pair with a login.
func (r *Run) refreshFrom(p *Pair) string {
	if p.Plan != PlanB1 || !p.HasPlanA() {
		return ""
	}
	if p.Provider == ProviderDrive {
		return "g-web"
	}
	for _, o := range r.Pairs {
		if o.Provider == ProviderOneDrive && o.Dest != p.Dest && o.HandoffOK {
			return o.Dest
		}
	}
	return ""
}

// Meta is what the envelope says of its host and account.
type Meta struct {
	Host, Set, OS  string
	PiModel        *results.PiModelInfo
	Storage        string
	SdConfirmation *results.SdConfirmation
	Sealer         string
}

// Envelope is the run's envelope at endMs, with the host's gaps and the
// device's handoffs (keys "acct-1/google:android").
func (r *Run) Envelope(m Meta, gaps []results.Gap, handoffs map[string]int, endMs int64) results.Envelope {
	kind := "test"
	if r.Drive == "personal" {
		kind = "personal"
	}
	run := results.Run{RunID: r.RunID, StartMs: r.StartMs, EndMs: endMs, Valid: r.ScanFailed == "", InvalidReason: r.ScanFailed}
	for _, g := range gaps {
		if g.EndMs > r.StartMs && g.StartMs < endMs {
			run.Gaps = append(run.Gaps, g)
		}
	}
	data := &results.Mi6Data{OneDriveEnabled: r.OneDrive, Pairs: []results.Mi6Pair{}, Visibility: slices.Clone(r.Visibility),
		Handoffs: []results.Handoff{}}
	for _, p := range r.Pairs {
		data.Pairs = append(data.Pairs, results.Mi6Pair{Provider: p.Provider, Platform: p.Platform, Dest: p.Dest, Plan: p.Plan,
			HandoffOK: p.HandoffOK, PlanAFailure: p.PlanAFailure, B1Failure: p.B1Failure, Transient: p.Transient,
			IssuedAtMs: p.IssuedAtMs, RefreshFrom: r.refreshFrom(p), Refresh: p.Series(gaps, endMs)})
	}
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range handoffs {
			if !yield(k) {
				return
			}
		}
	}) {
		account, client, _ := strings.Cut(k, "/")
		data.Handoffs = append(data.Handoffs, results.Handoff{Account: account, Client: client, Count: handoffs[k]})
	}
	return results.Envelope{Schema: results.Schema, Code: "MI6", RunID: r.RunID, Set: m.Set, Host: m.Host, OS: m.OS,
		PiModel: m.PiModel, Storage: m.Storage, SdConfirmation: m.SdConfirmation, Sealer: m.Sealer, Account: Account,
		AccountKind: kind, Runs: []results.Run{run}, MI6: data}
}

// ClientFor is the client that a login of the pair under plan hands its
// code to (RouteFor's table): Google's Android or Desktop client under
// plan A, its TV client under B1; Microsoft's Entra client.
func ClientFor(d PairDef, plan string) string {
	if d.Provider == ProviderOneDrive {
		return "microsoft:entra"
	}
	switch {
	case plan == PlanB1 || d.Platform == "web":
		return "google:tv"
	case d.Platform == "windows":
		return "google:desktop"
	}
	return "google:android"
}

// HandoffKey is the run state's key of an account and client.
func HandoffKey(client string) string { return Account + "/" + client }

// ErrCap is a login refused at the handoff cap; its text is CapRefused.
var ErrCap error = capError{}

type capError struct{}

func (capError) Error() string { return CapRefused }

// AllowHandoff refuses a login of client once its account handed it
// HandoffCap logins.
func AllowHandoff(handoffs map[string]int, client string) error {
	if handoffs[HandoffKey(client)] >= HandoffCap {
		return ErrCap
	}
	return nil
}

// CountHandoff counts a completed login of a client type ("android",
// "desktop", "tv", "entra").
func CountHandoff(handoffs map[string]int, provider, clientType string) {
	client := "google:" + clientType
	if provider == ProviderOneDrive {
		client = "microsoft:" + clientType
	}
	handoffs[HandoffKey(client)]++
}
