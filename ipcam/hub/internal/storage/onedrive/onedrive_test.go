package onedrive

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/outbox"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakegraph "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/graph"
)

const (
	kib = 1 << 10
	mib = 1 << 20
)

// tokenSource hands out its current token, or its error.
type tokenSource struct {
	mu    sync.Mutex
	token string
	err   error
}

func (s *tokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return &oauth2.Token{AccessToken: s.token, TokenType: "Bearer"}, nil
}

func (s *tokenSource) set(token string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token, s.err = token, err
}

// request is what the test's server saw of one request.
type request struct {
	Method, Path, Query       string
	ContentRange, Range, Auth string
	Length                    int64  // the body's bytes
	Body                      []byte // the body of a request that is not a fragment
}

// testEnv is one test's fake Graph behind an httptest server and its
// tokens. The server records each request, then runs hook, when set, before
// the fake; serve, when set, answers the requests for which it returns true
// in the fake's place.
type testEnv struct {
	t      *testing.T
	fake   *fakegraph.Fake
	srv    *httptest.Server
	tokens *tokenSource

	mu    sync.Mutex
	log   []request
	hook  func(*http.Request)
	serve func(http.ResponseWriter, *http.Request) bool
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	e := &testEnv{t: t, fake: fakegraph.New(fakegraph.Options{}), tokens: &tokenSource{token: "tok-1"}}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := request{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			ContentRange: r.Header.Get("Content-Range"), Range: r.Header.Get("Range"), Auth: r.Header.Get("Authorization"),
			Length: r.ContentLength,
		}
		if !strings.HasPrefix(r.URL.Path, "/up/") {
			rec.Body, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(rec.Body))
		}
		e.mu.Lock()
		e.log = append(e.log, rec)
		hook, serve := e.hook, e.serve
		e.mu.Unlock()
		if hook != nil {
			hook(r)
		}
		if serve != nil && serve(w, r) {
			return
		}
		// A panic of the fake fails the test: the server alone would only
		// log it and drop the connection, which the driver maps to
		// ErrTransient.
		defer func() {
			if p := recover(); p != nil {
				if p != http.ErrAbortHandler {
					t.Errorf("the fake panicked serving %s %s: %v", r.Method, r.URL.Path, p)
				}
				panic(http.ErrAbortHandler)
			}
		}()
		e.fake.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *testEnv) setHook(hook func(*http.Request)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hook = hook
}

func (e *testEnv) setServe(serve func(http.ResponseWriter, *http.Request) bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.serve = serve
}

// requests returns the requests that the server saw.
func (e *testEnv) requests() []request {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]request(nil), e.log...)
}

// options are the Options of a driver of the env's fake, as ipcam-hub builds
// them for a fake: a plain client and the token source.
func (e *testEnv) options() Options {
	return Options{DestID: "dst-od", HubID: "H", HTTPClient: e.srv.Client(), Tokens: e.tokens, BaseURL: e.srv.URL}
}

// driver returns a new driver of the env's fake, after change changed its
// Options.
func (e *testEnv) driver(change ...func(*Options)) *OneDrive {
	e.t.Helper()
	opts := e.options()
	for _, c := range change {
		c(&opts)
	}
	d, err := New(opts)
	if err != nil {
		e.t.Fatalf("New: %v", err)
	}
	return d
}

// files returns the fake's live files, without its folders.
func (e *testEnv) files() []fakegraph.Object {
	var out []fakegraph.Object
	for _, o := range e.fake.Objects() {
		if !o.Folder {
			out = append(out, o)
		}
	}
	return out
}

// describe names objects for a failure message, with their sizes and not
// their bytes.
func describe(objects []fakegraph.Object) string {
	parts := make([]string, len(objects))
	for i, o := range objects {
		parts[i] = fmt.Sprintf("%s %q (%d bytes)", o.ID, o.Path, len(o.Data))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// calls returns the fake's counts that grew since before.
func calls(after, before map[string]int) map[string]int {
	out := map[string]int{}
	for method, n := range after {
		if n != before[method] {
			out[method] = n - before[method]
		}
	}
	return out
}

func randomBytes(n int, seed byte) []byte {
	b := make([]byte, n)
	_, _ = rand.NewChaCha8([32]byte{seed}).Read(b)
	return b
}

func quickXorOf(t *testing.T, data []byte) string {
	t.Helper()
	sum, err := storage.Digest(storage.HashQuickXor, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

// sendFragments sends data from off to its end through st, as the outbox
// does: fragments of the driver's PartSize, each from the offset that the
// last one confirmed.
func sendFragments(t *testing.T, d *OneDrive, st *storage.UploadState, data []byte, off int64) {
	t.Helper()
	part := d.Capabilities().PartSize
	for off < int64(len(data)) {
		n := min(part, int64(len(data))-off)
		if err := d.PutChunk(t.Context(), st, off, bytes.NewReader(data[off:off+n]), n); err != nil {
			t.Fatalf("PutChunk at %d: %v", off, err)
		}
		if st.Offset != off+n {
			t.Fatalf("after the fragment at %d of %d bytes the offset is %d, want %d", off, n, st.Offset, off+n)
		}
		off = st.Offset
	}
}

// uploadAll uploads data to key through d and commits it.
func uploadAll(t *testing.T, d *OneDrive, key storage.ObjectKey, data []byte) (storage.ObjectRef, storage.UploadState) {
	t.Helper()
	st, err := d.BeginUpload(t.Context(), key, int64(len(data)), storage.Meta{ContentType: "video/mp4"})
	if err != nil {
		t.Fatalf("BeginUpload(%s): %v", key, err)
	}
	sendFragments(t, d, &st, data, 0)
	ref, err := d.Commit(t.Context(), &st)
	if err != nil {
		t.Fatalf("Commit(%s): %v", key, err)
	}
	return ref, st
}

func goldenCaps() storage.Caps {
	return storage.Caps{
		Resumable: storage.ResumeUploadURL, ChunkAlign: 320 * kib, PartSize: 10 * mib, MaxRequest: 60*mib - 1,
		Playback: storage.LinkPreauthShort, RangeRead: true, Delete: storage.DeleteRecycleBin,
		Quota: storage.QuotaAccount, Hashes: []storage.HashKind{storage.HashQuickXor},
	}
}

func TestOneDrive_CapsGolden(t *testing.T) {
	e := newEnv(t)
	live := e.options()
	live.BaseURL = ""
	for name, opts := range map[string]Options{"the live API": live, "the fake": e.options()} {
		d, err := New(opts)
		if err != nil {
			t.Fatalf("%s: New: %v", name, err)
		}
		if got := d.Capabilities(); !reflect.DeepEqual(got, goldenCaps()) {
			t.Errorf("%s: Capabilities() = %+v, want %+v", name, got, goldenCaps())
		}
		if d.Kind() != storage.KindOneDrivePersonal {
			t.Errorf("%s: Kind() = %s, want %s", name, d.Kind(), storage.KindOneDrivePersonal)
		}
	}
	if Authority != "consumers" || Scope != "Files.ReadWrite.AppFolder offline_access" {
		t.Errorf("Authority %q and Scope %q, want consumers and Files.ReadWrite.AppFolder offline_access (P36)", Authority, Scope)
	}
	if n := len(e.requests()); n != 0 {
		t.Errorf("New made %d requests", n)
	}
}

// TestOneDrive_FragmentsAndNoAuthHeader (STO-08): a 25 MiB upload sends its
// fragments in order, each under 60 MiB and each but the last a multiple of
// 320 KiB, with no Authorization on the uploadUrl, while every API request
// carries the bearer token; a 429 with Retry-After: 12 is
// ErrRateLimited{12 s} at once; no error's text holds the uploadUrl, a reset
// connection on a fragment included.
func TestOneDrive_FragmentsAndNoAuthHeader(t *testing.T) {
	e := newEnv(t)
	d := e.driver()
	data := randomBytes(25*mib, 1)
	ref, st := uploadAll(t, d, "hub/c1/2026-10-17/a.mp4", data)
	if ref.Hash != quickXorOf(t, data) || ref.Size != int64(len(data)) {
		t.Errorf("Commit = %+v, want the bytes' size and quickXorHash", ref)
	}
	var next int64
	fragments := 0
	for _, r := range e.requests() {
		if !strings.HasPrefix(r.Path, "/up/") {
			if r.Auth != "Bearer tok-1" {
				t.Errorf("%s %s carried Authorization %q, want Bearer tok-1", r.Method, r.Path, r.Auth)
			}
			continue
		}
		if r.Auth != "" {
			t.Errorf("%s on the uploadUrl carried Authorization %q, want none", r.Method, r.Auth)
		}
		if r.Method != http.MethodPut {
			continue
		}
		fragments++
		var first, last, total int64
		if _, err := fmt.Sscanf(r.ContentRange, "bytes %d-%d/%d", &first, &last, &total); err != nil {
			t.Fatalf("Content-Range %q: %v", r.ContentRange, err)
		}
		n := last - first + 1
		switch {
		case first != next || total != int64(len(data)) || n != r.Length:
			t.Errorf("fragment %d is %q of %d bytes, want bytes from %d of %d", fragments, r.ContentRange, r.Length, next, len(data))
		case n >= 60*mib:
			t.Errorf("fragment %d holds %d bytes, want under 60 MiB", fragments, n)
		case last+1 < total && n%(320*kib) != 0:
			t.Errorf("fragment %d holds %d bytes, not a multiple of 320 KiB", fragments, n)
		}
		next = last + 1
	}
	if fragments != 3 || next != int64(len(data)) {
		t.Errorf("%d fragments up to byte %d, want 3 fragments of 10 MiB, 10 MiB and 5 MiB", fragments, next)
	}

	leaks := func(err error) {
		t.Helper()
		for _, leak := range []string{st.Session, e.srv.URL, "/up/", "fake-session", "tok-1"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("the error's text %q holds %q", err, leak)
			}
		}
	}
	st, err := d.BeginUpload(t.Context(), "hub/c1/2026-10-17/b.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	e.fake.FailNext("upload.put", http.StatusTooManyRequests, "activityLimitReached", 12*time.Second)
	start := time.Now()
	err = d.PutChunk(t.Context(), &st, 0, bytes.NewReader(data[:10*mib]), 10*mib)
	var limited storage.ErrRateLimited
	if !errors.As(err, &limited) || limited.RetryAfter != 12*time.Second || time.Since(start) > 5*time.Second {
		t.Errorf("a 429 with Retry-After: 12 gave %v after %s, want ErrRateLimited{RetryAfter: 12s} at once", err, time.Since(start))
	}
	if err != nil {
		leaks(err)
	}
	e.fake.FailNext("upload.put", fakegraph.Reset, "", 0)
	err = d.PutChunk(t.Context(), &st, 0, bytes.NewReader(data[:10*mib]), 10*mib)
	if !errors.Is(err, storage.ErrTransient) {
		t.Errorf("a reset fragment gave %v, want ErrTransient", err)
	}
	if err != nil {
		leaks(err)
	}
}

// TestOneDrive_PlainClientOnUploadURL (P27): the driver as ipcam-hub builds
// it, a plain client and the TokenSource, uploads; a client that adds the
// bearer token to every request, as oauth2.NewClient's does, gets the
// strict fake's 401 on the uploadUrl.
func TestOneDrive_PlainClientOnUploadURL(t *testing.T) {
	e := newEnv(t)
	d := e.driver(func(o *Options) { o.PartSize = 320 * kib })
	if d.client != e.srv.Client() {
		t.Fatal("the driver does not use the HTTPClient it was given")
	}
	data := randomBytes(700*kib, 1)
	uploadAll(t, d, "a.mp4", data)

	bearing := &http.Client{Transport: &oauth2.Transport{Source: e.tokens, Base: e.srv.Client().Transport}}
	d = e.driver(func(o *Options) { o.PartSize, o.HTTPClient = 320*kib, bearing })
	st, err := d.BeginUpload(t.Context(), "b.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatalf("BeginUpload through the bearing client: %v", err)
	}
	err = d.PutChunk(t.Context(), &st, 0, bytes.NewReader(data[:320*kib]), 320*kib)
	if !errors.Is(err, storage.ErrAuthExpired) || !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("a fragment through the bearing client: %v, want the uploadUrl's 401", err)
	}
	if files := e.files(); len(files) != 1 {
		t.Errorf("files at the fake %s, want the plain client's only", describe(files))
	}
}

// liveGraph is a transport that answers the live API's requests, to
// graph.microsoft.com, in Graph's place, with an uploadUrl and a download
// URL of its scheme, and records the host of every request. Any other host
// is an error: the test has no network.
type liveGraph struct {
	mu     sync.Mutex
	scheme string
	hosts  []string
}

func (g *liveGraph) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
		_ = req.Body.Close()
	}
	g.mu.Lock()
	g.hosts = append(g.hosts, req.URL.Host)
	scheme := g.scheme
	g.mu.Unlock()
	answer := func(status int, body string) (*http.Response, error) {
		return &http.Response{
			StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), Request: req,
		}, nil
	}
	switch {
	case req.URL.Host != "graph.microsoft.com":
		return nil, errors.New("this test has no network")
	case strings.HasSuffix(req.URL.Path, ":/createUploadSession"):
		return answer(http.StatusOK, `{"uploadUrl":"`+scheme+`://up.example/s1","nextExpectedRanges":["0-"]}`)
	case strings.HasPrefix(req.URL.Path, "/v1.0/me/drive/items/"):
		return answer(http.StatusOK, `{"id":"i1","@microsoft.graph.downloadUrl":"`+scheme+`://dl.example/i1"}`)
	}
	return answer(http.StatusNotFound, `{"error":{"code":"itemNotFound"}}`)
}

// TestOneDrive_LiveAPITakesHTTPSURLsOnly (Task 6 review minor 6): with the
// live API (no BaseURL), an uploadUrl or a download URL that is not https is
// refused: createUploadSession's is ErrTransient, a stored session's is
// ErrSessionExpired with no request sent to it, and a playback link falls
// back to the hub's proxy. https ones are taken; the fakes' http ones only
// with BaseURL set (the other tests).
func TestOneDrive_LiveAPITakesHTTPSURLsOnly(t *testing.T) {
	graph := &liveGraph{scheme: "http"}
	d, err := New(Options{DestID: "dst-od", HubID: "H", HTTPClient: &http.Client{Transport: graph}, Tokens: &tokenSource{token: "tok-1"}})
	if err != nil {
		t.Fatal(err)
	}
	ref := storage.ObjectRef{ID: "i1", Key: "a.mp4", Size: 10}
	if _, err := d.BeginUpload(t.Context(), "a.mp4", 10, storage.Meta{}); !errors.Is(err, storage.ErrTransient) {
		t.Errorf("BeginUpload with an http uploadUrl: %v, want ErrTransient", err)
	}
	if link, err := d.PlaybackURL(t.Context(), ref, time.Minute); !errors.Is(err, storage.ErrProxyRequired) {
		t.Errorf("PlaybackURL with an http download URL = %q, %v, want ErrProxyRequired", link, err)
	}
	st := storage.UploadState{Key: "a.mp4", Size: 10, Session: "http://up.example/s1"}
	if err := d.PutChunk(t.Context(), &st, 0, strings.NewReader("0123456789"), 10); !errors.Is(err, storage.ErrSessionExpired) {
		t.Errorf("PutChunk to an http session: %v, want ErrSessionExpired", err)
	}
	if _, err := d.Resume(t.Context(), &st); !errors.Is(err, storage.ErrSessionExpired) {
		t.Errorf("Resume of an http session: %v, want ErrSessionExpired", err)
	}
	if err := d.Abort(t.Context(), &st); err != nil {
		t.Errorf("Abort of an http session: %v, want nil", err)
	}
	graph.mu.Lock()
	for _, host := range graph.hosts {
		if host != "graph.microsoft.com" {
			t.Errorf("a request went to %s, the host of an http URL", host)
		}
	}
	graph.scheme = "https"
	graph.mu.Unlock()
	st, err = d.BeginUpload(t.Context(), "a.mp4", 10, storage.Meta{})
	if err != nil || st.Session != "https://up.example/s1" {
		t.Errorf("BeginUpload with an https uploadUrl: session %q, %v, want it", st.Session, err)
	}
	if link, err := d.PlaybackURL(t.Context(), ref, time.Minute); err != nil || link != "https://dl.example/i1" {
		t.Errorf("PlaybackURL with an https download URL = %q, %v, want it", link, err)
	}
}

// TestOneDrive_ResumeFromNextExpectedRanges: an upload cut after two
// fragments resumes, in a new driver from the session alone, at 2 × 10 MiB,
// from the uploadUrl's nextExpectedRanges, and commits once.
func TestOneDrive_ResumeFromNextExpectedRanges(t *testing.T) {
	e := newEnv(t)
	data := randomBytes(25*mib, 2)
	first := e.driver()
	st, err := first.BeginUpload(t.Context(), "hub/c1/2026-10-17/a.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	for off := int64(0); off < 20*mib; off += 10 * mib {
		if err := first.PutChunk(t.Context(), &st, off, bytes.NewReader(data[off:off+10*mib]), 10*mib); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var restored storage.UploadState
	if err := json.Unmarshal(saved, &restored); err != nil {
		t.Fatal(err)
	}
	restored.Offset = 0 // the catalog may hold an older offset: the session's answer decides
	second := e.driver()
	next, err := second.Resume(t.Context(), &restored)
	if err != nil || next != 20*mib || restored.Offset != 20*mib {
		t.Fatalf("Resume = %d, %v (offset %d), want 20 MiB", next, err, restored.Offset)
	}
	sendFragments(t, second, &restored, data, next)
	ref, err := second.Commit(t.Context(), &restored)
	if err != nil {
		t.Fatal(err)
	}
	files := e.files()
	if len(files) != 1 || files[0].ID != ref.ID || !bytes.Equal(files[0].Data, data) || ref.Hash != quickXorOf(t, data) {
		t.Errorf("files at the fake %s and Commit %+v, want one item of the bytes", describe(files), ref)
	}
	if c := e.fake.Counts(); c["createUploadSession"] != 1 || c["upload.status"] != 1 || c["upload.put"] != 3 {
		t.Errorf("the fake's counts %v, want one session, one status query and three fragments", c)
	}
}

// TestOneDrive_ResumeDetectsCompletedSessionAfter404: Graph forgets a session
// once it completes, so after the last fragment the uploadUrl answers 404,
// and Resume finds the item of Size bytes at the key: it returns Size with no
// new session, and Commit returns that item. With no item at the key, or one
// of another size, Resume is ErrSessionExpired. Every session asks for
// conflictBehavior replace.
func TestOneDrive_ResumeDetectsCompletedSessionAfter404(t *testing.T) {
	e := newEnv(t)
	part := func(o *Options) { o.PartSize = 320 * kib }
	data := randomBytes(600*kib, 3)
	d := e.driver(part)
	st, err := d.BeginUpload(t.Context(), "c1/a.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	sendFragments(t, d, &st, data, 0)
	restored := storage.UploadState{Key: st.Key, Size: st.Size, Session: st.Session, Offset: 320 * kib}
	again := e.driver(part)
	before := e.fake.Counts()
	next, err := again.Resume(t.Context(), &restored)
	if err != nil || next != int64(len(data)) || restored.Offset != int64(len(data)) {
		t.Fatalf("Resume = %d, %v, want the size %d", next, err, len(data))
	}
	if got, want := calls(e.fake.Counts(), before), map[string]int{"upload.status": 1, "items.getByPath": 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("Resume made the calls %v, want %v: the uploadUrl's 404, then the item at the key", got, want)
	}
	for _, r := range e.requests() {
		if r.Path == "/v1.0/me/drive/special/approot:/c1/a.mp4" && r.Query != "$select=id,size,file" {
			t.Errorf("the item at the key was asked with %q, want $select=id,size,file", r.Query)
		}
	}
	ref, err := again.Commit(t.Context(), &restored)
	files := e.files()
	if err != nil || len(files) != 1 || ref.ID != files[0].ID || ref.Size != int64(len(data)) || ref.Hash != quickXorOf(t, data) {
		t.Fatalf("Commit = %+v, %v with files %s, want the one item of the bytes", ref, err, describe(files))
	}
	if n := e.fake.Counts()["createUploadSession"]; n != 1 {
		t.Errorf("%d sessions, want 1", n)
	}

	// No item at the key: the upload completed, and the item was deleted.
	if _, err := d.Delete(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	if _, err := e.driver(part).Resume(t.Context(), &restored); !errors.Is(err, storage.ErrSessionExpired) {
		t.Errorf("Resume with no item at the key: %v, want ErrSessionExpired", err)
	}
	// An item of another size at the key: a later upload replaced it.
	_, st = uploadAll(t, d, "c1/b.mp4", data)
	uploadAll(t, d, "c1/b.mp4", data[:500*kib])
	if _, err := e.driver(part).Resume(t.Context(), &st); !errors.Is(err, storage.ErrSessionExpired) {
		t.Errorf("Resume with an item of another size at the key: %v, want ErrSessionExpired", err)
	}

	sessions := 0
	for _, r := range e.requests() {
		if strings.HasSuffix(r.Path, ":/createUploadSession") {
			sessions++
			var body, want any
			_ = json.Unmarshal(r.Body, &body)
			_ = json.Unmarshal([]byte(`{"item":{"@microsoft.graph.conflictBehavior":"replace"}}`), &want)
			if !reflect.DeepEqual(body, want) {
				t.Errorf("createUploadSession's body %s, want exactly conflictBehavior replace", r.Body)
			}
		}
	}
	if sessions != 3 {
		t.Errorf("%d session requests, want 3", sessions)
	}
}

// TestOneDrive_QuickXorVerify (P34): Commit's quickXorHash and size are the
// bytes' as storage.Digest computes them, which the fake's own QuickXor
// gives too; Stat reads the same.
func TestOneDrive_QuickXorVerify(t *testing.T) {
	e := newEnv(t)
	d := e.driver(func(o *Options) { o.PartSize = 320 * kib })
	data := randomBytes(3*320*kib+7, 4)
	ref, _ := uploadAll(t, d, "c1/a.mp4", data)
	want := quickXorOf(t, data)
	files := e.files()
	if ref.Hash != want || ref.HashKind != storage.HashQuickXor || ref.Size != int64(len(data)) || ref.Key != "c1/a.mp4" ||
		len(files) != 1 || files[0].QuickXor != want || files[0].ID != ref.ID {
		t.Errorf("Commit = %+v with files %s, want %d bytes with quickXorHash %s", ref, describe(files), len(data), want)
	}
	info, err := d.Stat(t.Context(), ref)
	if err != nil || info.Ref != ref || info.ModTimeMs == 0 {
		t.Errorf("Stat = %+v, %v, want %+v and a time", info, err, ref)
	}
}

// TestOneDrive_CommitReadsAMissingHash: when the last fragment's item comes
// without its quickXorHash (Graph's answer is the item's default set, and the
// hash may come later), Commit reads it once (items.get, $select=id,size,file).
func TestOneDrive_CommitReadsAMissingHash(t *testing.T) {
	e := newEnv(t)
	d := e.driver(func(o *Options) { o.PartSize = 320 * kib })
	e.setServe(func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodPut || !strings.HasPrefix(r.URL.Path, "/up/") {
			return false
		}
		rec := httptest.NewRecorder()
		e.fake.Handler().ServeHTTP(rec, r)
		body := rec.Body.Bytes()
		if rec.Code == http.StatusCreated || rec.Code == http.StatusOK {
			var it map[string]any
			_ = json.Unmarshal(body, &it)
			delete(it, "file")
			body, _ = json.Marshal(it)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rec.Code)
		_, _ = w.Write(body)
		return true
	})
	data := randomBytes(400*kib, 11)
	ref, st := uploadAll(t, d, "c1/a.mp4", data)
	if ref.Hash != quickXorOf(t, data) || ref.HashKind != storage.HashQuickXor || ref.Size != int64(len(data)) {
		t.Errorf("Commit = %+v, want the bytes' quickXorHash and size", ref)
	}
	if want := (storage.CallCounts{"createUploadSession": 1, "upload.put": 2, "items.get": 1}); !reflect.DeepEqual(st.Calls, want) {
		t.Errorf("the upload's calls %v, want %v", st.Calls, want)
	}
	for _, r := range e.requests() {
		if r.Path == "/v1.0/me/drive/items/"+ref.ID && r.Query != "$select=id,size,file" {
			t.Errorf("Commit asked the item with %q, want $select=id,size,file", r.Query)
		}
	}
}

// TestOneDrive_DeleteIsRecycled_QuotaInBin: Delete moves the item to the
// recycle bin (DeletedRecycled), whose bytes keep occupying the quota: InBin
// grows by the object's size and Free does not, until the bin empties.
func TestOneDrive_DeleteIsRecycled_QuotaInBin(t *testing.T) {
	e := newEnv(t)
	d := e.driver(func(o *Options) { o.PartSize = 320 * kib })
	data := randomBytes(700*kib, 5)
	ref, _ := uploadAll(t, d, "c1/a.mp4", data)
	before, err := d.Quota(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := d.Delete(t.Context(), ref)
	if err != nil || outcome != storage.DeletedRecycled {
		t.Fatalf("Delete = %v, %v, want DeletedRecycled", outcome, err)
	}
	after, err := d.Quota(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.InBin != before.InBin+int64(len(data)) || after.Free != before.Free || after.Used != before.Used {
		t.Errorf("the quota after Delete %+v (before %+v), want %d more in the bin and the same free bytes", after, before, len(data))
	}
	if _, err := d.Stat(t.Context(), ref); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat of the deleted item: %v, want ErrNotFound", err)
	}
	e.fake.EmptyBin()
	emptied, err := d.Quota(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if emptied.InBin != 0 || emptied.Free != after.Free+int64(len(data)) {
		t.Errorf("the quota after EmptyBin %+v, want an empty bin and %d more free bytes", emptied, len(data))
	}
}

// TestOneDrive_QuotaFull: 507, or the code quotaLimitReached, is
// ErrQuotaFull, whether the fake injects it or a fragment passes its quota.
func TestOneDrive_QuotaFull(t *testing.T) {
	e := newEnv(t)
	d := e.driver(func(o *Options) { o.PartSize = 320 * kib })
	data := randomBytes(700*kib, 6)
	e.fake.FailNext("createUploadSession", http.StatusInsufficientStorage, "", 0)
	if _, err := d.BeginUpload(t.Context(), "a.mp4", int64(len(data)), storage.Meta{}); !errors.Is(err, storage.ErrQuotaFull) {
		t.Errorf("a session answered 507: %v, want ErrQuotaFull", err)
	}
	st, err := d.BeginUpload(t.Context(), "a.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	e.fake.FailNext("upload.put", http.StatusForbidden, "quotaLimitReached", 0)
	if err := d.PutChunk(t.Context(), &st, 0, bytes.NewReader(data[:320*kib]), 320*kib); !errors.Is(err, storage.ErrQuotaFull) {
		t.Errorf("a fragment answered quotaLimitReached: %v, want ErrQuotaFull", err)
	}
	e.fake.SetQuota(500*kib, 0)
	if err := d.PutChunk(t.Context(), &st, 0, bytes.NewReader(data[:320*kib]), 320*kib); err != nil {
		t.Fatal(err)
	}
	err = d.PutChunk(t.Context(), &st, 320*kib, bytes.NewReader(data[320*kib:]), int64(len(data))-320*kib)
	if !errors.Is(err, storage.ErrQuotaFull) || !strings.Contains(err.Error(), "507") {
		t.Errorf("a fragment past the quota: %v, want the fake's 507 as ErrQuotaFull", err)
	}
}

// TestOneDrive_QuotaSelectOnly (P45): the drive is asked for its quota
// only, exactly $select=quota, never its owner, and Quota reports it.
func TestOneDrive_QuotaSelectOnly(t *testing.T) {
	e := newEnv(t)
	e.fake.SetQuota(1000, 100)
	q, err := e.driver().Quota(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var queries []string
	for _, r := range e.requests() {
		if r.Path == "/v1.0/me/drive" {
			queries = append(queries, r.Query)
		}
	}
	if len(queries) != 1 || queries[0] != "$select=quota" {
		t.Errorf("drive queries %q, want exactly $select=quota", queries)
	}
	if want := (storage.Quota{Total: 1000, Used: 100, Free: 900}); q != want {
		t.Errorf("Quota() = %+v, want %+v", q, want)
	}
}

// TestOneDrive_Probe (P35): Probe writes .ipcam/<HubID>/README.txt under the
// app folder, writes 1 MiB, reads a range of it, deletes it into the recycle
// bin and reads the quota, with no folder create call; a second Probe
// replaces the README.
func TestOneDrive_Probe(t *testing.T) {
	e := newEnv(t)
	d := e.driver()
	report, err := d.Probe(t.Context())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !report.Writable || !report.RangeRead || !report.Deletable || len(report.Warnings) != 0 || report.Quota.InBin != 1*mib {
		t.Errorf("Probe's report %+v, want writable, range-readable and deletable, no warning, and 1 MiB in the bin", report)
	}
	// One probe's calls: two sessions (the README and the 1 MiB object),
	// one fragment each, a range read through its redirect, a delete and a
	// quota read.
	each := map[string]int{"createUploadSession": 2, "upload.put": 2, "items.content": 1, "download": 1, "items.delete": 1, "drive.get": 1}
	check := func(probes int) {
		t.Helper()
		files := e.files()
		if len(files) != 1 || files[0].Path != ".ipcam/H/README.txt" || string(files[0].Data) != storage.ProbeReadme {
			t.Errorf("after %d probes the files are %s, want only .ipcam/H/README.txt with the fixed text", probes, describe(files))
		}
		bin := e.fake.Bin()
		if len(bin) != probes || bin[probes-1].Path != ".ipcam/H/probe.bin" || len(bin[probes-1].Data) != 1*mib {
			t.Errorf("after %d probes the recycle bin holds %s, want one 1 MiB probe.bin each", probes, describe(bin))
		}
		got, want := e.fake.Counts(), map[string]int{}
		for method, n := range each {
			want[method] = n * probes
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("after %d probes the fake's counts %v, want %v: no folder create, no other call", probes, got, want)
		}
	}
	check(1)
	if _, err := d.Probe(t.Context()); err != nil {
		t.Fatalf("the second Probe: %v", err)
	}
	check(2)
}

// TestOneDrive_ErrorMapping: every status and code maps to its typed error,
// and no error's text holds the uploadUrl, a download URL, a request URL or
// a token, a reset connection included. On an uploadUrl, 404 and 410 are
// ErrSessionExpired and 416 is ErrTransient; off one, 416 is ErrPermanent
// as any other 4xx (C2-R12).
func TestOneDrive_ErrorMapping(t *testing.T) {
	type step func(t *testing.T, e *testEnv, d *OneDrive, st *storage.UploadState) func() error
	data := randomBytes(700*kib, 7)
	begin := func(t *testing.T, d *OneDrive) storage.UploadState {
		t.Helper()
		st, err := d.BeginUpload(t.Context(), "c1/a.mp4", int64(len(data)), storage.Meta{})
		if err != nil {
			t.Fatalf("BeginUpload: %v", err)
		}
		return st
	}
	committed := func(t *testing.T, d *OneDrive) storage.ObjectRef {
		t.Helper()
		ref, _ := uploadAll(t, d, "c1/a.mp4", data)
		return ref
	}
	var (
		opBegin step = func(t *testing.T, _ *testEnv, d *OneDrive, _ *storage.UploadState) func() error {
			return func() error {
				_, err := d.BeginUpload(t.Context(), "c1/a.mp4", int64(len(data)), storage.Meta{})
				return err
			}
		}
		opPut step = func(t *testing.T, _ *testEnv, d *OneDrive, st *storage.UploadState) func() error {
			*st = begin(t, d)
			return func() error { return d.PutChunk(t.Context(), st, 0, bytes.NewReader(data[:320*kib]), 320*kib) }
		}
		opResume step = func(t *testing.T, _ *testEnv, d *OneDrive, st *storage.UploadState) func() error {
			*st = begin(t, d)
			return func() error {
				_, err := d.Resume(t.Context(), st)
				return err
			}
		}
		// opResumeDone resumes a session that completed, which asks the item
		// at the key.
		opResumeDone step = func(t *testing.T, _ *testEnv, d *OneDrive, st *storage.UploadState) func() error {
			*st = begin(t, d)
			sendFragments(t, d, st, data, 0)
			return func() error {
				_, err := d.Resume(t.Context(), st)
				return err
			}
		}
		opStat step = func(t *testing.T, _ *testEnv, d *OneDrive, _ *storage.UploadState) func() error {
			ref := committed(t, d)
			return func() error {
				_, err := d.Stat(t.Context(), ref)
				return err
			}
		}
		opRead step = func(t *testing.T, _ *testEnv, d *OneDrive, _ *storage.UploadState) func() error {
			ref := committed(t, d)
			return func() error {
				r, err := d.OpenRange(t.Context(), ref, 0, 10)
				if err == nil {
					_, err = io.ReadAll(r)
					_ = r.Close()
				}
				return err
			}
		}
		opPlayback step = func(t *testing.T, _ *testEnv, d *OneDrive, _ *storage.UploadState) func() error {
			ref := committed(t, d)
			return func() error {
				_, err := d.PlaybackURL(t.Context(), ref, time.Minute)
				return err
			}
		}
		opDelete step = func(t *testing.T, _ *testEnv, d *OneDrive, _ *storage.UploadState) func() error {
			ref := committed(t, d)
			return func() error {
				_, err := d.Delete(t.Context(), ref)
				return err
			}
		}
		opAbort step = func(t *testing.T, _ *testEnv, d *OneDrive, st *storage.UploadState) func() error {
			*st = begin(t, d)
			return func() error { return d.Abort(t.Context(), st) }
		}
		opQuota step = func(t *testing.T, _ *testEnv, d *OneDrive, _ *storage.UploadState) func() error {
			return func() error {
				_, err := d.Quota(t.Context())
				return err
			}
		}
	)
	sentinel := "SENT" + "INEL-token-url"
	limit := func(d time.Duration) *time.Duration { return &d }
	for _, tc := range []struct {
		name       string
		method     string // the API method that fails, by FailNext
		status     int
		code       string
		retryAfter time.Duration
		times      int   // how many answers FailNext queues: 2 for a GET, which Go's transport sends again on a reused connection that drops
		tokenErr   error // instead of FailNext: the token source's error
		op         step
		want       error
		wantLimit  *time.Duration // ErrRateLimited's RetryAfter
	}{
		{name: "401", method: "drive.get", status: 401, code: "unauthenticated", op: opQuota, want: storage.ErrAuthExpired},
		{name: "401 on the uploadUrl", method: "upload.put", status: 401, code: "unauthenticated", op: opPut, want: storage.ErrAuthExpired},
		{name: "507", method: "createUploadSession", status: 507, code: "insufficientStorage", op: opBegin, want: storage.ErrQuotaFull},
		{name: "quotaLimitReached", method: "upload.put", status: 403, code: "quotaLimitReached", op: opPut, want: storage.ErrQuotaFull},
		{name: "429 with Retry-After 7", method: "items.get", status: 429, code: "activityLimitReached", retryAfter: 7 * time.Second, op: opStat, wantLimit: limit(7 * time.Second)},
		{name: "429 without Retry-After", method: "drive.get", status: 429, code: "activityLimitReached", op: opQuota, wantLimit: limit(0)},
		{name: "404 on a fragment", method: "upload.put", status: 404, code: "itemNotFound", op: opPut, want: storage.ErrSessionExpired},
		{name: "404 on the status, no item at the key", method: "upload.status", status: 404, code: "itemNotFound", op: opResume, want: storage.ErrSessionExpired},
		{name: "410 on a fragment", method: "upload.put", status: 410, code: "itemNotFound", op: opPut, want: storage.ErrSessionExpired},
		{name: "410 on the status", method: "upload.status", status: 410, code: "itemNotFound", op: opResume, want: storage.ErrSessionExpired},
		{name: "500 on the item at the key", method: "items.getByPath", status: 500, code: "generalException", op: opResumeDone, want: storage.ErrTransient},
		{name: "404 on an item", method: "items.get", status: 404, code: "itemNotFound", op: opStat, want: storage.ErrNotFound},
		{name: "404 on a delete", method: "items.delete", status: 404, code: "itemNotFound", op: opDelete, want: storage.ErrNotFound},
		{name: "404 on the playback link", method: "items.get", status: 404, code: "itemNotFound", op: opPlayback, want: storage.ErrNotFound},
		{name: "500 on a session", method: "createUploadSession", status: 500, code: "generalException", op: opBegin, want: storage.ErrTransient},
		{name: "503", method: "drive.get", status: 503, code: "serviceNotAvailable", op: opQuota, want: storage.ErrTransient},
		{name: "500 on Abort's cancel", method: "upload.cancel", status: 500, code: "generalException", op: opAbort, want: storage.ErrTransient},
		{name: "416 on a fragment", method: "upload.put", status: 416, code: "invalidRange", op: opPut, want: storage.ErrTransient},
		{name: "416 off an uploadUrl", method: "items.content", status: 416, code: "invalidRange", op: opRead, want: storage.ErrPermanent},
		{name: "400 on a fragment", method: "upload.put", status: 400, code: "invalidRange", op: opPut, want: storage.ErrPermanent},
		{name: "403 on a read", method: "items.content", status: 403, code: "accessDenied", op: opRead, want: storage.ErrPermanent},
		{name: "410 on the download URL", method: "download", status: 410, code: "", op: opRead, want: storage.ErrPermanent},
		{name: "reset on a fragment", method: "upload.put", status: fakegraph.Reset, op: opPut, want: storage.ErrTransient},
		{name: "reset on a session", method: "createUploadSession", status: fakegraph.Reset, op: opBegin, want: storage.ErrTransient},
		{name: "reset on the status", method: "upload.status", status: fakegraph.Reset, times: 2, op: opResume, want: storage.ErrTransient},
		{name: "reset on the download URL", method: "download", status: fakegraph.Reset, times: 2, op: opRead, want: storage.ErrTransient},
		{name: "reset on Abort's cancel", method: "upload.cancel", status: fakegraph.Reset, times: 2, op: opAbort, want: storage.ErrTransient},
		{name: "a token error wrapping ErrTransient", tokenErr: fmt.Errorf("token endpoint: %w", storage.ErrTransient), op: opQuota, want: storage.ErrTransient},
		{name: "a token error wrapping ErrAuthExpired", tokenErr: fmt.Errorf("invalid_grant: %w", storage.ErrAuthExpired), op: opQuota, want: storage.ErrAuthExpired},
		{name: "a plain token error", tokenErr: errors.New("oauth2: cannot fetch token"), op: opQuota, want: storage.ErrTransient},
		{name: "a token error quoting a URL", tokenErr: &url.Error{Op: "Post", URL: "https://token.invalid/?" + sentinel, Err: errors.New("connection refused")}, op: opQuota, want: storage.ErrTransient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			d := e.driver(func(o *Options) { o.PartSize = 320 * kib })
			var st storage.UploadState
			call := tc.op(t, e, d, &st)
			if tc.tokenErr != nil {
				e.tokens.set("", tc.tokenErr)
			} else {
				for range max(tc.times, 1) {
					e.fake.FailNext(tc.method, tc.status, tc.code, tc.retryAfter)
				}
			}
			err := call()
			var rl storage.ErrRateLimited
			switch {
			case err == nil:
				t.Fatal("no error")
			case tc.wantLimit != nil:
				if !errors.As(err, &rl) || rl.RetryAfter != *tc.wantLimit {
					t.Errorf("error %v, want ErrRateLimited{RetryAfter: %s}", err, *tc.wantLimit)
				}
			case !errors.Is(err, tc.want):
				t.Errorf("error %v, want %v", err, tc.want)
			}
			if tc.want == storage.ErrTransient && errors.Is(err, storage.ErrPermanent) {
				t.Errorf("error %v is ErrPermanent too", err)
			}
			text := err.Error()
			for _, leak := range []string{st.Session, e.srv.URL, "/up/", "/download/", "fake-session", "tok-1", sentinel} {
				if leak != "" && strings.Contains(text, leak) {
					t.Errorf("the error's text %q holds %q", text, leak)
				}
			}
			if tc.method != "" && tc.method != "download" && !strings.Contains(text, tc.method) {
				t.Errorf("the error's text %q does not name the method %s", text, tc.method)
			}
			if tc.status > 0 && !strings.Contains(text, fmt.Sprint(tc.status)) {
				t.Errorf("the error's text %q does not name the status %d", text, tc.status)
			}
			if tc.method != "" && e.fake.Counts()[tc.method] == 0 {
				t.Errorf("no %s call reached the fake", tc.method)
			}
		})
	}
}

// TestOneDrive_EmptyObjectRefused (C2-R12): an object holds one byte at
// least, as on every destination (an upload session takes no empty file):
// BeginUpload of 0 bytes and PutSmall of none are ErrPermanent before any
// call.
func TestOneDrive_EmptyObjectRefused(t *testing.T) {
	e := newEnv(t)
	d := e.driver()
	if st, err := d.BeginUpload(t.Context(), "c1/empty.mp4", 0, storage.Meta{}); !errors.Is(err, storage.ErrPermanent) {
		t.Errorf("BeginUpload of 0 bytes = %+v, %v; want ErrPermanent", st, err)
	}
	for _, data := range [][]byte{nil, {}} {
		if ref, err := d.PutSmall(t.Context(), "c1/empty.json", data); !errors.Is(err, storage.ErrPermanent) {
			t.Errorf("PutSmall of %#v = %+v, %v; want ErrPermanent", data, ref, err)
		}
	}
	if counts := e.fake.Counts(); len(counts) != 0 {
		t.Errorf("the refused writes sent %v; want no call", counts)
	}
}

// TestOneDrive_PartSizeOverrideOnlyWithBaseURL: another PartSize is taken
// only with BaseURL set, as a multiple of 320 KiB under 60 MiB, and the
// fragments follow it; New refuses Options that lack what it needs.
func TestOneDrive_PartSizeOverrideOnlyWithBaseURL(t *testing.T) {
	e := newEnv(t)
	d := e.driver(func(o *Options) { o.PartSize = 640 * kib })
	if got := d.Capabilities().PartSize; got != 640*kib {
		t.Errorf("Capabilities().PartSize = %d, want 640 KiB", got)
	}
	ref, _ := uploadAll(t, d, "a.mp4", randomBytes(1_500_000, 1))
	if n := e.fake.Counts()["upload.put"]; n != 3 || ref.Size != 1_500_000 {
		t.Errorf("a 1.5 MB upload sent %d fragments and made %d bytes, want 3 fragments of 1.5 MB", n, ref.Size)
	}
	for name, change := range map[string]func(*Options){
		"without BaseURL":        func(o *Options) { o.BaseURL, o.PartSize = "", 640*kib },
		"not a multiple":         func(o *Options) { o.PartSize = 300 * kib },
		"negative":               func(o *Options) { o.PartSize = -320 * kib },
		"60 MiB":                 func(o *Options) { o.PartSize = 192 * 320 * kib },
		"no HTTPClient":          func(o *Options) { o.HTTPClient = nil },
		"no Tokens":              func(o *Options) { o.Tokens = nil },
		"no DestID":              func(o *Options) { o.DestID = "" },
		"no HubID":               func(o *Options) { o.HubID = "" },
		"a BaseURL with no host": func(o *Options) { o.BaseURL = "graph.invalid" },
	} {
		opts := e.options()
		change(&opts)
		if _, err := New(opts); err == nil {
			t.Errorf("New %s succeeded, want an error", name)
		}
	}
}

// TestOneDrive_AbortCancelsTheSession: Abort of an upload that does not
// hold every byte cancels its session (a DELETE of the uploadUrl, with no
// Authorization), so the bytes it held are freed; Abort of a complete one
// leaves its item at the key, which the next upload of the key replaces, and
// puts nothing in the recycle bin.
func TestOneDrive_AbortCancelsTheSession(t *testing.T) {
	e := newEnv(t)
	d := e.driver(func(o *Options) { o.PartSize = 320 * kib })
	data := randomBytes(700*kib, 8)
	st, err := d.BeginUpload(t.Context(), "c1/a.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.PutChunk(t.Context(), &st, 0, bytes.NewReader(data[:320*kib]), 320*kib); err != nil {
		t.Fatal(err)
	}
	before := e.fake.Counts()
	if err := d.Abort(t.Context(), &st); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if got, want := calls(e.fake.Counts(), before), map[string]int{"upload.cancel": 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("Abort made the calls %v, want %v", got, want)
	}
	if n := e.fake.PendingBytes(); n != 0 {
		t.Errorf("the fake holds %d bytes of open sessions after Abort, want 0", n)
	}
	if _, err := d.Resume(t.Context(), &st); !errors.Is(err, storage.ErrSessionExpired) {
		t.Errorf("Resume of the cancelled session: %v, want ErrSessionExpired", err)
	}

	st, err = d.BeginUpload(t.Context(), "c1/a.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	sendFragments(t, d, &st, data, 0)
	if err := d.Abort(t.Context(), &st); err != nil {
		t.Fatalf("Abort of a complete upload: %v", err)
	}
	if files, bin := e.files(), e.fake.Bin(); len(files) != 1 || !bytes.Equal(files[0].Data, data) || len(bin) != 0 {
		t.Errorf("after Abort of a complete upload the files are %s and the bin %s, want the item and an empty bin", describe(files), describe(bin))
	}
	if err := d.Abort(t.Context(), &storage.UploadState{Key: "c1/b.mp4"}); err != nil {
		t.Errorf("Abort of an upload with no session: %v", err)
	}
	for _, r := range e.requests() {
		if strings.HasPrefix(r.Path, "/up/") && r.Auth != "" {
			t.Errorf("%s on the uploadUrl carried Authorization", r.Method)
		}
	}
}

// TestOneDrive_PlaybackURLIsPreauthenticated: PlaybackURL is the item's
// @microsoft.graph.downloadUrl (LinkPreauthShort), which serves the bytes
// with no token.
func TestOneDrive_PlaybackURLIsPreauthenticated(t *testing.T) {
	e := newEnv(t)
	d := e.driver(func(o *Options) { o.PartSize = 320 * kib })
	data := randomBytes(400*kib, 9)
	ref, _ := uploadAll(t, d, "c1/a.mp4", data)
	link, err := d.PlaybackURL(t.Context(), ref, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{}).Get(link)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, data) {
		t.Errorf("the link answered HTTP %d with %d bytes, want the object", resp.StatusCode, len(got))
	}
	var queries []string
	for _, r := range e.requests() {
		if r.Path == "/v1.0/me/drive/items/"+ref.ID {
			queries = append(queries, r.Query)
		}
	}
	if len(queries) != 1 || queries[0] != "$select=id,@microsoft.graph.downloadUrl" {
		t.Errorf("the item was asked with %q, want $select=id,@microsoft.graph.downloadUrl", queries)
	}
}

// TestOneDrive_RangeRead: OpenRange reads a range of an object through
// /content's redirect to the download URL, which gets the Range, a range
// that ends past the object's end included.
func TestOneDrive_RangeRead(t *testing.T) {
	e := newEnv(t)
	d := e.driver(func(o *Options) { o.PartSize = 320 * kib })
	data := randomBytes(400*kib, 10)
	ref, _ := uploadAll(t, d, "c1/a.mp4", data)
	for _, tc := range []struct{ off, n int64 }{{0, 10}, {1000, 5000}, {int64(len(data)) - 100, 100}, {int64(len(data)) - 100, 1000}, {5, 0}} {
		before := len(e.requests())
		r, err := d.OpenRange(t.Context(), ref, tc.off, tc.n)
		if err != nil {
			t.Fatalf("OpenRange(%d, %d): %v", tc.off, tc.n, err)
		}
		got, err := io.ReadAll(r)
		_ = r.Close()
		end := min(tc.off+tc.n, int64(len(data)))
		if err != nil || !bytes.Equal(got, data[tc.off:end]) {
			t.Errorf("OpenRange(%d, %d) read %d bytes, %v, want bytes %d to %d", tc.off, tc.n, len(got), err, tc.off, end)
		}
		var ranges []string
		for _, r := range e.requests()[before:] {
			if strings.HasPrefix(r.Path, "/download/") {
				ranges = append(ranges, r.Range)
			}
		}
		if want := fmt.Sprintf("bytes=%d-%d", tc.off, tc.off+tc.n-1); tc.n > 0 && (len(ranges) != 1 || ranges[0] != want) {
			t.Errorf("OpenRange(%d, %d) sent the download URL the Ranges %q, want %s", tc.off, tc.n, ranges, want)
		}
	}
}

// TestOneDrive_InvalidKeys: a key that is not a clean relative path, or
// whose name OneDrive refuses, is ErrPermanent before any call.
func TestOneDrive_InvalidKeys(t *testing.T) {
	e := newEnv(t)
	d := e.driver()
	for _, key := range []storage.ObjectKey{"", ".", "/a.mp4", "a/../b.mp4", "a//b.mp4", `a\b.mp4`, "a:b.mp4", "a?.mp4", "a*.mp4", "a|b.mp4"} {
		if _, err := d.BeginUpload(t.Context(), key, 10, storage.Meta{}); !errors.Is(err, storage.ErrPermanent) {
			t.Errorf("BeginUpload(%q): %v, want ErrPermanent", key, err)
		}
		if _, err := d.PutSmall(t.Context(), key, []byte("x")); !errors.Is(err, storage.ErrPermanent) {
			t.Errorf("PutSmall(%q): %v, want ErrPermanent", key, err)
		}
	}
	if _, err := d.BeginUpload(t.Context(), "a.mp4", 0, storage.Meta{}); !errors.Is(err, storage.ErrPermanent) {
		t.Errorf("BeginUpload of 0 bytes: %v, want ErrPermanent", err)
	}
	if n := len(e.requests()); n != 0 {
		t.Errorf("%d requests, want none", n)
	}
}

// TestOneDrive_SupersededUploadLeavesOneItem (Task 4 report concern 5): a
// placement superseded during its last fragment, or during an earlier one,
// leaves exactly one item at the key, holding the new bytes: the next
// upload's session replaces the old item (conflictBehavior replace), and an
// incomplete session is cancelled. Nothing goes to the recycle bin.
func TestOneDrive_SupersededUploadLeavesOneItem(t *testing.T) {
	for _, during := range []string{"the last fragment", "an earlier fragment"} {
		t.Run("during "+during, func(t *testing.T) { testRewritten(t, during, true) })
	}
}

// TestOneDrive_MismatchLeavesOneItem (P34): a spool file whose bytes change
// in place, same size, during its last fragment fails verification. The
// outbox deletes the copy, which goes to the recycle bin, and uploads the
// new bytes after Backoff: one live item holds them.
func TestOneDrive_MismatchLeavesOneItem(t *testing.T) { testRewritten(t, "the last fragment", false) }

// testRewritten runs the real outbox and catalog with the driver on the
// fake, and rewrites the segment's spool file while its upload is at during:
// with supersede, as the recorder does (new bytes and size, the placement
// changed in the same transaction); without, in place (same size, the
// placement unchanged), so that the copy fails P34's verification.
func testRewritten(t *testing.T, during string, supersede bool) {
	e := newEnv(t)
	d := e.driver(func(o *Options) { o.PartSize = 320 * kib })
	ctx := t.Context()
	dir := t.TempDir()
	cat, err := catalog.Open(ctx, filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	recDir := filepath.Join(dir, "rec")
	old, newer := randomBytes(700*kib, 1), randomBytes(500*kib, 2)
	if !supersede {
		newer = randomBytes(len(old), 2)
	}
	start := time.Date(2026, 10, 17, 8, 15, 0, 0, time.UTC).UnixMilli()
	seg := catalog.Segment{Cam: "c1", Profile: "main", Path: "c1/main/2026-10-17/a.mp4",
		StartMs: start, EndMs: start + 300_000, Bytes: int64(len(old)), State: catalog.StateComplete}
	spool := filepath.Join(recDir, filepath.FromSlash(seg.Path))
	if err := os.MkdirAll(filepath.Dir(spool), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spool, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cat.PutSegment(ctx, seg); err != nil {
		t.Fatal(err)
	}
	segs, err := cat.Segments(ctx)
	if err != nil || len(segs) != 1 {
		t.Fatalf("segments %+v, %v", segs, err)
	}
	seg = segs[0]
	// No jitter: a mismatch's retry comes after Backoff's first second.
	q := outbox.New(cat, map[string]storage.Destination{"dst-od": d},
		outbox.Options{RecDir: recDir, HubID: "H", HubName: "hub", Rand: func() float64 { return 0 }})
	enqueue := func(s catalog.Segment) error {
		return cat.WithTx(ctx, func(tx *sql.Tx) error {
			if s.Bytes != int64(len(old)) {
				if err := catalog.PutSegmentTx(ctx, tx, s); err != nil {
					return err
				}
			}
			return q.Enqueue(ctx, tx, outbox.PlacementFor(s, "dst-od", catalog.ClassContinuous))
		})
	}
	if err := enqueue(seg); err != nil {
		t.Fatal(err)
	}

	// The recorder rewrites the file while the old bytes' upload sends a
	// fragment: the row and the placement change in one transaction, as Task
	// 7b's callback does.
	var once sync.Once
	var hookErr error
	rewritten := make(chan struct{})
	at := fmt.Sprintf("-%d/%d", len(old)-1, len(old)) // the last fragment's Content-Range ends so
	if during == "an earlier fragment" {
		at = fmt.Sprintf("bytes 0-%d/", 320*kib-1)
	}
	e.setHook(func(r *http.Request) {
		cr := r.Header.Get("Content-Range")
		if r.Method == http.MethodPut && (strings.HasSuffix(cr, at) || strings.HasPrefix(cr, at)) {
			once.Do(func() {
				defer close(rewritten)
				if hookErr = os.WriteFile(spool, newer, 0o600); hookErr == nil && supersede {
					s := seg
					s.Bytes = int64(len(newer))
					hookErr = enqueue(s)
				}
			})
		}
	})
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- q.Run(runCtx) }()
	deadline := time.Now().Add(30 * time.Second)
	wantHash := quickXorOf(t, newer)
	var p catalog.Placement
	for {
		all, err := cat.Placements(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) == 1 {
			p = all[0]
		}
		if p.State == catalog.PlacementVerified && p.Bytes == int64(len(newer)) && p.RemoteHash == wantHash {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatalf("the new bytes are not verified after 30 s: %+v", p)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-rewritten:
	default:
		t.Fatal("the rewrite never ran")
	}
	if hookErr != nil {
		t.Fatal(hookErr)
	}
	files := e.files()
	key := string(outbox.KeyFor("H", "hub", seg, catalog.ClassContinuous))
	if len(files) != 1 || files[0].Path != key || !bytes.Equal(files[0].Data, newer) || p.RemoteRef != files[0].ID {
		t.Errorf("files at the fake %s (the placement's %s), want one: %s with the new %d bytes", describe(files), p.RemoteRef, key, len(newer))
	}
	if n := e.fake.PendingBytes(); n != 0 {
		t.Errorf("the fake holds %d bytes of open sessions, want 0", n)
	}
	bin := e.fake.Bin()
	if supersede && len(bin) != 0 {
		t.Errorf("the recycle bin holds %s, want nothing", describe(bin))
	}
	if !supersede && (len(bin) != 1 || !bytes.Equal(bin[0].Data, old)) {
		t.Errorf("the recycle bin holds %s, want the copy that failed verification", describe(bin))
	}
}
