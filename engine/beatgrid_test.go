package engine

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/nateranda/djtools/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func analyzedGrid() []BeatAnchor {
	return []BeatAnchor{
		{Sample: -88200, Beat: -4, BeatsUntilNext: 132},
		{Sample: 2822400, Beat: 128, BeatsUntilNext: 8},
		{Sample: 3000000.5, Beat: 136},
	}
}

func hintCompressed(t *testing.T, hint uint32, payload []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	_ = binary.Write(&out, binary.BigEndian, hint)
	w := zlib.NewWriter(&out)
	_, err := w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return out.Bytes()
}

func TestBeatDataRoundTripsEveryField(t *testing.T) {
	data, err := NewBeatData(44100, 3100000, analyzedGrid())
	require.NoError(t, err)
	data.Default = []BeatAnchor{{Sample: 0, Beat: 0, BeatsUntilNext: 4}, {Sample: 88200, Beat: 4}}
	data.Source = 3
	data.ReferenceSample = 1234.25

	blob, err := EncodeBeatData(data)
	require.NoError(t, err)
	assert.Equal(t, uint32(beatDataHeaderBytes+16+5*beatAnchorBytes+beatDataTailBytes), binary.BigEndian.Uint32(blob))

	decoded, err := DecodeBeatData(blob)
	require.NoError(t, err)
	assert.Equal(t, data, decoded)
}

func TestNewBeatDataCopiesTheAnalyzedGridIntoBothVectors(t *testing.T) {
	anchors := analyzedGrid()
	data, err := NewBeatData(48000, 4000000, anchors)
	require.NoError(t, err)
	anchors[0].Sample = 1
	assert.True(t, data.IsSet)
	assert.Equal(t, BeatGridSourceAnalysis, data.Source)
	assert.Equal(t, data.Default, data.Adjusted)
	assert.Equal(t, float64(-88200), data.Adjusted[0].Sample)
	data.Default[0].Sample = 7
	assert.NotEqual(t, data.Default[0].Sample, data.Adjusted[0].Sample)
}

func TestDecodeBeatDataToleratesQtLengthHintsAndMissingTails(t *testing.T) {
	data, err := NewBeatData(44100, 0, analyzedGrid())
	require.NoError(t, err)
	data.Source = 3
	data.ReferenceSample = 99
	payload := encodeBeatPayload(data)

	withoutTail, err := DecodeBeatData(hintCompressed(t, 1, payload[:len(payload)-beatDataTailBytes]))
	require.NoError(t, err)
	assert.Equal(t, int8(0), withoutTail.Source)
	assert.Zero(t, withoutTail.ReferenceSample)
	assert.Equal(t, data.Adjusted, withoutTail.Adjusted)

	sourceOnly, err := DecodeBeatData(hintCompressed(t, 0, payload[:len(payload)-8]))
	require.NoError(t, err)
	assert.Equal(t, int8(3), sourceOnly.Source)
	assert.Zero(t, sourceOnly.ReferenceSample)
}

func TestDecodeBeatDataRejectsMalformedPayloads(t *testing.T) {
	valid, err := NewBeatData(44100, 0, analyzedGrid())
	require.NoError(t, err)
	payload := encodeBeatPayload(valid)
	nanRate := append([]byte(nil), payload...)
	binary.BigEndian.PutUint64(nanRate, math.Float64bits(math.NaN()))
	hugeCount := append([]byte(nil), payload...)
	binary.BigEndian.PutUint64(hugeCount[beatDataHeaderBytes:], 1<<40)
	oversized := make([]byte, maxBeatPayloadBytes+1)

	for name, blob := range map[string][]byte{
		"short blob":         {0, 0, 0, 1},
		"not zlib":           {0, 0, 0, 9, 1, 2, 3, 4, 5},
		"short header":       hintCompressed(t, 3, payload[:beatDataHeaderBytes-1]),
		"invalid rate":       hintCompressed(t, 0, nanRate),
		"missing vector":     hintCompressed(t, 0, payload[:beatDataHeaderBytes+4]),
		"truncated vector":   hintCompressed(t, 0, payload[:beatDataHeaderBytes+8+beatAnchorBytes]),
		"oversized count":    hintCompressed(t, 0, hugeCount),
		"missing adjusted":   hintCompressed(t, 0, payload[:beatDataHeaderBytes+8+3*beatAnchorBytes]),
		"decompression bomb": hintCompressed(t, 0, oversized),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeBeatData(blob)
			assert.ErrorIs(t, err, ErrInvalidBeatData)
		})
	}

	_, err = DecodeBeatData(append(hintCompressed(t, 0, payload)[:12], 0))
	assert.ErrorIs(t, err, ErrInvalidBeatData, "a truncated zlib stream must fail")
}

func TestBeatDataValidationRejectsUnusableGrids(t *testing.T) {
	grid := analyzedGrid
	for name, mutate := range map[string]func(*BeatData){
		"zero rate":          func(d *BeatData) { d.SampleRate = 0 },
		"negative limit":     func(d *BeatData) { d.SampleLimit = -1 },
		"infinite reference": func(d *BeatData) { d.ReferenceSample = math.Inf(1) },
		"single anchor":      func(d *BeatData) { d.Adjusted = d.Adjusted[:1] },
		"bad default":        func(d *BeatData) { d.Default = nil },
		"non-finite sample":  func(d *BeatData) { d.Adjusted[1].Sample = math.NaN() },
		"huge beat":          func(d *BeatData) { d.Adjusted[0].Beat = 1 << 53 },
		"counting tail":      func(d *BeatData) { d.Adjusted[2].BeatsUntilNext = 1 },
		"empty segment":      func(d *BeatData) { d.Adjusted[0].BeatsUntilNext = 0 },
		"backwards samples":  func(d *BeatData) { d.Adjusted[1].Sample = -90000 },
		"wrong beat count":   func(d *BeatData) { d.Adjusted[1].Beat = 127 },
		"too many anchors":   func(d *BeatData) { d.Adjusted = make([]BeatAnchor, maxBeatAnchors+1) },
	} {
		t.Run(name, func(t *testing.T) {
			data := BeatData{SampleRate: 44100, Default: grid(), Adjusted: grid()}
			mutate(&data)
			_, err := EncodeBeatData(data)
			assert.ErrorIs(t, err, ErrInvalidBeatGrid)
		})
	}
	_, err := NewBeatData(44100, 0, grid()[:1])
	assert.ErrorIs(t, err, ErrInvalidBeatGrid)
}

func TestBeatGridBPMUsesTheWholeGridSpan(t *testing.T) {
	anchors := []BeatAnchor{{Sample: 0, Beat: -4, BeatsUntilNext: 128}, {Sample: 128 * 22050, Beat: 124}}
	assert.InDelta(t, 120, BeatGridBPM(44100, anchors), 1e-9)
	assert.Zero(t, BeatGridBPM(44100, anchors[:1]))
	assert.Zero(t, BeatGridBPM(0, anchors))
	assert.Zero(t, BeatGridBPM(44100, []BeatAnchor{{Sample: 5}, {Sample: 5}}))
}

func TestMarkersBecomeNativeAnchorsCoveringTheTrack(t *testing.T) {
	raw, err := createBeatDataBlob(44100, 10*44100, []lib.Marker{
		{StartPosition: 0.5, Bpm: 120, BeatNumber: 2},
		{StartPosition: 4.5, Bpm: 60, BeatNumber: 2},
	})
	require.NoError(t, err)
	data, err := decodeBeatPayload(raw)
	require.NoError(t, err)
	require.NoError(t, data.Validate())
	assert.Equal(t, []BeatAnchor{
		{Sample: 0.5 * 44100, Beat: 2, BeatsUntilNext: 8},
		{Sample: 4.5 * 44100, Beat: 10, BeatsUntilNext: 6},
		{Sample: 10.5 * 44100, Beat: 16},
	}, data.Adjusted)
	assert.Equal(t, data.Default, data.Adjusted)
	assert.Equal(t, []lib.Marker{
		{StartPosition: 0.5, Bpm: 120, BeatNumber: 2},
		{StartPosition: 4.5, Bpm: 60, BeatNumber: 2},
	}, gridFromBeatData(data.SampleRate, data.Adjusted))
}

func TestMarkerConversionKeepsShortTracksAndRejectsUnusableMarkers(t *testing.T) {
	raw, err := createBeatDataBlob(44100, 0, []lib.Marker{{StartPosition: 1, Bpm: 120}})
	require.NoError(t, err)
	data, err := decodeBeatPayload(raw)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), data.Adjusted[0].BeatsUntilNext)

	unset, err := createBeatDataBlob(44100, 100, nil)
	require.NoError(t, err)
	empty, err := decodeBeatPayload(unset)
	require.NoError(t, err)
	assert.False(t, empty.IsSet)
	assert.Empty(t, empty.Adjusted)

	for name, markers := range map[string][]lib.Marker{
		"no tempo":  {{StartPosition: 0}},
		"unordered": {{StartPosition: 2, Bpm: 120}, {StartPosition: 1, Bpm: 120}},
	} {
		_, err := createBeatDataBlob(44100, 1000, markers)
		assert.ErrorIs(t, err, ErrInvalidBeatGrid, name)
	}
}

func TestPrerollBeatsKeepTheirPositionInTheBar(t *testing.T) {
	grid := gridFromBeatData(44100, []BeatAnchor{{Sample: -3 * 22050, Beat: -3, BeatsUntilNext: 4}, {Sample: 22050, Beat: 1}})
	require.Len(t, grid, 1)
	// Beat -3 sits one beat into its bar; three preroll steps reach beat 0 at time 0.
	assert.Equal(t, 0, grid[0].BeatNumber)
	assert.InDelta(t, 0, grid[0].StartPosition, 1e-9)
	assert.InDelta(t, 120, grid[0].Bpm, 1e-9)
	assert.Equal(t, 1, beatInBar(-7))
}

func TestUpdatePerformanceDataEncodesGridsToTheTrackLength(t *testing.T) {
	ctx := t.Context()
	db, err := Open(t.TempDir(), false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.CreateSchema(ctx))
	id, err := db.InsertTrack(ctx, &Track{Title: "Grid", Path: "grid.mp3", Length: 9})
	require.NoError(t, err)
	require.NoError(t, db.UpdatePerformanceData(ctx, &PerformanceData{TrackID: id, SampleRate: 44100, BeatGrid: []lib.Marker{{StartPosition: 0, Bpm: 120}}}))

	var blob []byte
	require.NoError(t, db.mDB.QueryRowContext(ctx, "SELECT beatData FROM PerformanceData WHERE trackId = ?", id).Scan(&blob))
	data, err := DecodeBeatData(blob)
	require.NoError(t, err)
	assert.Equal(t, BeatAnchor{Sample: 20 * 22050, Beat: 20}, data.Adjusted[1])

	err = db.UpdatePerformanceData(ctx, &PerformanceData{TrackID: id, BeatGrid: []lib.Marker{{StartPosition: 0}}})
	assert.True(t, errors.Is(err, ErrInvalidBeatGrid))
}
