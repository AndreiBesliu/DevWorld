package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
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

// TestCatalog_MigrationAddsUploadTables (P03, g1-3): c2's migration, the
// last of migrations, takes a catalog of the schema before it to the newest
// one with its rows kept and the four upload tables added, and a second Open
// changes nothing.
func TestCatalog_MigrationAddsUploadTables(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "catalog.db")
	before := len(migrations) - 1
	older, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := append(slices.Clone(migrations[:before]),
		fmt.Sprintf("PRAGMA user_version = %d", before),
		`INSERT INTO segments (id, camera_id, profile, local_path, start_ms, end_ms, bytes, encrypted, local_state)
			VALUES ('seg-1', 'c1', 'main', 'c1/main/d/a.mp4', 1, 2, 3, 0, 'complete')`,
		"INSERT INTO gaps (camera_id, profile, start_ms, end_ms, reason) VALUES ('c1', 'main', 100, 200, 'restart')",
		"INSERT INTO tail_reasons (local_path, reason) VALUES ('c1/main/d/b.mp4.part', 'card')")
	for _, stmt := range stmts {
		if _, err := older.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	_ = older.Close()

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open of a catalog one migration behind: %v", err)
	}
	first := dumpCatalog(t, db)
	_ = db.Close()
	if first.version != len(migrations) {
		t.Errorf("user_version = %d, want %d", first.version, len(migrations))
	}
	for _, table := range []string{"placements", "upload_sessions", "secrets", "remote_folders"} {
		if !slices.Contains(first.tables, table) {
			t.Errorf("no table %s after the migration; tables %q", table, first.tables)
		}
	}
	wantRows := []string{
		"gaps: c1 main 100 200 restart",
		"segments: seg-1 c1/main/d/a.mp4 complete 3",
		"tail_reasons: c1/main/d/b.mp4.part card",
	}
	if !slices.Equal(first.rows, wantRows) {
		t.Errorf("rows after the migration %q, want %q", first.rows, wantRows)
	}

	db, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("a second Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if second := dumpCatalog(t, db); !reflect.DeepEqual(second, first) {
		t.Errorf("a second Open changed the catalog: %+v, was %+v", second, first)
	}
}

// catalogDump is what TestCatalog_MigrationAddsUploadTables compares: the
// schema version, the schema and c1's rows.
type catalogDump struct {
	version int
	tables  []string
	schema  []string
	rows    []string
}

func dumpCatalog(t *testing.T, db *DB) catalogDump {
	t.Helper()
	ctx := t.Context()
	var d catalogDump
	if err := db.sql.QueryRowContext(ctx, "PRAGMA user_version").Scan(&d.version); err != nil {
		t.Fatal(err)
	}
	rows, err := db.sql.QueryContext(ctx, "SELECT type, name, coalesce(sql, '') FROM sqlite_master ORDER BY type, name")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var kind, name, text string
		if err := rows.Scan(&kind, &name, &text); err != nil {
			t.Fatal(err)
		}
		if kind == "table" {
			d.tables = append(d.tables, name)
		}
		d.schema = append(d.schema, kind+" "+name+": "+text)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"SELECT 'gaps: ' || camera_id || ' ' || profile || ' ' || start_ms || ' ' || end_ms || ' ' || reason FROM gaps",
		"SELECT 'segments: ' || id || ' ' || local_path || ' ' || local_state || ' ' || bytes FROM segments",
		"SELECT 'tail_reasons: ' || local_path || ' ' || reason FROM tail_reasons",
	} {
		rows, err := db.sql.QueryContext(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Fatal(err)
			}
			d.rows = append(d.rows, row)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// putTestSegment writes a complete segment of c1/main starting at startMs and
// returns its row.
func putTestSegment(t *testing.T, db *DB, name string, startMs, bytes int64) Segment {
	t.Helper()
	s := Segment{Cam: "c1", Profile: "main", Path: "c1/main/d/" + name + ".mp4", StartMs: startMs, EndMs: startMs + 1000, Bytes: bytes, State: StateComplete}
	if err := db.PutSegment(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	segs, err := db.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range segs {
		if row.Path == s.Path {
			return row
		}
	}
	t.Fatalf("no row for %s", s.Path)
	return Segment{}
}

// putPlacement puts p in a transaction of its own and returns whether it
// superseded the pair's row.
func putPlacement(t *testing.T, db *DB, p Placement) bool {
	t.Helper()
	var superseded bool
	err := db.WithTx(t.Context(), func(tx *sql.Tx) error {
		var err error
		superseded, err = PutPlacementTx(t.Context(), tx, p)
		return err
	})
	if err != nil {
		t.Fatalf("PutPlacementTx(%+v): %v", p, err)
	}
	return superseded
}

// placementOf returns the placement row of objectID on destID.
func placementOf(t *testing.T, db *DB, objectID, destID string) Placement {
	t.Helper()
	all, err := db.Placements(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		if p.ObjectID == objectID && p.DestinationID == destID {
			return p
		}
	}
	t.Fatalf("no placement of %s on %s in %+v", objectID, destID, all)
	return Placement{}
}

// newPlacement is a placement of s on destID, as outbox.PlacementFor makes
// it.
func newPlacement(s Segment, destID, class string) Placement {
	return Placement{ObjectKind: ObjectSegment, ObjectID: s.ID, DestinationID: destID, Class: class,
		Encrypt: s.Encrypted, State: PlacementPending, Bytes: s.Bytes}
}

// setPlacement writes p's state, attempts and next attempt over the row as
// it is now.
func setPlacement(t *testing.T, db *DB, p Placement) {
	t.Helper()
	was := placementOf(t, db, p.ObjectID, p.DestinationID)
	p.ID = was.ID
	if err := db.UpdatePlacement(t.Context(), p, was.State, was.Bytes); err != nil {
		t.Fatalf("UpdatePlacement(%+v): %v", p, err)
	}
}

func hasSession(t *testing.T, db *DB, placementID string) bool {
	t.Helper()
	_, _, ok, err := db.UploadSession(t.Context(), placementID)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// TestCatalog_StaleWritesChangeNothing (g1-4): a placement write that
// names a state or bytes the row no longer has changes nothing and says so,
// and a session is stored only while its placement is uploading the bytes
// the worker read.
func TestCatalog_StaleWritesChangeNothing(t *testing.T) {
	db := openTest(t)
	ctx := t.Context()
	s := putTestSegment(t, db, "a", 1000, 10)
	putPlacement(t, db, newPlacement(s, "dst-a", ClassContinuous))
	p := placementOf(t, db, s.ID, "dst-a")

	taken := p
	taken.State = PlacementUploading
	for _, tc := range []struct {
		wasState string
		wasBytes int64
	}{{PlacementFailed, 10}, {PlacementPending, 11}} {
		if err := db.UpdatePlacement(ctx, taken, tc.wasState, tc.wasBytes); !errors.Is(err, ErrStalePlacement) {
			t.Errorf("UpdatePlacement with was %s/%d = %v, want ErrStalePlacement", tc.wasState, tc.wasBytes, err)
		}
		if got := placementOf(t, db, s.ID, "dst-a"); got != p {
			t.Errorf("a stale UpdatePlacement changed the row to %+v, was %+v", got, p)
		}
	}
	if err := db.PutUploadSession(ctx, p.ID, 10, []byte(`{"Offset":0}`), 0); !errors.Is(err, ErrStalePlacement) {
		t.Errorf("PutUploadSession of a pending placement = %v, want ErrStalePlacement", err)
	}
	if hasSession(t, db, p.ID) {
		t.Error("PutUploadSession stored a session of a pending placement")
	}

	if err := db.UpdatePlacement(ctx, taken, PlacementPending, 10); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id    string
		bytes int64
	}{{p.ID, 11}, {"no-such-placement", 10}} {
		if err := db.PutUploadSession(ctx, tc.id, tc.bytes, []byte(`{"Offset":4}`), 4); !errors.Is(err, ErrStalePlacement) {
			t.Errorf("PutUploadSession(%s, %d bytes) = %v, want ErrStalePlacement", tc.id, tc.bytes, err)
		}
		if hasSession(t, db, tc.id) {
			t.Errorf("PutUploadSession(%s, %d bytes) stored a session", tc.id, tc.bytes)
		}
	}
	for _, off := range []int64{4, 8} {
		if err := db.PutUploadSession(ctx, p.ID, 10, fmt.Appendf(nil, `{"Offset":%d}`, off), off); err != nil {
			t.Fatalf("PutUploadSession of the uploading placement: %v", err)
		}
	}
	state, off, ok, err := db.UploadSession(ctx, p.ID)
	if err != nil || !ok || off != 8 || string(state) != `{"Offset":8}` {
		t.Errorf("UploadSession = %s, %d, %v, %v; want the last put, at 8", state, off, ok, err)
	}
	if err := db.DeleteUploadSession(ctx, p.ID); err != nil || hasSession(t, db, p.ID) {
		t.Errorf("DeleteUploadSession = %v, and the session is still there: %v", err, hasSession(t, db, p.ID))
	}
}

// TestCatalog_PutPlacementTx_SupersedesUnverified (P03, g1-4, g2-06): new
// bytes reset an unverified placement to pending from 0, and a lost
// segment's placement that came back with the same bytes is due again; a
// placement still due later keeps its row.
func TestCatalog_PutPlacementTx_SupersedesUnverified(t *testing.T) {
	db := openTest(t)
	ctx := t.Context()
	s := putTestSegment(t, db, "a", 1000, 10)
	if putPlacement(t, db, newPlacement(s, "dst-a", ClassContinuous)) {
		t.Error("a new pair says superseded")
	}
	p := placementOf(t, db, s.ID, "dst-a")
	if p.ID == "" || p.State != PlacementPending || p.Bytes != 10 || p.Attempts != 0 || p.NextAttemptAtMs != 0 || p.ObjectKind != ObjectSegment {
		t.Errorf("the new placement is %+v, want pending with an id and 10 bytes", p)
	}

	for _, state := range []string{PlacementPending, PlacementUploading, PlacementFailed} {
		setPlacement(t, db, Placement{ObjectID: s.ID, DestinationID: "dst-a", State: state, Attempts: 3, NextAttemptAtMs: 5000})
		if state == PlacementUploading {
			if err := db.PutUploadSession(ctx, p.ID, p.Bytes, []byte(`{}`), 4); err != nil {
				t.Fatal(err)
			}
		}
		next := newPlacement(s, "dst-a", ClassContinuous)
		next.Bytes = p.Bytes + 1
		if !putPlacement(t, db, next) {
			t.Errorf("new bytes on a %s placement: not superseded", state)
		}
		got := placementOf(t, db, s.ID, "dst-a")
		if got.ID != p.ID || got.State != PlacementPending || got.Bytes != next.Bytes || got.Attempts != 0 || got.NextAttemptAtMs != 0 {
			t.Errorf("new bytes on a %s placement left %+v, want pending at 0 attempts with %d bytes and id %s", state, got, next.Bytes, p.ID)
		}
		if hasSession(t, db, p.ID) {
			t.Errorf("new bytes on a %s placement kept its upload session", state)
		}
		p = got
	}

	// A lost segment's placement, never due again, whose file came back with
	// the same bytes.
	if err := db.UpdatePlacement(ctx, Placement{ID: p.ID, State: PlacementUploading, Attempts: 2}, PlacementPending, p.Bytes); err != nil {
		t.Fatal(err)
	}
	if err := db.PutUploadSession(ctx, p.ID, p.Bytes, []byte(`{}`), 4); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdatePlacement(ctx, Placement{ID: p.ID, State: PlacementFailed, Attempts: 2}, PlacementUploading, p.Bytes); err != nil {
		t.Fatal(err)
	}
	if putPlacement(t, db, newPlacement(Segment{ID: s.ID, Bytes: p.Bytes}, "dst-a", ClassContinuous)) {
		t.Error("the same bytes on a failed placement say superseded")
	}
	if got := placementOf(t, db, s.ID, "dst-a"); got.State != PlacementPending || got.Attempts != 0 || got.NextAttemptAtMs != 0 || got.Bytes != p.Bytes {
		t.Errorf("the same bytes on a failed placement never due again left %+v, want pending at 0 attempts", got)
	}
	if hasSession(t, db, p.ID) {
		t.Error("the same bytes on a failed placement kept its upload session")
	}

	// A failed placement due later keeps its row and its attempts.
	setPlacement(t, db, Placement{ObjectID: s.ID, DestinationID: "dst-a", State: PlacementFailed, Attempts: 4, NextAttemptAtMs: 9000})
	before := placementOf(t, db, s.ID, "dst-a")
	if putPlacement(t, db, newPlacement(Segment{ID: s.ID, Bytes: p.Bytes}, "dst-a", ClassContinuous)) {
		t.Error("the same bytes on a failed placement due later say superseded")
	}
	if got := placementOf(t, db, s.ID, "dst-a"); got != before {
		t.Errorf("the same bytes on a failed placement due later left %+v, want it unchanged: %+v", got, before)
	}
}

// TestCatalog_PutPlacementTx_SupersedeRefreshesFields (P03): a placement
// superseded by other bytes takes the new placement's encryption, class and
// rule IDs with them, so its row describes the copy now to be made.
func TestCatalog_PutPlacementTx_SupersedeRefreshesFields(t *testing.T) {
	db := openTest(t)
	s := putTestSegment(t, db, "a", 1000, 10)
	putPlacement(t, db, newPlacement(s, "dst-a", ClassContinuous))
	was := placementOf(t, db, s.ID, "dst-a")

	next := newPlacement(s, "dst-a", ClassEventClip)
	next.Bytes, next.Encrypt, next.RuleIDs = 11, true, "rule-1,rule-2"
	if !putPlacement(t, db, next) {
		t.Fatal("other bytes did not supersede the placement")
	}
	got := placementOf(t, db, s.ID, "dst-a")
	want := was
	want.Bytes, want.Encrypt, want.Class, want.RuleIDs = 11, true, ClassEventClip, "rule-1,rule-2"
	if got != want {
		t.Errorf("the superseded placement is %+v, want %+v", got, want)
	}
}

// TestCatalog_PutPlacementTx_KeepsVerified (P03): a verified copy cannot be
// replaced yet, so its placement stays as it is, whatever the bytes.
func TestCatalog_PutPlacementTx_KeepsVerified(t *testing.T) {
	db := openTest(t)
	s := putTestSegment(t, db, "a", 1000, 10)
	putPlacement(t, db, newPlacement(s, "dst-a", ClassEventClip))
	setPlacement(t, db, Placement{ObjectID: s.ID, DestinationID: "dst-a", State: PlacementVerified, RemoteRef: "ref-1", RemoteHash: "h", Attempts: 1})
	before := placementOf(t, db, s.ID, "dst-a")
	for _, bytes := range []int64{10, 11} {
		next := newPlacement(s, "dst-a", ClassEventClip)
		next.Bytes = bytes
		if putPlacement(t, db, next) {
			t.Errorf("%d bytes on a verified placement say superseded", bytes)
		}
		if got := placementOf(t, db, s.ID, "dst-a"); got != before {
			t.Errorf("%d bytes on a verified placement left %+v, want it unchanged: %+v", bytes, got, before)
		}
	}
}

// TestCatalog_DuePlacementsOrder (§9.7): event clips and thumbnails, then
// continuous, then exports, the oldest first in each class; only pending
// rows due now and failed rows due again by now, of complete segments.
func TestCatalog_DuePlacementsOrder(t *testing.T) {
	db := openTest(t)
	ctx := t.Context()
	at := func(hhmm string) int64 {
		tm, err := time.Parse("2006-01-02 15:04", "2026-10-17 "+hhmm)
		if err != nil {
			t.Fatal(err)
		}
		return tm.UnixMilli()
	}
	now := at("12:00")
	put := func(name, class, hhmm string, set Placement) string {
		s := putTestSegment(t, db, name, at(hhmm), 10)
		putPlacement(t, db, newPlacement(s, "dst-a", class))
		if set.State != "" {
			set.ObjectID, set.DestinationID = s.ID, "dst-a"
			setPlacement(t, db, set)
		}
		return s.ID
	}
	export := put("export", ClassExport, "09:00", Placement{})
	contLate := put("cont-late", ClassContinuous, "10:00", Placement{})
	clipLate := put("clip-late", ClassEventClip, "10:05", Placement{})
	contEarly := put("cont-early", ClassContinuous, "09:30", Placement{State: PlacementFailed, Attempts: 1, NextAttemptAtMs: now})
	thumb := put("thumb", ClassThumbnail, "10:03", Placement{})
	clipEarly := put("clip-early", ClassEventClip, "10:01", Placement{State: PlacementPending, NextAttemptAtMs: now - 1})
	// Not due: a pending or failed row due later, a failed row never due
	// again, rows in other states, a segment still being written or lost.
	put("later", ClassEventClip, "08:00", Placement{State: PlacementPending, NextAttemptAtMs: now + 1})
	put("failed-later", ClassEventClip, "08:00", Placement{State: PlacementFailed, NextAttemptAtMs: now + 1})
	put("failed-never", ClassEventClip, "08:00", Placement{State: PlacementFailed})
	put("uploading", ClassEventClip, "08:00", Placement{State: PlacementUploading})
	put("verified", ClassEventClip, "08:00", Placement{State: PlacementVerified})
	put("deleted", ClassEventClip, "08:00", Placement{State: PlacementDeleted})
	writing := putTestSegment(t, db, "writing", at("08:00"), 10)
	writing.State = StateWriting
	if err := db.PutSegment(ctx, writing); err != nil {
		t.Fatal(err)
	}
	putPlacement(t, db, newPlacement(writing, "dst-a", ClassEventClip))
	lost := put("lost", ClassEventClip, "08:00", Placement{})
	lostSeg := Segment{ID: lost, Cam: "c1", Profile: "main", Path: "c1/main/d/lost.mp4", StartMs: at("08:00"), EndMs: at("08:00") + 1000, Bytes: 10, State: StateLost}
	if err := db.PutSegment(ctx, lostSeg); err != nil {
		t.Fatal(err)
	}

	due, err := db.DuePlacements(ctx, now, []string{"dst-a"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{clipEarly, thumb, clipLate, contEarly, contLate, export}
	if got := objectIDs(due); !slices.Equal(got, want) {
		t.Errorf("DuePlacements order %q, want %q", got, want)
	}
	if due, err := db.DuePlacements(ctx, now, []string{"dst-a"}, 2); err != nil || !slices.Equal(objectIDs(due), want[:2]) {
		t.Errorf("DuePlacements with limit 2 = %q, %v; want %q", objectIDs(due), err, want[:2])
	}
	if lostDue, err := db.LostPlacements(ctx, now, []string{"dst-a"}); err != nil || !slices.Equal(objectIDs(lostDue), []string{lost}) {
		t.Errorf("LostPlacements = %q, %v; want only the lost segment's", objectIDs(lostDue), err)
	}
	if seg, err := db.SegmentByID(ctx, clipEarly); err != nil || seg.Path != "c1/main/d/clip-early.mp4" || seg.StartMs != at("10:01") {
		t.Errorf("SegmentByID = %+v, %v", seg, err)
	}
	if _, err := db.SegmentByID(ctx, "no-such-segment"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("SegmentByID of an unknown id = %v, want sql.ErrNoRows", err)
	}
}

func objectIDs(ps []Placement) []string {
	ids := make([]string, 0, len(ps))
	for _, p := range ps {
		ids = append(ids, p.ObjectID)
	}
	return ids
}

// TestCatalog_DuePlacementsOnlyNamedDestinations: a queue reads only the
// rows of the destinations it has.
func TestCatalog_DuePlacementsOnlyNamedDestinations(t *testing.T) {
	db := openTest(t)
	s := putTestSegment(t, db, "a", 1000, 10)
	for _, dest := range []string{"dst-a", "dst-gone"} {
		putPlacement(t, db, newPlacement(s, dest, ClassContinuous))
	}
	due, err := db.DuePlacements(t.Context(), 2000, []string{"dst-a"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].DestinationID != "dst-a" {
		t.Errorf("DuePlacements for [dst-a] = %+v, want only dst-a's row", due)
	}
	if due, err := db.DuePlacements(t.Context(), 2000, nil, 10); err != nil || len(due) != 0 {
		t.Errorf("DuePlacements for no destination = %+v, %v; want none", due, err)
	}
}
