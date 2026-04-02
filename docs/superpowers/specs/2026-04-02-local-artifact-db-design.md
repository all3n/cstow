# Local Artifact Database Design

**Date:** 2026-04-02  
**Scope:** local artifact metadata indexing and CLI inspection  
**In scope:** `cstow artifact list`, `cstow artifact sync`, automatic SQLite updates after successful `fetch` / `install` flows  
**Out of scope:** remote registry metadata sync, arbitrary local directory import, artifact integrity validation, dependency graph queries

---

## Goal

Add a local SQLite-backed artifact index so `cstow` can list cached artifacts quickly without rescanning the filesystem every time. The filesystem cache remains the source of truth; the database is a queryable index that is updated on successful artifact operations and can be repaired by an explicit sync command.

---

## Current Context

Today artifact metadata is implicit:

- cache paths under `~/.cstow/cache/<name>/<version>/<abi>/<build_type>/`
- legacy cache paths under `~/.cstow/cache/<name>/<version>/<abi>/`
- lock file entries in `cstow.lock`
- registry manifest metadata used during `fetch`

There is no local index for answering "what artifacts do I already have?" quickly. `fetch` and `install` already determine the concrete package name, version, ABI tag, build type, and install directory, so those command paths are the natural place to update an index.

Important current-reality constraint: runtime cache resolution still follows `resolver.NewFSCache()` (`CSTOW_CACHE_DIR` or `~/.cstow/cache`) rather than the global `cache.dir` config field. The artifact index should use the same cache root for now so command behavior stays consistent with existing code.

---

## Architecture

### Source of Truth

- The cache directory stays authoritative.
- `~/.cstow/cstow.db` stores an index of what has been observed in that cache.
- `cstow artifact list` reads only from SQLite.
- `cstow artifact sync` rescans the cache and repairs SQLite to match disk.

### Package Boundary

Add a new `internal/artifactdb/` package rather than extending `internal/resolver/`.

Responsibilities:

- open the default database path
- initialize schema and schema version
- upsert one artifact record
- list indexed artifacts
- reconcile database state with the cache directory

Non-responsibilities:

- lock file resolution
- build/install orchestration
- remote registry access
- validating artifact contents against repository `artifacts` definitions

### SQLite Access

Use `database/sql` with a SQLite driver and keep the command code unaware of SQL details. The package should expose small, purpose-built methods instead of leaking SQL statements into `cmd/`.

Preferred driver: `modernc.org/sqlite`, because it avoids CGO and keeps the CLI portable in the same way as the rest of the current Go codebase.

### Database Location

- database file: `~/.cstow/cstow.db`
- parent directory: `~/.cstow/`
- schema versioning: `PRAGMA user_version = 1`

The database lives next to `config.toml`, not inside the cache tree, so cache cleanup and database lifecycle stay separable.

---

## Data Model

Use a single table in v1.

```sql
CREATE TABLE IF NOT EXISTS artifacts (
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    abi_tag TEXT NOT NULL,
    build_type TEXT NOT NULL DEFAULT '',
    install_dir TEXT NOT NULL,
    origin TEXT NOT NULL DEFAULT 'unknown',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    PRIMARY KEY (name, version, abi_tag, build_type)
);

CREATE INDEX IF NOT EXISTS idx_artifacts_name
    ON artifacts (name);

CREATE INDEX IF NOT EXISTS idx_artifacts_updated_at
    ON artifacts (updated_at DESC);
```

Field meanings:

- `name`, `version`, `abi_tag`, `build_type`: artifact identity
- `install_dir`: absolute cache path on disk
- `origin`: `registry`, `repository`, or `unknown`
- `created_at`: first time the row was inserted
- `updated_at`: last time metadata changed
- `last_seen_at`: last time the artifact was confirmed to exist on disk

### Build Type Representation

- typed cache layout: store `static`, `shared`, or `header-only`
- legacy cache layout without build type directory: store `""`
- UI output should render empty build type as `default`, matching existing display behavior in `fetch`

### Record Type

```go
type Record struct {
    Name       string
    Version    string
    ABITag     string
    BuildType  string
    InstallDir string
    Origin     string
    CreatedAt  time.Time
    UpdatedAt  time.Time
    LastSeenAt time.Time
}
```

`internal/artifactdb` should normalize timestamps to UTC RFC3339 strings when storing them.

---

## Command Surface

Add a new top-level command group:

```text
cstow artifact list
cstow artifact sync
```

### `cstow artifact list`

Purpose: show indexed local artifacts quickly.

Behavior:

- opens `~/.cstow/cstow.db`
- initializes schema if the file does not exist yet
- queries all rows ordered by `name`, `version`, `abi_tag`, `build_type`
- prints a table to stdout
- does not scan the filesystem

Initial output columns:

- `NAME`
- `VERSION`
- `ABI`
- `TYPE`
- `ORIGIN`
- `PATH`
- `UPDATED`

If there are no indexed rows, print a clear empty-state message and exit `0`:

```text
No indexed artifacts found.
Run `cstow artifact sync` to scan the local cache.
```

Use `text/tabwriter` for stable CLI formatting.

### `cstow artifact sync`

Purpose: repair or rebuild the SQLite index from the cache directory.

Behavior:

- resolves cache root through the same mechanism as current runtime code (`resolver.NewFSCache()`)
- scans the cache directory for artifact install prefixes
- upserts records for directories present on disk
- deletes rows whose `install_dir` no longer exists after the scan
- prints a short summary

Summary format:

```text
artifact sync complete
inserted: N
updated: N
deleted: N
```

`sync` is the explicit repair path. It is not run implicitly by `list`.

---

## Cache Scan Rules

`artifact sync` only scans the standard cache root. It does not accept arbitrary import paths.

Recognized layouts:

1. Typed layout:
   `/<cache>/<name>/<version>/<abi>/<build_type>/`
2. Legacy layout:
   `/<cache>/<name>/<version>/<abi>/`

Rules:

- only index directories under the cache root
- only accept `build_type` values `static`, `shared`, `header-only`
- treat any 3-level leaf (`name/version/abi`) as a legacy artifact only when it is not followed by a valid build type directory
- ignore empty candidate directories to reduce false positives from failed downloads/builds that created a directory but never installed files
- do not inspect file contents or try to validate `include/lib/bin/share` semantics in v1

When `sync` inserts a record that was not previously known, `origin` is `unknown`. When `sync` sees an existing row, it preserves a known `origin` instead of overwriting it with `unknown`.

When a command or sync path observes a legacy cache directory, the database must store `build_type = ""` based on the real on-disk layout rather than the originally requested build type. This keeps automatic updates and `sync` reconciliation consistent for backward-compatible cache hits.

---

## Automatic Database Updates

The index should be updated for successful command outcomes, including cache hits that resolve to a real artifact path.

### `cmd/fetch.go`

Update points:

1. cache hit through `findCachedPackage(...)`
   - upsert row with:
     - `install_dir = resolved path`
     - `origin = unknown` unless an existing row already has a known origin
     - `build_type` derived from the resolved path layout, not blindly from the requested build type
2. registry download + extract success
   - upsert row with `origin = registry`
3. source fallback result from repository build
   - upsert row with `origin = repository`

`fetch` already knows the concrete package name, version, ABI tag, build type, and destination directory, so no additional resolution pass is needed.

### `cmd/install.go` / `installFromRepository(...)`

Update points:

1. cached repository artifact returned from `installFromRepository(...)`
   - upsert row with `origin = repository`
   - store legacy cache hits with `build_type = ""` if the resolved path came from the compatibility layout
2. successful repository build/install
   - upsert row with `origin = repository`

Placing the write near `installFromRepository(...)` keeps the repository build flow consistent for both direct `cstow install` and `fetch --source-fallback`.

### Upsert Semantics

Use `INSERT ... ON CONFLICT DO UPDATE`.

Rules:

- row absent: insert with `created_at = updated_at = last_seen_at = now`
- row present and metadata changed: update `install_dir`, `origin` if the new origin is known, `updated_at`, `last_seen_at`
- row present and metadata unchanged: refresh `last_seen_at`; leave `updated_at` unchanged
- never overwrite a known `origin` with `unknown`

---

## Internal API Shape

`internal/artifactdb/` should stay small and explicit.

```go
type Store struct {
    db *sql.DB
}

type SyncStats struct {
    Inserted int
    Updated  int
    Deleted  int
}

func OpenDefault() (*Store, error)
func Open(path string) (*Store, error)
func (s *Store) Close() error
func (s *Store) Upsert(record Record) error
func (s *Store) List() ([]Record, error)
func (s *Store) SyncFromCache(cacheRoot string) (SyncStats, error)
```

Command code should only call these methods and should not embed SQL.

---

## Error Handling

### Database Errors

- `artifact list`: return the database error and fail the command
- `artifact sync`: return the database or scan error and fail the command
- `fetch` / `install`: database update failures should fail the command

Rationale: a stale or silently missing index defeats the purpose of making artifact metadata reliably queryable. Since database writes happen after success has already been established, surfacing the error is better than pretending the operation completed fully.

### Missing Home Directory / `.cstow`

- ensure `~/.cstow/` exists before opening `cstow.db`
- return a wrapped error if the home directory cannot be resolved

### Missing Cache Directory

- `artifact sync` against a non-existent cache root succeeds with zero rows after deleting stale index entries
- `artifact list` remains purely database-backed

---

## Testing

### `internal/artifactdb/`

Add focused unit tests for:

- schema creation on first open
- insert then list
- upsert updates `updated_at` only when metadata changes
- known origin is not downgraded to `unknown`
- sync inserts typed-layout artifacts
- sync inserts legacy-layout artifacts with empty `build_type`
- sync ignores empty artifact directories
- sync deletes stale rows missing on disk

Use temp directories and a temp database path; do not touch real `~/.cstow/`.

### CLI Tests

Add command-level tests for:

- `cstow artifact list` empty-state message
- `cstow artifact list` tabular output ordering
- `cstow artifact sync` summary counters

### Integration With Existing Flows

Extend existing tests to verify automatic indexing:

- repository install integration test writes a row to the database
- cached repository install still writes/backfills a row
- fetch path tests should cover database upsert on cache hit and on extracted downloads where practical

If full registry-backed `fetch` integration is too heavy, cover the database update logic behind a small helper with unit tests and keep one CLI-level happy-path test for command wiring.

---

## Rollout Notes

- This design intentionally keeps SQLite as an index, not an authority layer.
- `artifact list` is intentionally minimal in v1: no filters, no detail view, no mutation commands.
- Future additions can extend the schema with size, checksum, manifest data, or recipe path without changing the core command shape.
