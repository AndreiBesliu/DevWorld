// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/hubproc"
)

// ReachResult is what diag reach saw from the blocked identity: whether
// every provider host is blocked over IPv4 and over IPv6, and whether the
// LAN probe (its IPv4 line, C2-R15 (c)) and loopback answer.
type ReachResult struct{ ProviderBlockedV4, ProviderBlockedV6, Lan, Loopback bool }

// The block's failures, each the owner's line.
var (
	ErrBlockCutLAN     = ownerError(MsgBlockCutLAN)
	ErrProviderReachV4 = ownerError(MsgProviderReachV4)
	ErrProviderReachV6 = ownerError(MsgProviderReachV6)
)

// errNoProviderLine is a diag reach that printed no provider line.
var errNoProviderLine = errors.New("diag reach printed no provider line")

// ownerError is an error whose text is a whole line for the owner.
type ownerError string

func (e ownerError) Error() string { return string(e) }

// OwnerLine marks the error's text as a whole line for the owner.
func (ownerError) OwnerLine() {}

// Failure is a failure whose text is a whole Romanian line for the owner
// (OwnerLine), with the cause it stands for: errors.Is and errors.As reach
// the cause, and the lab's and the tests' runs, which are never judged,
// print its text on a detail line (uploadspike's mi5); the owner's screen
// never shows it.
type Failure struct {
	Line  string
	Cause error
}

func (e *Failure) Error() string { return e.Line }

// Unwrap is the failure's cause.
func (e *Failure) Unwrap() error { return e.Cause }

// OwnerLine marks the failure's text as a whole line for the owner.
func (*Failure) OwnerLine() {}

// reachLine is one line of diag reach.
type reachLine struct {
	Schema string `json:"schema"`
	Target string `json:"target"`
	Family string `json:"family"`
	OK     bool   `json:"ok"`
	Err    string `json:"err"`
}

// blockedKinds are the errors that count as a blocked path: refused is a
// reset from the far end, so the path is open (Verification).
var blockedKinds = []string{"timeout", "unreachable", "denied", "no-address"}

// VerifyBlocked runs `ipcam-hub diag reach --hosts <hosts> --lan <lan>` as
// the blocked identity (hub's RunAsUser on Linux; the same ipcam-hub.exe
// on Windows) and checks its lines: a LAN or loopback probe that does not
// answer is ErrBlockCutLAN; a provider host reachable, or refused, over a
// family is ErrProviderReachV4 or ErrProviderReachV6.
func VerifyBlocked(ctx context.Context, hub hubproc.Config, hosts []string, lan string) (ReachResult, error) {
	args := []string{"reach", "--hosts", strings.Join(hosts, ",")}
	if lan != "" {
		args = append(args, "--lan", lan)
	}
	lines, err := hubproc.Diag(ctx, hub, args, nil)
	if err != nil {
		return ReachResult{}, &Failure{Line: MsgReachFailed, Cause: err}
	}
	return judgeReach(lines)
}

// judgeReach reads diag reach's lines (VerifyBlocked).
func judgeReach(lines []json.RawMessage) (ReachResult, error) {
	res := ReachResult{ProviderBlockedV4: true, ProviderBlockedV6: true, Loopback: true}
	providers, loopbacks := 0, 0
	for _, raw := range lines {
		var l reachLine
		if json.Unmarshal(raw, &l) != nil || l.Schema != "ipcam.diag.reach/1" {
			continue
		}
		switch l.Target {
		case "provider":
			providers++
			blocked := !l.OK && slices.Contains(blockedKinds, l.Err)
			if l.Family == "v4" {
				res.ProviderBlockedV4 = res.ProviderBlockedV4 && blocked
			} else {
				res.ProviderBlockedV6 = res.ProviderBlockedV6 && blocked
			}
		case "lan":
			if l.Family == "v4" {
				res.Lan = l.OK
			}
		case "loopback":
			loopbacks++
			res.Loopback = res.Loopback && l.OK
		}
	}
	if providers == 0 {
		res.ProviderBlockedV4, res.ProviderBlockedV6 = false, false
	}
	res.Loopback = res.Loopback && loopbacks > 0
	switch {
	case providers == 0:
		return res, &Failure{Line: MsgReachNoProvider, Cause: errNoProviderLine}
	case !res.Lan || !res.Loopback:
		return res, ErrBlockCutLAN
	case !res.ProviderBlockedV4:
		return res, ErrProviderReachV4
	case !res.ProviderBlockedV6:
		return res, ErrProviderReachV6
	}
	return res, nil
}
