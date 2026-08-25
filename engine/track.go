package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrTrackNotFound = errors.New("track not found")

// GetTrackByID retrieves a single track record by primary key ID.
func (db *DB) GetTrackByID(ctx context.Context, id int64) (*Track, error) {
	cols, err := getTableColumns(db.mDB, "Track")
	if err != nil {
		return nil, fmt.Errorf("reading track table columns: %w", err)
	}

	query := buildTrackSelectQuery(cols, "WHERE id = ?")
	row := db.mDB.QueryRowContext(ctx, query, id)
	t, err := scanTrack(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTrackNotFound
		}
		return nil, fmt.Errorf("getting track %d: %w", id, err)
	}
	return t, nil
}

// FindTrack finds a track by numeric ID, exact path, filename, or title.
func (db *DB) FindTrack(ctx context.Context, query string) (*Track, error) {
	if query == "" {
		return nil, ErrTrackNotFound
	}

	cols, err := getTableColumns(db.mDB, "Track")
	if err != nil {
		return nil, fmt.Errorf("reading track table columns: %w", err)
	}

	// 1. Try parsing numeric ID
	if id, err := strconv.ParseInt(query, 10, 64); err == nil {
		t, err := db.GetTrackByID(ctx, id)
		if err == nil {
			return t, nil
		}
	}

	// 2. Try exact path match
	q := buildTrackSelectQuery(cols, "WHERE path = ? LIMIT 1")
	row := db.mDB.QueryRowContext(ctx, q, query)
	if t, err := scanTrack(row); err == nil {
		return t, nil
	}

	// 3. Try filename match
	q = buildTrackSelectQuery(cols, "WHERE filename = ? LIMIT 1")
	row = db.mDB.QueryRowContext(ctx, q, query)
	if t, err := scanTrack(row); err == nil {
		return t, nil
	}

	// 4. Try case-insensitive title match
	q = buildTrackSelectQuery(cols, "WHERE title = ? COLLATE NOCASE LIMIT 1")
	row = db.mDB.QueryRowContext(ctx, q, query)
	if t, err := scanTrack(row); err == nil {
		return t, nil
	}

	return nil, ErrTrackNotFound
}

// GetAllTracks returns a slice of all tracks in the collection ordered by ID.
func (db *DB) GetAllTracks(ctx context.Context) ([]Track, error) {
	cols, err := getTableColumns(db.mDB, "Track")
	if err != nil {
		return nil, fmt.Errorf("reading track table columns: %w", err)
	}

	query := buildTrackSelectQuery(cols, "ORDER BY id")
	rows, err := db.mDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying tracks: %w", err)
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		t, err := scanTrackRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning track row: %w", err)
		}
		tracks = append(tracks, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating track rows: %w", err)
	}

	return tracks, nil
}

// InsertTrack inserts a new Track record and returns the generated ID.
func (db *DB) InsertTrack(ctx context.Context, t *Track) (int64, error) {
	if db.readonly {
		return 0, errors.New("cannot insert track into read-only database")
	}

	now := time.Now().UTC()
	if t.DateAdded == nil {
		t.DateAdded = &now
	}
	if t.DateCreated == nil {
		t.DateCreated = &now
	}
	if t.LastEditTime == nil {
		t.LastEditTime = &now
	}
	if t.Filename == "" && t.Path != "" {
		t.Filename = filepath.Base(t.Path)
	}

	var albumArtID sql.NullInt64
	if t.AlbumArtID > 0 {
		albumArtID = sql.NullInt64{Int64: t.AlbumArtID, Valid: true}
	}

	var bpmAnalyzed sql.NullFloat64
	if t.BPMAnalyzed > 0 {
		bpmAnalyzed = sql.NullFloat64{Float64: t.BPMAnalyzed, Valid: true}
	}

	query := `INSERT INTO Track (
		playOrder, length, bpm, year, path, filename, bitrate, bpmAnalyzed, albumArtId, fileBytes,
		title, artist, album, genre, comment, label, composer, remixer, key, rating, albumArt,
		timeLastPlayed, isPlayed, fileType, isAnalyzed, dateCreated, dateAdded, isAvailable,
		isMetadataOfPackedTrackChanged, isPerfomanceDataOfPackedTrackChanged, playedIndicator,
		isMetadataImported, pdbImportKey, streamingSource, uri, isBeatGridLocked, originDatabaseUuid,
		originTrackId, streamingFlags, explicitLyrics, lastEditTime, albumArtSourceHash
	) VALUES (
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?, ?,
		?, ?, ?,
		?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?
	)`

	var insertID int64
	err := db.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, query,
			t.PlayOrder, t.Length, t.BPM, t.Year, t.Path, t.Filename, t.Bitrate, bpmAnalyzed, albumArtID, t.FileBytes,
			t.Title, t.Artist, t.Album, t.Genre, t.Comment, t.Label, t.Composer, t.Remixer, t.Key, t.Rating, t.AlbumArt,
			t.TimeLastPlayed, t.IsPlayed, t.FileType, t.IsAnalyzed, t.DateCreated, t.DateAdded, t.IsAvailable,
			t.IsMetadataOfPackedTrackChanged, t.IsPerformanceDataOfPackedTrackChanged, t.PlayedIndicator,
			t.IsMetadataImported, t.PdbImportKey, t.StreamingSource, t.URI, t.IsBeatGridLocked, t.OriginDatabaseUUID,
			t.OriginTrackID, t.StreamingFlags, t.ExplicitLyrics, t.LastEditTime, t.AlbumArtSourceHash,
		)
		if err != nil {
			return fmt.Errorf("executing insert track: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("getting last insert id: %w", err)
		}
		insertID = id
		t.ID = id
		return nil
	})

	if err != nil {
		return 0, err
	}
	return insertID, nil
}

// UpdateTrack updates track metadata for an existing track.
func (db *DB) UpdateTrack(ctx context.Context, t *Track) error {
	if db.readonly {
		return errors.New("cannot update track in read-only database")
	}
	if t.ID <= 0 {
		return errors.New("invalid track ID for update")
	}

	now := time.Now().UTC()
	if t.LastEditTime == nil {
		t.LastEditTime = &now
	}
	if t.Filename == "" && t.Path != "" {
		t.Filename = filepath.Base(t.Path)
	}

	var albumArtID sql.NullInt64
	if t.AlbumArtID > 0 {
		albumArtID = sql.NullInt64{Int64: t.AlbumArtID, Valid: true}
	}

	var bpmAnalyzed sql.NullFloat64
	if t.BPMAnalyzed > 0 {
		bpmAnalyzed = sql.NullFloat64{Float64: t.BPMAnalyzed, Valid: true}
	}

	query := `UPDATE Track SET
		playOrder = ?, length = ?, bpm = ?, year = ?, path = ?, filename = ?, bitrate = ?, bpmAnalyzed = ?,
		albumArtId = ?, fileBytes = ?, title = ?, artist = ?, album = ?, genre = ?, comment = ?,
		label = ?, composer = ?, remixer = ?, key = ?, rating = ?, albumArt = ?, timeLastPlayed = ?,
		isPlayed = ?, fileType = ?, isAnalyzed = ?, dateCreated = ?, dateAdded = ?, isAvailable = ?,
		isMetadataOfPackedTrackChanged = ?, isPerfomanceDataOfPackedTrackChanged = ?, playedIndicator = ?,
		isMetadataImported = ?, pdbImportKey = ?, streamingSource = ?, uri = ?, isBeatGridLocked = ?,
		originDatabaseUuid = ?, originTrackId = ?, streamingFlags = ?, explicitLyrics = ?,
		lastEditTime = ?, albumArtSourceHash = ?
		WHERE id = ?`

	return db.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, query,
			t.PlayOrder, t.Length, t.BPM, t.Year, t.Path, t.Filename, t.Bitrate, bpmAnalyzed,
			albumArtID, t.FileBytes, t.Title, t.Artist, t.Album, t.Genre, t.Comment,
			t.Label, t.Composer, t.Remixer, t.Key, t.Rating, t.AlbumArt, t.TimeLastPlayed,
			t.IsPlayed, t.FileType, t.IsAnalyzed, t.DateCreated, t.DateAdded, t.IsAvailable,
			t.IsMetadataOfPackedTrackChanged, t.IsPerformanceDataOfPackedTrackChanged, t.PlayedIndicator,
			t.IsMetadataImported, t.PdbImportKey, t.StreamingSource, t.URI, t.IsBeatGridLocked,
			t.OriginDatabaseUUID, t.OriginTrackID, t.StreamingFlags, t.ExplicitLyrics,
			t.LastEditTime, t.AlbumArtSourceHash, t.ID,
		)
		if err != nil {
			return fmt.Errorf("executing update track %d: %w", t.ID, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking rows affected: %w", err)
		}
		if affected == 0 {
			return ErrTrackNotFound
		}
		return nil
	})
}

// DeleteTrack deletes a track and cascades removal of its PlaylistEntity and PerformanceData records.
func (db *DB) DeleteTrack(ctx context.Context, id int64) error {
	if db.readonly {
		return errors.New("cannot delete track from read-only database")
	}

	return db.WithTx(ctx, func(tx *sql.Tx) error {
		// Find playlist entities that reference this track so we can relink their playlists
		var entities []struct {
			id           int64
			listID       int64
			nextEntityID int64
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, listId, nextEntityId FROM PlaylistEntity WHERE trackId = ?`, id)
		if err != nil {
			return fmt.Errorf("querying playlist entities for track %d: %w", id, err)
		}
		for rows.Next() {
			var e struct {
				id           int64
				listID       int64
				nextEntityID int64
			}
			if err := rows.Scan(&e.id, &e.listID, &e.nextEntityID); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scanning playlist entity for track %d: %w", id, err)
			}
			entities = append(entities, e)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterating playlist entities for track %d: %w", id, err)
		}
		_ = rows.Close()

		for _, e := range entities {
			// Relink previous entity to nextEntityID
			_, err = tx.ExecContext(ctx, `UPDATE PlaylistEntity SET nextEntityId = ? WHERE listId = ? AND nextEntityId = ?`,
				e.nextEntityID, e.listID, e.id)
			if err != nil {
				return fmt.Errorf("relinking playlist entity: %w", err)
			}
		}

		// Delete from PlaylistEntity
		if _, err := tx.ExecContext(ctx, `DELETE FROM PlaylistEntity WHERE trackId = ?`, id); err != nil {
			return fmt.Errorf("deleting playlist entities for track %d: %w", id, err)
		}

		// Delete from PerformanceData
		if _, err := tx.ExecContext(ctx, `DELETE FROM PerformanceData WHERE trackId = ?`, id); err != nil {
			return fmt.Errorf("deleting performance data for track %d: %w", id, err)
		}

		// Delete from Track
		res, err := tx.ExecContext(ctx, `DELETE FROM Track WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("deleting track %d: %w", id, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("checking affected rows: %w", err)
		}
		if affected == 0 {
			return ErrTrackNotFound
		}

		return nil
	})
}

// RelocateTracks batch-replaces path prefixes (e.g. migrating drive mount points).
func (db *DB) RelocateTracks(ctx context.Context, oldPrefix, newPrefix string) (int, error) {
	if db.readonly {
		return 0, errors.New("cannot relocate tracks in read-only database")
	}

	tracks, err := db.GetAllTracks(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting tracks for relocation: %w", err)
	}

	count := 0
	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		for _, t := range tracks {
			if strings.HasPrefix(t.Path, oldPrefix) {
				newPath := newPrefix + strings.TrimPrefix(t.Path, oldPrefix)
				newFilename := filepath.Base(newPath)
				now := time.Now().UTC()

				_, err := tx.ExecContext(ctx, `UPDATE Track SET path = ?, filename = ?, lastEditTime = ? WHERE id = ?`,
					newPath, newFilename, now, t.ID)
				if err != nil {
					return fmt.Errorf("updating track path %d: %w", t.ID, err)
				}
				count++
			}
		}
		return nil
	})

	if err != nil {
		return 0, err
	}
	return count, nil
}

func buildTrackSelectQuery(cols map[string]bool, suffix string) string {
	colExpr := func(col string, fallback string) string {
		if cols[col] {
			return col
		}
		return fallback + " AS " + col
	}

	return fmt.Sprintf(`SELECT id, title, artist, composer, album, genre, fileType, fileBytes, length, year,
		bpm, %s, dateAdded, bitrate, comment, rating, path, remixer, key, label, lastEditTime,
		%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s
		FROM Track %s`,
		colExpr("bpmAnalyzed", "0"),
		colExpr("albumArtId", "NULL"),
		colExpr("timeLastPlayed", "NULL"),
		colExpr("isPlayed", "0"),
		colExpr("isAnalyzed", "0"),
		colExpr("dateCreated", "NULL"),
		colExpr("playedIndicator", "0"),
		colExpr("streamingSource", "NULL"),
		colExpr("uri", "NULL"),
		colExpr("isBeatGridLocked", "0"),
		colExpr("originDatabaseUuid", "NULL"),
		colExpr("originTrackId", "NULL"),
		colExpr("streamingFlags", "0"),
		colExpr("explicitLyrics", "0"),
		colExpr("albumArtSourceHash", "NULL"),
		colExpr("playOrder", "0"),
		colExpr("filename", "NULL"),
		colExpr("albumArt", "NULL"),
		colExpr("isAvailable", "1"),
		colExpr("isMetadataOfPackedTrackChanged", "0"),
		colExpr("isPerfomanceDataOfPackedTrackChanged", "0"),
		colExpr("isMetadataImported", "0"),
		colExpr("pdbImportKey", "0"),
		suffix,
	)
}

type trackScanner interface {
	Scan(dest ...any) error
}

func scanTrackRecord(scanner trackScanner) (*Track, error) {
	var (
		id                                    sql.NullInt64
		title                                 sql.NullString
		artist                                sql.NullString
		composer                              sql.NullString
		album                                 sql.NullString
		genre                                 sql.NullString
		fileType                              sql.NullString
		fileBytes                             sql.NullInt64
		length                                sql.NullInt64
		year                                  sql.NullInt64
		bpm                                   sql.NullInt64
		bpmAnalyzed                           sql.NullFloat64
		dateAdded                             nullTime
		bitrate                               sql.NullInt64
		comment                               sql.NullString
		rating                                sql.NullInt64
		path                                  sql.NullString
		remixer                               sql.NullString
		key                                   sql.NullInt32
		label                                 sql.NullString
		lastEditTime                          nullTime
		albumArtID                            sql.NullInt64
		timeLastPlayed                        nullTime
		isPlayed                              sql.NullBool
		isAnalyzed                            sql.NullBool
		dateCreated                           nullTime
		playedIndicator                       sql.NullInt64
		streamingSource                       sql.NullString
		uri                                   sql.NullString
		isBeatGridLocked                      sql.NullBool
		originDatabaseUUID                    sql.NullString
		originTrackID                         sql.NullInt64
		streamingFlags                        sql.NullInt64
		explicitLyrics                        sql.NullBool
		albumArtSourceHash                    sql.NullString
		playOrder                             sql.NullInt64
		filename                              sql.NullString
		albumArt                              sql.NullString
		isAvailable                           sql.NullBool
		isMetadataOfPackedTrackChanged        sql.NullBool
		isPerformanceDataOfPackedTrackChanged sql.NullBool
		isMetadataImported                    sql.NullBool
		pdbImportKey                          sql.NullInt64
	)

	err := scanner.Scan(
		&id, &title, &artist, &composer, &album, &genre, &fileType, &fileBytes, &length, &year,
		&bpm, &bpmAnalyzed, &dateAdded, &bitrate, &comment, &rating, &path, &remixer, &key, &label, &lastEditTime,
		&albumArtID, &timeLastPlayed, &isPlayed, &isAnalyzed, &dateCreated,
		&playedIndicator, &streamingSource, &uri, &isBeatGridLocked, &originDatabaseUUID,
		&originTrackID, &streamingFlags, &explicitLyrics, &albumArtSourceHash,
		&playOrder, &filename, &albumArt, &isAvailable,
		&isMetadataOfPackedTrackChanged, &isPerformanceDataOfPackedTrackChanged, &isMetadataImported, &pdbImportKey,
	)
	if err != nil {
		return nil, err
	}

	t := &Track{
		ID:                                    id.Int64,
		PlayOrder:                             playOrder.Int64,
		Length:                                length.Int64,
		BPM:                                   bpm.Int64,
		Year:                                  year.Int64,
		Path:                                  path.String,
		Filename:                              filename.String,
		Bitrate:                               bitrate.Int64,
		BPMAnalyzed:                           bpmAnalyzed.Float64,
		AlbumArtID:                            albumArtID.Int64,
		FileBytes:                             fileBytes.Int64,
		Title:                                 title.String,
		Artist:                                artist.String,
		Album:                                 album.String,
		Genre:                                 genre.String,
		Comment:                               comment.String,
		Label:                                 label.String,
		Composer:                              composer.String,
		Remixer:                               remixer.String,
		Key:                                   key.Int32,
		Rating:                                rating.Int64,
		AlbumArt:                              albumArt.String,
		IsPlayed:                              isPlayed.Bool,
		FileType:                              fileType.String,
		IsAnalyzed:                            isAnalyzed.Bool,
		IsAvailable:                           isAvailable.Bool,
		IsMetadataOfPackedTrackChanged:        isMetadataOfPackedTrackChanged.Bool,
		IsPerformanceDataOfPackedTrackChanged: isPerformanceDataOfPackedTrackChanged.Bool,
		PlayedIndicator:                       playedIndicator.Int64,
		IsMetadataImported:                    isMetadataImported.Bool,
		PdbImportKey:                          pdbImportKey.Int64,
		StreamingSource:                       streamingSource.String,
		URI:                                   uri.String,
		IsBeatGridLocked:                      isBeatGridLocked.Bool,
		OriginDatabaseUUID:                    originDatabaseUUID.String,
		OriginTrackID:                         originTrackID.Int64,
		StreamingFlags:                        streamingFlags.Int64,
		ExplicitLyrics:                        explicitLyrics.Bool,
		AlbumArtSourceHash:                    albumArtSourceHash.String,
	}

	if dateAdded.Valid {
		t.DateAdded = &dateAdded.Time
	}
	if dateCreated.Valid {
		t.DateCreated = &dateCreated.Time
	}
	if lastEditTime.Valid {
		t.LastEditTime = &lastEditTime.Time
	}
	if timeLastPlayed.Valid {
		t.TimeLastPlayed = &timeLastPlayed.Time
	}

	return t, nil
}

func scanTrack(row *sql.Row) (*Track, error) {
	return scanTrackRecord(row)
}

func scanTrackRows(rows *sql.Rows) (*Track, error) {
	return scanTrackRecord(rows)
}
