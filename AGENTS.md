# CLAUDE.md

Symlinked to `AGENTS.md`. Update this file when agent-facing guidance changes.

## Project Snapshot

`cstow` is a Go CLI for C++ package and build workflows. Module: `github.com/all3n/cstow`, Go 1.25+.

Two coexisting flows (not yet fully unified):

- **Registry flow**: prebuilt artifacts published to / fetched from S3-compatible storage.
- **Repository flow**: package build recipes in repository directories, used by `cstow install` to build from source.

Do not mark a feature as "done" unless it is wired through the CLI and covered by tests.

## Build And Test

```bash
go build -o cstow .
```

### Test categories

| Category | Command | Requirements |
|----------|---------|-------------|
| Unit | `go test ./...` | Go only |
| Integration (install) | `go test -run TestIntegration ./cmd/` | CMake + C++ compiler |
| E2E (publish/fetch) | `go test -run TestE2E ./cmd/` | S3 registry + credentials |

Use targeted package tests while iterating (`go test ./internal/registry/...`), finish with `go test ./...`.

## Code Map

- `cmd/`
  - Commands: `init`, `build`, `add`, `fetch`, `publish`, `install`, `migrate`, `ci`, `workspace list`, `workspace build`, `check-abi`, `artifact list`, `artifact sync`, `artifact show`
  - `deps.go` — shared types (`repositoryInstallContext`) used by `fetch` and `install`
- `internal/config/`
  - Project config (`cstow.toml`) and user config (`~/.cstow/config.toml`)
- `internal/project/`
  - `cstow init` scaffolding
- `internal/toolchain/`
  - Compiler detection and CMake toolchain flag generation
- `internal/abi/`
  - ABI tag parsing, formatting, compatibility, and detection
- `internal/resolver/`
  - Dependency declaration mutation and lock-file generation
- `internal/registry/`
  - S3-compatible publish/download/manifest operations
- `internal/artifactdb/`
  - Local SQLite artifact index (`~/.cstow/cstow.db`): store, upsert, list, sync, hash_id lookup
- `internal/repository/`
  - Repository package definitions, version lookup, layered build config merge, source fetch
- `internal/builder/`
  - Source build/install execution for repository packages (CMake: static/shared/header-only)
- `internal/workspace/`
  - Workspace root discovery and member expansion
- `internal/hooks/`
  - Shell hook runner for lifecycle scripts
- `internal/legacy/`
  - CMake migration scanner and `cstow.toml` generation
- `internal/pack/`
  - `.tar.zst` creation/extraction

## Key Data Flow

```
add      → resolver → cstow.toml + cstow.lock
fetch    → registry (S3) → cache → cstow_deps/
         → artifactdb (SQLite) index
         → fallback: repository recipe → builder → cache → cstow_deps/
         → --artifact <hash>: fetchByHashID direct lookup
install  → repository recipe → builder (CMake) → cache → cstow_deps/
build    → CMake + cstow_deps/ → project build
publish  → pack → registry (S3) + artifactdb index
```

## Current Status

Follow the code as it exists today, not design docs. Key gaps to know about:

- `add` does not validate dependencies against repository or registry yet.
- `build` does not consume repository recipes; relies on `cstow_deps` and raw CMake.
- `fetch` supports manifest-based ABI/build_type matching, hash-based direct fetch (`--artifact`), and source-build fallback (`--source-fallback`).
- `install` merges repository config but does not recursively build recipe dependencies.
- `publish` supports both project-directory and direct local-artifact publish by `(name, version, abi_tag, build_type)`; populates `hash_id` and `build_tags` metadata.
- Fetch and publish use **decoupled registry client interfaces** (`fetchRegistryClient` / `publishRegistryClient`).
- `internal/repository/source.go` supports `git` sources; `archive` is a stub.
- `internal/builder/` supports CMake only (no make/autoconf/meson/custom).
- Dependency `build_type` flows through `cstow.toml`, `cstow.lock`, cache paths, and registry artifact keys; backward-compatible with legacy `<abi>` directories.
- Version-specific patches are merged into config but not applied during source builds.

## Key Command Flags

```
cstow fetch --artifact <hash>        # fetch by hash_id (or unique prefix)
cstow fetch --source-fallback=false  # disable source-build fallback
cstow fetch --toolchain <name>       # override compiler for ABI detection
cstow install --type static|shared   # override package build type
cstow install --force                # rebuild even if cached
cstow publish --build-tag key=val    # attach build tag metadata (repeatable)
cstow publish --version <ver>        # required for local-artifact mode
cstow add --build-type static|shared # set dependency build type
cstow build --profile debug|release  # build profile (default: debug)
cstow build --toolchain auto|gcc|clang  # compiler selection
```

## Code Style

- Follow standard Go conventions; `gofmt` formatting.
- Wrap errors with `fmt.Errorf("context: %w", err)`.
- Command packages use interface-based registry clients for testability (`fetchRegistryClient`, `publishRegistryClient`).
- Test files: `*_test.go` for unit, `*_integration_test.go` for integration, `*_e2e_test.go` for end-to-end.
- Flag reset helpers (e.g. `resetFetchFlagState`) prevent cross-test flag pollution.

## Working Rules

- Keep project-build flow and source-build flow conceptually separate unless explicitly unifying them.
- Before changing dependency behavior, inspect the interaction among `cmd/add.go`, `cmd/fetch.go`, `cmd/install.go`, `cmd/build.go`, `internal/resolver/`, and `internal/repository/`.
- If you change TOML schema or repository semantics, update tests in the same package.
- Prefer extending existing tests over only changing docs.
- Treat `PLAN.md` as the execution roadmap and keep it aligned with the actual implementation state.
- If a design doc conflicts with the code, either update the doc or clearly note that the code is still incomplete.

## Environment Variables

- `CSTOW_REGISTRY_KEY` / `CSTOW_REGISTRY_SECRET` — S3 credentials
- `CSTOW_REGISTRY_URL` — registry endpoint
- `CSTOW_CXX` / `CSTOW_CC` — override compiler detection
- `CSTOW_SYSROOT` — cross-compilation sysroot
- `CSTOW_CACHE_DIR` — override default cache location
- `CSTOW_CI` — CI mode flag
- `AWS_PROFILE` — AWS credential profile

## Useful Docs

- `PLAN.md` — current roadmap and priorities (in Chinese)
- `repo.md` — repository package definition format and long-form design
- `docs/superpowers/specs/2026-03-31-repository-system-design.md`
- `docs/superpowers/specs/2026-04-01-repository-core-design.md`
- `docs/superpowers/plans/2026-04-01-repository-core.md`
