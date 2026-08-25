package engine_test

import (
	"testing"

	"github.com/nateranda/djtools/engine"
	"github.com/nateranda/djtools/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPerformanceData_CRUD(t *testing.T) {
	db, ctx := setupTestDB(t)

	// Create track
	trackID, err := db.InsertTrack(ctx, &engine.Track{
		Title: "Performance Track",
		Path:  "perf.mp3",
	})
	require.NoError(t, err)

	perfData := &engine.PerformanceData{
		TrackID:              trackID,
		SampleRate:           44100,
		TrackData:            []byte("track-waveform-blob"),
		OverviewWaveFormData: []byte("overview-waveform-blob"),
		ActiveOnLoadLoops:    2,
		MainCue:              2.5,
		BeatGrid: []lib.Marker{
			{
				StartPosition: 0.1,
				Bpm:           126.0,
				BeatNumber:    0,
			},
			{
				StartPosition: 100.0,
				Bpm:           126.0,
				BeatNumber:    0,
			},
		},
		HotCues: []lib.HotCue{
			{
				Name:     "Drop",
				Offset:   32.0,
				Position: 1,
				Color:    "#FF0000",
			},
		},
		Loops: []lib.Loop{
			{
				Name:     "Build Loop",
				Start:    16.0,
				End:      32.0,
				Position: 1,
				Color:    "#00FF00",
			},
		},
	}

	// 1. Update/Insert performance data
	err = db.UpdatePerformanceData(ctx, perfData)
	require.NoError(t, err)

	// 2. GetPerformanceData
	fetched, err := db.GetPerformanceData(ctx, trackID)
	require.NoError(t, err)
	assert.Equal(t, trackID, fetched.TrackID)
	assert.Equal(t, float64(44100), fetched.SampleRate)
	assert.Equal(t, []byte("track-waveform-blob"), fetched.TrackData)
	assert.Equal(t, []byte("overview-waveform-blob"), fetched.OverviewWaveFormData)
	assert.Equal(t, 2, fetched.ActiveOnLoadLoops)
	assert.Equal(t, 2.5, fetched.MainCue)
	require.Len(t, fetched.HotCues, 1)
	assert.Equal(t, "Drop", fetched.HotCues[0].Name)
	assert.Equal(t, 32.0, fetched.HotCues[0].Offset)
	assert.Equal(t, "#FF0000", fetched.HotCues[0].Color)
	require.Len(t, fetched.Loops, 1)
	assert.Equal(t, "Build Loop", fetched.Loops[0].Name)
	assert.Equal(t, 16.0, fetched.Loops[0].Start)
	assert.Equal(t, 32.0, fetched.Loops[0].End)
	assert.Equal(t, "#00FF00", fetched.Loops[0].Color)
}

func TestQCompress_And_QUncompress(t *testing.T) {
	data := []byte("hello compression world 1234567890")
	comp, err := engine.QCompress(data)
	require.NoError(t, err)

	decomp, err := engine.QUncompress(comp)
	require.NoError(t, err)
	assert.Equal(t, data, decomp)

	// Short blob error
	_, err = engine.QUncompress([]byte{0, 1})
	assert.ErrorContains(t, err, "must contain 5 or more bytes")

	// Bad zlib payload
	_, err = engine.QUncompress([]byte{0, 0, 0, 10, 99, 98, 97})
	assert.Error(t, err)

	// Length mismatch
	compWithFakeLen := make([]byte, len(comp))
	copy(compWithFakeLen, comp)
	compWithFakeLen[3] = 99 // Corrupt declared length
	_, err = engine.QUncompress(compWithFakeLen)
	assert.ErrorContains(t, err, "VerificationError")
}
