package gdrive

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
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
	fakegdrive "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/gdrive"
)

const (
	kib = 1 << 10
	mib = 1 << 20
)

// stepClock is a fake clock. Now returns its time and then moves it on by
// step: 0 by default, so the time moves only when a test sets it; a step of
// a second or so spaces every create past the 2-per-second bucket. While it
// follows the real time, its time moves as the real time does.
type stepClock struct {
	mu     sync.Mutex
	now    time.Time
	step   time.Duration
	follow time.Time // when it started to follow the real time; zero while it does not
}

func newStepClock() *stepClock {
	return &stepClock{now: time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)}
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.follow.IsZero() {
		return c.now.Add(time.Since(c.follow))
	}
	now := c.now
	c.now = c.now.Add(c.step)
	return now
}

// FollowRealTime makes the clock's time move as the real time does, or,
// with on false, stop there.
func (c *stepClock) FollowRealTime(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.follow.IsZero() {
		c.now = c.now.Add(time.Since(c.follow))
	}
	c.follow = time.Time{}
	if on {
		c.follow = time.Now()
	}
}

func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *stepClock) SetStep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.step = d
}

// memFolders is a FolderStore in memory that, as the catalog's, keeps the
// first ID stored for a path. beforePut, when set, runs at the start of
// each PutFolder, as another process writing first.
type memFolders struct {
	mu        sync.Mutex
	ids       map[string]string
	beforePut func(path string)
}

func newMemFolders() *memFolders { return &memFolders{ids: map[string]string{}} }

func (m *memFolders) Folder(_ context.Context, destID, p string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.ids[destID+"|"+p]
	return id, ok, nil
}

func (m *memFolders) PutFolder(_ context.Context, destID, p, remoteID string) error {
	m.mu.Lock()
	hook := m.beforePut
	m.mu.Unlock()
	if hook != nil {
		hook(p)
	}
	m.put(destID, p, remoteID)
	return nil
}

func (m *memFolders) put(destID, p, remoteID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.ids[destID+"|"+p]; !ok {
		m.ids[destID+"|"+p] = remoteID
	}
}

func (m *memFolders) get(p string) string {
	id, _, _ := m.Folder(context.Background(), "dst-1", p)
	return id
}

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

// testEnv is one test's fake Drive behind an httptest server, its clock,
// folder store and tokens. hook, when set, sees each request before the
// fake does; serve, when set, answers the requests for which it returns
// true in the fake's place.
type testEnv struct {
	t       *testing.T
	fake    *fakegdrive.Fake
	srv     *httptest.Server
	clock   *stepClock
	folders *memFolders
	tokens  *tokenSource

	mu    sync.Mutex
	hook  func(*http.Request)
	serve func(http.ResponseWriter, *http.Request) bool
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	clock := newStepClock()
	e := &testEnv{
		t: t, fake: fakegdrive.New(fakegdrive.Options{Clock: clock.Now}), clock: clock,
		folders: newMemFolders(), tokens: &tokenSource{token: "tok-1"},
	}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
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

// options are the Options of a driver of the env's fake, as ipcam-hub
// builds them for a fake: a plain client and the token source.
func (e *testEnv) options() Options {
	return Options{
		DestID: "dst-1", HTTPClient: e.srv.Client(), Tokens: e.tokens, BaseURL: e.srv.URL,
		Clock: e.clock.Now, Folders: e.folders, HubID: "H",
	}
}

// drive returns a new driver of the env's fake, after change changed its
// Options.
func (e *testEnv) drive(change ...func(*Options)) *Drive {
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

// request sends a request to the fake as another client of the account.
func (e *testEnv) request(method, p string, body any) []byte {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+p, r)
	req.Header.Set("Authorization", "Bearer tok-other")
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		e.t.Fatalf("%s %s: HTTP %d %s", method, p, resp.StatusCode, data)
	}
	return data
}

// generateID returns an ID from the fake's files.generateIds, as another
// process gets one.
func (e *testEnv) generateID() string {
	e.t.Helper()
	var ids struct{ IDs []string }
	_ = json.Unmarshal(e.request(http.MethodGet, "/drive/v3/files/generateIds?count=1&space=drive&type=files", nil), &ids)
	return ids.IDs[0]
}

// createFolder creates a folder at the fake, as another process does.
func (e *testEnv) createFolder(id, name, parent string) {
	e.t.Helper()
	e.request(http.MethodPost, "/drive/v3/files", map[string]any{
		"id": id, "name": name, "mimeType": fakegdrive.FolderMimeType, "parents": []string{parent},
	})
}

// objects returns the fake's files (folder false) or folders (true).
func (e *testEnv) objects(folders bool) []fakegdrive.Object {
	var out []fakegdrive.Object
	for _, o := range e.fake.Objects() {
		if o.Folder == folders {
			out = append(out, o)
		}
	}
	return out
}

// describe names objects for a failure message, with their sizes and not
// their bytes.
func describe(objects []fakegdrive.Object) string {
	parts := make([]string, len(objects))
	for i, o := range objects {
		parts[i] = fmt.Sprintf("%s %q in %s (%d bytes)", o.ID, o.Name, o.Parent, len(o.Data))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func randomBytes(n int, seed byte) []byte {
	b := make([]byte, n)
	_, _ = rand.NewChaCha8([32]byte{seed}).Read(b)
	return b
}

func md5Of(t *testing.T, data []byte) string {
	t.Helper()
	sum, err := storage.Digest(storage.HashMD5, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

// sendChunks sends data from off to its end through st, as the outbox does:
// chunks of the driver's PartSize, each from the offset that the last one
// confirmed.
func sendChunks(t *testing.T, d *Drive, st *storage.UploadState, data []byte, off int64) {
	t.Helper()
	part := d.Capabilities().PartSize
	for off < int64(len(data)) {
		n := min(part, int64(len(data))-off)
		if err := d.PutChunk(t.Context(), st, off, bytes.NewReader(data[off:off+n]), n); err != nil {
			t.Fatalf("PutChunk at %d: %v", off, err)
		}
		if st.Offset != off+n {
			t.Fatalf("after the chunk at %d of %d bytes the offset is %d, want %d", off, n, st.Offset, off+n)
		}
		off = st.Offset
	}
}

// uploadAll uploads data to key through d and commits it.
func uploadAll(t *testing.T, d *Drive, key storage.ObjectKey, data []byte) (storage.ObjectRef, storage.UploadState) {
	t.Helper()
	st, err := d.BeginUpload(t.Context(), key, int64(len(data)), storage.Meta{ContentType: "video/mp4"})
	if err != nil {
		t.Fatalf("BeginUpload(%s): %v", key, err)
	}
	sendChunks(t, d, &st, data, 0)
	ref, err := d.Commit(t.Context(), &st)
	if err != nil {
		t.Fatalf("Commit(%s): %v", key, err)
	}
	return ref, st
}

func goldenCaps() storage.Caps {
	return storage.Caps{
		Resumable: storage.ResumeSessionURI, ChunkAlign: 256 * kib, PartSize: 16 * mib,
		Playback: storage.LinkProxyOnly, RangeRead: true, Delete: storage.DeletePermanent,
		Quota: storage.QuotaAccount, Hashes: []storage.HashKind{storage.HashMD5},
		DailyUploadCap: 750 << 30, CreatesPerSec: 2,
	}
}

func TestDrive_CapsGolden(t *testing.T) {
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
		if d.Kind() != storage.KindGoogleDrive {
			t.Errorf("%s: Kind() = %s, want %s", name, d.Kind(), storage.KindGoogleDrive)
		}
	}
	if n := len(e.fake.Counts()); n != 0 {
		t.Errorf("New made calls: %v", e.fake.Counts())
	}
}

// TestDrive_ResumableUploadAndVerify: an upload of 40 MiB + 1 byte that
// stops after its first chunk resumes, in a new driver from the session
// alone, through upload.status at 16 MiB, and commits once; the hash is the
// bytes' MD5, which the fake reports as md5Checksum.
func TestDrive_ResumableUploadAndVerify(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	data := randomBytes(40*mib+1, 1)
	key := storage.ObjectKey("hub/c1/2026-10-17/a.mp4")
	first := e.drive()
	st, err := first.BeginUpload(t.Context(), key, int64(len(data)), storage.Meta{ContentType: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.PutChunk(t.Context(), &st, 0, bytes.NewReader(data[:16*mib]), 16*mib); err != nil {
		t.Fatal(err)
	}
	saved, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}

	var restored storage.UploadState
	if err := json.Unmarshal(saved, &restored); err != nil {
		t.Fatal(err)
	}
	second := e.drive()
	next, err := second.Resume(t.Context(), &restored)
	if err != nil || next != 16*mib || restored.Offset != 16*mib {
		t.Fatalf("Resume = %d, %v (offset %d), want 16 MiB", next, err, restored.Offset)
	}
	sendChunks(t, second, &restored, data, next)
	ref, err := second.Commit(t.Context(), &restored)
	if err != nil {
		t.Fatal(err)
	}

	files := e.objects(false)
	if len(files) != 1 {
		t.Fatalf("%d files at the fake, want 1: %s", len(files), describe(files))
	}
	want := md5Of(t, data)
	if ref.Hash != want || files[0].MD5 != want || ref.HashKind != storage.HashMD5 || ref.Size != int64(len(data)) ||
		ref.ID != files[0].ID || ref.Key != key {
		t.Errorf("Commit = %+v, want ID %s, %d bytes, MD5 %s (the fake's md5Checksum %s)", ref, files[0].ID, len(data), want, files[0].MD5)
	}
	if files[0].Name != "a.mp4" || files[0].Parent != e.folders.get("ipcam/hub/c1/2026-10-17") {
		t.Errorf("the file %s in %s, want a.mp4 in the day folder %s", files[0].Name, files[0].Parent, e.folders.get("ipcam/hub/c1/2026-10-17"))
	}
	if c := e.fake.Counts(); c["upload.status"] != 1 || c["files.create.resumable"] != 1 || c["files.get"] != 1 || c["upload.put"] != 3 {
		t.Errorf("the fake's counts %v, want one session, one status query, three chunks and one files.get", c)
	}
}

// TestDrive_ResumeDetectsCompleteSession: a session that received every
// byte answers the status query 200, so Resume returns Size and Commit
// returns that file; no second object is made.
func TestDrive_ResumeDetectsCompleteSession(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	data := randomBytes(600*kib, 2)
	d := e.drive(func(o *Options) { o.PartSize = 256 * kib })
	st, err := d.BeginUpload(t.Context(), "a.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	sendChunks(t, d, &st, data, 0)
	restored := storage.UploadState{Key: st.Key, Size: st.Size, Session: st.Session, Offset: 512 * kib}
	again := e.drive(func(o *Options) { o.PartSize = 256 * kib })
	next, err := again.Resume(t.Context(), &restored)
	if err != nil || next != int64(len(data)) {
		t.Fatalf("Resume = %d, %v, want the size %d", next, err, len(data))
	}
	ref, err := again.Commit(t.Context(), &restored)
	if err != nil || ref.Hash != md5Of(t, data) || ref.Size != int64(len(data)) {
		t.Fatalf("Commit = %+v, %v, want the file of the bytes", ref, err)
	}
	if files := e.objects(false); len(files) != 1 || files[0].ID != ref.ID {
		t.Errorf("files at the fake %s, want only %s", describe(files), ref.ID)
	}
	if c := e.fake.Counts(); c["files.create.resumable"] != 1 || c["upload.status"] != 1 {
		t.Errorf("the fake's counts %v, want one session and one status query", c)
	}
}

// TestDrive_ZeroListInNormalOperation (Review Focus 3, STO-07): a probe, ten
// uploads with their verification and a range read make no files.list call;
// ten BeginUpload calls at one instant send two creates and refuse eight
// with ErrRateLimited at once; the fake never sees more than two creates in
// a second.
func TestDrive_ZeroListInNormalOperation(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(600 * time.Millisecond)
	d := e.drive(func(o *Options) { o.PartSize = 256 * kib })
	if _, err := d.Probe(t.Context()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	var refs []storage.ObjectRef
	for i := range 10 {
		data := randomBytes(300*kib+i, byte(i))
		ref, _ := uploadAll(t, d, storage.ObjectKey(fmt.Sprintf("hub/c1/2026-10-17/seg-%d.mp4", i)), data)
		info, err := d.Stat(t.Context(), ref)
		if err != nil || info.Ref.Hash != md5Of(t, data) || info.Ref.Size != int64(len(data)) || ref.Hash != info.Ref.Hash {
			t.Fatalf("Stat = %+v, %v, want the MD5 and size of the bytes", info, err)
		}
		refs = append(refs, ref)
	}
	r, err := d.OpenRange(t.Context(), refs[3], 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || !bytes.Equal(got, randomBytes(300*kib+3, 3)[100:150]) {
		t.Errorf("OpenRange read %d bytes, %v, want bytes 100 to 150 of the object", len(got), err)
	}

	e.clock.SetStep(0)
	e.clock.Advance(time.Second)
	before := e.fake.Counts()["files.create.resumable"]
	var began, limited int
	for i := range 10 {
		_, err := d.BeginUpload(t.Context(), storage.ObjectKey(fmt.Sprintf("hub/c1/2026-10-17/burst-%d.mp4", i)), 10, storage.Meta{})
		var rl storage.ErrRateLimited
		switch {
		case err == nil:
			began++
		case errors.As(err, &rl) && rl.RetryAfter > 0 && rl.RetryAfter <= time.Second:
			limited++
		default:
			t.Errorf("BeginUpload %d: %v, want nil or ErrRateLimited with RetryAfter in (0, 1 s]", i, err)
		}
	}
	if sent := e.fake.Counts()["files.create.resumable"] - before; began != 2 || limited != 8 || sent != 2 {
		t.Errorf("the burst began %d, was limited %d times and sent %d creates; want 2, 8 and 2", began, limited, sent)
	}
	if n := e.fake.Counts()["files.list"]; n != 0 {
		t.Errorf("%d files.list calls, want 0", n)
	}
	if n := e.fake.MaxCreatesPerSec(); n > 2 {
		t.Errorf("the fake saw %d creates in a second, want at most 2", n)
	}
}

// TestDrive_RestartReusesFolders_ZeroList (Review Focus 3): a second driver
// on the same folder store uploads into the same day folder with no
// files.list call and no folder create.
func TestDrive_RestartReusesFolders_ZeroList(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	first, _ := uploadAll(t, e.drive(), "hub/c1/2026-10-17/a.mp4", randomBytes(1000, 1))
	before := e.fake.Counts()
	restarted := e.drive()
	second, _ := uploadAll(t, restarted, "hub/c1/2026-10-17/b.mp4", randomBytes(1000, 2))
	after := e.fake.Counts()
	if after["files.list"] != 0 || after["files.create"] != before["files.create"] || after["files.generateIds"] != before["files.generateIds"] {
		t.Errorf("the restarted driver's calls %v (before it %v), want no files.list, folder create or ID", after, before)
	}
	if want := (storage.CallCounts{"files.create.resumable": 1, "upload.put": 1, "files.get": 1}); !reflect.DeepEqual(restarted.CallLog(), want) {
		t.Errorf("the restarted driver's CallLog() = %v, want %v", restarted.CallLog(), want)
	}
	parents := map[string]string{}
	for _, f := range e.objects(false) {
		parents[f.ID] = f.Parent
	}
	if parents[first.ID] == "" || parents[first.ID] != parents[second.ID] {
		t.Errorf("the two files' folders %q and %q, want the same day folder", parents[first.ID], parents[second.ID])
	}
}

// TestDrive_FolderCreateIsIdempotent: whichever ID the store holds is the
// one created, and each case leaves exactly one folder.
func TestDrive_FolderCreateIsIdempotent(t *testing.T) {
	check := func(t *testing.T, e *testEnv, ref storage.ObjectRef, id string) {
		t.Helper()
		folders := e.objects(true)
		if len(folders) != 1 || folders[0].ID != id || folders[0].Name != "ipcam" || folders[0].Parent != "root" {
			t.Errorf("folders %s, want one: ipcam in My Drive with ID %s", describe(folders), id)
		}
		if files := e.objects(false); len(files) != 1 || files[0].ID != ref.ID || files[0].Parent != id {
			t.Errorf("files %s, want %s in %s", describe(files), ref.ID, id)
		}
	}
	t.Run("a stored ID with no folder", func(t *testing.T) {
		e := newEnv(t)
		e.clock.SetStep(time.Second)
		id := e.generateID()
		e.folders.put("dst-1", "ipcam", id) // persisted by a process that died before files.create
		ref, _ := uploadAll(t, e.drive(), "a.mp4", randomBytes(100, 1))
		check(t, e, ref, id)
		if c := e.fake.Counts(); c["files.create"] != 1 {
			t.Errorf("%d folder creates, want 1, with the stored ID", c["files.create"])
		}
	})
	t.Run("a stored ID whose folder exists", func(t *testing.T) {
		e := newEnv(t)
		e.clock.SetStep(time.Second)
		var other string
		e.folders.beforePut = func(p string) { // another process stored and created it first
			if p == "ipcam" && other == "" {
				other = e.generateID()
				e.createFolder(other, "ipcam", "root")
				e.folders.put("dst-1", p, other)
			}
		}
		ref, _ := uploadAll(t, e.drive(), "a.mp4", randomBytes(100, 1))
		check(t, e, ref, other)
		if c := e.fake.Counts(); c["files.create"] != 2 {
			t.Errorf("%d folder creates, want 2: the other process's, and the driver's answered 409", c["files.create"])
		}
	})
	t.Run("another process's ID stored first", func(t *testing.T) {
		e := newEnv(t)
		e.clock.SetStep(time.Second)
		var other string
		e.folders.beforePut = func(p string) { // another process stored it and has not created it yet
			if p == "ipcam" && other == "" {
				other = e.generateID()
				e.folders.put("dst-1", p, other)
			}
		}
		ref, _ := uploadAll(t, e.drive(), "a.mp4", randomBytes(100, 1))
		check(t, e, ref, other)
		if got := e.folders.get("ipcam"); got != other {
			t.Errorf("the store holds %s, want the first ID %s", got, other)
		}
	})
}

// TestDrive_DailyCapPauses: Drive's 403 dailyLimitExceeded is
// ErrDailyCap{ResetAfter: 24 h}.
func TestDrive_DailyCapPauses(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	e.fake.DailyCapAfter(256 * kib)
	d := e.drive(func(o *Options) { o.PartSize = 256 * kib })
	data := randomBytes(512*kib, 1)
	st, err := d.BeginUpload(t.Context(), "a.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.PutChunk(t.Context(), &st, 0, bytes.NewReader(data[:256*kib]), 256*kib); err != nil {
		t.Fatal(err)
	}
	err = d.PutChunk(t.Context(), &st, 256*kib, bytes.NewReader(data[256*kib:]), 256*kib)
	var capped storage.ErrDailyCap
	if !errors.As(err, &capped) || capped.ResetAfter != 24*time.Hour {
		t.Errorf("PutChunk past the daily cap: %v, want ErrDailyCap{ResetAfter: 24h}", err)
	}
}

// TestDrive_QuotaUnitsRecorded (P18): one 40 MiB upload into a folder that
// exists makes one session, three chunks and one files.get, which its
// UploadState counts; an upload that makes its folders counts their calls
// too; CallLog totals every call, as the fake counts them.
func TestDrive_QuotaUnitsRecorded(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	d := e.drive()
	_, first := uploadAll(t, d, "hub/c1/2026-10-17/a.mp4", randomBytes(1000, 1))
	if want := (storage.CallCounts{"files.generateIds": 4, "files.create": 4, "files.create.resumable": 1, "upload.put": 1, "files.get": 1}); !reflect.DeepEqual(first.Calls, want) {
		t.Errorf("the first upload's Calls = %v, want %v (the root, hub, camera and day folders)", first.Calls, want)
	}
	_, st := uploadAll(t, d, "hub/c1/2026-10-17/b.mp4", randomBytes(40*mib, 2))
	if want := (storage.CallCounts{"files.create.resumable": 1, "upload.put": 3, "files.get": 1}); !reflect.DeepEqual(st.Calls, want) {
		t.Errorf("the 40 MiB upload's Calls = %v, want %v", st.Calls, want)
	}
	if _, err := d.Quota(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, want := d.CallLog(), storage.CallCounts(e.fake.Counts()); !reflect.DeepEqual(got, want) {
		t.Errorf("CallLog() = %v, want the fake's counts %v", got, want)
	}
}

// TestDrive_ErrorMapping: every status and reason maps to its typed error,
// and no error's text holds the session URI, a request URL or a token, a
// reset connection on a chunk, Abort and Delete included.
func TestDrive_ErrorMapping(t *testing.T) {
	// A step runs a call's setup through d and returns the call under test.
	type step func(t *testing.T, e *testEnv, d *Drive, st *storage.UploadState) func() error
	data := randomBytes(512*kib, 1)
	begin := func(t *testing.T, d *Drive) storage.UploadState {
		t.Helper()
		st, err := d.BeginUpload(t.Context(), "a.mp4", int64(len(data)), storage.Meta{})
		if err != nil {
			t.Fatalf("BeginUpload: %v", err)
		}
		return st
	}
	committed := func(t *testing.T, d *Drive) storage.ObjectRef {
		t.Helper()
		ref, _ := uploadAll(t, d, "a.mp4", data)
		return ref
	}
	var (
		opBegin step = func(t *testing.T, _ *testEnv, d *Drive, _ *storage.UploadState) func() error {
			return func() error {
				_, err := d.BeginUpload(t.Context(), "a.mp4", int64(len(data)), storage.Meta{})
				return err
			}
		}
		opPut step = func(t *testing.T, _ *testEnv, d *Drive, st *storage.UploadState) func() error {
			*st = begin(t, d)
			return func() error { return d.PutChunk(t.Context(), st, 0, bytes.NewReader(data[:256*kib]), 256*kib) }
		}
		opResume step = func(t *testing.T, _ *testEnv, d *Drive, st *storage.UploadState) func() error {
			*st = begin(t, d)
			return func() error {
				_, err := d.Resume(t.Context(), st)
				return err
			}
		}
		opCommit step = func(t *testing.T, _ *testEnv, d *Drive, st *storage.UploadState) func() error {
			*st = begin(t, d)
			sendChunks(t, d, st, data, 0)
			return func() error {
				_, err := d.Commit(t.Context(), st)
				return err
			}
		}
		// opAbort aborts a complete session from a new driver, as after a
		// restart, so Abort asks the session for its file, then deletes it.
		opAbort step = func(t *testing.T, e *testEnv, d *Drive, st *storage.UploadState) func() error {
			*st = begin(t, d)
			sendChunks(t, d, st, data, 0)
			restarted := e.drive(func(o *Options) { o.PartSize = 256 * kib })
			return func() error { return restarted.Abort(t.Context(), st) }
		}
		opStat step = func(t *testing.T, _ *testEnv, d *Drive, _ *storage.UploadState) func() error {
			ref := committed(t, d)
			return func() error {
				_, err := d.Stat(t.Context(), ref)
				return err
			}
		}
		opRead step = func(t *testing.T, _ *testEnv, d *Drive, _ *storage.UploadState) func() error {
			ref := committed(t, d)
			return func() error {
				r, err := d.OpenRange(t.Context(), ref, 0, 10)
				if err == nil {
					_ = r.Close()
				}
				return err
			}
		}
		opDelete step = func(t *testing.T, _ *testEnv, d *Drive, _ *storage.UploadState) func() error {
			ref := committed(t, d)
			return func() error {
				_, err := d.Delete(t.Context(), ref)
				return err
			}
		}
		opQuota step = func(t *testing.T, _ *testEnv, d *Drive, _ *storage.UploadState) func() error {
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
		reason     string
		retryAfter time.Duration
		tokenErr   error // instead of FailNext: the token source's error
		op         step
		want       error
		wantLimit  *time.Duration // ErrRateLimited's RetryAfter
		wantCap    bool           // ErrDailyCap{ResetAfter: 24 h}
	}{
		{name: "401", method: "about.get", status: 401, reason: "authError", op: opQuota, want: storage.ErrAuthExpired},
		{name: "403 storageQuotaExceeded", method: "files.create.resumable", status: 403, reason: "storageQuotaExceeded", op: opBegin, want: storage.ErrQuotaFull},
		{name: "403 userRateLimitExceeded", method: "about.get", status: 403, reason: "userRateLimitExceeded", op: opQuota, wantLimit: limit(0)},
		{name: "403 rateLimitExceeded", method: "files.create.resumable", status: 403, reason: "rateLimitExceeded", op: opBegin, wantLimit: limit(0)},
		{name: "429 with Retry-After 7", method: "upload.put", status: 429, reason: "rateLimitExceeded", retryAfter: 7 * time.Second, op: opPut, wantLimit: limit(7 * time.Second)},
		{name: "403 dailyLimitExceeded", method: "upload.put", status: 403, reason: "dailyLimitExceeded", op: opPut, wantCap: true},
		{name: "403 uploadLimitExceeded", method: "files.create.resumable", status: 403, reason: "uploadLimitExceeded", op: opBegin, wantCap: true},
		{name: "404 on the session's chunk", method: "upload.put", status: 404, reason: "notFound", op: opPut, want: storage.ErrSessionExpired},
		{name: "410 on the session's status", method: "upload.status", status: 410, reason: "gone", op: opResume, want: storage.ErrSessionExpired},
		{name: "404 on a file", method: "files.get", status: 404, reason: "notFound", op: opStat, want: storage.ErrNotFound},
		{name: "404 on a delete", method: "files.delete", status: 404, reason: "notFound", op: opDelete, want: storage.ErrNotFound},
		{name: "500 on Commit", method: "files.get", status: 500, reason: "backendError", op: opCommit, want: storage.ErrTransient},
		{name: "503", method: "about.get", status: 503, reason: "backendError", op: opQuota, want: storage.ErrTransient},
		{name: "reset on the chunk PUT", method: "upload.put", status: fakegdrive.Reset, op: opPut, want: storage.ErrTransient},
		{name: "reset on the session's create", method: "files.create.resumable", status: fakegdrive.Reset, op: opBegin, want: storage.ErrTransient},
		{name: "reset on the status query", method: "upload.status", status: fakegdrive.Reset, op: opResume, want: storage.ErrTransient},
		{name: "400", method: "upload.put", status: 400, reason: "badRequest", op: opPut, want: storage.ErrPermanent},
		{name: "403 of another reason", method: "files.get.media", status: 403, reason: "insufficientPermissions", op: opRead, want: storage.ErrPermanent},
		{name: "500 on Abort's status query", method: "upload.status", status: 500, reason: "backendError", op: opAbort, want: storage.ErrTransient},
		{name: "reset on Abort's status query", method: "upload.status", status: fakegdrive.Reset, op: opAbort, want: storage.ErrTransient},
		{name: "500 on Abort's delete", method: "files.delete", status: 500, reason: "backendError", op: opAbort, want: storage.ErrTransient},
		{name: "500 on Delete", method: "files.delete", status: 500, reason: "backendError", op: opDelete, want: storage.ErrTransient},
		{name: "a token error wrapping ErrTransient", tokenErr: fmt.Errorf("token endpoint: %w", storage.ErrTransient), op: opQuota, want: storage.ErrTransient},
		{name: "a token error wrapping ErrAuthExpired", tokenErr: fmt.Errorf("invalid_grant: %w", storage.ErrAuthExpired), op: opQuota, want: storage.ErrAuthExpired},
		{name: "a plain token error", tokenErr: errors.New("oauth2: cannot fetch token"), op: opQuota, want: storage.ErrTransient},
		{name: "a token error quoting a URL", tokenErr: &url.Error{Op: "Post", URL: "https://token.invalid/?" + sentinel, Err: errors.New("connection refused")}, op: opQuota, want: storage.ErrTransient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.clock.SetStep(time.Second)
			d := e.drive(func(o *Options) { o.PartSize = 256 * kib })
			var st storage.UploadState
			call := tc.op(t, e, d, &st)
			if tc.tokenErr != nil {
				e.tokens.set("", tc.tokenErr)
			} else {
				e.fake.FailNext(tc.method, tc.status, tc.reason, tc.retryAfter)
			}
			err := call()
			var rl storage.ErrRateLimited
			var capped storage.ErrDailyCap
			switch {
			case err == nil:
				t.Fatal("no error")
			case tc.wantLimit != nil:
				if !errors.As(err, &rl) || rl.RetryAfter != *tc.wantLimit {
					t.Errorf("error %v, want ErrRateLimited{RetryAfter: %s}", err, *tc.wantLimit)
				}
			case tc.wantCap:
				if !errors.As(err, &capped) || capped.ResetAfter != 24*time.Hour {
					t.Errorf("error %v, want ErrDailyCap{ResetAfter: 24h}", err)
				}
			case !errors.Is(err, tc.want):
				t.Errorf("error %v, want %v", err, tc.want)
			}
			if tc.want == storage.ErrTransient && errors.Is(err, storage.ErrPermanent) {
				t.Errorf("error %v is ErrPermanent too", err)
			}
			text := err.Error()
			for _, leak := range []string{st.Session, "upload_id", e.srv.URL, "tok-1", sentinel} {
				if leak != "" && strings.Contains(text, leak) {
					t.Errorf("the error's text %q holds %q", text, leak)
				}
			}
			if tc.method != "" && !strings.Contains(text, tc.method) {
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

// TestDrive_PartSizeOverrideOnlyWithBaseURL: another PartSize is taken only
// with BaseURL set and as a multiple of 256 KiB, and the chunks follow it.
func TestDrive_PartSizeOverrideOnlyWithBaseURL(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	d := e.drive(func(o *Options) { o.PartSize = 512 * kib })
	if got := d.Capabilities().PartSize; got != 512*kib {
		t.Errorf("Capabilities().PartSize = %d, want 512 KiB", got)
	}
	ref, _ := uploadAll(t, d, "a.mp4", randomBytes(1_900_000, 1))
	if n := e.fake.Counts()["upload.put"]; n != 4 || ref.Size != 1_900_000 {
		t.Errorf("a 1.9 MB upload sent %d chunks and made %d bytes, want 4 chunks of 1.9 MB", n, ref.Size)
	}
	for name, change := range map[string]func(*Options){
		"without BaseURL":         func(o *Options) { o.BaseURL, o.PartSize = "", 512*kib },
		"not a multiple":          func(o *Options) { o.PartSize = 300 * kib },
		"negative":                func(o *Options) { o.PartSize = -256 * kib },
		"no HTTPClient":           func(o *Options) { o.HTTPClient = nil },
		"no Tokens":               func(o *Options) { o.Tokens = nil },
		"no Folders":              func(o *Options) { o.Folders = nil },
		"no DestID":               func(o *Options) { o.DestID = "" },
		"no HubID":                func(o *Options) { o.HubID = "" },
		"a RootName with a slash": func(o *Options) { o.RootName = "a/b" },
	} {
		opts := e.options()
		change(&opts)
		if _, err := New(opts); err == nil {
			t.Errorf("New %s succeeded, want an error", name)
		}
	}
}

// recordingTransport records the Authorization header of each request
// that its client sends.
type recordingTransport struct {
	base http.RoundTripper
	mu   sync.Mutex
	auth []string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.auth = append(rt.auth, req.Header.Get("Authorization"))
	rt.mu.Unlock()
	return rt.base.RoundTrip(req)
}

// TestDrive_BearerFromTokenSourceOnPlainClient (P27): the driver sends every
// request through the client it was given, with the bearer token that
// Tokens holds at that call.
func TestDrive_BearerFromTokenSourceOnPlainClient(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	rt := &recordingTransport{base: e.srv.Client().Transport}
	client := &http.Client{Transport: rt}
	var mu sync.Mutex
	var accepted []string
	e.fake.AcceptToken(func(tok string) bool {
		mu.Lock()
		defer mu.Unlock()
		accepted = append(accepted, tok)
		return true
	})
	d := e.drive(func(o *Options) { o.HTTPClient = client })
	if d.client != client {
		t.Fatal("the driver does not use the HTTPClient it was given")
	}
	if _, err := d.Quota(t.Context()); err != nil {
		t.Fatal(err)
	}
	e.tokens.set("tok-2", nil)
	uploadAll(t, d, "hub/a.mp4", randomBytes(1000, 1))

	if len(rt.auth) < 2 || len(rt.auth) != len(accepted) {
		t.Fatalf("the client sent %d requests and the fake took %d tokens, want the same, at least 2", len(rt.auth), len(accepted))
	}
	for i, auth := range rt.auth {
		want := "tok-2"
		if i == 0 {
			want = "tok-1"
		}
		if auth != "Bearer "+want || accepted[i] != want {
			t.Errorf("request %d carried %q (the fake saw %q), want Bearer %s", i, auth, accepted[i], want)
		}
	}
}

// TestDrive_AboutAsksStorageQuotaOnly (P45): about.get asks for the storage
// quota only, never the user, and Quota reports it.
func TestDrive_AboutAsksStorageQuotaOnly(t *testing.T) {
	e := newEnv(t)
	var queries []string
	e.setHook(func(r *http.Request) {
		if r.URL.Path == "/drive/v3/about" {
			queries = append(queries, r.URL.RawQuery)
		}
	})
	e.fake.SetQuota(1000, 100)
	q, err := e.drive().Quota(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 || queries[0] != "fields=storageQuota" {
		t.Errorf("about.get queries %q, want exactly fields=storageQuota", queries)
	}
	if want := (storage.Quota{Total: 1000, Used: 100, Free: 900}); q != want {
		t.Errorf("Quota() = %+v, want %+v", q, want)
	}
}

// probeText is the README that Probe writes (§8.7 step 6).
const probeText = "Acest folder conține înregistrările unui hub ipcam. Fișierele .age sunt criptate cu age " +
	"(https://age-encryption.org) și se deschid cu: age -d -i <cheia de recuperare> <fișier>.mp4.age > <fișier>.mp4"

// TestDrive_Probe (P35): Probe creates and remembers the root, confirms the
// 409 of a create with its ID, writes the README, writes, reads and deletes
// 1 MiB and reads the quota, waiting in real time for its create slots; a
// second Probe creates no second root or README.
func TestDrive_Probe(t *testing.T) {
	e := newEnv(t)
	e.clock.FollowRealTime(true)
	d := e.drive()
	e.fake.SetQuota(10*1000*mib, 5*mib)
	start := time.Now()
	report, err := d.Probe(t.Context())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	// Its six creates, two a second, take two seconds at least. The fake's
	// meter is not asserted here: it times a request when it arrives, and in
	// real time the requests' latencies differ.
	if c := e.fake.Counts(); c["files.create"]+c["files.create.resumable"] != 6 || time.Since(start) < 2*time.Second {
		t.Errorf("Probe made %d creates in %s, want 6 in 2 s or more", c["files.create"]+c["files.create.resumable"], time.Since(start))
	}
	if !report.Writable || !report.RangeRead || !report.Deletable || len(report.Warnings) != 0 {
		t.Errorf("Probe's report %+v, want writable, range-readable and deletable, with no warning", report)
	}
	readme := int64(len(probeText))
	if want := (storage.Quota{Total: 10 * 1000 * mib, Used: 5*mib + readme, Free: 10*1000*mib - 5*mib - readme}); report.Quota != want {
		t.Errorf("Probe's quota %+v, want %+v", report.Quota, want)
	}
	check := func(probes int) {
		t.Helper()
		root := e.folders.get("ipcam")
		var roots, readmes int
		for _, o := range e.fake.Objects() {
			switch {
			case o.Folder && o.Name == "ipcam":
				roots++
				if o.ID != root || o.Parent != "root" {
					t.Errorf("the root folder %+v, want %s in My Drive", o, root)
				}
			case o.Name == "README.txt":
				readmes++
				if string(o.Data) != probeText || o.Parent != e.folders.get("ipcam/.ipcam/H") {
					t.Errorf("README.txt holds %q in %s, want the fixed text in .ipcam/H", o.Data, o.Parent)
				}
			case !o.Folder:
				t.Errorf("Probe left %s (%d bytes)", o.Name, len(o.Data))
			}
		}
		if roots != 1 || readmes != 1 {
			t.Errorf("after %d probes: %d root folders and %d READMEs, want one of each", probes, roots, readmes)
		}
		c := e.fake.Counts()
		if c["files.create.resumable"] != probes || c["upload.put"] != probes || c["files.get.media"] != probes ||
			c["files.delete"] != probes || c["about.get"] != probes || c["files.list"] != 0 {
			t.Errorf("after %d probes the fake's counts %v, want one 1 MiB write, read and delete and one quota read each", probes, c)
		}
	}
	check(1)
	if c := e.fake.Counts(); c["files.create"] != 5 {
		t.Errorf("%d files.create, want 5: the root, its 409, .ipcam, .ipcam/H and README.txt", c["files.create"])
	}

	e.clock.FollowRealTime(false)
	e.clock.SetStep(time.Second)
	if _, err := d.Probe(t.Context()); err != nil {
		t.Fatalf("the second Probe: %v", err)
	}
	check(2)
	if c := e.fake.Counts(); c["files.create"] != 6 || c["files.update"] != 1 {
		t.Errorf("the second probe's creates %d and updates %d, want the root's 409 and README.txt's update", c["files.create"]-5, c["files.update"])
	}

	// A Drive that answers a create of an existing ID with 200 twice, not
	// 409, is reported.
	e.fake.FailNext("files.create", http.StatusOK, "", 0)
	e.fake.FailNext("files.create", http.StatusOK, "", 0)
	report, err = d.Probe(t.Context())
	if err != nil || len(report.Warnings) != 1 {
		t.Errorf("a probe without the 409: %v, warnings %q, want one warning", err, report.Warnings)
	}
}

func TestDrive_PlaybackURLIsProxyRequired(t *testing.T) {
	e := newEnv(t)
	link, err := e.drive().PlaybackURL(t.Context(), storage.ObjectRef{ID: "x", Key: "a.mp4"}, time.Minute)
	if !errors.Is(err, storage.ErrProxyRequired) || link != "" {
		t.Errorf("PlaybackURL = %q, %v, want ErrProxyRequired", link, err)
	}
	if c := e.fake.Counts(); len(c) != 0 {
		t.Errorf("PlaybackURL made calls: %v", c)
	}
}

// TestDrive_ListWalksStoredFolders: List yields the objects under a prefix
// from the folders that the store holds, through files.list, and nothing
// for a folder the driver never made.
func TestDrive_ListWalksStoredFolders(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	d := e.drive()
	want := map[storage.ObjectKey]string{}
	for i, key := range []storage.ObjectKey{"hub/c1/2026-10-17/a.mp4", "hub/c1/2026-10-17/b.mp4", "hub/c1/2026-10-18/a.mp4", "hub/c2/2026-10-17/a.mp4"} {
		ref, _ := uploadAll(t, d, key, randomBytes(100+i, byte(i)))
		want[key] = ref.Hash
	}
	list := func(prefix storage.ObjectKey) map[storage.ObjectKey]string {
		got := map[storage.ObjectKey]string{}
		for info, err := range d.List(t.Context(), prefix) {
			if err != nil {
				t.Fatalf("List(%q): %v", prefix, err)
			}
			if info.Ref.HashKind != storage.HashMD5 || info.Ref.ID == "" || info.ModTimeMs == 0 {
				t.Errorf("List(%q) yielded %+v, want an ID, an MD5 and a time", prefix, info)
			}
			got[info.Ref.Key] = info.Ref.Hash
		}
		return got
	}
	if got := list("hub/c1/"); len(got) != 3 || got["hub/c1/2026-10-18/a.mp4"] != want["hub/c1/2026-10-18/a.mp4"] {
		t.Errorf("List(hub/c1/) = %v, want the camera's three objects", got)
	}
	if got := list("hub/c1/2026-10-17/b"); len(got) != 1 || got["hub/c1/2026-10-17/b.mp4"] != want["hub/c1/2026-10-17/b.mp4"] {
		t.Errorf("List(hub/c1/2026-10-17/b) = %v, want b.mp4", got)
	}
	if got := list(""); len(got) != 4 {
		t.Errorf("List(\"\") = %v, want all four objects", got)
	}
	before := e.fake.Counts()["files.list"]
	if got := list("other/"); len(got) != 0 || e.fake.Counts()["files.list"] != before {
		t.Errorf("List(other/) = %v with %d files.list calls, want nothing and no call", got, e.fake.Counts()["files.list"]-before)
	}
}

// TestDrive_AbortAfterDelete (P34): the outbox deletes a copy that does not
// match the spool file, then aborts its session, which Commit has already
// left. Abort asks the session for its file. Whether the session still
// answers the file that it made (the fake, as Google's upload server keeps a
// finished upload's answer) or 404 (which the live API may answer instead),
// Abort returns nil and leaves no file, and the fake does not panic.
func TestDrive_AbortAfterDelete(t *testing.T) {
	for _, tc := range []struct {
		name      string
		gone      bool           // the session answers 404
		wantCalls map[string]int // Abort's calls at the fake
	}{
		{name: "the session answers its file", wantCalls: map[string]int{"upload.status": 1, "files.delete": 1}},
		{name: "the session answers 404", gone: true, wantCalls: map[string]int{"upload.status": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.clock.SetStep(time.Second)
			d := e.drive(func(o *Options) { o.PartSize = 256 * kib })
			ref, st := uploadAll(t, d, "c1/a.mp4", randomBytes(600*kib, 1))
			if _, err := d.Delete(t.Context(), ref); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if tc.gone {
				e.fake.FailNext("upload.status", http.StatusNotFound, "notFound", 0)
			}
			before := e.fake.Counts()
			if err := d.Abort(t.Context(), &st); err != nil {
				t.Errorf("Abort after Delete: %v, want nil", err)
			}
			calls := map[string]int{}
			for method, n := range e.fake.Counts() {
				if n != before[method] {
					calls[method] = n - before[method]
				}
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Errorf("Abort made the calls %v, want %v", calls, tc.wantCalls)
			}
			if files := e.objects(false); len(files) != 0 {
				t.Errorf("files at the fake after Delete and Abort %s, want none", describe(files))
			}
		})
	}
}

// TestDrive_SupersededUploadLeavesOneObject (Task 4 report concern 5): a
// placement superseded after its last chunk, or after its Commit, keeps no
// copy at Drive, where names do not identify files: the outbox aborts the
// old session (g1-4) and Abort deletes the file that the session made, so
// the new bytes' upload leaves exactly one object at the key.
func TestDrive_SupersededUploadLeavesOneObject(t *testing.T) {
	for _, during := range []string{"the last chunk", "Commit"} {
		t.Run("during "+during, func(t *testing.T) { testRewritten(t, during, true) })
	}
}

// TestDrive_MismatchLeavesOneObject (P34): a spool file whose bytes change
// in place, same size, while its upload is at Commit fails verification.
// The outbox deletes the copy and aborts the session (Abort's status query
// answers the deleted file, and Abort's delete gets 404), then uploads the
// new bytes after Backoff. Exactly one object stays, and Abort costs the
// two calls that the Task 5 report names.
func TestDrive_MismatchLeavesOneObject(t *testing.T) { testRewritten(t, "Commit", false) }

// testRewritten runs the real outbox and catalog with the driver on the
// fake, and rewrites the segment's spool file while its upload is at during:
// with supersede, as the recorder does (new bytes and size, the placement
// changed in the same transaction); without, in place (same size, the
// placement unchanged), so that the copy fails P34's verification.
func testRewritten(t *testing.T, during string, supersede bool) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	d := e.drive(func(o *Options) { o.PartSize = 256 * kib })
	ctx := t.Context()
	dir := t.TempDir()
	cat, err := catalog.Open(ctx, filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	recDir := filepath.Join(dir, "rec")
	old, newer := randomBytes(600*kib, 1), randomBytes(500*kib, 2)
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
	q := outbox.New(cat, map[string]storage.Destination{"dst-1": d},
		outbox.Options{RecDir: recDir, HubID: "H", HubName: "hub", Rand: func() float64 { return 0 }})
	enqueue := func(s catalog.Segment) error {
		return cat.WithTx(ctx, func(tx *sql.Tx) error {
			if s.Bytes != int64(len(old)) {
				if err := catalog.PutSegmentTx(ctx, tx, s); err != nil {
					return err
				}
			}
			return q.Enqueue(ctx, tx, outbox.PlacementFor(s, "dst-1", catalog.ClassContinuous))
		})
	}
	if err := enqueue(seg); err != nil {
		t.Fatal(err)
	}

	// The recorder rewrites the file while the old bytes' upload is at its
	// last chunk or its Commit: the row and the placement change in one
	// transaction, as Task 7b's callback does.
	var once sync.Once
	var hookErr error
	rewritten := make(chan struct{})
	lastChunk := fmt.Sprintf("-%d/%d", len(old)-1, len(old))
	e.setHook(func(r *http.Request) {
		hit := (during == "the last chunk" && r.Method == http.MethodPut && strings.HasSuffix(r.Header.Get("Content-Range"), lastChunk)) ||
			(during == "Commit" && r.Method == http.MethodGet && r.URL.Query().Get("fields") == "id,md5Checksum,size")
		if hit {
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
	wantHash := md5Of(t, newer)
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
	files := e.objects(false)
	name := path.Base(string(outbox.KeyFor("H", "hub", seg, catalog.ClassContinuous)))
	if len(files) != 1 || files[0].Name != name || !bytes.Equal(files[0].Data, newer) || p.RemoteRef != files[0].ID {
		t.Errorf("files at the fake %s (the placement's %s), want one: %s with the new %d bytes", describe(files), p.RemoteRef, name, len(newer))
	}
	counts := e.fake.Counts()
	if !supersede {
		// Delete of the bad copy, then Abort's status query, which answers
		// that copy, and Abort's delete of it, which answers 404.
		if counts["files.delete"] != 2 || counts["upload.status"] != 1 {
			t.Errorf("%d files.delete and %d upload.status calls, want 2 and 1: the outbox's Delete, then Abort's status query and delete",
				counts["files.delete"], counts["upload.status"])
		}
		return
	}
	if n := counts["files.delete"]; n != 1 {
		t.Errorf("%d files.delete calls, want 1: the old copy", n)
	}
}

// TestDrive_RecreatesFoldersTheOwnerDeleted (P35, Task 5 review M1 and M7):
// after the owner deletes a folder that the store holds, the next upload
// makes it again with its stored ID on its first attempt, going up as many
// levels as are gone: through ensure for a new day's folder, and through
// into for a day folder that the store holds.
func TestDrive_RecreatesFoldersTheOwnerDeleted(t *testing.T) {
	for _, tc := range []struct {
		name    string
		deleted string // the folder that the owner deletes, by its path in the store
		key     storage.ObjectKey
	}{
		{name: "a new day under a deleted camera folder", deleted: "ipcam/hub/c1", key: "hub/c1/2026-10-18/a.mp4"},
		{name: "a new day under a deleted root", deleted: "ipcam", key: "hub/c1/2026-10-18/a.mp4"},
		{name: "a stored day under a deleted hub folder", deleted: "ipcam/hub", key: "hub/c1/2026-10-17/b.mp4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.clock.SetStep(time.Second)
			d := e.drive()
			uploadAll(t, d, "hub/c1/2026-10-17/a.mp4", randomBytes(1000, 1))
			ids := map[string]string{}
			for _, p := range []string{"ipcam", "ipcam/hub", "ipcam/hub/c1"} {
				ids[p] = e.folders.get(p)
			}
			e.request(http.MethodDelete, "/drive/v3/files/"+e.folders.get(tc.deleted), nil)

			data := randomBytes(2000, 2)
			ref, _ := uploadAll(t, d, tc.key, data) // fails the test unless its first attempt succeeds
			dir := path.Join("ipcam", path.Dir(string(tc.key)))
			ids[dir] = e.folders.get(dir)
			byID := map[string]fakegdrive.Object{}
			for _, o := range e.fake.Objects() {
				byID[o.ID] = o
			}
			parent := "root"
			for _, p := range []string{"ipcam", "ipcam/hub", "ipcam/hub/c1", dir} {
				o, ok := byID[ids[p]]
				if !ok || !o.Folder || o.Name != path.Base(p) || o.Parent != parent {
					t.Errorf("the folder %s: %+v (found %t), want %s with its stored ID %s in %s", p, o, ok, path.Base(p), ids[p], parent)
				}
				parent = ids[p]
			}
			if f, ok := byID[ref.ID]; !ok || f.Parent != ids[dir] || !bytes.Equal(f.Data, data) {
				t.Errorf("the upload's file %+v (found %t), want its bytes in %s", f.Name, ok, ids[dir])
			}
		})
	}
}

// TestDrive_TokenFailureSpendsNoCreateSlot (Task 5 review M6): a create whose
// token the token source cannot give spends no slot of the 2-per-second
// bucket, whether the slot is a folder's, a session's or a small file's: at
// one instant, after any number of token failures, two creates still go.
func TestDrive_TokenFailureSpendsNoCreateSlot(t *testing.T) {
	e := newEnv(t) // the clock stands still
	d := e.drive()
	tokenFails := func(what string, call func() error) {
		t.Helper()
		e.tokens.set("", errors.New("oauth2: cannot fetch token"))
		for i := range 3 {
			if err := call(); !errors.Is(err, storage.ErrTransient) {
				t.Fatalf("%s %d without a token: %v, want the token source's ErrTransient", what, i, err)
			}
		}
		e.tokens.set("tok-1", nil)
	}
	beginUpload := func(key storage.ObjectKey) func() error {
		return func() error {
			_, err := d.BeginUpload(t.Context(), key, 10, storage.Meta{})
			return err
		}
	}

	tokenFails("a folder's create", beginUpload("a.mp4"))
	if err := beginUpload("a.mp4")(); err != nil { // the root folder and the session: two creates
		t.Fatalf("BeginUpload after the token failures: %v, want the two slots of this second", err)
	}
	e.clock.Advance(time.Second)
	tokenFails("a session's create", beginUpload("b.mp4"))
	for _, key := range []storage.ObjectKey{"b.mp4", "c.mp4"} {
		if err := beginUpload(key)(); err != nil {
			t.Fatalf("BeginUpload(%s) after the token failures: %v, want one of the two slots of this second", key, err)
		}
	}
	e.clock.Advance(time.Second)
	putSmall := func() error {
		_, err := d.PutSmall(t.Context(), "x.txt", []byte("x"))
		return err
	}
	tokenFails("a small file's create", putSmall)
	if err := putSmall(); err != nil {
		t.Fatalf("PutSmall after the token failures: %v, want a slot of this second", err)
	}
	if err := beginUpload("d.mp4")(); err != nil {
		t.Fatalf("BeginUpload after PutSmall: %v, want the second slot of this second", err)
	}
}

// TestDrive_ResetInAMediaBody (Task 5 review M7): a connection reset in the
// middle of a files.get.media body ends the read with ErrTransient, after the
// bytes that came, and the error's text names no URL.
func TestDrive_ResetInAMediaBody(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	d := e.drive()
	data := randomBytes(8*kib, 1)
	ref, _ := uploadAll(t, d, "c1/a.mp4", data)
	e.setServe(func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Query().Get("alt") != "media" {
			return false
		}
		w.Header().Set("Content-Length", "4096")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-4095/%d", len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[:1000])
		rc := http.NewResponseController(w)
		_ = rc.Flush()
		conn, _, err := rc.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return true
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = conn.Close()
		return true
	})
	r, err := d.OpenRange(t.Context(), ref, 0, 4096)
	if err != nil {
		t.Fatalf("OpenRange: %v", err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if !errors.Is(err, storage.ErrTransient) || !bytes.Equal(got, data[:1000]) {
		t.Fatalf("the read gave %d bytes and %v, want the 1000 bytes sent and ErrTransient", len(got), err)
	}
	for _, leak := range []string{e.srv.URL, "http", "alt=media", ref.ID, "tok-1"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error's text %q holds %q", err, leak)
		}
	}
}

// TestDrive_AbortCancelsAnIncompleteSession (Task 5 review, out of scope
// item): Abort of a session that does not hold every byte cancels it with a
// DELETE of its URI, so the provider (and the fake) frees the bytes it held;
// the session then answers 499, which is ErrSessionExpired.
func TestDrive_AbortCancelsAnIncompleteSession(t *testing.T) {
	e := newEnv(t)
	e.clock.SetStep(time.Second)
	d := e.drive(func(o *Options) { o.PartSize = 256 * kib })
	data := randomBytes(600*kib, 1)
	st, err := d.BeginUpload(t.Context(), "c1/a.mp4", int64(len(data)), storage.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.PutChunk(t.Context(), &st, 0, bytes.NewReader(data[:256*kib]), 256*kib); err != nil {
		t.Fatal(err)
	}
	if n := e.fake.PendingBytes(); n != 256*kib {
		t.Fatalf("the fake holds %d bytes of open sessions, want 256 KiB", n)
	}
	before := e.fake.Counts()
	if err := d.Abort(t.Context(), &st); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	calls := map[string]int{}
	for method, n := range e.fake.Counts() {
		if n != before[method] {
			calls[method] = n - before[method]
		}
	}
	if want := map[string]int{"upload.status": 1, "upload.cancel": 1}; !reflect.DeepEqual(calls, want) {
		t.Errorf("Abort made the calls %v, want %v", calls, want)
	}
	if n := e.fake.PendingBytes(); n != 0 {
		t.Errorf("the fake holds %d bytes of open sessions after Abort, want 0", n)
	}
	if _, err := d.Resume(t.Context(), &st); !errors.Is(err, storage.ErrSessionExpired) {
		t.Errorf("Resume of the cancelled session: %v, want ErrSessionExpired", err)
	}
	if files := e.objects(false); len(files) != 0 {
		t.Errorf("files at the fake %s, want none", describe(files))
	}
}
