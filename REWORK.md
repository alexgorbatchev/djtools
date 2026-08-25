---
created_on: 2026-08-25 09:00
last_modified: 2026-08-25 09:25
status: current
---

# Architecture Rework: Granular In-Place Engine DJ Database Operations

This document defines the architectural specification and implementation roadmap for transitioning `djtools` from a **whole-library batch serialization model** to a **granular, transactional in-place Engine DJ database engine**.

---

## 1. Problem Statement

In its current architecture, `djtools` only supports two primary Engine DJ operations:

1. **`engine.Import(path, options) (lib.Library, error)`**: Scans and loads every table, track, playlist entity, cue point, loop, and artwork BLOB across `m.db` and `hm.db` into a monolithic in-memory `lib.Library` struct.
2. **`engine.Export(library, path, options) error`**: Completely recreates `Database2/m.db` and `hm.db` schemas from scratch and re-inserts every record in a single batch pass.

### Limitations of the Current Model

- **No Single-Record Mutation**: Updating a single track's BPM, fixing one cue point, or adding a song to a playlist requires reading 50,000+ tracks into memory, mutating the struct, and completely rewriting the SQLite database file on disk.
- **Disk Churn & USB Wear**: Rewriting multi-gigabyte SQLite databases and binary cache files on external DJ USB flash drives causes excessive I/O latency, wears flash cells, and risks corrupting collection databases during live sets.
- **Risk of Dropping Unknown Schema Fields**: Any future Engine DJ columns, tables, or device-specific sync markers not explicitly modeled in `lib.Library` are silently dropped during a full re-export.
- **CGO Dependency**: The package currently imports `github.com/mattn/go-sqlite3`, requiring CGO and local C toolchains, which prevents effortless static cross-compilation across macOS, Linux, and Windows.

---

## 2. Target Design Goals

1. **Pure Go Zero-CGO SQLite**: Migrate from `github.com/mattn/go-sqlite3` to modern `modernc.org/sqlite`.
2. **Granular In-Place CRUD Engine (`*engine.DB`)**: Provide an open database handle with targeted, low-latency SQL operations for individual tracks, playlists, memberships, album art, and cue points.
3. **Transaction & Concurrency Safety**: Wrap multi-step mutations (such as linked-list entity relinking and track removals) in atomic SQLite transactions.
4. **Preserve High-Level Import/Export**: Keep `engine.Import()` and `engine.Export()` as high-level convenience bridges for cross-format conversions (Rekordbox / Serato), re-implementing them on top of the granular engine.
5. **Lossless Pass-Through**: Targeted updates only modify the specific columns and rows requested; all other Engine DJ tables (PerformanceData, Smartlist, SoundSwitch, History) remain 100% untouched.

---

## 3. Proposed Package Architecture

```
djtools/
├── engine/
│   ├── db.go                # *DB handle, Open(), Close(), transactions, schema helpers
│   ├── track.go             # Track CRUD, tag synchronization, path relocation
│   ├── playlist.go          # Playlist/folder hierarchy CRUD, tree queries
│   ├── entity.go            # PlaylistEntity linked-list insertion, removal, reordering
│   ├── perf.go              # PerformanceData (beatgrid, hot cues, loops) zlib encoders/decoders
│   ├── artwork.go           # AlbumArt BLOB and SHA-1 hash management
│   ├── import.go            # High-level engine.Import() bridge
│   ├── export.go            # High-level engine.Export() bridge
│   └── models.go            # Engine DJ SQLite record types
```

---

## 4. API Specification

### 4.1 Database Lifecycle (`*engine.DB`)

```go
package engine

import (
	"context"
	"database/sql"
)

type DB struct {
	mDB  *sql.DB // Database2/m.db (master collection & performance data)
	hmDB *sql.DB // Database2/hm.db (history & play counts, optional)
	path string  // Base library directory path
}

// Open opens an existing Engine DJ library database directory in read-write or read-only mode.
func Open(libPath string, readonly ...bool) (*DB, error)

// Close closes the underlying SQLite database connections.
func (db *DB) Close() error

// Ping validates connectivity to m.db and hm.db.
func (db *DB) Ping(ctx context.Context) error
```

---

### 4.2 Track Operations

```go
// GetTrackByID retrieves a single track record by primary key.
func (db *DB) GetTrackByID(ctx context.Context, id int64) (*Track, error)

// FindTrack finds a track by numeric ID, exact path, filename, or title.
func (db *DB) FindTrack(ctx context.Context, identifier string) (*Track, error)

// GetAllTracks returns an iterator or slice of all tracks in the collection.
func (db *DB) GetAllTracks(ctx context.Context) ([]Track, error)

// InsertTrack inserts a new Track record and returns the generated ID.
func (db *DB) InsertTrack(ctx context.Context, t *Track) (int64, error)

// UpdateTrack updates track metadata (title, artist, album, BPM, fileBytes, rating, etc.).
func (db *DB) UpdateTrack(ctx context.Context, t *Track) error

// DeleteTrack deletes a track and cascades removal of its PlaylistEntity associations.
func (db *DB) DeleteTrack(ctx context.Context, id int64) error

// RelocateTracks batch-replaces path prefixes (e.g. migrating drive mount points).
func (db *DB) RelocateTracks(ctx context.Context, oldPrefix, newPrefix string) (int, error)
```

---

### 4.3 Playlist & Folder Operations

```go
// GetPlaylistHierarchy constructs the full folder and playlist tree.
func (db *DB) GetPlaylistHierarchy(ctx context.Context) ([]PlaylistNode, error)

// CreatePlaylist creates a new playlist or folder.
func (db *DB) CreatePlaylist(ctx context.Context, title string, parentID int64, isFolder bool) (*Playlist, error)

// DeletePlaylist deletes a playlist and its PlaylistEntity associations (with optional cascade).
func (db *DB) DeletePlaylist(ctx context.Context, id int64, force bool) error

// MovePlaylist reparents a playlist or folder in the tree, checking against circular loops.
func (db *DB) MovePlaylist(ctx context.Context, id int64, newParentID int64) error
```

---

### 4.4 Playlist Membership & Linked-List Entity Operations

Engine DJ stores playlist track order as an explicit linked-list in `PlaylistEntity` using `nextEntityId`:

```go
// GetPlaylistTracks returns all tracks in a playlist ordered by linked-list sequence.
func (db *DB) GetPlaylistTracks(ctx context.Context, listID int64) ([]Track, error)

// AddTrackToPlaylist appends a track to a playlist and updates the previous tail's nextEntityId.
func (db *DB) AddTrackToPlaylist(ctx context.Context, listID int64, trackID int64) (int64, error)

// RemoveTrackFromPlaylist deletes an entity link and connects the preceding entity to nextEntityId.
func (db *DB) RemoveTrackFromPlaylist(ctx context.Context, listID int64, trackID int64) error

// MoveTrackBetweenPlaylists transfers track membership from one playlist to another atomically.
func (db *DB) MoveTrackBetweenPlaylists(ctx context.Context, srcListID, dstListID, trackID int64) error
```

---

### 4.5 Performance Data (Cue Points, Loops, Beat Grids)

```go
type PerformanceData struct {
	TrackID  int64
	BeatGrid []lib.Marker
	HotCues  []lib.HotCue
	MainCue  float64
	Loops    []lib.Loop
}

// GetPerformanceData extracts and decompresses the zlib binary blobs for a track.
func (db *DB) GetPerformanceData(ctx context.Context, trackID int64) (*PerformanceData, error)

// UpdatePerformanceData encodes and writes binary blobs into PerformanceData table.
func (db *DB) UpdatePerformanceData(ctx context.Context, data *PerformanceData) error
```

---

### 4.6 Album Artwork Management

```go
// GetAlbumArt retrieves an artwork record by ID.
func (db *DB) GetAlbumArt(ctx context.Context, id int64) (*AlbumArtRecord, error)

// InsertOrUpdateAlbumArt inserts or updates an image BLOB by its SHA-1 hash.
func (db *DB) InsertOrUpdateAlbumArt(ctx context.Context, hash string, blob []byte) (int64, error)

// DeleteAlbumArt removes an unreferenced album art record.
func (db *DB) DeleteAlbumArt(ctx context.Context, id int64) error
```

---

## 5. Implementation Roadmap

| Phase | Description | Deliverables | Status |
| :--- | :--- | :--- | :---: |
| **Phase 1: Driver Migration** | Replace `mattn/go-sqlite3` with `modernc.org/sqlite` in `go.mod`. | Zero-CGO builds, clean CI matrix without C compiler requirements. | ✅ Completed |
| **Phase 2: Core `*engine.DB` Handle** | Create `engine.Open()`, connection management, and table models. | `db.go`, `models.go`, basic connectivity unit tests. | Planned |
| **Phase 3: Granular CRUD Layer** | Implement Track, Playlist, Entity (linked list), and AlbumArt CRUD. | `track.go`, `playlist.go`, `entity.go`, `artwork.go`. | Planned |
| **Phase 4: Binary Performance Blobs** | Port zlib cue point, loop, and beatgrid encode/decode into `perf.go`. | Granular cue point read/write methods. | Planned |
| **Phase 5: Refactor Import & Export** | Re-implement `engine.Import()` and `engine.Export()` over `*engine.DB`. | Full backwards compatibility with existing conversion tests. | Planned |
| **Phase 6: Verification & Test Suite** | Build table-driven unit tests for all CRUD operations using `t.TempDir()`. | >= 90% statement code coverage across all packages. | Planned |

---

## 6. Migration Guidelines for Consumers

Existing code using `djtools`:
```go
// LEGACY (Whole-library batch dump)
library, _ := engine.Import(libPath, opts)
library.Songs[0].Title = "New Title"
engine.Export(library, libPath, exportOpts)
```

Upgraded code using granular operations:
```go
// MODERN (Granular in-place mutation)
db, err := engine.Open(libPath)
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// Update single track instantly
err = db.UpdateTrack(ctx, &engine.Track{
    ID:    42,
    Title: "New Title",
})

// Add song to playlist with atomic linked-list ordering
_, err = db.AddTrackToPlaylist(ctx, playlistID, 42)
```
