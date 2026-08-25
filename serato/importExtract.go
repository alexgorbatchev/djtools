package serato

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/dhowden/tag"
	"github.com/nateranda/djtools/lib"
)

func importExtract(path string) (lib.Library, error) {
	crates, err := ExtractCrates(path)
	if err != nil {
		return lib.Library{}, err
	}

	var library lib.Library
	pathSongMap := make(map[string]int) // path -> songID
	songIDCounter := 1

	for crateIdx, crate := range crates {
		var plSongIDs []int

		for _, trackPath := range crate.Paths {
			songID, exists := pathSongMap[trackPath]
			if !exists {
				songID = songIDCounter
				songIDCounter++
				pathSongMap[trackPath] = songID

				// Attempt extracting tags from song file if accessible
				s, err := ExtractSong(trackPath)
				if err != nil {
					// Fallback to basic song from path
					filename := filepath.Base(trackPath)
					ext := strings.TrimPrefix(filepath.Ext(trackPath), ".")
					title := strings.TrimSuffix(filename, filepath.Ext(filename))

					library.Songs = append(library.Songs, lib.Song{
						SongID:   songID,
						Title:    title,
						Path:     trackPath,
						Filetype: ext,
					})
				} else {
					ext := strings.TrimPrefix(filepath.Ext(trackPath), ".")
					title := s.Tags.Title
					if title == "" {
						title = strings.TrimSuffix(filepath.Base(trackPath), filepath.Ext(trackPath))
					}
					library.Songs = append(library.Songs, lib.Song{
						SongID:      songID,
						Title:       title,
						Artist:      s.Tags.Artist,
						Album:       s.Tags.Album,
						Composer:    s.Tags.Composer,
						Genre:       s.Tags.Genre,
						Year:        s.Tags.Year,
						TrackNumber: s.Tags.TrackNumber,
						Bpm:         float32(s.Tags.BPM),
						Comment:     s.Tags.Comment,
						Path:        trackPath,
						Filetype:    ext,
						Cues:        s.Cues,
						Loops:       s.Loops,
					})
				}
			}
			plSongIDs = append(plSongIDs, songID)
		}

		library.Playlists = append(library.Playlists, lib.Playlist{
			PlaylistID: crateIdx + 1,
			Name:       crate.Filename,
			Songs:      plSongIDs,
		})
	}

	return library, nil
}

// ExtractCrates finds and extracts all .crate files under a base path or Subcrates directory.
func ExtractCrates(path string) ([]Crate, error) {
	cratePaths, err := listCrateFiles(path)
	if err != nil {
		return nil, err
	}
	var crates []Crate
	for _, cp := range cratePaths {
		crate, err := ReadCrate(cp)
		if err != nil {
			return nil, err
		}
		crates = append(crates, *crate)
	}
	sort.Slice(crates, func(i, j int) bool {
		return crates[i].Filename < crates[j].Filename
	})
	return crates, nil
}

// ReadCrate reads and parses a Serato .crate file.
func ReadCrate(path string) (*Crate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading crate file %q: %w", path, err)
	}
	crate, err := DecodeCrate(data)
	if err != nil {
		return nil, fmt.Errorf("decoding crate file %q: %w", path, err)
	}
	pathWithoutExt := path[:len(path)-len(filepath.Ext(path))]
	crate.Filename = filepath.Base(pathWithoutExt)
	return crate, nil
}

// WriteCrate writes a Crate struct to a binary .crate file on disk.
func WriteCrate(crate *Crate, path string) error {
	data, err := EncodeCrate(crate)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory for crate %q: %w", path, err)
	}
	return os.WriteFile(path, data, 0644)
}

// DecodeCrate decodes raw binary data from a .crate file into a Crate struct.
func DecodeCrate(file []byte) (*Crate, error) {
	crate := &Crate{}
	for len(file) > 0 {
		var err error
		file, err = extractCrateEntry(crate, file)
		if err != nil {
			return nil, err
		}
	}
	return crate, nil
}

// EncodeCrate serializes a Crate struct into the binary Serato format.
func EncodeCrate(crate *Crate) ([]byte, error) {
	var buf bytes.Buffer

	// Version
	version := crate.Version
	if version == "" {
		version = "1.0/8.0"
	}
	vBytes := stringToUTF16(version)
	writeTaggedChunk(&buf, "vrsn", vBytes)

	// Tracks
	for _, p := range crate.Paths {
		cleanPath := strings.TrimPrefix(p, string(filepath.Separator))
		pBytes := stringToUTF16(cleanPath)
		var otrkBuf bytes.Buffer
		writeTaggedChunk(&otrkBuf, "ptrk", pBytes)
		writeTaggedChunk(&buf, "otrk", otrkBuf.Bytes())
	}

	return buf.Bytes(), nil
}

func writeTaggedChunk(buf *bytes.Buffer, tag string, data []byte) {
	buf.WriteString(tag)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	buf.Write(lenBuf[:])
	buf.Write(data)
}

func extractCrateEntry(c *Crate, file []byte) ([]byte, error) {
	if len(file) < 4 {
		return nil, fmt.Errorf("file too short to extract key")
	}
	key := string(file[:4])
	file = file[4:]

	if len(file) < 4 {
		return nil, fmt.Errorf("file too short to extract chunk length")
	}
	length := int(binary.BigEndian.Uint32(file[:4]))
	file = file[4:]

	if len(file) < length {
		return nil, fmt.Errorf("file too short to extract value: need %d bytes, got %d", length, len(file))
	}
	valueBytes := file[:length]
	file = file[length:]

	switch key {
	case "otrk":
		innerCrate := &Crate{}
		for len(valueBytes) > 0 {
			var err error
			valueBytes, err = extractCrateEntry(innerCrate, valueBytes)
			if err != nil {
				return nil, err
			}
		}
		c.Paths = append(c.Paths, innerCrate.Paths...)
		return file, nil
	case "vrsn":
		version, err := utf16ToString(valueBytes)
		if err != nil {
			return nil, err
		}
		c.Version = version
		return file, nil
	case "ptrk":
		path, err := utf16ToString(valueBytes)
		if err != nil {
			return nil, err
		}
		c.Paths = append(c.Paths, string(filepath.Separator)+path)
		return file, nil
	}

	// Skip standard unrecognized Serato tags safely
	switch key[:1] {
	case "o", "t", "p", "u", "s", "b":
		return file, nil
	}

	return nil, fmt.Errorf("key %s not of supported type", key)
}

// ExtractSong extracts metadata, GEOB tags, and cues from an audio file at path.
func ExtractSong(path string) (Song, error) {
	var s Song
	file, err := os.Open(path)
	if err != nil {
		return Song{}, fmt.Errorf("error opening audio file: %w", err)
	}
	defer file.Close()

	s.Path = path

	metadata, err := tag.ReadFrom(file)
	if err != nil {
		return Song{}, fmt.Errorf("error reading audio metadata: %w", err)
	}

	s.Tags.Title = metadata.Title()
	s.Tags.Album = metadata.Album()
	s.Tags.Artist = metadata.Artist()
	s.Tags.Composer = metadata.Composer()
	s.Tags.Genre = metadata.Genre()
	s.Tags.Year = metadata.Year()
	s.Tags.Comment = metadata.Comment()
	s.Tags.TrackNumber, _ = metadata.Track()

	raw := metadata.Raw()
	if raw != nil {
		for key, val := range raw {
			if strings.HasPrefix(key, "GEOB") {
				if valueBytes, ok := val.([]byte); ok && len(valueBytes) > 0 {
					desc, data := extractGEOBData(valueBytes)
					name := key
					if desc != "" {
						name = fmt.Sprintf("%s:%s", key, desc)
					}
					s.GEOBs = append(s.GEOBs, GEOB{
						Name:  name,
						Value: valueBytes,
					})

					// Parse Serato Markers if present in data or valueBytes
					cues, loops, err := ParseSeratoMarkers(data)
					if err == nil {
						s.Cues = append(s.Cues, cues...)
						s.Loops = append(s.Loops, loops...)
					} else {
						cues, loops, err = ParseSeratoMarkers(valueBytes)
						if err == nil {
							s.Cues = append(s.Cues, cues...)
							s.Loops = append(s.Loops, loops...)
						}
					}
				}
			}
		}
		sort.Slice(s.GEOBs, func(i, j int) bool {
			return s.GEOBs[i].Name < s.GEOBs[j].Name
		})
	}

	return s, nil
}

func extractGEOBData(geobRaw []byte) (string, []byte) {
	if len(geobRaw) < 4 {
		return "", geobRaw
	}
	idx := 1 // skip encoding byte
	// Skip mime
	for idx < len(geobRaw) && geobRaw[idx] != 0 {
		idx++
	}
	idx++ // skip null
	// Skip filename
	for idx < len(geobRaw) && geobRaw[idx] != 0 {
		idx++
	}
	idx++ // skip null
	// Read description
	descStart := idx
	for idx < len(geobRaw) && geobRaw[idx] != 0 {
		idx++
	}
	desc := ""
	if descStart < len(geobRaw) && idx <= len(geobRaw) {
		desc = string(geobRaw[descStart:idx])
	}
	idx++ // skip null
	if idx < len(geobRaw) {
		return desc, geobRaw[idx:]
	}
	return desc, nil
}

// ParseSeratoMarkers extracts cue points and loops from raw Serato GEOB marker bytes.
func ParseSeratoMarkers(data []byte) ([]lib.HotCue, []lib.Loop, error) {
	if len(data) == 0 {
		return nil, nil, errors.New("empty marker data")
	}

	// If data is base64 encoded (Serato Markers2 often base64-encodes its payload)
	raw := data
	if decoded, err := base64.StdEncoding.DecodeString(string(data)); err == nil {
		raw = decoded
	}

	var cues []lib.HotCue
	var loops []lib.Loop

	// Marker parsing loop
	i := 0
	for i+8 <= len(raw) {
		entryType := string(raw[i : i+4])
		length := int(binary.BigEndian.Uint32(raw[i+4 : i+8]))
		i += 8
		if i+length > len(raw) || length < 0 {
			break
		}
		entryData := raw[i : i+length]
		i += length

		switch entryType {
		case "CUE\x00", "CUE ":
			if len(entryData) >= 8 {
				pos := int(entryData[0]) + 1
				offsetMs := binary.BigEndian.Uint32(entryData[1:5])
				cues = append(cues, lib.HotCue{
					Position: pos,
					Offset:   float64(offsetMs) / 1000.0,
					Name:     fmt.Sprintf("Cue %d", pos),
					Color:    "#00FFFF",
				})
			}
		case "LOOP":
			if len(entryData) >= 12 {
				pos := int(entryData[0]) + 1
				startMs := binary.BigEndian.Uint32(entryData[1:5])
				endMs := binary.BigEndian.Uint32(entryData[5:9])
				loops = append(loops, lib.Loop{
					Position: pos,
					Start:    float64(startMs) / 1000.0,
					End:      float64(endMs) / 1000.0,
					Name:     fmt.Sprintf("Loop %d", pos),
					Color:    "#00FF00",
				})
			}
		}
	}

	return cues, loops, nil
}

func stringToUTF16(s string) []byte {
	runes := []rune(s)
	u16 := utf16.Encode(runes)
	buf := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.BigEndian.PutUint16(buf[i*2:], v)
	}
	return buf
}

func utf16ToString(data []byte) (string, error) {
	if len(data)%2 != 0 {
		return "", fmt.Errorf("invalid UTF-16 byte slice length: %d", len(data))
	}

	uint16s := make([]uint16, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		uint16s[i/2] = binary.BigEndian.Uint16(data[i : i+2])
	}

	runes := utf16.Decode(uint16s)
	return string(runes), nil
}

func listCrateFiles(basePath string) ([]string, error) {
	candidates := []string{
		filepath.Join(basePath, "_Serato_", "Subcrates"),
		filepath.Join(basePath, "Subcrates"),
		basePath,
	}

	for _, p := range candidates {
		entries, err := os.ReadDir(p)
		if err == nil {
			var files []string
			for _, entry := range entries {
				if !entry.IsDir() && filepath.Ext(entry.Name()) == ".crate" {
					files = append(files, filepath.Join(p, entry.Name()))
				}
			}
			if len(files) > 0 {
				return files, nil
			}
		}
	}

	return nil, fmt.Errorf("no .crate files found in %q", basePath)
}
