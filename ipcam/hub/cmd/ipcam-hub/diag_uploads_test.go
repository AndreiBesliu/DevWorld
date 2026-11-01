package main

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/outbox"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakegdrive "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/gdrive"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/gdrive"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/local"
)

// TestDiag_NoCatalogCreatesNothing (g2-11): on a data directory where no run
// left a catalog, diag readopen, dest-probe and uploads refuse with "no
// catalog" and create neither state/catalog.db nor state/hub_id. With a
// catalog and no state/hub_id, as a run without uploads leaves it, readopen
// reads, dest-probe and uploads refuse with "no hub id", and still no
// state/hub_id is there.
func TestDiag_NoCatalogCreatesNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "local")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(t.TempDir(), "data")
	config := uploadConfig(t, dataDir, uploadsTo(localDest(root), 0))
	commands := map[string][]string{
		"readopen":   {"readopen", "--config", config, "--cam", testCam, "--profile", testProfile},
		"dest-probe": {"dest-probe", "--config", config, "--dest", "dst-1"},
		"uploads":    {"uploads", "--config", config, "--list-remote"},
	}
	absent := func(when string) {
		t.Helper()
		for _, name := range []string{"catalog.db", "hub_id"} {
			if _, err := os.Stat(filepath.Join(dataDir, "state", name)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s: state/%s is there (%v)", when, name, err)
			}
		}
	}
	for name, args := range commands {
		if code, lines, stderr := runDiag(t, args...); code != exitFatal || len(lines) != 0 || !strings.Contains(stderr, "no catalog") {
			t.Errorf("diag %s with no catalog = %d, %v, %q; want exit 1 and no catalog", name, code, lines, stderr)
		}
	}
	absent("with no catalog")

	if err := os.MkdirAll(filepath.Join(dataDir, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Open(t.Context(), filepath.Join(dataDir, "state", "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.Close(); err != nil {
		t.Fatal(err)
	}
	if code, lines, stderr := runDiag(t, commands["readopen"]...); code != exitOK || len(lines) != 1 {
		t.Errorf("diag readopen with no hub id = %d, %v, %q; want its line, exit 0", code, lines, stderr)
	}
	for _, name := range []string{"dest-probe", "uploads"} {
		if code, lines, stderr := runDiag(t, commands[name]...); code != exitFatal || len(lines) != 0 || !strings.Contains(stderr, "no hub id") {
			t.Errorf("diag %s with no hub id = %d, %v, %q; want exit 1 and no hub id", name, code, lines, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "state", "hub_id")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state/hub_id is there (%v)", err)
	}
}

// stepClock is a clock that moves on a second at each reading: Drive's
// create limit (2 a second) never holds the test's uploads back.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Second)
	return c.now
}

// uploadsLine returns diag uploads' line of dest.
func uploadsLine(t *testing.T, lines []map[string]any, dest string) map[string]any {
	t.Helper()
	for _, l := range lines {
		if l["schema"] != "ipcam.diag.uploads/1" {
			t.Errorf("line %v has no uploads schema", l)
		}
		if l["dest"] == dest {
			return l
		}
	}
	t.Fatalf("no line of %s in %v", dest, lines)
	return nil
}

// wantCounts fails t unless line's fields hold want.
func wantCounts(t *testing.T, line map[string]any, want map[string]float64) {
	t.Helper()
	for k, v := range want {
		if got, ok := line[k].(float64); !ok || got != v {
			t.Errorf("diag uploads %v: %s is %v, want %v", line, k, line[k], v)
		}
	}
}

// TestDiagUploads_CountsMissingAndDuplicates (OK14, MI5): with
// --list-remote, diag uploads lists each day folder of the destination
// through List, the reconciliation path: a fake Drive that holds one key
// twice and lacks another reports duplicates 1 and missing 1, and the list
// calls it made.
func TestDiagUploads_CountsMissingAndDuplicates(t *testing.T) {
	withTestToken(t)
	fake := fakegdrive.New(fakegdrive.Options{})
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	dest := map[string]any{"id": "dst-1", "kind": "GOOGLE_DRIVE", "folder": "ipcam", "tokenRef": "dst-1", "baseUrl": srv.URL, "uploadBaseUrl": "", "partSize": 256 << 10}
	f := newDiagFixture(t, uploadsTo(dest, 0), testHubID)
	clock := &stepClock{now: time.Now()}
	d, err := gdrive.New(gdrive.Options{DestID: "dst-1", HTTPClient: srv.Client(), Tokens: mustTokens(t), BaseURL: srv.URL, PartSize: 256 << 10,
		Clock: clock.Now, Folders: f.cat, HubID: testHubID})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC).UnixMilli()
	twice := seedSegment(t, f, start, randomFixtureBytes(1000, 1))
	data, err := os.ReadFile(filepath.Join(f.dataDir, "rec", filepath.FromSlash(twice.Path))) //nolint:gosec // a file of the test
	if err != nil {
		t.Fatal(err)
	}
	uploadObject(t, d, outbox.KeyFor(testHubID, "hub", twice, catalog.ClassContinuous), data) // an earlier copy at the key
	seedVerified(t, f, twice, d, "dst-1")
	seedVerified(t, f, seedSegment(t, f, start+10_000, randomFixtureBytes(1000, 2)), d, "dst-1")
	lacking := seedSegment(t, f, start+20_000, randomFixtureBytes(1000, 3))
	seedPlacement(t, f, lacking, "dst-1", catalog.PlacementPending, storage.ObjectRef{})

	code, lines, stderr := runDiag(t, "uploads", "--config", f.config, "--list-remote")
	if code != exitOK {
		t.Fatalf("diag uploads = %d (%s), want 0", code, stderr)
	}
	line := uploadsLine(t, lines, "dst-1")
	wantCounts(t, line, map[string]float64{"segments": 3, "verified": 2, "pending": 1, "failed": 0, "deleted": 0,
		"missing": 1, "duplicates": 1, "hashMismatches": 0})
	if calls, _ := line["listCalls"].(float64); calls <= 0 {
		t.Errorf("listCalls %v, want the list calls made", line["listCalls"])
	}

	code, lines, _ = runDiag(t, "uploads", "--config", f.config)
	if code != exitOK {
		t.Fatalf("diag uploads without --list-remote = %d, want 0", code)
	}
	line = uploadsLine(t, lines, "dst-1")
	wantCounts(t, line, map[string]float64{"segments": 3, "verified": 2, "pending": 1, "listCalls": 0})
	for _, k := range []string{"missing", "duplicates", "hashMismatches"} {
		if v, ok := line[k]; !ok || v != nil {
			t.Errorf("diag uploads without --list-remote: %s is %v, want null: not measured", k, v)
		}
	}
}

// TestDiagUploads_DeletedPlacementNotMissing (P35): after dest-probe
// --delete-segment, the deleted placement's segment is counted as deleted,
// and left out of segments, verified and missing.
func TestDiagUploads_DeletedPlacementNotMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "local")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	f := newDiagFixture(t, uploadsTo(localDest(root), 0), testHubID)
	d, err := local.New(root)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC).UnixMilli()
	for i := range 3 {
		seedVerified(t, f, seedSegment(t, f, start+int64(i)*10_000, randomFixtureBytes(500, byte(i))), d, "dst-1")
	}
	if code, _, stderr := runDiag(t, "dest-probe", "--config", f.config, "--dest", "dst-1", "--delete-segment", "--poll", "1ms", "--for", "1ms"); code != exitOK {
		t.Fatalf("diag dest-probe = %d (%s)", code, stderr)
	}
	code, lines, stderr := runDiag(t, "uploads", "--config", f.config, "--list-remote")
	if code != exitOK {
		t.Fatalf("diag uploads = %d (%s), want 0", code, stderr)
	}
	wantCounts(t, uploadsLine(t, lines, "dst-1"), map[string]float64{"segments": 2, "verified": 2, "deleted": 1, "missing": 0, "duplicates": 0, "hashMismatches": 0})
}

// TestDiagUploads_CountsOnlyTheRouteWindow (D1-B): a complete segment that
// starts before its route's fromMs is in no count: one without a placement
// is neither in segments nor missing, and one with a verified placement, of
// an earlier configuration, is not in verified.
func TestDiagUploads_CountsOnlyTheRouteWindow(t *testing.T) {
	root := filepath.Join(t.TempDir(), "local")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	fromMs := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC).UnixMilli()
	f := newDiagFixture(t, uploadsTo(localDest(root), fromMs), testHubID)
	d, err := local.New(root)
	if err != nil {
		t.Fatal(err)
	}
	seedSegment(t, f, fromMs-10_000, randomFixtureBytes(500, 1))                                 // before the window, no placement
	seedVerified(t, f, seedSegment(t, f, fromMs-20_000, randomFixtureBytes(500, 2)), d, "dst-1") // before the window, verified
	seedVerified(t, f, seedSegment(t, f, fromMs+10_000, randomFixtureBytes(500, 3)), d, "dst-1") // in it
	code, lines, stderr := runDiag(t, "uploads", "--config", f.config, "--list-remote")
	if code != exitOK {
		t.Fatalf("diag uploads = %d (%s), want 0", code, stderr)
	}
	wantCounts(t, uploadsLine(t, lines, "dst-1"), map[string]float64{"segments": 1, "verified": 1, "pending": 0, "missing": 0, "duplicates": 0, "hashMismatches": 0})
}

// TestDiagUploads_HashMismatch (OK14): a copy whose bytes are not the
// segment's, the destination's hash told apart from Digest of the local
// file, is a hash mismatch.
func TestDiagUploads_HashMismatch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "local")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	f := newDiagFixture(t, uploadsTo(localDest(root), 0), testHubID)
	d, err := local.New(root)
	if err != nil {
		t.Fatal(err)
	}
	s := seedSegment(t, f, time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC).UnixMilli(), randomFixtureBytes(500, 1))
	seedVerified(t, f, s, d, "dst-1")
	copyPath := filepath.Join(root, filepath.FromSlash(string(outbox.KeyFor(testHubID, "hub", s, catalog.ClassContinuous))))
	if err := os.WriteFile(copyPath, randomFixtureBytes(500, 9), 0o600); err != nil {
		t.Fatal(err)
	}
	code, lines, stderr := runDiag(t, "uploads", "--config", f.config, "--list-remote")
	if code != exitOK {
		t.Fatalf("diag uploads = %d (%s), want 0", code, stderr)
	}
	wantCounts(t, uploadsLine(t, lines, "dst-1"), map[string]float64{"segments": 1, "missing": 0, "duplicates": 0, "hashMismatches": 1})
}
