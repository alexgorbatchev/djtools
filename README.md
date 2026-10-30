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
- **Zero CGO**: Pure Go SQLite powered by `modernc.org/sqlite` — compiles and runs across macOS, Linux, and Windows without GCC or local C toolchains.

---

## Installation

```bash
go get github.com/alexgorbatchev/djtools
```

---

## Usage

### 1. Granular In-Place Engine DJ Operations (Recommended)

`*engine.DB` allows direct, low-latency queries and mutations on Engine DJ collections without batch re-serialization:

```go
package main

import (
	"context"
	"log"

	"github.com/alexgorbatchev/djtools/engine"
)

func main() {
	ctx := context.Background()

	// Open Engine DJ database directory
	db, err := engine.Open("/path/to/Engine Library")
	if err != nil {
		log.Fatalf("Error opening Engine database: %v", err)
	}
	defer db.Close()

	// Find and update a single track
	track, err := db.FindTrack(ctx, "Strobe")
	if err != nil {
		log.Fatalf("Track not found: %v", err)
	}
	track.Rating = 100
	if err := db.UpdateTrack(ctx, track); err != nil {
		log.Fatalf("Error updating track: %v", err)
	}

	// Add track to playlist with linked-list ordering
	playlist, err := db.CreatePlaylist(ctx, "Favorites", 0, false)
	if err != nil {
		log.Fatalf("Error creating playlist: %v", err)
	}
	if _, err := db.AddTrackToPlaylist(ctx, playlist.ID, track.ID); err != nil {
		log.Fatalf("Error adding track to playlist: %v", err)
	}
}
```

### 2. Whole-Library Batch Import & Export

Below illustrates batch usage of `djtools` for cross-format migration. This example imports an Engine DJ library, filters playlists, and exports the collection to both Rekordbox XML and Engine DJ SQLite database format.

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
	exportOpts := rbxml.ExportOptions{UseUTC: true}
	if err := rbxml.Export(&library, "/path/to/rekordbox.xml", exportOpts); err != nil {
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

### 3. Native Engine DJ Beat Grids

`engine.DecodeBeatData` and `engine.EncodeBeatData` read and write `PerformanceData.beatData` without converting it to `lib.Marker` values. `BeatData` keeps the sample rate, sample-length limit, the default grid Engine restores on reset, the adjusted grid it plays and edits, and the trailing source and reference-sample fields. Each `BeatAnchor` stores a sample position, a zero-based beat index, and the beat count until the next anchor; the final anchor counts no beats. Encoding validates both grids first.

```go
// Analyzed grids start one bar before the first downbeat and end past the track.
anchors := []engine.BeatAnchor{
	{Sample: -88200, Beat: -4, BeatsUntilNext: 520},
	{Sample: 11377800, Beat: 516},
}
data, err := engine.NewBeatData(44100, 11500000, anchors)
if err != nil {
	log.Fatal(err)
}
blob, err := engine.EncodeBeatData(data)
if err != nil {
	log.Fatal(err)
}
decoded, err := engine.DecodeBeatData(blob)
if err != nil {
	log.Fatal(err)
}
log.Printf("%.2f BPM", engine.BeatGridBPM(decoded.SampleRate, decoded.Adjusted))
```

### 4. Native Engine DJ Hot Cues

`engine.SetSavedHotCue(beat, quickCues, cue)` returns a Qt-compressed `quickCues` blob with one slot replaced. `lib.HotCue.Position` selects slot 1–8; `Offset` is fractional source seconds, `Name` is valid UTF-8 up to 255 bytes, and `Color` is `#RRGGBB`. The beat blob supplies the native sample rate. Other slots, existing cue alpha, and the original/modified main-cue trailer retain their exact payload bytes. An empty slot receives opaque color.

`engine.ClearSavedHotCue(quickCues, slot)` clears one slot and preserves the other records and main cue. Clearing an absent blob leaves it absent. Both functions reject malformed existing data and leave their inputs unchanged; they do not open or write databases.

For a track without saved beat data, `engine.EncodeUnsetBeatData(sampleRate, sampleLimit)` encodes a validated native sample header with no beat anchors and no analyzed-grid flag. Supply the actual audio sample rate and sample-length limit. Persist only the returned cue blob and any necessary initial beat blob; these functions do not replace other performance columns or loop data.

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
