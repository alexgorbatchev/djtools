package rbxml

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nateranda/djtools/lib"
	"github.com/nateranda/djtools/musicalkey"
)

func importConvert(djPlaylists *djPlaylists) (lib.Library, error) {
	var library lib.Library
	refs, err := importReferences(djPlaylists)
	if err != nil {
		return lib.Library{}, err
	}
	library.Songs, err = importConvertSong(djPlaylists, refs.songIDs)
	if err != nil {
		return lib.Library{}, err
	}
	library.Playlists, err = importConvertPlaylists(djPlaylists, refs)
	if err != nil {
		return lib.Library{}, err
	}

	return library, nil
}

func importConvertSong(djPlaylists *djPlaylists, songIDs []int) ([]lib.Song, error) {
	var songs []lib.Song
	for i, track := range djPlaylists.Collection.Tracks {
		dateModified, err := dateToUnix(track.DateModified)
		if err != nil {
			return nil, fmt.Errorf("error converting song: %w", err)
		}
		dateAdded, err := dateToUnix(track.DateAdded)
		if err != nil {
			return nil, fmt.Errorf("error converting song: %w", err)
		}
		lastPlayed, err := dateToUnix(track.LastPlayed)
		if err != nil {
			return nil, fmt.Errorf("error converting song: %w", err)
		}
		rating, err := importConvertRating(track.Rating)
		if err != nil {
			return nil, fmt.Errorf("error converting song: %w", err)
		}
		path, err := uriToPath(track.Location)
		if err != nil {
			return nil, fmt.Errorf("error converting song: %w", err)
		}
		key, err := tonalityToInt(track.Tonality)
		if err != nil {
			return nil, fmt.Errorf("error converting song: %w", err)
		}
		markers := importConvertGrid(track)
		cues, loops := importConvertCuesLoops(track)
		song := lib.Song{
			SongID:       songIDs[i],
			Title:        track.Name,
			Artist:       track.Artist,
			Composer:     track.Composer,
			Album:        track.Album,
			Grouping:     track.Grouping,
			Genre:        track.Genre,
			Filetype:     track.Kind,
			Size:         int(track.Size),
			Length:       float32(track.TotalTime),
			TrackNumber:  int(track.TrackNumber),
			Year:         int(track.Year),
			Bpm:          float32(track.AverageBpm),
			DateModified: dateModified,
			DateAdded:    dateAdded,
			Bitrate:      int(track.BitRate),
			SampleRate:   track.SampleRate,
			Comment:      track.Comments,
			PlayCount:    int(track.PlayCount),
			LastPlayed:   lastPlayed,
			Rating:       rating,
			Path:         path,
			Remixer:      track.Remixer,
			Key:          key,
			Label:        track.Label,
			Mix:          track.Mix,
			Color:        track.Colour,
			Grid:         markers,
			Cues:         cues,
			Loops:        loops,
		}
		songs = append(songs, song)
	}
	return songs, nil
}

func importConvertPlaylists(djPlaylists *djPlaylists, refs trackReferences) ([]lib.Playlist, error) {
	if djPlaylists.Playlists.Node.Nodes == nil {
		return nil, nil
	}

	var id int = 1 // playlists don't have ids, so they will be assigned incrementally

	return importConvertSubPlaylists(djPlaylists.Playlists.Node.Nodes, &id, refs)
}

func importConvertSubPlaylists(rbPlaylists *[]node, id *int, refs trackReferences) ([]lib.Playlist, error) {
	var playlists []lib.Playlist
	for _, node := range *rbPlaylists {
		if node.NodeType != 0 && node.NodeType != 1 {
			return nil, fmt.Errorf("playlist %q has unsupported node type %d", node.Name, node.NodeType)
		}
		if node.NodeType == 1 && node.KeyType != 0 && node.KeyType != 1 {
			return nil, fmt.Errorf("playlist %q has unsupported key type %d", node.Name, node.KeyType)
		}
		playlist := lib.Playlist{
			PlaylistID: *id,
			Name:       node.Name,
		}
		*id++ // increment id

		// populate direct songs if any
		if node.Tracks != nil {
			var songs []int
			for _, track := range *node.Tracks {
				songID, err := refs.resolve(node.KeyType, track.Key)
				if err != nil {
					return nil, fmt.Errorf("playlist %q: %w", node.Name, err)
				}
				songs = append(songs, songID)
			}
			playlist.Songs = songs
		}

		// populate sub-playlists if any (do not concatenate into parent playlist)
		if node.Nodes != nil {
			var err error
			playlist.SubPlaylists, err = importConvertSubPlaylists(node.Nodes, id, refs)
			if err != nil {
				return nil, err
			}
		}
		playlists = append(playlists, playlist)
	}

	return playlists, nil
}

func importConvertGrid(track track) []lib.Marker {
	if track.Tempo == nil {
		return nil
	}
	var markers []lib.Marker
	for _, tempo := range *track.Tempo {
		markers = append(markers, lib.Marker{
			StartPosition: tempo.Inizio,
			Bpm:           tempo.Bpm,
			BeatNumber:    int(tempo.Battito) - 1, // BeatNumber is 0-indexed
		})
	}
	return markers
}

func importConvertCuesLoops(track track) ([]lib.HotCue, []lib.Loop) {
	if track.PositionMark == nil {
		return nil, nil
	}
	var cues []lib.HotCue
	var loops []lib.Loop
	for _, mark := range *track.PositionMark {
		if mark.MarkType == 0 {
			cues = append(cues, lib.HotCue{
				Name:     mark.Name,
				Offset:   mark.Start,
				Position: int(mark.Num) + 1, // Position is 1-indexed
			})
		}
		if mark.MarkType == 4 {
			loops = append(loops, lib.Loop{
				Name:     mark.Name,
				Start:    mark.Start,
				End:      mark.End,
				Position: int(mark.Num) + 1, // Position is 1-indexed
			})
		}
	}
	return cues, loops
}

func dateToUnix(date string) (int, error) {
	if date == "" {
		return 0, nil
	}
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0, fmt.Errorf("error converting date to Unix timestamp: %w", err)
	}
	return int(t.Unix()), nil
}

func uriToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("error converting URI to filepath: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "file") || u.Opaque != "" {
		return "", fmt.Errorf("location must be an absolute file URI")
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return "", fmt.Errorf("remote file URI authorities are unsupported")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(uri, "#") {
		return "", fmt.Errorf("file URI must not contain credentials, a query, or a fragment")
	}
	// URL.Path is already decoded. A second unescape would alter literal percent sequences.
	path := u.Path
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("file URI contains a null byte")
	}
	if runtime.GOOS == "windows" {
		if len(path) < 4 || path[0] != '/' || path[2] != ':' || path[3] != '/' || strings.Contains(path, `\`) {
			return "", fmt.Errorf("file URI must identify an absolute native drive path")
		}
		path = path[1:]
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("file URI must identify an absolute native path")
	}
	return path, nil
}

func tonalityToInt(tonality string) (int, error) {
	if strings.TrimSpace(tonality) == "" {
		return musicalkey.None, nil
	}
	key, err := musicalkey.Parse(tonality)
	if err != nil {
		return 0, fmt.Errorf("converting tonality: %w", err)
	}
	return int(key), nil
}

func importConvertRating(rating int32) (int, error) {
	switch rating {
	case 0:
		return 0, nil
	case 51:
		return 20, nil
	case 102:
		return 40, nil
	case 153:
		return 60, nil
	case 204:
		return 80, nil
	case 255:
		return 100, nil
	}
	if rating >= 1 && rating <= 5 {
		return int(rating * 20), nil
	}
	if rating > 0 && rating <= 100 {
		return int(rating), nil
	}
	return 0, nil
}
