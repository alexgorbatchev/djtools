---
created_on: 2026-06-04 12:00
last_modified: 2026-06-04 12:30
status: current
ticket_status: closed
---

# Ticket: Engine DB Schema Version & Information Checking

## Problem
`djtools` currently opens `m.db` and queries tables directly without reading or validating the `Information` table. When Engine DJ updates its database schema version (e.g. from 2.x to 3.x or 4.x), queries may break silently or produce invalid conversions due to schema mismatches.

## Why this matters
Reading and validating the `Information` table (`uuid`, `schemaVersionMajor`, `schemaVersionMinor`, `schemaVersionPatch`) ensures `djtools` can verify database compatibility before extracting data and store the database UUID in `lib.Library`.

## Observed context
- `engine/importExtract.go`: `initDB()` opens `m.db` but doesn't query `Information`.
- `engine/engine.go`: `library` struct lacks `information` metadata.
- `lib/library.go`: `Library` struct lacks `DatabaseUUID` and schema version fields.

## Acceptance criteria
- [x] Add `Information` struct and query in `engine/importExtract.go` to extract `uuid`, `schemaVersionMajor`, `schemaVersionMinor`, and `schemaVersionPatch`.
- [x] Expose `DatabaseUUID` and schema version in `lib.Library`.
- [x] Add unit tests verifying `Information` table extraction.
- [x] Mandatory Review Pass: Run a code review pass to verify implementation correctness and error wrapping.
