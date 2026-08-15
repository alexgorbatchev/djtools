# djtools

`djtools` is a Go library for manipulating, converting, and exporting DJ software libraries. It provides fast, simple data structures for converting playlists, tracks, cue points, loops, beat grids, smartlists, and artwork across DJ formats.

This repository is a maintained fork of [`nateranda/djtools`](https://github.com/nateranda/djtools), hosted at [`github.com/alexgorbatchev/djtools`](https://github.com/alexgorbatchev/djtools).

---

## Features

### Supported Formats & Platforms

| Platform | Import | Export |
| :--- | :---: | :---: |
| **Engine DJ** (`m.db` / `p.db` / `hm.db`) | ✅ | ✅ |
| **Rekordbox XML** | ✅ | ✅ |
| **Serato** | ✅ | ⌛ Planned |

### Conversions & Metadata
`djtools` converts and preserves:
- Tracks & metadata (title, artist, album, genre, BPM, key, rating, play counts)
- Engine DJ analyzed floating-point BPMs and cross-drive tracking UUIDs (`originDatabaseUuid`, `originTrackId`)
- Album artwork hashes and BLOBs (`AlbumArt` table)
- Playlists and folder hierarchies
- Smartlists and rule configurations
- Hot cues and cue colors
- Saved loops and loop colors
- Beat grids and grid markers
- Waveform cache data (`overviewWaveFormData`, `trackData`)

---

## Installation

```bash
go get github.com/alexgorbatchev/djtools
```

---

## Usage

Below illustrates basic usage of `djtools`. This example imports an Engine DJ library, filters playlists, and exports the collection to both Rekordbox XML and Engine DJ SQLite database format.

```go
package main

import (
	"log"

	"github.com/alexgorbatchev/djtools/engine"
	"github.com/alexgorbatchev/djtools/rbxml"
)

func main() {
	// Import an Engine DJ library
	importOptions := engine.ImportOptions{
		PreserveOriginalPaths: true,
		ImportOriginalCues:    true,
		ImportOriginalGrids:   true,
	}

	library, err := engine.Import("/path/to/Engine Library", importOptions)
	if err != nil {
		log.Fatalf("Error importing Engine library: %v", err)
	}

	// Modify playlists or track metadata in Go
	if len(library.Playlists) > 1 {
		library.Playlists = library.Playlists[1:]
	}

	// Export to a Rekordbox XML file
	if err := rbxml.Export(&library, "/path/to/rekordbox.xml"); err != nil {
		log.Fatalf("Error exporting Rekordbox XML: %v", err)
	}

	// Export to Engine DJ SQLite database format
	exportOptions := engine.ExportOptions{
		Overwrite: true,
	}
	if err := engine.Export(library, "/path/to/Output Engine Library", exportOptions); err != nil {
		log.Fatalf("Error exporting Engine DJ database: %v", err)
	}
}
```

---

## Testing

Run all unit tests across packages:

```bash
go test ./...
```

Run tests with coverage:

```bash
go test -cover ./engine
```

---

## License

`djtools` is licensed under the MIT License. See [LICENSE](LICENSE) for details.
