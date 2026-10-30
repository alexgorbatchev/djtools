package rbxml_test

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nateranda/djtools/rbxml"
)

func importReferenceDocument(t *testing.T, collection, playlists string) *rbxml.Document {
	t.Helper()
	path := filepath.Join(t.TempDir(), "references.xml")
	source := fmt.Sprintf(`<DJ_PLAYLISTS Version="1.0.0"><COLLECTION>%s</COLLECTION><PLAYLISTS><NODE Type="0" Name="ROOT">%s</NODE></PLAYLISTS></DJ_PLAYLISTS>`, collection, playlists)
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := rbxml.OpenDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func referenceLocation(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(name), 0600); err != nil {
		t.Fatal(err)
	}
	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, []byte(nativeFileURI(path))); err != nil {
		t.Fatal(err)
	}
	return escaped.String()
}

func TestImportResolvesLocationPlaylistReferences(t *testing.T) {
	a, b := referenceLocation(t, "a #%.wav"), referenceLocation(t, "b.wav")
	collection := fmt.Sprintf(`<TRACK TrackID="41" Location="%s"/><TRACK TrackID="7" Location="%s"/>`, a, b)
	playlists := fmt.Sprintf(`<NODE Type="0" Name="Folder"><NODE Type="1" Name="Locations" KeyType="1"><TRACK Key="%s"/><TRACK Key="%s"/></NODE><NODE Type="1" Name="IDs" KeyType="0"><TRACK Key="41"/><TRACK Key="7"/></NODE></NODE>`, b, a)
	doc := importReferenceDocument(t, collection, playlists)
	library, err := doc.ToLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Playlists) != 1 || len(library.Playlists[0].SubPlaylists) != 2 {
		t.Fatalf("hierarchy lost: %+v", library.Playlists)
	}
	for i, want := range [][]int{{7, 41}, {41, 7}} {
		if got := library.Playlists[0].SubPlaylists[i].Songs; !reflect.DeepEqual(got, want) {
			t.Fatalf("playlist %d membership=%v; want %v", i, got, want)
		}
	}
}

func TestImportAssignsMissingTrackIDsWithoutChangingDocument(t *testing.T) {
	a, b, c, d := referenceLocation(t, "a.wav"), referenceLocation(t, "b.wav"), referenceLocation(t, "c.wav"), referenceLocation(t, "d.wav")
	collection := fmt.Sprintf(`<TRACK Location="%s"/><TRACK TrackID="0" Location="%s"/><TRACK Location="%s"/><TRACK TrackID="1" Location="%s"/>`, a, b, c, d)
	playlists := fmt.Sprintf(`<NODE Type="1" Name="Locations" KeyType="1"><TRACK Key="%s"/><TRACK Key="%s"/><TRACK Key="%s"/></NODE><NODE Type="1" Name="Zero" KeyType="0"><TRACK Key="0"/></NODE>`, c, a, b)
	doc := importReferenceDocument(t, collection, playlists)
	before, err := xml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	library, err := doc.ToLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Songs) != 4 || library.Songs[0].SongID != 2 || library.Songs[1].SongID != 0 || library.Songs[2].SongID != 3 || library.Songs[3].SongID != 1 {
		t.Fatalf("missing and explicit zero identities lost: %+v", library.Songs)
	}
	if !reflect.DeepEqual(library.Playlists[0].Songs, []int{3, 2, 0}) || !reflect.DeepEqual(library.Playlists[1].Songs, []int{0}) {
		t.Fatalf("resolved memberships=%+v", library.Playlists)
	}
	after, err := xml.Marshal(doc)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("conversion mutated document: before=%s after=%s error=%v", before, after, err)
	}
	repeated, err := doc.ToLibrary()
	if err != nil || !reflect.DeepEqual(library, repeated) {
		t.Fatalf("repeated conversion changed identities: %+v, %v", repeated, err)
	}
}

func TestImportRejectsInvalidPlaylistReferences(t *testing.T) {
	location := referenceLocation(t, "track.wav")
	for _, test := range []struct{ name, collection, playlists string }{
		{"unknown ID", fmt.Sprintf(`<TRACK TrackID="1" Location="%s"/>`, location), `<NODE Type="1" Name="Set" KeyType="0"><TRACK Key="2"/></NODE>`},
		{"duplicate ID", fmt.Sprintf(`<TRACK TrackID="1" Location="%s"/><TRACK TrackID="1" Location="%s"/>`, location, location), ""},
		{"missing location reference", fmt.Sprintf(`<TRACK TrackID="1" Location="%s"/>`, location), `<NODE Type="1" Name="Set" KeyType="1"><TRACK Key="file://localhost/missing.wav"/></NODE>`},
		{"ambiguous location", fmt.Sprintf(`<TRACK TrackID="1" Location="%s"/><TRACK TrackID="2" Location="%s"/>`, location, location), fmt.Sprintf(`<NODE Type="1" Name="Set" KeyType="1"><TRACK Key="%s"/></NODE>`, location)},
		{"unknown key type", fmt.Sprintf(`<TRACK TrackID="1" Location="%s"/>`, location), `<NODE Type="1" Name="Set" KeyType="2"><TRACK Key="1"/></NODE>`},
		{"invalid ID text", fmt.Sprintf(`<TRACK TrackID="1" Location="%s"/>`, location), `<NODE Type="1" Name="Set" KeyType="0"><TRACK Key="not-an-id"/></NODE>`},
		{"generated ID is not a source ID", fmt.Sprintf(`<TRACK Location="%s"/>`, location), `<NODE Type="1" Name="Set" KeyType="0"><TRACK Key="1"/></NODE>`},
		{"nested unknown ID", fmt.Sprintf(`<TRACK TrackID="1" Location="%s"/>`, location), `<NODE Type="0" Name="Folder"><NODE Type="1" Name="Set"><TRACK Key="2"/></NODE></NODE>`},
		{"unknown node type", "", `<NODE Type="2" Name="Set"/>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := importReferenceDocument(t, test.collection, test.playlists)
			if library, err := doc.ToLibrary(); err == nil {
				t.Fatalf("invalid references accepted: %+v", library)
			}
		})
	}
}

func TestDocumentMaintainsTracksWithoutSourceIDs(t *testing.T) {
	doc := rbxml.NewDocument()
	doc.AddTrack(rbxml.Track{Location: "file://localhost/a.wav", Name: "A"})
	doc.AddTrack(rbxml.Track{Location: "file://localhost/b.wav", Name: "B"})
	doc.AddTrack(rbxml.Track{TrackId: new(0), Location: "file://localhost/c.wav", Name: "Zero"})
	doc.AddTrack(rbxml.Track{Location: "file://localhost/a.wav", Name: "Updated A"})
	if len(doc.Collection.Tracks) != 3 || doc.Collection.Tracks[0].Name != "Updated A" || doc.Collection.Tracks[1].Name != "B" {
		t.Fatalf("ID-less tracks overwritten: %+v", doc.Collection.Tracks)
	}
	if track := doc.FindTrackByID(0); track == nil || track.Name != "Zero" {
		t.Fatalf("explicit zero ID confused with absent ID: %+v", track)
	}
	doc.RemoveTrack(0)
	if len(doc.Collection.Tracks) != 2 || doc.FindTrackByID(0) != nil {
		t.Fatalf("wrong track removed: %+v", doc.Collection.Tracks)
	}
}

func TestImportPreservesRepeatedLocationsWithIDReferences(t *testing.T) {
	location := referenceLocation(t, "shared.wav")
	collection := fmt.Sprintf(`<TRACK TrackID="1" Location="%s"/><TRACK TrackID="2" Location="%s"/>`, location, location)
	doc := importReferenceDocument(t, collection, `<NODE Type="1" Name="IDs"><TRACK Key="2"/><TRACK Key="1"/></NODE>`)
	library, err := doc.ToLibrary()
	if err != nil || len(library.Songs) != 2 || !reflect.DeepEqual(library.Playlists[0].Songs, []int{2, 1}) {
		t.Fatalf("unambiguous ID membership lost: %+v, %v", library, err)
	}
}
