// Package fakegdrive is a fake of the Google Drive API v3 for the hub's
// tests (P04): it serves over HTTP the requests that the gdrive driver
// makes, with Drive's semantics, so that the driver's tests need no network
// and no account. It keeps files and folders in memory, issues IDs with
// files.generateIds and answers 409 to a create that reuses the ID of a file
// that exists, runs resumable uploads (308 with Range until the last byte,
// then the file), gives each file the md5Checksum of its bytes, counts the
// requests of each API method and meters the creates in any second
// (STO-07). Tests inject answers with FailNext, Drive's daily upload cap with
// DailyCapAfter, a quota with SetQuota and a token check with AcceptToken.
// It is permanent test infrastructure, as the drivers are (P04).
package fakegdrive

import (
	"bytes"
	"crypto/md5" //nolint:gosec // G501: Drive's md5Checksum, which the fake reproduces
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FolderMimeType is the MIME type of a Drive folder.
const FolderMimeType = "application/vnd.google-apps.folder"

// Reset is the status of FailNext that drops the connection without an
// answer, as a route that went down does.
const Reset = -1

const (
	// rootID is the alias of My Drive's root, the parent of the files in it.
	rootID = "root"
	// chunkAlign is the multiple that every chunk but the last must be.
	chunkAlign = 256 << 10
	// maxBody bounds the body of one request.
	maxBody = 128 << 20
	// capPause is how long the daily cap holds once it is reached.
	capPause = 24 * time.Hour
	// defaultLimit is a new fake's storage limit: a free account's 15 GiB.
	defaultLimit = 15 << 30
)

// Options configure a Fake.
type Options struct {
	// Clock is the time of the create meter, of modifiedTime and of the
	// daily cap; time.Now by default.
	Clock func() time.Time
}

// Object is a file or a folder that the fake holds.
type Object struct {
	ID, Name, MimeType string
	Parent             string // "root" for a file in My Drive's root
	Folder             bool
	Data               []byte
	MD5                string // md5Checksum in hex; "" for a folder
	Modified           time.Time
}

// Fake is the fake Drive. Its methods are safe for concurrent use.
type Fake struct {
	clock func() time.Time

	mu       sync.Mutex
	seq      int
	files    map[string]*file
	issued   map[string]bool // the IDs that files.generateIds gave out
	sessions map[string]*session
	counts   map[string]int
	fails    map[string][]failure
	creates  []time.Time // when each create arrived
	limit    int64       // the storage limit; 0 = none
	used     int64       // the bytes used besides the fake's files
	capBytes int64       // the daily cap's bytes; < 0 = none
	capSent  int64       // the bytes uploaded toward the cap
	capHit   time.Time   // when the cap was reached; zero while it is not
	accept   func(token string) bool
}

type file struct {
	Object
	seq int // creation order
}

// session is a resumable upload.
type session struct {
	size int64
	meta metadata
	data []byte
	file string // the ID of the file that the upload created, once complete
}

// failure is an answer that FailNext injected.
type failure struct {
	status     int
	reason     string
	retryAfter time.Duration
}

// metadata is a file's resource as a create's body gives it.
type metadata struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	MimeType string   `json:"mimeType"`
	Parents  []string `json:"parents"`
}

// New returns an empty fake with a 15 GiB limit, no daily cap, and a token
// check that accepts any bearer token.
func New(opts Options) *Fake {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &Fake{
		clock:    opts.Clock,
		files:    map[string]*file{},
		issued:   map[string]bool{},
		sessions: map[string]*session{},
		counts:   map[string]int{},
		fails:    map[string][]failure{},
		limit:    defaultLimit,
		capBytes: -1,
	}
}

// Handler serves the Drive API: the paths under /drive/v3 and
// /upload/drive/v3, which may sit under a prefix that a mux strips.
func (f *Fake) Handler() http.Handler { return http.HandlerFunc(f.serve) }

// Counts returns how many requests of each API method arrived, by the names
// of the driver's CallLog, such as "files.create.resumable" or "upload.put".
func (f *Fake) Counts() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := make(map[string]int, len(f.counts))
	for k, v := range f.counts {
		counts[k] = v
	}
	return counts
}

// Objects returns the files and folders that the fake holds, in the order
// they were created.
func (f *Fake) Objects() []Object {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := make([]*file, 0, len(f.files))
	for _, fl := range f.files {
		all = append(all, fl)
	}
	slices.SortFunc(all, func(a, b *file) int { return a.seq - b.seq })
	objects := make([]Object, len(all))
	for i, fl := range all {
		objects[i] = fl.Object
		objects[i].Data = bytes.Clone(fl.Data)
	}
	return objects
}

// SetQuota sets the storage limit to total bytes (0: no limit) and the
// bytes used besides the fake's files to used.
func (f *Fake) SetQuota(total, used int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limit, f.used = total, used
}

// FailNext makes the next request of method answer status with Drive's
// error body naming reason, and a Retry-After header of retryAfter when it
// is positive. A status below 400 answers with an empty resource; Reset
// drops the connection. Several calls queue in order.
func (f *Fake) FailNext(method string, status int, reason string, retryAfter time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails[method] = append(f.fails[method], failure{status: status, reason: reason, retryAfter: retryAfter})
}

// DailyCapAfter caps the bytes uploaded from now on at bytes, as Drive's
// 750 GiB a day: an upload that would pass it answers 403
// dailyLimitExceeded, and so does every upload until 24 h after the cap was
// reached, when the count starts again. A negative bytes removes the cap.
func (f *Fake) DailyCapAfter(bytes int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.capBytes, f.capSent, f.capHit = bytes, 0, time.Time{}
}

// AcceptToken sets the check of each request's bearer token: a request
// whose token it refuses, or with none, answers 401. nil accepts any token.
func (f *Fake) AcceptToken(accept func(token string) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accept = accept
}

// MaxCreatesPerSec returns the most creates (files.create.resumable and
// files.create) that arrived within any 1-s window (t − 1 s, t] of the
// fake's clock.
func (f *Fake) MaxCreatesPerSec() int {
	f.mu.Lock()
	times := slices.Clone(f.creates)
	f.mu.Unlock()
	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	most, from := 0, 0
	for i, t := range times {
		for !times[from].After(t.Add(-time.Second)) {
			from++
		}
		most = max(most, i-from+1)
	}
	return most
}

// route names the API method of r and the file ID in its path, or returns
// "" for a request the fake does not serve.
func route(r *http.Request) (method, id string) {
	p, q := r.URL.Path, r.URL.Query()
	fileID := func(prefix string) string {
		id := strings.TrimPrefix(p, prefix)
		if id == p || id == "" || strings.Contains(id, "/") {
			return ""
		}
		return id
	}
	switch {
	case p == "/upload/drive/v3/files" && r.Method == http.MethodPost && q.Get("uploadType") == "resumable":
		return "files.create.resumable", ""
	case p == "/upload/drive/v3/files" && r.Method == http.MethodPost && q.Get("uploadType") == "multipart":
		return "files.create", ""
	case p == "/upload/drive/v3/files" && r.Method == http.MethodPut && q.Get("upload_id") != "":
		if strings.HasPrefix(r.Header.Get("Content-Range"), "bytes */") {
			return "upload.status", ""
		}
		return "upload.put", ""
	case r.Method == http.MethodPatch && q.Get("uploadType") == "media" && fileID("/upload/drive/v3/files/") != "":
		return "files.update", fileID("/upload/drive/v3/files/")
	case p == "/drive/v3/files" && r.Method == http.MethodPost:
		return "files.create", ""
	case p == "/drive/v3/files" && r.Method == http.MethodGet:
		return "files.list", ""
	case p == "/drive/v3/files/generateIds" && r.Method == http.MethodGet:
		return "files.generateIds", ""
	case r.Method == http.MethodGet && fileID("/drive/v3/files/") != "":
		if q.Get("alt") == "media" {
			return "files.get.media", fileID("/drive/v3/files/")
		}
		return "files.get", fileID("/drive/v3/files/")
	case r.Method == http.MethodDelete && fileID("/drive/v3/files/") != "":
		return "files.delete", fileID("/drive/v3/files/")
	case p == "/drive/v3/about" && r.Method == http.MethodGet:
		return "about.get", ""
	}
	return "", ""
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	method, id := route(r)
	if method == "" {
		writeError(w, http.StatusNotFound, "notFound", 0)
		return
	}
	fail, failed, authorized := f.arrive(method, r)
	switch {
	case failed && fail.status == Reset:
		reset(w)
		return
	case failed && fail.status < http.StatusBadRequest:
		writeJSON(w, fail.status, map[string]any{})
		return
	case failed:
		writeError(w, fail.status, fail.reason, fail.retryAfter)
		return
	case !authorized:
		writeError(w, http.StatusUnauthorized, "authError", 0)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return // the client went away
	}
	if len(body) > maxBody {
		writeError(w, http.StatusRequestEntityTooLarge, "uploadTooLarge", 0)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case "files.create.resumable":
		f.createSession(w, r, body)
	case "upload.put", "upload.status":
		f.putSession(w, r, body)
	case "files.create":
		f.create(w, r, body)
	case "files.update":
		f.update(w, r, id, body)
	case "files.get":
		f.get(w, r, id)
	case "files.get.media":
		f.media(w, r, id)
	case "files.delete":
		f.delete(w, id)
	case "files.generateIds":
		f.generateIDs(w, r)
	case "files.list":
		f.list(w, r)
	case "about.get":
		f.about(w, r)
	}
}

// arrive counts r as a request of method, meters it when it is a create,
// and returns the failure that FailNext queued for it, if any, and whether
// its bearer token is accepted.
func (f *Fake) arrive(method string, r *http.Request) (fail failure, failed, authorized bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[method]++
	if method == "files.create.resumable" || method == "files.create" {
		f.creates = append(f.creates, f.clock())
	}
	if queued := f.fails[method]; len(queued) > 0 {
		fail, f.fails[method] = queued[0], queued[1:]
		return fail, true, false
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	authorized = ok && token != "" && (f.accept == nil || f.accept(token))
	return failure{}, false, authorized
}

// createSession opens a resumable upload of X-Upload-Content-Length bytes,
// whose file the body describes, and answers its session URI in Location,
// under the path that the request came to.
func (f *Fake) createSession(w http.ResponseWriter, r *http.Request, body []byte) {
	size, err := strconv.ParseInt(r.Header.Get("X-Upload-Content-Length"), 10, 64)
	if err != nil || size < 0 {
		writeError(w, http.StatusBadRequest, "badContentLength", 0)
		return
	}
	meta, status, reason := f.newFile(body)
	if status == 0 {
		status, reason = f.uploadRefused(size, 0)
	}
	if status != 0 {
		writeError(w, status, reason, 0)
		return
	}
	reqURL, err := url.ParseRequestURI(r.RequestURI)
	if err != nil {
		writeError(w, http.StatusBadRequest, "badRequest", 0)
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	id := f.newID("session")
	f.sessions[id] = &session{size: size, meta: meta, data: []byte{}}
	w.Header().Set("Location", scheme+"://"+r.Host+reqURL.EscapedPath()+"?uploadType=resumable&upload_id="+id)
	w.WriteHeader(http.StatusOK)
}

// putSession stores a chunk of a session (Content-Range: bytes a-b/size) or
// answers a status query (bytes */size): 308 with the Range of the bytes
// held, or 200 with the file once the session holds every byte.
func (f *Fake) putSession(w http.ResponseWriter, r *http.Request, body []byte) {
	s := f.sessions[r.URL.Query().Get("upload_id")]
	if s == nil {
		writeError(w, http.StatusNotFound, "notFound", 0)
		return
	}
	if s.file != "" {
		f.writeFile(w, http.StatusOK, s.file, "")
		return
	}
	spec, ok := strings.CutPrefix(r.Header.Get("Content-Range"), "bytes ")
	rng, total, found := strings.Cut(spec, "/")
	if !ok || !found || total != strconv.FormatInt(s.size, 10) {
		writeError(w, http.StatusBadRequest, "badContentRange", 0)
		return
	}
	if rng != "*" {
		if status, reason := f.storeChunk(s, rng, body); status != 0 {
			writeError(w, status, reason, 0)
			return
		}
	}
	if int64(len(s.data)) < s.size {
		if len(s.data) > 0 {
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(s.data)-1))
		}
		w.WriteHeader(http.StatusPermanentRedirect)
		return
	}
	if status, reason := f.claim(s.meta.ID); status != 0 {
		writeError(w, status, reason, 0)
		return
	}
	s.file = f.add(s.meta, s.data)
	f.writeFile(w, http.StatusOK, s.file, "")
}

// storeChunk stores body as the bytes rng ("a-b") of s, or returns the
// status and reason that refuse it: a chunk that starts past the bytes held,
// whose length is not the range's, that is not the last and not a multiple
// of 256 KiB, or that the daily cap refuses.
func (f *Fake) storeChunk(s *session, rng string, body []byte) (int, string) {
	first, last, ok := parseRange(rng)
	switch {
	case !ok || last >= s.size || first > int64(len(s.data)) || last-first+1 != int64(len(body)):
		return http.StatusBadRequest, "badContentRange"
	case last+1 < s.size && len(body)%chunkAlign != 0:
		return http.StatusBadRequest, "badChunkSize"
	}
	if status, reason := f.uploadRefused(0, int64(len(body))); status != 0 {
		return status, reason
	}
	s.data = append(s.data[:first], body...)
	return 0, ""
}

// create is a metadata-only create (a folder) or a multipart create of a
// small file.
func (f *Fake) create(w http.ResponseWriter, r *http.Request, body []byte) {
	var data []byte
	if r.URL.Query().Get("uploadType") == "multipart" {
		var err error
		if body, data, err = multipartParts(r.Header.Get("Content-Type"), body); err != nil {
			writeError(w, http.StatusBadRequest, "badMultipart", 0)
			return
		}
	}
	meta, status, reason := f.newFile(body)
	if status == 0 {
		status, reason = f.claim(meta.ID)
	}
	if status == 0 && data != nil {
		status, reason = f.uploadRefused(int64(len(data)), int64(len(data)))
	}
	if status != 0 {
		writeError(w, status, reason, 0)
		return
	}
	f.writeFile(w, http.StatusOK, f.add(meta, data), r.URL.Query().Get("fields"))
}

// update replaces the bytes of a small file with body.
func (f *Fake) update(w http.ResponseWriter, r *http.Request, id string, body []byte) {
	fl := f.files[id]
	switch {
	case fl == nil:
		writeError(w, http.StatusNotFound, "notFound", 0)
		return
	case fl.Folder:
		writeError(w, http.StatusBadRequest, "badRequest", 0)
		return
	}
	if status, reason := f.uploadRefused(int64(len(body)-len(fl.Data)), int64(len(body))); status != 0 {
		writeError(w, status, reason, 0)
		return
	}
	fl.Data, fl.MD5, fl.Modified = bytes.Clone(body), md5Hex(body), f.clock()
	f.writeFile(w, http.StatusOK, id, r.URL.Query().Get("fields"))
}

func (f *Fake) get(w http.ResponseWriter, r *http.Request, id string) {
	if f.files[id] == nil {
		writeError(w, http.StatusNotFound, "notFound", 0)
		return
	}
	f.writeFile(w, http.StatusOK, id, r.URL.Query().Get("fields"))
}

// media serves a file's bytes, or the Range of them that the request asks
// for (bytes=a-b or bytes=a-) with 206.
func (f *Fake) media(w http.ResponseWriter, r *http.Request, id string) {
	fl := f.files[id]
	switch {
	case fl == nil:
		writeError(w, http.StatusNotFound, "notFound", 0)
		return
	case fl.Folder:
		writeError(w, http.StatusForbidden, "fileNotDownloadable", 0)
		return
	}
	spec, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes=")
	if !ok {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fl.Data)
		return
	}
	size := int64(len(fl.Data))
	if strings.HasSuffix(spec, "-") && size > 0 {
		spec += strconv.FormatInt(size-1, 10)
	}
	first, last, ok := parseRange(spec)
	if !ok || first >= size {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		writeError(w, http.StatusRequestedRangeNotSatisfiable, "requestedRangeNotSatisfiable", 0)
		return
	}
	last = min(last, size-1)
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, size))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(fl.Data[first : last+1])
}

// delete deletes a file, or a folder and everything in it, for good.
func (f *Fake) delete(w http.ResponseWriter, id string) {
	if f.files[id] == nil {
		writeError(w, http.StatusNotFound, "notFound", 0)
		return
	}
	gone := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for fid, fl := range f.files {
			if !gone[fid] && gone[fl.Parent] {
				gone[fid], changed = true, true
			}
		}
	}
	for fid := range gone {
		delete(f.files, fid)
	}
	w.WriteHeader(http.StatusNoContent)
}

// generateIDs gives out count IDs (10 by default) for files in My Drive.
func (f *Fake) generateIDs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	count := 10
	if c := q.Get("count"); c != "" {
		n, err := strconv.Atoi(c)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "invalid", 0)
			return
		}
		count = n
	}
	if (q.Get("space") != "" && q.Get("space") != "drive") || (q.Get("type") != "" && q.Get("type") != "files") {
		writeError(w, http.StatusBadRequest, "invalid", 0)
		return
	}
	ids := make([]string, count)
	for i := range ids {
		ids[i] = f.newID("id")
		f.issued[ids[i]] = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "drive#generatedIds", "space": "drive", "ids": ids})
}

// list answers a q of the form "'<folder>' in parents and trashed = false"
// with the folder's children in creation order, pageSize (100 by default)
// at a time.
func (f *Fake) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	parent, ok := parseQuery(q.Get("q"))
	pageSize, from := 100, 0
	if s := q.Get("pageSize"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 1000 {
			ok = false
		}
		pageSize = n
	}
	if t := q.Get("pageToken"); t != "" {
		n, err := strconv.Atoi(t)
		if err != nil || n < 0 {
			ok = false
		}
		from = n
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "invalidQuery", 0)
		return
	}
	var children []*file
	for _, fl := range f.files {
		if fl.Parent == parent {
			children = append(children, fl)
		}
	}
	slices.SortFunc(children, func(a, b *file) int { return a.seq - b.seq })
	page := children[min(from, len(children)):min(from+pageSize, len(children))]
	resources := make([]map[string]any, len(page))
	for i, fl := range page {
		resources[i] = resource(fl, "id,name,mimeType,md5Checksum,size,modifiedTime,parents")
	}
	answer := map[string]any{"kind": "drive#fileList", "files": resources}
	if from+pageSize < len(children) {
		answer["nextPageToken"] = strconv.Itoa(from + pageSize)
	}
	writeJSON(w, http.StatusOK, answer)
}

// about answers the storage quota; Drive refuses about.get without fields.
func (f *Fake) about(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("fields") == "" {
		writeError(w, http.StatusBadRequest, "required", 0)
		return
	}
	usage := strconv.FormatInt(f.usage(), 10)
	quota := map[string]string{"usage": usage, "usageInDrive": usage, "usageInDriveTrash": "0"}
	if f.limit > 0 {
		quota["limit"] = strconv.FormatInt(f.limit, 10)
	}
	writeJSON(w, http.StatusOK, map[string]any{"storageQuota": quota})
}

// newFile reads a create's metadata, or returns the status and reason that
// refuse it: no name, more than one parent, or a parent that is not a
// folder of the fake. No parent is My Drive's root.
func (f *Fake) newFile(body []byte) (metadata, int, string) {
	var meta metadata
	if err := json.Unmarshal(body, &meta); err != nil || meta.Name == "" || len(meta.Parents) > 1 {
		return meta, http.StatusBadRequest, "badRequest"
	}
	if len(meta.Parents) == 0 {
		meta.Parents = []string{rootID}
	}
	if p := meta.Parents[0]; p != rootID && (f.files[p] == nil || !f.files[p].Folder) {
		return meta, http.StatusNotFound, "notFound"
	}
	return meta, 0, ""
}

// claim returns the status and reason that refuse a create with id: 409 for
// the ID of a file that exists, 400 for one that files.generateIds did not
// give out. An empty id gets a new one.
func (f *Fake) claim(id string) (int, string) {
	switch {
	case id == "":
		return 0, ""
	case f.files[id] != nil:
		return http.StatusConflict, "duplicate"
	case !f.issued[id]:
		return http.StatusBadRequest, "invalid"
	}
	return 0, ""
}

// uploadRefused returns the status and reason that refuse an upload that
// adds grow bytes to the storage and sends sent bytes: 403
// storageQuotaExceeded past the limit, 403 dailyLimitExceeded past the
// daily cap. It counts sent toward the cap when it accepts.
func (f *Fake) uploadRefused(grow, sent int64) (int, string) {
	if f.limit > 0 && f.usage()+grow > f.limit {
		return http.StatusForbidden, "storageQuotaExceeded"
	}
	if f.capBytes < 0 {
		return 0, ""
	}
	now := f.clock()
	if !f.capHit.IsZero() && !now.Before(f.capHit.Add(capPause)) {
		f.capHit, f.capSent = time.Time{}, 0
	}
	if f.capHit.IsZero() && f.capSent+sent > f.capBytes {
		f.capHit = now
	}
	if !f.capHit.IsZero() {
		return http.StatusForbidden, "dailyLimitExceeded"
	}
	f.capSent += sent
	return 0, ""
}

// usage is the bytes used: those set by SetQuota plus the files' bytes.
func (f *Fake) usage() int64 {
	n := f.used
	for _, fl := range f.files {
		n += int64(len(fl.Data))
	}
	return n
}

// add stores a new file of meta with data (a folder when meta's MIME type
// is one) and returns its ID.
func (f *Fake) add(meta metadata, data []byte) string {
	id := meta.ID
	if id == "" {
		id = f.newID("id")
	}
	f.seq++
	fl := &file{seq: f.seq, Object: Object{
		ID: id, Name: meta.Name, MimeType: meta.MimeType, Parent: meta.Parents[0],
		Folder: meta.MimeType == FolderMimeType, Modified: f.clock(),
	}}
	if !fl.Folder {
		fl.Data, fl.MD5 = bytes.Clone(data), md5Hex(data)
		if fl.MimeType == "" {
			fl.MimeType = "application/octet-stream"
		}
	}
	f.files[id] = fl
	return id
}

// newID returns a new opaque ID.
func (f *Fake) newID(kind string) string {
	f.seq++
	return fmt.Sprintf("fake-%s-%06d", kind, f.seq)
}

// writeFile answers the file id with the fields asked (Drive's default ones
// when fields is empty).
func (f *Fake) writeFile(w http.ResponseWriter, status int, id, fields string) {
	writeJSON(w, status, resource(f.files[id], fields))
}

// resource is fl's resource with fields, a comma-separated list of its
// top-level fields: kind, id, name and mimeType when fields is empty, as
// Drive answers. Sizes are strings, as Drive writes an int64.
func resource(fl *file, fields string) map[string]any {
	all := map[string]any{
		"kind": "drive#file", "id": fl.ID, "name": fl.Name, "mimeType": fl.MimeType,
		"parents": []string{fl.Parent}, "modifiedTime": fl.Modified.UTC().Format(time.RFC3339Nano),
	}
	if !fl.Folder {
		all["md5Checksum"], all["size"] = fl.MD5, strconv.Itoa(len(fl.Data))
	}
	want := []string{"kind", "id", "name", "mimeType"}
	if fields != "" {
		want = strings.Split(fields, ",")
	}
	out := map[string]any{}
	for _, k := range want {
		if v, ok := all[strings.TrimSpace(k)]; ok {
			out[strings.TrimSpace(k)] = v
		}
	}
	return out
}

// multipartParts splits a multipart/related body into its metadata and its
// media.
func multipartParts(contentType string, body []byte) (meta, data []byte, err error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/related" || params["boundary"] == "" {
		return nil, nil, errors.New("not multipart/related")
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var parts [][]byte
	for len(parts) < 3 {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		b, err := io.ReadAll(part)
		if err != nil {
			return nil, nil, err
		}
		parts = append(parts, b)
	}
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("%d parts, want 2", len(parts))
	}
	return parts[0], parts[1], nil
}

// parseRange parses "a-b" with 0 ≤ a ≤ b.
func parseRange(s string) (first, last int64, ok bool) {
	a, b, found := strings.Cut(s, "-")
	first, err1 := strconv.ParseInt(a, 10, 64)
	last, err2 := strconv.ParseInt(b, 10, 64)
	return first, last, found && err1 == nil && err2 == nil && first >= 0 && last >= first
}

// parseQuery returns the folder of "'<folder>' in parents and trashed =
// false", the one q that the fake serves.
func parseQuery(q string) (parent string, ok bool) {
	in, trashed, found := strings.Cut(q, " and ")
	parent, isIn := strings.CutSuffix(strings.TrimSpace(in), "' in parents")
	parent, quoted := strings.CutPrefix(parent, "'")
	return parent, found && isIn && quoted && parent != "" && strings.TrimSpace(trashed) == "trashed = false"
}

func md5Hex(b []byte) string {
	sum := md5.Sum(b) //nolint:gosec // G401: Drive's md5Checksum, not a security check
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers Drive's error body for status and reason, with
// Retry-After in whole seconds when retryAfter is positive.
func writeError(w http.ResponseWriter, status int, reason string, retryAfter time.Duration) {
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(int64((retryAfter+time.Second-1)/time.Second), 10))
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code": status, "message": reason,
		"errors": []map[string]string{{"domain": "global", "reason": reason, "message": reason}},
	}})
}

// reset drops the request's connection with a TCP reset, or aborts the
// response where the connection cannot be taken over.
func reset(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic(http.ErrAbortHandler)
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
}
