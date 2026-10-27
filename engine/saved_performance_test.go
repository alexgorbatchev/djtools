package engine

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/nateranda/djtools/lib"
)

func TestSavedPerformanceRejectsTruncation(t *testing.T) {
	for _, test := range []struct {
		name   string
		decode func([]byte) error
		size   int
	}{
		{"cues", func(data []byte) error { _, err := cuesFromBlob(48000, data); return err }, 129},
		{"loops", func(data []byte) error { _, err := loopsFromBlob(48000, data); return err }, 192},
	} {
		t.Run(test.name, func(t *testing.T) {
			for size := 0; size < test.size; size++ {
				if err := test.decode(make([]byte, size)); err == nil {
					t.Fatalf("accepted %d bytes", size)
				}
			}
		})
	}
}

func TestSavedPerformanceUnnamedAndSparseSlots(t *testing.T) {
	cues := make([]byte, 129)
	binary.BigEndian.PutUint64(cues, 8)
	loops := make([]byte, 192)
	binary.LittleEndian.PutUint64(loops, 8)
	for i := range 8 {
		binary.BigEndian.PutUint64(cues[8+i*13+1:], math.Float64bits(-1))
		binary.LittleEndian.PutUint64(loops[8+i*23+1:], math.Float64bits(-1))
		binary.LittleEndian.PutUint64(loops[8+i*23+9:], math.Float64bits(-1))
	}
	// The native empty slot sentinel is -1, independent of the name length.
	binary.BigEndian.PutUint64(cues[8+7*13+1:], math.Float64bits(96000))
	copy(cues[8+7*13+9:], []byte{255, 244, 211, 56})
	binary.LittleEndian.PutUint64(loops[8+7*23+1:], math.Float64bits(48000))
	binary.LittleEndian.PutUint64(loops[8+7*23+9:], math.Float64bits(144000))
	copy(loops[8+7*23+17:], []byte{1, 1, 255, 244, 211, 56})
	gotCues, err := cuesFromBlob(48000, cues)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotCues.cues) != 1 || gotCues.cues[0].Position != 8 || gotCues.cues[0].Offset != 2 {
		t.Fatalf("cues %+v", gotCues)
	}
	gotLoops, err := loopsFromBlob(48000, loops)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotLoops) != 1 || gotLoops[0].Position != 8 || gotLoops[0].Start != 1 || gotLoops[0].End != 3 {
		t.Fatalf("loops %+v", gotLoops)
	}
}

func TestDecodeSavedPerformanceValidation(t *testing.T) {
	compress := func(data []byte) []byte {
		t.Helper()
		result, err := QCompress(data)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	rate := make([]byte, 17)
	binary.BigEndian.PutUint64(rate, math.Float64bits(48000))
	beat := compress(rate)
	cues := createQuickCuesBlob(48000, 0, nil)
	loops := createLoopsBlob(48000, nil)
	for _, test := range []struct {
		name              string
		beat, cues, loops []byte
		wantError         bool
	}{
		{"absent", nil, nil, nil, false},
		{"empty slots", beat, compress(cues), loops, false},
		{"missing rate", nil, compress(cues), nil, true},
		{"short rate", compress([]byte{1}), compress(cues), nil, true},
		{"zero rate", compress(make([]byte, 17)), compress(cues), nil, true},
		{"bad compression", beat, []byte{1}, nil, true},
		{"bad cues", beat, compress([]byte{1}), nil, true},
		{"bad loops", beat, nil, []byte{1}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeSavedPerformance(test.beat, test.cues, test.loops)
			if (err != nil) != test.wantError {
				t.Fatalf("error %v", err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		mutate func([]byte)
		loop   bool
	}{
		{"cue count", func(b []byte) { b[7] = 9 }, false},
		{"cue name length", func(b []byte) { b[8] = 255 }, false},
		{"cue name encoding", func(b []byte) { b[8] = 1; b[9] = 255 }, false},
		{"cue NaN", func(b []byte) { binary.BigEndian.PutUint64(b[9:], math.Float64bits(math.NaN())) }, false},
		{"negative cue", func(b []byte) { binary.BigEndian.PutUint64(b[9:], math.Float64bits(-0.5)) }, false},
		{"main cue NaN", func(b []byte) { binary.BigEndian.PutUint64(b[len(b)-17:], math.Float64bits(math.NaN())) }, false},
		{"loop name length", func(b []byte) { b[8] = 255 }, true},
		{"loop NaN", func(b []byte) { binary.LittleEndian.PutUint64(b[9:], math.Float64bits(math.NaN())) }, true},
		{"loop reversed", func(b []byte) { binary.LittleEndian.PutUint64(b[9:], math.Float64bits(1)) }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.loop {
				data := append([]byte(nil), loops...)
				test.mutate(data)
				if _, err := loopsFromBlob(48000, data); err == nil {
					t.Fatal("accepted malformed loops")
				}
				return
			}
			data := append([]byte(nil), cues...)
			test.mutate(data)
			if _, err := cuesFromBlob(48000, data); err == nil {
				t.Fatal("accepted malformed cues")
			}
		})
	}
	for _, data := range [][]byte{cues[:len(cues)-1], append(cues, 0)} {
		if _, err := cuesFromBlob(48000, data); err == nil {
			t.Fatal("accepted cue tail")
		}
	}
	if _, err := loopsFromBlob(48000, append(loops, 0)); err == nil {
		t.Fatal("accepted loop tail")
	}
}

func TestSavedPerformanceCompressionBounds(t *testing.T) {
	oversized := []byte{0x08, 0, 0, 1, 0}
	if _, err := QUncompress(oversized); err == nil {
		t.Fatal("accepted oversized payload")
	}
	compressed, err := QCompress(make([]byte, 4096))
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint32(compressed, 1)
	if _, err := QUncompress(compressed); err == nil {
		t.Fatal("accepted dishonest size")
	}
	if _, _, _, err := savedSlot(nil, 12); err == nil {
		t.Fatal("accepted absent slot")
	}
}

func TestDecodeSavedPerformanceNativeFixtures(t *testing.T) {
	sql, err := os.ReadFile("testdata/import/fixtures/alteredPerformanceData/m.sql")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/import/stubs/alteredPerformanceData.json")
	if err != nil {
		t.Fatal(err)
	}
	var library lib.Library
	if err := json.Unmarshal(data, &library); err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile(`INSERT INTO PerformanceData VALUES\([^\n]+`).FindAll(sql, -1)
	for i, row := range rows {
		matches := regexp.MustCompile(`X'([0-9a-fA-F]*)'`).FindAllSubmatch(row, -1)
		blobs := make([][]byte, len(matches))
		for j, match := range matches {
			blobs[j], err = hex.DecodeString(string(match[1]))
			if err != nil {
				t.Fatal(err)
			}
		}
		got, err := DecodeSavedPerformance(blobs[2], blobs[3], blobs[4])
		if err != nil {
			t.Fatalf("track %d: %v", i+1, err)
		}
		want := library.Songs[i]
		if !reflect.DeepEqual(got.HotCues, want.Cues) || !reflect.DeepEqual(got.Loops, want.Loops) {
			t.Fatalf("track %d: got %+v, want cues %+v, loops %+v", i+1, got, want.Cues, want.Loops)
		}
	}
}
