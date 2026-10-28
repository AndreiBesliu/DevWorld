// Package catalog is the hub's SQLite catalog (§4.6), in its Phase 0 subset:
// the recorded segments and the gaps between them (ruling G07). The files
// under rec/ are the source of truth (§4.1 (5)); ReconcileFiles makes the
// rows match them.
//
// It uses modernc.org/sqlite (pure Go) with journal_mode=WAL and
// synchronous=FULL on every connection, so a committed row survives a power
// cut, and forward-only migrations. Segment ids are UUIDv7 and times are in
// UTC milliseconds.
package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // the database/sql driver "sqlite"
)

// GapReason is why a stream has no recording for a while (§4.5).
type GapReason string

// The gap reasons. The gaps table's CHECK (migration 2) lists the same.
const (
	GapRestart       GapReason = "restart"        // a crash or a cut: the lost tail until recording resumed
	GapHubStopped    GapReason = "hub_stopped"    // the hub was stopped
	GapDiskFull      GapReason = "disk_full"      // §13.8
	GapCard          GapReason = "card"           // the SD card failed (T40)
	GapCameraOffline GapReason = "camera_offline" // the camera sent nothing
)

func (r GapReason) valid() bool {
	switch r {
	case GapRestart, GapHubStopped, GapDiskFull, GapCard, GapCameraOffline:
		return true
	}
	return false
}

// The states of a segment.
const (
	StateWriting  = "writing"
	StateComplete = "complete"
	StateLost     = "lost" // its file is gone
)

// Segment is a recorded segment. Path is relative to rec/, with "/".
type Segment struct {
	ID, Cam, Profile, Path string
	StartMs, EndMs, Bytes  int64
	Encrypted              bool
	State                  string // writing|complete|lost
	VerifiedAtMs           *int64 // when the background verification passed it
}

// Gap is a time without recording of one stream. EndMs is 0 while it is open.
type Gap struct {
	Cam, Profile   string
	StartMs, EndMs int64
	Reason         GapReason
}

// FileFact is what a segment file on disk says about itself. StartMs and
// EndMs may be 0 when only the size was read: they are needed for a file
// that has no row, or whose size changed.
type FileFact struct {
	Cam, Profile, Path    string
	StartMs, EndMs, Bytes int64
	Encrypted             bool
}

// ReconcileDiff lists, by path, where the rows and the files disagree: a
// file without a row (Added), a row whose file says something else
// (Updated), and a row whose file is gone (Lost).
type ReconcileDiff struct {
	Added, Updated, Lost []string
}

// Count is the number of disagreements.
func (d ReconcileDiff) Count() int { return len(d.Added) + len(d.Updated) + len(d.Lost) }

// DB is an open catalog.
type DB struct{ sql *sql.DB }

// migrations are applied in order, each once; PRAGMA user_version counts
// those applied. They only ever go forward.
var migrations = []string{
	`CREATE TABLE segments (
		id             TEXT PRIMARY KEY,
		camera_id      TEXT NOT NULL,
		profile        TEXT NOT NULL,
		local_path     TEXT NOT NULL UNIQUE,
		start_ms       INTEGER NOT NULL,
		end_ms         INTEGER NOT NULL,
		bytes          INTEGER NOT NULL,
		encrypted      INTEGER NOT NULL,
		local_state    TEXT NOT NULL CHECK (local_state IN ('writing', 'complete', 'lost')),
		verified_at_ms INTEGER
	);
	CREATE INDEX segments_stream ON segments (camera_id, profile, start_ms);
	CREATE TABLE gaps (
		id        INTEGER PRIMARY KEY,
		camera_id TEXT NOT NULL,
		profile   TEXT NOT NULL,
		start_ms  INTEGER NOT NULL,
		end_ms    INTEGER NOT NULL,
		reason    TEXT NOT NULL
	);
	CREATE INDEX gaps_stream ON gaps (camera_id, profile, start_ms);`,
	// 2: SQLite checks the gap reason itself. A table's constraints cannot be
	// altered, so gaps is rebuilt with its rows.
	`CREATE TABLE gaps_v2 (
		id        INTEGER PRIMARY KEY,
		camera_id TEXT NOT NULL,
		profile   TEXT NOT NULL,
		start_ms  INTEGER NOT NULL,
		end_ms    INTEGER NOT NULL,
		reason    TEXT NOT NULL CHECK (reason IN ('restart', 'hub_stopped', 'disk_full', 'card', 'camera_offline'))
	);
	INSERT INTO gaps_v2 (id, camera_id, profile, start_ms, end_ms, reason)
		SELECT id, camera_id, profile, start_ms, end_ms, reason FROM gaps;
	DROP TABLE gaps;
	ALTER TABLE gaps_v2 RENAME TO gaps;
	CREATE INDEX gaps_stream ON gaps (camera_id, profile, start_ms);`,
}

// Open opens the catalog at path, creating it if needed, and migrates it.
// It refuses a catalog whose schema is newer than this code.
func Open(ctx context.Context, path string) (*DB, error) {
	// The pragmas run on every connection the pool opens.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(10000)&_txlock=immediate"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("catalog: open %s: %w", path, err)
	}
	db := &DB{sql: sdb}
	if err := db.migrate(ctx); err != nil {
		_ = sdb.Close()
		return nil, fmt.Errorf("catalog: %s: %w", path, err)
	}
	return db, nil
}

func (db *DB) migrate(ctx context.Context) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("the catalog's schema version %d is newer than this hub's %d", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(migrations))); err != nil {
		return err
	}
	return tx.Commit()
}

// Close closes the catalog.
func (db *DB) Close() error { return db.sql.Close() }

// PutSegment writes s, by its path: a new path gets a row with a new UUIDv7
// id (or s.ID when it is set), a known path is updated and keeps its id. A
// row whose bytes or state change is no longer verified.
func (db *DB) PutSegment(ctx context.Context, s Segment) error {
	if s.State == "" {
		s.State = StateComplete
	}
	if s.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		s.ID = id.String()
	}
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO segments (id, camera_id, profile, local_path, start_ms, end_ms, bytes, encrypted, local_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (local_path) DO UPDATE SET
			camera_id = excluded.camera_id, profile = excluded.profile,
			start_ms = excluded.start_ms, end_ms = excluded.end_ms,
			verified_at_ms = CASE WHEN segments.bytes = excluded.bytes AND segments.local_state = excluded.local_state
				THEN segments.verified_at_ms END,
			bytes = excluded.bytes, encrypted = excluded.encrypted, local_state = excluded.local_state`,
		s.ID, s.Cam, s.Profile, s.Path, s.StartMs, s.EndMs, s.Bytes, s.Encrypted, s.State)
	if err != nil {
		return fmt.Errorf("catalog: put segment %s: %w", s.Path, err)
	}
	return nil
}

// Segments returns every segment, by path.
func (db *DB) Segments(ctx context.Context) ([]Segment, error) {
	rows, err := db.sql.QueryContext(ctx, `
		SELECT id, camera_id, profile, local_path, start_ms, end_ms, bytes, encrypted, local_state, verified_at_ms
		FROM segments ORDER BY local_path`)
	if err != nil {
		return nil, fmt.Errorf("catalog: list segments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var segs []Segment
	for rows.Next() {
		var s Segment
		var verified sql.NullInt64
		if err := rows.Scan(&s.ID, &s.Cam, &s.Profile, &s.Path, &s.StartMs, &s.EndMs, &s.Bytes, &s.Encrypted, &s.State, &verified); err != nil {
			return nil, fmt.Errorf("catalog: list segments: %w", err)
		}
		if verified.Valid {
			s.VerifiedAtMs = &verified.Int64
		}
		segs = append(segs, s)
	}
	return segs, rows.Err()
}

// MarkVerified records that the segment at path passed the verification at atMs.
func (db *DB) MarkVerified(ctx context.Context, path string, atMs int64) error {
	if _, err := db.sql.ExecContext(ctx, "UPDATE segments SET verified_at_ms = ? WHERE local_path = ?", atMs, path); err != nil {
		return fmt.Errorf("catalog: mark %s verified: %w", path, err)
	}
	return nil
}

// PutGap writes a gap.
func (db *DB) PutGap(ctx context.Context, g Gap) error {
	if !g.Reason.valid() {
		return fmt.Errorf("catalog: unknown gap reason %q", g.Reason)
	}
	if _, err := db.sql.ExecContext(ctx, "INSERT INTO gaps (camera_id, profile, start_ms, end_ms, reason) VALUES (?, ?, ?, ?, ?)",
		g.Cam, g.Profile, g.StartMs, g.EndMs, string(g.Reason)); err != nil {
		return fmt.Errorf("catalog: put gap: %w", err)
	}
	return nil
}

// HasGap reports whether the stream has a gap of reason that starts at
// startMs.
func (db *DB) HasGap(ctx context.Context, cam, profile string, startMs int64, reason GapReason) (bool, error) {
	var n int
	if err := db.sql.QueryRowContext(ctx, "SELECT count(*) FROM gaps WHERE camera_id = ? AND profile = ? AND start_ms = ? AND reason = ?",
		cam, profile, startMs, string(reason)).Scan(&n); err != nil {
		return false, fmt.Errorf("catalog: look for a gap of %s/%s: %w", cam, profile, err)
	}
	return n > 0, nil
}

// CloseGap ends every open gap of the stream at endMs, or at its start if
// that is later.
func (db *DB) CloseGap(ctx context.Context, cam, profile string, endMs int64) error {
	if _, err := db.sql.ExecContext(ctx, "UPDATE gaps SET end_ms = max(?, start_ms) WHERE camera_id = ? AND profile = ? AND end_ms = 0",
		endMs, cam, profile); err != nil {
		return fmt.Errorf("catalog: close the gaps of %s/%s: %w", cam, profile, err)
	}
	return nil
}

// MoveGapStart moves the start of the stream's gaps that start at fromMs to
// toMs: reconcile's background rewrite recovers more of a lost tail than
// its interim file kept (C1-R9). A closed gap ends no earlier than its new
// start; an open gap stays open.
func (db *DB) MoveGapStart(ctx context.Context, cam, profile string, fromMs, toMs int64) error {
	if _, err := db.sql.ExecContext(ctx,
		"UPDATE gaps SET start_ms = ?, end_ms = CASE WHEN end_ms = 0 THEN 0 ELSE max(end_ms, ?) END WHERE camera_id = ? AND profile = ? AND start_ms = ?",
		toMs, toMs, cam, profile, fromMs); err != nil {
		return fmt.Errorf("catalog: move a gap of %s/%s: %w", cam, profile, err)
	}
	return nil
}

// Gaps returns every gap, by stream and start.
func (db *DB) Gaps(ctx context.Context) ([]Gap, error) {
	rows, err := db.sql.QueryContext(ctx, "SELECT camera_id, profile, start_ms, end_ms, reason FROM gaps ORDER BY camera_id, profile, start_ms, id")
	if err != nil {
		return nil, fmt.Errorf("catalog: list gaps: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var gaps []Gap
	for rows.Next() {
		var g Gap
		var reason string
		if err := rows.Scan(&g.Cam, &g.Profile, &g.StartMs, &g.EndMs, &reason); err != nil {
			return nil, fmt.Errorf("catalog: list gaps: %w", err)
		}
		g.Reason = GapReason(reason)
		gaps = append(gaps, g)
	}
	return gaps, rows.Err()
}

// DiffFiles compares the rows with files, the segment files on disk, and
// changes nothing.
func (db *DB) DiffFiles(ctx context.Context, files []FileFact) (ReconcileDiff, error) {
	segs, err := db.Segments(ctx)
	if err != nil {
		return ReconcileDiff{}, err
	}
	return Diff(segs, files), nil
}

// ReconcileFiles makes the rows match files, the segment files on disk: the
// files win (§4.1 (5)). A file without a row gets one, a row whose file says
// otherwise takes the file's facts, and a row without a file becomes lost.
// It returns what it changed, all in one transaction.
func (db *DB) ReconcileFiles(ctx context.Context, files []FileFact) (ReconcileDiff, error) {
	segs, err := db.Segments(ctx)
	if err != nil {
		return ReconcileDiff{}, err
	}
	d := Diff(segs, files)
	if d.Count() == 0 {
		return d, nil
	}
	facts := map[string]FileFact{}
	for _, f := range files {
		facts[f.Path] = f
	}
	rows := map[string]Segment{}
	for _, s := range segs {
		rows[s.Path] = s
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return ReconcileDiff{}, fmt.Errorf("catalog: reconcile the files: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, path := range d.Added {
		f := facts[path]
		id, err := uuid.NewV7()
		if err != nil {
			return ReconcileDiff{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO segments (id, camera_id, profile, local_path, start_ms, end_ms, bytes, encrypted, local_state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id.String(), f.Cam, f.Profile, f.Path, f.StartMs, f.EndMs, f.Bytes, f.Encrypted, StateComplete); err != nil {
			return ReconcileDiff{}, fmt.Errorf("catalog: add %s: %w", path, err)
		}
	}
	for _, path := range d.Updated {
		f, s := facts[path], rows[path]
		start, end := s.StartMs, s.EndMs
		if f.EndMs != 0 {
			start, end = f.StartMs, f.EndMs
		}
		if _, err := tx.ExecContext(ctx, `UPDATE segments SET start_ms = ?, end_ms = ?, bytes = ?, encrypted = ?, local_state = ?, verified_at_ms = NULL
			WHERE local_path = ?`, start, end, f.Bytes, f.Encrypted, StateComplete, path); err != nil {
			return ReconcileDiff{}, fmt.Errorf("catalog: update %s: %w", path, err)
		}
	}
	for _, path := range d.Lost {
		if _, err := tx.ExecContext(ctx, "UPDATE segments SET local_state = ? WHERE local_path = ?", StateLost, path); err != nil {
			return ReconcileDiff{}, fmt.Errorf("catalog: mark %s lost: %w", path, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ReconcileDiff{}, fmt.Errorf("catalog: reconcile the files: %w", err)
	}
	return d, nil
}

// Diff compares segs, rows read from the catalog, with files, the segment
// files on disk. A fact without EndMs is compared by its size and
// encryption only.
func Diff(segs []Segment, files []FileFact) ReconcileDiff {
	rows := map[string]Segment{}
	for _, s := range segs {
		rows[s.Path] = s
	}
	var d ReconcileDiff
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.Path] = true
		s, ok := rows[f.Path]
		switch {
		case !ok:
			d.Added = append(d.Added, f.Path)
		case s.State != StateComplete || s.Bytes != f.Bytes || s.Encrypted != f.Encrypted ||
			(f.EndMs != 0 && (s.StartMs != f.StartMs || s.EndMs != f.EndMs)):
			d.Updated = append(d.Updated, f.Path)
		}
	}
	for _, s := range segs {
		if !seen[s.Path] && s.State != StateLost {
			d.Lost = append(d.Lost, s.Path)
		}
	}
	slices.Sort(d.Added)
	slices.Sort(d.Updated)
	slices.Sort(d.Lost)
	return d
}
