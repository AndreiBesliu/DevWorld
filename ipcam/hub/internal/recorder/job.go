// Package recorder is the hub's adapter over the MediaMTX process (§4.2,
// §4.3): it generates MediaMTX's configuration, starts the process, starts
// it again when it exits, receives its segment-closed hook, catalogs the
// segments and their gaps, reconciles torn recordings at startup, and
// verifies closed segments in the background.
//
// It holds no private key (§4.3): it sees the recipients of each job, and a
// TailSealer and a Recording that ipcam-hub's main builds from RI_g.
package recorder

import (
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/crypto"
)

// SmallPartBytes is the size under which an encrypted part counts as small
// (§4.5 as amended by O17): a "stream below 512 kbps" means parts smaller
// than this, one age chunk (crypto.ChunkSize). With the camera's setting on,
// MTX-2 pads every small part to the chunk boundary.
const SmallPartBytes = 65536

// CameraID names a camera. It is a directory name under rec/.
type CameraID string

// Profile is a camera's stream: "main" or "sub".
type Profile string

// The profiles.
const (
	ProfileMain Profile = "main"
	ProfileSub  Profile = "sub"
)

// Job is what to record of one stream.
type Job struct {
	Cam         CameraID
	Profile     Profile
	RestreamURL string // where MediaMTX pulls the stream, over RTSP/TCP
	SegmentSecs int    // segments close at the first keyframe after this
	PartSecs    int    // each part is fsynced when it closes
	// Scope holds the two public recipients of an encrypted recording; nil
	// records in clear.
	Scope *crypto.KeyScope
	// PadLowBitrate is the camera's O17 setting „Pierdere minimă la pană de
	// curent” (cameras.min_power_loss): true pads every encrypted part smaller
	// than SmallPartBytes to the 64 KiB chunk boundary.
	PadLowBitrate bool
}

// NewJob returns the job of a stream with the spec's defaults: 300 s
// segments of 1 s parts (§4.5), and padding on (O17).
func NewJob(cam CameraID, p Profile, restreamURL string, scope *crypto.KeyScope) Job {
	return Job{Cam: cam, Profile: p, RestreamURL: restreamURL, SegmentSecs: 300, PartSecs: 1, Scope: scope, PadLowBitrate: true}
}

// pathName is the job's MediaMTX path, <cam>/<profile>. MediaMTX's %path in
// recordPath expands to it, so the stream's files are under
// rec/<cam>/<profile>/.
func (j Job) pathName() string { return string(j.Cam) + "/" + string(j.Profile) }

func (j Job) key() streamKey { return streamKey{j.Cam, j.Profile} }

// streamKey names a stream.
type streamKey struct {
	cam     CameraID
	profile Profile
}

func (k streamKey) String() string { return string(k.cam) + "/" + string(k.profile) }

// SegmentClosed is a segment that MediaMTX completed and renamed to its
// final name. Path is absolute.
type SegmentClosed struct {
	Cam            CameraID
	Profile        Profile
	Path           string
	StartMs, EndMs int64
	Encrypted      bool
}

// RecStatus is how a stream records. The ages are -1 until there is one.
type RecStatus struct {
	LastPartAgoMs    int64 // since the stream's open .part last grew, polled every 250 ms
	LastSegmentAgoMs int64 // since its last SegmentClosed
	Running          bool  // the stream is applied and MediaMTX runs
}

// ReconcileReport is what reconcile did at startup, with what the
// background rewrites and the repairs of the .part files that MediaMTX
// left while the hub runs added since (ReconcileObserver), or what a
// background verification found.
type ReconcileReport struct {
	Finalized   int // .part files given their final name
	Truncated   int // files cut back to their durable end, their last part salvaged
	LostTails   int // streams that lost the end of a segment: each is a restart gap
	GapsWritten int
	Rewritten   int // salvages rewritten under a fresh file key in the background (C1-R9)
	// Unreadable counts files that could not be decrypted or parsed, TornParts
	// files that end in a torn fMP4 part, and CatalogMismatches the rows and
	// files that disagree after reconcile (G07): all three must stay 0.
	Unreadable, TornParts, CatalogMismatches int
	Aborted                                  bool // more than half of the segments would be lost: nothing changed (HUB-09)
}

// Observer is told what the recorder does. Its methods may be called from
// any goroutine, and must return quickly.
type Observer interface {
	ChildRestarted(at time.Time)
	// ChildLog gets every line the MediaMTX child writes, on stdout (its
	// log) or stderr (a crash), in order.
	ChildLog(line string)
	// FirstPart is the first growth of a new .part after the child started.
	FirstPart(cam CameraID, p Profile, at time.Time)
	// Verified gets the Unreadable, TornParts and CatalogMismatches of each
	// background verification.
	Verified(r ReconcileReport)
}

// The Observer may also implement these.
type (
	// GapObserver is told of each gap written, open, or closed.
	GapObserver interface{ Gap(g catalog.Gap) }
	// SegmentVerifiedObserver is told of each segment that the background
	// verification passed, by its absolute path.
	SegmentVerifiedObserver interface{ SegmentVerified(path string) }
	// ReconcileObserver gets the startup's reconcile report, then the whole
	// report again each time a background rewrite or a repair of the .part
	// files that MediaMTX left while the hub runs has added to it, or such
	// work was queued: the update with the highest Seq holds everything.
	ReconcileObserver interface{ Reconciled(u ReconcileUpdate) }
)

// ReconcileUpdate is the reconcile report as it stands. Seq numbers the
// updates, 1 for the startup's and one more for each later one; updates
// from two goroutines may arrive out of order, so a reader keeps the one
// with the highest Seq. Final is true when no background rewrite or repair
// is pending: the report is complete until MediaMTX leaves another .part.
type ReconcileUpdate struct {
	ReconcileReport
	Seq   int
	Final bool
}

// nopObserver is the Observer of a recorder without one.
type nopObserver struct{}

func (nopObserver) ChildRestarted(time.Time)               {}
func (nopObserver) ChildLog(string)                        {}
func (nopObserver) FirstPart(CameraID, Profile, time.Time) {}
func (nopObserver) Verified(ReconcileReport)               {}
