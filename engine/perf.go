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
				p.SampleRate = bData.sampleRate
				p.BeatGrid = gridFromBeatData(bData.sampleRate, bData.adjBeatgrid)
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
		raw := createBeatDataBlob(sampleRate, data.BeatGrid)
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
	buffer := bytes.NewBuffer(file[4:])
	r, err := zlib.NewReader(buffer)
	if err != nil {
		return nil, fmt.Errorf("error uncompressing file: %w", err)
	}
	defer r.Close()

	var out bytes.Buffer
	if _, err := io.Copy(&out, r); err != nil {
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

func beatDataFromBlob(blob []byte) (beatData, error) {
	if len(blob) < 33 {
		return beatData{}, fmt.Errorf("InvalidBlobError: beatData blob should be at least 33 bytes")
	}

	var bd beatData
	i := 0

	bd.sampleRate = math.Float64frombits(binary.BigEndian.Uint64(blob[i : i+8]))
	i += 17 // skip past track length and beatgrid set byte

	if len(blob) < i+8 {
		return bd, fmt.Errorf("InvalidBlobError: truncated default beatgrid header")
	}
	numMarkers := binary.BigEndian.Uint64(blob[i : i+8])
	i += 8

	for range numMarkers {
		if len(blob) < i+28 {
			break
		}
		var m marker
		m.offset = math.Float64frombits(binary.LittleEndian.Uint64(blob[i : i+8]))
		i += 8
		m.beatNumber = int64(binary.LittleEndian.Uint64(blob[i : i+8]))
		i += 8
		m.numBeats = binary.LittleEndian.Uint32(blob[i : i+4])
		i += 8 // skip unknown int32
		bd.defaultBeatgrid = append(bd.defaultBeatgrid, m)
	}

	if len(blob) < i+8 {
		return bd, nil
	}
	numMarkers = binary.BigEndian.Uint64(blob[i : i+8])
	i += 8
	for range numMarkers {
		if len(blob) < i+28 {
			break
		}
		var m marker
		m.offset = math.Float64frombits(binary.LittleEndian.Uint64(blob[i : i+8]))
		i += 8
		m.beatNumber = int64(binary.LittleEndian.Uint64(blob[i : i+8]))
		i += 8
		m.numBeats = binary.LittleEndian.Uint32(blob[i : i+4])
		i += 8
		bd.adjBeatgrid = append(bd.adjBeatgrid, m)
	}

	return bd, nil
}

func gridFromBeatData(sampleRate float64, enGrid []marker) []lib.Marker {
	var grid []lib.Marker
	for i := range len(enGrid) - 1 {
		var m lib.Marker
		m.StartPosition = enGrid[i].offset / sampleRate
		lenMarker := enGrid[i+1].offset - enGrid[i].offset
		if lenMarker > 0 {
			m.Bpm = sampleRate * 60 * float64(enGrid[i].numBeats) / lenMarker
		}
		m.BeatNumber = int(enGrid[i].beatNumber) % 4
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

func cuesFromBlob(sampleRate float64, blob []byte) (cueData, error) {
	var blobCueData cueData
	i := 8 // byte index, skipping number of cues (always 8)

	for pos := range 8 {
		if len(blob) <= i {
			break
		}
		labelLength := int(blob[i])
		if labelLength == 0 {
			i += 13
			continue
		}
		i++
		if len(blob) < i+labelLength+12 {
			break
		}
		var cue lib.HotCue
		cue.Position = pos + 1
		cue.Name = string(blob[i : i+labelLength])
		i += labelLength
		cue.Offset = math.Float64frombits(binary.BigEndian.Uint64(blob[i:i+8])) / sampleRate
		i += 9 // skip 1-byte alpha channel
		r := int(blob[i])
		i++
		g := int(blob[i])
		i++
		b := int(blob[i])
		i++
		color, err := lib.RgbToHex(r, g, b)
		if err != nil {
			return cueData{}, fmt.Errorf("extracting cues from cueData blob: %w", err)
		}
		cue.Color = color
		blobCueData.cues = append(blobCueData.cues, cue)
	}

	if len(blob) >= i+17 {
		blobCueData.cueModified = math.Float64frombits(binary.BigEndian.Uint64(blob[i:i+8])) / sampleRate
		i += 9
		blobCueData.cueOriginal = math.Float64frombits(binary.BigEndian.Uint64(blob[i:i+8])) / sampleRate
	}

	return blobCueData, nil
}

func loopsFromBlob(sampleRate float64, blob []byte) ([]lib.Loop, error) {
	var loops []lib.Loop
	i := 8 // byte index, skipping number of loops (always 8)
	for pos := range 8 {
		if len(blob) <= i {
			break
		}
		labelLength := int(blob[i])
		if labelLength == 0 {
			i += 24
			continue
		}
		i++
		if len(blob) < i+labelLength+22 {
			break
		}
		var loop lib.Loop
		loop.Position = pos + 1
		loop.Name = string(blob[i : i+labelLength])
		i += labelLength
		loop.Start = math.Float64frombits(binary.LittleEndian.Uint64(blob[i:i+8])) / sampleRate
		i += 8
		loop.End = math.Float64frombits(binary.LittleEndian.Uint64(blob[i:i+8])) / sampleRate
		i += 8
		i += 3 // skip alpha and set bytes
		r := int(blob[i])
		i++
		g := int(blob[i])
		i++
		b := int(blob[i])
		i++
		color, err := lib.RgbToHex(r, g, b)
		if err != nil {
			return nil, fmt.Errorf("extracting loops from loops blob: %w", err)
		}
		loop.Color = color
		loops = append(loops, loop)
	}

	return loops, nil
}

func createBeatDataBlob(sampleRate float64, grid []lib.Marker) []byte {
	var buf bytes.Buffer
	var b8 [8]byte
	var b4 [4]byte

	binary.BigEndian.PutUint64(b8[:], math.Float64bits(sampleRate))
	buf.Write(b8[:])

	// skip 17 bytes (track length, set flags)
	buf.Write(make([]byte, 17))

	// default beatgrid marker count
	binary.BigEndian.PutUint64(b8[:], uint64(len(grid)))
	buf.Write(b8[:])

	for _, m := range grid {
		binary.LittleEndian.PutUint64(b8[:], math.Float64bits(m.StartPosition*sampleRate))
		buf.Write(b8[:])

		binary.LittleEndian.PutUint64(b8[:], uint64(m.BeatNumber))
		buf.Write(b8[:])

		binary.LittleEndian.PutUint32(b4[:], 1)
		buf.Write(b4[:])

		buf.Write(make([]byte, 8))
	}

	// adjusted beatgrid marker count
	binary.BigEndian.PutUint64(b8[:], uint64(len(grid)))
	buf.Write(b8[:])

	for _, m := range grid {
		binary.LittleEndian.PutUint64(b8[:], math.Float64bits(m.StartPosition*sampleRate))
		buf.Write(b8[:])

		binary.LittleEndian.PutUint64(b8[:], uint64(m.BeatNumber))
		buf.Write(b8[:])

		binary.LittleEndian.PutUint32(b4[:], 1)
		buf.Write(b4[:])

		buf.Write(make([]byte, 8))
	}

	return buf.Bytes()
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
			buf.Write(make([]byte, 13))
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

	binary.BigEndian.PutUint64(b8[:], 8)
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

			buf.Write(make([]byte, 3))

			r, g, b, err := lib.HexToRgb(l.Color)
			if err != nil {
				r, g, b = 0, 0, 0
			}
			buf.WriteByte(byte(r))
			buf.WriteByte(byte(g))
			buf.WriteByte(byte(b))
		} else {
			buf.Write(make([]byte, 24))
		}
	}

	return buf.Bytes()
}
