package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB encapsulates the connection handles to an Engine DJ database directory.
type DB struct {
	mDB      *sql.DB
	hmDB     *sql.DB
	path     string
	readonly bool
}

// Open opens an existing Engine DJ library database directory in read-write or read-only mode.
func Open(libPath string, readonly ...bool) (*DB, error) {
	isReadOnly := len(readonly) > 0 && readonly[0]

	// Determine directory containing m.db
	db2Dir := filepath.Join(libPath, "Database2")
	mPath := filepath.Join(db2Dir, "m.db")
	hmPath := filepath.Join(db2Dir, "hm.db")

	if _, err := os.Stat(mPath); os.IsNotExist(err) {
		// Fallback: check if libPath directly contains m.db
		altMPath := filepath.Join(libPath, "m.db")
		if _, altErr := os.Stat(altMPath); altErr == nil {
			db2Dir = libPath
			mPath = altMPath
			hmPath = filepath.Join(libPath, "hm.db")
		} else if !isReadOnly {
			// If creating in read-write mode, ensure Database2 exists
			if err := os.MkdirAll(db2Dir, 0755); err != nil {
				return nil, fmt.Errorf("creating database directory: %w", err)
			}
		}
	}

	mDSN := mPath
	hmDSN := hmPath
	if isReadOnly {
		mDSN = fmt.Sprintf("file:%s?mode=ro", url.PathEscape(mPath))
		hmDSN = fmt.Sprintf("file:%s?mode=ro", url.PathEscape(hmPath))
	}

	m, err := sql.Open("sqlite", mDSN)
	if err != nil {
		return nil, fmt.Errorf("error opening m.db: %w", err)
	}

	if err := m.Ping(); err != nil {
		_ = m.Close()
		return nil, fmt.Errorf("error initializing m.db: %w", err)
	}

	var hm *sql.DB
	// Check if hm.db exists or we are creating new in RW mode
	if _, err := os.Stat(hmPath); err == nil || !isReadOnly {
		h, err := sql.Open("sqlite", hmDSN)
		if err == nil {
			if err := h.Ping(); err == nil {
				hm = h
			} else {
				_ = h.Close()
			}
		}
	}

	engineDB := &DB{
		mDB:      m,
		hmDB:     hm,
		path:     libPath,
		readonly: isReadOnly,
	}

	return engineDB, nil
}

// Close closes the underlying SQLite database connections.
func (db *DB) Close() error {
	var errs []error
	if db.mDB != nil {
		if err := db.mDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing m.db: %w", err))
		}
		db.mDB = nil
	}
	if db.hmDB != nil {
		if err := db.hmDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing hm.db: %w", err))
		}
		db.hmDB = nil
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Ping validates connectivity to m.db and hm.db.
func (db *DB) Ping(ctx context.Context) error {
	if db.mDB == nil {
		return errors.New("m.db connection is not open")
	}
	if err := db.mDB.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging m.db: %w", err)
	}
	if db.hmDB != nil {
		if err := db.hmDB.PingContext(ctx); err != nil {
			return fmt.Errorf("pinging hm.db: %w", err)
		}
	}
	return nil
}

// Path returns the base library path.
func (db *DB) Path() string {
	return db.path
}

// IsReadOnly returns true if the database was opened in read-only mode.
func (db *DB) IsReadOnly() bool {
	return db.readonly
}

// MDB returns the underlying *sql.DB for m.db.
func (db *DB) MDB() *sql.DB {
	return db.mDB
}

// HMDB returns the underlying *sql.DB for hm.db, which may be nil.
func (db *DB) HMDB() *sql.DB {
	return db.hmDB
}

// WithTx executes the given function inside a transaction on m.db.
func (db *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if db.readonly {
		return errors.New("cannot begin write transaction on read-only database")
	}
	tx, err := db.mDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// CreateSchema initializes the standard Engine DJ tables in m.db and hm.db.
func (db *DB) CreateSchema(ctx context.Context) error {
	if db.readonly {
		return errors.New("cannot create schema on read-only database")
	}
	if err := createEngineSchema(db.mDB); err != nil {
		return fmt.Errorf("creating m.db schema: %w", err)
	}
	if db.hmDB != nil {
		if err := createHMSchema(db.hmDB); err != nil {
			return fmt.Errorf("creating hm.db schema: %w", err)
		}
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
		var dfltValue any
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err == nil {
			cols[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading table info for %s: %w", tableName, err)
	}
	return cols, nil
}
