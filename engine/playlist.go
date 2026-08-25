package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrPlaylistNotFound     = errors.New("playlist not found")
	ErrPlaylistHasChildren  = errors.New("playlist has child playlists (use force to delete)")
	ErrCircularPlaylistMove = errors.New("cannot move playlist into itself or its descendant")
)

// GetPlaylistByID retrieves a single playlist record by primary key ID.
func (db *DB) GetPlaylistByID(ctx context.Context, id int64) (*Playlist, error) {
	query := `SELECT id, title, parentListId, isPersisted, nextListId, lastEditTime, isExplicitlyExported
		FROM Playlist WHERE id = ?`
	row := db.mDB.QueryRowContext(ctx, query, id)

	var p Playlist
	var lastEdit nullTime
	var isPersisted, isExplicitlyExported sql.NullBool

	err := row.Scan(&p.ID, &p.Title, &p.ParentListID, &isPersisted, &p.NextListID, &lastEdit, &isExplicitlyExported)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlaylistNotFound
		}
		return nil, fmt.Errorf("getting playlist %d: %w", id, err)
	}

	p.IsPersisted = isPersisted.Bool
	p.IsExplicitlyExported = isExplicitlyExported.Bool
	if lastEdit.Valid {
		p.LastEditTime = &lastEdit.Time
	}
	return &p, nil
}

// GetAllPlaylists returns all playlists in the database.
func (db *DB) GetAllPlaylists(ctx context.Context) ([]Playlist, error) {
	query := `SELECT id, title, parentListId, isPersisted, nextListId, lastEditTime, isExplicitlyExported
		FROM Playlist ORDER BY id`
	rows, err := db.mDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying playlists: %w", err)
	}
	defer rows.Close()

	var playlists []Playlist
	for rows.Next() {
		var p Playlist
		var lastEdit nullTime
		var isPersisted, isExplicitlyExported sql.NullBool
		if err := rows.Scan(&p.ID, &p.Title, &p.ParentListID, &isPersisted, &p.NextListID, &lastEdit, &isExplicitlyExported); err != nil {
			return nil, fmt.Errorf("scanning playlist: %w", err)
		}
		p.IsPersisted = isPersisted.Bool
		p.IsExplicitlyExported = isExplicitlyExported.Bool
		if lastEdit.Valid {
			p.LastEditTime = &lastEdit.Time
		}
		playlists = append(playlists, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating playlist rows: %w", err)
	}
	return playlists, nil
}

// GetPlaylistHierarchy returns the complete hierarchical tree of playlists and folders.
func (db *DB) GetPlaylistHierarchy(ctx context.Context) ([]PlaylistNode, error) {
	playlists, err := db.GetAllPlaylists(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching playlists for hierarchy: %w", err)
	}
	if len(playlists) == 0 {
		return nil, nil
	}

	// Group playlists by parentListId
	byParent := make(map[int64][]Playlist)
	for _, p := range playlists {
		byParent[p.ParentListID] = append(byParent[p.ParentListID], p)
	}

	// Sort each group by nextListId linked list
	for parentID, siblings := range byParent {
		byParent[parentID] = orderPlaylistSiblings(siblings)
	}

	var buildTree func(parentID int64) ([]PlaylistNode, error)
	buildTree = func(parentID int64) ([]PlaylistNode, error) {
		siblings := byParent[parentID]
		var nodes []PlaylistNode
		for _, p := range siblings {
			children, err := buildTree(p.ID)
			if err != nil {
				return nil, err
			}

			tracks, err := db.GetPlaylistTracks(ctx, p.ID)
			if err != nil {
				return nil, fmt.Errorf("fetching playlist %d tracks: %w", p.ID, err)
			}

			isFolder := len(children) > 0 && len(tracks) == 0

			nodes = append(nodes, PlaylistNode{
				Playlist: p,
				IsFolder: isFolder,
				Children: children,
				Tracks:   tracks,
			})
		}
		return nodes, nil
	}

	return buildTree(0)
}

// CreatePlaylist creates a new playlist or folder under parentID, linking it to the sibling chain.
func (db *DB) CreatePlaylist(ctx context.Context, title string, parentID int64, isFolder bool) (*Playlist, error) {
	if db.readonly {
		return nil, errors.New("cannot create playlist in read-only database")
	}

	now := time.Now().UTC()
	p := &Playlist{
		Title:                title,
		ParentListID:         parentID,
		IsPersisted:          true,
		NextListID:           0,
		LastEditTime:         &now,
		IsExplicitlyExported: true,
	}

	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		// Find current tail sibling under parentID
		var tailID int64
		row := tx.QueryRowContext(ctx, `SELECT id FROM Playlist WHERE parentListId = ? AND (nextListId = 0 OR nextListId IS NULL) LIMIT 1`, parentID)
		if err := row.Scan(&tailID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("finding tail sibling: %w", err)
		}

		query := `INSERT INTO Playlist (title, parentListId, isPersisted, nextListId, lastEditTime, isExplicitlyExported)
			VALUES (?, ?, ?, 0, ?, ?)`
		res, err := tx.ExecContext(ctx, query, p.Title, p.ParentListID, p.IsPersisted, p.LastEditTime, p.IsExplicitlyExported)
		if err != nil {
			return fmt.Errorf("inserting playlist: %w", err)
		}

		newID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("getting new playlist id: %w", err)
		}
		p.ID = newID

		// If there was a tail sibling, update its nextListId to point to newID
		if tailID > 0 && tailID != newID {
			_, err = tx.ExecContext(ctx, `UPDATE Playlist SET nextListId = ? WHERE id = ?`, newID, tailID)
			if err != nil {
				return fmt.Errorf("updating predecessor nextListId: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return p, nil
}

// DeletePlaylist deletes a playlist and its track memberships. If force is true, child playlists are also deleted.
func (db *DB) DeletePlaylist(ctx context.Context, id int64, force bool) error {
	if db.readonly {
		return errors.New("cannot delete playlist in read-only database")
	}

	return db.WithTx(ctx, func(tx *sql.Tx) error {
		// Check if playlist exists
		var parentListID, nextListID int64
		err := tx.QueryRowContext(ctx, `SELECT parentListId, nextListId FROM Playlist WHERE id = ?`, id).Scan(&parentListID, &nextListID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPlaylistNotFound
			}
			return fmt.Errorf("querying playlist %d: %w", id, err)
		}

		// Check for child playlists
		var childCount int64
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM Playlist WHERE parentListId = ?`, id).Scan(&childCount)
		if err != nil {
			return fmt.Errorf("checking child playlists: %w", err)
		}
		if childCount > 0 && !force {
			return ErrPlaylistHasChildren
		}

		// Relink predecessor sibling to nextListID
		_, err = tx.ExecContext(ctx, `UPDATE Playlist SET nextListId = ? WHERE parentListId = ? AND nextListId = ?`,
			nextListID, parentListID, id)
		if err != nil {
			return fmt.Errorf("relinking predecessor playlist: %w", err)
		}

		// Helper to delete recursively
		var deleteSubtree func(plID int64) error
		deleteSubtree = func(plID int64) error {
			// Find all children
			cRows, err := tx.QueryContext(ctx, `SELECT id FROM Playlist WHERE parentListId = ?`, plID)
			if err != nil {
				return fmt.Errorf("querying child playlists of %d: %w", plID, err)
			}
			var children []int64
			for cRows.Next() {
				var cID int64
				if err := cRows.Scan(&cID); err != nil {
					_ = cRows.Close()
					return fmt.Errorf("scanning child playlist ID: %w", err)
				}
				children = append(children, cID)
			}
			if err := cRows.Err(); err != nil {
				_ = cRows.Close()
				return fmt.Errorf("iterating child playlists: %w", err)
			}
			_ = cRows.Close()

			for _, cID := range children {
				if err := deleteSubtree(cID); err != nil {
					return err
				}
			}

			// Delete entities
			if _, err := tx.ExecContext(ctx, `DELETE FROM PlaylistEntity WHERE listId = ?`, plID); err != nil {
				return fmt.Errorf("deleting playlist entities for %d: %w", plID, err)
			}

			// Delete playlist
			if _, err := tx.ExecContext(ctx, `DELETE FROM Playlist WHERE id = ?`, plID); err != nil {
				return fmt.Errorf("deleting playlist %d: %w", plID, err)
			}
			return nil
		}

		return deleteSubtree(id)
	})
}

// MovePlaylist reparents a playlist, validating against circular nesting and maintaining sibling linked lists.
func (db *DB) MovePlaylist(ctx context.Context, id int64, newParentID int64) error {
	if db.readonly {
		return errors.New("cannot move playlist in read-only database")
	}
	if id == newParentID {
		return ErrCircularPlaylistMove
	}

	return db.WithTx(ctx, func(tx *sql.Tx) error {
		// Validate target is not a descendant of id
		curr := newParentID
		for curr > 0 {
			if curr == id {
				return ErrCircularPlaylistMove
			}
			var pParent int64
			err := tx.QueryRowContext(ctx, `SELECT parentListId FROM Playlist WHERE id = ?`, curr).Scan(&pParent)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					break
				}
				return fmt.Errorf("checking hierarchy for cycle: %w", err)
			}
			curr = pParent
		}

		// Get current playlist parent and nextListId
		var oldParentID, nextListID int64
		err := tx.QueryRowContext(ctx, `SELECT parentListId, nextListId FROM Playlist WHERE id = ?`, id).Scan(&oldParentID, &nextListID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPlaylistNotFound
			}
			return fmt.Errorf("getting moving playlist %d: %w", id, err)
		}

		if oldParentID == newParentID {
			return nil // No change
		}

		// 1. Unlink from old sibling chain: update predecessor
		_, err = tx.ExecContext(ctx, `UPDATE Playlist SET nextListId = ? WHERE parentListId = ? AND nextListId = ?`,
			nextListID, oldParentID, id)
		if err != nil {
			return fmt.Errorf("unlinking from old sibling chain: %w", err)
		}

		// 2. Find tail in new parent chain
		var newTailID int64
		row := tx.QueryRowContext(ctx, `SELECT id FROM Playlist WHERE parentListId = ? AND (nextListId = 0 OR nextListId IS NULL) AND id != ? LIMIT 1`, newParentID, id)
		if err := row.Scan(&newTailID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("finding new parent tail: %w", err)
		}

		// 3. Update moving playlist's parentListId and nextListId = 0
		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `UPDATE Playlist SET parentListId = ?, nextListId = 0, lastEditTime = ? WHERE id = ?`,
			newParentID, now, id)
		if err != nil {
			return fmt.Errorf("updating playlist %d parent: %w", id, err)
		}

		// 4. Link previous tail in new parent chain to id
		if newTailID > 0 {
			_, err = tx.ExecContext(ctx, `UPDATE Playlist SET nextListId = ? WHERE id = ?`, id, newTailID)
			if err != nil {
				return fmt.Errorf("linking new tail predecessor: %w", err)
			}
		}

		return nil
	})
}

// orderPlaylistSiblings orders a slice of playlists based on the nextListId linked list.
func orderPlaylistSiblings(playlists []Playlist) []Playlist {
	if len(playlists) <= 1 {
		return playlists
	}

	nextIDMap := make(map[int64]struct{})
	for _, p := range playlists {
		if p.NextListID != 0 {
			nextIDMap[p.NextListID] = struct{}{}
		}
	}

	// Find the head (the one not referenced as nextListID)
	var headID int64 = -1
	for _, p := range playlists {
		if _, referenced := nextIDMap[p.ID]; !referenced {
			headID = p.ID
			break
		}
	}

	if headID == -1 {
		return playlists
	}

	idMap := make(map[int64]Playlist, len(playlists))
	for _, p := range playlists {
		idMap[p.ID] = p
	}

	var ordered []Playlist
	currID := headID
	visited := make(map[int64]bool)

	for currID != 0 && !visited[currID] {
		p, exists := idMap[currID]
		if !exists {
			break
		}
		visited[currID] = true
		ordered = append(ordered, p)
		currID = p.NextListID
	}

	// Append any unreachable playlists
	if len(ordered) < len(playlists) {
		for _, p := range playlists {
			if !visited[p.ID] {
				ordered = append(ordered, p)
			}
		}
	}

	return ordered
}
