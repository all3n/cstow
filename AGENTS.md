# AGENTS.md (主 PROMPT)

这是项目的**主 PROMPT** (Main Prompt) 文件。所有 AI Agent 在本项目中的行为准则、架构理解和开发规范都必须**绝对服从**本文件的定义。

## 🎯 核心开发纪律 (Core Rules)

1. **MVP 迭代驱动 (MVP-Driven)**：每次功能迭代都必须是一个最小可行性产品 (MVP)。不追求大而全，而是追求小而完整。
2. **完整测试 (Fully Testable)**：每个 MVP 功能必须具备可测试性，并且在提交前必须经过完整的验证（包括单元测试/集成测试，以及真实的 CLI 路径走通）。
3. **单次提交 (Atomic Commits)**：**完整测好功能，就提交一次**。严禁在功能未测试闭环前提交破碎的代码，严禁把一个独立的功能拆成毫无意义的多次微小 commit。
4. **文档同步 (Docs as Code)**：如果代码实现偏离了设计文档，必须同步修改文档（重点是本文件及 `PLAN.md`），保持代码和提示词的语义一致。

## Project Snapshot

`cstow` is a Go CLI for C++ package and build workflows. Module: `github.com/all3n/cstow`, Go 1.25+.

Three coexisting flows:

- **Registry flow**: prebuilt artifacts published to / fetched from S3-compatible storage.
- **Repository flow**: package build recipes in repository directories, used by `cstow install` to build from source.
- **Git flow**: direct Git repository dependencies with CMake build options, declared in `cstow.toml`.

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
  - Commands: `init`, `build`, `add`, `fetch`, `publish`, `install`, `migrate`, `ci`, `workspace`, `check-abi`, `artifact`
  - `fetch.go` — downloads prebuilt artifacts, uses manifest metadata for ABI/build_type matching, falls back to source builds (git or repository), and indexes outcomes in artifact DB.
  - `deps.go` — builds repository packages and git sources from source, and indexes results in artifact DB.
  - `artifact.go` — exposes artifact list/sync/show commands; SQLite is a query index over the cache.
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
  - Repository package definitions, version lookup, layered build config merge, source fetch (git/archive)
- `internal/builder/`
  - Source build/install execution (CMake: static/shared/header-only), patch application
- `internal/workspace/`
  - Workspace root discovery and member expansion, topological sort
- `internal/hooks/`
  - Shell hook runner for lifecycle scripts
- `internal/legacy/`
  - CMake migration scanner and `cstow.toml` generation
- `internal/pack/`
  - `.tar.zst` creation/extraction

## Key Data Flow

```
add      → resolver → cstow.toml + cstow.lock (registry/git/local/repository)
fetch    → registry (S3) → cache → cstow_deps/
         → artifactdb (SQLite) index
         → git source → builder → cache → cstow_deps/
         → fallback: repository recipe → builder → cache → cstow_deps/
         → --artifact <hash>: fetchByHashID direct lookup
install  → repository recipe → builder (CMake) → cache → cstow_deps/
         → git source → builder → cache → cstow_deps/
build    → CMake + cstow_deps/ → project build
publish  → pack → registry (S3) + artifactdb index
```

## Current Status

Follow the code as it exists today, not design docs. Key capabilities:

- `add` supports `--source registry|git|local|repository`. Git source supports `--git-url`, `--tag`, and `--cmake-define`.
- `build` supports `--fetch` for automatic dependency补全.
- `fetch` supports manifest-based ABI/build_type matching, hash-based direct fetch (`--artifact`), and source-build fallback (git/archive/repository).
- `install` supports git sources and repository recipes with recursive dependency resolution.
- `publish` populates `hash_id` and `build_tags` metadata in manifests.
- `internal/repository/source.go` supports both `git` and `archive` (.tar.gz, .zip) sources.
- `internal/builder/` supports CMake only, handles patch application before build.
- Workspace supports topological build order and parallel building (`--jobs`).
- Local artifact DB (`~/.cstow/cstow.db`) indexes all successful builds and fetches.


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
- `README.md` — repository package definition format and long-form design
- `docs/superpowers/specs/2026-03-31-repository-system-design.md`
- `docs/superpowers/specs/2026-04-01-repository-core-design.md`
- `docs/superpowers/plans/2026-04-01-repository-core.md`
