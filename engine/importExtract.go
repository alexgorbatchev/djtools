package engine

import (
	"database/sql"
	"fmt"
	"path/filepath"
)

func importExtract(path string) (library, error) {
	var enLibrary library
	var err error

	m, hm, err := initDB(path)
	if err != nil {
		return library{}, err
	}
	defer m.Close()
	defer hm.Close()
	enLibrary.info, err = importExtractInformation(m)
	if err != nil {
		// Information table might be empty or missing in older/test databases, fallback gracefully
		enLibrary.info = information{}
	}
	enLibrary.albumArtList, _ = importExtractAlbumArt(m)
	enLibrary.songs, err = importExtractTrack(m)
	if err != nil {
		return library{}, fmt.Errorf("error extracting track data: %v", err)
	}
	enLibrary.songHistoryList, _ = importExtractHistory(hm)
	enLibrary.perfData, err = importExtractPerformanceData(m)
	if err != nil {
		return library{}, fmt.Errorf("error extracting performance data: %v", err)
	}
	enLibrary.playlists, err = importExtractPlaylist(m)
	if err != nil {
		return library{}, fmt.Errorf("error extracting playlists: %v", err)
	}
	enLibrary.playlistEntityList, err = importExtractPlaylistEntity(m)
	if err != nil {
		return library{}, fmt.Errorf("error extracting playlist data: %v", err)
	}
	enLibrary.smartlistList, _ = importExtractSmartlist(m)
	return enLibrary, nil
}

// initDB initializes the Engine SQL database at a given path.
func initDB(path string) (*sql.DB, *sql.DB, error) {
	// Construct platform-independent file paths
	mPath := filepath.Join(path, "Database2", "m.db")
	hmPath := filepath.Join(path, "Database2", "hm.db")

	// Open and ping the m.db database
	m, err := sql.Open("sqlite3", mPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error opening m.db: %v", err)
	}
	if err = m.Ping(); err != nil {
		return nil, nil, fmt.Errorf("error initializing m.db: %v", err)
	}

	// Open and ping the hm.db database
	hm, err := sql.Open("sqlite3", hmPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error opening hm.db: %v", err)
	}
	if err = hm.Ping(); err != nil {
		return nil, nil, fmt.Errorf("error initializing hm.db: %v", err)
	}

	return m, hm, nil
}

func importExtractInformation(db *sql.DB) (information, error) {
	cols, err := getTableColumns(db, "Information")
	if err != nil || len(cols) == 0 {
		return information{}, nil
	}
	query := `SELECT id, uuid, schemaVersionMajor, schemaVersionMinor, schemaVersionPatch FROM Information LIMIT 1`
	var info information
	err = db.QueryRow(query).Scan(&info.id, &info.uuid, &info.schemaVersionMajor, &info.schemaVersionMinor, &info.schemaVersionPatch)
	if err != nil {
		return information{}, nil
	}
	return info, nil
}

func importExtractAlbumArt(db *sql.DB) ([]albumArtEntry, error) {
	cols, err := getTableColumns(db, "AlbumArt")
	if err != nil || len(cols) == 0 {
		return nil, nil
	}
	query := `SELECT id, hash, albumArt FROM AlbumArt ORDER BY id`
	return queryAndScanRows(db, query, func(r *sql.Rows) (albumArtEntry, error) {
		var entry albumArtEntry
		err := r.Scan(&entry.id, &entry.hash, &entry.data)
		return entry, err
	})
}

func importExtractTrack(db *sql.DB) ([]songNull, error) {
	cols, err := getTableColumns(db, "Track")
	if err != nil {
		return nil, fmt.Errorf("error reading Track columns: %w", err)
	}

	colExpr := func(col string, fallback string) string {
		if cols[col] {
			return col
		}
		return fallback + " AS " + col
	}

	query := fmt.Sprintf(`SELECT id, title, artist, composer, album, genre, fileType, fileBytes, length, year,
		bpm, %s, dateAdded, bitrate, comment, rating, path, remixer, key, label, lastEditTime,
		%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s
		FROM Track ORDER BY id`,
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
	)

	return queryAndScanRows(db, query, func(r *sql.Rows) (songNull, error) {
		var song songNull
		err := r.Scan(
			&song.id, &song.title, &song.artist, &song.composer, &song.album, &song.genre, &song.filetype,
			&song.size, &song.length, &song.year, &song.bpm, &song.bpmAnalyzed, &song.dateAdded, &song.bitrate,
			&song.comment, &song.rating, &song.path, &song.remixer, &song.key, &song.label, &song.lastEditTime,
			&song.albumArtId, &song.timeLastPlayed, &song.isPlayed, &song.isAnalyzed, &song.dateCreated,
			&song.playedIndicator, &song.streamingSource, &song.uri, &song.isBeatGridLocked, &song.originDatabaseUuid,
			&song.originTrackId, &song.streamingFlags, &song.explicitLyrics, &song.albumArtSourceHash,
		)
		return song, err
	})
}

func importExtractHistory(db *sql.DB) ([]songHistory, error) {
	query := `SELECT Track.originTrackId, COUNT(HistorylistEntity.trackId), MAX(HistorylistEntity.startTime) 
		FROM Track JOIN HistorylistEntity ON Track.id=HistorylistEntity.trackId
		GROUP BY Track.originTrackId ORDER BY Track.originTrackId`

	return queryAndScanRows(db, query, func(r *sql.Rows) (songHistory, error) {
		var songHistory songHistory
		err := r.Scan(&songHistory.id, &songHistory.plays, &songHistory.lastPlayed)
		return songHistory, err
	})
}

func importExtractPerformanceData(db *sql.DB) ([]performanceDataEntry, error) {
	cols, err := getTableColumns(db, "PerformanceData")
	if err != nil {
		return nil, fmt.Errorf("error reading PerformanceData columns: %w", err)
	}

	colExpr := func(col string, fallback string) string {
		if cols[col] {
			return col
		}
		return fallback + " AS " + col
	}

	query := fmt.Sprintf(`SELECT trackId, beatData, quickCues, loops, %s, %s, %s FROM PerformanceData ORDER BY trackId`,
		colExpr("trackData", "NULL"),
		colExpr("overviewWaveFormData", "NULL"),
		colExpr("activeOnLoadLoops", "0"),
	)

	return queryAndScanRows(db, query, func(r *sql.Rows) (performanceDataEntry, error) {
		var perfData performanceDataEntry
		err := r.Scan(&perfData.id, &perfData.beatDataBlob, &perfData.quickCuesBlob, &perfData.loopsBlob,
			&perfData.trackDataBlob, &perfData.overviewWaveFormDataBlob, &perfData.activeOnLoadLoops)
		return perfData, err
	})
}

func getTableColumns(db *sql.DB, tableName string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err == nil {
			cols[name] = true
		}
	}
	return cols, nil
}

func importExtractPlaylist(db *sql.DB) ([]playlist, error) {
	query := `SELECT id, title, parentListId, nextListId FROM Playlist ORDER BY id`

	return queryAndScanRows(db, query, func(r *sql.Rows) (playlist, error) {
		var playlist playlist
		err := r.Scan(&playlist.id, &playlist.title, &playlist.parentListId, &playlist.nextListId)
		return playlist, err
	})
}

func importExtractPlaylistEntity(db *sql.DB) ([]playlistEntity, error) {
	query := `SELECT id, listId, trackId, nextEntityId FROM PlaylistEntity ORDER BY listId`

	return queryAndScanRows(db, query, func(r *sql.Rows) (playlistEntity, error) {
		var playlistEntity playlistEntity
		err := r.Scan(&playlistEntity.id, &playlistEntity.listId,
			&playlistEntity.trackId, &playlistEntity.nextEntityId)
		return playlistEntity, err
	})
}

func importExtractSmartlist(db *sql.DB) ([]smartlist, error) {
	cols, err := getTableColumns(db, "Smartlist")
	if err != nil || len(cols) == 0 {
		return nil, nil
	}
	query := `SELECT listUuid, title, parentPlaylistPath, nextPlaylistPath, nextListUuid, rules
		FROM Smartlist ORDER BY listUuid`

	return queryAndScanRows(db, query, func(r *sql.Rows) (smartlist, error) {
		var smartlist smartlist
		err := r.Scan(&smartlist.listUuid, &smartlist.title, &smartlist.parentPlaylistPath,
			&smartlist.nextPlaylistPath, &smartlist.nextListUuid, &smartlist.rules)
		return smartlist, err
	})
}

// queryAndScanRows queries a given database and scans
// each row in the response based on a given function.
func queryAndScanRows[T any](db *sql.DB, query string, scanFunc func(*sql.Rows) (T, error)) ([]T, error) {
	r, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query '%s': %v", query, err)
	}
	defer r.Close()

	var results []T
	for r.Next() {
		item, err := scanFunc(r)
		if err != nil {
			return nil, fmt.Errorf("scan error: %v", err)
		}
		results = append(results, item)
	}
	return results, nil
}
