package rbxml_test

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nateranda/djtools/rbxml"
)

func nativeFileURI(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Host: "localhost", Path: path}).String()
}

func TestImportRejectsNonlocalAndInvalidLocations(t *testing.T) {
	local := nativeFileURI(filepath.Join(t.TempDir(), "track.wav"))
	for _, location := range []string{
		strings.Replace(local, "localhost", "remote.example", 1),
		strings.Replace(local, "localhost", "user@localhost", 1),
		strings.Replace(local, "file:", "https:", 1),
		local + "?download=1",
		local + "?",
		local + "#section",
		local + "#",
		local + "%00",
		local + "%invalid",
		"file:relative.wav",
		"file://localhost",
	} {
		t.Run(location, func(t *testing.T) {
			doc := rbxml.NewDocument()
			doc.AddTrack(rbxml.Track{TrackId: 1, Location: location, AverageBpm: 120})
			if library, err := doc.ToLibrary(); err == nil {
				t.Fatalf("invalid file URI accepted: %+v", library.Songs)
			}
		})
	}
}

func TestImportResolvesLocalFileAuthorities(t *testing.T) {
	media := filepath.Join(t.TempDir(), "literal %23 #.wav")
	if err := os.WriteFile(media, []byte("local fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	local := nativeFileURI(media)
	for _, location := range []string{
		local,
		strings.Replace(local, "localhost", "LOCALHOST", 1),
		strings.Replace(local, "localhost", "", 1),
	} {
		t.Run(location, func(t *testing.T) {
			doc := rbxml.NewDocument()
			doc.AddTrack(rbxml.Track{TrackId: 1, Location: location})
			library, err := doc.ToLibrary()
			if err != nil || len(library.Songs) != 1 {
				t.Fatalf("local file import=%+v error=%v", library, err)
			}
			if library.Songs[0].Path != media {
				t.Fatalf("decoded path=%q; want %q", library.Songs[0].Path, media)
			}
			data, err := os.ReadFile(library.Songs[0].Path)
			if err != nil || string(data) != "local fixture" {
				t.Fatalf("original local file not resolved: %q, %v", data, err)
			}
		})
	}
}

func TestImportPreservesUnanalyzedTracksAndModificationDate(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "未解析 #100%.wav")
	if err := os.WriteFile(media, []byte("native fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	// Written directly against the Rekordbox XML specification, not our exporter.
	source := fmt.Sprintf(`<DJ_PLAYLISTS Version="1.0.0">
  <COLLECTION Entries="1"><TRACK TrackID="41" Name="Unanalyzed" Location="%s" DateModified="2025-04-21"/></COLLECTION>
  <PLAYLISTS><NODE Type="0" Name="ROOT" Count="1"><NODE Type="0" Name="Folder" Count="1"><NODE Type="1" Name="Set" KeyType="0" Entries="1"><TRACK Key="41"/></NODE></NODE></NODE></PLAYLISTS>
</DJ_PLAYLISTS>`, nativeFileURI(media))
	path := filepath.Join(dir, "rekordbox.xml")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	library, err := rbxml.Import(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Songs) != 1 {
		t.Fatalf("valid unanalyzed track was removed: %+v", library.Songs)
	}
	song := library.Songs[0]
	if song.Bpm != 0 || song.Corrupt || song.Path != media {
		t.Fatalf("unanalyzed metadata changed: %+v", song)
	}
	wantDate := time.Date(2025, time.April, 21, 0, 0, 0, 0, time.UTC).Unix()
	if int64(song.DateModified) != wantDate {
		t.Fatalf("modification date = %d; want %d", song.DateModified, wantDate)
	}
	if len(library.Playlists) != 1 || len(library.Playlists[0].SubPlaylists) != 1 {
		t.Fatalf("nested hierarchy lost: %+v", library.Playlists)
	}
	members := library.Playlists[0].SubPlaylists[0].Songs
	if len(members) != 1 || members[0] != song.SongID {
		t.Fatalf("unanalyzed membership removed: %v", members)
	}
	data, err := os.ReadFile(song.Path)
	if err != nil || string(data) != "native fixture" {
		t.Fatalf("decoded URI does not identify original native file: %q, %v", data, err)
	}
}
