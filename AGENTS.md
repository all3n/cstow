# AGENTS.md / CLAUDE.md

This repository keeps `CLAUDE.md` as a symlink to `AGENTS.md`. Update this file when agent-facing guidance changes.

## Project Snapshot

`cstow` is a Go CLI for C++ package and build workflows. The codebase is no longer just a minimal Phase 1-4 prototype: it now contains project scaffolding, project builds, lock-file resolution, ABI/toolchain detection, S3 artifact publishing and fetching, repository-based source build recipes, workspace support, migration helpers, and CI generation.

The important distinction is:

- **Registry flow**: prebuilt artifacts are published to and fetched from S3-compatible storage.
- **Repository flow**: package build recipes live in repository directories and are used by `cstow install` to build packages from source.

Those two flows coexist, but they are not fully unified yet.

## Current Reality

When changing behavior, follow the code as it exists today instead of the original high-level design docs.

- `cmd/build.go` builds the current project with CMake and consumes already-fetched dependency prefixes from `./cstow_deps`.
- `cmd/install.go` is the only command that currently uses `internal/repository/` and `internal/builder/` to build third-party packages from source.
- `cmd/add.go` updates `cstow.toml` and `cstow.lock` through `internal/resolver`; it can record dependency `build_type`, but it still does **not** validate dependencies against repository definitions yet.
- `cmd/fetch.go` downloads prebuilt artifacts into the local cache, uses manifest metadata when available to match ABI/build_type, can fall back to repository source builds, and symlinks results into `./cstow_deps`.
- `cmd/publish.go` supports both project-directory publish and direct local-artifact publish by `(name, version, abi_tag, build_type)`; successful publish/fetch paths can populate artifact `hash_id` metadata (and `build_tags`) in local SQLite index and registry manifests.
- `internal/repository/source.go` supports `git` sources; `archive` sources are still a stub.
- `internal/builder/` currently supports CMake installs for `static` / `shared` libraries and `header-only` packages. It does not yet implement `make`, `autoconf`, `meson`, or generic custom builders.
- `cmd/install` has integration coverage for local `static` / `shared` CMake installs in `cmd/install_integration_test.go`.
- dependency `build_type` now flows through `cstow.toml`, `cstow.lock`, cache paths, and registry artifact keys; cache reads remain backward-compatible with legacy `<abi>` directories.
- local cached artifacts are indexed in `~/.cstow/cstow.db` (SQLite); the filesystem cache is still the source of truth. `cstow artifact list` reads the index, `cstow artifact sync` rescans the cache, and `cstow artifact show <hashid>` looks up by hash prefix.
- `repo.md` and `docs/superpowers/specs/` describe the intended repository system. They are useful, but some parts are still ahead of the current implementation.

Do not mark a feature as "done" unless it is wired through the CLI and covered by tests.

## Build And Test

```bash
go build -o cstow .
go test ./...
go test ./internal/repository/...
go test ./internal/config/...
go test ./internal/workspace/...
go test -run TestIntegration ./internal/repository/
```

Use targeted package tests while iterating, then finish with `go test ./...` when practical.

## Code Map

- `cmd/`
  - CLI surface: `init`, `build`, `add`, `fetch`, `publish`, `install`, `migrate`, `ci`, `workspace`, `checkabi`, `artifact list`, `artifact sync`, `artifact show`
- `internal/config/`
  - Project config (`cstow.toml`) and user config (`~/.cstow/config.toml`)
- `internal/project/`
  - `cstow init` scaffolding
- `internal/toolchain/`
  - compiler detection and CMake toolchain flag generation
- `internal/abi/`
  - ABI tag parsing, formatting, compatibility, and detection
- `internal/resolver/`
  - dependency declaration mutation and lock-file generation
- `internal/registry/`
  - S3-compatible publish/download/manifest operations
- `internal/artifactdb/`
  - local SQLite artifact index (store, upsert, list, sync, hash_id lookup)
- `internal/repository/`
  - repository package definitions, version lookup, layered build config merge, source fetch
- `internal/builder/`
  - source build/install execution for repository packages
- `internal/workspace/`
  - workspace root discovery and member expansion
- `internal/hooks/`
  - shell hook runner for lifecycle scripts
- `internal/legacy/`
  - CMake migration scanner and `cstow.toml` generation
- `internal/pack/`
  - `.tar.zst` creation/extraction

## Working Rules For This Repo

- Keep project-build flow and source-build flow conceptually separate unless you are explicitly unifying them.
- Before changing dependency behavior, inspect the interaction among `cmd/add.go`, `cmd/fetch.go`, `cmd/install.go`, `cmd/build.go`, `internal/resolver/`, and `internal/repository/`.
- If you change TOML schema or repository semantics, update tests in the same package.
- Prefer extending existing tests over only changing docs.
- Treat `PLAN.md` as the execution roadmap and keep it aligned with the actual implementation state.
- If a design doc conflicts with the code, either update the doc or clearly note that the code is still incomplete.

## High-Value Gaps

These are the areas most likely to need follow-up work:

- `cmd/build.go` does not yet consume repository recipes directly; it mostly relies on `cstow_deps` and raw CMake invocation.
- merged `CXXFlags` / `LinkFlags` already enter the current `install` CMake path, but install result validation via `artifacts` / `install_targets` is still missing.
- repository package dependencies are not recursively built by `cstow install` yet.
- version-specific patches are merged into config but not applied during source builds.
- archive downloads, checksum verification, and manifest-driven ABI selection still need hardening.
- workspace builds are serial and do not model dependency ordering.

## Useful Docs

- `PLAN.md`: current roadmap and priorities
- `repo.md`: repository package definition format and long-form design
- `docs/superpowers/specs/2026-03-31-repository-system-design.md`
- `docs/superpowers/specs/2026-04-01-repository-core-design.md`
- `docs/superpowers/plans/2026-04-01-repository-core.md`

## Environment Variables

- `CSTOW_REGISTRY_KEY` / `CSTOW_REGISTRY_SECRET`
- `CSTOW_REGISTRY_URL`
- `CSTOW_CXX` / `CSTOW_CC`
- `CSTOW_SYSROOT`
- `CSTOW_CACHE_DIR`
- `CSTOW_CI`
- `AWS_PROFILE`
