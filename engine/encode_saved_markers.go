package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/nateranda/djtools/lib"
)

// SavedMarkers describes new saved positions in source seconds. SampleRate and
// SampleLimit must come from the same authoritative media extent as the grid.
// A nil MainCue is absent; a pointer to zero is an explicit cue at the start.
type SavedMarkers struct {
	SampleRate  float64
	SampleLimit float64
	MainCue     *float64
	HotCues     []lib.HotCue
	Loops       []lib.Loop
}

// SavedMarkerBlobs contains complete new-import payloads. This does not edit
// existing performance data: callers editing saved slots must preserve the
// existing records and main-cue history instead.
type SavedMarkerBlobs struct {
	QuickCues []byte
	Loops     []byte
}

// EncodeSavedMarkers encodes Engine's eight cue and loop slots, rejecting
// duplicate slots, malformed records, and positions outside the media extent.
// Loop ends may equal the exclusive media end; cue positions and loop starts
// must precede it. Newly imported main cues have no modification history.
func EncodeSavedMarkers(markers SavedMarkers) (SavedMarkerBlobs, error) {
	if !finite(markers.SampleRate) || markers.SampleRate <= 0 || !finite(markers.SampleLimit) || markers.SampleLimit <= 0 {
		return SavedMarkerBlobs{}, errors.New("invalid saved-marker media extent")
	}
	cues, err := encodeImportedCues(markers)
	if err != nil {
		return SavedMarkerBlobs{}, err
	}
	loops, err := encodeImportedLoops(markers)
	if err != nil {
		return SavedMarkerBlobs{}, err
	}
	compressed, err := qCompress(cues)
	if err != nil {
		return SavedMarkerBlobs{}, fmt.Errorf("compressing imported cues: %w", err)
	}
	return SavedMarkerBlobs{QuickCues: compressed, Loops: loops}, nil
}

func markerSamples(markers SavedMarkers, seconds float64, end bool) (float64, error) {
	samples := seconds * markers.SampleRate
	if !finite(seconds) || seconds < 0 || !finite(samples) || samples > markers.SampleLimit || (!end && samples == markers.SampleLimit) {
		return 0, errors.New("saved position is outside media extent")
	}
	return samples, nil
}

func markerNameAndColor(slot int, name, color string) ([]byte, error) {
	if err := validateHotCueSlot(slot); err != nil {
		return nil, err
	}
	if !utf8.ValidString(name) || len(name) > savedHotCueNameBytes {
		return nil, errors.New("invalid saved-marker name")
	}
	return hotCueColor(color)
}

func encodeImportedCues(markers SavedMarkers) ([]byte, error) {
	var records [savedHotCueSlots][]byte
	for _, cue := range markers.HotCues {
		color, err := markerNameAndColor(cue.Position, cue.Name, cue.Color)
		if err != nil {
			return nil, fmt.Errorf("encoding cue: %w", err)
		}
		if records[cue.Position-1] != nil {
			return nil, errors.New("duplicate hot cue slot")
		}
		samples, err := markerSamples(markers, cue.Offset, false)
		if err != nil {
			return nil, fmt.Errorf("encoding cue: %w", err)
		}
		record := append([]byte{byte(len(cue.Name))}, cue.Name...)
		record = binary.BigEndian.AppendUint64(record, math.Float64bits(samples))
		records[cue.Position-1] = append(append(record, 255), color...)
	}
	payload := binary.BigEndian.AppendUint64(nil, savedHotCueSlots)
	for _, record := range records {
		if record == nil {
			record = emptyHotCueRecord()
		}
		payload = append(payload, record...)
	}
	main := -1.0
	if markers.MainCue != nil {
		var err error
		main, err = markerSamples(markers, *markers.MainCue, false)
		if err != nil {
			return nil, fmt.Errorf("encoding main cue: %w", err)
		}
	}
	payload = binary.BigEndian.AppendUint64(payload, math.Float64bits(main))
	payload = append(payload, 0)
	return binary.BigEndian.AppendUint64(payload, math.Float64bits(main)), nil
}

func encodeImportedLoops(markers SavedMarkers) ([]byte, error) {
	var records [savedHotCueSlots][]byte
	for _, loop := range markers.Loops {
		color, err := markerNameAndColor(loop.Position, loop.Name, loop.Color)
		if err != nil {
			return nil, fmt.Errorf("encoding loop: %w", err)
		}
		if records[loop.Position-1] != nil {
			return nil, errors.New("duplicate loop slot")
		}
		start, err := markerSamples(markers, loop.Start, false)
		if err != nil {
			return nil, fmt.Errorf("encoding loop start: %w", err)
		}
		end, err := markerSamples(markers, loop.End, true)
		if err != nil || end <= start {
			return nil, errors.New("invalid loop end")
		}
		record := append([]byte{byte(len(loop.Name))}, loop.Name...)
		record = binary.LittleEndian.AppendUint64(record, math.Float64bits(start))
		record = binary.LittleEndian.AppendUint64(record, math.Float64bits(end))
		// Desktop-written alteredPerformanceData/m.sql uses these two flags
		// followed by opaque alpha and RGB for populated saved loops.
		records[loop.Position-1] = append(append(record, 1, 1, 255), color...)
	}
	payload := binary.LittleEndian.AppendUint64(nil, savedHotCueSlots)
	for _, record := range records {
		if record == nil {
			record = make([]byte, 23)
			binary.LittleEndian.PutUint64(record[1:], math.Float64bits(-1))
			binary.LittleEndian.PutUint64(record[9:], math.Float64bits(-1))
		}
		payload = append(payload, record...)
	}
	return payload, nil
}
