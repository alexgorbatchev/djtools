package rbxml

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/nateranda/djtools/lib"
)

func importConvert(djPlaylists *djPlaylists) (lib.Library, error) {
	var library lib.Library
	var err error
	library.Songs, err = importConvertSong(djPlaylists)
	if err != nil {
		return lib.Library{}, err
	}
	library.Playlists = importConvertPlaylists(djPlaylists)

	return library, nil
}

func importConvertSong(djPlaylists *djPlaylists) ([]lib.Song, error) {
	var songs []lib.Song
	for _, track := range djPlaylists.Collection.Tracks {
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
		var corrupt bool
		if track.AverageBpm == 0 {
			corrupt = true
		}
		markers := importConvertGrid(track)
		cues, loops := importConvertCuesLoops(track)
		song := lib.Song{
			SongID:       track.TrackId,
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
			Corrupt:      corrupt,
		}
		songs = append(songs, song)
	}
	return songs, nil
}

func importConvertPlaylists(djPlaylists *djPlaylists) []lib.Playlist {
	if djPlaylists.Playlists.Node.Nodes == nil {
		return nil
	}

	var id int = 1 // playlists don't have ids, so they will be assigned incrementally

	return importConvertSubPlaylists(djPlaylists.Playlists.Node.Nodes, &id)
}

func importConvertSubPlaylists(rbPlaylists *[]node, id *int) []lib.Playlist {
	var playlists []lib.Playlist
	for _, node := range *rbPlaylists {
		playlist := lib.Playlist{
			PlaylistID: *id,
			Name:       node.Name,
		}
		*id++ // increment id

		// populate direct songs if any
		if node.Tracks != nil {
			var songs []int
			for _, track := range *node.Tracks {
				songs = append(songs, int(track.Id))
			}
			playlist.Songs = songs
		}

		// populate sub-playlists if any (do not concatenate into parent playlist)
		if node.Nodes != nil {
			playlist.SubPlaylists = importConvertSubPlaylists(node.Nodes, id)
		}
		playlists = append(playlists, playlist)
	}

	return playlists
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
	// Parse the URI
	parsedURI, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("error converting URI to filepath: %w", err)
	}

	// Ensure the scheme is "file"
	if parsedURI.Scheme != "file" {
		return "", fmt.Errorf("error converting URI to filepath: %w", err)
	}

	// Decode the path to handle any escaped characters
	path := parsedURI.Path
	path = strings.ReplaceAll(path, "/", string(filepath.Separator)) // Adjust for OS-specific separators
	return path, nil
}

func tonalityToInt(tonality string) (int, error) {
	switch tonality {
	case "":
		return 0, nil
	case "8B":
		return 0, nil
	case "8A":
		return 1, nil
	case "9B":
		return 2, nil
	case "9A":
		return 3, nil
	case "10B":
		return 4, nil
	case "10A":
		return 5, nil
	case "11B":
		return 6, nil
	case "11A":
		return 7, nil
	case "12B":
		return 8, nil
	case "12A":
		return 9, nil
	case "1B":
		return 10, nil
	case "1A":
		return 11, nil
	case "2B":
		return 12, nil
	case "2A":
		return 13, nil
	case "3B":
		return 14, nil
	case "3A":
		return 15, nil
	case "4B":
		return 16, nil
	case "4A":
		return 17, nil
	case "5B":
		return 18, nil
	case "5A":
		return 19, nil
	case "6B":
		return 20, nil
	case "6A":
		return 21, nil
	case "7B":
		return 22, nil
	case "7A":
		return 23, nil
	}
	return 0, nil // Fallback to 0 for unrecognized key
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
