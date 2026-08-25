package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrEntityNotFound = errors.New("playlist entity not found")
)

// GetPlaylistEntities returns all PlaylistEntity records for a playlist, ordered by nextEntityId linked list.
func (db *DB) GetPlaylistEntities(ctx context.Context, listID int64) ([]PlaylistEntity, error) {
	query := `SELECT id, listId, trackId, databaseUuid, nextEntityId, membershipReference
		FROM PlaylistEntity WHERE listId = ?`
	rows, err := db.mDB.QueryContext(ctx, query, listID)
	if err != nil {
		return nil, fmt.Errorf("querying playlist entities for list %d: %w", listID, err)
	}
	defer rows.Close()

	var entities []PlaylistEntity
	for rows.Next() {
		var e PlaylistEntity
		var dbUUID sql.NullString
		var memRef sql.NullInt64
		if err := rows.Scan(&e.ID, &e.ListID, &e.TrackID, &dbUUID, &e.NextEntityID, &memRef); err != nil {
			return nil, fmt.Errorf("scanning playlist entity: %w", err)
		}
		e.DatabaseUUID = dbUUID.String
		e.MembershipReference = memRef.Int64
		entities = append(entities, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating playlist entities: %w", err)
	}

	return orderPlaylistEntities(entities), nil
}

// GetPlaylistTracks returns all tracks in a playlist ordered by linked-list sequence.
func (db *DB) GetPlaylistTracks(ctx context.Context, listID int64) ([]Track, error) {
	entities, err := db.GetPlaylistEntities(ctx, listID)
	if err != nil {
		return nil, err
	}
	if len(entities) == 0 {
		return nil, nil
	}

	tracks := make([]Track, 0, len(entities))
	for _, entity := range entities {
		track, err := db.GetTrackByID(ctx, entity.TrackID)
		if err != nil {
			// Skip missing tracks or log
			continue
		}
		tracks = append(tracks, *track)
	}

	return tracks, nil
}

// AddTrackToPlaylist appends a track to a playlist and updates the previous tail's nextEntityId.
func (db *DB) AddTrackToPlaylist(ctx context.Context, listID int64, trackID int64) (int64, error) {
	if db.readonly {
		return 0, errors.New("cannot add track to playlist in read-only database")
	}

	var newEntityID int64
	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		// Find current tail entity in this playlist
		entities, err := getEntitiesInTx(ctx, tx, listID)
		if err != nil {
			return err
		}

		ordered := orderPlaylistEntities(entities)
		var tailID int64
		if len(ordered) > 0 {
			tailID = ordered[len(ordered)-1].ID
		}

		// Insert new entity
		query := `INSERT INTO PlaylistEntity (listId, trackId, databaseUuid, nextEntityId, membershipReference)
			VALUES (?, ?, '', 0, 0)`
		res, err := tx.ExecContext(ctx, query, listID, trackID)
		if err != nil {
			return fmt.Errorf("inserting playlist entity: %w", err)
		}

		newID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("getting last insert entity id: %w", err)
		}
		newEntityID = newID

		// If there was a tail entity, update its nextEntityId
		if tailID > 0 {
			_, err = tx.ExecContext(ctx, `UPDATE PlaylistEntity SET nextEntityId = ? WHERE id = ?`, newID, tailID)
			if err != nil {
				return fmt.Errorf("updating tail entity nextEntityId: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return 0, err
	}
	return newEntityID, nil
}

// RemoveTrackFromPlaylist deletes an entity link and connects the preceding entity to nextEntityId.
func (db *DB) RemoveTrackFromPlaylist(ctx context.Context, listID int64, trackID int64) error {
	if db.readonly {
		return errors.New("cannot remove track from playlist in read-only database")
	}

	return db.WithTx(ctx, func(tx *sql.Tx) error {
		// Find target entity
		var targetID, nextEntityID int64
		row := tx.QueryRowContext(ctx, `SELECT id, nextEntityId FROM PlaylistEntity WHERE listId = ? AND trackId = ? LIMIT 1`, listID, trackID)
		if err := row.Scan(&targetID, &nextEntityID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrEntityNotFound
			}
			return fmt.Errorf("finding target playlist entity: %w", err)
		}

		// Relink predecessor
		_, err := tx.ExecContext(ctx, `UPDATE PlaylistEntity SET nextEntityId = ? WHERE listId = ? AND nextEntityId = ?`,
			nextEntityID, listID, targetID)
		if err != nil {
			return fmt.Errorf("relinking predecessor playlist entity: %w", err)
		}

		// Delete target entity
		_, err = tx.ExecContext(ctx, `DELETE FROM PlaylistEntity WHERE id = ?`, targetID)
		if err != nil {
			return fmt.Errorf("deleting playlist entity %d: %w", targetID, err)
		}

		return nil
	})
}

// MoveTrackBetweenPlaylists transfers track membership from one playlist to another atomically.
func (db *DB) MoveTrackBetweenPlaylists(ctx context.Context, srcListID, dstListID, trackID int64) error {
	if db.readonly {
		return errors.New("cannot move track between playlists in read-only database")
	}

	return db.WithTx(ctx, func(tx *sql.Tx) error {
		// 1. Remove from source
		var targetID, nextEntityID int64
		row := tx.QueryRowContext(ctx, `SELECT id, nextEntityId FROM PlaylistEntity WHERE listId = ? AND trackId = ? LIMIT 1`, srcListID, trackID)
		if err := row.Scan(&targetID, &nextEntityID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrEntityNotFound
			}
			return fmt.Errorf("finding track in source playlist: %w", err)
		}

		// Relink predecessor in source
		_, err := tx.ExecContext(ctx, `UPDATE PlaylistEntity SET nextEntityId = ? WHERE listId = ? AND nextEntityId = ?`,
			nextEntityID, srcListID, targetID)
		if err != nil {
			return fmt.Errorf("relinking source predecessor: %w", err)
		}

		// Delete from source
		_, err = tx.ExecContext(ctx, `DELETE FROM PlaylistEntity WHERE id = ?`, targetID)
		if err != nil {
			return fmt.Errorf("deleting from source playlist: %w", err)
		}

		// 2. Add to destination tail
		entities, err := getEntitiesInTx(ctx, tx, dstListID)
		if err != nil {
			return err
		}
		ordered := orderPlaylistEntities(entities)
		var tailID int64
		if len(ordered) > 0 {
			tailID = ordered[len(ordered)-1].ID
		}

		query := `INSERT INTO PlaylistEntity (listId, trackId, databaseUuid, nextEntityId, membershipReference)
			VALUES (?, ?, '', 0, 0)`
		res, err := tx.ExecContext(ctx, query, dstListID, trackID)
		if err != nil {
			return fmt.Errorf("inserting destination playlist entity: %w", err)
		}

		newID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("getting new entity ID in destination: %w", err)
		}

		if tailID > 0 {
			_, err = tx.ExecContext(ctx, `UPDATE PlaylistEntity SET nextEntityId = ? WHERE id = ?`, newID, tailID)
			if err != nil {
				return fmt.Errorf("updating destination tail nextEntityId: %w", err)
			}
		}

		return nil
	})
}

func getEntitiesInTx(ctx context.Context, tx *sql.Tx, listID int64) ([]PlaylistEntity, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, listId, trackId, databaseUuid, nextEntityId, membershipReference FROM PlaylistEntity WHERE listId = ?`, listID)
	if err != nil {
		return nil, fmt.Errorf("querying entities in tx: %w", err)
	}
	defer rows.Close()

	var entities []PlaylistEntity
	for rows.Next() {
		var e PlaylistEntity
		var dbUUID sql.NullString
		var memRef sql.NullInt64
		if err := rows.Scan(&e.ID, &e.ListID, &e.TrackID, &dbUUID, &e.NextEntityID, &memRef); err != nil {
			return nil, fmt.Errorf("scanning entity in tx: %w", err)
		}
		e.DatabaseUUID = dbUUID.String
		e.MembershipReference = memRef.Int64
		entities = append(entities, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating entities in tx: %w", err)
	}
	return entities, nil
}

// orderPlaylistEntities orders entities along nextEntityId linked list.
func orderPlaylistEntities(entities []PlaylistEntity) []PlaylistEntity {
	if len(entities) <= 1 {
		return entities
	}

	nextIDMap := make(map[int64]struct{}, len(entities))
	for _, e := range entities {
		if e.NextEntityID != 0 {
			nextIDMap[e.NextEntityID] = struct{}{}
		}
	}

	// Find the head entity (not referenced by any nextEntityId)
	var headID int64 = -1
	for _, e := range entities {
		if _, referenced := nextIDMap[e.ID]; !referenced {
			headID = e.ID
			break
		}
	}

	if headID == -1 {
		return entities
	}

	idMap := make(map[int64]PlaylistEntity, len(entities))
	for _, e := range entities {
		idMap[e.ID] = e
	}

	var ordered []PlaylistEntity
	currID := headID
	visited := make(map[int64]bool)

	for currID != 0 && !visited[currID] {
		e, exists := idMap[currID]
		if !exists {
			break
		}
		visited[currID] = true
		ordered = append(ordered, e)
		currID = e.NextEntityID
	}

	// Append any leftover unlinked entities
	if len(ordered) < len(entities) {
		for _, e := range entities {
			if !visited[e.ID] {
				ordered = append(ordered, e)
			}
		}
	}

	return ordered
}
