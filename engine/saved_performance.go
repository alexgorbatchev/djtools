package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/nateranda/djtools/lib"
)

// SavedPerformance contains saved positions in seconds, preserving native slots.
type SavedPerformance struct {
	SampleRate float64
	HotCues    []lib.HotCue
	Loops      []lib.Loop
}

// DecodeSavedPerformance decodes native Engine blobs without opening a database.
// Missing blobs mean no saved markers; malformed blobs fail instead of returning
// partial markers or guessing a sample rate.
func DecodeSavedPerformance(beat, cues, loops []byte) (SavedPerformance, error) {
	var result SavedPerformance
	if len(cues) == 0 && len(loops) == 0 {
		return result, nil
	}
	grid, err := qUncompress(beat)
	if err != nil {
		return result, fmt.Errorf("reading saved-position sample rate: %w", err)
	}
	if len(grid) < 17 {
		return result, errors.New("missing saved-position sample rate")
	}
	result.SampleRate = math.Float64frombits(binary.BigEndian.Uint64(grid))
	if !finite(result.SampleRate) || result.SampleRate <= 0 {
		return SavedPerformance{}, errors.New("invalid saved-position sample rate")
	}
	if len(cues) > 0 {
		payload, err := qUncompress(cues)
		if err != nil {
			return SavedPerformance{}, fmt.Errorf("decompressing saved cues: %w", err)
		}
		decoded, err := cuesFromBlob(result.SampleRate, payload)
		if err != nil {
			return SavedPerformance{}, err
		}
		result.HotCues = decoded.cues
	}
	if len(loops) > 0 {
		result.Loops, err = loopsFromBlob(result.SampleRate, loops)
		if err != nil {
			return SavedPerformance{}, err
		}
	}
	return result, nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func savedSlot(data []byte, size int) (string, []byte, []byte, error) {
	if len(data) == 0 {
		return "", nil, nil, errors.New("missing saved slot")
	}
	length := int(data[0])
	if len(data) < 1+length+size {
		return "", nil, nil, errors.New("truncated saved slot")
	}
	name := string(data[1 : 1+length])
	if !utf8.ValidString(name) {
		return "", nil, nil, errors.New("invalid saved slot name")
	}
	return name, data[1+length : 1+length+size], data[1+length+size:], nil
}

func cuesFromBlob(sampleRate float64, blob []byte) (cueData, error) {
	var result cueData
	if len(blob) < 8 || binary.BigEndian.Uint64(blob) != 8 {
		return result, errors.New("invalid cue count")
	}
	blob = blob[8:]
	for slot := 1; slot <= 8; slot++ {
		name, record, rest, err := savedSlot(blob, 12)
		if err != nil {
			return cueData{}, err
		}
		blob = rest
		position := math.Float64frombits(binary.BigEndian.Uint64(record))
		if !finite(position) || (position < 0 && position != -1) || !finite(position/sampleRate) {
			return cueData{}, errors.New("invalid cue position")
		}
		if position == -1 {
			continue
		}
		result.cues = append(result.cues, lib.HotCue{Name: name, Position: slot, Offset: position / sampleRate, Color: fmt.Sprintf("#%02X%02X%02X", record[9], record[10], record[11])})
	}
	if len(blob) != 17 {
		return cueData{}, errors.New("invalid main cue length")
	}
	result.cueModified = math.Float64frombits(binary.BigEndian.Uint64(blob)) / sampleRate
	result.cueOriginal = math.Float64frombits(binary.BigEndian.Uint64(blob[9:])) / sampleRate
	if !finite(result.cueModified) || !finite(result.cueOriginal) {
		return cueData{}, errors.New("invalid main cue position")
	}
	return result, nil
}

func loopsFromBlob(sampleRate float64, blob []byte) ([]lib.Loop, error) {
	if len(blob) < 8 || binary.LittleEndian.Uint64(blob) != 8 {
		return nil, errors.New("invalid loop count")
	}
	blob = blob[8:]
	var result []lib.Loop
	for slot := 1; slot <= 8; slot++ {
		name, record, rest, err := savedSlot(blob, 22)
		if err != nil {
			return nil, err
		}
		blob = rest
		start := math.Float64frombits(binary.LittleEndian.Uint64(record))
		end := math.Float64frombits(binary.LittleEndian.Uint64(record[8:]))
		if !finite(start) || !finite(end) {
			return nil, errors.New("invalid loop position")
		}
		if start == -1 && end == -1 {
			continue
		}
		if start < 0 || end <= start || !finite(start/sampleRate) || !finite(end/sampleRate) {
			return nil, errors.New("invalid loop boundaries")
		}
		result = append(result, lib.Loop{Name: name, Position: slot, Start: start / sampleRate, End: end / sampleRate, Color: fmt.Sprintf("#%02X%02X%02X", record[19], record[20], record[21])})
	}
	if len(blob) != 0 {
		return nil, errors.New("unexpected loop data")
	}
	return result, nil
}
