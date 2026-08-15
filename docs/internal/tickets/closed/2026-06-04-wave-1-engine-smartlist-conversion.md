---
created_on: 2026-06-04 12:00
last_modified: 2026-06-04 12:30
status: current
ticket_status: closed
---

# Ticket: Smartlist Conversion in Engine DJ Import

## Problem
`importExtractSmartlist` queries smartlists from `m.db`, but `importConvert` completely ignores `enLibrary.smartlistList`. Smartlists/rules are discarded when converting an Engine DJ database into `lib.Library`.

## Why this matters
Smartlists/dynamic playlists contain user rules and dynamic collection criteria that should be preserved in the `lib.Library` data structure.

## Observed context
- `engine/importExtract.go`: `importExtractSmartlist` extracts smartlists into `enLibrary.smartlistList`.
- `engine/importConvert.go`: `importConvert` never calls a conversion function for smartlists.
- `lib/library.go`: `Library` needs a `Smartlists` field.

## Acceptance criteria
- [x] Add `Smartlist` struct to `lib/library.go` and `Smartlists []Smartlist` to `lib.Library`.
- [x] Implement `importConvertSmartlist` in `engine/importConvert.go` to convert extracted smartlists into `lib.Smartlist`.
- [x] Add unit tests verifying smartlist extraction and conversion.
- [x] Mandatory Review Pass: Run a code review pass to verify implementation correctness and error wrapping.
