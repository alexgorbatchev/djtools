package engine

import (
	"bytes"
	"compress/zlib"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/nateranda/djtools/lib"
)

var ErrPerformanceDataNotFound = errors.New("performance data not found")

type cueData struct {
	cues        []lib.HotCue
	cueOriginal float64
	cueModified float64
}

// GetPerformanceData retrieves and decompresses performance data (cue points, loops, beat grids, waveforms) for a track.
func (db *DB) GetPerformanceData(ctx context.Context, trackID int64) (*PerformanceData, error) {
	cols, err := getTableColumns(db.mDB, "PerformanceData")
	if err != nil || len(cols) == 0 {
		return nil, ErrPerformanceDataNotFound
	}

	colExpr := func(col string, fallback string) string {
		if cols[col] {
			return col
		}
		return fallback + " AS " + col
	}

	query := fmt.Sprintf(`SELECT trackId, beatData, quickCues, loops, %s, %s, %s
		FROM PerformanceData WHERE trackId = ? LIMIT 1`,
		colExpr("trackData", "NULL"),
		colExpr("overviewWaveFormData", "NULL"),
		colExpr("activeOnLoadLoops", "0"),
	)

	row := db.mDB.QueryRowContext(ctx, query, trackID)

	var p PerformanceData
	var activeLoops sql.NullInt64
	err = row.Scan(&p.TrackID, &p.BeatDataBlob, &p.QuickCuesBlob, &p.LoopsBlob,
		&p.TrackData, &p.OverviewWaveFormData, &activeLoops)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPerformanceDataNotFound
		}
		return nil, fmt.Errorf("reading performance data for track %d: %w", trackID, err)
	}
	p.ActiveOnLoadLoops = int(activeLoops.Int64)

	// Decompress and parse beat data if present
	if len(p.BeatDataBlob) >= 5 {
		decompBeat, err := qUncompress(p.BeatDataBlob)
		if err == nil {
			bData, err := beatDataFromBlob(decompBeat)
			if err == nil {
				p.SampleRate = bData.SampleRate
				p.BeatGrid = gridFromBeatData(bData.SampleRate, bData.Adjusted)
			}
		}
	}

	if p.SampleRate <= 0 {
		p.SampleRate = 44100
	}

	// Decompress and parse quick cues if present
	if len(p.QuickCuesBlob) >= 5 {
		decompCues, err := qUncompress(p.QuickCuesBlob)
		if err == nil {
			cData, err := cuesFromBlob(p.SampleRate, decompCues)
			if err == nil {
				p.HotCues = cData.cues
				p.MainCue = cData.cueModified
			}
		}
	}

	// Parse loops if present
	if len(p.LoopsBlob) > 0 {
		loops, err := loopsFromBlob(p.SampleRate, p.LoopsBlob)
		if err == nil {
			p.Loops = loops
		}
	}

	return &p, nil
}

// UpdatePerformanceData encodes and saves performance data into the database.
func (db *DB) UpdatePerformanceData(ctx context.Context, data *PerformanceData) error {
	if db.readonly {
		return errors.New("cannot update performance data in read-only database")
	}
	if data.TrackID <= 0 {
		return errors.New("invalid track ID for performance data")
	}

	sampleRate := data.SampleRate
	if sampleRate <= 0 {
		sampleRate = 44100
	}

	// If raw blobs are not provided, encode from structured data
	beatBlob := data.BeatDataBlob
	if len(beatBlob) == 0 && len(data.BeatGrid) > 0 {
		var length sql.NullFloat64
		if err := db.mDB.QueryRowContext(ctx, "SELECT length FROM Track WHERE id = ?", data.TrackID).Scan(&length); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("reading track %d length for beat data: %w", data.TrackID, err)
		}
		raw, err := createBeatDataBlob(sampleRate, (length.Float64+1)*sampleRate, data.BeatGrid)
		if err != nil {
			return fmt.Errorf("encoding beat data: %w", err)
		}
		comp, err := qCompress(raw)
		if err != nil {
			return fmt.Errorf("compressing beat data: %w", err)
		}
		beatBlob = comp
	}

	cuesBlob := data.QuickCuesBlob
	if len(cuesBlob) == 0 && (len(data.HotCues) > 0 || data.MainCue > 0) {
		raw := createQuickCuesBlob(sampleRate, data.MainCue, data.HotCues)
		comp, err := qCompress(raw)
		if err != nil {
			return fmt.Errorf("compressing cues data: %w", err)
		}
		cuesBlob = comp
	}

	loopsBlob := data.LoopsBlob
	if len(loopsBlob) == 0 && len(data.Loops) > 0 {
		loopsBlob = createLoopsBlob(sampleRate, data.Loops)
	}

	query := `INSERT INTO PerformanceData (
		trackId, trackData, overviewWaveFormData, beatData, quickCues, loops, thirdPartySourceId, activeOnLoadLoops
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(trackId) DO UPDATE SET
		trackData = excluded.trackData,
		overviewWaveFormData = excluded.overviewWaveFormData,
		beatData = excluded.beatData,
		quickCues = excluded.quickCues,
		loops = excluded.loops,
		thirdPartySourceId = excluded.thirdPartySourceId,
		activeOnLoadLoops = excluded.activeOnLoadLoops`

	return db.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query,
			data.TrackID, data.TrackData, data.OverviewWaveFormData,
			beatBlob, cuesBlob, loopsBlob,
			data.ThirdPartySourceID, data.ActiveOnLoadLoops,
		)
		if err != nil {
			return fmt.Errorf("upserting performance data for track %d: %w", data.TrackID, err)
		}
		return nil
	})
}

// QCompress compresses a byte slice prepending the uncompressed uint32 big-endian length.
func QCompress(data []byte) ([]byte, error) {
	return qCompress(data)
}

// QUncompress uncompresses a uInt32-appended byte slice using zlib.
func QUncompress(file []byte) ([]byte, error) {
	return qUncompress(file)
}

// qUncompress uncompresses a uInt32-appended byte slice using zlib.
func qUncompress(file []byte) ([]byte, error) {
	if len(file) < 5 {
		return nil, fmt.Errorf("error uncompressing file: blob must contain 5 or more bytes")
	}
	uncompressLength := binary.BigEndian.Uint32(file[:4])
	const maxPerformanceBytes = 128 << 20
	if uncompressLength > maxPerformanceBytes {
		return nil, errors.New("performance blob exceeds decompression limit")
	}
	buffer := bytes.NewBuffer(file[4:])
	r, err := zlib.NewReader(buffer)
	if err != nil {
		return nil, fmt.Errorf("error uncompressing file: %w", err)
	}
	defer r.Close()

	var out bytes.Buffer
	if _, err := io.Copy(&out, io.LimitReader(r, int64(uncompressLength)+1)); err != nil {
		return nil, fmt.Errorf("decompressing zlib stream: %w", err)
	}

	fileDecomp := out.Bytes()
	if len(fileDecomp) != int(uncompressLength) {
		return nil, errors.New("VerificationError: uncompressed file length does not match length header")
	}
	return fileDecomp, nil
}

// qCompress compresses a byte slice prepending the uncompressed uint32 big-endian length.
func qCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	buf.Write(lenBuf[:])

	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func beatDataFromBlob(payload []byte) (BeatData, error) {
	return decodeBeatPayload(payload)
}

func gridFromBeatData(sampleRate float64, anchors []BeatAnchor) []lib.Marker {
	var grid []lib.Marker
	for i := range len(anchors) - 1 {
		var m lib.Marker
		m.StartPosition = anchors[i].Sample / sampleRate
		lenMarker := anchors[i+1].Sample - anchors[i].Sample
		if lenMarker > 0 {
			m.Bpm = sampleRate * 60 * float64(anchors[i].BeatsUntilNext) / lenMarker
		}
		m.BeatNumber = beatInBar(anchors[i].Beat)
		grid = append(grid, m)
	}

	if len(grid) >= 1 && grid[0].Bpm > 0 {
		beatLength := 60 / grid[0].Bpm
		for grid[0].StartPosition < 0 {
			grid[0].StartPosition += beatLength
			grid[0].BeatNumber = (grid[0].BeatNumber + 1) % 4
		}
	}

	return grid
}

// beatInBar returns a beat's zero-based position in its 4/4 bar, including
// negative preroll beats.
func beatInBar(beat int64) int {
	return int(((beat % 4) + 4) % 4)
}

// createBeatDataBlob converts tempo markers into Engine's native anchors. Each
// marker starts a segment that lasts until the next marker; the last segment
// extends past sampleLimit. Without markers it writes an unset grid, which
// Engine replaces with its fallback.
func createBeatDataBlob(sampleRate, sampleLimit float64, grid []lib.Marker) ([]byte, error) {
	if len(grid) == 0 {
		return encodeBeatPayload(BeatData{SampleRate: sampleRate, SampleLimit: sampleLimit}), nil
	}
	anchors, err := beatAnchorsFromMarkers(sampleRate, sampleLimit, grid)
	if err != nil {
		return nil, err
	}
	data, err := NewBeatData(sampleRate, sampleLimit, anchors)
	if err != nil {
		return nil, err
	}
	return encodeBeatPayload(data), nil
}

func beatAnchorsFromMarkers(sampleRate, sampleLimit float64, grid []lib.Marker) ([]BeatAnchor, error) {
	anchors := make([]BeatAnchor, 0, len(grid)+1)
	beat := int64(beatInBar(int64(grid[0].BeatNumber)))
	for i, m := range grid {
		if !finite(m.Bpm) || m.Bpm <= 0 || !finite(m.StartPosition) {
			return nil, fmt.Errorf("%w: marker %d has no usable tempo", ErrInvalidBeatGrid, i)
		}
		sample := m.StartPosition * sampleRate
		samplesPerBeat := sampleRate * 60 / m.Bpm
		var beats float64
		if i+1 < len(grid) {
			beats = math.Round((grid[i+1].StartPosition*sampleRate - sample) / samplesPerBeat)
		} else {
			beats = math.Ceil((sampleLimit - sample) / samplesPerBeat)
		}
		if beats < 1 {
			beats = 1
		}
		if i > 0 && sample <= anchors[i-1].Sample {
			return nil, fmt.Errorf("%w: markers must be ordered by start position", ErrInvalidBeatGrid)
		}
		anchors = append(anchors, BeatAnchor{Sample: sample, Beat: beat, BeatsUntilNext: uint32(beats)})
		beat += int64(beats)
	}
	last := grid[len(grid)-1]
	end := anchors[len(anchors)-1].Sample + float64(anchors[len(anchors)-1].BeatsUntilNext)*sampleRate*60/last.Bpm
	return append(anchors, BeatAnchor{Sample: end, Beat: beat}), nil
}

func createQuickCuesBlob(sampleRate float64, mainCue float64, cues []lib.HotCue) []byte {
	var buf bytes.Buffer
	var b8 [8]byte

	// header: 8 positions
	binary.BigEndian.PutUint64(b8[:], 8)
	buf.Write(b8[:])

	cueMap := make(map[int]lib.HotCue)
	for _, c := range cues {
		cueMap[c.Position] = c
	}

	for pos := 1; pos <= 8; pos++ {
		if c, exists := cueMap[pos]; exists {
			nameBytes := []byte(c.Name)
			buf.WriteByte(byte(len(nameBytes)))
			buf.Write(nameBytes)

			binary.BigEndian.PutUint64(b8[:], math.Float64bits(c.Offset*sampleRate))
			buf.Write(b8[:])

			buf.WriteByte(255) // alpha

			r, g, b, err := lib.HexToRgb(c.Color)
			if err != nil {
				r, g, b = 0, 255, 255
			}
			buf.WriteByte(byte(r))
			buf.WriteByte(byte(g))
			buf.WriteByte(byte(b))
		} else {
			buf.WriteByte(0)
			binary.BigEndian.PutUint64(b8[:], math.Float64bits(-1))
			buf.Write(b8[:])
			buf.Write(make([]byte, 4))
		}
	}

	binary.BigEndian.PutUint64(b8[:], math.Float64bits(mainCue*sampleRate))
	buf.Write(b8[:])

	buf.WriteByte(255)

	binary.BigEndian.PutUint64(b8[:], math.Float64bits(mainCue*sampleRate))
	buf.Write(b8[:])

	return buf.Bytes()
}

func createLoopsBlob(sampleRate float64, loops []lib.Loop) []byte {
	var buf bytes.Buffer
	var b8 [8]byte

	binary.LittleEndian.PutUint64(b8[:], 8)
	buf.Write(b8[:])

	loopMap := make(map[int]lib.Loop)
	for _, l := range loops {
		loopMap[l.Position] = l
	}

	for pos := 1; pos <= 8; pos++ {
		if l, exists := loopMap[pos]; exists {
			nameBytes := []byte(l.Name)
			buf.WriteByte(byte(len(nameBytes)))
			buf.Write(nameBytes)

			binary.LittleEndian.PutUint64(b8[:], math.Float64bits(l.Start*sampleRate))
			buf.Write(b8[:])

			binary.LittleEndian.PutUint64(b8[:], math.Float64bits(l.End*sampleRate))
			buf.Write(b8[:])

			buf.Write([]byte{1, 1, 255})

			r, g, b, err := lib.HexToRgb(l.Color)
			if err != nil {
				r, g, b = 0, 0, 0
			}
			buf.WriteByte(byte(r))
			buf.WriteByte(byte(g))
			buf.WriteByte(byte(b))
		} else {
			buf.WriteByte(0)
			binary.LittleEndian.PutUint64(b8[:], math.Float64bits(-1))
			buf.Write(b8[:])
			buf.Write(b8[:])
			buf.Write(make([]byte, 6))
		}
	}

	return buf.Bytes()
}
