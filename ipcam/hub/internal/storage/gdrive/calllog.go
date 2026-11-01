package gdrive

import (
	"maps"
	"sync"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// The API methods, by the names of the call log (P18): Drive's reference
// names, and three of the resumable upload protocol.
const (
	methodCreateResumable = "files.create.resumable" // a resumable upload's session
	methodUploadPut       = "upload.put"             // a chunk sent to a session
	methodUploadStatus    = "upload.status"          // a session's status query
	methodGet             = "files.get"
	methodGetMedia        = "files.get.media"
	methodDelete          = "files.delete"
	methodAbout           = "about.get"
	methodGenerateIDs     = "files.generateIds"
	methodCreate          = "files.create" // a folder, or a small file in one request
	methodUpdate          = "files.update" // a small file's new bytes
	methodList            = "files.list"   // inside List only
)

// callLog counts the driver's API calls by method since New.
type callLog struct {
	mu     sync.Mutex
	counts storage.CallCounts
}

// add counts a call of method in the log, and in upload, the counts of the
// upload that the call serves (nil when it serves none). An upload's counts
// belong to its own goroutine.
func (l *callLog) add(method string, upload storage.CallCounts) {
	l.mu.Lock()
	l.counts[method]++
	l.mu.Unlock()
	if upload != nil {
		upload[method]++
	}
}

// CallLog returns the API calls that the driver sent since New, by method
// (P18): those of uploads, probes, quota reads and lists alike. Each upload's
// own are in its UploadState.Calls, the folders that its BeginUpload made
// included.
func (d *Drive) CallLog() storage.CallCounts {
	d.calls.mu.Lock()
	defer d.calls.mu.Unlock()
	return maps.Clone(d.calls.counts)
}
