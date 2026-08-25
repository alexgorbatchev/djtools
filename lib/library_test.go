package lib_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nateranda/djtools/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLib_ColorConversions(t *testing.T) {
	tests := []struct {
		name    string
		r, g, b int
		hex     string
		wantErr bool
	}{
		{"black", 0, 0, 0, "#000000", false},
		{"white", 255, 255, 255, "#FFFFFF", false},
		{"cyan", 0, 255, 255, "#00FFFF", false},
		{"red", 255, 0, 0, "#FF0000", false},
		{"out of range r", 300, 0, 0, "", true},
		{"negative g", 0, -10, 0, "", true},
		{"out of range b", 0, 0, 256, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hex, err := lib.RgbToHex(tt.r, tt.g, tt.b)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.hex, hex)

				r, g, b, err := lib.HexToRgb(hex)
				require.NoError(t, err)
				assert.Equal(t, tt.r, r)
				assert.Equal(t, tt.g, g)
				assert.Equal(t, tt.b, b)
			}
		})
	}

	// HexToRgb invalid
	_, _, _, err := lib.HexToRgb("invalid")
	assert.Error(t, err)
}

func TestLibrary_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "library.json")

	original := lib.Library{
		DatabaseUUID: "test-uuid-999",
		Songs: []lib.Song{
			{
				SongID: 1,
				Title:  "Track 1",
				Path:   "/music/t1.mp3",
			},
		},
		Playlists: []lib.Playlist{
			{
				PlaylistID: 10,
				Name:       "Favorites",
				Songs:      []int{1},
			},
		},
	}

	err := original.Save(filePath)
	require.NoError(t, err)

	var loaded lib.Library
	err = loaded.Load(filePath)
	require.NoError(t, err)

	assert.Equal(t, original.DatabaseUUID, loaded.DatabaseUUID)
	assert.Equal(t, len(original.Songs), len(loaded.Songs))
	assert.Equal(t, original.Songs[0].Title, loaded.Songs[0].Title)

	// Save error with invalid path
	err = original.Save("/invalid/path/test.json")
	assert.Error(t, err)

	// Load error with non-existent file
	var nonExistent lib.Library
	err = nonExistent.Load("/invalid/path/missing.json")
	assert.Error(t, err)
}

func TestLibrary_SongAndPlaylistOperations(t *testing.T) {
	l := lib.Library{
		Songs: []lib.Song{
			{
				SongID: 2,
				Title:  "Zebra",
				Path:   "/music/zebra.mp3",
				Cues: []lib.HotCue{
					{Position: 3, Name: "Third"},
					{Position: 1, Name: "First"},
				},
				Loops: []lib.Loop{
					{Position: 2, Name: "Second"},
					{Position: 1, Name: "First"},
				},
			},
			{
				SongID: 1,
				Title:  "Apple",
				Path:   "/music/apple.mp3",
			},
			{
				SongID:  3,
				Title:   "Corrupted",
				Path:    "/music/corrupt.mp3",
				Corrupt: true,
			},
		},
		Playlists: []lib.Playlist{
			{
				PlaylistID: 100,
				Name:       "Root Playlist",
				Songs:      []int{1, 2, 3},
				SubPlaylists: []lib.Playlist{
					{
						PlaylistID: 200,
						Name:       "Sub Playlist",
						Songs:      []int{3},
					},
				},
			},
		},
	}

	// 1. SortSongs
	l.SortSongs()
	assert.Equal(t, 1, l.Songs[0].SongID)
	assert.Equal(t, 2, l.Songs[1].SongID)
	assert.Equal(t, 1, l.Songs[1].Cues[0].Position)
	assert.Equal(t, 1, l.Songs[1].Loops[0].Position)

	// 2. Find helpers
	s1 := l.FindSongByID(1)
	require.NotNil(t, s1)
	assert.Equal(t, "Apple", s1.Title)

	sNotFound := l.FindSongByID(999)
	assert.Nil(t, sNotFound)

	sByPath := l.FindSongByPath("/music/apple.mp3")
	require.NotNil(t, sByPath)
	assert.Equal(t, 1, sByPath.SongID)

	sByPathNotFound := l.FindSongByPath("/music/none.mp3")
	assert.Nil(t, sByPathNotFound)

	sByTitle := l.FindSongByTitle("Apple")
	require.NotNil(t, sByTitle)
	assert.Equal(t, 1, sByTitle.SongID)

	sByTitleNotFound := l.FindSongByTitle("Unknown")
	assert.Nil(t, sByTitleNotFound)

	// 3. Find Playlist
	pRoot := l.FindPlaylistByID(100)
	require.NotNil(t, pRoot)
	assert.Equal(t, "Root Playlist", pRoot.Name)

	pSub := l.FindPlaylistByID(200)
	require.NotNil(t, pSub)
	assert.Equal(t, "Sub Playlist", pSub.Name)

	pSubByName := l.FindPlaylistByName("Sub Playlist")
	require.NotNil(t, pSubByName)
	assert.Equal(t, 200, pSubByName.PlaylistID)

	pNone := l.FindPlaylistByID(999)
	assert.Nil(t, pNone)

	pNoneByName := l.FindPlaylistByName("NoName")
	assert.Nil(t, pNoneByName)

	// 4. AddSong
	l.AddSong(lib.Song{SongID: 4, Title: "New Track"})
	assert.NotNil(t, l.FindSongByID(4))

	// 5. CheckCorruptedSongs
	l.CheckCorruptedSongs()
	assert.Nil(t, l.FindSongByID(3))
	assert.NotContains(t, l.Playlists[0].Songs, 3)
	assert.NotContains(t, l.Playlists[0].SubPlaylists[0].Songs, 3)

	// 6. RemoveSong
	l.RemoveSong(1)
	assert.Nil(t, l.FindSongByID(1))
	assert.NotContains(t, l.Playlists[0].Songs, 1)
}

func TestLib_CopyFile(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "src.txt")
	dst := filepath.Join(tempDir, "dst.txt")

	err := os.WriteFile(src, []byte("hello world"), 0644)
	require.NoError(t, err)

	lib.CopyFile(t, src, dst)

	content, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}
