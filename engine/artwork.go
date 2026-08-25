package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrAlbumArtNotFound = errors.New("album art not found")
)

// GetAlbumArt retrieves an artwork record by primary key ID.
func (db *DB) GetAlbumArt(ctx context.Context, id int64) (*AlbumArtRecord, error) {
	cols, err := getTableColumns(db.mDB, "AlbumArt")
	if err != nil || len(cols) == 0 {
		return nil, ErrAlbumArtNotFound
	}

	query := `SELECT id, hash, albumArt FROM AlbumArt WHERE id = ?`
	row := db.mDB.QueryRowContext(ctx, query, id)

	var art AlbumArtRecord
	if err := row.Scan(&art.ID, &art.Hash, &art.Data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAlbumArtNotFound
		}
		return nil, fmt.Errorf("getting album art %d: %w", id, err)
	}
	return &art, nil
}

// GetAlbumArtByHash retrieves an artwork record by its unique SHA-1 hash.
func (db *DB) GetAlbumArtByHash(ctx context.Context, hash string) (*AlbumArtRecord, error) {
	cols, err := getTableColumns(db.mDB, "AlbumArt")
	if err != nil || len(cols) == 0 {
		return nil, ErrAlbumArtNotFound
	}

	query := `SELECT id, hash, albumArt FROM AlbumArt WHERE hash = ? LIMIT 1`
	row := db.mDB.QueryRowContext(ctx, query, hash)

	var art AlbumArtRecord
	if err := row.Scan(&art.ID, &art.Hash, &art.Data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAlbumArtNotFound
		}
		return nil, fmt.Errorf("getting album art by hash %q: %w", hash, err)
	}
	return &art, nil
}

// GetAllAlbumArt returns all album art records in the database.
func (db *DB) GetAllAlbumArt(ctx context.Context) ([]AlbumArtRecord, error) {
	cols, err := getTableColumns(db.mDB, "AlbumArt")
	if err != nil || len(cols) == 0 {
		return nil, nil
	}

	query := `SELECT id, hash, albumArt FROM AlbumArt ORDER BY id`
	rows, err := db.mDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying album art: %w", err)
	}
	defer rows.Close()

	var results []AlbumArtRecord
	for rows.Next() {
		var art AlbumArtRecord
		if err := rows.Scan(&art.ID, &art.Hash, &art.Data); err != nil {
			return nil, fmt.Errorf("scanning album art row: %w", err)
		}
		results = append(results, art)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating album art rows: %w", err)
	}
	return results, nil
}

// InsertOrUpdateAlbumArt inserts a new album art BLOB or updates an existing record matching the hash.
func (db *DB) InsertOrUpdateAlbumArt(ctx context.Context, hash string, blob []byte) (int64, error) {
	if db.readonly {
		return 0, errors.New("cannot insert/update album art in read-only database")
	}

	var artID int64
	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		var existingID int64
		row := tx.QueryRowContext(ctx, `SELECT id FROM AlbumArt WHERE hash = ? LIMIT 1`, hash)
		err := row.Scan(&existingID)
		if err == nil {
			// Update existing
			_, err = tx.ExecContext(ctx, `UPDATE AlbumArt SET albumArt = ? WHERE id = ?`, blob, existingID)
			if err != nil {
				return fmt.Errorf("updating album art %d: %w", existingID, err)
			}
			artID = existingID
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("checking existing album art hash: %w", err)
		}

		// Insert new
		res, err := tx.ExecContext(ctx, `INSERT INTO AlbumArt (hash, albumArt) VALUES (?, ?)`, hash, blob)
		if err != nil {
			return fmt.Errorf("inserting album art: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("getting last insert album art id: %w", err)
		}
		artID = id
		return nil
	})

	if err != nil {
		return 0, err
	}
	return artID, nil
}

// DeleteAlbumArt removes an album art record by ID.
func (db *DB) DeleteAlbumArt(ctx context.Context, id int64) error {
	if db.readonly {
		return errors.New("cannot delete album art from read-only database")
	}

	return db.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM AlbumArt WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("deleting album art %d: %w", id, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking deleted album art rows: %w", err)
		}
		if affected == 0 {
			return ErrAlbumArtNotFound
		}
		return nil
	})
}
