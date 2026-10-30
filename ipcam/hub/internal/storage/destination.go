package storage

import (
	"context"
	"io"
	"iter"
	"time"
)

// Kind is a destination's provider type (§9.1). The spec also names
// "NAS_SMB", "ONEDRIVE_BUSINESS", "S3", "WEBDAV" and "SFTP", which have no
// driver yet.
type Kind string

// The kinds that have a driver.
const (
	KindLocal            Kind = "LOCAL"
	KindGoogleDrive      Kind = "GOOGLE_DRIVE"
	KindOneDrivePersonal Kind = "ONEDRIVE_PERSONAL"
)

// Destination is one provider's driver (§9.1). A new provider is a new
// Destination that passes the conformance suite (STO-01), with no change to
// the rules, the outbox or the UI. Its errors are the typed errors of this
// package (errors.go), which Reason maps to §7.7's reasons.
type Destination interface {
	// Kind is the provider type.
	Kind() Kind
	// Capabilities is what the driver declares; the rest of the hub relies
	// on it only.
	Capabilities() Caps
	// Probe writes, reads and deletes a test object, and reads the quota
	// and the permissions.
	Probe(ctx context.Context) (ProbeReport, error)
	// BeginUpload starts an upload of size bytes to key.
	BeginUpload(ctx context.Context, key ObjectKey, size int64, meta Meta) (UploadState, error)
	// PutChunk sends the n bytes that r holds for offset off of st's object.
	PutChunk(ctx context.Context, st *UploadState, off int64, r io.Reader, n int64) error
	// Resume returns the offset from which st's upload continues. A session
	// that already holds every byte gives st.Size.
	Resume(ctx context.Context, st *UploadState) (nextOffset int64, err error)
	// Commit finishes st's upload and returns the object with the provider's
	// id, size and hash.
	Commit(ctx context.Context, st *UploadState) (ObjectRef, error)
	// Abort cancels st's upload and releases its session.
	Abort(ctx context.Context, st *UploadState) error
	// Stat returns what the provider says about ref's object now.
	Stat(ctx context.Context, ref ObjectRef) (ObjectInfo, error)
	// OpenRange reads n bytes of ref's object from offset off.
	OpenRange(ctx context.Context, ref ObjectRef, off, n int64) (io.ReadCloser, error)
	// PlaybackURL returns a link to ref's object valid for ttl, or
	// ErrProxyRequired when the hub must serve it.
	PlaybackURL(ctx context.Context, ref ObjectRef, ttl time.Duration) (string, error)
	// Delete deletes ref's object, permanently or into a recycle bin.
	Delete(ctx context.Context, ref ObjectRef) (DeleteOutcome, error)
	// Quota reads the destination's quota, as Capabilities().Quota says.
	Quota(ctx context.Context) (Quota, error)
	// PutSmall writes or atomically replaces a small object: the catalog,
	// a README.
	PutSmall(ctx context.Context, key ObjectKey, data []byte) (ObjectRef, error)
	// List yields the objects under prefix. It serves reconciliation and
	// import only, never normal operation.
	List(ctx context.Context, prefix ObjectKey) iter.Seq2[ObjectInfo, error]
}
