---
created_on: 2026-06-04 12:00
last_modified: 2026-06-04 12:30
status: current
ticket_status: closed
---

# Ticket: Engine DJ Database Export Support

## Problem
`djtools` has no mechanism to export or write a `lib.Library` struct into an Engine DJ SQLite database (`m.db` / `p.db`). Users can only import from Engine DJ, making `djtools` a read-only tool for Engine DJ users.

## Why this matters
Writing and generating Engine DJ databases is essential for converting collections from other software (e.g. Rekordbox XML, Serato) into Engine DJ format, and for writing modified Engine DJ databases back to disk.

## Observed context
- `engine/`: Contains `importExtract.go` and `importConvert.go`, but no `exportConvert.go` or `exportGenerate.go`.
- `engine/engine.go`: Lacks an `Export(library lib.Library, path string, exportOptions ExportOptions) error` function.

## Acceptance criteria
- [x] Implement `engine.Export(library lib.Library, path string, exportOptions ExportOptions) error`.
- [x] Create `m.db` database schema (including `Information`, `Track`, `Playlist`, `PlaylistEntity`, `PerformanceData`, `Smartlist`, `AlbumArt`, and required triggers/indexes) when exporting.
- [x] Generate binary performance blobs (`beatData`, `quickCues`, `loops`, `trackData`, `overviewWaveFormData`) using QT C++ qCompress/zlib compression where required.
- [x] Add round-trip unit tests: Export a `lib.Library` to Engine DJ format -> Import it back -> Verify equality of songs, playlists, cues, loops, and grids.
- [x] Mandatory Review Pass: Run a code review pass to verify implementation correctness and error wrapping.
