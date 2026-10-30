package outbox

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
)

// The test binary is also the kill test's helper process: with killHelperEnv
// set to 1 it runs a queue on the catalog and spool that the other variables
// name, against the fake destination that the parent test serves at
// killURLEnv, until the placement is verified or it is killed.
const (
	killHelperEnv  = "OUTBOX_KILL_HELPER"
	killCatalogEnv = "OUTBOX_KILL_CATALOG"
	killRecDirEnv  = "OUTBOX_KILL_RECDIR"
	killURLEnv     = "OUTBOX_KILL_URL"
)

func TestMain(m *testing.M) {
	if os.Getenv(killHelperEnv) == "1" {
		os.Exit(runKillHelper())
	}
	os.Exit(m.Run())
}

// runKillHelper runs the helper process; it exits 0 once its queue verified
// a placement.
func runKillHelper() int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cat, err := catalog.Open(ctx, os.Getenv(killCatalogEnv))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer func() { _ = cat.Close() }()
	base := os.Getenv(killURLEnv)
	obs := &httpObserver{url: base, verified: cancel}
	q := New(cat, map[string]storage.Destination{"dst-a": &httpDest{url: base}}, Options{
		RecDir: os.Getenv(killRecDirEnv), HubID: "H", HubName: "hub", Observer: obs,
	})
	if err := q.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if !obs.done {
		fmt.Fprintln(os.Stderr, "the helper's queue verified nothing")
		return 1
	}
	return 0
}

// TestOutbox_ResumesFromLastOffset (P25, §9.9): a hub killed during an
// upload, once offset K is persisted, leaves the placement uploading; the
// next hub resets it to pending before its first poll and resumes it at K,
// sending only the rest, with one Commit and one object.
func TestOutbox_ResumesFromLastOffset(t *testing.T) {
	dir := t.TempDir()
	catPath, recDir := filepath.Join(dir, "catalog.db"), filepath.Join(dir, "rec")
	data := randomBytes(5*fakePart+123, 1)
	const k = 2 * fakePart
	s := catalog.Segment{Cam: "c1", Profile: "main", Path: "c1/main/2026-10-17/081500.mp4",
		StartMs: time.Date(2026, 10, 17, 8, 15, 0, 0, time.UTC).UnixMilli(), Bytes: int64(len(data)), State: catalog.StateComplete}
	s.EndMs = s.StartMs + 300_000
	if err := os.MkdirAll(filepath.Join(recDir, "c1", "main", "2026-10-17"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recDir, filepath.FromSlash(s.Path)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Open(t.Context(), catPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.PutSegment(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	segs, err := cat.Segments(t.Context())
	if err != nil || len(segs) != 1 {
		t.Fatalf("Segments = %+v, %v", segs, err)
	}
	if err := cat.WithTx(t.Context(), func(tx *sql.Tx) error {
		_, err := catalog.PutPlacementTx(t.Context(), tx, PlacementFor(segs[0], "dst-a", catalog.ClassContinuous))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_ = cat.Close()

	f := newFakeDest(storage.KindGoogleDrive, time.Now)
	rec := &helperReports{}
	blocked, release := make(chan struct{}), make(chan struct{})
	var holdOnce sync.Once
	f.setFault(func(ctx context.Context, c fakeCall) error {
		if c.Method != "PutChunk" || c.Off != k || !rec.persisted(k) {
			return nil
		}
		hold := false
		holdOnce.Do(func() { hold = true })
		if !hold {
			return nil
		}
		close(blocked)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return storage.ErrTransient
		}
	})
	srv := httptest.NewServer(serveFake(f, rec))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	env := append(os.Environ(), killHelperEnv+"=1", killCatalogEnv+"="+catPath, killRecDirEnv+"="+recDir, killURLEnv+"="+srv.URL)
	first, firstOut := startKillHelper(t, env)
	select {
	case <-blocked:
	case <-time.After(time.Minute):
		_ = first.Process.Kill()
		t.Fatalf("the first helper never sent the chunk after offset %d; its output:\n%s", k, firstOut)
	}
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()

	cat, err = catalog.Open(t.Context(), catPath)
	if err != nil {
		t.Fatal(err)
	}
	ps, err := cat.Placements(t.Context())
	if err != nil || len(ps) != 1 || ps[0].State != catalog.PlacementUploading {
		t.Fatalf("after the kill the placements are %+v, %v; want the one uploading", ps, err)
	}
	if _, off, ok, err := cat.UploadSession(t.Context(), ps[0].ID); err != nil || !ok || off != k {
		t.Fatalf("after the kill the upload session is at %d, %v, %v; want %d", off, ok, err, k)
	}
	_ = cat.Close()

	mark := len(f.callsOf(""))
	queuedBefore := len(rec.queuedStates())
	second, secondOut := startKillHelper(t, env)
	waited := make(chan error, 1)
	go func() { waited <- second.Wait() }()
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("the second helper: %v; its output:\n%s", err, secondOut)
		}
	case <-time.After(time.Minute):
		_ = second.Process.Kill()
		t.Fatalf("the second helper did not finish within 1 min; its output:\n%s", secondOut)
	}

	if states := rec.queuedStates()[queuedBefore:]; len(states) != 1 || states[0] != catalog.PlacementPending {
		t.Errorf("the second helper first read the placement as %q, want pending: Run resets it before its first poll", states)
	}
	var chunks []fakeCall
	commits := 0
	for _, c := range f.callsOf("")[mark:] {
		switch c.Method {
		case "PutChunk":
			chunks = append(chunks, c)
		case "Commit":
			commits++
		}
	}
	var sent int64
	for _, c := range chunks {
		sent += c.N
	}
	if len(chunks) == 0 || chunks[0].Off != k || sent != int64(len(data))-k {
		t.Errorf("after the restart the chunks %+v, want the first at %d and %d bytes in all", chunks, k, int64(len(data))-k)
	}
	if commits != 1 || f.count("Commit") != 1 {
		t.Errorf("%d Commit calls after the restart, %d in all; want 1", commits, f.count("Commit"))
	}
	objects := f.objectMap()
	if len(objects) != 1 {
		t.Fatalf("%d objects, want 1", len(objects))
	}
	for key, got := range objects {
		if !bytes.Equal(got, data) {
			t.Errorf("object %s holds %d bytes, not the segment's %d", key, len(got), len(data))
		}
	}
}

// startKillHelper starts the test binary as the helper process with env,
// and returns it and its combined output, read once it has exited.
func startKillHelper(t *testing.T, env []string) (*exec.Cmd, *bytes.Buffer) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec // G204: the test binary itself
	cmd.Env = env
	out := &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd, out
}

// helperReports is what the helpers' observers reported to the parent.
type helperReports struct {
	mu      sync.Mutex
	offsets map[int64]bool // the persisted offsets that Progress reported
	queued  []string
}

func (r *helperReports) persisted(off int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.offsets[off]
}

func (r *helperReports) queuedStates() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.queued...)
}

// rpcRequest and rpcReply carry a Destination call between httpDest and
// serveFake.
type rpcRequest struct {
	State storage.UploadState
	Key   storage.ObjectKey
	Size  int64
	Off   int64
	N     int64
	Data  []byte
	Ref   storage.ObjectRef
}

type rpcReply struct {
	State storage.UploadState
	Next  int64
	Ref   storage.ObjectRef
	Err   string
	// Expired is true when Err is ErrSessionExpired.
	Expired bool
}

// serveFake serves f's upload methods at /<method>, and records the
// helpers' observer reports at /report.
func serveFake(f *fakeDest, rec *helperReports) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /report", func(_ http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		rec.mu.Lock()
		defer rec.mu.Unlock()
		switch q.Get("event") {
		case "progress":
			off, _ := strconv.ParseInt(q.Get("offset"), 10, 64)
			if rec.offsets == nil {
				rec.offsets = map[int64]bool{}
			}
			rec.offsets[off] = true
		case "queued":
			rec.queued = append(rec.queued, q.Get("state"))
		}
	})
	mux.HandleFunc("POST /{method}", func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		var reply rpcReply
		var err error
		switch r.PathValue("method") {
		case "BeginUpload":
			reply.State, err = f.BeginUpload(ctx, req.Key, req.Size, storage.Meta{})
		case "PutChunk":
			err = f.PutChunk(ctx, &req.State, req.Off, bytes.NewReader(req.Data), req.N)
			reply.State = req.State
		case "Resume":
			reply.Next, err = f.Resume(ctx, &req.State)
			reply.State = req.State
		case "Commit":
			reply.Ref, err = f.Commit(ctx, &req.State)
			reply.State = req.State
		case "Abort":
			err = f.Abort(ctx, &req.State)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			reply.Err, reply.Expired = err.Error(), errors.Is(err, storage.ErrSessionExpired)
		}
		_ = json.NewEncoder(w).Encode(reply)
	})
	return mux
}

// httpDest is the helper's Destination: the parent's fakeDest over HTTP.
type httpDest struct{ url string }

var _ storage.Destination = (*httpDest)(nil)

func (d *httpDest) call(ctx context.Context, method string, req rpcRequest) (rpcReply, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return rpcReply{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url+"/"+method, bytes.NewReader(body))
	if err != nil {
		return rpcReply{}, err
	}
	resp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return rpcReply{}, fmt.Errorf("%s: %w: %w", method, storage.ErrTransient, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var reply rpcReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return rpcReply{}, fmt.Errorf("%s: %w: %w", method, storage.ErrTransient, err)
	}
	switch {
	case reply.Expired:
		return reply, fmt.Errorf("%s: %s: %w", method, reply.Err, storage.ErrSessionExpired)
	case reply.Err != "":
		return reply, fmt.Errorf("%s: %s: %w", method, reply.Err, storage.ErrTransient)
	}
	return reply, nil
}

func (d *httpDest) Kind() storage.Kind         { return storage.KindGoogleDrive }
func (d *httpDest) Capabilities() storage.Caps { return fakeCaps() }

func (d *httpDest) Probe(context.Context) (storage.ProbeReport, error) {
	return storage.ProbeReport{}, storage.ErrPermanent
}

func (d *httpDest) BeginUpload(ctx context.Context, key storage.ObjectKey, size int64, _ storage.Meta) (storage.UploadState, error) {
	reply, err := d.call(ctx, "BeginUpload", rpcRequest{Key: key, Size: size})
	return reply.State, err
}

func (d *httpDest) PutChunk(ctx context.Context, st *storage.UploadState, off int64, r io.Reader, n int64) error {
	data, err := io.ReadAll(io.LimitReader(r, n))
	if err != nil {
		return err
	}
	reply, err := d.call(ctx, "PutChunk", rpcRequest{State: *st, Off: off, N: n, Data: data})
	if err == nil {
		*st = reply.State
	}
	return err
}

func (d *httpDest) Resume(ctx context.Context, st *storage.UploadState) (int64, error) {
	reply, err := d.call(ctx, "Resume", rpcRequest{State: *st})
	if err == nil {
		*st = reply.State
	}
	return reply.Next, err
}

func (d *httpDest) Commit(ctx context.Context, st *storage.UploadState) (storage.ObjectRef, error) {
	reply, err := d.call(ctx, "Commit", rpcRequest{State: *st})
	if err == nil {
		*st = reply.State
	}
	return reply.Ref, err
}

func (d *httpDest) Abort(ctx context.Context, st *storage.UploadState) error {
	_, err := d.call(ctx, "Abort", rpcRequest{State: *st})
	return err
}

func (d *httpDest) Stat(context.Context, storage.ObjectRef) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, storage.ErrPermanent
}

func (d *httpDest) OpenRange(context.Context, storage.ObjectRef, int64, int64) (io.ReadCloser, error) {
	return nil, storage.ErrPermanent
}

func (d *httpDest) PlaybackURL(context.Context, storage.ObjectRef, time.Duration) (string, error) {
	return "", storage.ErrProxyRequired
}

func (d *httpDest) Delete(context.Context, storage.ObjectRef) (storage.DeleteOutcome, error) {
	return storage.DeletedRecycled, storage.ErrPermanent
}

func (d *httpDest) Quota(context.Context) (storage.Quota, error) {
	return storage.Quota{Unknown: true}, nil
}

func (d *httpDest) PutSmall(context.Context, storage.ObjectKey, []byte) (storage.ObjectRef, error) {
	return storage.ObjectRef{}, storage.ErrPermanent
}

func (d *httpDest) List(context.Context, storage.ObjectKey) iter.Seq2[storage.ObjectInfo, error] {
	return func(yield func(storage.ObjectInfo, error) bool) { yield(storage.ObjectInfo{}, storage.ErrPermanent) }
}

// httpObserver is the helper's Observer: it reports to the parent each
// persisted offset and the state in which the queue first read the
// placement, and cancels the helper's queue once the placement is verified.
type httpObserver struct {
	url      string
	verified context.CancelFunc
	done     bool
}

func (o *httpObserver) report(values url.Values) {
	resp, err := http.Post(o.url+"/report?"+values.Encode(), "text/plain", nil) //nolint:gosec,noctx // G107: the parent test's server
	if err == nil {
		_ = resp.Body.Close()
	}
}

func (o *httpObserver) Queued(p catalog.Placement) {
	o.report(url.Values{"event": {"queued"}, "state": {p.State}})
}

func (o *httpObserver) Resumed(catalog.Placement, int64) {}

func (o *httpObserver) Progress(_ catalog.Placement, confirmedOffset, _ int64) {
	o.report(url.Values{"event": {"progress"}, "offset": {strconv.FormatInt(confirmedOffset, 10)}})
}

func (o *httpObserver) Verified(catalog.Placement, storage.ObjectRef, storage.CallCounts) {
	o.done = true
	o.verified()
}

func (o *httpObserver) Failed(_ catalog.Placement, reason string, attempts int) {
	fmt.Fprintf(os.Stderr, "failed: %s, %d attempts\n", reason, attempts)
}

func (o *httpObserver) DestState(string, DestState, string) {}

func (o *httpObserver) LocalError(err error) { fmt.Fprintln(os.Stderr, "local error:", err) }
