package engine_test

import (
	"path/filepath"
	"testing"

	"github.com/nateranda/djtools/engine"
	"github.com/nateranda/djtools/lib"
	"github.com/stretchr/testify/assert"
)

func TestExportAndRoundTripImport(t *testing.T) {
	tempDir := t.TempDir()

	originalLib := lib.Library{
		DatabaseUUID:       "test-uuid-12345",
		SchemaVersionMajor:  2,
		SchemaVersionMinor:  20,
		SchemaVersionPatch:  0,
		AlbumArt: []lib.AlbumArt{
			{
				ID:   1,
				Hash: "hash123",
				Data: []byte("fake-image-bytes"),
			},
		},
		Songs: []lib.Song{
			{
				SongID:               1,
				Title:                "Export Track 1",
				Artist:               "Test Artist",
				Album:                "Test Album",
				Genre:                "House",
				Filetype:             "mp3",
				Size:                 102400,
				Length:               300,
				Year:                 2024,
				Bpm:                  128,
				BpmAnalyzed:          128.05,
				DateAdded:            1700000000,
				DateModified:         1700000050,
				Bitrate:              320,
				SampleRate:           44100,
				Comment:              "Test Comment",
				Rating:               80,
				Path:                 "music/track1.mp3",
				Key:                  5,
				Label:                "Test Label",
				Cue:                  1.5,
				IsAnalyzed:           true,
				IsBeatGridLocked:     true,
				ExplicitLyrics:       false,
				OriginDatabaseUUID:   "test-uuid-12345",
				OriginTrackID:        1,
				AlbumArtID:           1,
				OverviewWaveFormData: []byte("overview-waveform"),
				TrackData:            []byte("detailed-trackdata"),
				ActiveOnLoadLoops:    1,
				Grid: []lib.Marker{
					{
						StartPosition: 0.12,
						Bpm:           128.0,
						BeatNumber:    0,
					},
				},
				Cues: []lib.HotCue{
					{
						Name:     "Cue 1",
						Offset:   10.5,
						Position: 1,
						Color:    "#00FFFF",
					},
				},
				Loops: []lib.Loop{
					{
						Name:     "Loop 1",
						Start:    30.0,
						End:      45.0,
						Position: 1,
						Color:    "#FF0000",
					},
				},
			},
			{
				SongID:     2,
				Title:      "Export Track 2",
				Artist:     "Second Artist",
				Album:      "Second Album",
				Genre:      "Techno",
				Filetype:   "wav",
				Size:       204800,
				Length:     240,
				Year:       2025,
				Bpm:        135,
				DateAdded:  1700001000,
				Bitrate:    1411,
				SampleRate: 44100,
				Path:       "music/track2.wav",
				Key:        12,
				Label:      "Techno Label",
				IsAnalyzed: true,
				Grid: []lib.Marker{
					{
						StartPosition: 0.05,
						Bpm:           135.0,
						BeatNumber:    0,
					},
				},
			},
		},
		Playlists: []lib.Playlist{
			{
				PlaylistID: 1,
				Name:       "Main Playlist",
				Songs:      []int{1, 2},
				SubPlaylists: []lib.Playlist{
					{
						PlaylistID: 2,
						Name:       "Sub Playlist",
						Songs:      []int{1},
					},
				},
			},
		},
		Smartlists: []lib.Smartlist{
			{
				ListUUID: "smart-1",
				Title:    "Top Rated",
				Rules:    `{"rules": "rating > 80"}`,
			},
		},
	}

	err := engine.Export(originalLib, tempDir, engine.ExportOptions{Overwrite: true})
	assert.NoError(t, err, "Export should succeed without errors")

	importedLib, err := engine.Import(tempDir, engine.ImportOptions{
		PreserveOriginalPaths: true,
		ImportOriginalCues:    true,
		ImportOriginalGrids:   true,
	})
	assert.NoError(t, err, "Import of exported DB should succeed without errors")

	assert.Equal(t, originalLib.DatabaseUUID, importedLib.DatabaseUUID)
	assert.Equal(t, originalLib.SchemaVersionMajor, importedLib.SchemaVersionMajor)
	assert.Equal(t, len(originalLib.Songs), len(importedLib.Songs))
	assert.Equal(t, len(originalLib.Playlists), len(importedLib.Playlists))
	assert.Equal(t, len(originalLib.Smartlists), len(importedLib.Smartlists))
	assert.Equal(t, len(originalLib.AlbumArt), len(importedLib.AlbumArt))

	// Verify track details
	song1 := importedLib.Songs[0]
	assert.Equal(t, "Export Track 1", song1.Title)
	assert.Equal(t, "Test Artist", song1.Artist)
	assert.Equal(t, float32(128.0), song1.Bpm)
	assert.Equal(t, 128.05, song1.BpmAnalyzed)
	assert.Equal(t, true, song1.IsAnalyzed)
	assert.Equal(t, true, song1.IsBeatGridLocked)
	assert.Equal(t, []byte("overview-waveform"), song1.OverviewWaveFormData)
	assert.Equal(t, []byte("detailed-trackdata"), song1.TrackData)
	assert.Equal(t, 1, song1.ActiveOnLoadLoops)
	assert.Len(t, song1.Cues, 1)
	assert.Equal(t, "Cue 1", song1.Cues[0].Name)
	assert.Equal(t, 10.5, song1.Cues[0].Offset)
	assert.Len(t, song1.Loops, 1)
	assert.Equal(t, "Loop 1", song1.Loops[0].Name)
	assert.Equal(t, 30.0, song1.Loops[0].Start)
	assert.Equal(t, 45.0, song1.Loops[0].End)

	// Verify playlist hierarchy
	assert.Equal(t, "Main Playlist", importedLib.Playlists[0].Name)
	assert.Equal(t, []int{1, 2}, importedLib.Playlists[0].Songs)
	assert.Len(t, importedLib.Playlists[0].SubPlaylists, 1)
	assert.Equal(t, "Sub Playlist", importedLib.Playlists[0].SubPlaylists[0].Name)
	assert.Equal(t, []int{1}, importedLib.Playlists[0].SubPlaylists[0].Songs)

	// Verify smartlist
	assert.Equal(t, "smart-1", importedLib.Smartlists[0].ListUUID)
	assert.Equal(t, "Top Rated", importedLib.Smartlists[0].Title)

	// Verify album art
	assert.Equal(t, 1, importedLib.AlbumArt[0].ID)
	assert.Equal(t, "hash123", importedLib.AlbumArt[0].Hash)
	assert.Equal(t, []byte("fake-image-bytes"), importedLib.AlbumArt[0].Data)
}

func TestExportDefaultValuesAndRelativePaths(t *testing.T) {
	tempDir := t.TempDir()

	originalLib := lib.Library{
		Songs: []lib.Song{
			{
				SongID:     1,
				Title:      "Default Track",
				Path:       "relative/path/song.mp3",
				SampleRate: 0, // Should default to 44100
				Cues: []lib.HotCue{
					{
						Name:     "Bad Color Cue",
						Position: 2,
						Offset:   5.0,
						Color:    "invalid-hex", // Fallback color test
					},
				},
				Loops: []lib.Loop{
					{
						Name:     "Bad Color Loop",
						Position: 3,
						Start:    10.0,
						End:      20.0,
						Color:    "invalid-hex", // Fallback color test
					},
				},
			},
		},
		Playlists: []lib.Playlist{
			{
				PlaylistID: 0, // Auto-assign ID
				Name:       "Auto ID Playlist",
				Songs:      []int{1},
			},
		},
	}

	err := engine.Export(originalLib, tempDir, engine.ExportOptions{Overwrite: true})
	assert.NoError(t, err)

	importedLib, err := engine.Import(tempDir, engine.ImportOptions{
		PreserveOriginalPaths: false, // Exercises fullPathFromRelativePath
	})
	assert.NoError(t, err)

	assert.Equal(t, "export-generated-uuid", importedLib.DatabaseUUID)
	assert.Equal(t, 2, importedLib.SchemaVersionMajor)
	assert.Len(t, importedLib.Songs, 1)

	// Verify relative path resolution
	expectedPath, _ := filepath.Abs(filepath.Join(tempDir, "relative/path/song.mp3"))
	assert.Equal(t, expectedPath, importedLib.Songs[0].Path)

	// Verify cue fallback color
	assert.Len(t, importedLib.Songs[0].Cues, 1)
	assert.Equal(t, "#00FFFF", importedLib.Songs[0].Cues[0].Color)

	// Verify loop fallback color
	assert.Len(t, importedLib.Songs[0].Loops, 1)
	assert.Equal(t, "#000000", importedLib.Songs[0].Loops[0].Color)
}

func TestExportInvalidPath(t *testing.T) {
	invalidPath := "/dev/null/invalid_dir"
	err := engine.Export(lib.Library{}, invalidPath, engine.ExportOptions{})
	assert.Error(t, err)
}
