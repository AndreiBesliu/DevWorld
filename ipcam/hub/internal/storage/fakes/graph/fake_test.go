package fakegraph

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fragment = 320 << 10 // Graph's fragment alignment

// client sends requests to a fake behind an httptest server: to the API
// with a bearer token, to an uploadUrl or a download URL without one.
type client struct {
	t     *testing.T
	f     *Fake
	srv   *httptest.Server
	token string
}

func newClient(t *testing.T) *client {
	t.Helper()
	return newClockedClient(t, func() time.Time { return time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC) })
}

// newClockedClient is newClient with a fake whose clock is clock.
func newClockedClient(t *testing.T, clock func() time.Time) *client {
	t.Helper()
	f := New(Options{Clock: clock})
	srv := httptest.NewServer(guard(t, f.Handler()))
	t.Cleanup(srv.Close)
	return &client{t: t, f: f, srv: srv, token: "tok-1"}
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

// api sends a request to the API path p, with the client's token.
func (c *client) api(method, p string, body any) (*http.Response, []byte) {
	c.t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	header := map[string]string{"Content-Type": "application/json"}
	if c.token != "" {
		header["Authorization"] = "Bearer " + c.token
	}
	return c.do(method, c.srv.URL+p, header, data)
}

// do sends a request to the URL u with header and body, and returns the
// answer with its body read.
func (c *client) do(method, u string, header map[string]string, body []byte) (*http.Response, []byte) {
	c.t.Helper()
	req, err := http.NewRequest(method, u, bytes.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.srv.Client().Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, u, err)
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

// session opens an upload session of key with conflictBehavior conflict
// ("" for none) and returns its uploadUrl.
func (c *client) session(key, conflict string) string {
	c.t.Helper()
	item := map[string]any{}
	if conflict != "" {
		item["@microsoft.graph.conflictBehavior"] = conflict
	}
	resp, body := c.api(http.MethodPost, "/v1.0/me/drive/special/approot:/"+key+":/createUploadSession", map[string]any{"item": item})
	c.want(resp, body, http.StatusOK)
	var s struct {
		UploadURL string   `json:"uploadUrl"`
		Next      []string `json:"nextExpectedRanges"`
	}
	if err := json.Unmarshal(body, &s); err != nil || !strings.HasPrefix(s.UploadURL, c.srv.URL+"/up/") {
		c.t.Fatalf("createUploadSession answered %s (%v), want an uploadUrl under %s/up/", body, err, c.srv.URL)
	}
	return s.UploadURL
}

// put sends data[from:to] of a len(data)-byte upload to uploadURL.
func (c *client) put(uploadURL string, data []byte, from, to int) (*http.Response, []byte) {
	c.t.Helper()
	return c.do(http.MethodPut, uploadURL, map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", from, to-1, len(data))}, data[from:to])
}

// upload uploads data to key in fragments of size fragment and returns the
// item that the last one answered.
func (c *client) upload(key, conflict string, data []byte) map[string]any {
	c.t.Helper()
	u := c.session(key, conflict)
	var resp *http.Response
	var body []byte
	for off := 0; off < len(data); off += fragment {
		resp, body = c.put(u, data, off, min(off+fragment, len(data)))
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		c.t.Fatalf("the last fragment of %s: HTTP %d %s, want 200 or 201", key, resp.StatusCode, body)
	}
	var it map[string]any
	_ = json.Unmarshal(body, &it)
	return it
}

func pattern(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7) + seed
	}
	return b
}

func next(t *testing.T, body []byte) []string {
	t.Helper()
	var s struct {
		Next []string `json:"nextExpectedRanges"`
	}
	if err := json.Unmarshal(body, &s); err != nil {
		t.Fatalf("%s: %v", body, err)
	}
	return s.Next
}

// TestFake_QuickXorVectors: the fake's QuickXor, its own implementation,
// gives each entry of the independent vectors (rclone's tests, as
// storage.Digest's) its expected hash.
func TestFake_QuickXorVectors(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/quickxor/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Size     int    `json:"size"`
		Data     string `json:"data"`
		QuickXor string `json:"quickxor"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil || len(vectors) == 0 {
		t.Fatalf("vectors: %d, %v", len(vectors), err)
	}
	for _, v := range vectors {
		data, err := base64.StdEncoding.DecodeString(v.Data)
		if err != nil || len(data) != v.Size {
			t.Fatalf("vector of size %d: %d bytes, %v", v.Size, len(data), err)
		}
		if got := quickXorHash(data); got != v.QuickXor {
			t.Errorf("quickXorHash of the %d-byte vector = %s, want %s", v.Size, got, v.QuickXor)
		}
	}
}

// TestFake_UploadSession: fragments answer 202 with the next expected range
// until the last, which answers 201 with the item, its size and its
// quickXorHash; the parents are made as folders; the completed session's
// uploadUrl then answers 404.
func TestFake_UploadSession(t *testing.T) {
	c := newClient(t)
	data := pattern(2*fragment+100, 1)
	u := c.session(".ipcam/H/a.bin", "replace")
	resp, body := c.put(u, data, 0, fragment)
	c.want(resp, body, http.StatusAccepted)
	if got := next(t, body); !reflect.DeepEqual(got, []string{strconv.Itoa(fragment) + "-"}) {
		t.Errorf("nextExpectedRanges after the first fragment %q, want %d-", got, fragment)
	}
	resp, body = c.do(http.MethodGet, u, nil, nil)
	c.want(resp, body, http.StatusOK)
	if got := next(t, body); !reflect.DeepEqual(got, []string{strconv.Itoa(fragment) + "-"}) {
		t.Errorf("the status's nextExpectedRanges %q, want %d-", got, fragment)
	}
	resp, body = c.put(u, data, fragment, 2*fragment)
	c.want(resp, body, http.StatusAccepted)
	resp, body = c.put(u, data, 2*fragment, len(data))
	c.want(resp, body, http.StatusCreated)
	var it struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
		File struct {
			Hashes struct {
				QuickXor string `json:"quickXorHash"`
			} `json:"hashes"`
		} `json:"file"`
	}
	if err := json.Unmarshal(body, &it); err != nil || it.ID == "" || it.Name != "a.bin" || it.Size != int64(len(data)) ||
		it.File.Hashes.QuickXor != quickXorHash(data) {
		t.Fatalf("the last fragment answered %s, want the item a.bin of %d bytes with its quickXorHash", body, len(data))
	}
	resp, body = c.do(http.MethodGet, u, nil, nil)
	c.want(resp, body, http.StatusNotFound)
	resp, body = c.put(u, data, 2*fragment, len(data))
	c.want(resp, body, http.StatusNotFound)

	objects := c.f.Objects()
	if len(objects) != 3 || !objects[0].Folder || objects[0].Path != ".ipcam" || !objects[1].Folder || objects[1].Path != ".ipcam/H" ||
		objects[2].Path != ".ipcam/H/a.bin" || objects[2].ID != it.ID || !bytes.Equal(objects[2].Data, data) {
		t.Errorf("Objects() = %d objects, want the folders .ipcam and .ipcam/H and the file", len(objects))
	}
	if c.f.PendingBytes() != 0 {
		t.Errorf("PendingBytes() = %d after the upload, want 0", c.f.PendingBytes())
	}
}

// TestFake_FragmentRules: a fragment must start at the next expected byte
// (416 for one held already, 400 past it), name the session's total and its
// own length, be a multiple of 320 KiB unless it is the last, and be under
// 60 MiB (413); an uploadUrl request with Authorization is 401. A refused
// fragment changes nothing.
func TestFake_FragmentRules(t *testing.T) {
	c := newClient(t)
	data := pattern(3*fragment, 2)
	u := c.session("a.bin", "")
	resp, body := c.put(u, data, 0, fragment)
	c.want(resp, body, http.StatusAccepted)
	for _, tc := range []struct {
		name   string
		header map[string]string
		body   []byte
		status int
	}{
		{"a fragment held already", map[string]string{"Content-Range": fmt.Sprintf("bytes 0-%d/%d", fragment-1, len(data))}, data[:fragment], http.StatusRequestedRangeNotSatisfiable},
		{"a fragment past the next byte", map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", 2*fragment, 3*fragment-1, len(data))}, data[2*fragment:], http.StatusBadRequest},
		{"not a multiple of 320 KiB", map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", fragment, fragment+999, len(data))}, data[fragment : fragment+1000], http.StatusBadRequest},
		{"another total", map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", fragment, 2*fragment-1, len(data)+1)}, data[fragment : 2*fragment], http.StatusBadRequest},
		{"a length that is not the range's", map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", fragment, 2*fragment-1, len(data))}, data[fragment : 2*fragment-1], http.StatusBadRequest},
		{"no Content-Range", nil, data[fragment : 2*fragment], http.StatusBadRequest},
		{"with Authorization", map[string]string{"Content-Range": fmt.Sprintf("bytes %d-%d/%d", fragment, 2*fragment-1, len(data)), "Authorization": "Bearer tok-1"}, data[fragment : 2*fragment], http.StatusUnauthorized},
	} {
		resp, body := c.do(http.MethodPut, u, tc.header, tc.body)
		if resp.StatusCode != tc.status {
			t.Errorf("%s: HTTP %d %s, want %d", tc.name, resp.StatusCode, body, tc.status)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		resp, body := c.do(method, u, map[string]string{"Authorization": "Bearer tok-1"}, nil)
		c.want(resp, body, http.StatusUnauthorized)
	}
	if n := c.f.PendingBytes(); n != fragment {
		t.Errorf("PendingBytes() = %d after the refusals, want %d", n, fragment)
	}

	big := c.session("big.bin", "")
	huge := make([]byte, 60<<20)
	resp, body = c.do(http.MethodPut, big, map[string]string{"Content-Range": fmt.Sprintf("bytes 0-%d/%d", len(huge)-1, len(huge))}, huge)
	c.want(resp, body, http.StatusRequestEntityTooLarge)
	last := huge[:60<<20-1]
	resp, body = c.do(http.MethodPut, big, map[string]string{"Content-Range": fmt.Sprintf("bytes 0-%d/%d", len(last)-1, len(last))}, last)
	c.want(resp, body, http.StatusCreated)
}

// TestFake_ConflictBehavior: an item at the key fails the last fragment with
// 409 nameAlreadyExists under fail, Graph's default; replace keeps its ID
// and answers 200 with the new bytes; rename makes "a 1.bin". A session
// request with another behaviour, deferCommit or another name is 400.
func TestFake_ConflictBehavior(t *testing.T) {
	c := newClient(t)
	old, newer := pattern(1000, 1), pattern(2000, 2)
	first := c.upload("d/a.bin", "replace", old)
	for _, conflict := range []string{"", "fail"} {
		u := c.session("d/a.bin", conflict)
		resp, body := c.put(u, newer, 0, len(newer))
		c.want(resp, body, http.StatusConflict)
		if !strings.Contains(string(body), "nameAlreadyExists") {
			t.Errorf("conflictBehavior %q answered %s, want nameAlreadyExists", conflict, body)
		}
	}
	u := c.session("d/a.bin", "replace")
	resp, body := c.put(u, newer, 0, len(newer))
	c.want(resp, body, http.StatusOK)
	var replaced struct {
		ID   string `json:"id"`
		Size int    `json:"size"`
	}
	_ = json.Unmarshal(body, &replaced)
	if replaced.ID != first["id"] || replaced.Size != len(newer) {
		t.Errorf("replace answered %s, want the item %v with %d bytes", body, first["id"], len(newer))
	}
	renamed := c.upload("d/a.bin", "rename", old)
	if renamed["name"] != "a 1.bin" {
		t.Errorf("rename made %v, want a 1.bin", renamed["name"])
	}
	var files []string
	for _, o := range c.f.Objects() {
		if !o.Folder {
			files = append(files, fmt.Sprintf("%s:%d", o.Path, len(o.Data)))
		}
	}
	if want := []string{"d/a.bin:2000", "d/a 1.bin:1000"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files %v, want %v", files, want)
	}
	for name, req := range map[string]any{
		"an unknown behaviour": map[string]any{"item": map[string]any{"@microsoft.graph.conflictBehavior": "overwrite"}},
		"deferCommit":          map[string]any{"item": map[string]any{}, "deferCommit": true},
		"another name":         map[string]any{"item": map[string]any{"name": "b.bin"}},
	} {
		resp, body := c.api(http.MethodPost, "/v1.0/me/drive/special/approot:/d/a.bin:/createUploadSession", req)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d %s, want 400", name, resp.StatusCode, body)
		}
	}
}

// TestFake_CancelFreesTheSession: a DELETE of an uploadUrl drops the session
// and its bytes; the uploadUrl then answers 404.
func TestFake_CancelFreesTheSession(t *testing.T) {
	c := newClient(t)
	data := pattern(2*fragment, 3)
	u := c.session("a.bin", "replace")
	resp, body := c.put(u, data, 0, fragment)
	c.want(resp, body, http.StatusAccepted)
	resp, body = c.do(http.MethodDelete, u, nil, nil)
	c.want(resp, body, http.StatusNoContent)
	if n := c.f.PendingBytes(); n != 0 {
		t.Errorf("PendingBytes() = %d after the cancel, want 0", n)
	}
	resp, body = c.do(http.MethodGet, u, nil, nil)
	c.want(resp, body, http.StatusNotFound)
	resp, body = c.do(http.MethodDelete, u, nil, nil)
	c.want(resp, body, http.StatusNotFound)
}

// TestFake_SessionExpiresAfterItsLastRequest: as Graph's, a session expires
// a day (sessionLife) after its last request, each of which moves its
// expirationDateTime on, and its bytes are freed then; its uploadUrl
// answers 404 from then on. A long lab run so does not keep every abandoned
// upload in memory.
func TestFake_SessionExpiresAfterItsLastRequest(t *testing.T) {
	var now atomic.Int64
	start := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	now.Store(start.UnixNano())
	advance := func(d time.Duration) { now.Add(int64(d)) }
	c := newClockedClient(t, func() time.Time { return time.Unix(0, now.Load()).UTC() })
	data := pattern(3*fragment, 5)
	expiration := func(body []byte) time.Time {
		t.Helper()
		var s struct {
			Expiration time.Time `json:"expirationDateTime"`
		}
		if err := json.Unmarshal(body, &s); err != nil {
			t.Fatalf("the session's answer %s: %v", body, err)
		}
		return s.Expiration
	}
	u := c.session("a.bin", "replace")
	advance(23 * time.Hour)
	resp, body := c.put(u, data, 0, fragment)
	c.want(resp, body, http.StatusAccepted)
	if got, want := expiration(body), start.Add(23*time.Hour+sessionLife); !got.Equal(want) {
		t.Errorf("expirationDateTime after a fragment = %s, want %s", got, want)
	}
	advance(sessionLife - time.Second)
	resp, body = c.do(http.MethodGet, u, nil, nil)
	c.want(resp, body, http.StatusOK)
	advance(sessionLife - time.Second)
	if n := c.f.PendingBytes(); n != fragment {
		t.Errorf("PendingBytes() = %d a second before the expiry, want %d", n, fragment)
	}
	resp, body = c.put(u, data, fragment, 2*fragment)
	c.want(resp, body, http.StatusAccepted)
	advance(sessionLife)
	if n := c.f.PendingBytes(); n != 0 {
		t.Errorf("PendingBytes() = %d a day after the last request, want 0", n)
	}
	resp, body = c.do(http.MethodGet, u, nil, nil)
	c.want(resp, body, http.StatusNotFound)
	resp, body = c.put(u, data, 2*fragment, 3*fragment)
	c.want(resp, body, http.StatusNotFound)
	if objects := c.f.Objects(); len(objects) != 0 {
		t.Errorf("Objects() = %d objects, want none", len(objects))
	}
}

// quota is the drive's quota as the fake answers $select=quota.
type quota struct {
	Total, Used, Remaining, Deleted int64
	State                           string
}

func (c *client) quota() quota {
	c.t.Helper()
	resp, body := c.api(http.MethodGet, "/v1.0/me/drive?$select=quota", nil)
	c.want(resp, body, http.StatusOK)
	var d struct{ Quota quota }
	if err := json.Unmarshal(body, &d); err != nil {
		c.t.Fatal(err)
	}
	return d.Quota
}

// TestFake_RecycleBinOccupiesQuota: a deleted item, or a deleted folder's
// items, go to the recycle bin, where their bytes stay in used and in
// deleted, and remaining does not grow; EmptyBin frees them.
func TestFake_RecycleBinOccupiesQuota(t *testing.T) {
	c := newClient(t)
	c.f.SetQuota(10000, 500)
	a := c.upload("d/a.bin", "replace", pattern(1000, 1))
	c.upload("d/e/b.bin", "replace", pattern(2000, 2))
	if q := c.quota(); q != (quota{Total: 10000, Used: 3500, Remaining: 6500, State: "normal"}) {
		t.Errorf("the quota after two uploads %+v", q)
	}
	resp, body := c.api(http.MethodDelete, "/v1.0/me/drive/items/"+a["id"].(string), nil)
	c.want(resp, body, http.StatusNoContent)
	if q := c.quota(); q != (quota{Total: 10000, Used: 3500, Remaining: 6500, Deleted: 1000, State: "normal"}) {
		t.Errorf("the quota after a delete %+v, want 1000 bytes deleted and the same remaining", q)
	}
	resp, body = c.api(http.MethodGet, "/v1.0/me/drive/items/"+a["id"].(string), nil)
	c.want(resp, body, http.StatusNotFound)
	resp, body = c.api(http.MethodDelete, "/v1.0/me/drive/items/"+a["id"].(string), nil)
	c.want(resp, body, http.StatusNotFound)

	var dir string
	for _, o := range c.f.Objects() {
		if o.Path == "d/e" {
			dir = o.ID
		}
	}
	resp, body = c.api(http.MethodDelete, "/v1.0/me/drive/items/"+dir, nil)
	c.want(resp, body, http.StatusNoContent)
	if bin := c.f.Bin(); len(bin) != 3 || bin[0].Path != "d/a.bin" || bin[1].Path != "d/e" || bin[2].Path != "d/e/b.bin" {
		t.Errorf("Bin() = %d items, want d/a.bin, then the folder d/e and its file", len(bin))
	}
	if q := c.quota(); q.Deleted != 3000 || q.Remaining != 6500 {
		t.Errorf("the quota after the folder's delete %+v, want 3000 bytes deleted, 6500 remaining", q)
	}
	c.f.EmptyBin()
	if q := c.quota(); q != (quota{Total: 10000, Used: 500, Remaining: 9500, State: "normal"}) {
		t.Errorf("the quota after EmptyBin %+v", q)
	}
	c.f.SetQuota(1000, 950)
	u := c.session("c.bin", "replace")
	resp, body = c.put(u, pattern(100, 3), 0, 100)
	c.want(resp, body, http.StatusInsufficientStorage)
	if !strings.Contains(string(body), "quotaLimitReached") {
		t.Errorf("a fragment past the quota answered %s, want quotaLimitReached", body)
	}
}

// TestFake_Select: item and drive answers hold exactly the properties of
// $select, a property that the fake does not know is 400, and the drive's
// default set names its owner.
func TestFake_Select(t *testing.T) {
	c := newClient(t)
	it := c.upload("a.bin", "replace", pattern(100, 1))
	keys := func(body []byte) []string {
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		return slices.Sorted(maps.Keys(m))
	}
	for p, want := range map[string][]string{
		"/v1.0/me/drive/items/" + it["id"].(string) + "?$select=id,size,file": {"file", "id", "size"},
		"/v1.0/me/drive/special/approot:/a.bin?$select=id,size":               {"id", "size"},
		"/v1.0/me/drive?$select=quota":                                        {"quota"},
	} {
		resp, body := c.api(http.MethodGet, p, nil)
		c.want(resp, body, http.StatusOK)
		if got := keys(body); !reflect.DeepEqual(got, want) {
			t.Errorf("GET %s answered the properties %v, want %v", p, got, want)
		}
	}
	for _, p := range []string{"/v1.0/me/drive/items/" + it["id"].(string) + "?$select=id,owner", "/v1.0/me/drive?$select=quota,email"} {
		resp, body := c.api(http.MethodGet, p, nil)
		c.want(resp, body, http.StatusBadRequest)
	}
	resp, body := c.api(http.MethodGet, "/v1.0/me/drive", nil)
	c.want(resp, body, http.StatusOK)
	if !strings.Contains(string(body), `"owner"`) {
		t.Errorf("the drive's default set %s names no owner", body)
	}
}

// TestFake_ContentRedirectsToADownloadURL: /content answers 302 to a
// pre-authenticated download URL, which serves a Range with 206 and no
// token, 416 past the end, and 404 once the item is deleted; the item's
// @microsoft.graph.downloadUrl is the same URL.
func TestFake_ContentRedirectsToADownloadURL(t *testing.T) {
	c := newClient(t)
	data := pattern(5000, 4)
	it := c.upload("a.bin", "replace", data)
	id := it["id"].(string)
	req, _ := http.NewRequest(http.MethodGet, c.srv.URL+"/v1.0/me/drive/items/"+id+"/content", nil)
	req.Header.Set("Authorization", "Bearer tok-1")
	resp, err := c.srv.Client().Transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	location := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(location, c.srv.URL+"/download/") {
		t.Fatalf("/content answered %d to %q, want 302 to a download URL", resp.StatusCode, location)
	}
	resp2, body := c.api(http.MethodGet, "/v1.0/me/drive/items/"+id+"?$select=id,@microsoft.graph.downloadUrl", nil)
	c.want(resp2, body, http.StatusOK)
	if !strings.Contains(string(body), location) {
		t.Errorf("the item's downloadUrl %s, want %s", body, location)
	}
	resp, got := c.do(http.MethodGet, location, map[string]string{"Range": "bytes=100-199"}, nil)
	c.want(resp, got, http.StatusPartialContent)
	if !bytes.Equal(got, data[100:200]) {
		t.Errorf("the range read %d bytes that differ from bytes 100 to 200", len(got))
	}
	resp, got = c.do(http.MethodGet, location, map[string]string{"Range": "bytes=4990-"}, nil)
	c.want(resp, got, http.StatusPartialContent)
	if !bytes.Equal(got, data[4990:]) {
		t.Errorf("an open range read %d bytes, want the last 10", len(got))
	}
	resp, got = c.do(http.MethodGet, location, map[string]string{"Range": "bytes=5000-5009"}, nil)
	c.want(resp, got, http.StatusRequestedRangeNotSatisfiable)
	resp, got = c.api(http.MethodDelete, "/v1.0/me/drive/items/"+id, nil)
	c.want(resp, got, http.StatusNoContent)
	resp, got = c.do(http.MethodGet, location, nil, nil)
	c.want(resp, got, http.StatusNotFound)
}

// TestFake_FailNextAndTokens: FailNext answers the next request of a method
// with its status, code and Retry-After, in order, or drops it; the API
// refuses a missing or refused token with 401, while uploadUrls ignore
// AcceptToken; AcceptToken's callback may call the fake.
func TestFake_FailNextAndTokens(t *testing.T) {
	c := newClient(t)
	c.f.FailNext("drive.get", http.StatusTooManyRequests, "activityLimitReached", 12*time.Second)
	c.f.FailNext("drive.get", http.StatusServiceUnavailable, "serviceNotAvailable", 0)
	resp, body := c.api(http.MethodGet, "/v1.0/me/drive?$select=quota", nil)
	c.want(resp, body, http.StatusTooManyRequests)
	if resp.Header.Get("Retry-After") != "12" || !strings.Contains(string(body), `"code":"activityLimitReached"`) {
		t.Errorf("the injected 429: Retry-After %q, body %s", resp.Header.Get("Retry-After"), body)
	}
	resp, body = c.api(http.MethodGet, "/v1.0/me/drive?$select=quota", nil)
	c.want(resp, body, http.StatusServiceUnavailable)
	resp, body = c.api(http.MethodGet, "/v1.0/me/drive?$select=quota", nil)
	c.want(resp, body, http.StatusOK)
	c.f.FailNext("drive.get", Reset, "", 0)
	// A new connection: Go's transport sends a GET again when a reused
	// connection drops it.
	fresh := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	if resp, err := fresh.Get(c.srv.URL + "/v1.0/me/drive?$select=quota"); err == nil {
		_ = resp.Body.Close()
		t.Errorf("a Reset answered HTTP %d, want no answer", resp.StatusCode)
	}

	var blocked atomic.Bool
	c.f.AcceptToken(func(tok string) bool {
		got := make(chan struct{})
		go func() {
			defer close(got)
			_ = c.f.Counts()
		}()
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			blocked.Store(true)
		}
		return tok == "tok-good"
	})
	for _, tc := range []struct {
		token  string
		status int
	}{{"", http.StatusUnauthorized}, {"tok-bad", http.StatusUnauthorized}, {"tok-good", http.StatusOK}} {
		c.token = tc.token
		resp, body := c.api(http.MethodPost, "/v1.0/me/drive/special/approot:/a.bin:/createUploadSession", map[string]any{"item": map[string]any{}})
		c.want(resp, body, tc.status)
	}
	if blocked.Load() {
		t.Error("the callback's Counts() waited 5 s: AcceptToken runs under the fake's lock")
	}
	u := c.session("b.bin", "replace")
	resp, body = c.put(u, pattern(10, 1), 0, 10)
	c.want(resp, body, http.StatusCreated)
	if n := c.f.Counts()["unknown"]; n != 0 {
		t.Errorf("%d unknown requests", n)
	}
	resp, body = c.api(http.MethodPost, "/v1.0/me/drive/items/x/children", map[string]any{"name": "d", "folder": map[string]any{}})
	c.want(resp, body, http.StatusBadRequest)
	if n := c.f.Counts()["unknown"]; n != 1 {
		t.Errorf("%d unknown requests after a folder create, want 1", n)
	}
}

// TestFake_URLsUnderPrefix: served under a mux prefix, as fakeproviders
// serves it (Task 7), the uploadUrl and the download URL keep the prefix.
func TestFake_URLsUnderPrefix(t *testing.T) {
	f := New(Options{})
	mux := http.NewServeMux()
	mux.Handle("/graph/", http.StripPrefix("/graph", f.Handler()))
	srv := httptest.NewServer(guard(t, mux))
	t.Cleanup(srv.Close)
	c := &client{t: t, f: f, srv: srv, token: "tok-1"}
	resp, body := c.do(http.MethodPost, srv.URL+"/graph/v1.0/me/drive/special/approot:/a.bin:/createUploadSession",
		map[string]string{"Authorization": "Bearer tok-1"}, []byte(`{"item":{"@microsoft.graph.conflictBehavior":"replace"}}`))
	c.want(resp, body, http.StatusOK)
	var s struct {
		UploadURL string `json:"uploadUrl"`
	}
	_ = json.Unmarshal(body, &s)
	if !strings.HasPrefix(s.UploadURL, srv.URL+"/graph/up/") {
		t.Fatalf("uploadUrl %q, want it under %s/graph/up/", s.UploadURL, srv.URL)
	}
	resp, body = c.put(s.UploadURL, pattern(10, 1), 0, 10)
	c.want(resp, body, http.StatusCreated)
	var it struct{ ID string }
	_ = json.Unmarshal(body, &it)
	resp, body = c.do(http.MethodGet, srv.URL+"/graph/v1.0/me/drive/items/"+it.ID+"?$select=@microsoft.graph.downloadUrl",
		map[string]string{"Authorization": "Bearer tok-1"}, nil)
	c.want(resp, body, http.StatusOK)
	if !strings.Contains(string(body), srv.URL+"/graph/download/") {
		t.Errorf("downloadUrl %s, want it under %s/graph/download/", body, srv.URL)
	}
}

// TestFake_ResetDropsTheConnection: Reset on a fragment drops it with no
// answer, and the session keeps the bytes it held.
func TestFake_ResetDropsTheConnection(t *testing.T) {
	c := newClient(t)
	data := pattern(2*fragment, 5)
	u := c.session("a.bin", "replace")
	c.f.FailNext("upload.put", Reset, "", 0)
	req, _ := http.NewRequest(http.MethodPut, u, bytes.NewReader(data[:fragment]))
	req.Header.Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", fragment-1, len(data)))
	resp, err := c.srv.Client().Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("a reset fragment answered HTTP %d, want no answer", resp.StatusCode)
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Errorf("the reset was a timeout: %v", err)
	}
	resp2, body := c.do(http.MethodGet, u, nil, nil)
	c.want(resp2, body, http.StatusOK)
	if got := next(t, body); !reflect.DeepEqual(got, []string{"0-"}) {
		t.Errorf("nextExpectedRanges after a reset %q, want 0-", got)
	}
}
