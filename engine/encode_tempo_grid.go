package engine

import (
	"fmt"

	"github.com/nateranda/djtools/lib"
)

// BeatGridSourceImported is Engine's source tag for imported tempo markers.
const BeatGridSourceImported int8 = 3

const beatsPerBar = 4

// EncodeTempoGrid converts ordered 4/4 tempo markers into a complete new-import
// beatData blob, using the same marker conversion as UpdatePerformanceData.
// Positions are source seconds; SampleLimit is the authoritative exclusive
// media extent in samples, not an estimated duration. The final anchor may
// extend beyond that extent to finish a whole beat. Empty markers produce an
// unset grid rather than inventing a tempo.
//
// The payload uses Engine's imported source tag and zero reference sample.
// Marker interpolation follows djtools' conversion; it does not reproduce
// Engine Desktop's XML import factory's preroll and normalization algorithm.
func EncodeTempoGrid(sampleRate, sampleLimit float64, markers []lib.Marker) ([]byte, error) {
	if err := validateTempoMarkers(sampleRate, sampleLimit, markers); err != nil {
		return nil, err
	}
	if len(markers) == 0 {
		return EncodeUnsetBeatData(sampleRate, sampleLimit)
	}
	anchors, err := beatAnchorsFromMarkers(sampleRate, sampleLimit, markers)
	if err != nil {
		return nil, err
	}
	data, err := NewBeatData(sampleRate, sampleLimit, anchors)
	if err != nil {
		return nil, err
	}
	data.Source = BeatGridSourceImported
	return EncodeBeatData(data)
}

func validateTempoMarkers(sampleRate, sampleLimit float64, markers []lib.Marker) error {
	if !finite(sampleRate) || sampleRate <= 0 || !finite(sampleLimit) || sampleLimit <= 0 {
		return fmt.Errorf("%w: invalid imported media extent", ErrInvalidBeatGrid)
	}
	if len(markers) >= maxBeatAnchors {
		return fmt.Errorf("%w: too many tempo markers", ErrInvalidBeatGrid)
	}
	for i, marker := range markers {
		sample := marker.StartPosition * sampleRate
		if !finite(marker.StartPosition) || marker.StartPosition < 0 || !finite(sample) || sample >= sampleLimit {
			return fmt.Errorf("%w: tempo marker %d is outside the media extent", ErrInvalidBeatGrid, i)
		}
		if !finite(marker.Bpm) || marker.Bpm <= 0 || marker.BeatNumber < 0 || marker.BeatNumber >= beatsPerBar {
			return fmt.Errorf("%w: tempo marker %d has an unsupported tempo or bar position", ErrInvalidBeatGrid, i)
		}
	}
	return nil
}
