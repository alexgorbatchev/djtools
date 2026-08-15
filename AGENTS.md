# AGENTS.md

`djtools` is a Go library to manipulate, convert, and manage DJ software libraries (Engine DJ, Rekordbox XML, Serato, etc.).

## Core Commands

```bash
# Run all tests
go test ./...

# Run tests with race detection and verbose output
go test -v -race ./...

# Run tests for a specific package
go test -v ./engine

# Format code
go fmt ./...

# Run go vet
go vet ./...
```

## Architecture & Package Topology

- `engine/`: Engine DJ SQLite database (`m.db`, `p.db`, `hm.db`) import, extraction, conversion, and export logic.
- `rbxml/`: Rekordbox XML import and export logic.
- `serato/`: Serato library import/extraction logic.
- `lib/`: Canonical core data models (`Library`, `Song`, `Playlist`, `HotCue`, `Loop`, `Marker`, `Smartlist`, `AlbumArt`) shared across all formats.

## Key Rules & Conventions

- **Red/Green Development:** All new features or fixes must be accompanied by unit tests.
- **Go Idioms:** Follow standard Go rules (`fmt.Errorf` with `%w`, table-driven tests, concise parameter names, no `panic`s on runtime errors).
- **Error Handling:** Always wrap database errors with context describing what operation was being attempted.
- **Zero Values:** Ensure struct zero values are valid and safe.
- **No Binaries in Git:** Never commit built binaries or compiled test databases. Use test SQL fixtures or temporary SQLite files via `t.TempDir()`.

## Boundaries

- **Always:** Verify `go test ./...` and `go vet ./...` pass before committing code.
- **Ask First:** Before modifying existing `lib.Library` public API fields if it breaks backwards compatibility with callers.
- **Never:** Hardcode absolute file paths in tests or code; use `t.TempDir()` or relative fixture paths.
