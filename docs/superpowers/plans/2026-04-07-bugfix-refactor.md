# Bugfix & Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix Git lock version 0.0.0 bug and parallel build log interleaving, then refactor large files (s3client.go, fetch.go).

**Architecture:** Minimal bug fixes first (resolver.go, cmd/workspace.go), then pure code movement refactors that don't change behavior (file splits within same package).

**Tech Stack:** Go 1.25+, standard library + existing test framework (testify)

---

## Task 1: Fix Git lock version 0.0.0 — write test

**Files:**
- Modify: `internal/resolver/resolver_test.go`

- [ ] **Step 1: Add test for git source with empty version but non-empty rev**

Add a new test after `TestResolveGitSourceNoRevDefaultsToMain` (after line 230 in `internal/resolver/resolver_test.go`):

```go
func TestResolveGitSourceEmptyVersionUsesRev(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{
			Name:    "mylib",
			Version: "",
			Source:  "git",
			Git:     "https://github.com/user/mylib.git",
			Rev:     "v2.1.0",
		},
	})
	require.NoError(t, err)
	require.Len(t, lf.Packages, 1)
	assert.Equal(t, "v2.1.0", lf.Packages[0].Version)
	assert.Equal(t, "git:https://github.com/user/mylib.git", lf.Packages[0].Source)
}

func TestResolveGitSourceWildcardVersionUsesRev(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{
			Name:    "mylib",
			Version: "*",
			Source:  "git",
			Git:     "https://github.com/user/mylib.git",
			Rev:     "main",
		},
	})
	require.NoError(t, err)
	require.Len(t, lf.Packages, 1)
	assert.Equal(t, "main", lf.Packages[0].Version)
}

func TestResolveGitSourceNoVersionNoRev(t *testing.T) {
	r := New(nil, nil)
	lf, err := r.Resolve([]config.Dependency{
		{
			Name:   "mylib",
			Source: "git",
			Git:    "https://github.com/user/mylib.git",
		},
	})
	require.NoError(t, err)
	require.Len(t, lf.Packages, 1)
	assert.Equal(t, "0.0.0", lf.Packages[0].Version)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolver/ -run TestResolveGitSourceEmptyVersionUsesRev -v`
Expected: FAIL — version is `"0.0.0"` instead of `"v2.1.0"`

---

## Task 2: Fix Git lock version 0.0.0 — implement

**Files:**
- Modify: `internal/resolver/resolver.go:100-106`

- [ ] **Step 1: Update the git source branch in resolver.go**

Replace lines 100-106 in `internal/resolver/resolver.go`:

```go
		case "git":
			chosenVer = dep.Version
			if chosenVer == "*" || chosenVer == "" {
				if dep.Rev != "" {
					chosenVer = dep.Rev
				} else {
					chosenVer = "0.0.0"
				}
			}
			source = "git:" + dep.Git
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/resolver/ -v`
Expected: ALL PASS, including the 3 new tests

- [ ] **Step 3: Commit**

```bash
git add internal/resolver/resolver.go internal/resolver/resolver_test.go
git commit -m "fix(resolver): use rev as fallback version for git deps instead of 0.0.0"
```

---

## Task 3: Parallel build log buffering — write test

**Files:**
- Create: `cmd/module_logger_test.go`

- [ ] **Step 1: Add moduleLogger unit test**

Create `cmd/module_logger_test.go`:

```go
package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModuleLoggerTail(t *testing.T) {
	var log moduleLogger
	log.module = "testmod"
	log.Write([]byte("line1\nline2\nline3\nline4\nline5\n"))

	lines := log.tailLines(3)
	assert.Equal(t, []string{"line3", "line4", "line5"}, lines)
}

func TestModuleLoggerTailFewerThanMax(t *testing.T) {
	var log moduleLogger
	log.module = "testmod"
	log.Write([]byte("line1\nline2\n"))

	lines := log.tailLines(20)
	assert.Equal(t, []string{"line1", "line2"}, lines)
}

func TestModuleLoggerTailEmpty(t *testing.T) {
	var log moduleLogger
	lines := log.tailLines(20)
	assert.Equal(t, 0, len(lines))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestModuleLogger -v`
Expected: FAIL — `moduleLogger` type and `tailLines` method don't exist yet

---

## Task 4: Parallel build log buffering — implement

**Files:**
- Modify: `cmd/workspace.go`

- [ ] **Step 1: Add moduleLogger type and tailLines method**

Add after the imports in `cmd/workspace.go` (after line 16):

```go
// moduleLogger buffers build output for a single module so that
// parallel builds don't interleave their CMake output.
type moduleLogger struct {
	buf    bytes.Buffer
	module string
}

func (l *moduleLogger) Write(p []byte) (n int, err error) {
	return l.buf.Write(p)
}

// tailLines returns the last n non-empty lines from the buffer.
func (l *moduleLogger) tailLines(n int) []string {
	content := l.buf.String()
	if content == "" {
		return nil
	}
	raw := strings.Split(content, "\n")
	var lines []string
	for _, l := range raw {
		if l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
```

Note: `bytes` and `strings` are already imported in this file.

- [ ] **Step 2: Update the task function in workspaceBuildCmd to use moduleLogger**

In `cmd/workspace.go`, find the `task := func(ctx context.Context, m *workspace.Module) error {` block inside `workspaceBuildCmd.RunE` and replace the entire function body with:

```go
		task := func(ctx context.Context, m *workspace.Module) error {
			mu.Lock()
			count++
			current := count
			mu.Unlock()

			logger := &moduleLogger{module: m.Name}
			var stdout, stderr io.Writer

			if jobs > 1 {
				// Buffer output for parallel jobs
				stdout = logger
				stderr = logger
				fmt.Printf(">> [%d/%d] building %s...\n", current, total, m.Name)
			} else {
				// Direct output for sequential jobs
				stdout = cmd.OutOrStdout()
				stderr = cmd.ErrOrStderr()
				fmt.Printf("\n>> [%d/%d] building %s\n", current, total, m.Name)
			}

			// Pass autoFetch=false to individual builds because we've already fetched everything
			err := runBuild(m.Path, profile, toolchainName, false, stdout, stderr)
			if err != nil {
				if jobs > 1 {
					// Show the last 20 lines of buffered output on failure
					tail := logger.tailLines(20)
					fmt.Fprintf(cmd.ErrOrStderr(), "\n=== %s FAILED ===\n", m.Name)
					for _, line := range tail {
						fmt.Fprintln(cmd.ErrOrStderr(), line)
					}
				}
				return fmt.Errorf("build %s failed: %w", m.Name, err)
			}

			if jobs > 1 {
				fmt.Printf(">> [%d/%d] %s complete\n", current, total, m.Name)
			}
			return nil
		}
```

Key changes from the original:
- Uses `moduleLogger` instead of raw `bytes.Buffer`
- On failure, uses `tailLines(20)` to get the last 20 lines instead of dumping the full buffer
- Writes the tail output to `cmd.ErrOrStderr()` instead of stdout via `fmt.Printf`

- [ ] **Step 3: Verify the old `var outBuf bytes.Buffer` line is replaced**

The old code had `var outBuf bytes.Buffer` and `stdout = &outBuf` / `stderr = &outBuf`. Make sure this variable is completely gone and replaced by the `logger` variable. The old code also had `fmt.Println(outBuf.String())` which should now be replaced by the tail lines logic.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run TestModuleLogger -v`
Expected: ALL PASS

Run: `go test ./... -count=1`
Expected: ALL PASS (no regressions)

- [ ] **Step 5: Commit**

```bash
git add cmd/workspace.go cmd/module_logger_test.go
git commit -m "fix(workspace): buffer parallel build logs and show last 20 lines on failure"
```

---

## Task 5: Refactor — split s3client.go

This is pure code movement within the same package. No behavior changes.

**Files:**
- Create: `internal/registry/s3config.go`
- Modify: `internal/registry/s3client.go`

- [ ] **Step 1: Create s3config.go with moved functions**

Create `internal/registry/s3config.go` with `package registry` and the following imports:

```go
package registry

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/all3n/cstow/internal/config"
)
```

Then copy these functions **exactly as-is** from `internal/registry/s3client.go` into the new file:

1. `registryRuntimeConfig` struct (currently at ~line 120)
2. `resolveRegistryRuntimeConfig` function (currently at ~line 183)
3. `loadAWSProfileS3Endpoint` function (currently at ~line 224)
4. `parseNestedS3Endpoint` function (currently at ~line 251)
5. `matchesAWSProfileSection` function (currently at ~line 305)
6. `parseKeyValue` function (currently at ~line 314)
7. `leadingWhitespace` function (currently at ~line 322)
8. `needsPathStyle` function (currently at ~line 326)
9. `defaultRegionForEndpoint` function (currently at ~line 338)

Copy each function verbatim — no changes to the code.

- [ ] **Step 2: Remove the moved functions from s3client.go**

Delete all the functions listed in Step 1 from `internal/registry/s3client.go`.

Then in `s3client.go`, update `NewS3Client` to remove the now-unused `credentials` import. The `credentials.NewStaticCredentialsProvider` call is now in s3config.go, so remove this import from s3client.go:

```go
"github.com/aws/aws-sdk-go-v2/credentials"
```

- [ ] **Step 3: Run tests to verify no regressions**

Run: `go test ./internal/registry/ -v`
Expected: ALL PASS

Run: `go build ./...`
Expected: success (compiles cleanly)

- [ ] **Step 4: Commit**

```bash
git add internal/registry/s3config.go internal/registry/s3client.go
git commit -m "refactor(registry): extract AWS config parsing into s3config.go"
```

---

## Task 6: Refactor — split fetch.go

This is pure code movement within the same package. No behavior changes.

**Files:**
- Create: `cmd/fetch_utils.go`
- Modify: `cmd/fetch.go`

- [ ] **Step 1: Create fetch_utils.go with moved functions**

Create `cmd/fetch_utils.go` with `package cmd` and the necessary imports derived from the moved functions.

Copy these functions **exactly as-is** from `cmd/fetch.go` into the new file:

1. `fetchByHashID` function (currently at ~line 417-589)
2. `fetchManifestCandidate` type (currently at ~line 591-594)
3. `fetchManifestCandidates` function (currently at ~line 596-628)
4. `prependUniqueCandidate` function (currently at ~line 630-645)
5. `linkFetchedArtifactAt` function (currently at ~line 647-657)
6. `linkFetchedArtifact` function (currently at ~line 659-661)
7. `isArtifactNotFoundError` function (currently at ~line 663-670)
8. `verifyArtifactHash` function (currently at ~line 690-700)

For imports in `fetch_utils.go`, include only the imports used by these moved functions:
- `context`, `crypto/sha256`, `errors`, `fmt`, `os`, `path/filepath`, `strings`
- `"github.com/all3n/cstow/internal/artifactdb"`
- `"github.com/all3n/cstow/internal/config"`
- `"github.com/all3n/cstow/internal/pack"`
- `"github.com/all3n/cstow/internal/registry"`
- `"github.com/all3n/cstow/internal/resolver"`

Copy each function verbatim — no changes to the code.

- [ ] **Step 2: Remove the moved functions from fetch.go**

Delete all the functions listed in Step 1 from `cmd/fetch.go`.

Remove imports no longer used by the remaining code in `cmd/fetch.go`:
- `"crypto/sha256"` — used only by `verifyArtifactHash`
- `"errors"` — used only by `fetchManifestCandidates`
- `"github.com/all3n/cstow/internal/artifactdb"` — check if still used by `runFetch`
- `"github.com/all3n/cstow/internal/pack"` — check if still used by `runFetch`

Run `go build ./cmd/` to verify the import list is correct.

- [ ] **Step 3: Run tests to verify no regressions**

Run: `go test ./cmd/ -v -count=1`
Expected: ALL PASS

Run: `go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/fetch.go cmd/fetch_utils.go
git commit -m "refactor(fetch): extract hash-ID and symlink utilities into fetch_utils.go"
```

---

## Task 7: Update ISSUES.md and verify

**Files:**
- Modify: `ISSUES.md`

- [ ] **Step 1: Mark fixed items in ISSUES.md**

Change the Git lock version issue header from:

```markdown
### [ENHANCEMENT] Git 源码在 lock 文件中的版本记录薄弱
```

to:

```markdown
### [FIXED] Git 源码在 lock 文件中的版本记录薄弱
```

Change the parallel build log issue header from:

```markdown
### [ENHANCEMENT] 并行构建日志交织
```

to:

```markdown
### [FIXED] 并行构建日志交织
```

- [ ] **Step 2: Run full test suite**

Run: `go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 3: Commit**

```bash
git add ISSUES.md
git commit -m "docs: mark fixed issues in ISSUES.md"
```

---

## Task 8: Update CLAUDE.md code map if needed

**Files:**
- Modify: `CLAUDE.md` (only if file structure changed significantly)

- [ ] **Step 1: Check if CLAUDE.md code map needs updates**

The refactors only split files within existing packages (no new packages, no behavioral changes). The code map in CLAUDE.md describes packages, not individual files. Check if `s3client.go` or `fetch.go` are mentioned by filename.

Run: `grep -n 's3client\|fetch\.go' CLAUDE.md`

If no matches or only package-level references, no changes needed. Skip to end.

- [ ] **Step 2: Commit if changes were made**

Only if CLAUDE.md was actually modified.
