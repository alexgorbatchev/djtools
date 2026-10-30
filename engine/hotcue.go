package engine

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/nateranda/djtools/lib"
)

const (
	savedHotCueSlots       = 8
	savedHotCueRecordBytes = 12
	savedHotCueNameBytes   = 255
)

// SetSavedHotCue replaces one native slot without rewriting the main cue or
// other slots. Position is 1–8 and Offset is source seconds. Existing native
// alpha is retained; newly populated slots use opaque color.
func SetSavedHotCue(beat, quickCues []byte, cue lib.HotCue) ([]byte, error) {
	if err := validateHotCueSlot(cue.Position); err != nil {
		return nil, err
	}
	if !utf8.ValidString(cue.Name) || len(cue.Name) > savedHotCueNameBytes {
		return nil, errors.New("invalid hot cue name")
	}
	color, err := hotCueColor(cue.Color)
	if err != nil {
		return nil, err
	}
	grid, err := DecodeBeatData(beat)
	if err != nil {
		return nil, fmt.Errorf("reading hot cue sample rate: %w", err)
	}
	samples := cue.Offset * grid.SampleRate
	if !finite(cue.Offset) || cue.Offset < 0 || !finite(samples) {
		return nil, errors.New("invalid hot cue position")
	}
	payload, err := hotCuePayload(quickCues, grid.SampleRate)
	if err != nil {
		return nil, err
	}
	start, end := hotCueRecordBounds(payload, cue.Position)
	old := payload[start:end]
	alpha := byte(255)
	if math.Float64frombits(binary.BigEndian.Uint64(old[1+int(old[0]):])) != -1 {
		alpha = old[len(old)-4]
	}
	record := append([]byte{byte(len(cue.Name))}, cue.Name...)
	record = binary.BigEndian.AppendUint64(record, math.Float64bits(samples))
	record = append(record, alpha)
	record = append(record, color...)
	return replaceHotCueRecord(payload, start, end, record)
}

// ClearSavedHotCue clears one slot, preserving every other record and the
// original/modified main-cue trailer. An absent blob remains absent.
func ClearSavedHotCue(quickCues []byte, slot int) ([]byte, error) {
	if err := validateHotCueSlot(slot); err != nil {
		return nil, err
	}
	if len(quickCues) == 0 {
		return nil, nil
	}
	payload, err := hotCuePayload(quickCues, 1)
	if err != nil {
		return nil, err
	}
	start, end := hotCueRecordBounds(payload, slot)
	return replaceHotCueRecord(payload, start, end, emptyHotCueRecord())
}

func validateHotCueSlot(slot int) error {
	if slot < 1 || slot > savedHotCueSlots {
		return errors.New("hot cue slot must be between 1 and 8")
	}
	return nil
}

func hotCueColor(color string) ([]byte, error) {
	if len(color) != 7 || color[0] != '#' {
		return nil, errors.New("hot cue color must be #RRGGBB")
	}
	rgb, err := hex.DecodeString(color[1:])
	if err != nil {
		return nil, fmt.Errorf("invalid hot cue color: %w", err)
	}
	return rgb, nil
}

func emptyHotCueRecord() []byte {
	record := make([]byte, 1+savedHotCueRecordBytes)
	binary.BigEndian.PutUint64(record[1:], math.Float64bits(-1))
	return record
}

func hotCuePayload(blob []byte, sampleRate float64) ([]byte, error) {
	if len(blob) == 0 {
		payload := binary.BigEndian.AppendUint64(nil, savedHotCueSlots)
		for range savedHotCueSlots {
			payload = append(payload, emptyHotCueRecord()...)
		}
		// Native empty records in songs/m.sql use -1 for both main-cue positions
		// and zero for the modification flag.
		payload = binary.BigEndian.AppendUint64(payload, math.Float64bits(-1))
		payload = append(payload, 0)
		return binary.BigEndian.AppendUint64(payload, math.Float64bits(-1)), nil
	}
	payload, err := qUncompress(blob)
	if err != nil {
		return nil, fmt.Errorf("decompressing saved cues: %w", err)
	}
	if _, err := cuesFromBlob(sampleRate, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// The complete payload has already passed cuesFromBlob validation.
func hotCueRecordBounds(payload []byte, slot int) (int, int) {
	start := 8
	for range slot - 1 {
		start += 1 + int(payload[start]) + savedHotCueRecordBytes
	}
	return start, start + 1 + int(payload[start]) + savedHotCueRecordBytes
}

func replaceHotCueRecord(payload []byte, start, end int, record []byte) ([]byte, error) {
	result := make([]byte, 0, len(payload)-(end-start)+len(record))
	result = append(result, payload[:start]...)
	result = append(result, record...)
	result = append(result, payload[end:]...)
	return qCompress(result)
}
