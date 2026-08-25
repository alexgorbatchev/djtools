package engine_test

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nateranda/djtools/engine"
	"github.com/nateranda/djtools/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDB_EdgeCasesAndReadOnlyErrors(t *testing.T) {
	tempDir := t.TempDir()
	ctx := context.Background()

	// RW bootstrap
	dbRW, err := engine.Open(tempDir, false)
	require.NoError(t, err)
	require.NoError(t, dbRW.CreateSchema(ctx))
	require.NotNil(t, dbRW.HMDB())
	require.NoError(t, dbRW.Close())

	// Re-close is safe
	assert.NoError(t, dbRW.Close())

	// Open read-only
	dbRO, err := engine.Open(tempDir, true)
	require.NoError(t, err)
	defer dbRO.Close()

	// All mutating methods should return error on read-only DB
	_, err = dbRO.InsertTrack(ctx, &engine.Track{Title: "T"})
	assert.ErrorContains(t, err, "read-only")

	err = dbRO.UpdateTrack(ctx, &engine.Track{ID: 1, Title: "T"})
	assert.ErrorContains(t, err, "read-only")

	err = dbRO.DeleteTrack(ctx, 1)
	assert.ErrorContains(t, err, "read-only")

	_, err = dbRO.RelocateTracks(ctx, "a", "b")
	assert.ErrorContains(t, err, "read-only")

	_, err = dbRO.CreatePlaylist(ctx, "P", 0, false)
	assert.ErrorContains(t, err, "read-only")

	err = dbRO.DeletePlaylist(ctx, 1, false)
	assert.ErrorContains(t, err, "read-only")

	err = dbRO.MovePlaylist(ctx, 1, 2)
	assert.ErrorContains(t, err, "read-only")

	_, err = dbRO.AddTrackToPlaylist(ctx, 1, 1)
	assert.ErrorContains(t, err, "read-only")

	err = dbRO.RemoveTrackFromPlaylist(ctx, 1, 1)
	assert.ErrorContains(t, err, "read-only")

	err = dbRO.MoveTrackBetweenPlaylists(ctx, 1, 2, 1)
	assert.ErrorContains(t, err, "read-only")

	_, err = dbRO.InsertOrUpdateAlbumArt(ctx, "h", []byte("d"))
	assert.ErrorContains(t, err, "read-only")

	err = dbRO.DeleteAlbumArt(ctx, 1)
	assert.ErrorContains(t, err, "read-only")

	err = dbRO.UpdatePerformanceData(ctx, &engine.PerformanceData{TrackID: 1})
	assert.ErrorContains(t, err, "read-only")
}

func TestTrack_ValidationAndCascadeDeletion(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Update with invalid ID (0 or negative)
	err := db.UpdateTrack(ctx, &engine.Track{ID: 0, Title: "Zero"})
	assert.ErrorContains(t, err, "invalid track ID")

	// Update non-existent track
	err = db.UpdateTrack(ctx, &engine.Track{ID: 9999, Title: "Non-existent"})
	assert.ErrorIs(t, err, engine.ErrTrackNotFound)

	// Delete non-existent track
	err = db.DeleteTrack(ctx, 9999)
	assert.ErrorIs(t, err, engine.ErrTrackNotFound)

	// Test track deletion cascades playlist entity relinking and performance data
	t1, err := db.InsertTrack(ctx, &engine.Track{Title: "Track A", Path: "a.mp3"})
	require.NoError(t, err)
	t2, err := db.InsertTrack(ctx, &engine.Track{Title: "Track B", Path: "b.mp3"})
	require.NoError(t, err)
	t3, err := db.InsertTrack(ctx, &engine.Track{Title: "Track C", Path: "c.mp3"})
	require.NoError(t, err)

	pl, err := db.CreatePlaylist(ctx, "Cascade Playlist", 0, false)
	require.NoError(t, err)

	_, err = db.AddTrackToPlaylist(ctx, pl.ID, t1)
	require.NoError(t, err)
	_, err = db.AddTrackToPlaylist(ctx, pl.ID, t2)
	require.NoError(t, err)
	_, err = db.AddTrackToPlaylist(ctx, pl.ID, t3)
	require.NoError(t, err)

	// Add perf data for t2
	err = db.UpdatePerformanceData(ctx, &engine.PerformanceData{
		TrackID: t2,
		HotCues: []lib.HotCue{{Position: 1, Name: "Drop", Color: "#FFFFFF"}},
	})
	require.NoError(t, err)

	// Delete t2
	err = db.DeleteTrack(ctx, t2)
	require.NoError(t, err)

	// Perf data for t2 should be gone
	_, err = db.GetPerformanceData(ctx, t2)
	assert.ErrorIs(t, err, engine.ErrPerformanceDataNotFound)

	// Playlist tracks should be relinked to [t1, t3]
	plTracks, err := db.GetPlaylistTracks(ctx, pl.ID)
	require.NoError(t, err)
	require.Len(t, plTracks, 2)
	assert.Equal(t, "Track A", plTracks[0].Title)
	assert.Equal(t, "Track C", plTracks[1].Title)

	// Test track deletion when track is in multiple playlists
	tMulti, err := db.InsertTrack(ctx, &engine.Track{Title: "Multi PL Track", Path: "multi.mp3"})
	require.NoError(t, err)
	plB, err := db.CreatePlaylist(ctx, "Second PL", 0, false)
	require.NoError(t, err)
	_, _ = db.AddTrackToPlaylist(ctx, pl.ID, tMulti)
	_, _ = db.AddTrackToPlaylist(ctx, plB.ID, tMulti)

	err = db.DeleteTrack(ctx, tMulti)
	require.NoError(t, err)

	plBTracks, err := db.GetPlaylistTracks(ctx, plB.ID)
	require.NoError(t, err)
	assert.Empty(t, plBTracks)
}

func TestPlaylist_EdgeCases(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Delete non-existent playlist
	err := db.DeletePlaylist(ctx, 9999, false)
	assert.ErrorIs(t, err, engine.ErrPlaylistNotFound)

	// Move non-existent playlist
	err = db.MovePlaylist(ctx, 9999, 1)
	assert.ErrorIs(t, err, engine.ErrPlaylistNotFound)

	// Move with same parent (no-op)
	p1, err := db.CreatePlaylist(ctx, "P1", 0, false)
	require.NoError(t, err)
	err = db.MovePlaylist(ctx, p1.ID, 0)
	assert.NoError(t, err)

	// Remove non-existent track from playlist
	err = db.RemoveTrackFromPlaylist(ctx, p1.ID, 9999)
	assert.ErrorIs(t, err, engine.ErrEntityNotFound)

	// Move non-existent track between playlists
	p2, err := db.CreatePlaylist(ctx, "P2", 0, false)
	require.NoError(t, err)
	err = db.MoveTrackBetweenPlaylists(ctx, p1.ID, p2.ID, 9999)
	assert.ErrorIs(t, err, engine.ErrEntityNotFound)
}

func TestPerformanceData_EdgeCases(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Get perf for non-existent track
	_, err := db.GetPerformanceData(ctx, 9999)
	assert.ErrorIs(t, err, engine.ErrPerformanceDataNotFound)

	// Update with invalid track ID
	err = db.UpdatePerformanceData(ctx, &engine.PerformanceData{TrackID: 0})
	assert.ErrorContains(t, err, "invalid track ID")

	// Get album art by non-existent hash
	_, err = db.GetAlbumArtByHash(ctx, "nonexistent-hash")
	assert.ErrorIs(t, err, engine.ErrAlbumArtNotFound)

	// Delete non-existent album art
	err = db.DeleteAlbumArt(ctx, 9999)
	assert.ErrorIs(t, err, engine.ErrAlbumArtNotFound)

	// Ping with nil mDB
	closedDB, _ := engine.Open(t.TempDir(), false)
	_ = closedDB.Close()
	assert.Error(t, closedDB.Ping(ctx))
}

func TestPerformanceData_BlobDecompressionErrors(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Track with corrupt/truncated beat blob
	tID, err := db.InsertTrack(ctx, &engine.Track{Title: "Corrupt Beat", Path: "cbeat.mp3"})
	require.NoError(t, err)

	// Insert raw invalid blobs directly into PerformanceData table
	_, err = db.MDB().ExecContext(ctx, `INSERT INTO PerformanceData (trackId, beatData, quickCues, loops) VALUES (?, ?, ?, ?)`,
		tID, []byte{0x00, 0x00, 0x00, 0x01}, []byte{0x00, 0x00, 0x00, 0x01}, []byte{0x00, 0x00})
	require.NoError(t, err)

	perf, err := db.GetPerformanceData(ctx, tID)
	require.NoError(t, err)
	assert.Equal(t, float64(44100), perf.SampleRate)

	// Partially truncated beat data (valid zlib but truncated marker entries)
	rawBeat := make([]byte, 40) // Less than required for full markers
	binary.BigEndian.PutUint64(rawBeat[0:8], 44100)
	binary.BigEndian.PutUint64(rawBeat[25:33], 5) // declares 5 markers, but only 40 bytes total
	compBeat, err := engine.QCompress(rawBeat)
	require.NoError(t, err)

	// Partially truncated quick cues
	rawCues := make([]byte, 20)
	binary.BigEndian.PutUint64(rawCues[0:8], 8)
	rawCues[8] = 50 // Declares label length 50 but not enough bytes
	compCues, err := engine.QCompress(rawCues)
	require.NoError(t, err)

	// Partially truncated loops
	rawLoops := make([]byte, 20)
	binary.BigEndian.PutUint64(rawLoops[0:8], 8)
	rawLoops[8] = 50 // Declares label length 50 but not enough bytes

	tID2, err := db.InsertTrack(ctx, &engine.Track{Title: "Truncated Blobs", Path: "trunc.mp3"})
	require.NoError(t, err)

	_, err = db.MDB().ExecContext(ctx, `INSERT INTO PerformanceData (trackId, beatData, quickCues, loops) VALUES (?, ?, ?, ?)`,
		tID2, compBeat, compCues, rawLoops)
	require.NoError(t, err)

	perf2, err := db.GetPerformanceData(ctx, tID2)
	require.NoError(t, err)
	assert.NotNil(t, perf2)

	// Beat blob < 33 bytes
	shortBeat, err := engine.QCompress([]byte{1, 2, 3})
	require.NoError(t, err)
	tID3, err := db.InsertTrack(ctx, &engine.Track{Title: "Short Beat", Path: "short.mp3"})
	require.NoError(t, err)
	_, err = db.MDB().ExecContext(ctx, `INSERT INTO PerformanceData (trackId, beatData) VALUES (?, ?)`, tID3, shortBeat)
	require.NoError(t, err)
	perf3, err := db.GetPerformanceData(ctx, tID3)
	require.NoError(t, err)
	assert.NotNil(t, perf3)

	// Beat blob with default markers count = 0, but no adj header (33 bytes)
	exact33Beat := make([]byte, 33)
	binary.BigEndian.PutUint64(exact33Beat[0:8], 44100)
	comp33, err := engine.QCompress(exact33Beat)
	require.NoError(t, err)
	tID4, err := db.InsertTrack(ctx, &engine.Track{Title: "Exact 33 Beat", Path: "exact33.mp3"})
	require.NoError(t, err)
	_, err = db.MDB().ExecContext(ctx, `INSERT INTO PerformanceData (trackId, beatData) VALUES (?, ?)`, tID4, comp33)
	require.NoError(t, err)
	perf4, err := db.GetPerformanceData(ctx, tID4)
	require.NoError(t, err)
	assert.NotNil(t, perf4)
}

func TestMoreEdgeCases(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Relocate tracks when none match
	count, err := db.RelocateTracks(ctx, "/no/match", "/new/path")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// GetPlaylistTracks for empty playlist
	pl, err := db.CreatePlaylist(ctx, "Empty Playlist", 0, false)
	require.NoError(t, err)
	tracks, err := db.GetPlaylistTracks(ctx, pl.ID)
	require.NoError(t, err)
	assert.Empty(t, tracks)

	// Move track from empty playlist should fail
	err = db.MoveTrackBetweenPlaylists(ctx, pl.ID, pl.ID, 123)
	assert.ErrorIs(t, err, engine.ErrEntityNotFound)

	// GetAllPlaylists
	all, err := db.GetAllPlaylists(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, all)

	// GetAllAlbumArt when empty
	emptyDB, emptyCtx := setupTestDB(t)
	art, err := emptyDB.GetAllAlbumArt(emptyCtx)
	require.NoError(t, err)
	assert.Empty(t, art)
}

func TestGranular_AdditionalBranches(t *testing.T) {
	db, ctx := setupTestDB(t)

	// 1. Create multiple sibling playlists to exercise tail predecessor linking
	p1, err := db.CreatePlaylist(ctx, "Playlist Alpha", 0, false)
	require.NoError(t, err)
	p2, err := db.CreatePlaylist(ctx, "Playlist Beta", 0, false)
	require.NoError(t, err)
	p3, err := db.CreatePlaylist(ctx, "Playlist Gamma", 0, false)
	require.NoError(t, err)

	// Verify sibling order
	playlists, err := db.GetAllPlaylists(ctx)
	require.NoError(t, err)
	assert.Len(t, playlists, 3)

	// Delete middle sibling (p2) to test predecessor relinking
	err = db.DeletePlaylist(ctx, p2.ID, false)
	require.NoError(t, err)

	// 2. Add tracks to playlist with multiple items to exercise tail entity linking
	t1, err := db.InsertTrack(ctx, &engine.Track{Title: "Track 1", Path: "/m/1.mp3"})
	require.NoError(t, err)
	t2, err := db.InsertTrack(ctx, &engine.Track{Title: "Track 2", Path: "/m/2.mp3"})
	require.NoError(t, err)
	t3, err := db.InsertTrack(ctx, &engine.Track{Title: "Track 3", Path: "/m/3.mp3"})
	require.NoError(t, err)

	_, err = db.AddTrackToPlaylist(ctx, p1.ID, t1)
	require.NoError(t, err)
	_, err = db.AddTrackToPlaylist(ctx, p1.ID, t2)
	require.NoError(t, err)
	_, err = db.AddTrackToPlaylist(ctx, p1.ID, t3)
	require.NoError(t, err)

	// Move track from p1 to p3 where p3 already has tracks (exercises destination tail linking)
	_, err = db.AddTrackToPlaylist(ctx, p3.ID, t1)
	require.NoError(t, err)
	err = db.MoveTrackBetweenPlaylists(ctx, p1.ID, p3.ID, t2)
	require.NoError(t, err)

	p3Tracks, err := db.GetPlaylistTracks(ctx, p3.ID)
	require.NoError(t, err)
	require.Len(t, p3Tracks, 2)
	assert.Equal(t, "Track 1", p3Tracks[0].Title)
	assert.Equal(t, "Track 2", p3Tracks[1].Title)

	// 3. Move playlist to new parent that already has children
	folderParent, err := db.CreatePlaylist(ctx, "Folder Parent", 0, true)
	require.NoError(t, err)
	_, err = db.CreatePlaylist(ctx, "Folder Child 1", folderParent.ID, false)
	require.NoError(t, err)
	err = db.MovePlaylist(ctx, p1.ID, folderParent.ID)
	require.NoError(t, err)

	// Check hierarchy
	nodes, err := db.GetPlaylistHierarchy(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, nodes)
}

func TestInternalHelper_EdgeCoverages(t *testing.T) {
	// 1. Export with default information fields
	tempDir := t.TempDir()
	emptyLib := lib.Library{
		Smartlists: []lib.Smartlist{
			{ListUUID: "uuid-1", Title: "Smart 1", Rules: "{}"},
		},
		AlbumArt: []lib.AlbumArt{
			{ID: 1, Hash: "art-hash", Data: []byte("img")},
		},
	}
	err := engine.Export(emptyLib, tempDir, engine.ExportOptions{Overwrite: true})
	require.NoError(t, err)

	imported, err := engine.Import(tempDir, engine.ImportOptions{PreserveOriginalPaths: false})
	require.NoError(t, err)
	assert.Equal(t, "export-generated-uuid", imported.DatabaseUUID)
	assert.Equal(t, 2, imported.SchemaVersionMajor)
	assert.Len(t, imported.Smartlists, 1)
	assert.Len(t, imported.AlbumArt, 1)

	// 2. Open without hm.db
	mOnlyDir := t.TempDir()
	db, err := engine.Open(mOnlyDir, false)
	require.NoError(t, err)
	require.NoError(t, db.CreateSchema(context.Background()))
	require.NoError(t, db.Close())

	// Remove hm.db so only m.db exists
	_ = os.Remove(filepath.Join(mOnlyDir, "Database2", "hm.db"))
	dbMOnly, err := engine.Open(mOnlyDir, true)
	require.NoError(t, err)
	assert.Nil(t, dbMOnly.HMDB())
	assert.NoError(t, dbMOnly.Ping(context.Background()))
	assert.NoError(t, dbMOnly.Close())
}

func TestDB_MissingTablesHandling(t *testing.T) {
	tempDir := t.TempDir()
	// Open DB without creating schema
	db, err := engine.Open(tempDir, false)
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()

	// Album art queries on empty schema
	art, err := db.GetAlbumArt(ctx, 1)
	assert.ErrorIs(t, err, engine.ErrAlbumArtNotFound)
	assert.Nil(t, art)

	artHash, err := db.GetAlbumArtByHash(ctx, "abc")
	assert.ErrorIs(t, err, engine.ErrAlbumArtNotFound)
	assert.Nil(t, artHash)

	allArt, err := db.GetAllAlbumArt(ctx)
	assert.NoError(t, err)
	assert.Empty(t, allArt)

	// Performance data on empty schema
	perf, err := db.GetPerformanceData(ctx, 1)
	assert.ErrorIs(t, err, engine.ErrPerformanceDataNotFound)
	assert.Nil(t, perf)
}

func TestPlaylist_DeepCascadeDeletion(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Level 1: Root Folder
	rootF, err := db.CreatePlaylist(ctx, "Root Folder", 0, true)
	require.NoError(t, err)

	// Level 2: Sub Folder
	subF, err := db.CreatePlaylist(ctx, "Sub Folder", rootF.ID, true)
	require.NoError(t, err)

	// Level 3: Leaf Playlist
	leafPL, err := db.CreatePlaylist(ctx, "Leaf Playlist", subF.ID, false)
	require.NoError(t, err)

	t1, err := db.InsertTrack(ctx, &engine.Track{Title: "Deep Track", Path: "deep.mp3"})
	require.NoError(t, err)

	_, err = db.AddTrackToPlaylist(ctx, leafPL.ID, t1)
	require.NoError(t, err)

	// Cascade delete root folder
	err = db.DeletePlaylist(ctx, rootF.ID, true)
	require.NoError(t, err)

	_, err = db.GetPlaylistByID(ctx, rootF.ID)
	assert.ErrorIs(t, err, engine.ErrPlaylistNotFound)

	_, err = db.GetPlaylistByID(ctx, subF.ID)
	assert.ErrorIs(t, err, engine.ErrPlaylistNotFound)

	_, err = db.GetPlaylistByID(ctx, leafPL.ID)
	assert.ErrorIs(t, err, engine.ErrPlaylistNotFound)
}

func TestMoveTrackBetweenPlaylists_MultipleTracksBothSides(t *testing.T) {
	db, ctx := setupTestDB(t)

	pSrc, err := db.CreatePlaylist(ctx, "Src PL", 0, false)
	require.NoError(t, err)
	pDst, err := db.CreatePlaylist(ctx, "Dst PL", 0, false)
	require.NoError(t, err)

	t1, _ := db.InsertTrack(ctx, &engine.Track{Title: "T1", Path: "1.mp3"})
	t2, _ := db.InsertTrack(ctx, &engine.Track{Title: "T2", Path: "2.mp3"})
	t3, _ := db.InsertTrack(ctx, &engine.Track{Title: "T3", Path: "3.mp3"})
	t4, _ := db.InsertTrack(ctx, &engine.Track{Title: "T4", Path: "4.mp3"})

	// Src has: t1, t2, t3
	_, _ = db.AddTrackToPlaylist(ctx, pSrc.ID, t1)
	_, _ = db.AddTrackToPlaylist(ctx, pSrc.ID, t2)
	_, _ = db.AddTrackToPlaylist(ctx, pSrc.ID, t3)

	// Dst has: t4
	_, _ = db.AddTrackToPlaylist(ctx, pDst.ID, t4)

	// Move middle track t2 from Src to Dst
	err = db.MoveTrackBetweenPlaylists(ctx, pSrc.ID, pDst.ID, t2)
	require.NoError(t, err)

	srcTracks, err := db.GetPlaylistTracks(ctx, pSrc.ID)
	require.NoError(t, err)
	require.Len(t, srcTracks, 2)
	assert.Equal(t, "T1", srcTracks[0].Title)
	assert.Equal(t, "T3", srcTracks[1].Title)

	dstTracks, err := db.GetPlaylistTracks(ctx, pDst.ID)
	require.NoError(t, err)
	require.Len(t, dstTracks, 2)
	assert.Equal(t, "T4", dstTracks[0].Title)
	assert.Equal(t, "T2", dstTracks[1].Title)
}

func TestHelperFunctions_DirectUnitCoverage(t *testing.T) {
	// 1. Export with various date fields populated
	tempDir := t.TempDir()
	now := time.Now().UTC()
	unixNow := int(now.Unix())

	libWithAllDates := lib.Library{
		DatabaseUUID:       "custom-uuid-456",
		SchemaVersionMajor: 3,
		SchemaVersionMinor: 1,
		SchemaVersionPatch: 0,
		Songs: []lib.Song{
			{
				SongID:         1,
				Title:          "Date Track",
				Path:           "dates.mp3",
				DateAdded:      unixNow,
				DateModified:   unixNow,
				DateCreated:    unixNow,
				TimeLastPlayed: unixNow,
				BpmAnalyzed:    128.5,
				AlbumArtID:     10,
			},
		},
		AlbumArt: []lib.AlbumArt{
			{ID: 10, Hash: "art-10", Data: []byte("blob10")},
		},
		Playlists: []lib.Playlist{
			{
				PlaylistID: 1,
				Name:       "Root PL",
				Songs:      []int{1},
				SubPlaylists: []lib.Playlist{
					{
						PlaylistID: 2,
						Name:       "Child PL",
						Songs:      []int{1},
					},
				},
			},
		},
	}

	err := engine.Export(libWithAllDates, tempDir, engine.ExportOptions{Overwrite: true})
	require.NoError(t, err)

	imported, err := engine.Import(tempDir, engine.ImportOptions{
		PreserveOriginalPaths: true,
		ImportOriginalCues:    true,
		ImportOriginalGrids:   true,
	})
	require.NoError(t, err)
	assert.Equal(t, "custom-uuid-456", imported.DatabaseUUID)
	assert.Equal(t, 3, imported.SchemaVersionMajor)
	assert.Len(t, imported.Songs, 1)
	assert.Equal(t, 128.5, imported.Songs[0].BpmAnalyzed)
	assert.Equal(t, 10, imported.Songs[0].AlbumArtID)
}

func TestPerformanceData_BlobEncodingAndDecoding(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Negative grid start position adjustment test
	trackID, err := db.InsertTrack(ctx, &engine.Track{Title: "Negative Grid Track", Path: "neg.mp3"})
	require.NoError(t, err)

	perfData := &engine.PerformanceData{
		TrackID:    trackID,
		SampleRate: 44100,
		BeatGrid: []lib.Marker{
			{
				StartPosition: -0.5,
				Bpm:           120.0,
				BeatNumber:    3,
			},
			{
				StartPosition: 10.0,
				Bpm:           120.0,
				BeatNumber:    0,
			},
		},
		HotCues: []lib.HotCue{
			{
				Position: 8, // Sparse position
				Name:     "End Cue",
				Offset:   180.0,
				Color:    "#FFFFFF",
			},
		},
		Loops: []lib.Loop{
			{
				Position: 8, // Sparse position
				Name:     "Outro Loop",
				Start:    120.0,
				End:      150.0,
				Color:    "#FFFFFF",
			},
		},
	}

	err = db.UpdatePerformanceData(ctx, perfData)
	require.NoError(t, err)

	fetched, err := db.GetPerformanceData(ctx, trackID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, fetched.BeatGrid[0].StartPosition, 0.0)
	assert.Len(t, fetched.HotCues, 1)
	assert.Equal(t, 8, fetched.HotCues[0].Position)
	assert.Len(t, fetched.Loops, 1)
	assert.Equal(t, 8, fetched.Loops[0].Position)

	// Update with raw blobs directly populated
	perfData.BeatDataBlob = fetched.BeatDataBlob
	perfData.QuickCuesBlob = fetched.QuickCuesBlob
	perfData.LoopsBlob = fetched.LoopsBlob
	err = db.UpdatePerformanceData(ctx, perfData)
	assert.NoError(t, err)
}

func TestPlaylistAndEntity_DirectOrderingEdgeCases(t *testing.T) {
	db, ctx := setupTestDB(t)

	// 1. Single playlist in DB (tests orderPlaylistSiblings len <= 1)
	p1, err := db.CreatePlaylist(ctx, "Only Playlist", 0, false)
	require.NoError(t, err)

	// GetPlaylistHierarchy with 1 playlist and 0 tracks
	nodes, err := db.GetPlaylistHierarchy(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "Only Playlist", nodes[0].Playlist.Title)

	// 2. Add single track to playlist (tests orderPlaylistEntities len <= 1)
	t1, err := db.InsertTrack(ctx, &engine.Track{Title: "Solo Track", Path: "solo.mp3"})
	require.NoError(t, err)

	_, err = db.AddTrackToPlaylist(ctx, p1.ID, t1)
	require.NoError(t, err)

	entities, err := db.GetPlaylistEntities(ctx, p1.ID)
	require.NoError(t, err)
	require.Len(t, entities, 1)

	// 3. Get all tracks and find by ID/path/title
	all, err := db.GetAllTracks(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	// 4. Disconnected sibling playlists and entities in database
	p2, err := db.CreatePlaylist(ctx, "P2", 0, false)
	require.NoError(t, err)
	p3, err := db.CreatePlaylist(ctx, "P3", 0, false)
	require.NoError(t, err)
	// Break sibling chain so p3 is not linked to p2
	_, err = db.MDB().ExecContext(ctx, `UPDATE Playlist SET nextListId = 0 WHERE id = ?`, p2.ID)
	require.NoError(t, err)

	nodesAll, err := db.GetPlaylistHierarchy(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, nodesAll)

	// Add disconnected entities in p3
	t2, _ := db.InsertTrack(ctx, &engine.Track{Title: "T2", Path: "t2.mp3"})
	t3, _ := db.InsertTrack(ctx, &engine.Track{Title: "T3", Path: "t3.mp3"})
	_, err = db.MDB().ExecContext(ctx, `INSERT INTO PlaylistEntity (id, listId, trackId, nextEntityId) VALUES (101, ?, ?, 0)`, p3.ID, t2)
	require.NoError(t, err)
	_, err = db.MDB().ExecContext(ctx, `INSERT INTO PlaylistEntity (id, listId, trackId, nextEntityId) VALUES (102, ?, ?, 0)`, p3.ID, t3)
	require.NoError(t, err)

	p3Entities, err := db.GetPlaylistEntities(ctx, p3.ID)
	require.NoError(t, err)
	assert.Len(t, p3Entities, 2)
}

func TestImport_CyclicPlaylistEntitiesFallback(t *testing.T) {
	tempDir := t.TempDir()
	db, err := engine.Open(tempDir, false)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, db.CreateSchema(ctx))

	t1, err := db.InsertTrack(ctx, &engine.Track{Title: "Track A", Path: "a.mp3"})
	require.NoError(t, err)
	t2, err := db.InsertTrack(ctx, &engine.Track{Title: "Track B", Path: "b.mp3"})
	require.NoError(t, err)

	pl, err := db.CreatePlaylist(ctx, "Cyclic Playlist", 0, false)
	require.NoError(t, err)

	// Make playlists cyclic to test sortPlaylistsHelper fallback
	pl2, err := db.CreatePlaylist(ctx, "Cyclic Playlist 2", 0, false)
	require.NoError(t, err)
	_, err = db.MDB().ExecContext(ctx, `UPDATE Playlist SET nextListId = ? WHERE id = ?`, pl2.ID, pl.ID)
	require.NoError(t, err)
	_, err = db.MDB().ExecContext(ctx, `UPDATE Playlist SET nextListId = ? WHERE id = ?`, pl.ID, pl2.ID)
	require.NoError(t, err)

	// Insert cyclic entities directly into PlaylistEntity: e1 -> e2, e2 -> e1
	_, err = db.MDB().ExecContext(ctx, `INSERT INTO PlaylistEntity (id, listId, trackId, nextEntityId) VALUES (10, ?, ?, 20)`, pl.ID, t1)
	require.NoError(t, err)
	_, err = db.MDB().ExecContext(ctx, `INSERT INTO PlaylistEntity (id, listId, trackId, nextEntityId) VALUES (20, ?, ?, 10)`, pl.ID, t2)
	require.NoError(t, err)

	require.NoError(t, db.Close())

	// Import should gracefully fallback without crashing
	imported, err := engine.Import(tempDir, engine.ImportOptions{PreserveOriginalPaths: true})
	require.NoError(t, err)
	require.Len(t, imported.Playlists, 2)
	assert.Len(t, imported.Playlists[0].Songs, 2)
}

func TestGetPlaylistTracks_SkippingMissingTrack(t *testing.T) {
	db, ctx := setupTestDB(t)

	pl, err := db.CreatePlaylist(ctx, "PL With Ghost", 0, false)
	require.NoError(t, err)

	// Add entity with non-existent track directly
	_, err = db.MDB().ExecContext(ctx, `INSERT INTO PlaylistEntity (listId, trackId, nextEntityId) VALUES (?, 99999, 0)`, pl.ID)
	require.NoError(t, err)

	tracks, err := db.GetPlaylistTracks(ctx, pl.ID)
	require.NoError(t, err)
	assert.Empty(t, tracks) // skipped missing track
}

func TestPerformanceData_StructuredOnlyUpdate(t *testing.T) {
	db, ctx := setupTestDB(t)

	tID, err := db.InsertTrack(ctx, &engine.Track{Title: "Main Cue Only Track", Path: "maincue.mp3"})
	require.NoError(t, err)

	perf := &engine.PerformanceData{
		TrackID: tID,
		MainCue: 5.25,
	}

	err = db.UpdatePerformanceData(ctx, perf)
	require.NoError(t, err)

	fetched, err := db.GetPerformanceData(ctx, tID)
	require.NoError(t, err)
	assert.Equal(t, 5.25, fetched.MainCue)
}
