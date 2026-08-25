package rbxml_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nateranda/djtools/lib"
	"github.com/nateranda/djtools/rbxml"
	"github.com/stretchr/testify/assert"
)

var xmlDirExport string = filepath.Join("testdata", "export", "xml")
var jsonDirExport string = filepath.Join("testdata", "export", "json")
var xmlDirImport string = filepath.Join("testdata", "import", "xml")
var jsonDirImport string = filepath.Join("testdata", "import", "json")

var exportOptions = rbxml.ExportOptions{
	UseUTC: true,
}

type test struct {
	name     string // name of test
	jsonName string // json stub name
	xmlName  string // xml stub name
	saveStub bool   // save a new xml stub or not
}

func loadXml(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("unexpected error reading from XML file at path %s: %v", path, err)
	}

	return data
}

func TestExportInvalidPath(t *testing.T) {
	var library lib.Library
	err := rbxml.Export(&library, "invalid/path.xml", exportOptions)
	assert.ErrorContains(t, err, "error exporting library: open invalid/path.xml: no such file or directory")
}

func TestExport(t *testing.T) {
	tests := []test{
		{"Empty", "empty.json", "empty.xml", false},
		{"Songs", "songs.json", "songs.xml", false},
		{"Playlists", "playlists.json", "playlists.xml", false},
		{"NestedPlaylists", "nestedPlaylists.json", "nestedPlaylists.xml", false},
		{"History", "history.json", "history.xml", false},
		{"CuesLoops", "cuesLoops.json", "cuesLoops.xml", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var library lib.Library
			path := filepath.Join(jsonDirExport, test.jsonName)
			liberr := library.Load(path)
			if liberr != nil {
				t.Fatal(liberr)
			}
			tempPath := filepath.Join(t.TempDir(), "library.xml")
			err := rbxml.Export(&library, tempPath, exportOptions)
			path = filepath.Join(xmlDirExport, test.xmlName)
			if test.saveStub {
				lib.CopyFile(t, tempPath, path)
				t.Fail()
			}
			export := loadXml(t, tempPath)
			check := loadXml(t, path)
			assert.Nil(t, err, "Valid database import should return no errors.")
			assert.Equal(t, string(check), string(export), "Library should match expected output.")
		})
	}
}

func TestImportInvalidPath(t *testing.T) {
	_, err := rbxml.Import("invalid/path/library.xml")
	assert.ErrorContains(t, err, "error reading file: open invalid/path/library.xml: no such file or directory")
}

func TestImport(t *testing.T) {
	tests := []test{
		{"Empty", "empty.json", "empty.xml", false},
		{"Songs", "songs.json", "songs.xml", false},
		{"CorruptSong", "corruptSong.json", "corruptSong.xml", false},
		{"CuesLoops", "cuesLoops.json", "cuesLoops.xml", false},
		{"Playlists", "playlists.json", "playlists.xml", false},
		{"NestedPlaylists", "nestedPlaylists.json", "nestedPlaylists.xml", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(xmlDirImport, test.xmlName)
			library, liberr := rbxml.Import(path)
			library.SortSongs()
			path = filepath.Join(jsonDirImport, test.jsonName)
			if test.saveStub {
				err := library.Save(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Fail()
			}
			var stub lib.Library
			err := stub.Load(path)
			if liberr != nil {
				t.Fatal(err)
			}
			assert.Nil(t, liberr, "Valid database import should return no errors.")
			assert.Equal(t, library, stub, "Library should match expected output.")
		})
	}
}

func TestDocument_GranularOperations(t *testing.T) {
	doc := rbxml.NewDocument()
	assert.Equal(t, "djtools", doc.Product.Name)

	// Add track
	track := rbxml.Track{
		TrackId:  1,
		Name:     "Test Track",
		Artist:   "Test Artist",
		Location: "file://localhost/music/test.mp3",
	}
	doc.AddTrack(track)
	assert.Equal(t, int32(1), doc.Collection.Entries)

	// Find by ID
	found := doc.FindTrackByID(1)
	assert.NotNil(t, found)
	assert.Equal(t, "Test Track", found.Name)

	// Find by ID not found
	assert.Nil(t, doc.FindTrackByID(999))

	// Find by Location
	foundByLoc := doc.FindTrackByLocation("file://localhost/music/test.mp3")
	assert.NotNil(t, foundByLoc)
	assert.Equal(t, 1, foundByLoc.TrackId)

	// Find by Location not found
	assert.Nil(t, doc.FindTrackByLocation("file://localhost/missing.mp3"))

	// Update track
	track.Name = "Updated Track"
	doc.AddTrack(track)
	assert.Equal(t, int32(1), doc.Collection.Entries)
	assert.Equal(t, "Updated Track", doc.FindTrackByID(1).Name)

	// Remove track
	doc.RemoveTrack(1)
	assert.Equal(t, int32(0), doc.Collection.Entries)
	assert.Nil(t, doc.FindTrackByID(1))

	// Find Node in Playlists
	rootNode := doc.FindNode("ROOT")
	assert.NotNil(t, rootNode)
	assert.Nil(t, doc.FindNode("NonExistentNode"))

	// Save and Open Document
	tempFile := filepath.Join(t.TempDir(), "roundtrip.xml")
	doc.AddTrack(rbxml.Track{TrackId: 2, Name: "Saved Track"})
	err := doc.Save(tempFile)
	assert.NoError(t, err)

	loadedDoc, err := rbxml.OpenDocument(tempFile)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), loadedDoc.Collection.Entries)
}

func TestRbxml_ConversionsAndTonalityCoverage(t *testing.T) {
	tonalities := []string{
		"8B", "8A", "9B", "9A", "10B", "10A", "11B", "11A", "12B", "12A",
		"1B", "1A", "2B", "2A", "3B", "3A", "4B", "4A", "5B", "5A", "6B", "6A", "7B", "7A",
	}

	for keyIdx, tonalityStr := range tonalities {
		// Test round trip export and import via lib.Song
		song := lib.Song{
			SongID:   keyIdx + 1,
			Title:    "Key Track " + tonalityStr,
			Key:      keyIdx,
			Path:     "/music/key_track.mp3",
			Rating:   80,
			Bpm:      120,
			Filetype: "mp3",
		}
		library := lib.Library{
			Songs: []lib.Song{song},
		}

		doc, err := rbxml.FromLibrary(&library)
		assert.NoError(t, err)
		assert.Equal(t, tonalityStr, doc.Collection.Tracks[0].Tonality)

		importedLib, err := doc.ToLibrary()
		assert.NoError(t, err)
		assert.Equal(t, keyIdx, importedLib.Songs[0].Key)
	}

	// Ratings tests: 0, 20, 40, 60, 80, 100, and 1-5 scale
	ratings := []int{0, 20, 40, 60, 80, 100, 1, 2, 3, 4, 5, 250, -1}
	for _, r := range ratings {
		song := lib.Song{
			SongID:   1,
			Title:    "Rating Track",
			Path:     "/music/rating.mp3",
			Rating:   r,
			Bpm:      120,
			Filetype: "wav",
		}
		library := lib.Library{Songs: []lib.Song{song}}
		doc, err := rbxml.FromLibrary(&library)
		assert.NoError(t, err)
		assert.NotNil(t, doc)
	}

	// Long URI truncation test
	longPath := "/music/" + string(make([]byte, 300)) + "verylongtrackname.mp3"
	songLong := lib.Song{
		SongID:   1,
		Title:    "Long Path",
		Path:     longPath,
		Bpm:      120,
		Filetype: "mp3",
	}
	docLong, err := rbxml.FromLibrary(&lib.Library{Songs: []lib.Song{songLong}})
	assert.NoError(t, err)
	assert.Contains(t, docLong.Collection.Tracks[0].Location, "file://localhost/")

	// Nested node search
	nestedNode := rbxml.Node{
		NodeType: 0,
		Name:     "Parent Folder",
		Nodes: &[]rbxml.Node{
			{
				NodeType: 1,
				Name:     "Deep Playlist",
			},
		},
	}
	docWithNested := rbxml.NewDocument()
	docWithNested.Playlists.Node.Nodes = &[]rbxml.Node{nestedNode}
	assert.NotNil(t, docWithNested.FindNode("Deep Playlist"))
}
