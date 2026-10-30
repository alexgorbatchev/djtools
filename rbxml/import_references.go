package rbxml

import (
	"fmt"
	"maps"
	"strconv"
)

type trackReferences struct {
	songIDs    []int
	byID       map[int]bool
	byLocation map[string][]int
}

func importReferences(doc *Document) (trackReferences, error) {
	refs := trackReferences{
		songIDs:    make([]int, len(doc.Collection.Tracks)),
		byID:       make(map[int]bool),
		byLocation: make(map[string][]int),
	}
	for _, track := range doc.Collection.Tracks {
		if track.TrackId == nil {
			continue
		}
		id := *track.TrackId
		if refs.byID[id] {
			return trackReferences{}, fmt.Errorf("duplicate collection track ID %d", id)
		}
		refs.byID[id] = true
	}
	used := maps.Clone(refs.byID)
	nextID := 1
	for i, track := range doc.Collection.Tracks {
		var id int
		if track.TrackId != nil {
			id = *track.TrackId
		} else {
			for used[nextID] {
				nextID++
			}
			id = nextID
			used[id] = true
			nextID++
		}
		refs.songIDs[i] = id
		refs.byLocation[track.Location] = append(refs.byLocation[track.Location], id)
	}
	return refs, nil
}

func (r trackReferences) resolve(keyType int32, key string) (int, error) {
	if keyType == 1 {
		ids := r.byLocation[key]
		if len(ids) != 1 {
			return 0, fmt.Errorf("location reference %q matches %d collection tracks", key, len(ids))
		}
		return ids[0], nil
	}
	id, err := strconv.Atoi(key)
	if err != nil {
		return 0, fmt.Errorf("reading track ID reference %q: %w", key, err)
	}
	if !r.byID[id] {
		return 0, fmt.Errorf("track ID reference %d is absent from the collection", id)
	}
	return id, nil
}
