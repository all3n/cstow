# Local Artifact Index Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the in-flight local artifact index work so `cstow artifact list` / `cstow artifact sync` are supported by automatic indexing from successful `fetch` and repository `install` flows, with tests and docs aligned to the real implementation.

**Architecture:** Keep the existing split: `internal/artifactdb` remains the SQLite-backed index layer, `cmd/artifact.go` remains the CLI surface, and command code (`cmd/deps.go`, `cmd/fetch.go`) stays responsible for deciding when a successful artifact outcome should be indexed. Add the missing repository cache-hit indexing, add fetch-path tests with a fake registry seam, then update current-reality docs after the code is green.

**Tech Stack:** Go, Cobra, `database/sql`, `modernc.org/sqlite`, tempdir-backed integration tests, `stretchr/testify`, existing CMake/git-based repository install helpers

---

## File Structure

- Modify: `cmd/deps.go`
  Close the repository install indexing gap by recording both fresh builds and cache-hit reuse through the same command-layer path.
- Modify: `cmd/install_integration_test.go`
  Keep the repository install indexing regression pinned with a targeted integration test.
- Create: `cmd/fetch_artifact_test.go`
  Add fetch command coverage for cache-hit indexing, registry-download indexing, and repository-fallback indexing.
- Modify: `cmd/fetch.go`
  Add a small registry-client seam for tests while keeping runtime behavior unchanged.
- Modify: `PLAN.md`
  State that the local artifact index exists and is updated automatically by fetch/install flows.
- Modify: `AGENTS.md`
  Tell future agents that `cstow artifact list/sync` exist and that SQLite is only an index over the cache.

### Task 1: Close Repository Install Indexing And Backfill

**Files:**
- Modify: `cmd/install_integration_test.go`
- Modify: `cmd/deps.go`

- [ ] **Step 1: Extend the existing repository install regression test**

Update `cmd/install_integration_test.go` so the second install explicitly proves cache reuse as well as DB backfill:

```go
	cachedResult, err := installFromRepository("mini-indexed", "^1", repositoryInstallOptions{
		Context: ctx,
		Force:   false,
	})
	require.NoError(t, err)
	assert.True(t, cachedResult.Cached)
	assert.Equal(t, result.InstallDir, cachedResult.InstallDir)

	store, err = artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	rows, err = store.List()
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "repository", rows[0].Origin)
	assert.Equal(t, result.InstallDir, rows[0].InstallDir)
```

This keeps the current first-assert failure and adds one more guard: a cached repository install must repopulate the DB after `cstow.db` is removed.

- [ ] **Step 2: Run the red integration test**

Run:

```bash
go test ./cmd -run TestInstallFromRepositoryIndexesArtifactsAndBackfillsCachedRows -count=1
```

Expected today: FAIL. The current failure is the missing-row assertion:

```text
"[]" should have 1 item(s), but has 0
```

- [ ] **Step 3: Route both fresh builds and cache hits through one repository-indexing path**

In `cmd/deps.go`, replace the early cache-hit return with an indexed return, and use the same indexed-return helper for the fresh-build path:

```go
	recordInstall := func(installDir, resolvedABITag, buildType string, cached bool) (*repositoryInstallResult, error) {
		if err := indexSuccessfulArtifact(cache, indexedArtifact{
			Name:       name,
			Version:    pkg.Version,
			ABITag:     resolvedABITag,
			BuildType:  buildType,
			InstallDir: installDir,
			Origin:     "repository",
		}); err != nil {
			return nil, err
		}

		return &repositoryInstallResult{
			InstallDir: installDir,
			Version:    pkg.Version,
			ABITag:     resolvedABITag,
			BuildType:  buildType,
			RepoPath:   pkg.RepoPath,
			Cached:     cached,
		}, nil
	}

	if !opts.Force {
		if resolvedPath, resolvedABITag, ok := findCachedPackage(cache, name, pkg.Version, []string{abiTag}, buildType); ok {
			return recordInstall(resolvedPath, resolvedABITag, buildType, true)
		}
	}
```

Then replace the fresh-build return at the bottom of `installFromRepository(...)` with:

```go
	return recordInstall(result.InstallDir, abiTag, buildType, false)
```

This removes the “cache hit skips indexing” hole and ensures all successful repository installs use the same indexing path.

- [ ] **Step 4: Re-run the targeted repository install test**

Run:

```bash
go test ./cmd -run TestInstallFromRepositoryIndexesArtifactsAndBackfillsCachedRows -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the repository indexing fix**

```bash
git add cmd/deps.go cmd/install_integration_test.go
git commit -m "fix: index repository installs in artifact db"
```

### Task 2: Add Fetch Artifact Indexing Coverage

**Files:**
- Create: `cmd/fetch_artifact_test.go`
- Modify: `cmd/fetch.go`

- [ ] **Step 1: Write failing fetch-path tests**

Create `cmd/fetch_artifact_test.go` with one helper set and three tests: cache hit, registry download, and repository fallback.

```go
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/all3n/cstow/internal/artifactdb"
	"github.com/all3n/cstow/internal/config"
	"github.com/all3n/cstow/internal/pack"
	"github.com/all3n/cstow/internal/registry"
	"github.com/all3n/cstow/internal/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeArtifactRegistryClient struct {
	manifest    *registry.Manifest
	manifestErr error
	archive     []byte
	downloadErr error
}

func (f *fakeArtifactRegistryClient) GetManifest(context.Context, string, string) (*registry.Manifest, error) {
	if f.manifestErr != nil {
		return nil, f.manifestErr
	}
	return f.manifest, nil
}

func (f *fakeArtifactRegistryClient) Download(context.Context, string, string, string, string) ([]byte, error) {
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	return f.archive, nil
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origWD) })
}

func writeFetchProject(t *testing.T, dir, cfg string, lock *resolver.LockFile) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cstow.toml"), []byte(cfg), 0o644))
	require.NoError(t, resolver.SaveLock(filepath.Join(dir, "cstow.lock"), lock))
}

func readIndexedRows(t *testing.T) []artifactdb.Record {
	t.Helper()
	store, err := artifactdb.OpenDefault()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	rows, err := store.List()
	require.NoError(t, err)
	return rows
}

func TestFetchIndexesCachedArtifact(t *testing.T) {
	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".cstow", "cache")
	workdir := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", cacheRoot)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))

	writeFetchProject(t, workdir, `
[package]
name = "demo"
version = "0.1.0"

[[dependencies]]
name = "fmt"
version = "10.2.1"
source = "registry"
build_type = "shared"
`, &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{{
			Name:      "fmt",
			Version:   "10.2.1",
			Source:    "registry:default",
			ABITag:    "abi-cache",
			BuildType: "shared",
		}},
	})
	withWorkingDir(t, workdir)

	cache := resolver.NewFSCache()
	installDir := cache.Path("fmt", "10.2.1", "abi-cache", "shared")
	require.NoError(t, os.MkdirAll(installDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installDir, "marker.txt"), []byte("cached"), 0o644))

	output := executeRootForTest(t, "fetch")
	assert.Contains(t, output, "[cached] fmt@10.2.1")

	rows := readIndexedRows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "fmt", rows[0].Name)
	assert.Equal(t, "unknown", rows[0].Origin)
	assert.Equal(t, installDir, rows[0].InstallDir)
}

func TestFetchIndexesRegistryDownload(t *testing.T) {
	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".cstow", "cache")
	workdir := t.TempDir()
	payloadDir := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", cacheRoot)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(payloadDir, "include", "fmt"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(payloadDir, "include", "fmt", "format.h"), []byte("fmt"), 0o644))

	archive, err := pack.CreateTarZst(payloadDir)
	require.NoError(t, err)

	oldFactory := newArtifactRegistryClient
	newArtifactRegistryClient = func(context.Context, config.Registry) (artifactRegistryClient, error) {
		return &fakeArtifactRegistryClient{
			manifest: &registry.Manifest{
				Name:    "fmt",
				Version: "10.2.1",
				Artifacts: []registry.Artifact{{
					ABITag:    "abi-registry",
					BuildType: "shared",
				}},
			},
			archive: archive,
		}, nil
	}
	t.Cleanup(func() { newArtifactRegistryClient = oldFactory })

	writeFetchProject(t, workdir, `
[package]
name = "demo"
version = "0.1.0"

[[dependencies]]
name = "fmt"
version = "10.2.1"
source = "registry"
build_type = "shared"

[[registry]]
name = "default"
url = "s3://example/cstow"
provider = "custom"
region = "auto"
`, &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{{
			Name:      "fmt",
			Version:   "10.2.1",
			Source:    "registry:default",
			BuildType: "shared",
		}},
	})
	withWorkingDir(t, workdir)

	output := executeRootForTest(t, "fetch")
	assert.Contains(t, output, "[done]  fmt@10.2.1")

	rows := readIndexedRows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "registry", rows[0].Origin)
	assert.Equal(t, "abi-registry", rows[0].ABITag)
	assert.FileExists(t, filepath.Join(rows[0].InstallDir, "include", "fmt", "format.h"))
}

func TestFetchIndexesRepositoryFallbackArtifact(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("repository fallback integration is only covered on Unix-like hosts")
	}
	requireTool(t, "git")
	requireTool(t, "cmake")
	requireTool(t, "g++")

	home := t.TempDir()
	cacheRoot := filepath.Join(home, ".cstow", "cache")
	repoRoot := filepath.Join(home, "repository")
	workdir := t.TempDir()
	sourceRepo := createTaggedLibraryRepo(t)

	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", cacheRoot)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cstow"), 0o755))
	writeRepositoryPackage(t, repoRoot, "mini-fetch", sourceRepo, packageOptions{buildType: "static"})
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".cstow", "config.toml"),
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

	writeFetchProject(t, workdir, `
[package]
name = "demo"
version = "0.1.0"

[[dependencies]]
name = "mini-fetch"
version = "1.0.0"
source = "registry"
build_type = "static"
`, &resolver.LockFile{
		Version: 1,
		Packages: []resolver.LockEntry{{
			Name:      "mini-fetch",
			Version:   "1.0.0",
			Source:    "registry:default",
			BuildType: "static",
		}},
	})
	withWorkingDir(t, workdir)

	output := executeRootForTest(t, "fetch")
	assert.Contains(t, output, "[built] mini-fetch@1.0.0")

	rows := readIndexedRows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, "repository", rows[0].Origin)
	assert.DirExists(t, rows[0].InstallDir)
}
```

These tests intentionally refer to `newArtifactRegistryClient` / `artifactRegistryClient`, which do not exist yet.

- [ ] **Step 2: Run the new fetch tests and confirm the red state**

Run:

```bash
go test ./cmd -run 'TestFetchIndexes(CachedArtifact|RegistryDownload|RepositoryFallbackArtifact)' -count=1
```

Expected today: FAIL at compile time because the test seam is missing, with errors like:

```text
undefined: newArtifactRegistryClient
undefined: artifactRegistryClient
```

- [ ] **Step 3: Add a tiny registry-client seam without changing runtime behavior**

In `cmd/fetch.go`, add the interface and factory right after the imports:

```go
type artifactRegistryClient interface {
	GetManifest(context.Context, string, string) (*registry.Manifest, error)
	Download(context.Context, string, string, string, string) ([]byte, error)
}

var newArtifactRegistryClient = func(ctx context.Context, reg config.Registry) (artifactRegistryClient, error) {
	return registry.NewS3Client(ctx, reg)
}
```

Then replace the concrete client construction block with the seam:

```go
		var s3client artifactRegistryClient
		if len(cfg.Registries) > 0 {
			s3client, err = newArtifactRegistryClient(context.Background(), cfg.Registries[0])
			if err != nil {
				return fmt.Errorf("create S3 client: %w", err)
			}
		}
```

Leave the rest of the fetch logic alone. The point is testability, not a broader refactor.

- [ ] **Step 4: Run the focused fetch tests**

Run:

```bash
go test ./cmd -run 'TestFetchIndexes(CachedArtifact|RegistryDownload|RepositoryFallbackArtifact)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the fetch indexing coverage**

```bash
git add cmd/fetch.go cmd/fetch_artifact_test.go
git commit -m "test: cover fetch artifact indexing"
```

### Task 3: Sync Current-Reality Docs

**Files:**
- Modify: `PLAN.md`
- Modify: `AGENTS.md`

- [ ] **Step 1: Update `PLAN.md` to describe the implemented artifact index**

In the “已落地能力” / “已实现但未闭环” sections, replace the artifact-index language with the implemented state:

```md
- 本地 artifact 元数据现已索引到 `~/.cstow/cstow.db`
- `cstow artifact list` 读取 SQLite 索引
- `cstow artifact sync` 会扫描标准 cache 布局并修复索引
- `fetch` 与 repository `install` 成功后会自动 upsert 本地 artifact 索引
```

Keep the remaining unresolved items explicit, for example that artifact content validation against recipe `artifacts` / `install_targets` is still missing.

- [ ] **Step 2: Update `AGENTS.md` so future agents see the new current reality**

Add or update the relevant bullets so they say:

```md
- `cmd/fetch.go` downloads prebuilt artifacts into the local cache, uses manifest metadata when available to match ABI/build_type, can fall back to repository source builds, symlinks results into `./cstow_deps`, and indexes successful outcomes in the local artifact DB.
- `cmd/install.go` / `cmd/deps.go` build repository packages from source and index both fresh installs and cached repository hits in the local artifact DB.
- `cmd/artifact.go` exposes `cstow artifact list` and `cstow artifact sync`; the filesystem cache remains authoritative and `~/.cstow/cstow.db` is only a local query index.
```

Do not claim artifact integrity validation or repository `artifacts` checking is complete.

- [ ] **Step 3: Commit the doc sync**

```bash
git add PLAN.md AGENTS.md
git commit -m "docs: sync artifact index current reality"
```

### Task 4: Final Verification

**Files:**
- Verify only

- [ ] **Step 1: Run artifactdb and command-level tests**

Run:

```bash
go test ./internal/artifactdb/... -count=1
go test ./cmd/... -count=1
```

Expected: PASS for both commands.

- [ ] **Step 2: Run the full repository test suite**

Run:

```bash
go test ./... -count=1
```

Expected: PASS. If a non-artifact pre-existing failure appears, stop and document it before touching unrelated code.

- [ ] **Step 3: Record the final state in git**

```bash
git status --short
git log --oneline -n 3
```

Expected:

```text
working tree clean
```

and the last commits should include:

```text
docs: sync artifact index current reality
test: cover fetch artifact indexing
fix: index repository installs in artifact db
```

---

## Self-Review

- Spec coverage:
  - repository install indexing and cache-hit backfill are covered by Task 1
  - fetch cache hit / registry download / repository fallback indexing are covered by Task 2
  - roadmap and agent doc sync are covered by Task 3
  - targeted and full verification are covered by Task 4
- Placeholder scan:
  - no `TODO`, `TBD`, or “similar to above” placeholders remain
  - every code-changing step includes concrete snippets or exact file content
- Type consistency:
  - the test seam uses `artifactRegistryClient` and `newArtifactRegistryClient` consistently across Task 2
  - repository indexing still flows through existing `indexSuccessfulArtifact(...)`
