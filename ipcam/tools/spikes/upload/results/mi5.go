// SPIKE-ONLY: ipcam/docs/superpowers/plans/2026-10-18-phase0c2-upload-spike.md

package results

// Mi5Data is MI5's run (G14): the hub's WAN blocked for BlockSecs while
// it records, one hub restart in the middle of an upload after the
// unblock, then the drain until every placement of the run's own
// segments is verified.
//   - RecoverySecs is the drain's end minus the unblock, and UplinkMbps
//     the bytes uploaded during the drain over its seconds;
//   - DNSResolvedDuringBlock records whether the providers' names still
//     resolved while blocked;
//   - Lab is set on the lab's 15-min variant (TST-07), which never
//     decides G14;
//   - Reach are the block's checks, before the clock and every 10 min;
//   - Restart is the restart's evidence (P16), ReadOpen the open-segment
//     reads, InjectedGaps the recording gaps that the restart caused,
//     which are never missing segments;
//   - HubCrashes counts the times the hub ended on its own during the
//     run (neither stopped nor killed by it), each restarted at once with
//     a hub-crash gap in the run's gaps (Task 9).
type Mi5Data struct {
	BlockSecs              float64       `json:"blockSecs"`
	RecoverySecs           float64       `json:"recoverySecs"`
	UplinkMbps             float64       `json:"uplinkMbps"`
	DNSResolvedDuringBlock bool          `json:"dnsResolvedDuringBlock"`
	Lab                    bool          `json:"lab"`
	Reach                  []ReachCheck  `json:"reach"`
	Dests                  []Mi5Dest     `json:"dests"`
	Restart                *Mi5Restart   `json:"restart"`
	ReadOpen               []ReadOpenRun `json:"readOpen"`
	InjectedGaps           int           `json:"injectedGaps"`
	HubCrashes             int           `json:"hubCrashes,omitempty"`
}

// ReachCheck is one check of the block, as diag reach saw it from the
// blocked identity.
type ReachCheck struct {
	AtMs            int64 `json:"atMs"`
	ProviderBlocked bool  `json:"providerBlocked"`
	Lan             bool  `json:"lan"`
	Loopback        bool  `json:"loopback"`
}

// Mi5Dest is one destination's outcome:
//   - Segments are the run's own segments that its routes place there
//     (deleted ones aside), SegmentsDuringBlock those recorded during the
//     block; Verified, Missing, Duplicates and HashMismatches come from
//     the audit (diag uploads --list-remote), or, on OneDrive, from the
//     hub's upload_verified events and the catalog (AuditFrom says which,
//     C2-R15 (c));
//   - VerifiedDuringBlock, FailedDuringBlock (upload_failed reason
//     transient) and UnreachableDuringBlock (dest_state UNREACHABLE) are
//     the hub's events between Block + 2 min and Unblock;
//     RateLimitedDuringBlock counts upload_failed reason rate_limited
//     apart (C2-R15 (d));
//   - CallsPerSegment is the provider calls per verified segment, by
//     call, from the upload_verified events;
//   - QuotaAfterDelete is OneDrive's quota after a delete (only if A5
//     passed); Deferred, when set, says why the destination did not run.
type Mi5Dest struct {
	Dest                   string             `json:"dest"`
	Kind                   string             `json:"kind"`
	Account                string             `json:"account"`
	AccountKind            string             `json:"accountKind,omitempty"`
	Cameras                int                `json:"cameras"`
	Segments               int                `json:"segments"`
	SegmentsDuringBlock    int                `json:"segmentsDuringBlock"`
	Verified               int                `json:"verified"`
	Missing                int                `json:"missing"`
	Duplicates             int                `json:"duplicates"`
	HashMismatches         int                `json:"hashMismatches"`
	AuditFrom              string             `json:"auditFrom,omitempty"`
	VerifiedDuringBlock    int                `json:"verifiedDuringBlock"`
	FailedDuringBlock      int                `json:"failedDuringBlock"`
	RateLimitedDuringBlock int                `json:"rateLimitedDuringBlock,omitempty"`
	UnreachableDuringBlock bool               `json:"unreachableDuringBlock"`
	CallsPerSegment        map[string]float64 `json:"callsPerSegment,omitempty"`
	QuotaAfterDelete       []QuotaSample      `json:"quotaAfterDelete,omitempty"`
	Deferred               string             `json:"deferred,omitempty"`
}

// QuotaSample is a quota reading after a delete: free bytes and bytes in
// the recycle bin.
type QuotaSample struct {
	TMs   int64 `json:"tMs"`
	Free  int64 `json:"free"`
	InBin int64 `json:"inBin"`
}

// Mi5Restart is the restart's evidence (P16): when the hub was killed, the
// size of the placement in flight and its last confirmed offset before
// the kill, the offset at which the new hub resumed it, and whether the
// torn segment that the restart left was uploaded.
type Mi5Restart struct {
	KilledAtMs          int64 `json:"killedAtMs"`
	Size                int64 `json:"size"`
	LastConfirmedOffset int64 `json:"lastConfirmedOffset"`
	ResumedOffset       int64 `json:"resumedOffset"`
	TornSegmentUploaded bool  `json:"tornSegmentUploaded"`
}

// ReadOpenRun is a series of open-segment reads (diag readopen): on the
// encrypted pre-run's camera, or on a clear camera during the block.
type ReadOpenRun struct {
	Encrypted bool    `json:"encrypted"`
	Calls     int     `json:"calls"`
	OK        int     `json:"ok"`
	P50Ms     float64 `json:"p50Ms"`
	MaxMs     float64 `json:"maxMs"`
	// Failures counts the failed reads by diag readopen's error
	// (no-open-segment, unreadable) or the diag's exit ("exit N").
	Failures map[string]int `json:"failures,omitempty"`
}

// PiProbeSet is the set of uploadspike-probe's marker for an armv6 Pi
// (P17): an MI5 envelope that measures nothing.
const PiProbeSet = "pi-probe"

// PiProbeMarker is what uploadspike-probe writes on an armv6 Pi, which
// cannot run the hub, as c1's results.PiProbeMarker: an MI5 envelope of
// host "pi" in PiProbeSet, with the Pi's model as the probe read it
// (raw, its revision, and the source, "cpuinfo" or "device-tree"), known
// and unsupported, and no run, so the report can say why the Pi has no
// data. Its run id is the set's first, so that running the probe again
// replaces it. atMs is when the probe ran.
func PiProbeMarker(raw, revision, source string, atMs int64) Envelope {
	return Envelope{
		Schema: Schema, Code: "MI5", RunID: PiProbeSet + "-1", AtMs: atMs, Set: PiProbeSet, Host: "pi", OS: "linux",
		Storage: "unknown", Runs: []Run{},
		PiModel: &PiModelInfo{Raw: raw, Revision: revision, Tier: "unsupported", Known: true, Source: source},
	}
}
