package engine

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nateranda/djtools/lib"
)

// Export writes a lib.Library struct into an Engine DJ SQLite database directory (Database2/m.db) at path.
func Export(library lib.Library, path string, options ExportOptions) error {
	db2Dir := filepath.Join(path, "Database2")
	if err := os.MkdirAll(db2Dir, 0755); err != nil {
		return fmt.Errorf("error creating Database2 directory: %w", err)
	}

	mPath := filepath.Join(db2Dir, "m.db")
	hmPath := filepath.Join(db2Dir, "hm.db")

	if options.Overwrite {
		_ = os.Remove(mPath)
		_ = os.Remove(hmPath)
	}

	db, err := Open(path, false)
	if err != nil {
		return fmt.Errorf("error opening database for export: %w", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.CreateSchema(ctx); err != nil {
		return fmt.Errorf("error creating schema: %w", err)
	}

	err = db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := exportInformation(ctx, tx, library); err != nil {
			return fmt.Errorf("error exporting Information: %w", err)
		}

		if err := exportAlbumArt(ctx, tx, library); err != nil {
			return fmt.Errorf("error exporting AlbumArt: %w", err)
		}

		if err := exportTracksAndPerf(ctx, tx, library); err != nil {
			return fmt.Errorf("error exporting Tracks: %w", err)
		}

		if err := exportPlaylists(ctx, tx, library); err != nil {
			return fmt.Errorf("error exporting Playlists: %w", err)
		}

		if err := exportSmartlists(ctx, tx, library); err != nil {
			return fmt.Errorf("error exporting Smartlists: %w", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func exportInformation(ctx context.Context, db sqlExecutor, library lib.Library) error {
	major := library.SchemaVersionMajor
	if major == 0 {
		major = 2
		library.SchemaVersionMinor = 20
	}
	uuid := library.DatabaseUUID
	if uuid == "" {
		uuid = "export-generated-uuid"
	}
	query := `INSERT INTO Information (id, uuid, schemaVersionMajor, schemaVersionMinor, schemaVersionPatch, currentPlayedIndiciator, lastRekordBoxLibraryImportReadCounter)
		VALUES (1, ?, ?, ?, ?, 0, 0)`
	_, err := db.ExecContext(ctx, query, uuid, major, library.SchemaVersionMinor, library.SchemaVersionPatch)
	return err
}

func exportAlbumArt(ctx context.Context, db sqlExecutor, library lib.Library) error {
	for _, art := range library.AlbumArt {
		query := `INSERT INTO AlbumArt (id, hash, albumArt) VALUES (?, ?, ?)`
		if _, err := db.ExecContext(ctx, query, art.ID, art.Hash, art.Data); err != nil {
			return err
		}
	}
	return nil
}

func exportTracksAndPerf(ctx context.Context, db sqlExecutor, library lib.Library) error {
	for _, song := range library.Songs {
		filename := filepath.Base(song.Path)
		var bpmAnalyzed sql.NullFloat64
		if song.BpmAnalyzed > 0 {
			bpmAnalyzed = sql.NullFloat64{Float64: song.BpmAnalyzed, Valid: true}
		}

		var albumArtID sql.NullInt64
		if song.AlbumArtID > 0 {
			albumArtID = sql.NullInt64{Int64: int64(song.AlbumArtID), Valid: true}
		}

		query := `INSERT INTO Track (
			id, playOrder, length, bpm, year, path, filename, bitrate, bpmAnalyzed, albumArtId, fileBytes,
			title, artist, album, genre, comment, label, composer, remixer, key, rating, albumArt,
			timeLastPlayed, isPlayed, fileType, isAnalyzed, dateCreated, dateAdded, isAvailable,
			isMetadataOfPackedTrackChanged, isPerfomanceDataOfPackedTrackChanged, playedIndicator,
			isMetadataImported, pdbImportKey, streamingSource, uri, isBeatGridLocked, originDatabaseUuid,
			originTrackId, streamingFlags, explicitLyrics, lastEditTime, albumArtSourceHash
		) VALUES (
			?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '',
			?, ?, ?, ?, ?, ?, 1,
			0, 0, ?,
			0, 0, ?, ?, ?, ?,
			?, ?, ?, ?, ?
		)`

		var timeLastPlayed, dateCreated, dateAdded, lastEditTime *time.Time
		if song.TimeLastPlayed > 0 {
			t := time.Unix(int64(song.TimeLastPlayed), 0).UTC()
			timeLastPlayed = &t
		}
		if song.DateCreated > 0 {
			t := time.Unix(int64(song.DateCreated), 0).UTC()
			dateCreated = &t
		}
		if song.DateAdded > 0 {
			t := time.Unix(int64(song.DateAdded), 0).UTC()
			dateAdded = &t
		}
		if song.DateModified > 0 {
			t := time.Unix(int64(song.DateModified), 0).UTC()
			lastEditTime = &t
		}

		_, err := db.ExecContext(ctx, query,
			song.SongID, int(song.Length), int(song.Bpm), song.Year, song.Path, filename, song.Bitrate, bpmAnalyzed, albumArtID, song.Size,
			song.Title, song.Artist, song.Album, song.Genre, song.Comment, song.Label, song.Composer, song.Remixer, song.Key, song.Rating,
			timeLastPlayed, song.IsPlayed, song.Filetype, song.IsAnalyzed, dateCreated, dateAdded,
			song.PlayedIndicator,
			song.StreamingSource, song.URI, song.IsBeatGridLocked, song.OriginDatabaseUUID,
			song.OriginTrackID, song.StreamingFlags, song.ExplicitLyrics, lastEditTime, song.AlbumArtSourceHash,
		)
		if err != nil {
			return fmt.Errorf("error inserting Track id %d: %w", song.SongID, err)
		}

		// PerformanceData export
		sampleRate := song.SampleRate
		if sampleRate <= 0 {
			sampleRate = 44100
		}

		beatDataBlobRaw := createBeatDataBlob(sampleRate, song.Grid)
		beatDataBlobComp, err := qCompress(beatDataBlobRaw)
		if err != nil {
			return fmt.Errorf("error compressing beatData for song %d: %w", song.SongID, err)
		}

		quickCuesBlobRaw := createQuickCuesBlob(sampleRate, song.Cue, song.Cues)
		quickCuesBlobComp, err := qCompress(quickCuesBlobRaw)
		if err != nil {
			return fmt.Errorf("error compressing quickCues for song %d: %w", song.SongID, err)
		}

		loopsBlob := createLoopsBlob(sampleRate, song.Loops)

		perfQuery := `INSERT INTO PerformanceData (
			trackId, trackData, overviewWaveFormData, beatData, quickCues, loops, thirdPartySourceId, activeOnLoadLoops
		) VALUES (?, ?, ?, ?, ?, ?, 0, ?)`

		_, err = db.ExecContext(ctx, perfQuery,
			song.SongID, song.TrackData, song.OverviewWaveFormData, beatDataBlobComp, quickCuesBlobComp, loopsBlob, song.ActiveOnLoadLoops,
		)
		if err != nil {
			return fmt.Errorf("error inserting PerformanceData for track %d: %w", song.SongID, err)
		}
	}
	return nil
}

func exportPlaylists(ctx context.Context, db sqlExecutor, library lib.Library) error {
	playlistIDCounter := 1
	playlistEntityIDCounter := 1

	var processPlaylist func(pl lib.Playlist, parentID int) error
	processPlaylist = func(pl lib.Playlist, parentID int) error {
		currentID := pl.PlaylistID
		if currentID == 0 {
			currentID = playlistIDCounter
			playlistIDCounter++
		}

		query := `INSERT INTO Playlist (id, title, parentListId, isPersisted, nextListId, lastEditTime, isExplicitlyExported)
			VALUES (?, ?, ?, 1, 0, strftime('%s'), 1)`
		if _, err := db.ExecContext(ctx, query, currentID, pl.Name, parentID); err != nil {
			return err
		}

		for idx, songID := range pl.Songs {
			entityID := playlistEntityIDCounter
			playlistEntityIDCounter++
			nextEntityID := 0
			if idx < len(pl.Songs)-1 {
				nextEntityID = entityID + 1
			}

			peQuery := `INSERT INTO PlaylistEntity (id, listId, trackId, databaseUuid, nextEntityId, membershipReference)
				VALUES (?, ?, ?, ?, ?, 0)`
			if _, err := db.ExecContext(ctx, peQuery, entityID, currentID, songID, library.DatabaseUUID, nextEntityID); err != nil {
				return err
			}
		}

		for _, sub := range pl.SubPlaylists {
			if err := processPlaylist(sub, currentID); err != nil {
				return err
			}
		}
		return nil
	}

	for _, rootPl := range library.Playlists {
		if err := processPlaylist(rootPl, 0); err != nil {
			return err
		}
	}

	return nil
}

func exportSmartlists(ctx context.Context, db sqlExecutor, library lib.Library) error {
	for _, sl := range library.Smartlists {
		query := `INSERT INTO Smartlist (listUuid, title, parentPlaylistPath, nextPlaylistPath, nextListUuid, rules, lastEditTime)
			VALUES (?, ?, ?, ?, ?, ?, strftime('%s'))`
		if _, err := db.ExecContext(ctx, query, sl.ListUUID, sl.Title, sl.ParentPlaylistPath, sl.NextPlaylistPath, sl.NextListUUID, sl.Rules); err != nil {
			return err
		}
	}
	return nil
}
