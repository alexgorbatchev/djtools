package engine

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// nativeWriterRow is one Engine DJ payload write: the grid metadata and anchor records Engine serialized, and the
// payload bytes its native writer produced.
type nativeWriterRow struct {
	data     BeatData
	expected []byte
}

func nativeAnchors(raw []byte) []BeatAnchor {
	var out []BeatAnchor
	for len(raw) >= beatAnchorBytes {
		out = append(out, BeatAnchor{
			Sample:         math.Float64frombits(binary.LittleEndian.Uint64(raw)),
			Beat:           int64(binary.LittleEndian.Uint64(raw[8:])),
			BeatsUntilNext: binary.LittleEndian.Uint32(raw[16:]),
		})
		raw = raw[beatAnchorBytes:]
	}
	return out
}

// readNativeWriterRows reads beat_grid_payload_write.tsv: hex metadata (little-endian sample rate and limit, set
// flag, source, padding, reference sample), fallback anchors, current anchors, and the native payload.
func readNativeWriterRows(t *testing.T, path string) []nativeWriterRow {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	var rows []nativeWriterRow
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		columns := strings.Split(scanner.Text(), "\t")
		require.Len(t, columns, 4)
		var fields [4][]byte
		for i := range fields {
			fields[i], err = hex.DecodeString(columns[i])
			require.NoError(t, err)
		}
		meta := fields[0]
		require.Len(t, meta, 33)
		rows = append(rows, nativeWriterRow{
			data: BeatData{
				SampleRate:      math.Float64frombits(binary.LittleEndian.Uint64(meta)),
				SampleLimit:     math.Float64frombits(binary.LittleEndian.Uint64(meta[8:])),
				IsSet:           meta[16] != 0,
				Source:          int8(meta[17]),
				ReferenceSample: math.Float64frombits(binary.LittleEndian.Uint64(meta[24:])),
				Default:         nativeAnchors(fields[1]),
				Adjusted:        nativeAnchors(fields[2]),
			},
			expected: fields[3],
		})
	}
	require.NoError(t, scanner.Err())
	return rows
}

// zeroRecordPadding clears the four padding bytes of every anchor record; Engine copies them uninitialized and
// djtools writes zeros.
func zeroRecordPadding(payload []byte) []byte {
	out := bytes.Clone(payload)
	offset := beatDataHeaderBytes
	for range 2 {
		count := int(binary.BigEndian.Uint64(out[offset:]))
		offset += 8
		for range count {
			copy(out[offset+20:offset+24], []byte{0, 0, 0, 0})
			offset += beatAnchorBytes
		}
	}
	return out
}

// TestEncoderMatchesEngineNativeWriter compares the encoder with Engine DJ's own payload writer. The fixtures are
// recovered from Engine DJ and are not distributed with djtools; set DJTOOLS_ENGINE_FIXTURES to the directory holding
// beat_grid_payload_write.tsv (engine-dj-app's tests/fixtures) to run it.
func TestEncoderMatchesEngineNativeWriter(t *testing.T) {
	dir := os.Getenv("DJTOOLS_ENGINE_FIXTURES")
	if dir == "" {
		t.Skip("set DJTOOLS_ENGINE_FIXTURES to Engine DJ's native beat-grid fixtures")
	}
	rows := readNativeWriterRows(t, filepath.Join(dir, "beat_grid_payload_write.tsv"))
	require.NotEmpty(t, rows)
	decoded := 0
	for i, row := range rows {
		want := zeroRecordPadding(row.expected)
		got := encodeBeatPayload(row.data)
		require.Equalf(t, want, got, "row %d", i)
		if !finite(row.data.SampleRate) || row.data.SampleRate <= 0 {
			continue
		}
		back, err := decodeBeatPayload(row.expected)
		require.NoErrorf(t, err, "row %d", i)
		require.Equalf(t, want, encodeBeatPayload(back), "row %d decode", i)
		decoded++
	}
	t.Logf("matched %d native payload writes; decoded %d of them back exactly", len(rows), decoded)
}
