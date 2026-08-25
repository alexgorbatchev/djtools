package engine_test

import (
	"testing"

	"github.com/nateranda/djtools/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaylist_CRUDAndHierarchy(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Create root folder
	folder, err := db.CreatePlaylist(ctx, "Electronic", 0, true)
	require.NoError(t, err)
	assert.Equal(t, "Electronic", folder.Title)
	assert.Equal(t, int64(0), folder.ParentListID)

	// Create sub-playlist 1
	sub1, err := db.CreatePlaylist(ctx, "House", folder.ID, false)
	require.NoError(t, err)
	assert.Equal(t, "House", sub1.Title)
	assert.Equal(t, folder.ID, sub1.ParentListID)

	// Create sub-playlist 2
	sub2, err := db.CreatePlaylist(ctx, "Techno", folder.ID, false)
	require.NoError(t, err)
	assert.Equal(t, "Techno", sub2.Title)
	assert.Equal(t, folder.ID, sub2.ParentListID)

	// Add track to sub1
	tID, err := db.InsertTrack(ctx, &engine.Track{Title: "Track 1", Path: "track1.mp3"})
	require.NoError(t, err)
	_, err = db.AddTrackToPlaylist(ctx, sub1.ID, tID)
	require.NoError(t, err)

	// Verify Hierarchy
	nodes, err := db.GetPlaylistHierarchy(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "Electronic", nodes[0].Playlist.Title)
	assert.True(t, nodes[0].IsFolder)
	require.Len(t, nodes[0].Children, 2)
	assert.Equal(t, "House", nodes[0].Children[0].Playlist.Title)
	assert.Len(t, nodes[0].Children[0].Tracks, 1)
	assert.Equal(t, "Track 1", nodes[0].Children[0].Tracks[0].Title)
	assert.Equal(t, "Techno", nodes[0].Children[1].Playlist.Title)

	// Delete without force should fail if folder has children
	err = db.DeletePlaylist(ctx, folder.ID, false)
	assert.ErrorIs(t, err, engine.ErrPlaylistHasChildren)

	// Delete sub1
	err = db.DeletePlaylist(ctx, sub1.ID, false)
	require.NoError(t, err)

	_, err = db.GetPlaylistByID(ctx, sub1.ID)
	assert.ErrorIs(t, err, engine.ErrPlaylistNotFound)

	// Delete folder with force
	err = db.DeletePlaylist(ctx, folder.ID, true)
	require.NoError(t, err)

	_, err = db.GetPlaylistByID(ctx, folder.ID)
	assert.ErrorIs(t, err, engine.ErrPlaylistNotFound)

	_, err = db.GetPlaylistByID(ctx, sub2.ID)
	assert.ErrorIs(t, err, engine.ErrPlaylistNotFound)
}

func TestPlaylist_MoveAndCyclePrevention(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Folder A -> Sub A1 -> Sub A2
	folderA, err := db.CreatePlaylist(ctx, "Folder A", 0, true)
	require.NoError(t, err)

	subA1, err := db.CreatePlaylist(ctx, "Sub A1", folderA.ID, true)
	require.NoError(t, err)

	subA2, err := db.CreatePlaylist(ctx, "Sub A2", subA1.ID, false)
	require.NoError(t, err)

	folderB, err := db.CreatePlaylist(ctx, "Folder B", 0, true)
	require.NoError(t, err)

	// 1. Move subA2 into folderB
	err = db.MovePlaylist(ctx, subA2.ID, folderB.ID)
	require.NoError(t, err)

	p, err := db.GetPlaylistByID(ctx, subA2.ID)
	require.NoError(t, err)
	assert.Equal(t, folderB.ID, p.ParentListID)

	// 2. Cyclic move: cannot move folderA into subA1
	err = db.MovePlaylist(ctx, folderA.ID, subA1.ID)
	assert.ErrorIs(t, err, engine.ErrCircularPlaylistMove)

	// 3. Cannot move into itself
	err = db.MovePlaylist(ctx, folderA.ID, folderA.ID)
	assert.ErrorIs(t, err, engine.ErrCircularPlaylistMove)
}
