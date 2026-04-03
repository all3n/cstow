# Local Artifact Index Closure Design

**Date:** 2026-04-03  
**Scope:** close the in-flight local artifact index work so the CLI behavior, automatic indexing paths, tests, and docs match each other  
**In scope:** `cstow artifact list`, `cstow artifact sync`, automatic SQLite updates after successful `fetch` and repository `install`, cache-hit backfill, command/package tests, roadmap and agent docs sync  
**Out of scope:** remote registry metadata sync, artifact integrity verification, repository `artifacts` / `install_targets` install result validation, broader artifact service refactors

---

## Goal

Finish the local artifact index feature as an actual usable path rather than a partially wired branch. After this work:

- `cstow artifact list` must reliably show indexed local artifacts
- `cstow artifact sync` must rebuild the index from the standard cache layout
- successful `fetch` and repository `install` flows must populate or backfill the index automatically
- tests must cover the real command-layer behavior
- `PLAN.md` and `AGENTS.md` must describe the new current reality correctly

The filesystem cache remains the source of truth. SQLite remains a local query index.

---

## Current Context

The branch already contains most of the intended shape:

- `internal/artifactdb/` provides SQLite schema, list, upsert, and cache sync logic
- `cmd/artifact.go` exposes `cstow artifact list` and `cstow artifact sync`
- `cmd/artifact_index.go` centralizes command-layer artifact indexing
- `cmd/fetch.go` and `cmd/deps.go` already attempt to call the indexing helper
- tests exist for the store, sync logic, command output, and indexing helper

The gap is that the feature is not yet closed end-to-end. Current evidence from the branch shows:

- `internal/artifactdb` tests pass
- at least one `cmd` integration test still fails because repository install results are not ending up in the database as expected
- docs describe the direction, but `PLAN.md` and `AGENTS.md` still need to be aligned to the branch state once behavior is fixed

This design is intentionally about finishing the existing architecture, not replacing it.

---

## Recommended Approach

Keep the current package boundaries and finish the missing command-layer wiring.

Why this approach:

- it matches the existing in-flight implementation instead of restarting the design
- it is small enough to complete safely in one iteration
- it preserves the current rule that command code owns the decision to index a successful artifact outcome, while `internal/artifactdb` remains storage-focused

Alternatives considered:

1. Only patch the single failing repository-install test.
   This is too narrow for the requested goal because it would leave `artifact list/sync` and fetch/install consistency insufficiently verified.

2. Move indexing into a new lower-level artifact service.
   This would likely improve abstraction later, but it expands scope into refactoring rather than closing the user-visible feature.

---

## Architecture

### Source Of Truth

- cache directories under the standard runtime cache root stay authoritative
- SQLite stores an index of observed artifacts
- `artifact sync` repairs the index from disk
- automatic updates during successful command paths keep the index warm between syncs

### Package Boundary

Keep these responsibilities:

- `internal/artifactdb`
  - open/init the database
  - upsert and list records
  - reconcile database state with cache layout
- `cmd/artifact_index.go`
  - translate successful command outcomes into `artifactdb.Record` inputs
  - normalize legacy cache-path hits so stored `build_type` reflects real disk layout
- `cmd/artifact.go`
  - CLI output for `list`
  - CLI summary reporting for `sync`
- `cmd/fetch.go` and `cmd/deps.go`
  - decide when an artifact operation has succeeded and should be indexed

No new service layer is introduced in this slice.

---

## Required Behavior

### `cstow artifact list`

`artifact list` must:

- open the default database location
- initialize schema if needed
- read rows only from SQLite
- print a stable table ordered by `name`, `version`, `abi_tag`, `build_type`
- render empty `build_type` as `default`
- print the documented empty-state message when no rows exist

It must not rescan the filesystem implicitly.

### `cstow artifact sync`

`artifact sync` must:

- resolve the cache root through the same runtime path as the rest of the CLI
- scan both typed and legacy cache layouts
- ignore empty candidate directories
- upsert existing on-disk artifacts into SQLite
- delete stale rows that no longer exist on disk
- print the short inserted/updated/deleted summary

### Automatic Indexing

Successful artifact-producing command paths must upsert rows automatically.

`fetch` must index:

- cache hits resolved through the real cache layout
- successful registry download and extraction
- successful repository source fallback builds

`installFromRepository` must index:

- successful fresh repository builds
- successful cache-hit reuse when a repository artifact is already present on disk

### Legacy Layout Normalization

When a cache hit resolves to the legacy layout:

- the database must store `build_type = ""`
- the displayed CLI value remains `default`
- command-layer indexing must use the actual resolved install directory rather than the requested build type

This keeps the index consistent with `artifact sync`, which infers records from disk layout.

---

## Data And State Rules

The existing single-table model remains sufficient for this slice.

Important rules to preserve:

- identity remains `(name, version, abi_tag, build_type)`
- `origin` remains one of `registry`, `repository`, or `unknown`
- sync-originated inserts default to `unknown`
- later cache-hit backfills must not overwrite a known non-`unknown` origin with `unknown`
- timestamps remain normalized to UTC/RFC3339 storage

No schema change is required for the closure work unless a failing command test proves that the current schema or upsert behavior is incorrect.

---

## Error Handling

This slice keeps error handling simple and explicit:

- if the database cannot be opened or updated during a command path, return an error rather than silently skipping indexing
- `artifact list` and `artifact sync` should fail fast on database errors
- sync should continue to ignore non-artifact directories by layout rules rather than treating them as errors

The goal here is correctness and observability, not background best-effort indexing.

---

## Testing Plan

The implementation must be driven and verified through tests that reflect real command behavior.

Required coverage:

- `internal/artifactdb/store_test.go`
  - upsert/list
  - preserve known `origin` when later observations are `unknown`
- `internal/artifactdb/sync_test.go`
  - typed cache layout indexing
  - legacy cache layout indexing
  - empty-directory ignore
  - stale-row deletion
- `cmd/artifact_test.go`
  - empty-state output
  - sorted table output
  - sync summary output
- `cmd/artifact_index_test.go`
  - command-layer indexing stores the real disk layout for legacy cache hits
- `cmd/install_integration_test.go`
  - successful repository install writes a row to the database
  - cached repository install backfills a missing row without rebuilding
- existing `fetch` tests or new command-level tests
  - verify successful fetch paths call the indexing helper for cache hits, registry downloads, and repository fallback as appropriate

Required verification commands for completion:

- `go test ./internal/artifactdb/... -count=1`
- `go test ./cmd/... -count=1`
- `go test ./...` when practical after targeted fixes pass

---

## Documentation Updates

Once behavior and tests are green:

- update `PLAN.md` to state that local artifact metadata is indexed in SQLite and exposed through `cstow artifact list/sync`
- update `AGENTS.md` so future agents know:
  - the local artifact index exists
  - SQLite is a query index only
  - the filesystem cache remains authoritative

The docs must describe the implemented state, not the aspirational future state.

---

## Completion Criteria

This slice is complete when all of the following are true:

- `cstow artifact list` and `cstow artifact sync` behave as designed
- successful `fetch` and repository `install` outcomes populate or backfill the local index
- legacy cache hits are stored with `build_type = ""`
- targeted artifactdb and cmd tests pass
- the current failing repository install indexing integration test passes
- `PLAN.md` and `AGENTS.md` reflect the new current reality

Anything beyond that belongs to a later artifact-related slice.
