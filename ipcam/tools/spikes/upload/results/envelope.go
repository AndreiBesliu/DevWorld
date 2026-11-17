// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

// Package results is the upload spike's results envelope (Task 9): every
// measurement of MI5, MI6 and MI8, on the owner's devices and in the lab,
// is written as an Envelope, and each gate's Judge reads them back. Results
// are public (PH0-22): an envelope names its host only by one of four
// pseudonyms, its account only as acct-N, and carries no host name,
// address, user name, client ID, token or camera credential.
package results

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
)

// Schema names the envelope format.
const Schema = "ipcam.phase0c2.result/1"

// The measurements that write envelopes, the hosts' pseudonyms (PH0-22),
// the kinds of storage under rec/ as diag host reports them, the sealers'
// backends, and the kinds of account.
var (
	codes        = []string{"MI5", "MI6", "MI8"}
	hosts        = []string{"pc", "pi", "lab-linux", "lab-windows"}
	storages     = []string{"ssd", "sd", "hdd", "unknown"}
	sealers      = []string{"dpapi-machine", "software"}
	accountKinds = []string{"test", "personal"}
)

// The gap reasons (P49, P53): a reboot and a sleep of the host, which the
// run state's heartbeat finds, and a hub that ended on its own.
const (
	GapReboot   = "reboot"
	GapSleep    = "sleep"
	GapHubCrash = "hub-crash"
)

// namePattern is a run id's or a set's form: it names a file and a folder.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

// accountPattern is an account's pseudonym.
var accountPattern = regexp.MustCompile(`^acct-[1-9][0-9]?$`)

// Envelope is one results file: the runs of one measurement on one host, in
// one set. Task 8 adds MI5's data, Task 10 MI8's.
type Envelope struct {
	Schema string `json:"schema"`
	// Code is "MI5", "MI6" or "MI8".
	Code  string `json:"code"`
	RunID string `json:"runId"`
	// Set is the envelope's set, such as "pc", "pi" or "lab-linux".
	Set string `json:"set"`
	// Host is "pc", "pi", "lab-linux" or "lab-windows".
	Host string `json:"host"`
	OS   string `json:"os"`
	// PiModel is the Pi's model, nil on a PC.
	PiModel *PiModelInfo `json:"piModel,omitempty"`
	// Storage is "ssd", "sd", "hdd" or "unknown".
	Storage string `json:"storage"`
	// SdConfirmation is the owner's answer to the SD-card warning (P02).
	SdConfirmation *SdConfirmation `json:"sdConfirmation,omitempty"`
	// Sealer is the hub's sealer: "dpapi-machine" or "software".
	Sealer string `json:"sealer,omitempty"`
	// Account is the login's account, "acct-1", and AccountKind "test" or
	// "personal" (D1, C2-R1).
	Account     string `json:"account,omitempty"`
	AccountKind string `json:"accountKind,omitempty"`
	Cameras     int    `json:"cameras,omitempty"`
	// Part and Parts are a lab annotation's position in its set; 0
	// elsewhere.
	Part  int `json:"part,omitempty"`
	Parts int `json:"parts,omitempty"`
	// AtMs is when the envelope was stored, in Unix ms on its writer's
	// clock, as c1's C1-R22: Write stamps it.
	AtMs int64 `json:"atMs"`
	Runs []Run `json:"runs"`
	// MI6 is an MI6 envelope's logins, refreshes and visibility (G15).
	MI6 *Mi6Data `json:"mi6,omitempty"`
}

// PiModelInfo is the Pi's model as ipcam-hub's diag host reports it.
type PiModelInfo struct {
	Raw            string `json:"raw"`
	Revision       string `json:"revision"`
	Tier           string `json:"tier"`
	Source         string `json:"source"`
	Supported      bool   `json:"supported"`
	Known          bool   `json:"known"`
	RecommendedMin int    `json:"recommendedMin"`
	RecommendedMax int    `json:"recommendedMax"`
}

// SdConfirmation is the text that the owner typed to confirm the SD-card
// warning, and when (Unix ms).
type SdConfirmation struct {
	Text string `json:"text"`
	AtMs int64  `json:"atMs"`
}

// Run is one run of the envelope's measurement, its validity and the gaps
// of the host under it.
type Run struct {
	RunID         string `json:"runId"`
	StartMs       int64  `json:"startMs"`
	EndMs         int64  `json:"endMs"`
	Valid         bool   `json:"valid"`
	InvalidReason string `json:"invalidReason,omitempty"`
	Gaps          []Gap  `json:"gaps,omitempty"`
}

// Gap is a stretch of a run when the test, not the hub, stopped it: a
// reboot or a sleep of the host; or when the hub crashed, which no judge
// relabels.
type Gap struct {
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Reason  string `json:"reason"`
}

// Mi6Data is MI6's run (G15): its pairs, the visibility matrix and the
// handoffs per account and client.
type Mi6Data struct {
	OneDriveEnabled bool             `json:"oneDriveEnabled"`
	Pairs           []Mi6Pair        `json:"pairs"`
	Visibility      []VisibilityCell `json:"visibility"`
	Handoffs        []Handoff        `json:"handoffs"`
}

// Mi6Pair is one platform × provider pair (P39):
//   - Dest is its destination's pseudonym (g-android … m-web);
//   - Plan is "A", "B1", or "none" while no login completed;
//   - HandoffOK is set once a login of Plan completed;
//   - PlanAFailure is the provider's refusal of plan A's login, when it
//     refused one (the hub's oauth_failed reason), and B1Failure B1's;
//   - Transient counts the logins that ended without the provider's
//     answer (oauth_failed reason "transient" or "error", C2-R16 (c)),
//     which judge nothing;
//   - Refresh is the pair's daily refresh, days 1 to 7 from IssuedAtMs;
//     under B1, after a plan A failure, it is RefreshFrom's series, the
//     pair that shares its client (Drive: g-web).
type Mi6Pair struct {
	Provider     string       `json:"provider"`
	Platform     string       `json:"platform"`
	Dest         string       `json:"dest"`
	Plan         string       `json:"plan"`
	HandoffOK    bool         `json:"handoffOk"`
	PlanAFailure string       `json:"planAFailure,omitempty"`
	B1Failure    string       `json:"b1Failure,omitempty"`
	Transient    int          `json:"transient,omitempty"`
	IssuedAtMs   int64        `json:"issuedAtMs,omitempty"`
	RefreshFrom  string       `json:"refreshFrom,omitempty"`
	Refresh      []RefreshDay `json:"refresh"`
}

// The outcomes of a refresh day.
const (
	OutcomeOK             = "ok"
	OutcomeFailed         = "failed"
	OutcomeExpectedExpiry = "expected-expiry"
	OutcomeBlockedByTest  = "blocked-by-test"
)

// RefreshDay is a day of a pair's daily refresh: "ok", "failed",
// "expected-expiry" (day 7 in Testing mode) or "blocked-by-test" (every
// attempt fell in a reboot or sleep gap of the host).
type RefreshDay struct {
	Day      int    `json:"day"`
	Attempts int    `json:"attempts"`
	Outcome  string `json:"outcome"`
}

// VisibilityCell is one cell of the cross-client visibility matrix (P21):
// a file that a hub client of type Creator made, listed and read by
// Reader; Self marks the cells of one client, which are not judged.
type VisibilityCell struct {
	Provider string `json:"provider"`
	Creator  string `json:"creator"`
	Reader   string `json:"reader"`
	Listed   bool   `json:"listed"`
	Read     bool   `json:"read"`
	Self     bool   `json:"self"`
}

// Handoff is how many logins of an account handed a client's code to the
// hub (capped at 20, P39).
type Handoff struct {
	Account string `json:"account"`
	Client  string `json:"client"`
	Count   int    `json:"count"`
}

// Validate checks e before it is written: the schema, the code, file-safe
// names, a host pseudonym and the closed lists of its other fields.
func (e Envelope) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	check(e.Schema == Schema, "results: schema %q, want %q", e.Schema, Schema)
	check(slices.Contains(codes, e.Code), "results: unknown code %q", e.Code)
	check(namePattern.MatchString(e.RunID), "results: run id %q is not a file name", e.RunID)
	check(namePattern.MatchString(e.Set), "results: set %q is not a folder name", e.Set)
	check(slices.Contains(hosts, e.Host), "results: host %q is not one of the pseudonyms %v", e.Host, hosts)
	check(slices.Contains(storages, e.Storage), "results: unknown storage %q", e.Storage)
	check(e.Sealer == "" || slices.Contains(sealers, e.Sealer), "results: unknown sealer %q", e.Sealer)
	check(e.Account == "" || accountPattern.MatchString(e.Account), "results: account %q is not a pseudonym", e.Account)
	check(e.AccountKind == "" || slices.Contains(accountKinds, e.AccountKind), "results: unknown account kind %q", e.AccountKind)
	check((e.MI6 != nil) == (e.Code == "MI6"), "results: MI6 data belongs to an MI6 envelope only")
	return errors.Join(errs...)
}
