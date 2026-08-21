package rbxml

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/nateranda/djtools/lib"
)

func unixToDate(date int, options ExportOptions) string {
	if date == 0 {
		return ""
	}
	t := time.Unix(int64(date), 0)
	if options.UseUTC {
		t = t.UTC()
	}
	return t.Format("2006-01-02")
}

func pathToURI(path string) string {
	cleaned := filepath.ToSlash(filepath.Clean(path))
	cleaned = strings.TrimPrefix(cleaned, "/")

	parts := strings.Split(cleaned, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	encodedPath := strings.Join(parts, "/")

	uri := "file://localhost/" + encodedPath
	if len(uri) > 255 {
		ext := filepath.Ext(cleaned)
		filename := filepath.Base(cleaned)
		prefix := "file://localhost/Contents/Music/"
		maxFilenameLen := 255 - len(prefix) - len(ext)
		if maxFilenameLen > 5 {
			nameNoExt := strings.TrimSuffix(filename, ext)
			if len(nameNoExt) > maxFilenameLen {
				nameNoExt = nameNoExt[:maxFilenameLen]
			}
			return prefix + url.PathEscape(nameNoExt+ext)
		}
	}

	return uri
}

func exportConvertTonality(key int) (string, error) {
	switch key {
	case 0:
		return "8B", nil
	case 1:
		return "8A", nil
	case 2:
		return "9B", nil
	case 3:
		return "9A", nil
	case 4:
		return "10B", nil
	case 5:
		return "10A", nil
	case 6:
		return "11B", nil
	case 7:
		return "11A", nil
	case 8:
		return "12B", nil
	case 9:
		return "12A", nil
	case 10:
		return "1B", nil
	case 11:
		return "1A", nil
	case 12:
		return "2B", nil
	case 13:
		return "2A", nil
	case 14:
		return "3B", nil
	case 15:
		return "3A", nil
	case 16:
		return "4B", nil
	case 17:
		return "4A", nil
	case 18:
		return "5B", nil
	case 19:
		return "5A", nil
	case 20:
		return "6B", nil
	case 21:
		return "6A", nil
	case 22:
		return "7B", nil
	case 23:
		return "7A", nil
	}
	return "", nil // Fallback to empty string for unknown/invalid key
}

func exportConvertRating(rating int) (int32, error) {
	switch rating {
	case 0:
		return 0, nil
	case 20:
		return 51, nil
	case 40:
		return 102, nil
	case 60:
		return 153, nil
	case 80:
		return 204, nil
	case 100:
		return 255, nil
	}
	if rating > 0 && rating <= 5 {
		return int32(rating * 51), nil
	}
	if rating > 0 && rating <= 255 {
		return int32(rating), nil
	}
	return 0, nil // Fallback to 0 rating
}

func exportConvertPositionMarks(song *lib.Song) []positionMark {
	var offset float64
	if song.Filetype == "mp3" {
		offset = 0.05
	} else {
		offset = 0
	}
	var positionMarks []positionMark
	// add cue point
	positionMarks = append(positionMarks, positionMark{
		MarkType: 0,
		Start:    song.Cue + offset,
		Num:      -1,
	})

	// add hot cues
	for _, cue := range song.Cues {
		positionMarks = append(positionMarks, positionMark{
			Name:     cue.Name,
			MarkType: 0,
			Start:    cue.Offset + offset,
			Num:      int32(cue.Position - 1),
		})
	}

	// add loops
	for _, loop := range song.Loops {
		positionMarks = append(positionMarks, positionMark{
			Name:     loop.Name,
			MarkType: 4,
			Start:    loop.Start + offset,
			End:      loop.End + offset,
			Num:      int32(loop.Position - 1),
		})
	}

	return positionMarks
}

func exportConvertGrid(song *lib.Song) []tempo {
	var tempos []tempo

	var offset float64
	if song.Filetype == "mp3" {
		offset = 0.05
	} else {
		offset = 0
	}

	for _, grid := range song.Grid {
		tempos = append(tempos, tempo{
			Inizio:  grid.StartPosition + offset,
			Bpm:     grid.Bpm,
			Metro:   "4/4",                      // assumed, may add time signature support later
			Battito: int32(grid.BeatNumber) + 1, // Battito is 1-indexed
		})
	}

	return tempos
}

func exportConvertSubPlaylists(playlist lib.Playlist) node {
	// If playlist has sub-playlists, it must be represented as a Folder node (Type 0)
	if len(playlist.SubPlaylists) > 0 {
		var children []node

		// If it ALSO has direct songs, place a Playlist node (Type 1) inside this Folder node
		if len(playlist.Songs) > 0 {
			var tracks []nodeTrack
			for _, id := range playlist.Songs {
				tracks = append(tracks, nodeTrack{Id: int32(id)})
			}
			children = append(children, node{
				NodeType: 1,
				Name:     playlist.Name,
				Entries:  int32(len(playlist.Songs)),
				KeyType:  0,
				Tracks:   &tracks,
			})
		}

		// Convert and append all sub-playlists as child nodes inside this Folder node
		for _, sub := range playlist.SubPlaylists {
			children = append(children, exportConvertSubPlaylists(sub))
		}

		return node{
			NodeType: 0,
			Name:     playlist.Name,
			Count:    int32(len(children)),
			KeyType:  0,
			Nodes:    &children,
		}
	}

	// Simple playlist with no sub-playlists (Type 1)
	var tracks []nodeTrack
	for _, id := range playlist.Songs {
		tracks = append(tracks, nodeTrack{Id: int32(id)})
	}
	return node{
		NodeType: 1,
		Name:     playlist.Name,
		Entries:  int32(len(playlist.Songs)),
		KeyType:  0,
		Tracks:   &tracks,
	}
}

func exportConvertPlaylist(library *lib.Library) node {
	var nodes []node
	for _, playlist := range library.Playlists {
		nodes = append(nodes, exportConvertSubPlaylists(playlist))
	}

	return node{
		NodeType: 0,
		Name:     "ROOT",
		Count:    int32(len(nodes)),
		KeyType:  0,
		Nodes:    &nodes,
	}
}

func exportConvertSong(library *lib.Library, options ExportOptions) ([]track, error) {
	var tracks []track
	for _, song := range library.Songs {
		rating, err := exportConvertRating(song.Rating)
		if err != nil {
			return nil, fmt.Errorf("error converting song rating: %v", err)
		}
		path := pathToURI(song.Path)
		tonality, err := exportConvertTonality(song.Key)
		if err != nil {
			return nil, fmt.Errorf("error converting song tonality: %v", err)
		}
		positionMarks := exportConvertPositionMarks(&song)
		tempos := exportConvertGrid(&song)

		bpm := float64(song.Bpm)
		if bpm <= 0 && song.BpmAnalyzed > 0 {
			bpm = song.BpmAnalyzed
		} else if bpm <= 0 && len(song.Grid) > 0 && song.Grid[0].Bpm > 0 {
			bpm = song.Grid[0].Bpm
		}

		tracks = append(tracks, track{
			TrackId:      song.SongID,
			Name:         song.Title,
			Artist:       song.Artist,
			Composer:     song.Composer,
			Album:        song.Album,
			Grouping:     song.Grouping,
			Genre:        song.Genre,
			Kind:         song.Filetype,
			Size:         int64(song.Size),
			TotalTime:    float64(song.Length), // make sure this is rounded?
			TrackNumber:  int32(song.TrackNumber),
			Year:         int32(song.Year),
			AverageBpm:   bpm,
			DateModified: unixToDate(song.DateModified, options),
			DateAdded:    unixToDate(song.DateAdded, options),
			BitRate:      int32(song.Bitrate),
			SampleRate:   song.SampleRate,
			Comments:     song.Comment,
			PlayCount:    int32(song.PlayCount),
			LastPlayed:   unixToDate(song.LastPlayed, options),
			Rating:       rating,
			Location:     path,
			Remixer:      song.Remixer,
			Tonality:     tonality,
			Label:        song.Label,
			Mix:          song.Mix,
			Colour:       song.Color,
			PositionMark: &positionMarks,
			Tempo:        &tempos,
		})
	}
	return tracks, nil
}

func exportConvert(library *lib.Library, options ExportOptions) (djPlaylists, error) {
	djPlaylists := djPlaylists{
		Version: "1.0.0",
		Product: product{
			Name:    "djtools",
			Version: version,
			Company: "djtools",
		},
	}
	var err error

	djPlaylists.Collection.Tracks, err = exportConvertSong(library, options)
	djPlaylists.Collection.Entries = int32(len(djPlaylists.Collection.Tracks))
	if err != nil {
		return djPlaylists, err
	}

	djPlaylists.Playlists = playlists{Node: exportConvertPlaylist(library)}

	return djPlaylists, nil
}
