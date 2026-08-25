package engine_test

import (
	"testing"

	"github.com/nateranda/djtools/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlbumArt_CRUD(t *testing.T) {
	db, ctx := setupTestDB(t)

	// 1. Insert new
	id1, err := db.InsertOrUpdateAlbumArt(ctx, "hash_abc", []byte("raw_image_data_1"))
	require.NoError(t, err)
	assert.Greater(t, id1, int64(0))

	// 2. GetAlbumArt
	art, err := db.GetAlbumArt(ctx, id1)
	require.NoError(t, err)
	assert.Equal(t, "hash_abc", art.Hash)
	assert.Equal(t, []byte("raw_image_data_1"), art.Data)

	// 3. GetAlbumArtByHash
	artByHash, err := db.GetAlbumArtByHash(ctx, "hash_abc")
	require.NoError(t, err)
	assert.Equal(t, id1, artByHash.ID)

	// 4. Update existing by inserting same hash with new data
	idUpdated, err := db.InsertOrUpdateAlbumArt(ctx, "hash_abc", []byte("updated_image_data"))
	require.NoError(t, err)
	assert.Equal(t, id1, idUpdated)

	artUpdated, err := db.GetAlbumArt(ctx, id1)
	require.NoError(t, err)
	assert.Equal(t, []byte("updated_image_data"), artUpdated.Data)

	// 5. GetAllAlbumArt
	_, err = db.InsertOrUpdateAlbumArt(ctx, "hash_def", []byte("second_image"))
	require.NoError(t, err)

	allArt, err := db.GetAllAlbumArt(ctx)
	require.NoError(t, err)
	assert.Len(t, allArt, 2)

	// 6. DeleteAlbumArt
	err = db.DeleteAlbumArt(ctx, id1)
	require.NoError(t, err)

	_, err = db.GetAlbumArt(ctx, id1)
	assert.ErrorIs(t, err, engine.ErrAlbumArtNotFound)
}
