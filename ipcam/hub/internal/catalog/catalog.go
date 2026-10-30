// Package catalog is the hub's SQLite catalog (§4.6), in its Phase 0 subset:
// the recorded segments and the gaps between them (ruling G07), the reason
// of each lost tail that a .part's repair will write (T40), and the
// placements of segments on destinations with their upload sessions (P03).
// The files under rec/ are the source of truth (§4.1 (5)); ReconcileFiles
// makes the rows match them.
//
// It uses modernc.org/sqlite (pure Go) with journal_mode=WAL and
// synchronous=FULL on every connection, so a committed row survives a power
// cut, and forward-only migrations. Segment ids are UUIDv7 and times are in
// UTC milliseconds.
package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// The states of a placement (§4.6, P25): pending → uploading (a worker
// took it) → verified, failed (due again at its next attempt, or never when
// that is 0) or pending again (its destination paused it). deleted is set
// only by diag dest-probe --delete-segment.
const (
	PlacementPending   = "pending"
	PlacementUploading = "uploading"
	PlacementVerified  = "verified"
	PlacementFailed    = "failed"
	PlacementDeleted   = "deleted"
)

// ObjectSegment is the object kind of a segment's placement, the only kind
// in Phase 0.
const ObjectSegment = "segment"

// The classes of a placement (§9.3 ContentType), in the queue's order
// (§9.7): event clips and thumbnails, then continuous, then exports.
const (
	ClassEventClip  = "EVENT_CLIP"
	ClassThumbnail  = "THUMBNAIL"
	ClassContinuous = "CONTINUOUS"
	ClassExport     = "EXPORT"
)

// Placement is one object's copy on one destination: a row of placements
// (§4.6). Times are in UTC milliseconds.
type Placement struct {
	ID, ObjectKind, ObjectID, DestinationID, Class, RuleIDs string
	Encrypt                                                 bool
	RetainUntilMs                                           int64
	State, RemoteRef, RemoteHash                            string
	Bytes                                                   int64
	Attempts                                                int
	NextAttemptAtMs                                         int64
}

// ErrStalePlacement means the placement row changed since the worker read
// it: its state or its bytes are no longer those the write names (g1-4).
var ErrStalePlacement = errors.New("catalog: the placement changed since it was read")

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
	// 3: the reason of the tail gap of a .part that MediaMTX could not write
	// (T40), until the file's repair writes that gap.
	`CREATE TABLE tail_reasons (
		local_path TEXT PRIMARY KEY,
		reason     TEXT NOT NULL CHECK (reason IN ('restart', 'hub_stopped', 'disk_full', 'card', 'camera_offline'))
	);`,
	// c2's upload tables, after c1's migrations (§4.6 L552, L557; P03): the
	// placements of segments on destinations, their upload sessions, the
	// sealed secrets and the destinations' remote folder IDs. A catalog that
	// holds them is refused by a hub without this migration.
	`CREATE TABLE placements (
		id TEXT PRIMARY KEY,
		object_kind TEXT NOT NULL CHECK (object_kind IN ('segment')),
		object_id TEXT NOT NULL, destination_id TEXT NOT NULL,
		class TEXT NOT NULL CHECK (class IN ('EVENT_CLIP','THUMBNAIL','CONTINUOUS','EXPORT')),
		rule_ids TEXT NOT NULL DEFAULT '', encrypt INTEGER NOT NULL, retain_until_ms INTEGER NOT NULL DEFAULT 0,
		state TEXT NOT NULL CHECK (state IN ('pending','uploading','verified','failed','deleted')),
		remote_ref TEXT NOT NULL DEFAULT '', remote_hash TEXT NOT NULL DEFAULT '',
		bytes INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at_ms INTEGER NOT NULL DEFAULT 0,
		UNIQUE (object_id, destination_id));
	CREATE INDEX placements_queue ON placements (state, next_attempt_at_ms);
	CREATE TABLE upload_sessions (placement_id TEXT PRIMARY KEY REFERENCES placements(id), state_json BLOB NOT NULL, offset INTEGER NOT NULL);
	CREATE TABLE secrets (id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK (kind IN ('camera','oauth','s3','webdav','sftp','smb')), sealed_blob BLOB NOT NULL, sealer TEXT NOT NULL);
	CREATE TABLE remote_folders (destination_id TEXT NOT NULL, path TEXT NOT NULL, remote_id TEXT NOT NULL, PRIMARY KEY (destination_id, path));`,
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

// WithTx runs fn in a transaction of the catalog and commits it when fn
// returns nil. Otherwise it rolls the transaction back and returns fn's
// error as it is.
func (db *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: begin a transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: commit a transaction: %w", err)
	}
	return nil
}

// execer is what a segment upsert needs: the catalog or a transaction of it.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// PutSegment writes s, by its path: a new path gets a row with a new UUIDv7
// id (or s.ID when it is set), a known path is updated and keeps its id. A
// row whose bytes or state change is no longer verified.
func (db *DB) PutSegment(ctx context.Context, s Segment) error {
	return putSegment(ctx, db.sql, s)
}

// PutSegmentTx is PutSegment on the caller's transaction tx, so that the row
// and what the caller writes with it, such as its placements, commit
// together (S3).
func PutSegmentTx(ctx context.Context, tx *sql.Tx, s Segment) error {
	return putSegment(ctx, tx, s)
}

func putSegment(ctx context.Context, db execer, s Segment) error {
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
	_, err := db.ExecContext(ctx, `
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

// segmentColumns are the columns that scanSegment reads, in its order.
const segmentColumns = "id, camera_id, profile, local_path, start_ms, end_ms, bytes, encrypted, local_state, verified_at_ms"

// rowScanner is a row of a query: *sql.Row or *sql.Rows.
type rowScanner interface{ Scan(dest ...any) error }

func scanSegment(row rowScanner) (Segment, error) {
	var s Segment
	var verified sql.NullInt64
	if err := row.Scan(&s.ID, &s.Cam, &s.Profile, &s.Path, &s.StartMs, &s.EndMs, &s.Bytes, &s.Encrypted, &s.State, &verified); err != nil {
		return Segment{}, err
	}
	if verified.Valid {
		s.VerifiedAtMs = &verified.Int64
	}
	return s, nil
}

// Segments returns every segment, by path.
func (db *DB) Segments(ctx context.Context) ([]Segment, error) {
	rows, err := db.sql.QueryContext(ctx, "SELECT "+segmentColumns+" FROM segments ORDER BY local_path")
	if err != nil {
		return nil, fmt.Errorf("catalog: list segments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var segs []Segment
	for rows.Next() {
		s, err := scanSegment(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: list segments: %w", err)
		}
		segs = append(segs, s)
	}
	return segs, rows.Err()
}

// SegmentByID returns the segment whose id is id. An unknown id is an error
// that wraps sql.ErrNoRows.
func (db *DB) SegmentByID(ctx context.Context, id string) (Segment, error) {
	s, err := scanSegment(db.sql.QueryRowContext(ctx, "SELECT "+segmentColumns+" FROM segments WHERE id = ?", id))
	if err != nil {
		return Segment{}, fmt.Errorf("catalog: read segment %s: %w", id, err)
	}
	return s, nil
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

// FindGap returns the start of the stream's earliest gap of reason that
// starts in [fromMs, toMs], and whether there is one.
func (db *DB) FindGap(ctx context.Context, cam, profile string, reason GapReason, fromMs, toMs int64) (startMs int64, found bool, err error) {
	var start sql.NullInt64
	if err := db.sql.QueryRowContext(ctx, "SELECT min(start_ms) FROM gaps WHERE camera_id = ? AND profile = ? AND reason = ? AND start_ms BETWEEN ? AND ?",
		cam, profile, string(reason), fromMs, toMs).Scan(&start); err != nil {
		return 0, false, fmt.Errorf("catalog: look for a gap of %s/%s: %w", cam, profile, err)
	}
	return start.Int64, start.Valid, nil
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

// MoveGapStart moves the start of the stream's gaps of reason that start
// at fromMs to toMs: reconcile's tail gap moves between the file's start and
// what the file keeps, and the background rewrite recovers more of a lost
// tail than its interim file kept (C1-R9). A gap of another reason, such as
// an SD card's (T40), stays where it is. A closed gap ends no earlier than
// its new start; an open gap stays open.
func (db *DB) MoveGapStart(ctx context.Context, cam, profile string, reason GapReason, fromMs, toMs int64) error {
	if _, err := db.sql.ExecContext(ctx,
		"UPDATE gaps SET start_ms = ?, end_ms = CASE WHEN end_ms = 0 THEN 0 ELSE max(end_ms, ?) END WHERE camera_id = ? AND profile = ? AND reason = ? AND start_ms = ?",
		toMs, toMs, cam, profile, string(reason), fromMs); err != nil {
		return fmt.Errorf("catalog: move a gap of %s/%s: %w", cam, profile, err)
	}
	return nil
}

// PutTailReason records reason for the tail gap of the .part at path,
// relative to rec/ with "/": the repair of the file writes that gap with it
// instead of restart, also after a start of the hub. A second call for the
// same path replaces the reason.
func (db *DB) PutTailReason(ctx context.Context, path string, reason GapReason) error {
	if !reason.valid() {
		return fmt.Errorf("catalog: unknown gap reason %q", reason)
	}
	if _, err := db.sql.ExecContext(ctx, "INSERT OR REPLACE INTO tail_reasons (local_path, reason) VALUES (?, ?)", path, string(reason)); err != nil {
		return fmt.Errorf("catalog: put the tail reason of %s: %w", path, err)
	}
	return nil
}

// TailReason returns the reason recorded for the tail gap of the .part at
// path, and whether there is one.
func (db *DB) TailReason(ctx context.Context, path string) (GapReason, bool, error) {
	var reason string
	switch err := db.sql.QueryRowContext(ctx, "SELECT reason FROM tail_reasons WHERE local_path = ?", path).Scan(&reason); {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("catalog: read the tail reason of %s: %w", path, err)
	}
	return GapReason(reason), true, nil
}

// DeleteTailReason forgets the reason of the .part at path, once its repair
// has written the gap and the file has gone.
func (db *DB) DeleteTailReason(ctx context.Context, path string) error {
	if _, err := db.sql.ExecContext(ctx, "DELETE FROM tail_reasons WHERE local_path = ?", path); err != nil {
		return fmt.Errorf("catalog: delete the tail reason of %s: %w", path, err)
	}
	return nil
}

// TailReasonPaths returns the paths of the .part files that have a tail
// reason, sorted.
func (db *DB) TailReasonPaths(ctx context.Context) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, "SELECT local_path FROM tail_reasons ORDER BY local_path")
	if err != nil {
		return nil, fmt.Errorf("catalog: list the tail reasons: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("catalog: list the tail reasons: %w", err)
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: list the tail reasons: %w", err)
	}
	return paths, nil
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

// placementColumns are the columns that scanPlacement reads, in its order,
// of the placements table under the alias p.
const placementColumns = `p.id, p.object_kind, p.object_id, p.destination_id, p.class, p.rule_ids, p.encrypt, p.retain_until_ms,
	p.state, p.remote_ref, p.remote_hash, p.bytes, p.attempts, p.next_attempt_at_ms`

func scanPlacement(row rowScanner) (Placement, error) {
	var p Placement
	err := row.Scan(&p.ID, &p.ObjectKind, &p.ObjectID, &p.DestinationID, &p.Class, &p.RuleIDs, &p.Encrypt, &p.RetainUntilMs,
		&p.State, &p.RemoteRef, &p.RemoteHash, &p.Bytes, &p.Attempts, &p.NextAttemptAtMs)
	return p, err
}

// PutPlacementTx writes p on the caller's transaction tx, by its object and
// destination (P03):
//   - a new pair gets a pending row, with a new UUIDv7 id or p.ID when it is
//     set;
//   - a pair that is not verified and whose bytes differ from p's is reset to
//     pending from 0 with p's bytes, and its upload session is deleted: it is
//     superseded, so a worker still uploading the old bytes changes nothing
//     more (g1-4);
//   - a failed pair that is never due again, with p's bytes, is reset to
//     pending from 0 without its session: its segment was lost and its file
//     came back (g2-06);
//   - any other pair, a verified one included, stays as it is: a verified
//     copy cannot be replaced yet.
func PutPlacementTx(ctx context.Context, tx *sql.Tx, p Placement) (superseded bool, err error) {
	var id, state string
	var bytes, next int64
	err = tx.QueryRowContext(ctx, "SELECT id, state, bytes, next_attempt_at_ms FROM placements WHERE object_id = ? AND destination_id = ?",
		p.ObjectID, p.DestinationID).Scan(&id, &state, &bytes, &next)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if p.ID == "" {
			uid, err := uuid.NewV7()
			if err != nil {
				return false, err
			}
			p.ID = uid.String()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO placements (id, object_kind, object_id, destination_id, class, rule_ids, encrypt, retain_until_ms, state, bytes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.ObjectKind, p.ObjectID, p.DestinationID, p.Class, p.RuleIDs, p.Encrypt, p.RetainUntilMs, PlacementPending, p.Bytes); err != nil {
			return false, fmt.Errorf("catalog: put the placement of %s on %s: %w", p.ObjectID, p.DestinationID, err)
		}
		return false, nil
	case err != nil:
		return false, fmt.Errorf("catalog: read the placement of %s on %s: %w", p.ObjectID, p.DestinationID, err)
	}
	superseded = state != PlacementVerified && bytes != p.Bytes
	returned := state == PlacementFailed && next == 0 && bytes == p.Bytes
	if !superseded && !returned {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE placements SET bytes = ?, state = ?, attempts = 0, next_attempt_at_ms = 0, remote_ref = '', remote_hash = ''
		WHERE id = ?`, p.Bytes, PlacementPending, id); err != nil {
		return false, fmt.Errorf("catalog: reset placement %s: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM upload_sessions WHERE placement_id = ?", id); err != nil {
		return false, fmt.Errorf("catalog: delete the upload session of %s: %w", id, err)
	}
	return superseded, nil
}

// DuePlacements returns at most limit placements of the destinations destIDs
// that are due at nowMs: pending ones due by now, and failed ones due again
// by now, of complete segments only. They come in the queue's order (§9.7):
// event clips and thumbnails, then continuous, then exports, each class by
// its segments' start, the oldest first.
func (db *DB) DuePlacements(ctx context.Context, nowMs int64, destIDs []string, limit int) ([]Placement, error) {
	return db.duePlacements(ctx, StateComplete, nowMs, destIDs, limit)
}

// LostPlacements returns the placements of destIDs that DuePlacements's
// rule makes due at nowMs but whose segment is lost: its file is gone.
func (db *DB) LostPlacements(ctx context.Context, nowMs int64, destIDs []string) ([]Placement, error) {
	return db.duePlacements(ctx, StateLost, nowMs, destIDs, -1) // LIMIT -1: no limit
}

func (db *DB) duePlacements(ctx context.Context, segState string, nowMs int64, destIDs []string, limit int) ([]Placement, error) {
	if len(destIDs) == 0 {
		return nil, nil
	}
	dests, err := json.Marshal(destIDs)
	if err != nil {
		return nil, err
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+placementColumns+` FROM placements p JOIN segments s ON p.object_id = s.id
		WHERE p.object_kind = 'segment' AND s.local_state = ? AND p.destination_id IN (SELECT value FROM json_each(?))
			AND ((p.state = 'pending' AND p.next_attempt_at_ms <= ?) OR (p.state = 'failed' AND p.next_attempt_at_ms BETWEEN 1 AND ?))
		ORDER BY CASE p.class WHEN 'EVENT_CLIP' THEN 0 WHEN 'THUMBNAIL' THEN 0 WHEN 'CONTINUOUS' THEN 1 ELSE 2 END, s.start_ms, p.id
		LIMIT ?`, segState, string(dests), nowMs, nowMs, limit)
	if err != nil {
		return nil, fmt.Errorf("catalog: list the due placements: %w", err)
	}
	return collectPlacements(rows)
}

// Placements returns every placement, by id.
func (db *DB) Placements(ctx context.Context) ([]Placement, error) {
	rows, err := db.sql.QueryContext(ctx, "SELECT "+placementColumns+" FROM placements p ORDER BY p.id")
	if err != nil {
		return nil, fmt.Errorf("catalog: list placements: %w", err)
	}
	return collectPlacements(rows)
}

func collectPlacements(rows *sql.Rows) ([]Placement, error) {
	defer func() { _ = rows.Close() }()
	var ps []Placement
	for rows.Next() {
		p, err := scanPlacement(rows)
		if err != nil {
			return nil, fmt.Errorf("catalog: list placements: %w", err)
		}
		ps = append(ps, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: list placements: %w", err)
	}
	return ps, nil
}

// UpdatePlacement writes p's state, remote ref and hash, attempts and next
// attempt time to its row, only while the row is still in wasState with
// wasBytes, the state and bytes that the writer read. Otherwise it changes
// nothing and returns ErrStalePlacement (g1-4).
func (db *DB) UpdatePlacement(ctx context.Context, p Placement, wasState string, wasBytes int64) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE placements SET state = ?, remote_ref = ?, remote_hash = ?, attempts = ?, next_attempt_at_ms = ?
		WHERE id = ? AND state = ? AND bytes = ?`,
		p.State, p.RemoteRef, p.RemoteHash, p.Attempts, p.NextAttemptAtMs, p.ID, wasState, wasBytes)
	if err != nil {
		return fmt.Errorf("catalog: update placement %s: %w", p.ID, err)
	}
	return staleUnlessChanged(res, p.ID)
}

// PutUploadSession stores the upload session of placementID, stateJSON at
// offset, only while the placement is uploading with bytes, the bytes that
// the worker read. Otherwise it stores nothing and returns
// ErrStalePlacement (g1-4).
func (db *DB) PutUploadSession(ctx context.Context, placementID string, bytes int64, stateJSON []byte, offset int64) error {
	res, err := db.sql.ExecContext(ctx, `INSERT INTO upload_sessions (placement_id, state_json, offset)
		SELECT ?, ?, ? WHERE EXISTS (SELECT 1 FROM placements WHERE id = ? AND state = ? AND bytes = ?)
		ON CONFLICT (placement_id) DO UPDATE SET state_json = excluded.state_json, offset = excluded.offset`,
		placementID, stateJSON, offset, placementID, PlacementUploading, bytes)
	if err != nil {
		return fmt.Errorf("catalog: put the upload session of %s: %w", placementID, err)
	}
	return staleUnlessChanged(res, placementID)
}

// staleUnlessChanged returns ErrStalePlacement when res changed no row.
func staleUnlessChanged(res sql.Result, placementID string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("catalog: placement %s: %w", placementID, err)
	}
	if n == 0 {
		return fmt.Errorf("placement %s: %w", placementID, ErrStalePlacement)
	}
	return nil
}

// UploadSession returns the upload session of placementID and its offset,
// and whether there is one.
func (db *DB) UploadSession(ctx context.Context, placementID string) (stateJSON []byte, offset int64, ok bool, err error) {
	switch err := db.sql.QueryRowContext(ctx, "SELECT state_json, offset FROM upload_sessions WHERE placement_id = ?", placementID).Scan(&stateJSON, &offset); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, 0, false, nil
	case err != nil:
		return nil, 0, false, fmt.Errorf("catalog: read the upload session of %s: %w", placementID, err)
	}
	return stateJSON, offset, true, nil
}

// DeleteUploadSession forgets the upload session of placementID: its upload
// is verified, or starts again from 0.
func (db *DB) DeleteUploadSession(ctx context.Context, placementID string) error {
	if _, err := db.sql.ExecContext(ctx, "DELETE FROM upload_sessions WHERE placement_id = ?", placementID); err != nil {
		return fmt.Errorf("catalog: delete the upload session of %s: %w", placementID, err)
	}
	return nil
}
