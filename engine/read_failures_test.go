package engine_test

import (
	"testing"

	"github.com/nateranda/djtools/engine"
	"github.com/stretchr/testify/require"
)

func TestMalformedRowsFailReads(t *testing.T) {
	for _, test := range []struct {
		name string
		sql  string
		read func(*engine.DB) error
	}{
		{"artwork by id", `DROP TABLE AlbumArt; CREATE TABLE AlbumArt(id,hash,albumArt); INSERT INTO AlbumArt VALUES('bad','hash',x'01')`, func(db *engine.DB) error { _, err := db.GetAlbumArtByHash(t.Context(), "hash"); return err }},
		{"artwork list", `DROP TABLE AlbumArt; CREATE TABLE AlbumArt(id,hash,albumArt); INSERT INTO AlbumArt VALUES('bad','hash',x'01')`, func(db *engine.DB) error { _, err := db.GetAllAlbumArt(t.Context()); return err }},
		{"artwork missing column", `DROP TABLE AlbumArt; CREATE TABLE AlbumArt(id,hash)`, func(db *engine.DB) error { _, err := db.GetAllAlbumArt(t.Context()); return err }},
		{"playlist list", `INSERT INTO Playlist(id,title,parentListId) VALUES(1,'Broken','invalid')`, func(db *engine.DB) error { _, err := db.GetAllPlaylists(t.Context()); return err }},
		{"playlist lookup", `INSERT INTO Playlist(id,title,parentListId) VALUES(1,'Broken','invalid')`, func(db *engine.DB) error { _, err := db.GetPlaylistByID(t.Context(), 1); return err }},
		{"playlist hierarchy", `INSERT INTO Playlist(id,title,parentListId) VALUES(1,'Broken','invalid')`, func(db *engine.DB) error { _, err := db.GetPlaylistHierarchy(t.Context()); return err }},
		{"track list", `INSERT INTO Track(id,title,bpm) VALUES(1,'Broken','invalid')`, func(db *engine.DB) error { _, err := db.GetAllTracks(t.Context()); return err }},
		{"track lookup", `INSERT INTO Track(id,title,bpm) VALUES(1,'Broken','invalid')`, func(db *engine.DB) error { _, err := db.GetTrackByID(t.Context(), 1); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, ctx := setupTestDB(t)
			_, err := db.MDB().ExecContext(ctx, test.sql)
			require.NoError(t, err)
			require.Error(t, test.read(db))
		})
	}
}
