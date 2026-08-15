---
created_on: 2026-06-04 12:00
last_modified: 2026-06-04 12:30
status: current
ticket_status: closed
---

# Ticket: Extended Track Metadata Fields and AlbumArt Support

## Problem
`djtools` drops critical track metadata fields present in Engine DJ's `Track` table, such as `bpmAnalyzed`, `originDatabaseUuid`, `originTrackId`, `isAnalyzed`, `isBeatGridLocked`, `explicitLyrics`, `timeLastPlayed`, `isPlayed`, `playedIndicator`, `dateCreated`, and streaming track metadata (`streamingSource`, `uri`, `streamingFlags`). Furthermore, the `AlbumArt` table and track artwork references (`albumArtId`, `albumArtSourceHash`) are completely omitted.

## Why this matters
Without these fields, tracks converted or analyzed lose essential Engine DJ metadata, including accurate analyzed BPMs, cross-drive tracking UUIDs, artwork relationships, explicit lyrics tags, play history, and streaming flags.

## Observed context
- `engine/engine.go`: `songNull` struct is missing extended fields.
- `engine/importExtract.go`: `importExtractTrack` query omits extended columns.
- `engine/importConvert.go`: `importConvertSong` omits extended fields when populating `lib.Song`.
- `lib/library.go`: `Song` struct lacks extended Engine DJ fields.

## Acceptance criteria
- [x] Extend `lib.Song` with `BpmAnalyzed`, `OriginDatabaseUUID`, `OriginTrackID`, `IsAnalyzed`, `IsBeatGridLocked`, `ExplicitLyrics`, `TimeLastPlayed`, `IsPlayed`, `PlayedIndicator`, `DateCreated`, `StreamingSource`, `URI`, `StreamingFlags`, `AlbumArtID`, and `AlbumArtSourceHash`.
- [x] Add `AlbumArt` struct to `lib/library.go` and extract `AlbumArt` table rows into `lib.Library.AlbumArt`.
- [x] Update `importExtractTrack` and `importConvertSong` to map all extended columns cleanly.
- [x] Add unit tests verifying extraction and conversion of extended track fields and artwork.
- [x] Mandatory Review Pass: Run a code review pass to verify implementation correctness and error wrapping.
