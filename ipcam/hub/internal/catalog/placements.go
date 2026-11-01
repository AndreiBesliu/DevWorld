package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

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
//     pending from 0 with p's bytes, class, rule IDs and encryption: it is
//     superseded, so a worker still uploading the old bytes changes nothing
//     more (g1-4);
//   - a failed pair that is never due again, with p's bytes, is reset the
//     same way: its segment was lost and its file came back (g2-06);
//   - any other pair, a verified one included, stays as it is: a verified
//     copy cannot be replaced yet.
//
// A reset keeps the pair's upload session (Task 5 review M2): a session of
// other bytes may have made its object already (Drive makes the file at the
// last chunk), and only the session names it, so the next upload aborts
// that session at its provider before it begins a new one, after a restart
// too, and a session of the same bytes resumes.
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
	if _, err := tx.ExecContext(ctx, `UPDATE placements SET bytes = ?, class = ?, rule_ids = ?, encrypt = ?, state = ?, attempts = 0, next_attempt_at_ms = 0,
		remote_ref = '', remote_hash = '' WHERE id = ?`, p.Bytes, p.Class, p.RuleIDs, p.Encrypt, PlacementPending, id); err != nil {
		return false, fmt.Errorf("catalog: reset placement %s: %w", id, err)
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

// uploadingQuery reads the uploading placements of the destinations in a
// JSON array, through the placements_queue index (C2-R10).
const uploadingQuery = `SELECT ` + placementColumns + ` FROM placements p
	WHERE p.state = 'uploading' AND p.destination_id IN (SELECT value FROM json_each(?)) ORDER BY p.id`

// UploadingPlacements returns the uploading placements of the destinations
// destIDs, by id: the outbox's start read of the rows that an earlier run
// left uploading (C2-R10), which reads no other row.
func (db *DB) UploadingPlacements(ctx context.Context, destIDs []string) ([]Placement, error) {
	if len(destIDs) == 0 {
		return nil, nil
	}
	dests, err := json.Marshal(destIDs)
	if err != nil {
		return nil, err
	}
	rows, err := db.sql.QueryContext(ctx, uploadingQuery, string(dests))
	if err != nil {
		return nil, fmt.Errorf("catalog: list the uploading placements: %w", err)
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

// Folder returns the remote ID stored for path on the destination destID,
// a folder's or a small file's, and whether there is one: the Drive driver's
// FolderStore (C2-R11 (c)).
func (db *DB) Folder(ctx context.Context, destID, path string) (string, bool, error) {
	var id string
	switch err := db.sql.QueryRowContext(ctx, "SELECT remote_id FROM remote_folders WHERE destination_id = ? AND path = ?", destID, path).Scan(&id); {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("catalog: read the remote ID of %s on %s: %w", path, destID, err)
	}
	return id, true, nil
}

// PutFolder stores remoteID for path on destID, unless an ID is stored for
// it already: the first ID stays (g2-11), so two processes that both missed
// it make one folder, the one whose ID was stored first.
func (db *DB) PutFolder(ctx context.Context, destID, path, remoteID string) error {
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO remote_folders (destination_id, path, remote_id) VALUES (?, ?, ?)
		ON CONFLICT (destination_id, path) DO NOTHING`, destID, path, remoteID); err != nil {
		return fmt.Errorf("catalog: put the remote ID of %s on %s: %w", path, destID, err)
	}
	return nil
}
