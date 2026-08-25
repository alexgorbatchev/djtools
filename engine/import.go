package engine

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/nateranda/djtools/lib"
)

// Import converts an Engine DJ database directory into a lib.Library struct.
func Import(path string, importOptions ImportOptions) (lib.Library, error) {
	db, err := Open(path, true)
	if err != nil {
		return lib.Library{}, err
	}
	defer db.Close()

	ctx := context.Background()

	var enLib lib.Library

	// 1. Information
	info, err := db.getInformation(ctx)
	if err == nil {
		enLib.DatabaseUUID = info.UUID
		enLib.SchemaVersionMajor = info.SchemaVersionMajor
		enLib.SchemaVersionMinor = info.SchemaVersionMinor
		enLib.SchemaVersionPatch = info.SchemaVersionPatch
	}

	// 2. AlbumArt
	artList, err := db.GetAllAlbumArt(ctx)
	if err == nil {
		for _, art := range artList {
			enLib.AlbumArt = append(enLib.AlbumArt, lib.AlbumArt{
				ID:   int(art.ID),
				Hash: art.Hash,
				Data: art.Data,
			})
		}
	}

	// 3. Tracks
	tracks, err := db.GetAllTracks(ctx)
	if err != nil {
		return lib.Library{}, fmt.Errorf("error extracting track data: %w", err)
	}

	for _, t := range tracks {
		songPath := t.Path
		if !importOptions.PreserveOriginalPaths {
			absPath, err := fullPathFromRelativePath(path, t.Path)
			if err != nil {
				return lib.Library{}, fmt.Errorf("error converting song path: %w", err)
			}
			songPath = absPath
		}

		songID := int(t.ID)
		if t.OriginTrackID > 0 {
			songID = int(t.OriginTrackID)
		}

		dateAdded := int(time.Time{}.Unix())
		if t.DateAdded != nil {
			dateAdded = int(t.DateAdded.Unix())
		}
		dateModified := int(time.Time{}.Unix())
		if t.LastEditTime != nil {
			dateModified = int(t.LastEditTime.Unix())
		}
		dateCreated := int(time.Time{}.Unix())
		if t.DateCreated != nil {
			dateCreated = int(t.DateCreated.Unix())
		}
		timeLastPlayed := int(time.Time{}.Unix())
		if t.TimeLastPlayed != nil {
			timeLastPlayed = int(t.TimeLastPlayed.Unix())
		}

		enLib.Songs = append(enLib.Songs, lib.Song{
			SongID:             songID,
			Title:              t.Title,
			Artist:             t.Artist,
			Composer:           t.Composer,
			Album:              t.Album,
			Genre:              t.Genre,
			Filetype:           t.FileType,
			Size:               int(t.FileBytes),
			Length:             float32(t.Length),
			Year:               int(t.Year),
			Bpm:                float32(t.BPM),
			BpmAnalyzed:        t.BPMAnalyzed,
			DateAdded:          dateAdded,
			DateModified:       dateModified,
			DateCreated:        dateCreated,
			Bitrate:            int(t.Bitrate),
			Comment:            t.Comment,
			Rating:             int(t.Rating),
			Path:               songPath,
			Remixer:            t.Remixer,
			Key:                int(t.Key),
			Label:              t.Label,
			AlbumArtID:         int(t.AlbumArtID),
			TimeLastPlayed:     timeLastPlayed,
			IsPlayed:           t.IsPlayed,
			IsAnalyzed:         t.IsAnalyzed,
			PlayedIndicator:    int(t.PlayedIndicator),
			StreamingSource:    t.StreamingSource,
			URI:                t.URI,
			IsBeatGridLocked:   t.IsBeatGridLocked,
			OriginDatabaseUUID: t.OriginDatabaseUUID,
			OriginTrackID:      int(t.OriginTrackID),
			StreamingFlags:     int(t.StreamingFlags),
			ExplicitLyrics:     t.ExplicitLyrics,
			AlbumArtSourceHash: t.AlbumArtSourceHash,
		})
	}

	// 4. PerformanceData
	songMap := make(map[int]*lib.Song, len(enLib.Songs))
	for i := range enLib.Songs {
		songMap[enLib.Songs[i].SongID] = &enLib.Songs[i]
	}

	for _, t := range tracks {
		song := songMap[int(t.ID)]
		if song == nil {
			if t.OriginTrackID > 0 {
				song = songMap[int(t.OriginTrackID)]
			}
			if song == nil {
				continue
			}
		}

		perf, err := db.GetPerformanceData(ctx, t.ID)
		if err != nil {
			continue
		}

		if len(perf.BeatDataBlob) == 0 && len(perf.BeatGrid) == 0 {
			// Check if corrupt
			if perf.BeatDataBlob == nil {
				song.Corrupt = true
				continue
			}
		}

		song.SampleRate = perf.SampleRate
		song.OverviewWaveFormData = perf.OverviewWaveFormData
		song.TrackData = perf.TrackData
		song.ActiveOnLoadLoops = perf.ActiveOnLoadLoops

		if len(perf.BeatDataBlob) >= 5 {
			decompBeat, err := qUncompress(perf.BeatDataBlob)
			if err == nil {
				bData, err := beatDataFromBlob(decompBeat)
				if err == nil {
					song.SampleRate = bData.sampleRate
					if importOptions.ImportOriginalGrids {
						song.Grid = gridFromBeatData(bData.sampleRate, bData.defaultBeatgrid)
					} else {
						song.Grid = gridFromBeatData(bData.sampleRate, bData.adjBeatgrid)
					}
				}
			}
		} else if len(perf.BeatGrid) > 0 {
			song.Grid = perf.BeatGrid
		}

		if len(perf.QuickCuesBlob) >= 5 {
			decompCues, err := qUncompress(perf.QuickCuesBlob)
			if err == nil {
				cData, err := cuesFromBlob(song.SampleRate, decompCues)
				if err == nil {
					song.Cues = cData.cues
					if importOptions.ImportOriginalCues {
						song.Cue = cData.cueOriginal
					} else {
						song.Cue = cData.cueModified
					}
				}
			}
		} else {
			song.Cues = perf.HotCues
			song.Cue = perf.MainCue
		}

		if len(perf.LoopsBlob) > 0 {
			loops, err := loopsFromBlob(song.SampleRate, perf.LoopsBlob)
			if err == nil {
				song.Loops = loops
			}
		} else {
			song.Loops = perf.Loops
		}
	}

	// 5. History from hm.db
	if db.hmDB != nil {
		history, _ := db.getHistory(ctx)
		for _, h := range history {
			if s, exists := songMap[int(h.id)]; exists {
				s.PlayCount = h.plays
				s.LastPlayed = h.lastPlayed
			}
		}
	}

	// 6. Playlists & Hierarchy
	playlists, err := db.GetAllPlaylists(ctx)
	if err == nil && len(playlists) > 0 {
		entities, _ := db.getAllPlaylistEntities(ctx)
		if err := convertPlaylists(&enLib, playlists, entities); err != nil {
			return lib.Library{}, err
		}
	}

	// 7. Smartlists
	smartlists, err := db.getSmartlists(ctx)
	if err == nil {
		for _, sl := range smartlists {
			enLib.Smartlists = append(enLib.Smartlists, lib.Smartlist{
				ListUUID:           sl.ListUUID,
				Title:              sl.Title,
				ParentPlaylistPath: sl.ParentPlaylistPath,
				NextPlaylistPath:   sl.NextPlaylistPath,
				NextListUUID:       sl.NextListUUID,
				Rules:              sl.Rules,
			})
		}
	}

	enLib.CheckCorruptedSongs()

	return enLib, nil
}

func (db *DB) getInformation(ctx context.Context) (InformationRecord, error) {
	cols, err := getTableColumns(db.mDB, "Information")
	if err != nil || len(cols) == 0 {
		return InformationRecord{}, nil
	}
	query := `SELECT id, uuid, schemaVersionMajor, schemaVersionMinor, schemaVersionPatch FROM Information LIMIT 1`
	var info InformationRecord
	err = db.mDB.QueryRowContext(ctx, query).Scan(&info.ID, &info.UUID, &info.SchemaVersionMajor, &info.SchemaVersionMinor, &info.SchemaVersionPatch)
	if err != nil {
		return InformationRecord{}, nil
	}
	return info, nil
}

type historyEntry struct {
	id         int64
	plays      int
	lastPlayed int
}

func (db *DB) getHistory(ctx context.Context) ([]historyEntry, error) {
	if db.hmDB == nil {
		return nil, nil
	}
	query := `SELECT Track.originTrackId, COUNT(HistorylistEntity.trackId), MAX(HistorylistEntity.startTime) 
		FROM Track JOIN HistorylistEntity ON Track.id=HistorylistEntity.trackId
		GROUP BY Track.originTrackId ORDER BY Track.originTrackId`

	rows, err := db.hmDB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []historyEntry
	for rows.Next() {
		var h historyEntry
		if err := rows.Scan(&h.id, &h.plays, &h.lastPlayed); err == nil {
			entries = append(entries, h)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating history rows: %w", err)
	}
	return entries, nil
}

func (db *DB) getAllPlaylistEntities(ctx context.Context) ([]PlaylistEntity, error) {
	query := `SELECT id, listId, trackId, nextEntityId FROM PlaylistEntity ORDER BY listId`
	rows, err := db.mDB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []PlaylistEntity
	for rows.Next() {
		var e PlaylistEntity
		if err := rows.Scan(&e.ID, &e.ListID, &e.TrackID, &e.NextEntityID); err == nil {
			results = append(results, e)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating all playlist entities: %w", err)
	}
	return results, nil
}

func (db *DB) getSmartlists(ctx context.Context) ([]SmartlistRecord, error) {
	cols, err := getTableColumns(db.mDB, "Smartlist")
	if err != nil || len(cols) == 0 {
		return nil, nil
	}
	query := `SELECT listUuid, title, parentPlaylistPath, nextPlaylistPath, nextListUuid, rules
		FROM Smartlist ORDER BY listUuid`
	rows, err := db.mDB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SmartlistRecord
	for rows.Next() {
		var sl SmartlistRecord
		var parentPath, nextPath, nextUUID sql.NullString
		if err := rows.Scan(&sl.ListUUID, &sl.Title, &parentPath, &nextPath, &nextUUID, &sl.Rules); err == nil {
			sl.ParentPlaylistPath = parentPath.String
			sl.NextPlaylistPath = nextPath.String
			sl.NextListUUID = nextUUID.String
			results = append(results, sl)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating smartlist rows: %w", err)
	}
	return results, nil
}

type playlistHelper struct {
	id           int
	title        string
	parentListId int
	nextListId   int
	songs        []int
}

func convertPlaylists(library *lib.Library, playlists []Playlist, playlistEntityList []PlaylistEntity) error {
	if len(playlists) == 0 {
		return nil
	}

	helpers := make([]playlistHelper, len(playlists))
	for i, p := range playlists {
		helpers[i] = playlistHelper{
			id:           int(p.ID),
			title:        p.Title,
			parentListId: int(p.ParentListID),
			nextListId:   int(p.NextListID),
		}
	}

	helpers, err := populatePlaylistsHelper(playlistEntityList, helpers)
	if err != nil {
		return err
	}

	parentPlaylistAddressMap := make(map[int]*lib.Playlist)
	var parentPlaylists []playlistHelper
	var playlistsNew []playlistHelper

	for _, playlist := range helpers {
		if playlist.parentListId == 0 {
			parentPlaylists = append(parentPlaylists, playlist)
		} else {
			playlistsNew = append(playlistsNew, playlist)
		}
	}

	parentPlaylists, err = sortPlaylistsHelper(parentPlaylists)
	if err != nil {
		return err
	}

	for _, playlist := range parentPlaylists {
		newPlaylist := lib.Playlist{
			Name:       playlist.title,
			PlaylistID: playlist.id,
			Songs:      playlist.songs,
		}
		library.Playlists = append(library.Playlists, newPlaylist)
		for i, pl := range library.Playlists {
			parentPlaylistAddressMap[pl.PlaylistID] = &library.Playlists[i]
		}
	}

	playlistsRemaining := playlistsNew
	for len(playlistsRemaining) > 0 {
		var parentPlaylistsNew []playlistHelper
		var playlistsNext []playlistHelper

		parentPlaylistIDMap := make(map[int]struct{})
		for _, pl := range parentPlaylists {
			parentPlaylistIDMap[pl.id] = struct{}{}
		}

		for _, pl := range playlistsRemaining {
			if _, exists := parentPlaylistIDMap[pl.parentListId]; exists {
				parentPlaylistsNew = append(parentPlaylistsNew, pl)
			} else {
				playlistsNext = append(playlistsNext, pl)
			}
		}

		parentPlaylistsNew, err = sortPlaylistsHelper(parentPlaylistsNew)
		if err != nil {
			return err
		}

		for _, pl := range parentPlaylistsNew {
			newPlaylist := lib.Playlist{
				Name:       pl.title,
				PlaylistID: pl.id,
				Songs:      pl.songs,
			}
			parentPlaylist := parentPlaylistAddressMap[pl.parentListId]
			if parentPlaylist != nil {
				parentPlaylist.SubPlaylists = append(parentPlaylist.SubPlaylists, newPlaylist)
				for i, sub := range parentPlaylist.SubPlaylists {
					parentPlaylistAddressMap[sub.PlaylistID] = &parentPlaylist.SubPlaylists[i]
				}
			}
		}

		playlistsRemaining = playlistsNext
		parentPlaylists = parentPlaylistsNew
	}

	return nil
}

func populatePlaylistsHelper(playlistEntityList []PlaylistEntity, playlists []playlistHelper) ([]playlistHelper, error) {
	if len(playlistEntityList) == 0 || len(playlists) == 0 {
		return playlists, nil
	}

	playlistMap := make(map[int]int)
	for i, playlist := range playlists {
		playlistMap[playlist.id] = i
	}

	playlistEntityMap := make(map[int64]*PlaylistEntity)
	for i, entity := range playlistEntityList {
		playlistEntityMap[entity.ID] = &playlistEntityList[i]
	}

	firstSongs, err := findFirstSongsHelper(playlistEntityList)
	if err != nil || len(firstSongs) == 0 {
		for _, entity := range playlistEntityList {
			if idx, ok := playlistMap[int(entity.ListID)]; ok {
				playlists[idx].songs = append(playlists[idx].songs, int(entity.TrackID))
			}
		}
		return playlists, nil
	}

	for _, track := range firstSongs {
		trackID := track.TrackID
		listID := track.ListID
		nextEntityID := track.NextEntityID

		if idx, ok := playlistMap[int(listID)]; ok {
			playlists[idx].songs = append(playlists[idx].songs, int(trackID))

			for range len(playlistEntityList) {
				if nextEntityID == 0 {
					break
				}
				nextEntity, ok := playlistEntityMap[nextEntityID]
				if !ok {
					break
				}
				trackID = nextEntity.TrackID
				nextEntityID = nextEntity.NextEntityID
				playlists[idx].songs = append(playlists[idx].songs, int(trackID))
			}
		}
	}

	return playlists, nil
}

func findFirstSongsHelper(playlistEntityList []PlaylistEntity) ([]PlaylistEntity, error) {
	if len(playlistEntityList) == 0 {
		return nil, nil
	}

	var firstSongs []PlaylistEntity
	nextEntityIDMap := make(map[int64]struct{})
	for _, entity := range playlistEntityList {
		if entity.NextEntityID != 0 {
			nextEntityIDMap[entity.NextEntityID] = struct{}{}
		}
	}

	for _, entity := range playlistEntityList {
		if _, exists := nextEntityIDMap[entity.ID]; !exists {
			firstSongs = append(firstSongs, entity)
		}
	}

	if len(firstSongs) == 0 {
		return nil, fmt.Errorf("NotFoundError: did not find any first songs")
	}
	return firstSongs, nil
}

func sortPlaylistsHelper(playlists []playlistHelper) ([]playlistHelper, error) {
	if len(playlists) == 0 {
		return playlists, nil
	}

	i, err := findFirstPlaylistHelper(playlists)
	if err != nil {
		return playlists, nil
	}

	playlistMap := make(map[int]int)
	for j, playlist := range playlists {
		playlistMap[playlist.id] = j
	}
	var playlistsSorted []playlistHelper
	for range playlists {
		j, exists := playlistMap[i]
		if !exists {
			break
		}
		playlistsSorted = append(playlistsSorted, playlists[j])
		i = playlists[j].nextListId
	}

	if len(playlistsSorted) < len(playlists) {
		return playlists, nil
	}

	return playlistsSorted, nil
}

func findFirstPlaylistHelper(playlists []playlistHelper) (int, error) {
	if len(playlists) == 0 {
		return 0, fmt.Errorf("NotFoundError: did not find the first playlist")
	}

	nextListIDMap := make(map[int]struct{})
	for _, playlist := range playlists {
		if playlist.nextListId != 0 {
			nextListIDMap[playlist.nextListId] = struct{}{}
		}
	}

	for _, playlist := range playlists {
		if _, exists := nextListIDMap[playlist.id]; !exists {
			return playlist.id, nil
		}
	}
	return 0, fmt.Errorf("NotFoundError: did not find the first playlist")
}

func fullPathFromRelativePath(basePath string, relativePath string) (string, error) {
	fullPath := filepath.Join(basePath, relativePath)
	return filepath.Abs(fullPath)
}
