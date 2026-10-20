package catalog

import (
	"path/filepath"
	"slices"
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
	if err := db.MoveGapStart(ctx, "c1", "main", 100, 120); err != nil {
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
	if err := db.MoveGapStart(ctx, "c1", "main", 120, 950); err != nil {
		t.Fatal(err)
	}
	if err := db.MoveGapStart(ctx, "c1", "sub", 150, 160); err != nil {
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
