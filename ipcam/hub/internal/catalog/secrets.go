package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// The secrets table (§4.6 L552, P19) holds every secret of the hub as a
// sealed row: its id, its kind (the table's CHECK: camera, oauth, s3,
// webdav, sftp, smb), the sealed blob and the name of the sealer's backend.
// The catalog never sees a plaintext; no method holds a transaction across
// its caller's I/O, so a sealer or a token endpoint never holds the
// catalog's write lock. Errors name the row's id only.

// PutSecret writes the row id, or replaces the one there.
func (db *DB) PutSecret(ctx context.Context, id, kind string, sealed []byte, sealer string) error {
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO secrets (id, kind, sealed_blob, sealer) VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET kind = excluded.kind, sealed_blob = excluded.sealed_blob, sealer = excluded.sealer`,
		id, kind, sealed, sealer); err != nil {
		return fmt.Errorf("catalog: put the secret %s: %w", id, err)
	}
	return nil
}

// Secret returns the sealed blob of the row id and its sealer's backend, and
// whether there is one.
func (db *DB) Secret(ctx context.Context, id string) (sealed []byte, sealer string, ok bool, err error) {
	switch err := db.sql.QueryRowContext(ctx, "SELECT sealed_blob, sealer FROM secrets WHERE id = ?", id).Scan(&sealed, &sealer); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, "", false, nil
	case err != nil:
		return nil, "", false, fmt.Errorf("catalog: read the secret %s: %w", id, err)
	}
	return sealed, sealer, true, nil
}

// SwapSecret writes next to the row id only while the row still holds old,
// the blob that the caller read: a compare-and-swap in one short
// transaction. swapped is false, and nothing changes, when another writer
// changed the row first or there is none.
func (db *DB) SwapSecret(ctx context.Context, id string, old, next []byte, sealer string) (swapped bool, err error) {
	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		var stored []byte
		switch err := tx.QueryRowContext(ctx, "SELECT sealed_blob FROM secrets WHERE id = ?", id).Scan(&stored); {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return err
		case !bytes.Equal(stored, old):
			return nil
		}
		if _, err := tx.ExecContext(ctx, "UPDATE secrets SET sealed_blob = ?, sealer = ? WHERE id = ?", next, sealer, id); err != nil {
			return err
		}
		swapped = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("catalog: swap the secret %s: %w", id, err)
	}
	return swapped, nil
}

// DeleteSecret deletes the row id, if there is one.
func (db *DB) DeleteSecret(ctx context.Context, id string) error {
	if _, err := db.sql.ExecContext(ctx, "DELETE FROM secrets WHERE id = ?", id); err != nil {
		return fmt.Errorf("catalog: delete the secret %s: %w", id, err)
	}
	return nil
}

// SecretIDs returns the ids of the rows that start with prefix, sorted. The
// prefix is matched as it is, never as a LIKE pattern.
func (db *DB) SecretIDs(ctx context.Context, prefix string) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, "SELECT id FROM secrets WHERE substr(id, 1, length(?1)) = ?1 ORDER BY id", prefix)
	if err != nil {
		return nil, fmt.Errorf("catalog: list the secrets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("catalog: list the secrets: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: list the secrets: %w", err)
	}
	return ids, nil
}

// ReplaceSecrets writes rows, each of kind and sealer, and deletes every
// other row whose id starts with prefix (taken as it is, never as a LIKE
// pattern), in one short transaction: either every change is made, or none
// (Task 9A review M7). The rows are written in the order of their ids.
func (db *DB) ReplaceSecrets(ctx context.Context, prefix, kind string, rows map[string][]byte, sealer string) error {
	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		for _, id := range slices.Sorted(maps.Keys(rows)) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO secrets (id, kind, sealed_blob, sealer) VALUES (?, ?, ?, ?)
				ON CONFLICT (id) DO UPDATE SET kind = excluded.kind, sealed_blob = excluded.sealed_blob, sealer = excluded.sealer`,
				id, kind, rows[id], sealer); err != nil {
				return fmt.Errorf("put %s: %w", id, err)
			}
		}
		ids, err := tx.QueryContext(ctx, "SELECT id FROM secrets WHERE substr(id, 1, length(?1)) = ?1", prefix)
		if err != nil {
			return err
		}
		var stale []string
		for ids.Next() {
			var id string
			if err := ids.Scan(&id); err != nil {
				_ = ids.Close()
				return err
			}
			if _, keep := rows[id]; !keep {
				stale = append(stale, id)
			}
		}
		if err := errors.Join(ids.Err(), ids.Close()); err != nil {
			return err
		}
		for _, id := range stale {
			if _, err := tx.ExecContext(ctx, "DELETE FROM secrets WHERE id = ?", id); err != nil {
				return fmt.Errorf("delete %s: %w", id, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("catalog: replace the secrets %s*: %w", prefix, err)
	}
	return nil
}
