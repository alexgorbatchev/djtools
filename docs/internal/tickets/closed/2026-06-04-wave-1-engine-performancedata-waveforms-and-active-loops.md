---
created_on: 2026-06-04 12:00
last_modified: 2026-06-04 12:30
status: current
ticket_status: closed
---

# Ticket: Extract Performance Waveform Data and Active Loops

## Problem
In `PerformanceData`, Engine DJ stores waveform displays (`overviewWaveFormData`, `trackData`) and loop settings (`activeOnLoadLoops`). `djtools` ignores these columns during performance data extraction.

## Why this matters
Waveform overview blobs allow DJ software and hardware to render track waveforms instantly, and `activeOnLoadLoops` preserves loop activation behavior configured by the DJ.

## Observed context
- `engine/importExtract.go`: `importExtractPerformanceData` queries only `trackId, beatData, quickCues, loops`.
- `engine/engine.go`: `performanceDataEntry` lacks waveform and active loops fields.
- `lib/library.go`: `Song` lacks performance waveform and active loop attributes.

## Acceptance criteria
- [x] Add `OverviewWaveFormData`, `TrackData`, and `ActiveOnLoadLoops` fields to `performanceDataEntry` and `lib.Song`.
- [x] Update `importExtractPerformanceData` and `importConvertPerformanceData` to capture waveform blobs and active loops.
- [x] Add unit tests verifying waveform data and active loop extraction.
- [x] Mandatory Review Pass: Run a code review pass to verify implementation correctness and error wrapping.
