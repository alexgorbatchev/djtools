package engine_test

import (
	"testing"

	"github.com/nateranda/djtools/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaylistEntity_LinkedListOperations(t *testing.T) {
	db, ctx := setupTestDB(t)

	pl1, err := db.CreatePlaylist(ctx, "Playlist 1", 0, false)
	require.NoError(t, err)

	pl2, err := db.CreatePlaylist(ctx, "Playlist 2", 0, false)
	require.NoError(t, err)

	// Create 3 tracks
	t1, _ := db.InsertTrack(ctx, &engine.Track{Title: "Track 1", Path: "track1.mp3"})
	t2, _ := db.InsertTrack(ctx, &engine.Track{Title: "Track 2", Path: "track2.mp3"})
	t3, _ := db.InsertTrack(ctx, &engine.Track{Title: "Track 3", Path: "track3.mp3"})

	// Add tracks to pl1 in order: t1, t2, t3
	_, err = db.AddTrackToPlaylist(ctx, pl1.ID, t1)
	require.NoError(t, err)
	_, err = db.AddTrackToPlaylist(ctx, pl1.ID, t2)
	require.NoError(t, err)
	_, err = db.AddTrackToPlaylist(ctx, pl1.ID, t3)
	require.NoError(t, err)

	// Verify order
	tracks, err := db.GetPlaylistTracks(ctx, pl1.ID)
	require.NoError(t, err)
	require.Len(t, tracks, 3)
	assert.Equal(t, "Track 1", tracks[0].Title)
	assert.Equal(t, "Track 2", tracks[1].Title)
	assert.Equal(t, "Track 3", tracks[2].Title)

	// Remove middle track t2 and check relink
	err = db.RemoveTrackFromPlaylist(ctx, pl1.ID, t2)
	require.NoError(t, err)

	tracksAfterRemove, err := db.GetPlaylistTracks(ctx, pl1.ID)
	require.NoError(t, err)
	require.Len(t, tracksAfterRemove, 2)
	assert.Equal(t, "Track 1", tracksAfterRemove[0].Title)
	assert.Equal(t, "Track 3", tracksAfterRemove[1].Title)

	// Move t3 from pl1 to pl2
	err = db.MoveTrackBetweenPlaylists(ctx, pl1.ID, pl2.ID, t3)
	require.NoError(t, err)

	pl1Tracks, err := db.GetPlaylistTracks(ctx, pl1.ID)
	require.NoError(t, err)
	require.Len(t, pl1Tracks, 1)
	assert.Equal(t, "Track 1", pl1Tracks[0].Title)

	pl2Tracks, err := db.GetPlaylistTracks(ctx, pl2.ID)
	require.NoError(t, err)
	require.Len(t, pl2Tracks, 1)
	assert.Equal(t, "Track 3", pl2Tracks[0].Title)
}
