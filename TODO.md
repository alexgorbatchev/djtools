---
created_on: 2026-08-25 09:30
last_modified: 2026-08-25 10:15
status: current
---

# `djtools` Full Architecture Rework & Granular Operations - TODO

This checklist tracks the implementation of granular in-place operations, CRUD APIs, package refactoring, and comprehensive test coverage across the entire `djtools` repository (`engine`, `rbxml`, `serato`, `lib`, and `engine-to-rekordbox`).

---

## Phase 1: Core Engine DJ Granular Database (`engine/`)

### 1.1 Data Models & Types (`engine/models.go`)
- [x] Define `Track` struct with complete Engine DJ SQLite schema fields and helper conversion methods
- [x] Define `Playlist` and `PlaylistNode` structs with linked-list hierarchy support
- [x] Define `PlaylistEntity` struct for track membership ordering
- [x] Define `PerformanceData` struct for beat grids, hot cues, loops, and waveforms
- [x] Define `AlbumArtRecord` struct for BLOBs and SHA-1 hashes
- [x] Define `SmartlistRecord` and `InformationRecord` structs
- [x] Retain `ImportOptions`, `ExportOptions`, and `nullTime` driver Scanner/Valuer

### 1.2 Database Lifecycle & Connection Management (`engine/db.go`)
- [x] Implement `DB` struct holding `mDB (*sql.DB)`, `hmDB (*sql.DB)`, and base path
- [x] Implement `Open(libPath string, readonly ...bool) (*DB, error)` with read-only/read-write modes
- [x] Implement `(db *DB) Close() error` with safe closing of both handles
- [x] Implement `(db *DB) Ping(ctx context.Context) error`
- [x] Implement `(db *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error` for atomic transactions
- [x] Implement schema bootstrap helpers (`createEngineSchema`, `createHMSchema`)
- [x] Implement table column reflection helpers (`getTableColumns`)
- [x] Add unit tests in `engine/db_test.go`

### 1.3 Track Operations (`engine/track.go`)
- [x] Implement `(db *DB) GetTrackByID(ctx context.Context, id int64) (*Track, error)`
- [x] Implement `(db *DB) FindTrack(ctx context.Context, query string) (*Track, error)` (by ID, exact path, filename, title)
- [x] Implement `(db *DB) GetAllTracks(ctx context.Context) ([]Track, error)`
- [x] Implement `(db *DB) InsertTrack(ctx context.Context, t *Track) (int64, error)`
- [x] Implement `(db *DB) UpdateTrack(ctx context.Context, t *Track) error`
- [x] Implement `(db *DB) DeleteTrack(ctx context.Context, id int64) error` (with cascade entity & perf cleanup)
- [x] Implement `(db *DB) RelocateTracks(ctx context.Context, oldPrefix, newPrefix string) (int, error)`
- [x] Add unit tests in `engine/track_test.go`

### 1.4 Playlist & Folder Tree Operations (`engine/playlist.go`)
- [x] Implement `(db *DB) GetPlaylistByID(ctx context.Context, id int64) (*Playlist, error)`
- [x] Implement `(db *DB) GetAllPlaylists(ctx context.Context) ([]Playlist, error)`
- [x] Implement `(db *DB) GetPlaylistHierarchy(ctx context.Context) ([]PlaylistNode, error)` (ordered tree)
- [x] Implement `(db *DB) CreatePlaylist(ctx context.Context, title string, parentID int64, isFolder bool) (*Playlist, error)`
- [x] Implement `(db *DB) DeletePlaylist(ctx context.Context, id int64, force bool) error` (with sibling relinking & cascade)
- [x] Implement `(db *DB) MovePlaylist(ctx context.Context, id int64, newParentID int64) error` (with cycle prevention)
- [x] Add unit tests in `engine/playlist_test.go`

### 1.5 Playlist Membership & Linked-List Entities (`engine/entity.go`)
- [x] Implement `(db *DB) GetPlaylistEntities(ctx context.Context, listID int64) ([]PlaylistEntity, error)`
- [x] Implement `(db *DB) GetPlaylistTracks(ctx context.Context, listID int64) ([]Track, error)` (in linked-list order)
- [x] Implement `(db *DB) AddTrackToPlaylist(ctx context.Context, listID int64, trackID int64) (int64, error)`
- [x] Implement `(db *DB) RemoveTrackFromPlaylist(ctx context.Context, listID int64, trackID int64) error`
- [x] Implement `(db *DB) MoveTrackBetweenPlaylists(ctx context.Context, srcListID, dstListID, trackID int64) error`
- [x] Add unit tests in `engine/entity_test.go`

### 1.6 Album Artwork Management (`engine/artwork.go`)
- [x] Implement `(db *DB) GetAlbumArt(ctx context.Context, id int64) (*AlbumArtRecord, error)`
- [x] Implement `(db *DB) GetAlbumArtByHash(ctx context.Context, hash string) (*AlbumArtRecord, error)`
- [x] Implement `(db *DB) GetAllAlbumArt(ctx context.Context) ([]AlbumArtRecord, error)`
- [x] Implement `(db *DB) InsertOrUpdateAlbumArt(ctx context.Context, hash string, blob []byte) (int64, error)`
- [x] Implement `(db *DB) DeleteAlbumArt(ctx context.Context, id int64) error`
- [x] Add unit tests in `engine/artwork_test.go`

### 1.7 Performance Data, Beat Grids & Waveforms (`engine/perf.go`)
- [x] Implement `qCompress` and `qUncompress` zlib helpers
- [x] Implement `(db *DB) GetPerformanceData(ctx context.Context, trackID int64) (*PerformanceData, error)`
- [x] Implement `(db *DB) UpdatePerformanceData(ctx context.Context, data *PerformanceData) error`
- [x] Implement beat data blob serialization / deserialization
- [x] Implement quick cues blob serialization / deserialization
- [x] Implement loops blob serialization / deserialization
- [x] Add unit tests in `engine/perf_test.go`

### 1.8 High-Level Import & Export Bridge (`engine/import.go` & `engine/export.go`)
- [x] Implement `Import(path string, importOptions ImportOptions) (lib.Library, error)` using `*engine.DB`
- [x] Implement `Export(library lib.Library, path string, options ExportOptions) error` using `*engine.DB`
- [x] Clean up redundant monolithic files (`importExtract.go`, `importConvert.go`, `engine.go`)
- [x] Verify existing test suite passes: `engine_test.go`, `export_test.go`, `null_time_test.go`

---

## Phase 2: Core Library Canonical Models (`lib/`)
- [x] Implement `lib/library_test.go` to test all `lib` methods:
  - [x] `Library.Save` and `Library.Load` JSON serialization
  - [x] `Library.SortSongs` song ordering and cue/loop sorting
  - [x] `Library.CheckCorruptedSongs` and `removeSongFromPlaylists`
  - [x] `HexToRgb` and `RgbToHex` color conversions
- [x] Add helper methods to `lib.Library`:
  - [x] `FindSongByID(id int) *Song`
  - [x] `FindSongByPath(path string) *Song`
  - [x] `FindPlaylistByID(id int) *Playlist`
- [x] Reach >= 90% statement coverage for `lib/` (94.1%)

---

## Phase 3: Rekordbox XML Granular Operations & Inspection (`rbxml/`)
- [x] Implement granular document inspection and mutation methods on `djPlaylists` / `Document`:
  - [x] `FindTrackByID(id int) *track`
  - [x] `FindTrackByLocation(location string) *track`
  - [x] `AddTrack(t track) int`
  - [x] `FindNode(name string) *node`
- [x] Expand `rbxml/rbxml_test.go` with table-driven tests for edge cases (corrupt files, invalid XML, UTC vs Local dates)
- [x] Reach >= 90% statement coverage for `rbxml/` (91.8%)

---

## Phase 4: Serato Library & Crate Extraction (`serato/`)
- [x] Complete `serato.Import(path string) (lib.Library, error)` converting crates & songs to `lib.Library`
- [x] Implement GEOB parser for Serato cues, loops, and track metadata (markers, beatgrids)
- [x] Add `serato_test.go` with mock crate files / test fixtures
- [x] Reach >= 90% statement coverage for `serato/` (90.6%)

---

## Phase 5: Verification, Integration & Documentation
- [x] Verify `engine-to-rekordbox` sync tool builds and passes tests cleanly
- [x] Run full test suite with race detector: `go test -v -race ./...`
- [x] Verify statement coverage is >= 90% across every package
- [x] Run `go vet ./...` and `go fmt ./...`
- [x] Update `AGENTS.md` and `README.md` to reflect granular database APIs and cross-package capabilities
- [x] Provide completion score and Due Diligence report
