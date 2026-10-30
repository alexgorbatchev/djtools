package engine

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/nateranda/djtools/lib"
)

func TestEncodeTempoGridRejectsUnrepresentableSegmentPromptly(t *testing.T) {
	const selector = "DJTOOLS_TEST_TEMPO_OVERFLOW"
	if scenario := os.Getenv(selector); scenario != "" {
		testUnrepresentableTempo(t, scenario)
		return
	}
	for _, scenario := range []string{"huge finite alignment", "infinite alignment", "tail count overflow"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEncodeTempoGridRejectsUnrepresentableSegmentPromptly$")
			cmd.Env = append(os.Environ(), selector+"="+scenario)
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil || err != nil {
				t.Fatalf("unrepresentable grid did not reject promptly: %v, %v\n%s", ctx.Err(), err, output)
			}
		})
	}
}

func testUnrepresentableTempo(t *testing.T, scenario string) {
	t.Helper()
	markers := []lib.Marker{{Bpm: 1e308}, {StartPosition: 1, Bpm: 120}}
	limit := 480000.0
	if scenario == "infinite alignment" {
		markers[1].StartPosition = 1000
		limit = 480000000
	}
	if scenario == "tail count overflow" {
		markers = []lib.Marker{{Bpm: 60 * (float64(math.MaxUint32) + 1)}}
	}
	// Exercise the existing database callers' conversion as well as the public
	// pure boundary, so safety cannot depend on a wrapper's validation alone.
	if _, err := createBeatDataBlob(48000, limit, markers); !errors.Is(err, ErrInvalidBeatGrid) {
		t.Fatalf("existing marker conversion accepted an unrepresentable count: %v", err)
	}
	if blob, err := EncodeTempoGrid(48000, limit, markers); !errors.Is(err, ErrInvalidBeatGrid) || blob != nil {
		t.Fatalf("public encoder produced an unrepresentable grid: %d bytes, %v", len(blob), err)
	}
}

func TestEncodeTempoGridPreservesMarkerTimingAndNativeFields(t *testing.T) {
	markers := []lib.Marker{
		{StartPosition: 0.5, Bpm: 120, BeatNumber: 2},
		{StartPosition: 4.5, Bpm: 60, BeatNumber: 2},
	}
	before := append([]lib.Marker(nil), markers...)
	blob, err := EncodeTempoGrid(44100, 10*44100, markers)
	if err != nil {
		t.Fatal(err)
	}
	data, err := DecodeBeatData(blob)
	if err != nil {
		t.Fatal(err)
	}
	want := []BeatAnchor{
		{Sample: 0.5 * 44100, Beat: 2, BeatsUntilNext: 8},
		{Sample: 4.5 * 44100, Beat: 10, BeatsUntilNext: 6},
		{Sample: 10.5 * 44100, Beat: 16},
	}
	if !reflect.DeepEqual(data.Adjusted, want) || !reflect.DeepEqual(data.Default, want) {
		t.Fatalf("tempo changes or fallback changed: %+v", data)
	}
	if data.Source != 3 || data.ReferenceSample != 0 || !data.IsSet || data.SampleRate != 44100 || data.SampleLimit != 441000 {
		t.Fatalf("imported native metadata: %+v", data)
	}
	if !reflect.DeepEqual(markers, before) {
		t.Fatal("encoding mutated caller-owned markers")
	}
}

func TestEncodeTempoGridWithoutMarkersRemainsUnset(t *testing.T) {
	blob, err := EncodeTempoGrid(48000, 123456.5, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := DecodeBeatData(blob)
	if err != nil {
		t.Fatal(err)
	}
	if data.IsSet || data.Source != 0 || len(data.Default) != 0 || len(data.Adjusted) != 0 || data.SampleLimit != 123456.5 || data.SampleRate != 48000 {
		t.Fatalf("missing markers became an invented grid: %+v", data)
	}
}

func TestEncodeTempoGridRejectsInvalidImportsAtomically(t *testing.T) {
	valid := lib.Marker{StartPosition: 0.25, Bpm: 120, BeatNumber: 0}
	tests := []struct {
		name    string
		rate    float64
		limit   float64
		markers []lib.Marker
	}{
		{"zero rate", 0, 480000, nil},
		{"nan rate", math.NaN(), 480000, nil},
		{"zero extent", 48000, 0, nil},
		{"infinite extent", 48000, math.Inf(1), nil},
		{"negative position", 48000, 480000, []lib.Marker{{StartPosition: -1, Bpm: 120}}},
		{"at exclusive end", 48000, 480000, []lib.Marker{{StartPosition: 10, Bpm: 120}}},
		{"nan position", 48000, 480000, []lib.Marker{{StartPosition: math.NaN(), Bpm: 120}}},
		{"overflowed sample", math.MaxFloat64, 480000, []lib.Marker{{StartPosition: 2, Bpm: 120}}},
		{"zero tempo", 48000, 480000, []lib.Marker{{StartPosition: 0}}},
		{"infinite tempo", 48000, 480000, []lib.Marker{{Bpm: math.Inf(1)}}},
		{"overflowed beat length", 48000, 480000, []lib.Marker{{Bpm: math.SmallestNonzeroFloat64}}},
		{"invalid bar", 48000, 480000, []lib.Marker{{Bpm: 120, BeatNumber: 4}}},
		{"negative bar", 48000, 480000, []lib.Marker{{Bpm: 120, BeatNumber: -1}}},
		{"unordered", 48000, 480000, []lib.Marker{valid, {StartPosition: 0.1, Bpm: 120}}},
		{"duplicate time", 48000, 480000, []lib.Marker{valid, valid}},
		{"unrepresentable count", 48000, 480000, []lib.Marker{{Bpm: math.MaxFloat64}}},
		{"too many markers", 48000, 480000, make([]lib.Marker, maxBeatAnchors)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob, err := EncodeTempoGrid(tt.rate, tt.limit, tt.markers)
			if !errors.Is(err, ErrInvalidBeatGrid) || blob != nil {
				t.Fatalf("invalid grid produced partial bytes: %d bytes, %v", len(blob), err)
			}
		})
	}
}
