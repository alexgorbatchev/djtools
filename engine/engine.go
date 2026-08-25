// This package contains import and export functions for Engine's database format.
package engine

import (
	"bytes"
	"compress/zlib"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/nateranda/djtools/lib"
	_ "modernc.org/sqlite"
)

// ImportOptions contains the options used when importing an Engine library.
type ImportOptions struct {
	ImportOriginalGrids   bool
	ImportOriginalCues    bool
	PreserveOriginalPaths bool
}

type library struct {
	info               information
	songs              []songNull
	songHistoryList    []songHistory
	perfData           []performanceDataEntry
	playlists          []playlist
	playlistEntityList []playlistEntity
	smartlistList      []smartlist
	albumArtList       []albumArtEntry
}

type information struct {
	id                 int
	uuid               string
	schemaVersionMajor int
	schemaVersionMinor int
	schemaVersionPatch int
}

type albumArtEntry struct {
	id   int
	hash string
	data []byte
}

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

type songNull struct {
	id                 sql.NullInt64
	title              sql.NullString
	artist             sql.NullString
	composer           sql.NullString
	album              sql.NullString
	genre              sql.NullString
	filetype           sql.NullString
	size               sql.NullInt64
	length             sql.NullFloat64
	year               sql.NullInt64
	bpm                sql.NullFloat64
	bpmAnalyzed        sql.NullFloat64
	dateAdded          nullTime
	bitrate            sql.NullInt64
	comment            sql.NullString
	rating             sql.NullInt64
	path               sql.NullString
	remixer            sql.NullString
	key                sql.NullInt32
	label              sql.NullString
	lastEditTime       nullTime
	albumArtId         sql.NullInt64
	timeLastPlayed     nullTime
	isPlayed           sql.NullBool
	isAnalyzed         sql.NullBool
	dateCreated        nullTime
	playedIndicator    sql.NullInt64
	streamingSource    sql.NullString
	uri                sql.NullString
	isBeatGridLocked   sql.NullBool
	originDatabaseUuid sql.NullString
	originTrackId      sql.NullInt64
	streamingFlags     sql.NullInt64
	explicitLyrics     sql.NullBool
	albumArtSourceHash sql.NullString
}

type songHistory struct {
	id         int
	plays      int
	lastPlayed int
}

type performanceDataEntry struct {
	id                       int
	beatDataBlob             []byte
	quickCuesBlob            []byte
	loopsBlob                []byte
	trackDataBlob            []byte
	overviewWaveFormDataBlob []byte
	activeOnLoadLoops        sql.NullInt64
}

type playlist struct {
	id           int
	title        string
	parentListId int
	nextListId   int
	songs        []int
}

type playlistEntity struct {
	id           int
	listId       int
	trackId      int
	nextEntityId int
}

type smartlist struct {
	listUuid           string
	title              string
	parentPlaylistPath sql.NullString
	nextPlaylistPath   sql.NullString
	nextListUuid       sql.NullString
	rules              string
}

type beatData struct {
	sampleRate      float64
	defaultBeatgrid []marker
	adjBeatgrid     []marker
}

type cueData struct {
	cues        []lib.HotCue
	cueOriginal float64
	cueModified float64
}

type marker struct {
	offset     float64
	beatNumber int64
	numBeats   uint32
}

// qUncompress uncompresses a uInt32-appended byte slice using zlib,
// used for blobs compressed with the QT C++ library's qCompress function.
func qUncompress(file []byte) ([]byte, error) {
	if len(file) < 5 {
		return nil, fmt.Errorf("error uncompressing file: blob must contain 5 or more bytes")
	}
	uncompressLength := binary.BigEndian.Uint32(file[:4])
	buffer := bytes.NewBuffer(file[4:])
	r, err := zlib.NewReader(buffer)
	if err != nil {
		return nil, fmt.Errorf("error uncompressing file: %v", err)
	}

	defer r.Close()

	var out bytes.Buffer
	io.Copy(&out, r)

	fileDecomp := out.Bytes()

	// check if the file's uncompressed length matches the header
	if len(fileDecomp) != int(uncompressLength) {
		return []byte{}, errors.New("VerificationError: uncompressed file length does not match length header")
	} else {
		return fileDecomp, nil
	}
}

// Import converts an Engine database into a djtools Library struct
func Import(path string, importOptions ImportOptions) (lib.Library, error) {
	enLibrary, err := importExtract(path)
	if err != nil {
		return lib.Library{}, err
	}
	library, err := importConvert(enLibrary, path, importOptions)
	if err != nil {
		return lib.Library{}, err
	}
	return library, nil
}
