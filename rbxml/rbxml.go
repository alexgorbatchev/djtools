package rbxml

import (
	"encoding/xml"
	"fmt"
	"os"
	"sort"

	"github.com/nateranda/djtools/lib"
)

const version string = "0.1"

// ExportOptions options for exporting Rekordbox XML.
type ExportOptions struct {
	UseUTC bool
}

// Document represents a full Rekordbox XML (DJ_PLAYLISTS) document.
type Document struct {
	XMLName    xml.Name   `xml:"DJ_PLAYLISTS"`
	Version    string     `xml:"Version,attr"`
	Product    Product    `xml:"PRODUCT"`
	Collection Collection `xml:"COLLECTION"`
	Playlists  Playlists  `xml:"PLAYLISTS"`
}

type Product struct {
	Name    string `xml:"Name,attr"`
	Version string `xml:"Version,attr"`
	Company string `xml:"Company,attr"`
}

type Track struct {
	TrackId      int             `xml:"TrackID,attr"`
	Name         string          `xml:"Name,attr,omitempty"`
	Artist       string          `xml:"Artist,attr,omitempty"`
	Composer     string          `xml:"Composer,attr,omitempty"`
	Album        string          `xml:"Album,attr,omitempty"`
	Grouping     string          `xml:"Grouping,attr,omitempty"`
	Genre        string          `xml:"Genre,attr,omitempty"`
	Kind         string          `xml:"Kind,attr,omitempty"`
	Size         int64           `xml:"Size,attr,omitempty"`
	TotalTime    float64         `xml:"TotalTime,attr,omitempty"`
	DiscNumber   int32           `xml:"DiscNumber,attr,omitempty"`
	TrackNumber  int32           `xml:"TrackNumber,attr,omitempty"`
	Year         int32           `xml:"Year,attr,omitempty"`
	AverageBpm   float64         `xml:"AverageBpm,attr,omitempty"`
	DateModified string          `xml:"DateModidied,attr,omitempty"` // yyyy-mm-dd
	DateAdded    string          `xml:"DateAdded,attr,omitempty"`    // yyyy-mm-dd
	BitRate      int32           `xml:"BitRate,attr,omitempty"`
	SampleRate   float64         `xml:"SampleRate,attr,omitempty"`
	Comments     string          `xml:"Comments,attr,omitempty"`
	PlayCount    int32           `xml:"PlayCount,attr,omitempty"`
	LastPlayed   string          `xml:"LastPlayed,attr,omitempty"` // yyyy-mm-dd
	Rating       int32           `xml:"Rating,attr,omitempty"`
	Location     string          `xml:"Location,attr,omitempty"` // URI formatted
	Remixer      string          `xml:"Remixer,attr,omitempty"`
	Tonality     string          `xml:"Tonality,attr,omitempty"`
	Label        string          `xml:"Label,attr,omitempty"`
	Mix          string          `xml:"Mix,attr,omitempty"`
	Colour       string          `xml:"Colour,attr,omitempty"` // 0x-appended hex
	Tempo        *[]Tempo        `xml:"TEMPO,omitempty"`
	PositionMark *[]PositionMark `xml:"POSITION_MARK,omitempty"`
}

type Tempo struct {
	Inizio  float64 `xml:"Inizio,attr"`
	Bpm     float64 `xml:"Bpm,attr"`
	Metro   string  `xml:"Metro,attr"` // 4/4, 3/4, etc.
	Battito int32   `xml:"Battito,attr"`
}

type PositionMark struct {
	Name     string  `xml:"Name,attr"`
	MarkType int32   `xml:"Type,attr"` // cue=0, fade-in=1, fade-out=2, load=3, loop=4
	Start    float64 `xml:"Start,attr"`
	End      float64 `xml:"End,attr,omitempty"`
	Num      int32   `xml:"Num,attr"` // hot cue: 0, 1, 2... memory cue: -1
}

type Node struct {
	NodeType int32        `xml:"Type,attr"` // folder=0, playlist=1
	Name     string       `xml:"Name,attr"`
	Count    int32        `xml:"Count,attr,omitempty"`   // number of sub-nodes
	Entries  int32        `xml:"Entries,attr,omitempty"` // number of tracks in playlist
	KeyType  int32        `xml:"KeyType,attr"`           // trackId=0, location=1, should always be 0
	Tracks   *[]NodeTrack `xml:"TRACK,omitempty"`
	Nodes    *[]Node      `xml:"NODE,omitempty"`
}

type NodeTrack struct {
	Id int32 `xml:"Key,attr"`
}

type Collection struct {
	Entries int32   `xml:"Entries,attr"` // number of tracks
	Tracks  []Track `xml:"TRACK"`
}

type Playlists struct {
	Node Node `xml:"NODE"`
}

// Internal compatibility aliases
type product = Product
type track = Track
type tempo = Tempo
type positionMark = PositionMark
type node = Node
type nodeTrack = NodeTrack
type collection = Collection
type playlists = Playlists
type djPlaylists = Document

// NewDocument creates a new empty Rekordbox XML document.
func NewDocument() *Document {
	return &Document{
		Version: "1.0.0",
		Product: Product{
			Name:    "djtools",
			Version: version,
			Company: "djtools",
		},
		Playlists: Playlists{
			Node: Node{
				NodeType: 0,
				Name:     "ROOT",
				KeyType:  0,
			},
		},
	}
}

// OpenDocument reads and parses a Rekordbox XML document from path.
func OpenDocument(path string) (*Document, error) {
	doc := NewDocument()
	if err := doc.read(path); err != nil {
		return nil, err
	}
	return doc, nil
}

// Save writes the document to an XML file at path.
func (d *Document) Save(path string) error {
	return d.write(path)
}

// FindTrackByID finds a track by its TrackId. Returns nil if not found.
func (d *Document) FindTrackByID(id int) *Track {
	for i := range d.Collection.Tracks {
		if d.Collection.Tracks[i].TrackId == id {
			return &d.Collection.Tracks[i]
		}
	}
	return nil
}

// FindTrackByLocation finds a track by exact location URI. Returns nil if not found.
func (d *Document) FindTrackByLocation(location string) *Track {
	for i := range d.Collection.Tracks {
		if d.Collection.Tracks[i].Location == location {
			return &d.Collection.Tracks[i]
		}
	}
	return nil
}

// AddTrack adds or updates a track in the collection.
func (d *Document) AddTrack(t Track) {
	for i := range d.Collection.Tracks {
		if d.Collection.Tracks[i].TrackId == t.TrackId {
			d.Collection.Tracks[i] = t
			return
		}
	}
	d.Collection.Tracks = append(d.Collection.Tracks, t)
	d.Collection.Entries = int32(len(d.Collection.Tracks))
}

// RemoveTrack removes a track by TrackId from the collection.
func (d *Document) RemoveTrack(id int) {
	for i := range d.Collection.Tracks {
		if d.Collection.Tracks[i].TrackId == id {
			d.Collection.Tracks = append(d.Collection.Tracks[:i], d.Collection.Tracks[i+1:]...)
			d.Collection.Entries = int32(len(d.Collection.Tracks))
			break
		}
	}
}

// FindNode recursively searches for a playlist or folder node by name.
func (d *Document) FindNode(name string) *Node {
	var search func(n *Node) *Node
	search = func(n *Node) *Node {
		if n.Name == name {
			return n
		}
		if n.Nodes != nil {
			for i := range *n.Nodes {
				if found := search(&(*n.Nodes)[i]); found != nil {
					return found
				}
			}
		}
		return nil
	}
	return search(&d.Playlists.Node)
}

// ToLibrary converts the Document into a canonical lib.Library struct.
func (d *Document) ToLibrary() (lib.Library, error) {
	library, err := importConvert(d)
	if err != nil {
		return lib.Library{}, err
	}
	library.CheckCorruptedSongs()
	return library, nil
}

// FromLibrary creates a Rekordbox XML Document from a lib.Library struct.
func FromLibrary(library *lib.Library, options ...ExportOptions) (*Document, error) {
	opt := ExportOptions{UseUTC: true}
	if len(options) > 0 {
		opt = options[0]
	}
	doc, err := exportConvert(library, opt)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// sort sorts a djPlaylists struct's songs by song id, then each
// song's PositionMarks by cue, then cue points by id, then loops by id.
func (d *djPlaylists) sort() {
	sort.Slice(d.Collection.Tracks, func(i, j int) bool {
		return d.Collection.Tracks[i].TrackId < d.Collection.Tracks[j].TrackId
	})

	for i, track := range d.Collection.Tracks {
		if track.PositionMark == nil {
			continue
		}
		positionMarks := *track.PositionMark
		sort.Slice(positionMarks, func(i, j int) bool {
			if positionMarks[i].MarkType != positionMarks[j].MarkType {
				return positionMarks[i].MarkType < positionMarks[j].MarkType
			}
			return positionMarks[i].Num < positionMarks[j].Num
		})
		d.Collection.Tracks[i].PositionMark = &positionMarks
	}
}

// write writes a djPlaylists struct to an XML file at the given path.
func (d *djPlaylists) write(path string) error {
	xmlData, err := xml.MarshalIndent(d, " ", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(path, xmlData, 0644)
	if err != nil {
		return fmt.Errorf("error exporting library: %w", err)
	}
	return nil
}

// read reads a djPlaylists struct from an XML file at the given path.
func (d *djPlaylists) read(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	err = xml.Unmarshal(data, d)
	if err != nil {
		return fmt.Errorf("error unmarshaling XML file: %w", err)
	}

	return nil
}

// Import converts a Rekordbox XML file at path into a lib.Library struct.
func Import(path string) (lib.Library, error) {
	doc, err := OpenDocument(path)
	if err != nil {
		return lib.Library{}, err
	}
	return doc.ToLibrary()
}

// Export writes a lib.Library struct into a Rekordbox XML file at path.
func Export(library *lib.Library, path string, options ExportOptions) error {
	doc, err := FromLibrary(library, options)
	if err != nil {
		return err
	}
	doc.sort()
	return doc.Save(path)
}
