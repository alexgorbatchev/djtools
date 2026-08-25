package serato_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/nateranda/djtools/serato"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSerato_CrateEncodeDecode(t *testing.T) {
	original := &serato.Crate{
		Filename: "Festival Favorites",
		Version:  "1.0/8.0",
		Paths: []string{
			"/Music/House/track1.mp3",
			"/Music/Techno/track2.wav",
		},
	}

	data, err := serato.EncodeCrate(original)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	decoded, err := serato.DecodeCrate(data)
	require.NoError(t, err)
	assert.Equal(t, original.Version, decoded.Version)
	assert.Equal(t, original.Paths, decoded.Paths)
}

func TestSerato_WriteReadAndImport(t *testing.T) {
	tempDir := t.TempDir()
	subcratesDir := filepath.Join(tempDir, "Subcrates")

	crate1 := &serato.Crate{
		Filename: "Peak",
		Version:  "1.0/8.0",
		Paths: []string{
			"/DJ/TrackA.mp3",
			"/DJ/TrackB.mp3",
		},
	}

	crate2 := &serato.Crate{
		Filename: "Warmup",
		Version:  "1.0/8.0",
		Paths: []string{
			"/DJ/TrackB.mp3",
			"/DJ/TrackC.mp3",
		},
	}

	err := serato.WriteCrate(crate1, filepath.Join(subcratesDir, "Peak.crate"))
	require.NoError(t, err)
	err = serato.WriteCrate(crate2, filepath.Join(subcratesDir, "Warmup.crate"))
	require.NoError(t, err)

	// Extract crates
	crates, err := serato.ExtractCrates(tempDir)
	require.NoError(t, err)
	assert.Len(t, crates, 2)

	// Import into Library
	library, err := serato.Import(tempDir)
	require.NoError(t, err)
	assert.Len(t, library.Songs, 3)     // TrackA, TrackB, TrackC
	assert.Len(t, library.Playlists, 2) // Peak, Warmup

	// Check playlist memberships
	assert.Equal(t, "Peak", library.Playlists[0].Name)
	assert.Equal(t, []int{1, 2}, library.Playlists[0].Songs)
	assert.Equal(t, "Warmup", library.Playlists[1].Name)
	assert.Equal(t, []int{2, 3}, library.Playlists[1].Songs)

	// Read single crate
	singleCrate, err := serato.ReadCrate(filepath.Join(subcratesDir, "Peak.crate"))
	require.NoError(t, err)
	assert.Equal(t, "Peak", singleCrate.Filename)
	assert.Equal(t, []string{"/DJ/TrackA.mp3", "/DJ/TrackB.mp3"}, singleCrate.Paths)
}

func TestSerato_ParseMarkers(t *testing.T) {
	var buf []byte

	// 1. CUE entry: 4 bytes "CUE\x00" + 4 bytes length (8) + 1 byte pos(0) + 4 bytes ms (1500) + 3 bytes padding
	cueEntry := make([]byte, 8)
	cueEntry[0] = 0 // pos 0 -> Position 1
	binary.BigEndian.PutUint32(cueEntry[1:5], 1500)

	buf = append(buf, []byte("CUE\x00")...)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(cueEntry)))
	buf = append(buf, lenBuf...)
	buf = append(buf, cueEntry...)

	// 2. LOOP entry: 4 bytes "LOOP" + 4 bytes length (12) + 1 byte pos(0) + 4 bytes start (10000) + 4 bytes end (20000) + 3 bytes padding
	loopEntry := make([]byte, 12)
	loopEntry[0] = 0 // pos 0 -> Position 1
	binary.BigEndian.PutUint32(loopEntry[1:5], 10000)
	binary.BigEndian.PutUint32(loopEntry[5:9], 20000)

	buf = append(buf, []byte("LOOP")...)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(loopEntry)))
	buf = append(buf, lenBuf...)
	buf = append(buf, loopEntry...)

	cues, loops, err := serato.ParseSeratoMarkers(buf)
	require.NoError(t, err)
	require.Len(t, cues, 1)
	assert.Equal(t, 1, cues[0].Position)
	assert.Equal(t, 1.5, cues[0].Offset)

	require.Len(t, loops, 1)
	assert.Equal(t, 1, loops[0].Position)
	assert.Equal(t, 10.0, loops[0].Start)
	assert.Equal(t, 20.0, loops[0].End)
}

func TestSerato_ExtractSongWithID3(t *testing.T) {
	tempDir := t.TempDir()
	mp3Path := filepath.Join(tempDir, "test_tagged.mp3")

	// Build ID3v2.3 frame payload
	var framesBuf bytes.Buffer

	// TIT2
	writeID3TextFrame(&framesBuf, "TIT2", "Sample Title")
	// TPE1
	writeID3TextFrame(&framesBuf, "TPE1", "Sample Artist")
	// TALB
	writeID3TextFrame(&framesBuf, "TALB", "Sample Album")
	// TCON
	writeID3TextFrame(&framesBuf, "TCON", "House")
	// TYER
	writeID3TextFrame(&framesBuf, "TYER", "2024")
	// TRCK
	writeID3TextFrame(&framesBuf, "TRCK", "3")
	// COMM
	writeID3CommFrame(&framesBuf, "A comment")

	// GEOB with Serato Markers
	markerPayload := buildMockMarkerBytes()
	writeID3GEOBFrame(&framesBuf, "Serato Markers_", markerPayload)

	tagData := framesBuf.Bytes()
	tagSize := len(tagData)

	var fileBuf bytes.Buffer
	fileBuf.WriteString("ID3")
	fileBuf.WriteByte(3) // Version 2.3
	fileBuf.WriteByte(0) // Revision
	fileBuf.WriteByte(0) // Flags

	// 4 bytes syncsafe size
	fileBuf.WriteByte(byte((tagSize >> 21) & 0x7F))
	fileBuf.WriteByte(byte((tagSize >> 14) & 0x7F))
	fileBuf.WriteByte(byte((tagSize >> 7) & 0x7F))
	fileBuf.WriteByte(byte(tagSize & 0x7F))

	fileBuf.Write(tagData)
	// Add some dummy MPEG audio frames
	fileBuf.Write(make([]byte, 1024))

	err := os.WriteFile(mp3Path, fileBuf.Bytes(), 0644)
	require.NoError(t, err)

	song, err := serato.ExtractSong(mp3Path)
	require.NoError(t, err)
	assert.Equal(t, "Sample Title", song.Tags.Title)
	assert.Equal(t, "Sample Artist", song.Tags.Artist)
	assert.Equal(t, "Sample Album", song.Tags.Album)
	assert.Equal(t, "House", song.Tags.Genre)
	assert.Equal(t, 2024, song.Tags.Year)
	assert.Equal(t, 3, song.Tags.TrackNumber)
	assert.NotEmpty(t, song.GEOBs)
	assert.NotEmpty(t, song.Cues)
}

func writeID3TextFrame(buf *bytes.Buffer, frameID, text string) {
	buf.WriteString(frameID)
	payload := append([]byte{0x00}, []byte(text)...) // 0x00 = ISO-8859-1
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	buf.Write(lenBuf[:])
	buf.Write([]byte{0x00, 0x00}) // Flags
	buf.Write(payload)
}

func writeID3CommFrame(buf *bytes.Buffer, comment string) {
	buf.WriteString("COMM")
	// Encoding (1 byte), Language (3 bytes "eng"), Short desc (1 byte null), Comment text
	payload := append([]byte{0x00, 'e', 'n', 'g', 0x00}, []byte(comment)...)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	buf.Write(lenBuf[:])
	buf.Write([]byte{0x00, 0x00})
	buf.Write(payload)
}

func writeID3GEOBFrame(buf *bytes.Buffer, desc string, data []byte) {
	buf.WriteString("GEOB")
	// Encoding (0x00), MIME (null terminated), filename (null terminated), desc (null terminated), binary data
	var payload []byte
	payload = append(payload, 0x00)
	payload = append(payload, []byte("application/octet-stream\x00")...)
	payload = append(payload, []byte("\x00")...)
	payload = append(payload, []byte(desc+"\x00")...)
	payload = append(payload, data...)

	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	buf.Write(lenBuf[:])
	buf.Write([]byte{0x00, 0x00})
	buf.Write(payload)
}

func buildMockMarkerBytes() []byte {
	var buf []byte
	cueEntry := make([]byte, 8)
	cueEntry[0] = 0 // pos 0 -> Position 1
	binary.BigEndian.PutUint32(cueEntry[1:5], 2500)
	buf = append(buf, []byte("CUE\x00")...)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(cueEntry)))
	buf = append(buf, lenBuf...)
	buf = append(buf, cueEntry...)
	return buf
}

func TestSerato_ErrorsAndEdgeCases(t *testing.T) {
	// Import non-existent directory
	_, err := serato.Import(t.TempDir())
	assert.ErrorContains(t, err, "no .crate files found")

	// Read non-existent crate
	_, err = serato.ReadCrate("/non/existent/crate.crate")
	assert.Error(t, err)

	// Decode truncated data
	_, err = serato.DecodeCrate([]byte{0x01, 0x02})
	assert.ErrorContains(t, err, "file too short")

	// Parse empty markers
	_, _, err = serato.ParseSeratoMarkers(nil)
	assert.Error(t, err)

	// Extract non-existent audio song
	_, err = serato.ExtractSong("/non/existent/audio.mp3")
	assert.Error(t, err)

	// WriteCrate to invalid path
	err = serato.WriteCrate(&serato.Crate{}, "/dev/null/invalid/crate.crate")
	assert.Error(t, err)

	// Decode crate with invalid tag
	_, err = serato.DecodeCrate([]byte("xxxx\x00\x00\x00\x04\x00\x00\x00\x00"))
	assert.ErrorContains(t, err, "not of supported type")

	// Decode crate with truncated chunk length
	_, err = serato.DecodeCrate([]byte("vrsn\x00\x00"))
	assert.ErrorContains(t, err, "file too short to extract chunk length")

	// Parse base64 encoded markers
	rawMarkers := buildMockMarkerBytes()
	b64Markers := []byte(base64.StdEncoding.EncodeToString(rawMarkers))
	cues, _, err := serato.ParseSeratoMarkers(b64Markers)
	assert.NoError(t, err)
	assert.Len(t, cues, 1)

	// Decode crate with value too short for length
	_, err = serato.DecodeCrate([]byte("vrsn\x00\x00\x00\x50\x00\x00"))
	assert.ErrorContains(t, err, "file too short to extract value")

	// Decode crate with odd UTF16 bytes
	_, err = serato.DecodeCrate([]byte("vrsn\x00\x00\x00\x03\x00\x00\x00"))
	assert.ErrorContains(t, err, "invalid UTF-16")
}
