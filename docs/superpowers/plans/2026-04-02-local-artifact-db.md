# Local Artifact Database Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a local SQLite-backed artifact index so `cstow artifact list` can show cached artifacts quickly and `cstow artifact sync` can rebuild the index from the standard cache layout while `fetch` and repository `install` update the index automatically.

**Architecture:** Keep the filesystem cache as the source of truth and add a new `internal/artifactdb` package as a queryable index layer. Wire successful `fetch` and `installFromRepository` outcomes into that store, then expose read/repair flows through a new `artifact` Cobra command group.

**Tech Stack:** Go, Cobra CLI, `database/sql`, `modernc.org/sqlite`, filesystem cache under `~/.cstow/cache`, tempdir-backed tests with `stretchr/testify`

---

## File Structure

- Create: `internal/artifactdb/store.go`
  Database open/close, schema init, record upsert, record list, default DB path handling.
- Create: `internal/artifactdb/sync.go`
  Cache scan logic, typed-vs-legacy layout detection, stale-row deletion, sync stats.
- Create: `internal/artifactdb/store_test.go`
  Unit tests for schema init, upsert/list behavior, origin preservation.
- Create: `internal/artifactdb/sync_test.go`
  Unit tests for typed/legacy scanning, empty-dir ignore, stale-row deletion.
- Create: `cmd/artifact_index.go`
  Shared command-layer helper that converts successful artifact operations into `artifactdb.Record` values and writes them to the DB.
- Create: `cmd/artifact_index_test.go`
  Tests for command-layer indexing helper, especially legacy cache-path normalization.
- Create: `cmd/artifact.go`
  New `cstow artifact list` and `cstow artifact sync` commands.
- Create: `cmd/artifact_test.go`
  Command tests for empty list output, sorted list output, and sync summary output.
- Modify: `cmd/fetch.go`
  Index cache hits and successful registry downloads.
- Modify: `cmd/deps.go`
  Index cached and freshly built repository installs inside `installFromRepository(...)`.
- Modify: `cmd/install_integration_test.go`
  Verify repository install populates the DB and cached installs backfill missing rows.
- Modify: `go.mod`
  Add the SQLite driver dependency.
- Modify: `go.sum`
  Record the new dependency checksums.
- Modify: `PLAN.md`
  Note that local artifact metadata now has a SQLite index and artifact CLI commands.
- Modify: `AGENTS.md`
  Update current-reality guidance so future agents know the local artifact index exists and that the cache remains authoritative.

### Task 1: Add the `internal/artifactdb` package with schema, upsert, list, and sync behavior

**Files:**
- Create: `internal/artifactdb/store.go`
- Create: `internal/artifactdb/sync.go`
- Create: `internal/artifactdb/store_test.go`
- Create: `internal/artifactdb/sync_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Write failing unit tests for the new package**

Add `internal/artifactdb/store_test.go` with tests that pin the required database behavior:

```go
package artifactdb

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreUpsertAndList(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "gcc13-cxx17-linux-x86_64",
		BuildType:  "shared",
		InstallDir: "/tmp/cache/fmt/10.2.1/gcc13-cxx17-linux-x86_64/shared",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))

	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "fmt", rows[0].Name)
	assert.Equal(t, "shared", rows[0].BuildType)
	assert.Equal(t, "registry", rows[0].Origin)
}

func TestStoreUpsertPreservesKnownOriginWhenUnknownSeenLater(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cstow.db")
	store, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	firstSeen := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi",
		BuildType:  "shared",
		InstallDir: "/tmp/cache/fmt/10.2.1/abi/shared",
		Origin:     "registry",
		CreatedAt:  firstSeen,
		UpdatedAt:  firstSeen,
		LastSeenAt: firstSeen,
	}))

	secondSeen := firstSeen.Add(time.Hour)
	require.NoError(t, store.Upsert(Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi",
		BuildType:  "shared",
		InstallDir: "/tmp/cache/fmt/10.2.1/abi/shared",
		Origin:     "unknown",
		CreatedAt:  secondSeen,
		UpdatedAt:  secondSeen,
		LastSeenAt: secondSeen,
	}))

	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "registry", rows[0].Origin)
	assert.Equal(t, secondSeen.UTC(), rows[0].LastSeenAt.UTC())
}
```

Add `internal/artifactdb/sync_test.go` with cache-layout coverage:

```go
package artifactdb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncFromCacheIndexesTypedAndLegacyArtifacts(t *testing.T) {
	cacheRoot := t.TempDir()
	typed := filepath.Join(cacheRoot, "fmt", "10.2.1", "abi-1", "shared")
	legacy := filepath.Join(cacheRoot, "spdlog", "1.13.0", "abi-2")
	require.NoError(t, os.MkdirAll(typed, 0o755))
	require.NoError(t, os.MkdirAll(legacy, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(typed, "marker.txt"), []byte("typed"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(legacy, "marker.txt"), []byte("legacy"), 0o644))

	store, err := Open(filepath.Join(t.TempDir(), "cstow.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	stats, err := store.SyncFromCache(cacheRoot)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.Inserted)
	assert.Equal(t, 0, stats.Updated)
	assert.Equal(t, 0, stats.Deleted)

	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "shared", rows[0].BuildType)
	assert.Equal(t, "", rows[1].BuildType)
}

func TestSyncFromCacheDeletesStaleRowsAndIgnoresEmptyDirectories(t *testing.T) {
	cacheRoot := t.TempDir()
	empty := filepath.Join(cacheRoot, "fmt", "10.2.1", "abi-1", "shared")
	require.NoError(t, os.MkdirAll(empty, 0o755))

	store, err := Open(filepath.Join(t.TempDir(), "cstow.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.Upsert(Record{
		Name:       "obsolete",
		Version:    "1.0.0",
		ABITag:     "abi-old",
		BuildType:  "static",
		InstallDir: filepath.Join(cacheRoot, "obsolete", "1.0.0", "abi-old", "static"),
		Origin:     "unknown",
	}))

	stats, err := store.SyncFromCache(cacheRoot)
	require.NoError(t, err)
	assert.Equal(t, 0, stats.Inserted)
	assert.Equal(t, 0, stats.Updated)
	assert.Equal(t, 1, stats.Deleted)

	rows, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, rows)
}
```

- [ ] **Step 2: Run the new package tests to verify they fail for the expected reason**

Run:

```bash
go test ./internal/artifactdb -count=1
```

Expected: FAIL with compile errors such as `undefined: Open`, `undefined: Store`, or package build failures because the new package is not implemented yet.

- [ ] **Step 3: Add the SQLite dependency and implement the store/sync package**

First add the dependency:

```bash
go get modernc.org/sqlite
go mod tidy
```

Implement `internal/artifactdb/store.go`:

```go
package artifactdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

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

type Store struct {
	db *sql.DB
}

type SyncStats struct {
	Inserted int
	Updated  int
	Deleted  int
}

func OpenDefault() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".cstow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	return Open(filepath.Join(dir, "cstow.db"))
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &Store{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initSchema() error {
	const schema = `
PRAGMA user_version = 1;
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
CREATE INDEX IF NOT EXISTS idx_artifacts_name ON artifacts (name);
CREATE INDEX IF NOT EXISTS idx_artifacts_updated_at ON artifacts (updated_at DESC);
`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}
```

Add explicit list/upsert behavior in the same package:

```go
func (s *Store) List() ([]Record, error) {
	rows, err := s.db.Query(`
SELECT name, version, abi_tag, build_type, install_dir, origin, created_at, updated_at, last_seen_at
FROM artifacts
ORDER BY name, version, abi_tag, build_type`)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var rec Record
		var createdAt, updatedAt, lastSeenAt string
		if err := rows.Scan(&rec.Name, &rec.Version, &rec.ABITag, &rec.BuildType, &rec.InstallDir, &rec.Origin, &createdAt, &updatedAt, &lastSeenAt); err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}
		rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		rec.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		rec.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeenAt)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) Upsert(rec Record) error {
	now := time.Now().UTC()
	if rec.Origin == "" {
		rec.Origin = "unknown"
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	if rec.LastSeenAt.IsZero() {
		rec.LastSeenAt = now
	}

	var existingOrigin, existingInstallDir string
	err := s.db.QueryRow(`
SELECT origin, install_dir
FROM artifacts
WHERE name = ? AND version = ? AND abi_tag = ? AND build_type = ?`,
		rec.Name, rec.Version, rec.ABITag, rec.BuildType,
	).Scan(&existingOrigin, &existingInstallDir)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("query existing artifact: %w", err)
	}

	if err == nil {
		if rec.Origin == "unknown" && existingOrigin != "" && existingOrigin != "unknown" {
			rec.Origin = existingOrigin
		}
		if existingInstallDir == rec.InstallDir && existingOrigin == rec.Origin {
			rec.UpdatedAt = now
		}
	}

	_, err = s.db.Exec(`
INSERT INTO artifacts (name, version, abi_tag, build_type, install_dir, origin, created_at, updated_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name, version, abi_tag, build_type) DO UPDATE SET
    install_dir = excluded.install_dir,
    origin = CASE
        WHEN excluded.origin = 'unknown' AND artifacts.origin <> 'unknown' THEN artifacts.origin
        ELSE excluded.origin
    END,
    updated_at = CASE
        WHEN artifacts.install_dir = excluded.install_dir
         AND (artifacts.origin = excluded.origin OR excluded.origin = 'unknown')
        THEN artifacts.updated_at
        ELSE excluded.updated_at
    END,
    last_seen_at = excluded.last_seen_at`,
		rec.Name, rec.Version, rec.ABITag, rec.BuildType, rec.InstallDir, rec.Origin,
		rec.CreatedAt.UTC().Format(time.RFC3339),
		rec.UpdatedAt.UTC().Format(time.RFC3339),
		rec.LastSeenAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert artifact: %w", err)
	}
	return nil
}
```

Implement `internal/artifactdb/sync.go` with explicit cache scanning:

```go
package artifactdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var validBuildTypes = map[string]struct{}{
	"static":      {},
	"shared":      {},
	"header-only": {},
}

func (s *Store) SyncFromCache(cacheRoot string) (SyncStats, error) {
	now := time.Now().UTC()
	records, err := scanCache(cacheRoot, now)
	if err != nil {
		return SyncStats{}, err
	}

	var stats SyncStats
	seen := make(map[string]struct{}, len(records))
	for _, rec := range records {
		key := strings.Join([]string{rec.Name, rec.Version, rec.ABITag, rec.BuildType}, "\x00")
		seen[key] = struct{}{}
		existed, changed, err := s.upsertAndReport(rec)
		if err != nil {
			return SyncStats{}, err
		}
		switch {
		case !existed:
			stats.Inserted++
		case changed:
			stats.Updated++
		}
	}

	rows, err := s.List()
	if err != nil {
		return SyncStats{}, err
	}
	for _, rec := range rows {
		key := strings.Join([]string{rec.Name, rec.Version, rec.ABITag, rec.BuildType}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		if _, err := s.db.Exec(`
DELETE FROM artifacts
WHERE name = ? AND version = ? AND abi_tag = ? AND build_type = ?`,
			rec.Name, rec.Version, rec.ABITag, rec.BuildType,
		); err != nil {
			return SyncStats{}, fmt.Errorf("delete stale artifact: %w", err)
		}
		stats.Deleted++
	}

	return stats, nil
}

func scanCache(cacheRoot string, now time.Time) ([]Record, error) {
	if _, err := os.Stat(cacheRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat cache root: %w", err)
	}

	var records []Record
	err := filepath.WalkDir(cacheRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == cacheRoot {
			return nil
		}

		rel, err := filepath.Rel(cacheRoot, path)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		switch len(parts) {
		case 3:
			if hasFiles(path) && !hasTypedChildren(path) {
				records = append(records, Record{
					Name:       parts[0],
					Version:    parts[1],
					ABITag:     parts[2],
					BuildType:  "",
					InstallDir: path,
					Origin:     "unknown",
					CreatedAt:  now,
					UpdatedAt:  now,
					LastSeenAt: now,
				})
				return filepath.SkipDir
			}
		case 4:
			if _, ok := validBuildTypes[parts[3]]; ok && hasFiles(path) {
				records = append(records, Record{
					Name:       parts[0],
					Version:    parts[1],
					ABITag:     parts[2],
					BuildType:  parts[3],
					InstallDir: path,
					Origin:     "unknown",
					CreatedAt:  now,
					UpdatedAt:  now,
					LastSeenAt: now,
				})
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan cache: %w", err)
	}
	return records, nil
}
```

Keep the helper functions small and in the same file:

```go
func hasFiles(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func hasTypedChildren(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, ok := validBuildTypes[entry.Name()]; ok {
			return true
		}
	}
	return false
}

func (s *Store) upsertAndReport(rec Record) (bool, bool, error) {
	var oldInstallDir, oldOrigin string
	err := s.db.QueryRow(`
SELECT install_dir, origin
FROM artifacts
WHERE name = ? AND version = ? AND abi_tag = ? AND build_type = ?`,
		rec.Name, rec.Version, rec.ABITag, rec.BuildType,
	).Scan(&oldInstallDir, &oldOrigin)
	if err != nil && err != sql.ErrNoRows {
		return false, false, fmt.Errorf("query artifact before sync upsert: %w", err)
	}
	existed := err == nil
	changed := !existed || oldInstallDir != rec.InstallDir || (rec.Origin != "unknown" && oldOrigin != rec.Origin)
	if err := s.Upsert(rec); err != nil {
		return false, false, err
	}
	return existed, changed, nil
}
```

- [ ] **Step 4: Run the package tests again and keep them green**

Run:

```bash
go test ./internal/artifactdb -count=1
```

Expected: PASS

- [ ] **Step 5: Commit the new package and dependency wiring**

Run:

```bash
git add go.mod go.sum internal/artifactdb/store.go internal/artifactdb/sync.go internal/artifactdb/store_test.go internal/artifactdb/sync_test.go
git commit -m "feat: add local artifact database store"
```

### Task 2: Wire successful fetch/install flows into the artifact index

**Files:**
- Create: `cmd/artifact_index.go`
- Create: `cmd/artifact_index_test.go`
- Modify: `cmd/fetch.go`
- Modify: `cmd/deps.go`
- Modify: `cmd/install_integration_test.go`

- [ ] **Step 1: Write failing tests for command-layer indexing and repository-install backfill**

Create `cmd/artifact_index_test.go`:

```go
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexSuccessfulArtifactUsesRealCacheLayout(t *testing.T) {
	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".cstow", "cache")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	t.Setenv("HOME", home)

	cache := &resolver.FSCache{Root: cacheRoot}
	legacyPath := cache.LegacyPath("fmt", "10.2.1", "abi-1")
	require.NoError(t, os.MkdirAll(legacyPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(legacyPath, "marker.txt"), []byte("legacy"), 0o644))

	require.NoError(t, indexSuccessfulArtifact(cache, indexedArtifact{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi-1",
		BuildType:  "shared",
		InstallDir: legacyPath,
		Origin:     "unknown",
	}))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "", rows[0].BuildType)
	assert.Equal(t, legacyPath, rows[0].InstallDir)
}
```

Extend `cmd/install_integration_test.go` with a repository install index check:

```go
func TestInstallFromRepositoryIndexesArtifactsAndBackfillsCachedRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("shared/static install verification is only covered on Unix-like hosts")
	}
	requireTool(t, "git")
	requireTool(t, "cmake")
	requireTool(t, "g++")

	fakeHome := t.TempDir()
	cacheDir := filepath.Join(fakeHome, ".cstow", "cache")
	repoRoot := filepath.Join(fakeHome, "repository")
	sourceRepo := createTaggedLibraryRepo(t)

	t.Setenv("HOME", fakeHome)
	t.Setenv("CSTOW_CACHE_DIR", cacheDir)
	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".cstow"), 0o755))
	writeRepositoryPackage(t, repoRoot, "mini-indexed", sourceRepo, packageOptions{buildType: "static"})
	require.NoError(t, os.WriteFile(
		filepath.Join(fakeHome, ".cstow", "config.toml"),
		[]byte(fmt.Sprintf(`
[[repositories]]
name = "local"
path = %q
priority = 10

[defaults]
std = "c++17"
profile = "debug"
`, repoRoot)),
		0o644,
	))

	ctx, err := newRepositoryInstallContext(nil, "debug", "gcc")
	require.NoError(t, err)

	result, err := installFromRepository("mini-indexed", "^1", repositoryInstallOptions{
		Context: ctx,
		Force:   true,
	})
	require.NoError(t, err)

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, result.InstallDir, rows[0].InstallDir)
	assert.Equal(t, "repository", rows[0].Origin)

	require.NoError(t, store.Close())
	require.NoError(t, os.Remove(filepath.Join(fakeHome, ".cstow", "cstow.db")))

	_, err = installFromRepository("mini-indexed", "^1", repositoryInstallOptions{
		Context: ctx,
		Force:   false,
	})
	require.NoError(t, err)

	store, err = artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	rows, err = store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "repository", rows[0].Origin)
}
```

- [ ] **Step 2: Run the indexing-focused tests to verify the red phase**

Run:

```bash
go test ./cmd -run 'TestIndexSuccessfulArtifactUsesRealCacheLayout|TestInstallFromRepositoryIndexesArtifactsAndBackfillsCachedRows' -count=1
```

Expected: FAIL with compile errors like `undefined: indexSuccessfulArtifact` or assertion failures because the DB is not being populated yet.

- [ ] **Step 3: Implement a shared indexing helper and call it from fetch/install paths**

Create `cmd/artifact_index.go`:

```go
package cmd

import (
	"fmt"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/resolver"
)

type indexedArtifact struct {
	Name       string
	Version    string
	ABITag     string
	BuildType  string
	InstallDir string
	Origin     string
}

func indexSuccessfulArtifact(cache resolver.LocalCache, item indexedArtifact) error {
	store, err := artifactdb.OpenDefault()
	if err != nil {
		return fmt.Errorf("open artifact db: %w", err)
	}
	defer store.Close()

	buildType := item.BuildType
	if legacyPath := cache.LegacyPath(item.Name, item.Version, item.ABITag); item.InstallDir == legacyPath {
		buildType = ""
	}

	return store.Upsert(artifactdb.Record{
		Name:       item.Name,
		Version:    item.Version,
		ABITag:     item.ABITag,
		BuildType:  buildType,
		InstallDir: item.InstallDir,
		Origin:     item.Origin,
	})
}
```

Modify `cmd/deps.go` so `installFromRepository(...)` indexes both cache hits and successful builds:

```go
if !opts.Force {
	if resolvedPath, _, ok := findCachedPackage(cache, name, pkg.Version, []string{abiTag}, buildType); ok {
		if err := indexSuccessfulArtifact(cache, indexedArtifact{
			Name:       name,
			Version:    pkg.Version,
			ABITag:     abiTag,
			BuildType:  buildType,
			InstallDir: resolvedPath,
			Origin:     "repository",
		}); err != nil {
			return nil, err
		}
		return &repositoryInstallResult{
			InstallDir: resolvedPath,
			Version:    pkg.Version,
			ABITag:     abiTag,
			BuildType:  buildType,
			RepoPath:   pkg.RepoPath,
			Cached:     true,
		}, nil
	}
}
```

Add the same helper after a successful build result:

```go
if err := indexSuccessfulArtifact(cache, indexedArtifact{
	Name:       name,
	Version:    pkg.Version,
	ABITag:     abiTag,
	BuildType:  buildType,
	InstallDir: result.InstallDir,
	Origin:     "repository",
}); err != nil {
	return nil, err
}
```

Modify `cmd/fetch.go` to index cache hits and extracted registry downloads:

```go
if path, resolvedABITag, ok := findCachedPackage(cache, pkg.Name, pkg.Version, abiTags, buildType); ok {
	if err := indexSuccessfulArtifact(cache, indexedArtifact{
		Name:       pkg.Name,
		Version:    pkg.Version,
		ABITag:     resolvedABITag,
		BuildType:  buildType,
		InstallDir: path,
		Origin:     "unknown",
	}); err != nil {
		return err
	}
	depPaths[pkg.Name] = dependencyLinkTarget(*pkg, path)
	// existing lock update logic stays here
}
```

After a successful registry extract:

```go
if err := indexSuccessfulArtifact(cache, indexedArtifact{
	Name:       pkg.Name,
	Version:    pkg.Version,
	ABITag:     fetchedABITag,
	BuildType:  fetchedBuildType,
	InstallDir: destDir,
	Origin:     "registry",
}); err != nil {
	return err
}
```

Do not add another explicit call in the `fetch` source-fallback branch, because `installFromRepository(...)` now owns repository-flow indexing.

- [ ] **Step 4: Re-run the indexing tests and keep existing repository-install coverage green**

Run:

```bash
go test ./cmd -run 'TestIndexSuccessfulArtifactUsesRealCacheLayout|TestInstallFromRepositoryBuildsStaticAndSharedLibraries|TestInstallFromRepositoryIndexesArtifactsAndBackfillsCachedRows' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit the indexing integration**

Run:

```bash
git add cmd/artifact_index.go cmd/artifact_index_test.go cmd/fetch.go cmd/deps.go cmd/install_integration_test.go
git commit -m "feat: index successful local artifacts"
```

### Task 3: Add the `cstow artifact list` and `cstow artifact sync` commands

**Files:**
- Create: `cmd/artifact.go`
- Create: `cmd/artifact_test.go`

- [ ] **Step 1: Write failing command tests for list and sync**

Create `cmd/artifact_test.go` with command-output coverage:

```go
package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func executeRootForTest(t *testing.T, args ...string) string {
	t.Helper()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs(args)
	require.NoError(t, rootCmd.Execute())
	return buf.String()
}

func TestArtifactListEmptyState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	output := executeRootForTest(t, "artifact", "list")
	assert.Contains(t, output, "No indexed artifacts found.")
	assert.Contains(t, output, "cstow artifact sync")
}

func TestArtifactListDisplaysSortedRows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "zlib",
		Version:    "1.3.1",
		ABITag:     "abi-z",
		BuildType:  "static",
		InstallDir: "/tmp/cache/zlib/1.3.1/abi-z/static",
		Origin:     "registry",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))
	require.NoError(t, store.Upsert(artifactdb.Record{
		Name:       "fmt",
		Version:    "10.2.1",
		ABITag:     "abi-f",
		BuildType:  "",
		InstallDir: "/tmp/cache/fmt/10.2.1/abi-f",
		Origin:     "unknown",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}))

	output := executeRootForTest(t, "artifact", "list")
	assert.Contains(t, output, "NAME")
	assert.True(t, bytes.Index([]byte(output), []byte("fmt")) < bytes.Index([]byte(output), []byte("zlib")))
	assert.Contains(t, output, "default")
}

func TestArtifactSyncScansCacheAndReportsCounters(t *testing.T) {
	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".cstow", "cache")
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", cacheRoot)
	require.NoError(t, os.MkdirAll(filepath.Join(cacheRoot, "fmt", "10.2.1", "abi-f", "shared"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cacheRoot, "fmt", "10.2.1", "abi-f", "shared", "marker.txt"), []byte("x"), 0o644))

	output := executeRootForTest(t, "artifact", "sync")
	assert.Contains(t, output, "artifact sync complete")
	assert.Contains(t, output, "inserted: 1")
	assert.Contains(t, output, "updated: 0")
	assert.Contains(t, output, "deleted: 0")

	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	rows, err := store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "fmt", rows[0].Name)
}
```

- [ ] **Step 2: Run the command tests to verify the red phase**

Run:

```bash
go test ./cmd -run 'TestArtifactListEmptyState|TestArtifactListDisplaysSortedRows|TestArtifactSyncScansCacheAndReportsCounters' -count=1
```

Expected: FAIL with `unknown command "artifact"` or output/assertion failures because the command group does not exist yet.

- [ ] **Step 3: Implement the new Cobra command group**

Create `cmd/artifact.go`:

```go
package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/spf13/cobra"
)

var artifactCmd = &cobra.Command{
	Use:   "artifact",
	Short: "Inspect and repair the local artifact index",
}

var artifactListCmd = &cobra.Command{
	Use:   "list",
	Short: "List indexed local artifacts",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := artifactdb.OpenDefault()
		if err != nil {
			return err
		}
		defer store.Close()

		rows, err := store.List()
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No indexed artifacts found.")
			fmt.Fprintln(cmd.OutOrStdout(), "Run `cstow artifact sync` to scan the local cache.")
			return nil
		}

		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tVERSION\tABI\tTYPE\tORIGIN\tPATH\tUPDATED")
		for _, row := range rows {
			buildType := row.BuildType
			if buildType == "" {
				buildType = "default"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				row.Name, row.Version, row.ABITag, buildType, row.Origin, row.InstallDir, row.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"))
		}
		return tw.Flush()
	},
}

var artifactSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Rebuild the local artifact index from the cache directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := artifactdb.OpenDefault()
		if err != nil {
			return err
		}
		defer store.Close()

		cache := resolver.NewFSCache()
		stats, err := store.SyncFromCache(cache.Root)
		if err != nil {
			return err
		}

		fmt.Fprintln(cmd.OutOrStdout(), "artifact sync complete")
		fmt.Fprintf(cmd.OutOrStdout(), "inserted: %d\n", stats.Inserted)
		fmt.Fprintf(cmd.OutOrStdout(), "updated: %d\n", stats.Updated)
		fmt.Fprintf(cmd.OutOrStdout(), "deleted: %d\n", stats.Deleted)
		return nil
	},
}

func init() {
	artifactCmd.AddCommand(artifactListCmd)
	artifactCmd.AddCommand(artifactSyncCmd)
	rootCmd.AddCommand(artifactCmd)
}
```

Keep the new command code using `cmd.OutOrStdout()` and `cmd.ErrOrStderr()`-compatible output so tests can capture it without hijacking global stdout.

- [ ] **Step 4: Re-run the command tests and a broader cmd package pass**

Run:

```bash
go test ./cmd -run 'TestArtifact|TestInstallFromRepositoryIndexesArtifactsAndBackfillsCachedRows' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit the new CLI surface**

Run:

```bash
git add cmd/artifact.go cmd/artifact_test.go
git commit -m "feat: add artifact list and sync commands"
```

### Task 4: Update current-reality docs and run full verification

**Files:**
- Modify: `PLAN.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Update the roadmap and agent guidance to reflect the new artifact index**

Add concise, implementation-accurate notes to `PLAN.md` and `AGENTS.md`.

For `PLAN.md`, add a current-state note like:

```md
- local artifact metadata is now indexed in `~/.cstow/cstow.db`
- `cstow artifact list` reads the SQLite index
- `cstow artifact sync` rescans the cache directory to repair stale or missing rows
```

For `AGENTS.md`, extend the "Current Reality" section with:

```md
- local cached artifacts are indexed in `~/.cstow/cstow.db`; the filesystem cache is still the source of truth
- `cstow artifact list` reads the local SQLite index
- `cstow artifact sync` rescans the standard cache layout and reconciles the database
```

- [ ] **Step 2: Run the full repository test suite**

Run:

```bash
go test ./... -count=1
```

Expected: PASS

- [ ] **Step 3: Build the CLI binary to catch command-registration or dependency issues**

Run:

```bash
go build -o cstow .
```

Expected: command exits `0` and writes `./cstow`

- [ ] **Step 4: Commit the documentation and verified feature**

Run:

```bash
git add PLAN.md AGENTS.md
git commit -m "docs: document local artifact index"
```
