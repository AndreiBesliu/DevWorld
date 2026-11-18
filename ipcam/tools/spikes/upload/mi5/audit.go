// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package mi5

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/AndreiBesliu/DevWorld/ipcam/tools/spikes/upload/results"
)

// The sources of a destination's missing, duplicate and mismatch counts
// (C2-R15 (c)).
const (
	AuditListRemote = "list-remote"
	AuditEvents     = "events-and-catalog"
)

// uploadsLine is diag uploads' line for a destination; the three checks
// are null without --list-remote.
type uploadsLine struct {
	Schema         string `json:"schema"`
	Dest           string `json:"dest"`
	Segments       int    `json:"segments"`
	Verified       int    `json:"verified"`
	Pending        int    `json:"pending"`
	Failed         int    `json:"failed"`
	Deleted        int    `json:"deleted"`
	Missing        *int   `json:"missing"`
	Duplicates     *int   `json:"duplicates"`
	HashMismatches *int   `json:"hashMismatches"`
}

// destProbeLine is diag dest-probe's line.
type destProbeLine struct {
	Schema string `json:"schema"`
	Probe  struct {
		Quota struct {
			Free  int64 `json:"free"`
			InBin int64 `json:"inBin"`
		} `json:"quota"`
	} `json:"probe"`
	QuotaAfterDelete []results.QuotaSample `json:"quotaAfterDelete"`
}

// audit is steps 7 and 8, with the hub stopped: OneDrive's quota after a
// delete (only with A5), then `diag uploads --list-remote` (on OneDrive,
// whose List is unsupported, the hub's events and the catalog), the
// calls per segment from upload_verified, and the restart's evidence.
func (r *run) audit(ctx context.Context) error {
	quota := map[string][]results.QuotaSample{}
	for _, d := range r.opts.Dests {
		if d.Kind != KindOneDrive {
			continue
		}
		lines, err := r.host.Diag(ctx, []string{"dest-probe", "--config", r.host.RunJSON(), "--dest", d.ID, "--delete-segment"})
		if err != nil {
			return fmt.Errorf("mi5: dest-probe %s: %w", d.ID, err)
		}
		for _, raw := range lines {
			var l destProbeLine
			if json.Unmarshal(raw, &l) == nil && l.Schema == "ipcam.diag.destprobe/1" {
				quota[d.ID] = l.QuotaAfterDelete
			}
		}
	}
	listed := map[string]uploadsLine{}
	lines, err := r.host.Diag(ctx, []string{"uploads", "--config", r.host.RunJSON(), "--list-remote"})
	if err != nil {
		return fmt.Errorf("mi5: diag uploads: %w", err)
	}
	for _, raw := range lines {
		var l uploadsLine
		if json.Unmarshal(raw, &l) == nil && l.Schema == "ipcam.diag.uploads/1" {
			listed[l.Dest] = l
		}
	}
	r.withTally(func(t *tally) {
		for _, d := range r.opts.Dests {
			r.data.Dests = append(r.data.Dests, t.dest(d, listed[d.ID], quota[d.ID]))
		}
		if r.data.Restart != nil {
			r.data.Restart.ResumedOffset = t.resumedOffset
			r.data.Restart.TornSegmentUploaded = t.tornUploaded()
		}
		r.data.InjectedGaps = t.gapsAfterKill
	})
	r.data.Dests = append(r.data.Dests, DeferredDests(r.cfg)...)
	return nil
}

// dest is a destination's outcome from its audit line l and the events.
func (t *tally) dest(d DestConfig, l uploadsLine, quota []results.QuotaSample) results.Mi5Dest {
	out := results.Mi5Dest{Dest: d.ID, Kind: d.Kind, Account: d.Account, AccountKind: d.AccountKind, Cameras: len(d.Cameras),
		Segments: l.Segments, Verified: l.Verified, SegmentsDuringBlock: t.segmentsDuringBlock(d),
		VerifiedDuringBlock: t.verifiedInBlock[d.ID], FailedDuringBlock: t.failedInBlock[d.ID],
		RateLimitedDuringBlock: t.rateLimitedInBlock[d.ID], UnreachableDuringBlock: t.unreachable[d.ID], QuotaAfterDelete: quota}
	if l.Missing != nil && l.Duplicates != nil && l.HashMismatches != nil {
		out.Missing, out.Duplicates, out.HashMismatches, out.AuditFrom = *l.Missing, *l.Duplicates, *l.HashMismatches, AuditListRemote
	} else {
		// OneDrive (List unsupported, C2-R12): a segment is missing when
		// the catalog verified none of its placements' uploads, duplicate
		// when the hub verified a placement twice; the hash and size are
		// the hub's own check at verification.
		out.Missing, out.AuditFrom = max(l.Segments-l.Verified-l.Deleted, 0), AuditEvents
	}
	if n := t.verified[d.ID]; n > 0 {
		out.CallsPerSegment = map[string]float64{}
		for k, v := range maps.All(t.calls[d.ID]) {
			out.CallsPerSegment[k] = v / float64(n)
		}
	}
	return out
}
