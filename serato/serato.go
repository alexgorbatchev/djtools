package serato

import (
	"github.com/nateranda/djtools/lib"
)

// Crate represents a Serato .crate file containing playlist paths and version information.
type Crate struct {
	Filename string   `json:"filename"`
	Version  string   `json:"version"`
	Paths    []string `json:"paths"`
}

// Tags represents extracted ID3/audio tags from a track.
type Tags struct {
	Title       string  `json:"title"`
	Album       string  `json:"album"`
	Artist      string  `json:"artist"`
	Composer    string  `json:"composer"`
	Genre       string  `json:"genre"`
	Year        int     `json:"year"`
	TrackNumber int     `json:"trackNumber"`
	BPM         float64 `json:"bpm"`
	Key         string  `json:"key"`
	Comment     string  `json:"comment"`
}

// GEOB represents a General Encapsulated Object tag found in audio files.
type GEOB struct {
	Name  string `json:"name"`
	Value []byte `json:"value"`
}

// Song represents a Serato track with metadata, raw GEOB tags, and extracted performance cues.
type Song struct {
	Path  string       `json:"path"`
	Tags  Tags         `json:"tags"`
	GEOBs []GEOB       `json:"geobs,omitempty"`
	Cues  []lib.HotCue `json:"cues,omitempty"`
	Loops []lib.Loop   `json:"loops,omitempty"`
}

// Import converts a Serato library directory (or Subcrates directory) into a canonical lib.Library struct.
func Import(path string) (lib.Library, error) {
	return importExtract(path)
}
