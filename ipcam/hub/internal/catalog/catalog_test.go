package catalog

import (
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestCatalog_OpensWALSynchronousFull: every connection of the catalog runs
// in WAL mode with synchronous=FULL (§4.6), so a committed row survives a
// power cut.
func TestCatalog_OpensWALSynchronousFull(t *testing.T) {
	db := openTest(t)
	// Several connections at once: the pragmas hold on each, not only the first.
	conns := make([]interface{ Close() error }, 0, 3)
	for range 3 {
		conn, err := db.sql.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
		var mode string
		var sync int
		if err := conn.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRowContext(t.Context(), "PRAGMA synchronous").Scan(&sync); err != nil {
			t.Fatal(err)
		}
		if mode != "wal" || sync != 2 {
			t.Errorf("journal_mode = %q, synchronous = %d; want wal and 2 (FULL)", mode, sync)
		}
	}
	for _, c := range conns {
		_ = c.Close()
	}
}

// TestCatalog_MigrationsRunOnceAndForwardOnly: a second Open of the same file
// keeps its rows, and a catalog newer than this code is refused.
func TestCatalog_MigrationsRunOnceAndForwardOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutSegment(t.Context(), Segment{Cam: "c1", Profile: "main", Path: "c1/main/a.mp4", StartMs: 1, EndMs: 2, Bytes: 3, State: StateComplete}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	db, err = Open(t.Context(), path)
	if err != nil {
		t.Fatalf("a second Open: %v", err)
	}
	if segs, err := db.Segments(t.Context()); err != nil || len(segs) != 1 {
		t.Fatalf("after a second Open: %d rows, %v; want the 1 row", len(segs), err)
	}
	if _, err := db.sql.ExecContext(t.Context(), "PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if db, err := Open(t.Context(), path); err == nil {
		_ = db.Close()
		t.Fatal("Open accepted a catalog of a newer schema")
	}
}

// TestCatalog_PutSegmentAssignsUUIDv7AndUpsertsByPath: the catalog assigns
// the segment id (UUIDv7, §4.6) and a second row for the same path updates
// it, keeping the id.
func TestCatalog_PutSegmentAssignsUUIDv7AndUpsertsByPath(t *testing.T) {
	db := openTest(t)
	s := Segment{Cam: "c1", Profile: "main", Path: "c1/main/2026-10-20/101010_000001.mp4.age", StartMs: 1000, EndMs: 2000, Bytes: 10, Encrypted: true, State: StateComplete}
	if err := db.PutSegment(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	verified := int64(5)
	if err := db.MarkVerified(t.Context(), s.Path, verified); err != nil {
		t.Fatal(err)
	}
	segs, err := db.Segments(t.Context())
	if err != nil || len(segs) != 1 {
		t.Fatalf("Segments = %v, %v", segs, err)
	}
	id := segs[0].ID
	if len(id) != 36 || id[14] != '7' {
		t.Errorf("id %q is not a UUIDv7", id)
	}
	if segs[0].VerifiedAtMs == nil || *segs[0].VerifiedAtMs != verified {
		t.Errorf("VerifiedAtMs = %v, want %d", segs[0].VerifiedAtMs, verified)
	}
	s.EndMs, s.Bytes = 3000, 20
	if err := db.PutSegment(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	segs, _ = db.Segments(t.Context())
	if len(segs) != 1 || segs[0].ID != id || segs[0].EndMs != 3000 || segs[0].Bytes != 20 || !segs[0].Encrypted {
		t.Errorf("after the update: %+v; want one row with id %s, end 3000, 20 bytes", segs, id)
	}
	if segs[0].VerifiedAtMs != nil {
		t.Error("a row whose bytes changed is still marked verified")
	}
}

// TestCatalog_ReconcileFiles_FilesWin: the files are the source of truth
// (§4.1 (5)): a row without a file becomes lost, a file without a row gets
// one, and a row whose file changed takes the file's facts.
func TestCatalog_ReconcileFiles_FilesWin(t *testing.T) {
	db := openTest(t)
	ctx := t.Context()
	for _, s := range []Segment{
		{Cam: "c1", Profile: "main", Path: "c1/main/d/gone.mp4", StartMs: 1, EndMs: 2, Bytes: 5, State: StateComplete},
		{Cam: "c1", Profile: "main", Path: "c1/main/d/same.mp4", StartMs: 3, EndMs: 4, Bytes: 6, State: StateComplete},
		{Cam: "c1", Profile: "main", Path: "c1/main/d/grown.mp4", StartMs: 5, EndMs: 6, Bytes: 7, State: StateComplete},
		{Cam: "c1", Profile: "main", Path: "c1/main/d/resized.mp4", StartMs: 7, EndMs: 8, Bytes: 8, State: StateComplete},
	} {
		if err := db.PutSegment(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	files := []FileFact{
		{Cam: "c1", Profile: "main", Path: "c1/main/d/same.mp4", Bytes: 6},
		{Cam: "c1", Profile: "main", Path: "c1/main/d/grown.mp4", StartMs: 5, EndMs: 9, Bytes: 70},
		{Cam: "c1", Profile: "sub", Path: "c1/sub/d/new.mp4.age", StartMs: 10, EndMs: 20, Bytes: 8, Encrypted: true},
		// A fact without its times, as the verification has them: its size
		// alone differs.
		{Cam: "c1", Profile: "main", Path: "c1/main/d/resized.mp4", Bytes: 9},
	}

	dry, err := db.DiffFiles(ctx, files)
	if err != nil {
		t.Fatal(err)
	}
	if dry.Count() != 4 {
		t.Errorf("DiffFiles = %+v, want 1 added, 2 updated and 1 lost", dry)
	}
	if segs, _ := db.Segments(ctx); len(segs) != 4 || segs[0].State != StateComplete {
		t.Fatal("DiffFiles changed the catalog")
	}

	diff, err := db.ReconcileFiles(ctx, files)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(diff.Lost, []string{"c1/main/d/gone.mp4"}) ||
		!slices.Equal(diff.Added, []string{"c1/sub/d/new.mp4.age"}) ||
		!slices.Equal(diff.Updated, []string{"c1/main/d/grown.mp4", "c1/main/d/resized.mp4"}) {
		t.Errorf("ReconcileFiles = %+v", diff)
	}
	byPath := map[string]Segment{}
	segs, _ := db.Segments(ctx)
	for _, s := range segs {
		byPath[s.Path] = s
	}
	if s := byPath["c1/main/d/gone.mp4"]; s.State != StateLost {
		t.Errorf("the row without a file is %q, want lost", s.State)
	}
	if s := byPath["c1/sub/d/new.mp4.age"]; s.State != StateComplete || !s.Encrypted || s.StartMs != 10 || s.EndMs != 20 || s.Bytes != 8 || s.ID == "" {
		t.Errorf("the added row is %+v", s)
	}
	if s := byPath["c1/main/d/resized.mp4"]; s.StartMs != 7 || s.EndMs != 8 || s.Bytes != 9 {
		t.Errorf("the resized row is %+v, want its times kept and 9 bytes", s)
	}
	if s := byPath["c1/main/d/grown.mp4"]; s.EndMs != 9 || s.Bytes != 70 {
		t.Errorf("the updated row is %+v, want end 9 and 70 bytes", s)
	}
	if again, err := db.DiffFiles(ctx, files); err != nil || again.Count() != 0 {
		t.Errorf("after ReconcileFiles, DiffFiles = %+v, %v; want no difference", again, err)
	}
}

// TestCatalog_GapsOpenAndClose: a gap is written open (EndMs 0) with its
// reason, and CloseGap closes every open gap of that stream.
func TestCatalog_GapsOpenAndClose(t *testing.T) {
	db := openTest(t)
	ctx := t.Context()
	for _, g := range []Gap{
		{Cam: "c1", Profile: "main", StartMs: 100, Reason: GapRestart},
		{Cam: "c1", Profile: "sub", StartMs: 150, Reason: GapCard},
	} {
		if err := db.PutGap(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.MoveGapStart(ctx, "c1", "main", GapRestart, 100, 120); err != nil {
		t.Fatal(err)
	}
	if err := db.CloseGap(ctx, "c1", "main", 900); err != nil {
		t.Fatal(err)
	}
	gaps, err := db.Gaps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Gap{
		{Cam: "c1", Profile: "main", StartMs: 120, EndMs: 900, Reason: GapRestart},
		{Cam: "c1", Profile: "sub", StartMs: 150, EndMs: 0, Reason: GapCard},
	}
	if !slices.Equal(gaps, want) {
		t.Errorf("Gaps = %+v, want %+v", gaps, want)
	}
	// Moving the start of a closed gap past its end ends it there too; an
	// open gap stays open.
	if err := db.MoveGapStart(ctx, "c1", "main", GapRestart, 120, 950); err != nil {
		t.Fatal(err)
	}
	if err := db.MoveGapStart(ctx, "c1", "sub", GapCard, 150, 160); err != nil {
		t.Fatal(err)
	}
	if gaps, err = db.Gaps(ctx); err != nil {
		t.Fatal(err)
	}
	want = []Gap{
		{Cam: "c1", Profile: "main", StartMs: 950, EndMs: 950, Reason: GapRestart},
		{Cam: "c1", Profile: "sub", StartMs: 160, EndMs: 0, Reason: GapCard},
	}
	if !slices.Equal(gaps, want) {
		t.Errorf("after the moves, Gaps = %+v, want %+v", gaps, want)
	}
	if err := db.PutGap(ctx, Gap{Cam: "c1", Profile: "main", StartMs: 1, Reason: "power"}); err == nil {
		t.Error("PutGap accepted an unknown reason")
	}
}

// TestCatalog_GapsOfOneReason (Task 8, 8a M1): FindGap finds a stream's gap
// of one reason that starts in a range, the earliest, and MoveGapStart
// moves only the gaps of its reason: a card gap (T40) that starts where a
// restart gap does is never found or moved for it.
func TestCatalog_GapsOfOneReason(t *testing.T) {
	db := openTest(t)
	ctx := t.Context()
	for _, g := range []Gap{
		{Cam: "c1", Profile: "main", StartMs: 100, Reason: GapCard},
		{Cam: "c1", Profile: "main", StartMs: 100, Reason: GapRestart},
		{Cam: "c1", Profile: "main", StartMs: 300, Reason: GapRestart},
		{Cam: "c1", Profile: "sub", StartMs: 200, Reason: GapRestart},
	} {
		if err := db.PutGap(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		profile  string
		reason   GapReason
		from, to int64
		start    int64
		found    bool
	}{
		{"main", GapRestart, 100, 300, 100, true},
		{"main", GapRestart, 101, 300, 300, true},
		{"main", GapRestart, 101, 299, 0, false},
		{"main", GapCard, 0, 1000, 100, true},
		{"main", GapHubStopped, 0, 1000, 0, false},
		{"sub", GapRestart, 200, 200, 200, true},
		{"sub", GapCard, 0, 1000, 0, false},
	} {
		start, found, err := db.FindGap(ctx, "c1", tc.profile, tc.reason, tc.from, tc.to)
		if err != nil || start != tc.start || found != tc.found {
			t.Errorf("FindGap(c1/%s, %s, [%d, %d]) = %d, %v, %v; want %d, %v", tc.profile, tc.reason, tc.from, tc.to, start, found, err, tc.start, tc.found)
		}
	}
	if err := db.MoveGapStart(ctx, "c1", "main", GapRestart, 100, 150); err != nil {
		t.Fatal(err)
	}
	gaps, err := db.Gaps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Gap{
		{Cam: "c1", Profile: "main", StartMs: 100, Reason: GapCard},
		{Cam: "c1", Profile: "main", StartMs: 150, Reason: GapRestart},
		{Cam: "c1", Profile: "main", StartMs: 300, Reason: GapRestart},
		{Cam: "c1", Profile: "sub", StartMs: 200, Reason: GapRestart},
	}
	if !slices.Equal(gaps, want) {
		t.Errorf("after moving the restart gap, Gaps = %+v, want %+v: the card gap stays", gaps, want)
	}
}

// TestCatalog_GapReasonCheckedBySQLite: the gaps table checks the reason
// itself, so a row that does not come through PutGap cannot hold an unknown
// one either. A catalog of the first schema (Task 5's, without the check)
// is migrated with its gaps kept.
func TestCatalog_GapReasonCheckedBySQLite(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "catalog.db")
	v1, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		migrations[0],
		"PRAGMA user_version = 1",
		"INSERT INTO gaps (camera_id, profile, start_ms, end_ms, reason) VALUES ('c1', 'main', 100, 200, 'restart')",
	} {
		if _, err := v1.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	_ = v1.Close()

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open of a first-schema catalog: %v", err)
	}
	defer func() { _ = db.Close() }()
	gaps, err := db.Gaps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Gap{{Cam: "c1", Profile: "main", StartMs: 100, EndMs: 200, Reason: GapRestart}}; !slices.Equal(gaps, want) {
		t.Errorf("after the migration, gaps %+v, want %+v", gaps, want)
	}
	for _, reason := range []GapReason{GapRestart, GapHubStopped, GapDiskFull, GapCard, GapCameraOffline} {
		if _, err := db.sql.ExecContext(ctx, "INSERT INTO gaps (camera_id, profile, start_ms, end_ms, reason) VALUES ('c1', 'main', 1, 0, ?)", string(reason)); err != nil {
			t.Errorf("SQLite refused the reason %q: %v", reason, err)
		}
	}
	_, err = db.sql.ExecContext(ctx, "INSERT INTO gaps (camera_id, profile, start_ms, end_ms, reason) VALUES ('c1', 'main', 1, 0, 'power')")
	if err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("an unknown reason written past PutGap: %v, want SQLite's CHECK constraint failure", err)
	}
}

// TestCatalog_TailReasons (T40; Task 8 review M3): the reason of the tail
// gap of a .part that MediaMTX could not write is kept by the file's path,
// so that a start of the hub before the file's repair still knows it; a
// path has one reason, the last put, until it is deleted; an unknown reason
// is refused, by the catalog and by SQLite.
func TestCatalog_TailReasons(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "catalog.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	part := "c1/main/2026-10-28/120000_000000.mp4.part"
	if reason, found, err := db.TailReason(ctx, part); err != nil || found {
		t.Errorf("TailReason before any = %q, %v, %v; want none", reason, found, err)
	}
	for _, reason := range []GapReason{GapRestart, GapCard} {
		if err := db.PutTailReason(ctx, part, reason); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if reason, found, err := db.TailReason(ctx, part); err != nil || !found || reason != GapCard {
		t.Errorf("TailReason after a new Open = %q, %v, %v; want card, the last put", reason, found, err)
	}
	other := "c1/main/2026-10-28/115500_000000.mp4.part"
	if err := db.PutTailReason(ctx, other, GapCard); err != nil {
		t.Fatal(err)
	}
	if paths, err := db.TailReasonPaths(ctx); err != nil || !slices.Equal(paths, []string{other, part}) {
		t.Errorf("TailReasonPaths = %q, %v; want both paths, sorted", paths, err)
	}
	if err := db.PutTailReason(ctx, part, "power"); err == nil {
		t.Error("PutTailReason accepted an unknown reason")
	}
	if _, err := db.sql.ExecContext(ctx, "INSERT INTO tail_reasons (local_path, reason) VALUES ('x.mp4.part', 'power')"); err == nil ||
		!strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Errorf("an unknown reason written past PutTailReason: %v, want SQLite's CHECK constraint failure", err)
	}
	if err := db.DeleteTailReason(ctx, part); err != nil {
		t.Fatal(err)
	}
	if reason, found, err := db.TailReason(ctx, part); err != nil || found {
		t.Errorf("TailReason after DeleteTailReason = %q, %v, %v; want none", reason, found, err)
	}
}
