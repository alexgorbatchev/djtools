package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/nateranda/djtools/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) (*engine.DB, context.Context) {
	t.Helper()
	tempDir := t.TempDir()
	ctx := context.Background()

	db, err := engine.Open(tempDir, false)
	require.NoError(t, err)
	err = db.CreateSchema(ctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db, ctx
}

func TestTrack_CRUD(t *testing.T) {
	db, ctx := setupTestDB(t)

	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	// 1. Insert track
	track1 := &engine.Track{
		Title:       "Test Track 1",
		Artist:      "Artist A",
		Album:       "Album A",
		Genre:       "Techno",
		Path:        "/music/artist_a/track1.mp3",
		Length:      360,
		BPM:         130,
		BPMAnalyzed: 130.02,
		AlbumArtID:  3,
		Year:        2024,
		Bitrate:     320,
		Key:         5,
		Rating:      80,
		DateAdded:   &now,
		DateCreated: &now,
	}

	id1, err := db.InsertTrack(ctx, track1)
	require.NoError(t, err)
	assert.Equal(t, id1, track1.ID)
	assert.Equal(t, "track1.mp3", track1.Filename)

	// 2. GetTrackByID
	fetched, err := db.GetTrackByID(ctx, id1)
	require.NoError(t, err)
	assert.Equal(t, "Test Track 1", fetched.Title)
	assert.Equal(t, "Artist A", fetched.Artist)
	assert.Equal(t, 130.02, fetched.BPMAnalyzed)
	assert.Equal(t, "track1.mp3", fetched.Filename)

	// 3. UpdateTrack
	fetched.Title = "Updated Title"
	fetched.BPM = 132
	fetched.AlbumArtID = 5
	fetched.Filename = "" // Trigger auto-filename from Path
	err = db.UpdateTrack(ctx, fetched)
	require.NoError(t, err)

	updated, err := db.GetTrackByID(ctx, id1)
	require.NoError(t, err)
	assert.Equal(t, "Updated Title", updated.Title)
	assert.Equal(t, int64(132), updated.BPM)
	assert.Equal(t, int64(5), updated.AlbumArtID)
	assert.Equal(t, "track1.mp3", updated.Filename)

	// 4. GetAllTracks
	track2 := &engine.Track{
		Title:  "Test Track 2",
		Artist: "Artist B",
		Path:   "/music/artist_b/track2.wav",
	}
	id2, err := db.InsertTrack(ctx, track2)
	require.NoError(t, err)

	allTracks, err := db.GetAllTracks(ctx)
	require.NoError(t, err)
	assert.Len(t, allTracks, 2)
	assert.Equal(t, id1, allTracks[0].ID)
	assert.Equal(t, id2, allTracks[1].ID)

	// 5. DeleteTrack
	err = db.DeleteTrack(ctx, id1)
	require.NoError(t, err)

	_, err = db.GetTrackByID(ctx, id1)
	assert.ErrorIs(t, err, engine.ErrTrackNotFound)

	allRemaining, err := db.GetAllTracks(ctx)
	require.NoError(t, err)
	assert.Len(t, allRemaining, 1)
	assert.Equal(t, id2, allRemaining[0].ID)
}

func TestTrack_FindTrack(t *testing.T) {
	db, ctx := setupTestDB(t)

	track := &engine.Track{
		Title:  "Strobe",
		Artist: "deadmau5",
		Path:   "/music/deadmau5/strobe.flac",
	}
	id, err := db.InsertTrack(ctx, track)
	require.NoError(t, err)

	tests := []struct {
		name    string
		query   string
		wantID  int64
		wantErr bool
	}{
		{"by numeric ID", "1", id, false},
		{"by exact path", "/music/deadmau5/strobe.flac", id, false},
		{"by filename", "strobe.flac", id, false},
		{"by title exact", "Strobe", id, false},
		{"by title case-insensitive", "strobe", id, false},
		{"empty query", "", 0, true},
		{"not found query", "Nonexistent", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found, err := db.FindTrack(ctx, tt.query)
			if tt.wantErr {
				assert.ErrorIs(t, err, engine.ErrTrackNotFound)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantID, found.ID)
			}
		})
	}
}

func TestTrack_RelocateTracks(t *testing.T) {
	db, ctx := setupTestDB(t)

	_, err := db.InsertTrack(ctx, &engine.Track{
		Title: "Track A",
		Path:  "/Volumes/OldDrive/Music/SongA.mp3",
	})
	require.NoError(t, err)

	_, err = db.InsertTrack(ctx, &engine.Track{
		Title: "Track B",
		Path:  "/Volumes/OldDrive/Music/SongB.mp3",
	})
	require.NoError(t, err)

	_, err = db.InsertTrack(ctx, &engine.Track{
		Title: "Track C",
		Path:  "/Volumes/OtherDrive/Music/SongC.mp3",
	})
	require.NoError(t, err)

	// Relocate
	count, err := db.RelocateTracks(ctx, "/Volumes/OldDrive/Music", "/Volumes/NewDrive/DJTracks")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify
	tA, err := db.FindTrack(ctx, "Track A")
	require.NoError(t, err)
	assert.Equal(t, "/Volumes/NewDrive/DJTracks/SongA.mp3", tA.Path)
	assert.Equal(t, "SongA.mp3", tA.Filename)

	tC, err := db.FindTrack(ctx, "Track C")
	require.NoError(t, err)
	assert.Equal(t, "/Volumes/OtherDrive/Music/SongC.mp3", tC.Path)
}
