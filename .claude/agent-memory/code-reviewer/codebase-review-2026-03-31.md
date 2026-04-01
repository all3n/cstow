---
name: codebase-review-2026-03-31
description: Full codebase review findings for cstow project - bugs, warnings, and suggestions across all modules
type: project
---

Full codebase review completed 2026-03-31. All code compiles, vet passes, tests pass. Found issues below.

## Critical Issues
1. pack.go CreateTarZst: deferred file.Close() inside Walk closure causes file descriptor accumulation
2. pack.go ExtractTarZst: path traversal vulnerability via tar filenames containing ".." or absolute paths
3. resolver.go resolveRecursive: "local" source with semver constraint like "^10.0.0" is used as exact version

## Warnings
1. abi.go: clang on Linux defaults to libstdc (correct) but clang without explicit stdlib flag could use either
2. resolver.go pickBest: returns Original() string but resolver tests use hardcoded version strings
3. workspace.go Load: walks up to filesystem root; could be slow if started from deep path
4. cmd/workspace.go: uses os.Chdir which mutates global process state
5. cmd/build.go: guessJobs() hardcoded to 4, ignores runtime.NumCPU()
6. cmd/fetch.go: overwrites existing symlinks silently, no error on Remove
7. toolchain/detect.go: CC derivation via strings.Replace("++","") is fragile
8. s3client.go: parseBucketURL doesn't validate empty bucket
9. resolver.go SaveLock: writes header then encodes duplicate version field

## Modules With No Tests
- cmd/, config/, project/, registry/, hooks/
