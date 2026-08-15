package engine

import (
	"bytes"
	"compress/zlib"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nateranda/djtools/lib"
)

// ExportOptions options for exporting an Engine library database.
type ExportOptions struct {
	Overwrite bool
}

// Export writes a lib.Library struct into an Engine DJ SQLite database (m.db) at the given path.
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

	m, err := sql.Open("sqlite3", mPath)
	if err != nil {
		return fmt.Errorf("error opening m.db for export: %w", err)
	}
	defer m.Close()

	hm, err := sql.Open("sqlite3", hmPath)
	if err != nil {
		return fmt.Errorf("error opening hm.db for export: %w", err)
	}
	defer hm.Close()

	if err := createEngineSchema(m); err != nil {
		return fmt.Errorf("error creating m.db schema: %w", err)
	}
	if err := createHMSchema(hm); err != nil {
		return fmt.Errorf("error creating hm.db schema: %w", err)
	}

	if err := exportInformation(m, library); err != nil {
		return fmt.Errorf("error exporting Information: %w", err)
	}
	if err := exportAlbumArt(m, library); err != nil {
		return fmt.Errorf("error exporting AlbumArt: %w", err)
	}
	if err := exportTracksAndPerf(m, library); err != nil {
		return fmt.Errorf("error exporting Tracks: %w", err)
	}
	if err := exportPlaylists(m, library); err != nil {
		return fmt.Errorf("error exporting Playlists: %w", err)
	}
	if err := exportSmartlists(m, library); err != nil {
		return fmt.Errorf("error exporting Smartlists: %w", err)
	}

	return nil
}

func createEngineSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS Information (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		uuid TEXT,
		schemaVersionMajor INTEGER,
		schemaVersionMinor INTEGER,
		schemaVersionPatch INTEGER,
		currentPlayedIndiciator INTEGER,
		lastRekordBoxLibraryImportReadCounter INTEGER
	);

	CREATE TABLE IF NOT EXISTS AlbumArt (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		hash TEXT,
		albumArt BLOB
	);

	CREATE TABLE IF NOT EXISTS Track (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		playOrder INTEGER,
		length INTEGER,
		bpm INTEGER,
		year INTEGER,
		path TEXT,
		filename TEXT,
		bitrate INTEGER,
		bpmAnalyzed REAL,
		albumArtId INTEGER,
		fileBytes INTEGER,
		title TEXT,
		artist TEXT,
		album TEXT,
		genre TEXT,
		comment TEXT,
		label TEXT,
		composer TEXT,
		remixer TEXT,
		key INTEGER,
		rating INTEGER,
		albumArt TEXT,
		timeLastPlayed DATETIME,
		isPlayed BOOLEAN,
		fileType TEXT,
		isAnalyzed BOOLEAN,
		dateCreated DATETIME,
		dateAdded DATETIME,
		isAvailable BOOLEAN,
		isMetadataOfPackedTrackChanged BOOLEAN,
		isPerfomanceDataOfPackedTrackChanged BOOLEAN,
		playedIndicator INTEGER,
		isMetadataImported BOOLEAN,
		pdbImportKey INTEGER,
		streamingSource TEXT,
		uri TEXT,
		isBeatGridLocked BOOLEAN,
		originDatabaseUuid TEXT,
		originTrackId INTEGER,
		streamingFlags INTEGER,
		explicitLyrics BOOLEAN,
		lastEditTime DATETIME,
		albumArtSourceHash CHAR(40),
		CONSTRAINT C_path UNIQUE (path)
	);

	CREATE TABLE IF NOT EXISTS PerformanceData (
		trackId INTEGER PRIMARY KEY,
		trackData BLOB,
		overviewWaveFormData BLOB,
		beatData BLOB,
		quickCues BLOB,
		loops BLOB,
		thirdPartySourceId INTEGER,
		activeOnLoadLoops INTEGER,
		FOREIGN KEY(trackId) REFERENCES Track(id) ON DELETE CASCADE ON UPDATE CASCADE
	);

	CREATE TABLE IF NOT EXISTS Playlist (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		parentListId INTEGER,
		isPersisted BOOLEAN,
		nextListId INTEGER,
		lastEditTime DATETIME,
		isExplicitlyExported BOOLEAN
	);

	CREATE TABLE IF NOT EXISTS PlaylistEntity (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		listId INTEGER,
		trackId INTEGER,
		databaseUuid TEXT,
		nextEntityId INTEGER,
		membershipReference INTEGER,
		FOREIGN KEY (listId) REFERENCES Playlist (id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS Smartlist (
		listUuid TEXT NOT NULL PRIMARY KEY,
		title TEXT,
		parentPlaylistPath TEXT,
		nextPlaylistPath TEXT,
		nextListUuid TEXT,
		rules TEXT,
		lastEditTime DATETIME
	);
	`
	_, err := db.Exec(schema)
	return err
}

func createHMSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS HistorylistEntity (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trackId INTEGER,
		startTime DATETIME
	);
	`
	_, err := db.Exec(schema)
	return err
}

func exportInformation(db *sql.DB, library lib.Library) error {
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
	_, err := db.Exec(query, uuid, major, library.SchemaVersionMinor, library.SchemaVersionPatch)
	return err
}

func exportAlbumArt(db *sql.DB, library lib.Library) error {
	for _, art := range library.AlbumArt {
		query := `INSERT INTO AlbumArt (id, hash, albumArt) VALUES (?, ?, ?)`
		if _, err := db.Exec(query, art.ID, art.Hash, art.Data); err != nil {
			return err
		}
	}
	return nil
}

func exportTracksAndPerf(db *sql.DB, library lib.Library) error {
	for _, song := range library.Songs {
		filename := filepath.Base(song.Path)
		var bpmAnalyzed sql.NullFloat64
		if song.BpmAnalyzed > 0 {
			bpmAnalyzed = sql.NullFloat64{Float64: song.BpmAnalyzed, Valid: true}
		}

		var albumArtId sql.NullInt64
		if song.AlbumArtID > 0 {
			albumArtId = sql.NullInt64{Int64: int64(song.AlbumArtID), Valid: true}
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

		_, err := db.Exec(query,
			song.SongID, int(song.Length), int(song.Bpm), song.Year, song.Path, filename, song.Bitrate, bpmAnalyzed, albumArtId, song.Size,
			song.Title, song.Artist, song.Album, song.Genre, song.Comment, song.Label, song.Composer, song.Remixer, song.Key, song.Rating,
			song.TimeLastPlayed, song.IsPlayed, song.Filetype, song.IsAnalyzed, song.DateCreated, song.DateAdded,
			song.PlayedIndicator,
			song.StreamingSource, song.URI, song.IsBeatGridLocked, song.OriginDatabaseUUID,
			song.OriginTrackID, song.StreamingFlags, song.ExplicitLyrics, song.DateModified, song.AlbumArtSourceHash,
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

		_, err = db.Exec(perfQuery,
			song.SongID, song.TrackData, song.OverviewWaveFormData, beatDataBlobComp, quickCuesBlobComp, loopsBlob, song.ActiveOnLoadLoops,
		)
		if err != nil {
			return fmt.Errorf("error inserting PerformanceData for track %d: %w", song.SongID, err)
		}
	}
	return nil
}

func exportPlaylists(db *sql.DB, library lib.Library) error {
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
		if _, err := db.Exec(query, currentID, pl.Name, parentID); err != nil {
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
			if _, err := db.Exec(peQuery, entityID, currentID, songID, library.DatabaseUUID, nextEntityID); err != nil {
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

func exportSmartlists(db *sql.DB, library lib.Library) error {
	for _, sl := range library.Smartlists {
		query := `INSERT INTO Smartlist (listUuid, title, parentPlaylistPath, nextPlaylistPath, nextListUuid, rules, lastEditTime)
			VALUES (?, ?, ?, ?, ?, ?, strftime('%s'))`
		if _, err := db.Exec(query, sl.ListUUID, sl.Title, sl.ParentPlaylistPath, sl.NextPlaylistPath, sl.NextListUUID, sl.Rules); err != nil {
			return err
		}
	}
	return nil
}

func createBeatDataBlob(sampleRate float64, grid []lib.Marker) []byte {
	var buf bytes.Buffer
	var b8 [8]byte
	var b4 [4]byte

	// sampleRate
	binary.BigEndian.PutUint64(b8[:], math.Float64bits(sampleRate))
	buf.Write(b8[:])

	// skip 17 bytes (track length, set flags)
	buf.Write(make([]byte, 17))

	// default beatgrid marker count
	binary.BigEndian.PutUint64(b8[:], uint64(len(grid)))
	buf.Write(b8[:])

	for _, m := range grid {
		// offset (little endian)
		binary.LittleEndian.PutUint64(b8[:], math.Float64bits(m.StartPosition*sampleRate))
		buf.Write(b8[:])

		// beatNumber (little endian)
		binary.LittleEndian.PutUint64(b8[:], uint64(m.BeatNumber))
		buf.Write(b8[:])

		// numBeats = 1 (little endian)
		binary.LittleEndian.PutUint32(b4[:], 1)
		buf.Write(b4[:])

		// 8 bytes padding
		buf.Write(make([]byte, 8))
	}

	// adjusted beatgrid marker count
	binary.BigEndian.PutUint64(b8[:], uint64(len(grid)))
	buf.Write(b8[:])

	for _, m := range grid {
		binary.LittleEndian.PutUint64(b8[:], math.Float64bits(m.StartPosition*sampleRate))
		buf.Write(b8[:])

		binary.LittleEndian.PutUint64(b8[:], uint64(m.BeatNumber))
		buf.Write(b8[:])

		binary.LittleEndian.PutUint32(b4[:], 1)
		buf.Write(b4[:])

		buf.Write(make([]byte, 8))
	}

	return buf.Bytes()
}

func createQuickCuesBlob(sampleRate float64, mainCue float64, cues []lib.HotCue) []byte {
	var buf bytes.Buffer
	var b8 [8]byte

	// header: 8 positions
	binary.BigEndian.PutUint64(b8[:], 8)
	buf.Write(b8[:])

	cueMap := make(map[int]lib.HotCue)
	for _, c := range cues {
		cueMap[c.Position] = c
	}

	for pos := 1; pos <= 8; pos++ {
		if c, exists := cueMap[pos]; exists {
			nameBytes := []byte(c.Name)
			buf.WriteByte(byte(len(nameBytes)))
			buf.Write(nameBytes)

			// offset
			binary.BigEndian.PutUint64(b8[:], math.Float64bits(c.Offset*sampleRate))
			buf.Write(b8[:])

			// alpha channel (255)
			buf.WriteByte(255)

			// color r, g, b
			r, g, b, err := lib.HexToRgb(c.Color)
			if err != nil {
				r, g, b = 0, 255, 255
			}
			buf.WriteByte(byte(r))
			buf.WriteByte(byte(g))
			buf.WriteByte(byte(b))
		} else {
			// empty cue slot: 1 byte len=0, 12 bytes padding (total 13 bytes)
			buf.Write(make([]byte, 13))
		}
	}

	// modified cue
	binary.BigEndian.PutUint64(b8[:], math.Float64bits(mainCue*sampleRate))
	buf.Write(b8[:])

	// alpha byte
	buf.WriteByte(255)

	// original cue
	binary.BigEndian.PutUint64(b8[:], math.Float64bits(mainCue*sampleRate))
	buf.Write(b8[:])

	return buf.Bytes()
}

func createLoopsBlob(sampleRate float64, loops []lib.Loop) []byte {
	var buf bytes.Buffer
	var b8 [8]byte

	// header: 8 loop positions
	binary.BigEndian.PutUint64(b8[:], 8)
	buf.Write(b8[:])

	loopMap := make(map[int]lib.Loop)
	for _, l := range loops {
		loopMap[l.Position] = l
	}

	for pos := 1; pos <= 8; pos++ {
		if l, exists := loopMap[pos]; exists {
			nameBytes := []byte(l.Name)
			buf.WriteByte(byte(len(nameBytes)))
			buf.Write(nameBytes)

			// start position (little endian)
			binary.LittleEndian.PutUint64(b8[:], math.Float64bits(l.Start*sampleRate))
			buf.Write(b8[:])

			// end position (little endian)
			binary.LittleEndian.PutUint64(b8[:], math.Float64bits(l.End*sampleRate))
			buf.Write(b8[:])

			// 3 bytes skip/padding
			buf.Write(make([]byte, 3))

			// color r, g, b
			r, g, b, err := lib.HexToRgb(l.Color)
			if err != nil {
				r, g, b = 0, 255, 255
			}
			buf.WriteByte(byte(r))
			buf.WriteByte(byte(g))
			buf.WriteByte(byte(b))
		} else {
			// empty loop slot: 1 byte len=0, 23 bytes padding
			buf.Write(make([]byte, 24))
		}
	}

	return buf.Bytes()
}

func qCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	buf.Write(lenBuf[:])

	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
