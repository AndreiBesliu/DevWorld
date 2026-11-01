package fakegdrive

import (
	"bytes"
	"crypto/md5" //nolint:gosec // G501: Drive's md5Checksum, which the fake reproduces
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const chunk = 256 << 10 // Drive's chunk alignment

// testClock is a clock that moves only when a test sets it.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// client sends requests to a fake behind an httptest server, with a bearer
// token.
type client struct {
	t     *testing.T
	f     *Fake
	srv   *httptest.Server
	clock *testClock
	token string
}

func newClient(t *testing.T) *client {
	t.Helper()
	clock := newTestClock()
	f := New(Options{Clock: clock.Now})
	srv := httptest.NewServer(guard(t, f.Handler()))
	t.Cleanup(srv.Close)
	return &client{t: t, f: f, srv: srv, clock: clock, token: "tok-1"}
}

// guard serves h and fails t when h panics: the server alone would only
// log the panic and drop the connection, which a client sees as a reset.
func guard(t *testing.T, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				if p != http.ErrAbortHandler {
					t.Errorf("the fake panicked serving %s %s: %v", r.Method, r.URL.Path, p)
				}
				panic(http.ErrAbortHandler)
			}
		}()
		h.ServeHTTP(w, r)
	})
}

// do sends a request to url (the server's URL is added to a path) and
// returns the answer with its body read.
func (c *client) do(method, url string, header map[string]string, body []byte) (*http.Response, []byte) {
	c.t.Helper()
	if strings.HasPrefix(url, "/") {
		url = c.srv.URL + url
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.srv.Client().Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp, data
}

// want fails the test unless resp has status.
func (c *client) want(resp *http.Response, body []byte, status int) {
	c.t.Helper()
	if resp.StatusCode != status {
		c.t.Fatalf("%s %s: HTTP %d %s, want %d", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, body, status)
	}
}

// generateID returns one ID from files.generateIds.
func (c *client) generateID() string {
	c.t.Helper()
	resp, body := c.do(http.MethodGet, "/drive/v3/files/generateIds?count=1&space=drive&type=files", nil, nil)
	c.want(resp, body, http.StatusOK)
	var ids struct{ IDs []string }
	if err := json.Unmarshal(body, &ids); err != nil || len(ids.IDs) != 1 {
		c.t.Fatalf("generateIds answered %s (%v), want one ID", body, err)
	}
	return ids.IDs[0]
}

// folder creates a folder named name in parent with id and returns the
// answer.
func (c *client) folder(id, name, parent string) (*http.Response, []byte) {
	c.t.Helper()
	meta, _ := json.Marshal(map[string]any{"id": id, "name": name, "mimeType": FolderMimeType, "parents": []string{parent}})
	return c.do(http.MethodPost, "/drive/v3/files?fields=id", map[string]string{"Content-Type": "application/json"}, meta)
}

// session opens a resumable upload of size bytes named name in parent and
// returns its session URI.
func (c *client) session(name, parent, id string, size int) string {
	c.t.Helper()
	meta, _ := json.Marshal(map[string]any{"id": id, "name": name, "parents": []string{parent}})
	resp, body := c.do(http.MethodPost, "/upload/drive/v3/files?uploadType=resumable",
		map[string]string{"Content-Type": "application/json", "X-Upload-Content-Length": strconv.Itoa(size)}, meta)
	c.want(resp, body, http.StatusOK)
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, c.srv.URL+"/upload/drive/v3/files?") || !strings.Contains(loc, "upload_id=") {
		c.t.Fatalf("Location %q, want a session URI under %s", loc, c.srv.URL)
	}
	return loc
}

// put sends data[from:to] of a total-byte upload to the session.
func (c *client) put(session string, data []byte, from, to int) (*http.Response, []byte) {
	c.t.Helper()
	return c.do(http.MethodPut, session,
		map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", from, to-1, len(data))}, data[from:to])
}

// status asks the session how many bytes it holds.
func (c *client) status(session string, size int) (*http.Response, []byte) {
	c.t.Helper()
	return c.do(http.MethodPut, session, map[string]string{"Content-Range": fmt.Sprintf("bytes */%d", size)}, nil)
}

func pattern(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7) + seed
	}
	return b
}

func wantMD5(b []byte) string {
	sum := md5.Sum(b) //nolint:gosec // G401: Drive's md5Checksum
	return hex.EncodeToString(sum[:])
}

// TestFake_ResumableUpload: a session answers 308 with the bytes it holds
// until the last chunk, which creates the file with the pre-generated ID; a
// status query then answers 200 with the file, whose md5Checksum and size
// are the bytes'. Every request is counted by method.
func TestFake_ResumableUpload(t *testing.T) {
	c := newClient(t)
	data := pattern(2*chunk+100, 1)
	id := c.generateID()
	session := c.session("a.mp4", "root", id, len(data))

	resp, body := c.put(session, data, 0, chunk)
	c.want(resp, body, http.StatusPermanentRedirect)
	if got := resp.Header.Get("Range"); got != fmt.Sprintf("bytes=0-%d", chunk-1) {
		t.Errorf("Range after the first chunk %q, want bytes=0-%d", got, chunk-1)
	}
	resp, body = c.status(session, len(data))
	c.want(resp, body, http.StatusPermanentRedirect)
	if got := resp.Header.Get("Range"); got != fmt.Sprintf("bytes=0-%d", chunk-1) {
		t.Errorf("Range of the status query %q, want bytes=0-%d", got, chunk-1)
	}
	resp, body = c.put(session, data, chunk, len(data))
	c.want(resp, body, http.StatusOK)
	var file struct{ ID string }
	if err := json.Unmarshal(body, &file); err != nil || file.ID != id {
		t.Fatalf("the last chunk answered %s, want the file %s", body, id)
	}
	resp, body = c.status(session, len(data))
	c.want(resp, body, http.StatusOK)
	if !strings.Contains(string(body), id) {
		t.Errorf("the status query of a complete session answered %s, want the file %s", body, id)
	}

	resp, body = c.do(http.MethodGet, "/drive/v3/files/"+id+"?fields=id,md5Checksum,size", nil, nil)
	c.want(resp, body, http.StatusOK)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"id": id, "md5Checksum": wantMD5(data), "size": strconv.Itoa(len(data))}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("files.get answered %v, want only the asked fields %v", got, want)
	}
	objects := c.f.Objects()
	if len(objects) != 1 || objects[0].ID != id || objects[0].Parent != "root" || !bytes.Equal(objects[0].Data, data) || objects[0].MD5 != wantMD5(data) {
		t.Errorf("Objects() = %+v, want the one file", objects)
	}
	counts := c.f.Counts()
	wantCounts := map[string]int{"files.generateIds": 1, "files.create.resumable": 1, "upload.put": 2, "upload.status": 2, "files.get": 1}
	if fmt.Sprint(counts) != fmt.Sprint(wantCounts) {
		t.Errorf("Counts() = %v, want %v", counts, wantCounts)
	}
}

// TestFake_CompleteSessionOutlivesItsFile: a session that holds every byte
// answers a status query, and its last chunk sent again, with the answer
// that its last chunk had, after the file that it made was deleted too, as
// Google's upload server keeps a finished upload's answer. The outbox's P34
// mismatch path asks so: it deletes the copy, then aborts the session. The
// fake makes no second file.
func TestFake_CompleteSessionOutlivesItsFile(t *testing.T) {
	c := newClient(t)
	data := pattern(chunk+100, 3)
	id := c.generateID()
	session := c.session("a.mp4", "root", id, len(data))
	resp, body := c.put(session, data, 0, chunk)
	c.want(resp, body, http.StatusPermanentRedirect)
	resp, body = c.put(session, data, chunk, len(data))
	c.want(resp, body, http.StatusOK)
	made := string(body)
	if !strings.Contains(made, id) {
		t.Fatalf("the last chunk answered %s, want the file %s", made, id)
	}
	resp, body = c.do(http.MethodDelete, "/drive/v3/files/"+id, nil, nil)
	c.want(resp, body, http.StatusNoContent)

	resp, body = c.status(session, len(data))
	c.want(resp, body, http.StatusOK)
	if string(body) != made {
		t.Errorf("the status query after the delete answered %s, want the last chunk's answer %s", body, made)
	}
	resp, body = c.put(session, data, chunk, len(data))
	c.want(resp, body, http.StatusOK)
	if string(body) != made {
		t.Errorf("the last chunk sent again after the delete answered %s, want %s", body, made)
	}
	if objects := c.f.Objects(); len(objects) != 0 {
		t.Errorf("Objects() = %d objects after the delete, want none", len(objects))
	}
	resp, body = c.do(http.MethodGet, "/drive/v3/files/"+id+"?fields=id", nil, nil)
	c.want(resp, body, http.StatusNotFound)
}

// TestFake_ChunkRules: a chunk other than the last must be a multiple of
// 256 KiB, start at most at the bytes held, and name the session's size; the
// fake refuses others with 400 and keeps the bytes it held. An unknown
// session is 404.
func TestFake_ChunkRules(t *testing.T) {
	c := newClient(t)
	data := pattern(3*chunk, 2)
	session := c.session("a.mp4", "root", "", len(data))
	for _, tc := range []struct {
		name     string
		from, to int
		header   string
	}{
		{"misaligned", 0, chunk + 1, ""},
		{"past the bytes held", chunk, 2 * chunk, ""},
		{"another size", 0, chunk, fmt.Sprintf("bytes 0-%d/%d", chunk-1, len(data)+1)},
		{"length not the range's", 0, chunk, fmt.Sprintf("bytes 0-%d/%d", chunk, len(data))},
	} {
		header := tc.header
		if header == "" {
			header = fmt.Sprintf("bytes %d-%d/%d", tc.from, tc.to-1, len(data))
		}
		resp, body := c.do(http.MethodPut, session, map[string]string{"Content-Range": header}, data[tc.from:tc.to])
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d %s, want 400", tc.name, resp.StatusCode, body)
		}
	}
	resp, body := c.status(session, len(data))
	c.want(resp, body, http.StatusPermanentRedirect)
	if got := resp.Header.Get("Range"); got != "" {
		t.Errorf("Range %q after refused chunks, want none", got)
	}
	resp, body = c.status(strings.Replace(session, "upload_id=", "upload_id=x", 1), len(data))
	c.want(resp, body, http.StatusNotFound)
}

// TestFake_IDs: a create may use only an ID that files.generateIds issued,
// and a create with the ID of a file that exists is 409; a parent that does
// not exist is 404.
func TestFake_IDs(t *testing.T) {
	c := newClient(t)
	resp, body := c.folder("made-up", "ipcam", "root")
	c.want(resp, body, http.StatusBadRequest)
	id := c.generateID()
	resp, body = c.folder(id, "ipcam", "root")
	c.want(resp, body, http.StatusOK)
	resp, body = c.folder(id, "ipcam", "root")
	c.want(resp, body, http.StatusConflict)
	if !strings.Contains(string(body), `"code":409`) {
		t.Errorf("the 409 body %s, want Drive's error shape", body)
	}
	resp, body = c.folder(c.generateID(), "hub", c.generateID())
	c.want(resp, body, http.StatusNotFound)
	meta, _ := json.Marshal(map[string]any{"name": "a", "parents": []string{"gone"}})
	resp, body = c.do(http.MethodPost, "/upload/drive/v3/files?uploadType=resumable",
		map[string]string{"X-Upload-Content-Length": "1"}, meta)
	c.want(resp, body, http.StatusNotFound)
	if objects := c.f.Objects(); len(objects) != 1 || !objects[0].Folder || objects[0].Name != "ipcam" {
		t.Errorf("Objects() = %+v, want the one folder", objects)
	}
}

// TestFake_FailNext: the next request of a method gets the injected status,
// with Drive's error body naming the reason and a Retry-After header; Reset
// drops the connection. Later requests are served, and every one is
// counted.
func TestFake_FailNext(t *testing.T) {
	c := newClient(t)
	c.f.FailNext("about.get", http.StatusForbidden, "userRateLimitExceeded", 7*time.Second)
	c.f.FailNext("about.get", http.StatusServiceUnavailable, "backendError", 0)
	resp, body := c.do(http.MethodGet, "/drive/v3/about?fields=storageQuota", nil, nil)
	c.want(resp, body, http.StatusForbidden)
	var apiErr struct {
		Error struct {
			Code   int
			Errors []struct{ Reason string }
		}
	}
	if err := json.Unmarshal(body, &apiErr); err != nil || apiErr.Error.Code != 403 || len(apiErr.Error.Errors) != 1 || apiErr.Error.Errors[0].Reason != "userRateLimitExceeded" {
		t.Errorf("the injected answer %s, want Drive's error with reason userRateLimitExceeded", body)
	}
	if got := resp.Header.Get("Retry-After"); got != "7" {
		t.Errorf("Retry-After %q, want 7", got)
	}
	resp, body = c.do(http.MethodGet, "/drive/v3/about?fields=storageQuota", nil, nil)
	c.want(resp, body, http.StatusServiceUnavailable)
	if resp.Header.Get("Retry-After") != "" {
		t.Errorf("Retry-After %q without one injected, want none", resp.Header.Get("Retry-After"))
	}
	resp, body = c.do(http.MethodGet, "/drive/v3/about?fields=storageQuota", nil, nil)
	c.want(resp, body, http.StatusOK)

	c.f.FailNext("files.create.resumable", Reset, "", 0)
	req, _ := http.NewRequest(http.MethodPost, c.srv.URL+"/upload/drive/v3/files?uploadType=resumable", strings.NewReader(`{"name":"a"}`))
	req.Header.Set("Authorization", "Bearer "+c.token)
	if resp, err := c.srv.Client().Do(req); err == nil {
		_ = resp.Body.Close()
		t.Errorf("a Reset answered HTTP %d, want a dropped connection", resp.StatusCode)
	}
	if got := c.f.Counts(); got["about.get"] != 3 || got["files.create.resumable"] != 1 {
		t.Errorf("Counts() = %v, want 3 about.get and 1 files.create.resumable", got)
	}
}

// TestFake_AcceptToken: a request without a bearer token, or with one that
// AcceptToken refuses, is 401.
func TestFake_AcceptToken(t *testing.T) {
	c := newClient(t)
	var seen []string
	c.f.AcceptToken(func(tok string) bool {
		seen = append(seen, tok)
		return tok == "tok-good"
	})
	for _, tc := range []struct {
		token  string
		status int
	}{{"", http.StatusUnauthorized}, {"tok-bad", http.StatusUnauthorized}, {"tok-good", http.StatusOK}} {
		c.token = tc.token
		resp, body := c.do(http.MethodGet, "/drive/v3/about?fields=storageQuota", nil, nil)
		c.want(resp, body, tc.status)
	}
	if fmt.Sprint(seen) != "[tok-bad tok-good]" {
		t.Errorf("AcceptToken saw %v, want the two tokens sent", seen)
	}
}

// TestFake_DailyCapAfter: once the bytes uploaded since DailyCapAfter would
// pass the cap, uploads answer 403 dailyLimitExceeded until 24 h after the
// cap was reached.
func TestFake_DailyCapAfter(t *testing.T) {
	c := newClient(t)
	c.f.DailyCapAfter(chunk)
	data := pattern(2*chunk, 3)
	session := c.session("a.mp4", "root", "", len(data))
	resp, body := c.put(session, data, 0, chunk)
	c.want(resp, body, http.StatusPermanentRedirect)
	resp, body = c.put(session, data, chunk, 2*chunk)
	c.want(resp, body, http.StatusForbidden)
	if !strings.Contains(string(body), "dailyLimitExceeded") {
		t.Errorf("the capped chunk answered %s, want dailyLimitExceeded", body)
	}
	c.clock.Advance(24*time.Hour - time.Second)
	resp, body = c.put(session, data, chunk, 2*chunk)
	c.want(resp, body, http.StatusForbidden)
	c.clock.Advance(time.Second)
	resp, body = c.put(session, data, chunk, 2*chunk)
	c.want(resp, body, http.StatusOK)
}

// TestFake_QuotaAndAbout: about.get needs fields and reports the quota that
// SetQuota set plus the bytes stored; an upload that would pass the limit is
// 403 storageQuotaExceeded.
func TestFake_QuotaAndAbout(t *testing.T) {
	c := newClient(t)
	c.f.SetQuota(1000, 100)
	resp, body := c.do(http.MethodGet, "/drive/v3/about", nil, nil)
	c.want(resp, body, http.StatusBadRequest)
	resp, body = c.do(http.MethodGet, "/drive/v3/about?fields=storageQuota", nil, nil)
	c.want(resp, body, http.StatusOK)
	if want := `{"storageQuota":{"limit":"1000","usage":"100","usageInDrive":"100","usageInDriveTrash":"0"}}`; strings.TrimSpace(string(body)) != want {
		t.Errorf("about.get answered %s, want %s", body, want)
	}
	meta, _ := json.Marshal(map[string]any{"name": "big", "parents": []string{"root"}})
	resp, body = c.do(http.MethodPost, "/upload/drive/v3/files?uploadType=resumable",
		map[string]string{"X-Upload-Content-Length": "901"}, meta)
	c.want(resp, body, http.StatusForbidden)
	if !strings.Contains(string(body), "storageQuotaExceeded") {
		t.Errorf("the upload past the limit answered %s, want storageQuotaExceeded", body)
	}
	data := pattern(900, 4)
	session := c.session("fits", "root", "", len(data))
	resp, body = c.put(session, data, 0, len(data))
	c.want(resp, body, http.StatusOK)
	_, body = c.do(http.MethodGet, "/drive/v3/about?fields=storageQuota", nil, nil)
	if !strings.Contains(string(body), `"usage":"1000"`) {
		t.Errorf("about.get after 900 bytes answered %s, want usage 1000", body)
	}
}

// TestFake_CreateMeter: MaxCreatesPerSec is the most creates that arrived in
// any 1-s window, as the fake's clock tells their times.
func TestFake_CreateMeter(t *testing.T) {
	c := newClient(t)
	create := func() {
		resp, body := c.folder(c.generateID(), "f", "root")
		c.want(resp, body, http.StatusOK)
	}
	create()
	c.clock.Advance(time.Second)
	create()
	c.clock.Advance(time.Second)
	create()
	if got := c.f.MaxCreatesPerSec(); got != 1 {
		t.Errorf("MaxCreatesPerSec() = %d after creates 1 s apart, want 1", got)
	}
	create()
	c.clock.Advance(500 * time.Millisecond)
	create()
	if got := c.f.MaxCreatesPerSec(); got != 3 {
		t.Errorf("MaxCreatesPerSec() = %d after 3 creates within 0.5 s, want 3", got)
	}
}

// TestFake_SmallFilesListAndDelete: a multipart create with an issued ID and
// a media update replace a small file's bytes; files.list pages a folder's
// children; files.get.media serves a range; deleting a folder deletes what
// it holds.
func TestFake_SmallFilesListAndDelete(t *testing.T) {
	c := newClient(t)
	dir := c.generateID()
	resp, body := c.folder(dir, "ipcam", "root")
	c.want(resp, body, http.StatusOK)
	small := c.generateID()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	_ = json.NewEncoder(part).Encode(map[string]any{"id": small, "name": "README.txt", "parents": []string{dir}})
	part, _ = mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/plain"}})
	_, _ = part.Write([]byte("one"))
	_ = mw.Close()
	resp, body = c.do(http.MethodPost, "/upload/drive/v3/files?uploadType=multipart&fields=id,md5Checksum,size",
		map[string]string{"Content-Type": "multipart/related; boundary=" + mw.Boundary()}, buf.Bytes())
	c.want(resp, body, http.StatusOK)
	resp, body = c.do(http.MethodPatch, "/upload/drive/v3/files/"+small+"?uploadType=media&fields=id,md5Checksum,size",
		map[string]string{"Content-Type": "text/plain"}, []byte("two!"))
	c.want(resp, body, http.StatusOK)
	if !strings.Contains(string(body), wantMD5([]byte("two!"))) || !strings.Contains(string(body), `"size":"4"`) {
		t.Errorf("files.update answered %s, want the new bytes' md5Checksum and size", body)
	}
	for i := range 3 {
		data := pattern(10+i, byte(i))
		s := c.session(fmt.Sprintf("seg-%d.mp4", i), dir, "", len(data))
		resp, body := c.put(s, data, 0, len(data))
		c.want(resp, body, http.StatusOK)
	}

	var names []string
	token := ""
	for pages := 0; ; pages++ {
		url := "/drive/v3/files?pageSize=2&fields=nextPageToken,files(id,name)&q=" +
			strings.ReplaceAll("'"+dir+"' in parents and trashed = false", " ", "%20")
		if token != "" {
			url += "&pageToken=" + token
		}
		resp, body := c.do(http.MethodGet, url, nil, nil)
		c.want(resp, body, http.StatusOK)
		var page struct {
			NextPageToken string
			Files         []struct{ Name string }
		}
		if err := json.Unmarshal(body, &page); err != nil {
			t.Fatal(err)
		}
		for _, f := range page.Files {
			names = append(names, f.Name)
		}
		if token = page.NextPageToken; token == "" || pages > 3 {
			break
		}
	}
	if fmt.Sprint(names) != "[README.txt seg-0.mp4 seg-1.mp4 seg-2.mp4]" {
		t.Errorf("files.list gave %v, want the folder's four files in two pages", names)
	}

	resp, body = c.do(http.MethodGet, "/drive/v3/files/"+small+"?alt=media", map[string]string{"Range": "bytes=1-2"}, nil)
	c.want(resp, body, http.StatusPartialContent)
	if string(body) != "wo" || resp.Header.Get("Content-Range") != "bytes 1-2/4" {
		t.Errorf("the range read gave %q (%s), want \"wo\" (bytes 1-2/4)", body, resp.Header.Get("Content-Range"))
	}
	resp, body = c.do(http.MethodDelete, "/drive/v3/files/"+dir, nil, nil)
	c.want(resp, body, http.StatusNoContent)
	if objects := c.f.Objects(); len(objects) != 0 {
		t.Errorf("Objects() = %+v after the folder's delete, want none", objects)
	}
	resp, body = c.do(http.MethodDelete, "/drive/v3/files/"+dir, nil, nil)
	c.want(resp, body, http.StatusNotFound)
}

// TestFake_LocationUnderPrefix: served under a path prefix, as
// fakeproviders serves it, the session URI keeps the prefix.
func TestFake_LocationUnderPrefix(t *testing.T) {
	f := New(Options{})
	mux := http.NewServeMux()
	mux.Handle("/gdrive-upload/", http.StripPrefix("/gdrive-upload", f.Handler()))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &client{t: t, f: f, srv: srv, token: "tok-1"}
	meta, _ := json.Marshal(map[string]any{"name": "a", "parents": []string{"root"}})
	resp, body := c.do(http.MethodPost, "/gdrive-upload/upload/drive/v3/files?uploadType=resumable",
		map[string]string{"X-Upload-Content-Length": "3"}, meta)
	c.want(resp, body, http.StatusOK)
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, srv.URL+"/gdrive-upload/upload/drive/v3/files?") {
		t.Fatalf("Location %q, want it under %s/gdrive-upload", loc, srv.URL)
	}
	resp, body = c.do(http.MethodPut, loc, map[string]string{"Content-Range": "bytes 0-2/3"}, []byte("abc"))
	c.want(resp, body, http.StatusOK)
}

// TestFake_AcceptTokenMayCallTheFake (Task 5 review M8): AcceptToken's
// callback runs with the fake unlocked, so it may call the fake's methods.
func TestFake_AcceptTokenMayCallTheFake(t *testing.T) {
	c := newClient(t)
	var blocked atomic.Bool
	c.f.AcceptToken(func(string) bool {
		got := make(chan struct{})
		go func() {
			defer close(got)
			_ = c.f.Counts()
		}()
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			blocked.Store(true) // Counts waits for the lock that the fake holds
		}
		return true
	})
	resp, body := c.do(http.MethodGet, "/drive/v3/about?fields=storageQuota", nil, nil)
	c.want(resp, body, http.StatusOK)
	if blocked.Load() {
		t.Error("the callback's Counts() waited 5 s: AcceptToken runs under the fake's lock")
	}
}

// TestFake_AbandonedSessionsAreFreed (Task 5 review, out of scope item): the
// bytes of a session that does not hold every byte are freed when the client
// cancels it (DELETE of its URI, answered 499, as every later request of it
// is) or when it expires a week after its start (404 from then on), so that a
// long lab run does not keep every abandoned upload in memory.
func TestFake_AbandonedSessionsAreFreed(t *testing.T) {
	c := newClient(t)
	data := pattern(2*chunk+10, 4)
	cancelled := c.session("a.mp4", "root", "", len(data))
	resp, body := c.put(cancelled, data, 0, chunk)
	c.want(resp, body, http.StatusPermanentRedirect)
	if n := c.f.PendingBytes(); n != chunk {
		t.Fatalf("PendingBytes() = %d after one chunk, want %d", n, chunk)
	}
	resp, body = c.do(http.MethodDelete, cancelled, nil, nil)
	c.want(resp, body, StatusCancelled)
	if n := c.f.PendingBytes(); n != 0 {
		t.Errorf("PendingBytes() = %d after the cancel, want 0", n)
	}
	resp, body = c.status(cancelled, len(data))
	c.want(resp, body, StatusCancelled)
	resp, body = c.put(cancelled, data, chunk, 2*chunk)
	c.want(resp, body, StatusCancelled)
	if n := c.f.Counts()["upload.cancel"]; n != 1 {
		t.Errorf("%d upload.cancel requests counted, want 1", n)
	}

	expiring := c.session("b.mp4", "root", "", len(data))
	resp, body = c.put(expiring, data, 0, chunk)
	c.want(resp, body, http.StatusPermanentRedirect)
	c.clock.Advance(7*24*time.Hour - time.Second)
	resp, body = c.status(expiring, len(data))
	c.want(resp, body, http.StatusPermanentRedirect)
	c.clock.Advance(time.Second)
	if n := c.f.PendingBytes(); n != 0 {
		t.Errorf("PendingBytes() = %d a week after the session's start, want 0", n)
	}
	resp, body = c.status(expiring, len(data))
	c.want(resp, body, http.StatusNotFound)
	if objects := c.f.Objects(); len(objects) != 0 {
		t.Errorf("Objects() = %d objects, want none", len(objects))
	}
}
