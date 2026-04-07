# Bug Fix & Code Refactor Design

Date: 2026-04-07

## Scope

Fix known bugs from ISSUES.md, then perform targeted code quality improvements on large files.

## Bug Fix 1: Git Source Lock Version 0.0.0

### Problem

Git dependencies are recorded as `version = "0.0.0"` in `cstow.lock` when no explicit `@version` is given, even though `--tag` may specify a meaningful ref. This makes cache paths indistinguishable and debugging harder.

### Root Cause

`internal/resolver/resolver.go` line 103-104: when `dep.Version` is `"*"` or `""`, the resolver falls back to `"0.0.0"` unconditionally, ignoring the `dep.Rev` field which may contain a tag/branch name.

### Fix

In `resolver.go`, `case "git":` branch:

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
```

When version is not explicitly set, prefer `dep.Rev` (tag/branch) as the logical version. Only fall back to `"0.0.0"` when both version and rev are empty.

### Impact

- Cache paths change from `~/.cstow/cache/fmt/0.0.0/...` to `~/.cstow/cache/fmt/v1.14.0/...`
- Existing caches with `0.0.0` paths will be re-fetched once (harmless, natural cache miss)
- Lock files will show meaningful versions for git deps

### Testing

- Update `resolver_test.go` to verify git deps with empty version but non-empty rev use the rev
- Verify existing git deps with explicit version still work

## Bug Fix 2: Parallel Build Log Interleaving

### Problem

During `cstow workspace build --jobs N`, CMake output from multiple modules mixes together in the terminal, making it hard to identify which module failed.

### Fix

In `internal/workspace/workspace.go` parallel build worker:

1. Each worker captures its CMake output into a `bytes.Buffer` via a custom `io.Writer`
2. On build success: discard buffer (or print to `--verbose` output)
3. On build failure: print the module name, then the last 20 lines from its buffer, and write the full log to a temp file with path printed for deeper investigation

### Pseudocode

```go
type moduleLogger struct {
    buf    bytes.Buffer
    module string
}

func (l *moduleLogger) Write(p []byte) (n int, err error) {
    return l.buf.Write(p)
}

func (l *moduleLogger) ReportFailure() {
    lines := strings.Split(l.buf.String(), "\n")
    tail := lines
    if len(tail) > 20 {
        tail = tail[len(tail)-20:]
    }
    fmt.Fprintf(os.Stderr, "\n=== %s FAILED ===\n%s\n", l.module, strings.Join(tail, "\n"))

    tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("cstow-%s-build.log", l.module))
    os.WriteFile(tmpFile, l.buf.Bytes(), 0644)
    fmt.Fprintf(os.Stderr, "Full log: %s\n", tmpFile)
}
```

### Testing

- Unit test for `moduleLogger` tail truncation logic
- Manual verification with a multi-module workspace

## Refactor 1: Split s3client.go

### Current State

`internal/registry/s3client.go` — 538 lines, mixing S3 operations with AWS config parsing.

### Target Structure

```
internal/registry/
├── s3client.go       (~340 lines) S3Client struct, Upload, Download, Manifest, ListVersions, key helpers
└── s3config.go       (~200 lines) resolveRegistryRuntimeConfig, loadAWSProfileS3Endpoint, parseNestedS3Endpoint, helpers
```

### Moved Functions

- `resolveRegistryRuntimeConfig`
- `loadAWSProfileS3Endpoint`
- `parseNestedS3Endpoint`
- `matchesAWSProfileSection`
- `parseKeyValue`
- `leadingWhitespace`
- `needsPathStyle`
- `defaultRegionForEndpoint`

## Refactor 2: Split fetch.go

### Current State

`cmd/fetch.go` — 700 lines, mixing the main fetch loop with hash-ID resolution and utility helpers.

### Target Structure

```
cmd/
├── fetch.go          (~400 lines) fetchCmd, runFetch, init(), fetchOptions
└── fetch_utils.go    (~300 lines) fetchByHashID, manifest candidates, symlink helpers, verifyArtifactHash
```

### Moved Functions

- `fetchByHashID`
- `fetchManifestCandidate` (type) + `fetchManifestCandidates`
- `prependUniqueCandidate`
- `linkFetchedArtifactAt` + `linkFetchedArtifact`
- `isArtifactNotFoundError`
- `verifyArtifactHash`

## Execution Order

1. Bug fix 1: Git lock version — modify `resolver.go`, update tests
2. Bug fix 2: Parallel log buffering — modify `workspace.go`, add tests
3. Refactor 1: Split `s3client.go`
4. Refactor 2: Split `fetch.go`
5. Run `go test ./...` to verify no regressions
6. Update ISSUES.md (mark fixed items)
7. Update CLAUDE.md if code map changes
