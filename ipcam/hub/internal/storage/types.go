package storage

// ObjectKey is an object's path under the destination's root, with "/".
type ObjectKey string

// Meta describes an object that an upload creates.
type Meta struct {
	ContentType string
	Class       string // the copy's class (§9.4, §9.7), such as CONTINUOUS
}

// CallCounts counts a driver's API calls by method name (P18), such as
// "files.create.resumable" or "upload.put".
type CallCounts map[string]int

// UploadState is an upload in progress. The outbox stores it in the catalog
// after each confirmed chunk (§9.1), so that Resume continues it after a
// restart.
type UploadState struct {
	Key  ObjectKey
	Size int64
	// Session is the provider's session: the Drive session URI, or Graph's
	// uploadUrl. It goes to the catalog only, never into an event, a log or a
	// result.
	Session string
	Offset  int64      // the bytes that the provider confirmed
	Calls   CallCounts // this upload's API calls
}

// ObjectRef identifies a stored object: Commit returns the provider's id for
// it, and the size and hash that the provider reports.
type ObjectRef struct {
	ID       string
	Key      ObjectKey
	Size     int64
	Hash     string // as Digest writes it for HashKind
	HashKind HashKind
}

// ObjectInfo is what Stat and List say about an object.
type ObjectInfo struct {
	Ref       ObjectRef
	ModTimeMs int64 // UTC milliseconds
}

// ProbeReport is what Probe found: whether a test object could be written,
// read by range and deleted, the quota, and warnings for the owner.
type ProbeReport struct {
	Writable, RangeRead, Deletable bool
	Quota                          Quota
	Warnings                       []string
}

// DeleteOutcome is what Delete did with an object.
type DeleteOutcome int

// The delete outcomes.
const (
	DeletedPermanent DeleteOutcome = iota // the object and its bytes are gone
	DeletedRecycled                       // the object is in a recycle bin, which may still take quota
)

// Quota is a destination's storage, in bytes. InBin is what the recycle bin
// holds (OneDrive's quota.deleted). Unknown is true when the provider has no
// quota (S3); the other fields are then 0.
type Quota struct {
	Total, Used, Free, InBin int64
	Unknown                  bool
}
