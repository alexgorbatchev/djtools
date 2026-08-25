package engine

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"time"

	"github.com/nateranda/djtools/lib"
)

// ImportOptions contains options for importing an Engine DJ library.
type ImportOptions struct {
	ImportOriginalGrids   bool
	ImportOriginalCues    bool
	PreserveOriginalPaths bool
}

// ExportOptions contains options for exporting an Engine DJ library.
type ExportOptions struct {
	Overwrite bool
}

// Track represents an Engine DJ Track table record.
type Track struct {
	ID                                    int64      `json:"id"`
	PlayOrder                             int64      `json:"playOrder"`
	Length                                int64      `json:"length"`
	BPM                                   int64      `json:"bpm"`
	Year                                  int64      `json:"year"`
	Path                                  string     `json:"path"`
	Filename                              string     `json:"filename"`
	Bitrate                               int64      `json:"bitrate"`
	BPMAnalyzed                           float64    `json:"bpmAnalyzed"`
	AlbumArtID                            int64      `json:"albumArtId"`
	FileBytes                             int64      `json:"fileBytes"`
	Title                                 string     `json:"title"`
	Artist                                string     `json:"artist"`
	Album                                 string     `json:"album"`
	Genre                                 string     `json:"genre"`
	Comment                               string     `json:"comment"`
	Label                                 string     `json:"label"`
	Composer                              string     `json:"composer"`
	Remixer                               string     `json:"remixer"`
	Key                                   int32      `json:"key"`
	Rating                                int64      `json:"rating"`
	AlbumArt                              string     `json:"albumArt"`
	TimeLastPlayed                        *time.Time `json:"timeLastPlayed,omitempty"`
	IsPlayed                              bool       `json:"isPlayed"`
	FileType                              string     `json:"fileType"`
	IsAnalyzed                            bool       `json:"isAnalyzed"`
	DateCreated                           *time.Time `json:"dateCreated,omitempty"`
	DateAdded                             *time.Time `json:"dateAdded,omitempty"`
	IsAvailable                           bool       `json:"isAvailable"`
	IsMetadataOfPackedTrackChanged        bool       `json:"isMetadataOfPackedTrackChanged"`
	IsPerformanceDataOfPackedTrackChanged bool       `json:"isPerformanceDataOfPackedTrackChanged"`
	PlayedIndicator                       int64      `json:"playedIndicator"`
	IsMetadataImported                    bool       `json:"isMetadataImported"`
	PdbImportKey                          int64      `json:"pdbImportKey"`
	StreamingSource                       string     `json:"streamingSource"`
	URI                                   string     `json:"uri"`
	IsBeatGridLocked                      bool       `json:"isBeatGridLocked"`
	OriginDatabaseUUID                    string     `json:"originDatabaseUuid"`
	OriginTrackID                         int64      `json:"originTrackId"`
	StreamingFlags                        int64      `json:"streamingFlags"`
	ExplicitLyrics                        bool       `json:"explicitLyrics"`
	LastEditTime                          *time.Time `json:"lastEditTime,omitempty"`
	AlbumArtSourceHash                    string     `json:"albumArtSourceHash"`
}

// Playlist represents an Engine DJ Playlist table record.
type Playlist struct {
	ID                   int64      `json:"id"`
	Title                string     `json:"title"`
	ParentListID         int64      `json:"parentListId"`
	IsPersisted          bool       `json:"isPersisted"`
	NextListID           int64      `json:"nextListId"`
	LastEditTime         *time.Time `json:"lastEditTime,omitempty"`
	IsExplicitlyExported bool       `json:"isExplicitlyExported"`
}

// PlaylistNode represents a node in the hierarchical playlist tree.
type PlaylistNode struct {
	Playlist Playlist       `json:"playlist"`
	IsFolder bool           `json:"isFolder"`
	Children []PlaylistNode `json:"children,omitempty"`
	Tracks   []Track        `json:"tracks,omitempty"`
}

// PlaylistEntity represents an Engine DJ PlaylistEntity table record.
type PlaylistEntity struct {
	ID                  int64  `json:"id"`
	ListID              int64  `json:"listId"`
	TrackID             int64  `json:"trackId"`
	DatabaseUUID        string `json:"databaseUuid"`
	NextEntityID        int64  `json:"nextEntityId"`
	MembershipReference int64  `json:"membershipReference"`
}

// PerformanceData holds raw and structured performance cues, loops, grids, and waveforms.
type PerformanceData struct {
	TrackID              int64        `json:"trackId"`
	TrackData            []byte       `json:"trackData,omitempty"`
	OverviewWaveFormData []byte       `json:"overviewWaveFormData,omitempty"`
	BeatDataBlob         []byte       `json:"beatDataBlob,omitempty"`
	QuickCuesBlob        []byte       `json:"quickCuesBlob,omitempty"`
	LoopsBlob            []byte       `json:"loopsBlob,omitempty"`
	ThirdPartySourceID   int64        `json:"thirdPartySourceId"`
	ActiveOnLoadLoops    int          `json:"activeOnLoadLoops"`
	SampleRate           float64      `json:"sampleRate"`
	BeatGrid             []lib.Marker `json:"beatGrid,omitempty"`
	HotCues              []lib.HotCue `json:"hotCues,omitempty"`
	MainCue              float64      `json:"mainCue"`
	Loops                []lib.Loop   `json:"loops,omitempty"`
}

// AlbumArtRecord represents an Engine DJ AlbumArt table record.
type AlbumArtRecord struct {
	ID   int64  `json:"id"`
	Hash string `json:"hash"`
	Data []byte `json:"data"`
}

// SmartlistRecord represents an Engine DJ Smartlist table record.
type SmartlistRecord struct {
	ListUUID           string     `json:"listUuid"`
	Title              string     `json:"title"`
	ParentPlaylistPath string     `json:"parentPlaylistPath"`
	NextPlaylistPath   string     `json:"nextPlaylistPath"`
	NextListUUID       string     `json:"nextListUuid"`
	Rules              string     `json:"rules"`
	LastEditTime       *time.Time `json:"lastEditTime,omitempty"`
}

// InformationRecord represents an Engine DJ Information table record.
type InformationRecord struct {
	ID                                    int64  `json:"id"`
	UUID                                  string `json:"uuid"`
	SchemaVersionMajor                    int    `json:"schemaVersionMajor"`
	SchemaVersionMinor                    int    `json:"schemaVersionMinor"`
	SchemaVersionPatch                    int    `json:"schemaVersionPatch"`
	CurrentPlayedIndicator                int64  `json:"currentPlayedIndicator"`
	LastRekordBoxLibraryImportReadCounter int64  `json:"lastRekordBoxLibraryImportReadCounter"`
}

// nullTime supports flexible SQLite datetime parsing across timestamps, strings, and unix epochs.
type nullTime struct {
	Time  time.Time
	Valid bool
}

func (nt *nullTime) Scan(value any) error {
	if value == nil {
		nt.Time, nt.Valid = time.Time{}, false
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		nt.Time, nt.Valid = v, true
		return nil
	case int64:
		nt.Time, nt.Valid = time.Unix(v, 0).UTC(), true
		return nil
	case int:
		nt.Time, nt.Valid = time.Unix(int64(v), 0).UTC(), true
		return nil
	case int32:
		nt.Time, nt.Valid = time.Unix(int64(v), 0).UTC(), true
		return nil
	case uint64:
		nt.Time, nt.Valid = time.Unix(int64(v), 0).UTC(), true
		return nil
	case float64:
		nt.Time, nt.Valid = time.Unix(int64(v), 0).UTC(), true
		return nil
	case string:
		if v == "" {
			nt.Time, nt.Valid = time.Time{}, false
			return nil
		}
		layouts := []string{
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05.999999999",
			"2006-01-02 15:04:05",
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, v); err == nil {
				nt.Time, nt.Valid = t, true
				return nil
			}
		}
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			nt.Time, nt.Valid = time.Unix(ts, 0).UTC(), true
			return nil
		}
		return fmt.Errorf("unable to parse datetime string: %q", v)
	case []byte:
		return nt.Scan(string(v))
	default:
		return fmt.Errorf("unsupported type for nullTime: %T", value)
	}
}

func (nt nullTime) Value() (driver.Value, error) {
	if !nt.Valid {
		return nil, nil
	}
	return nt.Time, nil
}
