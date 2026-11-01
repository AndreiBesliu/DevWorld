package main

import (
	"bytes"
	"database/sql"
	"errors"
	"math/rand/v2"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/catalog"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/outbox"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage"
	fakegraph "github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/fakes/graph"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/local"
	"github.com/AndreiBesliu/DevWorld/ipcam/hub/internal/storage/onedrive"
)

const testHubID = "01a19700-0000-7000-8000-000000000001"

// mustTokens is a token source of the tests' token.
func mustTokens(t *testing.T) oauth2.TokenSource {
	t.Helper()
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: testTokenFromParts(), TokenType: "Bearer"})
}

// withTestToken makes the drivers of this process take the tests' token.
func withTestToken(t *testing.T) {
	t.Helper()
	was := testToken
	testToken = testTokenFromParts()
	t.Cleanup(func() { testToken = was })
}

// seedSegment writes data as the file of a complete clear segment of
// cam1/sub that starts at startMs under f's rec/, and its row.
func seedSegment(t *testing.T, f *diagFixture, startMs int64, data []byte) catalog.Segment {
	t.Helper()
	start := time.UnixMilli(startMs).UTC()
	rel := testCam + "/" + testProfile + "/" + start.Format(time.DateOnly) + "/" + start.Format("150405") + "_000000.mp4"
	path := filepath.Join(f.dataDir, "rec", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s := catalog.Segment{Cam: testCam, Profile: testProfile, Path: rel, StartMs: startMs, EndMs: startMs + 10_000, Bytes: int64(len(data))}
	if err := f.cat.PutSegment(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	segs, err := f.cat.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range segs {
		if row.Path == rel {
			return row
		}
	}
	t.Fatalf("no row for %s", rel)
	return catalog.Segment{}
}

// seedPlacement puts s's continuous placement on destID in state, with ref
// as its remote copy when it is verified, and returns its row.
func seedPlacement(t *testing.T, f *diagFixture, s catalog.Segment, destID, state string, ref storage.ObjectRef) catalog.Placement {
	t.Helper()
	ctx := t.Context()
	err := f.cat.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := catalog.PutPlacementTx(ctx, tx, outbox.PlacementFor(s, destID, catalog.ClassContinuous))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := f.cat.Placements(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.ObjectID != s.ID || p.DestinationID != destID {
			continue
		}
		if state != catalog.PlacementPending {
			next := p
			next.State, next.RemoteRef, next.RemoteHash = state, ref.ID, ref.Hash
			if err := f.cat.UpdatePlacement(ctx, next, p.State, p.Bytes); err != nil {
				t.Fatal(err)
			}
			p = next
		}
		return p
	}
	t.Fatalf("no placement of %s on %s", s.ID, destID)
	return catalog.Placement{}
}

// uploadObject uploads data to key on d, in d's chunks, and commits it.
func uploadObject(t *testing.T, d storage.Destination, key storage.ObjectKey, data []byte) storage.ObjectRef {
	t.Helper()
	ctx := t.Context()
	st, err := d.BeginUpload(ctx, key, int64(len(data)), storage.Meta{ContentType: "video/mp4"})
	if err != nil {
		t.Fatalf("BeginUpload(%s): %v", key, err)
	}
	part := d.Capabilities().PartSize
	for off := int64(0); off < int64(len(data)); off = st.Offset {
		n := min(part, int64(len(data))-off)
		if err := d.PutChunk(ctx, &st, off, bytes.NewReader(data[off:off+n]), n); err != nil {
			t.Fatalf("PutChunk(%s, %d): %v", key, off, err)
		}
	}
	ref, err := d.Commit(ctx, &st)
	if err != nil {
		t.Fatalf("Commit(%s): %v", key, err)
	}
	return ref
}

// seedVerified uploads s's file to d as destID's copy and puts its verified
// placement.
func seedVerified(t *testing.T, f *diagFixture, s catalog.Segment, d storage.Destination, destID string) (catalog.Placement, storage.ObjectRef) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dataDir, "rec", filepath.FromSlash(s.Path))) //nolint:gosec // a file of the test
	if err != nil {
		t.Fatal(err)
	}
	ref := uploadObject(t, d, outbox.KeyFor(f.hubID, "hub", s, catalog.ClassContinuous), data)
	return seedPlacement(t, f, s, destID, catalog.PlacementVerified, ref), ref
}

func placementByID(t *testing.T, f *diagFixture, id string) catalog.Placement {
	t.Helper()
	ps, err := f.cat.Placements(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no placement %s", id)
	return catalog.Placement{}
}

// TestDiagDestProbe_LocalAndFakeGraphQuotaAfterDelete (P35): diag dest-probe
// runs the destination's Probe and prints its fields. With --delete-segment
// it deletes the copy of the oldest verified placement, marks it deleted,
// then reads the quota every --poll for --for: on OneDrive, whose recycle
// bin keeps occupying the quota, inBin rises and free does not.
func TestDiagDestProbe_LocalAndFakeGraphQuotaAfterDelete(t *testing.T) {
	start := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC).UnixMilli()
	probe := func(t *testing.T, f *diagFixture, kind string) (map[string]any, map[string]any) {
		t.Helper()
		code, lines, stderr := runDiag(t, "dest-probe", "--config", f.config, "--dest", firstDest(f), "--delete-segment", "--poll", "10ms", "--for", "50ms")
		if code != exitOK || len(lines) != 1 {
			t.Fatalf("diag dest-probe = %d, %v (%s); want one line, exit 0", code, lines, stderr)
		}
		got := lines[0]
		if got["schema"] != "ipcam.diag.destprobe/1" || got["kind"] != kind || got["dest"] != firstDest(f) {
			t.Errorf("diag dest-probe %v, want the schema, kind %s and its destination", got, kind)
		}
		p, _ := got["probe"].(map[string]any)
		q, _ := p["quota"].(map[string]any)
		if p["writable"] != true || p["rangeRead"] != true || p["deletable"] != true || q == nil || q["total"] == nil || q["inBin"] == nil {
			t.Errorf("probe %v, want writable, rangeRead, deletable and a quota", p)
		}
		if _, ok := p["warnings"].([]any); !ok {
			t.Errorf("probe %v, want a warnings array", p)
		}
		return got, q
	}

	t.Run("LOCAL", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "local")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		f := newDiagFixture(t, uploadsTo(localDest(root), 0), testHubID)
		d, err := local.New(root)
		if err != nil {
			t.Fatal(err)
		}
		older, ref := seedVerified(t, f, seedSegment(t, f, start, randomFixtureBytes(3000, 1)), d, "dst-1")
		newer, _ := seedVerified(t, f, seedSegment(t, f, start+10_000, randomFixtureBytes(3000, 2)), d, "dst-1")
		got, _ := probe(t, f, "LOCAL")
		if samples, _ := got["quotaAfterDelete"].([]any); len(samples) < 2 {
			t.Errorf("quotaAfterDelete %v, want a sample every 10 ms for 50 ms", got["quotaAfterDelete"])
		}
		if p := placementByID(t, f, older.ID); p.State != catalog.PlacementDeleted {
			t.Errorf("the oldest placement is %s, want deleted", p.State)
		}
		if p := placementByID(t, f, newer.ID); p.State != catalog.PlacementVerified {
			t.Errorf("the newer placement is %s, want verified", p.State)
		}
		if _, err := d.Stat(t.Context(), ref); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Stat of the deleted copy = %v, want ErrNotFound", err)
		}
	})

	t.Run("fakegraph", func(t *testing.T) {
		withTestToken(t)
		fake := fakegraph.New(fakegraph.Options{})
		srv := httptest.NewServer(fake.Handler())
		t.Cleanup(srv.Close)
		dest := map[string]any{"id": "dst-od", "kind": "ONEDRIVE_PERSONAL", "folder": "", "tokenRef": "dst-od", "baseUrl": srv.URL, "uploadBaseUrl": "", "partSize": 320 << 10}
		f := newDiagFixture(t, uploadsTo(dest, 0), testHubID)
		d, err := onedrive.New(onedrive.Options{DestID: "dst-od", HubID: testHubID, HTTPClient: srv.Client(), Tokens: mustTokens(t), BaseURL: srv.URL, PartSize: 320 << 10})
		if err != nil {
			t.Fatal(err)
		}
		seedVerified(t, f, seedSegment(t, f, start, randomFixtureBytes(400<<10, 3)), d, "dst-od")
		got, before := probe(t, f, "ONEDRIVE_PERSONAL")
		samples, _ := got["quotaAfterDelete"].([]any)
		if len(samples) == 0 {
			t.Fatalf("quotaAfterDelete %v, want samples", got["quotaAfterDelete"])
		}
		for _, s := range samples {
			sample := s.(map[string]any)
			if sample["inBin"].(float64) < before["inBin"].(float64)+400<<10 || sample["free"].(float64) > before["free"].(float64) {
				t.Errorf("after the delete, sample %v against the probe's %v: want inBin up by the copy and free not up", sample, before)
			}
		}
		if bin := fake.Bin(); len(bin) == 0 || !strings.HasSuffix(bin[len(bin)-1].Path, "_cont_sub.mp4") {
			t.Errorf("the fake's recycle bin %v, want the deleted copy last", bin)
		}
	})
}

// TestDiagDestProbe_RefusedWhileTheHubRuns (C2-R11): diag dest-probe refuses
// while ipcam-hub holds the hub lock, so the Drive account never sees the
// two processes' creates at once, and it deletes nothing then.
func TestDiagDestProbe_RefusedWhileTheHubRuns(t *testing.T) {
	root := filepath.Join(t.TempDir(), "local")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	f := newDiagFixture(t, uploadsTo(localDest(root), 0), testHubID)
	d, err := local.New(root)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := seedVerified(t, f, seedSegment(t, f, time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC).UnixMilli(), randomFixtureBytes(100, 1)), d, "dst-1")
	release, err := lockHub(filepath.Join(f.dataDir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	code, lines, stderr := runDiag(t, "dest-probe", "--config", f.config, "--dest", "dst-1", "--delete-segment", "--poll", "1ms", "--for", "1ms")
	if code != exitFatal || len(lines) != 0 || !strings.Contains(stderr, "hub lock") {
		t.Errorf("diag dest-probe while the hub runs = %d, %v, %q; want exit 1 naming the hub lock", code, lines, stderr)
	}
	if got := placementByID(t, f, p.ID); got.State != catalog.PlacementVerified {
		t.Errorf("the placement is %s, want verified: the refused probe deleted it", got.State)
	}
}

// firstDest is the first destination of f's uploads section.
func firstDest(f *diagFixture) string {
	cfg, _, _, err := loadRunConfig(f.config)
	if err != nil || cfg.Uploads == nil {
		return ""
	}
	return cfg.Uploads.Destinations[0].ID
}

// randomFixtureBytes returns n bytes from a ChaCha8 seeded with seed.
func randomFixtureBytes(n int, seed byte) []byte {
	b := make([]byte, n)
	_, _ = rand.NewChaCha8([32]byte{seed}).Read(b)
	return b
}
