package engine

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/nateranda/djtools/lib"
)

func nativePerformanceBlobs(t *testing.T, fixture string, row int) [][]byte {
	t.Helper()
	sql, err := os.ReadFile(filepath.Join("testdata", "import", "fixtures", fixture, "m.sql"))
	if err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile(`INSERT INTO PerformanceData VALUES\([^\n]+`).FindAll(sql, -1)
	if row < 1 || row > len(rows) {
		t.Fatalf("missing fixture row %d", row)
	}
	matches := regexp.MustCompile(`X'([0-9a-fA-F]*)'`).FindAllSubmatch(rows[row-1], -1)
	blobs := make([][]byte, len(matches))
	for i, match := range matches {
		blobs[i], err = hex.DecodeString(string(match[1]))
		if err != nil {
			t.Fatal(err)
		}
	}
	return blobs
}

func cuePayload(t *testing.T, blob []byte) []byte {
	t.Helper()
	payload, err := QUncompress(blob)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func compressedCuePayload(t *testing.T, payload []byte) []byte {
	t.Helper()
	blob, err := QCompress(payload)
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

func nativeCueRecords(t *testing.T, payload []byte) ([][]byte, []byte) {
	t.Helper()
	records := make([][]byte, 0, 8)
	rest := payload[8:]
	for range 8 {
		_, _, next, err := savedSlot(rest, 12)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, rest[:len(rest)-len(next)])
		rest = next
	}
	return records, rest
}

func TestSetSavedHotCuePreservesNativeRecords(t *testing.T) {
	for _, row := range []int{4, 5} {
		for slot := 1; slot <= 8; slot++ {
			blobs := nativePerformanceBlobs(t, "alteredPerformanceData", row)
			before := append([]byte(nil), blobs[3]...)
			records, trailer := nativeCueRecords(t, cuePayload(t, before))
			cue := lib.HotCue{Position: slot, Offset: 1.23456789, Name: "Écoute 🎵", Color: "#123aBc"}
			got, err := SetSavedHotCue(blobs[2], blobs[3], cue)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeSavedPerformance(blobs[2], got, blobs[4])
			if err != nil {
				t.Fatal(err)
			}
			want := cue
			want.Color = "#123ABC"
			found := false
			for _, saved := range decoded.HotCues {
				if saved.Position == slot {
					found = true
					if saved.Name != want.Name || saved.Color != want.Color || math.Abs(saved.Offset-want.Offset) > 1e-12 {
						t.Fatalf("saved cue %+v, want %+v", saved, want)
					}
				}
			}
			if !found {
				t.Fatalf("missing slot %d", slot)
			}
			afterRecords, afterTrailer := nativeCueRecords(t, cuePayload(t, got))
			for i := range records {
				if i != slot-1 && !bytes.Equal(records[i], afterRecords[i]) {
					t.Fatalf("changed unrelated slot %d", i+1)
				}
			}
			if !bytes.Equal(trailer, afterTrailer) || !bytes.Equal(before, blobs[3]) {
				t.Fatal("changed main cue or input blob")
			}
		}
	}
}

func TestSavedHotCueInitializationAndClear(t *testing.T) {
	blobs := nativePerformanceBlobs(t, "songs", 1)
	nativeEmpty := cuePayload(t, blobs[3])
	beat, err := EncodeUnsetBeatData(48000, 480000)
	if err != nil {
		t.Fatal(err)
	}
	grid, err := DecodeBeatData(beat)
	if err != nil {
		t.Fatal(err)
	}
	if grid.SampleRate != 48000 || grid.SampleLimit != 480000 || grid.IsSet || len(grid.Default) != 0 || len(grid.Adjusted) != 0 {
		t.Fatalf("initialized fake grid %+v", grid)
	}
	for slot := 1; slot <= 8; slot++ {
		got, err := SetSavedHotCue(beat, nil, lib.HotCue{Position: slot, Offset: 0, Color: "#F4D338"})
		if err != nil {
			t.Fatal(err)
		}
		records, _ := nativeCueRecords(t, cuePayload(t, got))
		if records[slot-1][len(records[slot-1])-4] != 255 {
			t.Fatal("new cue is not opaque")
		}
		got, err = ClearSavedHotCue(got, slot)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(cuePayload(t, got), nativeEmpty) {
			t.Fatalf("slot %d did not restore native empty payload", slot)
		}
	}
	got, err := ClearSavedHotCue(nil, 1)
	if err != nil || len(got) != 0 {
		t.Fatalf("clearing absent blob: %x, %v", got, err)
	}
}

func TestSavedHotCueMaximumNativeName(t *testing.T) {
	blobs := nativePerformanceBlobs(t, "songs", 1)
	name := strings.Repeat("é", 127) + "A"
	got, err := SetSavedHotCue(blobs[2], blobs[3], lib.HotCue{Position: 1, Name: name, Color: "#123456"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSavedPerformance(blobs[2], got, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.HotCues) != 1 || decoded.HotCues[0].Name != name {
		t.Fatalf("native byte-length name lost: %+v", decoded.HotCues)
	}
}

func TestClearSavedHotCuePreservesOtherNativeData(t *testing.T) {
	blobs := nativePerformanceBlobs(t, "alteredPerformanceData", 5)
	before := append([]byte(nil), blobs[3]...)
	records, trailer := nativeCueRecords(t, cuePayload(t, before))
	for slot := 1; slot <= 8; slot++ {
		got, err := ClearSavedHotCue(blobs[3], slot)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeSavedPerformance(blobs[2], got, blobs[4])
		if err != nil {
			t.Fatal(err)
		}
		if len(decoded.HotCues) != 7 {
			t.Fatalf("cleared %d cues", 8-len(decoded.HotCues))
		}
		for _, cue := range decoded.HotCues {
			if cue.Position == slot {
				t.Fatalf("slot %d still present", slot)
			}
		}
		afterRecords, afterTrailer := nativeCueRecords(t, cuePayload(t, got))
		for i := range records {
			if i != slot-1 && !bytes.Equal(records[i], afterRecords[i]) {
				t.Fatalf("changed slot %d", i+1)
			}
		}
		if !bytes.Equal(trailer, afterTrailer) || !bytes.Equal(before, blobs[3]) {
			t.Fatal("changed main cue or input")
		}
	}
}

func TestSavedHotCuePreservesAlpha(t *testing.T) {
	blobs := nativePerformanceBlobs(t, "alteredPerformanceData", 5)
	payload := cuePayload(t, blobs[3])
	records, _ := nativeCueRecords(t, payload)
	records[0][len(records[0])-4] = 73
	got, err := SetSavedHotCue(blobs[2], compressedCuePayload(t, payload), lib.HotCue{Position: 1, Offset: 2, Color: "#123456"})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := nativeCueRecords(t, cuePayload(t, got))
	if after[0][len(after[0])-4] != 73 {
		t.Fatal("changed native cue alpha")
	}
}

func TestSavedHotCueRejectsInvalidInputs(t *testing.T) {
	blobs := nativePerformanceBlobs(t, "alteredPerformanceData", 5)
	valid := lib.HotCue{Position: 1, Offset: 2, Color: "#123456"}
	for _, test := range []struct {
		name string
		cue  lib.HotCue
	}{
		{"slot zero", lib.HotCue{Position: 0, Color: "#123456"}},
		{"slot nine", lib.HotCue{Position: 9, Color: "#123456"}},
		{"negative", lib.HotCue{Position: 1, Offset: -1, Color: "#123456"}},
		{"NaN", lib.HotCue{Position: 1, Offset: math.NaN(), Color: "#123456"}},
		{"infinite", lib.HotCue{Position: 1, Offset: math.Inf(1), Color: "#123456"}},
		{"sample overflow", lib.HotCue{Position: 1, Offset: math.MaxFloat64, Color: "#123456"}},
		{"UTF8", lib.HotCue{Position: 1, Name: string([]byte{255}), Color: "#123456"}},
		{"name bytes", lib.HotCue{Position: 1, Name: strings.Repeat("é", 128), Color: "#123456"}},
		{"color missing", lib.HotCue{Position: 1}},
		{"color suffix", lib.HotCue{Position: 1, Color: "#1234567"}},
		{"color malformed", lib.HotCue{Position: 1, Color: "#12ZZ56"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := SetSavedHotCue(blobs[2], blobs[3], test.cue); err == nil {
				t.Fatal("accepted invalid cue")
			}
		})
	}
	for _, slot := range []int{0, 9} {
		if _, err := ClearSavedHotCue(blobs[3], slot); err == nil {
			t.Fatal("accepted invalid clear slot")
		}
	}
	for _, beat := range [][]byte{nil, {1}, compressedCuePayload(t, make([]byte, 17))} {
		if _, err := SetSavedHotCue(beat, blobs[3], valid); err == nil {
			t.Fatal("accepted invalid beat data")
		}
	}
	for _, test := range []struct{ rate, limit float64 }{{0, 1}, {-1, 1}, {math.NaN(), 1}, {math.Inf(1), 1}, {48000, -1}, {48000, math.NaN()}, {48000, math.Inf(1)}} {
		if _, err := EncodeUnsetBeatData(test.rate, test.limit); err == nil {
			t.Fatal("accepted invalid unset grid")
		}
	}
	payload := cuePayload(t, blobs[3])
	for _, malformed := range [][]byte{{1}, compressedCuePayload(t, payload[:len(payload)-1]), compressedCuePayload(t, append(payload, 0))} {
		if _, err := SetSavedHotCue(blobs[2], malformed, valid); err == nil {
			t.Fatal("set accepted malformed cues")
		}
		if _, err := ClearSavedHotCue(malformed, 1); err == nil {
			t.Fatal("clear accepted malformed cues")
		}
	}
	mutated := append([]byte(nil), payload...)
	binary.BigEndian.PutUint64(mutated[len(mutated)-17:], math.Float64bits(math.NaN()))
	if _, err := ClearSavedHotCue(compressedCuePayload(t, mutated), 1); err == nil {
		t.Fatal("clear accepted invalid main cue")
	}
	if !reflect.DeepEqual(payload, cuePayload(t, blobs[3])) {
		t.Fatal("validation modified input")
	}
}
