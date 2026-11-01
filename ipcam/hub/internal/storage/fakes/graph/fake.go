// Package fakegraph is a fake of the Microsoft Graph OneDrive API for the
// hub's tests (P04): it serves over HTTP the requests that the onedrive
// driver makes, with OneDrive personal's semantics, so that the driver's
// tests need no network and no account. Items live by path under the app
// folder (/special/approot), whose missing parents an upload creates. Upload
// sessions are strict (§9.2): fragments in order, each but the last a
// multiple of 320 KiB, each under 60 MiB, and a 401 for any request to an
// uploadUrl that carries Authorization; a completed, cancelled or expired
// session's uploadUrl answers 404, as Graph's does. A session expires, with
// the bytes it held, a day after its last request. conflictBehavior fail,
// replace and rename are honoured. A deleted item goes to a recycle bin that
// keeps occupying the quota (quota.deleted) until EmptyBin. Every item and
// drive answer follows $select, and refuses a property that the fake does
// not know. Content downloads redirect to a pre-authenticated URL. Tests
// inject answers (with Retry-After) with FailNext, a quota with SetQuota and
// a token check with AcceptToken. It is permanent test infrastructure, as
// the drivers are (P04).
package fakegraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reset is the status of FailNext that drops the connection without an
// answer, as a route that went down does.
const Reset = -1

const (
	// fragmentAlign is the multiple that every fragment but the last must be.
	fragmentAlign = 320 << 10
	// maxFragment is the largest fragment: Graph takes less than 60 MiB in
	// one request.
	maxFragment = 60<<20 - 1
	// maxBody bounds the body of a request that is not a fragment.
	maxBody = 1 << 20
	// sessionLife is how long a session lasts after its last request, as the
	// expirationDateTime that the fake answers says; the fake then drops it.
	sessionLife = 24 * time.Hour
	// defaultTotal is a new fake's quota: a free OneDrive's 5 GiB.
	defaultTotal = 5 << 30
	// rootPath is the prefix of the API's paths.
	rootPath = "/v1.0/me/drive"
	// approot addresses an item by its path under the app folder.
	approot = rootPath + "/special/approot:/"
)

// The API methods, by the names of the driver's errors and of Counts.
const (
	methodCreateSession = "createUploadSession"
	methodUploadPut     = "upload.put"    // a fragment PUT to an uploadUrl
	methodUploadStatus  = "upload.status" // a GET of an uploadUrl
	methodUploadCancel  = "upload.cancel" // a DELETE of an uploadUrl
	methodGetByPath     = "items.getByPath"
	methodGet           = "items.get"
	methodContent       = "items.content"
	methodDelete        = "items.delete"
	methodDrive         = "drive.get"
	methodDownload      = "download" // a GET of a pre-authenticated download URL
	methodUnknown       = "unknown"  // a request that the fake does not serve
)

// Options configure a Fake.
type Options struct {
	// Clock is the time of lastModifiedDateTime and of expirationDateTime;
	// time.Now by default.
	Clock func() time.Time
}

// Object is an item that the fake holds, live or in the recycle bin.
type Object struct {
	ID       string
	Path     string // under the app folder, with "/"
	Name     string
	Folder   bool
	Data     []byte
	QuickXor string // file.hashes.quickXorHash; "" for a folder
	Modified time.Time
}

// Fake is the fake Graph. Its methods are safe for concurrent use.
type Fake struct {
	clock func() time.Time

	mu       sync.Mutex
	seq      int
	items    map[string]*item // the live items, by ID
	paths    map[string]string
	bin      []*item // the deleted items, oldest first
	sessions map[string]*session
	counts   map[string]int
	fails    map[string][]failure
	total    int64 // the quota
	used     int64 // the bytes used besides the items
	accept   func(token string) bool
}

type item struct {
	Object
	seq int // creation order
}

// session is an upload session of an item at path.
type session struct {
	path     string
	conflict string    // fail, replace or rename
	size     int64     // the total that the first fragment named; -1 before it
	data     []byte    // the bytes received
	last     time.Time // the session's last request, from which it expires
}

// failure is an answer that FailNext injected.
type failure struct {
	status     int
	code       string
	retryAfter time.Duration
}

// New returns an empty fake with a 5 GiB quota and a token check that
// accepts any bearer token.
func New(opts Options) *Fake {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &Fake{
		clock:    opts.Clock,
		items:    map[string]*item{},
		paths:    map[string]string{},
		sessions: map[string]*session{},
		counts:   map[string]int{},
		fails:    map[string][]failure{},
		total:    defaultTotal,
	}
}

// Handler serves the API under /v1.0/me/drive, the uploadUrls under /up/ and
// the download URLs under /download/, which may sit under a prefix that a
// mux strips: the URLs that the fake answers keep it.
func (f *Fake) Handler() http.Handler { return http.HandlerFunc(f.serve) }

// Counts returns how many requests of each API method arrived, by the names
// of the driver's errors, such as "createUploadSession" or "upload.put";
// "unknown" counts the requests that the fake does not serve.
func (f *Fake) Counts() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := make(map[string]int, len(f.counts))
	for k, v := range f.counts {
		counts[k] = v
	}
	return counts
}

// Objects returns the live items, files and folders, in creation order.
func (f *Fake) Objects() []Object {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := make([]*item, 0, len(f.items))
	for _, it := range f.items {
		all = append(all, it)
	}
	slices.SortFunc(all, func(a, b *item) int { return a.seq - b.seq })
	return objects(all)
}

// Bin returns the items in the recycle bin, in the order they were deleted.
func (f *Fake) Bin() []Object {
	f.mu.Lock()
	defer f.mu.Unlock()
	return objects(f.bin)
}

func objects(items []*item) []Object {
	out := make([]Object, len(items))
	for i, it := range items {
		out[i] = it.Object
		out[i].Data = bytes.Clone(it.Data)
	}
	return out
}

// EmptyBin empties the recycle bin, which frees the quota its items held.
func (f *Fake) EmptyBin() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bin = nil
}

// PendingBytes returns the bytes that the open upload sessions hold.
func (f *Fake) PendingBytes() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expire()
	var n int64
	for _, s := range f.sessions {
		n += int64(len(s.data))
	}
	return n
}

// SetQuota sets the quota to total bytes and the bytes used besides the
// fake's items to used.
func (f *Fake) SetQuota(total, used int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.total, f.used = total, used
}

// FailNext makes the next request of method answer status with Graph's
// error body naming code, and a Retry-After header of retryAfter when it is
// positive. A status below 400 answers an empty object; Reset drops the
// connection. Several calls queue in order.
func (f *Fake) FailNext(method string, status int, code string, retryAfter time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails[method] = append(f.fails[method], failure{status: status, code: code, retryAfter: retryAfter})
}

// AcceptToken sets the check of the bearer token of each request to the API
// (not to an uploadUrl or a download URL): a request whose token it refuses,
// or with none, answers 401. nil accepts any token. The check runs with the
// fake unlocked, so it may call the fake's methods.
func (f *Fake) AcceptToken(accept func(token string) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accept = accept
}

// route names the API method of r and the path or ID it names, or returns
// methodUnknown.
func route(r *http.Request) (method, name string) {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, approot):
		rest := strings.TrimPrefix(p, approot)
		if key, ok := strings.CutSuffix(rest, ":/createUploadSession"); ok && r.Method == http.MethodPost {
			return methodCreateSession, key
		}
		if r.Method == http.MethodGet && !strings.Contains(strings.TrimSuffix(rest, ":"), ":") {
			return methodGetByPath, strings.TrimSuffix(rest, ":")
		}
	case strings.HasPrefix(p, rootPath+"/items/"):
		rest := strings.TrimPrefix(p, rootPath+"/items/")
		if id, ok := strings.CutSuffix(rest, "/content"); ok && r.Method == http.MethodGet && id != "" && !strings.Contains(id, "/") {
			return methodContent, id
		}
		if rest != "" && !strings.Contains(rest, "/") {
			switch r.Method {
			case http.MethodGet:
				return methodGet, rest
			case http.MethodDelete:
				return methodDelete, rest
			}
		}
	case p == rootPath && r.Method == http.MethodGet:
		return methodDrive, ""
	case strings.HasPrefix(p, "/up/"):
		id := strings.TrimPrefix(p, "/up/")
		switch r.Method {
		case http.MethodPut:
			return methodUploadPut, id
		case http.MethodGet:
			return methodUploadStatus, id
		case http.MethodDelete:
			return methodUploadCancel, id
		}
	case strings.HasPrefix(p, "/download/") && r.Method == http.MethodGet:
		return methodDownload, strings.TrimPrefix(p, "/download/")
	}
	return methodUnknown, ""
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	method, name := route(r)
	fail, failed, authorized := f.arrive(method, r)
	preauthenticated := method == methodUploadPut || method == methodUploadStatus || method == methodUploadCancel
	switch {
	case method == methodUnknown:
		writeError(w, http.StatusBadRequest, "invalidRequest", 0)
		return
	case failed && fail.status == Reset:
		reset(w)
		return
	case failed && fail.status < http.StatusBadRequest:
		writeJSON(w, fail.status, map[string]any{})
		return
	case failed:
		writeError(w, fail.status, fail.code, fail.retryAfter)
		return
	case preauthenticated && r.Header.Get("Authorization") != "":
		// An uploadUrl is pre-authenticated: Graph may answer 401 to a
		// request that carries a token anyway (STO-08).
		writeError(w, http.StatusUnauthorized, "unauthenticated", 0)
		return
	case !preauthenticated && method != methodDownload && !authorized:
		writeError(w, http.StatusUnauthorized, "unauthenticated", 0)
		return
	}
	limit := int64(maxBody)
	if method == methodUploadPut {
		limit = maxFragment
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return // the client went away
	}
	if int64(len(body)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "requestTooLarge", 0)
		return
	}
	base := baseURL(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expire()
	switch method {
	case methodCreateSession:
		f.createSession(w, base, name, body)
	case methodUploadPut:
		f.putFragment(w, r, name, body)
	case methodUploadStatus:
		f.status(w, name)
	case methodUploadCancel:
		f.cancel(w, name)
	case methodGetByPath:
		f.writeItem(w, r, f.paths[name], base)
	case methodGet:
		f.writeItem(w, r, name, base)
	case methodContent:
		f.content(w, name, base)
	case methodDownload:
		f.download(w, r, name)
	case methodDelete:
		f.delete(w, name)
	case methodDrive:
		f.drive(w, r)
	}
}

// arrive counts r as a request of method and returns the failure that
// FailNext queued for it, if any, and whether its bearer token is accepted.
// AcceptToken's check runs with the fake unlocked.
func (f *Fake) arrive(method string, r *http.Request) (fail failure, failed, authorized bool) {
	f.mu.Lock()
	f.counts[method]++
	if queued := f.fails[method]; len(queued) > 0 {
		fail, f.fails[method] = queued[0], queued[1:]
		f.mu.Unlock()
		return fail, true, false
	}
	accept := f.accept
	f.mu.Unlock()
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	authorized = ok && token != "" && (accept == nil || accept(token))
	return failure{}, false, authorized
}

// baseURL is the scheme, host and mux prefix that r came to, which the URLs
// that the fake answers keep.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	prefix := ""
	if full, err := url.ParseRequestURI(r.RequestURI); err == nil {
		prefix = strings.TrimSuffix(full.EscapedPath(), r.URL.EscapedPath())
	}
	return scheme + "://" + r.Host + prefix
}

// createSession opens an upload session of the item at key, whose body names
// its conflictBehavior (Graph's default is fail) and, if it likes, its name.
func (f *Fake) createSession(w http.ResponseWriter, base, key string, body []byte) {
	var req struct {
		Item struct {
			Conflict string `json:"@microsoft.graph.conflictBehavior"`
			Name     string `json:"name"`
		} `json:"item"`
		DeferCommit bool `json:"deferCommit"`
	}
	if !validPath(key) || json.Unmarshal(body, &req) != nil || req.DeferCommit ||
		(req.Item.Name != "" && req.Item.Name != path.Base(key)) {
		writeError(w, http.StatusBadRequest, "invalidRequest", 0)
		return
	}
	conflict := req.Item.Conflict
	switch conflict {
	case "":
		conflict = "fail"
	case "fail", "replace", "rename":
	default:
		writeError(w, http.StatusBadRequest, "invalidRequest", 0)
		return
	}
	id := f.newID("session")
	f.sessions[id] = &session{path: key, conflict: conflict, size: -1, last: f.clock()}
	writeJSON(w, http.StatusOK, map[string]any{
		"uploadUrl": base + "/up/" + id, "expirationDateTime": f.expiration(), "nextExpectedRanges": []string{"0-"},
	})
}

// putFragment stores a fragment (Content-Range: bytes a-b/total) of a
// session: in order, of the total that the first fragment named, each but
// the last a multiple of 320 KiB. The session answers 202 with the next
// expected range, or, after the last byte, the item: 201 for a new one, 200
// for one that it replaced. A completed session is gone.
func (f *Fake) putFragment(w http.ResponseWriter, r *http.Request, id string, body []byte) {
	s := f.sessions[id]
	if s == nil {
		writeError(w, http.StatusNotFound, "itemNotFound", 0)
		return
	}
	s.last = f.clock()
	first, last, total, ok := parseContentRange(r.Header.Get("Content-Range"))
	held := int64(len(s.data))
	switch {
	case !ok || total <= 0 || last >= total || last-first+1 != int64(len(body)) || (s.size >= 0 && total != s.size):
		writeError(w, http.StatusBadRequest, "invalidRange", 0)
		return
	case first < held: // a fragment that the session holds already
		writeError(w, http.StatusRequestedRangeNotSatisfiable, "invalidRange", 0)
		return
	case first > held: // out of order
		writeError(w, http.StatusBadRequest, "invalidRange", 0)
		return
	case last+1 < total && len(body)%fragmentAlign != 0:
		writeError(w, http.StatusBadRequest, "invalidRange", 0)
		return
	case f.usage()+held+int64(len(body)) > f.total:
		writeError(w, http.StatusInsufficientStorage, "quotaLimitReached", 0)
		return
	}
	s.size = total
	s.data = append(s.data, body...)
	if int64(len(s.data)) < total {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"expirationDateTime": f.expiration(), "nextExpectedRanges": []string{fmt.Sprintf("%d-", len(s.data))},
		})
		return
	}
	itemID, status, code := f.complete(s)
	if status >= http.StatusBadRequest {
		writeError(w, status, code, 0) // the session stays, as Graph keeps it until it expires
		return
	}
	delete(f.sessions, id)
	writeJSON(w, status, f.resource(f.items[itemID], "id,name,size,file,lastModifiedDateTime,parentReference,eTag", ""))
}

// complete makes the item of a session that holds every byte, after its
// conflictBehavior, with the parent folders that are missing, and returns
// its ID and the status to answer: 201 for a new item, 200 for a replaced
// one; 409 nameAlreadyExists for fail when an item is at the path, or for a
// path whose parent is a file.
func (f *Fake) complete(s *session) (string, int, string) {
	key := s.path
	if id, taken := f.paths[key]; taken {
		switch existing := f.items[id]; {
		case s.conflict == "replace" && !existing.Folder:
			existing.Data, existing.QuickXor, existing.Modified = s.data, quickXorHash(s.data), f.clock()
			return id, http.StatusOK, ""
		case s.conflict == "rename":
			key = f.freeName(key)
		default:
			return "", http.StatusConflict, "nameAlreadyExists"
		}
	}
	for dir := path.Dir(key); dir != "."; dir = path.Dir(dir) {
		if id, ok := f.paths[dir]; ok && !f.items[id].Folder {
			return "", http.StatusConflict, "nameAlreadyExists"
		}
	}
	f.makeParents(key)
	id := f.add(Object{Path: key, Name: path.Base(key), Data: s.data, QuickXor: quickXorHash(s.data)})
	return id, http.StatusCreated, ""
}

// freeName is key with " 1", " 2", … before its extension, the first that no
// item takes, as rename does.
func (f *Fake) freeName(key string) string {
	ext := path.Ext(key)
	stem := strings.TrimSuffix(key, ext)
	for n := 1; ; n++ {
		if p := fmt.Sprintf("%s %d%s", stem, n, ext); f.paths[p] == "" {
			return p
		}
	}
}

// makeParents creates the folders above key that do not exist.
func (f *Fake) makeParents(key string) {
	var missing []string
	for dir := path.Dir(key); dir != "."; dir = path.Dir(dir) {
		if f.paths[dir] == "" {
			missing = append(missing, dir)
		}
	}
	for _, dir := range slices.Backward(missing) {
		f.add(Object{Path: dir, Name: path.Base(dir), Folder: true})
	}
}

// add stores a new live item and returns its ID.
func (f *Fake) add(o Object) string {
	o.ID, o.Modified = f.newID("item"), f.clock()
	f.seq++
	f.items[o.ID] = &item{Object: o, seq: f.seq}
	f.paths[o.Path] = o.ID
	return o.ID
}

// status answers a GET of an uploadUrl: the next expected range.
func (f *Fake) status(w http.ResponseWriter, id string) {
	s := f.sessions[id]
	if s == nil {
		writeError(w, http.StatusNotFound, "itemNotFound", 0)
		return
	}
	s.last = f.clock()
	writeJSON(w, http.StatusOK, map[string]any{
		"expirationDateTime": f.expiration(), "nextExpectedRanges": []string{fmt.Sprintf("%d-", len(s.data))},
	})
}

// cancel deletes a session and the bytes that it holds.
func (f *Fake) cancel(w http.ResponseWriter, id string) {
	if f.sessions[id] == nil {
		writeError(w, http.StatusNotFound, "itemNotFound", 0)
		return
	}
	delete(f.sessions, id)
	w.WriteHeader(http.StatusNoContent)
}

// writeItem answers the live item id with the properties of $select.
func (f *Fake) writeItem(w http.ResponseWriter, r *http.Request, id, base string) {
	it := f.items[id]
	if it == nil {
		writeError(w, http.StatusNotFound, "itemNotFound", 0)
		return
	}
	sel, ok := selected(r, itemProperties)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalidRequest", 0)
		return
	}
	writeJSON(w, http.StatusOK, f.resource(it, sel, base))
}

// content redirects to the pre-authenticated download URL of a file, as
// Graph answers /content with 302.
func (f *Fake) content(w http.ResponseWriter, id, base string) {
	it := f.items[id]
	switch {
	case it == nil:
		writeError(w, http.StatusNotFound, "itemNotFound", 0)
	case it.Folder:
		writeError(w, http.StatusBadRequest, "invalidRequest", 0)
	default:
		w.Header().Set("Location", downloadURL(base, id))
		w.WriteHeader(http.StatusFound)
	}
}

// downloadURL is the pre-authenticated download URL of the item id.
func downloadURL(base, id string) string { return base + "/download/" + url.PathEscape(id) }

// download serves a live file's bytes, or the Range of them that the request
// asks for (bytes=a-b or bytes=a-) with 206. It needs no token.
func (f *Fake) download(w http.ResponseWriter, r *http.Request, id string) {
	it := f.items[id]
	if it == nil || it.Folder {
		writeError(w, http.StatusNotFound, "itemNotFound", 0)
		return
	}
	spec, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes=")
	if !ok {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(it.Data)
		return
	}
	size := int64(len(it.Data))
	a, b, found := strings.Cut(spec, "-")
	first, err1 := strconv.ParseInt(a, 10, 64)
	last, err2 := strconv.ParseInt(b, 10, 64)
	if b == "" {
		last, err2 = size-1, nil
	}
	if !found || err1 != nil || err2 != nil || first < 0 || last < first || first >= size {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		writeError(w, http.StatusRequestedRangeNotSatisfiable, "invalidRange", 0)
		return
	}
	last = min(last, size-1)
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, size))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(it.Data[first : last+1])
}

// delete moves an item, and a folder's items, to the recycle bin, where they
// keep occupying the quota.
func (f *Fake) delete(w http.ResponseWriter, id string) {
	it := f.items[id]
	if it == nil {
		writeError(w, http.StatusNotFound, "itemNotFound", 0)
		return
	}
	var gone []*item
	for _, other := range f.items {
		if other == it || strings.HasPrefix(other.Path, it.Path+"/") {
			gone = append(gone, other)
		}
	}
	slices.SortFunc(gone, func(a, b *item) int { return a.seq - b.seq })
	for _, g := range gone {
		delete(f.items, g.ID)
		delete(f.paths, g.Path)
	}
	f.bin = append(f.bin, gone...)
	w.WriteHeader(http.StatusNoContent)
}

// drive answers the drive's resource with the properties of $select. Its
// default set, as Graph's, names the account's owner, which a driver must
// not ask for (P45).
func (f *Fake) drive(w http.ResponseWriter, r *http.Request) {
	sel, ok := selected(r, driveProperties)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalidRequest", 0)
		return
	}
	inBin := bytesOf(f.bin)
	used := f.usage()
	state := "normal"
	switch remaining := f.total - used; {
	case remaining <= 0:
		state = "exceeded"
	case remaining < f.total/100:
		state = "critical"
	case remaining < f.total/10:
		state = "nearing"
	}
	all := map[string]any{
		"id": "fake-drive", "driveType": "personal",
		"owner": map[string]any{"user": map[string]any{"id": "fake-user", "displayName": "acct-1"}},
		"quota": map[string]any{"total": f.total, "used": used, "remaining": max(f.total-used, 0), "deleted": inBin, "state": state},
	}
	writeJSON(w, http.StatusOK, pick(all, sel))
}

// The properties that $select may name, of an item and of the drive.
var (
	itemProperties  = []string{"id", "name", "size", "file", "folder", "lastModifiedDateTime", "parentReference", "eTag", "@microsoft.graph.downloadUrl"}
	driveProperties = []string{"id", "driveType", "owner", "quota"}
)

// selected returns r's $select, or "" when it has none, and whether every
// property it names is one of known.
func selected(r *http.Request, known []string) (string, bool) {
	sel := r.URL.Query().Get("$select")
	if sel == "" {
		return "", true
	}
	for _, p := range strings.Split(sel, ",") {
		if !slices.Contains(known, strings.TrimSpace(p)) {
			return "", false
		}
	}
	return sel, true
}

// resource is it's driveItem with the properties of sel (every property
// when sel is empty, as Graph's default set); a file's
// @microsoft.graph.downloadUrl is under base.
func (f *Fake) resource(it *item, sel, base string) map[string]any {
	parent := path.Dir(it.Path)
	if parent == "." {
		parent = ""
	}
	all := map[string]any{
		"id": it.ID, "name": it.Name, "lastModifiedDateTime": it.Modified.UTC().Format(time.RFC3339Nano),
		"eTag": fmt.Sprintf("%s.%d", it.ID, it.Modified.UnixNano()),
		"parentReference": map[string]any{
			"driveType": "personal", "path": strings.TrimSuffix("/drive/special/approot:/"+parent, "/"),
		},
	}
	if it.Folder {
		all["folder"] = map[string]any{"childCount": f.children(it.Path)}
		all["size"] = 0
	} else {
		all["size"] = len(it.Data)
		all["file"] = map[string]any{"mimeType": "application/octet-stream", "hashes": map[string]any{"quickXorHash": it.QuickXor}}
		all["@microsoft.graph.downloadUrl"] = downloadURL(base, it.ID)
	}
	return pick(all, sel)
}

// children counts the live items right in the folder at dir.
func (f *Fake) children(dir string) int {
	n := 0
	for _, it := range f.items {
		if path.Dir(it.Path) == dir {
			n++
		}
	}
	return n
}

// pick keeps the properties of all that sel names (all of them when sel is
// empty); a property that the resource does not have is left out, as Graph
// leaves out a null facet.
func pick(all map[string]any, sel string) map[string]any {
	if sel == "" {
		return all
	}
	out := map[string]any{}
	for _, p := range strings.Split(sel, ",") {
		if v, ok := all[strings.TrimSpace(p)]; ok {
			out[strings.TrimSpace(p)] = v
		}
	}
	return out
}

// usage is the bytes used: those set by SetQuota, the live files' and the
// recycle bin's.
func (f *Fake) usage() int64 {
	n := f.used + bytesOf(f.bin)
	for _, it := range f.items {
		n += int64(len(it.Data))
	}
	return n
}

func bytesOf(items []*item) int64 {
	var n int64
	for _, it := range items {
		n += int64(len(it.Data))
	}
	return n
}

// expire drops the sessions whose last request is sessionLife ago or more,
// with the bytes they held: their uploadUrls answer 404 from then on.
func (f *Fake) expire() {
	now := f.clock()
	for id, s := range f.sessions {
		if !now.Before(s.last.Add(sessionLife)) {
			delete(f.sessions, id)
		}
	}
}

// expiration is the expirationDateTime that a session's answers carry: a
// session's request answers it, so it is sessionLife after that request.
func (f *Fake) expiration() string {
	return f.clock().Add(sessionLife).UTC().Format(time.RFC3339Nano)
}

// newID returns a new opaque ID.
func (f *Fake) newID(kind string) string {
	f.seq++
	return fmt.Sprintf("fake-%s-%06d", kind, f.seq)
}

// validPath reports whether key is a path that OneDrive takes: names that
// are not empty, "." or "..", without the characters that OneDrive refuses.
func validPath(key string) bool {
	if key == "" {
		return false
	}
	for _, name := range strings.Split(key, "/") {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `"*:<>?\|`) {
			return false
		}
	}
	return true
}

// parseContentRange parses "bytes a-b/total" with 0 ≤ a ≤ b.
func parseContentRange(s string) (first, last, total int64, ok bool) {
	spec, ok1 := strings.CutPrefix(s, "bytes ")
	rng, size, ok2 := strings.Cut(spec, "/")
	a, b, ok3 := strings.Cut(rng, "-")
	first, err1 := strconv.ParseInt(a, 10, 64)
	last, err2 := strconv.ParseInt(b, 10, 64)
	total, err3 := strconv.ParseInt(size, 10, 64)
	return first, last, total, ok1 && ok2 && ok3 && err1 == nil && err2 == nil && err3 == nil && first >= 0 && last >= first
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers Graph's error body for status and code, with
// Retry-After in whole seconds when retryAfter is positive.
func writeError(w http.ResponseWriter, status int, code string, retryAfter time.Duration) {
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(int64((retryAfter+time.Second-1)/time.Second), 10))
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code": code, "message": code, "innerError": map[string]any{"code": code},
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
