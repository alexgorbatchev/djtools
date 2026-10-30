package engine

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/nateranda/djtools/lib"
)

func TestEncodeSavedMarkersMatchesDesktopPayloads(t *testing.T) {
	for _, fixture := range []struct {
		name string
		rows []int
	}{{"songs", []int{1}}, {"alteredPerformanceData", []int{1, 2, 3, 5}}} {
		for _, row := range fixture.rows {
			blobs := nativePerformanceBlobs(t, fixture.name, row)
			payload := cuePayload(t, blobs[3])
			// A unit sample rate preserves each native fractional sample exactly;
			// the separate scale test verifies source-seconds conversion.
			cues, err := cuesFromBlob(1, payload)
			if err != nil {
				t.Fatal(err)
			}
			loops, err := loopsFromBlob(1, blobs[4])
			if err != nil {
				t.Fatal(err)
			}
			grid, err := DecodeBeatData(blobs[2])
			if err != nil {
				t.Fatal(err)
			}
			markers := SavedMarkers{SampleRate: 1, SampleLimit: grid.SampleLimit, HotCues: cues.cues, Loops: loops}
			if cues.cueOriginal >= 0 {
				markers.MainCue = &cues.cueOriginal
			}
			got, err := EncodeSavedMarkers(markers)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload, cuePayload(t, got.QuickCues)) || !bytes.Equal(blobs[4], got.Loops) {
				t.Fatalf("complete Desktop payload mismatch: %s row %d", fixture.name, row)
			}
		}
	}
}

func TestEncodeSavedMarkersScaleSparseSlotsAndMainCue(t *testing.T) {
	zero := 0.0
	markers := SavedMarkers{
		SampleRate: 48000, SampleLimit: 96000, MainCue: &zero,
		HotCues: []lib.HotCue{{Position: 8, Offset: 0.125001, Name: "Écoute 🎵", Color: "#aBcD09"}},
		Loops:   []lib.Loop{{Position: 3, Start: 0.25, End: 2, Name: strings.Repeat("é", 127) + "a", Color: "#123456"}},
	}
	got, err := EncodeSavedMarkers(markers)
	if err != nil {
		t.Fatal(err)
	}
	records, trailer := nativeCueRecords(t, cuePayload(t, got.QuickCues))
	for _, record := range records[:7] {
		if !bytes.Equal(record, emptyHotCueRecord()) {
			t.Fatal("populated an absent cue slot")
		}
	}
	position := math.Float64frombits(binary.BigEndian.Uint64(records[7][1+int(records[7][0]):]))
	if position != markers.HotCues[0].Offset*48000 || !bytes.Equal(trailer, make([]byte, 17)) {
		t.Fatal("lost fractional source position or explicit zero main cue")
	}
	beat, err := EncodeUnsetBeatData(48000, 96000)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := DecodeSavedPerformance(beat, got.QuickCues, got.Loops)
	if err != nil || len(saved.Loops) != 1 || saved.Loops[0].Position != 3 || saved.Loops[0].End != 2 || len(saved.HotCues) != 1 || saved.HotCues[0].Position != 8 {
		t.Fatalf("lost sparse slot or exclusive loop end: %+v, %v", saved, err)
	}
	markers.MainCue = nil
	absent, err := EncodeSavedMarkers(markers)
	if err != nil {
		t.Fatal(err)
	}
	_, absentTrailer := nativeCueRecords(t, cuePayload(t, absent.QuickCues))
	if bytes.Equal(trailer, absentTrailer) || math.Float64frombits(binary.BigEndian.Uint64(absentTrailer)) != -1 || absentTrailer[8] != 0 {
		t.Fatal("conflated absent main cue with cue at zero")
	}
}

func TestEncodeSavedMarkersRejectsInvalidRecordsAtomically(t *testing.T) {
	validCue := lib.HotCue{Position: 1, Offset: 0.25, Color: "#123456"}
	validLoop := lib.Loop{Position: 1, Start: 0.25, End: 0.5, Color: "#123456"}
	cases := map[string]func(*SavedMarkers){
		"rate zero":      func(m *SavedMarkers) { m.SampleRate = 0 },
		"rate infinite":  func(m *SavedMarkers) { m.SampleRate = math.Inf(1) },
		"limit zero":     func(m *SavedMarkers) { m.SampleLimit = 0 },
		"limit nan":      func(m *SavedMarkers) { m.SampleLimit = math.NaN() },
		"main at end":    func(m *SavedMarkers) { end := 1.0; m.MainCue = &end },
		"main negative":  func(m *SavedMarkers) { p := -1.0; m.MainCue = &p },
		"cue duplicate":  func(m *SavedMarkers) { m.HotCues = append(m.HotCues, validCue) },
		"cue slot":       func(m *SavedMarkers) { m.HotCues[0].Position = 9 },
		"cue name":       func(m *SavedMarkers) { m.HotCues[0].Name = strings.Repeat("é", 128) },
		"cue utf8":       func(m *SavedMarkers) { m.HotCues[0].Name = string([]byte{255}) },
		"cue color":      func(m *SavedMarkers) { m.HotCues[0].Color = "#notRGB" },
		"cue at end":     func(m *SavedMarkers) { m.HotCues[0].Offset = 1 },
		"cue nan":        func(m *SavedMarkers) { m.HotCues[0].Offset = math.NaN() },
		"loop duplicate": func(m *SavedMarkers) { m.Loops = append(m.Loops, validLoop) },
		"loop slot":      func(m *SavedMarkers) { m.Loops[0].Position = 0 },
		"loop name":      func(m *SavedMarkers) { m.Loops[0].Name = strings.Repeat("a", 256) },
		"loop color":     func(m *SavedMarkers) { m.Loops[0].Color = "red" },
		"loop negative":  func(m *SavedMarkers) { m.Loops[0].Start = -0.1 },
		"loop reverse":   func(m *SavedMarkers) { m.Loops[0].End = 0.1 },
		"loop past end":  func(m *SavedMarkers) { m.Loops[0].End = 1.1 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			markers := SavedMarkers{SampleRate: 48000, SampleLimit: 48000, HotCues: []lib.HotCue{validCue}, Loops: []lib.Loop{validLoop}}
			change(&markers)
			got, err := EncodeSavedMarkers(markers)
			if err == nil || got.QuickCues != nil || got.Loops != nil {
				t.Fatalf("invalid input produced partial payloads: %+v, %v", got, err)
			}
		})
	}
}
