package storage

// Caps is what a driver declares about its provider (§9.1). The rest of the
// hub relies on these fields only. The zero value of each kind is the most
// conservative one (P24), so a field left unset claims the least.
type Caps struct {
	Resumable  ResumeKind // how an interrupted upload resumes
	ChunkAlign int64      // every chunk but the last is a multiple of it: 256 KiB Drive, 320 KiB OneDrive; 0 = any
	PartSize   int64      // the fixed chunk size that the driver chose (§9.2)
	MaxRequest int64      // the largest request body: < 60 MiB OneDrive; 0 = no limit
	EqualParts bool       // every part but the last has the same size (Cloudflare R2 requires it)
	MaxObject  int64      // the largest object; 0 = no limit
	Playback   LinkKind   // how a client plays an object
	RangeRead  bool       // OpenRange reads a byte range without the rest
	Delete     DeleteKind // what Delete does with an object
	Quota      QuotaKind  // what Quota reads
	Hashes     []HashKind // the provider's hashes of an object; none = verification by reading back
	// ServerLifecycle is true when the provider expires objects itself (S3, R2, B2).
	ServerLifecycle bool
	DailyUploadCap  int64   // bytes per day per user: Drive 750 GiB; 0 = none
	CreatesPerSec   float64 // sustained object creations per second: Drive ≈ 2; 0 = no limit
}

// ResumeKind is how an interrupted upload resumes.
type ResumeKind int

// The resume kinds. The zero value, ResumeNone, means an interrupted upload
// starts again from 0.
const (
	ResumeNone        ResumeKind = iota
	ResumeSessionURI             // Drive: the resumable session's URI
	ResumeUploadURL              // OneDrive: the upload session's uploadUrl
	ResumeMultipart              // S3: the uploadId and the parts' ETags
	ResumeOffsetWrite            // local disk, SMB, SFTP: a .part written at offsets
	ResumeDavChunked             // Nextcloud: chunking v2
)

// LinkKind is how a client plays an object.
type LinkKind int

// The link kinds. The zero value, LinkProxyOnly, means only through the hub.
const (
	LinkProxyOnly    LinkKind = iota // through the hub's proxy (Drive)
	LinkPresigned                    // a presigned URL (S3)
	LinkPreauthShort                 // a pre-authenticated URL valid for minutes (OneDrive's downloadUrl)
	LinkLocalFile                    // a file on the hub's disk
)

// DeleteKind is what Delete does with an object.
type DeleteKind int

// The delete kinds. The zero value, DeleteRecycleBin, assumes that a deleted
// object may still take quota until the bin empties.
const (
	DeleteRecycleBin DeleteKind = iota // the object goes to a recycle bin, which may still take quota (OneDrive personal)
	DeletePermanent                    // the object and its bytes are gone
)

// QuotaKind is what Quota reads.
type QuotaKind int

// The quota kinds. The zero value, QuotaNone, means Quota reads nothing.
const (
	QuotaNone       QuotaKind = iota // no quota: Quota.Unknown (S3)
	QuotaAccount                     // the account's storage quota (Drive, OneDrive)
	QuotaFilesystem                  // the free space of a filesystem (local disk)
)

// HashKind names a provider's hash of an object; Digest computes it for
// local bytes.
type HashKind string

// The hash kinds.
const (
	HashMD5           HashKind = "MD5"            // Drive's md5Checksum
	HashQuickXor      HashKind = "QUICKXOR"       // OneDrive's quickXorHash
	HashSHA256        HashKind = "SHA256"         // the local disk's
	HashETagMultipart HashKind = "ETAG_MULTIPART" // S3's multipart ETag: per-part MD5s, never a digest of the object
)
