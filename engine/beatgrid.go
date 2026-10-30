package engine

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	beatDataHeaderBytes = 17
	beatAnchorBytes     = 24
	beatDataTailBytes   = 9
	maxBeatAnchors      = 1 << 16

	// BeatGridSourceAnalysis is the source tag Engine's grid constructor writes.
	BeatGridSourceAnalysis int8 = 2
)

var (
	// ErrInvalidBeatData reports a beatData payload that does not follow Engine's layout.
	ErrInvalidBeatData = errors.New("invalid beat data")
	// ErrInvalidBeatGrid reports anchors that do not describe a usable grid.
	ErrInvalidBeatGrid = errors.New("invalid beat grid")
)

// BeatAnchor is one native Engine beat-grid anchor. Sample positions may be
// negative for preroll; the final anchor ends the grid and counts no beats.
type BeatAnchor struct {
	Sample         float64 `json:"sample"`
	Beat           int64   `json:"beat"`
	BeatsUntilNext uint32  `json:"beatsUntilNext"`
}

// BeatData is the decoded PerformanceData.beatData payload. Default is the
// fallback grid Engine restores on reset; Adjusted is the grid Engine plays,
// shows, and edits.
type BeatData struct {
	SampleRate float64
	// SampleLimit is the sample-length limit Engine uses when extending or
	// trimming the grid's tail.
	SampleLimit float64
	IsSet       bool
	Default     []BeatAnchor
	Adjusted    []BeatAnchor
	// Source and ReferenceSample are the trailing grid metadata fields. Engine
	// writes source 2 from its constructor and 3 for imported tempo markers.
	Source          int8
	ReferenceSample float64
}

// NewBeatData builds analyzed grid data with identical default and adjusted
// grids, matching Engine's grid constructor.
func NewBeatData(sampleRate, sampleLimit float64, anchors []BeatAnchor) (BeatData, error) {
	data := BeatData{
		SampleRate:  sampleRate,
		SampleLimit: sampleLimit,
		IsSet:       true,
		Default:     append([]BeatAnchor(nil), anchors...),
		Adjusted:    append([]BeatAnchor(nil), anchors...),
		Source:      BeatGridSourceAnalysis,
	}
	return data, data.Validate()
}

// Validate checks the header values and both grids.
func (d BeatData) Validate() error {
	if !finite(d.SampleRate) || d.SampleRate <= 0 {
		return fmt.Errorf("%w: sample rate must be positive", ErrInvalidBeatGrid)
	}
	if !finite(d.SampleLimit) || d.SampleLimit < 0 {
		return fmt.Errorf("%w: sample limit must not be negative", ErrInvalidBeatGrid)
	}
	if !finite(d.ReferenceSample) {
		return fmt.Errorf("%w: reference sample must be finite", ErrInvalidBeatGrid)
	}
	if err := ValidateBeatAnchors(d.Default); err != nil {
		return fmt.Errorf("default grid: %w", err)
	}
	if err := ValidateBeatAnchors(d.Adjusted); err != nil {
		return fmt.Errorf("adjusted grid: %w", err)
	}
	return nil
}

// ValidateBeatAnchors requires at least two anchors with increasing samples,
// consistent beat indexes, a positive count on every segment, and a final
// anchor that counts no beats.
func ValidateBeatAnchors(anchors []BeatAnchor) error {
	if len(anchors) < 2 {
		return fmt.Errorf("%w: a grid needs at least two anchors", ErrInvalidBeatGrid)
	}
	if len(anchors) > maxBeatAnchors {
		return fmt.Errorf("%w: too many anchors", ErrInvalidBeatGrid)
	}
	for i, anchor := range anchors {
		if !finite(anchor.Sample) || anchor.Beat >= 1<<53 || anchor.Beat <= -(1<<53) {
			return fmt.Errorf("%w: anchor %d is out of range", ErrInvalidBeatGrid, i)
		}
		if i == len(anchors)-1 {
			if anchor.BeatsUntilNext != 0 {
				return fmt.Errorf("%w: the final anchor must count no beats", ErrInvalidBeatGrid)
			}
			break
		}
		next := anchors[i+1]
		if anchor.BeatsUntilNext == 0 || next.Sample <= anchor.Sample || next.Beat-anchor.Beat != int64(anchor.BeatsUntilNext) {
			return fmt.Errorf("%w: segment %d is inconsistent", ErrInvalidBeatGrid, i)
		}
	}
	return nil
}

// DecodeBeatData decodes a Qt-compressed beatData blob. Qt's length prefix is
// an allocation hint, so the decoder accepts payloads whose actual length
// differs from it. Grids are returned as stored; use Validate to check them.
func DecodeBeatData(blob []byte) (BeatData, error) {
	payload, err := qUncompressHint(blob)
	if err != nil {
		return BeatData{}, fmt.Errorf("%w: %w", ErrInvalidBeatData, err)
	}
	return decodeBeatPayload(payload)
}

func decodeBeatPayload(payload []byte) (BeatData, error) {
	if len(payload) < beatDataHeaderBytes {
		return BeatData{}, fmt.Errorf("%w: missing header", ErrInvalidBeatData)
	}
	data := BeatData{
		SampleRate:  math.Float64frombits(binary.BigEndian.Uint64(payload)),
		SampleLimit: math.Float64frombits(binary.BigEndian.Uint64(payload[8:])),
		IsSet:       payload[16] != 0,
	}
	if !finite(data.SampleRate) || data.SampleRate <= 0 {
		return BeatData{}, fmt.Errorf("%w: invalid sample rate", ErrInvalidBeatData)
	}
	rest := payload[beatDataHeaderBytes:]
	for _, target := range []*[]BeatAnchor{&data.Default, &data.Adjusted} {
		if len(rest) < 8 {
			return BeatData{}, fmt.Errorf("%w: missing grid vector", ErrInvalidBeatData)
		}
		count := binary.BigEndian.Uint64(rest)
		rest = rest[8:]
		if count > maxBeatAnchors || count > uint64(len(rest)/beatAnchorBytes) {
			return BeatData{}, fmt.Errorf("%w: truncated grid vector", ErrInvalidBeatData)
		}
		anchors := make([]BeatAnchor, count)
		for i := range anchors {
			anchors[i] = BeatAnchor{
				Sample:         math.Float64frombits(binary.LittleEndian.Uint64(rest)),
				Beat:           int64(binary.LittleEndian.Uint64(rest[8:])),
				BeatsUntilNext: binary.LittleEndian.Uint32(rest[16:]),
			}
			rest = rest[beatAnchorBytes:]
		}
		*target = anchors
	}
	// Older payloads omit the tail; Engine then loads source 0 and keeps the
	// reference sample at zero.
	if len(rest) >= 1 {
		data.Source = int8(rest[0])
	}
	if len(rest) >= beatDataTailBytes {
		data.ReferenceSample = math.Float64frombits(binary.BigEndian.Uint64(rest[1:]))
	}
	return data, nil
}

// EncodeBeatData validates and encodes a Qt-compressed beatData blob,
// including the trailing source and reference-sample fields.
func EncodeBeatData(data BeatData) ([]byte, error) {
	if err := data.Validate(); err != nil {
		return nil, err
	}
	return qCompress(encodeBeatPayload(data))
}

// EncodeUnsetBeatData initializes the native sample header for saved positions
// without inventing beat anchors or marking a grid as analyzed.
func EncodeUnsetBeatData(sampleRate, sampleLimit float64) ([]byte, error) {
	if !finite(sampleRate) || sampleRate <= 0 || !finite(sampleLimit) || sampleLimit < 0 {
		return nil, fmt.Errorf("%w: invalid unset grid sample rate or limit", ErrInvalidBeatData)
	}
	return qCompress(encodeBeatPayload(BeatData{SampleRate: sampleRate, SampleLimit: sampleLimit}))
}

func encodeBeatPayload(data BeatData) []byte {
	size := beatDataHeaderBytes + 16 + beatAnchorBytes*(len(data.Default)+len(data.Adjusted)) + beatDataTailBytes
	payload := make([]byte, 0, size)
	payload = binary.BigEndian.AppendUint64(payload, math.Float64bits(data.SampleRate))
	payload = binary.BigEndian.AppendUint64(payload, math.Float64bits(data.SampleLimit))
	if data.IsSet {
		payload = append(payload, 1)
	} else {
		payload = append(payload, 0)
	}
	for _, anchors := range [][]BeatAnchor{data.Default, data.Adjusted} {
		payload = binary.BigEndian.AppendUint64(payload, uint64(len(anchors)))
		for _, anchor := range anchors {
			payload = binary.LittleEndian.AppendUint64(payload, math.Float64bits(anchor.Sample))
			payload = binary.LittleEndian.AppendUint64(payload, uint64(anchor.Beat))
			payload = binary.LittleEndian.AppendUint32(payload, anchor.BeatsUntilNext)
			// Engine leaves the record padding uninitialized; it carries no data.
			payload = binary.LittleEndian.AppendUint32(payload, 0)
		}
	}
	payload = append(payload, byte(data.Source))
	return binary.BigEndian.AppendUint64(payload, math.Float64bits(data.ReferenceSample))
}

// BeatGridBPM returns the whole-grid tempo Engine reports: the beats spanned
// by the grid divided by its duration.
func BeatGridBPM(sampleRate float64, anchors []BeatAnchor) float64 {
	if len(anchors) < 2 || sampleRate <= 0 {
		return 0
	}
	first, last := anchors[0], anchors[len(anchors)-1]
	span := last.Sample - first.Sample
	if span <= 0 {
		return 0
	}
	return float64(last.Beat-first.Beat) / span * sampleRate * 60
}

const maxBeatPayloadBytes = 8 << 20

// qUncompressHint decompresses Qt framing while treating the length prefix as
// the allocation hint Qt documents rather than a checksum.
func qUncompressHint(blob []byte) ([]byte, error) {
	if len(blob) < 5 {
		return nil, errors.New("missing compressed payload")
	}
	r, err := zlib.NewReader(bytes.NewReader(blob[4:]))
	if err != nil {
		return nil, fmt.Errorf("opening payload: %w", err)
	}
	defer r.Close()
	payload, err := io.ReadAll(io.LimitReader(r, maxBeatPayloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("decompressing payload: %w", err)
	}
	if len(payload) > maxBeatPayloadBytes {
		return nil, errors.New("payload exceeds decompression limit")
	}
	return payload, nil
}
